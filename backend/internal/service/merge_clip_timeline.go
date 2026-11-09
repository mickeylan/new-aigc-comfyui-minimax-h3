package service

import (
	"fmt"
	"math"

	"comfyui-console/internal/models"
)

type mergeTimelineClip struct {
	video                            mergeResolvedVideo
	outStart, sourceStart, sourceEnd float64
}

func mergeTimelineClips(videos []mergeResolvedVideo, videoDurs []sceneVideo) ([]mergeTimelineClip, float64, error) {
	if len(videos) != len(videoDurs) {
		return nil, 0, fmt.Errorf("时间线与视频片段不一致")
	}
	out := 0.0
	clips := make([]mergeTimelineClip, 0, len(videos))
	for i, v := range videos {
		start, end := 0.0, videoDurs[i].dur*mergeClipSpeed(v)
		if v.SourceDuration > 0 {
			var err error
			start, end, err = resolvedMergeClipRange(v, v.SourceDuration)
			if err != nil {
				return nil, 0, err
			}
		}
		validationVideo := v
		if validationVideo.SourceDuration <= 0 {
			validationVideo.SourceDuration = end
		}
		if err := validateMergeClipEffects(validationVideo); err != nil {
			return nil, 0, err
		}
		clips = append(clips, mergeTimelineClip{video: v, outStart: out, sourceStart: start, sourceEnd: end})
		clipDuration := (end - start) / mergeClipSpeed(v)
		out += clipDuration
		if i < len(videos)-1 {
			overlap := mergeClipTransitionOverlap(v)
			if overlap < 0 || overlap > clipDuration {
				return nil, 0, fmt.Errorf("片段转场时长无效")
			}
			out -= overlap
		}
	}
	return clips, out, nil
}

func (s *ProjectService) episodeSourceScenes(p *models.Project, mt *models.MergeTask) ([]models.Scene, map[uint]float64, error) {
	var scenes []models.Scene
	generation := mt.Generation
	if generation == 0 {
		generation = p.Generation
	}
	if err := s.db.Where("project_id = ? AND episode_n = ? AND generation = ? AND archived = ?", p.ID, mt.EpisodeN, generation, false).Order("`order`, id").Find(&scenes).Error; err != nil {
		return nil, nil, err
	}
	bases := make(map[uint]float64, len(scenes))
	cursor := 0.0
	for _, sc := range scenes {
		bases[sc.ID] = cursor
		cursor += normalizeSceneDuration(sc.Duration)
	}
	return scenes, bases, nil
}

func (s *ProjectService) buildEditedMergeDubTimeline(p *models.Project, mt *models.MergeTask, videos []mergeResolvedVideo, videoDurs []sceneVideo) ([]dubSegment, error) {
	legacy := true
	for _, v := range videos {
		if v.SourceDuration > 0 {
			legacy = false
			break
		}
	}
	if legacy {
		ids := make([]uint, len(videos))
		for i := range videos {
			ids[i] = videos[i].SceneID
		}
		segs, _, _, err := s.buildDubTimeline(p, mt.EpisodeN, ids, videoDurs)
		return segs, err
	}
	clips, _, err := mergeTimelineClips(videos, videoDurs)
	if err != nil {
		return nil, err
	}
	var result []dubSegment
	for _, c := range clips {
		if c.video.SceneID == 0 {
			continue
		}
		base, _, _, err := s.buildDubTimeline(p, mt.EpisodeN, []uint{c.video.SceneID}, []sceneVideo{{dur: c.video.SourceDuration}})
		if err != nil {
			return nil, err
		}
		for _, seg := range base {
			a, b := math.Max(seg.Start, c.sourceStart), math.Min(seg.End, c.sourceEnd)
			if b <= a {
				continue
			}
			originalSegmentSpeed := seg.Speed
			seg.AudioSourceStart += (a - seg.Start) * originalSegmentSpeed
			seg.SourceDur = (b - a) * originalSegmentSpeed
			clipSpeed := mergeClipSpeed(c.video)
			seg.Start, seg.End = c.outStart+(a-c.sourceStart)/clipSpeed, c.outStart+(b-c.sourceStart)/clipSpeed
			seg.Speed *= clipSpeed
			result = append(result, seg)
		}
	}
	return result, nil
}

func mergeHasSceneClips(videos []mergeResolvedVideo) bool {
	for _, v := range videos {
		if v.SceneID != 0 {
			return true
		}
	}
	return false
}

func (s *ProjectService) mapMergeAudioLayers(p *models.Project, mt *models.MergeTask, videos []mergeResolvedVideo, layers []models.AudioLayer) ([]models.AudioLayer, error) {
	if !mergeHasSceneClips(videos) {
		return layers, nil
	}
	_, bases, err := s.episodeSourceScenes(p, mt)
	if err != nil {
		return nil, err
	}
	clips, _, err := mergeTimelineClips(videos, makeVideoDurations(videos))
	if err != nil {
		return nil, err
	}
	var out []models.AudioLayer
	for _, layer := range layers {
		for _, c := range clips {
			if c.video.SceneID == 0 {
				continue
			}
			base, ok := bases[c.video.SceneID]
			if !ok {
				continue
			}
			a, b := layer.StartTime-base, layer.EndTime-base
			if layer.SceneID != nil && *layer.SceneID != c.video.SceneID {
				continue
			}
			x, y := math.Max(a, c.sourceStart), math.Min(b, c.sourceEnd)
			if y <= x {
				continue
			}
			copy := layer
			speed := mergeClipSpeed(c.video)
			copy.StartTime = c.outStart + (x-c.sourceStart)/speed
			copy.EndTime = c.outStart + (y-c.sourceStart)/speed
			copy.SourceStart += x - a
			copy.RenderSpeed = speed
			// A clipped occurrence must not restart an envelope which began before it.
			copy.FadeIn = math.Min(copy.EndTime-copy.StartTime, math.Max(0, (layer.FadeIn-(x-a))/speed))
			copy.FadeOut = math.Min(copy.EndTime-copy.StartTime, math.Max(0, (layer.FadeOut-(b-y))/speed))
			out = append(out, copy)
		}
	}
	return out, nil
}

func makeVideoDurations(videos []mergeResolvedVideo) []sceneVideo {
	d := make([]sceneVideo, len(videos))
	for i, v := range videos {
		a, b, e := resolvedMergeClipRange(v, v.SourceDuration)
		if e == nil {
			d[i].dur = (b - a) / mergeClipSpeed(v)
		}
	}
	return d
}

func (s *ProjectService) mapMergeScreenTextCues(p *models.Project, mt *models.MergeTask, videos []mergeResolvedVideo, cues []models.ScreenTextCue) ([]models.ScreenTextCue, error) {
	if !mergeHasSceneClips(videos) {
		return cues, nil
	}
	_, bases, err := s.episodeSourceScenes(p, mt)
	if err != nil {
		return nil, err
	}
	clips, _, err := mergeTimelineClips(videos, makeVideoDurations(videos))
	if err != nil {
		return nil, err
	}
	var out []models.ScreenTextCue
	for _, cue := range cues {
		for _, c := range clips {
			if c.video.SceneID == 0 {
				continue
			}
			base, ok := bases[c.video.SceneID]
			if !ok {
				continue
			}
			a, b := cue.StartTime-base, cue.EndTime-base
			if cue.SceneID != nil {
				if *cue.SceneID != c.video.SceneID {
					continue
				}
				a, b = cue.StartTime, cue.EndTime
			}
			x, y := math.Max(a, c.sourceStart), math.Min(b, c.sourceEnd)
			if y <= x {
				continue
			}
			copy := cue
			speed := mergeClipSpeed(c.video)
			copy.StartTime = c.outStart + (x-c.sourceStart)/speed
			copy.EndTime = c.outStart + (y-c.sourceStart)/speed
			copy.SceneID = nil // Already remapped to absolute output time.
			out = append(out, copy)
		}
	}
	return out, nil
}
