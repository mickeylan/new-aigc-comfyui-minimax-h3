package service

import (
	"os"
	"path/filepath"
	"testing"

	"comfyui-console/internal/config"
)

func TestRemoteConfiguredMediaStillUsesLocalProbeWhenPathExists(t *testing.T) {
	path := filepath.Join(t.TempDir(), "scene.mp4")
	if err := os.WriteFile(path, []byte("media"), 0o644); err != nil {
		t.Fatal(err)
	}
	remote := NewRemoteExec(config.RemoteConfig{Host: "render.example.invalid"})
	if !remote.Enabled() {
		t.Fatal("test requires remote mode")
	}
	if !remote.canProbeMediaLocally(path) {
		t.Fatalf("remote configuration hid locally readable media: %s", path)
	}
	if remote.canProbeMediaLocally(filepath.Join(t.TempDir(), "missing.mp4")) {
		t.Fatal("missing remote-only media was considered locally readable")
	}
}
