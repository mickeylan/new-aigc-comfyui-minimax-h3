package service

import (
	"os"
	"path/filepath"
	"testing"

	"comfyui-console/internal/models"
	"github.com/glebarez/sqlite"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"
)

func newSkillPackageTestDB(t *testing.T) *gorm.DB {
	t.Helper()
	db, err := gorm.Open(sqlite.Open("file:"+t.Name()+"?mode=memory&cache=shared"), &gorm.Config{Logger: logger.Default.LogMode(logger.Silent)})
	if err != nil {
		t.Fatal(err)
	}
	if err := db.AutoMigrate(&models.Skill{}, &models.SkillPackageInstall{}); err != nil {
		t.Fatal(err)
	}
	return db
}

func writeSkillPackage(t *testing.T, root, name, manifest, prompt, system string) string {
	t.Helper()
	dir := filepath.Join(root, name)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	for filename, content := range map[string]string{"manifest.json": manifest, "prompt.md": prompt, "system.md": system} {
		if err := os.WriteFile(filepath.Join(dir, filename), []byte(content), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	return dir
}

func TestImportSkillPackagesIsOptInImmutableAndDisabledByDefault(t *testing.T) {
	db := newSkillPackageTestDB(t)
	root := t.TempDir()
	writeSkillPackage(t, root, "example", `{"name":"Example","code":"example-package","version":1,"description":"test","stage":"review","operation":"example-review","prompt_file":"prompt.md","system_file":"system.md"}`, "Review {{input}}", "Return JSON")
	svc := NewSkillService(db)
	results, err := svc.ImportSkillPackages(root)
	if err != nil {
		t.Fatal(err)
	}
	if len(results) != 1 || !results[0].Created {
		t.Fatalf("unexpected import: %+v", results)
	}
	var skill models.Skill
	if err := db.First(&skill, results[0].SkillID).Error; err != nil {
		t.Fatal(err)
	}
	if skill.Enabled || skill.IsSystem {
		t.Fatalf("package unexpectedly changed active system behavior: %+v", skill)
	}
	if _, err := svc.ImportSkillPackages(root); err != nil {
		t.Fatalf("idempotent import failed: %v", err)
	}
	if err := os.WriteFile(filepath.Join(root, "example", "system.md"), []byte("changed without version bump"), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := svc.ImportSkillPackages(root); err == nil {
		t.Fatal("changed immutable version was accepted")
	}
}

func TestPackagedSkillContentCannotBeEditedOrDeletedThroughCRUD(t *testing.T) {
	db := newSkillPackageTestDB(t)
	root := t.TempDir()
	writeSkillPackage(t, root, "example", `{"name":"Example","code":"example-package","version":1,"stage":"review","operation":"example-review","prompt_file":"prompt.md","system_file":"system.md"}`, "Review", "System")
	svc := NewSkillService(db)
	results, err := svc.ImportSkillPackages(root)
	if err != nil {
		t.Fatal(err)
	}
	id := results[0].SkillID
	if _, err := svc.UpdateSkill(id, map[string]any{"prompt_template": "changed"}); err == nil {
		t.Fatal("packaged skill content was edited")
	}
	if _, err := svc.UpdateSkill(id, map[string]any{"enabled": true}); err != nil {
		t.Fatal(err)
	}
	if err := svc.DeleteSkill(id); err == nil {
		t.Fatal("packaged skill audit source was deleted")
	}
}

func TestImportSkillPackagesRejectsTraversal(t *testing.T) {
	db := newSkillPackageTestDB(t)
	root := t.TempDir()
	writeSkillPackage(t, root, "unsafe", `{"name":"Unsafe","code":"unsafe-package","version":1,"stage":"review","operation":"unsafe","prompt_file":"../outside.md","system_file":"system.md"}`, "unused", "system")
	if _, err := NewSkillService(db).ImportSkillPackages(root); err == nil {
		t.Fatal("path traversal was accepted")
	}
}
