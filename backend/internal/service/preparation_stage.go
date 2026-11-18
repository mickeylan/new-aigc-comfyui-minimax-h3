package service

import (
	"encoding/json"
	"fmt"
	"strings"
	"sync"

	"comfyui-console/internal/models"
	"github.com/gin-gonic/gin"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

// One process owns the text calls. The per-key lock serializes identical steps,
// not different scenes/chunks; persisted successes survive process restarts.
var preparationLocks sync.Map

func preparationProviderVersion(provider TextProvider) string {
	if provider == nil {
		return "unconfigured"
	}
	values := []string{provider.Name()}
	if f, ok := provider.(*TextProviderFactory); ok {
		for _, key := range []string{SettingTextProvider, SettingLlamaBaseURL, SettingLlamaModel, SettingMiniMaxBaseURL, SettingMiniMaxModel, SettingVolcTextModel, SettingVolcBaseURL} {
			values = append(values, f.getter.GetSetting(key, ""))
		}
	}
	return hashString(strings.Join(values, "\x00"))
}

func preparationStep(db *gorm.DB, projectID, sceneID uint, stage, version, system, user string, call func() (string, error)) (string, error) {
	// Lightweight unit fixtures and old read-only databases retain their legacy path.
	// Production migrates this table before services start.
	if db == nil || !db.Migrator().HasTable(&models.PreparationStage{}) {
		return call()
	}
	key := hashString("preparation-v1\x00" + version + "\x00" + system + "\x00" + user)
	lockKey := fmt.Sprintf("%p:%d:%s:%s", db, sceneID, stage, key)
	value, _ := preparationLocks.LoadOrStore(lockKey, &sync.Mutex{})
	lock := value.(*sync.Mutex)
	lock.Lock()
	defer lock.Unlock()
	var row models.PreparationStage
	err := db.Where("scene_id = ? AND stage = ? AND input_hash = ?", sceneID, stage, key).First(&row).Error
	if err == nil && row.Status == "success" {
		if err := db.Model(&row).UpdateColumn("reuse_count", gorm.Expr("reuse_count + 1")).Error; err != nil {
			return "", err
		}
		return row.Result, nil
	}
	row = models.PreparationStage{ProjectID: projectID, SceneID: sceneID, Stage: stage, InputHash: key, Status: "running", Attempts: 1}
	if err := db.Clauses(clause.OnConflict{Columns: []clause.Column{{Name: "scene_id"}, {Name: "stage"}, {Name: "input_hash"}}, DoUpdates: clause.Assignments(map[string]any{"status": "running", "error": "", "attempts": gorm.Expr("attempts + 1")})}).Create(&row).Error; err != nil {
		return "", err
	}
	result, callErr := call()
	updates := map[string]any{"status": "success", "result": result, "error": ""}
	// Preserve the existing translator's policy: recovered-but-warned output is
	// reviewable, but only a clean translation may satisfy future reuse.
	if strings.HasPrefix(stage, "translation_") && callErr == nil {
		var translated preparedTranslation
		if json.Unmarshal([]byte(result), &translated) == nil && len(translated.Warnings) > 0 {
			updates["status"] = "warning"
			updates["error"] = strings.Join(translated.Warnings, "; ")
		}
	}
	if callErr != nil {
		updates["status"] = "failed"
		updates["error"] = sanitizeDiagnosticText(callErr.Error())
		updates["result"] = ""
	}
	if err := db.Model(&models.PreparationStage{}).Where("scene_id = ? AND stage = ? AND input_hash = ?", sceneID, stage, key).Updates(updates).Error; err != nil {
		return "", fmt.Errorf("保存阶段结果失败: %w", err)
	}
	return result, callErr
}

func (s *Service) HandleListPreparationStages(c *gin.Context) {
	sc, ok := s.loadScene(c)
	if !ok {
		return
	}
	var rows []models.PreparationStage
	if err := s.DB.Where("project_id = ? AND scene_id = ?", sc.ProjectID, sc.ID).Order("updated_at DESC,id DESC").Limit(100).Find(&rows).Error; err != nil {
		c.JSON(500, gin.H{"error": "无法读取阶段记录"})
		return
	}
	// Saved results are explicit drafts and never automatically applied to Scene.
	c.JSON(200, gin.H{"items": rows, "note": "输入版本相同的成功步骤自动复用；重新操作会仅请求未成功步骤。历史结果仅供审阅，不自动覆盖当前内容。"})
}

func (s *ProjectService) preparationVideoVersion(sc *models.Scene) string {
	refs, _ := s.sceneVideoReferenceFiles(sc, fmt.Sprint(sc.ProjectID))
	digests := []string{}
	for _, ref := range refs {
		digests = append(digests, s.fileSHA256(ref))
	}
	var skills []models.Skill
	var configs []models.ProjectSkillConfig
	if s.db.Migrator().HasTable(&models.Skill{}) {
		s.db.Order("id").Find(&skills)
	}
	if s.db.Migrator().HasTable(&models.ProjectSkillConfig{}) {
		s.db.Where("project_id = ?", sc.ProjectID).Order("id").Find(&configs)
	}
	return preparationJSONVersion(preparationProviderVersion(s.textProvider), sceneVideoPromptCompilerVersion, digests, skills, configs)
}

func preparationJSONVersion(values ...any) string {
	raw, _ := json.Marshal(values)
	return hashString(string(raw))
}
