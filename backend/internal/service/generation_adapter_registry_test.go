package service

import (
	"errors"
	"testing"

	"comfyui-console/internal/models"
	"github.com/glebarez/sqlite"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"
)

type testGenerationAdapter struct {
	descriptor GenerationAdapterDescriptor
	err        error
}

func (a testGenerationAdapter) Descriptor() GenerationAdapterDescriptor { return a.descriptor }
func (a testGenerationAdapter) ValidateInstallation() error             { return a.err }

func TestGenerationAdaptersAreDisabledByDefault(t *testing.T) {
	registry := NewGenerationAdapterRegistry(nil, false)
	if len(registry.List()) != 0 {
		t.Fatal("disabled registry exposed adapters")
	}
	if _, err := registry.Resolve("anything"); err == nil {
		t.Fatal("disabled registry resolved adapter")
	}
	if err := registry.Register(testGenerationAdapter{descriptor: GenerationAdapterDescriptor{Code: "music3", TemplateCode: "missing", MediaKind: "audio"}}); err == nil {
		t.Fatal("disabled registry registered adapter")
	}
}

func TestGenerationAdapterRequiresEnabledRealTemplateAndValidInstallation(t *testing.T) {
	db, err := gorm.Open(sqlite.Open("file::memory:?cache=shared"), &gorm.Config{Logger: logger.Default.LogMode(logger.Silent)})
	if err != nil {
		t.Fatal(err)
	}
	if err := db.AutoMigrate(&models.Template{}); err != nil {
		t.Fatal(err)
	}
	registry := NewGenerationAdapterRegistry(db, true)
	adapter := testGenerationAdapter{descriptor: GenerationAdapterDescriptor{Code: "music3-adapter", Name: "Music 3", TemplateCode: "music3-template", MediaKind: "audio"}}
	if err := registry.Register(adapter); err == nil {
		t.Fatal("adapter without a real template was registered")
	}
	if err := db.Create(&models.Template{Name: "Music", Code: "music3-template", Enabled: true, InputsJSON: `[]`, WorkflowJSON: `{}`}).Error; err != nil {
		t.Fatal(err)
	}
	adapter.err = errors.New("nodes unavailable")
	if err := registry.Register(adapter); err == nil {
		t.Fatal("adapter with invalid installation was registered")
	}
	adapter.err = nil
	if err := registry.Register(adapter); err != nil {
		t.Fatal(err)
	}
	items := registry.List()
	if len(items) != 1 || !items[0].Installed || !items[0].SubmissionReady {
		t.Fatalf("unexpected adapters: %+v", items)
	}
}
