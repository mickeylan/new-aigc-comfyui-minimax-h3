package service

import (
	"strconv"
	"strings"
	"testing"
)

func TestValidateH3TranslationPreservesProtocolAndDialogue(t *testing.T) {
	source := "summary:\n中文 <Picture 1> <Subject 1>\ndetailed_description:\n[Shot 1] At 00:01.000 says <d>[Chinese] 原文，字节不变</d>"
	good := "summary:\nEnglish <Picture 1> <Subject 1>\ndetailed_description:\n[Shot 1] At 00:01.000 says <d>[Chinese] 原文，字节不变</d>"
	if err := validateH3Translation(source, good); err != nil {
		t.Fatalf("valid translation rejected: %v", err)
	}
	bad := "summary:\nEnglish <Subject 1> <Picture 1>\ndetailed_description:\n[Shot 1] At 00:01.000 says <d>[Chinese] changed</d>"
	if err := validateH3Translation(source, bad); err == nil {
		t.Fatal("mutated protected tokens accepted")
	}
}

func TestValidateH3TranslationProtectsH3RuntimeTokensOnly(t *testing.T) {
	source := "task: 中文\n[0.00s] (S1) says <d>原文</d> <scenetrans>\n[2.50s] <cutoff>\nNote: 中文说明"
	good := "task: English\n[0.00s] (S1) says <d>原文</d> <scenetrans>\n[2.50s] <cutoff>\nNote: translated prose"
	if err := validateH3Translation(source, good); err != nil {
		t.Fatalf("valid runtime protocol translation rejected: %v", err)
	}
	for _, bad := range []string{
		"task: English\n[0.10s] (S1) says <d>原文</d> <scenetrans>\n[2.50s] <cutoff>",
		"task: English\n[0.00s] (S2) says <d>原文</d> <scenetrans>\n[2.50s] <cutoff>",
		"task: English\n[0.00s] (S1) says <d>翻译</d> <scenetrans>\n[2.50s] <cutoff>",
	} {
		if err := validateH3Translation(source, bad); err == nil {
			t.Fatalf("mutated protocol accepted: %q", bad)
		}
	}
}

func TestMaskRestoreH3StructureRoundTrip(t *testing.T) {
	source := "summary:\n<Picture 1> 前景 <d>[Chinese] 保留 <Subject 99> (S9)</d>\n[Shot 1] [3s] (S1)"
	masked, tokens := maskH3Structure(source)
	if strings.Contains(masked, "<Picture") || strings.Contains(masked, "<d>") {
		t.Fatalf("structure leaked into masked prompt: %q", masked)
	}
	restored, issues := restoreH3Structure(masked, tokens)
	if len(issues) != 0 || restored != source {
		t.Fatalf("round trip restored=%q issues=%v", restored, issues)
	}
	got := h3ProtocolTokens(source)
	if len(got) != 6 || got[2] != "<d>[Chinese] 保留 <Subject 99> (S9)</d>" {
		t.Fatalf("dialogue was not one ordered token: %#v", got)
	}
}

func TestRestoreH3StructureAcceptsMarkerKindDrift(t *testing.T) {
	source := "summary:\n[Shot 1] <d>原文</d>"
	masked, tokens := maskH3Structure(source)
	translated := strings.Replace(masked, "[[[D2]]]", "[[[T2]]]", 1)
	restored, issues := restoreH3Structure(translated, tokens)
	if len(issues) != 0 || restored != source {
		t.Fatalf("kind drift blocked restoration: restored=%q issues=%v", restored, issues)
	}
}

func TestRestoreH3StructureRepairsReportedBracketSpacingDrift(t *testing.T) {
	source := "retention_analysis:\n<Subject 1> appears in [Shot 1], [Shot 2], [Shot 3]."
	masked, tokens := maskH3Structure(source)
	translated := strings.Replace(masked, "[[[T1]]]", "[ [[T1]]]", 1)
	translated = strings.Replace(translated, "[[[T3]]]", "[[T3]]", 1)
	restored, issues := restoreH3Structure(translated, tokens)
	if len(issues) != 0 || restored != source || strings.Contains(restored, "[[T") {
		t.Fatalf("bracket drift leaked: masked=%q restored=%q issues=%v", masked, restored, issues)
	}
}

func TestRestoreH3StructureReportsMissingDuplicateOutOfOrderAndUnknown(t *testing.T) {
	_, tokens := maskH3Structure("summary:\n[Shot 1] <d>原文</d>")
	for name, translated := range map[string]string{
		"missing":      "[[[T0]]] [[[T1]]]",
		"duplicate":    "[[[T0]]] [[[T1]]] [[[T1]]] [[[D2]]]",
		"out of order": "[[[T1]]] [[[T0]]] [[[D2]]]",
		"unknown":      "[[[T0]]] [[[T1]]] [[[D2]]] [[[T99]]]",
	} {
		t.Run(name, func(t *testing.T) {
			if _, issues := restoreH3Structure(translated, tokens); len(issues) == 0 {
				t.Fatal("invalid markers accepted")
			}
		})
	}
}

func TestValidateH3TranslationAllowsProseDurationButRejectsStructureNumbering(t *testing.T) {
	source := "summary:\n持续3秒\ndetailed_description:\n[Shot 1] [3s]"
	if err := validateH3Translation(source, "summary:\nlasts 3 seconds\ndetailed_description:\n[Shot 1] [3s]"); err != nil {
		t.Fatalf("prose duration rejected: %v", err)
	}
	if err := validateH3Translation(source, "summary:\nlasts 3 seconds\ndetailed_description:\n[Shot 2] [3s]"); err == nil {
		t.Fatal("changed structure numbering accepted")
	}
}

func TestRestoreH3StructureLenientDoesNotRepairUnknownIndexByPosition(t *testing.T) {
	source := "summary:\ntranslated prose [Shot 1] <d>原文</d>"
	masked, tokens := maskH3Structure(source)
	translated := strings.Replace(masked, "[[[T1]]]", "[[[T99]]]", 1)
	restored, warnings := restoreH3StructureLenient(translated, tokens)
	if len(warnings) == 0 || !strings.Contains(restored, "translated prose") || !strings.Contains(restored, "[[[T99]]]") {
		t.Fatalf("unknown marker was silently replaced by position: restored=%q warnings=%v", restored, warnings)
	}
	if strings.Contains(restored, "[Shot 1]") {
		t.Fatalf("missing non-dialogue token was replaced despite absent ID: %q", restored)
	}
}

func TestRestoreH3StructureLenientRestoresValidIDsAcrossEqualCountCorruption(t *testing.T) {
	tokens := make([]string, 23)
	for i := range tokens {
		tokens[i] = "TOKEN-" + string(rune('A'+i))
	}
	tokens[20] = "<d>相同台词</d>"
	tokens[21] = "(S2)"
	tokens[22] = "<d>相同台词</d>"

	// There are still 23 markers: ID 20 is missing, ID 22 is duplicated, IDs 21/22
	// are physically reversed, and their kinds drift. This is the reported equal-count
	// failure mode; position-based repair would assign the wrong speaker/dialogue.
	var parts []string
	for i := 0; i < 20; i++ {
		parts = append(parts, "[[[T"+strconv.Itoa(i)+"]]]")
	}
	parts = append(parts, "translated-before", "[[[T22]]]", "translated-middle", "[[[D21]]]", "[[[T22]]]")
	restored, warnings := restoreH3StructureLenient(strings.Join(parts, " "), tokens)
	if len(warnings) == 0 {
		t.Fatal("missing/duplicate/reordered IDs produced no warning")
	}
	wantOrder := "translated-before <d>相同台词</d> translated-middle (S2) <d>相同台词</d>"
	if !strings.Contains(restored, wantOrder) {
		t.Fatalf("valid IDs or translated order were rewritten: %q", restored)
	}
	if strings.Count(restored, "<d>相同台词</d>") != 3 {
		t.Fatalf("missing dialogue ID 20 was not recovered independently of identical ID 22: %q", restored)
	}
}

func TestRestoreH3StructureLenientRecoversDialogueWithDifferentMarkerCounts(t *testing.T) {
	tokens := []string{"summary:", "<d>原话</d>", "[Shot 1]"}
	translated := "translated [[[T2]]] action [[[T0]]]"
	restored, warnings := restoreH3StructureLenient(translated, tokens)
	if len(warnings) == 0 || !strings.Contains(restored, "translated [Shot 1] action summary:") || !strings.Contains(restored, "<d>原话</d>") {
		t.Fatalf("different-count recovery lost translation/dialogue: restored=%q warnings=%v", restored, warnings)
	}
}

func TestRestoreH3StructureLenientPreservesModelOutputWhenMarkerMissing(t *testing.T) {
	source := "summary:\n中文\n[Shot 1] 动作 <d>原文</d>"
	masked, tokens := maskH3Structure(source)
	translated := strings.Replace(masked, "中文", "translated prose", 1)
	translated = strings.Replace(translated, "[[[T1]]]", "", 1)
	restored, warnings := restoreH3StructureLenient(translated, tokens)
	if len(warnings) == 0 || !strings.Contains(restored, "translated prose") || restored == source {
		t.Fatalf("model output was hidden: restored=%q warnings=%v", restored, warnings)
	}
}

func TestRestoreH3StructureLenientRecoversDroppedDialogueWithoutDiscardingTranslation(t *testing.T) {
	source := "detailed_description:\n[Shot 1] 燕承 (S1) says <d>[Chinese] 原话甲</d> then turns.\n[Shot 2] 画外人 (S2) says <d>[Chinese] 原话乙</d>.\noverall_soundscape:\n对白清晰。"
	masked, tokens := maskH3Structure(source)
	translated := strings.Replace(masked, "then turns", "then visibly turns toward the door", 1)
	translated = strings.Replace(translated, "[[[D3]]]", "", 1)
	restored, warnings := restoreH3StructureLenient(translated, tokens)
	if len(warnings) == 0 {
		t.Fatal("dropped dialogue marker did not produce a warning")
	}
	for _, want := range []string{"燕承 (S1) says", "画外人 (S2) says", "<d>[Chinese] 原话甲</d>", "<d>[Chinese] 原话乙</d>", "then visibly turns toward the door"} {
		if !strings.Contains(restored, want) {
			t.Fatalf("lenient recovery lost %q: %s", want, restored)
		}
	}
	if strings.Contains(strings.ToLower(restored), "no dialogue") {
		t.Fatalf("dialogue recovery silently muted the prompt: %s", restored)
	}
}

func TestHashStringBindsExactExecutionBytes(t *testing.T) {
	if hashString("prompt") == hashString("prompt\n") {
		t.Fatal("execution hash normalized exact bytes")
	}
}
