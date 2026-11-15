package service

import (
	"bytes"
	"encoding/binary"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"image"
	"image/color"
	"image/draw"
	"image/png"
	"io"
	"math"
	"net/http"
	"regexp"
	"strings"
	"time"

	"comfyui-console/internal/models"
	"github.com/gin-gonic/gin"
)

const (
	maxFaceCoverBoxes     = 30
	maxFaceCoverBytes     = 40 << 20
	maxFaceCoverPixels    = 32 << 20
	defaultFaceCoverColor = "#808080"
	// faceCoverReferenceNote follows a covered reference's label in its <Picture N> line. Wording is user-approved.
	faceCoverReferenceNote = "（局部遮脸版：纯色色块只表示此处不提供参考，画面中不出现色块；只参考未遮挡部分的服装、体态、道具与环境，不从此图获取人物长相）"
)

var (
	errFaceCoverBusy          = errors.New("这张参考图正在遮脸或恢复，请等待当前操作完成")
	errFaceCoverSourceChanged = errors.New("参考原图已改变，请重新打开遮脸窗口")
	errFaceCoverNotFound      = errors.New("参考图不存在或不属于当前Scene")
	faceCoverColorPattern     = regexp.MustCompile(`^#[0-9a-fA-F]{6}$`)
)

// FaceCoverBox is a rectangle in normalized 0-1 coordinates of the EXIF-oriented image,
// the same coordinate system a browser preview uses.
type FaceCoverBox struct {
	X float64 `json:"x"`
	Y float64 `json:"y"`
	W float64 `json:"w"`
	H float64 `json:"h"`
}

type FaceCoverView struct {
	Boxes       []FaceCoverBox `json:"boxes"`
	Color       string         `json:"color"`
	SourceFile  string         `json:"source_file"`
	CoveredFile string         `json:"covered_file"`
	UpdatedAt   time.Time      `json:"updated_at"`
}

type FaceCoverSource struct {
	Key          string         `json:"key"`
	Label        string         `json:"label"`
	Image        string         `json:"image"`
	SourceSHA256 string         `json:"source_sha256"`
	Width        int            `json:"width"`
	Height       int            `json:"height"`
	Cover        *FaceCoverView `json:"cover"`
	// Stale means a cover exists but was drawn on an earlier image; it is not applied.
	Stale bool `json:"stale"`
}

// Formal identity references (character, look, outfit) are never covered; only supplementary
// location/prop images and shared materials, which may show unrelated real faces.
func faceCoverAllowed(sourceType string) bool {
	return sourceType == "asset" || sourceType == "shared_material"
}

func faceCoverView(row models.SceneReferenceFaceCover) *FaceCoverView {
	boxes := []FaceCoverBox{}
	_ = json.Unmarshal([]byte(row.BoxesJSON), &boxes)
	return &FaceCoverView{Boxes: boxes, Color: row.Color, SourceFile: row.SourceFile, CoveredFile: row.CoveredFile, UpdatedAt: row.UpdatedAt}
}

func normalizeFaceCoverBoxes(boxes []FaceCoverBox) ([]FaceCoverBox, error) {
	if len(boxes) < 1 || len(boxes) > maxFaceCoverBoxes {
		return nil, fmt.Errorf("请框选 1 至 %d 个需要遮挡的脸部区域", maxFaceCoverBoxes)
	}
	out := make([]FaceCoverBox, 0, len(boxes))
	for _, b := range boxes {
		for _, v := range []float64{b.X, b.Y, b.W, b.H} {
			if math.IsNaN(v) || math.IsInf(v, 0) {
				return nil, fmt.Errorf("遮挡区域必须为有限数字")
			}
		}
		if b.X < 0 || b.Y < 0 || b.X >= 1 || b.Y >= 1 || b.W <= 0 || b.H <= 0 || b.X+b.W > 1+1e-9 || b.Y+b.H > 1+1e-9 {
			return nil, fmt.Errorf("遮挡区域必须位于图片内")
		}
		out = append(out, FaceCoverBox{X: b.X, Y: b.Y, W: math.Min(b.W, 1-b.X), H: math.Min(b.H, 1-b.Y)})
	}
	return out, nil
}

func parseFaceCoverColor(value string) (color.RGBA, string, error) {
	value = strings.TrimSpace(value)
	if value == "" {
		value = defaultFaceCoverColor
	}
	if !faceCoverColorPattern.MatchString(value) {
		return color.RGBA{}, "", fmt.Errorf("遮挡颜色必须为 #RRGGBB 格式")
	}
	raw, _ := hex.DecodeString(value[1:])
	return color.RGBA{R: raw[0], G: raw[1], B: raw[2], A: 255}, strings.ToLower(value), nil
}

// jpegEXIFOrientation returns the EXIF orientation tag (1-8) of a JPEG, or 1 when absent.
// Go's decoder ignores EXIF while browsers apply it, so covers must be drawn on the oriented image.
func jpegEXIFOrientation(data []byte) int {
	if len(data) < 4 || data[0] != 0xFF || data[1] != 0xD8 {
		return 1
	}
	for i := 2; i+4 <= len(data); {
		if data[i] != 0xFF {
			return 1
		}
		marker := data[i+1]
		if marker == 0xD9 || marker == 0xDA {
			return 1
		}
		size := int(binary.BigEndian.Uint16(data[i+2:]))
		if size < 2 || i+2+size > len(data) {
			return 1
		}
		segment := data[i+4 : i+2+size]
		if marker == 0xE1 && len(segment) >= 14 && string(segment[:6]) == "Exif\x00\x00" {
			return tiffOrientation(segment[6:])
		}
		i += 2 + size
	}
	return 1
}

func tiffOrientation(t []byte) int {
	if len(t) < 8 {
		return 1
	}
	var order binary.ByteOrder
	switch string(t[:2]) {
	case "II":
		order = binary.LittleEndian
	case "MM":
		order = binary.BigEndian
	default:
		return 1
	}
	offset := int(order.Uint32(t[4:]))
	if offset < 8 || offset+2 > len(t) {
		return 1
	}
	count := int(order.Uint16(t[offset:]))
	for k := 0; k < count; k++ {
		entry := offset + 2 + 12*k
		if entry+12 > len(t) {
			return 1
		}
		if order.Uint16(t[entry:]) == 0x0112 {
			if v := int(order.Uint16(t[entry+8:])); v >= 1 && v <= 8 {
				return v
			}
			return 1
		}
	}
	return 1
}

// orientImage applies an EXIF orientation to src, whose bounds start at the origin.
func orientImage(src *image.RGBA, orientation int) *image.RGBA {
	if orientation < 2 || orientation > 8 {
		return src
	}
	w, h := src.Rect.Dx(), src.Rect.Dy()
	dw, dh := w, h
	if orientation >= 5 {
		dw, dh = h, w
	}
	dst := image.NewRGBA(image.Rect(0, 0, dw, dh))
	for y := 0; y < dh; y++ {
		for x := 0; x < dw; x++ {
			var sx, sy int
			switch orientation {
			case 2:
				sx, sy = w-1-x, y
			case 3:
				sx, sy = w-1-x, h-1-y
			case 4:
				sx, sy = x, h-1-y
			case 5:
				sx, sy = y, x
			case 6:
				sx, sy = y, h-1-x
			case 7:
				sx, sy = w-1-y, h-1-x
			case 8:
				sx, sy = w-1-y, x
			}
			d, s := dst.PixOffset(x, y), src.PixOffset(sx, sy)
			copy(dst.Pix[d:d+4], src.Pix[s:s+4])
		}
	}
	return dst
}

// faceCoverDimensions returns the oriented size without decoding pixels.
func faceCoverDimensions(data []byte) (int, int, string, error) {
	cfg, format, err := image.DecodeConfig(bytes.NewReader(data))
	if err != nil {
		return 0, 0, "", fmt.Errorf("参考图损坏或格式不受支持（支持 PNG、JPEG、WebP），原图未修改")
	}
	if cfg.Width <= 0 || cfg.Height <= 0 || cfg.Width*cfg.Height > maxFaceCoverPixels {
		return 0, 0, "", fmt.Errorf("参考图像素过大，遮脸仅支持 3200 万像素以内的图片")
	}
	w, h := cfg.Width, cfg.Height
	if format == "jpeg" && jpegEXIFOrientation(data) >= 5 {
		w, h = h, w
	}
	return w, h, format, nil
}

func decodeOrientedImage(data []byte) (*image.RGBA, error) {
	_, _, format, err := faceCoverDimensions(data)
	if err != nil {
		return nil, err
	}
	img, _, err := image.Decode(bytes.NewReader(data))
	if err != nil {
		return nil, fmt.Errorf("参考图损坏或格式不受支持，原图未修改")
	}
	b := img.Bounds()
	rgba := image.NewRGBA(image.Rect(0, 0, b.Dx(), b.Dy()))
	draw.Draw(rgba, rgba.Bounds(), img, b.Min, draw.Src)
	if format == "jpeg" {
		return orientImage(rgba, jpegEXIFOrientation(data)), nil
	}
	return rgba, nil
}

// renderFaceCover paints opaque solid rectangles; it is purely local and never calls an image model.
func renderFaceCover(img *image.RGBA, boxes []FaceCoverBox, fill color.RGBA) ([]byte, error) {
	w, h := img.Rect.Dx(), img.Rect.Dy()
	for _, b := range boxes {
		left := min(w-1, int(math.Floor(b.X*float64(w)+1e-9)))
		top := min(h-1, int(math.Floor(b.Y*float64(h)+1e-9)))
		right := min(w, max(left+1, int(math.Ceil((b.X+b.W)*float64(w)-1e-9))))
		bottom := min(h, max(top+1, int(math.Ceil((b.Y+b.H)*float64(h)-1e-9))))
		draw.Draw(img, image.Rect(left, top, right, bottom), &image.Uniform{C: fill}, image.Point{}, draw.Src)
	}
	var buf bytes.Buffer
	if err := png.Encode(&buf, img); err != nil {
		return nil, err
	}
	return buf.Bytes(), nil
}

func (s *ProjectService) claimFaceCover(sceneID uint, key string) (func(), error) {
	s.faceCoverMu.Lock()
	defer s.faceCoverMu.Unlock()
	if s.faceCoverBusy == nil {
		s.faceCoverBusy = map[string]bool{}
	}
	claim := fmt.Sprintf("%d|%s", sceneID, key)
	if s.faceCoverBusy[claim] {
		return nil, errFaceCoverBusy
	}
	s.faceCoverBusy[claim] = true
	return func() {
		s.faceCoverMu.Lock()
		delete(s.faceCoverBusy, claim)
		s.faceCoverMu.Unlock()
	}, nil
}

func (s *ProjectService) faceCoverCandidate(sc *models.Scene, key string) (SceneReferenceCandidate, error) {
	for _, c := range s.sceneReferenceCandidates(sc) {
		if c.Key != key {
			continue
		}
		if !faceCoverAllowed(c.SourceType) {
			return c, fmt.Errorf("人物、造型与新形象是正式身份参考图，不做遮脸；遮脸只用于场景、道具和共享素材等补充参考图")
		}
		return c, nil
	}
	return SceneReferenceCandidate{}, errFaceCoverNotFound
}

func candidateFileMeta(projectID uint, c SceneReferenceCandidate) FileMeta {
	if c.SourceType == "shared_material" {
		return fileMetaForSharedMaterial(projectID, c.Image)
	}
	return FileMeta{TaskID: fmt.Sprint(projectID), Name: c.Image}
}

func (s *ProjectService) readFaceCoverSource(meta FileMeta) ([]byte, error) {
	if s.upload == nil || s.upload.remote == nil {
		return nil, fmt.Errorf("文件服务未配置")
	}
	f, err := s.upload.remote.Open(s.upload.ResolveReadPath(meta.TaskID, meta.Name))
	if err != nil {
		return nil, fmt.Errorf("读取参考原图失败: %w", err)
	}
	defer f.Close()
	data, err := io.ReadAll(io.LimitReader(f, maxFaceCoverBytes+1))
	if err != nil {
		return nil, fmt.Errorf("读取参考原图失败: %w", err)
	}
	if len(data) > maxFaceCoverBytes {
		return nil, fmt.Errorf("参考图超过 40 MB，无法遮脸")
	}
	return data, nil
}

func (s *ProjectService) sceneFaceCover(sceneID uint, key string) (*models.SceneReferenceFaceCover, error) {
	var row models.SceneReferenceFaceCover
	result := s.db.Where("scene_id = ? AND reference_key = ?", sceneID, key).Limit(1).Find(&row)
	if result.Error != nil || result.RowsAffected == 0 {
		return nil, result.Error
	}
	return &row, nil
}

// attachFaceCovers marks candidates whose current image has a cover for this Scene.
// A cover drawn on an earlier image is ignored, so a replaced source is never hidden by an old cover.
func (s *ProjectService) attachFaceCovers(sc *models.Scene, candidates []SceneReferenceCandidate) {
	if !s.db.Migrator().HasTable(&models.SceneReferenceFaceCover{}) {
		return
	}
	var covers []models.SceneReferenceFaceCover
	if s.db.Where("scene_id = ?", sc.ID).Find(&covers).Error != nil || len(covers) == 0 {
		return
	}
	byKey := make(map[string]models.SceneReferenceFaceCover, len(covers))
	for _, cover := range covers {
		byKey[cover.ReferenceKey] = cover
	}
	for i := range candidates {
		cover, ok := byKey[candidates[i].Key]
		if ok && faceCoverAllowed(candidates[i].SourceType) && cover.SourceFile == candidates[i].Image {
			candidates[i].CoveredImage = cover.CoveredFile
		}
	}
}

// FaceCoverSource describes the image a cover would be drawn on, with its hash for the later save.
func (s *ProjectService) FaceCoverSource(sc *models.Scene, key string) (*FaceCoverSource, error) {
	c, err := s.faceCoverCandidate(sc, key)
	if err != nil {
		return nil, err
	}
	data, err := s.readFaceCoverSource(candidateFileMeta(sc.ProjectID, c))
	if err != nil {
		return nil, err
	}
	w, h, _, err := faceCoverDimensions(data)
	if err != nil {
		return nil, err
	}
	out := &FaceCoverSource{Key: key, Label: c.Label, Image: c.Image, SourceSHA256: sha256Hex(data), Width: w, Height: h}
	if s.db.Migrator().HasTable(&models.SceneReferenceFaceCover{}) {
		row, err := s.sceneFaceCover(sc.ID, key)
		if err != nil {
			return nil, err
		}
		if row != nil {
			out.Cover = faceCoverView(*row)
			out.Stale = row.SourceFile != c.Image
		}
	}
	return out, nil
}

// SaveFaceCover renders a covered copy of one supplementary reference for this Scene. The source
// file is only read: its hash must match the previewed one both before and after rendering.
func (s *ProjectService) SaveFaceCover(sc *models.Scene, key, sourceSHA256 string, boxes []FaceCoverBox, colorValue string) (*models.SceneReferenceFaceCover, error) {
	boxes, err := normalizeFaceCoverBoxes(boxes)
	if err != nil {
		return nil, err
	}
	fill, colorValue, err := parseFaceCoverColor(colorValue)
	if err != nil {
		return nil, err
	}
	release, err := s.claimFaceCover(sc.ID, key)
	if err != nil {
		return nil, err
	}
	defer release()
	c, err := s.faceCoverCandidate(sc, key)
	if err != nil {
		return nil, err
	}
	meta := candidateFileMeta(sc.ProjectID, c)
	data, err := s.readFaceCoverSource(meta)
	if err != nil {
		return nil, err
	}
	if sha256Hex(data) != strings.TrimSpace(sourceSHA256) {
		return nil, errFaceCoverSourceChanged
	}
	img, err := decodeOrientedImage(data)
	if err != nil {
		return nil, err
	}
	covered, err := renderFaceCover(img, boxes, fill)
	if err != nil {
		return nil, fmt.Errorf("生成遮脸图失败: %w", err)
	}
	// The source may have been replaced while rendering; never publish a cover of an older image.
	again, err := s.readFaceCoverSource(meta)
	if err != nil {
		return nil, err
	}
	if sha256Hex(again) != sha256Hex(data) {
		return nil, errFaceCoverSourceChanged
	}
	name := fmt.Sprintf("face_cover_s%d_%d.png", sc.ID, time.Now().UnixNano())
	if _, _, err := s.upload.SaveFile(fmt.Sprint(sc.ProjectID), "image", name, covered); err != nil {
		return nil, fmt.Errorf("保存遮脸图失败: %w", err)
	}
	boxesJSON, _ := json.Marshal(boxes)
	row, err := s.sceneFaceCover(sc.ID, key)
	if err != nil {
		return nil, err
	}
	if row == nil {
		row = &models.SceneReferenceFaceCover{ProjectID: sc.ProjectID, SceneID: sc.ID, ReferenceKey: key}
	}
	// A replaced covered file stays on disk: earlier task snapshots may still reference it.
	row.SourceFile, row.SourceSHA256, row.CoveredFile, row.BoxesJSON, row.Color = c.Image, sha256Hex(data), name, string(boxesJSON), colorValue
	if err := s.db.Save(row).Error; err != nil {
		return nil, err
	}
	if err := s.faceCoverChanged(sc.ID, key, "场景参考图遮脸已更新"); err != nil {
		return nil, err
	}
	return row, nil
}

// RestoreFaceCover removes this Scene's cover so the untouched original is submitted again.
func (s *ProjectService) RestoreFaceCover(sc *models.Scene, key string) error {
	release, err := s.claimFaceCover(sc.ID, key)
	if err != nil {
		return err
	}
	defer release()
	row, err := s.sceneFaceCover(sc.ID, key)
	if err != nil {
		return err
	}
	if row == nil {
		return fmt.Errorf("此参考图没有遮脸记录，无需恢复")
	}
	if err := s.db.Delete(row).Error; err != nil {
		return err
	}
	return s.faceCoverChanged(sc.ID, key, "场景参考图已恢复原图")
}

// faceCoverChanged applies the same stale/cancel handling as editing the reference list, but only
// when the covered reference is actually selected; otherwise nothing that was submitted changes.
func (s *ProjectService) faceCoverChanged(sceneID uint, key, reason string) error {
	var current models.Scene
	if err := s.db.First(&current, sceneID).Error; err != nil {
		return err
	}
	selected, explicit := parseSceneReferences(&current)
	if !explicit {
		return nil
	}
	inUse := false
	for _, r := range s.normalizeSceneReferences(current.ProjectID, selected) {
		if referenceKey(r) == key && (r.UseKrea2 || r.UseH3) {
			inUse = true
		}
	}
	if !inUse {
		return nil
	}
	updates := map[string]any{}
	if strings.TrimSpace(current.VideoFullPrompt) != "" {
		updates["prompt_stale"] = true
	}
	cancelTasks := staleSceneTaskUpdates(s.db, &current, updates)
	if len(updates) > 0 {
		if err := s.db.Model(&current).Updates(updates).Error; err != nil {
			return err
		}
	}
	cancelSceneTasks(s.tasks, cancelTasks)
	return markSceneMediaSourceChanged(s.db, current.ProjectID, current.ID, "", reason)
}

func faceCoverStatus(err error) int {
	switch {
	case errors.Is(err, errFaceCoverBusy), errors.Is(err, errFaceCoverSourceChanged):
		return http.StatusConflict
	case errors.Is(err, errFaceCoverNotFound):
		return http.StatusNotFound
	}
	return http.StatusBadRequest
}

func (s *Service) HandleGetSceneReferenceFaceCover(c *gin.Context) {
	sc, ok := s.loadScene(c)
	if !ok {
		return
	}
	source, err := s.Projects.FaceCoverSource(sc, strings.TrimSpace(c.Query("key")))
	if err != nil {
		c.JSON(faceCoverStatus(err), gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, source)
}

func (s *Service) HandleSaveSceneReferenceFaceCover(c *gin.Context) {
	sc, ok := s.loadScene(c)
	if !ok {
		return
	}
	var req struct {
		Key          string         `json:"key"`
		SourceSHA256 string         `json:"source_sha256"`
		Boxes        []FaceCoverBox `json:"boxes"`
		Color        string         `json:"color"`
	}
	if c.ShouldBindJSON(&req) != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "参数错误"})
		return
	}
	row, err := s.Projects.SaveFaceCover(sc, strings.TrimSpace(req.Key), req.SourceSHA256, req.Boxes, req.Color)
	if err != nil {
		c.JSON(faceCoverStatus(err), gin.H{"error": err.Error()})
		return
	}
	s.Projects.PushProject(nil)
	c.JSON(http.StatusOK, gin.H{"cover": faceCoverView(*row), "message": "已保存遮脸版参考图，原图保持不变"})
}

func (s *Service) HandleRestoreSceneReferenceFaceCover(c *gin.Context) {
	sc, ok := s.loadScene(c)
	if !ok {
		return
	}
	if err := s.Projects.RestoreFaceCover(sc, strings.TrimSpace(c.Query("key"))); err != nil {
		c.JSON(faceCoverStatus(err), gin.H{"error": err.Error()})
		return
	}
	s.Projects.PushProject(nil)
	c.JSON(http.StatusOK, gin.H{"ok": true, "message": "已恢复原图"})
}
