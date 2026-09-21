package service

import (
	"errors"
	"net/http"
	"strings"

	"github.com/gin-gonic/gin"
)

func (s *Service) HandleListQwenImagePromptPrograms(c *gin.Context) {
	c.JSON(http.StatusOK, gin.H{"items": s.QwenImagePromptPrograms.List()})
}

func (s *Service) HandleGenerateQwenImagePrompt(c *gin.Context) {
	if s.QwenImagePromptPrograms == nil {
		c.JSON(http.StatusServiceUnavailable, gin.H{"error": "提示词程序未初始化"})
		return
	}
	var req PromptProgramInput
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "参数错误"})
		return
	}
	result, err := s.QwenImagePromptPrograms.Generate(c.Param("code"), req)
	if err != nil {
		status := http.StatusUnprocessableEntity
		if errors.Is(err, ErrPromptProgramNotFound) {
			status = http.StatusNotFound
		}
		if errors.Is(err, ErrMultimodalUnsupported) || strings.HasPrefix(err.Error(), "提示词生成失败") {
			status = http.StatusBadGateway
		}
		c.JSON(status, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, result)
}
