package service

import (
	"bytes"
	"fmt"
	"image"
	"image/png"
	"path/filepath"
	"strings"
	"time"

	"golang.org/x/image/draw"

	"comfyui-console/internal/models"
)

// Tail-frame upscaling runs the ComfyUI VOSR2 workflow through the same task pipeline as clay
// and line art (StartFrameConversion with kind "hd"). VOSR2 only takes an integer factor, so it
// upscales just past frameUpscaleLongEdge and applyFrameConversion resizes the result once so its
// longest edge is exactly frameUpscaleLongEdge. The source frame is never modified: the result is
// a separate derived FrameCandidate.
const (
	TemplateVOSR2Upscale = "vosr2_image_upscale"
	frameUpscaleLongEdge = 2048
	// frameUpscaleMaxFactor bounds the generative step for very small frames; the final resize
	// covers the rest.
	frameUpscaleMaxFactor = 4
)

// upscaleTargetSize scales w×h so the longest edge equals frameUpscaleLongEdge.
func upscaleTargetSize(w, h int) (int, int) {
	if w >= h {
		return frameUpscaleLongEdge, max(1, (h*frameUpscaleLongEdge+w/2)/w)
	}
	return max(1, (w*frameUpscaleLongEdge+h/2)/h), frameUpscaleLongEdge
}

// frameUpscaleFactor is the smallest integer factor that brings longEdge to frameUpscaleLongEdge.
func frameUpscaleFactor(longEdge int) int {
	return min(frameUpscaleMaxFactor, (frameUpscaleLongEdge+longEdge-1)/longEdge)
}

// frameUpscaleTaskRequest builds the VOSR2 task for one tail frame of projectID.
func (s *ProjectService) frameUpscaleTaskRequest(projectID uint, imageFile string, data []byte) (CreateTaskReq, error) {
	cfg, _, err := image.DecodeConfig(bytes.NewReader(data))
	if err != nil {
		return CreateTaskReq{}, fmt.Errorf("无法读取尾帧尺寸: %w", err)
	}
	if cfg.Width <= 0 || cfg.Height <= 0 {
		return CreateTaskReq{}, fmt.Errorf("尾帧尺寸无效")
	}
	longEdge := max(cfg.Width, cfg.Height)
	if longEdge >= frameUpscaleLongEdge {
		return CreateTaskReq{}, fmt.Errorf("尾帧长边已达 %d，无需超分", longEdge)
	}
	tpl, err := enabledTemplateByCode(s.db, TemplateVOSR2Upscale)
	if err != nil {
		return CreateTaskReq{}, err
	}
	return CreateTaskReq{TemplateID: tpl.ID, Params: map[string]any{"upscale": frameUpscaleFactor(longEdge)},
		Files: map[string][]FileMeta{"source_image": {{TaskID: fmt.Sprint(projectID), Name: imageFile}}}}, nil
}

// finishFrameUpscale resizes a downloaded VOSR2 result so its longest edge is
// frameUpscaleLongEdge with the source frame's exact aspect, and returns the file to record.
func (s *ProjectService) finishFrameUpscale(current *models.FrameCandidate, source, result []byte, resultFile string) (string, error) {
	src, _, err := image.DecodeConfig(bytes.NewReader(source))
	if err != nil {
		return "", fmt.Errorf("无法读取尾帧尺寸: %w", err)
	}
	img, _, err := image.Decode(bytes.NewReader(result))
	if err != nil {
		return "", fmt.Errorf("读取高清结果失败: %w", err)
	}
	b := img.Bounds()
	if max(b.Dx(), b.Dy()) <= max(src.Width, src.Height) {
		return "", fmt.Errorf("高清结果 %dx%d 没有比原尾帧 %dx%d 更大，已丢弃", b.Dx(), b.Dy(), src.Width, src.Height)
	}
	w, h := upscaleTargetSize(src.Width, src.Height)
	if b.Dx() == w && b.Dy() == h {
		return resultFile, nil
	}
	dst := image.NewRGBA(image.Rect(0, 0, w, h))
	draw.CatmullRom.Scale(dst, dst.Bounds(), img, b, draw.Src, nil)
	var buf bytes.Buffer
	if err := png.Encode(&buf, dst); err != nil {
		return "", err
	}
	// SaveImage writes PNG, so the resized image normally replaces the downloaded file in place.
	name := resultFile
	if !strings.EqualFold(filepath.Ext(name), ".png") {
		name = fmt.Sprintf("continuity_s%d_hd_%d.png", current.SceneID, time.Now().UnixNano())
	}
	path, _, err := s.upload.SaveFile(fmt.Sprint(current.ProjectID), "image", name, buf.Bytes())
	if err != nil {
		return "", fmt.Errorf("保存高清结果失败: %w", err)
	}
	return filepath.Base(path), nil
}
