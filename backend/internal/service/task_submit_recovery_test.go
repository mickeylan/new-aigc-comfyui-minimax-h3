package service

import (
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"

	"comfyui-console/internal/models"
	"github.com/gin-gonic/gin"
	"gorm.io/gorm"
)

func TestRecoveryAttemptDurableBeforeNetworkAndLostResponse(t *testing.T) {
	for _, response := range []string{"disconnect", "malformed", "gateway", "reject"} {
		t.Run(response, func(t *testing.T) {
			var s *TaskService
			var posts atomic.Int32
			handler := func(w http.ResponseWriter, r *http.Request) {
				switch r.URL.Path {
				case "/queue":
					fmt.Fprint(w, `{"queue_running":[],"queue_pending":[]}`)
				case "/object_info/RecoveryStub", "/object_info":
					fmt.Fprint(w, `{"RecoveryStub":{}}`)
				case "/system_stats":
					fmt.Fprint(w, `{"devices":[{"vram_free":30000000000,"vram_total":40000000000}]}`)
				case "/prompt":
					posts.Add(1)
					got := readRecoveryTask(t, s, "recovery-test")
					if got.SubmitAttemptedAt == nil || got.ProviderOutcome != "submitting" || got.Status != "queued" {
						t.Errorf("intent not durable before network: %+v", got)
					}
					switch response {
					case "disconnect":
						conn, _, err := w.(http.Hijacker).Hijack()
						if err != nil {
							t.Error(err)
							return
						}
						conn.Close()
					case "malformed":
						fmt.Fprint(w, `{`)
					case "gateway":
						http.Error(w, "gateway lost response", 502)
					case "reject":
						http.Error(w, `{"error":"invalid input"}`, 400)
					}
				default:
					http.NotFound(w, r)
				}
			}
			svc, task, _ := recoveryFixture(t, handler)
			s = svc
			tpl := models.Template{Code: "recovery-test", Enabled: true, InputsJSON: `[]`, WorkflowJSON: `{"1":{"class_type":"RecoveryStub","inputs":{}}}`}
			if err := s.db.Create(&tpl).Error; err != nil {
				t.Fatal(err)
			}
			if err := s.db.Model(task).Updates(map[string]any{"status": "pending", "comfy_prompt_id": "", "submit_attempted_at": nil, "provider_outcome": "", "template_id": tpl.ID, "params_json": "{}"}).Error; err != nil {
				t.Fatal(err)
			}
			// Once a submission was attempted, Execute preserves its binding and
			// reports the provider result through durable task state, not a creation error.
			if err := s.Execute(task.TaskID); err != nil {
				t.Fatalf("attempted submission must retain its binding: %v", err)
			}
			got := readRecoveryTask(t, s, task.TaskID)
			if posts.Load() != 1 || got.SubmitAttemptedAt == nil {
				t.Fatalf("submission not recorded exactly once: posts=%d task=%+v", posts.Load(), got)
			}
			if response == "reject" {
				if got.Status != "failed" {
					t.Fatalf("explicit rejection should fail: %+v", got)
				}
			} else {
				if got.Status != "queued" || got.ProviderOutcome != "unknown" {
					t.Fatalf("ambiguous response must remain recoverable: %+v", got)
				}
				s.recoverStartupOrphans()
				_ = s.Execute(task.TaskID)
				if posts.Load() != 1 {
					t.Fatalf("restart resubmitted %d times", posts.Load())
				}
			}
		})
	}
}

func TestRecoveryAttemptWriteFailurePreventsNetwork(t *testing.T) {
	var posts atomic.Int32
	s, task, _ := recoveryFixture(t, func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/queue":
			fmt.Fprint(w, `{"queue_running":[],"queue_pending":[]}`)
		case "/object_info/RecoveryStub", "/object_info":
			fmt.Fprint(w, `{"RecoveryStub":{}}`)
		case "/system_stats":
			fmt.Fprint(w, `{"devices":[{"vram_free":30000000000,"vram_total":40000000000}]}`)
		case "/prompt":
			posts.Add(1)
			http.Error(w, "must not submit", 500)
		default:
			http.NotFound(w, r)
		}
	})
	tpl := models.Template{Code: "write-failure", Enabled: true, InputsJSON: `[]`, WorkflowJSON: `{"1":{"class_type":"RecoveryStub","inputs":{}}}`}
	if err := s.db.Create(&tpl).Error; err != nil {
		t.Fatal(err)
	}
	if err := s.db.Model(task).Updates(map[string]any{"status": "pending", "comfy_prompt_id": "", "submit_attempted_at": nil, "template_id": tpl.ID, "params_json": "{}"}).Error; err != nil {
		t.Fatal(err)
	}
	var blocked atomic.Bool
	if err := s.db.Callback().Update().Before("gorm:update").Register("test:reject_attempt", func(tx *gorm.DB) {
		if values, ok := tx.Statement.Dest.(map[string]any); ok && values["submit_attempted_at"] != nil {
			blocked.Store(true)
			tx.AddError(errors.New("attempt write rejected"))
		}
	}); err != nil {
		t.Fatal(err)
	}
	if err := s.Execute(task.TaskID); err == nil {
		t.Fatal("intent write failure must refuse submission")
	}
	if !blocked.Load() || posts.Load() != 0 || readRecoveryTask(t, s, task.TaskID).SubmitAttemptedAt != nil {
		t.Fatalf("fail-closed guard not measured: blocked=%v posts=%d", blocked.Load(), posts.Load())
	}
}

func TestRecoveryGetIsReadOnlyAndExplicitQueryDoesNotSubmit(t *testing.T) {
	var requests atomic.Int32
	s, task, _ := recoveryFixture(t, func(w http.ResponseWriter, r *http.Request) {
		requests.Add(1)
		if r.Method != "GET" {
			t.Errorf("unexpected mutation %s", r.Method)
		}
		http.Error(w, "offline", 503)
	})
	svc := &Service{DB: s.db, Tasks: s}
	router := gin.New()
	router.GET("/api/tasks/:id", svc.HandleGetTask)
	router.POST("/api/tasks/:id/reconcile", svc.HandleReconcileTask)
	for i := 0; i < 2; i++ {
		w := httptest.NewRecorder()
		router.ServeHTTP(w, httptest.NewRequest("GET", "/api/tasks/"+task.TaskID, nil))
		if w.Code != 200 {
			t.Fatal(w.Code)
		}
	}
	if requests.Load() != 0 {
		t.Fatal("GET queried upstream")
	}
	w := httptest.NewRecorder()
	router.ServeHTTP(w, httptest.NewRequest("POST", "/api/tasks/"+task.TaskID+"/reconcile", nil))
	if w.Code != 200 || requests.Load() == 0 {
		t.Fatalf("explicit query not run: HTTP %d, requests=%d", w.Code, requests.Load())
	}
	got := readRecoveryTask(t, s, task.TaskID)
	if got.Status != "running" || got.ProviderOutcome != "unknown" || got.ComfyPromptID != "original" {
		t.Fatalf("outage lost identity: %+v", got)
	}
}
