package service

import (
	"encoding/json"
	"fmt"
	"io"
	"log"
	"os"
	"path/filepath"
	"regexp"
	"runtime"
	"strconv"
	"strings"

	"comfyui-console/internal/models"
)

// Generated asset images are named <prefix>_<entityID>_<timestamp>.<ext>. When a
// referenced file is deleted, the authoritative entity record still identifies the
// same artwork. Restoring the current authoritative file is exact-identity
// recovery; a historical variant is never substituted silently.
var (
	generatedCharSheetName    = regexp.MustCompile(`^char_sheet_(\d+)_\d+`)
	generatedCharPortraitName = regexp.MustCompile(`^char_(\d+)_\d+`)
	generatedPropSheetName    = regexp.MustCompile(`^prop_sheet_(\d+)_\d+`)
	generatedAssetImageName   = regexp.MustCompile(`^(?:loc|prop)_(\d+)_\d+`)
	generatedLookName         = regexp.MustCompile(`^look_(\d+)_\d+`)
)

type missingInputOwner struct {
	label      string
	file       string
	entityType string
	entityID   uint
}

func (s *TaskService) inputFilePath(projectID uint, name string) string {
	return filepath.Join(s.cfg.Comfy.ComfyDir, "input", strconv.FormatUint(uint64(projectID), 10), name)
}

func (s *TaskService) resolveMissingInputOwner(projectID uint, name string) (*missingInputOwner, error) {
	var character models.Character
	var asset models.Asset
	var look models.CharacterLook
	entityID := func(matches []string) uint {
		id, _ := strconv.ParseUint(matches[1], 10, 64)
		return uint(id)
	}
	switch {
	case generatedCharSheetName.MatchString(name):
		id := entityID(generatedCharSheetName.FindStringSubmatch(name))
		if err := s.db.Where("id = ? AND project_id = ?", id, projectID).First(&character).Error; err != nil {
			return nil, fmt.Errorf("四视图所属角色不存在")
		}
		return &missingInputOwner{fmt.Sprintf("角色「%s」四视图", character.Name), character.Sheet, VariantCharacterSheet, character.ID}, nil
	case generatedCharPortraitName.MatchString(name):
		id := entityID(generatedCharPortraitName.FindStringSubmatch(name))
		if err := s.db.Where("id = ? AND project_id = ?", id, projectID).First(&character).Error; err != nil {
			return nil, fmt.Errorf("标准像所属角色不存在")
		}
		return &missingInputOwner{fmt.Sprintf("角色「%s」标准像", character.Name), character.Portrait, VariantCharacterPortrait, character.ID}, nil
	case generatedPropSheetName.MatchString(name):
		id := entityID(generatedPropSheetName.FindStringSubmatch(name))
		if err := s.db.Where("id = ? AND project_id = ?", id, projectID).First(&asset).Error; err != nil {
			return nil, fmt.Errorf("四视图所属道具不存在")
		}
		return &missingInputOwner{fmt.Sprintf("道具「%s」四视图", asset.Name), asset.Sheet, VariantAssetSheet, asset.ID}, nil
	case generatedAssetImageName.MatchString(name):
		id := entityID(generatedAssetImageName.FindStringSubmatch(name))
		if err := s.db.Where("id = ? AND project_id = ?", id, projectID).First(&asset).Error; err != nil {
			return nil, fmt.Errorf("参考图所属场景或道具不存在")
		}
		kind := "道具"
		if asset.Kind == AssetKindLocation {
			kind = "场景"
		}
		return &missingInputOwner{fmt.Sprintf("%s「%s」参考图", kind, asset.Name), asset.Image, VariantAssetImage, asset.ID}, nil
	case generatedLookName.MatchString(name):
		id := entityID(generatedLookName.FindStringSubmatch(name))
		if err := s.db.Where("id = ? AND project_id = ?", id, projectID).First(&look).Error; err != nil {
			return nil, fmt.Errorf("造型参考图记录不存在")
		}
		return &missingInputOwner{fmt.Sprintf("造型「%s」参考图", look.Name), look.Image, VariantCharacterLook, look.ID}, nil
	}
	return nil, fmt.Errorf("非项目资产生成文件")
}

// restoreMissingInputFile recreates one deleted generated asset file from the
// entity's current authoritative image. Returns a human-readable source label.
func (s *TaskService) restoreMissingInputFile(projectID uint, name string) (string, error) {
	owner, err := s.resolveMissingInputOwner(projectID, name)
	if err != nil {
		return "", err
	}
	authoritative := strings.TrimSpace(owner.file)
	if authoritative == "" {
		return "", fmt.Errorf("%s缺少当前有效图片，请在资产卡重新生成", owner.label)
	}
	sourcePath, readable := s.readableInputPath(projectID, authoritative)
	if !readable {
		var versions []models.AssetVariant
		s.db.Where("project_id = ? AND entity_type = ? AND entity_id = ? AND file <> ''", projectID, owner.entityType, owner.entityID).
			Order("selected DESC, id DESC").Find(&versions)
		existing := []string{}
		for _, version := range versions {
			if file := strings.TrimSpace(version.File); file != "" {
				if _, ok := s.readableInputPath(projectID, file); !ok {
					continue
				}
				existing = append(existing, file)
			}
		}
		if len(existing) == 0 {
			return "", fmt.Errorf("%s的当前图片和历史版本都已丢失，请在资产卡重新生成", owner.label)
		}
		return "", fmt.Errorf("%s的当前图片已丢失（历史版本仍存在：%s）；请先在资产卡“版本”中选择一个仍然存在的图片，系统不会自动替换为旧版外观", owner.label, strings.Join(existing, "、"))
	}
	reader, err := s.remote.Open(sourcePath)
	if err != nil {
		return "", fmt.Errorf("读取%s当前图片失败: %w", owner.label, err)
	}
	data, readErr := io.ReadAll(io.LimitReader(reader, 64<<20))
	reader.Close()
	if readErr != nil {
		return "", fmt.Errorf("读取%s当前图片失败: %w", owner.label, readErr)
	}
	if len(data) == 0 {
		return "", fmt.Errorf("%s当前图片为空，请重新生成", owner.label)
	}
	if err := s.remote.WriteFile(s.inputFilePath(projectID, name), data, 0o644); err != nil {
		return "", fmt.Errorf("恢复%s失败: %w", owner.label, err)
	}
	log.Printf("[input-repair] restored %s from authoritative %s (project=%d)", name, authoritative, projectID)
	return owner.label + "（当前有效版本）", nil
}

// repairStaleSceneReferenceVariants rewrites selections that still point at a
// deleted historical variant to the authoritative image (AssetVariantID=0).
func (s *TaskService) repairStaleSceneReferenceVariants(projectID uint, missingName string) {
	// Scene selections store the variant ID, not the filename, so resolve the ID
	// first and then rewrite every Scene that selected that deleted version.
	var deleted []models.AssetVariant
	if err := s.db.Where("project_id = ? AND file = ?", projectID, missingName).Find(&deleted).Error; err != nil || len(deleted) == 0 {
		return
	}
	clauses := make([]string, 0, len(deleted))
	args := []any{projectID}
	for _, variant := range deleted {
		clauses = append(clauses, "reference_images_json LIKE ?")
		args = append(args, fmt.Sprintf("%%\"asset_variant_id\":%d%%", variant.ID))
	}
	var scenes []models.Scene
	if err := s.db.Where("project_id = ? AND ("+strings.Join(clauses, " OR ")+")", args...).Find(&scenes).Error; err != nil {
		return
	}
	for i := range scenes {
		var refs []SceneReferenceSelection
		if json.Unmarshal([]byte(scenes[i].ReferenceImagesJSON), &refs) != nil {
			continue
		}
		changed := false
		for j := range refs {
			if refs[j].AssetVariantID == 0 {
				continue
			}
			var variant models.AssetVariant
			if err := s.db.First(&variant, refs[j].AssetVariantID).Error; err != nil {
				continue
			}
			if strings.EqualFold(filepath.Base(variant.File), missingName) {
				refs[j].AssetVariantID = 0
				changed = true
			}
		}
		if !changed {
			continue
		}
		encoded, err := json.Marshal(refs)
		if err != nil {
			continue
		}
		_ = s.db.Model(&models.Scene{}).Where("id = ?", scenes[i].ID).Update("reference_images_json", string(encoded)).Error
	}
}

// fallbackComfyRoots are extra ComfyUI roots checked for an identical basename.
// Same basename means the same generated file, so copying it cannot change
// identity. Tests replace this slice with a temporary directory.
var fallbackComfyRoots []string

func (s *TaskService) candidateComfyRoots() []string {
	if len(fallbackComfyRoots) > 0 {
		return fallbackComfyRoots
	}
	if s.remote.Enabled() || runtime.GOOS != "windows" {
		return nil
	}
	roots := []string{}
	for letter := 'C'; letter <= 'Z'; letter++ {
		drive := string(letter) + ":\\"
		if _, err := os.Stat(drive); err != nil {
			continue
		}
		for _, rel := range []string{"ComfyUI", "comfyui", "mickeylan\\comfyui", "opt\\comfyUI"} {
			root := filepath.Join(drive, rel)
			if fi, err := os.Stat(root); err == nil && fi.IsDir() {
				roots = append(roots, root)
			}
		}
	}
	return roots
}

func sameFile(a, b string) bool {
	absA, errA := filepath.Abs(a)
	absB, errB := filepath.Abs(b)
	return errA == nil && errB == nil && strings.EqualFold(filepath.Clean(absA), filepath.Clean(absB))
}

// readableInputPath finds a file in the configured input root, then in an
// alternate local ComfyUI root with the identical basename.
func (s *TaskService) readableInputPath(projectID uint, name string) (string, bool) {
	configured := s.inputFilePath(projectID, name)
	if s.remote.Exists(configured) {
		return configured, true
	}
	if s.remote.Enabled() {
		return "", false
	}
	for _, root := range s.candidateComfyRoots() {
		if sameFile(filepath.Join(root, "input"), filepath.Join(s.cfg.Comfy.ComfyDir, "input")) {
			continue
		}
		candidate := filepath.Join(root, "input", strconv.FormatUint(uint64(projectID), 10), name)
		if fi, err := os.Stat(candidate); err == nil && fi.Mode().IsRegular() {
			return candidate, true
		}
	}
	return "", false
}

// ensureProjectInputFile returns a readable source path for a generated asset,
// restoring it when the configured input directory was cleared. Only an identical
// filename or the same entity's current authoritative artwork is used, never a
// different-looking historical version.
func (s *TaskService) ensureProjectInputFile(projectID uint, name string) (string, error) {
	if path, ok := s.readableInputPath(projectID, name); ok {
		if path != s.inputFilePath(projectID, name) {
			// Best-effort copy back so later tasks read the configured location; the
			// already-found path remains usable when the configured drive is offline.
			if data, err := os.ReadFile(path); err == nil && len(data) > 0 {
				_ = s.remote.WriteFile(s.inputFilePath(projectID, name), data, 0o644)
			}
			s.repairStaleSceneReferenceVariants(projectID, name)
		}
		return path, nil
	}
	projectInput := filepath.Dir(s.inputFilePath(projectID, name))
	if fi, err := os.Stat(projectInput); !s.remote.Enabled() && (err != nil || !fi.IsDir()) {
		return "", fmt.Errorf("ComfyUI input 目录不可访问：%s；请检查配置 comfy_dir 是否正确、网络盘是否已连接（素材文件本身可能未被删除）", s.cfg.Comfy.ComfyDir)
	}
	if _, err := s.restoreMissingInputFile(projectID, name); err != nil {
		return "", err
	}
	s.repairStaleSceneReferenceVariants(projectID, name)
	return s.inputFilePath(projectID, name), nil
}
