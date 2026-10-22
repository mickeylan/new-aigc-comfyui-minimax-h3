package service

import (
	"net/http"

	"comfyui-console/internal/models"
	"github.com/gin-gonic/gin"
)

func (s *Service) HandleExtensibilityStatus(c *gin.Context) {
	cfgEnabled := s.Cfg != nil
	var packages, toolAudits, edges, events int64
	_ = s.DB.Model(&models.SkillPackageInstall{}).Count(&packages).Error
	_ = s.DB.Model(&models.AgentToolAuditLog{}).Count(&toolAudits).Error
	_ = s.DB.Model(&models.ArtifactDependency{}).Count(&edges).Error
	_ = s.DB.Model(&models.ArtifactInvalidationEvent{}).Count(&events).Error
	capabilityCount, capabilityError := 0, ""
	if cfgEnabled && s.Cfg.Extensibility.EnableCapabilityManifests {
		if manifests, err := loadCapabilityManifests(s.Cfg.Extensibility.CapabilityManifestsDir); err != nil {
			capabilityError = err.Error()
		} else {
			capabilityCount = len(manifests)
		}
	}
	tools := []AgentToolDescriptor{}
	if s.AgentTools != nil {
		tools = s.AgentTools.List()
	}
	adapters := []GenerationAdapterDescriptor{}
	if s.GenerationAdapters != nil {
		adapters = s.GenerationAdapters.List()
	}
	c.JSON(http.StatusOK, gin.H{
		"skill_packages":       gin.H{"enabled": cfgEnabled && s.Cfg.Extensibility.EnableSkillPackages, "configured": cfgEnabled && s.Cfg.Extensibility.SkillPackagesDir != "", "installed_count": packages},
		"capability_manifests": gin.H{"enabled": cfgEnabled && s.Cfg.Extensibility.EnableCapabilityManifests, "configured": cfgEnabled && s.Cfg.Extensibility.CapabilityManifestsDir != "", "loaded_count": capabilityCount, "error": capabilityError},
		"agent_tools":          gin.H{"enabled": cfgEnabled && s.Cfg.Extensibility.EnableAgentTools, "registered_count": len(tools), "audit_count": toolAudits, "items": tools},
		"generation_adapters":  gin.H{"enabled": cfgEnabled && s.Cfg.Extensibility.EnableGenerationAdapters, "registered_count": len(adapters), "items": adapters},
		"dependency_graph":     gin.H{"enabled": cfgEnabled && s.Cfg.Extensibility.EnableDependencyGraph, "edge_count": edges, "invalidation_event_count": events},
	})
}

func (s *Service) HandleListSkillPackages(c *gin.Context) {
	var rows []models.SkillPackageInstall
	if err := s.DB.Order("code, version, id").Find(&rows).Error; err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	items := make([]gin.H, 0, len(rows))
	for _, row := range rows {
		var skill models.Skill
		if err := s.DB.Select("id", "enabled").First(&skill, row.SkillID).Error; err != nil {
			continue
		}
		items = append(items, gin.H{"skill_id": row.SkillID, "code": row.Code, "version": row.Version, "content_hash": row.ContentHash, "imported_at": row.ImportedAt, "enabled": skill.Enabled})
	}
	enabled := s.Cfg != nil && s.Cfg.Extensibility.EnableSkillPackages
	c.JSON(http.StatusOK, gin.H{"enabled": enabled, "items": items})
}

func (s *Service) HandleListAgentToolAudit(c *gin.Context) {
	query := s.DB.Model(&models.AgentToolAuditLog{}).Order("created_at DESC, id DESC").Limit(100)
	if tool := c.Query("tool_code"); tool != "" {
		query = query.Where("tool_code = ?", tool)
	}
	if project := c.Query("project_id"); project != "" {
		query = query.Where("project_id = ?", project)
	}
	var rows []models.AgentToolAuditLog
	if err := query.Find(&rows).Error; err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{"items": rows})
}
