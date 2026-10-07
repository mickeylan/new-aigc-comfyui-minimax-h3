package service

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"time"

	"comfyui-console/internal/models"
	"gorm.io/gorm"
)

func screenTextPreviewFFmpegArgs(width, height int, assPath, fontsDir, outputPath string) []string {
	filter := fmt.Sprintf("subtitles=filename=%s:fontsdir=%s:alpha=1", escapeFilterPath(assPath), escapeFilterPath(fontsDir))
	return []string{
		"-y", "-f", "lavfi", "-i", fmt.Sprintf("color=c=black@0.0:s=%dx%d:d=1,format=rgba", width, height),
		"-vf", filter, "-frames:v", "1", "-c:v", "png", outputPath,
	}
}

func (s *ScreenTextService) RenderPreviewPNG(projectID, cueID uint, width, height int, fonts *FontRegistry) ([]byte, error) {
	if width <= 0 {
		width = 1920
	}
	if height <= 0 {
		height = 1080
	}
	if width < 320 || height < 180 || width > 4096 || height > 4096 {
		return nil, fmt.Errorf("预览尺寸必须位于 320×180 到 4096×4096 范围内")
	}
	var cue models.ScreenTextCue
	if err := s.db.Where("id = ? AND project_id = ?", cueID, projectID).First(&cue).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, ErrScreenTextCueNotFound
		}
		return nil, err
	}
	font, err := fonts.Approved("noto-sans-sc")
	if err != nil {
		return nil, fmt.Errorf("功能文字预览被阻止: %w", err)
	}
	if _, err := fonts.FilePath(font); err != nil {
		return nil, err
	}

	previewCue := cue
	previewCue.SceneID = nil
	previewCue.StartTime = 0
	previewCue.EndTime = 1
	previewCue.Enabled = true
	previewCue.ReviewStatus = "approved"
	previewCue.Animation = "none" // PNG represents the fully-visible frame; animation remains in the preflight contract.
	assData, count := buildScreenTextASS([]models.ScreenTextCue{previewCue}, []models.Scene{{ID: 1, Duration: 1}}, []sceneVideo{{dur: 1}}, width, height, font.Family)
	if count != 1 {
		return nil, fmt.Errorf("功能文字预览没有可渲染内容")
	}

	tempDir, err := os.MkdirTemp("", "screen-text-preview-"+strconv.FormatUint(uint64(cueID), 10)+"-")
	if err != nil {
		return nil, err
	}
	defer os.RemoveAll(tempDir)
	assPath := filepath.Join(tempDir, "preview.ass")
	pngPath := filepath.Join(tempDir, "preview.png")
	if err := os.WriteFile(assPath, assData, 0o600); err != nil {
		return nil, err
	}
	ffmpeg, err := localFFmpegPath()
	if err != nil {
		return nil, err
	}
	if _, err := runLocalProgram(ffmpeg, screenTextPreviewFFmpegArgs(width, height, assPath, fonts.Root(), pngPath), 30*time.Second); err != nil {
		return nil, fmt.Errorf("功能文字透明预览渲染失败: %w", err)
	}
	data, err := os.ReadFile(pngPath)
	if err != nil {
		return nil, err
	}
	if len(data) < 8 || string(data[:8]) != "\x89PNG\r\n\x1a\n" {
		return nil, fmt.Errorf("功能文字预览输出不是有效 PNG")
	}
	return data, nil
}
