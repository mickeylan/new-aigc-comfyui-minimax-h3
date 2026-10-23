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

func TestProductionPreflightAgentToolRequiresProjectScope(t *testing.T) {
	db := newAgentToolTestDB(t)
	if err := db.AutoMigrate(&models.Project{}, &models.Scene{}, &models.Dialogue{}, &models.SceneContinuity{}, &models.MusicCueSheet{}); err != nil {
		t.Fatal(err)
	}
	project := models.Project{Title: "p"}
	db.Create(&project)
	db.Create(&models.Scene{ProjectID: project.ID, EpisodeN: 1, Duration: 8, ImageFile: "storyboard.png", VideoFile: "video.mp4"})
	registry := NewControlledAgentToolRegistry(db, true)
	if err := RegisterDefaultReadOnlyAgentTools(registry, db, ""); err != nil {
		t.Fatal(err)
	}
	if _, err := registry.Invoke(context.Background(), "production.preflight", AgentToolRequest{Input: json.RawMessage(`{"episode_n":1}`)}); err == nil {
		t.Fatal("project-less production check was accepted")
	}
	result, err := registry.Invoke(context.Background(), "production.preflight", AgentToolRequest{ProjectID: &project.ID, Input: json.RawMessage(`{"episode_n":1}`)})
	if err != nil {
		t.Fatal(err)
	}
	if !result.(ProductionCheckResult).Ready {
		t.Fatalf("unexpected result: %+v", result)
	}
}

func TestControlledAgentToolValidatesSchemaAndApproval(t *testing.T) {
	registry := NewControlledAgentToolRegistry(nil, true)
	tool := readOnlyAgentTool{descriptor: AgentToolDescriptor{Code: "scene-update", Permission: AgentToolDomainWrite, ReviewRequired: true, InputSchema: json.RawMessage(`{"type":"object","required":["title"],"properties":{"title":{"type":"string"}},"additionalProperties":false}`)}, invoke: func(context.Context, AgentToolRequest) (any, error) { return "ok", nil }}
	if err := registry.Register(tool); err != nil {
		t.Fatal(err)
	}
	if _, err := registry.Invoke(context.Background(), "scene-update", AgentToolRequest{Input: json.RawMessage(`{"title":"x"}`)}); err == nil {
		t.Fatal("write tool ran without approval")
	}
	if _, err := registry.Invoke(context.Background(), "scene-update", AgentToolRequest{Input: json.RawMessage(`{"wrong":1}`), Approved: true, ApprovalRef: "review-1"}); err == nil {
		t.Fatal("invalid input bypassed schema")
	}
	if _, err := registry.Invoke(context.Background(), "scene-update", AgentToolRequest{Input: json.RawMessage(`{"title":"x"}`), Approved: true, ApprovalRef: "review-1"}); err != nil {
		t.Fatal(err)
	}
}

func TestControlledAgentToolRequiresReviewForWrites(t *testing.T) {
	registry := NewControlledAgentToolRegistry(nil, true)
	write := readOnlyAgentTool{descriptor: AgentToolDescriptor{Code: "scene-update", Permission: AgentToolDomainWrite, InputSchema: json.RawMessage(`{}`)}, invoke: func(context.Context, AgentToolRequest) (any, error) { return nil, nil }}
	if err := registry.Register(write); err == nil {
		t.Fatal("write tool without review gate was registered")
	}
}
