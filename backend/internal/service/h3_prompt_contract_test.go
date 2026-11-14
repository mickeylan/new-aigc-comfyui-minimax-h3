package service

import (
	"fmt"
	"reflect"
	"strconv"
	"testing"
)

// legacyH3StructureIssues is a verbatim copy of the Subject-range and Shot-structure checks that
// lived inline in GenerateSceneVideoAction before parseH3PromptContract. It pins the migration:
// the parser must report exactly the same issues in the same order.
func legacyH3StructureIssues(value string, refCount, expectedShotCount int) []string {
	var issues []string
	for _, match := range h3SubjectTagPattern.FindAllStringSubmatch(value, -1) {
		n, _ := strconv.Atoi(match[1])
		if n < 1 || n > refCount {
			issues = append(issues, fmt.Sprintf("Subject编号%d超出已选参考图范围1-%d", n, refCount))
		}
	}
	if expectedShotCount > 0 {
		seen := map[int]bool{}
		for _, marker := range h3ShotMarkerPattern.FindAllStringSubmatch(value, -1) {
			if len(marker) != 2 {
				issues = append(issues, "无法读取的Shot标记")
				continue
			}
			n, _ := strconv.Atoi(marker[1])
			if n < 1 || n > expectedShotCount {
				issues = append(issues, fmt.Sprintf("Shot %d超出范围1-%d", n, expectedShotCount))
			}
			if seen[n] {
				issues = append(issues, fmt.Sprintf("Shot %d重复", n))
			}
			seen[n] = true
		}
		if len(seen) != expectedShotCount {
			issues = append(issues, fmt.Sprintf("Shot数量为%d，期望%d", len(seen), expectedShotCount))
		}
	}
	return issues
}

var h3ContractCorpus = []string{
	"",
	"<Subject 1> walks in.",
	"[Shot 1] <Subject 1> draws the sword. [Shot 2] At 00:03.000, <Subject 2> steps back.",
	"[Shot 1] A. [Shot 2] At 00:03.000, B. [Shot 2] At 00:05.000, C.",
	"[Shot 1] A. [Shot 3] At 00:06.000, C.",
	"[Shot 0] zero. [Shot 1] one.",
	"[Shot 99999999999999999999] overflow.",
	"[Shot 1 | wide] <Subject 3> turns. [Shot 2 | <Subject 4> close] At 00:04.500, reacts.",
	"[Shot  2]At 00:01.000, no space after bracket. [Shot 1] late first.",
	"[Shot 1] A strikes. At 00:02.000, B parries. [Shot 2] At 00:04.000, done.",
	"[Shot 1] 韩立 <Subject 1> 拔剑。[Shot 2] At 00:02.500, <Subject 0> 后退。",
	"preamble <Subject 2> [Shot 1] body <Subject 2> [Shot 2] At 01:02.003, tail <Subject 9>",
	"[shot 1] lowercase is not a marker. [Shot 1] real.",
}

func TestH3PromptContractMatchesLegacyStructureChecks(t *testing.T) {
	for _, text := range h3ContractCorpus {
		for _, refCount := range []int{0, 1, 2, 4} {
			for _, expected := range []int{0, 1, 2, 3} {
				doc := parseH3PromptContract(text)
				got := append(doc.SubjectRangeIssues(refCount), doc.ShotStructureIssues(expected)...)
				want := legacyH3StructureIssues(text, refCount, expected)
				if len(got) == 0 && len(want) == 0 {
					continue
				}
				if !reflect.DeepEqual(got, want) {
					t.Errorf("text=%q refs=%d expected=%d\n got=%q\nwant=%q", text, refCount, expected, got, want)
				}
			}
		}
	}
}

func TestH3PromptContractBareTimestampMatchesCombatCheck(t *testing.T) {
	for _, text := range h3ContractCorpus {
		if got, want := parseH3PromptContract(text).BareTimestamps == 0, h3CombatShotMarkersMatchTimestamps(text); got != want {
			t.Errorf("text=%q bare-free=%v legacy=%v", text, got, want)
		}
	}
}

func TestParseH3PromptContractReadsTimestampsBodiesAndSubjectOwners(t *testing.T) {
	doc := parseH3PromptContract("intro <Subject 1> [Shot 1] A <Subject 2>. [Shot 2 | <Subject 3>] At 01:02.500, B.")
	if doc.Preamble != "intro <Subject 1> " || len(doc.Shots) != 2 {
		t.Fatalf("doc=%+v", doc)
	}
	if doc.Shots[0].At != -1 || doc.Shots[1].At != 62.5 || doc.Shots[1].Body != " B." {
		t.Fatalf("shots=%+v", doc.Shots)
	}
	want := []h3SubjectRef{{1, 0}, {2, 1}, {3, 2}}
	if !reflect.DeepEqual(doc.Subjects, want) {
		t.Fatalf("subjects=%+v", doc.Subjects)
	}
}

func TestH3PromptContractTimelineNotesAreAdvisory(t *testing.T) {
	notes := parseH3PromptContract("[Shot 1] At 00:00.500, A. [Shot 2] At 00:06.000, B. [Shot 3] At 00:05.000, C. [Shot 4] At 00:09.000, D.").TimelineNotes(8)
	want := []string{"Shot 1不应带At时间", "Shot 3的At时间00:05.000没有晚于上一切点", "Shot 4的At时间00:09.000超出片长8.0秒"}
	if !reflect.DeepEqual(notes, want) {
		t.Fatalf("notes=%q", notes)
	}
	// Equal cut times within the 0.08 s tolerance count as non-increasing; 0.1 s later is fine.
	if notes := parseH3PromptContract("[Shot 1] A. [Shot 2] At 00:03.000, B. [Shot 3] At 00:03.100, C.").TimelineNotes(8.05); len(notes) != 0 {
		t.Fatalf("unexpected notes=%q", notes)
	}
	if notes := parseH3PromptContract("[Shot 2] At 00:08.050, end.").TimelineNotes(8); len(notes) != 0 {
		t.Fatalf("tolerance notes=%q", notes)
	}
}
