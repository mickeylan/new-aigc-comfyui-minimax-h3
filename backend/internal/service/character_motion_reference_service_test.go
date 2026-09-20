package service

import (
	"testing"

	"comfyui-console/internal/models"
)

func TestCharacterMotionReferenceListSelectDeleteAndIsolation(t *testing.T) {
	db := newTestDBWithNewModels(t)
	if err := db.AutoMigrate(&models.CharacterMotionReference{}); err != nil {
		t.Fatal(err)
	}
	p1 := models.Project{Title: "one"}
	p2 := models.Project{Title: "two"}
	db.Create(&p1)
	db.Create(&p2)
	c1 := models.Character{ProjectID: p1.ID, Name: "A"}
	c2 := models.Character{ProjectID: p2.ID, Name: "B"}
	db.Create(&c1)
	db.Create(&c2)
	r1 := models.CharacterMotionReference{ProjectID: p1.ID, CharacterID: c1.ID, Name: "walk", VideoPath: "motion/a.mp4", AudioPath: "motion/a.wav", Selected: true}
	r2 := models.CharacterMotionReference{ProjectID: p1.ID, CharacterID: c1.ID, Name: "run", VideoPath: "motion/b.mp4"}
	db.Create(&r1)
	db.Create(&r2)
	svc := NewCharacterMotionReferenceService(db, nil)
	refs, err := svc.List(p1.ID, c1.ID)
	if err != nil || len(refs) != 2 || refs[0].ID != r1.ID {
		t.Fatalf("refs=%+v err=%v", refs, err)
	}
	selected, err := svc.Select(p1.ID, c1.ID, r2.ID)
	if err != nil || !selected.Selected {
		t.Fatalf("selected=%+v err=%v", selected, err)
	}
	db.First(&r1, r1.ID)
	if r1.Selected {
		t.Fatal("old reference remained selected")
	}
	if _, err := svc.Select(p2.ID, c2.ID, r2.ID); err == nil {
		t.Fatal("cross-project select accepted")
	}
	if err := svc.Delete(p1.ID, c1.ID, r2.ID); err != nil {
		t.Fatal(err)
	}
	if err := svc.Delete(p1.ID, c1.ID, r2.ID); err != ErrCharacterMotionReferenceNotFound {
		t.Fatalf("second delete=%v", err)
	}
}
