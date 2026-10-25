package service

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"path/filepath"
	"strings"

	"comfyui-console/internal/models"
	"gorm.io/gorm"
)

const (
	music3EpisodeSegmentTemplate = "minimax_music_3_episode_segment"
	music3Provider               = "minimax_music_3_comfyui"
	maxMusic3SegmentSeconds      = 300.0
)

type music3Caption struct {
	GlobalMetadata string `json:"global_metadata"`
	VocalDetails   string `json:"vocal_details"`
	Arrangement    string `json:"arrangement"`
	LyricsInput    string `json:"lyrics_input"`
}

func compileMusic3Caption(raw string) (string, string, error) {
	var caption music3Caption
	if err := json.Unmarshal([]byte(strings.TrimSpace(raw)), &caption); err != nil {
		return "", "", fmt.Errorf("解析Music 3 Structured Caption失败: %w", err)
	}
	caption.GlobalMetadata = strings.TrimSpace(caption.GlobalMetadata)
	caption.VocalDetails = strings.TrimSpace(caption.VocalDetails)
	caption.Arrangement = strings.TrimSpace(caption.Arrangement)
	caption.LyricsInput = strings.TrimSpace(caption.LyricsInput)
	if caption.GlobalMetadata == "" || caption.VocalDetails == "" || caption.Arrangement == "" {
		return "", "", fmt.Errorf("Music 3 Caption缺少global_metadata、vocal_details或arrangement")
	}
	lower := strings.ToLower(caption.VocalDetails)
	if !strings.Contains(lower, "instrumental") {
		return "", "", fmt.Errorf("整集配乐必须明确为fully instrumental")
	}
	if !musicCaptionHasOrientalIdentity(musicStructuredCaption(caption)) {
		return "", "", fmt.Errorf("仙侠整集配乐缺少东方旋律和中国民族器乐身份，请重新生成Cue")
	}
	if musicCaptionHasForbiddenWesternStyle(musicStructuredCaption(caption)) {
		return "", "", fmt.Errorf("仙侠整集配乐含西洋预告片、EDM、摇滚、爵士或流行歌曲化指令，请重新生成Cue")
	}
	if musicCaptionHasVocalContent(musicStructuredCaption(caption)) {
		return "", "", fmt.Errorf("整集配乐包含歌词或人声意图，请重新生成Cue")
	}
	// Music 3 may interpret ordinary lyrics text as executable singing content.
	// Always submit structural tags only for an instrumental score, regardless of model prose.
	lyrics := "[Intro]\n[Instrumental]\n[Bridge]\n[Instrumental]\n[Outro]"
	compiled := "Global Metadata: " + caption.GlobalMetadata + "\n" + music3OrientalInstrumentalContract + "\n\nVocal Details: " + caption.VocalDetails + "\n" + music3OrientalInstrumentalContract + "\n\nArrangement: " + caption.Arrangement
	return compiled, lyrics, nil
}

func pendingMusicCandidateFingerprint(taskID string) string {
	digest := sha256.Sum256([]byte("pending-music3:" + strings.TrimSpace(taskID)))
	return hex.EncodeToString(digest[:])
}

func musicCueDuration(cue *models.MusicCueSheet) float64 {
	if cue.Duration > 0 {
		return cue.Duration
	}
	return cue.EndTime - cue.StartTime
}

func (s *ProjectService) StartMusic3Cue(projectID, cueID uint, seed int64) (*models.MusicCandidate, error) {
	if s.tasks == nil || s.upload == nil {
		return nil, fmt.Errorf("Music 3任务服务未配置")
	}
	var cue models.MusicCueSheet
	if err := s.db.Where("id = ? AND project_id = ?", cueID, projectID).First(&cue).Error; err != nil {
		return nil, ErrMusicCueSheetNotFound
	}
	if cue.ReviewStatus != "approved" || cue.Stale || !cue.Enabled {
		return nil, fmt.Errorf("配乐Cue必须已审核通过、启用且未过期")
	}
	duration := musicCueDuration(&cue)
	if duration < 5 || duration > maxMusic3SegmentSeconds {
		return nil, fmt.Errorf("Music 3单段时长必须为5–300秒，当前%.2f秒；更长Episode必须拆段", duration)
	}
	caption, lyrics, err := compileMusic3Caption(cue.CaptionJSON)
	if err != nil {
		return nil, err
	}
	if seed < 0 {
		seed = randomGenerationSeed()
	}
	var tpl models.Template
	if err := s.db.Where("code = ? AND enabled = ?", music3EpisodeSegmentTemplate, true).First(&tpl).Error; err != nil {
		return nil, fmt.Errorf("Music 3工作流模板未启用")
	}
	params := map[string]any{"caption": caption, "lyrics": lyrics, "duration": duration, "seed": seed}
	task, err := s.tasks.CreateTask(CreateTaskReq{TemplateID: tpl.ID, Prompt: caption, Params: params, Seed: &seed, AffinityKey: fmt.Sprintf("music3:p%d:e%d", projectID, cue.EpisodeN)})
	if err != nil {
		return nil, err
	}
	paramsJSON, _ := json.Marshal(params)
	candidate := models.MusicCandidate{ProjectID: projectID, MusicCueSheetID: cue.ID, TaskID: &task.ID, SourceVersionHash: cue.SourceVersionHash, Provider: music3Provider, Model: "MiniMax Music 3", Seed: seed, ParamsJSON: string(paramsJSON), FileSHA256: pendingMusicCandidateFingerprint(task.TaskID), ReviewStatus: "pending"}
	if err := s.db.Create(&candidate).Error; err != nil {
		_ = s.tasks.CancelTask(task.TaskID)
		return nil, err
	}
	go func(taskID string, candidateID uint) {
		if executeErr := s.tasks.Execute(taskID); executeErr != nil && !errors.Is(executeErr, errNoFreeGPU) {
			log.Printf("[music3 candidate %d] execute task %s failed: %v", candidateID, taskID, executeErr)
			s.db.Model(&models.MusicCandidate{}).Where("id = ? AND file = ''", candidateID).Update("error", executeErr.Error())
		}
	}(task.TaskID, candidate.ID)
	return &candidate, nil
}

func (s *ProjectService) syncMusic3Candidates() {
	if s.tasks == nil || s.upload == nil || !s.db.Migrator().HasTable(&models.MusicCandidate{}) {
		return
	}
	var candidates []models.MusicCandidate
	if err := s.db.Where("provider = ? AND task_id IS NOT NULL AND file = '' AND error = ''", music3Provider).Find(&candidates).Error; err != nil {
		return
	}
	for i := range candidates {
		candidate := &candidates[i]
		var task models.Task
		if s.db.First(&task, *candidate.TaskID).Error != nil {
			continue
		}
		if task.Status == "failed" || task.Status == "cancelled" {
			s.db.Model(candidate).Update("error", task.Error)
			continue
		}
		if task.Status != "success" {
			continue
		}
		output, _ := resultAudioOf(&task)
		if output == "" || task.Port == nil {
			s.db.Model(candidate).Update("error", "Music 3任务成功但没有音频输出")
			continue
		}
		subfolder, filename := filepath.ToSlash(filepath.Dir(output)), filepath.Base(output)
		if subfolder == "." {
			subfolder = ""
		}
		data, err := NewComfyClient(s.tasks.comfyHostForTask(&task), *task.Port).DownloadOutput(filename, subfolder, "output")
		if err != nil {
			s.db.Model(candidate).Update("error", err.Error())
			continue
		}
		digest := sha256.Sum256(data)
		ext := strings.ToLower(filepath.Ext(filename))
		if ext == "" {
			ext = ".flac"
		}
		storedName := fmt.Sprintf("music3_cue_%d_%s%s", candidate.MusicCueSheetID, hex.EncodeToString(digest[:8]), ext)
		stored, _, err := s.upload.SaveFile(fmt.Sprint(candidate.ProjectID), "audio", storedName, data)
		if err != nil {
			s.db.Model(candidate).Update("error", err.Error())
			continue
		}
		var cue models.MusicCueSheet
		if s.db.First(&cue, candidate.MusicCueSheetID).Error != nil || cue.SourceVersionHash != candidate.SourceVersionHash || cue.Stale {
			s.db.Model(candidate).Updates(map[string]any{"stale": true, "stale_reason": "Music 3生成期间Cue已变化"})
			continue
		}
		realHash := hex.EncodeToString(digest[:])
		_ = s.db.Transaction(func(tx *gorm.DB) error {
			var duplicate models.MusicCandidate
			err := tx.Where("id <> ? AND project_id = ? AND music_cue_sheet_id = ? AND source_version_hash = ? AND provider = ? AND file_sha256 = ?", candidate.ID, candidate.ProjectID, candidate.MusicCueSheetID, candidate.SourceVersionHash, candidate.Provider, realHash).First(&duplicate).Error
			if err == nil {
				if duplicate.File == "" {
					if updateErr := tx.Model(&duplicate).Updates(map[string]any{"file": filepath.Base(stored), "file_size": int64(len(data)), "duration": musicCueDuration(&cue), "error": ""}).Error; updateErr != nil {
						return updateErr
					}
				}
				return tx.Delete(candidate).Error
			}
			if err != gorm.ErrRecordNotFound {
				return err
			}
			return tx.Model(candidate).Updates(map[string]any{"file": filepath.Base(stored), "file_sha256": realHash, "file_size": int64(len(data)), "duration": musicCueDuration(&cue), "error": ""}).Error
		})
	}
}
