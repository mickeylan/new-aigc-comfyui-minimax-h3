package service

import (
	"encoding/json"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"strings"
	"sync"

	"comfyui-console/internal/models"
	"github.com/gin-gonic/gin"
	"gorm.io/gorm"
)

const studioLocationKey = "_studio_location"

var studioLocationCaptureMu sync.Mutex

type studioLocationInput struct {
	TemplateCode   string          `json:"template_code"`
	TemplateSHA256 string          `json:"template_sha256"`
	Prompt         string          `json:"prompt"`
	Reviewed       bool            `json:"reviewed"`
	ShotID         uint            `json:"shot_id"`
	Snapshot       FileMeta        `json:"snapshot"`
	References     []FileMeta      `json:"references"`
	Camera         json.RawMessage `json:"camera"`
}
type studioLocationSource struct {
	Source          string          `json:"source"`
	ProjectID       uint            `json:"project_id"`
	SceneID         uint            `json:"scene_id"`
	ShotID          uint            `json:"shot_id,omitempty"`
	TemplateCode    string          `json:"template_code"`
	TemplateSHA256  string          `json:"template_sha256"`
	PromptSHA256    string          `json:"prompt_sha256"`
	ReferenceSHA256 []string        `json:"reference_sha256"`
	Camera          json.RawMessage `json:"camera,omitempty"`
}
type studioLocationTemplate struct {
	Code            string `json:"code"`
	Name            string `json:"name"`
	MaxRefs         int    `json:"max_refs"`
	ReferenceSyntax string `json:"reference_syntax"`
	TemplateSHA256  string `json:"template_sha256"`
}

// Only engines whose ordered image-edit contract is already integrated are offered.
func (s *Service) studioLocationTemplates() []studioLocationTemplate {
	rows := []studioLocationTemplate{}
	for _, code := range []string{"qwen_image_2_1_multi_edit", "minimax_h3_image_edit"} {
		tpl, err := enabledTemplateByCode(s.DB, code)
		if err != nil {
			continue
		}
		var inputs []TemplateInput
		if json.Unmarshal([]byte(tpl.InputsJSON), &inputs) != nil {
			continue
		}
		for _, in := range inputs {
			if in.Key == "ref_images" && in.MaxCount > 0 {
				syntax := "<imageN>"
				if code == "minimax_h3_image_edit" {
					syntax = "<Picture N>"
				}
				rows = append(rows, studioLocationTemplate{code, tpl.Name, in.MaxCount, syntax, sha256Hex([]byte(tpl.WorkflowJSON + tpl.InputsJSON))})
			}
		}
	}
	return rows
}
func (s *Service) HandleStudioLocationTemplates(c *gin.Context) {
	c.JSON(200, gin.H{"items": s.studioLocationTemplates()})
}

func (s *Service) studioLocationRequest(scene *models.Scene, in studioLocationInput) (CreateTaskReq, error) {
	if !in.Reviewed || strings.TrimSpace(in.Prompt) == "" {
		return CreateTaskReq{}, fmt.Errorf("请填写提示词并人工确认取景、参考图与提示词")
	}
	if in.ShotID != 0 {
		var shot models.Shot
		if err := s.DB.Where("id = ? AND scene_id = ?", in.ShotID, scene.ID).First(&shot).Error; err != nil {
			return CreateTaskReq{}, fmt.Errorf("镜头不属于所选场次")
		}
	}
	limit := 0
	for _, item := range s.studioLocationTemplates() {
		if item.Code == in.TemplateCode {
			limit = item.MaxRefs
		}
	}
	refs := append([]FileMeta{in.Snapshot}, in.References...)
	if limit == 0 || len(refs) > limit {
		return CreateTaskReq{}, fmt.Errorf("模板不可用或参考图超出容量（含首张取景图），禁止截断")
	}
	tpl, err := enabledTemplateByCode(s.DB, in.TemplateCode)
	if err != nil {
		return CreateTaskReq{}, err
	}
	if in.TemplateSHA256 != sha256Hex([]byte(tpl.WorkflowJSON+tpl.InputsJSON)) {
		return CreateTaskReq{}, fmt.Errorf("模板已变化，请刷新模板并重新审阅")
	}
	source := studioLocationSource{Source: "studio_location", ProjectID: scene.ProjectID, SceneID: scene.ID, ShotID: in.ShotID, TemplateCode: tpl.Code, TemplateSHA256: sha256Hex([]byte(tpl.WorkflowJSON + tpl.InputsJSON)), PromptSHA256: sha256Hex([]byte(in.Prompt)), Camera: in.Camera}
	if s.Upload == nil {
		return CreateTaskReq{}, fmt.Errorf("上传服务未配置")
	}
	for _, ref := range refs {
		if _, err := safeFileSegment(ref.TaskID, "task_id"); err != nil {
			return CreateTaskReq{}, err
		}
		if _, err := safeFileSegment(ref.Name, "name"); err != nil {
			return CreateTaskReq{}, err
		}
		var upload models.UploadFile
		if err := s.DB.Where("task_id = ? AND name = ? AND type = ?", ref.TaskID, ref.Name, "image").First(&upload).Error; err != nil {
			return CreateTaskReq{}, fmt.Errorf("参考图未上传或不存在")
		}
		data, err := os.ReadFile(filepath.Join(s.Upload.InputDir(), ref.TaskID, ref.Name))
		if err != nil {
			return CreateTaskReq{}, fmt.Errorf("参考图无法读取: %w", err)
		}
		if uploadedSceneImageExt(data) == "" {
			return CreateTaskReq{}, fmt.Errorf("参考图不是有效图片")
		}
		source.ReferenceSHA256 = append(source.ReferenceSHA256, sha256Hex(data))
	}
	return CreateTaskReq{TemplateID: tpl.ID, Prompt: in.Prompt, Params: map[string]any{"prompt": in.Prompt, studioLocationKey: source}, Files: map[string][]FileMeta{"ref_images": refs}}, nil
}
func (s *Service) HandleCreateStudioLocation(c *gin.Context) {
	scene, ok := s.loadScene(c)
	if !ok {
		return
	}
	var in studioLocationInput
	if err := c.ShouldBindJSON(&in); err != nil {
		c.JSON(400, gin.H{"error": err.Error()})
		return
	}
	req, err := s.studioLocationRequest(scene, in)
	if err != nil {
		c.JSON(400, gin.H{"error": err.Error()})
		return
	}
	task, err := s.Tasks.CreateTask(req)
	if err != nil {
		c.JSON(400, gin.H{"error": err.Error()})
		return
	}
	go func() {
		if err := s.Tasks.Execute(task.TaskID); err != nil {
			log.Printf("[studio-location] execute %s: %v", task.TaskID, err)
		}
	}()
	c.JSON(200, gin.H{"task_id": task.TaskID})
}

// Check again when a queued task actually executes: edits must never silently change reviewed inputs.
func (s *TaskService) validateStudioLocationSnapshot(task *models.Task, tpl *models.Template) error {
	var params map[string]json.RawMessage
	if err := json.Unmarshal([]byte(task.ParamsJSON), &params); err != nil {
		return err
	}
	if _, ok := params[studioLocationKey]; !ok {
		return nil
	}
	source, err := studioLocationOf(task)
	if err != nil {
		return err
	}
	if !tpl.Enabled || source.TemplateCode != tpl.Code || source.TemplateSHA256 != sha256Hex([]byte(tpl.WorkflowJSON+tpl.InputsJSON)) || source.PromptSHA256 != sha256Hex([]byte(task.Prompt)) {
		return fmt.Errorf("取景生成模板或提示词已变化，请重新审阅")
	}
	var files map[string][]FileMeta
	if err := json.Unmarshal([]byte(task.InputsJSON), &files); err != nil {
		return err
	}
	refs := files["ref_images"]
	if len(refs) != len(source.ReferenceSHA256) || len(refs) == 0 {
		return fmt.Errorf("取景生成参考图快照损坏")
	}
	for i, ref := range refs {
		if _, err := safeFileSegment(ref.TaskID, "task_id"); err != nil {
			return err
		}
		if _, err := safeFileSegment(ref.Name, "name"); err != nil {
			return err
		}
		data, err := os.ReadFile(filepath.Join(s.cfg.AssetsRoot(), ref.TaskID, ref.Name))
		if err != nil {
			return err
		}
		if sha256Hex(data) != source.ReferenceSHA256[i] {
			return fmt.Errorf("取景生成参考图已变化，请重新审阅")
		}
	}
	return nil
}

func studioLocationOf(task *models.Task) (studioLocationSource, error) {
	var params map[string]json.RawMessage
	var source studioLocationSource
	if err := json.Unmarshal([]byte(task.ParamsJSON), &params); err != nil {
		return source, err
	}
	if err := json.Unmarshal(params[studioLocationKey], &source); err != nil {
		return source, err
	}
	if source.Source != "studio_location" || source.SceneID == 0 || source.ProjectID == 0 {
		return source, fmt.Errorf("不是取景生成任务")
	}
	return source, nil
}

// Retain output as a pending take only. Adoption continues through SelectCurrent.
func (s *ProjectService) captureStudioLocation(task *models.Task) (*models.GenerationCandidate, error) {
	studioLocationCaptureMu.Lock()
	defer studioLocationCaptureMu.Unlock()
	source, err := studioLocationOf(task)
	if err != nil {
		return nil, err
	}
	var scene models.Scene
	if err := s.db.Where("id = ? AND project_id = ?", source.SceneID, source.ProjectID).First(&scene).Error; err != nil {
		return nil, err
	}
	var row models.GenerationCandidate
	query := s.db.Where("project_id = ? AND task_id = ? AND media_type = ?", source.ProjectID, task.TaskID, "image")
	if err := query.First(&row).Error; err == nil {
		return &row, nil
	} else if err != gorm.ErrRecordNotFound {
		return nil, err
	}
	if task.Status != "success" {
		return nil, fmt.Errorf("任务尚未成功")
	}
	if s.tasks == nil || s.upload == nil {
		return nil, fmt.Errorf("结果存储服务未配置")
	}
	data, err := s.tasks.downloadTaskOutput(task)
	if err != nil {
		return nil, err
	}
	ext := uploadedSceneImageExt(data)
	if ext == "" {
		return nil, fmt.Errorf("任务未返回有效图片")
	}
	name := "location_" + task.TaskID + ext
	path, _, err := s.upload.SaveFile(fmt.Sprint(source.ProjectID), "image", name, data)
	if err != nil {
		return nil, err
	}
	err = s.db.Transaction(func(tx *gorm.DB) error {
		if err := tx.Where("project_id = ? AND task_id = ? AND media_type = ?", source.ProjectID, task.TaskID, "image").First(&row).Error; err == nil {
			return nil
		} else if err != gorm.ErrRecordNotFound {
			return err
		}
		row = models.GenerationCandidate{ProjectID: source.ProjectID, EntityType: "scene", EntityID: source.SceneID, MediaType: "image", TaskID: task.TaskID, File: filepath.Base(path), PromptSnapshot: task.Prompt, ReferencesJSON: task.InputsJSON, ParamsJSON: task.ParamsJSON, ProvenanceJSON: snapshotJSON(source), ReviewStatus: "pending", IsCurrent: false}
		return tx.Create(&row).Error
	})
	return &row, err
}
func (s *Service) HandleGetStudioLocation(c *gin.Context) {
	scene, ok := s.loadScene(c)
	if !ok {
		return
	}
	if c.Param("tid") == "" {
		var tasks []models.Task
		pattern := fmt.Sprintf("%%\"project_id\":%d,%%\"scene_id\":%d,%%", scene.ProjectID, scene.ID)
		if err := s.DB.Where("params_json LIKE ? AND params_json LIKE ?", "%\"_studio_location\"%", pattern).Order("id DESC").Limit(50).Find(&tasks).Error; err != nil {
			c.JSON(500, gin.H{"error": err.Error()})
			return
		}
		items := []gin.H{}
		for i := range tasks {
			source, err := studioLocationOf(&tasks[i])
			if err == nil && source.ProjectID == scene.ProjectID && source.SceneID == scene.ID {
				items = append(items, gin.H{"task_id": tasks[i].TaskID, "status": tasks[i].Status, "prompt": tasks[i].Prompt, "shot_id": source.ShotID})
			}
		}
		c.JSON(200, gin.H{"items": items})
		return
	}
	var task models.Task
	if err := s.DB.Where("task_id = ?", c.Param("tid")).First(&task).Error; err != nil {
		c.JSON(404, gin.H{"error": "任务不存在"})
		return
	}
	source, err := studioLocationOf(&task)
	if err != nil || source.ProjectID != scene.ProjectID || source.SceneID != scene.ID {
		c.JSON(404, gin.H{"error": "任务不属于此场次"})
		return
	}
	out := gin.H{"task_id": task.TaskID, "status": task.Status, "error": task.Error}
	if task.Status == "success" {
		candidate, err := s.Projects.captureStudioLocation(&task)
		if err != nil {
			c.JSON(409, gin.H{"error": "结果登记失败: " + err.Error()})
			return
		}
		out["candidate"] = candidate
		out["result_url"] = fmt.Sprintf("/api/input/%d/%s", scene.ProjectID, candidate.File)
	}
	c.JSON(200, out)
}

func (s *ProjectService) syncStudioLocations() {
	var tasks []models.Task
	// Task metadata is durable, so completion also survives leaving the Studio page.
	if s.db.Where("status = ? AND params_json LIKE ? AND task_id NOT IN (?)", "success", "%\"_studio_location\"%", s.db.Model(&models.GenerationCandidate{}).Select("task_id")).Find(&tasks).Error != nil {
		return
	}
	for i := range tasks {
		if _, err := s.captureStudioLocation(&tasks[i]); err != nil {
			log.Printf("[studio-location] register %s: %v", tasks[i].TaskID, err)
		}
	}
}
