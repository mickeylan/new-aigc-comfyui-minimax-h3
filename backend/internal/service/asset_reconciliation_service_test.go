package service

import (
	"encoding/json"
	"fmt"
	"strings"
	"testing"

	"comfyui-console/internal/models"
	"gorm.io/gorm"
)

func TestMergeDuplicateCharacterRetainsBothCurrentPortraitAndSheetInHistory(t *testing.T) {
	ps := newTestProjectService(t)
	if err := ps.db.AutoMigrate(&models.CharacterAliasCandidate{}, &models.AssetVariant{}, &models.CharacterMergeRedirect{}, &models.Dialogue{}); err != nil {
		t.Fatal(err)
	}
	if !reconciliationHasTable(ps.db, "asset_variants") {
		t.Fatal("asset_variants migration missing")
	}
	if err := ps.db.Transaction(func(tx *gorm.DB) error {
		if !reconciliationHasTable(tx, "asset_variants") {
			t.Fatal("asset_variants missing inside transaction")
		}
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	project := models.Project{Title: "merge history"}
	if err := ps.db.Create(&project).Error; err != nil {
		t.Fatal(err)
	}
	target := models.Character{ProjectID: project.ID, Name: "canonical", Portrait: "target-portrait.png", Sheet: "target-sheet.png", VoiceRef: "target.wav", VoiceID: "target-voice", VoiceModel: "target-model"}
	source := models.Character{ProjectID: project.ID, Name: "alias", Portrait: "source-portrait.png", Sheet: "source-sheet.png", VoiceRef: "source.wav", VoiceID: "source-voice", VoiceModel: "source-model"}
	if err := ps.db.Create(&target).Error; err != nil {
		t.Fatal(err)
	}
	if err := ps.db.Create(&source).Error; err != nil {
		t.Fatal(err)
	}

	if err := NewAssetReconciliationService(ps.db).Apply(project.ID, []AssetReconciliationDecision{{EntityType: "character", ProposedName: source.Name, Action: "merge", TargetID: target.ID}}); err != nil {
		t.Fatal(err)
	}
	var oldCount int64
	if err := ps.db.Model(&models.Character{}).Where("id = ?", source.ID).Count(&oldCount).Error; err != nil || oldCount != 0 {
		t.Fatalf("old character must be removed after inheritance: count=%d err=%v", oldCount, err)
	}
	var got models.Character
	if err := ps.db.First(&got, target.ID).Error; err != nil {
		t.Fatal(err)
	}
	if got.Portrait != target.Portrait || got.Sheet != target.Sheet || got.VoiceRef != target.VoiceRef || got.VoiceID != target.VoiceID || got.VoiceModel != target.VoiceModel {
		t.Fatalf("target selections/profile were overwritten: %+v", got)
	}
	rows, err := NewGeneratedMediaHistoryService(ps.db, nil).List(project.ID, "", "")
	if err != nil {
		t.Fatal(err)
	}
	files := map[string]GeneratedMediaHistoryItem{}
	fileCounts := map[string]int{}
	for _, row := range rows {
		files[row.File] = row
		fileCounts[row.File]++
	}
	for _, file := range []string{target.Portrait, source.Portrait, target.Sheet, source.Sheet} {
		if _, ok := files[file]; !ok {
			t.Fatalf("merged current asset %q absent from actual history query: %+v", file, rows)
		}
		if fileCounts[file] != 1 {
			t.Fatalf("merged asset %q displayed %d times: %+v", file, fileCounts[file], rows)
		}
	}
	if !files[target.Portrait].Selected || files[source.Portrait].Selected || !files[target.Sheet].Selected || files[source.Sheet].Selected {
		t.Fatalf("history selection does not match authoritative target: %+v", files)
	}
}

func TestMergeDuplicateCharacterEmptyTargetAdoptsConsistentAssetAndProfileState(t *testing.T) {
	ps := newTestProjectService(t)
	if err := ps.db.AutoMigrate(&models.CharacterAliasCandidate{}, &models.AssetVariant{}, &models.CharacterMergeRedirect{}, &models.Dialogue{}, &models.CharacterLook{}, &models.CharacterOutfit{}); err != nil {
		t.Fatal(err)
	}
	project := models.Project{Title: "merge inheritance"}
	ps.db.Create(&project)
	target := models.Character{ProjectID: project.ID, Name: "canonical"}
	source := models.Character{ProjectID: project.ID, Name: "alias", Portrait: "source.png", PortraitEngine: "qwen_image_2_1", PortraitTaskID: "portrait-task", PortraitError: "portrait-state", Sheet: "source-sheet.png", SheetTaskID: "sheet-task", SheetError: "sheet-state", Voice: "preset", VoiceRef: "voice.wav", VoiceID: "voice-id", VoiceModel: "voice-model"}
	ps.db.Create(&target)
	ps.db.Create(&source)
	look := models.CharacterLook{ProjectID: project.ID, CharacterID: source.ID, Name: "old look", Image: "look.png"}
	outfit := models.CharacterOutfit{ProjectID: project.ID, CharacterID: source.ID, Name: "old outfit", Image: "outfit.png"}
	ps.db.Create(&look)
	ps.db.Create(&outfit)

	if err := NewAssetReconciliationService(ps.db).Apply(project.ID, []AssetReconciliationDecision{{EntityType: "character", ProposedName: source.Name, Action: "merge", TargetID: target.ID}}); err != nil {
		t.Fatal(err)
	}
	var got models.Character
	ps.db.First(&got, target.ID)
	if got.Portrait != source.Portrait || got.PortraitEngine != source.PortraitEngine || got.PortraitTaskID != source.PortraitTaskID || got.PortraitError != source.PortraitError || got.Sheet != source.Sheet || got.SheetTaskID != source.SheetTaskID || got.SheetError != source.SheetError {
		t.Fatalf("asset projection state was not adopted consistently: %+v", got)
	}
	if got.Voice != source.Voice || got.VoiceRef != source.VoiceRef || got.VoiceID != source.VoiceID || got.VoiceModel != source.VoiceModel {
		t.Fatalf("voice profile was not adopted consistently: %+v", got)
	}
	ps.db.First(&look, look.ID)
	ps.db.First(&outfit, outfit.ID)
	if look.CharacterID != target.ID || outfit.CharacterID != target.ID {
		t.Fatalf("owned asset history not reassigned: look=%+v outfit=%+v", look, outfit)
	}
	var selected int64
	ps.db.Model(&models.AssetVariant{}).Where("project_id = ? AND entity_id = ? AND selected = ?", project.ID, target.ID, true).Count(&selected)
	if selected != 2 {
		t.Fatalf("adopted portrait and sheet should be selected history, got %d", selected)
	}
}

func TestMergedCharacterReferenceCandidatesDoNotDuplicateCurrentProjection(t *testing.T) {
	ps := newTestProjectService(t)
	if err := ps.db.AutoMigrate(&models.CharacterAliasCandidate{}, &models.AssetVariant{}, &models.CharacterMergeRedirect{}, &models.Dialogue{}); err != nil {
		t.Fatal(err)
	}
	project := models.Project{Title: "dedupe merged references"}
	ps.db.Create(&project)
	target := models.Character{ProjectID: project.ID, Name: "canonical", Portrait: "same-portrait.png", Sheet: "same-sheet.png"}
	source := models.Character{ProjectID: project.ID, Name: "alias", Portrait: "same-portrait.png", Sheet: "same-sheet.png"}
	ps.db.Create(&target)
	ps.db.Create(&source)
	for _, variant := range []models.AssetVariant{
		{ProjectID: project.ID, EntityType: VariantCharacterPortrait, EntityID: target.ID, File: target.Portrait, Selected: true},
		{ProjectID: project.ID, EntityType: VariantCharacterSheet, EntityID: target.ID, File: target.Sheet, Selected: true},
		{ProjectID: project.ID, EntityType: VariantCharacterPortrait, EntityID: source.ID, File: source.Portrait, Selected: true},
		{ProjectID: project.ID, EntityType: VariantCharacterSheet, EntityID: source.ID, File: source.Sheet, Selected: true},
	} {
		if err := ps.db.Create(&variant).Error; err != nil {
			t.Fatal(err)
		}
	}
	if err := NewAssetReconciliationService(ps.db).Apply(project.ID, []AssetReconciliationDecision{{EntityType: "character", ProposedName: source.Name, Action: "merge", TargetID: target.ID}}); err != nil {
		t.Fatal(err)
	}
	scene := models.Scene{ProjectID: project.ID}
	candidates := ps.sceneReferenceCandidates(&scene)
	files := map[string]int{}
	for _, candidate := range candidates {
		if candidate.SourceType == "character" && candidate.SourceID == target.ID {
			files[candidate.Image]++
		}
	}
	if files[source.Portrait] != 1 || files[source.Sheet] != 1 || len(files) != 2 {
		t.Fatalf("current portrait/sheet duplicated by history variants: candidates=%+v files=%+v", candidates, files)
	}
	var variants []models.AssetVariant
	ps.db.Where("project_id = ? AND entity_id = ?", project.ID, target.ID).Find(&variants)
	if len(variants) != 2 {
		t.Fatalf("duplicate physical assets survived merge: %+v", variants)
	}
	for _, variant := range variants {
		ref := SceneReferenceSelection{SourceType: "character", SourceID: target.ID, Variant: "portrait", AssetVariantID: variant.ID}
		if variant.EntityType == VariantCharacterSheet {
			ref.Variant = "sheet"
		}
		normalized := ps.normalizeSceneReferences(project.ID, []SceneReferenceSelection{ref})
		if len(normalized) != 1 || normalized[0].AssetVariantID != 0 {
			t.Fatalf("current projection variant did not collapse to base reference: variant=%+v normalized=%+v", variant, normalized)
		}
	}
}

func TestMergeDuplicateCharacterRewritesSceneReferencesAndPreservesSourceVariant(t *testing.T) {
	ps := newTestProjectService(t)
	if err := ps.db.AutoMigrate(&models.CharacterAliasCandidate{}, &models.AssetVariant{}, &models.CharacterMergeRedirect{}, &models.Dialogue{}); err != nil {
		t.Fatal(err)
	}
	project := models.Project{Title: "reference merge"}
	ps.db.Create(&project)
	target := models.Character{ProjectID: project.ID, Name: "canonical", Sheet: "target-sheet.png"}
	source := models.Character{ProjectID: project.ID, Name: "alias", Sheet: "source-sheet.png"}
	ps.db.Create(&target)
	ps.db.Create(&source)
	scene := models.Scene{ProjectID: project.ID, ReferenceImagesJSON: fmt.Sprintf(`[{"source_type":"character","source_id":%d,"variant":"sheet","use_krea2":true,"use_h3":false,"bind_character":"hero"},{"source_type":"character","source_id":%d,"variant":"sheet","use_krea2":false,"use_h3":true,"bind_character":"hero"}]`, source.ID, target.ID)}
	ps.db.Create(&scene)
	if err := NewAssetReconciliationService(ps.db).Apply(project.ID, []AssetReconciliationDecision{{EntityType: "character", ProposedName: source.Name, Action: "merge", TargetID: target.ID}}); err != nil {
		t.Fatal(err)
	}
	ps.db.First(&scene, scene.ID)
	var refs []SceneReferenceSelection
	if err := json.Unmarshal([]byte(scene.ReferenceImagesJSON), &refs); err != nil {
		t.Fatal(err)
	}
	if len(refs) != 2 {
		t.Fatalf("different files must remain distinct selections: %+v", refs)
	}
	var historical, current *SceneReferenceSelection
	for i := range refs {
		if refs[i].AssetVariantID != 0 {
			historical = &refs[i]
		} else {
			current = &refs[i]
		}
	}
	if historical == nil || current == nil || historical.SourceID != target.ID || current.SourceID != target.ID {
		t.Fatalf("references not canonically rewritten: %+v", refs)
	}
	if !historical.UseKrea2 || historical.UseH3 || current.UseKrea2 || !current.UseH3 {
		t.Fatalf("usage flags changed: %+v", refs)
	}
	var redirect models.CharacterMergeRedirect
	if err := ps.db.Where("project_id = ? AND source_character_id = ?", project.ID, source.ID).First(&redirect).Error; err != nil || redirect.TargetCharacterID != target.ID {
		t.Fatalf("redirect missing: %+v %v", redirect, err)
	}
	stale := ps.normalizeSceneReferences(project.ID, []SceneReferenceSelection{{SourceType: "character", SourceID: source.ID, Variant: "sheet", UseKrea2: true}})
	if len(stale) != 1 || stale[0].SourceID != target.ID || stale[0].AssetVariantID != historical.AssetVariantID {
		t.Fatalf("stale request not redirected to its original file: %+v", stale)
	}
	if err := ps.SaveSceneReferences(&scene, stale); err != nil {
		t.Fatalf("normalized stale request was rejected: %v", err)
	}
	matchedFile := ""
	for _, candidate := range ps.sceneReferenceCandidates(&scene) {
		if candidate.Key == referenceKey(stale[0]) {
			matchedFile = candidate.Image
			if !strings.HasPrefix(candidate.Label, "角色「canonical」四视图") {
				t.Fatalf("historical candidate has no target identity: %+v", candidate)
			}
		}
	}
	if matchedFile != source.Sheet {
		t.Fatalf("stale request silently changed files: got %q want %q", matchedFile, source.Sheet)
	}
	unknown := []SceneReferenceSelection{{SourceType: "character", SourceID: source.ID + 1000, Variant: "sheet", UseKrea2: true}}
	if err := ps.SaveSceneReferences(&scene, unknown); err == nil || !strings.Contains(err.Error(), "参考图不存在、未审核或不属于当前项目") {
		t.Fatalf("unknown deleted source should require manual resolution: %v", err)
	}
	valid, invalid := partitionSceneReferences(append(stale, unknown...), ps.sceneReferenceCandidates(&scene))
	if len(valid) != 1 || referenceKey(valid[0]) != referenceKey(stale[0]) || len(invalid) != 1 || referenceKey(invalid[0]) != referenceKey(unknown[0]) {
		t.Fatalf("historical invalid references were not isolated: valid=%+v invalid=%+v", valid, invalid)
	}
}
