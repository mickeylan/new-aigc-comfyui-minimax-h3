package service

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"gorm.io/gorm"

	"comfyui-console/internal/models"
)

// ModelCatalogEntry is the stable, read-only capability view derived from a Template.
// Template remains the source of truth so catalog data cannot drift from executable workflows.
type ModelCatalogEntry struct {
	Name               string                 `json:"name"`
	Description        string                 `json:"description"`
	Mode               string                 `json:"mode"`
	TemplateCode       string                 `json:"template_code"`
	MaxReferences      int                    `json:"max_references"`
	SupportsFirstFrame bool                   `json:"supports_first_frame"`
	SupportsLastFrame  bool                   `json:"supports_last_frame"`
	SupportsAudio      bool                   `json:"supports_audio"`
	Resolutions        []ResolutionCapability `json:"resolutions"`
	Duration           *NumericCapability     `json:"duration"`
	DefaultParams      map[string]any         `json:"default_params"`
	Capability         *CapabilityManifest    `json:"capability,omitempty"`
}

type ResolutionCapability struct {
	Width  NumericCapability `json:"width"`
	Height NumericCapability `json:"height"`
}

type NumericCapability struct {
	Min     *float64 `json:"min"`
	Max     *float64 `json:"max"`
	Step    *float64 `json:"step,omitempty"`
	Default *float64 `json:"default"`
}

type catalogInput struct {
	Key      string   `json:"key"`
	Type     string   `json:"type"`
	Default  any      `json:"default"`
	Min      *float64 `json:"min"`
	Max      *float64 `json:"max"`
	Step     *float64 `json:"step"`
	MaxCount int      `json:"max_count"`
}

type CapabilityManifest struct {
	TemplateCode   string   `json:"template_code"`
	MediaKind      string   `json:"media_kind"`
	ProviderFamily string   `json:"provider_family"`
	Inputs         []string `json:"inputs"`
	Outputs        []string `json:"outputs"`
	ReviewRequired bool     `json:"review_required"`
	AdapterCode    string   `json:"adapter_code,omitempty"`
	Notes          string   `json:"notes,omitempty"`
}

type ModelCatalogService struct {
	db          *gorm.DB
	manifestDir string
}

func NewModelCatalogService(db *gorm.DB, manifestDir ...string) *ModelCatalogService {
	s := &ModelCatalogService{db: db}
	if len(manifestDir) > 0 {
		s.manifestDir = strings.TrimSpace(manifestDir[0])
	}
	return s
}

func (s *ModelCatalogService) List() ([]ModelCatalogEntry, error) {
	var templates []models.Template
	if err := s.db.Where("enabled = ?", true).Order("code, id").Find(&templates).Error; err != nil {
		return nil, err
	}
	// Optional overlays are diagnostic metadata only; malformed files must not break the executable catalog.
	manifests, _ := loadCapabilityManifests(s.manifestDir)
	entries := make([]ModelCatalogEntry, 0, len(templates))
	for _, template := range templates {
		entry, err := catalogEntryFromTemplate(s.db, template)
		if err != nil {
			return nil, fmt.Errorf("derive catalog entry %s: %w", template.Code, err)
		}
		if manifest, ok := manifests[template.Code]; ok {
			copy := manifest
			entry.Capability = &copy
		}
		entries = append(entries, entry)
	}
	return entries, nil
}

func catalogEntryFromTemplate(db *gorm.DB, template models.Template) (ModelCatalogEntry, error) {
	var inputs []catalogInput
	if err := json.Unmarshal([]byte(template.InputsJSON), &inputs); err != nil {
		return ModelCatalogEntry{}, err
	}
	byKey := make(map[string]catalogInput, len(inputs))
	defaults := make(map[string]any)
	for _, input := range inputs {
		byKey[input.Key] = input
		if input.Default != nil {
			defaults[input.Key] = input.Default
		}
	}

	entry := ModelCatalogEntry{
		Name: template.Name, Description: template.Description, TemplateCode: template.Code,
		Mode: inferCatalogMode(byKey), SupportsFirstFrame: hasInput(byKey, "first_frame"),
		SupportsLastFrame: hasInput(byKey, "last_frame"), SupportsAudio: workflowSupportsAudio(template.WorkflowJSON),
		Resolutions: make([]ResolutionCapability, 0, 1), DefaultParams: defaults,
	}
	if refs, ok := byKey["ref_images"]; ok {
		entry.MaxReferences = refs.MaxCount
	}
	if width, widthOK := byKey["width"]; widthOK {
		if height, heightOK := byKey["height"]; heightOK {
			entry.Resolutions = append(entry.Resolutions, ResolutionCapability{
				Width: numericCapability(width), Height: numericCapability(height),
			})
		}
	}
	if duration, ok := byKey["duration"]; ok {
		value := numericCapability(duration)
		if isVideoTemplateCode(template.Code) && strings.HasPrefix(template.Code, "minimax_h3_") {
			max := configuredH3GenerationDuration(db)
			value.Max = &max
		}
		entry.Duration = &value
	}
	return entry, nil
}

func inferCatalogMode(inputs map[string]catalogInput) string {
	switch {
	case hasInput(inputs, "first_frame") && hasInput(inputs, "last_frame"):
		return "first-last-frame-to-video"
	case hasInput(inputs, "first_frame"):
		return "image-to-video"
	case hasInput(inputs, "ref_images") && hasInput(inputs, "duration"):
		return "reference-to-video"
	case hasInput(inputs, "duration"):
		return "text-to-video"
	case hasInput(inputs, "ref_images"):
		return "reference-to-image"
	case hasInput(inputs, "source_image"):
		return "image-to-image"
	default:
		return "text-to-image"
	}
}

func hasInput(inputs map[string]catalogInput, key string) bool {
	_, ok := inputs[key]
	return ok
}

func numericCapability(input catalogInput) NumericCapability {
	capability := NumericCapability{Min: input.Min, Max: input.Max, Step: input.Step}
	if value, ok := input.Default.(float64); ok {
		capability.Default = &value
	}
	return capability
}

func workflowSupportsAudio(workflowJSON string) bool {
	if !json.Valid([]byte(workflowJSON)) {
		return false
	}
	// Current H3 workflows declare audio nodes/ports with "audio" in their class,
	// input, or model names. Requiring a valid workflow avoids deriving from prose.
	return strings.Contains(strings.ToLower(workflowJSON), "audio")
}

func loadCapabilityManifests(dir string) (map[string]CapabilityManifest, error) {
	result := map[string]CapabilityManifest{}
	if strings.TrimSpace(dir) == "" {
		return result, nil
	}
	absolute, err := filepath.Abs(dir)
	if err != nil {
		return nil, err
	}
	entries, err := os.ReadDir(absolute)
	if err != nil {
		return nil, err
	}
	sort.Slice(entries, func(i, j int) bool { return entries[i].Name() < entries[j].Name() })
	for _, entry := range entries {
		if entry.IsDir() || !strings.HasSuffix(strings.ToLower(entry.Name()), ".json") {
			continue
		}
		content, err := os.ReadFile(filepath.Join(absolute, entry.Name()))
		if err != nil {
			return nil, err
		}
		var manifest CapabilityManifest
		if err := json.Unmarshal(content, &manifest); err != nil {
			return nil, fmt.Errorf("capability manifest %s: %w", entry.Name(), err)
		}
		manifest.TemplateCode = strings.TrimSpace(manifest.TemplateCode)
		if manifest.TemplateCode == "" || strings.TrimSpace(manifest.MediaKind) == "" || len(manifest.Outputs) == 0 {
			return nil, fmt.Errorf("capability manifest %s requires template_code, media_kind and outputs", entry.Name())
		}
		if _, exists := result[manifest.TemplateCode]; exists {
			return nil, fmt.Errorf("duplicate capability manifest for %s", manifest.TemplateCode)
		}
		result[manifest.TemplateCode] = manifest
	}
	return result, nil
}
