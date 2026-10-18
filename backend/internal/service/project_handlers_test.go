package service

import (
	"bytes"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/glebarez/sqlite"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"

	"comfyui-console/internal/models"
)

func TestSceneVideoReferenceBindingsPreserveEffectiveOrder(t *testing.T) {
	refs := []FileMeta{{TaskID: "1", Name: "character.png"}, {TaskID: "1", Name: "location.png"}, {TaskID: "1", Name: "storyboard.png"}}
	lines := []string{"- <Picture 1>：角色「舒寒」四视图", "- <Picture 2>：场景「内殿」参考图", "- <Picture 3>：当前分镜画面"}
	prompt := "detailed_description:\n[Shot 1] <Subject 1> stands in <Subject 2>.\n[Shot 2] <Subject 1> moves away."
	got := sceneVideoReferenceBindings(refs, lines, prompt)
	if len(got) != 3 {
		t.Fatalf("bindings=%+v", got)
	}
	for i := range got {
		if got[i].Picture != i+1 || got[i].Name != refs[i].Name {
			t.Fatalf("binding %d=%+v", i, got[i])
		}
	}
	if got[0].Subject == nil || *got[0].Subject != 1 || got[0].Role != "character" || got[0].Identity != "舒寒" {
		t.Fatalf("character=%+v", got[0])
	}
	if got[1].Subject == nil || got[1].Role != "location" || got[1].Identity != "内殿" {
		t.Fatalf("location=%+v", got[1])
	}
	if got[2].Subject == nil || *got[2].Subject != 3 || got[2].Role != "storyboard" || got[2].RetentionMode != "weak_reference" {
		t.Fatalf("storyboard=%+v", got[2])
	}
	if len(got[0].ShotOrders) != 2 || got[0].ShotOrders[0] != 1 || got[0].ShotOrders[1] != 2 || got[0].RetentionMode != "fully_preserved" {
		t.Fatalf("appearances=%+v", got[0])
	}
	if got[2].ImageURL != "/api/input/1/storyboard.png" {
		t.Fatalf("url=%q", got[2].ImageURL)
	}
}

func TestGenerateCharacterPortraitPreconditionReturns4xx(t *testing.T) {
	gin.SetMode(gin.TestMode)
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{Logger: logger.Default.LogMode(logger.Silent)})
	if err != nil {
		t.Fatal(err)
	}
	if err := db.AutoMigrate(&models.Project{}, &models.Character{}); err != nil {
		t.Fatal(err)
	}
	p := models.Project{Title: "p", Synopsis: "s"}
	db.Create(&p)
	ch := models.Character{ProjectID: p.ID, Name: "林夏", ProfileStatus: models.ProfileStatusDraft}
	db.Create(&ch)
	w := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(w)
	c.Params = gin.Params{{Key: "id", Value: strconv.FormatUint(uint64(p.ID), 10)}, {Key: "cid", Value: strconv.FormatUint(uint64(ch.ID), 10)}}
	svc := &Service{DB: db, Projects: &ProjectService{db: db}}
	svc.HandleGenerateCharacterPortrait(c)
	if w.Code != 409 {
		t.Fatalf("档案未审核应返回 409，实际 %d: %s", w.Code, w.Body.String())
	}
}

func dialogueCandidateHandlerFixture(t *testing.T) (*gorm.DB, *Service, models.Project, models.Dialogue, models.DialogueAudioCandidate) {
	t.Helper()
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{Logger: logger.Default.LogMode(logger.Silent)})
	if err != nil {
		t.Fatal(err)
	}
	if err := db.AutoMigrate(&models.Project{}, &models.Dialogue{}, &models.DialogueAudioCandidate{}); err != nil {
		t.Fatal(err)
	}
	project := models.Project{Title: "candidate-handler"}
	db.Create(&project)
	dialogue := models.Dialogue{ProjectID: project.ID, Text: "当前对白", Character: "甲", SpeechType: "dialogue", Speed: 1, Volume: 1}
	db.Create(&dialogue)
	projects := &ProjectService{db: db}
	row := models.DialogueAudioCandidate{ProjectID: project.ID, DialogueID: dialogue.ID, InputHash: projects.effectiveDialogueAudioHash(dialogue), File: "take.wav", FileSHA256: strings.Repeat("a", 64), FileSize: 128, ReviewStatus: "pending"}
	db.Create(&row)
	return db, &Service{DB: db, Projects: projects}, project, dialogue, row
}

func dialogueCandidateContext(body string, projectID, dialogueID, candidateID uint) (*gin.Context, *httptest.ResponseRecorder) {
	w := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(w)
	c.Request = httptest.NewRequest(http.MethodPatch, "/", bytes.NewBufferString(body))
	c.Request.Header.Set("Content-Type", "application/json")
	c.Params = gin.Params{{Key: "id", Value: strconv.FormatUint(uint64(projectID), 10)}, {Key: "did", Value: strconv.FormatUint(uint64(dialogueID), 10)}, {Key: "cid", Value: strconv.FormatUint(uint64(candidateID), 10)}}
	return c, w
}

func TestReviewDialogueAudioCandidateConflictReturns409(t *testing.T) {
	gin.SetMode(gin.TestMode)
	db, svc, project, dialogue, row := dialogueCandidateHandlerFixture(t)
	if err := db.Model(&dialogue).Update("text", "已修改对白").Error; err != nil {
		t.Fatal(err)
	}
	body := `{"status":"accepted","expected_input_hash":"` + row.InputHash + `","expected_file_sha256":"` + row.FileSHA256 + `"}`
	c, w := dialogueCandidateContext(body, project.ID, dialogue.ID, row.ID)
	svc.HandleReviewDialogueAudioCandidate(c)
	if w.Code != http.StatusConflict {
		t.Fatalf("status=%d body=%s", w.Code, w.Body.String())
	}
	db.First(&row, row.ID)
	if row.ReviewStatus != "pending" || row.ReviewConfirmation != "" {
		t.Fatalf("row mutated: %+v", row)
	}
}

func TestSelectDialogueAudioCandidateConflictReturns409(t *testing.T) {
	gin.SetMode(gin.TestMode)
	_, svc, project, dialogue, row := dialogueCandidateHandlerFixture(t)
	body := `{"expected_input_hash":"` + row.InputHash + `","expected_file_sha256":"` + row.FileSHA256 + `"}`
	c, w := dialogueCandidateContext(body, project.ID, dialogue.ID, row.ID)
	svc.HandleSelectDialogueAudioCandidate(c)
	if w.Code != http.StatusConflict {
		t.Fatalf("status=%d body=%s", w.Code, w.Body.String())
	}
}

func TestLoadSceneRejectsCrossProjectScene(t *testing.T) {
	gin.SetMode(gin.TestMode)
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{Logger: logger.Default.LogMode(logger.Silent)})
	if err != nil {
		t.Fatal(err)
	}
	if err := db.AutoMigrate(&models.Project{}, &models.Scene{}); err != nil {
		t.Fatal(err)
	}
	p1 := models.Project{Title: "p1", Synopsis: "s"}
	p2 := models.Project{Title: "p2", Synopsis: "s"}
	db.Create(&p1)
	db.Create(&p2)
	scene := models.Scene{ProjectID: p2.ID, Order: 1, Status: "pending"}
	db.Create(&scene)

	w := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(w)
	c.Params = gin.Params{
		{Key: "id", Value: strconv.FormatUint(uint64(p1.ID), 10)},
		{Key: "sid", Value: strconv.FormatUint(uint64(scene.ID), 10)},
	}
	svc := &Service{DB: db}
	if got, ok := svc.loadScene(c); ok || got != nil {
		t.Fatalf("跨项目场景不应加载成功: %+v", got)
	}
	if w.Code != 404 {
		t.Fatalf("状态码 = %d", w.Code)
	}
}
