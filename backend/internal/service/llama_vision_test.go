package service

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"sync/atomic"
	"testing"
)

type visionSettings map[string]string

func (s visionSettings) GetSetting(key, def string) string {
	if value, ok := s[key]; ok {
		return value
	}
	return def
}

func TestLlamaVisionSendsDataURL(t *testing.T) {
	var called atomic.Bool
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		called.Store(true)
		var body struct {
			Messages []struct {
				Content json.RawMessage `json:"content"`
			} `json:"messages"`
		}
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			t.Fatal(err)
		}
		payload := string(body.Messages[len(body.Messages)-1].Content)
		if !strings.Contains(payload, `"type":"image_url"`) || !strings.Contains(payload, `data:image/png;base64,`) {
			t.Fatalf("missing image data URL: %s", payload)
		}
		if strings.Contains(payload, "frame.png") {
			t.Fatal("request leaked filename")
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"choices":[{"message":{"role":"assistant","content":"ok"}}]}`))
	}))
	defer server.Close()

	image := t.TempDir() + "/frame.png"
	// DetectContentType only needs a valid PNG signature for this transport test.
	if err := os.WriteFile(image, append([]byte("\x89PNG\r\n\x1a\n"), make([]byte, 504)...), 0600); err != nil {
		t.Fatal(err)
	}
	provider := NewLlamaProvider(visionSettings{SettingLlamaVisionEnabled: "true", SettingLlamaBaseURL: server.URL, SettingLlamaModel: "vision"})
	if got, err := provider.ChatWithImage("system", "describe", image); err != nil || got != "ok" {
		t.Fatalf("got %q, err %v", got, err)
	}
	if !called.Load() {
		t.Fatal("HTTP server was not called")
	}
}

func TestLlamaVisionCapabilityGatesProvider(t *testing.T) {
	disabled := NewLlamaProvider(visionSettings{SettingLlamaVisionEnabled: "false"})
	if disabled.SupportsMultimodal() || TextProviderCapabilities(disabled).MultimodalMessages {
		t.Fatal("disabled llama advertised multimodal")
	}
	if _, err := disabled.ChatWithImage("", "", "missing"); err != ErrMultimodalUnsupported {
		t.Fatalf("expected unsupported, got %v", err)
	}

	minimax := NewMiniMaxCodingProvider(visionSettings{SettingLlamaVisionEnabled: "true"})
	if minimax.SupportsMultimodal() || TextProviderCapabilities(minimax).MultimodalMessages {
		t.Fatal("MiniMax advertised llama vision")
	}
}

func TestLlamaVisionRejectsInvalidImageBeforeHTTP(t *testing.T) {
	var calls atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) { calls.Add(1) }))
	defer server.Close()
	provider := NewLlamaProvider(visionSettings{SettingLlamaVisionEnabled: "true", SettingLlamaBaseURL: server.URL, SettingLlamaModel: "vision"})
	path := t.TempDir() + "/bad.png"
	if err := os.WriteFile(path, []byte("not an image"), 0600); err != nil {
		t.Fatal(err)
	}
	if _, err := provider.ChatWithImage("", "", path); err == nil {
		t.Fatal("expected malformed image rejection")
	}
	if calls.Load() != 0 {
		t.Fatal("HTTP called for malformed image")
	}
}
