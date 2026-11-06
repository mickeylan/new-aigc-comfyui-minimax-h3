package service

import (
	"errors"
	"net/http"
	"sort"
	"strconv"
	"strings"

	"comfyui-console/internal/models"
	"github.com/gin-gonic/gin"
	"gorm.io/gorm"
)

type StoryConsistencyCheck struct {
	Code    string `json:"code"`
	Status  string `json:"status"`
	Message string `json:"message"`
}

type StoryConsistencyReport struct {
	SceneID               uint                    `json:"scene_id"`
	ContentHash           string                  `json:"content_hash"`
	Advisory              bool                    `json:"advisory"`
	Status                string                  `json:"status"`
	Checks                []StoryConsistencyCheck `json:"checks"`
	DialogueCount         int                     `json:"dialogue_count"`
	RetainedDialogueCount int                     `json:"retained_dialogue_count,omitempty"`
	ExcludedDialogueCount int                     `json:"excluded_dialogue_count,omitempty"`
}

type DialoguePreservationReport struct {
	BeforeCount    int                     `json:"before_count"`
	AfterCount     int                     `json:"after_count"`
	Missing        []models.Dialogue       `json:"missing"`
	Reordered      []models.Dialogue       `json:"reordered"`
	SpeakerChanges []DialogueSpeakerChange `json:"speaker_changes"`
}

type DialogueSpeakerChange struct {
	Text   string `json:"text"`
	Before string `json:"before"`
	After  string `json:"after"`
}

// BuildDialoguePreservationReport compares exact dialogue text (including punctuation).
// Duplicate occurrences are paired in occurrence order; it does not infer target shots.
func BuildDialoguePreservationReport(before, after []models.Dialogue) DialoguePreservationReport {
	r := DialoguePreservationReport{BeforeCount: len(before), AfterCount: len(after), Missing: []models.Dialogue{}, Reordered: []models.Dialogue{}, SpeakerChanges: []DialogueSpeakerChange{}}
	positions := map[string][]int{}
	for i := range after {
		positions[after[i].Text] = append(positions[after[i].Text], i)
	}
	used := map[string]int{}
	matchedBefore, matchedAfter := []models.Dialogue{}, []int{}
	for _, d := range before {
		at := used[d.Text]
		if at >= len(positions[d.Text]) {
			r.Missing = append(r.Missing, d)
			continue
		}
		j := positions[d.Text][at]
		used[d.Text]++
		matchedBefore = append(matchedBefore, d)
		matchedAfter = append(matchedAfter, j)
		if d.Character != after[j].Character {
			r.SpeakerChanges = append(r.SpeakerChanges, DialogueSpeakerChange{Text: d.Text, Before: d.Character, After: after[j].Character})
		}
	}
	// Identify moved occurrences using the longest increasing subsequence of
	// matched output positions. Duplicates stay paired in occurrence order.
	tails, indices := []int{}, []int{}
	parents := make([]int, len(matchedAfter))
	for i, position := range matchedAfter {
		k := sort.SearchInts(tails, position)
		parents[i] = -1
		if k > 0 {
			parents[i] = indices[k-1]
		}
		if k == len(tails) {
			tails = append(tails, position)
			indices = append(indices, i)
		} else {
			tails[k], indices[k] = position, i
		}
	}
	kept := make([]bool, len(matchedAfter))
	if len(indices) > 0 {
		for i := indices[len(indices)-1]; i >= 0; i = parents[i] {
			kept[i] = true
		}
	}
	for i, d := range matchedBefore {
		if !kept[i] {
			r.Reordered = append(r.Reordered, d)
		}
	}
	return r
}

func (s *Service) HandleGetSceneStoryConsistency(c *gin.Context) {
	pid, e1 := strconv.ParseUint(c.Param("id"), 10, 32)
	sid, e2 := strconv.ParseUint(c.Param("sid"), 10, 32)
	if e1 != nil || e2 != nil || pid == 0 || sid == 0 {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid project or scene id"})
		return
	}
	var project models.Project
	if err := s.DB.First(&project, uint(pid)).Error; err != nil {
		storyConsistencyDBError(c, err)
		return
	}
	var scene models.Scene
	if err := s.DB.Where("id = ? AND project_id = ?", uint(sid), uint(pid)).First(&scene).Error; err != nil {
		storyConsistencyDBError(c, err)
		return
	}
	var raw []models.Dialogue
	if err := s.DB.Where("scene_id = ? AND project_id = ?", scene.ID, scene.ProjectID).Order("`order`").Find(&raw).Error; err != nil {
		c.JSON(500, gin.H{"error": err.Error()})
		return
	}
	ps := s.Projects
	if ps == nil {
		ps = &ProjectService{db: s.DB}
	}
	effective := ps.sceneVideoDialogues(&scene)
	checks := []StoryConsistencyCheck{}
	add := func(code, status, message string) {
		checks = append(checks, StoryConsistencyCheck{code, status, message})
	}
	if scene.DirectorSourceStale {
		add("director_source_stale", "attention", "旧导演设计早于当前正文，已保留作历史参考，不应作为当前剧情依据。")
	} else {
		add("director_source_stale", "ok", "未记录到导演设计因正文修改而过期；不代表已完成剧情语义审核。")
	}
	blockingStale := strings.TrimSpace(scene.BlockingJSON) != "" && ((scene.BlockingContentHash == "" && scene.StoryContentEdited) || (scene.BlockingContentHash != "" && scene.BlockingContentHash != sceneDialogueContentHash(scene.Content)))
	if blockingStale {
		add("blocking_source_stale", "attention", "已保存站位与当前正文版本不匹配。")
	} else {
		add("blocking_source_stale", "ok", "未记录到已保存站位过期。")
	}
	if strings.TrimSpace(scene.DialogueAnalysisJSON) != "" {
		if _, current := currentSavedDialogueAnalysis(&scene); !current {
			add("dialogue_analysis_stale", "attention", "对白分析来自旧正文，建议基于当前正文重新审核。")
		} else {
			add("dialogue_analysis_stale", "ok", "对白分析的正文来源哈希匹配。")
		}
	} else {
		add("dialogue_analysis_stale", "not_assessed", "暂无已保存对白分析，未评估其来源。")
	}
	if scene.PromptStale {
		add("saved_prompt_stale", "attention", "已有提示词被标记为来源已变，手工稿仍保留。")
	} else {
		add("saved_prompt_stale", "ok", "未记录到提示词过期；未进行剧情语义认证。")
	}
	retained := 0
	available := map[string]int{}
	for _, d := range effective {
		available[d.Text]++
	}
	for _, d := range raw {
		if d.Source != "user_excluded" && d.Text != "" && available[d.Text] > 0 {
			available[d.Text]--
			retained++
		}
	}
	coverageStatus, coverageMessage := assessCurrentStorySpeechCoverage(&scene, effective)
	add("dialogue_source_coverage", coverageStatus, coverageMessage)
	if scene.DirectorSourceStale {
		add("shot_dialogue_consistency", "not_assessed", "导演设计已过期，不以旧镜头对白检验新正文。")
	} else {
		var shots []models.Shot
		if err := s.DB.Where("scene_id = ?", scene.ID).Order("order_num").Find(&shots).Error; err != nil {
			c.JSON(500, gin.H{"error": err.Error()})
			return
		}
		if len(shots) == 0 {
			add("shot_dialogue_consistency", "not_assessed", "暂无镜头，未评估镜头对白覆盖。")
		} else {
			var shotText strings.Builder
			for _, sh := range shots {
				shotText.WriteString(sh.Dialogue)
			}
			canonicalShots := canonicalDialogueText(shotText.String())
			var expectedShots strings.Builder
			for _, d := range effective {
				expectedShots.WriteString(d.Text)
			}
			missing := canonicalShots != canonicalDialogueText(expectedShots.String())
			if missing {
				add("shot_dialogue_consistency", "attention", "现有镜头对白与有效对白的拼接顺序或出现次数不同。")
			} else {
				add("shot_dialogue_consistency", "ok", "现有镜头台词拼接与有效对白一致（规范化后）。")
			}
		}
	}
	add("narrative_coverage", "not_assessed", "未评估全文剧情是否完整或合乎常识；字面覆盖不代表语义正确。")
	sort.SliceStable(checks, func(i, j int) bool { return checks[i].Code < checks[j].Code })
	status := "no_known_issues"
	for _, ch := range checks {
		if ch.Status == "attention" {
			status = "attention"
			break
		}
	}
	c.JSON(200, StoryConsistencyReport{SceneID: scene.ID, ContentHash: sceneDialogueContentHash(scene.Content), Advisory: true, Status: status, Checks: checks, DialogueCount: len(effective), RetainedDialogueCount: retained, ExcludedDialogueCount: currentSceneDialogueExclusionCount(&scene)})
}

// assessCurrentStorySpeechCoverage compares the complete classified quoted-speech
// stream with the effective production stream. Concatenation deliberately permits a
// single source utterance to be cut into adjacent production fragments without
// weakening punctuation, duplicate, or ordering checks.
func assessCurrentStorySpeechCoverage(sc *models.Scene, effective []models.Dialogue) (string, string) {
	if sc == nil {
		return "not_assessed", "暂无场景正文，未评估对白覆盖。"
	}
	content := sceneDialogueGenerationContent(sc)
	quoted, spans := extractQuotedDialogueSpans(content)
	var expected strings.Builder
	classified := 0
	for i := range quoted {
		if sourceQuoteIsSpeech(content, []int{spans[i].start, spans[i].end, spans[i].start, spans[i].end}) {
			expected.WriteString(quoted[i].Text)
			classified++
		}
	}
	if classified == 0 {
		if currentSceneDialogueExclusionCount(sc) > 0 {
			return "not_assessed", "当前可分类引号台词均已明确排除，未要求其出现在有效对白中。"
		}
		return "not_assessed", "未找到可确定分类的引号台词，未进行全文语义推断。"
	}
	var actual strings.Builder
	for _, d := range effective {
		actual.WriteString(d.Text)
	}
	want, got := canonicalDialogueText(expected.String()), canonicalDialogueText(actual.String())
	if want == got {
		return "ok", "当前未排除的可分类引号台词与有效对白拼接一致。"
	}
	// Extra effective text can be legitimate unquoted, explicitly labelled speech.
	// This check cannot safely infer that mixed source shape, so avoid a false warning.
	if at := strings.Index(got, want); at >= 0 {
		// Additional text is not automatically a mixed-source exception: it
		// must actually occur outside source quotes. Otherwise duplicate
		// production lines would be silently dismissed as unquoted speech.
		var outside strings.Builder
		last := 0
		for _, span := range spans {
			outside.WriteString(content[last:span.start])
			last = span.end
		}
		outside.WriteString(content[last:])
		extra := got[:at] + got[at+len(want):]
		if extra != "" && strings.Contains(canonicalDialogueText(outside.String()), extra) {
			return "not_assessed", "有效对白还包含引号外文本，无法仅凭引号安全评估混合来源覆盖。"
		}
	}
	return "attention", "当前未排除的引号台词与有效对白的拼接顺序、内容或出现次数不同。"
}

func storyConsistencyDBError(c *gin.Context, err error) {
	if errors.Is(err, gorm.ErrRecordNotFound) {
		c.JSON(404, gin.H{"error": "not found"})
	} else {
		c.JSON(500, gin.H{"error": err.Error()})
	}
}
