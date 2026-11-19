package models

import "time"

// CharacterAgeStage 是同一角色在某个年龄段的外貌锚点。阶段外貌只在场景选用其下套装时
// 替代角色主档案；未挂阶段的套装继续以角色主标准像锁定身份与年龄感。
type CharacterAgeStage struct {
	ID             uint   `gorm:"primaryKey" json:"id"`
	ProjectID      uint   `gorm:"index;uniqueIndex:idx_age_stage_character_name" json:"project_id"`
	CharacterID    uint   `gorm:"index;uniqueIndex:idx_age_stage_character_name" json:"character_id"`
	Name           string `gorm:"uniqueIndex:idx_age_stage_character_name" json:"name"`
	Age            *int   `json:"age"`
	Appearance     string `gorm:"type:text" json:"appearance"`
	PortraitEngine string `gorm:"size:32;default:qwen_edit" json:"portrait_engine"` // qwen_edit / minimax_h3_edit：阶段四视图都以主标准像为参考
	PortraitPrompt string `gorm:"type:text" json:"portrait_prompt"`
	Portrait       string `json:"portrait"`
	// PortraitHistory 是被新结果替换下来的旧阶段标准像文件名（JSON 数组，新的在前），可随时恢复。
	PortraitHistory string              `gorm:"type:text" json:"portrait_history"`
	PortraitTaskID  string              `gorm:"index" json:"portrait_task_id"`
	PortraitError   string              `gorm:"type:text" json:"portrait_error"`
	AuditStatus     CharacterLookStatus `gorm:"default:draft" json:"audit_status"`
	AuditNote       string              `gorm:"type:text" json:"audit_note"`
	Version         int                 `gorm:"default:1" json:"version"`
	CreatedAt       time.Time           `json:"created_at"`
	UpdatedAt       time.Time           `json:"updated_at"`
}
