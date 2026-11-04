package service

import (
	"encoding/json"
	"strings"
	"testing"

	"comfyui-console/internal/models"
)

func adaptationRegenerationRaw(t *testing.T, script string, ledger []SourceDialogue) map[string]any {
	t.Helper()
	var raw map[string]any
	if err := json.Unmarshal([]byte(adaptationScriptFixture(t, script, ledger, true)), &raw); err != nil {
		t.Fatal(err)
	}
	return raw
}

func marshalAdaptationRegenerationRaw(t *testing.T, raw map[string]any) string {
	t.Helper()
	b, err := json.Marshal(raw)
	if err != nil {
		t.Fatal(err)
	}
	return string(b)
}

func TestGenerateScriptPreservesAdaptationStatesThroughDialogueRestore(t *testing.T) {
	provider := &adaptationMockTextProvider{}
	svc, project, chapter := newAdaptationDialogueTestService(t, provider)
	ledger := extractSourceDialogues(chapter, "虬髯大汉", "马脸男子", "齐姓道士")
	raw := adaptationRegenerationRaw(t, "regenerated-stateful-script", ledger)
	raw["opening_state"] = "三人抵达荒野边缘"
	raw["ending_state"] = map[string]any{"place": "山谷出口", "threat": "妖狐仍在逃"}
	raw["adaptation_unknown_root"] = map[string]any{"must_survive_helper": true}
	provider.responses = []string{marshalAdaptationRegenerationRaw(t, raw)}

	ep, err := svc.GenerateScript(project.ID, 1)
	if err != nil {
		t.Fatal(err)
	}
	if ep.OpeningState != `"三人抵达荒野边缘"` {
		t.Fatalf("opening_state=%q", ep.OpeningState)
	}
	var ending map[string]any
	if err := json.Unmarshal([]byte(ep.EndingState), &ending); err != nil {
		t.Fatalf("ending_state=%q: %v", ep.EndingState, err)
	}
	if ending["place"] != "山谷出口" || ending["threat"] != "妖狐仍在逃" {
		t.Fatalf("ending_state=%v", ending)
	}
	if len(provider.users) != 1 {
		t.Fatalf("provider calls=%d, want 1", len(provider.users))
	}
}

func TestGenerateScriptOmittedOrNullStatesRetainPreviousValues(t *testing.T) {
	for _, tc := range []struct {
		name    string
		setNull bool
	}{
		{name: "omitted"},
		{name: "explicit null", setNull: true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			provider := &adaptationMockTextProvider{}
			svc, project, chapter := newAdaptationDialogueTestService(t, provider)
			const oldOpening = `{"place":"旧开场"}`
			const oldEnding = `{"place":"旧结尾"}`
			if err := svc.db.Model(&models.EpisodeAdaptation{}).Where("project_id = ? AND episode_n = ?", project.ID, 1).
				Updates(map[string]any{"opening_state": oldOpening, "ending_state": oldEnding}).Error; err != nil {
				t.Fatal(err)
			}
			ledger := extractSourceDialogues(chapter, "虬髯大汉", "马脸男子", "齐姓道士")
			raw := adaptationRegenerationRaw(t, "regenerated-without-states", ledger)
			if tc.setNull {
				raw["opening_state"], raw["ending_state"] = nil, nil
			}
			provider.responses = []string{marshalAdaptationRegenerationRaw(t, raw)}

			ep, err := svc.GenerateScript(project.ID, 1)
			if err != nil {
				t.Fatal(err)
			}
			if ep.OpeningState != oldOpening || ep.EndingState != oldEnding {
				t.Fatalf("states=(%q,%q), want previous (%q,%q)", ep.OpeningState, ep.EndingState, oldOpening, oldEnding)
			}
		})
	}
}

func TestGenerateScriptEpisodeTwoUsesLatestPreviousScriptWithoutContinuityGate(t *testing.T) {
	for _, continuity := range []string{"pending", "conflict"} {
		t.Run(continuity, func(t *testing.T) {
			provider := &adaptationMockTextProvider{}
			svc, project, chapter := newAdaptationDialogueTestService(t, provider)
			const previousLatest = "上一集最新剧本：修士在界碑处停步，独有事件甲完成。"
			if err := svc.db.Model(&models.EpisodeAdaptation{}).Where("project_id = ? AND episode_n = ?", project.ID, 1).
				Updates(map[string]any{"script": previousLatest, "status": "scripted", "continuity_status": continuity}).Error; err != nil {
				t.Fatal(err)
			}
			// Mirror GenerateScriptCore's persisted latest-script index while independently
			// exercising the EpisodeAdaptation prerequisite and continuity status.
			scripts, _ := json.Marshal(map[int]string{1: previousLatest})
			if err := svc.db.Model(&models.Project{}).Where("id = ?", project.ID).Update("scripts", string(scripts)).Error; err != nil {
				t.Fatal(err)
			}
			ids, _ := json.Marshal([]uint{chapter.ID})
			// Episode 2 deliberately reuses the approved chapter-1 source mapping.
			ep2 := models.EpisodeAdaptation{ProjectID: project.ID, EpisodeN: 2, Title: "继续围捕", ChapterStart: 1, ChapterEnd: 1,
				SourceChapterIDs: string(ids), TargetDuration: 180, TargetScenes: 20, Status: "approved", ContinuityStatus: "pending"}
			if err := svc.db.Create(&ep2).Error; err != nil {
				t.Fatal(err)
			}
			ledger := extractSourceDialogues(chapter, "虬髯大汉", "马脸男子", "齐姓道士")
			provider.responses = []string{adaptationScriptFixture(t, "第二集独有事件乙完成", ledger, true)}

			got, err := svc.GenerateScript(project.ID, 2)
			if err != nil {
				t.Fatal(err)
			}
			if got.Status != "scripted" || len(provider.users) != 1 {
				t.Fatalf("status=%q provider calls=%d", got.Status, len(provider.users))
			}
			if !strings.Contains(provider.users[0], previousLatest) {
				t.Fatalf("generation context omitted latest previous script: %s", provider.users[0])
			}
		})
	}

	t.Run("missing previous script fails before provider", func(t *testing.T) {
		provider := &adaptationMockTextProvider{}
		svc, project, chapter := newAdaptationDialogueTestService(t, provider)
		ids, _ := json.Marshal([]uint{chapter.ID})
		if err := svc.db.Create(&models.EpisodeAdaptation{ProjectID: project.ID, EpisodeN: 2, Title: "继续围捕", ChapterStart: 1, ChapterEnd: 1,
			SourceChapterIDs: string(ids), TargetDuration: 180, TargetScenes: 20, Status: "approved", ContinuityStatus: "pending"}).Error; err != nil {
			t.Fatal(err)
		}
		_, err := svc.GenerateScript(project.ID, 2)
		if err == nil || !strings.Contains(err.Error(), "请先生成第1集剧本") {
			t.Fatalf("error=%v", err)
		}
		if len(provider.users) != 0 {
			t.Fatalf("provider called %d times before prerequisite error", len(provider.users))
		}
	})
}
