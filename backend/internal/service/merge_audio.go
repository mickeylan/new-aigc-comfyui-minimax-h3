package service

import (
	"fmt"
	"log"
	"net/http"
	"os"
	"path"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"comfyui-console/internal/models"
	"github.com/gin-gonic/gin"
)

// mergeAudioInput describes an additional FFmpeg audio input after the scene videos.
type mergeAudioInput struct {
	Index    int
	Start    float64
	Duration float64
	Volume   float64
	FadeIn   float64
	FadeOut  float64
	Loop     bool
	Kind     string
}

type mergeAudioGraph struct {
	Filters string
	Label   string
}

func ffnum(value float64) string {
	return strconv.FormatFloat(value, 'f', 3, 64)
}

// buildMergeAudioGraph builds only the audio portion of filter_complex. An empty label means
// no audio should be mapped. Native audio is included only when every video has an audio stream,
// matching the legacy merge behavior.
func buildMergeAudioGraph(videoCount int, nativeReady bool, totalDuration, nativeGain float64, extras []mergeAudioInput) mergeAudioGraph {
	if totalDuration <= 0 {
		return mergeAudioGraph{}
	}
	filters := make([]string, 0, len(extras)+2)
	labels := make([]string, 0, len(extras)+1)
	if nativeReady && videoCount > 0 {
		var head strings.Builder
		for i := 0; i < videoCount; i++ {
			fmt.Fprintf(&head, "[%d:a]", i)
		}
		filters = append(filters, fmt.Sprintf("%sconcat=n=%d:v=0:a=1,volume=%s,aresample=48000,aformat=sample_fmts=fltp:channel_layouts=stereo,apad,atrim=duration=%s[am_native]", head.String(), videoCount, ffnum(nativeGain), ffnum(totalDuration)))
		labels = append(labels, "[am_native]")
	}
	for i, input := range extras {
		if input.Duration <= 0 || input.Volume <= 0 {
			continue
		}
		duration := input.Duration
		if input.Start >= totalDuration {
			continue
		}
		if input.Start+duration > totalDuration {
			duration = totalDuration - input.Start
		}
		chain := fmt.Sprintf("[%d:a]atrim=start=0:end=%s,asetpts=PTS-STARTPTS,aresample=48000,aformat=sample_fmts=fltp:channel_layouts=stereo,volume=%s", input.Index, ffnum(duration), ffnum(input.Volume))
		if input.FadeIn > 0 {
			fade := input.FadeIn
			if fade > duration {
				fade = duration
			}
			chain += fmt.Sprintf(",afade=t=in:st=0:d=%s", ffnum(fade))
		}
		if input.FadeOut > 0 {
			fade := input.FadeOut
			if fade > duration {
				fade = duration
			}
			chain += fmt.Sprintf(",afade=t=out:st=%s:d=%s", ffnum(duration-fade), ffnum(fade))
		}
		chain += fmt.Sprintf(",adelay=%d:all=1,apad,atrim=duration=%s[am_extra_%d]", int64(input.Start*1000+0.5), ffnum(totalDuration), i)
		filters = append(filters, chain)
		labels = append(labels, fmt.Sprintf("[am_extra_%d]", i))
	}
	if len(labels) == 0 {
		return mergeAudioGraph{}
	}
	if len(labels) == 1 {
		return mergeAudioGraph{Filters: strings.Join(filters, ";"), Label: labels[0]}
	}
	filters = append(filters, strings.Join(labels, "")+fmt.Sprintf("amix=inputs=%d:duration=longest:dropout_transition=0:normalize=0,atrim=duration=%s,alimiter=limit=0.95[amix]", len(labels), ffnum(totalDuration)))
	return mergeAudioGraph{Filters: strings.Join(filters, ";"), Label: "[amix]"}
}

// HandleCreateAudioMerge is the opt-in mix-aware endpoint. The original /merge endpoint and its
// byte-compatible default path are intentionally left unchanged for legacy callers.
func (s *Service) HandleCreateAudioMerge(c *gin.Context) {
	p, ok := s.loadProject(c)
	if !ok {
		return
	}
	var req struct {
		SceneIDs       []uint   `json:"scene_ids"`
		Dub            bool     `json:"dub"`
		Subtitles      bool     `json:"subtitles"`
		NativeVolume   *float64 `json:"native_volume"`
		DialogueVolume *float64 `json:"dialogue_volume"`
		BGMVolume      *float64 `json:"bgm_volume"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "参数错误"})
		return
	}
	options, err := mergeAudioOptions(req.NativeVolume, req.DialogueVolume, req.BGMVolume)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	mt, err := s.Projects.createAudioMergeTask(p, req.SceneIDs, req.Dub, req.Subtitles, options)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, mt)
}

func (s *ProjectService) createAudioMergeTask(p *models.Project, sceneIDs []uint, dub, subtitles bool, audio MergeAudioOptions) (*models.MergeTask, error) {
	if len(sceneIDs) < 2 {
		return nil, fmt.Errorf("请至少选择 2 个场景进行合并")
	}
	var active int64
	if err := s.db.Model(&models.MergeTask{}).Where("project_id = ? AND generation = ? AND status IN ?", p.ID, p.Generation, []string{"pending", "running"}).Count(&active).Error; err != nil {
		return nil, err
	}
	if active > 0 {
		return nil, fmt.Errorf("已有进行中的合并任务，请等待完成")
	}
	var scenes []models.Scene
	for _, id := range sceneIDs {
		var scene models.Scene
		if err := s.db.Where("id = ? AND project_id = ?", id, p.ID).First(&scene).Error; err != nil {
			return nil, fmt.Errorf("场景 %d 不属于该项目", id)
		}
		if scene.Status != "video_ready" || scene.VideoFile == "" || scene.VideoGPU == nil {
			return nil, fmt.Errorf("场景 %d 的视频尚未完成，请先生成视频", scene.Order)
		}
		scenes = append(scenes, scene)
	}
	ids := make([]string, len(sceneIDs))
	for i, id := range sceneIDs {
		ids[i] = strconv.FormatUint(uint64(id), 10)
	}
	episodeN := scenes[0].EpisodeN
	if episodeN <= 0 {
		episodeN = 1
	}
	mt := models.MergeTask{ProjectID: p.ID, EpisodeN: episodeN, Title: fmt.Sprintf("第%d集 · %s", episodeN, p.Title), SceneOrder: strings.Join(ids, ","), Status: "pending", Generation: p.Generation, NativeVolume: audio.NativeVolume, DialogueVolume: audio.DialogueVolume, BGMVolume: audio.BGMVolume, DialogueMix: audio.DialogueMix}
	if err := s.db.Create(&mt).Error; err != nil {
		return nil, err
	}
	go s.runAudioMerge(p, &mt, scenes, dub, subtitles)
	s.pushProject(p)
	return &mt, nil
}

func (s *ProjectService) audioMixMediaPath(projectID uint, file string) (string, error) {
	clean := filepath.ToSlash(filepath.Clean(strings.TrimSpace(file)))
	if clean == "." || filepath.IsAbs(clean) || strings.HasPrefix(clean, "../") || strings.Contains(clean, "/../") {
		return "", fmt.Errorf("音频层文件路径无效")
	}
	projectPrefix := fmt.Sprintf("material_p%d/", projectID)
	if strings.HasPrefix(clean, projectPrefix) {
		return s.mediaPath("input", filepath.FromSlash(clean)), nil
	}
	return s.mediaPath("input", strconv.FormatUint(uint64(projectID), 10), filepath.FromSlash(clean)), nil
}

func (s *ProjectService) runAudioMerge(p *models.Project, mt *models.MergeTask, scenes []models.Scene, dub, subtitles bool) {
	claim := s.db.Model(&models.MergeTask{}).Where("id = ? AND status = ?", mt.ID, "pending").Update("status", "running")
	if claim.Error != nil || claim.RowsAffected == 0 {
		return
	}
	fail := func(err error) { s.failMerge(mt, p, err.Error()) }
	inputPaths := make([]string, 0, len(scenes)+8)
	remoteInputs := make([]string, 0, len(scenes)+8)
	videoDurs := make([]sceneVideo, 0, len(scenes))
	nativeCount := 0
	totalDuration := 0.0
	for _, scene := range scenes {
		abs := s.mediaPath("output_workers", fmt.Sprintf("gpu%d", *scene.VideoGPU), filepath.FromSlash(scene.VideoFile))
		if (s.remote == nil || !s.remote.Enabled()) && scene.VideoInputFile != "" {
			abs = filepath.Join(s.upload.InputDir(), fmt.Sprint(scene.ProjectID), filepath.FromSlash(scene.VideoInputFile))
		}
		inputPaths = append(inputPaths, abs)
		remoteInputs = append(remoteInputs, "-i", shellQuote(abs))
		hasAudio, err := s.remoteFileHasAudio(abs)
		if err != nil {
			fail(err)
			return
		}
		if hasAudio {
			nativeCount++
		}
		dur := normalizeSceneDuration(scene.Duration)
		if s.remote != nil {
			if info, err := s.remote.ProbeMedia(abs); err == nil && info.Duration > 0 {
				dur = info.Duration
			}
		}
		videoDurs = append(videoDurs, sceneVideo{abs: abs, dur: dur})
		totalDuration += dur
	}

	extras := make([]mergeAudioInput, 0)
	var layers []models.AudioLayer
	if err := s.db.Where("project_id = ? AND episode_n = ? AND status = ? AND muted = ? AND file != ''", p.ID, mt.EpisodeN, "ready", false).Order("start_time, id").Find(&layers).Error; err != nil {
		fail(err)
		return
	}
	for _, layer := range layers {
		abs, err := s.audioMixMediaPath(p.ID, layer.File)
		if err != nil {
			fail(err)
			return
		}
		idx := len(inputPaths)
		inputPaths = append(inputPaths, abs)
		if layer.Loop {
			remoteInputs = append(remoteInputs, "-stream_loop", "-1")
		}
		remoteInputs = append(remoteInputs, "-i", shellQuote(abs))
		gain := layer.Volume
		if layer.Kind == "bgm" {
			gain *= mt.BGMVolume
		}
		extras = append(extras, mergeAudioInput{Index: idx, Start: layer.StartTime, Duration: layer.EndTime - layer.StartTime, Volume: gain, FadeIn: layer.FadeIn, FadeOut: layer.FadeOut, Loop: layer.Loop, Kind: layer.Kind})
	}
	if mt.DialogueMix && mt.DialogueVolume > 0 {
		segments, _, ready, err := s.buildDubTimeline(p, mt.EpisodeN, sceneIDsFromStrings(strings.Split(mt.SceneOrder, ",")), videoDurs)
		if err != nil {
			fail(err)
			return
		}
		if ready {
			for _, segment := range segments {
				if segment.AudioAbs == "" {
					continue
				}
				idx := len(inputPaths)
				inputPaths = append(inputPaths, segment.AudioAbs)
				remoteInputs = append(remoteInputs, "-i", shellQuote(segment.AudioAbs))
				extras = append(extras, mergeAudioInput{Index: idx, Start: segment.Start, Duration: segment.End - segment.Start, Volume: mt.DialogueVolume, Kind: "dialogue"})
			}
		}
	}

	var concatHead strings.Builder
	for i := range scenes {
		fmt.Fprintf(&concatHead, "[%d:v]", i)
	}
	filters := []string{concatHead.String() + fmt.Sprintf("concat=n=%d:v=1:a=0[vc]", len(scenes)), "[vc]fps=24[v]"}
	outName := fmt.Sprintf("merged/%s_merged_%d.mp4", projectFileTag(p), mt.ID)
	outAbs := s.mediaPath("output_workers", "gpu0", filepath.FromSlash(outName))
	if subtitles {
		srtAbs := s.mediaPath("output_workers", "gpu0", filepath.FromSlash(strings.TrimSuffix(outName, ".mp4")+".srt"))
		if err := s.writeMergeSRT(p, mt, srtAbs, scenes); err != nil {
			fail(err)
			return
		}
		filters[1] = fmt.Sprintf("[vc]fps=24,subtitles=filename=%s:force_style=FontName\\=Noto Sans CJK SC\\,FontSize\\=14\\,MarginV\\=28[v]", escapeFilterPath(srtAbs))
	}
	audioGraph := buildMergeAudioGraph(len(scenes), dub && nativeCount == len(scenes), totalDuration, mt.NativeVolume, extras)
	if audioGraph.Filters != "" {
		filters = append(filters, audioGraph.Filters)
	}
	filterComplex := strings.Join(filters, ";")

	mapArgs := []string{"-map", "[v]"}
	if audioGraph.Label != "" {
		mapArgs = append(mapArgs, "-map", audioGraph.Label, "-c:a", "aac", "-b:a", "192k")
	}
	if s.remote == nil || !s.remote.Enabled() {
		if err := os.MkdirAll(filepath.Dir(outAbs), 0o755); err != nil {
			fail(err)
			return
		}
		ffmpeg, err := localFFmpegPath()
		if err != nil {
			fail(err)
			return
		}
		args := []string{"-y"}
		for i, input := range inputPaths {
			loop := false
			for _, extra := range extras {
				if extra.Index == i && extra.Loop {
					loop = true
					break
				}
			}
			if loop {
				args = append(args, "-stream_loop", "-1")
			}
			args = append(args, "-i", input)
		}
		args = append(args, "-filter_complex", filterComplex)
		args = append(args, mapArgs...)
		args = append(args, "-c:v", "libx264", "-crf", "18", "-preset", "medium", "-pix_fmt", "yuv420p", "-movflags", "+faststart", outAbs)
		if _, err := runLocalProgram(ffmpeg, args, 30*time.Minute); err != nil {
			fail(err)
			return
		}
	} else {
		maps := make([]string, len(mapArgs))
		for i, arg := range mapArgs {
			maps[i] = shellQuote(arg)
		}
		cmd := fmt.Sprintf("%s; mkdir -p %s; $FF -y %s -filter_complex %s %s -c:v libx264 -crf 18 -preset medium -pix_fmt yuv420p -movflags +faststart %s", ffmpegResolveCmd, shellQuote(path.Dir(outAbs)), strings.Join(remoteInputs, " "), shellQuote(filterComplex), strings.Join(maps, " "), shellQuote(outAbs))
		log.Printf("[audio merge %d] %s", mt.ID, cmd)
		if _, err := s.remote.RunTimeout(cmd, 30*time.Minute); err != nil {
			fail(err)
			return
		}
	}
	s.db.Model(mt).Updates(map[string]any{"status": "success", "output_file": outName, "subtitle": subtitles, "error": ""})
	s.finishMergeProject(p)
}
