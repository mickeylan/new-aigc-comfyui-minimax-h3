package service

import (
	"fmt"
	"path/filepath"
	"strings"

	"comfyui-console/internal/models"
)

// sceneFrameCacheCategory 是「查看全部帧」的专用缓存目录名。那些 PNG 只用于人工挑帧：
// 不写数据库、不被任何素材记录引用，一次浏览就可能产生上千张。放在固定的独立目录里，
// 用户可以整目录删除而不必担心误删素材，项目素材目录也保持干净。
const sceneFrameCacheCategory = "_frame_cache"

// Frame images and saved selections must use the same storage root served by
// /api/input. Remote-only execution retains its existing remote staging directory.
func (s *ContinuityService) sceneFrameRoot() string {
	root := s.cfg.AssetsRoot()
	if s.upload != nil {
		root = s.upload.InputDir()
	}
	if s.remote != nil && s.remote.Enabled() {
		root = filepath.Join(s.cfg.Comfy.ComfyDir, "input")
	}
	return root
}

func (s *ContinuityService) sceneFrameDirectory(projectID uint) string {
	return filepath.Join(s.sceneFrameRoot(), fmt.Sprint(projectID))
}

// sceneFrameCacheDirectory 与项目素材目录同根但互不混杂，仍由 /api/input/*path 提供预览。
func (s *ContinuityService) sceneFrameCacheDirectory(projectID uint) string {
	return filepath.Join(s.sceneFrameRoot(), sceneFrameCacheCategory, fmt.Sprint(projectID))
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
