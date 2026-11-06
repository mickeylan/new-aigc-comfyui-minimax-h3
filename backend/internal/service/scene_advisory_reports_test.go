package service

import (
	"strings"
	"testing"
)

func TestBuildSceneAdvisoryCorrectionPromptRetainsH3Format(t *testing.T) {
	report := SceneAdvisoryReport{Result: SceneVisualCheckResult{Status: "issues_found", Checks: []SceneVisualCheckItem{{Category: "orientation", Evidence: "朝内", Suggestion: "人物改为朝向门外"}}}}
	original := "subject_definitions:\n<Subject 1>\n\ndetailed_description:\n人物站在门边。\n\noverall_soundscape:\nN/A\n\nnon_diegetic_music:\nN/A"
	got, err := buildSceneAdvisoryCorrectionPrompt(original, report)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.HasPrefix(got, "subject_definitions:") || strings.Count(got, "detailed_description:") != 1 || strings.Count(got, "overall_soundscape:") != 1 || !strings.Contains(got, "人物改为朝向门外") || !strings.HasSuffix(got, "non_diegetic_music:\nN/A") {
		t.Fatalf("structured prompt was not retained: %s", got)
	}
}

func TestSceneAdvisoryReportHistoryIsBoundedToTwenty(t *testing.T) {
	reports := make([]SceneAdvisoryReport, 0, 21)
	for i := 0; i < 21; i++ {
		reports = append(reports, SceneAdvisoryReport{ID: string(rune('a' + i))})
	}
	if len(reports) > maxSceneAdvisoryReports {
		reports = reports[len(reports)-maxSceneAdvisoryReports:]
	}
	if len(reports) != 20 || reports[0].ID != "b" {
		t.Fatalf("unexpected bounded history: %#v", reports)
	}
}
