package service

import (
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"path/filepath"
	"strings"
	"time"

	"comfyui-console/internal/models"
	"gorm.io/gorm"
)

// AgeStagePortraitQwenEdit 以角色主标准像为参考、经 Qwen 多图编辑改写年龄；默认来源。
const AgeStagePortraitQwenEdit = "qwen_edit"

type AgeStageInput struct {
	Name           string `json:"name"`
	Age            *int   `json:"age"`
	Appearance     string `json:"appearance"`
	PortraitEngine string `json:"portrait_engine"`
}

func normalizeAgeStagePortraitEngine(value string) (string, error) {
	switch strings.TrimSpace(value) {
	case "", AgeStagePortraitQwenEdit:
		return AgeStagePortraitQwenEdit, nil
	case ImageEngineKrea2:
		return ImageEngineKrea2, nil
	case ImageEngineQwen21:
		return ImageEngineQwen21, nil
	default:
		return "", fmt.Errorf("不支持的年龄阶段标准像来源")
	}
}

func ageStageApproved(stage *models.CharacterAgeStage) bool {
	return stage.AuditStatus == models.LookStatusApproved || stage.AuditStatus == models.LookStatusPublished
}

// ageStageLabel 用于界面与提示词，如“童年（6岁）”。
func ageStageLabel(stage *models.CharacterAgeStage) string {
	if stage.Age != nil && *stage.Age > 0 {
		return fmt.Sprintf("%s（%d岁）", stage.Name, *stage.Age)
	}
	return stage.Name
}

func (s *CharacterLookService) loadAgeStage(db *gorm.DB, projectID, characterID, stageID uint) (*models.CharacterAgeStage, error) {
	var stage models.CharacterAgeStage
	if err := db.Where("id = ? AND project_id = ? AND character_id = ?", stageID, projectID, characterID).First(&stage).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, fmt.Errorf("年龄阶段不存在或不属于当前角色")
		}
		return nil, err
	}
	return &stage, nil
}

func (s *CharacterLookService) ListAgeStages(projectID, characterID uint) ([]models.CharacterAgeStage, error) {
	var stages []models.CharacterAgeStage
	err := s.db.Where("project_id = ? AND character_id = ?", projectID, characterID).Order("CASE WHEN age IS NULL THEN 1 ELSE 0 END, age, id").Find(&stages).Error
	return stages, err
}

func (s *CharacterLookService) SaveAgeStage(projectID, characterID, stageID uint, req AgeStageInput) (*models.CharacterAgeStage, error) {
	req.Name, req.Appearance = strings.TrimSpace(req.Name), strings.TrimSpace(req.Appearance)
	if req.Name == "" {
		return nil, fmt.Errorf("年龄阶段名称不能为空")
	}
	if req.Appearance == "" {
		return nil, fmt.Errorf("请填写该阶段的外貌描述（年龄、脸型、发型、身高体型）")
	}
	if req.Age != nil && (*req.Age < 1 || *req.Age > 120) {
		return nil, fmt.Errorf("年龄必须在1到120之间")
	}
	engine, err := normalizeAgeStagePortraitEngine(req.PortraitEngine)
	if err != nil {
		return nil, err
	}
	var count int64
	if err := s.db.Model(&models.Character{}).Where("id = ? AND project_id = ?", characterID, projectID).Count(&count).Error; err != nil {
		return nil, err
	}
	if count != 1 {
		return nil, fmt.Errorf("角色不存在")
	}
	if stageID == 0 {
		stage := models.CharacterAgeStage{ProjectID: projectID, CharacterID: characterID, Name: req.Name, Age: req.Age, Appearance: req.Appearance, PortraitEngine: engine, AuditStatus: models.LookStatusDraft, Version: 1}
		if err := s.db.Create(&stage).Error; err != nil {
			return nil, fmt.Errorf("保存年龄阶段失败（同一角色阶段名不能重复）: %w", err)
		}
		return &stage, nil
	}
	stage, err := s.loadAgeStage(s.db, projectID, characterID, stageID)
	if err != nil {
		return nil, err
	}
	appearanceChanged := stage.Appearance != req.Appearance || !sameOptionalInt(stage.Age, req.Age)
	updates := map[string]any{"name": req.Name, "age": req.Age, "appearance": req.Appearance, "portrait_engine": engine}
	if appearanceChanged || stage.PortraitEngine != engine {
		// 提示词由外貌与来源推导；旧提示词不再代表当前设计，审核需重新确认。
		updates["portrait_prompt"] = ""
		updates["audit_status"] = models.LookStatusDraft
		updates["audit_note"] = ""
		updates["version"] = gorm.Expr("version + 1")
	}
	if err := s.db.Model(stage).Updates(updates).Error; err != nil {
		return nil, fmt.Errorf("保存年龄阶段失败（同一角色阶段名不能重复）: %w", err)
	}
	if appearanceChanged || stage.Name != req.Name {
		if err := s.markAgeStageScenesStale(stage.ID, "角色年龄阶段外貌已修改"); err != nil {
			return nil, err
		}
	}
	return s.loadAgeStage(s.db, projectID, characterID, stage.ID)
}

func sameOptionalInt(a, b *int) bool {
	if a == nil || b == nil {
		return a == nil && b == nil
	}
	return *a == *b
}

func (s *CharacterLookService) DeleteAgeStage(projectID, characterID, stageID uint) error {
	stage, err := s.loadAgeStage(s.db, projectID, characterID, stageID)
	if err != nil {
		return err
	}
	var outfits int64
	if err := s.db.Model(&models.CharacterOutfit{}).Where("age_stage_id = ?", stage.ID).Count(&outfits).Error; err != nil {
		return err
	}
	if outfits > 0 {
		return fmt.Errorf("该年龄阶段下还有%d套服装；请先删除或改挂这些套装", outfits)
	}
	if stage.PortraitTaskID != "" && s.tasks != nil {
		_ = s.tasks.CancelTask(stage.PortraitTaskID)
	}
	return s.db.Delete(stage).Error
}

func (s *CharacterLookService) ApproveAgeStage(projectID, characterID, stageID uint) error {
	stage, err := s.loadAgeStage(s.db, projectID, characterID, stageID)
	if err != nil {
		return err
	}
	if strings.TrimSpace(stage.Portrait) == "" {
		return fmt.Errorf("请先生成或上传该阶段标准像")
	}
	return s.db.Model(stage).Updates(map[string]any{"audit_status": models.LookStatusApproved, "audit_note": ""}).Error
}

// AgeStagePortraitPrompt 返回当前保存的提示词（可能是用户改过的）与系统默认稿。
func (s *CharacterLookService) AgeStagePortraitPrompt(projectID, characterID, stageID uint) (saved, draft string, err error) {
	stage, err := s.loadAgeStage(s.db, projectID, characterID, stageID)
	if err != nil {
		return "", "", err
	}
	var project models.Project
	var ch models.Character
	if err := s.db.First(&project, projectID).Error; err != nil {
		return "", "", err
	}
	if err := s.db.Where("id = ? AND project_id = ?", characterID, projectID).First(&ch).Error; err != nil {
		return "", "", err
	}
	return stage.PortraitPrompt, ageStagePortraitPrompt(&project, &ch, stage), nil
}

func ageStagePortraitPrompt(project *models.Project, ch *models.Character, stage *models.CharacterAgeStage) string {
	label := ageStageLabel(stage)
	composition := "单一角色，正面肩部以上头像，头发与发型完整入镜，直视镜头，自然中性表情，衣领与肩部清晰，纯净背景，柔和均匀自然光，不出现文字或水印。"
	style := styleDescriptor(project.Style)
	if stage.PortraitEngine == "" || stage.PortraitEngine == AgeStagePortraitQwenEdit {
		return strings.Join(nonEmptyStrings([]string{
			fmt.Sprintf("<image1>是角色「%s」的主标准像，仅作为同一人物的血缘相似脸部特征（眼型、眉形、鼻型、脸部轮廓的延续）与项目画风参考；不沿用<image1>中的年龄、头身比例、发型与服装。", ch.Name),
			fmt.Sprintf("最终画面是同一人物在「%s」时的9:16竖版标准参考像。该阶段外貌以以下描述为准，年龄感、身高头身比例与面部成熟度必须严格符合该阶段：%s", label, stage.Appearance),
			style,
			composition,
		}), "\n\n")
	}
	parts := []string{}
	if stage.Age != nil && *stage.Age > 0 {
		parts = append(parts, fmt.Sprintf("%d岁人物", *stage.Age))
	}
	parts = append(parts, style, "人物外貌必须严格固定："+stage.Appearance, composition)
	return normalizePortraitStyle(strings.Join(nonEmptyStrings(parts), "，"), project)
}

func ageStagePortraitTemplate(engine string) string {
	switch engine {
	case ImageEngineKrea2:
		return "krea2_character_portrait"
	case ImageEngineQwen21:
		return TemplateQwen21T2I
	default:
		return TemplateQwen21MultiEdit
	}
}

// SaveAgeStagePortraitPrompt 只保存人工审阅后的阶段标准像提示词，不提交生成、不改动阶段标准像与审核状态。
func (s *CharacterLookService) SaveAgeStagePortraitPrompt(projectID, characterID, stageID uint, prompt string) error {
	stage, err := s.loadAgeStage(s.db, projectID, characterID, stageID)
	if err != nil {
		return err
	}
	prompt = strings.TrimSpace(prompt)
	if prompt == "" {
		return fmt.Errorf("提示词不能为空")
	}
	if (stage.PortraitEngine == "" || stage.PortraitEngine == AgeStagePortraitQwenEdit) && !strings.Contains(prompt, "<image1>") {
		return fmt.Errorf("以主标准像改年龄的提示词必须保留 <image1>")
	}
	return s.db.Model(&models.CharacterAgeStage{}).Where("id = ?", stage.ID).Update("portrait_prompt", prompt).Error
}

// StartAgeStagePortrait 提交阶段标准像任务。prompt 为空时使用已保存稿或默认稿；提交前由用户审阅。
func (s *CharacterLookService) StartAgeStagePortrait(projectID, characterID, stageID uint, prompt string) error {
	if s.tasks == nil {
		return fmt.Errorf("年龄阶段标准像生成依赖 ComfyUI 任务服务")
	}
	stage, err := s.loadAgeStage(s.db, projectID, characterID, stageID)
	if err != nil {
		return err
	}
	if stage.PortraitTaskID != "" {
		return fmt.Errorf("该阶段标准像正在生成，请勿重复提交")
	}
	var project models.Project
	var ch models.Character
	if err := s.db.First(&project, projectID).Error; err != nil {
		return err
	}
	if err := s.db.Where("id = ? AND project_id = ?", characterID, projectID).First(&ch).Error; err != nil {
		return err
	}
	engine, err := normalizeAgeStagePortraitEngine(stage.PortraitEngine)
	if err != nil {
		return err
	}
	prompt = strings.TrimSpace(prompt)
	if prompt == "" {
		prompt = strings.TrimSpace(stage.PortraitPrompt)
	}
	if prompt == "" {
		prompt = ageStagePortraitPrompt(&project, &ch, stage)
	}
	var files map[string][]FileMeta
	params := map[string]any{}
	switch engine {
	case AgeStagePortraitQwenEdit:
		if strings.TrimSpace(ch.Portrait) == "" {
			return fmt.Errorf("以主标准像改年龄需要角色「%s」先有标准像；也可改用文生图或上传", ch.Name)
		}
		if !strings.Contains(prompt, "<image1>") {
			return fmt.Errorf("提示词未绑定角色主标准像 <image1>，已阻止生成")
		}
		files = map[string][]FileMeta{"ref_images": {{TaskID: fmt.Sprint(projectID), Name: ch.Portrait}}}
		params = qwenPortraitParams()
	case ImageEngineQwen21:
		params = qwenPortraitParams()
	}
	tpl, err := enabledTemplateByCode(s.db, ageStagePortraitTemplate(engine))
	if err != nil {
		return err
	}
	task, err := s.tasks.CreateTask(CreateTaskReq{TemplateID: tpl.ID, Prompt: prompt, Params: params, Files: files})
	if err != nil {
		return err
	}
	res := s.db.Model(&models.CharacterAgeStage{}).Where("id = ? AND portrait_task_id = ''", stage.ID).
		Updates(map[string]any{"portrait_task_id": task.TaskID, "portrait_error": "", "portrait_prompt": prompt})
	if res.Error != nil || res.RowsAffected == 0 {
		_ = s.tasks.CancelTask(task.TaskID)
		if res.Error != nil {
			return res.Error
		}
		return fmt.Errorf("该阶段标准像任务已存在")
	}
	log.Printf("[age-stage-submit] project=%d character=%d stage=%d engine=%s files=%v", projectID, characterID, stage.ID, engine, files)
	go func() {
		if err := s.tasks.Execute(task.TaskID); err != nil && !errors.Is(err, errNoFreeGPU) {
			log.Printf("[age-stage %d] task %s failed: %v", stage.ID, task.TaskID, err)
		}
	}()
	return nil
}

func (s *CharacterLookService) SaveAgeStageUploadedPortrait(projectID, characterID, stageID uint, filename string, data []byte) error {
	if s.upload == nil {
		return fmt.Errorf("上传服务未配置")
	}
	ext := strings.ToLower(filepath.Ext(filename))
	if ext != ".png" && ext != ".jpg" && ext != ".jpeg" && ext != ".webp" {
		return fmt.Errorf("仅支持 PNG/JPG/WebP")
	}
	stage, err := s.loadAgeStage(s.db, projectID, characterID, stageID)
	if err != nil {
		return err
	}
	path, _, err := s.upload.SaveFile(fmt.Sprint(projectID), "image", fmt.Sprintf("age_stage_%d_%d%s", stage.ID, time.Now().UnixNano(), ext), data)
	if err != nil {
		return err
	}
	if stage.PortraitTaskID != "" && s.tasks != nil {
		_ = s.tasks.CancelTask(stage.PortraitTaskID)
	}
	return s.applyAgeStagePortrait(stage.ID, "", filepath.Base(path))
}

const ageStageOutfitOutdatedNote = "年龄阶段标准像已更换；已有定妆照与四视图保留，如需与新标准像一致请重新生成"

func ageStagePortraitHistory(raw string) []string {
	var files []string
	if strings.TrimSpace(raw) != "" {
		_ = json.Unmarshal([]byte(raw), &files)
	}
	return files
}

// pushAgeStagePortraitHistory 把被替换的标准像放到历史最前面，并从历史中移除即将成为当前的那张。
func pushAgeStagePortraitHistory(raw, previous, next string) string {
	out := []string{}
	if previous != "" && previous != next {
		out = append(out, previous)
	}
	for _, file := range ageStagePortraitHistory(raw) {
		if file != "" && file != next && file != previous {
			out = append(out, file)
		}
	}
	if len(out) == 0 {
		return ""
	}
	body, _ := json.Marshal(out)
	return string(body)
}

// RestoreAgeStagePortrait 把历史中的某张阶段标准像恢复为当前；当前这张进入历史，不删除任何文件。
func (s *CharacterLookService) RestoreAgeStagePortrait(projectID, characterID, stageID uint, file string) error {
	stage, err := s.loadAgeStage(s.db, projectID, characterID, stageID)
	if err != nil {
		return err
	}
	if stage.PortraitTaskID != "" {
		return fmt.Errorf("该阶段标准像正在生成，请完成后再恢复历史版本")
	}
	file = strings.TrimSpace(file)
	for _, old := range ageStagePortraitHistory(stage.PortraitHistory) {
		if old == file && file != "" {
			return s.applyAgeStagePortrait(stage.ID, "", file)
		}
	}
	return fmt.Errorf("该图片不在此阶段的历史标准像中")
}

// applyAgeStagePortrait 写入新阶段标准像，旧标准像进入历史（不删除文件）。阶段需重新审核；
// 其下套装已有的定妆照/四视图保留，只取消在途任务并提示可重新生成。
// expectTaskID 非空时只在任务仍是当前任务时写入，防止旧任务覆盖新结果。
func (s *CharacterLookService) applyAgeStagePortrait(stageID uint, expectTaskID, file string) error {
	var cancel []string
	err := s.db.Transaction(func(tx *gorm.DB) error {
		var current models.CharacterAgeStage
		if err := tx.First(&current, stageID).Error; err != nil {
			return err
		}
		if expectTaskID != "" && current.PortraitTaskID != expectTaskID {
			return nil
		}
		q := tx.Model(&models.CharacterAgeStage{}).Where("id = ?", stageID)
		if expectTaskID != "" {
			q = q.Where("portrait_task_id = ?", expectTaskID)
		}
		res := q.Updates(map[string]any{"portrait": file, "portrait_history": pushAgeStagePortraitHistory(current.PortraitHistory, current.Portrait, file), "portrait_task_id": "", "portrait_error": "", "audit_status": models.LookStatusDraft, "audit_note": "", "version": gorm.Expr("version + 1")})
		if res.Error != nil || res.RowsAffected == 0 {
			return res.Error
		}
		// 已生成的套装定妆照/四视图属于用户资产，保留不删；只取消以旧标准像在途的任务并提示可重新生成。
		var outfits []models.CharacterOutfit
		if err := tx.Where("age_stage_id = ?", stageID).Find(&outfits).Error; err != nil {
			return err
		}
		for _, outfit := range outfits {
			cancel = append(cancel, nonEmptyStrings([]string{outfit.ImageTaskID, outfit.SheetTaskID})...)
			if err := tx.Model(&outfit).Updates(map[string]any{"image_task_id": "", "image_error": ageStageOutfitOutdatedNote, "sheet_task_id": ""}).Error; err != nil {
				return err
			}
		}
		return nil
	})
	if err == nil && s.tasks != nil {
		for _, id := range cancel {
			_ = s.tasks.CancelTask(id)
		}
	}
	return err
}

func (s *CharacterLookService) SyncAgeStagePortraits() {
	if !s.db.Migrator().HasTable(&models.CharacterAgeStage{}) {
		return
	}
	var stages []models.CharacterAgeStage
	if s.db.Where("portrait_task_id != ''").Find(&stages).Error != nil {
		return
	}
	for i := range stages {
		stage := &stages[i]
		fail := func(reason string) {
			s.db.Model(stage).Where("portrait_task_id = ?", stage.PortraitTaskID).Updates(map[string]any{"portrait_task_id": "", "portrait_error": reason})
		}
		var task models.Task
		if s.db.Where("task_id = ?", stage.PortraitTaskID).First(&task).Error != nil {
			fail("任务不存在")
			continue
		}
		if task.Status == "failed" || task.Status == "cancelled" {
			fail(task.Error)
			continue
		}
		if task.Status != "success" {
			continue
		}
		file, _ := resultImageOf(&task)
		if file == "" || task.Port == nil || s.upload == nil {
			fail("任务成功但未返回图片")
			continue
		}
		sub, name := filepath.ToSlash(filepath.Dir(file)), filepath.Base(file)
		if sub == "." {
			sub = ""
		}
		data, err := NewComfyClient(s.tasks.comfyHostForTask(&task), *task.Port).DownloadOutput(name, sub, "output")
		if err != nil {
			fail(err.Error())
			continue
		}
		ext := filepath.Ext(file)
		if ext == "" {
			ext = ".png"
		}
		path, _, err := s.upload.SaveFile(fmt.Sprint(stage.ProjectID), "image", outfitResultFilename("age_stage", stage.ID, stage.PortraitTaskID, ext), data)
		if err != nil {
			fail(err.Error())
			continue
		}
		if err := s.applyAgeStagePortrait(stage.ID, stage.PortraitTaskID, filepath.Base(path)); err != nil {
			log.Printf("[age-stage %d] apply portrait failed: %v", stage.ID, err)
		}
	}
}

// sceneAgeStage 是场景所选套装所属的年龄阶段及该套装名。
type sceneAgeStage struct {
	Stage      models.CharacterAgeStage
	OutfitName string
}

// sceneAgeStagesByCharacter 返回场景开场造型（Shot 1 覆盖优先，见 ADR-0012）属于年龄阶段的角色
// （按角色 ID）。只认已审核套装，与参考图选择一致；查询失败（含旧库无此表）时返回空表，
// 调用方保持原有角色主档案输出。
func sceneAgeStagesByCharacter(db *gorm.DB, sc *models.Scene) map[uint]sceneAgeStage {
	out := map[uint]sceneAgeStage{}
	for _, outfit := range sceneOpeningOutfits(db, sc) {
		if outfit.AgeStageID == 0 {
			continue
		}
		var stage models.CharacterAgeStage
		if db.Where("id = ? AND project_id = ? AND character_id = ?", outfit.AgeStageID, sc.ProjectID, outfit.CharacterID).First(&stage).Error == nil {
			out[outfit.CharacterID] = sceneAgeStage{Stage: stage, OutfitName: outfit.Name}
		}
	}
	return out
}

// ageStageCharacterDescription 替代角色主档案的 Trait/Style：阶段外貌 + 本场套装名。
// 套装细节由套装参考图承担，此处不重复描述服装。
func ageStageCharacterDescription(selected sceneAgeStage) string {
	desc := ageStageLabel(&selected.Stage) + "：" + strings.TrimSpace(selected.Stage.Appearance)
	if name := strings.TrimSpace(selected.OutfitName); name != "" {
		desc += "；本场造型：" + name
	}
	return desc
}

// markAgeStageScenesStale 使选用该阶段套装的场景提示词过期；只断开在途任务，已生成媒体保留。
func (s *CharacterLookService) markAgeStageScenesStale(stageID uint, reason string) error {
	var sceneIDs []uint
	if err := s.db.Model(&models.SceneCharacterOutfit{}).
		Joins("JOIN character_outfits ON character_outfits.id = scene_character_outfits.outfit_id").
		Where("character_outfits.age_stage_id = ?", stageID).Distinct().Pluck("scene_character_outfits.scene_id", &sceneIDs).Error; err != nil {
		return err
	}
	for _, sceneID := range sceneIDs {
		if err := s.markSceneOutfitSourceChanged(sceneID, reason); err != nil {
			return err
		}
	}
	return nil
}

func (s *CharacterLookService) markSceneOutfitSourceChanged(sceneID uint, reason string) error {
	var scene models.Scene
	if err := s.db.First(&scene, sceneID).Error; err != nil {
		return err
	}
	updates := map[string]any{"prompt_stale": true}
	cancel := staleSceneTaskUpdates(s.db, &scene, updates)
	if err := s.db.Model(&models.Scene{}).Where("id = ?", sceneID).Updates(updates).Error; err != nil {
		return err
	}
	cancelSceneTasks(s.tasks, cancel)
	return markSceneMediaSourceChanged(s.db, scene.ProjectID, sceneID, "", reason)
}
