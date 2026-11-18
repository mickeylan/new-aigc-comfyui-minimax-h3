package service

import (
	"encoding/json"
	"fmt"
	"strings"
	"sync"

	"comfyui-console/internal/models"
)

type preparedTranslation struct {
	Text     string   `json:"text"`
	Warnings []string `json:"warnings"`
}

func (s *Service) translatePreparedH3(sc *models.Scene, sourceHash, language, system, source string) (string, []string, error) {
	chunks := []string{source}
	if len([]rune(source)) >= h3ChunkedTranslationMinRunes {
		chunks = splitH3TranslationChunks(source)
		system += " The input is one section of a longer prompt: translate only this section and do not add headings or content from other sections."
	}
	values := make([]preparedTranslation, len(chunks))
	errs := make([]error, len(chunks))
	slots := make(chan struct{}, h3TranslationConcurrency)
	version := preparationJSONVersion(preparationProviderVersion(s.TextProviderFact), sceneVideoPromptCompilerVersion, sourceHash, language)
	var wg sync.WaitGroup
	for i, chunk := range chunks {
		wg.Add(1)
		go func(i int, chunk string) {
			defer wg.Done()
			slots <- struct{}{}
			defer func() { <-slots }()
			body := strings.TrimSpace(chunk)
			start := strings.Index(chunk, body)
			raw, err := preparationStep(s.DB, sc.ProjectID, sc.ID, fmt.Sprintf("translation_%s_chunk_%d", language, i+1), version, system, body, func() (string, error) {
				text, warnings, err := translateH3Masked(s.TextProviderFact.Chat, system, body, nil)
				if err != nil {
					return "", err
				}
				data, err := json.Marshal(preparedTranslation{Text: text, Warnings: warnings})
				return string(data), err
			})
			if err != nil {
				errs[i] = err
				return
			}
			if err = json.Unmarshal([]byte(raw), &values[i]); err != nil {
				errs[i] = err
				return
			}
			values[i].Text = chunk[:start] + values[i].Text + chunk[start+len(body):]
		}(i, chunk)
	}
	wg.Wait()
	parts := make([]string, len(chunks))
	var warnings []string
	for i, value := range values {
		if errs[i] != nil {
			return "", nil, fmt.Errorf("第%d/%d段翻译失败（其他已完成步骤保留，下次仅重试失败步骤）：%w", i+1, len(chunks), errs[i])
		}
		parts[i] = value.Text
		for _, warning := range value.Warnings {
			warnings = append(warnings, fmt.Sprintf("第%d段：%s", i+1, warning))
		}
	}
	return strings.Join(parts, ""), warnings, nil
}
