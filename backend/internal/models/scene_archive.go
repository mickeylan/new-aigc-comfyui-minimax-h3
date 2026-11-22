package models

import "time"

// SceneArchive 是 3D 场景编辑器存档的元数据。存档 JSON 本身（含 base64 的 GLB）
// 落在 <data_dir>/scene-archives/<id>.json，表里只放元信息：十几 MB 的 base64
// 塞进 SQLite 行里会让全表扫描和备份都变慢，也没办法按流读取。
type SceneArchive struct {
	ID        uint      `gorm:"primaryKey" json:"id"`
	ProjectID *uint     `gorm:"index" json:"project_id"`
	Name      string    `gorm:"size:200" json:"name"`
	Size      int64     `json:"size"`
	CreatedAt time.Time `json:"created_at"`
}