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
	ProjectID *uint           `json:"project_id,omitempty"`
	Input     json.RawMessage `json:"input"`
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

func RegisterDefaultReadOnlyAgentTools(registry *ControlledAgentToolRegistry, db *gorm.DB, capabilityManifestDir string) error {
	catalog := NewModelCatalogService(db)
	if strings.TrimSpace(capabilityManifestDir) != "" {
		catalog = NewModelCatalogService(db, capabilityManifestDir)
	}
	return registry.Register(readOnlyAgentTool{
		descriptor: AgentToolDescriptor{Code: "model-catalog.list", Name: "读取模型能力目录", Description: "读取由可执行模板推导的模型能力，不修改模板或任务", Permission: AgentToolDomainRead, InputSchema: json.RawMessage(`{"type":"object","additionalProperties":false}`)},
		invoke:     func(_ context.Context, _ AgentToolRequest) (any, error) { return catalog.List() },
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
