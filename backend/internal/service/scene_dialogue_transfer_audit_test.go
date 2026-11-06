package service

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"testing"

	"comfyui-console/internal/models"
	"github.com/gin-gonic/gin"
)

func td(character, kind, text string) models.Dialogue {
	return models.Dialogue{Character: character, SpeechType: kind, Text: text}
}

func TestDialogueTransferAuditSplitSameOwnerIsConsistent(t *testing.T) {
	got := assessDialogueTransfer([]models.Dialogue{td("未知来客", "dialogue", "不要过来")}, []models.Dialogue{td("未知来客", "dialogue", "不要"), td("未知来客", "dialogue", "过来")})
	if got.Status != "consistent" || !got.Assessed || !got.Advisory || got.BeforeOccurrences != 1 || got.AfterOccurrences != 1 {
		t.Fatalf("audit = %+v", got)
	}
}

func TestDialogueTransferAuditDetectsSpeechStreamChanges(t *testing.T) {
	before := []models.Dialogue{td("甲", "dialogue", "一"), td("乙", "narration", "二"), td("甲", "dialogue", "一")}
	cases := map[string][]models.Dialogue{
		"order":          {td("乙", "narration", "二"), td("甲", "dialogue", "一"), td("甲", "dialogue", "一")},
		"repeat missing": {td("甲", "dialogue", "一"), td("乙", "narration", "二")},
		"type":           {td("甲", "dialogue", "一"), td("乙", "dialogue", "二"), td("甲", "dialogue", "一")},
	}
	for name, after := range cases {
		t.Run(name, func(t *testing.T) {
			if got := assessDialogueTransfer(before, after); got.Status != "attention" {
				t.Fatalf("audit = %+v", got)
			}
		})
	}
}

func TestDialogueTransferAuditMakesNoSemanticClaim(t *testing.T) {
	got := assessDialogueTransfer([]models.Dialogue{td("甲", "dialogue", "原句")}, nil)
	for _, warning := range got.Warnings {
		if strings.Contains(strings.ToLower(warning), "semantic") || strings.Contains(warning, "语义一致") {
			t.Fatalf("semantic claim in %q", warning)
		}
	}
}

func TestManualSceneMergeHTTPIncludesDialogueTransferAudit(t *testing.T) {
	gin.SetMode(gin.TestMode)
	db := newShotMaterializationDB(t)
	p := models.Project{Title: "audit"}
	db.Create(&p)
	a := models.Scene{ProjectID: p.ID, EpisodeN: 1, Generation: 1, Order: 1, Duration: 5, Content: "甲说话。"}
	b := models.Scene{ProjectID: p.ID, EpisodeN: 1, Generation: 1, Order: 2, Duration: 5, Content: "乙说话。"}
	db.Create(&a)
	db.Create(&b)
	db.Create(&models.Dialogue{ProjectID: p.ID, SceneID: a.ID, Order: 1, Character: "甲", SpeechType: "dialogue", Text: "一", Source: "user"})
	db.Create(&models.Dialogue{ProjectID: p.ID, SceneID: b.ID, Order: 1, Character: "乙", SpeechType: "dialogue", Text: "二", Source: "user"})
	svc := &Service{DB: db}
	preview, err := svc.PreviewManualSceneMerge(p.ID, []uint{a.ID, b.ID})
	if err != nil {
		t.Fatal(err)
	}
	body, _ := json.Marshal(manualMergeRequest{SceneIDs: preview.SceneIDs, SourceHash: preview.SourceHash})
	recorder := httptest.NewRecorder()
	ctx, _ := gin.CreateTestContext(recorder)
	ctx.Request = httptest.NewRequest(http.MethodPost, "/apply", strings.NewReader(string(body)))
	ctx.Request.Header.Set("Content-Type", "application/json")
	ctx.Params = gin.Params{{Key: "id", Value: strconv.FormatUint(uint64(p.ID), 10)}}
	svc.HandleApplyManualSceneMerge(ctx)
	if recorder.Code != http.StatusOK {
		t.Fatalf("status=%d body=%s", recorder.Code, recorder.Body.String())
	}
	var response struct {
		DialogueTransferAudit DialogueTransferAudit `json:"dialogue_transfer_audit"`
	}
	if err := json.Unmarshal(recorder.Body.Bytes(), &response); err != nil {
		t.Fatal(err)
	}
	if !response.DialogueTransferAudit.Assessed || response.DialogueTransferAudit.Status != "consistent" {
		t.Fatalf("audit=%+v", response.DialogueTransferAudit)
	}
}
