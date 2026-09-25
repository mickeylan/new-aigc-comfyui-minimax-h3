package service

import (
	"strings"
	"testing"

	"comfyui-console/internal/models"
)

const validSceneDirectorDraft = `{"shots":[{"act_type":"setup","shot_type":"中景","camera_angle":"平视","camera_movement":"固定","duration":2,"description":"女主走到桌边","dialogue":"","emotion":"警惕","transition_type":"cut","transition_note":"接视线","start_state":"女主站在门边","end_state":"女主停在桌边","prompt_subject":"女主面部清晰","prompt_action":"缓步走到桌边","prompt_camera":"中景平视构图","prompt_lighting":"室内暖侧光","prompt_style":"真人写实电影质感","negative_prompt":"水印，多余人物","checks":["动作可在2秒完成","无新增对白"]}]}`

func TestDialogueRhythmQueryQuotesReservedOrderColumn(t *testing.T) {
	db := newTestProjectService(t).db
	if err := db.AutoMigrate(&models.Dialogue{}); err != nil {
		t.Fatal(err)
	}
	project := models.Project{Title: "测试"}
	if err := db.Create(&project).Error; err != nil {
		t.Fatal(err)
	}
	scene := models.Scene{ProjectID: project.ID, EpisodeN: 1, Order: 1}
	if err := db.Create(&scene).Error; err != nil {
		t.Fatal(err)
	}
	if err := db.Create(&models.Dialogue{ProjectID: project.ID, SceneID: scene.ID, Order: 1, Character: "甲", Text: "对白"}).Error; err != nil {
		t.Fatal(err)
	}
	var rows []models.Dialogue
	if err := db.Where("scene_id = ? AND project_id = ?", scene.ID, project.ID).Order("`order` ASC, `id` ASC").Find(&rows).Error; err != nil {
		t.Fatal(err)
	}
	if len(rows) != 1 {
		t.Fatalf("dialogues=%d", len(rows))
	}
}

func TestValidateDialogueRhythmDraftPreservesDialogueExactly(t *testing.T) {
	draft, err := parseSceneDirectorDraft(`{"shots":[{"act_type":"setup","shot_type":"说话人近景","camera_angle":"平视","camera_movement":"固定","duration":8,"description":"若彤开口","dialogue":"姐姐，自从你跟太运宗使者比试之后，","emotion":"担忧","transition_type":"cut","transition_note":"切反应","start_state":"若彤停下","end_state":"若彤继续说","prompt_subject":"若彤面部清晰","prompt_action":"担忧地说话","prompt_camera":"中近景","prompt_lighting":"落日余晖","prompt_style":"真人写实","negative_prompt":"","checks":[]},{"act_type":"rising","shot_type":"听者反应","camera_angle":"平视","camera_movement":"固定","duration":9,"description":"若琳聆听，若彤画外音继续","dialogue":"这十年你都没有怎么好好闭关修炼过。","emotion":"忧虑","transition_type":"cut","transition_note":"","start_state":"若琳安静聆听","end_state":"若琳神情微变","prompt_subject":"若琳反应清晰","prompt_action":"安静聆听","prompt_camera":"反应特写","prompt_lighting":"落日余晖","prompt_style":"真人写实","negative_prompt":"","checks":[]}]}`)
	if err != nil {
		t.Fatal(err)
	}
	dialogues := []models.Dialogue{{Text: "姐姐，自从你跟太运宗使者比试之后，这十年你都没有怎么好好闭关修炼过。"}}
	if err := validateDialogueRhythmDraft(draft, dialogues); err != nil {
		t.Fatal(err)
	}
	draft.Shots[1].Dialogue = "这十年你没有闭关。"
	if err := validateDialogueRhythmDraft(draft, dialogues); err == nil {
		t.Fatal("改写对白未被拒绝")
	}
}

func TestNormalizeDialogueRhythmDraftRaisesShortShotToNativeMinimum(t *testing.T) {
	draft, err := parseSceneDirectorDraft(validSceneDirectorDraft)
	if err != nil {
		t.Fatal(err)
	}
	normalizeDialogueRhythmDraftDurations(draft)
	if draft.Shots[0].Duration != 3 {
		t.Fatalf("duration=%v, want 3", draft.Shots[0].Duration)
	}
	if err := validateDialogueRhythmDraft(draft, nil); err != nil {
		t.Fatal(err)
	}
	if len(draft.Shots[0].Checks) < 2 {
		t.Fatalf("normalization check missing: %#v", draft.Shots[0].Checks)
	}
}

func TestValidateDialogueRhythmDraftRejectsShotOverNativeMaximum(t *testing.T) {
	draft, err := parseSceneDirectorDraft(validSceneDirectorDraft)
	if err != nil {
		t.Fatal(err)
	}
	draft.Shots[0].Duration = 16
	normalizeDialogueRhythmDraftDurations(draft)
	if err := validateDialogueRhythmDraft(draft, nil); err == nil {
		t.Fatal("16秒镜头未被拒绝")
	}
}

func TestParseSceneDirectorDraftNamesMissingRequiredField(t *testing.T) {
	broken := strings.Replace(validSceneDirectorDraft, `"camera_movement":"固定"`, `"camera_movement":""`, 1)
	_, err := parseSceneDirectorDraft(broken)
	if err == nil || !strings.Contains(err.Error(), "镜头1的必填字段camera_movement为空") {
		t.Fatalf("error=%v", err)
	}
}

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
