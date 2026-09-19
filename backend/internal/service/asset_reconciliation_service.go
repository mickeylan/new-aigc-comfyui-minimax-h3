package service

import (
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
				if err := rewriteSceneNameReferences(tx, projectID, d.EntityType, d.ProposedName, targetName); err != nil {
					return err
				}
			default:
				return fmt.Errorf("invalid reconciliation action %q", d.Action)
			}
		}
		return nil
	})
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

func rewriteSceneNameReferences(tx *gorm.DB, projectID uint, entityType, from, to string) error {
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
		case "location":
			if normalizeCanonName(sc.LocationName) == normalizeCanonName(from) {
				updates["location_name"] = to
			}
		case "prop":
			if replaced, changed := replaceNameToken(sc.Props, from, to); changed {
				updates["props"] = replaced
			}
		}
		if len(updates) > 0 {
			updates["prompt_stale"] = true
			updates["image_task_id"], updates["image_file"] = "", ""
			updates["video_task_id"], updates["video_file"], updates["video_input_file"] = "", "", ""
			updates["video_gpu"], updates["status"] = nil, "pending"
			if err := tx.Model(sc).Updates(updates).Error; err != nil {
				return err
			}
			if err := MarkSceneCandidatesStale(tx, projectID, sc.ID, "", "资产对账已改变权威引用"); err != nil {
				return err
			}
			var dependents []models.SceneContinuity
			if err := tx.Where("source_scene_id = ? AND mode != ?", sc.ID, models.ContinuityModeIndependent).Find(&dependents).Error; err != nil {
				return err
			}
			for _, dependency := range dependents {
				if err := tx.Model(&dependency).Updates(map[string]any{"status": "source_invalidated", "error": "上游资产对账已变化"}).Error; err != nil {
					return err
				}
				if err := invalidateSceneVideoTx(tx, projectID, dependency.SceneID, "上游资产对账已变化"); err != nil {
					return err
				}
			}
		}
	}
	if entityType == "character" {
		var dialogues []models.Dialogue
		if err := tx.Where("project_id = ?", projectID).Find(&dialogues).Error; err != nil {
			return err
		}
		for _, dialogue := range dialogues {
			if normalizeCanonName(dialogue.Character) == normalizeCanonName(from) {
				if err := tx.Model(&dialogue).Updates(map[string]any{
					"character": to, "audio_stale": true, "audio_stale_reason": "角色资产对账已更新说话者身份", "status": "pending",
				}).Error; err != nil {
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
	changed := false
	out := make([]string, 0, len(parts))
	for _, part := range parts {
		name := strings.TrimSpace(part)
		if name == "" {
			continue
		}
		if normalizeCanonName(name) == normalizeCanonName(from) {
			name, changed = to, true
		}
		out = append(out, name)
	}
	if !changed {
		return value, false
	}
	return strings.Join(out, ","), true
}
