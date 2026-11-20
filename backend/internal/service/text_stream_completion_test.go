package service

import (
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

// An SSE response need not close its HTTP connection after its terminal choice.
// Further reads can block until the HTTP client timeout despite a complete answer.
type terminalChatReader struct {
	chunk                io.Reader
	readsAfterCompletion int
}

func (r *terminalChatReader) Read(p []byte) (int, error) {
	n, err := r.chunk.Read(p)
	if n > 0 {
		return n, nil
	}
	if err != nil {
		r.readsAfterCompletion++
		return 0, errors.New("connection remains open after finish_reason")
	}
	return n, err
}
func TestMiniMaxCompletedStreamDoesNotWaitForHTTPConnectionOrRetry(t *testing.T) {
	var calls atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls.Add(1)
		w.Header().Set("Content-Type", "text/event-stream")
		_, _ = io.WriteString(w, "data: {\"choices\":[{\"delta\":{\"content\":\"complete answer\"},\"finish_reason\":\"stop\"}]}\n\n")
		w.(http.Flusher).Flush()
		// Deliberately omit [DONE] and keep the connection open until the
		// caller closes its response body. This models a streaming gateway.
		<-r.Context().Done()
	}))
	defer server.Close()
	p := NewMiniMaxCodingProvider(miniMaxStreamGetter(server.URL))
	p.httpc.Timeout = time.Second
	started := time.Now()
	got, err := p.Chat("system", "user")
	if err != nil || got != "complete answer" {
		t.Fatalf("complete response lost: %q %v", got, err)
	}
	if calls.Load() != 1 {
		t.Fatalf("complete request was retried: %d", calls.Load())
	}
	if time.Since(started) >= p.httpc.Timeout {
		t.Fatal("waited for HTTP timeout after completion")
	}
}

func TestOpenAIStreamReturnsAtFinishReasonWithoutWaitingForConnectionClose(t *testing.T) {
	r := &terminalChatReader{chunk: strings.NewReader("data: {\"choices\":[{\"delta\":{\"content\":\"complete answer\"},\"finish_reason\":\"stop\"}]}\n\n")}
	got, err := readOpenAIChatStream(r, "test")
	if err != nil {
		t.Fatalf("complete answer discarded while waiting for stream close: %v", err)
	}
	if got != "complete answer" || r.readsAfterCompletion != 0 {
		t.Fatalf("got=%q extra reads=%d", got, r.readsAfterCompletion)
	}
}
