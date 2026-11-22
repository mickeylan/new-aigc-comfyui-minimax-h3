package service

import (
	"bytes"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"

	"comfyui-console/internal/models"
	"github.com/gin-gonic/gin"
)

// TestScene3DRefVideoWiring 场景选了 3D 参考视频时：模板走 ref2v_3d，文件进 ref_videos。
func TestScene3DRefVideoWiring(t *testing.T) {
	if !isH3Ref2VTemplate(h3Ref2V3DTemplate) {
		t.Fatalf("%s must be treated as an H3 Ref2V template", h3Ref2V3DTemplate)
	}
	// 没选视频：不能凭空造出 ref_videos（模板里那两个节点会被裁掉）。
	empty := &models.Scene{}
	if scene3DRefVideo(empty) != "" || scene3DRefVideoFiles(empty) != nil {
		t.Fatal("scene without 3D reference video must not produce ref_videos files")
	}
	if scene3DRefVideo(nil) != "" {
		t.Fatal("nil scene must be safe")
	}

	scene := &models.Scene{Video3DRefVideo: "  clay-seq-20261122-174038.mp4  "}
	if got := scene3DRefVideo(scene); got != "clay-seq-20261122-174038.mp4" {
		t.Fatalf("scene3DRefVideo = %q", got)
	}
	files := scene3DRefVideoFiles(scene)
	refs := files["ref_videos"]
	if len(refs) != 1 || refs[0].TaskID != "materials" || refs[0].Name != "clay-seq-20261122-174038.mp4" {
		t.Fatalf("ref_videos files = %+v", files)
	}
}

// TestScene3DRefVideoPromptRoundTrip 3D 参考视频字段要能被提示词接口存取，
// 并且「只换参考视频、正文不动」也算改动（否则新结构会沿用旧英文确认稿）。
func TestScene3DRefVideoPromptRoundTrip(t *testing.T) {
	f := newTranslationHTTPFixture(t)
	const clip = "clay-seq-20261122-174038.mp4"

	put := func(body map[string]any) *httptest.ResponseRecorder {
		raw, _ := json.Marshal(body)
		return translationRequest(f.svc, http.MethodPut, f.p.ID, f.sc.ID, "/master", string(raw))
	}

	// 存一条 3D 参考视频。
	w := put(map[string]any{"master_prompt_zh": translationMaster, "ref3d_video": clip})
	if w.Code != 200 {
		t.Fatalf("put status=%d %s", w.Code, w.Body.String())
	}
	var saved models.Scene
	if err := f.db.First(&saved, f.sc.ID).Error; err != nil {
		t.Fatal(err)
	}
	if saved.Video3DRefVideo != clip {
		t.Fatalf("video_3d_ref_video = %q, want %q", saved.Video3DRefVideo, clip)
	}

	// 读取接口要回显（编辑器打开弹窗靠这个恢复选中项）。
	w = translationRequest(f.svc, http.MethodGet, f.p.ID, f.sc.ID, "", "")
	if w.Code != 200 {
		t.Fatalf("get status=%d %s", w.Code, w.Body.String())
	}
	if got := decodeTranslationBody(t, w)["ref3d_video"]; got != clip {
		t.Fatalf("GET ref3d_video = %#v, want %q", got, clip)
	}

	// 正文一字不改、只清掉参考视频：必须判为改动并把旧英文确认稿作废。
	f.db.Model(&models.Scene{}).Where("id = ?", f.sc.ID).Updates(map[string]any{"prompt_stale": false, "video_exec_confirmed_hash": "stale-hash"})
	w = put(map[string]any{"master_prompt_zh": translationMaster, "ref3d_video": ""})
	if w.Code != 200 {
		t.Fatalf("put clear status=%d %s", w.Code, w.Body.String())
	}
	if err := f.db.First(&saved, f.sc.ID).Error; err != nil {
		t.Fatal(err)
	}
	if saved.Video3DRefVideo != "" {
		t.Fatalf("clearing 3D reference did not persist: %q", saved.Video3DRefVideo)
	}
	if !saved.PromptStale {
		t.Fatal("changing only the 3D reference video must mark the prompt stale")
	}
	if saved.VideoExecConfirmedHash != "" {
		t.Fatalf("stale English confirmation survived: %q", saved.VideoExecConfirmedHash)
	}
}

// TestUpdateSceneVideoPromptStoresRef3D 老编辑器那条 PUT（非双语主稿通路）也要能存/清 3D 参考视频。
func TestUpdateSceneVideoPromptStoresRef3D(t *testing.T) {
	f := newTranslationHTTPFixture(t)
	if err := f.db.AutoMigrate(&models.GenerationCandidate{}); err != nil {
		t.Fatal(err)
	}
	gin.SetMode(gin.TestMode)
	r := gin.New()
	base := "/api/projects/:id/scenes/:sid/video-prompt"
	r.PUT(base, f.svc.HandleUpdateSceneVideoPrompt)
	r.GET(base, f.svc.HandleGetSceneVideoPrompt)
	do := func(method, body string) *httptest.ResponseRecorder {
		q := httptest.NewRequest(method, fmt.Sprintf("/api/projects/%d/scenes/%d/video-prompt", f.p.ID, f.sc.ID), bytes.NewBufferString(body))
		q.Header.Set("Content-Type", "application/json")
		w := httptest.NewRecorder()
		r.ServeHTTP(w, q)
		return w
	}

	const clip = "clay-seq-20261122-174919.mp4"
	w := do(http.MethodPut, `{"prompt":"A person walks across an empty room.","template":"minimax_h3_ref2v","ref3d_video":"`+clip+`"}`)
	if w.Code != 200 {
		t.Fatalf("put status=%d %s", w.Code, w.Body.String())
	}
	if got := decodeTranslationBody(t, w)["ref3d_video"]; got != clip {
		t.Fatalf("PUT response ref3d_video = %#v", got)
	}
	if got := decodeTranslationBody(t, do(http.MethodGet, ""))["ref3d_video"]; got != clip {
		t.Fatalf("GET ref3d_video = %#v", got)
	}

	// 不传 ref3d_video 必须保持原值（指针 nil = 没改）。
	w = do(http.MethodPut, `{"prompt":"A person walks across an empty room.","template":"minimax_h3_ref2v"}`)
	if w.Code != 200 {
		t.Fatalf("put keep status=%d %s", w.Code, w.Body.String())
	}
	if got := decodeTranslationBody(t, w)["ref3d_video"]; got != clip {
		t.Fatalf("omitting ref3d_video must keep it, got %#v", got)
	}

	// 显式空串才清掉。
	w = do(http.MethodPut, `{"prompt":"A person walks across an empty room.","template":"minimax_h3_ref2v","ref3d_video":""}`)
	if w.Code != 200 {
		t.Fatalf("put clear status=%d %s", w.Code, w.Body.String())
	}
	if got := decodeTranslationBody(t, w)["ref3d_video"]; got != "" {
		t.Fatalf("empty ref3d_video must clear it, got %#v", got)
	}
}

// TestH3VideoSelectionAccepts3DTemplate 新模板要能通过模型目录的能力校验（不能一提交就被挡）。
func TestH3VideoSelectionAccepts3DTemplate(t *testing.T) {
	tpl := loadTemplateForTest(t, "minimax_h3_ref2v_3d.json")
	if !isH3Ref2VTemplate(tpl.Code) {
		t.Fatalf("template code %q is not routed as Ref2V", tpl.Code)
	}
	// 模板声明的输入里必须有 videos 类型的 ref_videos，并且允许不传（否则没 3D 视频时会被挡）。
	var declared []TemplateInput
	if err := json.Unmarshal([]byte(tpl.InputsJSON), &declared); err != nil {
		t.Fatal(err)
	}
	found := false
	for _, input := range declared {
		if input.Key != "ref_videos" {
			continue
		}
		found = true
		if input.Type != "videos" {
			t.Fatalf("ref_videos type = %q, want videos", input.Type)
		}
		if input.Required {
			t.Fatal("ref_videos must stay optional so scenes without a 3D reference still submit")
		}
	}
	if !found {
		t.Fatal("ref_videos input missing from template")
	}
}
