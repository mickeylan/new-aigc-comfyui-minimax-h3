package models

import "time"

// LocationReferenceDesign 是「场景参考设计」的一次审阅记录：以若干张场景图/人物形象/道具图为参考，
// 组合出一个新的场景画面。生成结果注册为该场景的 asset_image 变体（不自动替换当前场景图），
// Asset.Image 仍然只有一个选中的输出。
type LocationReferenceDesign struct {
	ID           uint   `gorm:"primaryKey" json:"id"`
	ProjectID    uint   `gorm:"index;not null" json:"project_id"`
	AssetID      uint   `gorm:"index;not null" json:"asset_id"`
	Engine       string `json:"engine"`
	Requirements string `gorm:"type:text" json:"requirements"`
	Prompt       string `gorm:"type:text" json:"prompt"`
	ParamsJSON   string `gorm:"type:text" json:"params_json"`
	ProfileHash  string `json:"profile_hash"`
	TaskID       string `gorm:"index" json:"task_id"`
	Error        string `gorm:"type:text" json:"error"`
	Skill        string `json:"skill"`
	// ResultVariantID 指向生成成功后注册的 asset_image 变体；置位后同一份设计不再重复生成。
	ResultVariantID uint                          `gorm:"column:result_variant_id" json:"result_variant_id"`
	CreatedAt       time.Time                     `json:"created_at"`
	References      []LocationReferenceDesignFile `gorm:"foreignKey:DesignID" json:"references"`
}

// LocationReferenceDesignFile 把参考图落成独立行：生命周期检查（是否被引用）与顺序都不依赖 JSON。
// Slot 区分 scene/character/prop，Presence 区分必须入画（required）与仅参考（optional）。
type LocationReferenceDesignFile struct {
	ID             uint   `gorm:"primaryKey" json:"id"`
	DesignID       uint   `gorm:"index;not null" json:"design_id"`
	ProjectID      uint   `gorm:"index;not null" json:"project_id"`
	File           string `gorm:"index;not null" json:"file"`
	SHA256         string `json:"sha256"`
	Slot           string `json:"slot"`
	Presence       string `json:"presence"`
	Responsibility string `gorm:"type:text" json:"responsibility"`
	Position       int    `json:"position"`
}
