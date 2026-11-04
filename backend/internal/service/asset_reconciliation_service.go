package service

import (
	"encoding/json"
	"fmt"
	"strings"
	"unicode"

	"comfyui-console/internal/models"
	"gorm.io/gorm"
)

type AssetReconciliationSuggestion struct {
	EntityType   string `json:"entity_type"`
	ProposedName string `json:"proposed_name"`
	MatchType    string `json:"match_type"` // exact, alias, or unmatched
	TargetID     uint   `json:"target_id,omitempty"`
	TargetName   string `json:"target_name,omitempty"`
	Recommended  string `json:"recommended"` // merge, create-local, or skip
}

type AssetReconciliationDecision struct {
	EntityType   string `json:"entity_type"`
	ProposedName string `json:"proposed_name"`
	Action       string `json:"action"`
	TargetID     uint   `json:"target_id,omitempty"`
}

type AssetReconciliationService struct{ db *gorm.DB }

func NewAssetReconciliationService(db *gorm.DB) *AssetReconciliationService {
	return &AssetReconciliationService{db: db}
}

func normalizeCanonName(s string) string {
	return strings.Map(func(r rune) rune {
		if unicode.IsLetter(r) || unicode.IsNumber(r) {
			return unicode.ToLower(r)
		}
		return -1
	}, strings.TrimSpace(s))
}

func (s *AssetReconciliationService) Suggest(projectID uint, proposed map[string][]string) ([]AssetReconciliationSuggestion, error) {
	var chars []models.Character
	var assets []models.Asset
	var aliases []models.CharacterAliasCandidate
	if err := s.db.Where("project_id = ?", projectID).Find(&chars).Error; err != nil {
		return nil, err
	}
	if err := s.db.Where("project_id = ?", projectID).Find(&assets).Error; err != nil {
		return nil, err
	}
	if err := s.db.Where("project_id = ? AND status = ?", projectID, "confirmed").Find(&aliases).Error; err != nil {
		return nil, err
	}
	type canon struct {
		id   uint
		name string
	}
	canonByType := map[string]map[string]canon{"character": {}, "location": {}, "prop": {}}
	for _, ch := range chars {
		canonByType["character"][normalizeCanonName(ch.Name)] = canon{ch.ID, ch.Name}
	}
	for _, a := range assets {
		canonByType[a.Kind][normalizeCanonName(a.Name)] = canon{a.ID, a.Name}
	}
	aliasMap := map[string]canon{}
	for _, a := range aliases {
		if target, ok := canonByType["character"][normalizeCanonName(a.CanonicalName)]; ok {
			aliasMap[normalizeCanonName(a.Alias)] = target
		}
	}
	var out []AssetReconciliationSuggestion
	seen := map[string]bool{}
	for _, entityType := range []string{"character", "location", "prop"} {
		for _, raw := range proposed[entityType] {
			name, norm := strings.TrimSpace(raw), normalizeCanonName(raw)
			if norm == "" || seen[entityType+":"+norm] {
				continue
			}
			seen[entityType+":"+norm] = true
			suggestion := AssetReconciliationSuggestion{EntityType: entityType, ProposedName: name, MatchType: "unmatched", Recommended: "create-local"}
			if target, ok := canonByType[entityType][norm]; ok {
				suggestion.MatchType, suggestion.TargetID, suggestion.TargetName, suggestion.Recommended = "exact", target.id, target.name, "skip"
			} else if entityType == "character" {
				if target, ok := aliasMap[norm]; ok {
					suggestion.MatchType, suggestion.TargetID, suggestion.TargetName, suggestion.Recommended = "alias", target.id, target.name, "merge"
				}
			}
			out = append(out, suggestion)
		}
	}
	return out, nil
}

func (s *AssetReconciliationService) Apply(projectID uint, decisions []AssetReconciliationDecision) error {
	return s.db.Transaction(func(tx *gorm.DB) error {
		for _, d := range decisions {
			d.EntityType = strings.TrimSpace(strings.ToLower(d.EntityType))
			d.ProposedName = strings.TrimSpace(d.ProposedName)
			if normalizeCanonName(d.ProposedName) == "" {
				return fmt.Errorf("proposed_name is required")
			}
			switch d.Action {
			case "skip":
				continue
			case "create-local":
				if err := createLocalCanon(tx, projectID, d.EntityType, d.ProposedName); err != nil {
					return err
				}
			case "merge":
				targetName, err := reconciliationTargetName(tx, projectID, d.EntityType, d.TargetID)
				if err != nil {
					return err
				}
				if d.EntityType == "character" && normalizeCanonName(d.ProposedName) != normalizeCanonName(targetName) {
					alias := models.CharacterAliasCandidate{ProjectID: projectID, CanonicalName: targetName, Alias: d.ProposedName, Status: "confirmed"}
					if err := tx.Where("project_id = ? AND alias = ?", projectID, d.ProposedName).Assign(map[string]any{"canonical_name": targetName, "status": "confirmed"}).FirstOrCreate(&alias).Error; err != nil {
						return err
					}
				}
				if d.EntityType == "character" && normalizeCanonName(d.ProposedName) != normalizeCanonName(targetName) {
					if err := mergeDuplicateCharacterRecord(tx, projectID, d.ProposedName, d.TargetID); err != nil {
						return err
					}
				}
				if err := rewriteSceneNameReferences(tx, projectID, d.EntityType, d.ProposedName, targetName, false); err != nil {
					return err
				}
			default:
				return fmt.Errorf("invalid reconciliation action %q", d.Action)
			}
		}
		return nil
	})
}

func mergeDuplicateCharacterRecord(tx *gorm.DB, projectID uint, sourceName string, targetID uint) error {
	var target models.Character
	if err := tx.Where("id = ? AND project_id = ?", targetID, projectID).First(&target).Error; err != nil {
		return err
	}
	var characters []models.Character
	if err := tx.Where("project_id = ?", projectID).Find(&characters).Error; err != nil {
		return err
	}
	var source *models.Character
	for i := range characters {
		if characters[i].ID != target.ID && normalizeCanonName(characters[i].Name) == normalizeCanonName(sourceName) {
			source = &characters[i]
			break
		}
	}
	if source == nil {
		return nil // the proposed name was only prose/alias, not a duplicate Character row
	}

	// The selected target is authoritative. Source values only fill missing target fields;
	// reviewed target assets and profile facts are never overwritten.
	updates := map[string]any{}
	fill := func(column, current, fallback string) {
		if strings.TrimSpace(current) == "" && strings.TrimSpace(fallback) != "" {
			updates[column] = fallback
		}
	}
	fill("role", target.Role, source.Role)
	fill("gender", target.Gender, source.Gender)
	fill("trait", target.Trait, source.Trait)
	fill("style", target.Style, source.Style)
	// Asset projections have associated engine/task/error state. Adopt that state as a
	// unit only when the target has no selected asset; mixing it with target state can
	// make a completed source task appear to own the target's file (or vice versa).
	if strings.TrimSpace(target.Portrait) == "" && strings.TrimSpace(source.Portrait) != "" {
		updates["portrait"] = source.Portrait
		updates["portrait_engine"] = source.PortraitEngine
		updates["portrait_task_id"] = source.PortraitTaskID
		updates["portrait_error"] = source.PortraitError
	}
	if strings.TrimSpace(target.Sheet) == "" && strings.TrimSpace(source.Sheet) != "" {
		updates["sheet"] = source.Sheet
		updates["sheet_task_id"] = source.SheetTaskID
		updates["sheet_error"] = source.SheetError
	}
	// Voice enrollment fields describe one profile and must not be combined from
	// different characters. Only an entirely empty target voice profile is adopted.
	if strings.TrimSpace(target.Voice) == "" && strings.TrimSpace(target.VoiceRef) == "" && strings.TrimSpace(target.VoiceID) == "" && strings.TrimSpace(target.VoiceModel) == "" {
		fill("voice", target.Voice, source.Voice)
		fill("voice_ref", target.VoiceRef, source.VoiceRef)
		fill("voice_id", target.VoiceID, source.VoiceID)
		fill("voice_model", target.VoiceModel, source.VoiceModel)
	}
	fill("appearance", target.Appearance, source.Appearance)
	fill("personality", target.Personality, source.Personality)
	fill("background", target.Background, source.Background)
	fill("relationships", target.Relationships, source.Relationships)
	fill("emotions", target.Emotions, source.Emotions)
	fill("habits", target.Habits, source.Habits)
	fill("wardrobe_detail", target.WardrobeDetail, source.WardrobeDetail)
	fill("lighting_mood", target.LightingMood, source.LightingMood)
	fill("color_palette", target.ColorPalette, source.ColorPalette)
	fill("reference_prompt", target.ReferencePrompt, source.ReferencePrompt)
	fill("review_note", target.ReviewNote, source.ReviewNote)
	if target.ProfileStatus != models.ProfileStatusApproved && source.ProfileStatus == models.ProfileStatusApproved {
		updates["profile_status"] = source.ProfileStatus
	}
	if source.ProfileVersion > target.ProfileVersion {
		updates["profile_version"] = source.ProfileVersion
	}
	if len(updates) > 0 {
		if err := tx.Model(&target).Updates(updates).Error; err != nil {
			return err
		}
	}

	// Preserve all dependent production data while collapsing the duplicate identity.
	if reconciliationHasTable(tx, "scene_character_outfits") {
		if err := tx.Exec("DELETE FROM scene_character_outfits WHERE character_id = ? AND EXISTS (SELECT 1 FROM scene_character_outfits t2 WHERE t2.character_id = ? AND scene_character_outfits.scene_id = t2.scene_id)", source.ID, target.ID).Error; err != nil {
			return err
		}
		if err := tx.Table("scene_character_outfits").Where("character_id = ?", source.ID).Update("character_id", target.ID).Error; err != nil {
			return err
		}
	}
	if reconciliationHasTable(tx, "shot_character_outfits") {
		if err := tx.Exec("DELETE FROM shot_character_outfits WHERE character_id = ? AND EXISTS (SELECT 1 FROM shot_character_outfits t2 WHERE t2.character_id = ? AND shot_character_outfits.shot_id = t2.shot_id)", source.ID, target.ID).Error; err != nil {
			return err
		}
		if err := tx.Table("shot_character_outfits").Where("character_id = ?", source.ID).Update("character_id", target.ID).Error; err != nil {
			return err
		}
	}
	for _, table := range []string{"character_looks", "character_outfits", "character_motion_references"} {
		if reconciliationHasTable(tx, table) {
			if err := tx.Table(table).Where("character_id = ?", source.ID).Update("character_id", target.ID).Error; err != nil {
				return fmt.Errorf("迁移%s失败（可能存在同名资产，请先重命名冲突项）: %w", table, err)
			}
		}
	}
	if reconciliationHasTable(tx, "screen_text_cues") {
		if err := tx.Table("screen_text_cues").Where("character_id = ?", source.ID).Update("character_id", target.ID).Error; err != nil {
			return err
		}
	}
	if reconciliationHasTable(tx, "asset_variants") {
		// Materialize both current projections before deleting the source row. Older
		// databases may have no variant for files selected before history was added.
		for _, asset := range []struct {
			entityType string
			targetFile string
			sourceFile string
		}{
			{VariantCharacterPortrait, target.Portrait, source.Portrait},
			{VariantCharacterSheet, target.Sheet, source.Sheet},
		} {
			if err := preserveMergedCharacterVariant(tx, projectID, target.ID, asset.entityType, asset.targetFile, true); err != nil {
				return err
			}
			adopted := strings.TrimSpace(asset.targetFile) == "" && strings.TrimSpace(asset.sourceFile) != ""
			var retained int64
			if err := tx.Model(&models.AssetVariant{}).Where("project_id = ? AND entity_type = ? AND entity_id IN ? AND file = ?", projectID, asset.entityType, []uint{source.ID, target.ID}, asset.sourceFile).Count(&retained).Error; err != nil {
				return err
			}
			if retained == 0 {
				if err := preserveMergedCharacterVariant(tx, projectID, target.ID, asset.entityType, asset.sourceFile, adopted); err != nil {
					return err
				}
			}
		}
		// If both identities already point at the same physical file, merging ownership
		// would create duplicate history rows. Fold source flags into the existing target
		// row and delete only that redundant database row; the media file remains intact.
		var sourceVariants []models.AssetVariant
		if err := tx.Where("project_id = ? AND entity_id = ? AND entity_type IN ?", projectID, source.ID, []string{VariantCharacterPortrait, VariantCharacterSheet}).Find(&sourceVariants).Error; err != nil {
			return err
		}
		for _, sourceVariant := range sourceVariants {
			var targetVariant models.AssetVariant
			err := tx.Where("project_id = ? AND entity_type = ? AND entity_id = ? AND file = ?", projectID, sourceVariant.EntityType, target.ID, sourceVariant.File).Order("id").First(&targetVariant).Error
			if err == gorm.ErrRecordNotFound {
				continue
			}
			if err != nil {
				return err
			}
			updates := map[string]any{"selected": targetVariant.Selected || sourceVariant.Selected, "favorite": targetVariant.Favorite || sourceVariant.Favorite}
			if strings.TrimSpace(targetVariant.Prompt) == "" && strings.TrimSpace(sourceVariant.Prompt) != "" {
				updates["prompt"] = sourceVariant.Prompt
			}
			if strings.TrimSpace(targetVariant.Provenance) == "" && strings.TrimSpace(sourceVariant.Provenance) != "" {
				updates["provenance"] = sourceVariant.Provenance
			}
			if err := tx.Model(&targetVariant).Updates(updates).Error; err != nil {
				return err
			}
			if err := tx.Delete(&sourceVariant).Error; err != nil {
				return err
			}
		}
		if err := tx.Table("asset_variants").Where("project_id = ? AND entity_id = ? AND entity_type IN ?", projectID, source.ID, []string{VariantCharacterPortrait, VariantCharacterSheet}).Update("entity_id", target.ID).Error; err != nil {
			return err
		}
		// The target projection remains authoritative. Retained source files are
		// history, not a second selected version.
		for _, selected := range []struct{ entityType, file string }{
			{VariantCharacterPortrait, firstNonBlank(target.Portrait, source.Portrait)},
			{VariantCharacterSheet, firstNonBlank(target.Sheet, source.Sheet)},
		} {
			if err := tx.Model(&models.AssetVariant{}).Where("project_id = ? AND entity_type = ? AND entity_id = ?", projectID, selected.entityType, target.ID).Update("selected", false).Error; err != nil {
				return err
			}
			if strings.TrimSpace(selected.file) != "" {
				if err := tx.Model(&models.AssetVariant{}).Where("project_id = ? AND entity_type = ? AND entity_id = ? AND file = ?", projectID, selected.entityType, target.ID, selected.file).Update("selected", true).Error; err != nil {
					return err
				}
			}
		}
	}
	if err := rewriteMergedCharacterSceneReferences(tx, projectID, source, &target); err != nil {
		return err
	}
	if reconciliationHasTable(tx, "character_merge_redirects") {
		redirect := models.CharacterMergeRedirect{
			TargetCharacterID:       target.ID,
			SourcePortraitVariantID: mergedCharacterVariantID(tx, projectID, target.ID, VariantCharacterPortrait, source.Portrait, firstNonBlank(target.Portrait, source.Portrait)),
			SourceSheetVariantID:    mergedCharacterVariantID(tx, projectID, target.ID, VariantCharacterSheet, source.Sheet, firstNonBlank(target.Sheet, source.Sheet)),
		}
		if err := tx.Where("project_id = ? AND source_character_id = ?", projectID, source.ID).
			Assign(redirect).
			FirstOrCreate(&models.CharacterMergeRedirect{ProjectID: projectID, SourceCharacterID: source.ID}).Error; err != nil {
			return err
		}
	}
	if reconciliationHasTable(tx, "dialogues") {
		if err := tx.Model(&models.Dialogue{}).Where("project_id = ? AND character = ?", projectID, source.Name).Update("character", target.Name).Error; err != nil {
			return err
		}
	}
	if reconciliationHasTable(tx, "shots") && reconciliationHasTable(tx, "scenes") {
		if err := tx.Model(&models.Shot{}).Where("listener_character = ? AND scene_id IN (SELECT id FROM scenes WHERE project_id = ?)", source.Name, projectID).Update("listener_character", target.Name).Error; err != nil {
			return err
		}
	}
	if reconciliationHasTable(tx, "character_alias_candidates") {
		if err := tx.Model(&models.CharacterAliasCandidate{}).Where("project_id = ? AND canonical_name = ?", projectID, source.Name).Update("canonical_name", target.Name).Error; err != nil {
			return err
		}
	}
	return tx.Delete(&models.Character{}, source.ID).Error
}

func rewriteMergedCharacterSceneReferences(tx *gorm.DB, projectID uint, source, target *models.Character) error {
	var scenes []models.Scene
	if err := tx.Where("project_id = ? AND reference_images_json != ''", projectID).Find(&scenes).Error; err != nil {
		return err
	}
	variantID := func(variant string) uint {
		file, entityType := source.Portrait, VariantCharacterPortrait
		targetFile := firstNonBlank(target.Portrait, source.Portrait)
		if variant == "sheet" {
			file, entityType, targetFile = source.Sheet, VariantCharacterSheet, firstNonBlank(target.Sheet, source.Sheet)
		}
		if strings.TrimSpace(file) == "" || file == targetFile {
			return 0
		}
		var row models.AssetVariant
		if err := tx.Where("project_id = ? AND entity_type = ? AND entity_id = ? AND file = ?", projectID, entityType, target.ID, file).First(&row).Error; err == nil {
			return row.ID
		}
		return 0
	}
	for i := range scenes {
		refs, ok := parseSceneReferences(&scenes[i])
		if !ok {
			continue
		}
		changed := false
		merged := make([]SceneReferenceSelection, 0, len(refs))
		indexes := map[string]int{}
		for _, ref := range refs {
			if ref.SourceType == "character" && ref.SourceID == source.ID {
				ref.SourceID, changed = target.ID, true
				ref.AssetVariantID = variantID(ref.Variant)
			}
			key := referenceKey(ref)
			if at, exists := indexes[key]; exists {
				merged[at].UseKrea2 = merged[at].UseKrea2 || ref.UseKrea2
				merged[at].UseH3 = merged[at].UseH3 || ref.UseH3
				if merged[at].BindCharacter == "" {
					merged[at].BindCharacter = ref.BindCharacter
				}
				changed = true
				continue
			}
			indexes[key] = len(merged)
			merged = append(merged, ref)
		}
		if !changed {
			continue
		}
		encoded, err := json.Marshal(merged)
		if err != nil {
			return err
		}
		if err := tx.Model(&models.Scene{}).Where("id = ? AND project_id = ?", scenes[i].ID, projectID).Update("reference_images_json", string(encoded)).Error; err != nil {
			return err
		}
	}
	return nil
}

func reconciliationHasTable(tx *gorm.DB, table string) bool {
	// GORM's SQLite migrator opens a separate connection; for an in-memory DB that
	// connection cannot see the transaction's schema. Querying sqlite_master keeps
	// optional-table checks transaction-local. Other dialects use their migrator.
	if tx.Dialector.Name() == "sqlite" {
		var count int64
		return tx.Raw("SELECT count(*) FROM sqlite_master WHERE type = 'table' AND name = ?", table).Scan(&count).Error == nil && count > 0
	}
	return tx.Migrator().HasTable(table)
}

func mergedCharacterVariantID(tx *gorm.DB, projectID, entityID uint, entityType, sourceFile, currentFile string) uint {
	if strings.TrimSpace(sourceFile) == "" || sourceFile == currentFile {
		return 0
	}
	var variant models.AssetVariant
	if err := tx.Where("project_id = ? AND entity_type = ? AND entity_id = ? AND file = ?", projectID, entityType, entityID, sourceFile).First(&variant).Error; err != nil {
		return 0
	}
	return variant.ID
}

func preserveMergedCharacterVariant(tx *gorm.DB, projectID, entityID uint, entityType, file string, selected bool) error {
	file = strings.TrimSpace(file)
	if file == "" {
		return nil
	}
	var row models.AssetVariant
	return tx.Where("project_id = ? AND entity_type = ? AND entity_id = ? AND file = ?", projectID, entityType, entityID, file).
		Attrs(models.AssetVariant{ProjectID: projectID, EntityType: entityType, EntityID: entityID, File: file, Selected: selected}).FirstOrCreate(&row).Error
}

func firstNonBlank(primary, fallback string) string {
	if strings.TrimSpace(primary) != "" {
		return primary
	}
	return fallback
}

func createLocalCanon(tx *gorm.DB, projectID uint, entityType, name string) error {
	var count int64
	norm := normalizeCanonName(name)
	switch entityType {
	case "character":
		var rows []models.Character
		if err := tx.Where("project_id = ?", projectID).Find(&rows).Error; err != nil {
			return err
		}
		for _, row := range rows {
			if normalizeCanonName(row.Name) == norm {
				return nil
			}
		}
		return tx.Create(&models.Character{ProjectID: projectID, Name: name, Source: "manual"}).Error
	case "location", "prop":
		var rows []models.Asset
		if err := tx.Where("project_id = ? AND kind = ?", projectID, entityType).Find(&rows).Error; err != nil {
			return err
		}
		for _, row := range rows {
			if normalizeCanonName(row.Name) == norm {
				return nil
			}
		}
		return tx.Create(&models.Asset{ProjectID: projectID, Kind: entityType, Name: name, Source: "manual"}).Error
	default:
		_ = count
		return fmt.Errorf("invalid entity_type %q", entityType)
	}
}

func reconciliationTargetName(tx *gorm.DB, projectID uint, entityType string, targetID uint) (string, error) {
	switch entityType {
	case "character":
		var row models.Character
		err := tx.Where("id = ? AND project_id = ?", targetID, projectID).First(&row).Error
		return row.Name, err
	case "location", "prop":
		var row models.Asset
		err := tx.Where("id = ? AND project_id = ? AND kind = ?", targetID, projectID, entityType).First(&row).Error
		return row.Name, err
	default:
		return "", fmt.Errorf("invalid entity_type %q", entityType)
	}
}

func rewriteSceneNameReferences(tx *gorm.DB, projectID uint, entityType, from, to string, renameOnly bool) error {
	var scenes []models.Scene
	if err := tx.Where("project_id = ?", projectID).Find(&scenes).Error; err != nil {
		return err
	}
	for i := range scenes {
		updates := map[string]any{}
		sc := &scenes[i]
		switch entityType {
		case "character":
			for field, value := range map[string]string{"characters": sc.Characters, "visible_characters": sc.VisibleCharacters, "voice_characters": sc.VoiceCharacters, "mentioned_characters": sc.MentionedCharacters} {
				if replaced, changed := replaceNameToken(value, from, to); changed {
					updates[field] = replaced
				}
			}
			if blocking, changed := rewriteBlockingCharacterNames(sc.BlockingJSON, from, to); changed {
				updates["blocking_json"] = blocking
			}
		case "location":
			if normalizeCanonName(sc.LocationName) == normalizeCanonName(from) {
				updates["location_name"] = to
			}
		case "prop":
			if replaced, changed := replaceNameToken(sc.Props, from, to); changed {
				updates["props"] = replaced
			}
		}
		if entityType == "character" && strings.TrimSpace(sc.ReferenceImagesJSON) != "" {
			if refs, ok := parseSceneReferences(sc); ok {
				changed := false
				for i := range refs {
					if normalizeCanonName(refs[i].BindCharacter) == normalizeCanonName(from) {
						refs[i].BindCharacter, changed = to, true
					}
				}
				if changed {
					encoded, err := json.Marshal(refs)
					if err != nil {
						return err
					}
					updates["reference_images_json"] = string(encoded)
				}
			}
		}
		if len(updates) > 0 {
			// Name changes preserve approved English prompts verbatim; stale is advisory only.
			updates["prompt_stale"] = true
			if err := tx.Model(sc).Updates(updates).Error; err != nil {
				return err
			}
		}
	}
	if entityType == "character" {
		if reconciliationHasTable(tx, "shots") && reconciliationHasTable(tx, "scenes") {
			var shots []models.Shot
			if err := tx.Where("scene_id IN (SELECT id FROM scenes WHERE project_id = ?)", projectID).Find(&shots).Error; err != nil {
				return err
			}
			for i := range shots {
				updates := map[string]any{}
				for column, value := range map[string]string{"prompt_subject": shots[i].PromptSubject, "prompt_action": shots[i].PromptAction, "start_state": shots[i].StartState, "end_state": shots[i].EndState, "description": shots[i].Description, "emotion": shots[i].Emotion} {
					if replaced := strings.ReplaceAll(value, from, to); replaced != value {
						updates[column] = replaced
					}
				}
				if normalizeCanonName(shots[i].ListenerCharacter) == normalizeCanonName(from) {
					updates["listener_character"] = to
				}
				if len(updates) > 0 {
					if err := tx.Model(&shots[i]).Updates(updates).Error; err != nil {
						return err
					}
				}
			}
		}
		var dialogues []models.Dialogue
		if err := tx.Where("project_id = ?", projectID).Find(&dialogues).Error; err != nil {
			return err
		}
		for _, dialogue := range dialogues {
			if normalizeCanonName(dialogue.Character) == normalizeCanonName(from) {
				updates := map[string]any{"character": to}
				if !renameOnly {
					updates["audio_stale"], updates["audio_stale_reason"], updates["status"] = true, "角色资产对账已更新说话者身份", "pending"
				}
				if err := tx.Model(&dialogue).Updates(updates).Error; err != nil {
					return err
				}
			}
		}
	}
	return nil
}

func replaceNameToken(value, from, to string) (string, bool) {
	if strings.TrimSpace(value) == "" {
		return value, false
	}
	parts := strings.FieldsFunc(value, func(r rune) bool { return r == ',' || r == '，' || r == ';' || r == '；' || r == '\n' })
	changed, seen := false, map[string]bool{}
	out := make([]string, 0, len(parts))
	for _, part := range parts {
		name := strings.TrimSpace(part)
		if name == "" {
			continue
		}
		if normalizeCanonName(name) == normalizeCanonName(from) {
			name, changed = to, true
		}
		norm := normalizeCanonName(name)
		if seen[norm] {
			changed = true
			continue
		}
		seen[norm] = true
		out = append(out, name)
	}
	if !changed {
		return value, false
	}
	return strings.Join(out, ","), true
}

func rewriteBlockingCharacterNames(raw, from, to string) (string, bool) {
	if strings.TrimSpace(raw) == "" {
		return raw, false
	}
	var blocking SceneBlocking
	if err := json.Unmarshal([]byte(raw), &blocking); err != nil {
		return raw, false
	}
	changed, count := false, 0
	for i := range blocking.Characters {
		if normalizeCanonName(blocking.Characters[i].Name) == normalizeCanonName(from) {
			blocking.Characters[i].Name, changed = to, true
		}
		if normalizeCanonName(blocking.Characters[i].Name) == normalizeCanonName(to) {
			count++
		}
	}
	if !changed {
		return raw, false
	}
	if count > 1 {
		blocking.Suggestions = append(blocking.Suggestions, "身份合并后同一角色存在多条站位记录；位置冲突已保留，请确认当前静止瞬间采用哪一条")
	}
	return blockingJSON(blocking), true
}
