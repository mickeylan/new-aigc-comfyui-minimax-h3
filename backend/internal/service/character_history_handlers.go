package service

import (
	"net/http"
	"strconv"

	"github.com/gin-gonic/gin"
	"gorm.io/gorm"
)

// HandleGetCharacterHistory returns a live projection for all project characters,
// or one character when character_id is supplied.
func (s *Service) HandleGetCharacterHistory(c *gin.Context) {
	project, ok := s.loadProject(c)
	if !ok {
		return
	}
	var characterID *uint
	if raw := c.Query("character_id"); raw != "" {
		id, err := strconv.ParseUint(raw, 10, 32)
		if err != nil || id == 0 {
			c.JSON(http.StatusBadRequest, gin.H{"error": "invalid character_id"})
			return
		}
		value := uint(id)
		characterID = &value
	}
	histories, err := NewCharacterHistoryService(s.DB).List(project.ID, characterID)
	if err != nil {
		if err == gorm.ErrRecordNotFound {
			c.JSON(http.StatusNotFound, gin.H{"error": "character not found"})
			return
		}
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{"characters": histories})
}
