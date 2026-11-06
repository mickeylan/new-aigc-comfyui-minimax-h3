package service

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"comfyui-console/internal/models"
)

type storyboardCheckProvider struct{ response string }

func (p storyboardCheckProvider) Name() string                        { return "test-vision" }
func (p storyboardCheckProvider) Chat(string, string) (string, error) { return "", nil }
func (p storyboardCheckProvider) HealthCheck(context.Context) error   { return nil }
func (p storyboardCheckProvider) ChatWithImage(system, user, path string) (string, error) {
	if !strings.Contains(system, "cannot prove actual movement") || !strings.Contains(user, "CURRENT Scene text") || path == "" {
		panic("visual check omitted contract context")
	}
	return p.response, nil
}

func TestCheckSceneStoryboardStrictTypedResponse(t *testing.T) {
	provider := storyboardCheckProvider{response: `{"status":"issues_found","checks":[{"category":"orientation","subject":"actor","evidence":"faces away","suggestion":"show the expected direction"}],"uncertainty":[]}`}
	got, err := checkSceneStoryboard(provider, &models.Scene{Content: "actor faces target"}, "frame.png")
	if err != nil || got.Status != "issues_found" || len(got.Checks) != 1 {
		t.Fatalf("result=%+v err=%v", got, err)
	}
	provider.response = `{"status":"no_obvious_issues","checks":[{"category":"story","evidence":"x","suggestion":"y"}],"uncertainty":[]}`
	if _, err := checkSceneStoryboard(provider, &models.Scene{}, "frame.png"); err == nil {
		t.Fatal("no_obvious_issues must not carry issue checks")
	}
}

func TestSecureStoryboardImageRejectsTraversalAndChecksBytes(t *testing.T) {
	data := t.TempDir()
	root := filepath.Join(data, "input", "7")
	if err := os.MkdirAll(root, 0o755); err != nil {
		t.Fatal(err)
	}
	png := append([]byte("\x89PNG\r\n\x1a\n"), []byte("payload")...)
	if err := os.WriteFile(filepath.Join(root, "frame.png"), png, 0o600); err != nil {
		t.Fatal(err)
	}
	_, hash, err := secureStoryboardImage(data, 7, "frame.png")
	if err != nil || hash == ([32]byte{}) {
		t.Fatalf("hash=%x err=%v", hash, err)
	}
	if _, _, err := secureStoryboardImage(data, 7, "../frame.png"); err == nil {
		t.Fatal("accepted traversal")
	}
	if err := os.WriteFile(filepath.Join(root, "fake.png"), []byte("not an image"), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, _, err := secureStoryboardImage(data, 7, "fake.png"); err == nil {
		t.Fatal("accepted extension-only image")
	}
}

func TestSceneVisualSourceHashDoesNotExposeFilenameAndChangesWithBytes(t *testing.T) {
	scene := &models.Scene{Content: "story", ImageFile: "secret-name.png"}
	a, b := [32]byte{1}, [32]byte{2}
	h1, h2 := sceneVisualSourceHash(scene, a), sceneVisualSourceHash(scene, b)
	if h1 == h2 || strings.Contains(h1, scene.ImageFile) || len(h1) != 64 {
		t.Fatalf("bad source hashes %q %q", h1, h2)
	}
}
