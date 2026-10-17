package service

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"comfyui-console/internal/config"
	"comfyui-console/internal/models"
)

func TestInitSystemTemplatesDisablesLegacyRustComfyWorkflow(t *testing.T) {
	db := safetyDB(t, &models.Template{})
	legacy := models.Template{Name: "legacy", Code: "indextts_2_5_rust_dialogue", Enabled: true}
	if err := db.Create(&legacy).Error; err != nil {
		t.Fatal(err)
	}
	if err := InitSystemTemplates(db); err != nil {
		t.Fatal(err)
	}
	if err := db.Where("code = ?", legacy.Code).First(&legacy).Error; err != nil {
		t.Fatal(err)
	}
	if legacy.Enabled {
		t.Fatal("legacy Rust-in-ComfyUI workflow remains enabled")
	}
	var current models.Template
	if err := db.Where("code = ?", indexTTSComfyTemplateCode).First(&current).Error; err != nil {
		t.Fatal(err)
	}
	if !current.Enabled {
		t.Fatal("Python ComfyUI workflow is disabled")
	}
}

func TestIndexTTSComfyTemplateRendersPythonAudioWorkflow(t *testing.T) {
	tpl := loadTemplateForTest(t, "indextts_2_5_comfyui_dialogue.json")
	params := map[string]any{
		"source_dir": `E:\mickeylan\ai\index-tts`, "model_dir": `E:\mickeylan\ai\index-tts\checkpoints`,
		"device": "auto", "precision": "auto", "text": "权威原文", "language": "ZH",
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
	for _, required := range []string{"IndexTTS25LocalModelLoader", "IndexTTS25LocalGenerate", "IndexTTS25LocalSaveReport", "LoadAudio", "SaveAudio"} {
		if !containsClass(classes, required) {
			t.Fatalf("missing class %s: %v", required, classes)
		}
	}
}

func TestInstanceSupportsRequiredPythonTTSNodes(t *testing.T) {
	classes := map[string]any{
		"IndexTTS25LocalModelLoader": map[string]any{}, "IndexTTS25LocalGenerate": map[string]any{},
		"IndexTTS25LocalSaveReport": map[string]any{}, "LoadAudio": map[string]any{}, "SaveAudio": map[string]any{},
	}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		const prefix = "/object_info/"
		if strings.HasPrefix(r.URL.Path, prefix) {
			name := strings.TrimPrefix(r.URL.Path, prefix)
			if value, ok := classes[name]; ok {
				_ = json.NewEncoder(w).Encode(map[string]any{name: value})
				return
			}
			_ = json.NewEncoder(w).Encode(map[string]any{})
			return
		}
		http.NotFound(w, r)
	}))
	defer server.Close()
	cfg := config.Default()
	svc := &TaskService{cfg: cfg, manager: NewInstanceManager(cfg, nil, NewRemoteExec(config.RemoteConfig{}))}
	instance := models.Instance{Port: comfyTestPort(server)}
	if err := svc.instanceSupportsClasses(instance, []string{"IndexTTS25LocalGenerate", "SaveAudio"}); err != nil {
		t.Fatal(err)
	}
	if err := svc.instanceSupportsClasses(instance, []string{"MissingPythonTTSNode"}); err == nil {
		t.Fatal("missing custom node was accepted")
	}
}

func TestResultReportFindsAuditableJSONSidecar(t *testing.T) {
	raw, _ := json.Marshal([]map[string]string{
		{"type": "audio", "filename": "dub.wav", "subfolder": "indextts"},
		{"type": "files", "filename": "report_task.json", "subfolder": "indextts"},
	})
	if got := resultReportOf(&models.Task{ResultFiles: string(raw)}); got != "indextts/report_task.json" {
		t.Fatalf("report=%q", got)
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
