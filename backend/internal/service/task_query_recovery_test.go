package service

import (
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"
	"time"

	"comfyui-console/internal/config"
	"comfyui-console/internal/models"
	"github.com/glebarez/sqlite"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"
)

func recoveryFixture(t *testing.T, handler http.HandlerFunc) (*TaskService, *models.Task, *httptest.Server) {
	t.Helper()
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{Logger: logger.Default.LogMode(logger.Silent)})
	if err != nil {
		t.Fatal(err)
	}
	sqlDB, err := db.DB()
	if err != nil {
		t.Fatal(err)
	}
	sqlDB.SetMaxOpenConns(1)
	t.Cleanup(func() { sqlDB.Close() })
	if err := db.AutoMigrate(&models.Task{}, &models.Template{}, &models.Instance{}, &models.Event{}, &models.Scene{}, &models.Setting{}); err != nil {
		t.Fatal(err)
	}
	server := httptest.NewServer(handler)
	t.Cleanup(server.Close)
	cfg := config.Default()
	remote := NewRemoteExec(config.RemoteConfig{})
	svc := NewTaskService(cfg, db, NewInstanceManager(cfg, db, remote), nil, NewHub(), remote)
	inst := models.Instance{Host: "127.0.0.1", Port: comfyTestPort(server), Status: "running"}
	if err := db.Create(&inst).Error; err != nil {
		t.Fatal(err)
	}
	now := time.Now()
	task := &models.Task{TaskID: "recovery-test", Status: "running", InstanceID: &inst.ID, Port: &inst.Port, GPUIndex: &inst.GPUIndex, ComfyPromptID: "original", SubmitAttemptedAt: &now, ProviderOutcome: "accepted"}
	if err := db.Create(task).Error; err != nil {
		t.Fatal(err)
	}
	return svc, task, server
}

func readRecoveryTask(t *testing.T, s *TaskService, id string) models.Task {
	t.Helper()
	var result models.Task
	if err := s.db.Where("task_id = ?", id).First(&result).Error; err != nil {
		t.Fatal(err)
	}
	return result
}

func TestRecoverySubmitClassification(t *testing.T) {
	for _, tc := range []struct {
		name     string
		code     int
		body     string
		rejected bool
		success  bool
	}{
		{"accepted", 200, `{"prompt_id":"original"}`, false, true},
		{"validation", 400, `{"error":"bad input"}`, true, false},
		{"lost-body", 200, `{`, false, false},
		{"empty-id", 200, `{"prompt_id":""}`, false, false},
		{"gateway", 502, `gateway failure`, false, false},
		{"timeout-response", 408, `timeout`, false, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { w.WriteHeader(tc.code); fmt.Fprint(w, tc.body) }))
			defer srv.Close()
			_, err := NewComfyClient("127.0.0.1", comfyTestPort(srv)).SubmitPrompt(map[string]any{}, "test")
			var rejected *PromptRejectedError
			if errors.As(err, &rejected) != tc.rejected || (err == nil) != tc.success {
				t.Fatalf("classification: err=%v rejected=%v", err, errors.As(err, &rejected))
			}
		})
	}
}

func TestRecoveryReconcileNeverSubmitsAndKeepsSceneBinding(t *testing.T) {
	var posts atomic.Int32
	history := `{"original":{"status":{"completed":true,"status_str":"success"},"outputs":{}}}`
	s, task, _ := recoveryFixture(t, func(w http.ResponseWriter, r *http.Request) {
		if r.Method == "POST" {
			posts.Add(1)
			t.Errorf("unexpected POST %s", r.URL.Path)
		}
		switch r.URL.Path {
		case "/history/original":
			fmt.Fprint(w, history)
		case "/queue":
			fmt.Fprint(w, `{"queue_running":[],"queue_pending":[]}`)
		default:
			http.NotFound(w, r)
		}
	})
	scene := models.Scene{VideoTaskID: task.TaskID, Status: "video_running"}
	if err := s.db.Create(&scene).Error; err != nil {
		t.Fatal(err)
	}
	for i := 0; i < 3; i++ {
		s.reconcile(task.TaskID)
	}
	got := readRecoveryTask(t, s, task.TaskID)
	if got.ProviderOutcome != "awaiting_result" || got.Status != "running" || got.ComfyPromptID != "original" {
		t.Fatalf("lost evidence: %+v", got)
	}
	history = `{"original":{"status":{"completed":true,"status_str":"success"},"outputs":{"9":{"videos":[{"filename":"original.mp4"}]}}}}`
	s.reconcile(task.TaskID)
	s.reconcile(task.TaskID)
	got = readRecoveryTask(t, s, task.TaskID)
	var bound models.Scene
	if err := s.db.First(&bound, scene.ID).Error; err != nil {
		t.Fatal(err)
	}
	if got.Status != "success" || got.ResultFiles == "" || bound.VideoTaskID != task.TaskID || posts.Load() != 0 {
		t.Fatalf("recovery or binding failed: task=%+v scene=%+v posts=%d", got, bound, posts.Load())
	}
}

func TestRecoveryUnknownRestartAndExecuteDoNotSubmit(t *testing.T) {
	var requests atomic.Int32
	s, task, _ := recoveryFixture(t, func(w http.ResponseWriter, r *http.Request) { requests.Add(1); http.Error(w, "must not call", 500) })
	if err := s.db.Model(task).Updates(map[string]any{"status": "pending", "comfy_prompt_id": ""}).Error; err != nil {
		t.Fatal(err)
	}
	s.recoverStartupOrphans()
	if err := s.Execute(task.TaskID); err == nil {
		t.Fatal("Execute must explicitly refuse an existing submission attempt")
	}
	got := readRecoveryTask(t, s, task.TaskID)
	if requests.Load() != 0 || got.ProviderOutcome != "unknown" || got.SubmitAttemptedAt == nil {
		t.Fatalf("resubmitted or lost evidence: %+v requests=%d", got, requests.Load())
	}
}

func TestRecoveryWrongPromptEventsIgnored(t *testing.T) {
	s, task, _ := recoveryFixture(t, func(w http.ResponseWriter, r *http.Request) { http.NotFound(w, r) })
	for _, kind := range []string{"execution_start", "progress", "execution_error", "execution_interrupted", "execution_success"} {
		s.handleWSEvent(task.TaskID, kind, map[string]any{"prompt_id": "someone-else", "value": 99.0, "max": 100.0})
	}
	got := readRecoveryTask(t, s, task.TaskID)
	if got.Status != "running" || got.Progress != 0 || got.ComfyPromptID != "original" {
		t.Fatalf("foreign event modified task: %+v", got)
	}
	if err := s.db.Model(task).Update("comfy_prompt_id", "").Error; err != nil {
		t.Fatal(err)
	}
	s.handleWSEvent(task.TaskID, "execution_start", map[string]any{"prompt_id": "someone-else"})
	if got := readRecoveryTask(t, s, task.TaskID); got.ComfyPromptID != "" {
		t.Fatal("unknown ID bound from foreign event")
	}
}

func TestRecoveryCancelAndRequeueDoNotInterruptOtherPrompts(t *testing.T) {
	for _, state := range []string{"running", "other", "unreachable", "delete-failed", "pending"} {
		t.Run(state, func(t *testing.T) {
			var interrupt, deletes atomic.Int32
			s, task, _ := recoveryFixture(t, func(w http.ResponseWriter, r *http.Request) {
				switch r.URL.Path {
				case "/interrupt":
					interrupt.Add(1)
					http.Error(w, "forbidden", 500)
				case "/queue":
					if r.Method == "POST" {
						deletes.Add(1)
						if state == "delete-failed" {
							http.Error(w, "delete failed", 500)
							return
						}
						fmt.Fprint(w, `{}`)
						return
					}
					if state == "unreachable" {
						http.Error(w, "offline", 503)
						return
					}
					switch {
					case state == "running":
						fmt.Fprint(w, `{"queue_running":[[1,"original"]],"queue_pending":[]}`)
					case state == "pending" && deletes.Load() > 0:
						fmt.Fprint(w, `{"queue_running":[],"queue_pending":[]}`)
					case state == "pending" || state == "delete-failed":
						fmt.Fprint(w, `{"queue_running":[[1,"other"]],"queue_pending":[[2,"original"]]}`)
					default:
						fmt.Fprint(w, `{"queue_running":[[1,"other"]],"queue_pending":[]}`)
					}
				default:
					http.NotFound(w, r)
				}
			})
			if err := s.db.Model(task).Update("status", "queued").Error; err != nil {
				t.Fatal(err)
			}
			if err := s.RequeueTask(task.TaskID); err == nil {
				t.Fatal("requeue must refuse submitted task")
			}
			if err := s.CancelTask(task.TaskID); err != nil {
				t.Fatal(err)
			}
			got := readRecoveryTask(t, s, task.TaskID)
			if interrupt.Load() != 0 || got.ComfyPromptID != "original" {
				t.Fatal("interrupt or cleared evidence")
			}
			if state == "pending" {
				if got.Status != "cancelled" {
					t.Fatalf("confirmed deletion not cancelled: %+v", got)
				}
			} else {
				if got.Status != "queued" || got.ProviderOutcome != "cancel_requested" {
					t.Fatalf("unconfirmed cancellation lied: %+v", got)
				}
				s.reconcile(task.TaskID)
				if latest := readRecoveryTask(t, s, task.TaskID); latest.ProviderOutcome != "cancel_requested" {
					t.Fatalf("query erased explicit stop request: %+v", latest)
				}
			}
		})
	}
}
