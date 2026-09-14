package service

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"
)

// TextProvider 文生文 provider 接口（抽象火山 / llama.cpp / OpenAI 等多种后端）
type TextProvider interface {
	// Name 返回 provider 标识名（用于日志和配置显示）
	Name() string
	// Chat 文生文：system 为系统提示词，user 为用户输入，返回模型文本
	Chat(system, user string) (string, error)
	// HealthCheck 检测 provider 连通性
	HealthCheck(ctx context.Context) error
}

// ---------- Llama.cpp (OpenAI-compatible) Provider ----------

// LlamaProviderConfig llama.cpp 服务器配置
type LlamaProviderConfig struct {
	BaseURL string `json:"base_url"` // 例如 http://localhost:8080/v1
	Model   string `json:"model"`
	APIKey  string `json:"-"` // 本地 llama.cpp 可为空；在线 OpenAI 兼容服务使用 Bearer 鉴权
}

// Configured 判断配置是否完整
func (c LlamaProviderConfig) Configured() bool {
	return c.BaseURL != "" && c.Model != ""
}

// LlamaProvider 本地 llama.cpp OpenAI-compatible 服务器客户端
type LlamaProvider struct {
	db           IGetter
	httpc        *http.Client
	name         string
	baseURLKey   string
	modelKey     string
	apiKeyKey    string
	defaultURL   string
	defaultModel string
}

// IGetter 设置读取接口（兼容 VolcClient 的 db 方式）
type IGetter interface {
	GetSetting(key, def string) string
}

// NewLlamaProvider 创建 llama provider
func NewLlamaProvider(db IGetter) *LlamaProvider {
	return newOpenAICompatibleProvider(db, "llama.cpp", SettingLlamaBaseURL, SettingLlamaModel, "", DefaultLlamaBaseURL, DefaultLlamaModel)
}

func NewMiniMaxCodingProvider(db IGetter) *LlamaProvider {
	return newOpenAICompatibleProvider(db, "MiniMax Coding Plan", SettingMiniMaxBaseURL, SettingMiniMaxModel, SettingMiniMaxAPIKey, "", DefaultMiniMaxModel)
}

func newOpenAICompatibleProvider(db IGetter, name, baseURLKey, modelKey, apiKeyKey, defaultURL, defaultModel string) *LlamaProvider {
	return &LlamaProvider{db: db, httpc: &http.Client{Timeout: 180 * time.Second}, name: name,
		baseURLKey: baseURLKey, modelKey: modelKey, apiKeyKey: apiKeyKey, defaultURL: defaultURL, defaultModel: defaultModel}
}

// Setting keys for llama provider
const (
	SettingLlamaBaseURL = "llama_base_url"
	SettingLlamaModel   = "llama_model"
	SettingTextProvider = "text_provider" // "volcano" | "llama"
)

const (
	DefaultLlamaBaseURL = "http://localhost:8080/v1"
	DefaultLlamaModel   = "qwen3.5-9b"
	DefaultTextProvider = "volcano" // 默认使用火山引擎

	SettingMiniMaxAPIKey  = "minimax_coding_api_key"
	SettingMiniMaxBaseURL = "minimax_coding_base_url"
	SettingMiniMaxModel   = "minimax_coding_model"
	DefaultMiniMaxModel   = "MiniMax-M2.5"
)

// Config 从平台设置读取 llama.cpp 配置
func (l *LlamaProvider) Config() LlamaProviderConfig {
	get := func(key, def string) string {
		if l.db != nil {
			if v := l.db.GetSetting(key, ""); v != "" {
				return v
			}
		}
		return def
	}
	apiKey := ""
	if l.apiKeyKey != "" {
		apiKey = get(l.apiKeyKey, "")
	}
	return LlamaProviderConfig{
		BaseURL: get(l.baseURLKey, l.defaultURL),
		Model:   get(l.modelKey, l.defaultModel),
		APIKey:  apiKey,
	}
}

func (l *LlamaProvider) Name() string {
	return l.name
}

// OpenAI-compatible request/response types
type openAIChatReq struct {
	Model          string                `json:"model"`
	Messages       []openAIMessage       `json:"messages"`
	Stream         bool                  `json:"stream"`
	ResponseFormat *openAIResponseFormat `json:"response_format,omitempty"`
}

type openAIResponseFormat struct {
	Type string `json:"type"`
}

type openAIMessage struct {
	Role    string `json:"role"`
	Content string `json:"content"`
}

type openAIChatResp struct {
	Choices []openAIChoice `json:"choices"`
	Error   *openAIError   `json:"error"`
}

type openAIChoice struct {
	Message openAIMessage `json:"message"`
}

type openAIMessageContent struct {
	Type string `json:"type"`
	Text string `json:"text"`
}

type openAIError struct {
	Message string `json:"message"`
	Type    string `json:"type"`
	Code    string `json:"code"`
}

const llamaJSONDiscipline = `

【JSON 严格输出规则】
- 所有自然语言内容（包括中文姓名、对白和场景描述）必须完整包含在 JSON 双引号字符串内部；姓名中的任何字都只是普通字符串内容。
- 字符串闭合后只能出现逗号、右花括号或右方括号，禁止在闭合引号后重复姓名、对白或任何其他字符。
- 字符串内部禁止直接换行；需要换行时必须输出转义序列 \\n。双引号和反斜杠也必须正确转义。
- 禁止尾随逗号、注释、Markdown、思考过程和 JSON 对象之外的文本。
- 输出前在内部逐字符检查：结果必须能被标准 JSON.parse 完整解析。最终只输出 JSON 对象。`

func requestsJSONObject(system string) bool {
	lower := strings.ToLower(system)
	return strings.Contains(lower, "json") && (strings.Contains(system, "只输出") || strings.Contains(lower, "output json") || strings.Contains(lower, "json object"))
}

// Chat 文生文：调用 OpenAI-compatible 接口
func (l *LlamaProvider) Chat(system, user string) (string, error) {
	var lastErr error
	for attempt := 0; attempt < 2; attempt++ {
		if attempt > 0 {
			time.Sleep(time.Second)
		}
		text, err := l.chatOnce(system, user)
		if err == nil {
			return text, nil
		}
		lastErr = err
	}
	return "", lastErr
}

func (l *LlamaProvider) chatOnce(system, user string) (string, error) {
	cfg := l.Config()
	if !cfg.Configured() {
		return "", fmt.Errorf("尚未配置 llama.cpp 服务器地址和模型名，请在「平台设置」中填写")
	}

	wantsJSON := l.name == "llama.cpp" && requestsJSONObject(system)
	if wantsJSON {
		system += llamaJSONDiscipline
	}
	messages := []openAIMessage{}
	if system != "" {
		messages = append(messages, openAIMessage{Role: "system", Content: system})
	}
	messages = append(messages, openAIMessage{Role: "user", Content: user})

	requestBody := openAIChatReq{Model: cfg.Model, Messages: messages, Stream: false}
	if wantsJSON {
		// llama.cpp maps OpenAI JSON mode to a grammar-constrained sampler. This
		// prevents ordinary Chinese names such as 雷/舒 from escaping a JSON string.
		requestBody.ResponseFormat = &openAIResponseFormat{Type: "json_object"}
	}
	body, _ := json.Marshal(requestBody)

	base := cfg.BaseURL
	base = strings.TrimRight(base, "/")
	endpoint := base + "/chat/completions"

	req, err := http.NewRequest("POST", endpoint, bytes.NewReader(body))
	if err != nil {
		return "", err
	}
	req.Header.Set("Content-Type", "application/json")
	if cfg.APIKey != "" {
		req.Header.Set("Authorization", "Bearer "+cfg.APIKey)
	}

	resp, err := l.httpc.Do(req)
	if err != nil {
		return "", fmt.Errorf("调用 llama.cpp 接口失败: %w", err)
	}
	defer resp.Body.Close()

	data, _ := io.ReadAll(resp.Body)
	if resp.StatusCode != 200 {
		return "", fmt.Errorf("llama.cpp 接口返回 %d: %s", resp.StatusCode, truncateStr(string(data), 300))
	}

	var out openAIChatResp
	if err := json.Unmarshal(data, &out); err != nil {
		return "", fmt.Errorf("解析 llama.cpp 响应失败: %w", err)
	}
	if out.Error != nil {
		return "", fmt.Errorf("llama.cpp 接口错误: %s", out.Error.Message)
	}
	if len(out.Choices) == 0 {
		return "", fmt.Errorf("llama.cpp 未返回内容（原始响应: %s）", truncateStr(string(data), 200))
	}
	content := stripThinkBlocks(out.Choices[0].Message.Content)
	if content == "" {
		return "", fmt.Errorf("llama.cpp 仅返回了思考内容，未返回最终答案")
	}
	return content, nil
}

// stripThinkBlocks 清除 Qwen 推理模型可能混入 content 的 <think>...</think>，避免干扰 JSON 解析。
func stripThinkBlocks(content string) string {
	content = strings.TrimSpace(content)
	for {
		start := strings.Index(content, "<think>")
		if start < 0 {
			break
		}
		endRel := strings.Index(content[start+len("<think>"):], "</think>")
		if endRel < 0 {
			content = strings.TrimSpace(content[:start])
			break
		}
		end := start + len("<think>") + endRel + len("</think>")
		content = strings.TrimSpace(content[:start] + content[end:])
	}
	return content
}

// HealthCheck 检测 llama.cpp 连通性
func (l *LlamaProvider) HealthCheck(ctx context.Context) error {
	cfg := l.Config()
	if !cfg.Configured() {
		return fmt.Errorf("未配置 llama.cpp 服务器")
	}
	base := strings.TrimRight(cfg.BaseURL, "/")
	base = strings.TrimSuffix(base, "/v1")
	healthURL := base + "/health"

	req, err := http.NewRequestWithContext(ctx, "GET", healthURL, nil)
	if err != nil {
		return err
	}
	resp, err := l.httpc.Do(req)
	if err != nil {
		return fmt.Errorf("llama.cpp 服务器连接失败: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != 200 {
		return fmt.Errorf("llama.cpp 服务器返回 %d", resp.StatusCode)
	}
	return nil
}

// TestText 测试 llama.cpp 文生文连通性
func (l *LlamaProvider) TestText() (string, error) {
	text, err := l.Chat("你是连接测试助手。", "请只回复两个字：正常")
	if err != nil {
		return "", err
	}
	return strings.TrimSpace(text), nil
}

// ---------- TextProviderFactory: 根据配置动态选择 provider ----------

// TextProviderFactory 文生文 provider 工厂，根据设置动态创建 provider
type TextProviderFactory struct {
	getter     IGetter // 读取配置
	volcGetter IGetter // VolcClient 的 db 方式（向后兼容）
}

// NewTextProviderFactory 创建 provider 工厂
func NewTextProviderFactory(getter IGetter) *TextProviderFactory {
	return &TextProviderFactory{getter: getter, volcGetter: getter}
}

// Name 返回当前配置选中的 provider 名称。
func (f *TextProviderFactory) Name() string {
	return f.Create().Name()
}

// Chat 每次请求时读取当前 provider 配置，因此设置切换后无需重启或替换 ProjectService 引用。
func (f *TextProviderFactory) Chat(system, user string) (string, error) {
	return f.Create().Chat(system, user)
}

// HealthCheck 检查当前配置选中的 provider。
func (f *TextProviderFactory) HealthCheck(ctx context.Context) error {
	return f.Create().HealthCheck(ctx)
}

// Create 返回当前选中的 TextProvider
func (f *TextProviderFactory) Create() TextProvider {
	provider := f.getter.GetSetting(SettingTextProvider, DefaultTextProvider)
	switch provider {
	case "llama":
		return NewLlamaProvider(f.getter)
	case "minimax_coding":
		return NewMiniMaxCodingProvider(f.getter)
	case "volcano", "":
		// VolcClient 也实现 TextProvider 接口（通过适配）
		return &VolcTextAdapter{volc: NewVolcClientFromGetter(f.volcGetter)}
	default:
		// 未知 provider，回退到火山引擎
		return &VolcTextAdapter{volc: NewVolcClientFromGetter(f.volcGetter)}
	}
}

// VolcTextAdapter 将 VolcClient 适配为 TextProvider 接口
type VolcTextAdapter struct {
	volc *VolcClient
}

func (a *VolcTextAdapter) Name() string {
	return "火山引擎 Ark"
}

func (a *VolcTextAdapter) Chat(system, user string) (string, error) {
	return a.volc.Chat(system, user)
}

func (a *VolcTextAdapter) HealthCheck(ctx context.Context) error {
	cfg := a.volc.Config()
	if !cfg.Configured() {
		return fmt.Errorf("未配置火山引擎 API Key")
	}
	// 通过 TestText 简单验证连通性
	_, err := a.volc.TestText()
	return err
}

// NewVolcClientWithGetter 从 IGetter 创建 VolcClient（用于 TextProviderFactory）
func NewVolcClientWithGetter(getter IGetter) *VolcClient {
	return &VolcClient{
		getter: getter,
		httpc:  &http.Client{Timeout: 120 * time.Second},
	}
}

// Alias for backward compatibility
var NewVolcClientFromGetter = NewVolcClientWithGetter
