package service

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"

	"comfyui-console/internal/models"
)

func TestPreparationChunksRetryFailedOnlyAfterNewService(t *testing.T) {
	db := newTestDB(t)
	conn, err := db.DB()
	if err != nil {
		t.Fatal(err)
	}
	conn.SetMaxOpenConns(1)
	if err := db.AutoMigrate(&models.PreparationStage{}); err != nil {
		t.Fatal(err)
	}
	var mutex sync.Mutex
	counts := map[string]int{}
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var req struct {
			Messages []struct{ Role, Content string }
		}
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			t.Error(err)
			w.WriteHeader(400)
			return
		}
		source := req.Messages[len(req.Messages)-1].Content
		key := "first"
		if strings.Contains(source, "第二段") {
			key = "second"
		}
		mutex.Lock()
		counts[key]++
		attempt := counts[key]
		mutex.Unlock()
		// The existing provider performs two attempts per failed Chat; keep it unchanged.
		if key == "second" && attempt <= 2 {
			w.WriteHeader(500)
			fmt.Fprint(w, "fixture rejected step")
			return
		}
		json.NewEncoder(w).Encode(map[string]any{"choices": []any{map[string]any{"message": map[string]string{"content": source}}}})
	}))
	defer upstream.Close()
	service := func() *Service {
		return &Service{DB: db, TextProviderFact: NewTextProviderFactory(&mockGetter{settings: map[string]string{SettingTextProvider: "llama", SettingLlamaBaseURL: upstream.URL, SettingLlamaModel: "fixture"}})}
	}
	source := "summary:\n" + strings.Repeat("第一段。", 250) + "\n\ndetailed_description:\n" + strings.Repeat("第二段。", 250)
	sc := &models.Scene{ID: 2, ProjectID: 1}
	if _, _, err := service().translatePreparedH3(sc, "unchanged-content", "en", "translate", source); err == nil {
		t.Fatal("partial failure must not pretend the whole draft succeeded")
	}
	out, _, err := service().translatePreparedH3(sc, "unchanged-content", "en", "translate", source)
	if err != nil || out != source {
		t.Fatalf("out chars=%d err=%v", len(out), err)
	}
	mutex.Lock()
	defer mutex.Unlock()
	if counts["first"] != 1 || counts["second"] != 3 {
		t.Fatalf("successful segment retried: %+v", counts)
	}
}

func TestPreparationWarningIsReviewableButNotReusable(t *testing.T) {
	db := newTestDB(t)
	if err := db.AutoMigrate(&models.PreparationStage{}); err != nil {
		t.Fatal(err)
	}
	calls := 0
	for i := 0; i < 2; i++ {
		result, err := preparationStep(db, 1, 2, "translation_en_chunk_1", "v1", "s", "u", func() (string, error) {
			calls++
			return `{"text":"reviewable","warnings":["structure recovered"]}`, nil
		})
		if err != nil || !strings.Contains(result, "reviewable") {
			t.Fatal(result, err)
		}
	}
	var row models.PreparationStage
	db.First(&row)
	if calls != 2 || row.Status != "warning" || row.Attempts != 2 || row.ReuseCount != 0 {
		t.Fatalf("warnings incorrectly reused: calls=%d row=%+v", calls, row)
	}
}
