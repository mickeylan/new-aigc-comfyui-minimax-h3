package service

import (
	"testing"

	"comfyui-console/internal/models"
)

func TestProductionCheckUsesAuthoritativeMusicCuePreflight(t *testing.T) {
	db := safetyDB(t, &models.SceneContinuity{}, &models.MusicCueSheet{}, &models.ProductionCheckReport{})
	project := models.Project{Title: "p"}
	db.Create(&project)
	db.Create(&models.Scene{ProjectID: project.ID, EpisodeN: 1, Duration: 5, ImageFile: "image.png", VideoFile: "video.mp4"})
	db.Create(&models.MusicCueSheet{ProjectID: project.ID, EpisodeN: 1, Kind: "scene", StartTime: 0, EndTime: 8, Enabled: true, CaptionJSON: "{}", ReviewStatus: "draft"})
	result, err := NewProductionCheckService(db).Check(project.ID, 1)
	if err != nil {
		t.Fatal(err)
	}
	codes := map[string]bool{}
	for _, finding := range result.Findings {
		codes[finding.Code] = true
	}
	if !codes["music_cue_timeline"] || !codes["music_cue_caption"] || !codes["music_cue_review"] {
		t.Fatalf("music preflight not reflected: %+v", result.Findings)
	}
}

func TestProductionCheckReportsAreSanitizedVersionedAndReviewable(t *testing.T) {
	db := safetyDB(t, &models.SceneContinuity{}, &models.MusicCueSheet{}, &models.ProductionCheckReport{})
	project := models.Project{Title: "p"}
	db.Create(&project)
	scene := models.Scene{ProjectID: project.ID, EpisodeN: 1, Duration: 8, ImageFile: "secret-storyboard.png", VideoFile: "secret-video.mp4", Content: "secret story", ImagePrompt: "secret prompt"}
	db.Create(&scene)
	svc := NewProductionCheckService(db)
	first, err := svc.CreateReport(project.ID, 1)
	if err != nil {
		t.Fatal(err)
	}
	if !first.Ready || first.FindingCount != 0 {
		t.Fatalf("unexpected first report: %+v", first)
	}
	db.Model(&scene).Updates(map[string]any{"video_file": "", "prompt_stale": true})
	second, err := svc.CreateReport(project.ID, 1)
	if err != nil {
		t.Fatal(err)
	}
	if second.Ready || second.FindingCount < 2 {
		t.Fatalf("unexpected second report: %+v", second)
	}
	if second.Findings[0].Action == "" || second.Findings[0].Recommendation == "" {
		t.Fatalf("finding lacks remediation metadata: %+v", second.Findings[0])
	}
	rows, err := svc.ListReports(project.ID, 1)
	if err != nil {
		t.Fatal(err)
	}
	if len(rows) != 2 || !rows[1].Superseded || rows[0].Superseded {
		t.Fatalf("unexpected history: %+v", rows)
	}
	for _, finding := range rows[0].Findings {
		if finding.Message == scene.Content || finding.Message == scene.ImagePrompt || finding.Message == scene.ImageFile {
			t.Fatal("sensitive content leaked")
		}
	}
	if _, err := svc.ReviewReport(project.ID, first.ID, "reviewed", "历史"); err == nil {
		t.Fatal("superseded report was mutable")
	}
	previousHash := second.SourceHash
	db.Model(&scene).Update("title", "只改变来源状态，不改变检查项")
	third, err := svc.CreateReport(project.ID, 1)
	if err != nil {
		t.Fatal(err)
	}
	if third.SourceHash == previousHash {
		t.Fatal("source hash ignored source state changes")
	}
	reviewed, err := svc.ReviewReport(project.ID, third.ID, "reviewed", "已处理")
	if err != nil {
		t.Fatal(err)
	}
	if reviewed.ReviewStatus != "reviewed" || reviewed.ReviewedAt == nil {
		t.Fatalf("not reviewed: %+v", reviewed)
	}
}
