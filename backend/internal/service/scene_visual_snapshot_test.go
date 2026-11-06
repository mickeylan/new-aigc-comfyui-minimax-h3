package service

import (
	"context"
	"crypto/sha256"
	"os"
	"path/filepath"
	"testing"

	"comfyui-console/internal/models"
)

type storyboardBytesProvider struct {
	want     [32]byte
	response string
	called   bool
}

func (p *storyboardBytesProvider) Name() string                        { return "bytes-test" }
func (p *storyboardBytesProvider) Chat(string, string) (string, error) { return "", nil }
func (p *storyboardBytesProvider) HealthCheck(context.Context) error   { return nil }
func (p *storyboardBytesProvider) ChatWithImageBytes(_, _ string, data []byte) (string, error) {
	p.called = true
	if sha256.Sum256(data) != p.want {
		panic("provider did not receive validated snapshot")
	}
	return p.response, nil
}

func TestSceneStoryboardBytesUsesSnapshotAfterPathRemoved(t *testing.T) {
	dataDir := t.TempDir()
	root := filepath.Join(dataDir, "input", "7")
	if err := os.MkdirAll(root, 0o755); err != nil {
		t.Fatal(err)
	}
	image := append([]byte("\x89PNG\r\n\x1a\n"), []byte("snapshot")...)
	path := filepath.Join(root, "frame.png")
	if err := os.WriteFile(path, image, 0o600); err != nil {
		t.Fatal(err)
	}
	_, hash, snapshot, err := secureStoryboardImageSnapshot(dataDir, 7, "frame.png")
	if err != nil {
		t.Fatal(err)
	}
	if err := os.Remove(path); err != nil {
		t.Fatal(err)
	}
	provider := &storyboardBytesProvider{want: hash, response: `{"status":"no_obvious_issues","checks":[],"uncertainty":[]}`}
	result, err := checkSceneStoryboardBytes(provider, &models.Scene{Content: "scene"}, snapshot)
	if err != nil || result.Status != "no_obvious_issues" || !provider.called {
		t.Fatalf("result=%+v called=%v err=%v", result, provider.called, err)
	}
}
