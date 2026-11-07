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
	if strings.TrimSpace(scene.VideoInputFile) == "" || scene.Status != "video_ready" {
		return nil, fmt.Errorf("当前分镜视频尚未完成或没有本地视频副本")
	}
	if s.remote == nil {
		return nil, fmt.Errorf("文件服务未配置")
	}
	inputDir := filepath.Join(s.cfg.Comfy.ComfyDir, "input", fmt.Sprint(projectID))
	videoPath := filepath.Join(inputDir, filepath.Base(scene.VideoInputFile))
	taskPart := continuityTaskIDSanitizer.ReplaceAllString(scene.VideoTaskID, "_")
	if taskPart == "" {
		taskPart = "current"
	}
	extractionID := fmt.Sprintf("scene_frames_s%d_%s_%d", scene.ID, taskPart, time.Now().UnixNano())
	pattern := filepath.Join(inputDir, extractionID+"_%06d.jpg")

	if s.remote.Enabled() {
		cmd := fmt.Sprintf("%s; $FF -y -i %s -map 0:v:0 -fps_mode passthrough -q:v 3 -frames:v %d %s", ffmpegResolveCmd, shellQuote(videoPath), maxSceneFrameBrowserFrames, shellQuote(pattern))
		if _, err := s.remote.RunTimeout(cmd, 5*time.Minute); err != nil {
			return nil, fmt.Errorf("提取分镜全部帧失败: %w", err)
		}
	} else {
		ffmpeg, err := localFFmpegPath()
		if err != nil {
			return nil, err
		}
		args := []string{"-y", "-i", videoPath, "-map", "0:v:0", "-fps_mode", "passthrough", "-q:v", "3", "-frames:v", strconv.Itoa(maxSceneFrameBrowserFrames), pattern}
		if output, err := runLocalProgram(ffmpeg, args, 5*time.Minute); err != nil {
			return nil, fmt.Errorf("提取分镜全部帧失败: %w: %s", err, output)
		}
	}

	entries, err := s.remote.ListDir(inputDir)
	if err != nil {
		return nil, fmt.Errorf("读取分镜帧失败: %w", err)
	}
	prefix := extractionID + "_"
	names := make([]string, 0)
	for _, entry := range entries {
		name := entry.Name()
		if !entry.IsDir() && strings.HasPrefix(name, prefix) && strings.EqualFold(filepath.Ext(name), ".jpg") {
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
			URL:         fmt.Sprintf("/api/input/%d/%s", projectID, name),
		})
	}
	return &SceneFrameBrowserResult{ExtractionID: extractionID, FrameRate: 24, Frames: frames}, nil
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
	name := fmt.Sprintf("%s_%06d.jpg", extractionID, frameIndex+1)
	source := filepath.Join(s.cfg.Comfy.ComfyDir, "input", fmt.Sprint(projectID), name)
	reader, err := s.remote.Open(source)
	if err != nil {
		return nil, fmt.Errorf("所选帧不存在或已失效")
	}
	defer reader.Close()
	data, err := io.ReadAll(io.LimitReader(reader, 30<<20))
	if err != nil {
		return nil, fmt.Errorf("读取所选帧失败: %w", err)
	}
	if len(data) == 0 {
		return nil, fmt.Errorf("所选帧为空")
	}
	savedName := fmt.Sprintf("scene_%d_selected_frame_%06d_%d.jpg", sceneID, frameIndex+1, time.Now().UnixNano())
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
