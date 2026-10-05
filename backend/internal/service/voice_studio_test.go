package service

import (
	"testing"

	"comfyui-console/internal/models"
)

func TestParseStructuredSRTMetadataFormats(t *testing.T) {
	input := "\ufeff1\r\n00:00:00,250 --> 00:00:02,500\r\n[voice-studio role=dialogue character=\"林夏\" voice=warm speed=1.15 emotion=calm delivery=\"softly spoken\" language=zh-CN seed=42]\r\n第一行\r\n第二行\r\n\r\n2\r\n00:00:03.000 --> 00:00:04.250\r\n{\"role\":\"narration\",\"character\":\"旁白\",\"voice\":\"narrator\",\"speed\":\"0.9\",\"emotion\":\"serious\",\"delivery\":\"measured\",\"language\":\"zh\",\"seed\":0}\r\n旁白原文"
	cues, err := ParseStructuredSRT(input)
	if err != nil {
		t.Fatal(err)
	}
	if len(cues) != 2 {
		t.Fatalf("got %d cues", len(cues))
	}
	first := cues[0]
	if first.Index != 1 || first.Start != .25 || first.End != 2.5 || first.Text != "第一行\n第二行" {
		t.Fatalf("unexpected first cue timing/text: %+v", first)
	}
	if first.Role != "dialogue" || first.Character != "林夏" || first.Voice != "warm" || first.Speed != 1.15 || first.Emotion != "calm" || first.Delivery != "softly spoken" || first.Language != "zh-CN" || first.Seed == nil || *first.Seed != 42 {
		t.Fatalf("metadata not preserved: %+v", first)
	}
	second := cues[1]
	if second.Role != "narration" || second.Character != "旁白" || second.Seed == nil || *second.Seed != 0 || second.Speed != .9 {
		t.Fatalf("JSON metadata not preserved: %+v", second)
	}
}

func TestParseStructuredSRTChineseMetadata(t *testing.T) {
	input := "1\n00:00:01,000 --> 00:00:04,500\n[角色=上官若琳; 音色=ruolin; 语速=1.1; 感情=温柔; 表达=克制; 语言=ZH; 种子=1201]\n相信姐姐。"
	cues, err := ParseStructuredSRT(input)
	if err != nil {
		t.Fatal(err)
	}
	cue := cues[0]
	if cue.Character != "上官若琳" || cue.Voice != "ruolin" || cue.Speed != 1.1 || cue.Emotion != "温柔" || cue.Delivery != "克制" || cue.Language != "ZH" || cue.Seed == nil || *cue.Seed != 1201 {
		t.Fatalf("Chinese metadata not preserved: %+v", cue)
	}
}

func TestParseStructuredSRTRejectsInvalidInput(t *testing.T) {
	cases := []string{
		"1\nnot a time\ntext",
		"1\n00:00:02,000 --> 00:00:01,000\ntext",
		"1\n00:00:00,000 --> 00:00:01,000\n[role=dialogue speed=0]\ntext",
		"1\n00:00:00,000 --> 00:00:01,000\n[role=dialogue seed=nope]\ntext",
	}
	for _, input := range cases {
		if _, err := ParseStructuredSRT(input); err == nil {
			t.Fatalf("expected parse error for %q", input)
		}
	}
}

func TestPreviewStructuredSRTIsEpisodeScopedAndReadOnly(t *testing.T) {
	db := safetyDB(t, &models.Episode{}, &models.Character{}, &models.VoiceStudioSRTImport{})
	first := models.Project{Title: "first"}
	second := models.Project{Title: "second"}
	db.Create(&first)
	db.Create(&second)
	firstScene := models.Scene{ProjectID: first.ID, EpisodeN: 1, Generation: 1, Order: 1, Duration: 3}
	secondScene := models.Scene{ProjectID: second.ID, EpisodeN: 2, Generation: 1, Order: 1, Duration: 3}
	db.Create(&firstScene)
	db.Create(&secondScene)
	dialogue := models.Dialogue{ProjectID: first.ID, SceneID: firstScene.ID, Order: 1, Character: "林夏", Text: "canonical"}
	db.Create(&dialogue)

	service := &ProjectService{db: db}
	input := "1\n00:00:00,000 --> 00:00:01,000\n[role=dialogue character=其他人 voice=test speed=1 emotion=happy delivery=fast language=zh seed=7]\nreplacement"
	preview, err := service.PreviewStructuredSRT(&first, 1, input)
	if err != nil {
		t.Fatal(err)
	}
	if !preview.ReadOnly || preview.ProjectID != first.ID || preview.EpisodeN != 1 || preview.Count != 1 {
		t.Fatalf("unexpected preview: %+v", preview)
	}
	if len(preview.RoleMapping) != 1 || preview.RoleMapping[0].Source != "其他人" || preview.RoleMapping[0].Matched || len(preview.Diagnostics) < 1 {
		t.Fatalf("unmapped SRT role was not diagnosed: %+v", preview)
	}
	if _, err := service.PreviewStructuredSRT(&first, 2, input); err != ErrVoiceStudioEpisodeNotFound {
		t.Fatalf("cross-project episode leaked into preview: %v", err)
	}
	var persisted models.Dialogue
	db.First(&persisted, dialogue.ID)
	if persisted.Text != dialogue.Text || persisted.Character != dialogue.Character || persisted.ProjectID != dialogue.ProjectID || persisted.SceneID != dialogue.SceneID || !persisted.UpdatedAt.Equal(dialogue.UpdatedAt) {
		t.Fatalf("preview mutated canonical dialogue: before=%+v after=%+v", dialogue, persisted)
	}
}

func TestApplyStructuredSRTUpdatesOnlyVoiceParameters(t *testing.T) {
	db := safetyDB(t, &models.Episode{}, &models.Character{}, &models.VoiceStudioSRTImport{}, &models.DialogueAudioCandidate{})
	project := models.Project{Title: "p"}
	db.Create(&project)
	scene := models.Scene{ProjectID: project.ID, EpisodeN: 1, Generation: 1, Order: 1, Duration: 4}
	db.Create(&scene)
	dialogue := models.Dialogue{ProjectID: project.ID, SceneID: scene.ID, Order: 1, Character: "林夏", SpeechType: "dialogue", Text: "原文不能修改", Speed: 1, Emotion: "中性", Delivery: "自然", AudioFile: "old.wav", AudioHash: "old", Status: "ready"}
	db.Create(&dialogue)
	db.Create(&models.Character{ProjectID: project.ID, Name: "林夏", VoiceRef: "voice.wav"})
	service := &ProjectService{db: db}
	input := "1\n00:00:00,000 --> 00:00:03,000\n[角色=林夏; 音色=local-ref; 语速=1.2; 情绪=紧张; 表达=压低声音]\n原文不能修改"
	preview, err := service.PreviewStructuredSRT(&project, 1, input)
	if err != nil {
		t.Fatal(err)
	}
	if preview.PreviewToken == "" || len(preview.Diagnostics) != 0 {
		t.Fatalf("unexpected preview: %+v", preview)
	}
	result, err := service.ApplyStructuredSRT(&project, 1, preview.PreviewToken)
	if err != nil {
		t.Fatal(err)
	}
	if result.Applied != 1 {
		t.Fatalf("applied=%d", result.Applied)
	}
	db.First(&dialogue, dialogue.ID)
	if dialogue.Text != "原文不能修改" || dialogue.Character != "林夏" || dialogue.Speed != 1.2 || dialogue.Emotion != "紧张" || dialogue.Delivery != "压低声音" || dialogue.Voice != "local-ref" || !dialogue.AudioStale || dialogue.Status != "pending" {
		t.Fatalf("unexpected applied dialogue: %+v", dialogue)
	}
	if _, err := service.ApplyStructuredSRT(&project, 1, preview.PreviewToken); err == nil {
		t.Fatal("preview token was applied twice")
	}
}

func TestApplyStructuredSRTRejectsDialogueChangedAfterPreview(t *testing.T) {
	db := safetyDB(t, &models.Episode{}, &models.Character{}, &models.VoiceStudioSRTImport{}, &models.DialogueAudioCandidate{})
	project := models.Project{Title: "p"}
	db.Create(&project)
	scene := models.Scene{ProjectID: project.ID, EpisodeN: 1, Generation: 1, Order: 1, Duration: 3}
	db.Create(&scene)
	dialogue := models.Dialogue{ProjectID: project.ID, SceneID: scene.ID, Order: 1, Character: "林夏", SpeechType: "dialogue", Text: "原文", Speed: 1}
	db.Create(&dialogue)
	service := &ProjectService{db: db}
	preview, err := service.PreviewStructuredSRT(&project, 1, "1\n00:00:00,000 --> 00:00:01,000\n[角色=林夏; 语速=1]\n原文")
	if err != nil {
		t.Fatal(err)
	}
	db.Model(&dialogue).Update("text", "已修改")
	if _, err := service.ApplyStructuredSRT(&project, 1, preview.PreviewToken); err == nil {
		t.Fatal("changed dialogue accepted stale preview")
	}
}

func TestVoiceStudioDataUsesCanonicalProjectDialogue(t *testing.T) {
	db := safetyDB(t, &models.Episode{}, &models.Character{}, &models.VoiceStudioSRTImport{}, &models.DialogueAudioCandidate{})
	project := models.Project{Title: "first"}
	other := models.Project{Title: "second"}
	db.Create(&project)
	db.Create(&other)
	scene := models.Scene{ProjectID: project.ID, EpisodeN: 1, Generation: 1, Order: 1, Duration: 3}
	otherScene := models.Scene{ProjectID: other.ID, EpisodeN: 1, Generation: 1, Order: 1, Duration: 3}
	db.Create(&scene)
	db.Create(&otherScene)
	db.Create(&models.Dialogue{ProjectID: project.ID, SceneID: scene.ID, Order: 1, Text: "canonical"})
	db.Create(&models.Dialogue{ProjectID: other.ID, SceneID: otherScene.ID, Order: 1, Text: "other"})
	db.Create(&models.Character{ProjectID: project.ID, Name: "林夏", Voice: "warm"})
	db.Create(&models.Character{ProjectID: other.ID, Name: "other", Voice: "other"})

	data, err := (&ProjectService{db: db}).VoiceStudioData(&project, 1)
	if err != nil {
		t.Fatal(err)
	}
	dialogues := data["dialogues"].([]models.Dialogue)
	characters := data["characters"].([]models.Character)
	if len(dialogues) != 1 || dialogues[0].Text != "canonical" || len(characters) != 1 || characters[0].Name != "林夏" {
		t.Fatalf("aggregate was not project scoped: dialogues=%+v characters=%+v", dialogues, characters)
	}
	if data["dialogue_authority"] != "dialogues" {
		t.Fatalf("authority marker missing: %+v", data)
	}
	roles := data["roles"].([]VoiceStudioRoleSummary)
	if len(roles) != 1 || roles[0].Name != "未指定说话人" || roles[0].DialogueCount != 1 {
		t.Fatalf("unexpected role summary: %+v", roles)
	}
}

func TestVoiceStudioSlotQAUsesSelectedActualDurationAndKeepsUnknownStatesExplicit(t *testing.T) {
	db := safetyDB(t, &models.DialogueAudioCandidate{})
	project := models.Project{Title: "slot qa"}
	db.Create(&project)
	scene := models.Scene{ProjectID: project.ID, EpisodeN: 1, Generation: 1, Order: 1, Duration: 10}
	db.Create(&scene)
	dialogues := []models.Dialogue{
		{ProjectID: project.ID, SceneID: scene.ID, Order: 1, Text: "overflow", Speed: 2, AudioFile: "overflow.wav", Status: "ready"},
		{ProjectID: project.ID, SceneID: scene.ID, Order: 2, Text: "fits", Speed: 2, AudioFile: "fits.wav", Status: "ready"},
		{ProjectID: project.ID, SceneID: scene.ID, Order: 3, Text: "stale", Speed: 1, AudioFile: "stale.wav", Status: "pending", AudioStale: true},
		{ProjectID: project.ID, SceneID: scene.ID, Order: 4, Text: "missing", Speed: 1, Status: "pending"},
		{ProjectID: project.ID, SceneID: scene.ID, Order: 5, Text: "duration unknown", Speed: 1, AudioFile: "legacy.wav", Status: "ready"},
	}
	for i := range dialogues {
		db.Create(&dialogues[i])
	}
	db.Create(&models.DialogueAudioCandidate{ProjectID: project.ID, DialogueID: dialogues[0].ID, InputHash: "a", Provider: "test", File: "overflow.wav", Duration: 6, IsCurrent: true})
	db.Create(&models.DialogueAudioCandidate{ProjectID: project.ID, DialogueID: dialogues[1].ID, InputHash: "b", Provider: "test", File: "fits.wav", Duration: 3, IsCurrent: true})
	db.Create(&models.DialogueAudioCandidate{ProjectID: project.ID, DialogueID: dialogues[2].ID, InputHash: "c", Provider: "test", File: "stale.wav", Duration: 1, IsCurrent: true, Stale: true})
	// A historical take must not be treated as the currently selected duration.
	db.Create(&models.DialogueAudioCandidate{ProjectID: project.ID, DialogueID: dialogues[4].ID, InputHash: "d", Provider: "test", File: "legacy.wav", Duration: 1, IsCurrent: false})

	rows, summary, err := (&ProjectService{db: db}).voiceStudioSlotQA(project.ID, 1)
	if err != nil {
		t.Fatal(err)
	}
	if len(rows) != 5 {
		t.Fatalf("rows=%d: %+v", len(rows), rows)
	}
	if rows[0].Status != "overflow" || rows[0].AudioDuration == nil || *rows[0].AudioDuration != 6 || rows[0].PlaybackDuration == nil || *rows[0].PlaybackDuration != 3 || rows[0].SlotDuration != 2 || rows[0].OverflowSeconds != 1 {
		t.Fatalf("selected duration/speed overflow not calculated: %+v", rows[0])
	}
	if rows[1].Status != "ok" || rows[1].PlaybackDuration == nil || *rows[1].PlaybackDuration != 1.5 {
		t.Fatalf("fitting take not evaluated: %+v", rows[1])
	}
	if rows[2].Status != "stale" || rows[3].Status != "missing" || rows[4].Status != "duration_missing" {
		t.Fatalf("unknown audio states were hidden: %+v", rows)
	}
	if summary.Total != 5 || summary.Evaluated != 2 || summary.OKCount != 1 || summary.OverflowCount != 1 || summary.StaleCount != 1 || summary.MissingCount != 1 || summary.DurationMissingCount != 1 || summary.IssueCount != 4 || summary.TotalOverflowSeconds != 1 || summary.MaxOverflowSeconds != 1 {
		t.Fatalf("unexpected episode QA summary: %+v", summary)
	}
}
