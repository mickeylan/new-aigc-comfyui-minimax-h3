package service

import (
	"io"
	"net/http"
	"strconv"

	"github.com/gin-gonic/gin"
)

func ageStageParam(c *gin.Context) (uint, bool) {
	id, err := strconv.ParseUint(c.Param("asid"), 10, 64)
	if err != nil || id == 0 {
		c.JSON(400, gin.H{"error": "无效年龄阶段 ID"})
		return 0, false
	}
	return uint(id), true
}

// loadAgeStageRoute 解析项目、角色与可选的年龄阶段路径参数并校验归属。
func (s *Service) loadAgeStageRoute(c *gin.Context, needStage bool) (projectID, characterID, stageID uint, ok bool) {
	p, ok := s.loadProject(c)
	if !ok {
		return 0, 0, 0, false
	}
	cid, ok := characterParam(c)
	if !ok {
		return 0, 0, 0, false
	}
	if !s.characterBelongsToProject(p.ID, cid) {
		c.JSON(http.StatusNotFound, gin.H{"error": "角色不存在"})
		return 0, 0, 0, false
	}
	if needStage {
		if stageID, ok = ageStageParam(c); !ok {
			return 0, 0, 0, false
		}
	}
	return p.ID, cid, stageID, true
}

func (s *Service) HandleListCharacterAgeStages(c *gin.Context) {
	pid, cid, _, ok := s.loadAgeStageRoute(c, false)
	if !ok {
		return
	}
	rows, err := s.CharacterLooks.ListAgeStages(pid, cid)
	if err != nil {
		c.JSON(500, gin.H{"error": err.Error()})
		return
	}
	c.JSON(200, rows)
}

func (s *Service) HandleCreateCharacterAgeStage(c *gin.Context) {
	pid, cid, _, ok := s.loadAgeStageRoute(c, false)
	if !ok {
		return
	}
	var req AgeStageInput
	if c.ShouldBindJSON(&req) != nil {
		c.JSON(400, gin.H{"error": "参数错误"})
		return
	}
	row, err := s.CharacterLooks.SaveAgeStage(pid, cid, 0, req)
	if err != nil {
		c.JSON(400, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusCreated, row)
}

func (s *Service) HandleUpdateCharacterAgeStage(c *gin.Context) {
	pid, cid, sid, ok := s.loadAgeStageRoute(c, true)
	if !ok {
		return
	}
	var req AgeStageInput
	if c.ShouldBindJSON(&req) != nil {
		c.JSON(400, gin.H{"error": "参数错误"})
		return
	}
	row, err := s.CharacterLooks.SaveAgeStage(pid, cid, sid, req)
	if err != nil {
		c.JSON(400, gin.H{"error": err.Error()})
		return
	}
	c.JSON(200, row)
}

func (s *Service) HandleDeleteCharacterAgeStage(c *gin.Context) {
	pid, cid, sid, ok := s.loadAgeStageRoute(c, true)
	if !ok {
		return
	}
	if err := s.CharacterLooks.DeleteAgeStage(pid, cid, sid); err != nil {
		c.JSON(400, gin.H{"error": err.Error()})
		return
	}
	c.Status(204)
}

func (s *Service) HandleApproveCharacterAgeStage(c *gin.Context) {
	pid, cid, sid, ok := s.loadAgeStageRoute(c, true)
	if !ok {
		return
	}
	if err := s.CharacterLooks.ApproveAgeStage(pid, cid, sid); err != nil {
		c.JSON(400, gin.H{"error": err.Error()})
		return
	}
	c.JSON(200, gin.H{"ok": true})
}

func (s *Service) HandlePreviewCharacterAgeStagePortraitPrompt(c *gin.Context) {
	pid, cid, sid, ok := s.loadAgeStageRoute(c, true)
	if !ok {
		return
	}
	saved, draft, err := s.CharacterLooks.AgeStagePortraitPrompt(pid, cid, sid)
	if err != nil {
		c.JSON(400, gin.H{"error": err.Error()})
		return
	}
	c.JSON(200, gin.H{"saved_prompt": saved, "draft_prompt": draft})
}

func (s *Service) HandleGenerateCharacterAgeStagePortrait(c *gin.Context) {
	pid, cid, sid, ok := s.loadAgeStageRoute(c, true)
	if !ok {
		return
	}
	var req struct {
		Prompt string `json:"prompt"`
	}
	if c.ShouldBindJSON(&req) != nil {
		c.JSON(400, gin.H{"error": "参数错误"})
		return
	}
	if err := s.CharacterLooks.StartAgeStagePortrait(pid, cid, sid, req.Prompt); err != nil {
		c.JSON(409, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusAccepted, gin.H{"ok": true})
}

func (s *Service) HandleUploadCharacterAgeStagePortrait(c *gin.Context) {
	pid, cid, sid, ok := s.loadAgeStageRoute(c, true)
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
	if err := s.CharacterLooks.SaveAgeStageUploadedPortrait(pid, cid, sid, header.Filename, data); err != nil {
		c.JSON(400, gin.H{"error": err.Error()})
		return
	}
	c.JSON(200, gin.H{"ok": true})
}
