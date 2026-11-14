package service

import (
	"errors"
	"regexp"
	"strconv"
	"strings"

	"gorm.io/gorm"

	"comfyui-console/internal/models"
)

const (
	sceneCallEpisodeOpening = "整集首场"
	sceneCallNewScene       = "普通新场景"
	sceneCallContinuation   = "同场景续段"
)

var (
	sceneContinuationMarker = regexp.MustCompile(`[（(]\s*续\s*[0-9一二三四五六七八九十]*\s*[）)]`)
	sceneNativeSegmentTitle = regexp.MustCompile(`·\s*Native段([0-9]+)\s*$`)
)

// sceneIsContinuation recognises only explicit split markers: a 「（续N）」 title or first
// line, or Native segment 2+. A new Scene at the same location is not a continuation.
func sceneIsContinuation(sc *models.Scene) bool {
	if sceneContinuationMarker.MatchString(sc.Title) {
		return true
	}
	if match := sceneNativeSegmentTitle.FindStringSubmatch(strings.TrimSpace(sc.Title)); match != nil {
		if n, err := strconv.Atoi(match[1]); err == nil && n > 1 {
			return true
		}
	}
	for _, line := range strings.Split(sc.Content, "\n") {
		if line = strings.TrimSpace(line); line != "" {
			return sceneContinuationMarker.MatchString(line)
		}
	}
	return false
}

// sceneCallType decides whether this Scene may open with a cold-open hook. Only the
// first active Scene of an episode qualifies; continuations, later Scenes and any
// lookup failure fail closed so a hook is never repeated mid-episode.
func sceneCallType(db *gorm.DB, sc *models.Scene) (string, bool) {
	if sceneIsContinuation(sc) {
		return sceneCallContinuation, false
	}
	_, err := previousEpisodeSceneFrom(db, sc)
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return sceneCallEpisodeOpening, true
	}
	return sceneCallNewScene, false
}

// sceneCallContext is a generation-time prompt block, kept out of review hashes like the
// blocking and inherited-state blocks. The hook only reshapes camera language and pacing;
// it never adds, reorders or advances story facts.
func sceneCallContext(db *gorm.DB, sc *models.Scene) string {
	callType, golden := sceneCallType(db, sc)
	rule := "否：不得做冷开场钩子、闪前、片头式蒙太奇或重新交代环境，直接承接上一场的人物状态与动作继续。"
	switch {
	case golden:
		rule = "是：开头约3秒可用本场已有的最强视觉信息（冲突、异象、悬念道具或人物反应）直接切入，以景别、运镜和节奏抓住观众；只能调整呈现顺序与镜头语言，不得新增、提前或改写剧情事实与对白。"
	case callType == sceneCallNewScene:
		rule = "否：不得做全片式冷开场钩子或闪前；可用一个简短建立镜头交代新时空后进入本场剧情。"
	}
	return "\n\n【场景调用上下文（仅供判断开场方式，不得原样输出）】\n场景调用类型=" + callType + "\n允许黄金3秒开场=" + rule
}
