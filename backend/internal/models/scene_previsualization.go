package models

import "time"

// ScenePrevisualization is an optional client-only rehearsal, never a generation input.
type ScenePrevisualization struct {
	ID                uint      `gorm:"primaryKey" json:"id"`
	ProjectID         uint      `gorm:"index" json:"project_id"`
	SceneID           uint      `gorm:"uniqueIndex" json:"scene_id"`
	SchemaVersion     int       `json:"schema_version"`
	Revision          uint      `json:"revision"`
	SourceContentHash string    `json:"source_content_hash"`
	DocumentJSON      string    `gorm:"type:text" json:"document_json"`
	CreatedAt         time.Time `json:"created_at"`
	UpdatedAt         time.Time `json:"updated_at"`
}
