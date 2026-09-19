package service

import (
	"errors"
	"net/http"
	"strconv"

	"github.com/gin-gonic/gin"
)

func (s *Service) HandleListSharedAssetReferences(c *gin.Context) {
	project, ok := s.loadProject(c)
	if !ok {
		return
	}
	refs, err := s.SharedAssetReferences.List(project.ID)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, refs)
}

func (s *Service) HandleListEffectiveSharedAssets(c *gin.Context) {
	project, ok := s.loadProject(c)
	if !ok {
		return
	}
	assets, err := s.SharedAssetReferences.ListEffective(project.ID)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, assets)
}

func (s *Service) HandleCreateSharedAssetReference(c *gin.Context) {
	project, ok := s.loadProject(c)
	if !ok {
		return
	}
	var input SharedAssetReferenceInput
	if err := c.ShouldBindJSON(&input); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	ref, err := s.SharedAssetReferences.Create(project.ID, input)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusCreated, ref)
}

func (s *Service) HandleUpdateSharedAssetReference(c *gin.Context) {
	project, ok := s.loadProject(c)
	if !ok {
		return
	}
	id, err := strconv.ParseUint(c.Param("rid"), 10, 32)
	if err != nil || id == 0 {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid reference id"})
		return
	}
	var input SharedAssetReferenceInput
	if err := c.ShouldBindJSON(&input); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	ref, err := s.SharedAssetReferences.Update(project.ID, uint(id), input)
	if err != nil {
		if errors.Is(err, ErrSharedAssetReferenceNotFound) {
			c.JSON(http.StatusNotFound, gin.H{"error": err.Error()})
			return
		}
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, ref)
}

func (s *Service) HandleDeleteSharedAssetReference(c *gin.Context) {
	project, ok := s.loadProject(c)
	if !ok {
		return
	}
	id, err := strconv.ParseUint(c.Param("rid"), 10, 32)
	if err != nil || id == 0 {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid reference id"})
		return
	}
	if err := s.SharedAssetReferences.Delete(project.ID, uint(id)); err != nil {
		if errors.Is(err, ErrSharedAssetReferenceNotFound) {
			c.JSON(http.StatusNotFound, gin.H{"error": err.Error()})
			return
		}
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{"ok": true})
}
