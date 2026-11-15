package service

import (
	"bytes"
	"encoding/binary"
	"encoding/json"
	"errors"
	"fmt"
	"image"
	"image/color"
	"image/jpeg"
	"image/png"
	"math"
	"path/filepath"
	"strings"
	"testing"

	"comfyui-console/internal/config"
	"comfyui-console/internal/models"
)

type faceCoverFixture struct {
	ps       *ProjectService
	upload   *UploadManager
	scene    models.Scene
	location models.Asset
	key      string
	original []byte
}

func colorPNG(t *testing.T, w, h int, c color.RGBA) []byte {
	t.Helper()
	img := image.NewRGBA(image.Rect(0, 0, w, h))
	for i := 0; i < len(img.Pix); i += 4 {
		img.Pix[i], img.Pix[i+1], img.Pix[i+2], img.Pix[i+3] = c.R, c.G, c.B, c.A
	}
	var buf bytes.Buffer
	if err := png.Encode(&buf, img); err != nil {
		t.Fatal(err)
	}
	return buf.Bytes()
}

func newFaceCoverFixture(t *testing.T) faceCoverFixture {
	t.Helper()
	ps := newTestProjectService(t)
	if err := ps.db.AutoMigrate(&models.UploadFile{}, &models.SceneReferenceFaceCover{}, &models.SharedAssetReference{}, &models.Material{}); err != nil {
		t.Fatal(err)
	}
	root := t.TempDir()
	cfg := &config.Config{Comfy: config.ComfyConfig{ComfyDir: filepath.Join(root, "ComfyUI")}, Storage: config.StorageConfig{DataDir: filepath.Join(root, "data")}}
	remote := NewRemoteExec(config.RemoteConfig{})
	remote.SetLocalRoots([]string{cfg.Comfy.ComfyDir, cfg.Storage.DataDir, cfg.AssetsRoot()})
	upload := NewUploadManager(cfg, ps.db, remote)
	ps.upload = upload
	p := models.Project{Title: "遮脸"}
	ps.db.Create(&p)
	original := colorPNG(t, 40, 20, color.RGBA{R: 200, G: 30, B: 30, A: 255})
	if _, _, err := upload.SaveFile(fmt.Sprint(p.ID), "image", "street.png", original); err != nil {
		t.Fatal(err)
	}
	if _, _, err := upload.SaveFile(fmt.Sprint(p.ID), "image", "hero.png", colorPNG(t, 8, 8, color.RGBA{A: 255})); err != nil {
		t.Fatal(err)
	}
	location := models.Asset{ProjectID: p.ID, Kind: AssetKindLocation, Name: "长街", Image: "street.png"}
	ps.db.Create(&location)
	ps.db.Create(&models.Character{ProjectID: p.ID, Name: "柳石", Portrait: "hero.png"})
	key := fmt.Sprintf("asset:%d:image", location.ID)
	refs, _ := json.Marshal([]SceneReferenceSelection{{SourceType: "asset", SourceID: location.ID, Variant: "image", UseKrea2: true, UseH3: true}})
	scene := models.Scene{ProjectID: p.ID, EpisodeN: 1, Generation: 1, Order: 1, ReferenceImagesJSON: string(refs), VideoFullPrompt: "reviewed", Status: "video_ready"}
	ps.db.Create(&scene)
	return faceCoverFixture{ps: ps, upload: upload, scene: scene, location: location, key: key, original: original}
}

func (f faceCoverFixture) read(t *testing.T, name string) []byte {
	t.Helper()
	data, err := f.ps.readFaceCoverSource(FileMeta{TaskID: fmt.Sprint(f.scene.ProjectID), Name: name})
	if err != nil {
		t.Fatal(err)
	}
	return data
}

func TestFaceCoverBoxesAreNormalizedAndBounded(t *testing.T) {
	if _, err := normalizeFaceCoverBoxes(nil); err == nil {
		t.Fatal("empty box list must be rejected")
	}
	if _, err := normalizeFaceCoverBoxes(make([]FaceCoverBox, maxFaceCoverBoxes+1)); err == nil {
		t.Fatal("more than 30 boxes must be rejected")
	}
	for _, bad := range []FaceCoverBox{{X: -0.1, Y: 0, W: 0.2, H: 0.2}, {X: 0.9, Y: 0, W: 0.2, H: 0.2}, {X: 0, Y: 0, W: 0, H: 0.2}, {X: math.NaN(), Y: 0, W: 0.1, H: 0.1}, {X: 1, Y: 0, W: 0.1, H: 0.1}} {
		if _, err := normalizeFaceCoverBoxes([]FaceCoverBox{bad}); err == nil {
			t.Fatalf("box %+v must be rejected", bad)
		}
	}
	got, err := normalizeFaceCoverBoxes([]FaceCoverBox{{X: 0.5, Y: 0.5, W: 0.5, H: 0.5}})
	if err != nil || len(got) != 1 {
		t.Fatalf("edge-touching box rejected: %v", err)
	}
	if _, _, err := parseFaceCoverColor("red"); err == nil {
		t.Fatal("non #RRGGBB color must be rejected")
	}
}

// jpegWithOrientation inserts an EXIF APP1 segment carrying the given orientation after SOI.
func jpegWithOrientation(t *testing.T, w, h, orientation int) []byte {
	t.Helper()
	var buf bytes.Buffer
	if err := jpeg.Encode(&buf, image.NewRGBA(image.Rect(0, 0, w, h)), nil); err != nil {
		t.Fatal(err)
	}
	tiff := []byte("MM\x00\x2a\x00\x00\x00\x08\x00\x01")
	entry := make([]byte, 12)
	binary.BigEndian.PutUint16(entry[0:], 0x0112)
	binary.BigEndian.PutUint16(entry[2:], 3)
	binary.BigEndian.PutUint32(entry[4:], 1)
	binary.BigEndian.PutUint16(entry[8:], uint16(orientation))
	payload := append(append(append([]byte("Exif\x00\x00"), tiff...), entry...), 0, 0, 0, 0)
	segment := []byte{0xFF, 0xE1, 0, 0}
	binary.BigEndian.PutUint16(segment[2:], uint16(len(payload)+2))
	data := buf.Bytes()
	return append(append(append([]byte{}, data[:2]...), append(segment, payload...)...), data[2:]...)
}

func TestFaceCoverUsesEXIFOrientedCoordinates(t *testing.T) {
	data := jpegWithOrientation(t, 40, 20, 6)
	if got := jpegEXIFOrientation(data); got != 6 {
		t.Fatalf("orientation=%d", got)
	}
	w, h, _, err := faceCoverDimensions(data)
	if err != nil || w != 20 || h != 40 {
		t.Fatalf("oriented size=%dx%d err=%v, want 20x40", w, h, err)
	}
	img, err := decodeOrientedImage(data)
	if err != nil || img.Rect.Dx() != 20 || img.Rect.Dy() != 40 {
		t.Fatalf("decoded oriented image %v err=%v", img.Rect, err)
	}
	// Orientation 6 is a 90° clockwise turn: the source top-left lands at the top-right.
	src := image.NewRGBA(image.Rect(0, 0, 3, 2))
	src.Set(0, 0, color.RGBA{R: 255, A: 255})
	dst := orientImage(src, 6)
	if dst.Rect.Dx() != 2 || dst.Rect.Dy() != 3 || dst.RGBAAt(1, 0).R != 255 {
		t.Fatalf("orientation 6 mapping wrong: rect=%v", dst.Rect)
	}
}

func TestSaveFaceCoverDerivesImageAndRestoreReturnsOriginal(t *testing.T) {
	f := newFaceCoverFixture(t)
	source, err := f.ps.FaceCoverSource(&f.scene, f.key)
	if err != nil {
		t.Fatal(err)
	}
	if source.Width != 40 || source.Height != 20 || source.Cover != nil || source.SourceSHA256 != sha256Hex(f.original) {
		t.Fatalf("source=%+v", source)
	}
	row, err := f.ps.SaveFaceCover(&f.scene, f.key, source.SourceSHA256, []FaceCoverBox{{X: 0, Y: 0, W: 0.25, H: 0.5}}, "#00ff00")
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(f.read(t, "street.png"), f.original) {
		t.Fatal("original reference image was modified")
	}
	covered, err := png.Decode(bytes.NewReader(f.read(t, row.CoveredFile)))
	if err != nil {
		t.Fatal(err)
	}
	if r, g, b, _ := covered.At(5, 5).RGBA(); r>>8 != 0 || g>>8 != 255 || b>>8 != 0 {
		t.Fatalf("covered pixel=(%d,%d,%d), want solid green", r>>8, g>>8, b>>8)
	}
	if r, g, _, _ := covered.At(30, 15).RGBA(); r>>8 != 200 || g>>8 != 30 {
		t.Fatal("pixels outside the box changed")
	}
	var scene models.Scene
	f.ps.db.First(&scene, f.scene.ID)
	if !scene.PromptStale {
		t.Fatal("covering a selected reference must mark the reviewed prompt stale")
	}
	files, lines, _ := f.ps.selectedSceneReferenceFiles(&scene, "h3")
	if len(files) != 1 || files[0].Name != row.CoveredFile || !strings.Contains(lines[0], faceCoverReferenceNote) {
		t.Fatalf("submission must use the covered copy: files=%+v lines=%v", files, lines)
	}

	if err := f.ps.RestoreFaceCover(&scene, f.key); err != nil {
		t.Fatal(err)
	}
	files, lines, _ = f.ps.selectedSceneReferenceFiles(&scene, "h3")
	if files[0].Name != "street.png" || strings.Contains(lines[0], "遮脸") {
		t.Fatalf("restore must submit the original again: files=%+v lines=%v", files, lines)
	}
	if err := f.ps.RestoreFaceCover(&scene, f.key); err == nil {
		t.Fatal("restoring twice must report that there is no cover")
	}
}

func TestSaveFaceCoverRejectsChangedSourceAndConcurrentWork(t *testing.T) {
	f := newFaceCoverFixture(t)
	boxes := []FaceCoverBox{{X: 0.1, Y: 0.1, W: 0.2, H: 0.2}}
	_, err := f.ps.SaveFaceCover(&f.scene, f.key, strings.Repeat("0", 64), boxes, "")
	if !errors.Is(err, errFaceCoverSourceChanged) || faceCoverStatus(err) != 409 {
		t.Fatalf("stale preview hash err=%v", err)
	}
	release, err := f.ps.claimFaceCover(f.scene.ID, f.key)
	if err != nil {
		t.Fatal(err)
	}
	_, err = f.ps.SaveFaceCover(&f.scene, f.key, sha256Hex(f.original), boxes, "")
	if !errors.Is(err, errFaceCoverBusy) || faceCoverStatus(err) != 409 {
		t.Fatalf("concurrent cover err=%v", err)
	}
	release()
	var count int64
	f.ps.db.Model(&models.SceneReferenceFaceCover{}).Count(&count)
	if count != 0 {
		t.Fatalf("rejected saves created %d cover rows", count)
	}
}

func TestFaceCoverRefusesIdentityReferencesAndIgnoresReplacedSource(t *testing.T) {
	f := newFaceCoverFixture(t)
	var hero models.Character
	f.ps.db.Where("name = ?", "柳石").First(&hero)
	if _, err := f.ps.FaceCoverSource(&f.scene, fmt.Sprintf("character:%d:portrait", hero.ID)); err == nil || !strings.Contains(err.Error(), "正式身份参考图") {
		t.Fatalf("character portrait must not be coverable: %v", err)
	}
	if _, err := f.ps.SaveFaceCover(&f.scene, fmt.Sprintf("character:%d:portrait", hero.ID), sha256Hex(f.original), []FaceCoverBox{{X: 0, Y: 0, W: 0.5, H: 0.5}}, ""); err == nil {
		t.Fatal("character portrait cover must be refused")
	}
	if _, err := f.ps.SaveFaceCover(&f.scene, f.key, sha256Hex(f.original), []FaceCoverBox{{X: 0, Y: 0, W: 0.5, H: 0.5}}, ""); err != nil {
		t.Fatal(err)
	}
	if _, _, err := f.upload.SaveFile(fmt.Sprint(f.scene.ProjectID), "image", "street2.png", colorPNG(t, 40, 20, color.RGBA{B: 255, A: 255})); err != nil {
		t.Fatal(err)
	}
	f.ps.db.Model(&f.location).Update("image", "street2.png")
	files, lines, _ := f.ps.selectedSceneReferenceFiles(&f.scene, "h3")
	if files[0].Name != "street2.png" || strings.Contains(lines[0], "遮脸") {
		t.Fatalf("a cover of the replaced image must not hide the new one: files=%+v", files)
	}
	source, err := f.ps.FaceCoverSource(&f.scene, f.key)
	if err != nil || source.Cover == nil || !source.Stale {
		t.Fatalf("replaced source must report the old cover as stale: %+v err=%v", source, err)
	}
}
