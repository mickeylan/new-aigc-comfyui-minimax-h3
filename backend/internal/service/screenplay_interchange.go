package service

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"path/filepath"
	"strings"

	"comfyui-console/internal/models"
	"comfyui-console/internal/screenplay"
	"github.com/gin-gonic/gin"
	"gorm.io/gorm"
)

const maxScreenplayImportBytes int64 = 2 << 20

type screenplayPreviewResponse struct {
	screenplay.Preview
	SceneCount   int `json:"scene_count"`
	ElementCount int `json:"element_count"`
}

func loadOwnedEpisode(db *gorm.DB, projectID uint, number int) (*models.Episode, error) {
	var episode models.Episode
	err := db.Where("project_id = ? AND episode_number = ?", projectID, number).First(&episode).Error
	return &episode, err
}

func inferScreenplayFormat(filename, input string) string {
	ext := strings.ToLower(strings.TrimPrefix(filepath.Ext(filepath.Base(filename)), "."))
	switch ext {
	case "fountain", "fdx", "txt":
		return ext
	}
	if strings.HasPrefix(strings.TrimSpace(input), "<?xml") || strings.Contains(input, "<FinalDraft") {
		return screenplay.FormatFDX
	}
	return screenplay.FormatFountain
}

func readLimitedScreenplay(r io.Reader) (string, error) {
	data, err := io.ReadAll(io.LimitReader(r, maxScreenplayImportBytes+1))
	if err != nil {
		return "", err
	}
	if int64(len(data)) > maxScreenplayImportBytes {
		return "", fmt.Errorf("screenplay exceeds 2 MiB limit")
	}
	return string(data), nil
}

func readMultipartScreenplay(c *gin.Context) (string, string, error) {
	file, header, err := c.Request.FormFile("file")
	if err != nil {
		return "", "", err
	}
	defer file.Close()
	text, err := readLimitedScreenplay(file)
	if err != nil {
		return "", "", err
	}
	format := strings.TrimSpace(c.PostForm("format"))
	if format == "" {
		format = inferScreenplayFormat(header.Filename, text)
	}
	return format, text, nil
}

func readScreenplayInput(c *gin.Context) (string, string, error) {
	c.Request.Body = http.MaxBytesReader(c.Writer, c.Request.Body, maxScreenplayImportBytes+64*1024)
	contentType := c.ContentType()
	if contentType == "multipart/form-data" {
		return readMultipartScreenplay(c)
	}
	if contentType == "application/json" {
		var req struct {
			Format string `json:"format"`
			Text   string `json:"text"`
		}
		decoder := json.NewDecoder(c.Request.Body)
		if err := decoder.Decode(&req); err != nil {
			return "", "", err
		}
		if int64(len(req.Text)) > maxScreenplayImportBytes {
			return "", "", fmt.Errorf("screenplay exceeds 2 MiB limit")
		}
		if req.Format == "" {
			req.Format = inferScreenplayFormat("", req.Text)
		}
		return req.Format, req.Text, nil
	}
	text, err := readLimitedScreenplay(c.Request.Body)
	format := c.Query("format")
	if format == "" {
		format = inferScreenplayFormat("", text)
	}
	return format, text, err
}

func validateScreenplayPreview(preview screenplay.Preview) error {
	if len(preview.Scenes) == 0 {
		return fmt.Errorf("screenplay has no scenes")
	}
	if len(preview.Scenes) > 1000 {
		return fmt.Errorf("screenplay has too many scenes")
	}
	for _, scene := range preview.Scenes {
		if strings.TrimSpace(scene.Heading) == "" || len(scene.Heading) > 500 {
			return fmt.Errorf("invalid scene heading")
		}
		if len(scene.Elements) > 5000 {
			return fmt.Errorf("scene has too many elements")
		}
		for _, element := range scene.Elements {
			switch element.Type {
			case "action", "character", "dialogue", "parenthetical", "transition":
			default:
				return fmt.Errorf("unsupported element type %q", element.Type)
			}
			if len(element.Text) > 100000 {
				return fmt.Errorf("screenplay element is too large")
			}
		}
	}
	return nil
}

// HandlePreviewScreenplayImport parses untrusted input but never mutates project data.
func (s *Service) HandlePreviewScreenplayImport(c *gin.Context) {
	project, ok := s.loadProject(c)
	if !ok {
		return
	}
	number, ok := episodeNumberParam(c)
	if !ok {
		return
	}
	if _, err := loadOwnedEpisode(s.DB, project.ID, number); errors.Is(err, gorm.ErrRecordNotFound) {
		c.JSON(http.StatusNotFound, gin.H{"error": "episode not found"})
		return
	} else if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	format, text, err := readScreenplayInput(c)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	preview, err := screenplay.Parse(format, text)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	if err := validateScreenplayPreview(preview); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, screenplayPreviewResponse{Preview: preview, SceneCount: len(preview.Scenes), ElementCount: preview.ElementCount()})
}

func previewToDatabase(tx *gorm.DB, project *models.Project, episodeN int, preview screenplay.Preview) error {
	var oldScenes []models.Scene
	if err := tx.Where("project_id = ? AND episode_n = ?", project.ID, episodeN).Find(&oldScenes).Error; err != nil {
		return err
	}
	ids := make([]uint, 0, len(oldScenes))
	for _, scene := range oldScenes {
		ids = append(ids, scene.ID)
	}
	if err := deleteSceneDependents(tx, project.ID, ids); err != nil {
		return err
	}
	if err := tx.Where("project_id = ? AND episode_n = ?", project.ID, episodeN).Delete(&models.Scene{}).Error; err != nil {
		return err
	}

	for sceneIndex, imported := range preview.Scenes {
		actions := []string{}
		for _, element := range imported.Elements {
			if element.Type == "action" {
				actions = append(actions, strings.TrimSpace(element.Text))
			}
		}
		scene := models.Scene{ProjectID: project.ID, EpisodeN: episodeN, Order: sceneIndex + 1, Generation: project.Generation,
			Title: strings.TrimSpace(imported.Heading), Content: strings.Join(actions, "\n\n"), Duration: 5, Status: "pending", PromptStale: true}
		if err := tx.Create(&scene).Error; err != nil {
			return err
		}
		shotOrder, dialogueOrder := 0, 0
		character, parenthetical := "", ""
		for _, element := range imported.Elements {
			text := strings.TrimSpace(element.Text)
			if text == "" {
				continue
			}
			switch element.Type {
			case "action", "transition":
				shotOrder++
				shot := models.Shot{SceneID: scene.ID, Order: shotOrder, ActType: models.ShotActSetup, ShotType: element.Type, Duration: 1.5, Description: text}
				if err := tx.Create(&shot).Error; err != nil {
					return err
				}
			case "character":
				character = text
				parenthetical = ""
			case "parenthetical":
				parenthetical = strings.Trim(text, "()")
			case "dialogue":
				dialogueOrder++
				dialogue := models.Dialogue{SceneID: scene.ID, ProjectID: project.ID, Order: dialogueOrder, Character: character,
					SpeechType: "dialogue", Text: text, Delivery: parenthetical, Speed: 1, Volume: 1, Status: "pending", AudioStale: true}
				if character == "" {
					dialogue.Character = "旁白"
					dialogue.SpeechType = "narration"
				}
				if err := tx.Create(&dialogue).Error; err != nil {
					return err
				}
				parenthetical = ""
			}
		}
		if err := tx.Model(&scene).Update("shot_count", shotOrder).Error; err != nil {
			return err
		}
	}
	var scripts map[int]string
	if strings.TrimSpace(project.Scripts) != "" {
		_ = json.Unmarshal([]byte(project.Scripts), &scripts)
	}
	if scripts == nil {
		scripts = map[int]string{}
	}
	scripts[episodeN] = screenplay.WriteText(preview)
	encoded, err := json.Marshal(scripts)
	if err != nil {
		return err
	}
	updates := map[string]any{"scripts": string(encoded), "script": scripts[episodeN]}
	return tx.Model(&models.Project{}).Where("id = ?", project.ID).Updates(updates).Error
}

// HandleApplyScreenplayImport snapshots first, then replaces only the selected episode hierarchy atomically.
func (s *Service) HandleApplyScreenplayImport(c *gin.Context) {
	project, ok := s.loadProject(c)
	if !ok {
		return
	}
	number, ok := episodeNumberParam(c)
	if !ok {
		return
	}
	if _, err := loadOwnedEpisode(s.DB, project.ID, number); errors.Is(err, gorm.ErrRecordNotFound) {
		c.JSON(http.StatusNotFound, gin.H{"error": "episode not found"})
		return
	} else if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	var preview screenplay.Preview
	c.Request.Body = http.MaxBytesReader(c.Writer, c.Request.Body, maxScreenplayImportBytes*4)
	if err := c.ShouldBindJSON(&preview); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid preview"})
		return
	}
	if err := validateScreenplayPreview(preview); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	var revision *models.ScriptRevision
	err := s.DB.Transaction(func(tx *gorm.DB) error {
		var err error
		revision, err = createScriptRevisionTx(tx, project.ID, number, "before_screenplay_import", nil)
		if err != nil {
			return err
		}
		return previewToDatabase(tx, project, number, preview)
	})
	if err != nil {
		c.JSON(http.StatusConflict, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{"ok": true, "revision": revision})
}

func databaseToPreview(db *gorm.DB, projectID uint, episodeN int, title string) (screenplay.Preview, error) {
	preview := screenplay.Preview{Title: title, Scenes: []screenplay.Scene{}}
	var scenes []models.Scene
	if err := db.Where("project_id = ? AND episode_n = ?", projectID, episodeN).Order("`order`, id").Find(&scenes).Error; err != nil {
		return preview, err
	}
	for _, scene := range scenes {
		item := screenplay.Scene{Heading: scene.Title, Elements: []screenplay.Element{}}
		var shots []models.Shot
		if err := db.Where("scene_id = ?", scene.ID).Order("order_num, id").Find(&shots).Error; err != nil {
			return preview, err
		}
		for _, shot := range shots {
			typ := "action"
			if shot.ShotType == "transition" {
				typ = "transition"
			}
			if strings.TrimSpace(shot.Description) != "" {
				item.Elements = append(item.Elements, screenplay.Element{Type: typ, Text: shot.Description})
			}
		}
		if len(shots) == 0 && strings.TrimSpace(scene.Content) != "" {
			item.Elements = append(item.Elements, screenplay.Element{Type: "action", Text: scene.Content})
		}
		var dialogues []models.Dialogue
		if err := db.Where("project_id = ? AND scene_id = ?", projectID, scene.ID).Order("`order`, id").Find(&dialogues).Error; err != nil {
			return preview, err
		}
		for _, dialogue := range dialogues {
			item.Elements = append(item.Elements, screenplay.Element{Type: "character", Text: dialogue.Character})
			if strings.TrimSpace(dialogue.Delivery) != "" {
				item.Elements = append(item.Elements, screenplay.Element{Type: "parenthetical", Text: dialogue.Delivery})
			}
			item.Elements = append(item.Elements, screenplay.Element{Type: "dialogue", Text: dialogue.Text})
		}
		preview.Scenes = append(preview.Scenes, item)
	}
	return preview, nil
}

func (s *Service) HandleExportScreenplay(c *gin.Context) {
	project, ok := s.loadProject(c)
	if !ok {
		return
	}
	number, ok := episodeNumberParam(c)
	if !ok {
		return
	}
	episode, err := loadOwnedEpisode(s.DB, project.ID, number)
	if errors.Is(err, gorm.ErrRecordNotFound) {
		c.JSON(http.StatusNotFound, gin.H{"error": "episode not found"})
		return
	} else if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	if episode.Status != "approved" {
		c.JSON(http.StatusConflict, gin.H{"error": "episode must be approved before export"})
		return
	}
	format := strings.ToLower(strings.TrimPrefix(c.Param("format"), "."))
	preview, err := databaseToPreview(s.DB, project.ID, number, episode.Title)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	var body, contentType string
	switch format {
	case screenplay.FormatFountain:
		body, contentType = screenplay.WriteFountain(preview), "text/plain; charset=utf-8"
	case screenplay.FormatText:
		body, contentType = screenplay.WriteText(preview), "text/plain; charset=utf-8"
	case screenplay.FormatFDX:
		body, err = screenplay.WriteFDX(preview)
		contentType = "application/xml; charset=utf-8"
	default:
		c.JSON(http.StatusBadRequest, gin.H{"error": "unsupported screenplay format"})
		return
	}
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	// The server-generated name deliberately excludes user-controlled path components.
	c.Header("Content-Disposition", fmt.Sprintf(`attachment; filename="episode-%d.%s"`, number, format))
	c.Data(http.StatusOK, contentType, []byte(body))
}
