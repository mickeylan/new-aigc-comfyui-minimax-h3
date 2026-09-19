package service

import (
	"embed"
	"encoding/json"
	"fmt"
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
	return nil
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

// InputDir 返回 ComfyUI 共享 input 目录
func (m *UploadManager) InputDir() string {
	return m.cfg.Comfy.ComfyDir + "/input"
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
	var target string
	if m.remote != nil && m.remote.Enabled() {
		// SFTP and remote shells always use POSIX paths, even when the API server runs on Windows.
		target = path.Join(filepath.ToSlash(m.InputDir()), relPath)
	} else {
		root := filepath.Clean(m.InputDir())
		target = filepath.Join(root, filepath.FromSlash(relPath))
		rel, err := filepath.Rel(root, target)
		if err != nil || rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
			return "", 0, fmt.Errorf("上传路径越界")
		}
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
