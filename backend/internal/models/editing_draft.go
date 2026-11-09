package models

import "time"

// EditingDraft is a non-destructive edit decision list, not a screenplay source.
type EditingDraft struct {
	ID           uint      `gorm:"primaryKey" json:"id"`
	ProjectID    uint      `gorm:"uniqueIndex:idx_editing_project_episode" json:"project_id"`
	EpisodeN     int       `gorm:"uniqueIndex:idx_editing_project_episode" json:"episode_n"`
	Revision     uint      `json:"revision"`
	EditSequence string    `gorm:"type:text" json:"edit_sequence"`
	UpdatedAt    time.Time `json:"updated_at"`
}
