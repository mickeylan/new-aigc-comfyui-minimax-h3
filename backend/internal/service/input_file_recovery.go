package service

import (
	"encoding/json"
	"fmt"
	"io"
	"log"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"

	"comfyui-console/internal/models"
)

// Generated asset images are named <prefix>_<entityID>_<timestamp>.<ext>. When the
// referenced copy in ComfyUI/input was cleared, the authoritative source is the
// generation task that produced it: its result file still lives in the ComfyUI
// output directory (for example output/krea2). Recovery therefore re-downloads the
// recorded output instead of guessing filenames or scanning local ComfyUI folders.
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

// Generated media is stored in the durable asset root, never in ComfyUI input.
func (s *TaskService) inputFilePath(projectID uint, name string) string {
	return filepath.Join(s.cfg.AssetsRoot(), strconv.FormatUint(uint64(projectID), 10), name)
}

// legacyInputFilePath is the pre-migration ComfyUI input location, read-only fallback.
func (s *TaskService) legacyInputFilePath(projectID uint, name string) string {
	return filepath.Join(s.cfg.Comfy.ComfyDir, "input", strconv.FormatUint(uint64(projectID), 10), name)
}

func (s *TaskService) existingInputPath(projectID uint, name string) (string, bool) {
	primary := s.inputFilePath(projectID, name)
	if s.remote.Exists(primary) {
		return primary, true
	}
	legacy := s.legacyInputFilePath(projectID, name)
	if s.remote.Exists(legacy) {
		return legacy, true
	}
	return "", false
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

func (s *TaskService) downloadTaskOutput(task *models.Task) ([]byte, error) {
	file, _ := resultImageOf(task)
	if strings.TrimSpace(file) == "" {
		return nil, fmt.Errorf("任务 %s 没有记录生成的图片结果", task.TaskID)
	}
	if s.outputDownloader != nil {
		return s.outputDownloader(task)
	}
	subfolder, filename := filepath.ToSlash(filepath.Dir(file)), filepath.Base(file)
	if subfolder == "." {
		subfolder = ""
	}
	client, err := s.clientForTask(task)
	if err != nil {
		return nil, err
	}
	// The recorded subfolder is authoritative. Configured folders are only fallbacks
	// for installations whose output layout was changed by the user.
	var lastErr error
	for _, candidate := range s.outputSubfolders(subfolder) {
		data, downloadErr := client.DownloadOutput(filename, candidate, "output")
		if downloadErr == nil && len(data) > 0 {
			return data, nil
		}
		lastErr = downloadErr
	}
	if lastErr == nil {
		lastErr = fmt.Errorf("输出文件 %s 不存在", file)
	}
	return nil, lastErr
}

// outputSubfolders returns the recorded folder first, then any user-configured
// ComfyUI output subfolders, then the output root. No folder name is hardcoded.
func (s *TaskService) outputSubfolders(recorded string) []string {
	candidates := []string{}
	seen := map[string]bool{}
	add := func(value string) {
		value = strings.Trim(strings.TrimSpace(value), "/")
		// Subfolders are relative to the ComfyUI output root; reject traversal,
		// absolute paths, drive letters and backslashes instead of silently using them.
		unsafe := strings.Contains(value, "..") || strings.ContainsAny(value, `:\`) || strings.HasPrefix(value, "/")
		if unsafe {
			return
		}
		if !seen[value] {
			seen[value] = true
			candidates = append(candidates, value)
		}
	}
	add(recorded)
	for _, value := range s.configuredOutputSubfolders() {
		add(value)
	}
	add("")
	return candidates
}

const settingComfyOutputSubfolders = "comfy_output_subfolders"

func (s *TaskService) configuredOutputSubfolders() []string {
	out := append([]string(nil), s.cfg.Comfy.OutputSubfolders...)
	if s.db != nil {
		var setting models.Setting
		if s.db.Where("key = ?", settingComfyOutputSubfolders).First(&setting).Error == nil {
			out = append(out, strings.FieldsFunc(setting.Value, func(r rune) bool {
				return r == ',' || r == ';' || r == '，' || r == '；' || r == '\n' || r == ' '
			})...)
		}
	}
	return out
}

// restoreFromRecordedOutput re-downloads the exact artwork recorded for this
// entity. Provenance stores the generating task id as "krea2_task:<taskID>".
func (s *TaskService) restoreFromRecordedOutput(projectID uint, name string) (string, error) {
	var versions []models.AssetVariant
	s.db.Where("project_id = ? AND file = ?", projectID, name).Order("id DESC").Find(&versions)
	if len(versions) == 0 {
		if owner, err := s.resolveMissingInputOwner(projectID, name); err == nil {
			s.db.Where("project_id = ? AND entity_type = ? AND entity_id = ?", projectID, owner.entityType, owner.entityID).
				Order("selected DESC, id DESC").Find(&versions)
		}
	}
	attempted := []string{}
	for _, version := range versions {
		provenance := strings.TrimSpace(version.Provenance)
		taskID := strings.TrimSpace(strings.TrimPrefix(provenance, "krea2_task:"))
		if taskID == "" || taskID == provenance {
			continue
		}
		var task models.Task
		if err := s.db.Where("task_id = ?", taskID).First(&task).Error; err != nil {
			attempted = append(attempted, "任务记录已不存在: "+taskID)
			continue
		}
		data, err := s.downloadTaskOutput(&task)
		if err != nil || len(data) == 0 {
			reason := "输出为空"
			if err != nil {
				reason = err.Error()
			}
			attempted = append(attempted, fmt.Sprintf("任务%s: %s", taskID, reason))
			continue
		}
		if err := s.remote.WriteFile(s.inputFilePath(projectID, name), data, 0o644); err != nil {
			return "", fmt.Errorf("恢复 %s 失败: %w", name, err)
		}
		log.Printf("[input-repair] restored %s from recorded ComfyUI output task=%s (project=%d)", name, taskID, projectID)
		return "生成任务输出", nil
	}
	if len(attempted) > 0 {
		return "", fmt.Errorf("素材 %s 在 input 目录已丢失，且无法从生成任务重新取回：%s", name, strings.Join(attempted, "；"))
	}
	return "", fmt.Errorf("素材 %s 在 input 目录已丢失，且没有记录可重新取回的生成任务", name)
}

// restoreFromAuthoritativeCurrent copies the entity's currently selected artwork
// when that file still exists in the input directory. Historical variants are never
// substituted silently, because that would change the appearance.
func (s *TaskService) restoreFromAuthoritativeCurrent(projectID uint, name string) (string, error) {
	owner, err := s.resolveMissingInputOwner(projectID, name)
	if err != nil {
		return "", err
	}
	authoritative := strings.TrimSpace(owner.file)
	if authoritative == "" {
		return "", fmt.Errorf("%s缺少当前有效图片，请在资产卡重新生成", owner.label)
	}
	sourcePath, found := s.existingInputPath(projectID, authoritative)
	if !found {
		sourcePath = ""
	}
	reader, openErr := s.remote.Open(sourcePath)
	if openErr != nil {
		return "", fmt.Errorf("%s的当前图片已丢失，且没有可重新取回的生成任务；请在资产卡重新生成或选择一个仍存在的历史版本", owner.label)
	}
	data, readErr := io.ReadAll(io.LimitReader(reader, 64<<20))
	reader.Close()
	if readErr != nil || len(data) == 0 {
		return "", fmt.Errorf("读取%s当前图片失败，请在资产卡重新生成", owner.label)
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
			for _, variant := range deleted {
				if refs[j].AssetVariantID == variant.ID {
					refs[j].AssetVariantID = 0
					changed = true
				}
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

// ensureProjectInputFile returns a readable source path for a generated asset,
// restoring it when the ComfyUI input copy was cleared. Only the recorded
// generation output or the entity's current authoritative artwork is used.
func (s *TaskService) ensureProjectInputFile(projectID uint, name string) (string, error) {
	configured := s.inputFilePath(projectID, name)
	if existing, ok := s.existingInputPath(projectID, name); ok {
		// Lazy migration: keep serving from the durable root once possible.
		if existing == configured {
			return configured, nil
		}
		if data, readErr := s.remote.Open(existing); readErr == nil {
			content, copyErr := io.ReadAll(io.LimitReader(data, 64<<20))
			data.Close()
			if copyErr == nil && len(content) > 0 {
				if writeErr := s.remote.WriteFile(configured, content, 0o644); writeErr == nil {
					log.Printf("[assets] migrated %s from legacy ComfyUI input (project=%d)", name, projectID)
					return configured, nil
				}
			}
		}
		return existing, nil
	}
	// The recorded output is this exact file's own artwork, so Scene selections that
	// reference it stay valid. Only falling back to the current authoritative image
	// changes what the file looks like, and only then are stale selections repaired.
	if _, err := s.restoreFromRecordedOutput(projectID, name); err == nil {
		return configured, nil
	} else {
		outputErr := err
		if _, currentErr := s.restoreFromAuthoritativeCurrent(projectID, name); currentErr != nil {
			projectInput := filepath.Dir(configured)
			if fi, statErr := os.Stat(projectInput); !s.remote.Enabled() && (statErr != nil || !fi.IsDir()) {
				return "", fmt.Errorf("素材目录不可访问：%s；请检查配置 asset_dir 或网络盘（素材文件本身未被删除）", s.cfg.AssetsRoot())
			}
			return "", fmt.Errorf("%v；%v", outputErr, currentErr)
		}
	}
	s.repairStaleSceneReferenceVariants(projectID, name)
	return configured, nil
}
