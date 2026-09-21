package service

import (
	"strings"
	"testing"

	"comfyui-console/internal/models"
)

func TestImageEngineLegacyDefaultsAndValidation(t *testing.T) {
	if got, _ := normalizeAssetImageEngine(""); got != ImageEngineKrea2 {
		t.Fatalf("asset default=%s", got)
	}
	if got, _ := normalizeSceneImageEngine(""); got != ImageEngineMiniMaxH3 {
		t.Fatalf("scene default=%s", got)
	}
	if _, err := normalizeAssetImageEngine("minimax_h3"); err == nil {
		t.Fatal("asset accepted H3")
	}
	if _, err := normalizeSceneImageEngine("krea2"); err == nil {
		t.Fatal("scene accepted Krea2")
	}
}
func TestQwenPortraitDefaultsToNineBySixteen(t *testing.T) {
	params := qwenPortraitParams()
	if params["aspect_ratio"] != "9:16 (Portrait Widescreen)" {
		t.Fatalf("portrait ratio = %#v", params["aspect_ratio"])
	}
}

func TestQwenAssetVisibleTextPrompt(t *testing.T) {
	p := &models.Project{Style: "真人写实"}
	a := &models.Asset{Kind: AssetKindLocation, Name: "山门", Description: "青石山门", ImageEngine: ImageEngineQwen21, VisibleText: "天阙宗"}
	prompt := buildAssetPrompt(p, a)
	if !strings.Contains(prompt, `"天阙宗"`) || strings.Contains(prompt, "禁止文字") {
		t.Fatalf("visible text contract lost: %s", prompt)
	}
}
func TestEnabledTemplateDoesNotFallback(t *testing.T) {
	ps := newTestProjectService(t)
	if err := ps.db.AutoMigrate(&models.Template{}); err != nil {
		t.Fatal(err)
	}
	if _, err := enabledTemplateByCode(ps.db, TemplateQwen21T2I); err == nil {
		t.Fatal("missing Qwen template silently accepted")
	}
	if err := ps.db.Create(&models.Template{Code: TemplateQwen21T2I, Name: "Qwen T2I", Enabled: true}).Error; err != nil {
		t.Fatal(err)
	}
	if tpl, err := enabledTemplateByCode(ps.db, TemplateQwen21T2I); err != nil || tpl.Code != TemplateQwen21T2I {
		t.Fatalf("template resolution failed: %+v %v", tpl, err)
	}
}
