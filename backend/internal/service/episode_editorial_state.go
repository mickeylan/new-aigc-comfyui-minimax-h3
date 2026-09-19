package service

import (
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"strings"

	"comfyui-console/internal/models"
	"gorm.io/gorm"
)

type episodeEditorialInput struct {
	Scenes []episodeEditorialScene `json:"scenes"`
}

type episodeEditorialScene struct {
	Order               int                        `json:"order"`
	Generation          uint                       `json:"generation"`
	Title               string                     `json:"title"`
	Content             string                     `json:"content"`
	ImagePrompt         string                     `json:"image_prompt"`
	NegativePrompt      string                     `json:"negative_prompt"`
	VideoPrompt         string                     `json:"video_prompt"`
	Duration            float64                    `json:"duration"`
	VisibleCharacters   string                     `json:"visible_characters"`
	VoiceCharacters     string                     `json:"voice_characters"`
	MentionedCharacters string                     `json:"mentioned_characters"`
	Location            string                     `json:"location"`
	Props               string                     `json:"props"`
	Shots               []episodeEditorialShot     `json:"shots"`
	Dialogues           []episodeEditorialDialogue `json:"dialogues"`
}

type episodeEditorialShot struct {
	Order          int                       `json:"order"`
	ActType        models.ShotActType        `json:"act_type"`
	ShotType       string                    `json:"shot_type"`
	CameraAngle    string                    `json:"camera_angle"`
	CameraMovement string                    `json:"camera_movement"`
	TransitionType models.ShotTransitionType `json:"transition_type"`
	TransitionNote string                    `json:"transition_note"`
	Duration       float64                   `json:"duration"`
	Description    string                    `json:"description"`
	Dialogue       string                    `json:"dialogue"`
	Emotion        string                    `json:"emotion"`
	PromptSubject  string                    `json:"prompt_subject"`
	PromptAction   string                    `json:"prompt_action"`
	PromptCamera   string                    `json:"prompt_camera"`
	PromptLighting string                    `json:"prompt_lighting"`
	PromptStyle    string                    `json:"prompt_style"`
	NegativePrompt string                    `json:"negative_prompt"`
}

type episodeEditorialDialogue struct {
	Order              int     `json:"order"`
	Character          string  `json:"character"`
	SpeechType         string  `json:"speech_type"`
	Text               string  `json:"text"`
	Position           float64 `json:"position"`
	Offset             float64 `json:"offset"`
	Emotion            string  `json:"emotion"`
	Delivery           string  `json:"delivery"`
	H3VoiceDescription string  `json:"h3_voice_description"`
}

// episodeEditorialSnapshot serializes only project-authored episode material. Generated media,
// task state and Episode summary outputs are deliberately excluded.
func episodeEditorialSnapshot(db *gorm.DB, projectID uint, episodeN int) ([]byte, error) {
	generation, err := latestEpisodeGeneration(db, projectID, episodeN)
	if err != nil {
		return nil, err
	}
	var scenes []models.Scene
	if err := db.Where("project_id = ? AND episode_n = ? AND generation = ?", projectID, episodeN, generation).Order("`order`, id").Find(&scenes).Error; err != nil {
		return nil, err
	}
	input := episodeEditorialInput{Scenes: make([]episodeEditorialScene, 0, len(scenes))}
	for _, scene := range scenes {
		item := episodeEditorialScene{
			Order: scene.Order, Generation: scene.Generation, Title: strings.TrimSpace(scene.Title),
			Content: strings.TrimSpace(scene.Content), ImagePrompt: strings.TrimSpace(scene.ImagePrompt), NegativePrompt: strings.TrimSpace(scene.NegativePrompt),
			VideoPrompt: strings.TrimSpace(scene.VideoPrompt), Duration: scene.Duration,
			VisibleCharacters: strings.TrimSpace(scene.VisibleCharacters), VoiceCharacters: strings.TrimSpace(scene.VoiceCharacters),
			MentionedCharacters: strings.TrimSpace(scene.MentionedCharacters), Location: strings.TrimSpace(scene.LocationName),
			Props: strings.TrimSpace(scene.Props), Shots: []episodeEditorialShot{}, Dialogues: []episodeEditorialDialogue{},
		}
		var shots []models.Shot
		if err := db.Where("scene_id = ?", scene.ID).Order("order_num, id").Find(&shots).Error; err != nil {
			return nil, err
		}
		for _, shot := range shots {
			item.Shots = append(item.Shots, episodeEditorialShot{
				Order: shot.Order, ActType: shot.ActType, ShotType: strings.TrimSpace(shot.ShotType), CameraAngle: strings.TrimSpace(shot.CameraAngle),
				CameraMovement: strings.TrimSpace(shot.CameraMovement), TransitionType: shot.TransitionType, TransitionNote: strings.TrimSpace(shot.TransitionNote),
				Duration: shot.Duration, Description: strings.TrimSpace(shot.Description), Dialogue: strings.TrimSpace(shot.Dialogue), Emotion: strings.TrimSpace(shot.Emotion),
				PromptSubject: strings.TrimSpace(shot.PromptSubject), PromptAction: strings.TrimSpace(shot.PromptAction), PromptCamera: strings.TrimSpace(shot.PromptCamera),
				PromptLighting: strings.TrimSpace(shot.PromptLighting), PromptStyle: strings.TrimSpace(shot.PromptStyle), NegativePrompt: strings.TrimSpace(shot.NegativePrompt),
			})
		}
		var dialogues []models.Dialogue
		if err := db.Where("project_id = ? AND scene_id = ?", projectID, scene.ID).Order("`order`, id").Find(&dialogues).Error; err != nil {
			return nil, err
		}
		for _, dialogue := range dialogues {
			item.Dialogues = append(item.Dialogues, episodeEditorialDialogue{
				Order: dialogue.Order, Character: strings.TrimSpace(dialogue.Character), SpeechType: strings.TrimSpace(dialogue.SpeechType),
				Text: strings.TrimSpace(dialogue.Text), Position: dialogue.Position, Offset: dialogue.Offset, Emotion: strings.TrimSpace(dialogue.Emotion),
				Delivery: strings.TrimSpace(dialogue.Delivery), H3VoiceDescription: strings.TrimSpace(dialogue.H3VoiceDescription),
			})
		}
		input.Scenes = append(input.Scenes, item)
	}
	return json.Marshal(input)
}

func episodeEditorialInputHash(db *gorm.DB, projectID uint, episodeN int) (string, error) {
	snapshot, err := episodeEditorialSnapshot(db, projectID, episodeN)
	if err != nil {
		return "", err
	}
	return fmt.Sprintf("%x", sha256.Sum256(snapshot)), nil
}

func refreshEpisodeEditorialStatus(db *gorm.DB, episode *models.Episode) error {
	hash, err := episodeEditorialInputHash(db, episode.ProjectID, episode.Number)
	if err != nil {
		return err
	}
	summaryStale := strings.TrimSpace(episode.Summary) != "" && episode.SummaryInputHash != hash
	nextHookStale := strings.TrimSpace(episode.NextHook) != "" && episode.NextHookInputHash != hash
	if episode.SummaryStale != summaryStale || episode.NextHookStale != nextHookStale {
		if err := db.Model(episode).Updates(map[string]any{"summary_stale": summaryStale, "next_hook_stale": nextHookStale}).Error; err != nil {
			return err
		}
		episode.SummaryStale, episode.NextHookStale = summaryStale, nextHookStale
	}
	return nil
}

func markEpisodeEditorialStale(db *gorm.DB, projectID uint, episodeN int) error {
	if projectID == 0 || episodeN < 1 {
		return nil
	}
	return db.Model(&models.Episode{}).Where("project_id = ? AND episode_number = ?", projectID, episodeN).
		Updates(map[string]any{"summary_stale": true, "next_hook_stale": true}).Error
}

func markEpisodeEditorialStaleByScene(db *gorm.DB, sceneID uint) error {
	var scene models.Scene
	if err := db.Select("project_id", "episode_n").First(&scene, sceneID).Error; err != nil {
		return err
	}
	return markEpisodeEditorialStale(db, scene.ProjectID, scene.EpisodeN)
}
