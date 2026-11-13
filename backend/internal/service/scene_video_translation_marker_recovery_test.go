package service

import (
	"fmt"
	"strings"
	"testing"
)

// 多镜头主稿：结构标记里既有行首的 [Shot N]，也有句中的 (S1)。
const droppedMarkerMaster = "detailed_description:\n[Shot 1] 甲说话。\n[Shot 2] 在 00:02.000，乙回应。"

// 翻译模型经常整块吞掉 [Shot N]。行首标记两侧仍有存活标记，位置是可确定的，
// 必须按原位补回，而不是只留一条警告让镜头时间线被压平。
func TestDroppedShotMarkerIsRestoredAtItsLineHead(t *testing.T) {
	masked, tokens := maskH3Structure(droppedMarkerMaster)
	dropped := strings.Replace(masked, "[[[T2]]] ", "", 1)
	if dropped == masked {
		t.Fatalf("fixture did not drop a marker: %q", masked)
	}

	restored, warnings, missing := restoreH3StructureWithRecovery(dropped, masked, tokens)
	if missing != 0 {
		t.Fatalf("行首标记未被补回: missing=%d restored=%q", missing, restored)
	}
	if restored != droppedMarkerMaster {
		t.Fatalf("补回后与原文不一致:\n got=%q\nwant=%q", restored, droppedMarkerMaster)
	}
	if !strings.Contains(strings.Join(warnings, "；"), "补回") {
		t.Fatalf("补回动作必须告知用户复核: %v", warnings)
	}
}

// 句中的 (S1)、<Subject N> 没有自己的锚点：译文把整句重写之后，任何插入位置都是猜的。
// 猜错会把说话人挂到别人身上，比留空更糟，所以必须保持缺失并上报。
func TestDroppedInlineBindingIsNotGuessedIntoPlace(t *testing.T) {
	source := "detailed_description:\n[Shot 1] 甲 (S1) 说道：<d>[Chinese] 你好</d>。"
	masked, tokens := maskH3Structure(source)
	dropped := strings.Replace(masked, "[[[T2]]] ", "", 1)
	if dropped == masked {
		t.Fatalf("fixture did not drop the inline marker: %q", masked)
	}

	restored, _, missing := restoreH3StructureWithRecovery(dropped, masked, tokens)
	if missing != 1 {
		t.Fatalf("行内标记缺失必须被上报: missing=%d", missing)
	}
	if strings.Contains(restored, "[Shot 1](S1)") || strings.Contains(restored, "(S1) 甲") {
		t.Fatalf("行内标记被插到了猜测位置: %q", restored)
	}
}

// 译文把两个镜头合并成一行时，承载 [Shot 2] 的换行也没了。此时把它接在上一个标记后面
// 会把两个镜头并成一个，必须宁可缺失。
func TestMergedShotLinesAreNotSilentlyRejoined(t *testing.T) {
	masked, tokens := maskH3Structure(droppedMarkerMaster)
	merged := "[[[T0]]]\n[[[T1]]] 甲说话。在 [[[T3]]]，乙回应。"

	restored, _, missing := restoreH3StructureWithRecovery(merged, masked, tokens)
	if missing != 1 {
		t.Fatalf("无换行锚点时必须保持缺失: missing=%d restored=%q", missing, restored)
	}
	if strings.Contains(restored, "[Shot 1][Shot 2]") || strings.Contains(restored, "甲说话。[Shot 2]") {
		t.Fatalf("缺失的镜头标记被塞进了同一行: %q", restored)
	}
}

// 译文打乱标记顺序时，前后标记围出的区间不再是真实区间，不能当作插入依据。
func TestReorderedMarkersAreNotGuessedPositionally(t *testing.T) {
	masked, tokens := maskH3Structure(droppedMarkerMaster)
	scrambled := "[[[T0]]]\n[[[T3]]] 乙回应。\n[[[T1]]] 甲说话。"

	restored, _, missing := restoreH3StructureWithRecovery(scrambled, masked, tokens)
	if missing != 1 {
		t.Fatalf("乱序时必须保持缺失: missing=%d restored=%q", missing, restored)
	}
	if strings.Contains(restored, "[Shot 2]") {
		t.Fatalf("乱序译文里不应凭空插入镜头标记: %q", restored)
	}
}

// 对白块仍由既有的宽松还原补回，本次改动不得影响它。
func TestStructureRecoveryStillRecoversDroppedDialogue(t *testing.T) {
	source := "detailed_description:\n[Shot 1] 甲说道：<d>[Chinese] 你好</d>。\n\noverall_soundscape:\n安静。"
	masked, tokens := maskH3Structure(source)
	dropped := strings.Replace(masked, "[[[D2]]]", "", 1)

	restored, warnings, missing := restoreH3StructureWithRecovery(dropped, masked, tokens)
	if missing != 0 {
		t.Fatalf("结构标记不应被误判为缺失: missing=%d", missing)
	}
	if !strings.Contains(restored, "<d>[Chinese] 你好</d>") {
		t.Fatalf("遗失的对白块未被恢复: %q", restored)
	}
	if !strings.Contains(strings.Join(warnings, "；"), "对白") {
		t.Fatalf("对白恢复必须告知用户复核: %v", warnings)
	}
}

// 整条链路：模型第一次吞掉 [Shot 2] 且顺序被打乱（无法按位置补回）时，必须自动重译一次，
// 并采用更完整的那一版，而不是把残缺译文直接交给用户确认。
func TestTranslationRetriesOnceWhenStructureCannotBeRecovered(t *testing.T) {
	f := newTranslationHTTPFixture(t)
	masked, _ := maskH3Structure(droppedMarkerMaster)
	f.rawResponse = true
	f.response = "[[[T0]]]\n[[[T3]]] B replies.\n[[[T1]]] A speaks."
	f.beforeReply = func() {
		if f.calls.Load() >= 2 {
			f.response = masked
		}
	}

	body := fmt.Sprintf(`{"master_prompt_zh":%q,"language":"en"}`, droppedMarkerMaster)
	w := translationRequest(f.svc, "POST", f.p.ID, f.sc.ID, "/translate", body)
	if w.Code != 200 {
		t.Fatalf("translate HTTP %d: %s", w.Code, w.Body.String())
	}
	if got := f.calls.Load(); got != 2 {
		t.Fatalf("结构标记无法补回时应自动重译一次: calls=%d", got)
	}

	preview, _ := decodeTranslationBody(t, w)["preview"].(string)
	if !strings.Contains(preview, "[Shot 1]") || !strings.Contains(preview, "[Shot 2]") {
		t.Fatalf("重译后的完整结构未被采用: %q", preview)
	}
}

// 首轮就完整时不得多花一次调用。
func TestCompleteTranslationIsNotRetried(t *testing.T) {
	f := newTranslationHTTPFixture(t)
	masked, _ := maskH3Structure(droppedMarkerMaster)
	f.rawResponse = true
	f.response = masked

	body := fmt.Sprintf(`{"master_prompt_zh":%q,"language":"en"}`, droppedMarkerMaster)
	w := translationRequest(f.svc, "POST", f.p.ID, f.sc.ID, "/translate", body)
	if w.Code != 200 {
		t.Fatalf("translate HTTP %d: %s", w.Code, w.Body.String())
	}
	if got := f.calls.Load(); got != 1 {
		t.Fatalf("首轮完整时不应重译: calls=%d", got)
	}
}
