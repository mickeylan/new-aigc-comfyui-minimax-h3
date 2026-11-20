package models

import "time"

// CharacterFaceDesign is an append-only reviewed input bundle. Character.Portrait
// remains the only selected output; TaskID links this bundle to existing history.
type CharacterFaceDesign struct {
	ID           uint                     `gorm:"primaryKey" json:"id"`
	ProjectID    uint                     `gorm:"index;not null" json:"project_id"`
	CharacterID  uint                     `gorm:"index;not null" json:"character_id"`
	Preset       string                   `json:"preset"`
	Engine       string                   `json:"engine"`
	Requirements string                   `gorm:"type:text" json:"requirements"`
	Prompt       string                   `gorm:"type:text" json:"prompt"`
	ParamsJSON   string                   `gorm:"type:text" json:"params_json"`
	ProfileHash  string                   `json:"profile_hash"`
	TaskID       string                   `gorm:"index" json:"task_id"`
	Skill        string                   `json:"skill"`
	CreatedAt    time.Time                `json:"created_at"`
	References   []CharacterFaceReference `gorm:"foreignKey:DesignID" json:"references"`
}

// File references are normalized so lifecycle checks can protect them without
// searching JSON. Order is the slice order, not a numerical blending weight.
type CharacterFaceReference struct {
	ID             uint   `gorm:"primaryKey" json:"id"`
	DesignID       uint   `gorm:"index;not null" json:"design_id"`
	ProjectID      uint   `gorm:"index;not null" json:"project_id"`
	File           string `gorm:"index;not null" json:"file"`
	SHA256         string `json:"sha256"`
	Slot           string `json:"slot"`
	Responsibility string `gorm:"type:text" json:"responsibility"`
	Position       int    `json:"position"`
}
