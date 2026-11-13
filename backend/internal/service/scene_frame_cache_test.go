package service

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"comfyui-console/internal/models"
)

// countFramePNGs 统计目录下由「查看全部帧」产生的浏览帧数量。
func countFramePNGs(t *testing.T, dir string) int {
	t.Helper()
	entries, err := os.ReadDir(dir)
	if err != nil {
		if os.IsNotExist(err) {
			return 0
		}
		t.Fatalf("read %s: %v", dir, err)
	}
	count := 0
	for _, entry := range entries {
		if !entry.IsDir() && strings.HasPrefix(entry.Name(), "scene_frames_s") {
			count++
		}
	}
	return count
}

// 浏览帧必须落在独立的缓存目录里，项目素材目录不能被它们污染：用户要能整目录删除，
// 又不会误删素材。重复浏览时上一批必须被清理，否则磁盘会被无限占满。
func TestSceneFrameBrowsingKeepsAssetDirectoryClean(t *testing.T) {
	ffmpeg, err := localFFmpegPath()
	if err != nil {
		t.Skipf("ffmpeg is required: %v", err)
	}
	continuity, db := newContinuityTestService(t)
	// 默认配置的 data/assets 是相对路径，测试不应写进仓库工作区。
	continuity.cfg.Storage.DataDir = t.TempDir()
	continuity.cfg.Storage.AssetDir = ""

	project := models.Project{Title: "frame cache"}
	if err := db.Create(&project).Error; err != nil {
		t.Fatal(err)
	}
	legacyDir := filepath.Join(continuity.cfg.Comfy.ComfyDir, "input", fmt.Sprint(project.ID))
	if err := os.MkdirAll(legacyDir, 0o755); err != nil {
		t.Fatal(err)
	}
	videoName := "cache-check.mp4"
	if output, err := runLocalProgram(ffmpeg, []string{
		"-y", "-f", "lavfi", "-i", "testsrc=size=64x64:rate=24:duration=0.5",
		"-an", "-c:v", "mpeg4", "-pix_fmt", "yuv420p", filepath.Join(legacyDir, videoName),
	}, 30*time.Second); err != nil {
		t.Fatalf("create video fixture: %v\n%s", err, output)
	}
	scene := models.Scene{ProjectID: project.ID, EpisodeN: 1, Generation: 1, Order: 1, Status: "video_ready", VideoTaskID: "cache-task", VideoInputFile: videoName, VideoFile: "keep.mp4", ImageFile: "storyboard.png"}
	if err := db.Create(&scene).Error; err != nil {
		t.Fatal(err)
	}

	assetDir := continuity.sceneFrameDirectory(project.ID)
	cacheDir := continuity.sceneFrameCacheDirectory(project.ID)
	if assetDir == cacheDir {
		t.Fatal("浏览帧缓存目录必须与项目素材目录分开")
	}

	first, err := continuity.ExtractAllSceneFrames(project.ID, scene.ID)
	if err != nil {
		t.Fatal(err)
	}
	if len(first.Frames) == 0 {
		t.Fatal("no frames extracted")
	}
	if first.CacheDir != cacheDir {
		t.Fatalf("cache_dir=%q want=%q（用户需要知道帧放在哪）", first.CacheDir, cacheDir)
	}
	if got := countFramePNGs(t, cacheDir); got != len(first.Frames) {
		t.Fatalf("缓存目录帧数=%d want=%d", got, len(first.Frames))
	}
	if got := countFramePNGs(t, assetDir); got != 0 {
		t.Fatalf("项目素材目录被 %d 张浏览帧污染", got)
	}
	if !strings.Contains(first.Frames[0].URL, sceneFrameCacheCategory) {
		t.Fatalf("帧预览 URL 未指向缓存目录: %s", first.Frames[0].URL)
	}

	// 保存的选帧是用户主动留下的素材，必须进项目素材目录，而不是缓存目录。
	saved, err := continuity.SaveExtractedSceneFrame(project.ID, scene.ID, first.ExtractionID, 3)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(assetDir, saved.File)); err != nil {
		t.Fatalf("saved frame missing from asset dir: %v", err)
	}
	if countFramePNGs(t, assetDir) != 0 {
		t.Fatal("保存选帧后项目素材目录仍不应出现浏览帧")
	}

	// 再次浏览：上一批必须被清理，缓存目录不得累积。
	second, err := continuity.ExtractAllSceneFrames(project.ID, scene.ID)
	if err != nil {
		t.Fatal(err)
	}
	if second.ExtractionID == first.ExtractionID {
		t.Fatal("重新提取应产生新的 extraction_id")
	}
	if got := countFramePNGs(t, cacheDir); got != len(second.Frames) {
		t.Fatalf("重新提取后缓存目录残留 %d 张（本批 %d 张），旧帧未被清理", got, len(second.Frames))
	}
	if second.PurgedFrames != len(first.Frames) {
		t.Fatalf("purged_frames=%d want=%d", second.PurgedFrames, len(first.Frames))
	}
	if _, err := os.Stat(filepath.Join(assetDir, saved.File)); err != nil {
		t.Fatalf("清理浏览帧不得影响已保存的选帧: %v", err)
	}
}
