package service

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"

	"comfyui-console/internal/models"
)

// Exercise the same language=en HTTP route as 保存中文并翻译英文预览.
// Echoing masked input tests routing/protocol preservation, not translation quality.
func TestSceneVideoTranslationHTTPLongEnglishPreviewChunksAndCaches(t *testing.T) {
	previousCache := h3ChunkTranslationCache
	h3ChunkTranslationCache = newH3TranslationCache()
	t.Cleanup(func() { h3ChunkTranslationCache = previousCache })
	f := newTranslationHTTPFixture(t)
	var calls atomic.Int32
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var request struct {
			Messages []struct {
				Role    string `json:"role"`
				Content string `json:"content"`
			} `json:"messages"`
		}
		if err := json.NewDecoder(r.Body).Decode(&request); err != nil {
			t.Errorf("decode upstream: %v", err)
			http.Error(w, "invalid request", http.StatusBadRequest)
			return
		}
		calls.Add(1)
		for _, message := range request.Messages {
			if message.Role == "user" {
				fmt.Fprintf(w, `{"choices":[{"message":{"content":%q}}]}`, message.Content)
				return
			}
		}
		http.Error(w, "missing user message", http.StatusBadRequest)
	}))
	t.Cleanup(upstream.Close)
	f.svc.TextProviderFact = NewTextProviderFactory(&mockGetter{settings: map[string]string{
		SettingTextProvider: "llama", SettingLlamaBaseURL: upstream.URL, SettingLlamaModel: "mock",
	}})
	source := strings.Replace(translationMaster, "人物立即以正常速度走过空房间。",
		strings.Repeat("人物立即以正常速度走过空房间。", 120)+" <Subject 1> (S1) says <d>[Chinese] 不要动！[Shot 9] 不是镜头标记。</d>", 1)
	if len([]rune(source)) < h3ChunkedTranslationMinRunes {
		t.Fatal("fixture must reach long-prompt threshold")
	}
	request := func(language string) map[string]any {
		t.Helper()
		body, _ := json.Marshal(map[string]string{"language": language, "master_prompt_zh": source})
		w := translationRequest(f.svc, http.MethodPost, f.p.ID, f.sc.ID, "/translate", string(body))
		if w.Code != http.StatusOK {
			t.Fatalf("translate status=%d body=%s", w.Code, w.Body.String())
		}
		out := decodeTranslationBody(t, w)
		if out["preview"] != source || out["source_hash"] == "" || out["language"] != language {
			t.Fatalf("preview/provenance/language changed: %v", out)
		}
		if warnings, ok := out["warnings"]; ok {
			t.Fatalf("echo should preserve every protocol/dialogue marker: %v", warnings)
		}
		return out
	}
	request("en")
	firstCalls := calls.Load()
	if want := int32(len(splitH3TranslationChunks(source))); firstCalls != want || firstCalls <= 1 {
		t.Fatalf("long en must use sections: calls=%d want=%d", firstCalls, want)
	}
	request("en")
	if calls.Load() != firstCalls {
		t.Fatalf("identical en preview missed chunk cache: calls=%d first=%d", calls.Load(), firstCalls)
	}
	request("zh")
	if calls.Load() != 2*firstCalls {
		t.Fatalf("target languages must not share cached translations: calls=%d", calls.Load())
	}
	var saved models.Scene
	if err := f.db.First(&saved, f.sc.ID).Error; err != nil {
		t.Fatal(err)
	}
	if saved.VideoPromptMasterZH != translationMaster || saved.VideoFullPrompt != "old reviewed prompt" {
		t.Fatal("translation preview persisted unconfirmed output")
	}
}
