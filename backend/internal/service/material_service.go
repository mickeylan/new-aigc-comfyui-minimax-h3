package service

import (
	"errors"
	"fmt"
	"path/filepath"
	"strings"

	"gorm.io/gorm"

	"comfyui-console/internal/config"
	"comfyui-console/internal/models"
)

var ErrMaterialReferenced = errors.New("material is referenced by a project")

// MaterialService 素材库：场景生成图片自动入库 + 手动上传管理
type MaterialService struct {
	cfg    *config.Config
	db     *gorm.DB
	remote *RemoteExec
	upload *UploadManager
}

func NewMaterialService(cfg *config.Config, db *gorm.DB, remote *RemoteExec, upload *UploadManager) *MaterialService {
	return &MaterialService{cfg: cfg, db: db, remote: remote, upload: upload}
}

// SaveGeneratedImage 场景画面生成成功后自动入库
func (m *MaterialService) SaveGeneratedImage(sc *models.Scene, inputPath string, size int64) {
	name := filepath.Base(inputPath)
	projectID := sc.ProjectID
	// 每个场景只保留当前画面记录，避免重试生成产生重复素材。
	m.db.Where("scene_id = ? AND source = ?", sc.ID, "scene").Delete(&models.Material{})
	m.db.Create(&models.Material{
		Name:      fmt.Sprintf("%s_%s", sceneTag(sc), name),
		Type:      "image",
		Source:    "scene",
		ProjectID: &projectID,
		SceneID:   &sc.ID,
		Path:      inputPath,
		Prompt:    sc.ImagePrompt,
		Size:      size,
	})
}

// SaveUpload 手动上传素材入库（保存到 input/<materials>/<name>）
func (m *MaterialService) SaveUpload(projectID *uint, ftype, name string, data []byte) (*models.Material, error) {
	return m.SaveUploadWithThumb(projectID, ftype, name, data, "", nil)
}

// SaveUploadWithThumb 与 SaveUpload 相同，另存一张预览图（3D 素材用：客户端截一帧）。
// 缩略图与模型文件落在同一 task 目录，删除素材时一起清理。
func (m *MaterialService) SaveUploadWithThumb(projectID *uint, ftype, name string, data []byte, thumbName string, thumb []byte) (*models.Material, error) {
	return m.saveUpload(projectID, ftype, name, data, thumbName, thumb, "upload")
}

// SaveStudioModel 保存 3D 编辑器导出的模型与缩略图，来源标记为 studio_3d，
// 与素材库手动上传区分开（素材页显示「3D 编辑器」）。
func (m *MaterialService) SaveStudioModel(projectID *uint, name string, data, thumb []byte) (*models.Material, error) {
	thumbName := ""
	if len(thumb) > 0 {
		thumbName = strings.TrimSuffix(name, filepath.Ext(name)) + "_thumb.png"
	}
	return m.saveUpload(projectID, "3d", name, data, thumbName, thumb, "studio_3d")
}

func (m *MaterialService) saveUpload(projectID *uint, ftype, name string, data []byte, thumbName string, thumb []byte, source string) (*models.Material, error) {
	taskID := "materials"
	if projectID != nil {
		taskID = fmt.Sprintf("material_p%d", *projectID)
	}
	path, size, err := m.upload.SaveFile(taskID, ftype, name, data)
	if err != nil {
		return nil, err
	}
	thumbPath := ""
	if len(thumb) > 0 && strings.TrimSpace(thumbName) != "" {
		thumbPath, _, err = m.upload.SaveFile(taskID, "image", thumbName, thumb)
		if err != nil {
			return nil, err
		}
	}
	mat := models.Material{
		Name: name, Type: ftype, Source: source,
		ProjectID: projectID, Path: path, ThumbPath: thumbPath, Size: size,
	}
	if err := m.db.Create(&mat).Error; err != nil {
		return nil, err
	}
	return &mat, nil
}

// List 素材列表，支持类型/项目筛选（不分页，老调用方用这个）
func (m *MaterialService) List(ftype string, projectID *uint) ([]models.Material, error) {
	return m.ListPaged(ftype, projectID, 1, 0)
}

// ListPaged 分页列表：page 从 1 开始；pageSize <= 0 表示不分页。
// 排序固定 id desc，新入的素材总在第一页。
func (m *MaterialService) ListPaged(ftype string, projectID *uint, page, pageSize int) ([]models.Material, error) {
	var list []models.Material
	q := m.db.Model(&models.Material{})
	if ftype != "" {
		q = q.Where("type = ?", ftype)
	}
	if projectID != nil {
		q = q.Where("project_id = ?", *projectID)
	}
	if pageSize > 0 {
		if page < 1 {
			page = 1
		}
		q = q.Limit(pageSize).Offset((page - 1) * pageSize)
	}
	if err := q.Order("id desc").Find(&list).Error; err != nil {
		return nil, err
	}
	return list, nil
}

// Delete 删除素材（记录 + 文件）
func (m *MaterialService) Delete(id uint) error {
	var mat models.Material
	if err := m.db.First(&mat, id).Error; err != nil {
		return err
	}
	var referenceCount int64
	if err := m.db.Model(&models.SharedAssetReference{}).Where("material_id = ?", id).Count(&referenceCount).Error; err != nil {
		return err
	}
	if referenceCount > 0 {
		return ErrMaterialReferenced
	}
	// 删除 input 文件（场景共享素材不物理删除，避免影响场景引用）
	if mat.Source == "upload" || mat.Source == "studio_3d" {
		for _, rel := range []string{mat.Path, mat.ThumbPath} {
			if strings.TrimSpace(rel) == "" {
				continue
			}
			// 同名同文件被重复存进库很常见（多行指向一个文件）。删其中一行时
			// 如果还有别的行引用同一份文件，就不能把文件带走。
			shared, err := m.otherMaterialUsesPath(rel, id)
			if err != nil {
				return err
			}
			if shared {
				continue
			}
			full := m.upload.InputDir() + "/" + rel
			if _, err := m.remote.Run(fmt.Sprintf("rm -f '%s'", full)); err != nil {
				return err
			}
		}
	}
	return m.db.Delete(&mat).Error
}

// otherMaterialUsesPath 查除 excludeID 之外的素材行是否还引用同一个文件路径。
func (m *MaterialService) otherMaterialUsesPath(rel string, excludeID uint) (bool, error) {
	var count int64
	err := m.db.Model(&models.Material{}).
		Where("id <> ? AND (path = ? OR thumb_path = ?)", excludeID, rel, rel).
		Count(&count).Error
	if err != nil {
		return false, err
	}
	return count > 0, nil
}

// TaskIDOf 解析素材 input 相对路径的目录段（用于预览 URL）
func TaskIDOf(path string) string {
	if i := strings.Index(path, "/"); i >= 0 {
		return path[:i]
	}
	return ""
}

// FileNameOf 解析素材 input 相对路径的文件名段
func FileNameOf(path string) string {
	if i := strings.LastIndex(path, "/"); i >= 0 {
		return path[i+1:]
	}
	return path
}

func sceneTag(sc *models.Scene) string {
	if sc.Title != "" {
		return strings.TrimSuffix(sc.Title, " ")
	}
	return fmt.Sprintf("场景%d", sc.Order)
}

var _ = fmt.Sprint
