package service

import (
	"fmt"
	"path/filepath"
	"strings"

	"comfyui-console/internal/models"
)

// Frame images and saved selections must use the same storage root served by
// /api/input. Remote-only execution retains its existing remote staging directory.
func (s *ContinuityService) sceneFrameDirectory(projectID uint) string {
	root := s.cfg.AssetsRoot()
	if s.upload != nil {
		root = s.upload.InputDir()
	}
	if s.remote != nil && s.remote.Enabled() {
		root = filepath.Join(s.cfg.Comfy.ComfyDir, "input")
	}
	return filepath.Join(root, fmt.Sprint(projectID))
}

func (s *ContinuityService) sceneFrameVideoPath(scene *models.Scene) (string, error) {
	if s.remote == nil {
		return "", fmt.Errorf("文件服务未配置")
	}
	var candidates []string
	if name := strings.TrimSpace(scene.VideoInputFile); name != "" {
		clean := filepath.Clean(filepath.FromSlash(name))
		if clean == "." || filepath.IsAbs(clean) || clean == ".." || strings.HasPrefix(clean, ".."+string(filepath.Separator)) {
			return "", fmt.Errorf("分镜视频素材路径无效")
		}
		candidates = append(candidates, filepath.Join(s.sceneFrameDirectory(scene.ProjectID), clean))
		// Old projects may not yet have been migrated. Only the recorded filename is
		// considered; never pick a different video merely because it exists nearby.
		legacy := filepath.Join(s.cfg.Comfy.ComfyDir, "input", fmt.Sprint(scene.ProjectID), clean)
		if legacy != candidates[0] {
			candidates = append(candidates, legacy)
		}
	}
	if scene.VideoGPU != nil && strings.TrimSpace(scene.VideoFile) != "" {
		clean := filepath.Clean(filepath.FromSlash(scene.VideoFile))
		if filepath.IsAbs(clean) || clean == ".." || strings.HasPrefix(clean, ".."+string(filepath.Separator)) {
			return "", fmt.Errorf("分镜视频输出路径无效")
		}
		candidates = append(candidates, filepath.Join(s.cfg.Comfy.ComfyDir, "output_workers", fmt.Sprintf("gpu%d", *scene.VideoGPU), clean))
	}
	for _, candidate := range candidates {
		if info, err := s.remote.Stat(candidate); err == nil && !info.IsDir() && info.Size() > 0 {
			return candidate, nil
		}
	}
	return "", fmt.Errorf("当前分镜的视频文件不存在：已检查项目素材目录及旧版视频位置，请检查素材是否已迁移或恢复")
}
