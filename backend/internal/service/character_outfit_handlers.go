package service

import (
	"io"
	"net/http"
	"strconv"

	"github.com/gin-gonic/gin"
)

func outfitParam(c *gin.Context) (uint, bool) {
	id, err := strconv.ParseUint(c.Param("oid"), 10, 64)
	if err != nil || id == 0 {
		c.JSON(400, gin.H{"error": "无效套装 ID"})
		return 0, false
	}
	return uint(id), true
}

func characterParam(c *gin.Context) (uint, bool) {
	id, err := strconv.ParseUint(c.Param("cid"), 10, 64)
	if err != nil || id == 0 {
		c.JSON(400, gin.H{"error": "无效角色 ID"})
		return 0, false
	}
	return uint(id), true
}

func (s *Service) HandleListCharacterOutfits(c *gin.Context) {
	p, ok := s.loadProject(c)
	if !ok {
		return
	}
	cid, ok := characterParam(c)
	if !ok {
		return
	}
	rows, err := s.CharacterLooks.ListOutfits(p.ID, cid)
	if err != nil {
		c.JSON(500, gin.H{"error": err.Error()})
		return
	}
	c.JSON(200, rows)
}

func (s *Service) HandleDesignCharacterOutfit(c *gin.Context) {
	p, ok := s.loadProject(c)
	if !ok {
		return
	}
	cid, ok := characterParam(c)
	if !ok {
		return
	}
	var req OutfitDesignInput
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "参数错误"})
		return
	}
	row, err := s.CharacterLooks.DesignOutfit(p.ID, cid, req)
	if err != nil {
		c.JSON(http.StatusBadGateway, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusCreated, row)
}

func (s *Service) HandleCreateCharacterOutfit(c *gin.Context) {
	p, ok := s.loadProject(c)
	if !ok {
		return
	}
	cid, ok := characterParam(c)
	if !ok {
		return
	}
	var req OutfitInput
	if c.ShouldBindJSON(&req) != nil {
		c.JSON(400, gin.H{"error": "参数错误"})
		return
	}
	row, err := s.CharacterLooks.SaveOutfit(p.ID, cid, 0, req)
	if err != nil {
		c.JSON(400, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusCreated, row)
}

func (s *Service) HandleUpdateCharacterOutfit(c *gin.Context) {
	p, ok := s.loadProject(c)
	if !ok {
		return
	}
	cid, ok := characterParam(c)
	if !ok {
		return
	}
	oid, ok := outfitParam(c)
	if !ok {
		return
	}
	var req OutfitInput
	if c.ShouldBindJSON(&req) != nil {
		c.JSON(400, gin.H{"error": "参数错误"})
		return
	}
	row, err := s.CharacterLooks.SaveOutfit(p.ID, cid, oid, req)
	if err != nil {
		c.JSON(400, gin.H{"error": err.Error()})
		return
	}
	c.JSON(200, row)
}

func (s *Service) HandleDeleteCharacterOutfit(c *gin.Context) {
	p, ok := s.loadProject(c)
	if !ok {
		return
	}
	cid, ok := characterParam(c)
	if !ok {
		return
	}
	oid, ok := outfitParam(c)
	if !ok {
		return
	}
	if err := s.CharacterLooks.DeleteOutfit(p.ID, cid, oid); err != nil {
		c.JSON(400, gin.H{"error": err.Error()})
		return
	}
	c.Status(204)
}

func (s *Service) HandleApproveCharacterOutfit(c *gin.Context) {
	p, ok := s.loadProject(c)
	if !ok {
		return
	}
	cid, ok := characterParam(c)
	if !ok {
		return
	}
	oid, ok := outfitParam(c)
	if !ok {
		return
	}
	if err := s.CharacterLooks.ApproveOutfit(p.ID, cid, oid); err != nil {
		c.JSON(400, gin.H{"error": err.Error()})
		return
	}
	c.JSON(200, gin.H{"ok": true})
}

func (s *Service) HandleGenerateCharacterOutfitImage(c *gin.Context) {
	p, ok := s.loadProject(c)
	if !ok {
		return
	}
	cid, ok := characterParam(c)
	if !ok {
		return
	}
	oid, ok := outfitParam(c)
	if !ok {
		return
	}
	if err := s.CharacterLooks.StartOutfitImage(p.ID, cid, oid, false); err != nil {
		c.JSON(409, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusAccepted, gin.H{"ok": true})
}

func (s *Service) HandleUploadCharacterOutfitImage(c *gin.Context) {
	p, ok := s.loadProject(c)
	if !ok {
		return
	}
	cid, ok := characterParam(c)
	if !ok {
		return
	}
	oid, ok := outfitParam(c)
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
	if err := s.CharacterLooks.SaveOutfitUploadedImage(p.ID, cid, oid, header.Filename, data, c.Query("kind") == "sheet"); err != nil {
		c.JSON(400, gin.H{"error": err.Error()})
		return
	}
	c.JSON(200, gin.H{"ok": true})
}

func (s *Service) HandleGenerateCharacterOutfitSheet(c *gin.Context) {
	p, ok := s.loadProject(c)
	if !ok {
		return
	}
	cid, ok := characterParam(c)
	if !ok {
		return
	}
	oid, ok := outfitParam(c)
	if !ok {
		return
	}
	if err := s.CharacterLooks.StartOutfitImage(p.ID, cid, oid, true); err != nil {
		c.JSON(409, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusAccepted, gin.H{"ok": true})
}

func (s *Service) HandleGetSceneOutfits(c *gin.Context) {
	p, ok := s.loadProject(c)
	if !ok {
		return
	}
	sc, ok := s.loadScene(c)
	if !ok {
		return
	}
	rows, err := s.CharacterLooks.ListSceneOutfits(p.ID, sc.ID)
	if err != nil {
		c.JSON(500, gin.H{"error": err.Error()})
		return
	}
	c.JSON(200, rows)
}

func (s *Service) HandleAssignSceneOutfits(c *gin.Context) {
	p, ok := s.loadProject(c)
	if !ok {
		return
	}
	sc, ok := s.loadScene(c)
	if !ok {
		return
	}
	var req struct {
		Outfits []OutfitAssignment `json:"outfits"`
	}
	if c.ShouldBindJSON(&req) != nil {
		c.JSON(400, gin.H{"error": "参数错误"})
		return
	}
	if err := s.CharacterLooks.AssignSceneOutfits(p.ID, sc.ID, req.Outfits); err != nil {
		c.JSON(400, gin.H{"error": err.Error()})
		return
	}
	c.JSON(200, gin.H{"ok": true})
}

func (s *Service) HandleGetShotOutfits(c *gin.Context) {
	p, ok := s.loadProject(c)
	if !ok {
		return
	}
	shotID, err := strconv.ParseUint(c.Param("shid"), 10, 64)
	if err != nil {
		c.JSON(400, gin.H{"error": "无效镜头 ID"})
		return
	}
	rows, err := s.CharacterLooks.ListShotOutfits(p.ID, uint(shotID))
	if err != nil {
		c.JSON(500, gin.H{"error": err.Error()})
		return
	}
	c.JSON(200, rows)
}

func (s *Service) HandleAssignShotOutfits(c *gin.Context) {
	p, ok := s.loadProject(c)
	if !ok {
		return
	}
	shotID, err := strconv.ParseUint(c.Param("shid"), 10, 64)
	if err != nil {
		c.JSON(400, gin.H{"error": "无效镜头 ID"})
		return
	}
	var req struct {
		Outfits []OutfitAssignment `json:"outfits"`
	}
	if c.ShouldBindJSON(&req) != nil {
		c.JSON(400, gin.H{"error": "参数错误"})
		return
	}
	if err := s.CharacterLooks.AssignShotOutfits(p.ID, uint(shotID), req.Outfits); err != nil {
		c.JSON(400, gin.H{"error": err.Error()})
		return
	}
	c.JSON(200, gin.H{"ok": true})
}
