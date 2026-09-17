package service

import (
	"fmt"
	"path/filepath"
	"regexp"
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
}

func NewContinuityService(cfg *config.Config, db *gorm.DB, remote *RemoteExec) *ContinuityService {
	return &ContinuityService{cfg: cfg, db: db, remote: remote}
}

func (s *ContinuityService) previousScene(scene *models.Scene) (*models.Scene, error) {
	var previous models.Scene
	err := s.db.Where("project_id = ? AND episode_n = ? AND generation = ? AND `order` < ?", scene.ProjectID, scene.EpisodeN, scene.Generation, scene.Order).Order("`order` DESC").First(&previous).Error
	if err == gorm.ErrRecordNotFound {
		return nil, nil
	}
	return &previous, err
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
	for i := 1; i <= count; i++ {
		name := fmt.Sprintf("%s%03d.png", prefix, i)
		if _, err := s.remote.Stat(filepath.Join(inputDir, name)); err != nil {
			continue
		}
		candidates = append(candidates, models.FrameCandidate{ProjectID: projectID, SceneID: sceneID, VideoTaskID: scene.VideoTaskID, Type: models.FrameCandidateCandidate, FrameIndex: i - 1, TimestampMS: int64(i-count) * 1000 / 24, ImageFile: name})
	}
	if len(candidates) == 0 {
		return nil, fmt.Errorf("ffmpeg未产生候选帧")
	}
	err := s.db.Transaction(func(tx *gorm.DB) error {
		if err := tx.Where("scene_id = ?", sceneID).Delete(&models.FrameCandidate{}).Error; err != nil {
			return err
		}
		return tx.Create(&candidates).Error
	})
	return candidates, err
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
		return tx.Model(&frame).Updates(map[string]any{"type": models.FrameCandidateSelected, "selected_at": now}).Error
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
	if s.db.Where("scene_id = ?", sceneID).First(&old).Error == nil {
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
		if source == nil || source.ID == scene.ID || source.EpisodeN != scene.EpisodeN || source.Generation != scene.Generation {
			return nil, fmt.Errorf("没有可用的上一场景")
		}
		var frame models.FrameCandidate
		q := s.db.Where("scene_id = ? AND video_task_id = ?", source.ID, source.VideoTaskID)
		if req.FrameID > 0 {
			q = q.Where("id = ?", req.FrameID)
		} else {
			q = q.Where("type = ?", models.FrameCandidateSelected).Order("frame_index DESC")
		}
		if q.First(&frame).Error != nil {
			return nil, fmt.Errorf("上一场景尚未选择有效衔接帧")
		}
		cfg.SourceSceneID, cfg.SelectedFrameID, cfg.SourceVideoTaskID, cfg.Status = &source.ID, &frame.ID, source.VideoTaskID, "ready"
		if cfg.SourceMode == "" {
			cfg.SourceMode = "auto_previous"
		}
	}
	if err := s.db.Save(&cfg).Error; err != nil {
		return nil, err
	}
	s.invalidateSceneVideo(sceneID)
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
	var cfg models.SceneContinuity
	if err := s.db.Preload("SelectedFrame").Where("scene_id = ?", scene.ID).First(&cfg).Error; err == gorm.ErrRecordNotFound {
		return nil
	} else if err != nil {
		return err
	}
	if cfg.Mode == models.ContinuityModeIndependent {
		return nil
	}
	if cfg.Status != "ready" || cfg.SelectedFrame == nil {
		return fmt.Errorf("连续性尚未就绪: %s", cfg.Status)
	}
	var source models.Scene
	if cfg.SourceSceneID == nil || s.db.First(&source, *cfg.SourceSceneID).Error != nil || source.VideoTaskID != cfg.SourceVideoTaskID || cfg.SelectedFrame.VideoTaskID != source.VideoTaskID {
		return fmt.Errorf("上一镜视频或衔接帧已失效，请重新选择")
	}
	if cfg.Mode == models.ContinuityModeBridge {
		scene.VideoTemplate, scene.VideoFirstFrameImg, scene.VideoLastFrameImg = "minimax_h3_first_last", cfg.SelectedFrame.ImageFile, scene.ImageFile
	}
	return nil
}
func (s *ContinuityService) ContinuationFrame(sceneID uint) (*models.FrameCandidate, string, error) {
	var cfg models.SceneContinuity
	if err := s.db.Preload("SelectedFrame").Where("scene_id = ?", sceneID).First(&cfg).Error; err != nil {
		return nil, "", err
	}
	if cfg.Mode != models.ContinuityModeContinue || cfg.SelectedFrame == nil || cfg.Status != "ready" {
		return nil, cfg.Mode, nil
	}
	return cfg.SelectedFrame, cfg.Mode, nil
}
func (s *ContinuityService) InvalidateDependents(sourceSceneID uint, reason string) {
	s.db.Model(&models.SceneContinuity{}).Where("source_scene_id = ? AND mode != ?", sourceSceneID, models.ContinuityModeIndependent).Updates(map[string]any{"status": "source_invalidated", "error": reason, "version": gorm.Expr("version + 1")})
}
func (s *ContinuityService) invalidateSceneVideo(sceneID uint) {
	s.db.Model(&models.Scene{}).Where("id = ?", sceneID).Updates(map[string]any{"video_task_id": "", "video_file": "", "video_input_file": "", "video_gpu": nil, "status": gorm.Expr("CASE WHEN image_file != '' THEN 'image_ready' ELSE status END")})
}
func (s *ContinuityService) FrameURL(frame models.FrameCandidate) string {
	return fmt.Sprintf("/api/input/%d/%s", frame.ProjectID, filepath.ToSlash(frame.ImageFile))
}
