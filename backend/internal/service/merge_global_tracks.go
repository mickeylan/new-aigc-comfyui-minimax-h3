package service

import (
	"encoding/json"
	"fmt"
	"strings"
)

func decodeGlobalVisualTracks(raw string) ([]clipVisualTrack, error) {
	if strings.TrimSpace(raw) == "" {
		return nil, nil
	}
	var tracks []clipVisualTrack
	if err := json.Unmarshal([]byte(raw), &tracks); err != nil {
		return nil, fmt.Errorf("全局叠加轨快照损坏")
	}
	return tracks, validateGlobalVisualTracks(tracks, 86400)
}
func validateGlobalVisualTracks(tracks []clipVisualTrack, duration float64) error {
	return validateClipTracks(mergeResolvedVideo{VisualTracks: tracks}, duration)
}

// Global overlays run after concat/xfade. Keep the final fps placeholder so
// existing ASS/subtitle filters stay on top of the complete composite.
func applyGlobalVisualTracks(filters []string, tracks []clipVisualTrack, width int) []string {
	if len(tracks) == 0 {
		return filters
	}
	filters = filters[:len(filters)-1]
	var label string
	filters, label = appendClipVisualTracks(filters, "vc", mergeResolvedVideo{VisualTracks: tracks}, width, -1)
	return append(filters, fmt.Sprintf("[%s]fps=24[v]", label))
}
