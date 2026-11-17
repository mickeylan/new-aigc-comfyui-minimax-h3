package service

import (
	"errors"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
)

const chunkedTranslationSource = `subject_definitions:
<Subject 1> is the character named “Liu Le’er” in the four-view reference image <Picture 1>.
<Subject 2> is the character named “Tall Man in Blue Robes” in the four-view reference image <Picture 2>.
<Picture 3> establishes the full-frame environment and spatial layout for the entire video: the referenced environment is named “Yellowed Wasteland.”

summary:
[Reference Generation] Based on the defined reference images and shot timeline, generate a 7-second video presented as one continuous full-frame scene.

retention_analysis:
<Subject 1> (appears in [Shot 1], [Shot 2]): fully_preserved - identity and visible appearance.
<Subject 2> (appears in [Shot 1]): fully_preserved - identity and visible appearance.

detailed_description:
[Shot 1] begins with the reference composition of <Picture 3>. <Subject 2> spreads his arms and crouches, pressing both palms against the ropes of the black net. <Subject 1> (S1) says <d>[Chinese] 快走！ [Shot 9] 不是标记</d> and squeezes through between his legs.

[Shot 2] At 00:03.800, the camera tracks laterally across the gravel, where a streak of cyan light contracts into a wooden silhouette and collapses into <Subject 1>.

overall_soundscape:
Only natural environmental sounds and narrative sound effects caused by physical actions are present.

non_diegetic_music:
There is no non-narrative music.`

// echoChat returns the masked input unchanged, so a correct restore reproduces the source.
func echoChat(calls *atomic.Int32) h3ChatFunc {
	return func(system, user string) (string, error) {
		calls.Add(1)
		return user, nil
	}
}

func TestSplitH3TranslationChunksRejoinsExactlyAndKeepsDialogueWhole(t *testing.T) {
	chunks := splitH3TranslationChunks(chunkedTranslationSource)
	if strings.Join(chunks, "") != chunkedTranslationSource {
		t.Fatal("chunks do not rejoin to the source byte for byte")
	}
	// 5 sections, detailed_description split into its 2 shots; the bare
	// "detailed_description:" heading joins [Shot 1] instead of standing alone.
	if len(chunks) != 7 {
		t.Fatalf("chunks=%d: %q", len(chunks), chunks)
	}
	if !strings.HasPrefix(chunks[3], "detailed_description:\n[Shot 1]") {
		t.Fatalf("bare heading was not merged into the first shot: %q", chunks[3])
	}
	for _, chunk := range chunks {
		if strings.Count(chunk, "<d>") != strings.Count(chunk, "</d>") {
			t.Fatalf("dialogue block was cut: %q", chunk)
		}
	}
}

func TestTranslateH3PromptChunkedRestoresProtocolAndRunsInParallel(t *testing.T) {
	h3ChunkTranslationCache = newH3TranslationCache()
	var active, peak atomic.Int32
	var calls atomic.Int32
	release := make(chan struct{})
	var once sync.Once
	chat := func(system, user string) (string, error) {
		calls.Add(1)
		now := active.Add(1)
		defer active.Add(-1)
		for {
			old := peak.Load()
			if now <= old || peak.CompareAndSwap(old, now) {
				break
			}
		}
		if now >= 2 {
			once.Do(func() { close(release) })
		}
		<-release
		return user, nil
	}
	got, warnings, err := translateH3PromptChunked(chat, "translate", chunkedTranslationSource)
	if err != nil {
		t.Fatal(err)
	}
	if got != chunkedTranslationSource || len(warnings) != 0 {
		t.Fatalf("protocol not restored exactly: warnings=%v\n%s", warnings, got)
	}
	if err := validateH3Translation(chunkedTranslationSource, got); err != nil {
		t.Fatal(err)
	}
	if calls.Load() != 7 || peak.Load() < 2 || peak.Load() > h3TranslationConcurrency {
		t.Fatalf("calls=%d peak=%d", calls.Load(), peak.Load())
	}
}

func TestTranslateH3PromptChunkedRetriesOnlyTheChunkThatLostAMarker(t *testing.T) {
	h3ChunkTranslationCache = newH3TranslationCache()
	var mu sync.Mutex
	perChunk := map[string]int{}
	chat := func(system, user string) (string, error) {
		mu.Lock()
		perChunk[user]++
		first := perChunk[user] == 1
		mu.Unlock()
		// The second shot drops its line-opening [Shot 2] marker and the line break
		// before it on the first pass, which cannot be put back by position.
		if first && strings.Contains(user, "streak of cyan light") {
			return strings.Replace(user, "[[[T", "[[[X", 1), nil
		}
		return user, nil
	}
	got, warnings, err := translateH3PromptChunked(chat, "translate", chunkedTranslationSource)
	if err != nil {
		t.Fatal(err)
	}
	if got != chunkedTranslationSource {
		t.Fatalf("retry did not recover the chunk:\n%s", got)
	}
	retried := 0
	for chunk, n := range perChunk {
		if n > 1 {
			retried++
			if !strings.Contains(chunk, "streak of cyan light") {
				t.Fatalf("an intact chunk was retranslated: %q", chunk)
			}
		}
	}
	if retried != 1 || len(warnings) != 1 || !strings.Contains(warnings[0], "已自动重译一次") {
		t.Fatalf("retried=%d warnings=%v", retried, warnings)
	}
}

func TestTranslateH3PromptChunkedCachesFinishedChunksAcrossAFailure(t *testing.T) {
	h3ChunkTranslationCache = newH3TranslationCache()
	var calls atomic.Int32
	failing := func(system, user string) (string, error) {
		calls.Add(1)
		if strings.Contains(user, "streak of cyan light") {
			return "", errors.New("upstream timeout")
		}
		return user, nil
	}
	if _, _, err := translateH3PromptChunked(failing, "translate", chunkedTranslationSource); err == nil || !strings.Contains(err.Error(), "upstream timeout") {
		t.Fatalf("chunk failure was not reported: %v", err)
	}
	if calls.Load() != 7 {
		t.Fatalf("all chunks must finish before the failure is returned, calls=%d", calls.Load())
	}
	// The retry only pays for the chunk that failed.
	calls.Store(0)
	got, _, err := translateH3PromptChunked(echoChat(&calls), "translate", chunkedTranslationSource)
	if err != nil || got != chunkedTranslationSource {
		t.Fatalf("retry err=%v got=%q", err, got)
	}
	if calls.Load() != 1 {
		t.Fatalf("retry called the model %d times, want 1", calls.Load())
	}
	// A different scene with the same template prose but other numbers reuses the cache
	// and still restores its own tokens.
	calls.Store(0)
	other := strings.ReplaceAll(chunkedTranslationSource, "<Picture 3>", "<Picture 5>")
	got, _, err = translateH3PromptChunked(echoChat(&calls), "translate", other)
	if err != nil || got != other || calls.Load() != 0 {
		t.Fatalf("cache reuse err=%v calls=%d", err, calls.Load())
	}
}

func TestSceneVideoTranslationHTTPChunksLongChinesePreview(t *testing.T) {
	h3ChunkTranslationCache = newH3TranslationCache()
	f := newTranslationHTTPFixture(t)
	f.rawResponse = true
	source := chunkedTranslationSource + "\n" + strings.Repeat("The wind keeps blowing across the empty wasteland. ", 30)
	if len([]rune(source)) < h3ChunkedTranslationMinRunes {
		t.Fatalf("fixture too short: %d", len([]rune(source)))
	}
	// The mock upstream returns this fixed reply for every chunk, so echo is
	// impossible here; only count calls and check the request succeeded.
	f.response = "translated"
	body := `{"direction":"to_zh","prompt":` + jsonString(source) + `}`
	w := translationRequest(f.svc, "POST", f.p.ID, f.sc.ID, "/translate", body)
	if w.Code != 200 {
		t.Fatalf("status=%d body=%s", w.Code, w.Body.String())
	}
	if f.calls.Load() < 7 {
		t.Fatalf("long Chinese preview was not chunked: calls=%d", f.calls.Load())
	}
}

func jsonString(s string) string {
	var b strings.Builder
	b.WriteByte('"')
	for _, r := range s {
		switch r {
		case '"':
			b.WriteString(`\"`)
		case '\\':
			b.WriteString(`\\`)
		case '\n':
			b.WriteString(`\n`)
		default:
			b.WriteRune(r)
		}
	}
	b.WriteByte('"')
	return b.String()
}
