package service

import (
	"fmt"
	"strings"
	"testing"

	"comfyui-console/internal/models"
)

type shotOutfitFixture struct {
	ageStageSceneFixture
	shots []models.Shot
	night models.CharacterOutfit
}

func newShotOutfitFixture(t *testing.T) shotOutfitFixture {
	t.Helper()
	f := newAgeStageSceneFixture(t)
	if err := f.svc.db.Model(&f.scene).Update("image_file", "sb.png").Error; err != nil {
		t.Fatal(err)
	}
	f.scene.ImageFile = "sb.png"
	shots := []models.Shot{{SceneID: f.scene.ID, Order: 1, Duration: 3}, {SceneID: f.scene.ID, Order: 2, Duration: 3}}
	for i := range shots {
		if err := f.svc.db.Create(&shots[i]).Error; err != nil {
			t.Fatal(err)
		}
	}
	night := models.CharacterOutfit{ProjectID: f.project.ID, CharacterID: f.character.ID, Name: "夜行衣", AuditStatus: models.LookStatusApproved, Image: "night.png", Sheet: "night_sheet.png"}
	if err := f.svc.db.Create(&night).Error; err != nil {
		t.Fatal(err)
	}
	return shotOutfitFixture{ageStageSceneFixture: f, shots: shots, night: night}
}

func (f shotOutfitFixture) override(t *testing.T, shotIndex int, outfitID uint) {
	t.Helper()
	assignments := []OutfitAssignment{}
	if outfitID != 0 {
		assignments = append(assignments, OutfitAssignment{CharacterID: f.character.ID, OutfitID: outfitID})
	}
	if err := f.svc.AssignShotOutfits(f.project.ID, f.shots[shotIndex].ID, assignments); err != nil {
		t.Fatal(err)
	}
}

func (f shotOutfitFixture) videoRefs() ([]FileMeta, []string) {
	return f.ps.sceneVideoReferenceFiles(&f.scene, fmt.Sprint(f.project.ID))
}

func refNames(refs []FileMeta) []string {
	names := make([]string, 0, len(refs))
	for _, ref := range refs {
		names = append(names, ref.Name)
	}
	return names
}

func TestShotOneOutfitOverrideBecomesOpeningOutfit(t *testing.T) {
	f := newShotOutfitFixture(t)
	baseRefs, baseLines := f.videoRefs()
	if got := strings.Join(refNames(baseRefs), ","); got != "adult_sheet.png,lu.png,sb.png" {
		t.Fatalf("baseline refs changed: %s", got)
	}
	// 两个镜头都覆盖为同一套装：只改变开场造型，没有后续换装。
	f.override(t, 0, f.night.ID)
	f.override(t, 1, f.night.ID)
	opening := f.ps.sceneCharacterOutfits(&f.scene)
	if len(opening) != 1 || opening[0].ID != f.night.ID {
		t.Fatalf("Shot 1 override is not the opening outfit: %+v", opening)
	}
	refs, lines := f.videoRefs()
	if got := strings.Join(refNames(refs), ","); got != "lu.png,night_sheet.png,sb.png" {
		t.Fatalf("opening refs did not switch to the Shot 1 outfit: %s", got)
	}
	if !strings.Contains(lines[1], "角色「林采微」") || !strings.Contains(lines[1], "造型套装「夜行衣」") {
		t.Fatalf("opening outfit line: %q", lines[1])
	}
	for _, line := range lines {
		if strings.Contains(line, "换装参考") {
			t.Fatalf("the same outfit on every shot must not add a later-change reference: %q", lines)
		}
	}

	// 未审核的覆盖被忽略，输出恢复为无覆盖时的结果。
	if err := f.svc.db.Model(&f.night).Update("audit_status", models.LookStatusDraft).Error; err != nil {
		t.Fatal(err)
	}
	refs, lines = f.videoRefs()
	if strings.Join(refNames(refs), ",") != strings.Join(refNames(baseRefs), ",") || strings.Join(lines, "\n") != strings.Join(baseLines, "\n") {
		t.Fatalf("unapproved override changed refs:\n%q\nwant\n%q", lines, baseLines)
	}
}

func TestShotOneStageOverrideDrivesCharacterContext(t *testing.T) {
	f := newShotOutfitFixture(t)
	f.override(t, 0, f.outfit.ID)
	got := f.ps.characterContextForScene(&f.scene)
	if !strings.Contains(got, "- 林采微：童年（6岁）：6岁女孩，圆脸，齐刘海短发，约115厘米；本场造型：童年日常") {
		t.Fatalf("Shot 1 stage override did not reach character context:\n%s", got)
	}
	// 只在 Shot 2 覆盖时，开场仍是主形象。
	f.override(t, 0, 0)
	f.override(t, 1, f.outfit.ID)
	if got := f.ps.characterContextForScene(&f.scene); strings.Contains(got, "童年") {
		t.Fatalf("a later-shot stage override must not change the opening profile:\n%s", got)
	}
}

func TestLaterShotOutfitAddsReferenceBeforeStoryboard(t *testing.T) {
	f := newShotOutfitFixture(t)
	baseRefs, baseLines := f.videoRefs()
	f.override(t, 1, f.night.ID)
	refs, lines := f.videoRefs()
	if got := strings.Join(refNames(refs), ","); got != "adult_sheet.png,lu.png,night_sheet.png,sb.png" {
		t.Fatalf("later outfit reference missing or misplaced: %s", got)
	}
	// 已有人物参考的编号与说明不变，分镜图仍在最后。
	for i := 0; i < len(baseLines)-1; i++ {
		if lines[i] != baseLines[i] {
			t.Fatalf("existing Picture line %d changed: %q -> %q", i+1, baseLines[i], lines[i])
		}
	}
	want := "- <Picture 3>：换装参考：「林采微」自 Shot 2 起换穿造型套装「夜行衣」（套装四视图；同一人物，开场不使用）"
	if lines[2] != want {
		t.Fatalf("change reference line:\n%q\nwant\n%q", lines[2], want)
	}
	if !strings.HasPrefix(lines[3], "- <Picture 4>：当前分镜画面") || len(baseRefs) != 3 {
		t.Fatalf("storyboard line: %q", lines[3])
	}

	definitions, _, subjects := h3VideoSubjects(lines)
	if len(subjects) != 2 {
		t.Fatalf("the change reference must not become a new Subject: %q", subjects)
	}
	if !strings.Contains(definitions[2], "<Picture 3> shows the later outfit of 「林采微」 from [Shot 2] on") || !strings.Contains(definitions[2], "not a new subject") {
		t.Fatalf("change reference definition: %q", definitions[2])
	}
	if isCharacterSubjectTag("<Subject 3>", lines) {
		t.Fatal("<Subject 3> must not resolve to a character")
	}
}

func TestShotOutfitChangeSentenceIsInsertedIdempotently(t *testing.T) {
	f := newShotOutfitFixture(t)
	prompt := "subject_definitions:\n<Subject 1> is 「林采微」 from <Picture 1>.\n\ndetailed_description:\n[Shot 1] <Subject 1> walks into the yard.\n[Shot 2] <Subject 1> turns around.\n\noverall_soundscape:\nN/A"
	if got := f.ps.applyShotOutfitChanges(&f.scene, prompt); got != prompt {
		t.Fatalf("prompt without overrides changed:\n%s", got)
	}
	f.override(t, 1, f.night.ID)
	once := f.ps.applyShotOutfitChanges(&f.scene, prompt)
	wantShot2 := "[Shot 2] From this shot on, 「林采微」 wears the outfit 「夜行衣」 shown in <Picture 3> (same person and face). <Subject 1> turns around."
	if !strings.Contains(once, wantShot2) {
		t.Fatalf("change sentence missing:\n%s", once)
	}
	if strings.Count(once, "From this shot on") != 1 || !strings.Contains(once, "[Shot 1] <Subject 1> walks into the yard.\n") {
		t.Fatalf("sentence must appear once, only in Shot 2:\n%s", once)
	}
	if twice := f.ps.applyShotOutfitChanges(&f.scene, once); twice != once {
		t.Fatalf("second application is not idempotent:\n%s", twice)
	}

	// 无 Picture 引用的模板只写套装名。
	plain := "[Shot 1] 林采微 walks.\n[Shot 2] 林采微 turns."
	if got := f.ps.applyShotOutfitChanges(&f.scene, plain); !strings.Contains(got, "[Shot 2] From this shot on, 「林采微」 wears the outfit 「夜行衣」 (same person and face). 林采微 turns.") {
		t.Fatalf("plain prompt sentence:\n%s", got)
	}

	// 提示词镜头数与导演镜头数不一致时不插入。
	single := "[Shot 1] <Subject 1> walks into the yard."
	if got := f.ps.applyShotOutfitChanges(&f.scene, single); got != single {
		t.Fatalf("mismatched shot count must not insert:\n%s", got)
	}

	// 删除覆盖后旧句被清除，回到原文。
	f.override(t, 1, 0)
	if got := f.ps.applyShotOutfitChanges(&f.scene, once); got != prompt {
		t.Fatalf("stale change sentence not removed:\n%s", got)
	}
}

func TestShotOutfitChangeBackToMainLook(t *testing.T) {
	f := newShotOutfitFixture(t)
	f.override(t, 0, f.night.ID)
	refs, lines := f.videoRefs()
	if got := strings.Join(refNames(refs), ","); got != "lu.png,night_sheet.png,adult_sheet.png,sb.png" {
		t.Fatalf("return-to-main reference: %s", got)
	}
	if lines[2] != "- <Picture 3>：换装参考：「林采微」自 Shot 2 起换回主形象（四视图；同一人物，开场不使用）" {
		t.Fatalf("return-to-main line: %q", lines[2])
	}
	got := f.ps.applyShotOutfitChanges(&f.scene, "subject_definitions:\n<Subject 2> is 「林采微」 from <Picture 2>.\n\ndetailed_description:\n[Shot 1] <Subject 2> walks.\n[Shot 2] <Subject 2> turns.")
	if !strings.Contains(got, "[Shot 2] From this shot on, 「林采微」 returns to the default look shown in <Picture 3> (same person and face).") {
		t.Fatalf("return-to-main sentence:\n%s", got)
	}
}

func TestShotOutfitOverridesIgnoredWhenDirectorShotsStale(t *testing.T) {
	f := newShotOutfitFixture(t)
	baseRefs, _ := f.videoRefs()
	f.override(t, 0, f.night.ID)
	f.override(t, 1, f.outfit.ID)
	if err := f.svc.db.Model(&f.scene).Update("director_source_stale", true).Error; err != nil {
		t.Fatal(err)
	}
	f.scene.DirectorSourceStale = true
	refs, _ := f.videoRefs()
	if strings.Join(refNames(refs), ",") != strings.Join(refNames(baseRefs), ",") {
		t.Fatalf("stale director shots must not apply overrides: %v", refNames(refs))
	}
}

func TestAssignShotOutfitsMarksSceneStaleOnlyOnChange(t *testing.T) {
	f := newShotOutfitFixture(t)
	reset := func() {
		if err := f.svc.db.Model(&models.Scene{}).Where("id = ?", f.scene.ID).Update("prompt_stale", false).Error; err != nil {
			t.Fatal(err)
		}
	}
	stale := func() bool {
		var sc models.Scene
		if err := f.svc.db.First(&sc, f.scene.ID).Error; err != nil {
			t.Fatal(err)
		}
		return sc.PromptStale
	}
	reset()
	f.override(t, 1, f.night.ID)
	if !stale() {
		t.Fatal("changing a shot override must mark the scene prompt stale")
	}
	reset()
	f.override(t, 1, f.night.ID)
	if stale() {
		t.Fatal("saving the same shot override must not mark the scene stale")
	}
	f.override(t, 1, 0)
	if !stale() {
		t.Fatal("clearing a shot override must mark the scene prompt stale")
	}
}
