package service

import (
	"math"
	"strings"
	"testing"

	"comfyui-console/internal/models"
)

const validSceneDirectorDraft = `{"shots":[{"act_type":"setup","shot_type":"中景","camera_angle":"平视","camera_movement":"固定","duration":3,"description":"女主走到桌边","dialogue":"","emotion":"警惕","transition_type":"cut","transition_note":"接视线","start_state":"女主站在门边","end_state":"女主停在桌边","prompt_subject":"女主面部清晰","prompt_action":"缓步走到桌边","prompt_camera":"中景平视构图","prompt_lighting":"室内暖侧光","prompt_style":"真人写实电影质感","negative_prompt":"水印，多余人物","checks":["动作可在3秒完成","无新增对白"]}]}`

const shortSceneDirectorDraft = `{"shots":[{"act_type":"setup","shot_type":"中景","camera_angle":"平视","camera_movement":"固定","duration":2,"description":"女主走到桌边","dialogue":"","emotion":"警惕","transition_type":"cut","transition_note":"接视线","start_state":"女主站在门边","end_state":"女主停在桌边","prompt_subject":"女主面部清晰","prompt_action":"缓步走到桌边","prompt_camera":"中景平视构图","prompt_lighting":"室内暖侧光","prompt_style":"真人写实电影质感","negative_prompt":"水印，多余人物","checks":["动作可在2秒完成","无新增对白"]}]}`

func TestParseSceneDirectorDraftMergesShortShotIntoFollowingShot(t *testing.T) {
	raw := `{"shots":[{"act_type":"setup","shot_type":"特写","camera_angle":"平视","camera_movement":"固定","duration":2,"description":"若琳抬眼","dialogue":"相信姐姐，","emotion":"坚定","transition_type":"cut","transition_note":"切双人景","start_state":"若琳垂眼","end_state":"若琳抬眼","prompt_subject":"若琳特写","prompt_action":"抬眼","prompt_camera":"固定特写","prompt_lighting":"夕阳","prompt_style":"古风仙侠","negative_prompt":"文字","checks":[]},{"act_type":"rising","shot_type":"双人中景","camera_angle":"平视","camera_movement":"固定","duration":5,"description":"若琳握住若彤的手","dialogue":"姐姐会保护你。","emotion":"温柔坚定","transition_type":"cut","transition_note":"接下一镜","start_state":"若琳抬眼","end_state":"姐妹相握","prompt_subject":"姐妹双人中景","prompt_action":"若琳握住若彤的手","prompt_camera":"固定中景","prompt_lighting":"夕阳余晖","prompt_style":"古风仙侠","negative_prompt":"文字","checks":[]}]}`
	draft, err := parseSceneDirectorDraft(raw)
	if err != nil {
		t.Fatal(err)
	}
	if len(draft.Shots) != 1 || draft.Shots[0].Duration != 7 {
		t.Fatalf("merged shots = %+v", draft.Shots)
	}
	shot := draft.Shots[0]
	if shot.Dialogue != "相信姐姐，姐姐会保护你。" || shot.StartState != "若琳垂眼" || shot.EndState != "姐妹相握" {
		t.Fatalf("authority/order lost: %+v", shot)
	}
	if shot.TransitionNote != "接下一镜" || !strings.Contains(shot.PromptAction, "抬眼；若琳握住若彤的手") {
		t.Fatalf("merged direction invalid: %+v", shot)
	}
}

func TestParseSceneDirectorDraftRejectsShortShotThatCannotMergeWithinLimit(t *testing.T) {
	raw := strings.Replace(validSceneDirectorDraft, `"duration":3`, `"duration":2`, 1)
	second := strings.TrimSuffix(strings.TrimPrefix(validSceneDirectorDraft, `{"shots":[`), `]}`)
	second = strings.Replace(second, `"duration":3`, `"duration":14`, 1)
	raw = strings.TrimSuffix(raw, `]}`) + `,` + second + `]}`
	if _, err := parseSceneDirectorDraft(raw); err == nil || !strings.Contains(err.Error(), "无法与相邻镜头合并") {
		t.Fatalf("unmergeable short shot accepted: %v", err)
	}
}

func TestParseSceneDirectorDraftDropsSingleIncompleteTimelinePlaceholder(t *testing.T) {
	raw := strings.Replace(validSceneDirectorDraft, `"checks":["动作可在3秒完成","无新增对白"]`, `"action_timeline":[{"start":0,"end":3,"subject":"","action":"走到桌边","state":"停下","camera":"固定"}],"checks":["动作可在3秒完成","无新增对白"]`, 1)
	draft, err := parseSceneDirectorDraft(raw)
	if err != nil {
		t.Fatal(err)
	}
	if len(draft.Shots[0].ActionTimeline) != 0 {
		t.Fatalf("incomplete placeholder timeline retained: %+v", draft.Shots[0].ActionTimeline)
	}
	if !strings.Contains(strings.Join(draft.Shots[0].Checks, "\n"), "不完整单段动作时间轴") {
		t.Fatalf("normalization was not audited: %+v", draft.Shots[0].Checks)
	}
}

func TestParseSceneDirectorDraftKeepsMultiSegmentTimelineStrict(t *testing.T) {
	raw := strings.Replace(validSceneDirectorDraft, `"checks":["动作可在3秒完成","无新增对白"]`, `"action_timeline":[{"start":0,"end":1.5,"subject":"","action":"走近","state":"移动中","camera":"固定"},{"start":1.5,"end":3,"subject":"女主","action":"停下","state":"桌边","camera":"固定"}],"checks":["动作可在3秒完成","无新增对白"]`, 1)
	if _, err := parseSceneDirectorDraft(raw); err == nil || !strings.Contains(err.Error(), "动作时间轴第1段必须填写主体") {
		t.Fatalf("multi-segment incomplete timeline was accepted: %v", err)
	}
}

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

func TestDialogueRhythmInstructionAllowsAttributedCrossCutListenerReaction(t *testing.T) {
	got := dialogueRhythmDirectorInstruction([]models.Dialogue{{Character: "上官若琳", SpeechType: "dialogue", Text: "继续说话"}})
	for _, want := range []string{"声音跨切延续", "听者必须闭口", "不能在听者镜头中开始一条新对白", "不得把这种连续对白改写成旁白或独立画外音", "narration或monologue"} {
		if !strings.Contains(got, want) {
			t.Fatalf("missing %q: %s", want, got)
		}
	}
}

func TestValidateDialogueRhythmDraftAllowsAttributedCrossCutListenerReaction(t *testing.T) {
	draft, err := parseSceneDirectorDraft(`{"shots":[{"act_type":"setup","shot_type":"说话人近景","camera_angle":"平视","camera_movement":"固定","duration":4,"description":"上官若琳开口","dialogue":"相信姐姐，","emotion":"坚定","transition_type":"cut","transition_note":"切到听者","start_state":"上官若琳看向妹妹","end_state":"上官若琳继续说","prompt_subject":"上官若琳近景","prompt_action":"同步口型说话","prompt_camera":"近景","prompt_lighting":"柔光","prompt_style":"真人写实","negative_prompt":"","checks":[]},{"act_type":"rising","shot_type":"听者反应","camera_angle":"平视","camera_movement":"固定","duration":6,"description":"上官若琳声音跨切延续，上官若彤闭口倾听","dialogue":"姐姐无论如何也不会让你去罗刹魔域！","emotion":"担忧","transition_type":"cut","transition_note":"","start_state":"上官若彤抬头","end_state":"上官若彤神情微变","prompt_subject":"上官若彤单人近景","prompt_action":"双唇闭合倾听，上官若琳声音跨切延续","prompt_camera":"反应近景","prompt_lighting":"柔光","prompt_style":"真人写实","negative_prompt":"","checks":[]}]}`)
	if err != nil {
		t.Fatal(err)
	}
	dialogues := []models.Dialogue{{Character: "上官若琳", SpeechType: "dialogue", Text: "相信姐姐，姐姐无论如何也不会让你去罗刹魔域！"}}
	if err := validateDialogueRhythmDraft(draft, dialogues); err != nil {
		t.Fatalf("valid cross-cut listener reaction rejected: %v", err)
	}

	draft.Shots[1].Description = "上官若彤倾听"
	draft.Shots[1].PromptAction = "双唇闭合倾听"
	if err := validateDialogueRhythmDraft(draft, dialogues); err == nil || !strings.Contains(err.Error(), "明确标注同一句对白跨切延续") {
		t.Fatalf("unattributed carryover accepted: %v", err)
	}

	draft.Shots[0].PromptSubject = "上官若彤单人近景"
	draft.Shots[0].Description = "上官若琳声音跨切延续，上官若彤闭口倾听"
	draft.Shots[0].PromptAction = "双唇闭合倾听，上官若琳声音跨切延续"
	if err := validateDialogueRhythmDraft(draft, dialogues); err == nil {
		t.Fatal("new dialogue incorrectly allowed to start off-screen in listener shot")
	}
}

func TestBuildDialogueRhythmReviewUsesAuthoritativeRangesAndTimeline(t *testing.T) {
	draft, err := parseSceneDirectorDraft(`{"shots":[{"act_type":"setup","shot_type":"说话人近景","camera_angle":"平视","camera_movement":"固定","duration":4,"description":"上官若琳开口","dialogue":"相信姐姐，","emotion":"坚定","transition_type":"cut","transition_note":"","start_state":"开口","end_state":"继续说","prompt_subject":"上官若琳近景","prompt_action":"同步口型","prompt_camera":"近景","prompt_lighting":"柔光","prompt_style":"真人写实","negative_prompt":"","checks":[]},{"act_type":"rising","shot_type":"听者反应","camera_angle":"平视","camera_movement":"固定","duration":6,"description":"上官若琳声音跨切延续，上官若彤闭口倾听","dialogue":"姐姐不会让你去。","emotion":"担忧","transition_type":"cut","transition_note":"","start_state":"倾听","end_state":"神情微变","prompt_subject":"上官若彤单人近景","prompt_action":"双唇闭合，上官若琳声音跨切延续","prompt_camera":"反应近景","prompt_lighting":"柔光","prompt_style":"真人写实","negative_prompt":"","checks":[]},{"act_type":"resolution","shot_type":"反应近景","camera_angle":"平视","camera_movement":"固定","duration":3,"description":"上官若彤沉默反应","dialogue":"","emotion":"释然","transition_type":"cut","transition_note":"","start_state":"神情微变","end_state":"轻轻点头","prompt_subject":"上官若彤近景","prompt_action":"闭口点头","prompt_camera":"反应近景","prompt_lighting":"柔光","prompt_style":"真人写实","negative_prompt":"","checks":[]}]}`)
	if err != nil {
		t.Fatal(err)
	}
	dialogues := []models.Dialogue{{Character: "上官若琳", SpeechType: "dialogue", Text: "相信姐姐，姐姐不会让你去。"}}
	review := buildDialogueRhythmReview(draft, dialogues)
	if len(review) != 3 {
		t.Fatalf("review=%+v", review)
	}
	if review[0].StartTime != 0 || review[0].EndTime != 4 || review[0].DialogueStart != 0 || !review[0].ContinuesToNext || review[0].Presentation != "visible_speaker_lipsync" {
		t.Fatalf("first=%+v", review[0])
	}
	if review[1].StartTime != 4 || review[1].EndTime != 10 || !review[1].ContinuesFromPrev || review[1].Presentation != "listener_reaction_carryover" || len(review[1].Speakers) != 1 || review[1].Speakers[0] != "上官若琳" {
		t.Fatalf("second=%+v", review[1])
	}
	if review[2].StartTime != 10 || review[2].EndTime != 13 || review[2].Presentation != "silent_visual" || review[2].DialogueStart != review[2].DialogueEnd {
		t.Fatalf("third=%+v", review[2])
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
	// Build draft directly so we can test normalization without hitting parse-time validation.
	draft := &sceneDirectorDraft{
		Shots: []sceneDirectorDraftShot{{
			ActType:        models.ShotActSetup,
			ShotType:       "中景",
			CameraAngle:    "平视",
			CameraMovement: "固定",
			Duration:       2, // below minimum — normalization should raise it
			Description:    "女主走到桌边",
			Dialogue:       "",
			Emotion:        "警惕",
			TransitionType: models.ShotTransitionCut,
			TransitionNote: "接视线",
			StartState:     "女主站在门边",
			EndState:       "女主停在桌边",
			PromptSubject:  "女主面部清晰",
			PromptAction:   "缓步走到桌边",
			PromptCamera:   "中景平视构图",
			PromptLighting: "室内暖侧光",
			PromptStyle:    "真人写实电影质感",
			NegativePrompt: "水印，多余人物",
			Checks:         []string{"动作可在2秒完成", "无新增对白"},
		}},
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

func TestNormalizeDialogueRhythmDraftUsesNaturalSpeechFloor(t *testing.T) {
	draft := &sceneDirectorDraft{Shots: []sceneDirectorDraftShot{{Duration: 5, Dialogue: "自化元婴的设想来自于诸天宇。当年火云刹那之变，足以证明余天成的真实身份。"}}}
	normalizeDialogueRhythmDraftDurations(draft)
	minimum := math.Ceil((dialogueTextDuration(draft.Shots[0].Dialogue)+0.6)*2) / 2
	if draft.Shots[0].Duration != minimum || minimum <= 5 {
		t.Fatalf("duration=%v minimum=%v", draft.Shots[0].Duration, minimum)
	}
	if !strings.Contains(strings.Join(draft.Shots[0].Checks, "\n"), "对白自然语速") {
		t.Fatalf("missing audit check: %+v", draft.Shots[0].Checks)
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

func TestRestoreDialogueRhythmDraftTextUsesAuthoritativeDialogue(t *testing.T) {
	draft := &sceneDirectorDraft{Shots: []sceneDirectorDraftShot{{Dialogue: "姐姐，你十年没修炼。", Duration: 8}, {Dialogue: "来得及吗？", Duration: 7}, {Dialogue: "", Duration: 3}}}
	dialogues := []models.Dialogue{{Text: "姐姐，自从你跟太运宗使者比试之后，这十年你都没有怎么好好闭关修炼过。"}, {Text: "还有不到四十年，这样真的来得及吗？"}}
	if err := restoreDialogueRhythmDraftText(draft, dialogues); err != nil {
		t.Fatal(err)
	}
	if err := validateDialogueRhythmDraft(draft, dialogues); err != nil {
		t.Fatal(err)
	}
	if draft.Shots[2].Dialogue != "" {
		t.Fatalf("reaction shot received dialogue: %q", draft.Shots[2].Dialogue)
	}
	if !strings.Contains(draft.Shots[0].Dialogue, "太运宗") || !strings.Contains(draft.Shots[1].Dialogue, "来得及吗") {
		t.Fatalf("unexpected split: %#v", draft.Shots)
	}
}

func TestActionRhythmInstructionCoversCombatAndSpellClashes(t *testing.T) {
	got := actionRhythmDirectorInstruction(nil)
	for _, want := range []string{"武戏", "仙术对轰", "3–6秒", "实时速度", "碰撞点", "无慢动作停顿", "不使用Endless"} {
		if !strings.Contains(got, want) {
			t.Fatalf("action instruction missing %q: %s", want, got)
		}
	}
}

func TestValidateActionRhythmDraftRejectsSlowMotionAndLongBeats(t *testing.T) {
	draft := &sceneDirectorDraft{Shots: []sceneDirectorDraftShot{{
		ActType: models.ShotActRising, ShotType: "中景", CameraAngle: "平视", CameraMovement: "固定", Duration: 4,
		Description: "剑修从画面左侧蹬地前冲，一剑被护盾挡开，身体向左后方落地", StartState: "剑修在左，术修在右", EndState: "剑修落在左后方，术修护盾仍在",
		PromptSubject: "剑修与术修保持左右位置", PromptAction: "实时速度立即前冲，一剑迅速完成并被护盾挡开，无慢动作停顿", PromptCamera: "固定中景", PromptLighting: "日光", PromptStyle: "仙侠写实",
		NegativePrompt: "慢动作，子弹时间，悬停，动作拖沓，重复动作", TransitionType: models.ShotTransitionCut,
	}}}
	if err := validateActionRhythmDraft(draft, nil); err != nil {
		t.Fatalf("valid action beat rejected: %v", err)
	}
	draft.Shots[0].Duration = 8
	if err := validateActionRhythmDraft(draft, nil); err == nil || !strings.Contains(err.Error(), "3至6秒") {
		t.Fatalf("long action beat accepted: %v", err)
	}
	draft.Shots[0].Duration = 4
	draft.Shots[0].PromptAction = "剑修缓缓挥剑，实时速度，无慢动作"
	if err := validateActionRhythmDraft(draft, nil); err == nil || !strings.Contains(err.Error(), "慢动作或拖延") {
		t.Fatalf("slow action language accepted: %v", err)
	}
}

func TestH3ClassifiesSpellClashAsAction(t *testing.T) {
	scene := &models.Scene{Content: "两名修士同时施法，剑气与雷法在半空对轰，冲击波击碎地面。"}
	if got := classifyH3SceneMode(scene, nil, nil); got != h3SceneAction {
		t.Fatalf("spell clash mode=%s", got)
	}
	instruction := h3SceneModeInstruction(h3SceneAction)
	for _, want := range []string{"仙术对轰", "no slow motion", "碰撞点"} {
		if !strings.Contains(instruction, want) {
			t.Fatalf("H3 action instruction missing %q: %s", want, instruction)
		}
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
