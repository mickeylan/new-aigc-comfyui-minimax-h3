package service

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"sort"
	"strings"
	"time"

	"comfyui-console/internal/models"
	"gorm.io/gorm"
)

type ProductionCheckFinding struct {
	Severity       string        `json:"severity"`
	Code           string        `json:"code"`
	EntityType     string        `json:"entity_type"`
	EntityID       uint          `json:"entity_id"`
	Message        string        `json:"message"`
	Action         string        `json:"action,omitempty"`
	Recommendation string        `json:"recommendation,omitempty"`
	Affected       []ArtifactRef `json:"affected,omitempty"`
}

func productionFinding(severity, code, entityType string, entityID uint, message, action, recommendation string) ProductionCheckFinding {
	return ProductionCheckFinding{Severity: severity, Code: code, EntityType: entityType, EntityID: entityID, Message: message, Action: action, Recommendation: recommendation}
}

type ProductionCheckResult struct {
	ProjectID uint                     `json:"project_id"`
	EpisodeN  int                      `json:"episode_n,omitempty"`
	Ready     bool                     `json:"ready"`
	Findings  []ProductionCheckFinding `json:"findings"`
}

type ProductionCheckService struct{ db *gorm.DB }

func NewProductionCheckService(db *gorm.DB) *ProductionCheckService {
	return &ProductionCheckService{db: db}
}

// Check returns bounded production-state diagnostics and never exposes prompts, story text or file paths.
func (s *ProductionCheckService) Check(projectID uint, episodeN int) (ProductionCheckResult, error) {
	result := ProductionCheckResult{ProjectID: projectID, EpisodeN: episodeN, Ready: true, Findings: []ProductionCheckFinding{}}
	if projectID == 0 {
		return result, fmt.Errorf("project_id is required")
	}
	if episodeN < 0 {
		return result, fmt.Errorf("episode_n cannot be negative")
	}
	var projectCount int64
	if err := s.db.Model(&models.Project{}).Where("id = ?", projectID).Count(&projectCount).Error; err != nil {
		return result, err
	}
	if projectCount != 1 {
		return result, gorm.ErrRecordNotFound
	}
	sceneQuery := s.db.Where("project_id = ?", projectID)
	if episodeN > 0 {
		sceneQuery = sceneQuery.Where("episode_n = ?", episodeN)
	}
	var scenes []models.Scene
	if err := sceneQuery.Order("episode_n, generation, `order`, id").Find(&scenes).Error; err != nil {
		return result, err
	}
	if len(scenes) == 0 {
		result.Findings = append(result.Findings, productionFinding("error", "episode_has_no_scenes", "episode", 0, "当前范围没有可生产的场景", "episode", "先生成或导入本集剧本场景"))
	}
	for _, scene := range scenes {
		if strings.TrimSpace(scene.VideoFile) == "" && strings.TrimSpace(scene.VideoInputFile) == "" {
			result.Findings = append(result.Findings, productionFinding("error", "scene_video_missing", "scene", scene.ID, "场景尚无已完成视频", "scene", "定位场景并完成视频生成与审核"))
		}
		if strings.TrimSpace(scene.ImageFile) == "" {
			result.Findings = append(result.Findings, productionFinding("warning", "scene_storyboard_missing", "scene", scene.ID, "场景尚无分镜图；仅T2V流程可忽略", "scene", "确认使用T2V，或为场景生成并审核分镜图"))
		}
		if scene.PromptStale {
			result.Findings = append(result.Findings, productionFinding("error", "scene_prompt_stale", "scene", scene.ID, "视频提示词已过期，正式生成前需重新生成或确认", "scene", "重新准备并人工确认视频提示词"))
		}
		if strings.TrimSpace(scene.Error) != "" {
			result.Findings = append(result.Findings, productionFinding("error", "scene_error", "scene", scene.ID, "场景存在未处理的生产错误", "scene", "定位场景查看错误并重试对应步骤"))
		}
		if scene.Duration < 3 || scene.Duration > 15 {
			result.Findings = append(result.Findings, productionFinding("warning", "scene_duration_out_of_range", "scene", scene.ID, "场景时长不在 MiniMax H3 的 3–15 秒范围", "scene", "调整Scene或拆分Shot，使单段保持3–15秒"))
		}
		var continuity models.SceneContinuity
		if q := s.db.Where("scene_id = ?", scene.ID).Limit(1).Find(&continuity); q.Error != nil {
			return result, q.Error
		} else if q.RowsAffected > 0 && continuity.Mode != models.ContinuityModeIndependent && (continuity.SelectedFrameID == nil || continuity.Status != "ready") {
			result.Findings = append(result.Findings, productionFinding("error", "continuity_not_ready", "scene", scene.ID, "连续性模式尚未绑定有效的人工确认帧", "scene", "打开连续性设置并确认来源尾帧"))
		}
	}
	dialogueQuery := s.db.Model(&models.Dialogue{}).Joins("JOIN scenes ON scenes.id = dialogues.scene_id").Where("dialogues.project_id = ? AND scenes.project_id = ?", projectID, projectID)
	if episodeN > 0 {
		dialogueQuery = dialogueQuery.Where("scenes.episode_n = ?", episodeN)
	}
	var dialogues []models.Dialogue
	if err := dialogueQuery.Select("dialogues.*").Find(&dialogues).Error; err != nil {
		return result, err
	}
	for _, dialogue := range dialogues {
		if dialogue.AudioStale {
			result.Findings = append(result.Findings, productionFinding("warning", "dialogue_audio_stale", "dialogue", dialogue.ID, "对白音频已过期，需要重新生成并审核候选", "voice", "进入声音工作台重新生成并审核该句配音"))
		}
	}
	if s.db.Migrator().HasTable(&models.MusicCueSheet{}) && episodeN > 0 {
		musicPreflight, err := NewMusicCueService(s.db).Preflight(projectID, episodeN)
		if err != nil {
			return result, err
		}
		for _, issue := range musicPreflight.Issues {
			severity := issue.Severity
			if issue.Code == "review" {
				severity = "warning"
			}
			finding := productionFinding(severity, "music_cue_"+issue.Code, "music_cue", issue.CueID, issue.Message, "music", "定位配乐Cue并处理时间轴、Caption或审核状态")
			if issue.RelatedCueID > 0 {
				finding.Recommendation = fmt.Sprintf("检查Cue #%d与Cue #%d的时间重叠", issue.CueID, issue.RelatedCueID)
			}
			result.Findings = append(result.Findings, finding)
		}
	}
	graph := NewArtifactDependencyService(s.db, s.db.Migrator().HasTable(&models.ArtifactDependency{}))
	for i := range result.Findings {
		var ref ArtifactRef
		switch result.Findings[i].EntityType {
		case "dialogue":
			ref = ArtifactRef{Kind: ArtifactKindDialogue, ID: result.Findings[i].EntityID}
		case "music_cue":
			ref = ArtifactRef{Kind: ArtifactKindMusicCue, ID: result.Findings[i].EntityID}
		}
		if ref.ID > 0 {
			affected, err := graph.Affected(projectID, ref)
			if err != nil {
				return result, err
			}
			result.Findings[i].Affected = affected
		}
	}
	sort.SliceStable(result.Findings, func(i, j int) bool {
		if result.Findings[i].EntityType == result.Findings[j].EntityType {
			return result.Findings[i].EntityID < result.Findings[j].EntityID
		}
		return result.Findings[i].EntityType < result.Findings[j].EntityType
	})
	for _, finding := range result.Findings {
		if finding.Severity == "error" {
			result.Ready = false
			break
		}
	}
	return result, nil
}

type ProductionCheckReportView struct {
	models.ProductionCheckReport
	Findings []ProductionCheckFinding `json:"findings"`
}

func (s *ProductionCheckService) sourceFingerprint(projectID uint, episodeN int, findings []ProductionCheckFinding) ([]byte, error) {
	type versionRow struct {
		ID        uint      `json:"id"`
		UpdatedAt time.Time `json:"updated_at"`
	}
	var scenes, dialogues, cues []versionRow
	if err := s.db.Model(&models.Scene{}).Select("id, updated_at").Where("project_id = ? AND episode_n = ?", projectID, episodeN).Order("id").Scan(&scenes).Error; err != nil {
		return nil, err
	}
	if err := s.db.Model(&models.Dialogue{}).Select("dialogues.id, dialogues.updated_at").Joins("JOIN scenes ON scenes.id = dialogues.scene_id").Where("dialogues.project_id = ? AND scenes.project_id = ? AND scenes.episode_n = ?", projectID, projectID, episodeN).Order("dialogues.id").Scan(&dialogues).Error; err != nil {
		return nil, err
	}
	if s.db.Migrator().HasTable(&models.MusicCueSheet{}) {
		if err := s.db.Model(&models.MusicCueSheet{}).Select("id, updated_at").Where("project_id = ? AND episode_n = ?", projectID, episodeN).Order("id").Scan(&cues).Error; err != nil {
			return nil, err
		}
	}
	return json.Marshal(struct {
		ProjectID               uint `json:"project_id"`
		EpisodeN                int  `json:"episode_n"`
		Scenes, Dialogues, Cues []versionRow
		Findings                []ProductionCheckFinding `json:"findings"`
	}{projectID, episodeN, scenes, dialogues, cues, findings})
}

func (s *ProductionCheckService) CreateReport(projectID uint, episodeN int) (*ProductionCheckReportView, error) {
	var episodeCount, sceneCount int64
	if err := s.db.Model(&models.Episode{}).Where("project_id = ? AND episode_number = ?", projectID, episodeN).Count(&episodeCount).Error; err != nil {
		return nil, err
	}
	if err := s.db.Model(&models.Scene{}).Where("project_id = ? AND episode_n = ?", projectID, episodeN).Count(&sceneCount).Error; err != nil {
		return nil, err
	}
	if episodeCount == 0 && sceneCount == 0 {
		return nil, fmt.Errorf("episode does not exist")
	}
	result, err := s.Check(projectID, episodeN)
	if err != nil {
		return nil, err
	}
	findings, err := json.Marshal(result.Findings)
	if err != nil {
		return nil, err
	}
	sourceFingerprint, err := s.sourceFingerprint(projectID, episodeN, result.Findings)
	if err != nil {
		return nil, err
	}
	hash := sha256.Sum256(sourceFingerprint)
	row := models.ProductionCheckReport{ProjectID: projectID, EpisodeN: episodeN, SourceHash: hex.EncodeToString(hash[:]), Ready: result.Ready, FindingsJSON: string(findings), FindingCount: len(result.Findings), ReviewStatus: "open"}
	err = s.db.Transaction(func(tx *gorm.DB) error {
		if err := tx.Model(&models.ProductionCheckReport{}).Where("project_id = ? AND episode_n = ? AND superseded = ?", projectID, episodeN, false).Update("superseded", true).Error; err != nil {
			return err
		}
		return tx.Create(&row).Error
	})
	if err != nil {
		return nil, err
	}
	return &ProductionCheckReportView{ProductionCheckReport: row, Findings: result.Findings}, nil
}

func (s *ProductionCheckService) ListReports(projectID uint, episodeN int) ([]ProductionCheckReportView, error) {
	query := s.db.Where("project_id = ?", projectID)
	if episodeN > 0 {
		query = query.Where("episode_n = ?", episodeN)
	}
	var rows []models.ProductionCheckReport
	if err := query.Order("created_at DESC, id DESC").Limit(50).Find(&rows).Error; err != nil {
		return nil, err
	}
	out := make([]ProductionCheckReportView, 0, len(rows))
	for _, row := range rows {
		var findings []ProductionCheckFinding
		if err := json.Unmarshal([]byte(row.FindingsJSON), &findings); err != nil {
			return nil, err
		}
		out = append(out, ProductionCheckReportView{ProductionCheckReport: row, Findings: findings})
	}
	return out, nil
}

func (s *ProductionCheckService) ReviewReport(projectID, id uint, status, note string) (*ProductionCheckReportView, error) {
	if len([]rune(note)) > 500 {
		return nil, fmt.Errorf("review note is too long")
	}
	if status != "reviewed" && status != "open" {
		return nil, fmt.Errorf("review_status must be open or reviewed")
	}
	updates := map[string]any{"review_status": status, "review_note": strings.TrimSpace(note)}
	if status == "reviewed" {
		updates["reviewed_at"] = time.Now()
	} else {
		updates["reviewed_at"] = nil
	}
	result := s.db.Model(&models.ProductionCheckReport{}).Where("id = ? AND project_id = ? AND superseded = ?", id, projectID, false).Updates(updates)
	if result.Error != nil {
		return nil, result.Error
	}
	if result.RowsAffected != 1 {
		return nil, gorm.ErrRecordNotFound
	}
	var row models.ProductionCheckReport
	if err := s.db.Where("id = ? AND project_id = ?", id, projectID).First(&row).Error; err != nil {
		return nil, err
	}
	var findings []ProductionCheckFinding
	if err := json.Unmarshal([]byte(row.FindingsJSON), &findings); err != nil {
		return nil, err
	}
	return &ProductionCheckReportView{ProductionCheckReport: row, Findings: findings}, nil
}
