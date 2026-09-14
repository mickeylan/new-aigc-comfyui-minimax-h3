package database

import (
	"fmt"
	"os"
	"path/filepath"

	"github.com/glebarez/sqlite"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"

	"comfyui-console/internal/config"
	"comfyui-console/internal/models"
)

func Init(cfg *config.Config) (*gorm.DB, error) {
	if err := os.MkdirAll(filepath.Dir(cfg.Storage.DBPath), 0o755); err != nil {
		return nil, err
	}
	db, err := gorm.Open(sqlite.Open(cfg.Storage.DBPath), &gorm.Config{
		Logger: logger.Default.LogMode(logger.Warn),
	})
	if err != nil {
		return nil, fmt.Errorf("open sqlite: %w", err)
	}
	// 早期 Skill 原型曾给 name/code 建单列唯一索引；版本化后同一技能必须共享它们。
	// 在 AutoMigrate 新的 (code, version) 联合唯一索引前清理旧索引，兼容已启动过原型的数据库。
	if db.Migrator().HasTable(&models.Skill{}) {
		for _, index := range []string{"idx_skills_name", "idx_skills_code"} {
			if db.Migrator().HasIndex(&models.Skill{}, index) {
				if err := db.Migrator().DropIndex(&models.Skill{}, index); err != nil {
					return nil, fmt.Errorf("drop obsolete skill index %s: %w", index, err)
				}
			}
		}
	}
	if err := db.AutoMigrate(
		&models.Instance{},
		&models.Template{},
		&models.UploadFile{},
		&models.Task{},
		&models.Event{},
		&models.Setting{},
		&models.Project{},
		&models.Scene{},
		&models.MergeTask{},
		&models.Material{},
		&models.Character{},
		&models.Asset{},
		&models.Dialogue{},
		&models.Skill{},
		&models.ProjectSkillConfig{},
		&models.SkillAuditLog{},
	); err != nil {
		return nil, fmt.Errorf("migrate: %w", err)
	}
	return db, nil
}
