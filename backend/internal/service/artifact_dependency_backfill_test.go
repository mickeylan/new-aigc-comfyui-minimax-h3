package service

import (
	"encoding/json"
	"testing"

	"comfyui-console/internal/models"
)

func TestArtifactDependencyBackfillIsIdempotentAndAuthoritative(t *testing.T) {
	db := safetyDB(t, &models.MusicCueSheet{}, &models.MusicCandidate{}, &models.DialogueAudioCandidate{}, &models.CharacterOutfit{}, &models.CharacterOutfitLook{}, &models.SceneAudioCandidate{}, &models.GenerationCandidate{}, &models.FrameCandidate{}, &models.SceneContinuity{}, &models.ArtifactDependency{}, &models.ArtifactInvalidationEvent{})
	project := models.Project{Title: "p"}
	db.Create(&project)
	cue := models.MusicCueSheet{ProjectID: project.ID, EpisodeN: 1, Kind: "emotion", StartTime: 0, EndTime: 3}
	db.Create(&cue)
	music := models.MusicCandidate{ProjectID: project.ID, MusicCueSheetID: cue.ID, SourceVersionHash: "cue-v1"}
	db.Create(&music)
	dialogue := models.Dialogue{ProjectID: project.ID, Text: "x"}
	db.Create(&dialogue)
	audio := models.DialogueAudioCandidate{ProjectID: project.ID, DialogueID: dialogue.ID, InputHash: "dialogue-v1", Provider: "test"}
	db.Create(&audio)
	character := models.Character{ProjectID: project.ID, Name: "c"}
	db.Create(&character)
	look := models.CharacterLook{ProjectID: project.ID, CharacterID: character.ID, Name: "l", Category: "clothing", Version: 2}
	db.Create(&look)
	outfit := models.CharacterOutfit{ProjectID: project.ID, CharacterID: character.ID, Name: "o", Version: 1}
	db.Create(&outfit)
	db.Create(&models.CharacterOutfitLook{OutfitID: outfit.ID, LookID: look.ID})
	snapshot := sceneAudioSnapshot{Sources: []sceneAudioSource{{DialogueID: dialogue.ID, CandidateID: audio.ID, InputHash: audio.InputHash}}}
	raw, _ := json.Marshal(snapshot)
	sceneAudio := models.SceneAudioCandidate{ProjectID: project.ID, SceneID: 1, SourceHash: "mix-v1", SourceJSON: string(raw)}
	db.Create(&sceneAudio)
	db.Create(&models.ArtifactDependency{ProjectID: project.ID, UpstreamKind: ArtifactKindCharacterLook, UpstreamID: 999, DownstreamKind: ArtifactKindCharacterOutfit, DownstreamID: 999})
	graph := NewArtifactDependencyService(db, true)
	first, err := graph.BackfillProject(project.ID)
	if err != nil {
		t.Fatal(err)
	}
	second, err := graph.BackfillProject(project.ID)
	if err != nil {
		t.Fatal(err)
	}
	if first.MusicCandidateEdges != 1 || first.DialogueAudioEdges != 1 || first.LookOutfitEdges != 1 || first.SceneAudioEdges != 1 {
		t.Fatalf("unexpected result: %+v", first)
	}
	var count int64
	db.Model(&models.ArtifactDependency{}).Count(&count)
	if count != 4 {
		t.Fatalf("expected four unique edges, got %d", count)
	}
	if second.MusicCandidateEdges != 1 {
		t.Fatalf("repeat backfill should be safe: %+v", second)
	}
}

func TestArtifactDependencyBackfillDisabledIsNoOp(t *testing.T) {
	db := safetyDB(t, &models.ArtifactDependency{}, &models.ArtifactInvalidationEvent{})
	result, err := NewArtifactDependencyService(db, false).BackfillProject(1)
	if err != nil {
		t.Fatal(err)
	}
	if result.Enabled {
		t.Fatal("disabled backfill reported enabled")
	}
}
