package service

import (
	"regexp"
	"strings"
	"time"

	"comfyui-console/internal/models"
	"github.com/gin-gonic/gin"
)

var diagnosticSecrets = []*regexp.Regexp{
	regexp.MustCompile(`(?i)(authorization|api[-_ ]?key|token|password)\s*[:=]\s*[^\s,;]+`),
	regexp.MustCompile(`(?i)bearer\s+[a-z0-9._~+/=-]+`),
	regexp.MustCompile(`(?i)([?&](?:key|token|signature|secret)=)[^&\s]+`),
}

func sanitizeDiagnosticText(value string) string {
	value = strings.TrimSpace(value)
	for _, re := range diagnosticSecrets {
		value = re.ReplaceAllString(value, "[redacted]")
	}
	if len(value) > 1000 {
		value = value[:1000] + "…"
	}
	return value
}

type taskDiagnosticEvent struct {
	Type      string    `json:"type"`
	Node      string    `json:"node,omitempty"`
	Progress  float64   `json:"progress"`
	Message   string    `json:"message,omitempty"`
	CreatedAt time.Time `json:"created_at"`
}

func taskElapsedMS(task *models.Task, now time.Time) int64 {
	start := task.CreatedAt
	if task.StartedAt != nil {
		start = *task.StartedAt
	}
	end := now
	if task.FinishedAt != nil {
		end = *task.FinishedAt
	}
	if end.Before(start) {
		return 0
	}
	return end.Sub(start).Milliseconds()
}

// HandleTaskDiagnostics returns operational identifiers and timing only. Prompt,
// input and parameter payloads are intentionally excluded and event text is redacted.
func (s *Service) HandleTaskDiagnostics(c *gin.Context) {
	var task models.Task
	if err := s.DB.Where("task_id = ?", c.Param("id")).First(&task).Error; err != nil {
		c.JSON(404, gin.H{"error": "task not found"})
		return
	}
	var events []models.Event
	_ = s.DB.Where("task_id = ?", task.TaskID).Order("id DESC").Limit(50).Find(&events).Error
	outEvents := make([]taskDiagnosticEvent, 0, len(events))
	for i := len(events) - 1; i >= 0; i-- {
		outEvents = append(outEvents, taskDiagnosticEvent{Type: events[i].Type, Node: events[i].Node, Progress: events[i].Progress, Message: sanitizeDiagnosticText(events[i].Message), CreatedAt: events[i].CreatedAt})
	}
	c.JSON(200, gin.H{
		"task_id": task.TaskID, "status": task.Status, "progress": task.Progress,
		"elapsed_ms": taskElapsedMS(&task, time.Now()), "created_at": task.CreatedAt,
		"started_at": task.StartedAt, "finished_at": task.FinishedAt,
		"provider_ids": gin.H{"template_id": task.TemplateID, "instance_id": task.InstanceID, "gpu_index": task.GPUIndex, "port": task.Port, "comfy_prompt_id": task.ComfyPromptID},
		"current_node": task.CurrentNode, "error": sanitizeDiagnosticText(task.Error), "events": outEvents,
	})
}
