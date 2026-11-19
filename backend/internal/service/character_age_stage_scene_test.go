package service

import (
	"strings"
	"testing"

	"comfyui-console/internal/models"
)

type ageStageSceneFixture struct {
	svc       *CharacterLookService
	ps        *ProjectService
	project   models.Project
	character models.Character
	scene     models.Scene
	stage     *models.CharacterAgeStage
	outfit    models.CharacterOutfit
}

func newAgeStageSceneFixture(t *testing.T) ageStageSceneFixture {
	t.Helper()
	svc, project, character := setupAgeStageTestDB(t)
	if err := svc.db.AutoMigrate(&models.Asset{}, &models.Dialogue{}, &models.FrameCandidate{}, &models.SceneContinuity{}); err != nil {
		t.Fatal(err)
	}
	if err := svc.db.Model(&character).Updates(map[string]any{"trait": "28岁女性，高挑，长发", "style": "白色风衣", "role": "主角"}).Error; err != nil {
		t.Fatal(err)
	}
	other := models.Character{ProjectID: project.ID, Name: "陆川", Trait: "寸头", Style: "黑夹克", Portrait: "lu.png"}
	if err := svc.db.Create(&other).Error; err != nil {
		t.Fatal(err)
	}
	scene := models.Scene{ProjectID: project.ID, EpisodeN: 1, Order: 1, Generation: 1, Title: "回忆", Content: "林采微与陆川在院中", Characters: character.Name + ", 陆川", VisibleCharacters: character.Name + ", 陆川"}
	if err := svc.db.Create(&scene).Error; err != nil {
		t.Fatal(err)
	}
	stage, err := svc.SaveAgeStage(project.ID, character.ID, 0, AgeStageInput{Name: "童年", Age: intPtr(6), Appearance: "6岁女孩，圆脸，齐刘海短发，约115厘米"})
	if err != nil {
		t.Fatal(err)
	}
	outfit := models.CharacterOutfit{ProjectID: project.ID, CharacterID: character.ID, AgeStageID: stage.ID, Name: "童年日常", AuditStatus: models.LookStatusApproved, Image: "child_outfit.png", Sheet: "child_outfit_sheet.png"}
	if err := svc.db.Create(&outfit).Error; err != nil {
		t.Fatal(err)
	}
	if err := svc.db.First(&character, character.ID).Error; err != nil {
		t.Fatal(err)
	}
	return ageStageSceneFixture{svc: svc, ps: &ProjectService{db: svc.db}, project: project, character: character, scene: scene, stage: stage, outfit: outfit}
}

func (f ageStageSceneFixture) assign(t *testing.T, outfitID uint) {
	t.Helper()
	if err := f.svc.AssignSceneOutfits(f.project.ID, f.scene.ID, []OutfitAssignment{{CharacterID: f.character.ID, OutfitID: outfitID}}); err != nil {
		t.Fatal(err)
	}
}

func TestCharacterContextUsesSelectedAgeStageOnly(t *testing.T) {
	f := newAgeStageSceneFixture(t)
	before := f.ps.characterContextForScene(&f.scene)
	want := "【角色设定（必须严格遵守，保证人物一致）】\n- 林采微：28岁女性，高挑，长发；白色风衣\n- 陆川：寸头；黑夹克"
	if before != want {
		t.Fatalf("context without stage changed:\n%q\nwant\n%q", before, want)
	}
	mainOutfit := models.CharacterOutfit{ProjectID: f.project.ID, CharacterID: f.character.ID, Name: "成人日常", AuditStatus: models.LookStatusApproved, Image: "adult.png"}
	if err := f.svc.db.Create(&mainOutfit).Error; err != nil {
		t.Fatal(err)
	}
	f.assign(t, mainOutfit.ID)
	if got := f.ps.characterContextForScene(&f.scene); got != want {
		t.Fatalf("main-profile outfit must keep context byte-identical:\n%q", got)
	}
	f.assign(t, f.outfit.ID)
	got := f.ps.characterContextForScene(&f.scene)
	if !strings.Contains(got, "- 林采微：童年（6岁）：6岁女孩，圆脸，齐刘海短发，约115厘米；本场造型：童年日常") || strings.Contains(got, "28岁") || strings.Contains(got, "白色风衣") {
		t.Fatalf("stage outfit did not replace adult profile:\n%s", got)
	}
	if !strings.Contains(got, "- 陆川：寸头；黑夹克") {
		t.Fatalf("other character changed:\n%s", got)
	}
	if err := f.svc.db.Model(&f.outfit).Update("audit_status", models.LookStatusDraft).Error; err != nil {
		t.Fatal(err)
	}
	if got := f.ps.characterContextForScene(&f.scene); got != want {
		t.Fatalf("unapproved stage outfit must not affect context:\n%q", got)
	}
}

func TestSceneContentExpandContextUsesSelectedAgeStage(t *testing.T) {
	f := newAgeStageSceneFixture(t)
	s := &Service{DB: f.svc.db}
	before, err := s.sceneContentExpandContext(&f.scene)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(before, "- 林采微（主角）；特征：28岁女性，高挑，长发；") {
		t.Fatalf("baseline context missing adult trait:\n%s", before)
	}
	f.assign(t, f.outfit.ID)
	after, err := s.sceneContentExpandContext(&f.scene)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(after, "- 林采微（主角）；特征：童年（6岁）：6岁女孩，圆脸，齐刘海短发，约115厘米；本场造型：童年日常；") || strings.Contains(after, "28岁女性") {
		t.Fatalf("expand context did not switch to stage:\n%s", after)
	}
	if strings.Replace(after, "童年（6岁）：6岁女孩，圆脸，齐刘海短发，约115厘米；本场造型：童年日常", "28岁女性，高挑，长发", 1) != before {
		t.Fatal("stage selection changed context beyond the character trait")
	}
}

func TestStoryboardSheetReferencesFollowSceneOutfit(t *testing.T) {
	f := newAgeStageSceneFixture(t)
	refs, lines := f.ps.storyboardSheetReferenceFiles(&f.scene)
	if len(refs) != 2 || refs[0].Name != "adult_sheet.png" || refs[1].Name != "lu.png" {
		t.Fatalf("baseline refs changed: %+v", refs)
	}
	if lines[0] != "<image1>是角色「林采微」的四视图" || lines[1] != "<image2>是角色「陆川」的标准像" {
		t.Fatalf("baseline lines changed: %q", lines)
	}
	f.assign(t, f.outfit.ID)
	refs, lines = f.ps.storyboardSheetReferenceFiles(&f.scene)
	if len(refs) != 2 || refs[0].Name != "child_outfit_sheet.png" || refs[1].Name != "lu.png" {
		t.Fatalf("scene outfit did not replace character master: %+v", refs)
	}
	if lines[0] != "<image1>是角色「林采微」的新形象「童年日常」四视图" {
		t.Fatalf("unexpected outfit line: %q", lines[0])
	}
	if err := f.svc.db.Model(&f.outfit).Update("sheet", "").Error; err != nil {
		t.Fatal(err)
	}
	refs, lines = f.ps.storyboardSheetReferenceFiles(&f.scene)
	if refs[0].Name != "child_outfit.png" || lines[0] != "<image1>是角色「林采微」的新形象「童年日常」定妆照" {
		t.Fatalf("outfit image fallback missing: %+v %q", refs, lines)
	}
}
