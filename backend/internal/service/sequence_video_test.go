package service

import (
	"bytes"
	"encoding/json"
	"fmt"
	"image"
	"image/color"
	"image/png"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"

	"comfyui-console/internal/config"
	"comfyui-console/internal/models"
)

func TestSequenceFrameOrderingRejectsGapsAndShortSequences(t *testing.T) {
	names := []string{
		"clay-seq-20261122-174038-002.png",
		"clay-seq-20261122-174038-000.png",
		"depth-seq-20261122-174038-001.png",
		"clay-seq-20261122-174038-001.png",
		"clay-seq-20261122-174039-003.png",
	}
	ordered, err := orderSequenceFrames(names, "clay", "20261122-174038")
	if err != nil {
		t.Fatal(err)
	}
	want := []string{
		"clay-seq-20261122-174038-000.png",
		"clay-seq-20261122-174038-001.png",
		"clay-seq-20261122-174038-002.png",
	}
	if strings.Join(ordered, ",") != strings.Join(want, ",") {
		t.Fatalf("frames = %v, want %v", ordered, want)
	}

	// 缺第 001 帧必须拒绝：ffmpeg 遇到缺口会静默截断序列。
	gap := []string{"clay-seq-20261122-174038-000.png", "clay-seq-20261122-174038-002.png"}
	if _, err := orderSequenceFrames(gap, "clay", "20261122-174038"); err == nil || !strings.Contains(err.Error(), "缺第 001 帧") {
		t.Fatalf("gap must be rejected, got %v", err)
	}

	one := []string{"clay-seq-20261122-174038-000.png"}
	if _, err := orderSequenceFrames(one, "clay", "20261122-174038"); err == nil || !strings.Contains(err.Error(), "至少需要 2 帧") {
		t.Fatalf("single frame must be rejected, got %v", err)
	}
}

func TestSequenceVideoNamingAndEncodingArgs(t *testing.T) {
	pass, err := normalizeSequencePass(" Depth ")
	if err != nil || pass != "depth" {
		t.Fatalf("pass = %q, err = %v", pass, err)
	}
	if _, err := normalizeSequencePass("exec"); err == nil {
		t.Fatal("unknown pass must be rejected so the name cannot escape the materials dir")
	}
	if got := normalizeSequenceFPS(0); got != sequenceVideoDefaultFPS {
		t.Fatalf("default fps = %d", got)
	}
	if got := normalizeSequenceFPS(240); got != sequenceVideoMaxFPS {
		t.Fatalf("fps clamp = %d", got)
	}
	if got := sequenceVideoName("clay", "20261122-174038"); got != "clay-seq-20261122-174038.mp4" {
		t.Fatalf("video name = %s", got)
	}
	args := strings.Join(sequenceVideoArgs("/tmp/clay-seq-1-%03d.png", 8, "/tmp/out.mp4"), " ")
	// 输入按采样帧率 8 fps 读，输出统一 24 fps：H3 的视频参考帧要求 24 fps。
	for _, want := range []string{"-framerate 8", "-start_number 0", "-c:v libx264", "-pix_fmt yuv420p", "-r 24", "/tmp/out.mp4"} {
		if !strings.Contains(args, want) {
			t.Fatalf("args missing %q: %s", want, args)
		}
	}
}

// TestComposeSequenceVideoHTTPIsolatesProjects 走真实 HTTP：multipart 上传 → 素材ID →
// 按项目 + 精确 ID 合成，不读业务库、不联网，全部落在临时目录。
func TestComposeSequenceVideoHTTPIsolatesProjects(t *testing.T) {
	r, s, upload := newSequenceHTTPRouter(t)
	for _, title := range []string{"A", "B"} {
		if err := s.DB.Create(&models.Project{Title: title}).Error; err != nil {
			t.Fatal(err)
		}
	}
	projectA, projectB := uint(1), uint(2)
	stampA := "20261124-111111-" + strings.Repeat("a", 32)
	stampB := "20261124-111111-" + strings.Repeat("b", 32)
	var idsA, idsB []uint
	for index := 0; index < 2; index++ {
		idsA = append(idsA, uploadFrameHTTP(t, r, projectA, frameName(stampA, index), framePNG(t, index)).ID)
		idsB = append(idsB, uploadFrameHTTP(t, r, projectB, frameName(stampB, index), framePNG(t, index)).ID)
	}
	// 上传时带的项目归属必须落库，后续合成才能按项目隔离。
	for _, probe := range []struct {
		id      uint
		project uint
	}{{idsA[0], projectA}, {idsB[1], projectB}} {
		var stored models.Material
		if err := s.DB.First(&stored, probe.id).Error; err != nil {
			t.Fatal(err)
		}
		if stored.ProjectID == nil || *stored.ProjectID != probe.project {
			t.Fatalf("material %d project = %v, want %d", probe.id, stored.ProjectID, probe.project)
		}
	}

	composeRaw := func(body string) (int, string) {
		rec := httptest.NewRecorder()
		request := httptest.NewRequest(http.MethodPost, "/api/materials/sequence-video", strings.NewReader(body))
		request.Header.Set("Content-Type", "application/json")
		r.ServeHTTP(rec, request)
		return rec.Code, rec.Body.String()
	}
	compose := func(projectID string, ids []uint) (int, string) {
		return composeRaw(fmt.Sprintf(`{"pass":"clay","stamp":%q,"fps":8,"project_id":%s,"frame_ids":[%d,%d]}`,
			stampA, projectID, ids[0], ids[1]))
	}

	// 另一个项目的帧ID不能借本项目名义合成。
	if status, body := compose(strconv.Itoa(int(projectA)), idsB); status != http.StatusBadRequest || !strings.Contains(body, "不属于") {
		t.Fatalf("foreign frame ids: status = %d body = %s", status, body)
	}
	// 无项目作用域只看无项目素材，不能把项目素材拉进来。
	if status, body := compose("null", idsA); status != http.StatusBadRequest || !strings.Contains(body, "不属于当前项目") {
		t.Fatalf("unassigned scope: status = %d body = %s", status, body)
	}
	if status, body := compose("0", idsA); status != http.StatusBadRequest || !strings.Contains(body, "必须为正整数") {
		t.Fatalf("zero project: status = %d body = %s", status, body)
	}
	if status, body := compose("999", idsA); status != http.StatusBadRequest || !strings.Contains(body, "项目不存在") {
		t.Fatalf("missing project: status = %d body = %s", status, body)
	}
	duplicate := fmt.Sprintf(`{"pass":"clay","stamp":%q,"fps":8,"project_id":%d,"frame_ids":[%d,%d]}`,
		stampA, projectA, idsA[0], idsA[0])
	if status, body := composeRaw(duplicate); status != http.StatusBadRequest || !strings.Contains(body, "重复") {
		t.Fatalf("duplicate ids: status = %d body = %s", status, body)
	}

	ffmpeg, err := localFFmpegPath()
	if err != nil {
		t.Logf("ffmpeg 不可用，跳过合成成功路径：%v", err)
		return
	}
	status, body := compose(strconv.Itoa(int(projectA)), idsA)
	if status != http.StatusOK {
		t.Fatalf("compose own frames: status = %d body = %s", status, body)
	}
	var first models.Material
	if err := json.Unmarshal([]byte(body), &first); err != nil {
		t.Fatal(err)
	}
	if first.Type != "video" || !strings.HasPrefix(first.Name, "clay-seq-"+stampA+"-") || !strings.HasSuffix(first.Name, ".mp4") {
		t.Fatalf("compose result = %+v", first)
	}
	// 同一批次再合成一次：结果不可覆盖，前一份文件和记录都还在。
	status, body = compose(strconv.Itoa(int(projectA)), idsA)
	if status != http.StatusOK {
		t.Fatalf("second compose: status = %d body = %s", status, body)
	}
	var second models.Material
	if err := json.Unmarshal([]byte(body), &second); err != nil {
		t.Fatal(err)
	}
	if second.ID == first.ID || second.Path == first.Path || second.Name == first.Name {
		t.Fatalf("second compose overwrote the first: %+v / %+v", first, second)
	}
	var videos int64
	if err := s.DB.Model(&models.Material{}).Where("type = ?", "video").Count(&videos).Error; err != nil {
		t.Fatal(err)
	}
	if videos != 2 {
		t.Fatalf("video rows = %d, want 2", videos)
	}
	outPath := filepath.Join(upload.InputDir(), filepath.FromSlash(first.Path))
	info, err := os.Stat(outPath)
	if err != nil || info.Size() == 0 {
		t.Fatalf("composed mp4 missing: %v", err)
	}
	// 只有本项目的两帧被放进私有临时目录：2 帧 @8fps = 0.25s。
	probe, _ := runLocalProgram(ffmpeg, []string{"-hide_banner", "-i", outPath}, sequenceVideoTimeout)
	if !strings.Contains(probe, "Video: h264") || !strings.Contains(probe, "Duration: 00:00:00.25") {
		t.Fatalf("composed mp4 must contain exactly the two selected frames: %s", probe)
	}
}

func newSequenceHTTPRouter(t *testing.T) (*gin.Engine, *Service, *UploadManager) {
	t.Helper()
	db := newTestDB(t)
	if err := db.AutoMigrate(&models.Material{}, &models.UploadFile{}); err != nil {
		t.Fatal(err)
	}
	root := t.TempDir()
	cfg := &config.Config{
		Comfy:   config.ComfyConfig{ComfyDir: filepath.Join(root, "ComfyUI")},
		Storage: config.StorageConfig{DataDir: filepath.Join(root, "data")},
	}
	remote := NewRemoteExec(config.RemoteConfig{})
	remote.SetLocalRoots([]string{cfg.Comfy.ComfyDir, cfg.Storage.DataDir, cfg.AssetsRoot()})
	upload := NewUploadManager(cfg, db, remote)
	service := &Service{DB: db, Cfg: cfg, Materials: NewMaterialService(cfg, db, remote, upload)}
	gin.SetMode(gin.TestMode)
	engine := gin.New()
	engine.POST("/api/materials", service.HandleUploadMaterial)
	engine.POST("/api/materials/sequence-video", service.HandleComposeSequenceVideo)
	return engine, service, upload
}

func frameName(stamp string, index int) string {
	return fmt.Sprintf("clay-seq-%s-%03d.png", stamp, index)
}

func framePNG(t *testing.T, index int) []byte {
	t.Helper()
	img := image.NewRGBA(image.Rect(0, 0, 64, 36))
	for x := 0; x < 64; x++ {
		img.Set(x, 8+index*6, color.RGBA{R: 255, G: 200, A: 255})
	}
	var buf bytes.Buffer
	if err := png.Encode(&buf, img); err != nil {
		t.Fatal(err)
	}
	return buf.Bytes()
}

func uploadFrameHTTP(t *testing.T, engine http.Handler, projectID uint, name string, data []byte) models.Material {
	t.Helper()
	body := &bytes.Buffer{}
	writer := multipart.NewWriter(body)
	if err := writer.WriteField("type", "image"); err != nil {
		t.Fatal(err)
	}
	if err := writer.WriteField("project_id", strconv.FormatUint(uint64(projectID), 10)); err != nil {
		t.Fatal(err)
	}
	part, err := writer.CreateFormFile("file", name)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := part.Write(data); err != nil {
		t.Fatal(err)
	}
	if err := writer.Close(); err != nil {
		t.Fatal(err)
	}
	rec := httptest.NewRecorder()
	request := httptest.NewRequest(http.MethodPost, "/api/materials", body)
	request.Header.Set("Content-Type", writer.FormDataContentType())
	engine.ServeHTTP(rec, request)
	if rec.Code != http.StatusOK {
		t.Fatalf("upload %s: status = %d body = %s", name, rec.Code, rec.Body.String())
	}
	var material models.Material
	if err := json.Unmarshal(rec.Body.Bytes(), &material); err != nil {
		t.Fatal(err)
	}
	if material.ID == 0 {
		t.Fatalf("upload %s returned no id: %s", name, rec.Body.String())
	}
	return material
}

// TestComposeSequenceVideoEncodesRealMP4 用本机 ffmpeg 真跑一次：合成出来的文件要能被
// ffmpeg 读回、帧数与时长对得上，正好守住「帧号连续 / 码率可解码」这两条。
func TestComposeSequenceVideoEncodesRealMP4(t *testing.T) {
	ffmpeg, err := localFFmpegPath()
	if err != nil {
		t.Skipf("ffmpeg not available: %v", err)
	}
	dir := t.TempDir()
	const frames = 3
	for index := 0; index < frames; index++ {
		img := image.NewRGBA(image.Rect(0, 0, 64, 36))
		for x := 0; x < 64; x++ {
			img.Set(x, 10+index*6, color.RGBA{R: 255, A: 255})
		}
		name := filepath.Join(dir, "clay-seq-20261122-174038-"+fmt.Sprintf("%03d", index)+".png")
		file, err := os.Create(name)
		if err != nil {
			t.Fatal(err)
		}
		if err := png.Encode(file, img); err != nil {
			file.Close()
			t.Fatal(err)
		}
		if err := file.Close(); err != nil {
			t.Fatal(err)
		}
	}
	outPath := filepath.Join(dir, "out.mp4")
	pattern := filepath.Join(dir, "clay-seq-20261122-174038-%03d.png")
	if output, err := runLocalProgram(ffmpeg, sequenceVideoArgs(pattern, 8, outPath), sequenceVideoTimeout); err != nil {
		t.Fatalf("ffmpeg failed: %v %s", err, output)
	}
	info, err := os.Stat(outPath)
	if err != nil || info.Size() == 0 {
		t.Fatalf("mp4 missing: %v", err)
	}
	probe, _ := runLocalProgram(ffmpeg, []string{"-hide_banner", "-i", outPath}, sequenceVideoTimeout)
	if !strings.Contains(probe, "Video: h264") {
		t.Fatalf("mp4 is not h264: %s", probe)
	}
	if !strings.Contains(probe, "24 fps") {
		t.Fatalf("mp4 must be re-timed to 24 fps for H3 reference frames: %s", probe)
	}
	if !strings.Contains(probe, "Duration: 00:00:00.38") {
		t.Fatalf("3 frames at 8 fps must stay 0.375s after the 24 fps re-time: %s", probe)
	}
}