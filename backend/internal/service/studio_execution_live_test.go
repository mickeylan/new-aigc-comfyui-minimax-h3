package service

import (
	"encoding/json"
	"os"
	"path/filepath"
	"regexp"
	"testing"

	"comfyui-console/internal/models"
	"github.com/gin-gonic/gin"
	"github.com/glebarez/sqlite"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"
)

// Explicit opt-in only: reads provider settings from a read-only DB connection;
// all source fixtures, Skill configuration and audit writes stay in the test DB.
// No ComfyUI, task creation, business source data or credentials in artifacts.
func TestStudioExecutionLiveTextPlanning(t *testing.T) {
	if os.Getenv("COMFYUI_CONSOLE_LIVE_EXECUTION") != "1" {
		t.Skip("opt-in real text model planning; no ComfyUI")
	}
	settings, err := gorm.Open(sqlite.Open("file:../../data/console.db?mode=ro"), &gorm.Config{Logger: logger.Default.LogMode(logger.Silent)})
	if err != nil {
		t.Fatal("read-only provider settings unavailable")
	}
	pool, err := settings.DB()
	if err != nil {
		t.Fatal("read-only settings connection unavailable")
	}
	defer pool.Close()
	// query_only is connection-local: keep all provider reads on this guarded connection.
	pool.SetMaxOpenConns(1)
	if err := settings.Exec("PRAGMA query_only=ON").Error; err != nil {
		t.Fatal("read-only settings guard failed")
	}
	getter := NewVolcClient(settings)
	factory := NewTextProviderFactory(getter)
	s, db, scene, shots := newLiveExecutionTestService(t)
	s.TextProviderFact = factory
	// Public-domain novel excerpt, checked at https://zh.wikisource.org/wiki/西遊記/第014回.
	// This is an execution-planning probe on human-authored cuts,
	// NOT a pass for automatic novel analysis/director generation or the full chain.
	content := "三藏見他意思，實有好心，真個像沙門中的人物，便叫：「徒弟啊，你姓甚麼？」猴王道：「我姓孫。」三藏道：「我與你起個法名，卻好呼喚。」猴王道：「不勞師父盛意，我原有個法名，叫做孫悟空。」"
	if err := db.Where("scene_id = ?", scene.ID).Delete(&models.Dialogue{}).Error; err != nil {
		t.Fatal(err)
	}
	if err := db.Model(&scene).Updates(map[string]any{"content": content, "characters": "三藏,孫悟空", "character_roles_set": true, "visible_characters": "三藏,孫悟空"}).Error; err != nil {
		t.Fatal(err)
	}
	for _, shot := range shots {
		if err := db.Model(&shot).Updates(map[string]any{"description": content, "prompt_action": "", "dialogue": "", "blocking": "", "performance": "", "camera_movement": "固定"}).Error; err != nil {
			t.Fatal(err)
		}
	}
	ctx := studioExecutionContext{Revision: "live-text-proxy-context-v1", Assets: []studioExecutionAsset{}, Objects: []studioExecutionObject{}}
	for i, name := range []string{"三藏", "孫悟空"} {
		id := []string{"tripitaka", "wukong"}[i]
		// These are explicit unanimated proxies, not asserted to be production rigs.
		ctx.Assets = append(ctx.Assets, studioExecutionAsset{ID: "object:" + id, Name: name, Clips: []studioExecutionClip{}})
		ctx.Objects = append(ctx.Objects, studioExecutionObject{ID: id, AssetID: "object:" + id, Name: name, Position: []float64{float64(i*6 - 3), 0, 0}, Rotation: []float64{0, 0, 0}, Scale: []float64{1, 1, 1}})
	}
	source, hash, err := s.studioExecutionSource(scene.ID)
	if err != nil {
		t.Fatal(err)
	}
	response := executionHTTP(t, s, scene.ProjectID, scene.ID, "POST", gin.H{"source_hash": hash, "context": ctx})
	if response.Code != 200 {
		// Capture only safe status/category before independent postcondition checks.
		t.Logf("LIVE_TEXT_RESPONSE http=%d category=%s (body withheld)", response.Code, liveExecutionFailureCategory(response.Code, response.Body.Bytes()))
	}
	var result struct {
		Plan       studioExecutionPlan `json:"plan"`
		Provenance *SkillProvenance    `json:"skill_provenance"`
	}
	after, afterHash, err := s.studioExecutionSource(scene.ID)
	if err != nil || afterHash != hash {
		t.Fatal("planning changed isolated source")
	}
	beforeJSON, _ := json.Marshal(source)
	afterJSON, _ := json.Marshal(after)
	if string(beforeJSON) != string(afterJSON) {
		t.Fatal("source changed")
	}
	var tasks int64
	if err := db.Model(&models.Task{}).Count(&tasks).Error; err != nil || tasks != 0 {
		t.Fatal("planning must not create tasks")
	}
	if response.Code != 200 {
		// Only fixed local categories are printable; never echo a body, even for 422.
		t.Fatalf("LIVE_TEXT_EXECUTION=FAIL http=%d category=%s source_unchanged=true tasks=0 (body withheld)", response.Code, liveExecutionFailureCategory(response.Code, response.Body.Bytes()))
	}
	if err := json.Unmarshal(response.Body.Bytes(), &result); err != nil || result.Provenance == nil {
		t.Fatal("real text planning missing validated provenance")
	}
	// Artifact contains only typed, validated model plan + synthetic source/proxies,
	// never factory/getter/settings/audit error text. Fixed workspace-local path.
	artifact := struct {
		Scope      string                 `json:"scope"`
		Source     studioExecutionSource  `json:"source"`
		Context    studioExecutionContext `json:"context"`
		Plan       studioExecutionPlan    `json:"plan"`
		Provenance *SkillProvenance       `json:"skill_provenance"`
	}{"real text execution planning only; public-domain excerpt; hand-authored cuts; unanimated proxies; NOT full chain acceptance", source, ctx, result.Plan, result.Provenance}
	raw, err := json.MarshalIndent(artifact, "", "  ")
	if err != nil {
		t.Fatal(err)
	}
	path := filepath.Join("..", "..", "..", ".rscode", "live-novel-execution-plan.json")
	if err := os.WriteFile(path, raw, 0600); err != nil {
		t.Fatal("cannot save non-sensitive planning artifact")
	}
	t.Logf("LIVE_TEXT_EXECUTION=PASS provider=%q cuts=%d tasks=0 persisted=false artifact=live-novel-execution-plan.json; full novel/director/browser/model-video chain NOT accepted", factory.Name(), len(result.Plan.Shots))
}

func newLiveExecutionTestService(t *testing.T) (*Service, *gorm.DB, models.Scene, []models.Shot) {
	t.Helper()
	s, db, scene, shots := newShotRefineTestService(t, "")
	// The director fixture intentionally has no task schema. Install it only in
	// this in-memory diagnostic DB, then prove the assertion is usable BEFORE AI.
	if err := db.AutoMigrate(&models.Task{}); err != nil {
		t.Fatal("isolated task schema unavailable")
	}
	var tasks int64
	if err := db.Model(&models.Task{}).Count(&tasks).Error; err != nil || tasks != 0 {
		t.Fatal("isolated task baseline invalid")
	}
	return s, db, scene, shots
}

func TestLiveExecutionFixtureRejectsWithoutTasks(t *testing.T) {
	s, db, scene, _ := newLiveExecutionTestService(t)
	_, hash, err := s.studioExecutionSource(scene.ID)
	if err != nil {
		t.Fatal(err)
	}
	useExecutionProvider(t, s, &captureTextProvider{response: `{"unexpected":"private-sentinel"}`})
	response := executionHTTP(t, s, scene.ProjectID, scene.ID, "POST", gin.H{"source_hash": hash, "context": executionContextFixture()})
	if response.Code != 422 || liveExecutionFailureCategory(response.Code, response.Body.Bytes()) != "json_schema" {
		t.Fatal("expected controlled JSON-schema rejection")
	}
	var tasks int64
	if err := db.Model(&models.Task{}).Count(&tasks).Error; err != nil || tasks != 0 {
		t.Fatal("task postcondition failed")
	}
	_, afterHash, err := s.studioExecutionSource(scene.ID)
	if err != nil || afterHash != hash {
		t.Fatal("controlled rejection changed source")
	}
}

// This diagnostic allowlist is test-only. Unknown content must never reach logs.
func liveExecutionFailureCategory(status int, body []byte) string {
	if status != 422 {
		return "non_validation_response"
	}
	var response struct {
		Error string `json:"error"`
	}
	if json.Unmarshal(body, &response) != nil {
		return "unclassified"
	}
	categories := map[string]string{
		"执行规划响应超过1MiB":        "response_size",
		"执行规划返回了未知字段或无效JSON":  "json_schema",
		"模型来源或目录修订不匹配":        "source_context_identity",
		"执行草稿结构或镜头覆盖无效":       "structure_shot_coverage",
		"布景对象或真实素材引用无效":       "object_catalog_reference",
		"不能替换已有对象素材":          "asset_replacement",
		"人物绑定重复或引用不存在":        "character_binding",
		"整份执行计划超过32个角色，请拆分场次": "plan_actor_limit",
		"镜内执行角色不能与旧走位混用于同一计划": "plan_mixed_motion",
		"镜内执行不能混用旧走位":         "shot_mixed_motion",
		"机位无效":                "camera_invalid", "机位与焦点重合": "camera_coincident",
		"表演对象、走位或时长无效":     "legacy_motion_invalid",
		"当前执行结构不支持原地动作":    "legacy_in_place_motion",
		"表演引用的动作不在该对象资产目录": "legacy_clip_reference",
		"镜内执行版本或规模无效":      "track_version_size",
		"机位轨道必须覆盖本镜完整时间":   "camera_time_coverage",
		"机位键无效":            "camera_key_invalid", "机位键时间不递增": "camera_key_order",
		"机位轨道内部视点与焦点重合":     "camera_track_coincident",
		"执行角色不是实际可用对象":      "track_object_reference",
		"执行角色未绑定可见人物":       "track_character_binding",
		"执行动作段超过128":        "beat_limit",
		"执行动作段区间或位置无效":      "beat_interval_position",
		"同角色动作段重叠或位置不连续":    "beat_overlap_continuity",
		"执行动作不属于当前对象真实clip": "track_clip_reference",
		"缺少原动作段索引":          "source_index_missing",
		"执行段改变了原动作主体或区间":    "source_subject_interval",
		"原镜头无动作段，不能虚构来源索引":  "source_index_invented",
		"动作时间轴的主体或区间尚未完整绑定": "source_subject_coverage",
	}
	if category, ok := categories[response.Error]; ok {
		return category
	}
	if regexp.MustCompile(`^第[0-9]+镜身份、时长或依据无效$`).MatchString(response.Error) {
		return "shot_identity_duration_evidence"
	}
	if regexp.MustCompile(`^第[0-9]+镜依据不在原文或导演字段中$`).MatchString(response.Error) {
		return "evidence_not_verbatim"
	}
	return "unclassified"
}

func TestLiveExecutionFailureCategoryWithholdsUntrustedText(t *testing.T) {
	cases := []struct {
		status     int
		body, want string
	}{
		{422, `{"error":"执行规划返回了未知字段或无效JSON"}`, "json_schema"},
		{422, `{"error":"第12镜依据不在原文或导演字段中"}`, "evidence_not_verbatim"},
		{422, `{"error":"执行动作不属于当前对象真实clip"}`, "track_clip_reference"},
		{422, `{"error":"private-url-and-token-sentinel"}`, "unclassified"},
		{422, `{"error":"第1镜依据不在原文或导演字段中 private-sentinel"}`, "unclassified"},
		{422, `not-json-private-sentinel`, "unclassified"},
		{502, `{"error":"执行动作不属于当前对象真实clip private-sentinel"}`, "non_validation_response"},
	}
	for _, tc := range cases {
		if got := liveExecutionFailureCategory(tc.status, []byte(tc.body)); got != tc.want {
			t.Fatalf("classification mismatch: got %q want %q", got, tc.want)
		}
	}
}
