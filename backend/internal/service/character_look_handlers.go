package service

import (
	"io"
	"net/http"
	"strconv"
	"strings"

	"github.com/gin-gonic/gin"

	"comfyui-console/internal/models"
)

func lookParam(c *gin.Context) (uint, bool) {
	v, err := strconv.ParseUint(c.Param("lid"), 10, 64)
	if err != nil || v == 0 {
		c.JSON(http.StatusBadRequest, gin.H{"error": "无效的造型 ID"})
		return 0, false
	}
	return uint(v), true
}
func (s *Service) loadCharacterLook(c *gin.Context) (*models.Project, *models.Character, *models.CharacterLook, bool) {
	p, ok := s.loadProject(c)
	if !ok {
		return nil, nil, nil, false
	}
	cid, err := strconv.ParseUint(c.Param("cid"), 10, 64)
	if err != nil {
		c.JSON(400, gin.H{"error": "无效的角色 ID"})
		return nil, nil, nil, false
	}
	var ch models.Character
	if s.DB.Where("id = ? AND project_id = ?", uint(cid), p.ID).First(&ch).Error != nil {
		c.JSON(404, gin.H{"error": "角色不存在"})
		return nil, nil, nil, false
	}
	lid, ok := lookParam(c)
	if !ok {
		return nil, nil, nil, false
	}
	var look models.CharacterLook
	if s.DB.Where("id = ? AND character_id = ? AND project_id = ?", lid, ch.ID, p.ID).First(&look).Error != nil {
		c.JSON(404, gin.H{"error": "造型不存在"})
		return nil, nil, nil, false
	}
	return p, &ch, &look, true
}
func (s *Service) HandleListCharacterLooks(c *gin.Context) {
	p, ok := s.loadProject(c)
	if !ok {
		return
	}
	cid, e := strconv.ParseUint(c.Param("cid"), 10, 64)
	if e != nil {
		c.JSON(400, gin.H{"error": "无效角色 ID"})
		return
	}
	var n int64
	s.DB.Model(&models.Character{}).Where("id=? AND project_id=?", uint(cid), p.ID).Count(&n)
	if n == 0 {
		c.JSON(404, gin.H{"error": "角色不存在"})
		return
	}
	v, e := s.CharacterLooks.ListByCharacter(uint(cid))
	if e != nil {
		c.JSON(500, gin.H{"error": e.Error()})
		return
	}
	c.JSON(200, v)
}
func (s *Service) HandleCreateCharacterLook(c *gin.Context) {
	p, ok := s.loadProject(c)
	if !ok {
		return
	}
	cid, e := strconv.ParseUint(c.Param("cid"), 10, 64)
	if e != nil {
		c.JSON(400, gin.H{"error": "无效角色 ID"})
		return
	}
	var ch models.Character
	if s.DB.Where("id=? AND project_id=?", uint(cid), p.ID).First(&ch).Error != nil {
		c.JSON(404, gin.H{"error": "角色不存在"})
		return
	}
	var req models.CharacterLook
	if c.ShouldBindJSON(&req) != nil {
		c.JSON(400, gin.H{"error": "参数错误"})
		return
	}
	req.ID = 0
	req.ProjectID = p.ID
	req.CharacterID = ch.ID
	v, e := s.CharacterLooks.CreateLook(req)
	if e != nil {
		c.JSON(400, gin.H{"error": e.Error()})
		return
	}
	c.JSON(201, v)
}
func (s *Service) HandlePreviewCharacterLookExpansion(c *gin.Context) {
	p, ok := s.loadProject(c)
	if !ok {
		return
	}
	cid, err := strconv.ParseUint(c.Param("cid"), 10, 64)
	if err != nil {
		c.JSON(400, gin.H{"error": "无效角色 ID"})
		return
	}
	var ch models.Character
	if s.DB.Where("id = ? AND project_id = ?", uint(cid), p.ID).First(&ch).Error != nil {
		c.JSON(404, gin.H{"error": "角色不存在"})
		return
	}
	var req models.CharacterLook
	if c.ShouldBindJSON(&req) != nil {
		c.JSON(400, gin.H{"error": "参数错误"})
		return
	}
	if strings.TrimSpace(req.Name) == "" || strings.TrimSpace(req.Description) == "" {
		c.JSON(400, gin.H{"error": "请填写造型名称和简单描述"})
		return
	}
	if req.Category != "" && !ValidLookCategories[req.Category] {
		c.JSON(400, gin.H{"error": "无效造型分类"})
		return
	}
	result, err := s.CharacterLooks.PreviewExpansion(&req, &ch, p)
	if err != nil {
		c.JSON(500, gin.H{"error": err.Error()})
		return
	}
	c.JSON(200, result)
}

func (s *Service) HandleGetCharacterLook(c *gin.Context) {
	_, _, v, ok := s.loadCharacterLook(c)
	if ok {
		c.JSON(200, v)
	}
}
func (s *Service) HandleUpdateCharacterLook(c *gin.Context) {
	_, _, v, ok := s.loadCharacterLook(c)
	if !ok {
		return
	}
	var req models.CharacterLook
	if c.ShouldBindJSON(&req) != nil {
		c.JSON(400, gin.H{"error": "参数错误"})
		return
	}
	out, e := s.CharacterLooks.UpdateLook(v.ID, req)
	if e != nil {
		c.JSON(400, gin.H{"error": e.Error()})
		return
	}
	c.JSON(200, out)
}
func (s *Service) HandleDeleteCharacterLook(c *gin.Context) {
	_, _, v, ok := s.loadCharacterLook(c)
	if !ok {
		return
	}
	if e := s.CharacterLooks.DeleteLook(v.ID); e != nil {
		c.JSON(500, gin.H{"error": e.Error()})
		return
	}
	c.JSON(200, gin.H{"ok": true})
}
func (s *Service) HandleExpandCharacterLookDescription(c *gin.Context) {
	p, ch, v, ok := s.loadCharacterLook(c)
	if !ok {
		return
	}
	out, e := s.CharacterLooks.ExpandDescription(v, ch, p)
	if e != nil {
		c.JSON(500, gin.H{"error": e.Error()})
		return
	}
	updated, _ := s.CharacterLooks.GetLook(v.ID)
	c.JSON(200, gin.H{"description": out, "prompt": updated.Prompt, "look": updated})
}
func (s *Service) HandleGenerateCharacterLookPrompt(c *gin.Context) {
	p, ch, v, ok := s.loadCharacterLook(c)
	if !ok {
		return
	}
	out, e := s.CharacterLooks.GeneratePrompt(v, ch, p)
	if e != nil {
		c.JSON(400, gin.H{"error": e.Error()})
		return
	}
	c.JSON(200, gin.H{"prompt": out})
}
func (s *Service) HandleGenerateCharacterLookImage(c *gin.Context) {
	_, _, v, ok := s.loadCharacterLook(c)
	if !ok {
		return
	}
	if err := s.CharacterLooks.StartImageGeneration(v); err != nil {
		c.JSON(400, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusAccepted, gin.H{"ok": true})
}
func (s *Service) HandleUploadCharacterLookImage(c *gin.Context) {
	_, _, v, ok := s.loadCharacterLook(c)
	if !ok {
		return
	}
	file, header, err := c.Request.FormFile("file")
	if err != nil {
		c.JSON(400, gin.H{"error": "请选择图片"})
		return
	}
	defer file.Close()
	data, err := io.ReadAll(io.LimitReader(file, 20<<20))
	if err != nil {
		c.JSON(500, gin.H{"error": err.Error()})
		return
	}
	if err := s.CharacterLooks.SaveUploadedImage(v, header.Filename, data); err != nil {
		c.JSON(400, gin.H{"error": err.Error()})
		return
	}
	c.JSON(200, gin.H{"ok": true})
}
func (s *Service) HandleApproveCharacterLook(c *gin.Context) {
	_, _, v, ok := s.loadCharacterLook(c)
	if !ok {
		return
	}
	var req struct {
		Note string `json:"note"`
	}
	_ = c.ShouldBindJSON(&req)
	if e := s.CharacterLooks.ApproveLook(v.ID, req.Note); e != nil {
		c.JSON(400, gin.H{"error": e.Error()})
		return
	}
	c.JSON(200, gin.H{"ok": true})
}
func (s *Service) HandleRejectCharacterLook(c *gin.Context) {
	_, _, v, ok := s.loadCharacterLook(c)
	if !ok {
		return
	}
	var req struct {
		Reason string `json:"reason"`
	}
	if c.ShouldBindJSON(&req) != nil || strings.TrimSpace(req.Reason) == "" {
		c.JSON(400, gin.H{"error": "请填写驳回原因"})
		return
	}
	if e := s.CharacterLooks.RejectLook(v.ID, req.Reason); e != nil {
		c.JSON(500, gin.H{"error": e.Error()})
		return
	}
	c.JSON(200, gin.H{"ok": true})
}
func (s *Service) HandlePublishCharacterLook(c *gin.Context) {
	_, _, v, ok := s.loadCharacterLook(c)
	if !ok {
		return
	}
	if e := s.CharacterLooks.PublishLook(v.ID); e != nil {
		c.JSON(400, gin.H{"error": e.Error()})
		return
	}
	c.JSON(200, gin.H{"ok": true})
}

func (s *Service) HandleGetSceneLooks(c *gin.Context) {
	sc, ok := s.loadScene(c)
	if !ok {
		return
	}
	v, e := s.CharacterLooks.GetSceneLooks(sc.ID)
	if e != nil {
		c.JSON(500, gin.H{"error": e.Error()})
		return
	}
	c.JSON(200, v)
}
func (s *Service) HandleAssignSceneLooks(c *gin.Context) {
	sc, ok := s.loadScene(c)
	if !ok {
		return
	}
	var req struct {
		Looks []SceneLookAssignmentRequest `json:"looks"`
	}
	if c.ShouldBindJSON(&req) != nil {
		c.JSON(400, gin.H{"error": "参数错误"})
		return
	}
	if e := s.CharacterLooks.AssignLooksToScene(sc.ID, req.Looks); e != nil {
		c.JSON(400, gin.H{"error": e.Error()})
		return
	}
	c.JSON(200, gin.H{"ok": true})
}
func (s *Service) HandleGetShotLooks(c *gin.Context) {
	p, ok := s.loadProject(c)
	if !ok {
		return
	}
	id, e := strconv.ParseUint(c.Param("shid"), 10, 64)
	if e != nil {
		c.JSON(400, gin.H{"error": "无效镜头 ID"})
		return
	}
	var shot models.Shot
	if s.DB.Joins("JOIN scenes ON scenes.id = shots.scene_id").Where("shots.id=? AND scenes.project_id=?", uint(id), p.ID).First(&shot).Error != nil {
		c.JSON(404, gin.H{"error": "镜头不存在"})
		return
	}
	v, e := s.CharacterLooks.GetShotLooks(shot.ID)
	if e != nil {
		c.JSON(500, gin.H{"error": e.Error()})
		return
	}
	c.JSON(200, v)
}
func (s *Service) HandleAssignShotLooks(c *gin.Context) {
	p, ok := s.loadProject(c)
	if !ok {
		return
	}
	id, e := strconv.ParseUint(c.Param("shid"), 10, 64)
	if e != nil {
		c.JSON(400, gin.H{"error": "无效镜头 ID"})
		return
	}
	var shot models.Shot
	if s.DB.Joins("JOIN scenes ON scenes.id=shots.scene_id").Where("shots.id=? AND scenes.project_id=?", uint(id), p.ID).First(&shot).Error != nil {
		c.JSON(404, gin.H{"error": "镜头不存在"})
		return
	}
	var req struct {
		Looks []ShotLookAssignmentRequest `json:"looks"`
	}
	if c.ShouldBindJSON(&req) != nil {
		c.JSON(400, gin.H{"error": "参数错误"})
		return
	}
	if e := s.CharacterLooks.AssignLooksToShot(shot.ID, req.Looks); e != nil {
		c.JSON(400, gin.H{"error": e.Error()})
		return
	}
	c.JSON(200, gin.H{"ok": true})
}
