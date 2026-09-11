package service

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// mockGetter implements IGetter for testing
type mockGetter struct {
	settings map[string]string
}

func (m *mockGetter) GetSetting(key, def string) string {
	if v, ok := m.settings[key]; ok && v != "" {
		return v
	}
	return def
}

func TestLlamaProviderConfigured(t *testing.T) {
	tests := []struct {
		name   string
		config LlamaProviderConfig
		want   bool
	}{
		{"完整配置", LlamaProviderConfig{BaseURL: "http://localhost:8080/v1", Model: "qwen2.5"}, true},
		{"缺少 BaseURL", LlamaProviderConfig{BaseURL: "", Model: "qwen2.5"}, false},
		{"缺少 Model", LlamaProviderConfig{BaseURL: "http://localhost:8080/v1", Model: ""}, false},
		{"空配置", LlamaProviderConfig{}, false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := tt.config.Configured(); got != tt.want {
				t.Errorf("Configured() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestLlamaProviderName(t *testing.T) {
	g := &mockGetter{settings: map[string]string{}}
	p := NewLlamaProvider(g)
	if got := p.Name(); got != "llama.cpp" {
		t.Errorf("Name() = %v, want llama.cpp", got)
	}
}

func TestLlamaProviderChat(t *testing.T) {
	// 模拟 llama.cpp 服务器
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/chat/completions" {
			t.Errorf("unexpected path: %s", r.URL.Path)
		}
		if r.Method != "POST" {
			t.Errorf("unexpected method: %s", r.Method)
		}
		// 返回模拟响应
		w.Write([]byte(`{"choices":[{"message":{"role":"assistant","content":"测试响应"}}],"error":null}`))
	}))
	defer server.Close()

	g := &mockGetter{settings: map[string]string{
		SettingLlamaBaseURL: server.URL,
		SettingLlamaModel:   "test-model",
	}}
	p := NewLlamaProvider(g)

	text, err := p.Chat("system prompt", "user message")
	if err != nil {
		t.Fatalf("Chat() error = %v", err)
	}
	if !strings.Contains(text, "测试响应") {
		t.Errorf("Chat() = %v, want to contain '测试响应'", text)
	}
}

func TestMiniMaxCodingProviderChatUsesBearerAuth(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/v1/chat/completions" {
			t.Errorf("unexpected path: %s", r.URL.Path)
		}
		if got := r.Header.Get("Authorization"); got != "Bearer test-key" {
			t.Errorf("Authorization = %q", got)
		}
		_, _ = w.Write([]byte(`{"choices":[{"message":{"role":"assistant","content":"正常"}}]}`))
	}))
	defer server.Close()

	g := &mockGetter{settings: map[string]string{
		SettingMiniMaxBaseURL: server.URL + "/v1",
		SettingMiniMaxModel:   "MiniMax-M2.5",
		SettingMiniMaxAPIKey:  "test-key",
	}}
	text, err := NewMiniMaxCodingProvider(g).Chat("system", "user")
	if err != nil || text != "正常" {
		t.Fatalf("Chat() = %q, %v", text, err)
	}
}

func TestStripThinkBlocks(t *testing.T) {
	got := stripThinkBlocks("<think>内部推理</think>\n{\"ok\":true}")
	if got != `{"ok":true}` {
		t.Fatalf("stripThinkBlocks() = %q", got)
	}
}

func TestLlamaProviderChatUnconfigured(t *testing.T) {
	g := &mockGetter{settings: map[string]string{}}
	p := NewLlamaProvider(g)

	_, err := p.Chat("system", "user")
	if err == nil {
		t.Error("Chat() expected error for unconfigured provider")
	}
}

func TestLlamaProviderHealthCheck(t *testing.T) {
	// 模拟健康检查端点
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/health" {
			w.WriteHeader(http.StatusOK)
		}
	}))
	defer server.Close()

	g := &mockGetter{settings: map[string]string{
		SettingLlamaBaseURL: server.URL,
		SettingLlamaModel:   "test-model",
	}}
	p := NewLlamaProvider(g)

	ctx := context.Background()
	if err := p.HealthCheck(ctx); err != nil {
		t.Errorf("HealthCheck() error = %v", err)
	}
}

func TestLlamaProviderHealthCheckUnconfigured(t *testing.T) {
	g := &mockGetter{settings: map[string]string{}}
	p := NewLlamaProvider(g)

	ctx := context.Background()
	if err := p.HealthCheck(ctx); err == nil {
		t.Error("HealthCheck() expected error for unconfigured provider")
	}
}

func TestTextProviderFactoryCreate(t *testing.T) {
	g := &mockGetter{settings: map[string]string{}}
	fact := NewTextProviderFactory(g)

	// 测试火山引擎 provider
	g.settings[SettingTextProvider] = "volcano"
	p := fact.Create()
	if p.Name() != "火山引擎 Ark" {
		t.Errorf("expected volcano provider, got %s", p.Name())
	}

	// 测试 llama provider
	g.settings[SettingTextProvider] = "llama"
	g.settings[SettingLlamaBaseURL] = "http://localhost:8080/v1"
	g.settings[SettingLlamaModel] = "qwen2.5"
	p = fact.Create()
	if p.Name() != "llama.cpp" {
		t.Errorf("expected llama provider, got %s", p.Name())
	}

	// 测试未知 provider 回退到火山
	g.settings[SettingTextProvider] = "unknown"
	p = fact.Create()
	if p.Name() != "火山引擎 Ark" {
		t.Errorf("expected fallback to volcano, got %s", p.Name())
	}
}

func TestVolcTextAdapterName(t *testing.T) {
	g := &mockGetter{settings: map[string]string{
		SettingVolcAPIKey: "test-key",
	}}
	volc := NewVolcClientWithGetter(g)
	adapter := &VolcTextAdapter{volc: volc}
	if got := adapter.Name(); got != "火山引擎 Ark" {
		t.Errorf("Name() = %v, want 火山引擎 Ark", got)
	}
}
