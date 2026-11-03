package service

import (
	"testing"

	"comfyui-console/internal/models"
	"gorm.io/gorm"
)

func TestMergeDuplicateCharacterRetainsBothCurrentPortraitAndSheetInHistory(t *testing.T) {
	ps := newTestProjectService(t)
	if err := ps.db.AutoMigrate(&models.CharacterAliasCandidate{}, &models.AssetVariant{}, &models.Dialogue{}); err != nil {
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
	for _, row := range rows {
		files[row.File] = row
	}
	for _, file := range []string{target.Portrait, source.Portrait, target.Sheet, source.Sheet} {
		if _, ok := files[file]; !ok {
			t.Fatalf("merged current asset %q absent from actual history query: %+v", file, rows)
		}
	}
	if !files[target.Portrait].Selected || files[source.Portrait].Selected || !files[target.Sheet].Selected || files[source.Sheet].Selected {
		t.Fatalf("history selection does not match authoritative target: %+v", files)
	}
}

func TestMergeDuplicateCharacterEmptyTargetAdoptsConsistentAssetAndProfileState(t *testing.T) {
	ps := newTestProjectService(t)
	if err := ps.db.AutoMigrate(&models.CharacterAliasCandidate{}, &models.AssetVariant{}, &models.Dialogue{}, &models.CharacterLook{}, &models.CharacterOutfit{}); err != nil {
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
