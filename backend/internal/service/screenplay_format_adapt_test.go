package service

import (
	"fmt"
	"net/http"
	"strings"
	"testing"

	"comfyui-console/internal/models"
	"comfyui-console/internal/screenplay"
	"github.com/gin-gonic/gin"
	"gorm.io/gorm"
)

const screenplayAdaptSource = "第1场 洞府 夜\n韩立盘坐洞中，灵力缓缓运转。洞外传来脚步声。\n南宫婉（低声）：师弟，宗门出事了。\n韩立：好。我这就去。\n"

const screenplayAdaptGood = ".第1场 洞府 夜\n\n韩立盘坐洞中，灵力缓缓运转。洞外传来脚步声。\n\n南宫婉\n(低声)\n师弟，宗门出事了。\n\n韩立\n好。我这就去。\n\n【格式冲突】\n无"

func newScreenplayAdaptTestService(t *testing.T, llmReply string) (*Service, *gorm.DB, models.Project) {
	t.Helper()
	svc, db, _, _ := newShotRefineTestService(t, llmReply)
	project := models.Project{Title: "adapt", Generation: 1}
	db.Create(&project)
	if err := db.Create(&models.Episode{ProjectID: project.ID, Number: 1, Title: "第一集"}).Error; err != nil {
		t.Fatal(err)
	}
	return svc, db, project
}

func screenplayAdaptRouter(s *Service) *gin.Engine {
	gin.SetMode(gin.TestMode)
	r := gin.New()
	r.POST("/api/projects/:id/episodes/:number/screenplay/adapt", s.HandleAdaptScreenplayImport)
	return r
}

func TestScreenplayFormatAdaptSkillIsSeeded(t *testing.T) {
	db := newTestDB(t)
	if err := NewSkillService(db).InitSystemSkills(); err != nil {
		t.Fatal(err)
	}
	var skill models.Skill
	if err := db.Where("code = ?", screenplayFormatAdaptSkillCode).First(&skill).Error; err != nil {
		t.Fatal(err)
	}
	if skill.Version != 1 || !skill.Enabled || skill.PromptTemplate != "外部剧本原文：{{source_text}}" ||
		!strings.Contains(skill.SystemPrompt, "原文的每一句话都必须原样出现在结果中且只出现一次") || !strings.Contains(skill.SystemPrompt, "导入格式：{{format_spec}}。") {
		t.Fatalf("skill=%+v", skill)
	}
}

func TestCheckScreenplayAdaptationAcceptsFormatOnlyChanges(t *testing.T) {
	result, err := adaptScreenplay(screenplayAdaptSource, screenplayAdaptGood)
	if err != nil {
		t.Fatal(err)
	}
	if len(result.Issues) != 0 || len(result.Warnings) != 0 || len(result.Conflicts) != 0 {
		t.Fatalf("issues=%+v warnings=%v conflicts=%v", result.Issues, result.Warnings, result.Conflicts)
	}
	scene := result.Preview.Scenes[0]
	if scene.Heading != "第1场 洞府 夜" || len(scene.Elements) != 6 || scene.Elements[2].Type != "parenthetical" || scene.Elements[5].Text != "好。我这就去。" {
		t.Fatalf("preview=%+v", result.Preview)
	}
	// No scene marker in the source: the fallback heading and dropped quotes are format shell.
	result, err = adaptScreenplay("韩立推门而入。\n韩立：“师姐，我回来了。”\n", ".场景1\n\n韩立推门而入。\n\n韩立\n师姐，我回来了。\n\n【格式冲突】\n无")
	if err != nil {
		t.Fatal(err)
	}
	if len(result.Issues) != 0 || len(result.Warnings) != 0 || len(result.Preview.Scenes[0].Elements) != 3 {
		t.Fatalf("issues=%+v warnings=%v preview=%+v", result.Issues, result.Warnings, result.Preview)
	}
}

func TestCheckScreenplayAdaptationRejectsContentChanges(t *testing.T) {
	cases := map[string]string{
		"dropped":   strings.Replace(screenplayAdaptGood, "洞外传来脚步声。", "", 1),
		"duplicate": strings.Replace(screenplayAdaptGood, "好。我这就去。", "好。我这就去。我这就去。", 1),
		"reworded":  strings.Replace(screenplayAdaptGood, "宗门出事了", "宗门出大事了", 1),
		"repunct":   strings.Replace(screenplayAdaptGood, "灵力缓缓运转。", "灵力缓缓运转！", 1),
	}
	for name, output := range cases {
		result, err := adaptScreenplay(screenplayAdaptSource, output)
		if err != nil {
			t.Fatalf("%s: %v", name, err)
		}
		if len(result.Issues) == 0 {
			t.Fatalf("%s: expected a content issue", name)
		}
	}
	// Text that only exists in the result is surfaced, and the conflict list is kept.
	added := strings.Replace(screenplayAdaptGood, "【格式冲突】\n无", "雷声大作。\n\n【格式冲突】\n1. 洞外传来脚步声。（归属不确定）", 1)
	result, err := adaptScreenplay(screenplayAdaptSource, added)
	if err != nil {
		t.Fatal(err)
	}
	if len(result.Warnings) != 1 || !strings.Contains(result.Warnings[0], "雷声大作") || len(result.Conflicts) != 1 {
		t.Fatalf("warnings=%v conflicts=%v", result.Warnings, result.Conflicts)
	}
}

func TestScreenplayAdaptEndpointReturnsPreviewWithoutPersisting(t *testing.T) {
	svc, db, project := newScreenplayAdaptTestService(t, screenplayAdaptGood)
	url := fmt.Sprintf("/api/projects/%d/episodes/1/screenplay/adapt", project.ID)
	w := shotRefineCall(screenplayAdaptRouter(svc), http.MethodPost, url, map[string]string{"text": screenplayAdaptSource})
	if w.Code != http.StatusOK || !strings.Contains(w.Body.String(), `"adapted":true`) || !strings.Contains(w.Body.String(), `"scene_count":1`) {
		t.Fatalf("status=%d body=%s", w.Code, w.Body.String())
	}
	var count int64
	db.Model(&models.Scene{}).Where("project_id = ?", project.ID).Count(&count)
	if count != 0 {
		t.Fatalf("adapt must not persist, scenes=%d", count)
	}
}

func TestScreenplayAdaptEndpointRejectsChangedContent(t *testing.T) {
	svc, _, project := newScreenplayAdaptTestService(t, strings.Replace(screenplayAdaptGood, "宗门出事了", "宗门出大事了", 1))
	url := fmt.Sprintf("/api/projects/%d/episodes/1/screenplay/adapt", project.ID)
	w := shotRefineCall(screenplayAdaptRouter(svc), http.MethodPost, url, map[string]string{"text": screenplayAdaptSource})
	if w.Code != http.StatusUnprocessableEntity || !strings.Contains(w.Body.String(), `"issues"`) || !strings.Contains(w.Body.String(), "宗门出事了") {
		t.Fatalf("status=%d body=%s", w.Code, w.Body.String())
	}
}

func TestScreenplayImportKeepsActionInSceneWithoutShots(t *testing.T) {
	db := newTestDBWithNewModels(t)
	if err := db.AutoMigrate(&models.Dialogue{}, &models.ScriptRevision{}); err != nil {
		t.Fatal(err)
	}
	project := models.Project{Title: "Project", Generation: 1}
	db.Create(&project)
	preview := screenplay.Preview{Scenes: []screenplay.Scene{{Heading: "第1场 洞府 夜", Elements: []screenplay.Element{
		{Type: "action", Text: "韩立盘坐洞中。"}, {Type: "transition", Text: "切至："}, {Type: "character", Text: "韩立"}, {Type: "dialogue", Text: "该走了。"},
	}}}}
	if err := db.Transaction(func(tx *gorm.DB) error { return previewToDatabase(tx, &project, 1, preview) }); err != nil {
		t.Fatal(err)
	}
	var scene models.Scene
	if err := db.Where("project_id = ?", project.ID).First(&scene).Error; err != nil {
		t.Fatal(err)
	}
	var shots int64
	db.Model(&models.Shot{}).Where("scene_id = ?", scene.ID).Count(&shots)
	if shots != 0 || scene.Content != "韩立盘坐洞中。\n\n切至：" || scene.ShotCount != 0 {
		t.Fatalf("shots=%d scene=%+v", shots, scene)
	}
}
