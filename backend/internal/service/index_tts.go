package service

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"comfyui-console/internal/config"
	"comfyui-console/internal/indextts"
	"comfyui-console/internal/models"
	"gorm.io/gorm"
)

type LocalIndexTTS struct {
	cfg          config.IndexTTSConfig
	comfyDir     string
	db           *gorm.DB
	mu           sync.Mutex
	model        indextts.Model
	voices       map[string]indextts.Voice
	fingerprint  string
	capabilities indextts.Capabilities
	modelInfo    indextts.ModelInfo
	startupError string
	queue        chan struct{}
	closed       bool
	loadModel    func(string, int) (indextts.Model, error)
}

func NewLocalIndexTTS(cfg *config.Config, db *gorm.DB) *LocalIndexTTS {
	if cfg == nil {
		return &LocalIndexTTS{db: db, voices: map[string]indextts.Voice{}, queue: make(chan struct{}, 1), loadModel: indextts.LoadCUDA}
	}
	size := cfg.IndexTTS.QueueSize
	if size <= 0 {
		size = 32
	}
	return &LocalIndexTTS{cfg: cfg.IndexTTS, comfyDir: cfg.Comfy.ComfyDir, db: db, voices: map[string]indextts.Voice{}, queue: make(chan struct{}, size), loadModel: indextts.LoadCUDA}
}

func (s *LocalIndexTTS) Enabled() bool { return s != nil && s.cfg.Enabled }

func (s *LocalIndexTTS) Available() bool {
	if !s.Enabled() {
		return false
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.startupError == "" && !s.closed
}

func (s *LocalIndexTTS) RuntimeDetails() (indextts.Capabilities, indextts.ModelInfo, indextts.Health) {
	if s == nil {
		return indextts.Capabilities{}, indextts.ModelInfo{}, indextts.Health{}
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	var health indextts.Health
	if s.model != nil {
		health, _ = s.model.Health()
	}
	return s.capabilities, s.modelInfo, health
}

func (s *LocalIndexTTS) Status() (bool, bool, string) {
	if s == nil {
		return false, false, ""
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.cfg.Enabled, s.startupError == "" && !s.closed, s.startupError
}

func (s *LocalIndexTTS) setStartupError(err error) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if err == nil {
		s.startupError = ""
		return nil
	}
	s.startupError = err.Error()
	return err
}

func (s *LocalIndexTTS) Start() error {
	if !s.Enabled() {
		return nil
	}
	if strings.TrimSpace(s.cfg.ModelDir) == "" {
		return s.setStartupError(fmt.Errorf("IndexTTS 已启用但 model_dir 为空"))
	}
	if info, err := os.Stat(s.cfg.ModelDir); err != nil || !info.IsDir() {
		if err == nil {
			err = fmt.Errorf("不是目录")
		}
		return s.setStartupError(fmt.Errorf("IndexTTS model_dir 不可用: %w", err))
	}
	// Startup is deliberately VRAM-free. The CUDA model is loaded on the first
	// explicit synthesis request, after the shared-GPU conflict check succeeds.
	s.mu.Lock()
	s.fingerprint = s.modelFingerprint("deferred-cuda-load")
	s.startupError = ""
	s.mu.Unlock()
	return nil
}

func (s *LocalIndexTTS) ensureModelLoaded() error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.closed {
		return fmt.Errorf("本地 IndexTTS 已关闭")
	}
	if s.model != nil {
		return nil
	}
	loader := s.loadModel
	if loader == nil {
		loader = indextts.LoadCUDA
	}
	model, err := loader(s.cfg.ModelDir, s.cfg.DeviceIndex)
	if err != nil {
		s.startupError = fmt.Sprintf("按需加载本地 IndexTTS CUDA 模型失败: %v", err)
		return fmt.Errorf("%s", s.startupError)
	}
	s.model = model
	s.capabilities = model.Capabilities()
	if info, infoErr := model.Info(); infoErr == nil {
		s.modelInfo = info
	}
	s.startupError = ""
	return nil
}

func (s *LocalIndexTTS) modelFingerprint(version string) string {
	hash := sha256.New()
	hash.Write([]byte("indextts-local-v2\x00" + version + "\x00" + s.modelInfo.ModelVersion + "\x00" + s.modelInfo.ManifestSHA256 + "\x00" + s.modelInfo.Backend + "\x00" + s.modelInfo.Device + "\x00" + s.cfg.ModelDir + "\x00" + fmt.Sprint(s.cfg.DeviceIndex)))
	for _, name := range []string{"manifest.json", "model_manifest.json"} {
		if data, err := os.ReadFile(filepath.Join(s.cfg.ModelDir, name)); err == nil {
			hash.Write(data)
			break
		}
	}
	return hex.EncodeToString(hash.Sum(nil))
}

func (s *LocalIndexTTS) Fingerprint() string {
	if !s.Enabled() {
		return ""
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.fingerprint
}

func (s *LocalIndexTTS) referenceFor(d models.Dialogue) (string, error) {
	var reference string
	if name := strings.TrimSpace(d.Character); name != "" {
		var character models.Character
		if err := s.db.Where("project_id = ? AND name = ?", d.ProjectID, name).First(&character).Error; err == nil {
			reference = strings.TrimSpace(character.VoiceRef)
		}
	}
	if reference == "" {
		reference = strings.TrimSpace(s.cfg.DefaultVoice)
	}
	if reference == "" {
		return "", fmt.Errorf("角色「%s」尚未配置本地参考音色", d.Character)
	}
	if filepath.IsAbs(reference) {
		return filepath.Clean(reference), nil
	}
	if strings.Contains(reference, "..") || filepath.Base(reference) != reference {
		return "", fmt.Errorf("本地参考音色路径无效")
	}
	return filepath.Join(s.comfyDir, "input", fmt.Sprint(d.ProjectID), reference), nil
}

func (s *LocalIndexTTS) ReferenceFingerprint(d models.Dialogue) (string, error) {
	path, err := s.referenceFor(d)
	if err != nil {
		return "", err
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return "", fmt.Errorf("本地参考音色不可读取: %w", err)
	}
	return fmt.Sprintf("%x", sha256.Sum256(data)), nil
}

func (s *LocalIndexTTS) voice(path string) (indextts.Voice, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("本地参考音色不可读取: %w", err)
	}
	key := fmt.Sprintf("%x", sha256.Sum256(data))
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.closed || s.model == nil {
		return nil, fmt.Errorf("本地 IndexTTS 尚未启动或已关闭")
	}
	if voice := s.voices[key]; voice != nil {
		return voice, nil
	}
	voice, err := s.model.PrepareVoice(path)
	if err != nil {
		return nil, err
	}
	s.voices[key] = voice
	return voice, nil
}

type LocalIndexTTSSynthesis struct {
	WAV  []byte
	Info indextts.GenerationInfo
}

func (s *LocalIndexTTS) ensureGPUAvailable() error {
	if s.db == nil {
		return nil
	}
	var active int64
	if err := s.db.Model(&models.Task{}).Where("gpu_index = ? AND status IN ?", s.cfg.DeviceIndex, []string{"pending", "queued", "running"}).Count(&active).Error; err != nil {
		return err
	}
	if active > 0 {
		return fmt.Errorf("GPU %d 正在执行 ComfyUI 任务，本地配音暂不启动；请稍后重试以避免显存冲突", s.cfg.DeviceIndex)
	}
	return nil
}

func (s *LocalIndexTTS) Synthesize(ctx context.Context, d models.Dialogue) (LocalIndexTTSSynthesis, error) {
	if !s.Enabled() {
		return LocalIndexTTSSynthesis{}, fmt.Errorf("本地 IndexTTS 未启用")
	}
	configured, available, startupError := s.Status()
	if configured && !available {
		if startupError == "" {
			startupError = "本地运行时未加载"
		}
		return LocalIndexTTSSynthesis{}, fmt.Errorf("本地 IndexTTS 当前不可用: %s", startupError)
	}
	if err := s.ensureGPUAvailable(); err != nil {
		return LocalIndexTTSSynthesis{}, err
	}
	select {
	case s.queue <- struct{}{}:
		defer func() { <-s.queue }()
	default:
		return LocalIndexTTSSynthesis{}, fmt.Errorf("本地 IndexTTS 队列已满，请稍后重试")
	}
	if err := s.ensureModelLoaded(); err != nil {
		return LocalIndexTTSSynthesis{}, err
	}
	path, err := s.referenceFor(d)
	if err != nil {
		return LocalIndexTTSSynthesis{}, err
	}
	voice, err := s.voice(path)
	if err != nil {
		return LocalIndexTTSSynthesis{}, fmt.Errorf("准备本地参考音色失败: %w", err)
	}
	s.mu.Lock()
	model := s.model
	s.mu.Unlock()
	language := strings.TrimSpace(s.cfg.Language)
	if language == "" {
		language = "ZH"
	}
	timeout := time.Duration(s.cfg.TimeoutSeconds) * time.Second
	if timeout <= 0 {
		timeout = 5 * time.Minute
	}
	callCtx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()
	emotionText := strings.TrimSpace(strings.Join([]string{strings.TrimSpace(d.Emotion), strings.TrimSpace(d.Delivery)}, "；"))
	emotionText = strings.Trim(emotionText, "；")
	options := indextts.Options{Language: language, Seed: 0, DurationFactor: 1}
	if emotionText != "" && s.capabilities.SupportsEmotionText {
		options.EmotionText = emotionText
		options.EmotionStrength = 1
	}
	result, err := model.GenerateContext(callCtx, voice, d.Text, options)
	if err != nil {
		return LocalIndexTTSSynthesis{}, fmt.Errorf("本地 IndexTTS 生成失败: %w", err)
	}
	wav, err := encodePCM16WAV(result.Audio)
	if err != nil {
		return LocalIndexTTSSynthesis{}, err
	}
	return LocalIndexTTSSynthesis{WAV: wav, Info: result.Info}, nil
}

func (s *LocalIndexTTS) Close() error {
	if s == nil {
		return nil
	}
	s.mu.Lock()
	if s.closed {
		s.mu.Unlock()
		return nil
	}
	s.closed = true
	model := s.model
	voices := s.voices
	s.model = nil
	s.voices = map[string]indextts.Voice{}
	s.mu.Unlock()
	if model != nil {
		_ = model.Cancel()
	}
	for _, voice := range voices {
		_ = voice.Close()
	}
	if model != nil {
		return model.Close()
	}
	return nil
}
