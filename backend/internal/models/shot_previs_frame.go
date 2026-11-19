package models

import "time"

// ShotPrevisFrame is immutable rehearsal history; only adoption metadata may change.
type ShotPrevisFrame struct {
	ID                uint      `gorm:"primaryKey" json:"id"`
	ProjectID         uint      `gorm:"index" json:"project_id"`
	SceneID           uint      `gorm:"index" json:"scene_id"`
	ShotID            uint      `gorm:"index" json:"shot_id"`
	PrevisRevision    uint      `json:"previs_revision"`
	TimeMS            int       `json:"time_ms"`
	SourceContentHash string    `json:"source_content_hash"`
	ShotHash          string    `json:"shot_hash"`
	FrameHash         string    `json:"frame_hash"`
	DocumentJSON      string    `gorm:"type:text" json:"document_json"`
	FrameJSON         string    `gorm:"type:text" json:"frame_json"`
	AppliedPromptHash string    `json:"applied_prompt_hash"`
	CreatedAt         time.Time `json:"created_at"`
}
