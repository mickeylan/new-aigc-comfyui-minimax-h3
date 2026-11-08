package service

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/glebarez/sqlite"
	"gorm.io/gorm"
)

// Optional, strictly read-only acceptance against a user-supplied diagnostic copy.
// No service startup, migration, model call or GenerateScript is performed.
func TestEpisodeBoundaryDiagnosticCopyReadOnly(t *testing.T) {
	path := os.Getenv("EPISODE_BOUNDARY_DIAGNOSTIC_DB")
	if path == "" {
		t.Skip("set EPISODE_BOUNDARY_DIAGNOSTIC_DB to a diagnostic SQLite copy")
	}
	db, err := gorm.Open(sqlite.Open("file:"+filepath.ToSlash(path)+"?mode=ro"), &gorm.Config{})
	if err != nil {
		t.Fatal(err)
	}
	sqlDB, err := db.DB()
	if err != nil {
		t.Fatal(err)
	}
	defer sqlDB.Close()
	sqlDB.SetMaxOpenConns(1)
	if err := db.Exec("PRAGMA query_only=ON").Error; err != nil {
		t.Fatal(err)
	}
	var integrity string
	if err := db.Raw("PRAGMA integrity_check").Scan(&integrity).Error; err != nil || integrity != "ok" {
		t.Fatalf("integrity=%s err=%v", integrity, err)
	}
	projects := NewProjectService(nil, db, nil, nil, nil, nil, nil, nil, nil)
	adapt := NewAdaptationService(db, nil, nil, projects)
	ctx, err := adapt.BuildContext(1, 3, 0)
	if err != nil {
		t.Fatal(err)
	}
	if len(ctx.CompletedDialogues) == 0 || len(ctx.AvailableDialogues) != 0 {
		t.Fatalf("diagnostic case not detected: completed=%d available=%d required=%d", len(ctx.CompletedDialogues), len(ctx.AvailableDialogues), len(ctx.DialogueLedger))
	}
	consumed, err := adapt.episodeSourceScopeConsumed(1, 3, ctx)
	if err != nil || !consumed {
		scenes, loadErr := projects.authoritativeEpisodeScenes(1, 2)
		if loadErr != nil {
			t.Fatal(loadErr)
		}
		var texts []string
		for _, scene := range scenes {
			texts = append(texts, scene.Content)
		}
		for _, assessment := range assessMustKeepEvents(texts, ctx.MustKeepEvents) {
			t.Logf("coverage=%.3f clauses=%.3f event=%s", assessment.Coverage, assessment.Clauses, assessment.Event)
		}
		t.Fatalf("replayed mapped events not detected: consumed=%v err=%v", consumed, err)
	}
	t.Logf("read-only diagnostic: episode=3 completed_source_dialogues=%d available=%d required=%d scope_consumed=%v integrity=%s", len(ctx.CompletedDialogues), len(ctx.AvailableDialogues), len(ctx.DialogueLedger), consumed, integrity)
	preview, _, err := adapt.previewReflow(1, 3)
	if err != nil {
		t.Fatal(err)
	}
	if len(preview.Items) != 7 || preview.Items[0].Title != "血仇与远行" || preview.UnplannedEpisode != 10 {
		t.Fatalf("unexpected repair preview: %+v", preview)
	}
	t.Logf("read-only repair preview: keep episodes 1-2; episode 3=%s; shifted mappings=%d; unplanned tail=%d", preview.Items[0].Title, len(preview.Items), preview.UnplannedEpisode)
}
