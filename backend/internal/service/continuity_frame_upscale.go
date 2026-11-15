package service

import (
	"bytes"
	"context"
	"crypto/sha256"
	"errors"
	"fmt"
	"image"
	"image/png"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/gin-gonic/gin"
	"golang.org/x/image/draw"

	"comfyui-console/internal/models"
)

// Local tail-frame upscaling with the Real-ESRGAN NCNN Vulkan executable. The engine is located
// from the realesrgan_path setting, then the REALESRGAN_PATH environment variable, then PATH;
// its models/realesrgan-x4plus.{bin,param} must sit next to it. The x4plus model always runs at
// its native 4x scale and the result is resized once so its longest edge is frameUpscaleLongEdge.
// The source frame is never modified: the result is a separate derived FrameCandidate.
const (
	SettingRealESRGANPath = "realesrgan_path"
	realESRGANExeName     = "realesrgan-ncnn-vulkan"
	realESRGANModel       = "realesrgan-x4plus"
	frameUpscaleLongEdge  = 2048
	frameUpscaleTimeout   = 300 * time.Second
)

// realESRGANTileSizes are tried in order; 0 lets NCNN choose. Smaller explicit tiles are only
// used after a GPU memory failure, which some integrated GPUs hit with the automatic size.
var realESRGANTileSizes = []int{0, 256, 128, 64}

var (
	errFrameUpscaleBusy = errors.New("本地超分正在处理另一张图片，请稍后再试")
	// frameUpscaleMu serializes engine runs: one GPU job at a time, extra requests get 409.
	frameUpscaleMu sync.Mutex
)

// realESRGANRun executes the engine and returns its combined output and exit code. Tests
// replace it with a fake engine.
var realESRGANRun = func(ctx context.Context, exe string, args []string) (string, int, error) {
	cmd := exec.CommandContext(ctx, exe, args...)
	cmd.Dir = filepath.Dir(exe)
	var output bytes.Buffer
	cmd.Stdout, cmd.Stderr = &output, &output
	err := cmd.Run()
	var exitErr *exec.ExitError
	if errors.As(err, &exitErr) {
		return output.String(), exitErr.ExitCode(), nil
	}
	return output.String(), 0, err
}

type realESRGANEngine struct {
	Exe, ModelDir string
}

// resolveRealESRGAN finds the engine; configured is the realesrgan_path setting value.
func resolveRealESRGAN(configured string) (*realESRGANEngine, error) {
	exe := ""
	for _, candidate := range []string{strings.TrimSpace(configured), strings.TrimSpace(os.Getenv("REALESRGAN_PATH"))} {
		if candidate == "" {
			continue
		}
		if info, err := os.Stat(candidate); err != nil || info.IsDir() {
			return nil, fmt.Errorf("本地超分引擎路径不存在或不是文件: %s", candidate)
		}
		exe = candidate
		break
	}
	if exe == "" {
		found, err := exec.LookPath(realESRGANExeName)
		if err != nil {
			return nil, fmt.Errorf("未找到本地超分引擎 %s.exe：请在设置中填写其完整路径，或设置 REALESRGAN_PATH / 加入 PATH", realESRGANExeName)
		}
		exe = found
	}
	if abs, err := filepath.Abs(exe); err == nil {
		exe = abs
	}
	modelDir := filepath.Join(filepath.Dir(exe), "models")
	for _, ext := range []string{".bin", ".param"} {
		if _, err := os.Stat(filepath.Join(modelDir, realESRGANModel+ext)); err != nil {
			return nil, fmt.Errorf("本地超分模型不完整：%s 下缺少 %s%s", modelDir, realESRGANModel, ext)
		}
	}
	return &realESRGANEngine{Exe: exe, ModelDir: modelDir}, nil
}

func (s *ProjectService) realESRGANSetting() string {
	if s == nil || s.volc == nil {
		return ""
	}
	return s.volc.GetSetting(SettingRealESRGANPath, "")
}

// isGPUMemoryFailure recognizes NCNN/Vulkan failures worth retrying with smaller tiles.
func isGPUMemoryFailure(exitCode int, output string) bool {
	text := strings.ToLower(output)
	for _, marker := range []string{"vkallocatememory failed", "vk_error_out_of_device_memory", "out of device memory", "out of gpu memory", "failed to allocate gpu memory"} {
		if strings.Contains(text, marker) {
			return true
		}
	}
	code := uint32(exitCode)
	return code == 0xC0000005 || code == 0xC0000017 // access violation after a failed allocation / STATUS_NO_MEMORY
}

// upscaleTargetSize scales w×h so the longest edge equals frameUpscaleLongEdge.
func upscaleTargetSize(w, h int) (int, int) {
	if w >= h {
		return frameUpscaleLongEdge, max(1, (h*frameUpscaleLongEdge+w/2)/w)
	}
	return max(1, (w*frameUpscaleLongEdge+h/2)/h), frameUpscaleLongEdge
}

// upscaleImage runs the engine on data (whose extension is ext) and returns a PNG whose longest
// edge is frameUpscaleLongEdge. The second result reports whether low-memory tiles were needed.
func upscaleImage(engine *realESRGANEngine, data []byte, ext string) ([]byte, bool, error) {
	cfg, _, err := image.DecodeConfig(bytes.NewReader(data))
	if err != nil {
		return nil, false, fmt.Errorf("无法读取尾帧尺寸: %w", err)
	}
	if cfg.Width <= 0 || cfg.Height <= 0 {
		return nil, false, fmt.Errorf("尾帧尺寸无效")
	}
	if max(cfg.Width, cfg.Height) >= frameUpscaleLongEdge {
		return nil, false, fmt.Errorf("尾帧长边已达 %d，无需超分", max(cfg.Width, cfg.Height))
	}
	dir, err := os.MkdirTemp("", "frame-upscale-")
	if err != nil {
		return nil, false, err
	}
	defer os.RemoveAll(dir)
	in, out := filepath.Join(dir, "source"+ext), filepath.Join(dir, "native.png")
	if err := os.WriteFile(in, data, 0o644); err != nil {
		return nil, false, err
	}
	ctx, cancel := context.WithTimeout(context.Background(), frameUpscaleTimeout)
	defer cancel()
	lowMemory := false
	for i, tile := range realESRGANTileSizes {
		_ = os.Remove(out)
		args := []string{"-i", in, "-o", out, "-m", engine.ModelDir, "-n", realESRGANModel, "-s", "4", "-t", fmt.Sprint(tile)}
		if tile > 0 {
			// One worker per stage avoids doubling tile buffers on low-memory GPUs.
			args = append(args, "-j", "1:1:1")
		}
		args = append(args, "-f", "png")
		output, code, runErr := realESRGANRun(ctx, engine.Exe, args)
		if ctx.Err() == context.DeadlineExceeded {
			return nil, false, fmt.Errorf("本地超分超时（%d 秒）", int(frameUpscaleTimeout.Seconds()))
		}
		if runErr != nil {
			return nil, false, fmt.Errorf("启动本地超分引擎失败: %w", runErr)
		}
		if code == 0 {
			lowMemory = tile > 0
			break
		}
		if !isGPUMemoryFailure(code, output) {
			return nil, false, fmt.Errorf("超分引擎退出码 %d: %s", code, tailText(output, 800))
		}
		if i == len(realESRGANTileSizes)-1 {
			return nil, false, fmt.Errorf("显卡可用显存不足，已自动尝试低显存模式仍失败；请关闭占用显卡的程序或更新显卡驱动后重试")
		}
	}
	native, err := os.ReadFile(out)
	if err != nil || len(native) == 0 {
		return nil, false, fmt.Errorf("超分引擎未生成有效图片")
	}
	img, _, err := image.Decode(bytes.NewReader(native))
	if err != nil {
		return nil, false, fmt.Errorf("读取超分结果失败: %w", err)
	}
	if b := img.Bounds(); b.Dx() != cfg.Width*4 || b.Dy() != cfg.Height*4 {
		return nil, false, fmt.Errorf("超分原生尺寸异常，期望 %dx%d，实际 %dx%d", cfg.Width*4, cfg.Height*4, b.Dx(), b.Dy())
	}
	w, h := upscaleTargetSize(cfg.Width, cfg.Height)
	dst := image.NewRGBA(image.Rect(0, 0, w, h))
	draw.CatmullRom.Scale(dst, dst.Bounds(), img, img.Bounds(), draw.Src, nil)
	var buf bytes.Buffer
	if err := png.Encode(&buf, dst); err != nil {
		return nil, false, err
	}
	return buf.Bytes(), lowMemory, nil
}

func tailText(text string, n int) string {
	text = strings.TrimSpace(text)
	if r := []rune(text); len(r) > n {
		return string(r[len(r)-n:])
	}
	return text
}

// UpscaleFrame upscales one tail frame of sceneID locally and records the result as a derived
// "hd" FrameCandidate. The result is discarded if the source image changed meanwhile.
func (s *ProjectService) UpscaleFrame(projectID, sceneID, frameID uint) (*models.FrameCandidate, string, error) {
	var frame models.FrameCandidate
	if err := s.db.Where("id = ? AND project_id = ? AND scene_id = ? AND type IN ?", frameID, projectID, sceneID,
		[]models.FrameCandidateType{models.FrameCandidateCandidate, models.FrameCandidateSelected}).First(&frame).Error; err != nil {
		return nil, "", fmt.Errorf("候选帧不存在")
	}
	var scene models.Scene
	if err := s.db.Where("id = ? AND project_id = ?", sceneID, projectID).First(&scene).Error; err != nil {
		return nil, "", fmt.Errorf("场景不存在")
	}
	if frame.VideoTaskID != scene.VideoTaskID {
		return nil, "", fmt.Errorf("候选帧来自旧视频，请重新提取")
	}
	engine, err := resolveRealESRGAN(s.realESRGANSetting())
	if err != nil {
		return nil, "", err
	}
	if !frameUpscaleMu.TryLock() {
		return nil, "", errFrameUpscaleBusy
	}
	defer frameUpscaleMu.Unlock()
	source, err := s.readProjectInput(projectID, frame.ImageFile)
	if err != nil {
		return nil, "", fmt.Errorf("读取尾帧失败: %w", err)
	}
	ext := strings.ToLower(filepath.Ext(frame.ImageFile))
	if ext != ".png" && ext != ".jpg" && ext != ".jpeg" && ext != ".webp" {
		return nil, "", fmt.Errorf("尾帧格式不支持超分: %s", ext)
	}
	result, lowMemory, err := upscaleImage(engine, source, ext)
	if err != nil {
		return nil, "", err
	}
	var current models.FrameCandidate
	if err := s.db.First(&current, frame.ID).Error; err != nil {
		return nil, "", fmt.Errorf("原尾帧已删除，高清结果已丢弃")
	}
	after, err := s.readProjectInput(projectID, current.ImageFile)
	if err != nil || current.ImageFile != frame.ImageFile || sha256.Sum256(after) != sha256.Sum256(source) {
		return nil, "", fmt.Errorf("原尾帧在超分期间已变化，高清结果已丢弃，请重新超分")
	}
	name := fmt.Sprintf("continuity_s%d_hd_%d.png", sceneID, time.Now().UnixNano())
	path, _, err := s.upload.SaveFile(fmt.Sprint(projectID), "image", name, result)
	if err != nil {
		return nil, "", fmt.Errorf("保存高清结果失败: %w", err)
	}
	if _, err := s.recordDerivedFrame(&current, FrameReferenceHD, filepath.Base(path)); err != nil {
		return nil, "", err
	}
	s.pushProject(nil)
	message := "尾帧高清图已生成，原尾帧保持不变"
	if lowMemory {
		message += "（低显存兼容模式）"
	}
	return &current, message, nil
}

func (s *Service) HandleUpscaleFrameCandidate(c *gin.Context) {
	pid, sid, ok := continuityIDs(c)
	if !ok {
		return
	}
	fid, ok := pathUint(c, "fid")
	if !ok {
		return
	}
	if s.proxyUnsupported() {
		c.JSON(http.StatusUnprocessableEntity, gin.H{"error": "SSH远程模式暂不支持本地超分；请在本机可读共享目录的模式下使用"})
		return
	}
	frame, message, err := s.Projects.UpscaleFrame(pid, sid, fid)
	if err != nil {
		status := http.StatusBadRequest
		if errors.Is(err, errFrameUpscaleBusy) {
			status = http.StatusConflict
		}
		c.JSON(status, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{"frame": s.frameResponses([]models.FrameCandidate{*frame})[0], "message": message})
}
