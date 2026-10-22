package service

import (
	"net/http"

	"github.com/gin-gonic/gin"
)

// HandleListGenerationAdapters exposes readiness only. It deliberately has no submit endpoint.
func (s *Service) HandleListGenerationAdapters(c *gin.Context) {
	if s.GenerationAdapters == nil || s.Cfg == nil || !s.Cfg.Extensibility.EnableGenerationAdapters {
		c.JSON(http.StatusOK, gin.H{"enabled": false, "items": []GenerationAdapterDescriptor{}, "message": "generation adapters are disabled; existing template task paths are unchanged"})
		return
	}
	c.JSON(http.StatusOK, gin.H{"enabled": true, "items": s.GenerationAdapters.List()})
}
