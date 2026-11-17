package service

import (
	"bytes"
	"fmt"
	"image"
	"image/color"
	"image/png"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"

	"comfyui-console/internal/models"
)

// quadrantPanorama colours each 90° longitude band: centre-left red, centre-right green,
// right edge blue, left edge yellow. Column w/2 is longitude 0 (straight ahead).
func quadrantPanorama(w, h int) *image.RGBA {
	img := image.NewRGBA(image.Rect(0, 0, w, h))
	bands := []color.RGBA{{255, 255, 0, 255}, {255, 0, 0, 255}, {0, 255, 0, 255}, {0, 0, 255, 255}}
	for y := 0; y < h; y++ {
		for x := 0; x < w; x++ {
			img.SetRGBA(x, y, bands[x*4/w])
		}
	}
	return img
}

func encodePNG(t *testing.T, img image.Image) []byte {
	t.Helper()
	var buf bytes.Buffer
	if err := png.Encode(&buf, img); err != nil {
		t.Fatal(err)
	}
	return buf.Bytes()
}

func TestERPToPerspectiveLooksInYawDirection(t *testing.T) {
	pano := quadrantPanorama(800, 400)
	// A view centred just right of 0° sees the green band; turning 90° more reaches blue,
	// 180° from there is the opposite side (yellow), and 270° is red.
	for _, c := range []struct {
		yaw  float64
		want color.RGBA
	}{{20, color.RGBA{0, 255, 0, 255}}, {110, color.RGBA{0, 0, 255, 255}}, {200, color.RGBA{255, 255, 0, 255}}, {290, color.RGBA{255, 0, 0, 255}}} {
		view := erpToPerspective(pano, 64, 36, c.yaw, 30)
		if got := view.RGBAAt(32, 18); got != c.want {
			t.Fatalf("yaw %g centre=%v want %v", c.yaw, got, c.want)
		}
	}
	// The seam behind the viewer wraps instead of clamping: yaw 180 shows blue on its left
	// half and yellow on its right half.
	view := erpToPerspective(pano, 64, 36, 180, 60)
	if l, r := view.RGBAAt(8, 18), view.RGBAAt(56, 18); l != (color.RGBA{0, 0, 255, 255}) || r != (color.RGBA{255, 255, 0, 255}) {
		t.Fatalf("seam left=%v right=%v", l, r)
	}
}

func TestSplitPanoramaRequiresTrueERP(t *testing.T) {
	if _, err := splitPanorama(encodePNG(t, quadrantPanorama(2400, 1000)), 6); err == nil || !strings.Contains(err.Error(), "不是标准 ERP") {
		t.Fatalf("a 2.4:1 crop must be rejected, err=%v", err)
	}
	if _, err := splitPanorama(encodePNG(t, quadrantPanorama(1600, 800)), 6); err == nil || !strings.Contains(err.Error(), "至少 2048") {
		t.Fatalf("a small panorama must be rejected, err=%v", err)
	}
	if _, err := splitPanorama(encodePNG(t, quadrantPanorama(2048, 1024)), 5); err == nil || !strings.Contains(err.Error(), "4、6 或 8") {
		t.Fatalf("unsupported view count must be rejected, err=%v", err)
	}
	views, err := splitPanorama(encodePNG(t, quadrantPanorama(2048, 1024)), 4)
	if err != nil {
		t.Fatal(err)
	}
	if len(views) != 4 || views[1].Yaw != 90 || views[3].Yaw != 270 {
		t.Fatalf("views=%d yaws=%v,%v", len(views), views[1].Yaw, views[3].Yaw)
	}
	cfg, format, _ := image.DecodeConfig(bytes.NewReader(views[0].PNG))
	if format != "png" || cfg.Width != 640 || cfg.Height != 360 {
		t.Fatalf("view %s %dx%d", format, cfg.Width, cfg.Height)
	}
}

func TestSplitLocationPanoramaAddsUnselectedVariants(t *testing.T) {
	f := newFrameConvertFixture(t)
	if err := f.ps.db.AutoMigrate(&models.Asset{}, &models.AssetVariant{}, &models.UploadFile{}); err != nil {
		t.Fatal(err)
	}
	pid := f.source.ProjectID
	if _, _, err := f.upload.SaveFile(fmt.Sprint(pid), "image", "loc_current.png", testPNG(t, 64, 36)); err != nil {
		t.Fatal(err)
	}
	asset := models.Asset{ProjectID: pid, Kind: AssetKindLocation, Name: "山门", Image: "loc_current.png"}
	f.ps.db.Create(&asset)
	f.ps.db.Create(&models.AssetVariant{ProjectID: pid, EntityType: VariantAssetImage, EntityID: asset.ID, File: "loc_current.png", Selected: true})

	rows, err := f.ps.SplitLocationPanorama(&asset, encodePNG(t, quadrantPanorama(2048, 1024)), ".png", 6)
	if err != nil {
		t.Fatal(err)
	}
	if len(rows) != 6 || rows[0].Selected || !strings.HasSuffix(rows[0].File, ".png") || !strings.Contains(rows[0].Prompt, "正前方") || !strings.Contains(rows[3].Prompt, "正后方") || !strings.HasPrefix(rows[1].Provenance, "panorama:pano_") {
		t.Fatalf("rows=%+v", rows)
	}
	var reloaded models.Asset
	f.ps.db.First(&reloaded, asset.ID)
	var selected models.AssetVariant
	f.ps.db.Where("entity_id = ? AND selected = ?", asset.ID, true).First(&selected)
	if reloaded.Image != "loc_current.png" || selected.File != "loc_current.png" {
		t.Fatalf("current reference changed: image=%q selected=%q", reloaded.Image, selected.File)
	}
	if _, err := f.ps.readProjectInput(pid, rows[5].File); err != nil {
		t.Fatalf("view file missing: %v", err)
	}
	// Explicitly selecting a view is the existing variant flow.
	if _, err := NewAssetVariantService(f.ps.db).Select(pid, rows[2].ID); err != nil {
		t.Fatal(err)
	}
	f.ps.db.First(&reloaded, asset.ID)
	if reloaded.Image != rows[2].File {
		t.Fatalf("selected view not applied: %q", reloaded.Image)
	}

	prop := models.Asset{ProjectID: pid, Kind: AssetKindProp, Name: "剑"}
	f.ps.db.Create(&prop)
	if _, err := f.ps.SplitLocationPanorama(&prop, encodePNG(t, quadrantPanorama(2048, 1024)), ".png", 6); err == nil {
		t.Fatal("props must not accept panoramas")
	}
}

func TestHandleSplitLocationPanoramaRejectsNonERP(t *testing.T) {
	f := newFrameConvertFixture(t)
	f.ps.db.AutoMigrate(&models.Asset{}, &models.AssetVariant{}, &models.UploadFile{})
	asset := models.Asset{ProjectID: f.source.ProjectID, Kind: AssetKindLocation, Name: "山门"}
	f.ps.db.Create(&asset)
	svc := &Service{DB: f.ps.db, Projects: f.ps}
	gin.SetMode(gin.TestMode)
	r := gin.New()
	r.POST("/api/projects/:id/assets/:kind/:aid/panorama", svc.HandleSplitLocationPanorama)
	var body bytes.Buffer
	mw := multipart.NewWriter(&body)
	mw.WriteField("view_count", "6")
	part, _ := mw.CreateFormFile("file", "wide.png")
	part.Write(encodePNG(t, quadrantPanorama(2400, 1000)))
	mw.Close()
	req := httptest.NewRequest(http.MethodPost, fmt.Sprintf("/api/projects/%d/assets/location/%d/panorama", asset.ProjectID, asset.ID), &body)
	req.Header.Set("Content-Type", mw.FormDataContentType())
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)
	if w.Code != http.StatusBadRequest || !strings.Contains(w.Body.String(), "ERP") {
		t.Fatalf("status=%d body=%s", w.Code, w.Body.String())
	}
	var count int64
	f.ps.db.Model(&models.AssetVariant{}).Count(&count)
	if count != 0 {
		t.Fatalf("rejected panorama created %d variants", count)
	}
}
