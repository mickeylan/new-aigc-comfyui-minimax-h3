package service

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"sort"
	"strings"
	"sync"
	"time"

	"comfyui-console/internal/models"
	"gorm.io/gorm"
)

const (
	AgentToolDomainRead       = "domain_read"
	AgentToolDomainWrite      = "domain_write"
	AgentToolGenerationSubmit = "generation_submit"
)

type AgentToolDescriptor struct {
	Code           string          `json:"code"`
	Name           string          `json:"name"`
	Description    string          `json:"description"`
	Permission     string          `json:"permission"`
	InputSchema    json.RawMessage `json:"input_schema"`
	ReviewRequired bool            `json:"review_required"`
}

type AgentToolRequest struct {
	ProjectID   *uint           `json:"project_id,omitempty"`
	Input       json.RawMessage `json:"input"`
	Approved    bool            `json:"approved"`
	ApprovalRef string          `json:"approval_ref,omitempty"`
}

type AgentDomainTool interface {
	Descriptor() AgentToolDescriptor
	Invoke(context.Context, AgentToolRequest) (any, error)
}

type ControlledAgentToolRegistry struct {
	db      *gorm.DB
	enabled bool
	mu      sync.RWMutex
	tools   map[string]AgentDomainTool
}

func NewControlledAgentToolRegistry(db *gorm.DB, enabled bool) *ControlledAgentToolRegistry {
	return &ControlledAgentToolRegistry{db: db, enabled: enabled, tools: map[string]AgentDomainTool{}}
}

func (r *ControlledAgentToolRegistry) Register(tool AgentDomainTool) error {
	if tool == nil {
		return fmt.Errorf("agent tool is nil")
	}
	d := tool.Descriptor()
	d.Code = strings.TrimSpace(d.Code)
	if !skillPackageCodePattern.MatchString(d.Code) {
		return fmt.Errorf("invalid agent tool code")
	}
	switch d.Permission {
	case AgentToolDomainRead:
	case AgentToolDomainWrite, AgentToolGenerationSubmit:
		if !d.ReviewRequired {
			return fmt.Errorf("write and generation tools must require human review")
		}
	default:
		return fmt.Errorf("permission %q is not allowed; shell, raw files, raw database and unrestricted network are forbidden", d.Permission)
	}
	if len(d.InputSchema) > 0 && !json.Valid(d.InputSchema) {
		return fmt.Errorf("invalid input schema")
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	if _, exists := r.tools[d.Code]; exists {
		return fmt.Errorf("agent tool %s already registered", d.Code)
	}
	r.tools[d.Code] = tool
	return nil
}

func (r *ControlledAgentToolRegistry) List() []AgentToolDescriptor {
	if r == nil || !r.enabled {
		return []AgentToolDescriptor{}
	}
	r.mu.RLock()
	defer r.mu.RUnlock()
	items := make([]AgentToolDescriptor, 0, len(r.tools))
	for _, tool := range r.tools {
		items = append(items, tool.Descriptor())
	}
	sort.Slice(items, func(i, j int) bool { return items[i].Code < items[j].Code })
	return items
}

func (r *ControlledAgentToolRegistry) Invoke(ctx context.Context, code string, request AgentToolRequest) (any, error) {
	if r == nil || !r.enabled {
		return nil, fmt.Errorf("agent tools are disabled")
	}
	r.mu.RLock()
	tool, ok := r.tools[strings.TrimSpace(code)]
	r.mu.RUnlock()
	if !ok {
		return nil, fmt.Errorf("agent tool is not registered")
	}
	d := tool.Descriptor()
	if d.ReviewRequired && (!request.Approved || strings.TrimSpace(request.ApprovalRef) == "") {
		return nil, fmt.Errorf("agent tool requires explicit human approval reference")
	}
	if err := validateAgentToolInput(d.InputSchema, request.Input); err != nil {
		return nil, fmt.Errorf("agent tool input invalid: %w", err)
	}
	started := time.Now()
	output, invokeErr := tool.Invoke(ctx, request)
	inputHash := sha256.Sum256(request.Input)
	audit := models.AgentToolAuditLog{ProjectID: request.ProjectID, ToolCode: d.Code, Permission: d.Permission, InputHash: hex.EncodeToString(inputHash[:]), Success: invokeErr == nil, DurationMS: time.Since(started).Milliseconds()}
	if invokeErr != nil {
		audit.Error = invokeErr.Error()
	}
	if r.db != nil {
		if err := r.db.Create(&audit).Error; err != nil && invokeErr == nil {
			return nil, fmt.Errorf("agent tool audit failed: %w", err)
		}
	}
	return output, invokeErr
}

func validateAgentToolInput(schemaJSON, inputJSON json.RawMessage) error {
	if len(schemaJSON) == 0 {
		return nil
	}
	var schema struct {
		Type       string   `json:"type"`
		Required   []string `json:"required"`
		Properties map[string]struct {
			Type string `json:"type"`
		} `json:"properties"`
		AdditionalProperties *bool `json:"additionalProperties"`
	}
	if err := json.Unmarshal(schemaJSON, &schema); err != nil {
		return err
	}
	if schema.Type != "object" {
		return fmt.Errorf("only object input schemas are supported")
	}
	var input map[string]any
	if len(inputJSON) == 0 {
		inputJSON = json.RawMessage(`{}`)
	}
	if err := json.Unmarshal(inputJSON, &input); err != nil {
		return err
	}
	for _, key := range schema.Required {
		if _, ok := input[key]; !ok {
			return fmt.Errorf("missing required field %s", key)
		}
	}
	for key, value := range input {
		property, known := schema.Properties[key]
		if !known {
			if schema.AdditionalProperties != nil && !*schema.AdditionalProperties {
				return fmt.Errorf("unknown field %s", key)
			}
			continue
		}
		valid := property.Type == "" || (property.Type == "string" && isString(value)) || (property.Type == "number" && isNumber(value)) || (property.Type == "boolean" && isBool(value)) || (property.Type == "object" && isObject(value)) || (property.Type == "array" && isArray(value))
		if !valid {
			return fmt.Errorf("field %s must be %s", key, property.Type)
		}
	}
	return nil
}
func isString(v any) bool { _, ok := v.(string); return ok }
func isNumber(v any) bool { _, ok := v.(float64); return ok }
func isBool(v any) bool   { _, ok := v.(bool); return ok }
func isObject(v any) bool { _, ok := v.(map[string]any); return ok }
func isArray(v any) bool  { _, ok := v.([]any); return ok }

func RegisterDefaultReadOnlyAgentTools(registry *ControlledAgentToolRegistry, db *gorm.DB, capabilityManifestDir string) error {
	catalog := NewModelCatalogService(db)
	if strings.TrimSpace(capabilityManifestDir) != "" {
		catalog = NewModelCatalogService(db, capabilityManifestDir)
	}
	if err := registry.Register(readOnlyAgentTool{
		descriptor: AgentToolDescriptor{Code: "model-catalog.list", Name: "读取模型能力目录", Description: "读取由可执行模板推导的模型能力，不修改模板或任务", Permission: AgentToolDomainRead, InputSchema: json.RawMessage(`{"type":"object","additionalProperties":false}`)},
		invoke:     func(_ context.Context, _ AgentToolRequest) (any, error) { return catalog.List() },
	}); err != nil {
		return err
	}
	productionChecks := NewProductionCheckService(db)
	return registry.Register(readOnlyAgentTool{
		descriptor: AgentToolDescriptor{Code: "production.preflight", Name: "读取漫剧生产检查", Description: "按项目和可选Episode读取过期、错误、审核及连续性问题；不返回正文、提示词或文件路径", Permission: AgentToolDomainRead, InputSchema: json.RawMessage(`{"type":"object","properties":{"episode_n":{"type":"number"}},"additionalProperties":false}`)},
		invoke: func(_ context.Context, request AgentToolRequest) (any, error) {
			if request.ProjectID == nil {
				return nil, fmt.Errorf("project_id is required")
			}
			var input struct {
				EpisodeN int `json:"episode_n"`
			}
			if len(request.Input) > 0 {
				if err := json.Unmarshal(request.Input, &input); err != nil {
					return nil, err
				}
			}
			return productionChecks.Check(*request.ProjectID, input.EpisodeN)
		},
	})
}

type readOnlyAgentTool struct {
	descriptor AgentToolDescriptor
	invoke     func(context.Context, AgentToolRequest) (any, error)
}

func (t readOnlyAgentTool) Descriptor() AgentToolDescriptor { return t.descriptor }
func (t readOnlyAgentTool) Invoke(ctx context.Context, r AgentToolRequest) (any, error) {
	return t.invoke(ctx, r)
}
