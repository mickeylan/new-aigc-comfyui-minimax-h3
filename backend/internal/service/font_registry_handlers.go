package service

import (
	"net/http"

	"github.com/gin-gonic/gin"
)

func (s *Service) HandleListApprovedFonts(c *gin.Context) {
	if s.Fonts == nil {
		c.JSON(http.StatusOK, gin.H{"fonts": []FontRegistryEntry{}, "approved_licenses": []string{"OFL-1.1", "Apache-2.0"}, "formal_export_requires_verified_font": true})
		return
	}
	fonts, err := s.Fonts.List()
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{
		"fonts":                                fonts,
		"approved_licenses":                    []string{"OFL-1.1", "Apache-2.0"},
		"formal_export_requires_verified_font": true,
	})
}
