package service

import (
	"testing"

	"comfyui-console/internal/models"
)

func dialogueBatchTestDB(t *testing.T) *ProjectService {
	t.Helper()
	db := safetyDB(t, &models.DialogueSynthesisBatch{}, &models.DialogueSynthesisItem{}, &models.Character{}, &models.Task{})
	return &ProjectService{db: db, stopped: make(chan struct{})}
}

func TestEpisodeDialoguesForSynthesisUsesSceneOrder(t *testing.T) {
	s := dialogueBatchTestDB(t)
	project := models.Project{Title: "p"}
	s.db.Create(&project)
	later := models.Scene{ProjectID: project.ID, EpisodeN: 1, Generation: 1, Order: 2}
	s.db.Create(&later)
	earlier := models.Scene{ProjectID: project.ID, EpisodeN: 1, Generation: 1, Order: 1}
	s.db.Create(&earlier)
	s.db.Create(&models.Dialogue{ProjectID: project.ID, SceneID: later.ID, Order: 1, Character: "乙", Text: "后", Speed: 1, Volume: 1})
	s.db.Create(&models.Dialogue{ProjectID: project.ID, SceneID: earlier.ID, Order: 1, Character: "甲", Text: "先", Speed: 1, Volume: 1})
	_, rows, err := s.episodeDialoguesForSynthesis(&project, 1, false)
	if err != nil {
		t.Fatal(err)
	}
	if len(rows) != 2 || rows[0].Text != "先" || rows[1].Text != "后" {
		t.Fatalf("unexpected order: %+v", rows)
	}
}

func TestCreateEpisodeDialogueSynthesisBatchRejectsConcurrentBatch(t *testing.T) {
	s := dialogueBatchTestDB(t)
	project := models.Project{Title: "p"}
	s.db.Create(&project)
	s.db.Create(&models.Scene{ProjectID: project.ID, EpisodeN: 1, Generation: 1, Order: 1})
	s.db.Create(&models.DialogueSynthesisBatch{ProjectID: project.ID, EpisodeN: 1, Generation: 1, Status: dialogueBatchRunning})
	if _, err := s.CreateEpisodeDialogueSynthesisBatch(&project, 1, true); err == nil {
		t.Fatal("concurrent batch accepted")
	}
}

func TestDialogueSynthesisBatchProjectIsolation(t *testing.T) {
	s := dialogueBatchTestDB(t)
	batch := models.DialogueSynthesisBatch{ProjectID: 1, EpisodeN: 1, Status: dialogueBatchCompleted}
	s.db.Create(&batch)
	if _, err := s.GetEpisodeDialogueSynthesisBatch(2, 1, batch.ID); err == nil {
		t.Fatal("cross-project batch leaked")
	}
	if _, err := s.RetryEpisodeDialogueSynthesisBatch(2, 1, batch.ID); err == nil {
		t.Fatal("cross-project retry accepted")
	}
}
