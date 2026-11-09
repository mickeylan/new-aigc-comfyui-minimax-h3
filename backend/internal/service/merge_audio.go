package service

import (
	"encoding/json"
	"fmt"
	"log"
	"math"
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
	Index          int
	Start          float64
	SourceStart    float64
	Duration       float64
	Volume         float64
	FadeIn         float64
	FadeOut        float64
	Loop           bool
	Kind           string
	SourceDuration float64
	Speed          float64
	Pitch          float64
	DuckWindows    [][2]float64
	VolumeEnvelope []clipVolumePoint
}

type mergeAudioGraph struct {
	Filters string
	Label   string
}

func buildNormalizedVideoGraph(videoCount, width, height int) []string {
	if videoCount <= 0 || width <= 0 || height <= 0 {
		return nil
	}
	filters := make([]string, 0, videoCount+2)
	var concat strings.Builder
	for i := 0; i < videoCount; i++ {
		label := fmt.Sprintf("vnorm%d", i)
		filters = append(filters, fmt.Sprintf("[%d:v]scale=%d:%d:force_original_aspect_ratio=decrease,pad=%d:%d:(ow-iw)/2:(oh-ih)/2,setsar=1[%s]", i, width, height, width, height, label))
		fmt.Fprintf(&concat, "[%s]", label)
	}
	filters = append(filters, concat.String()+fmt.Sprintf("concat=n=%d:v=1:a=0[vc]", videoCount), "[vc]fps=24[v]")
	return filters
}

// buildEditedVideoGraph trims each source before normalization and concatenation.
func buildEditedVideoGraph(videos []mergeResolvedVideo, width, height int) []string {
	if len(videos) == 0 || width <= 0 || height <= 0 {
		return nil
	}
	filters := make([]string, 0, len(videos)*2+2)
	legacy := true
	for _, v := range videos {
		if len(v.VisualTracks) > 0 || v.Transform != nil || v.Speed != 0 || v.Brightness != 0 || v.Contrast != 0 || v.Saturation != nil || mergeClipTransitionOverlap(v) != 0 || v.OverlayMaterialID != nil {
			legacy = false
		}
	}
	var concat strings.Builder
	for i, video := range videos {
		label := fmt.Sprintf("vnorm%d", i)
		baseLabel := label
		if len(video.VisualTracks) > 0 || video.OverlayMaterialID != nil {
			baseLabel = fmt.Sprintf("vbase%d", i)
		}
		chain := fmt.Sprintf("[%d:v]trim=start=%s", i, ffnum(video.SourceStart))
		if video.SourceEnd != nil {
			chain += ":end=" + ffnum(*video.SourceEnd)
		}
		chain += ",setpts=(PTS-STARTPTS)/" + ffnum(mergeClipSpeed(video)) + ",fps=24,settb=AVTB"
		if video.Transform != nil {
			input := fmt.Sprintf("transformInput%d", i)
			filters = append(filters, chain+"["+input+"]")
			var transformed string
			filters, transformed = appendClipTransform(filters, input, video.Transform, width, height, i)
			chain = "[" + transformed + "]null"
		} else {
			chain += fmt.Sprintf(",scale=%d:%d:force_original_aspect_ratio=decrease,pad=%d:%d:(ow-iw)/2:(oh-ih)/2,setsar=1", width, height, width, height)
		}
		contrast := video.Contrast
		if contrast == 0 {
			contrast = 1
		}
		sat := 1.0
		if video.Saturation != nil {
			sat = *video.Saturation
		}
		if video.Brightness != 0 || contrast != 1 || sat != 1 {
			chain += fmt.Sprintf(",eq=brightness=%s:contrast=%s:saturation=%s", ffnum(video.Brightness), ffnum(contrast), ffnum(sat))
		}
		outputDuration, _ := mergeClipOutputDuration(video)
		if video.FadeIn > 0 {
			fade := math.Min(video.FadeIn, outputDuration)
			chain += fmt.Sprintf(",fade=t=in:st=0:d=%s", ffnum(fade))
		}
		if video.FadeOut > 0 {
			fade := math.Min(video.FadeOut, outputDuration)
			chain += fmt.Sprintf(",fade=t=out:st=%s:d=%s", ffnum(outputDuration-fade), ffnum(fade))
		}
		chain += "[" + baseLabel + "]"
		filters = append(filters, chain)
		filters, label = appendMergeOverlay(filters, baseLabel, video, width, i)
		filters, label = appendClipVisualTracks(filters, label, video, width, i)
		// Downstream transition and concat paths expect a stable vnorm label.
		if label != fmt.Sprintf("vnorm%d", i) {
			filters = append(filters, fmt.Sprintf("[%s]null[vnorm%d]", label, i))
			label = fmt.Sprintf("vnorm%d", i)
		}
		fmt.Fprintf(&concat, "[%s]", label)
	}
	if legacy {
		filters[len(filters)-1] = strings.Replace(filters[len(filters)-1], ",setpts=(PTS-STARTPTS)/1.000,fps=24,settb=AVTB", ",setpts=PTS-STARTPTS", 1)
		for i := 0; i < len(filters)-1; i++ {
			filters[i] = strings.Replace(filters[i], ",setpts=(PTS-STARTPTS)/1.000,fps=24,settb=AVTB", ",setpts=PTS-STARTPTS", 1)
		}
		filters = append(filters, concat.String()+fmt.Sprintf("concat=n=%d:v=1:a=0[vc]", len(videos)), "[vc]fps=24[v]")
		return filters
	}
	current := "vnorm0"
	accumulated, _ := mergeClipOutputDuration(videos[0])
	for i := 1; i < len(videos); i++ {
		overlap := mergeClipTransitionOverlap(videos[i-1])
		out := fmt.Sprintf("vchain%d", i)
		if overlap > 0 {
			filters = append(filters, fmt.Sprintf("[%s][vnorm%d]xfade=transition=%s:duration=%s:offset=%s[%s]", current, i, videos[i-1].Transition, ffnum(overlap), ffnum(accumulated-overlap), out))
			accumulated -= overlap
		} else {
			filters = append(filters, fmt.Sprintf("[%s][vnorm%d]concat=n=2:v=1:a=0[%s]", current, i, out))
		}
		d, _ := mergeClipOutputDuration(videos[i])
		accumulated += d
		current = out
	}
	filters = append(filters, fmt.Sprintf("[%s]null[vc]", current), "[vc]fps=24[v]")
	return filters
}

func applyMergeTextFilters(filters []string, textFilters []string) []string {
	if len(textFilters) == 0 || len(filters) == 0 {
		return filters
	}
	filters[len(filters)-1] = "[vc]fps=24," + textFilters[0] + "[vtext0]"
	for i := 1; i < len(textFilters); i++ {
		filters = append(filters, fmt.Sprintf("[vtext%d]%s[vtext%d]", i-1, textFilters[i], i))
	}
	return append(filters, fmt.Sprintf("[vtext%d]null[v]", len(textFilters)-1))
}

func ffnum(value float64) string {
	return strconv.FormatFloat(value, 'f', 3, 64)
}

// atempoChain keeps each FFmpeg atempo stage inside its supported 0.5..2 range.
func atempoChain(value float64) string {
	var out strings.Builder
	for value > 2 {
		out.WriteString(",atempo=2.000")
		value /= 2
	}
	for value < 0.5 {
		out.WriteString(",atempo=0.500")
		value /= 0.5
	}
	if math.Abs(value-1) > 0.0001 {
		out.WriteString(",atempo=" + ffnum(value))
	}
	return out.String()
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
		sourceDuration := duration
		if input.SourceDuration > 0 {
			sourceDuration = input.SourceDuration
		}
		sourceStart := ffnum(input.SourceStart)
		if input.SourceStart == 0 {
			sourceStart = "0"
		}
		chain := fmt.Sprintf("[%d:a]atrim=start=%s:end=%s,asetpts=PTS-STARTPTS,aresample=48000,aformat=sample_fmts=fltp:channel_layouts=stereo", input.Index, sourceStart, ffnum(input.SourceStart+sourceDuration))
		if input.Pitch != 0 {
			factor := math.Pow(2, input.Pitch/12)
			chain += fmt.Sprintf(",asetrate=%s,aresample=48000", ffnum(48000*factor))
			chain += atempoChain(1 / factor)
		}
		if input.Speed > 0 && math.Abs(input.Speed-1) > 0.0001 {
			chain += atempoChain(input.Speed)
		}
		chain += ",volume=" + ffnum(input.Volume) + clipEnvelopeFilter(input.VolumeEnvelope)
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
		chain += fmt.Sprintf(",adelay=%d:all=1", int64(input.Start*1000+0.5))
		for _, window := range input.DuckWindows {
			if window[1] > window[0] {
				chain += fmt.Sprintf(",volume=0.35:enable='between(t,%s,%s)'", ffnum(window[0]), ffnum(window[1]))
			}
		}
		chain += fmt.Sprintf(",apad,atrim=duration=%s[am_extra_%d]", ffnum(totalDuration), i)
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

// buildEditedNativeAudioGraph preserves native sound from clips that have it and
// substitutes exact-length silence for clips that do not.
func buildEditedNativeAudioGraph(videos []mergeResolvedVideo, hasAudio []bool, totalDuration, gain float64) mergeAudioGraph {
	if len(videos) == 0 || len(hasAudio) != len(videos) || totalDuration <= 0 {
		return mergeAudioGraph{}
	}
	filters := make([]string, 0, len(videos)+1)
	var concat strings.Builder
	for i, video := range videos {
		start, end, err := resolvedMergeClipRange(video, video.SourceDuration)
		if err != nil {
			return mergeAudioGraph{}
		}
		duration := end - start
		label := fmt.Sprintf("am_native_clip_%d", i)
		outputDuration := duration / mergeClipSpeed(video)
		volume := 1.0
		if video.Volume != nil {
			volume = *video.Volume
		}
		commonFX := ",volume=" + ffnum(volume) + clipEnvelopeFilter(video.VolumeEnvelope)
		if video.FadeIn > 0 {
			commonFX += fmt.Sprintf(",afade=t=in:st=0:d=%s", ffnum(video.FadeIn))
		}
		if video.FadeOut > 0 {
			commonFX += fmt.Sprintf(",afade=t=out:st=%s:d=%s", ffnum(outputDuration-video.FadeOut), ffnum(video.FadeOut))
		}
		if hasAudio[i] {
			filters = append(filters, fmt.Sprintf("[%d:a]atrim=start=%s:end=%s,asetpts=PTS-STARTPTS,aresample=48000,aformat=sample_fmts=fltp:channel_layouts=stereo%s%s,apad,atrim=duration=%s[%s]", i, ffnum(start), ffnum(end), atempoChain(mergeClipSpeed(video)), commonFX, ffnum(outputDuration), label))
		} else {
			// Silence is already generated at output duration; applying atempo here
			// would shorten it a second time for sped-up clips.
			filters = append(filters, fmt.Sprintf("anullsrc=r=48000:cl=stereo,atrim=duration=%s,asetpts=PTS-STARTPTS%s[%s]", ffnum(outputDuration), commonFX, label))
		}
		fmt.Fprintf(&concat, "[%s]", label)
	}
	hasTransition := false
	for _, v := range videos {
		hasTransition = hasTransition || mergeClipTransitionOverlap(v) > 0
	}
	if !hasTransition {
		filters = append(filters, concat.String()+fmt.Sprintf("concat=n=%d:v=0:a=1,volume=%s,atrim=duration=%s[am_native_edited]", len(videos), ffnum(gain), ffnum(totalDuration)))
	} else {
		current := "am_native_clip_0"
		for i := 1; i < len(videos); i++ {
			label := fmt.Sprintf("am_cross_%d", i)
			if overlap := mergeClipTransitionOverlap(videos[i-1]); overlap > 0 {
				filters = append(filters, fmt.Sprintf("[%s][am_native_clip_%d]acrossfade=d=%s:c1=tri:c2=tri[%s]", current, i, ffnum(overlap), label))
			} else {
				filters = append(filters, fmt.Sprintf("[%s][am_native_clip_%d]concat=n=2:v=0:a=1[%s]", current, i, label))
			}
			current = label
		}
		filters = append(filters, fmt.Sprintf("[%s]volume=%s,atrim=duration=%s[am_native_edited]", current, ffnum(gain), ffnum(totalDuration)))
	}
	return mergeAudioGraph{Filters: strings.Join(filters, ";"), Label: "[am_native_edited]"}
}

type mergeEditClip struct {
	ID                 string            `json:"id"`
	SceneID            *uint             `json:"scene_id,omitempty"`
	MaterialID         *uint             `json:"material_id,omitempty"`
	Label              string            `json:"label"`
	SourceStart        float64           `json:"source_start,omitempty"`
	SourceEnd          *float64          `json:"source_end,omitempty"`
	Speed              float64           `json:"speed,omitempty"`
	Volume             *float64          `json:"volume,omitempty"`
	FadeIn             float64           `json:"fade_in,omitempty"`
	FadeOut            float64           `json:"fade_out,omitempty"`
	Transition         string            `json:"transition,omitempty"`
	TransitionDuration float64           `json:"transition_duration,omitempty"`
	Brightness         float64           `json:"brightness,omitempty"`
	Contrast           float64           `json:"contrast,omitempty"`
	Saturation         *float64          `json:"saturation,omitempty"`
	OverlayMaterialID  *uint             `json:"overlay_material_id,omitempty"`
	OverlayX           float64           `json:"overlay_x,omitempty"`
	OverlayY           float64           `json:"overlay_y,omitempty"`
	OverlayWidth       float64           `json:"overlay_width,omitempty"`
	OverlayOpacity     float64           `json:"overlay_opacity,omitempty"`
	Title              string            `json:"title,omitempty"`
	TitlePosition      string            `json:"title_position,omitempty"`
	TitleSize          int               `json:"title_size,omitempty"`
	Transform          *clipTransform    `json:"transform,omitempty"`
	VisualTracks       []clipVisualTrack `json:"visual_tracks,omitempty"`
	VolumeEnvelope     []clipVolumePoint `json:"volume_envelope,omitempty"`
	Markers            []clipMarker      `json:"markers,omitempty"`
}

func validateMergeEditSequence(clips []mergeEditClip) ([]uint, error) {
	if len(clips) < 2 {
		return nil, fmt.Errorf("最终成片至少需要2个视频片段")
	}
	sceneIDs, seenScenes := make([]uint, 0), map[uint]bool{}
	for index, clip := range clips {
		if (clip.SceneID == nil) == (clip.MaterialID == nil) {
			return nil, fmt.Errorf("第%d个片段必须且只能选择Scene或额外视频", index+1)
		}
		if (clip.SceneID != nil && *clip.SceneID == 0) || (clip.MaterialID != nil && *clip.MaterialID == 0) {
			return nil, fmt.Errorf("第%d个片段来源ID无效", index+1)
		}
		if math.IsNaN(clip.SourceStart) || math.IsInf(clip.SourceStart, 0) || clip.SourceStart < 0 {
			return nil, fmt.Errorf("第%d个片段裁剪起点无效", index+1)
		}
		if clip.SourceEnd != nil && (math.IsNaN(*clip.SourceEnd) || math.IsInf(*clip.SourceEnd, 0) || *clip.SourceEnd <= clip.SourceStart) {
			return nil, fmt.Errorf("第%d个片段裁剪终点无效", index+1)
		}
		if clip.SceneID != nil && !seenScenes[*clip.SceneID] {
			seenScenes[*clip.SceneID] = true
			sceneIDs = append(sceneIDs, *clip.SceneID)
		}
	}
	return sceneIDs, nil
}

// HandleCreateAudioMerge is the opt-in mix-aware endpoint. The original /merge endpoint and its
// byte-compatible default path are intentionally left unchanged for legacy callers.
func (s *Service) HandleCreateAudioMerge(c *gin.Context) {
	p, ok := s.loadProject(c)
	if !ok {
		return
	}
	var req struct {
		SceneIDs       []uint          `json:"scene_ids"`
		Dub            bool            `json:"dub"`
		Subtitles      bool            `json:"subtitles"`
		ScreenText     bool            `json:"screen_text"`
		NativeVolume   *float64        `json:"native_volume"`
		DialogueVolume *float64        `json:"dialogue_volume"`
		BGMVolume      *float64        `json:"bgm_volume"`
		EditSequence   []mergeEditClip `json:"edit_sequence"`
		EpisodeN       int             `json:"episode_n"`
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
	options.ScreenText = req.ScreenText
	if len(req.EditSequence) > 0 {
		var sequenceSceneIDs []uint
		sequenceSceneIDs, err = validateMergeEditSequence(req.EditSequence)
		if err != nil {
			c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
			return
		}
		req.SceneIDs = sequenceSceneIDs
	}
	mt, err := s.Projects.createAudioMergeTask(p, req.SceneIDs, req.EditSequence, req.EpisodeN, req.Dub, req.Subtitles, options)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, mt)
}

func (s *ProjectService) createAudioMergeTask(p *models.Project, sceneIDs []uint, editSequence []mergeEditClip, episodeN int, dub, subtitles bool, audio MergeAudioOptions) (*models.MergeTask, error) {
	mt, scenes, err := s.createMergeTaskRecord(p, sceneIDs, dub, subtitles, audio, mergeTaskSequence{Clips: editSequence, EpisodeN: episodeN})
	if err != nil {
		return nil, err
	}
	if err := s.validateMergeExecution(p, mt, scenes); err != nil {
		result := s.db.Where("id = ? AND project_id = ? AND status = ?", mt.ID, mt.ProjectID, "pending").Delete(&models.MergeTask{})
		if result.Error != nil || result.RowsAffected != 1 {
			return nil, fmt.Errorf("合并预检失败且任务清理失败")
		}
		return nil, err
	}
	if err := s.db.Model(&models.MergeTask{}).Where("id = ? AND project_id = ? AND status = ?", mt.ID, mt.ProjectID, "pending").Update("preflight_warnings", mt.PreflightWarnings).Error; err != nil {
		// Never leave an unstartable pending task when persisting preflight output fails.
		s.db.Where("id = ? AND project_id = ? AND status = ?", mt.ID, mt.ProjectID, "pending").Delete(&models.MergeTask{})
		return nil, err
	}
	go s.runAudioMerge(p, mt, scenes, dub, subtitles)
	s.pushProject(p)
	return mt, nil
}

func (s *ProjectService) audioMixMediaPath(projectID uint, file string) (string, error) {
	clean := filepath.ToSlash(filepath.Clean(strings.TrimSpace(file)))
	if clean == "." || filepath.IsAbs(clean) || strings.HasPrefix(clean, "../") || strings.Contains(clean, "/../") {
		return "", fmt.Errorf("音频层文件路径无效")
	}
	projectPrefix := fmt.Sprintf("material_p%d/", projectID)
	if s.upload != nil && (s.remote == nil || !s.remote.Enabled()) {
		if strings.HasPrefix(clean, projectPrefix) {
			return filepath.Join(s.upload.InputDir(), filepath.FromSlash(clean)), nil
		}
		return filepath.Join(s.upload.InputDir(), fmt.Sprint(projectID), filepath.FromSlash(clean)), nil
	}
	if strings.HasPrefix(clean, projectPrefix) {
		return s.mediaPath("input", filepath.FromSlash(clean)), nil
	}
	return s.mediaPath("input", strconv.FormatUint(uint64(projectID), 10), filepath.FromSlash(clean)), nil
}

type mergeResolvedVideo struct {
	SceneID                                          uint
	Order                                            int
	Path                                             string
	Label                                            string
	SourceStart                                      float64
	SourceEnd                                        *float64
	SourceDuration                                   float64
	Speed                                            float64
	Volume                                           *float64
	FadeIn, FadeOut                                  float64
	Transition                                       string
	TransitionDuration                               float64
	Brightness, Contrast                             float64
	Saturation                                       *float64
	OverlayMaterialID                                *uint
	OverlayX, OverlayY, OverlayWidth, OverlayOpacity float64
	OverlayInputIndex                                int
	OverlayIsImage                                   bool
	Title, TitlePosition                             string
	TitleSize                                        int
	Transform                                        *clipTransform
	VisualTracks                                     []clipVisualTrack
	VolumeEnvelope                                   []clipVolumePoint
	Markers                                          []clipMarker
}

func (s *ProjectService) resolveMergeVideos(p *models.Project, mt *models.MergeTask, scenes []models.Scene) ([]mergeResolvedVideo, error) {
	byID := make(map[uint]models.Scene, len(scenes))
	for _, scene := range scenes {
		byID[scene.ID] = scene
	}
	clips := []mergeEditClip{}
	if strings.TrimSpace(mt.EditSequence) != "" {
		if err := json.Unmarshal([]byte(mt.EditSequence), &clips); err != nil {
			return nil, fmt.Errorf("成片编排快照损坏: %w", err)
		}
	}
	if len(clips) == 0 {
		for _, scene := range scenes {
			id := scene.ID
			clips = append(clips, mergeEditClip{SceneID: &id, Label: scene.Title})
		}
	}
	out := make([]mergeResolvedVideo, 0, len(clips))
	for index, clip := range clips {
		if clip.SceneID != nil {
			scene, ok := byID[*clip.SceneID]
			if !ok {
				return nil, fmt.Errorf("编排中的Scene %d已不存在", *clip.SceneID)
			}
			var abs string
			if (s.remote == nil || !s.remote.Enabled()) && scene.VideoInputFile != "" {
				clean := filepath.Clean(filepath.FromSlash(scene.VideoInputFile))
				if filepath.IsAbs(clean) || strings.HasPrefix(clean, "..") {
					return nil, fmt.Errorf("Scene %d视频路径无效", scene.ID)
				}
				abs = filepath.Join(s.upload.InputDir(), fmt.Sprint(scene.ProjectID), clean)
			} else {
				if scene.VideoGPU == nil {
					return nil, fmt.Errorf("Scene %d缺少视频GPU信息", scene.ID)
				}
				abs = s.mediaPath("output_workers", fmt.Sprintf("gpu%d", *scene.VideoGPU), filepath.FromSlash(scene.VideoFile))
			}
			out = append(out, resolvedVideoFromClip(clip, mergeResolvedVideo{SceneID: scene.ID, Order: scene.Order, Path: abs, Label: clip.Label, SourceStart: clip.SourceStart, SourceEnd: clip.SourceEnd}))
			continue
		}
		var material models.Material
		if clip.MaterialID == nil || s.db.Where("id = ? AND project_id = ? AND type = ?", *clip.MaterialID, p.ID, "video").First(&material).Error != nil {
			return nil, fmt.Errorf("第%d个插入视频不存在或不属于项目", index+1)
		}
		clean := filepath.Clean(filepath.FromSlash(material.Path))
		if filepath.IsAbs(clean) || strings.HasPrefix(clean, "..") {
			return nil, fmt.Errorf("插入视频路径无效")
		}
		materialPath := filepath.Join(s.upload.InputDir(), clean)
		if s.remote != nil && s.remote.Enabled() {
			materialPath = s.mediaPath("input", clean)
		}
		out = append(out, resolvedVideoFromClip(clip, mergeResolvedVideo{Path: materialPath, Label: clip.Label, SourceStart: clip.SourceStart, SourceEnd: clip.SourceEnd}))
	}
	return out, nil
}

func (s *ProjectService) runAudioMerge(p *models.Project, mt *models.MergeTask, scenes []models.Scene, dub, subtitles bool) {
	// Rendering is driven by the persisted request, not mutable goroutine arguments.
	dub, subtitles = mt.RequestedDub, mt.RequestedSubtitles
	screenText := mt.RequestedScreenText
	claim := s.db.Model(&models.MergeTask{}).Where("id = ? AND project_id = ? AND generation = ? AND status = ?", mt.ID, mt.ProjectID, mt.Generation, "pending").Update("status", "running")
	if claim.Error != nil || claim.RowsAffected == 0 {
		return
	}
	fail := func(err error) { s.failMerge(mt, p, err.Error()) }
	currentScenes, err := s.validateMergeInputs(s.db, mt)
	if err != nil {
		fail(err)
		return
	}
	scenes = currentScenes
	videos, err := s.resolveMergeVideos(p, mt, scenes)
	if err != nil {
		fail(err)
		return
	}
	inputPaths := make([]string, 0, len(videos)+8)
	remoteInputs := make([]string, 0, len(videos)+8)
	inputLoopMode := make(map[int]string)
	videoDurs := make([]sceneVideo, 0, len(videos))
	timelineSceneIDs := make([]uint, 0, len(videos))
	nativeCount, totalDuration := 0, 0.0
	nativeAudio := make([]bool, 0, len(videos))
	firstWidth, firstHeight := 0, 0
	for index, video := range videos {
		abs := video.Path
		inputPaths = append(inputPaths, abs)
		remoteInputs = append(remoteInputs, "-i", shellQuote(abs))
		hasAudio, probeErr := s.remoteFileHasAudio(abs)
		if probeErr != nil {
			fail(probeErr)
			return
		}
		nativeAudio = append(nativeAudio, hasAudio)
		if hasAudio {
			nativeCount++
		}
		if s.remote == nil {
			fail(fmt.Errorf("媒体探测服务不可用"))
			return
		}
		info, probeErr := s.remote.ProbeMedia(abs)
		if probeErr != nil {
			fail(fmt.Errorf("探测编排片段%d失败", index+1))
			return
		}
		if info.Duration <= 0 {
			fail(fmt.Errorf("编排片段%d时长无效", index+1))
			return
		}
		if firstWidth == 0 {
			firstWidth, firstHeight = info.Width, info.Height
			if firstWidth <= 0 || firstHeight <= 0 {
				fail(fmt.Errorf("无法读取首个片段视频尺寸"))
				return
			}
		}
		_, _, rangeErr := resolvedMergeClipRange(video, info.Duration)
		if rangeErr != nil {
			fail(fmt.Errorf("编排片段%d: %w", index+1, rangeErr))
			return
		}
		videos[index].SourceDuration = info.Duration
		outputDuration, durationErr := mergeClipOutputDuration(videos[index])
		if durationErr != nil {
			fail(durationErr)
			return
		}
		videoDurs = append(videoDurs, sceneVideo{abs: abs, dur: outputDuration})
		timelineSceneIDs = append(timelineSceneIDs, video.SceneID)
		totalDuration += outputDuration
	}
	if err := validateMergeTimelineEffects(videos); err != nil {
		fail(err)
		return
	}
	for i := 0; i+1 < len(videos); i++ {
		totalDuration -= mergeClipTransitionOverlap(videos[i])
	}
	// Allocate every visual input before allocating audio inputs; indices must stay
	// identical in local and SSH ffmpeg commands.
	for i := range videos {
		for j := range videos[i].VisualTracks {
			tr := &videos[i].VisualTracks[j]
			if tr.Muted {
				continue
			}
			id := tr.MaterialID
			file, image, err := s.resolveMergeOverlay(p.ID, mergeResolvedVideo{OverlayMaterialID: &id})
			if err != nil {
				fail(err)
				return
			}
			info, err := s.remote.ProbeMedia(file)
			if err != nil || info.Width <= 0 || info.Height <= 0 || (!image && info.Duration <= 0) {
				fail(fmt.Errorf("叠加轨媒体或起点无效"))
				return
			}
			if image {
				tr.SourceStart = 0
			} else {
				tr.SourceStart = math.Mod(tr.SourceStart, info.Duration)
			}
			idx := len(inputPaths)
			tr.InputIndex = idx
			tr.IsImage = image
			inputPaths = append(inputPaths, file)
			if image {
				inputLoopMode[idx] = "image"
				remoteInputs = append(remoteInputs, "-loop", "1")
			} else {
				inputLoopMode[idx] = "video"
				remoteInputs = append(remoteInputs, "-stream_loop", "-1")
			}
			remoteInputs = append(remoteInputs, "-i", shellQuote(file))
		}
	}
	for i := range videos {
		videos[i].OverlayInputIndex = -1
		if videos[i].OverlayMaterialID == nil {
			continue
		}
		overlayPath, isImage, resolveErr := s.resolveMergeOverlay(p.ID, videos[i])
		if resolveErr != nil {
			fail(resolveErr)
			return
		}
		info, probeErr := s.remote.ProbeMedia(overlayPath)
		if probeErr != nil || info.Width <= 0 || info.Height <= 0 || (!isImage && info.Duration <= 0) {
			fail(fmt.Errorf("探测第%d个片段叠加素材失败", i+1))
			return
		}
		idx := len(inputPaths)
		videos[i].OverlayInputIndex, videos[i].OverlayIsImage = idx, isImage
		inputPaths = append(inputPaths, overlayPath)
		if isImage {
			inputLoopMode[idx] = "image"
			remoteInputs = append(remoteInputs, "-loop", "1")
		} else {
			inputLoopMode[idx] = "video"
			remoteInputs = append(remoteInputs, "-stream_loop", "-1")
		}
		remoteInputs = append(remoteInputs, "-i", shellQuote(overlayPath))
	}

	edited := strings.TrimSpace(mt.EditSequence) != ""
	extras := make([]mergeAudioInput, 0)
	var dialogueSegments []dubSegment
	if mt.DialogueMix && mt.DialogueVolume > 0 {
		segments, _, ready, err := s.buildDubTimeline(p, mt.EpisodeN, timelineSceneIDs, videoDurs)
		if edited {
			segments, err = s.buildEditedMergeDubTimeline(p, mt, videos, videoDurs)
			ready = err == nil && len(segments) > 0
		}
		if err != nil {
			fail(err)
			return
		}
		if ready {
			dialogueSegments = segments
		}
	}
	duckWindows := make([][2]float64, 0, len(dialogueSegments))
	for _, segment := range dialogueSegments {
		duckWindows = append(duckWindows, [2]float64{segment.Start, segment.End})
	}
	var layers []models.AudioLayer
	if err := s.db.Where("project_id = ? AND episode_n = ?", p.ID, mt.EpisodeN).Order("start_time, id").Find(&layers).Error; err != nil {
		fail(err)
		return
	}
	if edited {
		layers, err = s.mapMergeAudioLayers(p, mt, videos, layers)
		if err != nil {
			fail(err)
			return
		}
	}
	for _, decision := range resolveAudioLayerMix(layers) {
		if decision.Status != audioLayerMixIncluded && decision.Status != audioLayerMixPartialOverlap {
			continue
		}
		layer := decision.Layer
		abs, err := s.audioMixMediaPath(p.ID, layer.File)
		if err != nil {
			fail(err)
			return
		}
		idx := len(inputPaths)
		inputPaths = append(inputPaths, abs)
		if layer.Loop {
			inputLoopMode[idx] = "video"
			remoteInputs = append(remoteInputs, "-stream_loop", "-1")
		}
		remoteInputs = append(remoteInputs, "-i", shellQuote(abs))
		gain := layer.Volume
		if layer.Kind == "bgm" {
			gain *= mt.BGMVolume
		}
		windows := [][2]float64(nil)
		if layer.Kind == "bgm" && layer.Ducking {
			windows = duckWindows
		}
		renderSpeed := layer.RenderSpeed
		if renderSpeed <= 0 {
			renderSpeed = 1
		}
		extras = append(extras, mergeAudioInput{Index: idx, Start: layer.StartTime, SourceStart: layer.SourceStart, Duration: layer.EndTime - layer.StartTime, SourceDuration: (layer.EndTime - layer.StartTime) * renderSpeed, Speed: renderSpeed, Volume: gain, FadeIn: layer.FadeIn, FadeOut: layer.FadeOut, Loop: layer.Loop, Kind: layer.Kind, DuckWindows: windows, VolumeEnvelope: layer.VolumeEnvelope})
	}
	for _, segment := range dialogueSegments {
		if segment.AudioAbs == "" {
			continue
		}
		idx := len(inputPaths)
		inputPaths = append(inputPaths, segment.AudioAbs)
		remoteInputs = append(remoteInputs, "-i", shellQuote(segment.AudioAbs))
		extras = append(extras, mergeAudioInput{Index: idx, Start: segment.Start, SourceStart: segment.AudioSourceStart, Duration: segment.End - segment.Start, SourceDuration: segment.SourceDur, Volume: mt.DialogueVolume * segment.Volume, Speed: segment.Speed, Pitch: segment.Pitch, Kind: "dialogue"})
	}

	filters := buildNormalizedVideoGraph(len(videos), firstWidth, firstHeight)
	if edited {
		filters = buildEditedVideoGraph(videos, firstWidth, firstHeight)
	}
	outName := fmt.Sprintf("merged/%s_merged_%d.mp4", projectFileTag(p), mt.ID)
	outAbs := s.mediaPath("output_workers", "gpu0", filepath.FromSlash(outName))
	textFilters := make([]string, 0, 2)
	if subtitles {
		srtAbs := s.mediaPath("output_workers", "gpu0", filepath.FromSlash(strings.TrimSuffix(outName, ".mp4")+".srt"))
		written, err := s.writeMergeTimelineSRT(mt, srtAbs, videos, videoDurs)
		if err != nil {
			fail(err)
			return
		}
		if written {
			textFilters = append(textFilters, fmt.Sprintf("subtitles=filename=%s:force_style=FontName\\=Noto Sans SC\\,FontSize\\=28\\,MarginV\\=56", escapeFilterPath(srtAbs)))
		}
	}
	if screenText {
		var cues []models.ScreenTextCue
		selectedIDs := sceneIDsFromStrings(strings.Split(mt.SceneOrder, ","))
		if err := s.db.Where("project_id = ? AND episode_n = ? AND enabled = ? AND review_status = ? AND (scene_id IS NULL OR scene_id IN ?)", p.ID, mt.EpisodeN, true, "approved", selectedIDs).Order("start_time, `order`, id").Find(&cues).Error; err != nil {
			fail(err)
			return
		}
		if edited {
			cues, err = s.mapMergeScreenTextCues(p, mt, videos, cues)
			if err != nil {
				fail(err)
				return
			}
		}
		if len(cues) > 0 {
			registry := NewFontRegistry(s.cfg.Storage.DataDir)
			font, err := registry.Approved("noto-sans-sc")
			if err != nil {
				fail(fmt.Errorf("功能文字正式导出被阻止: %w", err))
				return
			}
			if _, err := registry.FilePath(font); err != nil {
				fail(err)
				return
			}
			families := map[string]string{"noto-sans-sc": font.Family}
			for _, cue := range cues {
				code := strings.TrimSpace(cue.FontCode)
				if code == "" {
					code = "noto-sans-sc"
				}
				if _, exists := families[code]; exists {
					continue
				}
				selected, fontErr := registry.Approved(code)
				if fontErr != nil {
					fail(fmt.Errorf("功能文字正式导出被阻止: %w", fontErr))
					return
				}
				families[code] = selected.Family
			}
			assScenes, assDurs := scenes, videoDurs
			if edited {
				assScenes = []models.Scene{{Duration: totalDuration}}
				assDurs = []sceneVideo{{dur: totalDuration}}
			}
			assData, count := buildScreenTextASS(cues, assScenes, assDurs, firstWidth, firstHeight, font.Family, families)
			if count > 0 {
				assAbs := s.mediaPath("output_workers", "gpu0", filepath.FromSlash(strings.TrimSuffix(outName, ".mp4")+".screen.ass"))
				if err := s.remote.WriteFile(assAbs, assData, 0o644); err != nil {
					fail(err)
					return
				}
				textFilters = append(textFilters, fmt.Sprintf("subtitles=filename=%s:fontsdir=%s", escapeFilterPath(assAbs), escapeFilterPath(registry.Root())))
				log.Printf("[audio merge %d] 功能文字 ASS 已生成: %s (%d 条)", mt.ID, assAbs, count)
			}
		}
	}
	if titleCues := buildMergeTitleCues(videos); len(titleCues) > 0 {
		registry := NewFontRegistry(s.cfg.Storage.DataDir)
		font, fontErr := registry.Approved("noto-sans-sc")
		if fontErr != nil {
			fail(fmt.Errorf("片段标题正式导出被阻止: %w", fontErr))
			return
		}
		if _, fontErr = registry.FilePath(font); fontErr != nil {
			fail(fontErr)
			return
		}
		assData, count := buildScreenTextASS(titleCues, []models.Scene{{Duration: totalDuration}}, []sceneVideo{{dur: totalDuration}}, firstWidth, firstHeight, font.Family, map[string]string{"noto-sans-sc": font.Family})
		if count > 0 {
			assAbs := s.mediaPath("output_workers", "gpu0", filepath.FromSlash(strings.TrimSuffix(outName, ".mp4")+".titles.ass"))
			if err := s.remote.WriteFile(assAbs, assData, 0o644); err != nil {
				fail(err)
				return
			}
			textFilters = append(textFilters, fmt.Sprintf("subtitles=filename=%s:fontsdir=%s", escapeFilterPath(assAbs), escapeFilterPath(registry.Root())))
		}
	}
	filters = applyMergeTextFilters(filters, textFilters)
	audioGraph := buildMergeAudioGraph(len(videos), dub && nativeCount == len(videos), totalDuration, mt.NativeVolume, extras)
	if edited {
		nativeGraph := mergeAudioGraph{}
		if dub {
			nativeGraph = buildEditedNativeAudioGraph(videos, nativeAudio, totalDuration, mt.NativeVolume)
		}
		extraGraph := buildMergeAudioGraph(0, false, totalDuration, 0, extras)
		switch {
		case nativeGraph.Label != "" && extraGraph.Label != "":
			filters = append(filters, nativeGraph.Filters, extraGraph.Filters, nativeGraph.Label+extraGraph.Label+fmt.Sprintf("amix=inputs=2:duration=longest:dropout_transition=0:normalize=0,atrim=duration=%s,alimiter=limit=0.95[amix_edited]", ffnum(totalDuration)))
			audioGraph = mergeAudioGraph{Label: "[amix_edited]"}
		case nativeGraph.Label != "":
			filters = append(filters, nativeGraph.Filters)
			audioGraph = nativeGraph
		case extraGraph.Label != "":
			filters = append(filters, extraGraph.Filters)
			audioGraph = extraGraph
		default:
			audioGraph = mergeAudioGraph{}
		}
	} else if audioGraph.Filters != "" {
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
			switch inputLoopMode[i] {
			case "image":
				args = append(args, "-loop", "1")
			case "video":
				args = append(args, "-stream_loop", "-1")
			}
			args = append(args, "-i", input)
		}
		args = append(args, "-filter_complex", filterComplex)
		args = append(args, mapArgs...)
		args = append(args, "-t", ffnum(totalDuration), "-c:v", "libx264", "-crf", "18", "-preset", "medium", "-pix_fmt", "yuv420p", "-movflags", "+faststart", outAbs)
		if _, err := runLocalProgram(ffmpeg, args, 30*time.Minute); err != nil {
			fail(err)
			return
		}
	} else {
		maps := make([]string, len(mapArgs))
		for i, arg := range mapArgs {
			maps[i] = shellQuote(arg)
		}
		cmd := fmt.Sprintf("%s; mkdir -p %s; $FF -y %s -filter_complex %s %s -t %s -c:v libx264 -crf 18 -preset medium -pix_fmt yuv420p -movflags +faststart %s", ffmpegResolveCmd, shellQuote(path.Dir(outAbs)), strings.Join(remoteInputs, " "), shellQuote(filterComplex), strings.Join(maps, " "), ffnum(totalDuration), shellQuote(outAbs))
		log.Printf("[audio merge %d] %s", mt.ID, cmd)
		if _, err := s.remote.RunTimeout(cmd, 30*time.Minute); err != nil {
			fail(err)
			return
		}
	}
	if err := s.completeMergeIfCurrent(mt, outName, subtitles); err != nil {
		fail(fmt.Errorf("合并结果已隔离: %w", err))
		return
	}
	s.finishMergeProject(p)
}
