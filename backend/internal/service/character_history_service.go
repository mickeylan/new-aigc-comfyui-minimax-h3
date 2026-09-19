package service

import (
	"sort"
	"strings"

	"comfyui-console/internal/models"
	"gorm.io/gorm"
)

// CharacterHistory is a read-only projection of project-owned production data.
// It is intentionally derived on request so edits to scenes, aliases and assets are reflected immediately.
type CharacterHistory struct {
	Character          CharacterHistoryCharacter `json:"character"`
	Aliases            []string                  `json:"aliases"`
	Episodes           []CharacterHistoryEpisode `json:"episodes"`
	LastApprovedLook   *CharacterHistoryLook     `json:"last_approved_look"`
	LastApprovedOutfit *CharacterHistoryOutfit   `json:"last_approved_outfit"`
	Props              []CharacterHistoryProp    `json:"props"`
	VoiceConfig        CharacterHistoryVoice     `json:"voice_config"`
	LastSeen           *CharacterHistoryLastSeen `json:"last_seen"`
}

type CharacterHistoryCharacter struct {
	ID   uint   `json:"id"`
	Name string `json:"name"`
}

type CharacterHistoryEpisode struct {
	Number int                     `json:"number"`
	Scenes []CharacterHistoryScene `json:"scenes"`
}

type CharacterHistoryScene struct {
	ID    uint                   `json:"id"`
	Order int                    `json:"order"`
	Title string                 `json:"title"`
	Props []CharacterHistoryProp `json:"props"`
	Shots []CharacterHistoryShot `json:"shots"`
}

type CharacterHistoryShot struct {
	ID    uint `json:"id"`
	Order int  `json:"order"`
}

type CharacterHistoryLook struct {
	ID          uint   `json:"id"`
	Name        string `json:"name"`
	AuditStatus string `json:"audit_status"`
}

type CharacterHistoryOutfit struct {
	ID          uint   `json:"id"`
	Name        string `json:"name"`
	AuditStatus string `json:"audit_status"`
}

type CharacterHistoryProp struct {
	ID   uint   `json:"id,omitempty"`
	Name string `json:"name"`
}

type CharacterHistoryVoice struct {
	Voice      string `json:"voice"`
	VoiceRef   string `json:"voice_ref"`
	VoiceID    string `json:"voice_id"`
	VoiceModel string `json:"voice_model"`
}

type CharacterHistoryLastSeen struct {
	EpisodeN   int    `json:"episode_n"`
	SceneID    uint   `json:"scene_id"`
	SceneOrder int    `json:"scene_order"`
	SceneTitle string `json:"scene_title"`
	ShotID     *uint  `json:"shot_id,omitempty"`
	ShotOrder  *int   `json:"shot_order,omitempty"`
}

type CharacterHistoryService struct{ db *gorm.DB }

func NewCharacterHistoryService(db *gorm.DB) *CharacterHistoryService {
	return &CharacterHistoryService{db: db}
}

// List returns all project characters, or one character when characterID is non-nil.
func (s *CharacterHistoryService) List(projectID uint, characterID *uint) ([]CharacterHistory, error) {
	var characters []models.Character
	q := s.db.Where("project_id = ?", projectID)
	if characterID != nil {
		q = q.Where("id = ?", *characterID)
	}
	if err := q.Order("id").Find(&characters).Error; err != nil {
		return nil, err
	}
	if characterID != nil && len(characters) == 0 {
		return nil, gorm.ErrRecordNotFound
	}

	var project models.Project
	if err := s.db.First(&project, projectID).Error; err != nil {
		return nil, err
	}
	var scenes []models.Scene
	if err := s.db.Where("project_id = ? AND generation = ?", projectID, project.Generation).Order("episode_n, `order`, id").Find(&scenes).Error; err != nil {
		return nil, err
	}
	sceneIDs := make([]uint, 0, len(scenes))
	for _, scene := range scenes {
		sceneIDs = append(sceneIDs, scene.ID)
	}

	var shots []models.Shot
	var dialogues []models.Dialogue
	var sceneLooks []models.SceneCharacterLook
	var sceneOutfits []models.SceneCharacterOutfit
	if len(sceneIDs) > 0 {
		if err := s.db.Where("scene_id IN ?", sceneIDs).Order("scene_id, order_num, id").Find(&shots).Error; err != nil {
			return nil, err
		}
		if err := s.db.Where("project_id = ? AND scene_id IN ?", projectID, sceneIDs).Find(&dialogues).Error; err != nil {
			return nil, err
		}
		if err := s.db.Where("scene_id IN ?", sceneIDs).Find(&sceneLooks).Error; err != nil {
			return nil, err
		}
		if err := s.db.Where("scene_id IN ?", sceneIDs).Find(&sceneOutfits).Error; err != nil {
			return nil, err
		}
	}
	shotIDs := make([]uint, 0, len(shots))
	for _, shot := range shots {
		shotIDs = append(shotIDs, shot.ID)
	}
	var shotLooks []models.ShotCharacterLook
	var shotOutfits []models.ShotCharacterOutfit
	if len(shotIDs) > 0 {
		if err := s.db.Where("shot_id IN ?", shotIDs).Find(&shotLooks).Error; err != nil {
			return nil, err
		}
		if err := s.db.Where("shot_id IN ?", shotIDs).Find(&shotOutfits).Error; err != nil {
			return nil, err
		}
	}

	var aliases []models.CharacterAliasCandidate
	if err := s.db.Where("project_id = ? AND status = ?", projectID, "confirmed").Find(&aliases).Error; err != nil {
		return nil, err
	}
	var looks []models.CharacterLook
	if err := s.db.Where("project_id = ?", projectID).Order("updated_at, id").Find(&looks).Error; err != nil {
		return nil, err
	}
	var outfits []models.CharacterOutfit
	if err := s.db.Where("project_id = ?", projectID).Order("updated_at, id").Find(&outfits).Error; err != nil {
		return nil, err
	}
	var assets []models.Asset
	if err := s.db.Where("project_id = ? AND kind = ?", projectID, "prop").Find(&assets).Error; err != nil {
		return nil, err
	}

	shotsByScene := map[uint][]models.Shot{}
	for _, shot := range shots {
		shotsByScene[shot.SceneID] = append(shotsByScene[shot.SceneID], shot)
	}
	dialoguesByScene := map[uint][]models.Dialogue{}
	for _, dialogue := range dialogues {
		dialoguesByScene[dialogue.SceneID] = append(dialoguesByScene[dialogue.SceneID], dialogue)
	}
	lookByID := map[uint]models.CharacterLook{}
	for _, look := range looks {
		lookByID[look.ID] = look
	}
	outfitByID := map[uint]models.CharacterOutfit{}
	for _, outfit := range outfits {
		outfitByID[outfit.ID] = outfit
	}
	propByName := map[string]models.Asset{}
	for _, asset := range assets {
		propByName[normalizeCanonName(asset.Name)] = asset
	}

	out := make([]CharacterHistory, 0, len(characters))
	for _, character := range characters {
		out = append(out, buildCharacterHistory(character, aliases, scenes, shotsByScene, dialoguesByScene, sceneLooks, shotLooks, sceneOutfits, shotOutfits, looks, outfits, lookByID, outfitByID, propByName))
	}
	return out, nil
}

func buildCharacterHistory(character models.Character, aliases []models.CharacterAliasCandidate, scenes []models.Scene, shotsByScene map[uint][]models.Shot, dialoguesByScene map[uint][]models.Dialogue, sceneLooks []models.SceneCharacterLook, shotLooks []models.ShotCharacterLook, sceneOutfits []models.SceneCharacterOutfit, shotOutfits []models.ShotCharacterOutfit, looks []models.CharacterLook, outfits []models.CharacterOutfit, lookByID map[uint]models.CharacterLook, outfitByID map[uint]models.CharacterOutfit, propByName map[string]models.Asset) CharacterHistory {
	h := CharacterHistory{
		Character: CharacterHistoryCharacter{ID: character.ID, Name: character.Name},
		Aliases:   []string{}, Episodes: []CharacterHistoryEpisode{}, Props: []CharacterHistoryProp{},
		VoiceConfig: CharacterHistoryVoice{Voice: character.Voice, VoiceRef: character.VoiceRef, VoiceID: character.VoiceID, VoiceModel: character.VoiceModel},
	}
	names := map[string]bool{normalizeCanonName(character.Name): true}
	for _, alias := range aliases {
		if normalizeCanonName(alias.CanonicalName) == normalizeCanonName(character.Name) {
			h.Aliases = append(h.Aliases, alias.Alias)
			names[normalizeCanonName(alias.Alias)] = true
		}
	}
	sort.Strings(h.Aliases)

	sceneLookID := map[uint]uint{}
	sceneOutfitID := map[uint]uint{}
	for _, row := range sceneLooks {
		if look, ok := lookByID[row.LookID]; ok && look.CharacterID == character.ID {
			sceneLookID[row.SceneID] = row.LookID
		}
	}
	for _, row := range sceneOutfits {
		if row.CharacterID == character.ID {
			sceneOutfitID[row.SceneID] = row.OutfitID
		}
	}
	shotLookID := map[uint]uint{}
	shotOutfitID := map[uint]uint{}
	for _, row := range shotLooks {
		if look, ok := lookByID[row.LookID]; ok && look.CharacterID == character.ID {
			shotLookID[row.ShotID] = row.LookID
		}
	}
	for _, row := range shotOutfits {
		if row.CharacterID == character.ID {
			shotOutfitID[row.ShotID] = row.OutfitID
		}
	}

	propSeen := map[string]bool{}
	episodeIndex := map[int]int{}
	for _, scene := range scenes {
		matched := historySceneNameMatch(scene, dialoguesByScene[scene.ID], names) || sceneLookID[scene.ID] != 0 || sceneOutfitID[scene.ID] != 0
		for _, shot := range shotsByScene[scene.ID] {
			if shotLookID[shot.ID] != 0 || shotOutfitID[shot.ID] != 0 {
				matched = true
			}
		}
		if !matched {
			continue
		}

		sceneHistory := CharacterHistoryScene{ID: scene.ID, Order: scene.Order, Title: scene.Title, Props: []CharacterHistoryProp{}, Shots: []CharacterHistoryShot{}}
		for _, raw := range historyTokens(scene.Props) {
			prop := CharacterHistoryProp{Name: raw}
			if asset, ok := propByName[normalizeCanonName(raw)]; ok {
				prop.ID, prop.Name = asset.ID, asset.Name
			}
			sceneHistory.Props = append(sceneHistory.Props, prop)
			key := normalizeCanonName(prop.Name)
			if key != "" && !propSeen[key] {
				h.Props = append(h.Props, prop)
				propSeen[key] = true
			}
		}
		// Scene assignments are effective first; approved shot overrides later in the scene win.
		if look, ok := lookByID[sceneLookID[scene.ID]]; ok && approvedHistoryStatus(string(look.AuditStatus)) {
			h.LastApprovedLook = historyLook(look)
		}
		if outfit, ok := outfitByID[sceneOutfitID[scene.ID]]; ok && approvedHistoryStatus(string(outfit.AuditStatus)) {
			h.LastApprovedOutfit = historyOutfit(outfit)
		}
		for _, shot := range shotsByScene[scene.ID] {
			sceneHistory.Shots = append(sceneHistory.Shots, CharacterHistoryShot{ID: shot.ID, Order: shot.Order})
			if look, ok := lookByID[shotLookID[shot.ID]]; ok && approvedHistoryStatus(string(look.AuditStatus)) {
				h.LastApprovedLook = historyLook(look)
			}
			if outfit, ok := outfitByID[shotOutfitID[shot.ID]]; ok && approvedHistoryStatus(string(outfit.AuditStatus)) {
				h.LastApprovedOutfit = historyOutfit(outfit)
			}
		}

		idx, ok := episodeIndex[scene.EpisodeN]
		if !ok {
			idx = len(h.Episodes)
			episodeIndex[scene.EpisodeN] = idx
			h.Episodes = append(h.Episodes, CharacterHistoryEpisode{Number: scene.EpisodeN, Scenes: []CharacterHistoryScene{}})
		}
		h.Episodes[idx].Scenes = append(h.Episodes[idx].Scenes, sceneHistory)
		last := &CharacterHistoryLastSeen{EpisodeN: scene.EpisodeN, SceneID: scene.ID, SceneOrder: scene.Order, SceneTitle: scene.Title}
		if n := len(sceneHistory.Shots); n > 0 {
			shot := sceneHistory.Shots[n-1]
			last.ShotID, last.ShotOrder = &shot.ID, &shot.Order
		}
		h.LastSeen = last
	}

	// If no approved assignment has appeared, expose the most recently edited approved asset.
	if h.LastApprovedLook == nil {
		for _, look := range looks {
			if look.CharacterID == character.ID && approvedHistoryStatus(string(look.AuditStatus)) {
				h.LastApprovedLook = historyLook(look)
			}
		}
	}
	if h.LastApprovedOutfit == nil {
		for _, outfit := range outfits {
			if outfit.CharacterID == character.ID && approvedHistoryStatus(string(outfit.AuditStatus)) {
				h.LastApprovedOutfit = historyOutfit(outfit)
			}
		}
	}
	return h
}

func historySceneNameMatch(scene models.Scene, dialogues []models.Dialogue, names map[string]bool) bool {
	for _, field := range []string{scene.Characters, scene.VisibleCharacters, scene.VoiceCharacters, scene.MentionedCharacters} {
		for _, token := range historyTokens(field) {
			if names[normalizeCanonName(token)] {
				return true
			}
		}
	}
	for _, dialogue := range dialogues {
		if names[normalizeCanonName(dialogue.Character)] {
			return true
		}
	}
	return false
}

func historyTokens(value string) []string {
	parts := strings.FieldsFunc(value, func(r rune) bool { return r == ',' || r == '，' || r == '、' || r == ';' || r == '；' || r == '\n' })
	out := make([]string, 0, len(parts))
	for _, part := range parts {
		if part = strings.TrimSpace(part); part != "" {
			out = append(out, part)
		}
	}
	return out
}

func approvedHistoryStatus(status string) bool {
	return status == string(models.LookStatusApproved) || status == string(models.LookStatusPublished)
}
func historyLook(look models.CharacterLook) *CharacterHistoryLook {
	return &CharacterHistoryLook{ID: look.ID, Name: look.Name, AuditStatus: string(look.AuditStatus)}
}
func historyOutfit(outfit models.CharacterOutfit) *CharacterHistoryOutfit {
	return &CharacterHistoryOutfit{ID: outfit.ID, Name: outfit.Name, AuditStatus: string(outfit.AuditStatus)}
}
