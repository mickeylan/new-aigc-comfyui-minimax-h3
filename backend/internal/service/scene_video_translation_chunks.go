package service

import (
	"fmt"
	"regexp"
	"strings"
	"sync"
	"unicode"
)

// A long H3 prompt translated in one call is slow and fragile: the whole prompt
// waits on one model response, and a single dropped marker anywhere pays for the
// whole prompt again. Chinese previews of long prompts are therefore translated per
// section and per [Shot N], in parallel, and a failed section is retried alone.
const (
	h3ChunkedTranslationMinRunes = 1500
	h3TranslationConcurrency     = 4
	h3TranslationCacheLimit      = 512
)

// Chunks open at a section heading or a [Shot N] at the start of a line.
var h3TranslationBoundaryRE = regexp.MustCompile(`(?m)^[ \t]*(?:(?:subject_definitions|summary|retention_analysis|detailed_description|integrated_multimodal_description|overall_soundscape|non_diegetic_music)[ \t]*:|\[Shot\s+\d+\])`)

type h3ChatFunc func(system, user string) (string, error)

// h3TranslationCache keeps the raw masked translation of a chunk. Masked text does
// not contain the protocol tokens, so one entry also serves the same prose with
// other picture numbers or speakers; the current tokens are restored on every hit.
// Template sections repeat across scenes, and after a timed-out request the chunks
// that did finish make the retry cheap.
type h3TranslationCache struct {
	mu    sync.Mutex
	items map[string]string
}

func newH3TranslationCache() *h3TranslationCache {
	return &h3TranslationCache{items: map[string]string{}}
}

var h3ChunkTranslationCache = newH3TranslationCache()

func (c *h3TranslationCache) get(key string) (string, bool) {
	c.mu.Lock()
	defer c.mu.Unlock()
	value, ok := c.items[key]
	return value, ok
}

func (c *h3TranslationCache) put(key, value string) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if len(c.items) >= h3TranslationCacheLimit {
		c.items = map[string]string{}
	}
	c.items[key] = value
}

// translateH3Masked translates one prompt or chunk with its protocol structure
// masked out, and pays for one more call when a structure marker could not be put
// back. Only translations that restored cleanly are cached.
func translateH3Masked(chat h3ChatFunc, system, source string, cache *h3TranslationCache) (string, []string, error) {
	masked, tokens := maskH3Structure(source)
	key := hashString(system + "\x00" + masked)
	if cache != nil {
		if raw, ok := cache.get(key); ok {
			translated, warnings, _ := restoreH3StructureWithRecovery(stripH3CodeFence(raw), masked, tokens)
			return translated, warnings, nil
		}
	}
	raw, err := chat(system, masked)
	if err != nil {
		return "", nil, err
	}
	translated, warnings, missing := restoreH3StructureWithRecovery(stripH3CodeFence(raw), masked, tokens)
	if missing > 0 {
		// Translators drop a shot marker often enough that a single pass is not
		// dependable, and a marker that could not be placed positionally would
		// silently flatten the shot timeline. One more call is cheaper than a
		// wrong video; the better of the two results wins.
		if retry, retryErr := chat(system, masked); retryErr == nil {
			candidate, candidateWarnings, candidateMissing := restoreH3StructureWithRecovery(stripH3CodeFence(retry), masked, tokens)
			if candidateMissing < missing {
				translated, warnings, missing = candidate, candidateWarnings, candidateMissing
			}
			warnings = append(warnings, "首轮翻译丢失结构标记且无法按位置补回，已自动重译一次")
		}
	}
	if cache != nil && len(warnings) == 0 {
		cache.put(key, raw)
	}
	return translated, warnings, nil
}

// splitH3TranslationChunks cuts a prompt into chunks whose concatenation is the
// prompt byte for byte. A boundary inside a dialogue block is ignored, and a chunk
// without prose (such as a bare "detailed_description:" line) joins the next one.
func splitH3TranslationChunks(source string) []string {
	dialogues := dialogueBlockRE.FindAllStringIndex(source, -1)
	insideDialogue := func(at int) bool {
		for _, span := range dialogues {
			if at > span[0] && at < span[1] {
				return true
			}
		}
		return false
	}
	cuts := []int{0}
	for _, loc := range h3TranslationBoundaryRE.FindAllStringIndex(source, -1) {
		if loc[0] > 0 && !insideDialogue(loc[0]) {
			cuts = append(cuts, loc[0])
		}
	}
	cuts = append(cuts, len(source))
	var chunks []string
	pending := ""
	for i := 0; i+1 < len(cuts); i++ {
		part := pending + source[cuts[i]:cuts[i+1]]
		if !h3ChunkHasProse(part) && i+2 < len(cuts) {
			pending = part
			continue
		}
		pending = ""
		chunks = append(chunks, part)
	}
	return chunks
}

func h3ChunkHasProse(chunk string) bool {
	masked, _ := maskH3Structure(chunk)
	return strings.IndexFunc(h3MarkerRE.ReplaceAllString(masked, ""), unicode.IsLetter) >= 0
}

// translateH3PromptChunked translates the chunks in parallel and joins them in
// order. Surrounding whitespace of each chunk is kept outside the model call, so
// section spacing survives. Every chunk is awaited even after a failure, which lets
// the finished ones reach the cache for the next attempt.
func translateH3PromptChunked(chat h3ChatFunc, system, source string) (string, []string, error) {
	chunks := splitH3TranslationChunks(source)
	system += " The input is one section of a longer prompt: translate only this section and do not add headings or content from other sections."
	results := make([]string, len(chunks))
	chunkWarnings := make([][]string, len(chunks))
	errs := make([]error, len(chunks))
	slots := make(chan struct{}, h3TranslationConcurrency)
	var wg sync.WaitGroup
	for i, chunk := range chunks {
		wg.Add(1)
		go func(i int, chunk string) {
			defer wg.Done()
			slots <- struct{}{}
			defer func() { <-slots }()
			body := strings.TrimSpace(chunk)
			start := strings.Index(chunk, body)
			lead, trail := chunk[:start], chunk[start+len(body):]
			translated, warnings, err := translateH3Masked(chat, system, body, h3ChunkTranslationCache)
			results[i], chunkWarnings[i], errs[i] = lead+translated+trail, warnings, err
		}(i, chunk)
	}
	wg.Wait()
	for i, err := range errs {
		if err != nil {
			return "", nil, fmt.Errorf("第%d/%d段翻译失败：%w", i+1, len(chunks), err)
		}
	}
	var warnings []string
	for i, list := range chunkWarnings {
		for _, warning := range list {
			warnings = append(warnings, fmt.Sprintf("第%d段：%s", i+1, warning))
		}
	}
	return strings.Join(results, ""), warnings, nil
}
