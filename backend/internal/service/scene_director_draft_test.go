package service

import (
	"testing"
)

const validSceneDirectorDraft = `{"shots":[{"act_type":"setup","shot_type":"中景","camera_angle":"平视","camera_movement":"固定","duration":2,"description":"女主走到桌边","dialogue":"","emotion":"警惕","transition_type":"cut","transition_note":"接视线","start_state":"女主站在门边","end_state":"女主停在桌边","prompt_subject":"女主面部清晰","prompt_action":"缓步走到桌边","prompt_camera":"中景平视构图","prompt_lighting":"室内暖侧光","prompt_style":"真人写实电影质感","negative_prompt":"水印，多余人物","checks":["动作可在2秒完成","无新增对白"]}]}`

func TestParseSceneDirectorDraftStrictAndComplete(t *testing.T) {
	draft, err := parseSceneDirectorDraft(validSceneDirectorDraft)
	if err != nil {
		t.Fatal(err)
	}
	if len(draft.Shots) != 1 || draft.Shots[0].StartState == "" || draft.Shots[0].PromptLighting == "" {
		t.Fatalf("draft incomplete: %+v", draft)
	}
	for _, bad := range []string{
		`{"shots":[{"act_type":"setup","unknown":"x"}]}`,
		validSceneDirectorDraft + " trailing",
		`{"shots":[{"act_type":"setup","shot_type":"中景","camera_angle":"平视","camera_movement":"固定","duration":2,"description":"x","dialogue":"","emotion":"","transition_type":"cut","transition_note":"","start_state":"","end_state":"x","prompt_subject":"x","prompt_action":"x","prompt_camera":"x","prompt_lighting":"x","prompt_style":"x","negative_prompt":"","checks":[]}]}`,
	} {
		if _, err := parseSceneDirectorDraft(bad); err == nil {
			t.Fatalf("invalid draft accepted: %s", bad)
		}
	}
}
