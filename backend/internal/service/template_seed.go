package service

import (
	"embed"
	"encoding/json"
	"fmt"
	"io"
	"io/fs"
	"log"
	"os"
	"path"
	"path/filepath"
	"sort"
	"strings"
	"unicode"

	"gorm.io/gorm"

	"comfyui-console/internal/config"
	"comfyui-console/internal/models"
)

//go:embed templates/*.json
var templateFS embed.FS

type templateFile struct {
	Name        string                    `json:"name"`
	Code        string                    `json:"code"`
	Description string                    `json:"description"`
	Inputs      []map[string]any          `json:"inputs"`
	Workflow    map[string]map[string]any `json:"workflow"`
}

// InitSystemTemplates 将模板同步到 DB。磁盘目录优先，embed 仅作为缺失文件的兜底。
// 因此部署后修改模板 JSON 再调用本函数即可生效，不需要重新编译 Go 可执行文件。
func InitSystemTemplates(db *gorm.DB, configuredDir ...string) error {
	files, diskDir, err := loadSystemTemplateFiles(firstString(configuredDir))
	if err != nil {
		return err
	}
	if diskDir != "" {
		log.Printf("[templates] loading runtime templates from %s", diskDir)
	} else {
		log.Printf("[templates] runtime directory not found; using embedded templates")
	}
	for _, name := range sortedTemplateNames(files) {
		data := files[name]
		var tf templateFile
		if err := json.Unmarshal(data, &tf); err != nil {
			return fmt.Errorf("parse template %s: %w", name, err)
		}
		normalizeAutogrowInputs(tf.Workflow)
		inputsJSON, _ := json.Marshal(tf.Inputs)
		workflowJSON, _ := json.Marshal(tf.Workflow)

		var existing models.Template
		err = db.Where("code = ?", tf.Code).First(&existing).Error
		if err == gorm.ErrRecordNotFound {
			tpl := models.Template{Name: tf.Name, Code: tf.Code, Description: tf.Description, WorkflowJSON: string(workflowJSON), InputsJSON: string(inputsJSON), IsSystem: true, Enabled: true}
			if err := db.Create(&tpl).Error; err != nil {
				return err
			}
			log.Printf("[templates] seeded %s", tf.Code)
		} else if err != nil {
			return err
		} else if err := db.Model(&existing).Updates(map[string]any{
			"name": tf.Name, "description": tf.Description,
			"workflow_json": string(workflowJSON), "inputs_json": string(inputsJSON),
		}).Error; err != nil {
			return err
		}
	}
	// The ComfyUI production path is the Python workflow. Keep the historical Rust
	// template record only for task auditability, but never offer it for new tasks.
	if err := db.Model(&models.Template{}).Where("code = ?", "indextts_2_5_rust_dialogue").Update("enabled", false).Error; err != nil {
		return err
	}
	return nil
}

// normalizeAutogrowInputs repairs UI-exported nested Autogrow inputs before persisting templates.
// ComfyUI API format requires flat keys such as ref_images.ref_image_0.
func normalizeAutogrowInputs(workflow map[string]map[string]any) {
	for _, node := range workflow {
		inputs, ok := node["inputs"].(map[string]any)
		if !ok {
			continue
		}
		for _, groupName := range []string{"ref_images", "ref_videos", "ref_audios"} {
			group, ok := inputs[groupName].(map[string]any)
			if !ok {
				continue
			}
			delete(inputs, groupName)
			for key, value := range group {
				inputs[groupName+"."+key] = value
			}
		}
	}
}

func firstString(values []string) string {
	if len(values) > 0 {
		return values[0]
	}
	return ""
}

func sortedTemplateNames(files map[string][]byte) []string {
	names := make([]string, 0, len(files))
	for name := range files {
		names = append(names, name)
	}
	sort.Strings(names)
	return names
}

func loadSystemTemplateFiles(configuredDir string) (map[string][]byte, string, error) {
	files := map[string][]byte{}
	if err := fs.WalkDir(templateFS, "templates", func(path string, entry fs.DirEntry, err error) error {
		if err != nil || entry.IsDir() || filepath.Ext(path) != ".json" {
			return err
		}
		data, err := templateFS.ReadFile(path)
		if err == nil {
			files[filepath.Base(path)] = data
		}
		return err
	}); err != nil {
		return nil, "", err
	}

	dir := findRuntimeTemplateDir(configuredDir)
	if dir == "" {
		return files, "", nil
	}
	entries, err := os.ReadDir(dir)
	if err != nil {
		return nil, "", err
	}
	for _, entry := range entries {
		if entry.IsDir() || filepath.Ext(entry.Name()) != ".json" {
			continue
		}
		data, err := os.ReadFile(filepath.Join(dir, entry.Name()))
		if err != nil {
			return nil, "", err
		}
		files[entry.Name()] = data
	}
	return files, dir, nil
}

func findRuntimeTemplateDir(configuredDir string) string {
	candidates := []string{configuredDir, os.Getenv("COMFYUI_TEMPLATE_DIR"), "templates", filepath.Join("internal", "service", "templates"), filepath.Join("backend", "internal", "service", "templates")}
	if exe, err := os.Executable(); err == nil {
		candidates = append(candidates, filepath.Join(filepath.Dir(exe), "templates"))
	}
	for _, dir := range candidates {
		if dir == "" {
			continue
		}
		if info, err := os.Stat(dir); err == nil && info.IsDir() {
			abs, _ := filepath.Abs(dir)
			return abs
		}
	}
	return ""
}

// UploadManager 素材文件管理
type UploadManager struct {
	cfg    *config.Config
	db     *gorm.DB
	remote *RemoteExec
}

func NewUploadManager(cfg *config.Config, db *gorm.DB, remote *RemoteExec) *UploadManager {
	return &UploadManager{cfg: cfg, db: db, remote: remote}
}

// InputDir 返回持久化的项目素材目录（data/assets），不再是 ComfyUI 的 input。
// ComfyUI 实例只接收每次任务提交前的临时上传，清空实例 input 不会丢素材。
func (m *UploadManager) InputDir() string {
	return m.cfg.AssetsRoot()
}

// LegacyInputDir 是旧版本存放素材的 ComfyUI input 目录，仅用于一次性/按需迁移读取。
func (m *UploadManager) LegacyInputDir() string {
	return filepath.Join(m.cfg.Comfy.ComfyDir, "input")
}

// ResolveReadPath 返回素材的持久路径：优先 data/assets；仅当旧文件仍留在 ComfyUI
// input 时按需复制到持久目录，避免用户只备份 data（含数据库）却丢素材。
func (m *UploadManager) ResolveReadPath(taskID, name string) string {
	rel := filepath.Join(taskID, filepath.FromSlash(name))
	primary := filepath.Join(m.InputDir(), rel)
	if m.remote != nil && !m.remote.Enabled() {
		if _, err := os.Stat(primary); err == nil {
			return primary
		}
	} else if m.remote != nil && m.remote.Exists(primary) {
		return primary
	}
	if m.remote == nil {
		return primary
	}
	legacy := filepath.Join(m.LegacyInputDir(), rel)
	if !m.remote.Exists(legacy) {
		return primary
	}
	// Only migrate files this application recorded as assets. Transient files such
	// as extracted video frames legitimately live in the ComfyUI input directory.
	var recorded int64
	if m.db != nil {
		_ = m.db.Model(&models.UploadFile{}).Where("task_id = ? AND name = ?", taskID, filepath.Base(name)).Count(&recorded).Error
	}
	if recorded == 0 {
		return legacy
	}
	if data, err := m.remote.Open(legacy); err == nil {
		content, readErr := io.ReadAll(io.LimitReader(data, 512<<20))
		data.Close()
		if readErr == nil && len(content) > 0 {
			if err := m.remote.WriteFile(primary, content, 0o644); err == nil {
				log.Printf("[assets] migrated %s from legacy ComfyUI input", rel)
				return primary
			}
		}
	}
	return legacy
}

// MigrateLegacyAssets copies previously recorded project media from the ComfyUI
// input directory into the durable asset root. Idempotent; missing files are
// reported but never removed from the legacy location.
func (m *UploadManager) MigrateLegacyAssets() {
	if m == nil || m.db == nil || m.remote == nil {
		return
	}
	legacyRoot := filepath.Clean(m.LegacyInputDir())
	if samePath(legacyRoot, filepath.Clean(m.InputDir())) {
		return
	}
	var files []models.UploadFile
	if err := m.db.Order("id").Find(&files).Error; err != nil {
		log.Printf("[assets] legacy migration skipped: %v", err)
		return
	}
	migrated, missing := 0, 0
	for _, file := range files {
		if strings.TrimSpace(file.TaskID) == "" || strings.TrimSpace(file.Name) == "" {
			continue
		}
		primary := filepath.Join(m.InputDir(), file.TaskID, filepath.FromSlash(file.Name))
		if m.remote.Exists(primary) {
			continue
		}
		legacy := filepath.Join(legacyRoot, file.TaskID, filepath.FromSlash(file.Name))
		reader, err := m.remote.Open(legacy)
		if err != nil {
			missing++
			continue
		}
		data, readErr := io.ReadAll(io.LimitReader(reader, 512<<20))
		reader.Close()
		if readErr != nil || len(data) == 0 {
			missing++
			continue
		}
		if err := m.remote.WriteFile(primary, data, 0o644); err != nil {
			log.Printf("[assets] migrate %s failed: %v", legacy, err)
			missing++
			continue
		}
		migrated++
	}
	log.Printf("[assets] legacy migration finished: migrated=%d missing=%d asset_dir=%s", migrated, missing, m.InputDir())
}

func samePath(a, b string) bool {
	absA, errA := filepath.Abs(a)
	absB, errB := filepath.Abs(b)
	return errA == nil && errB == nil && strings.EqualFold(filepath.Clean(absA), filepath.Clean(absB))
}

func safeFileSegment(value, label string) (string, error) {
	value = strings.TrimSpace(value)
	invalid := value == "" || value == "." || value == ".." ||
		strings.ContainsAny(value, `/\\`) ||
		(len(value) >= 2 && ((value[0] >= 'A' && value[0] <= 'Z') || (value[0] >= 'a' && value[0] <= 'z')) && value[1] == ':')
	if !invalid {
		for _, r := range value {
			if unicode.IsControl(r) {
				invalid = true
				break
			}
		}
	}
	if invalid {
		return "", fmt.Errorf("%s 路径无效", label)
	}
	return value, nil
}

// SaveFile 保存上传文件到 ComfyUI input 目录（远程模式走 SFTP），返回 input 相对路径 (taskId/filename)
func (m *UploadManager) SaveFile(taskID, ftype, filename string, data []byte) (string, int64, error) {
	var err error
	if taskID, err = safeFileSegment(taskID, "task_id"); err != nil {
		return "", 0, err
	}
	if filename, err = safeFileSegment(filename, "文件名"); err != nil {
		return "", 0, err
	}
	if ftype != "image" && ftype != "video" && ftype != "audio" && ftype != "novel" && ftype != "document" {
		return "", 0, fmt.Errorf("文件类型无效")
	}
	relPath := path.Join(taskID, filename)
	// Durable assets always live on the API host under <data_dir>/assets, even when
	// ComfyUI runs remotely; instances receive a temporary copy per submitted task.
	root := filepath.Clean(m.InputDir())
	target := filepath.Join(root, filepath.FromSlash(relPath))
	rel, err := filepath.Rel(root, target)
	if err != nil || rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
		return "", 0, fmt.Errorf("上传路径越界")
	}
	if m.remote == nil {
		return "", 0, fmt.Errorf("上传服务未配置")
	}
	if err := m.remote.WriteFile(target, data, 0o644); err != nil {
		return "", 0, err
	}
	rec := models.UploadFile{TaskID: taskID, Type: ftype, Name: filename, Path: relPath, Size: int64(len(data))}
	if err := m.db.Create(&rec).Error; err != nil {
		return "", 0, err
	}
	return relPath, int64(len(data)), nil
}
