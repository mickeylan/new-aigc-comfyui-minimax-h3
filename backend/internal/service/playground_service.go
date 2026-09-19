package service

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log"
	"net/http"
	"path/filepath"
	"strconv"
	"strings"

	"github.com/gin-gonic/gin"
	"gorm.io/gorm"

	"comfyui-console/internal/models"
)

const MaxPlaygroundBatch = 4

type PlaygroundService struct {
	db        *gorm.DB
	tasks     *TaskService
	materials *MaterialService
}

type CreatePlaygroundRunInput struct {
	Mode       string                `json:"mode"`
	Template   string                `json:"template"`
	Prompt     string                `json:"prompt"`
	Params     map[string]any        `json:"params"`
	Files      map[string][]FileMeta `json:"files"`
	BatchCount int                   `json:"batch_count"`
}

func NewPlaygroundService(db *gorm.DB, tasks *TaskService, materials *MaterialService) *PlaygroundService {
	return &PlaygroundService{db: db, tasks: tasks, materials: materials}
}

func (s *PlaygroundService) Create(input CreatePlaygroundRunInput) ([]models.PlaygroundRun, error) {
	input.Mode = strings.TrimSpace(input.Mode)
	input.Template = strings.TrimSpace(input.Template)
	input.Prompt = strings.TrimSpace(input.Prompt)
	if input.Template == "" || input.Mode == "" {
		return nil, fmt.Errorf("mode and template are required")
	}
	if input.BatchCount == 0 {
		input.BatchCount = 1
	}
	if input.BatchCount < 1 || input.BatchCount > MaxPlaygroundBatch {
		return nil, fmt.Errorf("batch_count must be between 1 and %d", MaxPlaygroundBatch)
	}
	if s.tasks == nil {
		return nil, fmt.Errorf("task service unavailable")
	}

	var template models.Template
	if err := s.db.Where("code = ? AND enabled = ?", input.Template, true).First(&template).Error; err != nil {
		return nil, fmt.Errorf("template is not available in model catalog")
	}
	entry, err := catalogEntryFromTemplate(template)
	if err != nil {
		return nil, fmt.Errorf("invalid template catalog entry: %w", err)
	}
	if entry.Mode != input.Mode {
		return nil, fmt.Errorf("template %s does not support mode %s", input.Template, input.Mode)
	}

	paramsJSON, err := json.Marshal(nonNilMap(input.Params))
	if err != nil {
		return nil, fmt.Errorf("invalid params: %w", err)
	}
	filesJSON, err := json.Marshal(nonNilFiles(input.Files))
	if err != nil {
		return nil, fmt.Errorf("invalid files: %w", err)
	}

	runs := make([]models.PlaygroundRun, 0, input.BatchCount)
	for i := 0; i < input.BatchCount; i++ {
		// CreateTask copies the request into its own persisted snapshot and performs
		// template input validation, including required references and limits.
		task, createErr := s.tasks.CreateTask(CreateTaskReq{
			TemplateID: template.ID,
			Prompt:     input.Prompt,
			Params:     cloneAnyMap(input.Params),
			Files:      input.Files,
		})
		if createErr != nil {
			s.cleanupCreatedBatch(runs)
			return nil, createErr
		}
		run := models.PlaygroundRun{
			Mode: input.Mode, Template: template.Code, Prompt: input.Prompt,
			Params: paramsJSON, Files: filesJSON, TaskID: task.TaskID,
			Status: task.Status, Result: json.RawMessage(`[]`),
		}
		if err := s.db.Create(&run).Error; err != nil {
			_ = s.db.Delete(task).Error
			s.cleanupCreatedBatch(runs)
			return nil, err
		}
		runs = append(runs, run)
	}
	return runs, nil
}

func (s *PlaygroundService) cleanupCreatedBatch(runs []models.PlaygroundRun) {
	for _, run := range runs {
		_ = s.db.Where("task_id = ? AND status = ?", run.TaskID, "pending").Delete(&models.Task{}).Error
		_ = s.db.Delete(&models.PlaygroundRun{}, run.ID).Error
	}
}

func nonNilMap(value map[string]any) map[string]any {
	if value == nil {
		return map[string]any{}
	}
	return value
}

func nonNilFiles(value map[string][]FileMeta) map[string][]FileMeta {
	if value == nil {
		return map[string][]FileMeta{}
	}
	return value
}

func cloneAnyMap(value map[string]any) map[string]any {
	copy := make(map[string]any, len(value))
	for key, item := range value {
		copy[key] = item
	}
	return copy
}

func (s *PlaygroundService) List(mode string, starredOnly bool, limit int) ([]models.PlaygroundRun, error) {
	if limit < 1 || limit > 100 {
		limit = 50
	}
	var runs []models.PlaygroundRun
	query := s.db.Order("id desc").Limit(limit)
	if mode != "" {
		query = query.Where("mode = ?", mode)
	}
	if starredOnly {
		query = query.Where("starred = ?", true)
	}
	if err := query.Find(&runs).Error; err != nil {
		return nil, err
	}
	for i := range runs {
		if err := s.sync(&runs[i]); err != nil {
			return nil, err
		}
	}
	return runs, nil
}

func (s *PlaygroundService) Get(id uint) (*models.PlaygroundRun, error) {
	var run models.PlaygroundRun
	if err := s.db.First(&run, id).Error; err != nil {
		return nil, err
	}
	if err := s.sync(&run); err != nil {
		return nil, err
	}
	return &run, nil
}

func (s *PlaygroundService) sync(run *models.PlaygroundRun) error {
	var task models.Task
	if err := s.db.Where("task_id = ?", run.TaskID).First(&task).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil
		}
		return err
	}
	result := json.RawMessage(task.ResultFiles)
	if len(result) == 0 || !json.Valid(result) {
		result = json.RawMessage(`[]`)
	}
	if run.Status != task.Status || string(run.Result) != string(result) {
		if err := s.db.Model(run).Updates(map[string]any{"status": task.Status, "result": []byte(result)}).Error; err != nil {
			return err
		}
		run.Status, run.Result = task.Status, result
	}
	return nil
}

func (s *PlaygroundService) SetStarred(id uint, starred bool) (*models.PlaygroundRun, error) {
	run, err := s.Get(id)
	if err != nil {
		return nil, err
	}
	if err := s.db.Model(run).Update("starred", starred).Error; err != nil {
		return nil, err
	}
	run.Starred = starred
	return run, nil
}

func (s *PlaygroundService) Promote(id uint, resultIndex int) (*models.Material, error) {
	run, err := s.Get(id)
	if err != nil {
		return nil, err
	}
	if run.Status != "success" {
		return nil, fmt.Errorf("only successful runs can be promoted")
	}
	var results []map[string]any
	if err := json.Unmarshal(run.Result, &results); err != nil || len(results) == 0 {
		return nil, fmt.Errorf("run has no result")
	}
	if resultIndex < 0 || resultIndex >= len(results) {
		return nil, fmt.Errorf("invalid result_index")
	}
	filename := filepath.Base(fmt.Sprint(results[resultIndex]["filename"]))
	if filename == "." || filename == "" {
		return nil, fmt.Errorf("result filename is invalid")
	}
	subfolder := strings.Trim(strings.ReplaceAll(fmt.Sprint(results[resultIndex]["subfolder"]), "\\", "/"), "/")
	cleanSubfolder := filepath.ToSlash(filepath.Clean(filepath.FromSlash(subfolder)))
	if cleanSubfolder == "." {
		cleanSubfolder = ""
	}
	if filepath.IsAbs(filepath.FromSlash(cleanSubfolder)) || cleanSubfolder == ".." || strings.HasPrefix(cleanSubfolder, "../") || strings.Contains(cleanSubfolder, "/../") {
		return nil, fmt.Errorf("result subfolder is invalid")
	}
	subfolder = cleanSubfolder
	kind := resultMaterialType(fmt.Sprint(results[resultIndex]["type"]), filename)

	var task models.Task
	if err := s.db.Where("task_id = ?", run.TaskID).First(&task).Error; err != nil {
		return nil, err
	}
	if s.materials != nil && s.materials.cfg != nil && s.materials.remote != nil && s.materials.upload != nil && task.GPUIndex != nil {
		outputRoot := filepath.Join(s.materials.cfg.Comfy.ComfyDir, "output_workers", fmt.Sprintf("gpu%d", *task.GPUIndex))
		outputPath := filepath.Join(outputRoot, filepath.FromSlash(subfolder), filename)
		rel, relErr := filepath.Rel(outputRoot, outputPath)
		if relErr != nil || rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
			return nil, fmt.Errorf("result path escapes output directory")
		}
		file, openErr := s.materials.remote.Open(outputPath)
		if openErr != nil {
			return nil, openErr
		}
		data, readErr := io.ReadAll(file)
		_ = file.Close()
		if readErr != nil {
			return nil, readErr
		}
		material, saveErr := s.materials.SaveUpload(nil, kind, filename, data)
		if saveErr != nil {
			return nil, saveErr
		}
		if err := s.db.Model(material).Updates(map[string]any{"source": "playground", "prompt": run.Prompt}).Error; err != nil {
			return nil, err
		}
		material.Source, material.Prompt = "playground", run.Prompt
		return material, nil
	}

	// Keeps the operation testable with a DB-only service; production services take
	// the branch above and copy the output into the shared input-backed material store.
	material := models.Material{
		Name: filename, Type: kind, Source: "playground", Prompt: run.Prompt,
		Path: filepath.ToSlash(filepath.Join(run.TaskID, subfolder, filename)),
	}
	if err := s.db.Create(&material).Error; err != nil {
		return nil, err
	}
	return &material, nil
}

func (s *Service) playground() *PlaygroundService {
	return NewPlaygroundService(s.DB, s.Tasks, s.Materials)
}

func (s *Service) HandleListPlaygroundRuns(c *gin.Context) {
	limit, _ := strconv.Atoi(c.DefaultQuery("limit", "50"))
	runs, err := s.playground().List(c.Query("mode"), c.Query("starred") == "true", limit)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{"items": runs})
}

func (s *Service) HandleCreatePlaygroundRun(c *gin.Context) {
	var input CreatePlaygroundRunInput
	if err := c.ShouldBindJSON(&input); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid request: " + err.Error()})
		return
	}
	runs, err := s.playground().Create(input)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	for _, run := range runs {
		taskID := run.TaskID
		go func() {
			if err := s.Tasks.Execute(taskID); err != nil {
				log.Printf("[playground] task %s execute failed: %v", taskID, err)
			}
		}()
	}
	c.JSON(http.StatusCreated, gin.H{"items": runs})
}

func playgroundRunID(c *gin.Context) (uint, bool) {
	value, err := strconv.ParseUint(c.Param("id"), 10, 32)
	if err != nil || value == 0 {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid id"})
		return 0, false
	}
	return uint(value), true
}

func (s *Service) HandleGetPlaygroundRun(c *gin.Context) {
	id, ok := playgroundRunID(c)
	if !ok {
		return
	}
	run, err := s.playground().Get(id)
	if errors.Is(err, gorm.ErrRecordNotFound) {
		c.JSON(http.StatusNotFound, gin.H{"error": "playground run not found"})
		return
	}
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, run)
}

func (s *Service) HandleStarPlaygroundRun(c *gin.Context) {
	id, ok := playgroundRunID(c)
	if !ok {
		return
	}
	request := struct {
		Starred *bool `json:"starred"`
	}{}
	_ = c.ShouldBindJSON(&request)
	starred := true
	if request.Starred != nil {
		starred = *request.Starred
	}
	run, err := s.playground().SetStarred(id, starred)
	if errors.Is(err, gorm.ErrRecordNotFound) {
		c.JSON(http.StatusNotFound, gin.H{"error": "playground run not found"})
		return
	}
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, run)
}

func (s *Service) HandlePromotePlaygroundRun(c *gin.Context) {
	id, ok := playgroundRunID(c)
	if !ok {
		return
	}
	request := struct {
		ResultIndex int `json:"result_index"`
	}{}
	_ = c.ShouldBindJSON(&request)
	material, err := s.playground().Promote(id, request.ResultIndex)
	if errors.Is(err, gorm.ErrRecordNotFound) {
		c.JSON(http.StatusNotFound, gin.H{"error": "playground run not found"})
		return
	}
	if err != nil {
		c.JSON(http.StatusConflict, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusCreated, material)
}

func resultMaterialType(raw, filename string) string {
	raw = strings.ToLower(strings.TrimSpace(raw))
	switch raw {
	case "image", "images", "gif", "gifs":
		return "image"
	case "video", "videos":
		return "video"
	case "audio", "audios":
		return "audio"
	}
	switch strings.ToLower(filepath.Ext(filename)) {
	case ".mp4", ".webm", ".mov":
		return "video"
	case ".mp3", ".wav", ".flac", ".m4a":
		return "audio"
	default:
		return "image"
	}
}
