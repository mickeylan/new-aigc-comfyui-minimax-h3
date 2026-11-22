package service

import (
	"strings"
	"testing"
)

// TestInjectH3Video1StructureReferenceEnglish 英文执行稿：三处都写进去，其它段落不动。
func TestInjectH3Video1StructureReferenceEnglish(t *testing.T) {
	got := injectH3Video1StructureReference(bilingualRef2VEnglish)
	if !strings.Contains(got, "<Video 1> is the camera-structure reference") {
		t.Fatalf("definition line missing:\n%s", got)
	}
	if !strings.Contains(got, "<Video 1> (appears in [Shot 1]): structure_only") {
		t.Fatalf("retention line missing:\n%s", got)
	}
	if !strings.Contains(got, "[Shot 1] The camera replicates <Video 1>'s camera path") {
		t.Fatalf("direction sentence missing:\n%s", got)
	}
	// 段落归属：定义在 subject_definitions，机位指令在 detailed_description，retention 在两者之间。
	def := h3PromptSection(got, "subject_definitions:")
	ret := h3PromptSection(got, "retention_analysis:")
	detail := h3PromptSection(got, "detailed_description:")
	if !strings.Contains(def, "<Video 1> is the camera-structure reference") || !strings.Contains(def, "<Subject 1>") {
		t.Fatalf("definition section damaged: %q", def)
	}
	if !strings.Contains(ret, "structure_only") || strings.Contains(ret, "camera-structure reference") {
		t.Fatalf("retention section wrong: %q", ret)
	}
	if !strings.Contains(detail, "The camera replicates <Video 1>'s camera path") {
		t.Fatalf("detailed_description missing camera instruction: %q", detail)
	}
	// 指令必须落在 [Shot 1] 正文之前，且正文那条 [Shot 1] 还在。
	first := strings.Index(detail, "[Shot 1]")
	body := -1
	if first >= 0 {
		if next := strings.Index(detail[first+1:], "[Shot 1]"); next >= 0 {
			body = first + 1 + next
		}
	}
	if body < 0 {
		t.Fatalf("shot 1 body line missing: %q", detail)
	}
	if instr := strings.Index(detail, "The camera replicates"); instr < 0 || instr > body {
		t.Fatalf("camera instruction is not ahead of [Shot 1] body: %q", detail)
	}
	// 声音段落不受影响。
	if h3PromptSection(got, "overall_soundscape:") != h3PromptSection(bilingualRef2VEnglish, "overall_soundscape:") {
		t.Fatal("soundscape section must stay untouched")
	}

	// 幂等：再注入一次不应出现第二条。
	if twice := injectH3Video1StructureReference(got); strings.Count(twice, "<Video 1> (appears in") != 1 {
		t.Fatalf("injection is not idempotent:\n%s", twice)
	}
}

// TestInjectH3Video1StructureReferenceChinese 中文主稿：写中文版措辞。
func TestInjectH3Video1StructureReferenceChinese(t *testing.T) {
	got := injectH3Video1StructureReference(bilingualRef2VMaster)
	if !strings.Contains(got, "<Video 1> 是 3D 编辑器导出的机位结构参考") {
		t.Fatalf("中文定义缺失:\n%s", got)
	}
	if !strings.Contains(got, "<Video 1> (出现在 [Shot 1]): structure_only") {
		t.Fatalf("中文 retention 缺失:\n%s", got)
	}
	if !strings.Contains(got, "[Shot 1] 镜头严格复刻 <Video 1> 的机位路径") {
		t.Fatalf("中文机位指令缺失:\n%s", got)
	}
	if strings.Contains(got, "is the camera-structure reference") {
		t.Fatal("中文主稿里混入了英文措辞")
	}
}

// TestInjectH3Video1StructureReferenceSkipsNonProtocol 自由文本/空提示词不能被改造成六段协议。
func TestInjectH3Video1StructureReferenceSkipsNonProtocol(t *testing.T) {
	for _, prompt := range []string{"", "   ", "一个人从 B 点跑到 A 点，镜头跟随。", "summary:\n只有摘要，没有其它段落。"} {
		if got := injectH3Video1StructureReference(prompt); got != prompt {
			t.Fatalf("non-protocol prompt changed: %q -> %q", prompt, got)
		}
	}
}

// TestH3PromptLooksChinese 英文执行稿里夹中文人名不能被判成中文。
func TestH3PromptLooksChinese(t *testing.T) {
	if !h3PromptLooksChinese(bilingualRef2VMaster) {
		t.Fatal("Chinese master must be detected as Chinese")
	}
	if h3PromptLooksChinese(bilingualRef2VEnglish) {
		t.Fatal("English execution draft (with Chinese names) must be detected as English")
	}
}
