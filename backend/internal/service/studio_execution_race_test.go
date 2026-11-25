package service

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"
)

type executionChangingProvider struct {
	stubTextProvider
	change func()
}

func (p *executionChangingProvider) Chat(system, user string) (string, error) {
	p.change()
	return p.stubTextProvider.Chat(system, user)
}

func TestStudioExecutionDiscardsReplyWhenDirectorChangesDuringModelCall(t *testing.T) {
	s, db, scene, shots := newShotRefineTestService(t, "")
	source, hash, err := s.studioExecutionSource(scene.ID)
	if err != nil {
		t.Fatal(err)
	}
	raw, _ := json.Marshal(executionPlanFixture(source, hash))
	provider := &executionChangingProvider{stubTextProvider: stubTextProvider{response: string(raw)}, change: func() {
		if err := db.Model(&shots[0]).Update("performance", "调用期间用户修改的表演").Error; err != nil {
			t.Error(err)
		}
	}}
	useExecutionProvider(t, s, provider)
	w := executionHTTP(t, s, scene.ProjectID, scene.ID, "POST", gin.H{"source_hash": hash, "context": executionContextFixture()})
	if w.Code != 409 || !strings.Contains(w.Body.String(), "规划期间导演来源已变化") || provider.calls != 1 {
		t.Fatalf("status=%d calls=%d body=%s", w.Code, provider.calls, w.Body.String())
	}
}

func TestStudioExecutionRequestRejectsInventedSourceAndMalformedDirectoryBeforeModel(t *testing.T) {
	s, _, scene, _ := newShotRefineTestService(t, "")
	_, hash, _ := s.studioExecutionSource(scene.ID)
	provider := &stubTextProvider{}
	useExecutionProvider(t, s, provider)
	invalid := executionContextFixture()
	invalid.Objects[0].AssetID = "unknown"
	for _, request := range []gin.H{
		{"source_hash": hash, "context": executionContextFixture(), "source": "客户端替换小说正文"},
		{"source_hash": hash, "context": invalid},
	} {
		w := executionHTTP(t, s, scene.ProjectID, scene.ID, "POST", request)
		if w.Code != 400 {
			t.Fatalf("status=%d body=%s", w.Code, w.Body.String())
		}
	}
	if provider.calls != 0 {
		t.Fatalf("invalid requests called model %d times", provider.calls)
	}
}
