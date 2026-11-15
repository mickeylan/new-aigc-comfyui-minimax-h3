package service

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strconv"
	"testing"

	"comfyui-console/internal/models"
	"github.com/gin-gonic/gin"
)

func TestDirectorDraftVersionIsDraftUntilImportConfirmed(t *testing.T) {
	s, db, _, _ := newShotRefineTestService(t, validSceneDirectorDraft)
	project := models.Project{Title: "director-version"}
	db.Create(&project)
	scene := models.Scene{ProjectID: project.ID, EpisodeN: 1, Order: 1, Duration: 3, Content: "女主走到桌边。"}
	db.Create(&scene)

	status, response := s.generateSceneDirectorDraft(&scene, sceneDirectorDraftRequest{Mode: "standard"})
	if status != http.StatusOK {
		t.Fatalf("status=%d response=%v", status, response)
	}
	versionID, _ := response["draft_version_id"].(uint)
	provenance, _ := response["skill_provenance"].(*SkillProvenance)
	if versionID == 0 || provenance == nil || provenance.Code != response["skill_code"] || len(provenance.InputSHA256) != 64 {
		t.Fatalf("version=%d provenance=%+v skill_code=%v", versionID, provenance, response["skill_code"])
	}
	var version models.PromptVersion
	db.First(&version, versionID)
	var meta map[string]any
	json.Unmarshal([]byte(version.Metadata), &meta)
	if version.State != PromptVersionStateDraft || version.EntityType != directorDraftVersionEntity || version.EntityID != scene.ID || meta["input_sha256"] != provenance.InputSHA256 || meta["skill_version"] != float64(provenance.Version) {
		t.Fatalf("version=%+v meta=%v", version, meta)
	}

	save := func(id uint) int {
		payload, _ := json.Marshal(map[string]any{"draft_version_id": id, "dialogue_snapshot": response["dialogue_snapshot"], "shots": []models.Shot{{ShotType: "中景", Duration: 3, Description: "女主走到桌边"}}})
		recorder := httptest.NewRecorder()
		ctx, _ := gin.CreateTestContext(recorder)
		ctx.Request = httptest.NewRequest(http.MethodPost, "/", bytes.NewReader(payload))
		ctx.Request.Header.Set("Content-Type", "application/json")
		ctx.Params = gin.Params{{Key: "id", Value: strconv.FormatUint(uint64(project.ID), 10)}, {Key: "sid", Value: strconv.FormatUint(uint64(scene.ID), 10)}}
		s.Shots, s.Projects = NewShotService(db), &ProjectService{db: db}
		s.HandleCreateShots(ctx)
		return recorder.Code
	}
	// Another Scene's version id must not be flipped by this Scene's import.
	other := models.PromptVersion{ProjectID: project.ID, EntityType: directorDraftVersionEntity, EntityID: scene.ID + 100, State: PromptVersionStateDraft}
	db.Create(&other)
	if code := save(other.ID); code != http.StatusOK {
		t.Fatalf("save status=%d", code)
	}
	db.First(&other, other.ID)
	db.First(&version, versionID)
	if other.State != PromptVersionStateDraft || version.State != PromptVersionStateDraft {
		t.Fatalf("foreign id must be ignored: other=%s version=%s", other.State, version.State)
	}
	if code := save(versionID); code != http.StatusOK {
		t.Fatalf("save status=%d", code)
	}
	db.First(&version, versionID)
	if version.State != PromptVersionStateApplied {
		t.Fatalf("confirmed import must mark the draft applied, got %s", version.State)
	}
}
