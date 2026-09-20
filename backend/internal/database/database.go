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
	if db.Migrator().HasTable(&models.ProjectSkillConfig{}) && db.Migrator().HasIndex(&models.ProjectSkillConfig{}, "idx_proj_stage") {
		if err := db.Migrator().DropIndex(&models.ProjectSkillConfig{}, "idx_proj_stage"); err != nil {
			return nil, fmt.Errorf("drop obsolete project skill index: %w", err)
		}
	}
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
		&models.PlaygroundRun{},
		&models.Event{},
		&models.Setting{},
		&models.Project{},
		&models.Episode{},
		&models.ScriptRevision{},
		&models.Scene{},
		&models.MergeTask{},
		&models.Material{},
		&models.Character{},
		&models.Asset{},
		&models.AssetVariant{},
		&models.Dialogue{},
		&models.Skill{},
		&models.ProjectSkillConfig{},
		&models.SkillAuditLog{},
		&models.Chapter{},
		&models.ChapterTask{},
		&models.StoryArc{},
		&models.StoryBible{},
		&models.AdaptationStrategy{},
		&models.EpisodeAdaptation{},
		&models.CharacterAliasCandidate{},
		&models.NovelJob{},
		&models.PlanningBatch{},
		&models.BatchEpisode{},
		&models.Shot{},
		&models.PromptVersion{},
		&models.StylePreset{},
		&models.CharacterLook{},
		&models.SceneCharacterLook{},
		&models.ShotCharacterLook{},
		&models.CharacterOutfit{},
		&models.CharacterOutfitLook{},
		&models.SceneCharacterOutfit{},
		&models.ShotCharacterOutfit{},
		&models.FrameCandidate{},
		&models.GenerationCandidate{},
		&models.AudioLayer{},
		&models.PromptPolicyOverride{},
		&models.CharacterMotionReference{},
		&models.SharedAssetReference{},
		&models.SceneContinuity{},
	); err != nil {
		return nil, fmt.Errorf("migrate: %w", err)
	}
	if err := db.Exec("UPDATE project_skill_configs SET operation = COALESCE((SELECT CASE WHEN TRIM(operation) <> '' THEN operation ELSE code END FROM skills WHERE skills.id = project_skill_configs.skill_id), stage) WHERE TRIM(COALESCE(operation, '')) = ''").Error; err != nil {
		return nil, fmt.Errorf("backfill project skill operation: %w", err)
	}
	if err := migrateLegacyDialogueAudioState(db); err != nil {
		return nil, fmt.Errorf("migrate legacy dialogue audio: %w", err)
	}
	return db, nil
}

// Legacy ready rows predate audio_hash and cannot prove that their audio matches the current
// speech inputs. Keep the file for QA/recovery, but require an explicit re-synthesis before use.
func migrateLegacyDialogueAudioState(db *gorm.DB) error {
	return db.Model(&models.Dialogue{}).
		Where("status = ? AND (audio_hash IS NULL OR TRIM(audio_hash) = '')", "ready").
		Updates(map[string]any{
			"status": "pending", "audio_stale": true,
			"audio_stale_reason": "旧版配音缺少输入摘要，需重新合成", "audio_token": "",
		}).Error
}
