package service

import (
	"fmt"
	"math"
)

// resolvedMergeClipRange validates a persisted edit range against the probed
// source duration. Invalid ranges are rejected rather than silently clamped.
func resolvedMergeClipRange(video mergeResolvedVideo, rawDuration float64) (start, end float64, err error) {
	if math.IsNaN(rawDuration) || math.IsInf(rawDuration, 0) || rawDuration <= 0 {
		return 0, 0, fmt.Errorf("片段源视频时长无效")
	}
	start = video.SourceStart
	end = rawDuration
	if video.SourceEnd != nil {
		end = *video.SourceEnd
	}
	if math.IsNaN(start) || math.IsInf(start, 0) || start < 0 ||
		math.IsNaN(end) || math.IsInf(end, 0) || end <= start || end > rawDuration {
		return 0, 0, fmt.Errorf("片段裁剪范围无效")
	}
	return start, end, nil
}
