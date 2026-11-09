package service

import (
	"fmt"
	"math"
	"os"
	"path/filepath"
	"testing"
	"time"

	"comfyui-console/internal/config"
	"comfyui-console/internal/models"
)

func TestRunAudioMergeEditedClipsActualPipeline(t *testing.T) {
	ffmpeg, err := localFFmpegPath()
	if err != nil {
		t.Skipf("actual merge pipeline requires a locally installed ffmpeg: %v", err)
	}

	const fixtureTimeout = 45 * time.Second
	root := t.TempDir()
	cfg := &config.Config{
		Comfy: config.ComfyConfig{ComfyDir: filepath.Join(root, "ComfyUI")},
		Storage: config.StorageConfig{
			DataDir:  filepath.Join(root, "data"),
			AssetDir: filepath.Join(root, "project-assets"),
		},
	}
	db := safetyDB(t, &models.MergeTask{}, &models.Material{}, &models.ScreenTextCue{})
	if err := db.AutoMigrate(&models.Task{}, &models.UploadFile{}); err != nil {
		t.Fatal(err)
	}
	remote := NewRemoteExec(config.RemoteConfig{})
	remote.SetLocalRoots([]string{cfg.Comfy.ComfyDir, cfg.Storage.DataDir, cfg.AssetsRoot()})
	upload := NewUploadManager(cfg, db, remote)
	materials := NewMaterialService(cfg, db, remote, upload)
	svc := NewProjectService(cfg, db, nil, nil, nil, remote, upload, nil, materials)

	project := models.Project{Title: "actual edited merge", Generation: 1}
	if err := db.Create(&project).Error; err != nil {
		t.Fatal(err)
	}

	makeAV := func(name, color, size, tone string) ([]byte, [32]byte) {
		t.Helper()
		path := filepath.Join(root, name)
		log, runErr := runLocalProgram(ffmpeg, []string{
			"-y", "-f", "lavfi", "-i", "color=c=" + color + ":s=" + size + ":r=24:d=4",
			"-f", "lavfi", "-i", "sine=frequency=" + tone + ":sample_rate=48000:duration=4",
			"-shortest", "-c:v", "libx264", "-threads", "1", "-pix_fmt", "yuv420p",
			"-c:a", "aac", "-sample_fmt", "fltp", path,
		}, fixtureTimeout)
		if runErr != nil {
			t.Fatalf("create fixture %s: %v\n%s", name, runErr, log)
		}
		data, readErr := os.ReadFile(path)
		if readErr != nil {
			t.Fatal(readErr)
		}
		return data, mergeFixtureSHA256(t, path)
	}
	firstData, _ := makeAV("first.mp4", "red", "160x96", "440")
	secondData, _ := makeAV("second.mp4", "blue", "96x160", "880")
	firstRel, _, err := upload.SaveFile(fmt.Sprint(project.ID), "video", "first.mp4", firstData)
	if err != nil {
		t.Fatal(err)
	}
	secondRel, _, err := upload.SaveFile(fmt.Sprint(project.ID), "video", "second.mp4", secondData)
	if err != nil {
		t.Fatal(err)
	}
	firstInput := filepath.Join(cfg.AssetsRoot(), filepath.FromSlash(firstRel))
	secondInput := filepath.Join(cfg.AssetsRoot(), filepath.FromSlash(secondRel))
	firstHash, secondHash := mergeFixtureSHA256(t, firstInput), mergeFixtureSHA256(t, secondInput)

	overlayFixture := filepath.Join(root, "overlay.png")
	if log, runErr := runLocalProgram(ffmpeg, []string{
		"-y", "-f", "lavfi", "-i", "color=c=green:s=48x48", "-frames:v", "1", "-threads", "1", overlayFixture,
	}, fixtureTimeout); runErr != nil {
		t.Fatalf("create overlay fixture: %v\n%s", runErr, log)
	}
	overlayData, err := os.ReadFile(overlayFixture)
	if err != nil {
		t.Fatal(err)
	}
	overlay, err := materials.SaveUpload(&project.ID, "image", "overlay.png", overlayData)
	if err != nil {
		t.Fatal(err)
	}
	overlayInput := filepath.Join(cfg.AssetsRoot(), filepath.FromSlash(overlay.Path))
	overlayHash := mergeFixtureSHA256(t, overlayInput)

	first := readyMergeScene(project.ID, 1, 1, 1, "scene-one", "unused-first.mp4")
	first.VideoInputFile = "first.mp4"
	first.Duration = 4
	second := readyMergeScene(project.ID, 1, 2, 1, "scene-two", "unused-second.mp4")
	second.VideoInputFile = "second.mp4"
	second.Duration = 4
	if err := db.Create(&first).Error; err != nil {
		t.Fatal(err)
	}
	if err := db.Create(&second).Error; err != nil {
		t.Fatal(err)
	}

	volume := .8
	clips := []mergeEditClip{
		{ID: "fast", SceneID: &first.ID, Speed: 2, Volume: &volume, Transition: "fade", TransitionDuration: .5,
			OverlayMaterialID: &overlay.ID, OverlayX: .05, OverlayY: .05, OverlayWidth: .3, OverlayOpacity: .8},
		{ID: "normal", SceneID: &second.ID, Volume: &volume},
	}
	mt, scenes, err := svc.createMergeTaskRecord(&project, []uint{first.ID, second.ID}, true, false,
		MergeAudioOptions{NativeVolume: 1, DialogueVolume: 1, BGMVolume: 1}, mergeTaskSequence{Clips: clips, EpisodeN: 1})
	if err != nil {
		t.Fatal(err)
	}
	originalSnapshot := mt.InputSnapshot
	if err := svc.validateMergeExecution(&project, mt, scenes); err != nil {
		t.Fatalf("validate actual merge execution: %v", err)
	}

	svc.runAudioMerge(&project, mt, scenes, true, false)

	var completed models.MergeTask
	if err := db.First(&completed, mt.ID).Error; err != nil {
		t.Fatal(err)
	}
	if completed.Status != "success" || completed.OutputFile == "" || completed.Error != "" {
		t.Fatalf("merge task did not succeed: status=%q output=%q error=%q", completed.Status, completed.OutputFile, completed.Error)
	}
	if completed.InputSnapshot != originalSnapshot {
		t.Fatal("immutable merge input snapshot changed before completion")
	}
	output := filepath.Join(cfg.Comfy.ComfyDir, "output_workers", "gpu0", filepath.FromSlash(completed.OutputFile))
	if _, err := os.Stat(output); err != nil {
		t.Fatalf("merged output missing: %v", err)
	}
	info, err := remote.ProbeMedia(output)
	if err != nil {
		t.Fatalf("probe merged output: %v", err)
	}
	if math.Abs(info.Duration-5.5) > .12 || info.Width != 160 || info.Height != 96 {
		t.Fatalf("merged output = %dx%d duration %.3f, want 160x96 duration 5.5±.12", info.Width, info.Height, info.Duration)
	}
	if log, runErr := runLocalProgram(ffmpeg, []string{"-v", "error", "-i", output, "-map", "0:a:0", "-f", "null", "-"}, fixtureTimeout); runErr != nil {
		t.Fatalf("merged output has no decodable audio: %v\n%s", runErr, log)
	}
	if mergeFixtureSHA256(t, firstInput) != firstHash || mergeFixtureSHA256(t, secondInput) != secondHash || mergeFixtureSHA256(t, overlayInput) != overlayHash {
		t.Fatal("an input file changed during the actual merge")
	}
	var gpuTasks int64
	if err := db.Model(&models.Task{}).Count(&gpuTasks).Error; err != nil {
		t.Fatal(err)
	}
	if gpuTasks != 0 {
		t.Fatalf("actual local merge created %d GPU tasks", gpuTasks)
	}
}
