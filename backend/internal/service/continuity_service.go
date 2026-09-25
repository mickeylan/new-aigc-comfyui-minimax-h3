package service

import (
	"fmt"
	"path/filepath"
	"regexp"
	"strings"
	"time"

	"gorm.io/gorm"

	"comfyui-console/internal/config"
	"comfyui-console/internal/models"
)

const DefaultCandidateFrameCount = 22

var continuityTaskIDSanitizer = regexp.MustCompile(`[^A-Za-z0-9_-]+`)

type ContinuityService struct {
	cfg    *config.Config
	db     *gorm.DB
	remote *RemoteExec
	upload *UploadManager
}

func NewContinuityService(cfg *config.Config, db *gorm.DB, remote *RemoteExec, uploads ...*UploadManager) *ContinuityService {
	var upload *UploadManager
	if len(uploads) > 0 {
		upload = uploads[0]
	}
	return &ContinuityService{cfg: cfg, db: db, remote: remote, upload: upload}
}

func (s *ContinuityService) previousScene(scene *models.Scene) (*models.Scene, error) {
	var previous models.Scene
	result := s.db.Where("project_id = ? AND episode_n = ? AND generation = ? AND `order` < ?", scene.ProjectID, scene.EpisodeN, scene.Generation, scene.Order).Order("`order` DESC").Limit(1).Find(&previous)
	if result.Error != nil {
		return nil, result.Error
	}
	if result.RowsAffected == 0 {
		return nil, nil
	}
	return &previous, nil
}

func (s *ContinuityService) ExtractFrameCandidates(projectID, sceneID uint, count int) ([]models.FrameCandidate, error) {
	if count <= 0 || count > DefaultCandidateFrameCount {
		count = DefaultCandidateFrameCount
	}
	var scene models.Scene
	if err := s.db.Where("id = ? AND project_id = ?", sceneID, projectID).First(&scene).Error; err != nil {
		return nil, fmt.Errorf("场景不存在")
	}
	if scene.VideoInputFile == "" || scene.VideoTaskID == "" || scene.Status != "video_ready" {
		return nil, fmt.Errorf("场景视频尚未就绪")
	}
	inputDir := filepath.Join(s.cfg.Comfy.ComfyDir, "input", fmt.Sprint(projectID))
	videoPath := filepath.Join(inputDir, filepath.Base(scene.VideoInputFile))
	prefix := fmt.Sprintf("continuity_s%d_%s_", scene.ID, continuityTaskIDSanitizer.ReplaceAllString(scene.VideoTaskID, "_"))
	pattern := filepath.Join(inputDir, prefix+"%03d.png")
	window := float64(count) / 24.0
	if s.remote != nil && s.remote.Enabled() {
		cmd := fmt.Sprintf("%s; $FF -y -sseof -%.6f -i %s -vf fps=24 -frames:v %d %s", ffmpegResolveCmd, window, shellQuote(videoPath), count, shellQuote(pattern))
		if _, err := s.remote.RunTimeout(cmd, 2*time.Minute); err != nil {
			return nil, fmt.Errorf("提取衔接帧失败: %w", err)
		}
	} else {
		ffmpeg, err := localFFmpegPath()
		if err != nil {
			return nil, err
		}
		args := []string{"-y", "-sseof", fmt.Sprintf("-%.6f", window), "-i", videoPath, "-vf", "fps=24", "-frames:v", fmt.Sprint(count), pattern}
		if _, err := runLocalProgram(ffmpeg, args, 2*time.Minute); err != nil {
			return nil, fmt.Errorf("提取衔接帧失败: %w", err)
		}
	}
	candidates := make([]models.FrameCandidate, 0, count)
	uploads := make([]models.UploadFile, 0, count)
	for i := 1; i <= count; i++ {
		name := fmt.Sprintf("%s%03d.png", prefix, i)
		info, err := s.remote.Stat(filepath.Join(inputDir, name))
		if err != nil {
			continue
		}
		candidates = append(candidates, models.FrameCandidate{ProjectID: projectID, SceneID: sceneID, VideoTaskID: scene.VideoTaskID, Type: models.FrameCandidateCandidate, FrameIndex: i - 1, TimestampMS: int64(i-count) * 1000 / 24, ImageFile: name, Source: "extracted"})
		uploads = append(uploads, models.UploadFile{TaskID: fmt.Sprint(projectID), Type: "image", Name: name, Path: filepath.ToSlash(filepath.Join(fmt.Sprint(projectID), name)), Size: info.Size()})
	}
	if len(candidates) == 0 {
		return nil, fmt.Errorf("ffmpeg未产生候选帧")
	}
	err := s.db.Transaction(func(tx *gorm.DB) error {
		if err := detachAndDeleteFrameCandidates(tx, sceneID); err != nil {
			return err
		}
		if err := tx.Where("task_id = ? AND name LIKE ?", fmt.Sprint(projectID), prefix+"%").Delete(&models.UploadFile{}).Error; err != nil {
			return err
		}
		if err := tx.Create(&candidates).Error; err != nil {
			return err
		}
		return tx.Create(&uploads).Error
	})
	return candidates, err
}

func detachAndDeleteFrameCandidates(tx *gorm.DB, sceneID uint) error {
	var oldFrameIDs []uint
	if err := tx.Model(&models.FrameCandidate{}).Where("scene_id = ?", sceneID).Pluck("id", &oldFrameIDs).Error; err != nil {
		return err
	}
	if len(oldFrameIDs) > 0 {
		// SelectedFrameID is a real foreign key. Detach downstream continuity rows before
		// replacing extracted frames, otherwise SQLite correctly rejects the delete.
		if err := tx.Model(&models.SceneContinuity{}).Where("selected_frame_id IN ?", oldFrameIDs).Updates(map[string]any{
			"selected_frame_id": nil,
			"status":            "waiting",
			"error":             "来源视频末尾帧已重新提取，请重新选择衔接帧",
			"version":           gorm.Expr("version + 1"),
		}).Error; err != nil {
			return err
		}
	}
	return tx.Where("scene_id = ?", sceneID).Delete(&models.FrameCandidate{}).Error
}

func (s *ContinuityService) ListFrames(projectID, sceneID uint) ([]models.FrameCandidate, error) {
	var frames []models.FrameCandidate
	err := s.db.Where("project_id = ? AND scene_id = ?", projectID, sceneID).Order("frame_index").Find(&frames).Error
	return frames, err
}
func (s *ContinuityService) SelectFrame(projectID, sceneID, frameID uint) (*models.FrameCandidate, error) {
	var frame models.FrameCandidate
	if s.db.Where("id = ? AND project_id = ? AND scene_id = ?", frameID, projectID, sceneID).First(&frame).Error != nil {
		return nil, fmt.Errorf("候选帧不存在")
	}
	var scene models.Scene
	if err := s.db.Where("id = ? AND project_id = ?", sceneID, projectID).First(&scene).Error; err != nil {
		return nil, err
	}
	if frame.VideoTaskID != scene.VideoTaskID {
		return nil, fmt.Errorf("候选帧来自旧视频，请重新提取")
	}
	now := time.Now()
	err := s.db.Transaction(func(tx *gorm.DB) error {
		if err := tx.Model(&models.FrameCandidate{}).Where("scene_id = ?", sceneID).Updates(map[string]any{"type": models.FrameCandidateCandidate, "selected_at": nil}).Error; err != nil {
			return err
		}
		if err := tx.Model(&frame).Updates(map[string]any{"type": models.FrameCandidateSelected, "selected_at": now}).Error; err != nil {
			return err
		}
		// A different selected tail frame changes every dependent Scene's continuity input.
		var dependentIDs []uint
		if err := tx.Model(&models.SceneContinuity{}).Where("source_scene_id = ? AND source_video_task_id = ?", sceneID, frame.VideoTaskID).Pluck("scene_id", &dependentIDs).Error; err != nil {
			return err
		}
		if err := tx.Model(&models.SceneContinuity{}).
			Where("source_scene_id = ? AND source_video_task_id = ?", sceneID, frame.VideoTaskID).
			Updates(map[string]any{"selected_frame_id": frame.ID, "status": "ready", "error": "", "version": gorm.Expr("version + 1")}).Error; err != nil {
			return err
		}
		for _, dependentID := range dependentIDs {
			if err := invalidateSceneVideoTx(tx, projectID, dependentID, "连续性衔接帧已变化"); err != nil {
				return err
			}
		}
		return invalidateContinuityDependentsTx(tx, projectID, dependentIDs, "连续性衔接帧已变化")
	})
	if err != nil {
		return nil, err
	}
	frame.Type, frame.SelectedAt = models.FrameCandidateSelected, &now
	return &frame, nil
}

type ConfigureContinuityRequest struct {
	Mode          string `json:"mode"`
	SourceMode    string `json:"source_mode"`
	SourceSceneID uint   `json:"source_scene_id"`
	FrameID       uint   `json:"frame_id"`
}

func (s *ContinuityService) Configure(projectID, sceneID uint, req ConfigureContinuityRequest) (*models.SceneContinuity, error) {
	var scene models.Scene
	if s.db.Where("id = ? AND project_id = ?", sceneID, projectID).First(&scene).Error != nil {
		return nil, fmt.Errorf("场景不存在")
	}
	if req.Mode == "" {
		req.Mode = models.ContinuityModeIndependent
	}
	if req.Mode != models.ContinuityModeIndependent && req.Mode != models.ContinuityModeContinue && req.Mode != models.ContinuityModeBridge {
		return nil, fmt.Errorf("不支持的连续性模式")
	}
	cfg := models.SceneContinuity{SceneID: sceneID, Mode: req.Mode, SourceMode: req.SourceMode, Status: "not_required", Version: 1}
	var old models.SceneContinuity
	if result := s.db.Where("scene_id = ?", sceneID).Limit(1).Find(&old); result.Error != nil {
		return nil, result.Error
	} else if result.RowsAffected > 0 {
		cfg.ID, cfg.Version = old.ID, old.Version+1
	}
	if req.Mode != models.ContinuityModeIndependent {
		var source *models.Scene
		if req.SourceSceneID > 0 {
			var x models.Scene
			if s.db.Where("id = ? AND project_id = ?", req.SourceSceneID, projectID).First(&x).Error != nil {
				return nil, fmt.Errorf("来源场景不存在")
			}
			source = &x
		} else {
			source, _ = s.previousScene(&scene)
		}
		if source == nil {
			return nil, fmt.Errorf("当前是本集第一镜，不能使用上一镜续接或首尾桥接，请选择独立多参考生成")
		}
		if source.ID == scene.ID || source.EpisodeN != scene.EpisodeN || source.Generation != scene.Generation {
			return nil, fmt.Errorf("来源场景不属于当前分集或版本")
		}
		cfg.SourceSceneID, cfg.SourceVideoTaskID = &source.ID, source.VideoTaskID
		cfg.Status = "waiting"
		if req.FrameID > 0 {
			var frame models.FrameCandidate
			if s.db.Where("id = ? AND scene_id = ? AND video_task_id = ?", req.FrameID, source.ID, source.VideoTaskID).First(&frame).Error == nil {
				cfg.SelectedFrameID = &frame.ID
				cfg.Status = "ready"
			}
		} else {
			var frame models.FrameCandidate
			if s.db.Where("scene_id = ? AND video_task_id = ? AND type = ?", source.ID, source.VideoTaskID, models.FrameCandidateSelected).Order("frame_index DESC").First(&frame).Error == nil {
				cfg.SelectedFrameID = &frame.ID
				cfg.Status = "ready"
			}
		}
		if cfg.SourceMode == "" {
			cfg.SourceMode = "auto_previous"
		}
	}
	if err := s.db.Transaction(func(tx *gorm.DB) error {
		if err := tx.Save(&cfg).Error; err != nil {
			return err
		}
		if err := invalidateSceneVideoTx(tx, projectID, sceneID, "连续性输入已变化"); err != nil {
			return err
		}
		return invalidateContinuityDependentsTx(tx, projectID, []uint{sceneID}, "连续性输入已变化")
	}); err != nil {
		return nil, err
	}
	return s.Get(projectID, sceneID)
}
func (s *ContinuityService) Get(projectID, sceneID uint) (*models.SceneContinuity, error) {
	var cfg models.SceneContinuity
	err := s.db.Preload("SelectedFrame").Joins("JOIN scenes ON scenes.id = scene_continuities.scene_id").Where("scene_continuities.scene_id = ? AND scenes.project_id = ?", sceneID, projectID).First(&cfg).Error
	if err == gorm.ErrRecordNotFound {
		return nil, nil
	}
	return &cfg, err
}
func (s *ContinuityService) PrepareScene(scene *models.Scene) error {
	// 用户在视频提示词窗口明确指定首尾帧时，该选择优先于历史连续性配置。
	if scene.VideoTemplate == "minimax_h3_first_last" && strings.TrimSpace(scene.VideoFirstFrameImg) != "" && strings.TrimSpace(scene.VideoLastFrameImg) != "" {
		return nil
	}
	var cfg models.SceneContinuity
	if err := s.db.Preload("SelectedFrame").Where("scene_id = ?", scene.ID).First(&cfg).Error; err == gorm.ErrRecordNotFound {
		return nil
	} else if err != nil {
		return err
	}
	if cfg.Mode == models.ContinuityModeIndependent {
		return nil
	}
	if cfg.Status == "waiting" || cfg.SelectedFrame == nil {
		return fmt.Errorf("请先选择上一镜的末尾帧；该帧将作为本镜 0.00 秒的开始画面")
	}
	if cfg.Status != "ready" {
		return fmt.Errorf("连续性尚未就绪: %s", cfg.Status)
	}
	var source models.Scene
	if cfg.SourceSceneID == nil || s.db.First(&source, *cfg.SourceSceneID).Error != nil || source.VideoTaskID != cfg.SourceVideoTaskID || cfg.SelectedFrame.VideoTaskID != source.VideoTaskID {
		return fmt.Errorf("上一镜视频或衔接帧已失效，请重新选择")
	}
	if cfg.Mode == models.ContinuityModeBridge {
		scene.VideoTemplate, scene.VideoFirstFrameImg, scene.VideoLastFrameImg = "minimax_h3_first_last", cfg.SelectedFrame.ImageFile, scene.ImageFile
	} else {
		// Continue mode keeps Ref2VA references, but its selected tail frame replaces the current storyboard image.
		scene.VideoTemplate, scene.VideoFirstFrameImg, scene.VideoLastFrameImg = "minimax_h3_ref2v", "", ""
	}
	return nil
}
func (s *ContinuityService) ContinuationFrame(sceneID uint) (*models.FrameCandidate, string, error) {
	var cfg models.SceneContinuity
	if err := s.db.Preload("SelectedFrame").Where("scene_id = ?", sceneID).First(&cfg).Error; err != nil {
		return nil, "", err
	}
	if cfg.Mode != models.ContinuityModeContinue {
		return nil, cfg.Mode, nil
	}
	// If SelectedFrame is already loaded (not nil), use it
	if cfg.SelectedFrame != nil && cfg.Status == "ready" {
		return cfg.SelectedFrame, cfg.Mode, nil
	}
	// Status is waiting; look up latest selected frame from source scene
	if cfg.Status == "waiting" && cfg.SourceSceneID != nil {
		var frame models.FrameCandidate
		if s.db.Where("scene_id = ? AND video_task_id = ? AND type = ?", *cfg.SourceSceneID, cfg.SourceVideoTaskID, models.FrameCandidateSelected).Order("frame_index DESC").First(&frame).Error == nil {
			return &frame, cfg.Mode, nil
		}
	}
	return nil, cfg.Mode, nil
}
func (s *ContinuityService) InvalidateDependents(sourceSceneID uint, reason string) {
	_ = s.db.Transaction(func(tx *gorm.DB) error {
		var scene models.Scene
		if err := tx.Select("project_id").First(&scene, sourceSceneID).Error; err != nil {
			return err
		}
		return invalidateContinuityDependentsTx(tx, scene.ProjectID, []uint{sourceSceneID}, reason)
	})
}

// invalidateContinuityDependentsTx invalidates every continuity consumer reachable from
// sourceSceneIDs. The visited set makes malformed cyclic continuity graphs safe.
func invalidateContinuityDependentsTx(tx *gorm.DB, projectID uint, sourceSceneIDs []uint, reason string) error {
	seen := make(map[uint]struct{}, len(sourceSceneIDs))
	queue := append([]uint(nil), sourceSceneIDs...)
	for _, id := range sourceSceneIDs {
		seen[id] = struct{}{}
	}
	for len(queue) > 0 {
		sourceID := queue[0]
		queue = queue[1:]
		var configs []models.SceneContinuity
		if err := tx.Table("scene_continuities").
			Joins("JOIN scenes ON scenes.id = scene_continuities.scene_id").
			Where("scene_continuities.source_scene_id = ? AND scene_continuities.mode != ? AND scenes.project_id = ?", sourceID, models.ContinuityModeIndependent, projectID).
			Find(&configs).Error; err != nil {
			return err
		}
		for _, cfg := range configs {
			if _, ok := seen[cfg.SceneID]; ok {
				continue
			}
			seen[cfg.SceneID] = struct{}{}
			if err := tx.Model(&models.SceneContinuity{}).Where("id = ?", cfg.ID).Updates(map[string]any{
				"status": "source_invalidated", "error": reason, "selected_frame_id": nil, "version": gorm.Expr("version + 1"),
			}).Error; err != nil {
				return err
			}
			if err := invalidateSceneVideoTx(tx, projectID, cfg.SceneID, reason); err != nil {
				return err
			}
			if err := tx.Model(&models.Scene{}).Where("id = ? AND project_id = ?", cfg.SceneID, projectID).Update("prompt_stale", true).Error; err != nil {
				return err
			}
			queue = append(queue, cfg.SceneID)
		}
	}
	return nil
}
func (s *ContinuityService) invalidateSceneVideo(sceneID uint) {
	var scene models.Scene
	if s.db.Select("project_id").First(&scene, sceneID).Error == nil {
		_ = invalidateSceneVideoTx(s.db, scene.ProjectID, sceneID, "连续性输入已变化")
	}
}

func invalidateSceneVideoTx(db *gorm.DB, projectID, sceneID uint, reason string) error {
	if err := db.Model(&models.Scene{}).Where("id = ? AND project_id = ?", sceneID, projectID).Updates(map[string]any{
		"video_task_id": "", "video_file": "", "video_input_file": "", "video_gpu": nil,
		"video_full_prompt": "", "video_template": "", "video_first_frame_img": "", "video_last_frame_img": "",
		"video_candidate_parent_id": nil, "video_retries": 0,
		"status": gorm.Expr("CASE WHEN image_file != '' THEN 'image_ready' ELSE status END"),
	}).Error; err != nil {
		return err
	}
	return MarkSceneCandidatesStale(db, projectID, sceneID, "video", reason)
}
func (s *ContinuityService) ReplaceSelectedFrame(projectID, sceneID uint, filename string, data []byte) (*models.FrameCandidate, error) {
	if s.upload == nil {
		return nil, fmt.Errorf("上传服务未配置")
	}
	ext := strings.ToLower(filepath.Ext(filename))
	if ext != ".png" && ext != ".jpg" && ext != ".jpeg" && ext != ".webp" {
		return nil, fmt.Errorf("仅支持 PNG/JPG/WebP")
	}
	var frame models.FrameCandidate
	if err := s.db.Where("project_id = ? AND scene_id = ? AND type = ?", projectID, sceneID, models.FrameCandidateSelected).First(&frame).Error; err != nil {
		return nil, fmt.Errorf("请先选择一张衔接帧")
	}
	var scene models.Scene
	if err := s.db.Where("id = ? AND project_id = ?", sceneID, projectID).First(&scene).Error; err != nil {
		return nil, fmt.Errorf("场景不存在")
	}
	if frame.VideoTaskID != scene.VideoTaskID {
		return nil, fmt.Errorf("已选帧来自旧视频，请重新提取")
	}
	name := fmt.Sprintf("continuity_s%d_manual_%d%s", sceneID, time.Now().UnixNano(), ext)
	path, _, err := s.upload.SaveFile(fmt.Sprint(projectID), "image", name, data)
	if err != nil {
		return nil, err
	}
	original := frame.OriginalImageFile
	if original == "" {
		original = frame.ImageFile
	}
	if err := s.db.Model(&frame).Updates(map[string]any{"image_file": filepath.Base(path), "original_image_file": original, "source": "manual_upload"}).Error; err != nil {
		return nil, err
	}
	frame.ImageFile, frame.OriginalImageFile, frame.Source = filepath.Base(path), original, "manual_upload"
	s.InvalidateDependents(sceneID, "来源衔接帧已被高清图替换，请重新确认连续性配置")
	return &frame, nil
}

func (s *ContinuityService) FrameURL(frame models.FrameCandidate) string {
	return fmt.Sprintf("/api/input/%d/%s", frame.ProjectID, filepath.ToSlash(frame.ImageFile))
}
