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
		var count int64
		if err := s.DB.Model(&models.Project{}).Where("id = ?", id).Count(&count).Error; err != nil {
			c.JSON(500, gin.H{"error": "查询项目失败"})
			return
		}
		if id == 0 || count == 0 {
			c.JSON(400, gin.H{"error": "项目不存在"})
			return
		}
		project = &id
	}
	scene, ok := s.validateSceneArchiveScene(c, c.PostForm("scene_id"), project)
	if !ok {
		return
	}
	archive := models.SceneArchive{ProjectID: project, SceneID: scene, Name: name, Size: int64(len(payload))}
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

// validateSceneArchiveScene 对可选来源作同一套创建/筛选校验，不能只凭客户端 id 关联。
func (s *Service) validateSceneArchiveScene(c *gin.Context, value string, project *uint) (*uint, bool) {
	if value == "" {
		return nil, true
	}
	parsed, err := strconv.ParseUint(value, 10, 64)
	if err != nil || parsed == 0 || uint64(uint(parsed)) != parsed || project == nil || *project == 0 {
		c.JSON(400, gin.H{"error": "scene_id 须为正整数且必须指定 project_id"})
		return nil, false
	}
	var count int64
	if err := s.DB.Model(&models.Scene{}).Where("id = ? AND project_id = ? AND archived = ?", uint(parsed), *project, false).Count(&count).Error; err != nil {
		c.JSON(500, gin.H{"error": "查询来源场次失败"})
		return nil, false
	}
	if count != 1 {
		c.JSON(400, gin.H{"error": "来源场次不存在、不属于该项目或已归档"})
		return nil, false
	}
	id := uint(parsed)
	return &id, true
}

// HandleListSceneArchives 列出存档元数据，可按 project_id / scene_id 过滤，新的排在前面。
func (s *Service) HandleListSceneArchives(c *gin.Context) {
	query := s.DB.Model(&models.SceneArchive{})
	var project *uint
	if value := strings.TrimSpace(c.Query("project_id")); value != "" {
		parsed, err := strconv.ParseUint(value, 10, 64)
		if err != nil {
			c.JSON(http.StatusBadRequest, gin.H{"error": "project_id 不是数字"})
			return
		}
		if parsed == 0 {
			query = query.Where("project_id IS NULL")
		} else {
			id := uint(parsed)
			project = &id
			query = query.Where("project_id = ?", id)
		}
	}
	scene, ok := s.validateSceneArchiveScene(c, c.Query("scene_id"), project)
	if !ok {
		return
	}
	if scene != nil {
		query = query.Where("scene_id = ?", *scene)
	}
	var total int64
	if err := query.Count(&total).Error; err != nil {
		c.JSON(500, gin.H{"error": "统计存档失败"})
		return
	}
	query = query.Order("created_at DESC, id DESC")
	if c.Query("page") != "" || c.Query("page_size") != "" {
		page, size := 1, 20
		if value := c.Query("page"); value != "" {
			n, err := strconv.Atoi(value)
			if err != nil || n < 1 || n > 1000000 {
				c.JSON(400, gin.H{"error": "page 无效"})
				return
			}
			page = n
		}
		if value := c.Query("page_size"); value != "" {
			n, err := strconv.Atoi(value)
			if err != nil || n < 1 || n > 100 {
				c.JSON(400, gin.H{"error": "page_size 须为 1–100"})
				return
			}
			size = n
		}
		query = query.Offset((page - 1) * size).Limit(size)
	}
	archives := []models.SceneArchive{}
	if err := query.Find(&archives).Error; err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "读取场景存档列表失败：" + err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{"items": archives, "total": total})
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

// HandleRenameSceneArchive 仅改展示名称，不重写存档正文。
func (s *Service) HandleRenameSceneArchive(c *gin.Context) {
	id, err := strconv.ParseUint(c.Param("id"), 10, 64)
	if err != nil || id == 0 {
		c.JSON(400, gin.H{"error": "存档 id 无效"})
		return
	}
	var body struct {
		Name string `json:"name"`
	}
	if c.ShouldBindJSON(&body) != nil {
		c.JSON(400, gin.H{"error": "名称无效"})
		return
	}
	name := strings.TrimSpace(body.Name)
	if len([]rune(name)) == 0 || len([]rune(name)) > 200 {
		c.JSON(400, gin.H{"error": "名称须为 1–200 字"})
		return
	}
	result := s.DB.Model(&models.SceneArchive{}).Where("id = ?", id).Update("name", name)
	if result.Error != nil {
		c.JSON(500, gin.H{"error": "重命名失败"})
		return
	}
	if result.RowsAffected == 0 {
		c.JSON(404, gin.H{"error": "存档不存在"})
		return
	}
	c.JSON(200, gin.H{"id": id, "name": name})
}

// HandleDeleteSceneArchive 软删除进回收区：原始 JSON 保留，可由管理员恢复。
func (s *Service) HandleDeleteSceneArchive(c *gin.Context) {
	id, err := strconv.ParseUint(c.Param("id"), 10, 64)
	if err != nil || id == 0 {
		c.JSON(400, gin.H{"error": "存档 id 无效"})
		return
	}
	result := s.DB.Where("id = ?", id).Delete(&models.SceneArchive{})
	if result.Error != nil {
		c.JSON(500, gin.H{"error": "移入回收区失败"})
		return
	}
	if result.RowsAffected == 0 {
		c.JSON(404, gin.H{"error": "存档不存在"})
		return
	}
	c.Status(204)
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
