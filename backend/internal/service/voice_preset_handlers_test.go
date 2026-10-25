package service

import (
	"testing"

	"comfyui-console/internal/models"
)

func TestProjectCustomVoiceFileIsScopedAndExcludesGeneratedOrPresetAudio(t *testing.T) {
	db := safetyDB(t, &models.UploadFile{})
	svc := &Service{DB: db}
	rows := []models.UploadFile{
		{TaskID: "1", Type: "audio", Name: "voice_1_custom.wav", Path: "1/voice_1_custom.wav", Size: 10},
		{TaskID: "1", Type: "audio", Name: "voice_narrator_custom.wav", Path: "1/voice_narrator_custom.wav", Size: 11},
		{TaskID: "1", Type: "audio", Name: "voice_preset_deadbeef.wav", Path: "1/voice_preset_deadbeef.wav", Size: 12},
		{TaskID: "1", Type: "audio", Name: "dub_52_hash.flac", Path: "1/dub_52_hash.flac", Size: 13},
		{TaskID: "2", Type: "audio", Name: "voice_other.wav", Path: "2/voice_other.wav", Size: 14},
	}
	for i := range rows {
		if err := db.Create(&rows[i]).Error; err != nil {
			t.Fatal(err)
		}
	}
	for _, name := range []string{"voice_1_custom.wav", "voice_narrator_custom.wav"} {
		got, err := svc.projectCustomVoiceFile(1, name)
		if err != nil || got != name {
			t.Fatalf("name=%q got=%q err=%v", name, got, err)
		}
	}
	for _, name := range []string{"voice_preset_deadbeef.wav", "dub_52_hash.flac", "voice_other.wav", "../voice_1_custom.wav"} {
		if _, err := svc.projectCustomVoiceFile(1, name); err == nil {
			t.Fatalf("unsafe/non-custom voice accepted: %q", name)
		}
	}
}
