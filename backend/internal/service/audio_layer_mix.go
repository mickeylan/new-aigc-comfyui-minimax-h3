package service

import (
	"sort"
	"strings"

	"comfyui-console/internal/models"
)

const (
	audioLayerMixIncluded       = "included"
	audioLayerMixYielded        = "yielded"
	audioLayerMixPartialOverlap = "partial_overlap"
	audioLayerMixInactive       = "inactive"
)

type audioLayerMixDecision struct {
	Layer  models.AudioLayer
	Status string
}

func activeAudioLayer(layer models.AudioLayer) bool {
	return layer.Status == "ready" && !layer.Muted && !layer.Stale && strings.TrimSpace(layer.File) != "" &&
		isFinite(layer.StartTime) && isFinite(layer.EndTime) && layer.StartTime >= 0 && layer.EndTime > layer.StartTime
}

// resolveAudioLayerMix is the single priority rule used by rendering and preflight.
// A manual BGM yields only when active Music 3-derived BGM intervals cover its
// complete window. Partial overlap remains in the mix and is surfaced as a warning.
func resolveAudioLayerMix(layers []models.AudioLayer) []audioLayerMixDecision {
	cueWindows := make([][2]float64, 0)
	for _, layer := range layers {
		if activeAudioLayer(layer) && layer.Kind == "bgm" && layer.Source == audioLayerSourceMusicCue && layer.EndTime > layer.StartTime {
			cueWindows = append(cueWindows, [2]float64{layer.StartTime, layer.EndTime})
		}
	}
	sort.Slice(cueWindows, func(i, j int) bool { return cueWindows[i][0] < cueWindows[j][0] })

	out := make([]audioLayerMixDecision, 0, len(layers))
	for _, layer := range layers {
		decision := audioLayerMixDecision{Layer: layer, Status: audioLayerMixInactive}
		if activeAudioLayer(layer) {
			decision.Status = audioLayerMixIncluded
			if layer.Kind == "bgm" && (layer.Source == audioLayerSourceManual || strings.TrimSpace(layer.Source) == "") {
				coveredUntil := layer.StartTime
				overlaps := false
				for _, window := range cueWindows {
					if window[1] <= layer.StartTime || window[0] >= layer.EndTime {
						continue
					}
					overlaps = true
					if window[0] > coveredUntil {
						break
					}
					if window[1] > coveredUntil {
						coveredUntil = window[1]
					}
					if coveredUntil >= layer.EndTime {
						decision.Status = audioLayerMixYielded
						break
					}
				}
				if decision.Status != audioLayerMixYielded && overlaps {
					decision.Status = audioLayerMixPartialOverlap
				}
			}
		}
		out = append(out, decision)
	}
	return out
}
