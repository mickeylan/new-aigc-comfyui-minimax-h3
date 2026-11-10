package service

import (
	"crypto/sha1"
	"encoding/hex"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/gin-gonic/gin"
	"gorm.io/gorm"

	"comfyui-console/internal/models"
)

// sceneVideoSource resolves the authoritative, unmodified Scene video. Proxy
// generation must never change Scene.VideoFile/VideoInputFile or Scene state.
func (s *ProjectService) sceneVideoSource(p *models.Project, scene *models.Scene) (string, error) {
	if (s.remote == nil || !s.remote.Enabled()) && scene.VideoInputFile != "" {
		clean := filepath.Clean(filepath.FromSlash(scene.VideoInputFile))
		if filepath.IsAbs(clean) || clean == ".." || strings.HasPrefix(clean, ".."+string(filepath.Separator)) {
			return "", fmt.Errorf("Scene %d 视频路径无效", scene.ID)
		}
		return filepath.Join(s.upload.InputDir(), fmt.Sprint(scene.ProjectID), clean), nil
	}
	if scene.VideoGPU == nil || strings.TrimSpace(scene.VideoFile) == "" {
		return "", fmt.Errorf("Scene %d 还没有可用的视频", scene.ID)
	}
	return s.mediaPath("output_workers", fmt.Sprintf("gpu%d", *scene.VideoGPU), filepath.FromSlash(scene.VideoFile)), nil
}

func (s *ProjectService) proxyRoot() string {
	return filepath.Join(s.cfg.Storage.DataDir, "proxies")
}

// proxyPath keys the cache on the resolved source identity, not on a mutable
// timeline position. Replacing a Scene video therefore yields a new proxy.
func (s *ProjectService) proxyPath(projectID uint, sceneID uint, source string) (string, error) {
	info, err := os.Stat(source)
	if err != nil {
		return "", fmt.Errorf("源视频不可读，无法生成代理: %w", err)
	}
	key := fmt.Sprintf("%s|%d|%d", source, info.Size(), info.ModTime().UnixNano())
	sum := sha1.Sum([]byte(key))
	name := fmt.Sprintf("p%d_s%d_%s.mp4", projectID, sceneID, hex.EncodeToString(sum[:])[:12])
	return filepath.Join(s.proxyRoot(), name), nil
}

func (s *Service) proxyUnsupported() bool {
	return s.Remote != nil && s.Remote.Enabled()
}

// HandleSceneMediaInfo returns authoritative metadata for frame stepping and
// preview caching. It never reads or rewrites Scene content.
func (s *Service) HandleSceneMediaInfo(c *gin.Context) {
	p, ok := s.loadProject(c)
	if !ok {
		return
	}
	sc, ok := s.loadScene(c)
	if !ok {
		return
	}
	result := gin.H{"scene_id": sc.ID, "has_video": false, "proxy_ready": false}
	if s.Remote == nil {
		c.JSON(http.StatusServiceUnavailable, gin.H{"error": "媒体探测服务不可用"})
		return
	}
	source, err := s.Projects.sceneVideoSource(p, sc)
	if err != nil {
		c.JSON(http.StatusOK, result)
		return
	}
	info, err := s.Remote.ProbeMedia(source)
	if err != nil {
		c.JSON(http.StatusOK, gin.H{"scene_id": sc.ID, "has_video": true, "error": "源视频暂时不可探测", "proxy_ready": false})
		return
	}
	proxyURL, proxyReady := "", false
	if s.Projects.proxyRoot() != "" {
		if path, pathErr := s.Projects.proxyPath(p.ID, sc.ID, source); pathErr == nil {
			if fi, statErr := os.Stat(path); statErr == nil && fi.Mode().IsRegular() {
				proxyReady = true
				proxyURL = fmt.Sprintf("/api/projects/%d/scenes/%d/proxy", p.ID, sc.ID)
			}
		}
	}
	c.JSON(http.StatusOK, gin.H{
		"scene_id": sc.ID, "has_video": true, "duration": info.Duration,
		"width": info.Width, "height": info.Height, "fps": info.FPS,
		"proxy_ready": proxyReady, "proxy_url": proxyURL, "proxy_supported": !s.proxyUnsupported(),
	})
}

// HandleCreateSceneProxy creates a cached 480p H.264 preview proxy. The source
// file and Scene remain untouched; failures leave no partial cache entry.
func (s *Service) HandleCreateSceneProxy(c *gin.Context) {
	p, ok := s.loadProject(c)
	if !ok {
		return
	}
	sc, ok := s.loadScene(c)
	if !ok {
		return
	}
	if s.proxyUnsupported() {
		c.JSON(http.StatusUnprocessableEntity, gin.H{"error": "SSH远程模式暂不支持代理生成；请在本机可读共享目录的模式下使用"})
		return
	}
	source, err := s.Projects.sceneVideoSource(p, sc)
	if err != nil {
		c.JSON(http.StatusUnprocessableEntity, gin.H{"error": err.Error()})
		return
	}
	ffmpeg, err := localFFmpegPath()
	if err != nil {
		c.JSON(http.StatusServiceUnavailable, gin.H{"error": err.Error()})
		return
	}
	target, err := s.Projects.proxyPath(p.ID, sc.ID, source)
	if err != nil {
		c.JSON(http.StatusUnprocessableEntity, gin.H{"error": err.Error()})
		return
	}
	if fi, statErr := os.Stat(target); statErr == nil && fi.Mode().IsRegular() && fi.Size() > 0 {
		c.JSON(http.StatusOK, gin.H{"cached": true, "url": fmt.Sprintf("/api/projects/%d/scenes/%d/proxy", p.ID, sc.ID)})
		return
	}
	if err := os.MkdirAll(s.Projects.proxyRoot(), 0o755); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "代理目录不可写"})
		return
	}
	partial := target + ".partial.mp4"
	defer os.Remove(partial)
	args := []string{"-y", "-v", "error", "-i", source, "-vf", "scale=-2:'min(480,ih)',scale=trunc(iw/2)*2:trunc(ih/2)*2", "-c:v", "libx264", "-preset", "veryfast", "-crf", "28", "-maxrate", "2500k", "-bufsize", "5000k", "-pix_fmt", "yuv420p", "-c:a", "aac", "-b:a", "96k", "-ac", "2", "-movflags", "+faststart", partial}
	if output, runErr := runLocalProgram(ffmpeg, args, 30*time.Minute); runErr != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "代理生成失败: " + tailMessage(output, runErr.Error())})
		return
	}
	if fi, statErr := os.Stat(partial); statErr != nil || fi.Size() == 0 {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "代理生成未产生有效文件"})
		return
	}
	if err := os.Rename(partial, target); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "代理缓存写入失败"})
		return
	}
	c.JSON(http.StatusOK, gin.H{"cached": false, "url": fmt.Sprintf("/api/projects/%d/scenes/%d/proxy", p.ID, sc.ID)})
}

func tailMessage(output, fallback string) string {
	output = strings.TrimSpace(output)
	if output == "" {
		output = fallback
	}
	runes := []rune(output)
	if len(runes) > 600 {
		output = string(runes[len(runes)-600:])
	}
	return output
}

func (s *Service) HandleGetSceneProxy(c *gin.Context) {
	p, ok := s.loadProject(c)
	if !ok {
		return
	}
	sc, ok := s.loadScene(c)
	if !ok {
		return
	}
	if s.proxyUnsupported() {
		c.JSON(http.StatusUnprocessableEntity, gin.H{"error": "远程模式暂无代理媒体"})
		return
	}
	source, err := s.Projects.sceneVideoSource(p, sc)
	if err != nil {
		c.JSON(http.StatusNotFound, gin.H{"error": err.Error()})
		return
	}
	target, err := s.Projects.proxyPath(p.ID, sc.ID, source)
	if err != nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "代理不存在"})
		return
	}
	fi, err := os.Stat(target)
	if err != nil || !fi.Mode().IsRegular() || fi.Size() == 0 {
		c.JSON(http.StatusNotFound, gin.H{"error": "代理尚未生成"})
		return
	}
	c.Header("Cache-Control", "private, max-age=3600")
	c.Header("ETag", fmt.Sprintf(`"%x-%x"`, fi.Size(), fi.ModTime().UnixNano()))
	c.Header("Content-Type", "video/mp4")
	c.File(target)
}

// preflightMerge validates the exact export payload without launching FFmpeg.
// It persists a pending task only inside a rollback transaction so no task,
// output history or Scene state can change.
func (s *Service) HandlePreflightAudioMerge(c *gin.Context) {
	p, ok := s.loadProject(c)
	if !ok {
		return
	}
	var req struct {
		SceneIDs     []uint            `json:"scene_ids"`
		Dub          bool              `json:"dub"`
		Subtitles    bool              `json:"subtitles"`
		ScreenText   bool              `json:"screen_text"`
		EditSequence []mergeEditClip   `json:"edit_sequence"`
		VisualTracks []clipVisualTrack `json:"visual_tracks"`
		EpisodeN     int               `json:"episode_n"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "参数错误"})
		return
	}
	options, err := mergeAudioOptions(nil, nil, nil)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	options.ScreenText = req.ScreenText
	sceneIDs := req.SceneIDs
	if len(req.EditSequence) > 0 {
		sceneIDs, err = validateMergeEditSequence(req.EditSequence)
		if err != nil {
			c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
			return
		}
	}
	var result struct {
		Duration float64  `json:"duration"`
		Clips    int      `json:"clips"`
		Warnings []string `json:"warnings"`
	}
	err = s.DB.Transaction(func(tx *gorm.DB) error {
		// Reuse the production creation path so snapshot, ownership, duration
		// and stale-input checks are identical to a real export.
		svcCopy := *s.Projects
		svcCopy.db = tx
		mt, scenes, err := svcCopy.createMergeTaskRecord(p, sceneIDs, req.Dub, req.Subtitles, options, mergeTaskSequence{Clips: req.EditSequence, EpisodeN: req.EpisodeN, VisualTracks: req.VisualTracks})
		if err != nil {
			return err
		}
		svcCopy.remote = s.Remote
		svcCopy.upload = s.Upload
		svcCopy.materials = s.Materials
		if err := svcCopy.validateMergeExecution(p, mt, scenes); err != nil {
			return err
		}
		resolved, err := svcCopy.resolveMergeVideos(p, mt, scenes)
		if err != nil {
			return err
		}
		for i := range resolved {
			info, probeErr := svcCopy.remote.ProbeMedia(resolved[i].Path)
			if probeErr != nil || info.Duration <= 0 {
				return fmt.Errorf("编排片段%d源视频不可用", i+1)
			}
			resolved[i].SourceDuration = info.Duration
		}
		total := 0.0
		for i, v := range resolved {
			d, err := mergeClipOutputDuration(v)
			if err != nil {
				return fmt.Errorf("编排片段%d: %w", i+1, err)
			}
			total += d
			if i+1 < len(resolved) {
				total -= mergeClipTransitionOverlap(v)
			}
		}
		result.Duration = total
		result.Clips = len(resolved)
		result.Warnings = strings.Split(strings.TrimSpace(mt.PreflightWarnings), "\n")
		if len(result.Warnings) == 1 && result.Warnings[0] == "" {
			result.Warnings = nil
		}
		return errPreflightRollback
	})
	if err != nil && err != errPreflightRollback {
		c.JSON(http.StatusUnprocessableEntity, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, result)
}

var errPreflightRollback = fmt.Errorf("preflight rollback")
