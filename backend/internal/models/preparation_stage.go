package models

import "time"

// PreparationStage is an immutable-input draft checkpoint, never an adopted Scene field.
// Each successful model step is reusable independently of later failed steps.
type PreparationStage struct {
	ID         uint      `gorm:"primaryKey" json:"id"`
	ProjectID  uint      `gorm:"index:idx_preparation_scene,priority:1" json:"project_id"`
	SceneID    uint      `gorm:"index:idx_preparation_scene,priority:2;uniqueIndex:idx_preparation_key,priority:1" json:"scene_id"`
	Stage      string    `gorm:"uniqueIndex:idx_preparation_key,priority:2" json:"stage"`
	InputHash  string    `gorm:"uniqueIndex:idx_preparation_key,priority:3" json:"input_hash"`
	Status     string    `json:"status"`
	Result     string    `gorm:"type:text" json:"result"`
	Error      string    `gorm:"type:text" json:"error"`
	Attempts   int       `json:"attempts"`
	ReuseCount int       `json:"reuse_count"`
	CreatedAt  time.Time `json:"created_at"`
	UpdatedAt  time.Time `json:"updated_at"`
}
