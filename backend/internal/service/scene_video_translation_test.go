package service

import (
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

func TestRestoreH3StructureLenientRepairsMarkerIndicesByPosition(t *testing.T) {
	source := "summary:\n[Shot 1] <d>原文</d>"
	masked, tokens := maskH3Structure(source)
	translated := strings.Replace(masked, "[[[T1]]]", "[[[T99]]]", 1)
	restored, warnings := restoreH3StructureLenient(translated, tokens)
	if len(warnings) == 0 || restored != source {
		t.Fatalf("lenient restore=%q warnings=%v", restored, warnings)
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

func TestHashStringBindsExactExecutionBytes(t *testing.T) {
	if hashString("prompt") == hashString("prompt\n") {
		t.Fatal("execution hash normalized exact bytes")
	}
}
