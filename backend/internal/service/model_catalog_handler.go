package service

import (
	"net/http"

	"github.com/gin-gonic/gin"
)

// HandleListModelCatalog exposes a compact read model without changing template APIs.
func (s *Service) HandleListModelCatalog(c *gin.Context) {
	entries, err := NewModelCatalogService(s.DB).List()
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{"items": entries})
}

// HandleModelCapabilities exposes the system-wide duration and reference limits.
func (s *Service) HandleModelCapabilities(c *gin.Context) {
	c.JSON(http.StatusOK, modelCapabilityLimits(s.DB))
}
