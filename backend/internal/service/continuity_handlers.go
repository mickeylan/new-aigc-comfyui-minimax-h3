package service

import (
	"io"
	"net/http"
	"strconv"

	"github.com/gin-gonic/gin"

	"comfyui-console/internal/models"
)

func pathUint(c *gin.Context, key string) (uint, bool) {
	n, err := strconv.ParseUint(c.Param(key), 10, 64)
	if err != nil || n == 0 {
		c.JSON(http.StatusBadRequest, gin.H{"error": "参数错误: " + key})
		return 0, false
	}
	return uint(n), true
}
func continuityIDs(c *gin.Context) (uint, uint, bool) {
	pid, ok := pathUint(c, "id")
	if !ok {
		return 0, 0, false
	}
	sid, ok := pathUint(c, "sid")
	return pid, sid, ok
}

func (s *Service) HandleExtractFrameCandidates(c *gin.Context) {
	pid, sid, ok := continuityIDs(c)
	if !ok {
		return
	}
	frames, err := s.Continuity.ExtractFrameCandidates(pid, sid, DefaultCandidateFrameCount)
	if err != nil {
		c.JSON(400, gin.H{"error": err.Error()})
		return
	}
	c.JSON(200, gin.H{"frames": s.frameResponses(frames)})
}
func (s *Service) HandleGetFrameCandidates(c *gin.Context) {
	pid, sid, ok := continuityIDs(c)
	if !ok {
		return
	}
	frames, err := s.Continuity.ListFrames(pid, sid)
	if err != nil {
		c.JSON(500, gin.H{"error": err.Error()})
		return
	}
	c.JSON(200, gin.H{"frames": s.frameResponses(frames)})
}
func (s *Service) frameResponses(frames []models.FrameCandidate) []gin.H {
	out := make([]gin.H, 0, len(frames))
	for _, f := range frames {
		out = append(out, gin.H{"id": f.ID, "scene_id": f.SceneID, "video_task_id": f.VideoTaskID, "frame_index": f.FrameIndex, "timestamp_ms": f.TimestampMS, "image_file": f.ImageFile, "original_image_file": f.OriginalImageFile, "source": f.Source, "image_url": s.Continuity.FrameURL(f), "selected": f.Type == models.FrameCandidateSelected})
	}
	return out
}
func (s *Service) HandleSelectFrame(c *gin.Context) {
	pid, sid, ok := continuityIDs(c)
	if !ok {
		return
	}
	var req struct {
		FrameID uint `json:"frame_id"`
	}
	if c.ShouldBindJSON(&req) != nil || req.FrameID == 0 {
		c.JSON(400, gin.H{"error": "frame_id必填"})
		return
	}
	frame, err := s.Continuity.SelectFrame(pid, sid, req.FrameID)
	if err != nil {
		c.JSON(400, gin.H{"error": err.Error()})
		return
	}
	c.JSON(200, gin.H{"frame": s.frameResponses([]models.FrameCandidate{*frame})[0]})
}
func (s *Service) HandleReplaceSelectedFrame(c *gin.Context) {
	pid, sid, ok := continuityIDs(c)
	if !ok {
		return
	}
	file, header, err := c.Request.FormFile("file")
	if err != nil {
		c.JSON(400, gin.H{"error": "请选择高清化图片"})
		return
	}
	defer file.Close()
	data, err := io.ReadAll(io.LimitReader(file, 25<<20))
	if err != nil {
		c.JSON(500, gin.H{"error": err.Error()})
		return
	}
	frame, err := s.Continuity.ReplaceSelectedFrame(pid, sid, header.Filename, data)
	if err != nil {
		c.JSON(400, gin.H{"error": err.Error()})
		return
	}
	c.JSON(200, gin.H{"frame": s.frameResponses([]models.FrameCandidate{*frame})[0]})
}

func (s *Service) HandleGetContinuity(c *gin.Context) {
	pid, sid, ok := continuityIDs(c)
	if !ok {
		return
	}
	cfg, err := s.Continuity.Get(pid, sid)
	if err != nil {
		c.JSON(500, gin.H{"error": err.Error()})
		return
	}
	c.JSON(200, gin.H{"continuity": cfg})
}
func (s *Service) HandleConfigureContinuity(c *gin.Context) {
	pid, sid, ok := continuityIDs(c)
	if !ok {
		return
	}
	var req ConfigureContinuityRequest
	if c.ShouldBindJSON(&req) != nil {
		c.JSON(400, gin.H{"error": "参数错误"})
		return
	}
	cfg, err := s.Continuity.Configure(pid, sid, req)
	if err != nil {
		c.JSON(400, gin.H{"error": err.Error()})
		return
	}
	c.JSON(200, gin.H{"continuity": cfg})
}
