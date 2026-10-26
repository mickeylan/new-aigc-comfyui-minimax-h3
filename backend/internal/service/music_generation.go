package service

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"math"
	"path/filepath"
	"strings"

	"comfyui-console/internal/models"
	"gorm.io/gorm"
)

const (
	music3EpisodeSegmentTemplate = "minimax_music_3_episode_segment"
	music3Provider               = "minimax_music_3_comfyui_calibrated_sections_v8"
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
	if !musicCaptionHasChineseScreenIdentity(musicStructuredCaption(caption)) {
		return "", "", fmt.Errorf("整集配乐缺少明确且符合故事环境的华语屏幕音乐主方向，请重新生成Cue")
	}
	if musicCaptionHasForbiddenWesternStyle(musicStructuredCaption(caption)) {
		return "", "", fmt.Errorf("整集配乐包含与故事环境不符的好莱坞预告片或泛西方奇幻套路，请重新生成Cue")
	}
	if musicCaptionHasVocalContent(musicStructuredCaption(caption)) {
		return "", "", fmt.Errorf("整集配乐包含歌词或人声意图，请重新生成Cue")
	}
	compiled := "### Global Metadata\n" + caption.GlobalMetadata + "\n\n### Vocal Details\n" + caption.VocalDetails + "\n\n### Arrangement\n" + caption.Arrangement
	return compiled, "", nil
}

func music3InstrumentalStructure(duration float64) string {
	// max_duration is the requested target song length. The model can still emit
	// EOS before reaching that target, so provide enough executable sections to
	// sustain long-form planning. Local calibration: 18 total tags yielded 126.84
	// seconds for a 157-second target, while 7 tags yielded 57.76 seconds. Use
	// roughly one executable section per six target seconds.
	sectionCount := int(math.Ceil(duration / 6.0))
	if sectionCount < 3 {
		sectionCount = 3
	}
	sections := make([]string, 0, sectionCount+2)
	sections = append(sections, "[Intro]")
	for i := 0; i < sectionCount; i++ {
		if duration >= 90 && i == sectionCount*2/3 {
			sections = append(sections, "[Solo]")
		} else {
			sections = append(sections, "[Instrumental]")
		}
	}
	sections = append(sections, "[Outro]")
	return strings.Join(sections, "\n")
}

type musicAudioInfo struct {
	Duration   float64
	SampleRate int
	Channels   int
}

func inspectMusicFLAC(data []byte) (musicAudioInfo, error) {
	if len(data) < 8 || string(data[:4]) != "fLaC" {
		return musicAudioInfo{}, fmt.Errorf("Music 3输出不是有效FLAC")
	}
	for pos := 4; pos+4 <= len(data); {
		header := data[pos]
		blockType := header & 0x7f
		length := int(data[pos+1])<<16 | int(data[pos+2])<<8 | int(data[pos+3])
		pos += 4
		if length < 0 || pos+length > len(data) {
			return musicAudioInfo{}, fmt.Errorf("Music 3 FLAC元数据损坏")
		}
		if blockType == 0 {
			if length < 18 {
				return musicAudioInfo{}, fmt.Errorf("Music 3 FLAC缺少完整STREAMINFO")
			}
			b := data[pos+10 : pos+18]
			packed := uint64(b[0])<<56 | uint64(b[1])<<48 | uint64(b[2])<<40 | uint64(b[3])<<32 | uint64(b[4])<<24 | uint64(b[5])<<16 | uint64(b[6])<<8 | uint64(b[7])
			sampleRate := int((packed >> 44) & 0xfffff)
			channels := int((packed>>41)&0x7) + 1
			totalSamples := packed & 0xfffffffff
			if sampleRate <= 0 || channels <= 0 || totalSamples == 0 {
				return musicAudioInfo{}, fmt.Errorf("Music 3 FLAC时长信息无效")
			}
			return musicAudioInfo{Duration: float64(totalSamples) / float64(sampleRate), SampleRate: sampleRate, Channels: channels}, nil
		}
		pos += length
		if header&0x80 != 0 {
			break
		}
	}
	return musicAudioInfo{}, fmt.Errorf("Music 3 FLAC缺少STREAMINFO")
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
	if !strings.HasPrefix(cue.SourceVersionHash, "music3-calibrated-sections-duration-v10:") {
		return nil, fmt.Errorf("配乐Cue来自旧Music 3策略；请重新生成AI配乐方案，新策略遵循官方Caption结构，并按实测约每6秒一个器乐段落校准长曲结构，再分离Vocals和校验真实音频时长")
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
	// max_duration is the target song length. Official section tags provide the
	// autoregressive planner with enough long-form instrumental structure to reduce
	// premature EOS. Any invented vocals are removed by downstream Demucs.
	lyrics = music3InstrumentalStructure(duration)
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
		info, inspectErr := inspectMusicFLAC(data)
		if inspectErr != nil {
			s.db.Model(candidate).Updates(map[string]any{"error": inspectErr.Error(), "stale": true, "stale_reason": "Music 3输出媒体无效"})
			continue
		}
		// Archive every structurally valid generated file before checking whether it
		// fully covers the planned Cue. A short take may still be musically useful and
		// must remain playable/downloadable even though it cannot be adopted as the
		// complete episode segment.
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
		realHash := hex.EncodeToString(digest[:])
		archived := map[string]any{
			"file": filepath.Base(stored), "file_sha256": realHash,
			"file_size": int64(len(data)), "duration": info.Duration,
			"sample_rate": info.SampleRate, "channels": info.Channels,
		}
		var cue models.MusicCueSheet
		if s.db.First(&cue, candidate.MusicCueSheetID).Error != nil || cue.SourceVersionHash != candidate.SourceVersionHash || cue.Stale {
			archived["stale"] = true
			archived["stale_reason"] = "Music 3生成期间Cue已变化；音频已归档，可试听或下载，但不能作为当前Cue采用"
			archived["error"] = ""
			s.db.Model(candidate).Updates(archived)
			continue
		}
		// Preserve the measured duration even when the model ends before the requested
		// target. Duration differences remain visible but do not block review/adoption;
		// episode music arrangement handles the uncovered timeline later.
		_ = s.db.Transaction(func(tx *gorm.DB) error {
			var duplicate models.MusicCandidate
			err := tx.Where("id <> ? AND project_id = ? AND music_cue_sheet_id = ? AND source_version_hash = ? AND provider = ? AND file_sha256 = ?", candidate.ID, candidate.ProjectID, candidate.MusicCueSheetID, candidate.SourceVersionHash, candidate.Provider, realHash).First(&duplicate).Error
			if err == nil {
				if duplicate.File == "" {
					if updateErr := tx.Model(&duplicate).Updates(map[string]any{"file": filepath.Base(stored), "file_size": int64(len(data)), "duration": info.Duration, "sample_rate": info.SampleRate, "channels": info.Channels, "error": ""}).Error; updateErr != nil {
						return updateErr
					}
				}
				return tx.Delete(candidate).Error
			}
			if err != gorm.ErrRecordNotFound {
				return err
			}
			return tx.Model(candidate).Updates(map[string]any{"file": filepath.Base(stored), "file_sha256": realHash, "file_size": int64(len(data)), "duration": info.Duration, "sample_rate": info.SampleRate, "channels": info.Channels, "error": ""}).Error
		})
	}
}
