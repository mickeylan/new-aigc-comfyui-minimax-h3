package service

import (
	"errors"
	"net/http"
	"strconv"

	"github.com/gin-gonic/gin"
)

func (s *Service) HandleListScreenTextCues(c *gin.Context) {
	project, ok := s.loadProject(c)
	if !ok {
		return
	}
	episodeN, _ := strconv.Atoi(c.Query("episode_n"))
	cues, err := s.ScreenTexts.List(project.ID, episodeN)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, cues)
}

func (s *Service) HandleCreateScreenTextCue(c *gin.Context) {
	project, ok := s.loadProject(c)
	if !ok {
		return
	}
	var input ScreenTextCueInput
	if err := c.ShouldBindJSON(&input); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "参数错误"})
		return
	}
	cue, err := s.ScreenTexts.Create(project.ID, input)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusCreated, cue)
}

func (s *Service) HandleUpdateScreenTextCue(c *gin.Context) {
	project, ok := s.loadProject(c)
	if !ok {
		return
	}
	id, err := strconv.ParseUint(c.Param("cid"), 10, 32)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid screen text cue id"})
		return
	}
	var input ScreenTextCueInput
	if err := c.ShouldBindJSON(&input); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "参数错误"})
		return
	}
	cue, err := s.ScreenTexts.Update(project.ID, uint(id), input)
	if err != nil {
		status := http.StatusBadRequest
		if errors.Is(err, ErrScreenTextCueNotFound) {
			status = http.StatusNotFound
		}
		c.JSON(status, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, cue)
}

func (s *Service) HandleDeleteScreenTextCue(c *gin.Context) {
	project, ok := s.loadProject(c)
	if !ok {
		return
	}
	id, err := strconv.ParseUint(c.Param("cid"), 10, 32)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid screen text cue id"})
		return
	}
	if err := s.ScreenTexts.Delete(project.ID, uint(id)); err != nil {
		status := http.StatusInternalServerError
		if errors.Is(err, ErrScreenTextCueNotFound) {
			status = http.StatusNotFound
		}
		c.JSON(status, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{"ok": true})
}
