package models

import "time"

// CharacterOutfit 将独立的服装、鞋履、发型和配饰组合成一套可按场景选择的完整造型。
type CharacterOutfit struct {
	ID          uint                  `gorm:"primaryKey" json:"id"`
	ProjectID   uint                  `gorm:"index;uniqueIndex:idx_outfit_character_name" json:"project_id"`
	CharacterID uint                  `gorm:"index;uniqueIndex:idx_outfit_character_name" json:"character_id"`
	Name        string                `gorm:"uniqueIndex:idx_outfit_character_name" json:"name"`
	Description string                `gorm:"type:text" json:"description"`
	IsDefault   bool                  `gorm:"default:false;index" json:"is_default"`
	AuditStatus CharacterLookStatus   `gorm:"default:draft" json:"audit_status"`
	AuditNote   string                `gorm:"type:text" json:"audit_note"`
	Image       string                `json:"image"`
	ImageTaskID string                `gorm:"index" json:"image_task_id"`
	ImageError  string                `gorm:"type:text" json:"image_error"`
	Sheet       string                `json:"sheet"`
	SheetTaskID string                `gorm:"index" json:"sheet_task_id"`
	SheetError  string                `gorm:"type:text" json:"sheet_error"`
	Version     int                   `gorm:"default:1" json:"version"`
	CreatedAt   time.Time             `json:"created_at"`
	UpdatedAt   time.Time             `json:"updated_at"`
	Character   *Character            `gorm:"foreignKey:CharacterID" json:"character,omitempty"`
	Items       []CharacterOutfitLook `gorm:"foreignKey:OutfitID" json:"items,omitempty"`
}

// CharacterOutfitLook 是套装与独立造型资产的有序关联。
type CharacterOutfitLook struct {
	ID        uint           `gorm:"primaryKey" json:"id"`
	OutfitID  uint           `gorm:"index;uniqueIndex:idx_outfit_look" json:"outfit_id"`
	LookID    uint           `gorm:"index;uniqueIndex:idx_outfit_look" json:"look_id"`
	Order     int            `gorm:"default:0" json:"order"`
	CreatedAt time.Time      `json:"created_at"`
	Look      *CharacterLook `gorm:"foreignKey:LookID" json:"look,omitempty"`
}

// SceneCharacterOutfit 保证一个场景内每个角色只选择一套造型。
type SceneCharacterOutfit struct {
	ID          uint             `gorm:"primaryKey" json:"id"`
	SceneID     uint             `gorm:"index;uniqueIndex:idx_scene_character_outfit" json:"scene_id"`
	CharacterID uint             `gorm:"index;uniqueIndex:idx_scene_character_outfit" json:"character_id"`
	OutfitID    uint             `gorm:"index" json:"outfit_id"`
	CreatedAt   time.Time        `json:"created_at"`
	Outfit      *CharacterOutfit `gorm:"foreignKey:OutfitID" json:"outfit,omitempty"`
}

// ShotCharacterOutfit 是镜头级覆盖；没有记录时继承场景选择。
type ShotCharacterOutfit struct {
	ID          uint             `gorm:"primaryKey" json:"id"`
	ShotID      uint             `gorm:"index;uniqueIndex:idx_shot_character_outfit" json:"shot_id"`
	CharacterID uint             `gorm:"index;uniqueIndex:idx_shot_character_outfit" json:"character_id"`
	OutfitID    uint             `gorm:"index" json:"outfit_id"`
	CreatedAt   time.Time        `json:"created_at"`
	Outfit      *CharacterOutfit `gorm:"foreignKey:OutfitID" json:"outfit,omitempty"`
}
