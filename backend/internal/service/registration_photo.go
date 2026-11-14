package service

import (
	"errors"
	"fmt"
	"log"
	"net/http"
	"strings"
	"time"

	"comfyui-console/internal/models"
	"github.com/gin-gonic/gin"
)

const characterRegistrationSkillCode = "character-registration-photo"

// characterRegistrationPhotoSkill is the image-edit prompt, not an LLM program: its SystemPrompt
// is sent to Qwen Edit after {{character_name}} substitution. Wording is user-approved; raise
// Version whenever it changes so InitSystemSkills publishes the new revision.
var characterRegistrationPhotoSkill = models.Skill{
	Name: "角色登记照", Code: characterRegistrationSkillCode, Operation: characterRegistrationSkillCode, Version: 1,
	Description:    "从四视图提取单人正面胸像登记照，作为分镜与H3的人脸参考（图像编辑提示词）",
	Stage:          models.SkillStageQwenImageEdit,
	PromptTemplate: "{{character_name}}",
	SystemPrompt:   "参考图是{{character_name}}的四视图人物设定。只取正面视图中的这个人，生成一张单人正面胸像登记照：头顶到胸口上缘完整入画，脸部居中、正视镜头、表情平静、嘴闭合；五官、脸型、肤色、发型发色、妆容和领口可见的服装细节与参考图完全一致，不美化、不改年龄；纯浅灰背景，柔和均匀的正面光，无投影；画面里只有这一个人，不出现其他视图、文字、边框、标注或水印。",
	IsSystem:       true, Enabled: true, SortOrder: 30,
}

var errRegistrationActive = errors.New("登记照正在生成")

// registrationPhotoPrompt uses the latest enabled Skill so a user-edited wording takes effect,
// falling back to the seeded wording when the Skill table is unavailable.
func (s *ProjectService) registrationPhotoPrompt(ch *models.Character) string {
	skill := &characterRegistrationPhotoSkill
	if s.skills != nil {
		if latest, err := s.skills.latestEnabledSkillByCode(characterRegistrationSkillCode); err == nil {
			skill = latest
		}
	}
	return strings.ReplaceAll(skill.SystemPrompt, "{{character_name}}", ch.Name)
}

// StartCharacterRegistrationPhoto submits a Qwen Edit task with the character sheet as its only
// reference. The photo is never auto-injected; scenes select it explicitly as a reference.
func (s *ProjectService) StartCharacterRegistrationPhoto(ch *models.Character) error {
	if strings.TrimSpace(ch.Sheet) == "" {
		return fmt.Errorf("请先为角色「%s」生成或上传四视图", ch.Name)
	}
	if s.tasks == nil {
		return fmt.Errorf("登记照生成依赖 ComfyUI 任务服务")
	}
	if s.sheetTaskActive(ch.RegistrationTaskID) {
		return fmt.Errorf("%w：角色「%s」", errRegistrationActive, ch.Name)
	}
	tpl, err := enabledTemplateByCode(s.db, TemplateQwen21MultiEdit)
	if err != nil {
		return err
	}
	params := qwenPortraitParams()
	params["aspect_ratio"] = "3:4 (Portrait Standard)"
	task, err := s.tasks.CreateTask(CreateTaskReq{TemplateID: tpl.ID, Prompt: s.registrationPhotoPrompt(ch), Params: params,
		Files: map[string][]FileMeta{"ref_images": {{TaskID: fmt.Sprint(ch.ProjectID), Name: ch.Sheet}}}})
	if err != nil {
		return fmt.Errorf("创建登记照任务失败: %w", err)
	}
	if err := s.db.Model(&models.Character{}).Where("id = ?", ch.ID).Updates(map[string]any{
		"registration_task_id": task.TaskID, "registration_error": "",
	}).Error; err != nil {
		_ = s.tasks.CancelTask(task.TaskID)
		return err
	}
	go func() {
		if err := s.tasks.Execute(task.TaskID); err != nil && !errors.Is(err, errNoFreeGPU) {
			log.Printf("[character %d] execute registration task %s failed: %v", ch.ID, task.TaskID, err)
		}
	}()
	s.pushProject(nil)
	return nil
}

func (s *ProjectService) syncRegistrationPhotos() bool {
	var chars []models.Character
	if s.db.Where("registration_task_id != ''").Find(&chars).Error != nil {
		return false
	}
	changed := false
	for _, ch := range chars {
		updates, ok := s.imageTaskUpdates(ch.RegistrationTaskID, fmt.Sprintf("char_registration_%d_%d", ch.ID, time.Now().UnixNano()), ch.ProjectID,
			"登记照", "registration_photo", "registration_task_id", "registration_error")
		if !ok {
			continue
		}
		res := s.db.Model(&models.Character{}).Where("id = ? AND registration_task_id = ?", ch.ID, ch.RegistrationTaskID).Updates(updates)
		changed = changed || res.RowsAffected > 0
	}
	return changed
}

func (s *Service) HandleGenerateCharacterRegistrationPhoto(c *gin.Context) {
	ch, ok := s.loadCharacter(c)
	if !ok {
		return
	}
	if err := s.Projects.StartCharacterRegistrationPhoto(ch); err != nil {
		status := http.StatusBadRequest
		if errors.Is(err, errRegistrationActive) {
			status = http.StatusConflict
		}
		c.JSON(status, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusAccepted, gin.H{"ok": true, "message": fmt.Sprintf("角色「%s」登记照任务已提交", ch.Name)})
}
