package service

import (
	"encoding/json"
	"errors"
	"fmt"
	"math"
	"net/http"
	"strings"

	"comfyui-console/internal/models"
	"github.com/gin-gonic/gin"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

type previsVector struct {
	X float64 `json:"x"`
	Y float64 `json:"y"`
	Z float64 `json:"z"`
}
type previsObject struct {
	ID       string       `json:"id"`
	Name     string       `json:"name"`
	Kind     string       `json:"kind"`
	Position previsVector `json:"position"`
	Yaw      float64      `json:"yaw"`
	Width    float64      `json:"width"`
	Height   float64      `json:"height"`
	Depth    float64      `json:"depth"`
}
type previsCamera struct {
	Position previsVector `json:"position"`
	Target   previsVector `json:"target"`
	FOV      float64      `json:"fov"`
}
type previsView struct {
	Name   string       `json:"name"`
	Camera previsCamera `json:"camera"`
}
type previsDocument struct {
	Weather         *previsWeather         `json:"weather,omitempty"`
	Models          []previsModelBinding   `json:"models,omitempty"`
	DurationMS      int                    `json:"duration_ms,omitempty"`
	Routes          []previsRoute          `json:"routes,omitempty"`
	Keyframes       []previsKeyframe       `json:"keyframes,omitempty"`
	CameraKeyframes []previsCameraKeyframe `json:"camera_keyframes,omitempty"`
	SchemaVersion   int                    `json:"schema_version"`
	Objects         []previsObject         `json:"objects"`
	Camera          previsCamera           `json:"camera"`
	Views           []previsView           `json:"views"`
}

func previsFinite(n float64, min, max float64) bool {
	return !math.IsNaN(n) && !math.IsInf(n, 0) && n >= min && n <= max
}
func validPrevisVector(v previsVector) bool {
	return previsFinite(v.X, -100, 100) && previsFinite(v.Y, -100, 100) && previsFinite(v.Z, -100, 100)
}
func validatePrevisCamera(c previsCamera) bool {
	dx, dy, dz := c.Position.X-c.Target.X, c.Position.Y-c.Target.Y, c.Position.Z-c.Target.Z
	return validPrevisVector(c.Position) && validPrevisVector(c.Target) && previsFinite(c.FOV, 15, 100) && dx*dx+dy*dy+dz*dz > 0.0001
}
func validatePrevisDocument(doc previsDocument) error {
	if doc.SchemaVersion != 1 && doc.SchemaVersion != 2 && doc.SchemaVersion != 3 {
		return fmt.Errorf("不支持的预演版本")
	}
	if err := validatePrevisEnvironment(doc); err != nil {
		return err
	}
	if err := validatePrevisTimeline(doc); err != nil {
		return err
	}
	if len(doc.Objects) > 32 || len(doc.Views) > 12 {
		return fmt.Errorf("最多32个对象和12个机位")
	}
	if !validatePrevisCamera(doc.Camera) {
		return fmt.Errorf("机位坐标或视角无效")
	}
	ids := map[string]bool{}
	for _, o := range doc.Objects {
		if strings.TrimSpace(o.ID) == "" || len(o.ID) > 80 || ids[o.ID] || strings.TrimSpace(o.Name) == "" || len([]rune(o.Name)) > 80 {
			return fmt.Errorf("对象身份重复或名称无效")
		}
		ids[o.ID] = true
		if o.Kind != "character" && o.Kind != "prop" && o.Kind != "wall" && o.Kind != "building" && o.Kind != "tree" && o.Kind != "plant" {
			return fmt.Errorf("对象类型无效")
		}
		if !validPrevisVector(o.Position) || !previsFinite(o.Yaw, -360, 360) || !previsFinite(o.Width, 0.05, 30) || !previsFinite(o.Height, 0.05, 30) || !previsFinite(o.Depth, 0.05, 30) {
			return fmt.Errorf("对象%s尺寸或坐标无效", o.Name)
		}
	}
	names := map[string]bool{}
	for _, v := range doc.Views {
		if strings.TrimSpace(v.Name) == "" || len([]rune(v.Name)) > 80 || names[v.Name] || !validatePrevisCamera(v.Camera) {
			return fmt.Errorf("命名机位重复或无效")
		}
		names[v.Name] = true
	}
	return nil
}
func (s *Service) HandleGetScenePrevisualization(c *gin.Context) {
	sc, ok := s.loadScene(c)
	if !ok {
		return
	}
	var row models.ScenePrevisualization
	err := s.DB.Where("project_id = ? AND scene_id = ?", sc.ProjectID, sc.ID).First(&row).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		c.JSON(200, gin.H{"item": nil, "source_content_hash": hashString(sc.Content)})
		return
	}
	if err != nil {
		c.JSON(500, gin.H{"error": "读取预演失败"})
		return
	}
	c.JSON(200, gin.H{"item": row, "source_content_hash": hashString(sc.Content), "stale": row.SourceContentHash != hashString(sc.Content)})
}
func (s *Service) HandleSaveScenePrevisualization(c *gin.Context) {
	sc, ok := s.loadScene(c)
	if !ok {
		return
	}
	var req struct {
		Revision          uint           `json:"revision"`
		SourceContentHash string         `json:"source_content_hash"`
		Document          previsDocument `json:"document"`
	}
	c.Request.Body = http.MaxBytesReader(c.Writer, c.Request.Body, 128<<10)
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(400, gin.H{"error": "预演数据格式错误或超出限制"})
		return
	}
	if err := validatePrevisDocument(req.Document); err != nil {
		c.JSON(400, gin.H{"error": err.Error()})
		return
	}
	data, _ := json.Marshal(req.Document)
	err := s.DB.Transaction(func(tx *gorm.DB) error {
		var current models.Scene
		if err := tx.First(&current, sc.ID).Error; err != nil {
			return err
		}
		if req.SourceContentHash != hashString(current.Content) {
			return gorm.ErrInvalidData
		}
		if req.Revision == 0 {
			row := models.ScenePrevisualization{ProjectID: sc.ProjectID, SceneID: sc.ID, SchemaVersion: req.Document.SchemaVersion, Revision: 1, SourceContentHash: req.SourceContentHash, DocumentJSON: string(data)}
			result := tx.Clauses(clause.OnConflict{DoNothing: true}).Create(&row)
			if result.Error != nil {
				return result.Error
			}
			if result.RowsAffected != 1 {
				return gorm.ErrInvalidData
			}
			return nil
		}
		result := tx.Model(&models.ScenePrevisualization{}).Where("project_id = ? AND scene_id = ? AND revision = ?", sc.ProjectID, sc.ID, req.Revision).Updates(map[string]any{"schema_version": req.Document.SchemaVersion, "revision": req.Revision + 1, "document_json": string(data), "source_content_hash": req.SourceContentHash})
		if result.Error != nil {
			return result.Error
		}
		if result.RowsAffected != 1 {
			return gorm.ErrInvalidData
		}
		return nil
	})
	if errors.Is(err, gorm.ErrInvalidData) {
		c.JSON(409, gin.H{"error": "Scene正文或预演已被修改，请保留本地草稿，重新载入后再保存"})
		return
	}
	if err != nil {
		c.JSON(500, gin.H{"error": "保存预演失败"})
		return
	}
	c.JSON(200, gin.H{"ok": true, "revision": req.Revision + 1, "source_content_hash": req.SourceContentHash})
}
