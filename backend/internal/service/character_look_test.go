package service

import (
	"strings"
	"testing"

	"github.com/glebarez/sqlite"
	"gorm.io/gorm"

	"comfyui-console/internal/models"
)

type db = *gorm.DB

// setupTestDB 创建测试数据库（使用 SQLite 内存数据库）
func setupTestDB(t *testing.T) *gorm.DB {
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	if err != nil {
		t.Skipf("跳过测试: %v", err)
	}
	db.AutoMigrate(
		&models.Project{},
		&models.Character{},
		&models.CharacterLook{},
		&models.SceneCharacterLook{},
		&models.ShotCharacterLook{},
		&models.Scene{},
		&models.Shot{},
	)
	return db
}

func TestLookCategoryLabel(t *testing.T) {
	cases := []struct {
		category string
		want     string
	}{
		{"clothing", "服装"},
		{"shoes", "鞋履"},
		{"hair", "发型"},
		{"hair_accessory", "发饰"},
		{"jewelry", "首饰"},
		{"bag", "包"},
		{"full", "旧版组合造型"},
		{"unknown", "unknown"},
	}

	for _, tc := range cases {
		t.Run(tc.category, func(t *testing.T) {
			got := LookCategoryLabel(tc.category)
			if got != tc.want {
				t.Errorf("LookCategoryLabel(%q) = %q, want %q", tc.category, got, tc.want)
			}
		})
	}
}

func TestValidLookCategories(t *testing.T) {
	valid := []string{"clothing", "shoes", "hair", "hair_accessory", "jewelry", "bag", "full"}
	for _, cat := range valid {
		if !ValidLookCategories[cat] {
			t.Errorf("ValidLookCategories should contain %q", cat)
		}
	}

	invalid := []string{"weapon", "costume", "makeup", "hat"}
	for _, cat := range invalid {
		if ValidLookCategories[cat] {
			t.Errorf("ValidLookCategories should not contain %q", cat)
		}
	}
}

func TestCreateLookValidation(t *testing.T) {
	db := setupTestDB(t)
	service := NewCharacterLookService(db, nil)

	// 创建测试项目和角色
	p := models.Project{Title: "测试项目"}
	if err := db.Create(&p).Error; err != nil {
		t.Fatal(err)
	}
	ch := models.Character{ProjectID: p.ID, Name: "林夏", Role: "女主"}
	if err := db.Create(&ch).Error; err != nil {
		t.Fatal(err)
	}

	// 测试空名称
	_, err := service.CreateLook(models.CharacterLook{
		ProjectID:   p.ID,
		CharacterID: ch.ID,
		Name:        "",
		Category:    "clothing",
	})
	if err == nil {
		t.Error("期望空名称返回错误")
	}

	// 测试无效分类
	_, err = service.CreateLook(models.CharacterLook{
		ProjectID:   p.ID,
		CharacterID: ch.ID,
		Name:        "日常装",
		Category:    "invalid_category",
	})
	if err == nil {
		t.Error("期望无效分类返回错误")
	}

	// 测试有效创建
	look, err := service.CreateLook(models.CharacterLook{
		ProjectID:   p.ID,
		CharacterID: ch.ID,
		Name:        "日常装",
		Category:    "clothing",
		Description: "简单的日常服装",
	})
	if err != nil {
		t.Fatalf("创建造型失败: %v", err)
	}
	if look.Source != "manual" {
		t.Errorf("Source = %q, want %q", look.Source, "manual")
	}
	if look.AuditStatus != models.LookStatusDraft {
		t.Errorf("AuditStatus = %q, want %q", look.AuditStatus, models.LookStatusDraft)
	}
}

func TestListByCharacter(t *testing.T) {
	db := setupTestDB(t)
	service := NewCharacterLookService(db, nil)

	// 创建测试项目和角色
	p := models.Project{Title: "测试项目"}
	if err := db.Create(&p).Error; err != nil {
		t.Fatal(err)
	}
	ch := models.Character{ProjectID: p.ID, Name: "林夏", Role: "女主"}
	if err := db.Create(&ch).Error; err != nil {
		t.Fatal(err)
	}

	// 创建多个造型
	for i := 1; i <= 3; i++ {
		_, err := service.CreateLook(models.CharacterLook{
			ProjectID:   p.ID,
			CharacterID: ch.ID,
			Name:        "造型" + string(rune('0'+i)),
			Category:    "clothing",
			Priority:    i,
		})
		if err != nil {
			t.Fatal(err)
		}
	}

	looks, err := service.ListByCharacter(ch.ID)
	if err != nil {
		t.Fatalf("ListByCharacter 失败: %v", err)
	}
	if len(looks) != 3 {
		t.Errorf("造型数量 = %d, want %d", len(looks), 3)
	}

	// 验证按优先级排序
	if looks[0].Priority < looks[1].Priority {
		t.Error("造型应按优先级降序排列")
	}
}

func TestApproveAndRejectLook(t *testing.T) {
	db := setupTestDB(t)
	service := NewCharacterLookService(db, nil)

	// 创建测试数据
	p := models.Project{Title: "测试项目"}
	if err := db.Create(&p).Error; err != nil {
		t.Fatal(err)
	}
	ch := models.Character{ProjectID: p.ID, Name: "林夏", Role: "女主"}
	if err := db.Create(&ch).Error; err != nil {
		t.Fatal(err)
	}
	look, err := service.CreateLook(models.CharacterLook{
		ProjectID:   p.ID,
		CharacterID: ch.ID,
		Name:        "日常装",
		Category:    "clothing",
		Description: "简单的日常服装",
	})
	if err != nil {
		t.Fatal(err)
	}

	// 测试驳回（无需描述）
	err = service.RejectLook(look.ID, "造型需要修改")
	if err != nil {
		t.Fatalf("RejectLook 失败: %v", err)
	}

	db.First(look, look.ID)
	if look.AuditStatus != models.LookStatusRejected {
		t.Errorf("AuditStatus = %q, want %q", look.AuditStatus, models.LookStatusRejected)
	}
	if look.AuditNote != "造型需要修改" {
		t.Errorf("AuditNote = %q, want %q", look.AuditNote, "造型需要修改")
	}

	// 测试审核通过（需要描述）
	err = service.ApproveLook(look.ID, "审核通过")
	if err != nil {
		t.Fatalf("ApproveLook 失败: %v", err)
	}

	db.First(look, look.ID)
	if look.AuditStatus != models.LookStatusApproved {
		t.Errorf("AuditStatus = %q, want %q", look.AuditStatus, models.LookStatusApproved)
	}
}

func TestPublishLookRequiresApproval(t *testing.T) {
	db := setupTestDB(t)
	service := NewCharacterLookService(db, nil)

	// 创建测试数据
	p := models.Project{Title: "测试项目"}
	if err := db.Create(&p).Error; err != nil {
		t.Fatal(err)
	}
	ch := models.Character{ProjectID: p.ID, Name: "林夏", Role: "女主"}
	if err := db.Create(&ch).Error; err != nil {
		t.Fatal(err)
	}
	look, err := service.CreateLook(models.CharacterLook{
		ProjectID:   p.ID,
		CharacterID: ch.ID,
		Name:        "日常装",
		Category:    "clothing",
		Description: "简单的日常服装",
	})
	if err != nil {
		t.Fatal(err)
	}

	// 未审核的造型不能发布
	err = service.PublishLook(look.ID)
	if err == nil {
		t.Error("期望未审核造型发布失败")
	}

	// 审核通过后可以发布
	if err := service.ApproveLook(look.ID, "通过"); err != nil {
		t.Fatal(err)
	}
	if err := service.PublishLook(look.ID); err != nil {
		t.Fatalf("审核后发布失败: %v", err)
	}

	db.First(look, look.ID)
	if look.AuditStatus != models.LookStatusPublished {
		t.Errorf("AuditStatus = %q, want %q", look.AuditStatus, models.LookStatusPublished)
	}
}

func TestSceneLookAssignment(t *testing.T) {
	db := setupTestDB(t)
	service := NewCharacterLookService(db, nil)

	// 创建测试数据
	p := models.Project{Title: "测试项目"}
	if err := db.Create(&p).Error; err != nil {
		t.Fatal(err)
	}
	ch := models.Character{ProjectID: p.ID, Name: "林夏", Role: "女主"}
	if err := db.Create(&ch).Error; err != nil {
		t.Fatal(err)
	}
	look1, _ := service.CreateLook(models.CharacterLook{
		ProjectID:   p.ID,
		CharacterID: ch.ID,
		Name:        "造型1",
		Category:    "clothing",
	})
	look2, _ := service.CreateLook(models.CharacterLook{
		ProjectID:   p.ID,
		CharacterID: ch.ID,
		Name:        "造型2",
		Category:    "hair",
	})
	sc := models.Scene{ProjectID: p.ID, EpisodeN: 1, Order: 1, Title: "测试场景"}
	if err := db.Create(&sc).Error; err != nil {
		t.Fatal(err)
	}

	// 批量关联造型到场景
	assignments := []SceneLookAssignmentRequest{
		{LookID: look1.ID, Order: 1, IsFeatured: true},
		{LookID: look2.ID, Order: 2, IsFeatured: false},
	}
	if err := service.AssignLooksToScene(sc.ID, assignments); err != nil {
		t.Fatalf("AssignLooksToScene 失败: %v", err)
	}

	// 获取场景造型
	scl, err := service.GetSceneLooks(sc.ID)
	if err != nil {
		t.Fatalf("GetSceneLooks 失败: %v", err)
	}
	if len(scl) != 2 {
		t.Errorf("场景造型数量 = %d, want %d", len(scl), 2)
	}

	// 验证主推造型
	for _, item := range scl {
		if item.LookID == look1.ID && !item.IsFeatured {
			t.Error("造型1应该是主推造型")
		}
	}
}

func TestShotLookAssignment(t *testing.T) {
	db := setupTestDB(t)
	service := NewCharacterLookService(db, nil)

	// 创建测试数据
	p := models.Project{Title: "测试项目"}
	if err := db.Create(&p).Error; err != nil {
		t.Fatal(err)
	}
	ch := models.Character{ProjectID: p.ID, Name: "林夏", Role: "女主"}
	if err := db.Create(&ch).Error; err != nil {
		t.Fatal(err)
	}
	look, _ := service.CreateLook(models.CharacterLook{
		ProjectID:     p.ID,
		CharacterID:   ch.ID,
		Name:          "战斗服装",
		Category:      "clothing",
		Description:   "镜头中需要保持一致的黑色战斗服装",
		IsShotRelated: true,
		Priority:      100,
	})
	if err := service.ApproveLook(look.ID, "通过"); err != nil {
		t.Fatal(err)
	}
	sc := models.Scene{ProjectID: p.ID, EpisodeN: 1, Order: 1, Title: "测试场景"}
	if err := db.Create(&sc).Error; err != nil {
		t.Fatal(err)
	}
	shot := models.Shot{SceneID: sc.ID, Order: 1, ShotType: "特写", Duration: 2.0}
	if err := db.Create(&shot).Error; err != nil {
		t.Fatal(err)
	}

	// 关联造型到镜头
	assignments := []ShotLookAssignmentRequest{
		{LookID: look.ID, Order: 1, IsFeatured: true},
	}
	if err := service.AssignLooksToShot(shot.ID, assignments); err != nil {
		t.Fatalf("AssignLooksToShot 失败: %v", err)
	}

	// 获取镜头相关造型
	shotLooks, err := service.GetShotRelatedLooks(shot.ID)
	if err != nil {
		t.Fatalf("GetShotRelatedLooks 失败: %v", err)
	}
	if len(shotLooks) != 1 {
		t.Errorf("镜头造型数量 = %d, want %d", len(shotLooks), 1)
	}

	// 验证按优先级排序
	for i := 0; i < len(shotLooks)-1; i++ {
		if shotLooks[i].Priority < shotLooks[i+1].Priority {
			t.Error("造型应按优先级降序排列")
		}
	}
}

func TestGetLookReferenceImages(t *testing.T) {
	db := setupTestDB(t)
	service := NewCharacterLookService(db, nil)

	// 创建测试数据
	p := models.Project{Title: "测试项目"}
	if err := db.Create(&p).Error; err != nil {
		t.Fatal(err)
	}
	ch := models.Character{ProjectID: p.ID, Name: "林夏", Role: "女主"}
	if err := db.Create(&ch).Error; err != nil {
		t.Fatal(err)
	}

	// 创建造型（无图片）
	looks := []models.CharacterLook{
		{ProjectID: p.ID, CharacterID: ch.ID, Name: "造型1", Category: "full", AuditStatus: models.LookStatusApproved},
		{ProjectID: p.ID, CharacterID: ch.ID, Name: "造型2", Category: "clothing", AuditStatus: models.LookStatusApproved},
	}
	for i := range looks {
		if err := db.Create(&looks[i]).Error; err != nil {
			t.Fatal(err)
		}
	}

	sc := models.Scene{ProjectID: p.ID, EpisodeN: 1, Order: 1, Title: "测试场景"}
	if err := db.Create(&sc).Error; err != nil {
		t.Fatal(err)
	}

	// 关联造型到场景
	for i, look := range looks {
		scl := models.SceneCharacterLook{
			SceneID: sc.ID, LookID: look.ID, Order: i + 1, IsFeatured: i == 0,
		}
		if err := db.Create(&scl).Error; err != nil {
			t.Fatal(err)
		}
	}

	// 获取参考图（预期为空，因为没有实际图片）
	refs, lines, err := service.GetLookReferenceImages(sc.ID, 9)
	if err != nil {
		t.Fatalf("GetLookReferenceImages 失败: %v", err)
	}
	if len(refs) != 0 {
		t.Errorf("无图片时参考图数量 = %d, want %d", len(refs), 0)
	}
	if len(lines) != 0 {
		t.Errorf("无图片时描述行数 = %d, want %d", len(lines), 0)
	}
}

func TestEnsureDefaultLooksSplitsWardrobeCatalog(t *testing.T) {
	db := setupTestDB(t)
	provider := &stubTextProvider{response: `{"assets":[{"name":"浅蓝常服","category":"clothing","description":"浅蓝色立领斜襟布衫与浅蓝色长裙"},{"name":"鹅黄常服","category":"clothing","description":"鹅黄色对襟布衫与鹅黄色长裙"},{"name":"珍珠耳坠","category":"jewelry","description":"一对淡粉色圆润珍珠耳坠"},{"name":"梅花绣鞋","category":"shoes","description":"浅蓝色布面梅花绣鞋"}]}`}
	svc := NewCharacterLookService(db, provider)
	project := models.Project{Title: "测试", Style: "国漫"}
	db.Create(&project)
	char := models.Character{ProjectID: project.ID, Name: "林采微", WardrobeDetail: "浅蓝与鹅黄两套常服，珍珠耳坠，梅花绣鞋"}
	db.Create(&char)
	looks, err := svc.EnsureDefaultLooks(&char, &project)
	if err != nil {
		t.Fatal(err)
	}
	if len(looks) != 4 {
		t.Fatalf("expected four separate assets, got %#v", looks)
	}
	for _, look := range looks {
		if look.Category == "full" || strings.Contains(look.Prompt, char.Name) {
			t.Fatalf("asset not separated: %#v", look)
		}
	}
	var count int64
	db.Model(&models.CharacterLook{}).Where("character_id = ? AND is_default = ?", char.ID, true).Count(&count)
	if count != 4 {
		t.Fatalf("expected four persisted assets, got %d", count)
	}
}

func TestGenerateLookPromptContainsOnlyOneAsset(t *testing.T) {
	db := setupTestDB(t)
	svc := NewCharacterLookService(db, nil)
	project := models.Project{Title: "测试", Style: "国漫"}
	db.Create(&project)
	char := models.Character{ProjectID: project.ID, Name: "林采微", Role: "女主", Appearance: "黑色长发，鹅蛋脸", WardrobeDetail: "浅蓝长裙，银色项链"}
	db.Create(&char)
	look := models.CharacterLook{ProjectID: project.ID, CharacterID: char.ID, Name: "珍珠耳坠", Category: "jewelry", Description: "一对淡粉色圆润珍珠耳坠，银质耳钩"}
	db.Create(&look)
	prompt, err := svc.GeneratePrompt(&look, &char, &project)
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{"一件首饰", "珍珠耳坠", "银质耳钩", "国漫"} {
		if !strings.Contains(prompt, want) {
			t.Fatalf("asset prompt missing %q: %s", want, prompt)
		}
	}
	for _, forbidden := range []string{"林采微", "女主", "黑色长发", "鹅蛋脸", "浅蓝长裙", "角色外貌", "单一角色", "全身照"} {
		if strings.Contains(prompt, forbidden) {
			t.Fatalf("asset prompt mixed character content %q: %s", forbidden, prompt)
		}
	}
}

func TestExpandLookGeneratesDescriptionAndPrompt(t *testing.T) {
	db := setupTestDB(t)
	provider := &stubTextProvider{response: `{"description":"深蓝色丝绒长裙，银线收边，搭配同色包头鞋，适合正式宴会场景。","prompt":"一套深蓝色丝绒晚礼服穿搭组合，银线收边，搭配同色包头鞋，材质和结构清晰，纯色背景，电影写实风格。"}`}
	svc := NewCharacterLookService(db, provider)
	project := models.Project{Title: "测试", Genre: "都市", Style: "真人电影感"}
	db.Create(&project)
	char := models.Character{ProjectID: project.ID, Name: "林夏", Appearance: "25岁女性，黑色长发"}
	db.Create(&char)
	look := models.CharacterLook{ProjectID: project.ID, CharacterID: char.ID, Name: "宴会装", Category: "full", Description: "蓝色礼服"}
	db.Create(&look)
	if _, err := svc.ExpandDescription(&look, &char, &project); err != nil {
		t.Fatal(err)
	}
	var got models.CharacterLook
	db.First(&got, look.ID)
	if !strings.Contains(got.Description, "深蓝色") || !strings.Contains(got.Prompt, "穿搭组合") {
		t.Fatalf("unexpected expanded look: %#v", got)
	}
	if got.AuditStatus != models.LookStatusDraft || got.Image != "" {
		t.Fatalf("expanded look must await review and invalidate image")
	}
}

func TestUpdateLookRequiresReaudit(t *testing.T) {
	db := setupTestDB(t)
	service := NewCharacterLookService(db, nil)

	// 创建测试数据
	p := models.Project{Title: "测试项目"}
	if err := db.Create(&p).Error; err != nil {
		t.Fatal(err)
	}
	ch := models.Character{ProjectID: p.ID, Name: "林夏", Role: "女主"}
	if err := db.Create(&ch).Error; err != nil {
		t.Fatal(err)
	}
	look, _ := service.CreateLook(models.CharacterLook{
		ProjectID:   p.ID,
		CharacterID: ch.ID,
		Name:        "日常装",
		Category:    "clothing",
		Description: "原始描述",
	})
	service.ApproveLook(look.ID, "通过")

	// 更新造型描述
	look.Description = "新描述"
	_, err := service.UpdateLook(look.ID, *look)
	if err != nil {
		t.Fatalf("UpdateLook 失败: %v", err)
	}

	db.First(look, look.ID)
	if look.AuditStatus != models.LookStatusDraft {
		t.Errorf("更新后状态 = %q, want %q", look.AuditStatus, models.LookStatusDraft)
	}
	if look.AuditVersion != 2 {
		t.Errorf("AuditVersion = %d, want %d", look.AuditVersion, 2)
	}
}
