package service

import (
	"comfyui-console/internal/models"
	"strings"
	"testing"
)

func TestDesignSelectionExactOccurrenceAndUnicode(t *testing.T) {
	current := " 前😀青玉，中段青玉。尾部 \n"
	offset := 8
	start, end, err := designSelection(current, "青玉", &offset)
	if err != nil || string([]rune(current)[start:end]) != "青玉" {
		t.Fatalf("start=%d end=%d err=%v", start, end, err)
	}
	for _, pos := range []int{-1, 2, 100} {
		if _, _, err := designSelection(current, "青玉", &pos); err == nil {
			t.Fatalf("invalid offset %d accepted", pos)
		}
	}
	if _, _, err := designSelection(current, "青玉", nil); err == nil {
		t.Fatal("ambiguous selection accepted")
	}
}

func TestReviseSelectionPreservesOutsideAndRejectsMalformed(t *testing.T) {
	ps := newTestProjectService(t)
	provider := &stubTextProvider{response: `{"replacement":"白玉","changes":[]}`}
	ps.textProvider = provider
	p := models.Project{Title: "局部设计"}
	ps.db.Create(&p)
	current := " 前😀青玉，中段青玉。尾部 \n"
	offset := 8
	req := AssetDesignRequest{Name: "玉佩", Current: current, Selection: "青玉", SelectionStart: &offset, Instruction: "只改第二处为白玉"}
	got, err := ps.ReviseAssetDesign(&p, AssetKindProp, req)
	if err != nil || got.Description != " 前😀青玉，中段白玉。尾部 \n" {
		t.Fatalf("draft=%+v err=%v", got, err)
	}
	for _, response := range []string{`{"description":"不允许整稿替代"}`, `{"replacement":""}`, "未经格式化的整篇描述"} {
		provider.response = response
		if _, err := ps.ReviseAssetDesign(&p, AssetKindProp, req); err == nil {
			t.Fatalf("invalid patch accepted: %s", response)
		}
	}
	before := provider.calls
	req.SelectionStart = nil
	if _, err := ps.ReviseAssetDesign(&p, AssetKindProp, req); err == nil || provider.calls != before {
		t.Fatal("ambiguous selection must fail before model call")
	}
	var saved models.Project
	ps.db.First(&saved, p.ID)
	if strings.TrimSpace(saved.Title) != "局部设计" {
		t.Fatal("preview modified project")
	}
}
