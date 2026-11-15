package service

import (
	"bytes"
	"encoding/binary"
	"encoding/json"
	"fmt"
	"math"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"comfyui-console/internal/config"
	"comfyui-console/internal/models"
)

// pcmWAV builds a 16-bit PCM WAV from mono float samples.
func pcmWAV(samples []float64, rate int) []byte {
	var buf bytes.Buffer
	buf.WriteString("RIFF")
	binary.Write(&buf, binary.LittleEndian, uint32(36+2*len(samples)))
	buf.WriteString("WAVEfmt ")
	binary.Write(&buf, binary.LittleEndian, uint32(16))
	binary.Write(&buf, binary.LittleEndian, uint16(1))
	binary.Write(&buf, binary.LittleEndian, uint16(1))
	binary.Write(&buf, binary.LittleEndian, uint32(rate))
	binary.Write(&buf, binary.LittleEndian, uint32(rate*2))
	binary.Write(&buf, binary.LittleEndian, uint16(2))
	binary.Write(&buf, binary.LittleEndian, uint16(16))
	buf.WriteString("data")
	binary.Write(&buf, binary.LittleEndian, uint32(2*len(samples)))
	for _, v := range samples {
		binary.Write(&buf, binary.LittleEndian, int16(v*32767))
	}
	return buf.Bytes()
}

func TestVoiceQualityMeasuresSpeechAndOnlyWarns(t *testing.T) {
	rate := 8000
	tone := make([]float64, rate*3)
	for i := range tone {
		tone[i] = 0.3 * math.Sin(2*math.Pi*220*float64(i)/float64(rate))
	}
	q, err := analyzeVoiceWAV(pcmWAV(tone, rate))
	if err != nil {
		t.Fatal(err)
	}
	if math.Abs(q.Duration-3) > 0.01 || q.SpeechRatio < 0.99 || q.Peak < 0.29 || voiceQualityWarning(q) != "" {
		t.Fatalf("clear voice measured as %+v warning=%q", q, voiceQualityWarning(q))
	}
	silent, err := analyzeVoiceWAV(pcmWAV(make([]float64, rate*3), rate))
	if err != nil {
		t.Fatal(err)
	}
	if voiceQualityWarning(silent) == "" {
		t.Fatalf("silent draft must carry an advisory warning: %+v", silent)
	}
	if _, err := analyzeVoiceWAV([]byte("not a wav")); err == nil {
		t.Fatal("invalid WAV accepted")
	}
}

func TestVoiceExtractRangeIsBounded(t *testing.T) {
	for _, bad := range [][2]float64{{-1, 5}, {0, 1.9}, {0, 30.1}, {math.NaN(), 5}, {0, math.NaN()}} {
		if err := validateVoiceExtractRange(bad[0], bad[1]); err == nil {
			t.Fatalf("range %v accepted", bad)
		}
	}
	for _, ok := range [][2]float64{{0, 2}, {12.5, 30}} {
		if err := validateVoiceExtractRange(ok[0], ok[1]); err != nil {
			t.Fatalf("range %v rejected: %v", ok, err)
		}
	}
}

func TestVocalSeparationTemplateKeepsOnlyDemucsVocals(t *testing.T) {
	tpl := loadAudioVideoTemplateFixture(t, "audio_vocal_separation.json")
	if tpl.Code != vocalSeparationTemplate {
		t.Fatalf("code=%q", tpl.Code)
	}
	var separation, save string
	for id, node := range tpl.Workflow {
		switch node.ClassType {
		case "AudioSeparation":
			separation = id
		case "SaveAudio":
			save = id
		}
	}
	if separation == "" || save == "" {
		t.Fatalf("template must use AudioSeparation and SaveAudio: %+v", tpl.Workflow)
	}
	// AudioSeparation RETURN_NAMES = (Bass, Drums, Other, Vocals); index 3 is Vocals.
	link, _ := tpl.Workflow[save].Inputs["audio"].([]any)
	if len(link) != 2 || link[0] != separation || link[1] != float64(3) {
		t.Fatalf("SaveAudio must read the Vocals output, got %v", tpl.Workflow[save].Inputs["audio"])
	}
	if len(tpl.Inputs) != 1 || tpl.Inputs[0].Type != "audio" || !tpl.Inputs[0].Required {
		t.Fatalf("template must take exactly one required audio input: %+v", tpl.Inputs)
	}
}

type voiceExtractFixture struct {
	ps     *ProjectService
	upload *UploadManager
	p      models.Project
	ch     models.Character
	scene  models.Scene
}

func newVoiceExtractFixture(t *testing.T) voiceExtractFixture {
	t.Helper()
	ffmpeg, err := localFFmpegPath()
	if err != nil {
		t.Skip("ffmpeg not available")
	}
	ps := newTestProjectService(t)
	if err := ps.db.AutoMigrate(&models.UploadFile{}, &models.CharacterVoiceExtraction{}, &models.Template{}, &models.Setting{}, &models.Instance{}, &models.Dialogue{}, &models.DialogueAudioCandidate{}); err != nil {
		t.Fatal(err)
	}
	root := t.TempDir()
	cfg := &config.Config{Comfy: config.ComfyConfig{ComfyDir: filepath.Join(root, "ComfyUI")}, Storage: config.StorageConfig{DataDir: filepath.Join(root, "data")}}
	remote := NewRemoteExec(config.RemoteConfig{})
	remote.SetLocalRoots([]string{cfg.Comfy.ComfyDir, cfg.Storage.DataDir, cfg.AssetsRoot()})
	ps.upload = NewUploadManager(cfg, ps.db, remote)
	ps.tasks = NewTaskService(cfg, ps.db, NewInstanceManager(cfg, ps.db, remote), nil, NewHub(), remote)
	tpl := loadTemplateForTest(t, "audio_vocal_separation.json")
	if err := ps.db.Create(&tpl).Error; err != nil {
		t.Fatal(err)
	}
	executed := []string{}
	previous := voiceExtractionExecute
	voiceExtractionExecute = func(_ *TaskService, taskID string) error { executed = append(executed, taskID); return nil }
	t.Cleanup(func() { voiceExtractionExecute = previous })

	p := models.Project{Title: "提取人声"}
	ps.db.Create(&p)
	ch := models.Character{ProjectID: p.ID, Name: "柳石", VoiceID: "old-clone", VoiceModel: "old-model"}
	ps.db.Create(&ch)
	videoDir := filepath.Join(cfg.AssetsRoot(), fmt.Sprint(p.ID))
	if err := os.MkdirAll(videoDir, 0o755); err != nil {
		t.Fatal(err)
	}
	args := []string{"-hide_banner", "-loglevel", "error", "-y", "-f", "lavfi", "-i", "sine=frequency=300:duration=6", "-f", "lavfi", "-i", "color=c=black:s=64x64:d=6",
		"-shortest", "-c:v", "mpeg4", "-c:a", "aac", filepath.Join(videoDir, "scene.mp4")}
	if out, err := runLocalProgram(ffmpeg, args, time.Minute); err != nil {
		t.Skipf("ffmpeg cannot synthesize test video: %v %s", err, out)
	}
	scene := models.Scene{ProjectID: p.ID, EpisodeN: 1, Order: 1, VideoInputFile: "scene.mp4"}
	ps.db.Create(&scene)
	ps.db.Create(&models.Dialogue{ProjectID: p.ID, SceneID: scene.ID, Order: 1, Character: "柳石", SpeechType: "dialogue", Text: "走。", Status: "ready", AudioToken: "tok"})
	return voiceExtractFixture{ps: ps, upload: ps.upload, p: p, ch: ch, scene: scene}
}

// completeVoiceTask marks the separation task successful on a fake ComfyUI that serves vocals.
func completeVoiceTask(t *testing.T, f voiceExtractFixture, taskID string, vocals []byte) {
	t.Helper()
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/view" && r.URL.Query().Get("filename") == "vocals.wav" && r.URL.Query().Get("subfolder") == "voice_extract" {
			w.Write(vocals)
			return
		}
		http.NotFound(w, r)
	}))
	t.Cleanup(server.Close)
	inst := models.Instance{GPUIndex: 0, Host: "127.0.0.1", Port: comfyTestPort(server), Status: "running"}
	f.ps.db.Create(&inst)
	files, _ := json.Marshal([]map[string]string{{"filename": "vocals.wav", "subfolder": "voice_extract", "type": "audio"}})
	port := inst.Port
	if err := f.ps.db.Model(&models.Task{}).Where("task_id = ?", taskID).Updates(map[string]any{"status": "success", "port": port, "instance_id": inst.ID, "result_files": string(files)}).Error; err != nil {
		t.Fatal(err)
	}
}

func TestVoiceExtractionDraftNeedsConfirmationBeforeBinding(t *testing.T) {
	f := newVoiceExtractFixture(t)
	row, err := f.ps.StartCharacterVoiceExtraction(&f.p, &f.ch, &f.scene, 1, 3)
	if err != nil {
		t.Fatal(err)
	}
	var task models.Task
	if err := f.ps.db.Where("task_id = ?", row.TaskID).First(&task).Error; err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(task.InputsJSON, row.SourceFile) || row.Status != voiceExtractStatusRunning {
		t.Fatalf("task must take the cut segment: inputs=%s row=%+v", task.InputsJSON, row)
	}
	segment, err := f.ps.readProjectInput(f.p.ID, row.SourceFile)
	if err != nil {
		t.Fatal(err)
	}
	if q, err := analyzeVoiceWAV(segment); err != nil || math.Abs(q.Duration-3) > 0.1 {
		t.Fatalf("segment must be the selected 3 s: %+v %v", q, err)
	}
	if _, err := f.ps.ConfirmCharacterVoiceExtraction(&f.ch, row.ID); err == nil {
		t.Fatal("a draft still separating must not be bindable")
	}

	completeVoiceTask(t, f, row.TaskID, segment)
	f.ps.syncCharacterVoiceExtractions()
	var ready models.CharacterVoiceExtraction
	f.ps.db.First(&ready, row.ID)
	if ready.Status != voiceExtractStatusReady || !strings.HasPrefix(ready.DraftFile, "voicedraft_") || ready.Warning != "" || ready.MeasuredDuration < 2.8 {
		t.Fatalf("draft not ready: %+v", ready)
	}
	var unchanged models.Character
	f.ps.db.First(&unchanged, f.ch.ID)
	if unchanged.VoiceRef != "" || unchanged.VoiceID != "old-clone" {
		t.Fatalf("draft must not touch the character before confirmation: %+v", unchanged)
	}

	confirmed, err := f.ps.ConfirmCharacterVoiceExtraction(&f.ch, row.ID)
	if err != nil {
		t.Fatal(err)
	}
	var bound models.Character
	f.ps.db.First(&bound, f.ch.ID)
	if bound.VoiceRef != confirmed.VoiceFile || !strings.HasPrefix(bound.VoiceRef, "voice_") || bound.VoiceID != "" || bound.VoiceModel != "" {
		t.Fatalf("confirmed draft not bound: %+v", bound)
	}
	svc := &Service{DB: f.ps.db}
	if _, err := svc.projectCustomVoiceFile(f.p.ID, bound.VoiceRef); err != nil {
		t.Fatalf("bound voice must be a valid custom voice: %v", err)
	}
	var dialogue models.Dialogue
	f.ps.db.Where("project_id = ?", f.p.ID).First(&dialogue)
	if dialogue.Status != "pending" || !dialogue.AudioStale {
		t.Fatalf("character dialogue audio must be invalidated: %+v", dialogue)
	}
	if _, err := f.ps.ConfirmCharacterVoiceExtraction(&f.ch, row.ID); err == nil {
		t.Fatal("a confirmed draft must not be confirmed twice")
	}
}

func TestVoiceExtractionRejectsRangePastVideoEnd(t *testing.T) {
	f := newVoiceExtractFixture(t)
	if _, err := f.ps.StartCharacterVoiceExtraction(&f.p, &f.ch, &f.scene, 5, 3); err == nil {
		t.Fatal("range beyond the 6 s video must be rejected")
	}
	var count int64
	f.ps.db.Model(&models.CharacterVoiceExtraction{}).Count(&count)
	if count != 0 {
		t.Fatalf("rejected range must not create a draft, got %d", count)
	}
}

func TestVoiceExtractionFailedTaskIsReported(t *testing.T) {
	f := newVoiceExtractFixture(t)
	row, err := f.ps.StartCharacterVoiceExtraction(&f.p, &f.ch, &f.scene, 0, 2)
	if err != nil {
		t.Fatal(err)
	}
	f.ps.db.Model(&models.Task{}).Where("task_id = ?", row.TaskID).Updates(map[string]any{"status": "failed", "error": "缺少 AudioSeparation 节点"})
	f.ps.syncCharacterVoiceExtractions()
	var got models.CharacterVoiceExtraction
	f.ps.db.First(&got, row.ID)
	if got.Status != voiceExtractStatusFailed || !strings.Contains(got.Error, "AudioSeparation") {
		t.Fatalf("failed task not reported: %+v", got)
	}
}
