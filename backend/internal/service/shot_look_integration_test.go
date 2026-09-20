package service

import (
	"fmt"
	"testing"

	"github.com/glebarez/sqlite"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"

	"comfyui-console/internal/models"
)

// TestShotLookAssignAndRetrieve tests assigning looks to a shot and retrieving them.
func TestShotLookAssignAndRetrieve(t *testing.T) {
	db := newTestDBWithNewModels(t)
	project := models.Project{Title: "shot-look-test"}
	db.Create(&project)
	scene := models.Scene{ProjectID: project.ID, EpisodeN: 1, Order: 1}
	db.Create(&scene)
	shot := models.Shot{SceneID: scene.ID, Order: 1, ShotType: "medium", Duration: 2}
	db.Create(&shot)
	char := models.Character{ProjectID: project.ID, Name: "主角"}
	db.Create(&char)
	lookSvc := NewCharacterLookService(db, nil)
	look := models.CharacterLook{
		ProjectID: project.ID, CharacterID: char.ID, Name: "战斗服装",
		Category: "clothing", Description: "黑色皮夹克",
		AuditStatus: models.LookStatusApproved, IsShotRelated: true, Priority: 100,
	}
	if err := db.Create(&look).Error; err != nil {
		t.Fatal(err)
	}

	// Assign look to shot via CharacterLookService
	if err := lookSvc.AssignToShot(shot.ID, look.ID, 1, true); err != nil {
		t.Fatalf("assign to shot %d: %v", shot.ID, err)
	}

	// Retrieve looks for shot
	looks, err := lookSvc.GetShotLooks(shot.ID)
	if err != nil {
		t.Fatal(err)
	}
	if len(looks) != 1 {
		t.Fatalf("expected 1 look, got %d", len(looks))
	}
	if looks[0].Look.ID != look.ID {
		t.Fatalf("look ID mismatch: expected %d, got %d", look.ID, looks[0].Look.ID)
	}
	if !looks[0].IsFeatured {
		t.Fatal("expected look to be featured")
	}
}

// TestShotLookBatchAssign tests batch assignment of looks to a shot.
func TestShotLookBatchAssign(t *testing.T) {
	db := newTestDBWithNewModels(t)
	project := models.Project{Title: "batch-shot-look"}
	db.Create(&project)
	scene := models.Scene{ProjectID: project.ID, EpisodeN: 1, Order: 1}
	db.Create(&scene)
	shot := models.Shot{SceneID: scene.ID, Order: 1, ShotType: "close_up", Duration: 1.5}
	db.Create(&shot)
	char := models.Character{ProjectID: project.ID, Name: "主角"}
	db.Create(&char)
	lookSvc := NewCharacterLookService(db, nil)

	// Create multiple looks
	var lookIDs []uint
	for i, name := range []string{"服装A", "服装B", "服装C"} {
		look := models.CharacterLook{
			ProjectID: project.ID, CharacterID: char.ID, Name: name,
			Category: "clothing", Description: "描述",
			AuditStatus: models.LookStatusApproved, IsShotRelated: true, Priority: i + 1,
		}
		db.Create(&look)
		lookIDs = append(lookIDs, look.ID)
	}

	// Batch assign
	assignments := []ShotLookAssignmentRequest{
		{LookID: lookIDs[0], Order: 3, IsFeatured: false},
		{LookID: lookIDs[1], Order: 1, IsFeatured: true},
		{LookID: lookIDs[2], Order: 2, IsFeatured: false},
	}
	if err := lookSvc.AssignLooksToShot(shot.ID, assignments); err != nil {
		t.Fatal(err)
	}

	// Verify assignments
	shcl, err := lookSvc.GetShotLooks(shot.ID)
	if err != nil {
		t.Fatal(err)
	}
	if len(shcl) != 3 {
		t.Fatalf("expected 3 looks, got %d", len(shcl))
	}

	// Verify ordering by order field
	if shcl[0].Order != 1 || shcl[0].LookID != lookIDs[1] {
		t.Fatalf("first look should be lookID %d with order 1, got lookID %d with order %d",
			lookIDs[1], shcl[0].LookID, shcl[0].Order)
	}

	// Verify featured flag
	for _, s := range shcl {
		if s.LookID == lookIDs[1] && !s.IsFeatured {
			t.Fatal("lookIDs[1] should be featured")
		}
	}
}

// TestShotLookUnassign tests removing a look from a shot.
func TestShotLookUnassign(t *testing.T) {
	db := newTestDBWithNewModels(t)
	project := models.Project{Title: "unassign-shot-look"}
	db.Create(&project)
	scene := models.Scene{ProjectID: project.ID, EpisodeN: 1, Order: 1}
	db.Create(&scene)
	shot := models.Shot{SceneID: scene.ID, Order: 1, ShotType: "medium", Duration: 2}
	db.Create(&shot)
	char := models.Character{ProjectID: project.ID, Name: "主角"}
	db.Create(&char)
	lookSvc := NewCharacterLookService(db, nil)

	look := models.CharacterLook{
		ProjectID: project.ID, CharacterID: char.ID, Name: "测试造型",
		Category: "clothing", Description: "描述",
		AuditStatus: models.LookStatusApproved,
	}
	db.Create(&look)

	// Assign
	if err := lookSvc.AssignToShot(shot.ID, look.ID, 1, false); err != nil {
		t.Fatal(err)
	}

	// Verify assigned
	looks, _ := lookSvc.GetShotLooks(shot.ID)
	if len(looks) != 1 {
		t.Fatalf("expected 1 look after assign, got %d", len(looks))
	}

	// Unassign
	if err := lookSvc.UnassignFromShot(shot.ID, look.ID); err != nil {
		t.Fatal(err)
	}

	// Verify unassigned
	looks, _ = lookSvc.GetShotLooks(shot.ID)
	if len(looks) != 0 {
		t.Fatalf("expected 0 looks after unassign, got %d", len(looks))
	}
}

// TestShotLookReplacement tests that batch assign replaces existing assignments.
func TestShotLookReplacement(t *testing.T) {
	db := newTestDBWithNewModels(t)
	project := models.Project{Title: "shot-look-replace"}
	db.Create(&project)
	scene := models.Scene{ProjectID: project.ID, EpisodeN: 1, Order: 1}
	db.Create(&scene)
	shot := models.Shot{SceneID: scene.ID, Order: 1, ShotType: "wide", Duration: 2}
	db.Create(&shot)
	char := models.Character{ProjectID: project.ID, Name: "主角"}
	db.Create(&char)
	lookSvc := NewCharacterLookService(db, nil)

	// Create looks
	look1 := models.CharacterLook{ProjectID: project.ID, CharacterID: char.ID, Name: "造型1", Category: "clothing", AuditStatus: models.LookStatusApproved}
	look2 := models.CharacterLook{ProjectID: project.ID, CharacterID: char.ID, Name: "造型2", Category: "clothing", AuditStatus: models.LookStatusApproved}
	look3 := models.CharacterLook{ProjectID: project.ID, CharacterID: char.ID, Name: "造型3", Category: "clothing", AuditStatus: models.LookStatusApproved}
	db.Create(&look1)
	db.Create(&look2)
	db.Create(&look3)

	// First batch assignment
	lookSvc.AssignLooksToShot(shot.ID, []ShotLookAssignmentRequest{
		{LookID: look1.ID, Order: 1},
		{LookID: look2.ID, Order: 2},
	})

	// Second batch assignment (should replace)
	lookSvc.AssignLooksToShot(shot.ID, []ShotLookAssignmentRequest{
		{LookID: look3.ID, Order: 1},
	})

	// Verify only look3 remains
	looks, _ := lookSvc.GetShotLooks(shot.ID)
	if len(looks) != 1 {
		t.Fatalf("expected 1 look after replacement, got %d", len(looks))
	}
	if looks[0].LookID != look3.ID {
		t.Fatalf("expected look3, got look %d", looks[0].LookID)
	}
}

// TestShotLookScopedToProject tests that looks are scoped to the project.
func TestShotLookScopedToProject(t *testing.T) {
	db := newTestDBWithNewModels(t)
	project1 := models.Project{Title: "项目1"}
	project2 := models.Project{Title: "项目2"}
	db.Create(&project1)
	db.Create(&project2)
	scene1 := models.Scene{ProjectID: project1.ID, EpisodeN: 1, Order: 1}
	scene2 := models.Scene{ProjectID: project2.ID, EpisodeN: 1, Order: 1}
	db.Create(&scene1)
	db.Create(&scene2)
	shot1 := models.Shot{SceneID: scene1.ID, Order: 1, ShotType: "medium", Duration: 2}
	shot2 := models.Shot{SceneID: scene2.ID, Order: 1, ShotType: "medium", Duration: 2}
	db.Create(&shot1)
	db.Create(&shot2)
	char1 := models.Character{ProjectID: project1.ID, Name: "角色1"}
	char2 := models.Character{ProjectID: project2.ID, Name: "角色2"}
	db.Create(&char1)
	db.Create(&char2)
	lookSvc := NewCharacterLookService(db, nil)

	// Create looks for project1
	look1 := models.CharacterLook{ProjectID: project1.ID, CharacterID: char1.ID, Name: "项目1造型", Category: "clothing", AuditStatus: models.LookStatusApproved}
	db.Create(&look1)

	// Assign to shot1
	if err := lookSvc.AssignToShot(shot1.ID, look1.ID, 1, false); err != nil {
		t.Fatal(err)
	}

	// Assign to shot2 should fail (look belongs to different project)
	if err := lookSvc.AssignToShot(shot2.ID, look1.ID, 1, false); err == nil {
		t.Fatal("should not allow assigning look from project1 to shot in project2")
	}
}

// TestGetShotRelatedLooks tests retrieving only shot-related approved looks.
func TestGetShotRelatedLooks(t *testing.T) {
	db := newTestDBWithNewModels(t)
	project := models.Project{Title: "shot-related-test"}
	db.Create(&project)
	scene := models.Scene{ProjectID: project.ID, EpisodeN: 1, Order: 1}
	db.Create(&scene)
	shot := models.Shot{SceneID: scene.ID, Order: 1, ShotType: "medium", Duration: 2}
	db.Create(&shot)
	char := models.Character{ProjectID: project.ID, Name: "主角"}
	db.Create(&char)
	lookSvc := NewCharacterLookService(db, nil)

	// Create approved shot-related look
	approvedShotRelated := models.CharacterLook{
		ProjectID: project.ID, CharacterID: char.ID, Name: "已发布造型",
		Category: "clothing", AuditStatus: models.LookStatusApproved, IsShotRelated: true,
	}
	// Create approved non-shot-related look
	approvedNonShotRelated := models.CharacterLook{
		ProjectID: project.ID, CharacterID: char.ID, Name: "非镜头造型",
		Category: "clothing", AuditStatus: models.LookStatusApproved, IsShotRelated: false,
	}
	// Create draft look
	draftLook := models.CharacterLook{
		ProjectID: project.ID, CharacterID: char.ID, Name: "草稿造型",
		Category: "clothing", AuditStatus: models.LookStatusDraft, IsShotRelated: true,
	}
	db.Create(&approvedShotRelated)
	db.Create(&approvedNonShotRelated)
	db.Create(&draftLook)

	// Assign all to shot
	lookSvc.AssignLooksToShot(shot.ID, []ShotLookAssignmentRequest{
		{LookID: approvedShotRelated.ID, Order: 1},
		{LookID: approvedNonShotRelated.ID, Order: 2},
		{LookID: draftLook.ID, Order: 3},
	})

	// Get shot-related looks
	shotRelated, err := lookSvc.GetShotRelatedLooks(shot.ID)
	if err != nil {
		t.Fatal(err)
	}
	// Should only return approved shot-related look
	if len(shotRelated) != 1 {
		t.Fatalf("expected 1 shot-related approved look, got %d", len(shotRelated))
	}
	if shotRelated[0].ID != approvedShotRelated.ID {
		t.Fatalf("expected approvedShotRelated, got look %d", shotRelated[0].ID)
	}
}

// TestBuildLookContextForShot tests building look context for H3 prompt.
func TestBuildLookContextForShot(t *testing.T) {
	db := newTestDBWithNewModels(t)
	project := models.Project{Title: "look-context-test", Style: "动漫风格"}
	db.Create(&project)
	scene := models.Scene{ProjectID: project.ID, EpisodeN: 1, Order: 1}
	db.Create(&scene)
	shot := models.Shot{SceneID: scene.ID, Order: 1, ShotType: "medium", Duration: 2}
	db.Create(&shot)
	char := models.Character{ProjectID: project.ID, Name: "小明"}
	db.Create(&char)
	lookSvc := NewCharacterLookService(db, nil)

	// Create and assign approved shot-related look
	look := models.CharacterLook{
		ProjectID: project.ID, CharacterID: char.ID, Name: "战斗服装",
		Category: "clothing", Description: "黑色皮夹克配牛仔裤",
		AuditStatus: models.LookStatusApproved, IsShotRelated: true, Priority: 100,
	}
	db.Create(&look)
	lookSvc.AssignLooksToShot(shot.ID, []ShotLookAssignmentRequest{
		{LookID: look.ID, Order: 1, IsFeatured: true},
	})

	// Build context
	context, err := lookSvc.BuildLookContextForShot(shot.ID, &char)
	if err != nil {
		t.Fatal(err)
	}
	if context == "" {
		t.Fatal("expected non-empty context")
	}
	// Context should contain character name and look info
	if !contains(context, "小明") {
		t.Fatal("context should contain character name")
	}
	if !contains(context, "服装") || !contains(context, "战斗服装") {
		t.Fatal("context should contain look category and name")
	}
}

func contains(s, substr string) bool {
	return len(s) >= len(substr) && (s == substr || len(s) > 0 && containsImpl(s, substr))
}

func containsImpl(s, substr string) bool {
	for i := 0; i <= len(s)-len(substr); i++ {
		if s[i:i+len(substr)] == substr {
			return true
		}
	}
	return false
}

// TestGetLooksByShot tests the simplified GetLooksByShot method.
func TestGetLooksByShot(t *testing.T) {
	db := newTestDBWithNewModels(t)
	project := models.Project{Title: "get-looks-by-shot"}
	db.Create(&project)
	scene := models.Scene{ProjectID: project.ID, EpisodeN: 1, Order: 1}
	db.Create(&scene)
	shot := models.Shot{SceneID: scene.ID, Order: 1, ShotType: "medium", Duration: 2}
	db.Create(&shot)
	char := models.Character{ProjectID: project.ID, Name: "主角"}
	db.Create(&char)
	lookSvc := NewCharacterLookService(db, nil)

	// Create looks with unique names (name is unique per character in a project)
	var lookIDs []uint
	for i := 1; i <= 3; i++ {
		look := models.CharacterLook{
			ProjectID: project.ID, CharacterID: char.ID,
			Name: fmt.Sprintf("造型%d", i), Category: "clothing", Description: "描述",
			AuditStatus: models.LookStatusApproved,
		}
		if err := db.Create(&look).Error; err != nil {
			t.Fatalf("create look: %v", err)
		}
		lookIDs = append(lookIDs, look.ID)
	}

	// Batch assign all looks to shot - use AssignLooksToShot which handles replace semantics
	assignments := make([]ShotLookAssignmentRequest, len(lookIDs))
	for i, id := range lookIDs {
		assignments[i] = ShotLookAssignmentRequest{LookID: id, Order: i + 1, IsFeatured: i == 0}
	}
	if err := lookSvc.AssignLooksToShot(shot.ID, assignments); err != nil {
		t.Fatalf("AssignLooksToShot: %v", err)
	}

	// Get looks by shot (simplified version)
	looks, err := lookSvc.GetLooksByShot(shot.ID)
	if err != nil {
		t.Fatal(err)
	}
	if len(looks) != 3 {
		t.Fatalf("expected 3 looks, got %d", len(looks))
	}
}

// TestShotLookDeleteCascades tests that deleting a look removes shot associations.
func TestShotLookDeleteCascades(t *testing.T) {
	// Create test DB with all required models including AssetVariant
	db, err := newTestDBWithAssetVariants(t)
	if err != nil {
		t.Fatal(err)
	}
	project := models.Project{Title: "look-delete-cascade"}
	db.Create(&project)
	scene := models.Scene{ProjectID: project.ID, EpisodeN: 1, Order: 1}
	db.Create(&scene)
	shot := models.Shot{SceneID: scene.ID, Order: 1, ShotType: "medium", Duration: 2}
	db.Create(&shot)
	char := models.Character{ProjectID: project.ID, Name: "主角"}
	db.Create(&char)
	lookSvc := NewCharacterLookService(db, nil)

	look := models.CharacterLook{
		ProjectID: project.ID, CharacterID: char.ID, Name: "待删造型",
		Category: "clothing", Description: "描述",
	}
	db.Create(&look)
	lookSvc.AssignToShot(shot.ID, look.ID, 1, false)

	// Verify assignment exists
	looks, _ := lookSvc.GetShotLooks(shot.ID)
	if len(looks) != 1 {
		t.Fatal("expected 1 look before delete")
	}

	// Delete look (should cascade to shot associations)
	if err := lookSvc.DeleteLook(look.ID); err != nil {
		t.Fatal(err)
	}

	// Verify association removed
	looks, _ = lookSvc.GetShotLooks(shot.ID)
	if len(looks) != 0 {
		t.Fatalf("expected 0 looks after look deletion, got %d", len(looks))
	}
}

// newTestDBWithAssetVariants creates a test DB with AssetVariant model included.
func newTestDBWithAssetVariants(t *testing.T) (*gorm.DB, error) {
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{Logger: logger.Default.LogMode(logger.Silent)})
	if err != nil {
		return nil, err
	}
	if err := db.AutoMigrate(
		&models.Project{}, &models.Episode{}, &models.Scene{}, &models.Shot{}, &models.PromptVersion{}, &models.StylePreset{},
		&models.Character{}, &models.CharacterLook{}, &models.ShotCharacterLook{},
		&models.AssetVariant{}, // Required for DeleteLook cascade
	); err != nil {
		return nil, err
	}
	return db, nil
}
