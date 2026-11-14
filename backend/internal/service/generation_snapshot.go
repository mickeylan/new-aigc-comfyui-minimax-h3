package service

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"io"
	"log"
	"net/http"
	"sort"

	"comfyui-console/internal/models"
	"github.com/gin-gonic/gin"
)

type generationSnapshotRef struct {
	Role   string `json:"role"`
	File   string `json:"file"`
	SHA256 string `json:"sha256"`
}

func sha256Hex(data []byte) string {
	sum := sha256.Sum256(data)
	return hex.EncodeToString(sum[:])
}

// fileSHA256 hashes a registered project input; an unreadable file yields an empty hash
// rather than failing, because the snapshot must never block a submission.
func (s *ProjectService) fileSHA256(ref FileMeta) string {
	if s.upload == nil || s.upload.remote == nil {
		return ""
	}
	f, err := s.upload.remote.Open(s.upload.ResolveReadPath(ref.TaskID, ref.Name))
	if err != nil {
		return ""
	}
	defer f.Close()
	h := sha256.New()
	if _, err := io.Copy(h, io.LimitReader(f, 512<<20)); err != nil {
		return ""
	}
	return hex.EncodeToString(h.Sum(nil))
}

// recordGenerationSnapshot stores the inputs of a task that was just created. Failures
// are logged only: provenance is diagnostic and must not stop a paid generation.
func (s *ProjectService) recordGenerationSnapshot(sc *models.Scene, kind string, task *models.Task, tpl *models.Template, files map[string][]FileMeta) {
	if task == nil || tpl == nil {
		return
	}
	roles := make([]string, 0, len(files))
	for role := range files {
		roles = append(roles, role)
	}
	sort.Strings(roles)
	refs := []generationSnapshotRef{}
	for _, role := range roles {
		for _, ref := range files[role] {
			refs = append(refs, generationSnapshotRef{Role: role, File: ref.TaskID + "/" + ref.Name, SHA256: s.fileSHA256(ref)})
		}
	}
	refsJSON, _ := json.Marshal(refs)
	row := models.GenerationSnapshot{
		ProjectID: sc.ProjectID, SceneID: sc.ID, Kind: kind, TaskID: task.TaskID, TemplateCode: tpl.Code,
		TemplateSHA256: sha256Hex([]byte(tpl.WorkflowJSON + "\x00" + tpl.InputsJSON)),
		PromptSHA256:   sha256Hex([]byte(task.Prompt)), FullPrompt: task.Prompt,
		RefsJSON: string(refsJSON), ParamsJSON: task.ParamsJSON,
	}
	if err := s.db.Create(&row).Error; err != nil {
		log.Printf("[generation-snapshot] scene=%d task=%s kind=%s failed: %v", sc.ID, task.TaskID, kind, err)
	}
}

// HandleListGenerationSnapshots returns the newest snapshots first; the UI compares the
// first two to show what changed between consecutive takes.
func (s *Service) HandleListGenerationSnapshots(c *gin.Context) {
	scene, ok := s.loadScene(c)
	if !ok {
		return
	}
	query := s.DB.Where("scene_id = ?", scene.ID)
	if kind := c.Query("kind"); kind != "" {
		query = query.Where("kind = ?", kind)
	}
	var rows []models.GenerationSnapshot
	if err := query.Order("id DESC").Limit(20).Find(&rows).Error; err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{"items": rows})
}
