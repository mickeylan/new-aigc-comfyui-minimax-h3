package service

import (
	"crypto/sha256"
	"encoding/binary"
	"encoding/hex"
	"errors"
	"fmt"
	"log"
	"math"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/gin-gonic/gin"
	"gorm.io/gorm"

	"comfyui-console/internal/models"
)

// Voice references cut from Scene videos: ffmpeg cuts the user-selected range locally,
// ComfyUI Demucs keeps only Vocals, and the user previews the draft before it is bound.
// The result is a timbre reference only; it is never treated as dialogue recognition.
const (
	vocalSeparationTemplate    = "audio_vocal_separation"
	minVoiceExtractSeconds     = 2.0
	maxVoiceExtractSeconds     = 30.0
	voiceExtractPostFilter     = "highpass=f=70,lowpass=f=14000,loudnorm=I=-18:TP=-1.5:LRA=9"
	voiceExtractSampleRate     = 44100
	voiceExtractStatusRunning  = "separating"
	voiceExtractStatusReady    = "ready"
	voiceExtractStatusFailed   = "failed"
	voiceExtractStatusAccepted = "confirmed"
)

// voiceExtractionExecute is replaced in tests so no scheduler goroutine outlives the test DB.
var voiceExtractionExecute = func(tasks *TaskService, taskID string) error { return tasks.Execute(taskID) }

type voiceQuality struct {
	Duration    float64
	RMS         float64
	Peak        float64
	SpeechRatio float64
}

// analyzeVoiceWAV measures a PCM16 WAV with the same 20 ms block heuristic as the reference
// project. It only produces advisory numbers; a weak result warns and never blocks.
func analyzeVoiceWAV(data []byte) (voiceQuality, error) {
	if len(data) < 44 || string(data[:4]) != "RIFF" || string(data[8:12]) != "WAVE" {
		return voiceQuality{}, fmt.Errorf("人声草稿不是有效WAV")
	}
	var channels, bits int
	var rate int
	for offset := 12; offset+8 <= len(data); {
		size := int(binary.LittleEndian.Uint32(data[offset+4 : offset+8]))
		body := offset + 8
		if size < 0 || body+size > len(data) {
			size = len(data) - body
		}
		switch string(data[offset : offset+4]) {
		case "fmt ":
			if size >= 16 {
				channels = int(binary.LittleEndian.Uint16(data[body+2 : body+4]))
				rate = int(binary.LittleEndian.Uint32(data[body+4 : body+8]))
				bits = int(binary.LittleEndian.Uint16(data[body+14 : body+16]))
			}
		case "data":
			if bits != 16 || channels <= 0 || rate <= 0 {
				return voiceQuality{}, fmt.Errorf("人声草稿必须是16位PCM WAV")
			}
			pcm := data[body : body+size]
			frames := len(pcm) / (2 * channels)
			if frames == 0 {
				return voiceQuality{}, fmt.Errorf("人声草稿没有音频数据")
			}
			samples := make([]float64, frames)
			for i := 0; i < frames; i++ {
				sum := 0.0
				for ch := 0; ch < channels; ch++ {
					pos := (i*channels + ch) * 2
					sum += float64(int16(binary.LittleEndian.Uint16(pcm[pos:pos+2]))) / 32768
				}
				samples[i] = sum / float64(channels)
			}
			q := voiceQuality{Duration: float64(frames) / float64(rate)}
			energy := 0.0
			for _, v := range samples {
				energy += v * v
				q.Peak = math.Max(q.Peak, math.Abs(v))
			}
			q.RMS = math.Sqrt(energy / float64(frames))
			block := rate / 50
			if block < 1 {
				block = 1
			}
			blocks, voiced := frames/block, 0
			for b := 0; b < blocks; b++ {
				e := 0.0
				for _, v := range samples[b*block : (b+1)*block] {
					e += v * v
				}
				if math.Sqrt(e/float64(block)) > 0.006 {
					voiced++
				}
			}
			if blocks > 0 {
				q.SpeechRatio = float64(voiced) / float64(blocks)
			}
			return q, nil
		}
		offset = body + size
		if offset%2 != 0 {
			offset++
		}
	}
	return voiceQuality{}, fmt.Errorf("人声草稿缺少data块")
}

func voiceQualityWarning(q voiceQuality) string {
	if q.Duration < 1.8 || q.RMS < 0.003 || q.Peak < 0.02 || q.SpeechRatio < 0.12 {
		return "分离后有效人声偏少（可能是音乐/音效过强或片段内无人说话）；请试听后再决定是否绑定，必要时换一段单人连续说话的片段"
	}
	return ""
}

func validateVoiceExtractRange(start, duration float64) error {
	if math.IsNaN(start) || math.IsInf(start, 0) || start < 0 {
		return fmt.Errorf("起始时间无效")
	}
	if math.IsNaN(duration) || duration < minVoiceExtractSeconds || duration > maxVoiceExtractSeconds {
		return fmt.Errorf("片段时长必须为%.0f–%.0f秒", minVoiceExtractSeconds, maxVoiceExtractSeconds)
	}
	return nil
}

func withTempDir(fn func(dir string) error) error {
	dir, err := os.MkdirTemp("", "voice-extract-")
	if err != nil {
		return err
	}
	defer os.RemoveAll(dir)
	return fn(dir)
}

// cutVideoAudio decodes only the selected range of the first audio stream into stereo PCM WAV.
func cutVideoAudio(source string, start, duration float64) ([]byte, error) {
	ffmpeg, err := localFFmpegPath()
	if err != nil {
		return nil, err
	}
	var data []byte
	err = withTempDir(func(dir string) error {
		out := filepath.Join(dir, "segment.wav")
		args := []string{"-hide_banner", "-loglevel", "error", "-y", "-ss", fmt.Sprintf("%.3f", start), "-i", source, "-t", fmt.Sprintf("%.3f", duration),
			"-map", "0:a:0", "-vn", "-ac", "2", "-ar", strconv.Itoa(voiceExtractSampleRate), "-c:a", "pcm_s16le", out}
		if _, runErr := runLocalProgram(ffmpeg, args, 2*time.Minute); runErr != nil {
			return fmt.Errorf("读取视频音轨失败（视频可能没有音轨）: %w", runErr)
		}
		var readErr error
		data, readErr = os.ReadFile(out)
		return readErr
	})
	if err != nil {
		return nil, err
	}
	if q, qErr := analyzeVoiceWAV(data); qErr != nil || q.Duration < minVoiceExtractSeconds-0.2 {
		return nil, fmt.Errorf("选中片段超出视频结尾或没有可用音频")
	}
	return data, nil
}

// postFilterVocals applies the reference project's speech-band filter and loudness normalization
// and writes mono 16-bit WAV, the format IndexTTS reads as a reference voice.
func postFilterVocals(input []byte, inputExt string) ([]byte, error) {
	ffmpeg, err := localFFmpegPath()
	if err != nil {
		return nil, err
	}
	var data []byte
	err = withTempDir(func(dir string) error {
		in := filepath.Join(dir, "vocals"+inputExt)
		out := filepath.Join(dir, "voice.wav")
		if writeErr := os.WriteFile(in, input, 0o644); writeErr != nil {
			return writeErr
		}
		args := []string{"-hide_banner", "-loglevel", "error", "-y", "-i", in, "-af", voiceExtractPostFilter,
			"-ar", strconv.Itoa(voiceExtractSampleRate), "-ac", "1", "-c:a", "pcm_s16le", out}
		if _, runErr := runLocalProgram(ffmpeg, args, 2*time.Minute); runErr != nil {
			return fmt.Errorf("人声后处理失败: %w", runErr)
		}
		var readErr error
		data, readErr = os.ReadFile(out)
		return readErr
	})
	return data, err
}

// StartCharacterVoiceExtraction cuts the selected Scene video range and submits Demucs separation.
// Nothing on the character changes here; the draft must be confirmed separately.
func (s *ProjectService) StartCharacterVoiceExtraction(p *models.Project, ch *models.Character, scene *models.Scene, start, duration float64) (*models.CharacterVoiceExtraction, error) {
	if err := validateVoiceExtractRange(start, duration); err != nil {
		return nil, err
	}
	if s.tasks == nil || s.upload == nil {
		return nil, fmt.Errorf("人声分离任务服务未配置")
	}
	if s.remote != nil && s.remote.Enabled() {
		return nil, fmt.Errorf("SSH远程模式暂不支持从视频提取参考语音；请导出片段后使用本地音频上传")
	}
	if scene.ProjectID != p.ID || ch.ProjectID != p.ID {
		return nil, fmt.Errorf("Scene或角色不属于当前项目")
	}
	var tpl models.Template
	if err := s.db.Where("code = ? AND enabled = ?", vocalSeparationTemplate, true).First(&tpl).Error; err != nil {
		return nil, fmt.Errorf("人声分离工作流模板未启用")
	}
	source, err := s.sceneVideoSource(p, scene)
	if err != nil {
		return nil, err
	}
	segment, err := cutVideoAudio(source, start, duration)
	if err != nil {
		return nil, err
	}
	name := fmt.Sprintf("voiceseg_%d_%d.wav", ch.ID, time.Now().UnixNano())
	if _, _, err := s.upload.SaveFile(fmt.Sprint(p.ID), "audio", name, segment); err != nil {
		return nil, err
	}
	task, err := s.tasks.CreateTask(CreateTaskReq{TemplateID: tpl.ID, Files: map[string][]FileMeta{"source_audio": {{TaskID: fmt.Sprint(p.ID), Name: name}}}, AffinityKey: fmt.Sprintf("voice-extract:p%d", p.ID)})
	if err != nil {
		return nil, err
	}
	row := models.CharacterVoiceExtraction{ProjectID: p.ID, CharacterID: ch.ID, SceneID: scene.ID, StartTime: start, Duration: duration, SourceFile: name, TaskID: task.TaskID, Status: voiceExtractStatusRunning}
	if err := s.db.Create(&row).Error; err != nil {
		_ = s.tasks.CancelTask(task.TaskID)
		return nil, err
	}
	go func(taskID string, rowID uint) {
		if executeErr := voiceExtractionExecute(s.tasks, taskID); executeErr != nil && !errors.Is(executeErr, errNoFreeGPU) {
			log.Printf("[voice extract %d] execute task %s failed: %v", rowID, taskID, executeErr)
			s.db.Model(&models.CharacterVoiceExtraction{}).Where("id = ? AND status = ?", rowID, voiceExtractStatusRunning).
				Updates(map[string]any{"status": voiceExtractStatusFailed, "error": executeErr.Error()})
		}
	}(task.TaskID, row.ID)
	return &row, nil
}

func (s *ProjectService) syncCharacterVoiceExtractions() {
	if s.tasks == nil || s.upload == nil || !s.db.Migrator().HasTable(&models.CharacterVoiceExtraction{}) {
		return
	}
	var rows []models.CharacterVoiceExtraction
	if err := s.db.Where("status = ?", voiceExtractStatusRunning).Find(&rows).Error; err != nil {
		return
	}
	for i := range rows {
		row := &rows[i]
		fail := func(msg string) {
			s.db.Model(row).Updates(map[string]any{"status": voiceExtractStatusFailed, "error": msg})
		}
		var task models.Task
		if s.db.Where("task_id = ?", row.TaskID).First(&task).Error != nil {
			continue
		}
		if task.Status == "failed" || task.Status == "cancelled" {
			msg := task.Error
			if msg == "" {
				msg = "人声分离任务已取消"
			}
			fail(msg)
			continue
		}
		if task.Status != "success" {
			continue
		}
		output, _ := resultAudioOf(&task)
		if output == "" || task.Port == nil {
			fail("人声分离任务成功但没有音频输出")
			continue
		}
		subfolder, filename := filepath.ToSlash(filepath.Dir(output)), filepath.Base(output)
		if subfolder == "." {
			subfolder = ""
		}
		raw, err := NewComfyClient(s.tasks.comfyHostForTask(&task), *task.Port).DownloadOutput(filename, subfolder, "output")
		if err != nil {
			fail(err.Error())
			continue
		}
		voice, err := postFilterVocals(raw, strings.ToLower(filepath.Ext(filename)))
		if err != nil {
			fail(err.Error())
			continue
		}
		quality, err := analyzeVoiceWAV(voice)
		if err != nil {
			fail(err.Error())
			continue
		}
		digest := sha256.Sum256(voice)
		name := fmt.Sprintf("voicedraft_%d_%s.wav", row.CharacterID, hex.EncodeToString(digest[:8]))
		if _, _, err := s.upload.SaveFile(fmt.Sprint(row.ProjectID), "audio", name, voice); err != nil {
			fail(err.Error())
			continue
		}
		s.db.Model(row).Where("status = ?", voiceExtractStatusRunning).Updates(map[string]any{
			"status": voiceExtractStatusReady, "draft_file": name, "measured_duration": math.Round(quality.Duration*1000) / 1000,
			"speech_ratio": math.Round(quality.SpeechRatio*1000) / 1000, "warning": voiceQualityWarning(quality), "error": "",
		})
		s.pushProject(nil)
	}
}

// ConfirmCharacterVoiceExtraction binds a previewed draft as the character's voice reference.
// The bound copy uses the voice_ prefix so it also appears in the custom voice library.
func (s *ProjectService) ConfirmCharacterVoiceExtraction(ch *models.Character, extractionID uint) (*models.CharacterVoiceExtraction, error) {
	var row models.CharacterVoiceExtraction
	if err := s.db.Where("id = ? AND project_id = ? AND character_id = ?", extractionID, ch.ProjectID, ch.ID).First(&row).Error; err != nil {
		return nil, fmt.Errorf("提取记录不存在")
	}
	if row.Status != voiceExtractStatusReady || row.DraftFile == "" {
		return nil, fmt.Errorf("只有已完成分离且未绑定的草稿才能确认")
	}
	data, err := s.readProjectInput(ch.ProjectID, row.DraftFile)
	if err != nil || len(data) == 0 {
		return nil, fmt.Errorf("人声草稿文件不可读")
	}
	name := fmt.Sprintf("voice_%d_%d.wav", ch.ID, time.Now().UnixNano())
	if _, _, err := s.upload.SaveFile(fmt.Sprint(ch.ProjectID), "audio", name, data); err != nil {
		return nil, err
	}
	err = s.db.Transaction(func(tx *gorm.DB) error {
		claim := tx.Model(&models.CharacterVoiceExtraction{}).Where("id = ? AND status = ?", row.ID, voiceExtractStatusReady).
			Updates(map[string]any{"status": voiceExtractStatusAccepted, "voice_file": name})
		if claim.Error != nil {
			return claim.Error
		}
		if claim.RowsAffected == 0 {
			return fmt.Errorf("该草稿已被确认，请刷新")
		}
		return tx.Model(&models.Character{}).Where("id = ? AND project_id = ?", ch.ID, ch.ProjectID).
			Updates(map[string]any{"voice_ref": name, "voice": "", "voice_id": "", "voice_model": ""}).Error
	})
	if err != nil {
		return nil, err
	}
	_ = invalidateCharacterDialogueAudio(s.db, ch.ProjectID, ch.Name, "角色参考音色已改为视频片段提取的人声")
	s.pushProject(nil)
	row.Status, row.VoiceFile = voiceExtractStatusAccepted, name
	return &row, nil
}

func (s *Service) HandleStartCharacterVoiceExtraction(c *gin.Context) {
	p, ok := s.loadProject(c)
	if !ok {
		return
	}
	ch, ok := s.loadCharacter(c)
	if !ok {
		return
	}
	var req struct {
		SceneID   uint    `json:"scene_id"`
		StartTime float64 `json:"start_time"`
		Duration  float64 `json:"duration"`
	}
	if c.ShouldBindJSON(&req) != nil || req.SceneID == 0 {
		c.JSON(400, gin.H{"error": "参数错误"})
		return
	}
	var scene models.Scene
	if err := s.DB.Where("id = ? AND project_id = ?", req.SceneID, p.ID).First(&scene).Error; err != nil {
		c.JSON(404, gin.H{"error": "Scene不存在"})
		return
	}
	row, err := s.Projects.StartCharacterVoiceExtraction(p, ch, &scene, req.StartTime, req.Duration)
	if err != nil {
		c.JSON(400, gin.H{"error": err.Error()})
		return
	}
	c.JSON(202, gin.H{"ok": true, "extraction": row, "message": "已提交人声分离；完成后请先试听，确认后才会绑定为角色参考音色"})
}

func (s *Service) HandleListCharacterVoiceExtractions(c *gin.Context) {
	ch, ok := s.loadCharacter(c)
	if !ok {
		return
	}
	rows := []models.CharacterVoiceExtraction{}
	if err := s.DB.Where("project_id = ? AND character_id = ?", ch.ProjectID, ch.ID).Order("id DESC").Limit(10).Find(&rows).Error; err != nil {
		c.JSON(500, gin.H{"error": err.Error()})
		return
	}
	c.JSON(200, rows)
}

func (s *Service) HandleConfirmCharacterVoiceExtraction(c *gin.Context) {
	ch, ok := s.loadCharacter(c)
	if !ok {
		return
	}
	id, err := strconv.ParseUint(c.Param("eid"), 10, 64)
	if err != nil || id == 0 {
		c.JSON(400, gin.H{"error": "无效的提取记录 ID"})
		return
	}
	row, err := s.Projects.ConfirmCharacterVoiceExtraction(ch, uint(id))
	if err != nil {
		c.JSON(409, gin.H{"error": err.Error()})
		return
	}
	c.JSON(200, gin.H{"ok": true, "extraction": row, "voice_ref": row.VoiceFile, "message": "已绑定为角色参考音色；该角色旧配音已标记待更新"})
}
