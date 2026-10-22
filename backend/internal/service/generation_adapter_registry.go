package service

import (
	"fmt"
	"sort"
	"strings"
	"sync"

	"comfyui-console/internal/models"
	"gorm.io/gorm"
)

type GenerationAdapterDescriptor struct {
	Code            string `json:"code"`
	Name            string `json:"name"`
	TemplateCode    string `json:"template_code"`
	MediaKind       string `json:"media_kind"`
	Installed       bool   `json:"installed"`
	SubmissionReady bool   `json:"submission_ready"`
	Reason          string `json:"reason,omitempty"`
}

// GenerationAdapter only describes and validates integration readiness here.
// Existing TaskService remains the sole task submission and recovery path.
type GenerationAdapter interface {
	Descriptor() GenerationAdapterDescriptor
	ValidateInstallation() error
}

type GenerationAdapterRegistry struct {
	db      *gorm.DB
	enabled bool
	mu      sync.RWMutex
	items   map[string]GenerationAdapter
}

func NewGenerationAdapterRegistry(db *gorm.DB, enabled bool) *GenerationAdapterRegistry {
	return &GenerationAdapterRegistry{db: db, enabled: enabled, items: map[string]GenerationAdapter{}}
}

func (r *GenerationAdapterRegistry) Register(adapter GenerationAdapter) error {
	if adapter == nil {
		return fmt.Errorf("generation adapter is nil")
	}
	d := adapter.Descriptor()
	if !skillPackageCodePattern.MatchString(strings.TrimSpace(d.Code)) || strings.TrimSpace(d.TemplateCode) == "" {
		return fmt.Errorf("adapter code and template code are required")
	}
	if !r.enabled {
		return fmt.Errorf("generation adapters are disabled")
	}
	var template models.Template
	if err := r.db.Where("code = ? AND enabled = ?", d.TemplateCode, true).First(&template).Error; err != nil {
		return fmt.Errorf("adapter template %s is unavailable: %w", d.TemplateCode, err)
	}
	if err := adapter.ValidateInstallation(); err != nil {
		return fmt.Errorf("adapter %s installation is invalid: %w", d.Code, err)
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	if _, exists := r.items[d.Code]; exists {
		return fmt.Errorf("generation adapter %s already registered", d.Code)
	}
	r.items[d.Code] = adapter
	return nil
}

func (r *GenerationAdapterRegistry) List() []GenerationAdapterDescriptor {
	if r == nil || !r.enabled {
		return []GenerationAdapterDescriptor{}
	}
	r.mu.RLock()
	defer r.mu.RUnlock()
	items := make([]GenerationAdapterDescriptor, 0, len(r.items))
	for _, adapter := range r.items {
		d := adapter.Descriptor()
		d.Installed = true
		d.SubmissionReady = true
		items = append(items, d)
	}
	sort.Slice(items, func(i, j int) bool { return items[i].Code < items[j].Code })
	return items
}

func (r *GenerationAdapterRegistry) Resolve(code string) (GenerationAdapter, error) {
	if r == nil || !r.enabled {
		return nil, fmt.Errorf("generation adapters are disabled")
	}
	r.mu.RLock()
	adapter, ok := r.items[strings.TrimSpace(code)]
	r.mu.RUnlock()
	if !ok {
		return nil, fmt.Errorf("generation adapter is not installed")
	}
	return adapter, nil
}
