package service

import (
	"fmt"
	"path/filepath"
	"strings"

	"comfyui-console/internal/models"
)

func isReviewedAudioVideoTemplate(code string) bool {
	switch strings.TrimSpace(code) {
	case h3AudioDrivenRef2VTemplate, h3AudioDrivenT8EnhancedTemplate, h3VideoThenAudioMuxTemplate:
		return true
	default:
		return false
	}
}

type sceneReviewedAudio struct {
	Ready     bool                           `json:"ready"`
	Reason    string                         `json:"reason,omitempty"`
	Dialogue  *models.Dialogue               `json:"dialogue,omitempty"`
	Candidate *models.DialogueAudioCandidate `json:"candidate,omitempty"`
}

// sceneReviewedAudioSelection is deliberately conservative: current H3 audio templates accept
// one reviewed track. Multi-dialogue scenes must first produce an explicitly reviewed Scene mix;
// concatenating independent takes here would invent timing and bypass the production timeline.
func (s *ProjectService) sceneReviewedAudioSelection(sc *models.Scene) sceneReviewedAudio {
	if sc == nil {
		return sceneReviewedAudio{Reason: "场景不存在"}
	}
	dialogues := validSceneDialogues(s.sceneVideoDialogues(sc))
	if len(dialogues) == 0 {
		return sceneReviewedAudio{Reason: "场景没有有效结构化 Dialogue"}
	}
	if len(dialogues) != 1 {
		return sceneReviewedAudio{Reason: fmt.Sprintf("场景包含 %d 条 Dialogue；请先生成并审核场景级连续音轨，禁止自动拼接独立候选", len(dialogues))}
	}
	dialogue := dialogues[0]
	if dialogue.AudioStale || strings.TrimSpace(dialogue.AudioFile) == "" || strings.TrimSpace(dialogue.AudioHash) == "" {
		return sceneReviewedAudio{Dialogue: &dialogue, Reason: "当前 Dialogue 没有有效且未过期的生产音频"}
	}
	var candidate models.DialogueAudioCandidate
	err := s.db.Where(
		"project_id = ? AND dialogue_id = ? AND is_current = ? AND review_status = ? AND stale = ? AND input_hash = ? AND file = ?",
		sc.ProjectID, dialogue.ID, true, "accepted", false, dialogue.AudioHash, dialogue.AudioFile,
	).Order("id DESC").First(&candidate).Error
	if err != nil {
		return sceneReviewedAudio{Dialogue: &dialogue, Reason: "生产音频未对应已接受、当前且输入哈希一致的候选"}
	}
	return sceneReviewedAudio{Ready: true, Dialogue: &dialogue, Candidate: &candidate}
}

func (s *ProjectService) attachReviewedSceneAudio(sc *models.Scene, files map[string][]FileMeta) (map[string][]FileMeta, error) {
	selection := s.sceneReviewedAudioSelection(sc)
	if !selection.Ready || selection.Candidate == nil {
		return nil, fmt.Errorf("所选视频模板需要已审核音频: %s", selection.Reason)
	}
	if files == nil {
		files = map[string][]FileMeta{}
	}
	files["dialogue_audio"] = []FileMeta{{TaskID: fmt.Sprint(sc.ProjectID), Name: filepath.Base(selection.Candidate.File)}}
	return files, nil
}
