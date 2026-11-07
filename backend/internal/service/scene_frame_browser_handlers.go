package service

import (
	"net/http"

	"github.com/gin-gonic/gin"
)

func (s *Service) HandleExtractAllSceneFrames(c *gin.Context) {
	projectID, sceneID, ok := continuityIDs(c)
	if !ok {
		return
	}
	result, err := s.Continuity.ExtractAllSceneFrames(projectID, sceneID)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, result)
}

func (s *Service) HandleSaveExtractedSceneFrame(c *gin.Context) {
	projectID, sceneID, ok := continuityIDs(c)
	if !ok {
		return
	}
	var request struct {
		ExtractionID string `json:"extraction_id"`
		FrameIndex   int    `json:"frame_index"`
	}
	if err := c.ShouldBindJSON(&request); err != nil || request.ExtractionID == "" || request.FrameIndex < 0 {
		c.JSON(http.StatusBadRequest, gin.H{"error": "请选择要保存的有效视频帧"})
		return
	}
	saved, err := s.Continuity.SaveExtractedSceneFrame(projectID, sceneID, request.ExtractionID, request.FrameIndex)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{"frame": saved, "message": "所选帧已保存到项目磁盘目录"})
}
