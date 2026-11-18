package service

import (
	"encoding/json"
	"fmt"
	"net/http"
	"time"

	"comfyui-console/internal/models"
	"github.com/gin-gonic/gin"
)

func (s *TaskService) recoveryState(task *models.Task, outcome, note string) {
	now := time.Now()
	updates := map[string]any{"provider_outcome": outcome, "recovery_note": note, "last_reconciled_at": now}
	// Recovery evidence must never resurrect a terminal task.
	query := s.db.Model(&models.Task{}).Where("id = ? AND status IN ?", task.ID, []string{"pending", "queued", "running"})
	// A query failure or queue acknowledgement must not erase an explicit stop
	// request. Only confirmed completion/cancellation supersedes that request.
	if outcome == "unknown" || outcome == "accepted" || outcome == "submitting" {
		query = query.Where("COALESCE(provider_outcome, '') != 'cancel_requested'")
	}
	result := query.Updates(updates)
	if result.Error == nil && result.RowsAffected > 0 {
		if s.db.First(task, task.ID).Error == nil {
			s.push(task)
		}
	}
}

// promptQueueState queries this exact upstream identity, not global queue counts.
func promptQueueState(c *ComfyClient, id string) (string, error) {
	resp, err := c.HTTP.Get(c.baseURL() + "/queue")
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("查询队列 HTTP %d", resp.StatusCode)
	}
	var queue map[string]json.RawMessage
	if err := json.NewDecoder(resp.Body).Decode(&queue); err != nil {
		return "", err
	}
	for _, key := range []string{"queue_running", "queue_pending"} {
		raw, ok := queue[key]
		if !ok {
			return "", fmt.Errorf("上游不支持按任务查询队列")
		}
		var entries [][]json.RawMessage
		if err := json.Unmarshal(raw, &entries); err != nil {
			return "", err
		}
		for _, entry := range entries {
			if len(entry) < 2 {
				return "", fmt.Errorf("无法识别上游队列身份")
			}
			var prompt string
			if json.Unmarshal(entry[1], &prompt) != nil {
				return "", fmt.Errorf("无法识别上游任务身份")
			}
			if prompt == id {
				return key, nil
			}
		}
	}
	return "absent", nil
}

func (s *Service) HandleReconcileTask(c *gin.Context) {
	id := c.Param("id")
	var task models.Task
	if s.DB.Where("task_id = ?", id).First(&task).Error != nil {
		c.JSON(404, gin.H{"error": "任务不存在"})
		return
	}
	if task.Status == "pending" || task.Status == "queued" || task.Status == "running" {
		s.Tasks.reconcile(id)
	} else if task.Status != "cancelled" {
		s.Tasks.RefreshTaskResult(id)
	}
	if err := s.DB.Where("task_id = ?", id).First(&task).Error; err != nil {
		c.JSON(500, gin.H{"error": "无法读取任务"})
		return
	}
	c.JSON(200, task)
}
