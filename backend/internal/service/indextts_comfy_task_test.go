package service

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"comfyui-console/internal/config"
	"comfyui-console/internal/models"
)

func TestIndexTTSRustTemplateRendersOrderedAudioWorkflow(t *testing.T) {
	tpl := loadTemplateForTest(t, "indextts_2_5_rust_dialogue.json")
	params := map[string]any{
		"dll_path": "", "model_dir": `E:\models\indextts25-rust`, "device_index": 0,
		"voice_key": "project-1-character-2", "text": "权威原文", "language": "ZH",
		"duration_factor": 1.0, "seed": uint64(42), "emotion_text": "克制而悲伤", "emotion_strength": 0.6,
		"_task_id": "tts-task",
	}
	files := map[string][]FileMeta{"reference_audio": {{TaskID: "1", Name: "voice.wav"}}}
	if err := normalizeTemplateFiles(&tpl, params, files); err != nil {
		t.Fatal(err)
	}
	workflow, err := (&TaskService{}).RenderWorkflow(&tpl, params)
	if err != nil {
		t.Fatal(err)
	}
	encoded, _ := json.Marshal(workflow)
	if containsPlaceholder(string(encoded)) {
		t.Fatalf("unresolved template: %s", encoded)
	}
	classes := workflowClassTypes(workflow)
	for _, required := range []string{"IndexTTS25RustModelLoader", "IndexTTS25RustPrepareVoice", "IndexTTS25RustGenerate", "LoadAudio", "SaveAudio"} {
		if !containsClass(classes, required) {
			t.Fatalf("missing class %s: %v", required, classes)
		}
	}
}

func TestInstanceSupportsRequiredRustTTSNodes(t *testing.T) {
	classes := map[string]any{
		"IndexTTS25RustModelLoader": map[string]any{}, "IndexTTS25RustPrepareVoice": map[string]any{},
		"IndexTTS25RustGenerate": map[string]any{}, "LoadAudio": map[string]any{}, "SaveAudio": map[string]any{},
	}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/object_info" {
			http.NotFound(w, r)
			return
		}
		_ = json.NewEncoder(w).Encode(classes)
	}))
	defer server.Close()
	cfg := config.Default()
	svc := &TaskService{cfg: cfg, manager: NewInstanceManager(cfg, nil, NewRemoteExec(config.RemoteConfig{}))}
	instance := models.Instance{Port: comfyTestPort(server)}
	if err := svc.instanceSupportsClasses(instance, []string{"IndexTTS25RustGenerate", "SaveAudio"}); err != nil {
		t.Fatal(err)
	}
	if err := svc.instanceSupportsClasses(instance, []string{"MissingRustNode"}); err == nil {
		t.Fatal("missing custom node was accepted")
	}
}

func TestResultAudioAcceptsComfyAudioAndAudiosShapes(t *testing.T) {
	for _, kind := range []string{"audio", "audios"} {
		raw, _ := json.Marshal([]map[string]string{{"type": kind, "filename": "dub.wav", "subfolder": "indextts"}})
		gpu := 2
		file, gotGPU := resultAudioOf(&models.Task{ResultFiles: string(raw), GPUIndex: &gpu})
		if file != "indextts/dub.wav" || gotGPU == nil || *gotGPU != gpu {
			t.Fatalf("kind=%s file=%q gpu=%v", kind, file, gotGPU)
		}
	}
}

func TestPCMWAVInfoReadsStandardPCMHeader(t *testing.T) {
	data := make([]byte, 44+22050*2)
	copy(data[:4], "RIFF")
	copy(data[8:12], "WAVE")
	copy(data[12:16], "fmt ")
	data[16] = 16
	data[20] = 1
	data[22] = 1
	data[24], data[25] = 0x22, 0x56
	data[34] = 16
	copy(data[36:40], "data")
	size := uint32(len(data) - 44)
	data[40], data[41], data[42], data[43] = byte(size), byte(size>>8), byte(size>>16), byte(size>>24)
	rate, channels, duration := pcmWAVInfo(data)
	if rate != 22050 || channels != 1 || duration != 1 {
		t.Fatalf("rate=%d channels=%d duration=%f", rate, channels, duration)
	}
}

func containsPlaceholder(value string) bool { return len(value) >= 4 && findString(value, "{{") }
func containsClass(values []string, target string) bool {
	for _, value := range values {
		if value == target {
			return true
		}
	}
	return false
}
func findString(value, target string) bool {
	for i := 0; i+len(target) <= len(value); i++ {
		if value[i:i+len(target)] == target {
			return true
		}
	}
	return false
}
