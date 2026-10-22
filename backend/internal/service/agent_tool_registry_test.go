package service

import (
	"context"
	"encoding/json"
	"testing"

	"comfyui-console/internal/models"
	"github.com/glebarez/sqlite"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"
)

func newAgentToolTestDB(t *testing.T) *gorm.DB {
	t.Helper()
	db, err := gorm.Open(sqlite.Open("file::memory:?cache=shared"), &gorm.Config{Logger: logger.Default.LogMode(logger.Silent)})
	if err != nil {
		t.Fatal(err)
	}
	if err := db.AutoMigrate(&models.AgentToolAuditLog{}, &models.Template{}, &models.Setting{}); err != nil {
		t.Fatal(err)
	}
	return db
}

func TestControlledAgentToolsAreDisabledByDefaultAndRejectUnsafePermissions(t *testing.T) {
	db := newAgentToolTestDB(t)
	registry := NewControlledAgentToolRegistry(db, false)
	unsafe := readOnlyAgentTool{descriptor: AgentToolDescriptor{Code: "unsafe-shell", Permission: "shell", InputSchema: json.RawMessage(`{}`)}, invoke: func(context.Context, AgentToolRequest) (any, error) { return nil, nil }}
	if err := registry.Register(unsafe); err == nil {
		t.Fatal("unsafe permission was registered")
	}
	if len(registry.List()) != 0 {
		t.Fatal("disabled registry exposed tools")
	}
	if _, err := registry.Invoke(context.Background(), "anything", AgentToolRequest{}); err == nil {
		t.Fatal("disabled registry invoked a tool")
	}
}

func TestControlledAgentToolInvokesReadOnlyDomainOperationAndAudits(t *testing.T) {
	db := newAgentToolTestDB(t)
	if err := db.Create(&models.Template{Name: "test", Code: "test-template", Enabled: true, InputsJSON: `[]`, WorkflowJSON: `{}`}).Error; err != nil {
		t.Fatal(err)
	}
	registry := NewControlledAgentToolRegistry(db, true)
	if err := RegisterDefaultReadOnlyAgentTools(registry, db, ""); err != nil {
		t.Fatal(err)
	}
	result, err := registry.Invoke(context.Background(), "model-catalog.list", AgentToolRequest{Input: json.RawMessage(`{}`)})
	if err != nil {
		t.Fatal(err)
	}
	if len(result.([]ModelCatalogEntry)) != 1 {
		t.Fatalf("unexpected result: %+v", result)
	}
	var audit models.AgentToolAuditLog
	if err := db.First(&audit).Error; err != nil {
		t.Fatal(err)
	}
	if !audit.Success || audit.Permission != AgentToolDomainRead || audit.ToolCode != "model-catalog.list" {
		t.Fatalf("unexpected audit: %+v", audit)
	}
}

func TestControlledAgentToolRequiresReviewForWrites(t *testing.T) {
	registry := NewControlledAgentToolRegistry(nil, true)
	write := readOnlyAgentTool{descriptor: AgentToolDescriptor{Code: "scene-update", Permission: AgentToolDomainWrite, InputSchema: json.RawMessage(`{}`)}, invoke: func(context.Context, AgentToolRequest) (any, error) { return nil, nil }}
	if err := registry.Register(write); err == nil {
		t.Fatal("write tool without review gate was registered")
	}
}
