package service

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"

	"comfyui-console/internal/models"
	"github.com/gin-gonic/gin"
)

type voicePresetManifest struct {
	Voices []voicePreset `json:"voices"`
}

type voicePreset struct {
	File     string   `json:"file"`
	Label    string   `json:"label"`
	Source   string   `json:"source"`
	Language string   `json:"language"`
	Tags     []string `json:"tags"`
	URL      string   `json:"url,omitempty"`
}

type customVoiceReference struct {
	File      string    `json:"file"`
	Label     string    `json:"label"`
	URL       string    `json:"url"`
	Size      int64     `json:"size"`
	BoundTo   []string  `json:"bound_to"`
	CreatedAt time.Time `json:"created_at"`
}

func (s *Service) voicePresetRoot() string {
	if s.Cfg == nil || strings.TrimSpace(s.Cfg.Comfy.ComfyDir) == "" {
		return ""
	}
	return filepath.Join(s.Cfg.Comfy.ComfyDir, "models", "TTS", "IndexTTS-2.5", "voices")
}

func safeVoicePresetPath(root, name string) (string, error) {
	name = filepath.ToSlash(strings.TrimSpace(name))
	if root == "" || name == "" || filepath.IsAbs(name) || strings.Contains(name, "\\") {
		return "", fmt.Errorf("预置音色路径无效")
	}
	clean := filepath.ToSlash(filepath.Clean(filepath.FromSlash(name)))
	if clean != name || clean == "." || strings.HasPrefix(clean, "../") || strings.ToLower(filepath.Ext(clean)) != ".wav" {
		return "", fmt.Errorf("预置音色路径无效")
	}
	root = filepath.Clean(root)
	candidate := filepath.Join(root, filepath.FromSlash(clean))
	rel, err := filepath.Rel(root, candidate)
	if err != nil || rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
		return "", fmt.Errorf("预置音色路径越界")
	}
	if info, err := os.Stat(candidate); err != nil || info.IsDir() {
		return "", fmt.Errorf("预置音色不存在")
	}
	return candidate, nil
}

func (s *Service) loadVoicePresets() ([]voicePreset, error) {
	root := s.voicePresetRoot()
	data, err := os.ReadFile(filepath.Join(root, "manifest.json"))
	if err != nil {
		return nil, fmt.Errorf("读取预置音色清单失败: %w", err)
	}
	var manifest voicePresetManifest
	if err := json.Unmarshal(data, &manifest); err != nil {
		return nil, fmt.Errorf("预置音色清单无效: %w", err)
	}
	out := make([]voicePreset, 0, len(manifest.Voices))
	for _, voice := range manifest.Voices {
		if _, err := safeVoicePresetPath(root, voice.File); err != nil {
			continue
		}
		voice.URL = "/api/projects/:id/voice-presets/audio?file=" + voice.File
		out = append(out, voice)
	}
	return out, nil
}

func (s *Service) HandleListVoicePresets(c *gin.Context) {
	p, ok := s.loadProject(c)
	if !ok {
		return
	}
	voices, err := s.loadVoicePresets()
	if err != nil {
		c.JSON(http.StatusServiceUnavailable, gin.H{"error": err.Error()})
		return
	}
	for i := range voices {
		voices[i].URL = fmt.Sprintf("/api/projects/%d/voice-presets/audio?file=%s", p.ID, voices[i].File)
	}
	c.JSON(http.StatusOK, voices)
}

func (s *Service) HandleGetVoicePresetAudio(c *gin.Context) {
	if _, ok := s.loadProject(c); !ok {
		return
	}
	path, err := safeVoicePresetPath(s.voicePresetRoot(), c.Query("file"))
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	c.Header("Content-Type", "audio/wav")
	c.File(path)
}

func (s *Service) saveVoicePresetForProject(projectID uint, presetFile string) (string, error) {
	path, err := safeVoicePresetPath(s.voicePresetRoot(), presetFile)
	if err != nil {
		return "", err
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return "", err
	}
	digest := sha256.Sum256(data)
	name := fmt.Sprintf("voice_preset_%s.wav", hex.EncodeToString(digest[:8]))
	var existing int64
	if s.DB.Model(&models.UploadFile{}).Where("task_id = ? AND name = ? AND type = ?", fmt.Sprint(projectID), name, "audio").Count(&existing).Error == nil && existing > 0 {
		return name, nil
	}
	stored, _, err := s.Upload.SaveFile(fmt.Sprint(projectID), "audio", name, data)
	if err != nil {
		return "", err
	}
	return filepath.Base(stored), nil
}

func (s *Service) projectCustomVoiceFile(projectID uint, name string) (string, error) {
	name = strings.TrimSpace(name)
	if name == "" || filepath.Base(name) != name || strings.HasPrefix(name, "voice_preset_") {
		return "", fmt.Errorf("自定义音色文件无效")
	}
	var upload models.UploadFile
	if err := s.DB.Where("task_id = ? AND type = ? AND name = ?", fmt.Sprint(projectID), "audio", name).First(&upload).Error; err != nil {
		return "", fmt.Errorf("自定义音色不存在")
	}
	lower := strings.ToLower(name)
	if !strings.HasPrefix(lower, "voice_") || strings.HasPrefix(lower, "dub_") {
		return "", fmt.Errorf("该文件不是参考音色")
	}
	return name, nil
}

func (s *Service) HandleListCustomVoices(c *gin.Context) {
	p, ok := s.loadProject(c)
	if !ok {
		return
	}
	var uploads []models.UploadFile
	if err := s.DB.Where("task_id = ? AND type = ? AND name LIKE ?", fmt.Sprint(p.ID), "audio", "voice_%").Order("created_at DESC").Find(&uploads).Error; err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	var characters []models.Character
	s.DB.Where("project_id = ? AND voice_ref <> ''", p.ID).Find(&characters)
	bindings := map[string][]string{}
	for _, character := range characters {
		bindings[character.VoiceRef] = append(bindings[character.VoiceRef], character.Name)
	}
	if p.NarratorVoiceRef != "" {
		bindings[p.NarratorVoiceRef] = append(bindings[p.NarratorVoiceRef], "旁白")
	}
	seen := map[string]bool{}
	out := []customVoiceReference{}
	for _, upload := range uploads {
		if seen[upload.Name] || strings.HasPrefix(upload.Name, "voice_preset_") {
			continue
		}
		if _, err := s.projectCustomVoiceFile(p.ID, upload.Name); err != nil {
			continue
		}
		seen[upload.Name] = true
		out = append(out, customVoiceReference{File: upload.Name, Label: upload.Name, URL: fmt.Sprintf("/api/input/%d/%s", p.ID, upload.Name), Size: upload.Size, BoundTo: bindings[upload.Name], CreatedAt: upload.CreatedAt})
	}
	c.JSON(http.StatusOK, out)
}

func (s *Service) HandleSelectCharacterCustomVoice(c *gin.Context) {
	ch, ok := s.loadCharacter(c)
	if !ok {
		return
	}
	var req struct {
		File string `json:"file"`
	}
	if c.ShouldBindJSON(&req) != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "参数错误"})
		return
	}
	name, err := s.projectCustomVoiceFile(ch.ProjectID, req.File)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	if err := s.DB.Model(&models.Character{}).Where("id = ? AND project_id = ?", ch.ID, ch.ProjectID).Updates(map[string]any{"voice_ref": name, "voice": "", "voice_id": "", "voice_model": ""}).Error; err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	_ = invalidateCharacterDialogueAudio(s.DB, ch.ProjectID, ch.Name, "角色自定义参考音色已切换")
	s.Projects.PushProject(nil)
	c.JSON(http.StatusOK, gin.H{"ok": true, "voice_ref": name})
}

func (s *Service) HandleSelectNarratorCustomVoice(c *gin.Context) {
	p, ok := s.loadProject(c)
	if !ok {
		return
	}
	var req struct {
		File string `json:"file"`
	}
	if c.ShouldBindJSON(&req) != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "参数错误"})
		return
	}
	name, err := s.projectCustomVoiceFile(p.ID, req.File)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	if err := s.DB.Model(&models.Project{}).Where("id = ?", p.ID).Update("narrator_voice_ref", name).Error; err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	_ = invalidateCharacterDialogueAudio(s.DB, p.ID, "旁白", "旁白自定义参考音色已切换")
	s.Projects.PushProject(nil)
	c.JSON(http.StatusOK, gin.H{"ok": true, "voice_ref": name})
}

func (s *Service) HandleSelectCharacterVoicePreset(c *gin.Context) {
	ch, ok := s.loadCharacter(c)
	if !ok {
		return
	}
	var req struct {
		File string `json:"file"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "参数错误"})
		return
	}
	stored, err := s.saveVoicePresetForProject(ch.ProjectID, req.File)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	if err := s.DB.Model(&models.Character{}).Where("id = ? AND project_id = ?", ch.ID, ch.ProjectID).Updates(map[string]any{
		"voice_ref": filepath.Base(stored), "voice": "", "voice_id": "", "voice_model": "",
	}).Error; err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	_ = invalidateCharacterDialogueAudio(s.DB, ch.ProjectID, ch.Name, "角色预置参考音色已更新")
	s.Projects.PushProject(nil)
	c.JSON(http.StatusOK, gin.H{"ok": true, "voice_ref": filepath.Base(stored), "preset": req.File})
}

func (s *Service) HandleSelectNarratorVoicePreset(c *gin.Context) {
	p, ok := s.loadProject(c)
	if !ok {
		return
	}
	var req struct {
		File string `json:"file"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "参数错误"})
		return
	}
	stored, err := s.saveVoicePresetForProject(p.ID, req.File)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	if err := s.DB.Model(&models.Project{}).Where("id = ?", p.ID).Update("narrator_voice_ref", stored).Error; err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	_ = invalidateCharacterDialogueAudio(s.DB, p.ID, "旁白", "旁白预置参考音色已更新")
	s.Projects.PushProject(nil)
	c.JSON(http.StatusOK, gin.H{"ok": true, "voice_ref": stored, "preset": req.File})
}

func (s *Service) HandleUploadNarratorVoice(c *gin.Context) {
	p, ok := s.loadProject(c)
	if !ok {
		return
	}
	file, header, err := c.Request.FormFile("file")
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "file required"})
		return
	}
	defer file.Close()
	data, err := io.ReadAll(io.LimitReader(file, 10*1024*1024+1))
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "读取参考音频失败"})
		return
	}
	if len(data) == 0 || len(data) > 10*1024*1024 {
		c.JSON(http.StatusBadRequest, gin.H{"error": "参考音频必须为非空且不超过10MB"})
		return
	}
	ext := strings.ToLower(filepath.Ext(header.Filename))
	if ext != ".wav" && ext != ".mp3" && ext != ".m4a" && ext != ".aac" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "仅支持 WAV/MP3/M4A/AAC"})
		return
	}
	digest := sha256.Sum256(data)
	name := fmt.Sprintf("voice_narrator_%s%s", hex.EncodeToString(digest[:8]), ext)
	stored, _, err := s.Upload.SaveFile(fmt.Sprint(p.ID), "audio", name, data)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	stored = filepath.Base(stored)
	if err := s.DB.Model(&models.Project{}).Where("id = ?", p.ID).Update("narrator_voice_ref", stored).Error; err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	_ = invalidateCharacterDialogueAudio(s.DB, p.ID, "旁白", "旁白自定义参考音色已更新")
	s.Projects.PushProject(nil)
	c.JSON(http.StatusOK, gin.H{"ok": true, "voice_ref": stored})
}
