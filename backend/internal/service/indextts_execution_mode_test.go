package service

import (
	"testing"

	"comfyui-console/internal/config"
	"comfyui-console/internal/models"
)

func TestIndexTTSExecutionModeDefaultsToComfyUIAndCanBeSelected(t *testing.T) {
	db := safetyDB(t, &models.Setting{})
	cfg := config.Default()
	cfg.IndexTTS.Enabled = true
	cfg.IndexTTS.ExecutionMode = ""
	volc := NewVolcClient(db)
	svc := &ProjectService{cfg: cfg, db: db, volc: volc}
	if got := svc.indexTTSExecutionMode(); got != "comfyui" {
		t.Fatalf("blank mode=%q; want comfyui", got)
	}
	volc.UpdateSettings(map[string]string{SettingIndexTTSExecutionMode: "native_local"})
	if got := svc.indexTTSExecutionMode(); got != "native_local" {
		t.Fatalf("selected mode=%q", got)
	}
	volc.UpdateSettings(map[string]string{SettingIndexTTSExecutionMode: "disabled"})
	if got := svc.indexTTSExecutionMode(); got != "disabled" {
		t.Fatalf("disabled mode=%q", got)
	}
}

func TestIndexTTSExecutionModeNeverAcceptsUnknownFallback(t *testing.T) {
	db := safetyDB(t, &models.Setting{})
	cfg := config.Default()
	cfg.IndexTTS.Enabled = true
	volc := NewVolcClient(db)
	volc.UpdateSettings(map[string]string{SettingIndexTTSExecutionMode: "surprise_local"})
	svc := &ProjectService{cfg: cfg, db: db, volc: volc}
	if got := svc.indexTTSExecutionMode(); got != "comfyui" {
		t.Fatalf("unknown mode=%q; want safe comfyui default", got)
	}
}
