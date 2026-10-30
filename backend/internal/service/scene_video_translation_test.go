package service

import "testing"

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

func TestHashStringBindsExactExecutionBytes(t *testing.T) {
	if hashString("prompt") == hashString("prompt\n") {
		t.Fatal("execution hash normalized exact bytes")
	}
}
