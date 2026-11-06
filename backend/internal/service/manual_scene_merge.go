package service

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"math"
	"net/http"
	"sort"
	"strconv"
	"strings"
	"time"

	"comfyui-console/internal/models"
	"github.com/gin-gonic/gin"
	"gorm.io/gorm"
)

var (
	ErrSceneMergeInvalid  = errors.New("invalid scene merge")
	ErrSceneMergeNotFound = errors.New("scene merge source not found")
	ErrSceneMergeStale    = errors.New("scene merge preview is stale")
)

type ManualSceneMergePreview struct {
	SourceHash string   `json:"source_hash"`
	Title      string   `json:"title"`
	Content    string   `json:"content"`
	Duration   float64  `json:"duration"`
	SceneIDs   []uint   `json:"scene_ids"`
	Warnings   []string `json:"warnings"`
}

type manualMergeSource struct {
	Scenes       []models.Scene
	Shots        []models.Shot
	Dialogues    []models.Dialogue
	SceneLooks   []models.SceneCharacterLook
	SceneOutfits []models.SceneCharacterOutfit
	ShotLooks    []models.ShotCharacterLook
	ShotOutfits  []models.ShotCharacterOutfit
}

func loadManualMergeSource(db *gorm.DB, projectID uint, ids []uint) (*manualMergeSource, error) {
	if len(ids) < 2 {
		return nil, fmt.Errorf("%w: at least two scene_ids are required", ErrSceneMergeInvalid)
	}
	seen := map[uint]bool{}
	for _, id := range ids {
		if id == 0 || seen[id] {
			return nil, fmt.Errorf("%w: scene_ids must be unique positive integers", ErrSceneMergeInvalid)
		}
		seen[id] = true
	}
	var scenes []models.Scene
	if err := db.Where("id IN ? AND project_id = ? AND (archived = ? OR archived IS NULL)", ids, projectID, false).Order("`order`, id").Find(&scenes).Error; err != nil {
		return nil, err
	}
	if len(scenes) != len(ids) {
		return nil, ErrSceneMergeNotFound
	}
	for i := range scenes {
		if scenes[i].EpisodeN != scenes[0].EpisodeN || scenes[i].Generation != scenes[0].Generation {
			return nil, fmt.Errorf("%w: scenes must share episode and generation", ErrSceneMergeInvalid)
		}
		if math.IsNaN(scenes[i].Duration) || math.IsInf(scenes[i].Duration, 0) || scenes[i].Duration <= 0 {
			return nil, fmt.Errorf("%w: duration must be finite and positive", ErrSceneMergeInvalid)
		}
		if i > 0 && scenes[i].Order != scenes[i-1].Order+1 {
			return nil, fmt.Errorf("%w: scenes must be consecutive", ErrSceneMergeInvalid)
		}
		if scenes[i].ImageToken != "" || scenes[i].Status == "image_pending" || scenes[i].Status == "video_creating" || scenes[i].Status == "video_pending" || scenes[i].Status == "video_running" {
			return nil, fmt.Errorf("%w: scene %d has active generation", ErrSceneMergeInvalid, scenes[i].ID)
		}
	}
	if db.Migrator().HasTable(&models.Task{}) {
		taskIDs := make([]string, 0, len(scenes)*2)
		for _, sc := range scenes {
			if sc.ImageTaskID != "" {
				taskIDs = append(taskIDs, sc.ImageTaskID)
			}
			if sc.VideoTaskID != "" {
				taskIDs = append(taskIDs, sc.VideoTaskID)
			}
		}
		if len(taskIDs) > 0 {
			var active int64
			if err := db.Model(&models.Task{}).Where("task_id IN ? AND status IN ?", taskIDs, []string{"pending", "queued", "running"}).Count(&active).Error; err != nil {
				return nil, err
			}
			if active > 0 {
				return nil, fmt.Errorf("%w: selected scenes have active generation tasks", ErrSceneMergeInvalid)
			}
		}
	}
	orderedIDs := make([]uint, len(scenes))
	for i := range scenes {
		orderedIDs[i] = scenes[i].ID
	}
	var shots []models.Shot
	if err := db.Where("scene_id IN ?", orderedIDs).Order("scene_id, order_num, id").Find(&shots).Error; err != nil {
		return nil, err
	}
	// Resolve each scene through the same read-only path used by video generation.
	// The dialogue table alone is not complete: reviewed analysis may not have been
	// materialized yet, and explicit source speech can also be effective dialogue.
	// Conversely, user_excluded rows are tombstones and must not be resurrected.
	var dialogues []models.Dialogue
	projectService := &ProjectService{db: db}
	for i := range scenes {
		// Validate the authoritative query before read-only recovery (which may
		// synthesize rows with no persisted ID or SceneID).
		var excluded []models.Dialogue
		if err := db.Where("scene_id = ? AND project_id = ? AND source = ?", scenes[i].ID, projectID, "user_excluded").Order("`order`, id").Find(&excluded).Error; err != nil {
			return nil, err
		}
		effective := projectService.sceneVideoDialogues(&scenes[i])
		for j := range effective {
			effective[j].SceneID = scenes[i].ID
			effective[j].ProjectID = projectID
		}
		dialogues = append(dialogues, effective...)
		dialogues = append(dialogues, excluded...)
	}
	sceneOrder := make(map[uint]int, len(scenes))
	for i, sc := range scenes {
		sceneOrder[sc.ID] = i
	}
	sort.SliceStable(shots, func(i, j int) bool {
		if sceneOrder[shots[i].SceneID] != sceneOrder[shots[j].SceneID] {
			return sceneOrder[shots[i].SceneID] < sceneOrder[shots[j].SceneID]
		}
		if shots[i].Order != shots[j].Order {
			return shots[i].Order < shots[j].Order
		}
		return shots[i].ID < shots[j].ID
	})
	sort.SliceStable(dialogues, func(i, j int) bool {
		if sceneOrder[dialogues[i].SceneID] != sceneOrder[dialogues[j].SceneID] {
			return sceneOrder[dialogues[i].SceneID] < sceneOrder[dialogues[j].SceneID]
		}
		if dialogues[i].Order != dialogues[j].Order {
			return dialogues[i].Order < dialogues[j].Order
		}
		return dialogues[i].ID < dialogues[j].ID
	})
	src := &manualMergeSource{Scenes: scenes, Shots: shots, Dialogues: dialogues}
	if db.Migrator().HasTable(&models.SceneCharacterLook{}) {
		if err := db.Where("scene_id IN ?", orderedIDs).Order("scene_id, `order`, id").Find(&src.SceneLooks).Error; err != nil {
			return nil, err
		}
	}
	if db.Migrator().HasTable(&models.SceneCharacterOutfit{}) {
		if err := db.Where("scene_id IN ?", orderedIDs).Order("scene_id, character_id, id").Find(&src.SceneOutfits).Error; err != nil {
			return nil, err
		}
		selected := map[uint]uint{}
		for _, row := range src.SceneOutfits {
			if old, ok := selected[row.CharacterID]; ok && old != row.OutfitID {
				return nil, fmt.Errorf("%w: 所选分镜同一角色套装不同，请先统一", ErrSceneMergeInvalid)
			}
			selected[row.CharacterID] = row.OutfitID
		}
	}
	shotIDs := make([]uint, len(shots))
	for i := range shots {
		shotIDs[i] = shots[i].ID
	}
	if len(shotIDs) > 0 && db.Migrator().HasTable(&models.ShotCharacterLook{}) {
		if err := db.Where("shot_id IN ?", shotIDs).Order("shot_id, id").Find(&src.ShotLooks).Error; err != nil {
			return nil, err
		}
	}
	if len(shotIDs) > 0 && db.Migrator().HasTable(&models.ShotCharacterOutfit{}) {
		if err := db.Where("shot_id IN ?", shotIDs).Order("shot_id, character_id, id").Find(&src.ShotOutfits).Error; err != nil {
			return nil, err
		}
	}
	return src, nil
}

func manualMergeHash(src *manualMergeSource) string {
	b, _ := json.Marshal(src)
	sum := sha256.Sum256(b)
	return hex.EncodeToString(sum[:])
}

// manualMergeSceneContent keeps the authored body byte-for-byte and only appends
// effective spoken lines which are absent from it. Counts are occurrence-based so
// repeated lines are neither collapsed nor multiplied. Unknown speakers remain
// unattributed rather than acquiring a guessed owner.
func manualMergeSceneContent(sc models.Scene, dialogues []models.Dialogue) string {
	content := sc.Content
	present := map[string]int{}
	for _, item := range extractQuotedDialogue(content) {
		present[canonicalDialogueText(item.Text)]++
	}
	needed := map[string]int{}
	var additions []string
	for _, d := range dialogues {
		if d.SceneID != sc.ID || d.Source == "user_excluded" {
			continue
		}
		text := d.Text
		key := canonicalDialogueText(text)
		if key == "" {
			continue
		}
		needed[key]++
		if needed[key] <= present[key] {
			continue
		}
		speaker := strings.TrimSpace(d.Character)
		if speaker == "" || speaker == "待确认" || speaker == "未知" {
			additions = append(additions, "“"+text+"”")
		} else {
			switch d.SpeechType {
			case "offscreen_dialogue":
				additions = append(additions, speaker+"从画外说道：“"+text+"”")
			case "narration":
				additions = append(additions, "旁白：“"+text+"”")
			case "monologue":
				additions = append(additions, speaker+"的内心独白：“"+text+"”")
			default:
				additions = append(additions, speaker+"说：“"+text+"”")
			}
		}
	}
	if len(additions) == 0 {
		return content
	}
	separator := "\n"
	if strings.TrimSpace(content) != "" {
		separator = "\n\n"
	}
	return content + separator + strings.Join(additions, "\n")
}

func manualMergePreview(src *manualMergeSource) *ManualSceneMergePreview {
	p := &ManualSceneMergePreview{SourceHash: manualMergeHash(src), Title: src.Scenes[0].Title + "（合镜）", Warnings: []string{"原场景及其媒体历史会保留并归档；新场景需要重新审核生成。", "仅保留首个场景的参考图选择；请在合并后复核参考图、屏幕文字和音频层。"}}
	parts := make([]string, len(src.Scenes))
	for i, sc := range src.Scenes {
		p.SceneIDs = append(p.SceneIDs, sc.ID)
		parts[i] = manualMergeSceneContent(sc, src.Dialogues)
		p.Duration += sc.Duration
	}
	p.Content = strings.Join(parts, "\n\n")
	for _, d := range src.Dialogues {
		if strings.TrimSpace(d.TimeSegment) != "" {
			if _, _, ok := parseDialogueTimeSegment(d.TimeSegment); !ok {
				p.Warnings = append(p.Warnings, fmt.Sprintf("对白 %d 的时间段格式不受支持；合并时将清空并等待复核。", d.ID))
			}
		}
	}
	return p
}
func (s *Service) PreviewManualSceneMerge(projectID uint, ids []uint) (*ManualSceneMergePreview, error) {
	src, err := loadManualMergeSource(s.DB, projectID, ids)
	if err != nil {
		return nil, err
	}
	return manualMergePreview(src), nil
}

func (s *Service) ApplyManualSceneMerge(projectID uint, ids []uint, expected string) (*models.Scene, error) {
	var result models.Scene
	err := s.DB.Transaction(func(tx *gorm.DB) error {
		src, err := loadManualMergeSource(tx, projectID, ids)
		if err != nil {
			return err
		}
		preview := manualMergePreview(src)
		if expected == "" || expected != preview.SourceHash {
			return ErrSceneMergeStale
		}
		orderedIDs := preview.SceneIDs
		if err := invalidateContinuityDependentsTx(tx, projectID, orderedIDs, "手动合镜已改变场景结构"); err != nil {
			return err
		}
		if err := tx.Model(&models.Scene{}).Where("id IN ?", orderedIDs).Update("archived", true).Error; err != nil {
			return err
		}
		first := src.Scenes[0]
		first.ID = 0
		first.CreatedAt = time.Time{}
		first.UpdatedAt = time.Time{}
		first.Archived = false
		first.Title = preview.Title
		first.Content = preview.Content
		first.Duration = preview.Duration
		union := func(field func(models.Scene) string) string {
			names := []string{}
			seen := map[string]bool{}
			for _, sc := range src.Scenes {
				for _, name := range parseSceneCharacters(field(sc)) {
					if !seen[name] {
						seen[name] = true
						names = append(names, name)
					}
				}
			}
			return joinSceneCharacters(names)
		}
		first.Characters = union(func(sc models.Scene) string { return sc.Characters })
		first.VisibleCharacters = union(func(sc models.Scene) string { return sc.VisibleCharacters })
		first.VoiceCharacters = union(func(sc models.Scene) string { return sc.VoiceCharacters })
		first.MentionedCharacters = union(func(sc models.Scene) string { return sc.MentionedCharacters })
		first.AdvisoryReportsJSON = "" // preserve history on archived sources only
		first.ImageFile = ""
		first.VideoFile = ""
		first.VideoInputFile = ""
		first.ImageToken = ""
		first.ImageTaskID = ""
		first.VideoTaskID = ""
		first.VideoGPU = nil
		first.ImageLocked = false
		first.VideoLocked = false
		first.ImageCandidateParentID = nil
		first.VideoCandidateParentID = nil
		// Reviewed/generated semantics from the first source are stale for the new scene.
		first.ImagePrompt = ""
		first.VideoPrompt = ""
		first.VideoPromptMasterZH = ""
		first.DialogueAnalysisJSON = ""
		first.BlockingJSON = ""
		first.BlockingImageAppliedHash = ""
		first.BlockingVideoAppliedHash = ""
		first.StoryboardVideoTime = nil
		first.VideoFullPrompt = ""
		first.VideoExecSourceHash = ""
		first.VideoExecConfirmedHash = ""
		first.VideoExecConfirmedAt = nil
		first.VideoFirstFrameImg = ""
		first.VideoLastFrameImg = ""
		first.Status = "pending"
		first.Error = ""
		first.PromptStale = true
		first.ImageRetries = 0
		first.VideoRetries = 0
		if err := tx.Create(&first).Error; err != nil {
			return err
		}
		result = first
		seenLooks := map[uint]bool{}
		for _, old := range src.SceneLooks {
			if seenLooks[old.LookID] {
				continue
			}
			seenLooks[old.LookID] = true
			n := old
			n.ID = 0
			n.SceneID = first.ID
			n.CreatedAt = time.Time{}
			if err := tx.Create(&n).Error; err != nil {
				return err
			}
		}
		seenOutfits := map[uint]bool{}
		for _, old := range src.SceneOutfits {
			if seenOutfits[old.CharacterID] {
				continue
			}
			seenOutfits[old.CharacterID] = true
			n := old
			n.ID = 0
			n.SceneID = first.ID
			n.CreatedAt = time.Time{}
			n.Outfit = nil
			if err := tx.Create(&n).Error; err != nil {
				return err
			}
		}
		dialogueMap := map[uint]uint{}
		dialogueOrder := 0
		offsets := map[uint]float64{}
		elapsed := 0.0
		for _, sc := range src.Scenes {
			offsets[sc.ID] = elapsed
			elapsed += sc.Duration
		}
		for _, old := range src.Dialogues {
			n := old
			oldID := n.ID
			n.ID = 0
			n.SceneID = first.ID
			n.Order = dialogueOrder + 1
			dialogueOrder++
			n.Position += offsets[old.SceneID]
			if strings.TrimSpace(n.TimeSegment) != "" {
				if start, end, ok := parseDialogueTimeSegment(n.TimeSegment); ok {
					n.TimeSegment = fmt.Sprintf("%g—%g秒", start+offsets[old.SceneID], end+offsets[old.SceneID])
				} else {
					n.TimeSegment = ""
				}
			}
			n.AudioTaskID = ""
			n.AudioToken = ""
			n.AudioTaskHash = ""
			n.CreatedAt = time.Time{}
			n.UpdatedAt = time.Time{}
			if n.AudioFile != "" {
				n.Status = "ready"
			} else {
				n.Status = "pending"
			}
			n.Error = ""
			if err := tx.Create(&n).Error; err != nil {
				return err
			}
			// Inferred effective dialogues have no persistent source ID. Copy them,
			// but do not let their shared zero ID collide in shot bookkeeping.
			if oldID > 0 {
				dialogueMap[oldID] = n.ID
			}
		}
		shotOrder := 0
		shotMap := map[uint]uint{}
		for _, old := range src.Shots {
			n := old
			// GORM's JSON serializer uses a slice; detach before remapping IDs so the
			// loaded source (and therefore its hash/original DB row) remains untouched.
			n.DialogueRanges = append([]models.ShotDialogueRange(nil), old.DialogueRanges...)
			n.ID = 0
			n.SceneID = first.ID
			shotOrder++
			n.Order = shotOrder
			n.CreatedAt = time.Time{}
			n.UpdatedAt = time.Time{}
			for i := range n.DialogueRanges {
				mapped, ok := dialogueMap[n.DialogueRanges[i].DialogueID]
				if !ok {
					return fmt.Errorf("%w: 镜头对白引用已失效，请先刷新导演设计", ErrSceneMergeInvalid)
				}
				n.DialogueRanges[i].DialogueID = mapped
				if n.DialogueRanges[i].GroupKey != "" {
					n.DialogueRanges[i].GroupKey = fmt.Sprintf("scene:%d:%s", old.SceneID, n.DialogueRanges[i].GroupKey)
				}
			}
			if err := tx.Create(&n).Error; err != nil {
				return err
			}
		}
		for i, old := range src.Shots { // IDs were allocated in the same stable order.
			var copied models.Shot
			if err := tx.Where("scene_id = ? AND order_num = ?", first.ID, i+1).First(&copied).Error; err != nil {
				return err
			}
			shotMap[old.ID] = copied.ID
		}
		for _, old := range src.ShotLooks {
			n := old
			n.ID = 0
			n.ShotID = shotMap[old.ShotID]
			n.CreatedAt = time.Time{}
			if err := tx.Create(&n).Error; err != nil {
				return err
			}
		}
		for _, old := range src.ShotOutfits {
			n := old
			n.ID = 0
			n.ShotID = shotMap[old.ShotID]
			n.CreatedAt = time.Time{}
			n.Outfit = nil
			if err := tx.Create(&n).Error; err != nil {
				return err
			}
		}
		result.ShotCount = shotOrder
		if err := tx.Model(&result).Update("shot_count", shotOrder).Error; err != nil {
			return err
		}
		var remaining []models.Scene
		if err := tx.Where("project_id=? AND generation=? AND episode_n=? AND (archived=? OR archived IS NULL)", projectID, first.Generation, first.EpisodeN, false).Order("`order`,id").Find(&remaining).Error; err != nil {
			return err
		}
		if err := tx.Model(&models.Scene{}).Where("id IN ?", func() []uint {
			v := []uint{}
			for _, x := range remaining {
				v = append(v, x.ID)
			}
			return v
		}()).Update("order", gorm.Expr("`order` + 1000000")).Error; err != nil {
			return err
		}
		for i, x := range remaining {
			if err := tx.Model(&models.Scene{}).Where("id=?", x.ID).Update("order", i+1).Error; err != nil {
				return err
			}
			if x.ID == result.ID {
				result.Order = i + 1
			}
		}
		if tx.Migrator().HasTable(&models.Episode{}) {
			return markEpisodeEditorialStale(tx, projectID, first.EpisodeN)
		}
		return nil
	})
	if err == nil && s.Hub != nil {
		s.Hub.Broadcast("project_update", map[string]any{"project_id": projectID})
	}
	return &result, err
}

type manualMergeRequest struct {
	SceneIDs   []uint `json:"scene_ids"`
	SourceHash string `json:"source_hash"`
}

func mergeHTTPError(c *gin.Context, err error) {
	code := http.StatusInternalServerError
	if errors.Is(err, ErrSceneMergeNotFound) {
		code = 404
	} else if errors.Is(err, ErrSceneMergeStale) {
		code = 409
	} else if errors.Is(err, ErrSceneMergeInvalid) {
		code = 422
	}
	c.JSON(code, gin.H{"error": err.Error()})
}
func (s *Service) HandlePreviewManualSceneMerge(c *gin.Context) {
	id, e := strconv.ParseUint(c.Param("id"), 10, 64)
	var req manualMergeRequest
	if e != nil || c.ShouldBindJSON(&req) != nil {
		c.JSON(422, gin.H{"error": "invalid request"})
		return
	}
	p, e := s.PreviewManualSceneMerge(uint(id), req.SceneIDs)
	if e != nil {
		mergeHTTPError(c, e)
		return
	}
	c.JSON(200, p)
}
func (s *Service) HandleApplyManualSceneMerge(c *gin.Context) {
	id, e := strconv.ParseUint(c.Param("id"), 10, 64)
	var req manualMergeRequest
	if e != nil || c.ShouldBindJSON(&req) != nil {
		c.JSON(422, gin.H{"error": "invalid request"})
		return
	}
	beforeDialogues, beforeAuditErr := resolveSceneDialogueTransfer(s.DB, uint(id), req.SceneIDs)
	p, e := s.ApplyManualSceneMerge(uint(id), req.SceneIDs, req.SourceHash)
	if e != nil {
		mergeHTTPError(c, e)
		return
	}
	audit := auditDialogueTransfer(s.DB, uint(id), beforeDialogues, beforeAuditErr, []uint{p.ID})
	c.JSON(200, struct {
		*models.Scene
		DialogueTransferAudit DialogueTransferAudit `json:"dialogue_transfer_audit"`
	}{Scene: p, DialogueTransferAudit: audit})
}
