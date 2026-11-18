package service

import (
	"comfyui-console/internal/models"
	"testing"
)

func TestCharacterProfilePreviewPatchesOnlySelectedFields(t *testing.T) {
	db := newTestProjectService(t).db
	provider := &stubTextProvider{response: `{"appearance":"少年，黑发束髻"}`}
	svc := NewCharacterProfileService(db, provider)
	p := models.Project{Title: "草稿"}
	if err := db.Create(&p).Error; err != nil {
		t.Fatal(err)
	}
	ch := models.Character{ProjectID: p.ID, Name: "柳乐儿", Appearance: "原外貌", ProfileStatus: "approved", ReferencePrompt: "已确认提示词", Sheet: "old.png"}
	if err := db.Create(&ch).Error; err != nil {
		t.Fatal(err)
	}
	req := CharacterProfileDraftRequest{Current: map[string]string{"appearance": "少年，散发", "personality": "沉静"}, Fields: []string{"appearance"}, Instruction: "只束发"}
	patch, err := svc.PreviewProfile(&ch, &p, req)
	if err != nil || len(patch) != 1 || patch["appearance"] != "少年，黑发束髻" {
		t.Fatalf("patch=%v err=%v", patch, err)
	}
	var saved models.Character
	if err := db.First(&saved, ch.ID).Error; err != nil {
		t.Fatal(err)
	}
	if saved.Appearance != ch.Appearance || saved.ProfileStatus != "approved" || saved.ReferencePrompt != ch.ReferencePrompt || saved.Sheet != "old.png" {
		t.Fatalf("preview persisted: %+v", saved)
	}
	for _, raw := range []string{`{"appearance":"新外貌","personality":"多余改动"}`, `{"personality":"错误字段"}`, `{"appearance":""}`, `not json`} {
		provider.response = raw
		if _, err := svc.PreviewProfile(&ch, &p, req); err == nil {
			t.Fatalf("invalid patch accepted: %s", raw)
		}
	}
	before := provider.calls
	req.Fields = []string{"sheet_prompt"}
	if _, err := svc.PreviewProfile(&ch, &p, req); err == nil || provider.calls != before {
		t.Fatal("unsupported field should fail before call")
	}
}

func TestCharacterProfileSelectionPreservesUnicodeAndWhitespace(t *testing.T) {
	provider := &stubTextProvider{response: `{"replacement":"白玉"}`}
	svc := NewCharacterProfileService(nil, provider)
	offset := 8
	req := CharacterProfileDraftRequest{Current: map[string]string{"appearance": " 前😀青玉，中段青玉。 \n"}, Fields: []string{"appearance"}, Instruction: "只改第二处", Selection: "青玉", SelectionStart: &offset}
	patch, err := svc.PreviewProfile(&models.Character{Name: "柳乐儿"}, &models.Project{}, req)
	if err != nil || patch["appearance"] != " 前😀青玉，中段白玉。 \n" {
		t.Fatalf("patch=%v err=%v", patch, err)
	}
	before := provider.calls
	req.SelectionStart = nil
	if _, err := svc.PreviewProfile(&models.Character{}, &models.Project{}, req); err == nil || provider.calls != before {
		t.Fatal("ambiguous selection accepted")
	}
}
