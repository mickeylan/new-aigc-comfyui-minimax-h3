package service

import (
	"strings"
	"testing"

	"comfyui-console/internal/models"
)

const h3TailClusterVisual = "[Shot 1] 画面可以从<Picture 7>中的参考状态开始。在稳定的中景镜头中，<Subject 2>微微垂下目光，同时提高音量向众人说话，并将注意力保持在<Subject 5>身上。随后镜头停留在<Subject 5>身上，他缓缓抬起自己那张木制且毫无表情的脸，向<Subject 2>短暂地侧目示意，没有说话。<Subject 1>紧贴在他身旁，右手握着旧拨浪鼓，左手攥住少年的裤脚；她张开嘴巴却没有发出声音，汗水渗入掌心。镜头稳定在一幅群体构图上，<Subject 2>、<Subject 4>和<Subject 3>短暂地相互交换眼神。<Subject 3>将白色马鬃拂尘搭在手臂上，同时注视着<Subject 5>。<Subject 4>微微转动身体，藏在身侧的左手仍紧握着暗藏的武器，嘴唇几乎不可察觉地朝同伴们动了动。"

const h3TailClusterFirstQuote = "不知阁下如何称呼？在下血刀会燕承，正与两位道友联手捉拿这妖狐，阁下若是与此事无关，还请自行离去。"
const h3TailClusterSecondQuote = "此人这般可疑，看来真有问题，与其在此浪费时间，不如我们……"

func h3TailClusterInput() string {
	return h3TailClusterVisual + "<Subject 2> (S1)说：<d>[Chinese] " + h3TailClusterFirstQuote + "</d>。<Subject 4> (S2)说：<d>[Chinese] " + h3TailClusterSecondQuote + "</d>。"
}

func TestH3DialogueOnlyAtShotEndsDetectsConsecutiveSpeakers(t *testing.T) {
	for _, body := range []string{
		h3TailClusterInput(),
		"[Shot 1] A looks up. <Subject 1> (S1) says: <d>[Chinese] 一。</d>. <Subject 2> (S2) says: <d>[Chinese] 二。</d>.<cutoff>",
	} {
		if !h3DialogueOnlyAtShotEnds(body) {
			t.Fatalf("terminal dialogue cluster escaped detection: %s", body)
		}
	}
	inline := "[Shot 1] A looks up. <Subject 1> (S1) says: <d>[Chinese] 一。</d>. B turns away. <Subject 2> (S2) says: <d>[Chinese] 二。</d>."
	if h3DialogueOnlyAtShotEnds(inline) {
		t.Fatal("intervening visual action must not be mistaken for a dialogue cluster")
	}
}

func TestComposeH3DialogueTimelineRepairsTailClusterAndRejectsUnchangedPlan(t *testing.T) {
	// An omitted anchor would simply append both quotes again. It must not pass.
	p := &h3DialogueRepairProvider{responses: []string{
		`{"placements":[{"index":0,"shot":1},{"index":1,"shot":1}]}`,
		`{"placements":[{"index":0,"shot":1,"after":"并将注意力保持在<Subject 5>身上。"},{"index":1,"shot":1,"after":"嘴唇几乎不可察觉地朝同伴们动了动。"}]}`,
	}}
	dubs := []models.Dialogue{{Character: "燕承", Text: h3TailClusterFirstQuote}, {Character: "同伴", Text: h3TailClusterSecondQuote}}
	refs := []string{"角色「女孩」", "角色「燕承」", "角色「道友」", "角色「同伴」", "角色「少年」"}
	got, err := (&ProjectService{textProvider: p}).composeH3DialogueTimeline(&models.Scene{}, h3TailClusterInput(), dubs, refs)
	if err != nil {
		t.Fatal(err)
	}
	if len(p.users) != 2 {
		t.Fatalf("expected clustered output to be replanned, calls=%d", len(p.users))
	}
	first, reaction, second := strings.Index(got, h3TailClusterFirstQuote), strings.Index(got, "随后镜头停留"), strings.Index(got, h3TailClusterSecondQuote)
	if first < 0 || first >= reaction || second <= reaction {
		t.Fatalf("speech did not follow its visual beat: %s", got)
	}
	for _, quote := range []string{h3TailClusterFirstQuote, h3TailClusterSecondQuote} {
		if strings.Count(got, quote) != 1 {
			t.Fatalf("quote changed or duplicated: %s", got)
		}
	}
	for _, binding := range []string{"<Subject 2> (S1)", "<Subject 4> (S2)"} {
		if !strings.Contains(got, binding) {
			t.Fatalf("speaker binding lost: %s", got)
		}
	}
	// The existing renderer normalizes speech-performance wording and inserts
	// spaces around clauses; compare both sides under that same normalization.
	compact := func(s string) string { return strings.Join(strings.Fields(s), "") }
	wantVisual := stripStructuredDialogueFromAction(h3TailClusterVisual, dubs)
	if stripped := stripStructuredDialogueFromAction(got, dubs); compact(stripped) != compact(wantVisual) {
		t.Fatalf("visual prose changed beyond existing normalization: %s", stripped)
	}
}
