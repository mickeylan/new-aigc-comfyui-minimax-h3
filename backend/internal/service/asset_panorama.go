package service

import (
	"bytes"
	"errors"
	"fmt"
	"image"
	"image/draw"
	"image/png"
	"log"
	"math"
	"net/http"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/gin-gonic/gin"

	"comfyui-console/internal/models"
)

// Location panoramas: a true equirectangular (ERP, 2:1, 360°×180°) image is sampled locally into
// horizontal 16:9 perspective PNG views. Each view is registered as an unselected asset_image variant
// of the location, so the user previews them in the version list and explicitly picks one; the
// current reference image and its consumers are untouched until then.
const (
	panoramaMaxUpload   = 64 << 20
	panoramaMinWidth    = 2048
	panoramaMaxPixels   = 8192 * 4096
	panoramaRatioMin    = 1.95
	panoramaRatioMax    = 2.05
	panoramaFOVDegrees  = 75.0
	panoramaMaxTileW    = 1280
	panoramaMinTileW    = 640
	panoramaDefaultView = 6
)

// panoramaViewCounts stay within MaxNonFavoriteAssetVariants so one split cannot push out the views
// it just created.
var panoramaViewCounts = map[int]bool{4: true, 6: true, 8: true}

// panoramaTileSize matches a view's native ERP resolution, clamped to [640,1280] wide.
func panoramaTileSize(panoWidth int) (int, int) {
	w := int(float64(panoWidth) * panoramaFOVDegrees / 360)
	w = min(panoramaMaxTileW, max(panoramaMinTileW, w/16*16))
	return w, w * 9 / 16
}

// erpToPerspective samples a pinhole view looking at yawDeg (0 = panorama centre, positive turns
// right) with pitch 0 and a horizontal field of view of fovDeg. The panorama wraps horizontally.
func erpToPerspective(pano *image.RGBA, outW, outH int, yawDeg, fovDeg float64) *image.RGBA {
	pw, ph := pano.Bounds().Dx(), pano.Bounds().Dy()
	out := image.NewRGBA(image.Rect(0, 0, outW, outH))
	f := float64(outW) / 2 / math.Tan(fovDeg*math.Pi/360)
	sinY, cosY := math.Sincos(yawDeg * math.Pi / 180)
	for y := 0; y < outH; y++ {
		gy := float64(y) + 0.5 - float64(outH)/2
		for x := 0; x < outW; x++ {
			gx := float64(x) + 0.5 - float64(outW)/2
			dx, dz := gx*cosY+f*sinY, -gx*sinY+f*cosY
			lon := math.Atan2(dx, dz)
			lat := math.Atan2(gy, math.Hypot(dx, dz))
			u := (lon/(2*math.Pi)+0.5)*float64(pw) - 0.5
			v := math.Min(math.Max((lat/math.Pi+0.5)*float64(ph)-0.5, 0), float64(ph-1))
			u0, v0 := int(math.Floor(u)), int(v)
			du, dv := u-float64(u0), v-float64(v0)
			u0 = ((u0 % pw) + pw) % pw
			u1, v1 := (u0+1)%pw, min(v0+1, ph-1)
			o := out.PixOffset(x, y)
			for c := 0; c < 3; c++ {
				p := func(px, py int) float64 { return float64(pano.Pix[pano.PixOffset(px, py)+c]) }
				top := p(u0, v0)*(1-du) + p(u1, v0)*du
				bot := p(u0, v1)*(1-du) + p(u1, v1)*du
				out.Pix[o+c] = uint8(math.Round(top*(1-dv) + bot*dv))
			}
			out.Pix[o+3] = 255
		}
	}
	return out
}

type panoramaView struct {
	Yaw float64
	PNG []byte
}

// splitPanorama validates data as a true ERP panorama and returns count evenly spaced views.
func splitPanorama(data []byte, count int) ([]panoramaView, error) {
	if !panoramaViewCounts[count] {
		return nil, fmt.Errorf("视角数只能是 4、6 或 8")
	}
	cfg, _, err := image.DecodeConfig(bytes.NewReader(data))
	if err != nil {
		return nil, fmt.Errorf("无法读取全景图: %w", err)
	}
	ratio := float64(cfg.Width) / float64(max(1, cfg.Height))
	if ratio < panoramaRatioMin || ratio > panoramaRatioMax {
		return nil, fmt.Errorf("不是标准 ERP 全景图：宽高比应为 2:1（360°×180°），当前 %dx%d（%.2f:1）；裁切过的宽幅图或普通照片无法拆出正确视角", cfg.Width, cfg.Height, ratio)
	}
	if cfg.Width < panoramaMinWidth {
		return nil, fmt.Errorf("全景图宽度至少 %d 像素，当前 %d", panoramaMinWidth, cfg.Width)
	}
	if cfg.Width*cfg.Height > panoramaMaxPixels {
		return nil, fmt.Errorf("全景图过大（最大 8192x4096），当前 %dx%d", cfg.Width, cfg.Height)
	}
	img, _, err := image.Decode(bytes.NewReader(data))
	if err != nil {
		return nil, fmt.Errorf("解码全景图失败: %w", err)
	}
	pano := image.NewRGBA(image.Rect(0, 0, img.Bounds().Dx(), img.Bounds().Dy()))
	draw.Draw(pano, pano.Bounds(), img, img.Bounds().Min, draw.Src)
	tw, th := panoramaTileSize(cfg.Width)
	views := make([]panoramaView, 0, count)
	for i := 0; i < count; i++ {
		yaw := float64(i) * 360 / float64(count)
		var buf bytes.Buffer
		if err := png.Encode(&buf, erpToPerspective(pano, tw, th, yaw, panoramaFOVDegrees)); err != nil {
			return nil, err
		}
		views = append(views, panoramaView{Yaw: yaw, PNG: buf.Bytes()})
	}
	return views, nil
}

// panoramaYawLabel names a view direction relative to the panorama centre.
func panoramaYawLabel(yaw float64) string {
	switch {
	case yaw == 0:
		return "正前方"
	case yaw == 180:
		return "正后方"
	case yaw < 180:
		return fmt.Sprintf("向右 %g°", yaw)
	default:
		return fmt.Sprintf("向左 %g°", 360-yaw)
	}
}

// SplitLocationPanorama stores the panorama and registers its views as unselected variants.
func (s *ProjectService) SplitLocationPanorama(asset *models.Asset, data []byte, ext string, count int) ([]models.AssetVariant, error) {
	if asset.Kind != AssetKindLocation {
		return nil, fmt.Errorf("全景拆视角只用于场景资产")
	}
	views, err := splitPanorama(data, count)
	if err != nil {
		return nil, err
	}
	stamp := time.Now().UnixNano()
	panoPath, _, err := s.upload.SaveFile(fmt.Sprint(asset.ProjectID), "image", fmt.Sprintf("pano_%d_%d%s", asset.ID, stamp, ext), data)
	if err != nil {
		return nil, fmt.Errorf("保存全景图失败: %w", err)
	}
	return s.registerPanoramaViews(asset, filepath.Base(panoPath), views, stamp)
}

// registerPanoramaViews saves views of the stored panorama panoFile as unselected variants.
func (s *ProjectService) registerPanoramaViews(asset *models.Asset, panoFile string, views []panoramaView, stamp int64) ([]models.AssetVariant, error) {
	pid := fmt.Sprint(asset.ProjectID)
	variants := NewAssetVariantService(s.db)
	out := make([]models.AssetVariant, 0, len(views))
	for i, view := range views {
		path, _, err := s.upload.SaveFile(pid, "image", fmt.Sprintf("pano_%d_%d_v%d.png", asset.ID, stamp, i+1), view.PNG)
		if err != nil {
			return out, fmt.Errorf("保存第 %d 个视角失败: %w", i+1, err)
		}
		row, err := variants.Register(models.AssetVariant{ProjectID: asset.ProjectID, EntityType: VariantAssetImage, EntityID: asset.ID,
			File: filepath.Base(path), Prompt: fmt.Sprintf("全景视角 %d/%d · %s（水平视场 %g°）", i+1, len(views), panoramaYawLabel(view.Yaw), panoramaFOVDegrees),
			Provenance: fmt.Sprintf("panorama:%s;yaw=%g;fov=%g", panoFile, view.Yaw, panoramaFOVDegrees)})
		if err != nil {
			return out, err
		}
		out = append(out, *row)
	}
	s.pushProject(nil)
	return out, nil
}

func (s *Service) HandleSplitLocationPanorama(c *gin.Context) {
	a, ok := s.loadAsset(c)
	if !ok {
		return
	}
	c.Request.Body = http.MaxBytesReader(c.Writer, c.Request.Body, panoramaMaxUpload+uploadMultipartOverhead)
	count := panoramaDefaultView
	if raw := c.PostForm("view_count"); raw != "" {
		n, err := strconv.Atoi(raw)
		if err != nil {
			c.JSON(http.StatusBadRequest, gin.H{"error": "视角数只能是 4、6 或 8"})
			return
		}
		count = n
	}
	file, _, err := c.Request.FormFile("file")
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "请上传全景图: " + err.Error()})
		return
	}
	defer file.Close()
	data, err := readBoundedUpload(file, panoramaMaxUpload)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	// detectImageExt falls back to .png; a non-image body then fails to decode in splitPanorama.
	ext := detectImageExt(data)
	if ext != ".jpg" && ext != ".png" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "全景图只支持 JPG 或 PNG"})
		return
	}
	rows, err := s.Projects.SplitLocationPanorama(a, data, ext, count)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error(), "variants": rows})
		return
	}
	c.JSON(http.StatusOK, gin.H{"variants": rows, "message": fmt.Sprintf("已拆出 %d 个视角，加入「参考图版本」；当前参考图未改变，请在版本中选用", len(rows))})
}

// Generated panoramas: MiniMax H3-Image renders a 2:1 ERP canvas from the location text alone (no
// reference image, no prompt expander, whose ratio list has no 2:1). The finished panorama is split
// automatically with the default view count. Seams and pole distortion are not checked.
const (
	TemplateH3ImagePanorama = "minimax_h3_image_panorama"
	panoramaGenWidth        = 5632
	panoramaGenHeight       = 2816
)

var errPanoramaActive = errors.New("该场景的全景图正在生成")

// buildPanoramaPrompt writes the approved English ERP framing around the location's own fields.
func buildPanoramaPrompt(p *models.Project, a *models.Asset) string {
	lines := []string{"A seamless 360-degree equirectangular panorama in 2:1 spherical projection, covering 360 degrees horizontally and 180 degrees vertically, of an empty environment with no people. The camera stands at eye level in the middle of the space, and the horizon runs straight across the exact vertical centre of the image. The left and right edges join seamlessly into one continuous scene. Straight lines curve naturally as in an equirectangular projection; the top edge is the sky or ceiling directly overhead and the bottom edge is the ground directly below, each stretched evenly across the full width. Every direction is fully built out: the front, both sides and the area behind the camera all show the same location with consistent lighting, time of day and materials."}
	if desc := styleDescriptor(p.Style); desc != "" {
		lines = append(lines, "Style: "+desc)
	}
	lines = append(lines, "Location: 「"+strings.TrimSpace(a.Name)+"」")
	if d := strings.TrimSpace(a.Description); d != "" {
		lines = append(lines, "Environment appearance, follow exactly: "+d)
	}
	noText := ", no text"
	if text := strings.TrimSpace(a.VisibleText); text != "" {
		lines = append(lines, "The following text must appear exactly as written, legible, unchanged and untranslated, with no other text: \""+strings.ReplaceAll(text, "\"", "\\\"")+"\"")
		noText = ""
	}
	lines = append(lines, "No people, no characters, no figures, no watermark, no captions"+noText+", no collage, no split panels, no borders, no circular fisheye frame.")
	return strings.Join(lines, "\n")
}

// StartLocationPanorama submits a MiniMax H3-Image panorama task for a location asset.
func (s *ProjectService) StartLocationPanorama(a *models.Asset) error {
	if a.Kind != AssetKindLocation {
		return fmt.Errorf("全景图只用于场景资产")
	}
	if s.tasks == nil {
		return fmt.Errorf("全景图生成依赖 ComfyUI 任务服务")
	}
	s.assetMu.Lock()
	defer s.assetMu.Unlock()
	var current models.Asset
	if err := s.db.First(&current, a.ID).Error; err != nil {
		return err
	}
	if s.sheetTaskActive(current.PanoramaTaskID) {
		return errPanoramaActive
	}
	var p models.Project
	if err := s.db.First(&p, current.ProjectID).Error; err != nil {
		return err
	}
	tpl, err := enabledTemplateByCode(s.db, TemplateH3ImagePanorama)
	if err != nil {
		return err
	}
	task, err := s.tasks.CreateTask(CreateTaskReq{TemplateID: tpl.ID, Prompt: buildPanoramaPrompt(&p, &current),
		Params: map[string]any{"width": panoramaGenWidth, "height": panoramaGenHeight}})
	if err != nil {
		return fmt.Errorf("创建全景图任务失败: %w", err)
	}
	if err := s.db.Model(&models.Asset{}).Where("id = ?", current.ID).Updates(map[string]any{
		"panorama_task_id": task.TaskID, "panorama_error": "",
	}).Error; err != nil {
		_ = s.tasks.CancelTask(task.TaskID)
		return err
	}
	go func() {
		if err := s.tasks.Execute(task.TaskID); err != nil && !errors.Is(err, errNoFreeGPU) {
			log.Printf("[asset %d] execute panorama task %s failed: %v", current.ID, task.TaskID, err)
		}
	}()
	s.pushProject(nil)
	return nil
}

// syncLocationPanoramas stores finished panoramas and splits each one exactly once: only the
// caller whose conditional update clears the task id goes on to register views.
func (s *ProjectService) syncLocationPanoramas() bool {
	var assets []models.Asset
	if s.db.Where("kind = ? AND panorama_task_id != ''", AssetKindLocation).Find(&assets).Error != nil {
		return false
	}
	changed := false
	for i := range assets {
		a := &assets[i]
		updates, ok := s.imageTaskUpdates(a.PanoramaTaskID, fmt.Sprintf("pano_%d_%d", a.ID, time.Now().UnixNano()), a.ProjectID,
			"全景图", "panorama_image", "panorama_task_id", "panorama_error")
		if !ok {
			continue
		}
		res := s.db.Model(&models.Asset{}).Where("id = ? AND panorama_task_id = ?", a.ID, a.PanoramaTaskID).Updates(updates)
		if res.Error != nil || res.RowsAffected == 0 {
			continue
		}
		changed = true
		file, _ := updates["panorama_image"].(string)
		if file == "" {
			continue
		}
		if err := s.splitGeneratedPanorama(a, file); err != nil {
			s.db.Model(&models.Asset{}).Where("id = ?", a.ID).Update("panorama_error", "全景图已保存，但自动拆视角失败: "+err.Error())
		}
	}
	return changed
}

func (s *ProjectService) splitGeneratedPanorama(a *models.Asset, file string) error {
	data, err := s.readProjectInput(a.ProjectID, file)
	if err != nil {
		return err
	}
	views, err := splitPanorama(data, panoramaDefaultView)
	if err != nil {
		return err
	}
	_, err = s.registerPanoramaViews(a, file, views, time.Now().UnixNano())
	return err
}

func (s *Service) HandleGenerateLocationPanorama(c *gin.Context) {
	a, ok := s.loadAsset(c)
	if !ok {
		return
	}
	if err := s.Projects.StartLocationPanorama(a); err != nil {
		status := http.StatusBadRequest
		if errors.Is(err, errPanoramaActive) {
			status = http.StatusConflict
		}
		c.JSON(status, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusAccepted, gin.H{"message": fmt.Sprintf("场景「%s」全景图任务已提交；完成后自动拆成 %d 个视角加入「参考图版本」，当前参考图不变", a.Name, panoramaDefaultView)})
}
