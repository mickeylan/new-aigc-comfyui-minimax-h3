package service

import (
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"strings"

	"github.com/gin-gonic/gin"

	"comfyui-console/internal/models"
)

// maxSceneArchiveBytes 是单份场景存档的上限。3D 编辑器把整份 GLB 以 base64
// 内嵌进 JSON，15 MB 几何体编码后约 20 MB，取 32 MB 留出余量。
const maxSceneArchiveBytes = 32 << 20

// SceneArchiveDir 是场景存档的落地目录。它跟数据库同属 data 卷，跟着
// storage.data_dir 走；配置为空（测试里直接构造 Service）时退回 ./data。
func (s *Service) SceneArchiveDir() string {
	root := "data"
	if s.Cfg != nil && strings.TrimSpace(s.Cfg.Storage.DataDir) != "" {
		root = strings.TrimSpace(s.Cfg.Storage.DataDir)
	}
	return filepath.Join(root, "scene-archives")
}

func (s *Service) sceneArchivePath(id uint) string {
	return filepath.Join(s.SceneArchiveDir(), fmt.Sprintf("%d.json", id))
}

// HandleCreateSceneArchive 接收前端「保存场景」导出的 JSON。优先读 multipart 的
// file 字段，也接受直接 POST 原始 body。内容不在这里校验：载入时 loadScene 会按
// version 与 model.data 判断是不是可识别的场景文件；只挡住超限与空内容。
func (s *Service) HandleCreateSceneArchive(c *gin.Context) {
	c.Request.Body = http.MaxBytesReader(c.Writer, c.Request.Body, maxSceneArchiveBytes)
	var (
		name    string
		payload []byte
		err     error
	)
	if strings.HasPrefix(c.ContentType(), "multipart/form-data") {
		file, header, formErr := c.Request.FormFile("file")
		if formErr != nil {
			c.JSON(http.StatusBadRequest, gin.H{"error": "缺少 file 字段"})
			return
		}
		defer file.Close()
		if header != nil {
			name = strings.TrimSpace(header.Filename)
		}
		payload, err = io.ReadAll(file)
	} else {
		payload, err = io.ReadAll(c.Request.Body)
	}
	if err != nil {
		// MaxBytesReader 超限返回 *http.MaxBytesError，要报 413 而不是 500。
		var tooLarge *http.MaxBytesError
		if errors.As(err, &tooLarge) {
			c.JSON(http.StatusRequestEntityTooLarge, gin.H{
				"error": fmt.Sprintf("场景存档超出上限（%d MB）", maxSceneArchiveBytes>>20),
			})
			return
		}
		c.JSON(http.StatusBadRequest, gin.H{"error": "读取存档内容失败：" + err.Error()})
		return
	}
	if len(payload) == 0 {
		c.JSON(http.StatusBadRequest, gin.H{"error": "存档内容为空"})
		return
	}
	if value := strings.TrimSpace(c.PostForm("name")); value != "" {
		name = value
	}
	name = truncateSceneArchiveName(name)
	var project *uint
	if value := strings.TrimSpace(c.PostForm("project_id")); value != "" {
		parsed, parseErr := strconv.ParseUint(value, 10, 64)
		if parseErr != nil {
			c.JSON(http.StatusBadRequest, gin.H{"error": "project_id 不是数字"})
			return
		}
		id := uint(parsed)
		project = &id
	}
	archive := models.SceneArchive{ProjectID: project, Name: name, Size: int64(len(payload))}
	if err := s.DB.Create(&archive).Error; err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "登记场景存档失败：" + err.Error()})
		return
	}
	// 文件名只用自增 id，客户端给的名字只进表、不进路径，避免路径穿越。
	if err := os.MkdirAll(s.SceneArchiveDir(), 0o755); err != nil {
		s.DB.Delete(&archive)
		c.JSON(http.StatusInternalServerError, gin.H{"error": "创建存档目录失败：" + err.Error()})
		return
	}
	if err := os.WriteFile(s.sceneArchivePath(archive.ID), payload, 0o644); err != nil {
		s.DB.Delete(&archive)
		c.JSON(http.StatusInternalServerError, gin.H{"error": "写入存档文件失败：" + err.Error()})
		return
	}
	c.JSON(http.StatusCreated, archive)
}

// HandleListSceneArchives 列出存档元数据，可按 project_id 过滤，新的排在前面。
func (s *Service) HandleListSceneArchives(c *gin.Context) {
	query := s.DB.Order("created_at DESC, id DESC")
	if value := strings.TrimSpace(c.Query("project_id")); value != "" {
		parsed, err := strconv.ParseUint(value, 10, 64)
		if err != nil {
			c.JSON(http.StatusBadRequest, gin.H{"error": "project_id 不是数字"})
			return
		}
		query = query.Where("project_id = ?", uint(parsed))
	}
	archives := []models.SceneArchive{}
	if err := query.Find(&archives).Error; err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "读取场景存档列表失败：" + err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{"items": archives, "total": len(archives)})
}

// HandleGetSceneArchive 原样返回存档 JSON，前端直接交给 loadScene 解析。
func (s *Service) HandleGetSceneArchive(c *gin.Context) {
	parsed, err := strconv.ParseUint(c.Param("id"), 10, 64)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "存档 id 不是数字"})
		return
	}
	var archive models.SceneArchive
	if err := s.DB.First(&archive, uint(parsed)).Error; err != nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "场景存档不存在"})
		return
	}
	payload, err := os.ReadFile(s.sceneArchivePath(archive.ID))
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "存档文件读不到：" + err.Error()})
		return
	}
	c.Data(http.StatusOK, "application/json; charset=utf-8", payload)
}

// truncateSceneArchiveName 把名字压到 200 个字符以内（按 rune 截断，避免切坏中文）。
func truncateSceneArchiveName(name string) string {
	trimmed := []rune(strings.TrimSpace(name))
	if len(trimmed) == 0 {
		return "场景存档.json"
	}
	if len(trimmed) > 200 {
		trimmed = trimmed[:200]
	}
	return string(trimmed)
}