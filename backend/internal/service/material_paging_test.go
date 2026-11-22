package service

import (
	"encoding/json"
	"net/http/httptest"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/glebarez/sqlite"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"

	"comfyui-console/internal/models"
)

func newMaterialPagingDB(t *testing.T) *gorm.DB {
	t.Helper()
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{Logger: logger.Default.LogMode(logger.Silent)})
	if err != nil {
		t.Fatal(err)
	}
	if err := db.AutoMigrate(&models.Material{}); err != nil {
		t.Fatal(err)
	}
	return db
}

// 素材列表分页：新素材总在第一页；page_size 不传时保持旧行为（拿全量），
// 老调用方不会因为这次改动突然只拿到一页。
func TestListMaterialsPagingKeepsNewestFirstAndDefaultsToAll(t *testing.T) {
	db := newMaterialPagingDB(t)
	for i := 1; i <= 5; i++ {
		mat := models.Material{Name: "m" + string(rune('0'+i)), Type: "3d", Path: "p", Size: int64(i)}
		if err := db.Create(&mat).Error; err != nil {
			t.Fatal(err)
		}
	}
	svc := &MaterialService{db: db}

	all, err := svc.List("3d", nil)
	if err != nil {
		t.Fatal(err)
	}
	if len(all) != 5 {
		t.Fatalf("不分页应返回全部 5 条，得到 %d", len(all))
	}
	if all[0].Name != "m5" {
		t.Fatalf("应按 id desc，最新在前，得到 %q", all[0].Name)
	}

	first, err := svc.ListPaged("3d", nil, 1, 2)
	if err != nil {
		t.Fatal(err)
	}
	if len(first) != 2 || first[0].Name != "m5" || first[1].Name != "m4" {
		t.Fatalf("第一页应是最新两条：%+v", first)
	}

	second, err := svc.ListPaged("3d", nil, 2, 2)
	if err != nil {
		t.Fatal(err)
	}
	if len(second) != 2 || second[0].Name != "m3" {
		t.Fatalf("第二页应从 m3 开始：%+v", second)
	}

	// page 非正数按第一页处理，别让 Offset 变负数
	clamped, err := svc.ListPaged("3d", nil, 0, 2)
	if err != nil {
		t.Fatal(err)
	}
	if len(clamped) != 2 || clamped[0].Name != "m5" {
		t.Fatalf("page=0 应按第一页处理：%+v", clamped)
	}

	// 越界页返回空列表而不是报错
	beyond, err := svc.ListPaged("3d", nil, 9, 2)
	if err != nil {
		t.Fatal(err)
	}
	if len(beyond) != 0 {
		t.Fatalf("越界页应为空，得到 %d 条", len(beyond))
	}
}

// 接口层：page_size 缺省/非法都不该减少返回条数。
func TestHandleListMaterialsIgnoresInvalidPaging(t *testing.T) {
	gin.SetMode(gin.TestMode)
	db := newMaterialPagingDB(t)
	for i := 0; i < 3; i++ {
		if err := db.Create(&models.Material{Name: "m", Type: "3d", Path: "p"}).Error; err != nil {
			t.Fatal(err)
		}
	}
	svc := &Service{Materials: &MaterialService{db: db}}

	for _, query := range []string{"/api/materials?type=3d", "/api/materials?type=3d&page_size=abc", "/api/materials?type=3d&page=2&page_size=0"} {
		rec := httptest.NewRecorder()
		ctx, _ := gin.CreateTestContext(rec)
		ctx.Request = httptest.NewRequest("GET", query, nil)
		svc.HandleListMaterials(ctx)
		if rec.Code != 200 {
			t.Fatalf("%s 应 200，得到 %d", query, rec.Code)
		}
		var list []models.Material
		if err := json.Unmarshal(rec.Body.Bytes(), &list); err != nil {
			t.Fatalf("%s 返回不是 JSON 列表：%v", query, err)
		}
		if len(list) != 3 {
			t.Fatalf("%s 应返回全量 3 条，得到 %d", query, len(list))
		}
	}

	rec := httptest.NewRecorder()
	ctx, _ := gin.CreateTestContext(rec)
	ctx.Request = httptest.NewRequest("GET", "/api/materials?type=3d&page=1&page_size=2", nil)
	svc.HandleListMaterials(ctx)
	var paged []models.Material
	if err := json.Unmarshal(rec.Body.Bytes(), &paged); err != nil {
		t.Fatalf("分页返回不是 JSON 列表：%v", err)
	}
	if len(paged) != 2 {
		t.Fatalf("page_size=2 应只返回 2 条，得到 %d", len(paged))
	}
}