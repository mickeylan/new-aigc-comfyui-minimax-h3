package service

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/glebarez/sqlite"
	"gorm.io/gorm"

	"comfyui-console/internal/models"
)

func TestInitSystemTemplatesFlattensRuntimeAutogrowReferences(t *testing.T) {
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	if err != nil {
		t.Fatal(err)
	}
	if err := db.AutoMigrate(&models.Template{}); err != nil {
		t.Fatal(err)
	}
	dir := t.TempDir()
	data := `{"name":"换装","code":"minimax_h3_look_reference","inputs":[{"key":"ref_images","type":"images","required":true}],"workflow":{"416":{"class_type":"MiniMaxH3ReferenceToVideo","inputs":{"ref_images":{"ref_image_0":["137",0],"ref_image_1":["139",0]}}}}}`
	if err := os.WriteFile(filepath.Join(dir, "minimax_h3_look_reference.json"), []byte(data), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := InitSystemTemplates(db, dir); err != nil {
		t.Fatal(err)
	}
	var tpl models.Template
	if err := db.Where("code = ?", "minimax_h3_look_reference").First(&tpl).Error; err != nil {
		t.Fatal(err)
	}
	if strings.Contains(tpl.WorkflowJSON, `"ref_images":{`) || !strings.Contains(tpl.WorkflowJSON, `"ref_images.ref_image_0"`) {
		t.Fatalf("runtime Autogrow refs were not flattened: %s", tpl.WorkflowJSON)
	}
}

func TestInitSystemTemplatesPrefersRuntimeDirectory(t *testing.T) {
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	if err != nil {
		t.Fatal(err)
	}
	if err := db.AutoMigrate(&models.Template{}); err != nil {
		t.Fatal(err)
	}

	dir := t.TempDir()
	data := `{"name":"运行时模板","code":"krea2_asset_reference","description":"disk override","inputs":[{"key":"unet_name","type":"string","default":"runtime-model.safetensors"}],"workflow":{"1":{"class_type":"UNETLoader","inputs":{"unet_name":"{{unet_name}}"}}}}`
	if err := os.WriteFile(filepath.Join(dir, "krea2_asset_reference.json"), []byte(data), 0o644); err != nil {
		t.Fatal(err)
	}

	if err := InitSystemTemplates(db, dir); err != nil {
		t.Fatal(err)
	}
	var tpl models.Template
	if err := db.Where("code = ?", "krea2_asset_reference").First(&tpl).Error; err != nil {
		t.Fatal(err)
	}
	if tpl.Name != "运行时模板" || !strings.Contains(tpl.InputsJSON, "runtime-model.safetensors") {
		t.Fatalf("runtime template did not override embedded template: %#v", tpl)
	}
}
