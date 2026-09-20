package service

import (
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"comfyui-console/internal/models"
	"gorm.io/gorm"
)

type RollingStateInput struct {
	CharacterStatesJSON string `json:"character_states_json"`
	RelationshipsJSON   string `json:"relationships_json"`
	WorldStateJSON      string `json:"world_state_json"`
	ClueStateJSON       string `json:"clue_state_json"`
}

func validJSONObject(raw string) bool {
	var v any
	return strings.TrimSpace(raw) != "" && json.Unmarshal([]byte(raw), &v) == nil
}
func validateRollingState(v RollingStateInput) error {
	for name, raw := range map[string]string{"character_states_json": v.CharacterStatesJSON, "relationships_json": v.RelationshipsJSON, "world_state_json": v.WorldStateJSON, "clue_state_json": v.ClueStateJSON} {
		if !validJSONObject(raw) {
			return fmt.Errorf("%s必须是合法JSON", name)
		}
	}
	return nil
}

func (s *BatchPlanningService) GetSnapshot(projectID, batchID uint) (*models.BatchStateSnapshot, error) {
	var v models.BatchStateSnapshot
	err := s.db.Where("project_id = ? AND batch_id = ?", projectID, batchID).First(&v).Error
	return &v, err
}
func (s *BatchPlanningService) SaveSnapshot(projectID, batchID uint, in RollingStateInput) (*models.BatchStateSnapshot, error) {
	if err := validateRollingState(in); err != nil {
		return nil, err
	}
	batch, err := s.GetBatch(projectID, batchID)
	if err != nil {
		return nil, err
	}
	if batch.Status == BatchStatusProduced {
		return nil, fmt.Errorf("已生产批次状态不可修改")
	}
	var old models.BatchStateSnapshot
	find := s.db.Where("project_id = ? AND batch_id = ?", projectID, batchID).First(&old).Error
	var prev *uint
	if batch.PreviousBatchID != nil {
		var p models.BatchStateSnapshot
		if s.db.Where("project_id = ? AND batch_id = ? AND status = ?", projectID, *batch.PreviousBatchID, "approved").First(&p).Error == nil {
			prev = &p.ID
		}
	}
	v := models.BatchStateSnapshot{ProjectID: projectID, BatchID: batchID, PreviousSnapshotID: prev, CharacterStatesJSON: in.CharacterStatesJSON, RelationshipsJSON: in.RelationshipsJSON, WorldStateJSON: in.WorldStateJSON, ClueStateJSON: in.ClueStateJSON, Status: "draft", Version: 1}
	if find == nil {
		v.ID = old.ID
		v.Version = old.Version + 1
	}
	if err := s.db.Save(&v).Error; err != nil {
		return nil, err
	}
	return &v, nil
}
func (s *BatchPlanningService) GenerateSnapshot(projectID, batchID uint) (*models.BatchStateSnapshot, error) {
	if s.skills == nil || s.provider == nil {
		return nil, fmt.Errorf("文本生成服务未配置")
	}
	detail, err := s.Detail(projectID, batchID)
	if err != nil {
		return nil, err
	}
	if detail.Batch.Status != BatchStatusReview && detail.Batch.Status != BatchStatusApproved {
		return nil, fmt.Errorf("请先生成批次分集草稿")
	}
	var bible models.StoryBible
	if err = s.db.Where("project_id = ? AND status = ?", projectID, "approved").First(&bible).Error; err != nil {
		return nil, fmt.Errorf("需要已审核故事圣经")
	}
	var previous models.BatchStateSnapshot
	if detail.Batch.PreviousBatchID != nil {
		_ = s.db.Where("project_id = ? AND batch_id = ? AND status = ?", projectID, *detail.Batch.PreviousBatchID, "approved").First(&previous).Error
	}
	payload, _ := json.Marshal(map[string]any{"story_bible": bible, "previous_snapshot": previous, "batch": detail.Batch, "episodes": detail.Episodes})
	system := "只输出JSON对象，字段必须为character_states、relationships、world_state、clues。character_states按角色名记录修为/能力/所在地/持有道具/身体状态；relationships记录关系变化；world_state记录势力与世界变化；clues为数组且每项含clue_key,title,description,status(open/developing/resolved/abandoned),opened_episode,last_touched_episode,resolved_episode。必须基于输入，不得创造未发生事件。"
	raw, err := s.skills.ChatWithSkill(projectID, models.SkillStageContinuityReview, s.provider, system, string(payload), map[string]string{"continuity_context": string(payload)})
	if err != nil {
		return nil, err
	}
	var parsed struct {
		CharacterStates any `json:"character_states"`
		Relationships   any `json:"relationships"`
		WorldState      any `json:"world_state"`
		Clues           any `json:"clues"`
	}
	if err = parseJSONObject(raw, &parsed); err != nil {
		return nil, err
	}
	marshal := func(v any) string { b, _ := json.Marshal(v); return string(b) }
	return s.SaveSnapshot(projectID, batchID, RollingStateInput{marshal(parsed.CharacterStates), marshal(parsed.Relationships), marshal(parsed.WorldState), marshal(parsed.Clues)})
}
func (s *BatchPlanningService) ReviewSnapshot(projectID, batchID uint, approve bool, override string) (*models.BatchStateSnapshot, error) {
	snap, err := s.GetSnapshot(projectID, batchID)
	if err != nil {
		return nil, err
	}
	ok, msg, err := s.ValidateBatchContinuity(batchID)
	if err != nil {
		return nil, err
	}
	if !ok && strings.TrimSpace(override) == "" {
		return nil, fmt.Errorf("跨批次连续性未通过：%s", msg)
	}
	status := "draft"
	var approvedAt *time.Time
	review := map[string]any{"passed": ok, "message": msg, "override_reason": override}
	if approve {
		status = "approved"
		now := time.Now()
		approvedAt = &now
	}
	b, _ := json.Marshal(review)
	if err = s.db.Model(snap).Updates(map[string]any{"status": status, "approved_at": approvedAt, "review_json": string(b)}).Error; err != nil {
		return nil, err
	}
	if approve {
		if err = s.applyClues(projectID, batchID, snap.ClueStateJSON); err != nil {
			return nil, err
		}
	}
	return s.GetSnapshot(projectID, batchID)
}
func (s *BatchPlanningService) applyClues(projectID, batchID uint, raw string) error {
	var clues []models.StoryClue
	if json.Unmarshal([]byte(raw), &clues) != nil {
		return fmt.Errorf("伏笔状态格式错误")
	}
	return s.db.Transaction(func(tx *gorm.DB) error {
		for _, clue := range clues {
			if clue.ClueKey == "" {
				continue
			}
			clue.ID = 0
			clue.ProjectID = projectID
			var old models.StoryClue
			err := tx.Where("project_id = ? AND clue_key = ?", projectID, clue.ClueKey).First(&old).Error
			if err == nil {
				clue.ID = old.ID
				clue.Version = old.Version + 1
			} else if err != gorm.ErrRecordNotFound {
				return err
			} else {
				clue.Version = 1
			}
			if err = tx.Save(&clue).Error; err != nil {
				return err
			}
		}
		return nil
	})
}
func (s *BatchPlanningService) ListClues(projectID uint) ([]models.StoryClue, error) {
	var rows []models.StoryClue
	err := s.db.Where("project_id = ?", projectID).Order("status, clue_key").Find(&rows).Error
	return rows, err
}
func (s *BatchPlanningService) TransitionClue(projectID, clueID uint, status string, episode int, override bool) (*models.StoryClue, error) {
	valid := map[string]bool{"open": true, "developing": true, "resolved": true, "abandoned": true}
	if !valid[status] {
		return nil, fmt.Errorf("无效伏笔状态")
	}
	var clue models.StoryClue
	if err := s.db.Where("id = ? AND project_id = ?", clueID, projectID).First(&clue).Error; err != nil {
		return nil, err
	}
	if (clue.Status == "resolved" || clue.Status == "abandoned") && !override {
		return nil, fmt.Errorf("已结束伏笔需要显式允许重开")
	}
	u := map[string]any{"status": status, "last_touched_episode": episode, "version": gorm.Expr("version + 1")}
	if status == "resolved" {
		u["resolved_episode"] = episode
	}
	if err := s.db.Model(&clue).Updates(u).Error; err != nil {
		return nil, err
	}
	return &clue, nil
}
