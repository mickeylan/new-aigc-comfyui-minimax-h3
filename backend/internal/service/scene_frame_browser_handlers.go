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
		ExtractionID    string `json:"extraction_id"`
		FrameIndex      int    `json:"frame_index"`
		SetAsSceneImage bool   `json:"set_as_scene_image"`
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
	message := "所选帧已以 PNG 保存到项目磁盘目录"
	if request.SetAsSceneImage {
		scene, ok := s.loadScene(c)
		if !ok {
			return
		}
		if s.Projects == nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": "项目服务未配置"})
			return
		}
		if err := s.Projects.ReplaceSceneImage(scene, saved.File); err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
			return
		}
		message = "所选帧已以 PNG 保存并设为当前分镜图；原分镜图和历史候选均保留"
	}
	c.JSON(http.StatusOK, gin.H{"frame": saved, "set_as_scene_image": request.SetAsSceneImage, "message": message})
}
