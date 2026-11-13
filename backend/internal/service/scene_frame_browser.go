package service

import (
	"fmt"
	"io"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"time"

	"comfyui-console/internal/models"
)

const maxSceneFrameBrowserFrames = 5000

var sceneFrameExtractionIDPattern = regexp.MustCompile(`^scene_frames_s([0-9]+)_[A-Za-z0-9_-]+_[0-9]+$`)

type SceneFrameBrowserFrame struct {
	Index       int    `json:"index"`
	TimestampMS int64  `json:"timestamp_ms"`
	File        string `json:"file"`
	URL         string `json:"url"`
}

type SceneFrameBrowserResult struct {
	ExtractionID string                   `json:"extraction_id"`
	FrameRate    int                      `json:"frame_rate"`
	Frames       []SceneFrameBrowserFrame `json:"frames"`
	// CacheDir 是这批浏览帧的真实磁盘目录，回给前端展示：用户要能找到并自行删除。
	CacheDir string `json:"cache_dir"`
	// PurgedFrames 是本次自动清理掉的上一批浏览帧数量。
	PurgedFrames int `json:"purged_frames"`
}

type SavedSceneFrame struct {
	Index       int    `json:"index"`
	TimestampMS int64  `json:"timestamp_ms"`
	File        string `json:"file"`
	URL         string `json:"url"`
}

// ExtractAllSceneFrames extracts the current Scene video frame-for-frame for review.
// The source video is not modified. Extracted review images remain project-scoped.
func (s *ContinuityService) ExtractAllSceneFrames(projectID, sceneID uint) (*SceneFrameBrowserResult, error) {
	var scene models.Scene
	if err := s.db.Where("id=? AND project_id=?", sceneID, projectID).First(&scene).Error; err != nil {
		return nil, fmt.Errorf("分镜不存在")
	}
	if s.remote == nil {
		return nil, fmt.Errorf("文件服务未配置")
	}
	inputDir := s.sceneFrameCacheDirectory(projectID)
	videoPath, err := s.sceneFrameVideoPath(&scene)
	if err != nil {
		return nil, err
	}
	if err := s.remote.MkdirAll(inputDir, 0o755); err != nil {
		return nil, fmt.Errorf("创建分镜帧缓存目录失败: %w", err)
	}
	taskPart := continuityTaskIDSanitizer.ReplaceAllString(scene.VideoTaskID, "_")
	if taskPart == "" {
		taskPart = "current"
	}
	extractionID := fmt.Sprintf("scene_frames_s%d_%s_%d", scene.ID, taskPart, time.Now().UnixNano())
	pattern := filepath.Join(inputDir, extractionID+"_%06d.png")

	if s.remote.Enabled() {
		cmd := fmt.Sprintf("%s; $FF -y -i %s -map 0:v:0 -fps_mode passthrough -frames:v %d %s", ffmpegResolveCmd, shellQuote(videoPath), maxSceneFrameBrowserFrames, shellQuote(pattern))
		if _, err := s.remote.RunTimeout(cmd, 5*time.Minute); err != nil {
			return nil, fmt.Errorf("提取分镜全部帧失败: %w", err)
		}
	} else {
		ffmpeg, err := localFFmpegPath()
		if err != nil {
			return nil, err
		}
		args := []string{"-y", "-i", videoPath, "-map", "0:v:0", "-fps_mode", "passthrough", "-frames:v", strconv.Itoa(maxSceneFrameBrowserFrames), pattern}
		if output, err := runLocalProgram(ffmpeg, args, 5*time.Minute); err != nil {
			return nil, fmt.Errorf("提取分镜全部帧失败: %w: %s", err, output)
		}
	}

	// 上一次浏览留下的帧此刻已无人引用，再保留只会占满磁盘。只删本分镜、本次之外的
	// 浏览帧：保存过的选帧早已复制进素材目录，不受影响。
	purged := s.purgeSceneFrameCache(projectID, scene.ID, extractionID)

	entries, err := s.remote.ListDir(inputDir)
	if err != nil {
		return nil, fmt.Errorf("读取分镜帧失败: %w", err)
	}
	prefix := extractionID + "_"
	names := make([]string, 0)
	for _, entry := range entries {
		name := entry.Name()
		if !entry.IsDir() && strings.HasPrefix(name, prefix) && strings.EqualFold(filepath.Ext(name), ".png") {
			names = append(names, name)
		}
	}
	sort.Strings(names)
	if len(names) == 0 {
		return nil, fmt.Errorf("FFmpeg 未产生任何分镜帧")
	}
	frames := make([]SceneFrameBrowserFrame, 0, len(names))
	for index, name := range names {
		frames = append(frames, SceneFrameBrowserFrame{
			Index:       index,
			TimestampMS: int64(index) * 1000 / 24,
			File:        name,
			URL:         sceneFrameCacheURL(projectID, name),
		})
	}
	return &SceneFrameBrowserResult{ExtractionID: extractionID, FrameRate: 24, Frames: frames, CacheDir: inputDir, PurgedFrames: purged}, nil
}

// sceneFrameCacheURL 指向缓存目录；/api/input/:taskid/*path 本就支持多级路径。
func sceneFrameCacheURL(projectID uint, name string) string {
	return fmt.Sprintf("/api/input/%s/%d/%s", sceneFrameCacheCategory, projectID, name)
}

// purgeSceneFrameCache 删除该分镜此前提取的浏览帧，返回删除数量。这些文件没有数据库
// 记录、也不被素材引用，重复浏览若不清理会在磁盘上无限累积。
func (s *ContinuityService) purgeSceneFrameCache(projectID, sceneID uint, keepExtractionID string) int {
	if s.remote == nil {
		return 0
	}
	dir := s.sceneFrameCacheDirectory(projectID)
	entries, err := s.remote.ListDir(dir)
	if err != nil {
		return 0
	}
	prefix := fmt.Sprintf("scene_frames_s%d_", sceneID)
	keep := ""
	if strings.TrimSpace(keepExtractionID) != "" {
		keep = keepExtractionID + "_"
	}
	removed := 0
	for _, entry := range entries {
		name := entry.Name()
		if entry.IsDir() || !strings.HasPrefix(name, prefix) || !strings.EqualFold(filepath.Ext(name), ".png") {
			continue
		}
		if keep != "" && strings.HasPrefix(name, keep) {
			continue
		}
		if err := s.remote.Remove(filepath.Join(dir, name)); err == nil {
			removed++
		}
	}
	return removed
}

// SaveExtractedSceneFrame copies one reviewed frame to a stable project input file.
func (s *ContinuityService) SaveExtractedSceneFrame(projectID, sceneID uint, extractionID string, frameIndex int) (*SavedSceneFrame, error) {
	matches := sceneFrameExtractionIDPattern.FindStringSubmatch(strings.TrimSpace(extractionID))
	if len(matches) != 2 || matches[1] != strconv.FormatUint(uint64(sceneID), 10) {
		return nil, fmt.Errorf("帧提取记录与当前分镜不匹配")
	}
	if frameIndex < 0 || frameIndex >= maxSceneFrameBrowserFrames {
		return nil, fmt.Errorf("帧序号无效")
	}
	var scene models.Scene
	if err := s.db.Where("id=? AND project_id=?", sceneID, projectID).First(&scene).Error; err != nil {
		return nil, fmt.Errorf("分镜不存在")
	}
	if s.remote == nil || s.upload == nil {
		return nil, fmt.Errorf("文件服务未配置")
	}
	name := fmt.Sprintf("%s_%06d.png", extractionID, frameIndex+1)
	source := filepath.Join(s.sceneFrameCacheDirectory(projectID), name)
	reader, err := s.remote.Open(source)
	if err != nil {
		// 升级前提取的帧仍留在项目素材目录里，回退读取一次，避免跨版本浏览中断。
		if legacy, legacyErr := s.remote.Open(filepath.Join(s.sceneFrameDirectory(projectID), name)); legacyErr == nil {
			reader = legacy
		} else {
			return nil, fmt.Errorf("所选帧不存在或已失效")
		}
	}
	defer reader.Close()
	data, err := io.ReadAll(io.LimitReader(reader, 30<<20))
	if err != nil {
		return nil, fmt.Errorf("读取所选帧失败: %w", err)
	}
	if len(data) == 0 {
		return nil, fmt.Errorf("所选帧为空")
	}
	savedName := fmt.Sprintf("scene_%d_selected_frame_%06d_%d.png", sceneID, frameIndex+1, time.Now().UnixNano())
	path, _, err := s.upload.SaveFile(fmt.Sprint(projectID), "image", savedName, data)
	if err != nil {
		return nil, err
	}
	file := filepath.Base(path)
	return &SavedSceneFrame{
		Index:       frameIndex,
		TimestampMS: int64(frameIndex) * 1000 / 24,
		File:        file,
		URL:         fmt.Sprintf("/api/input/%d/%s", projectID, file),
	}, nil
}
