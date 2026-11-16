package service

import (
	"fmt"
	"net/http"
	"strconv"

	"github.com/gin-gonic/gin"

	"comfyui-console/internal/models"
)

// Episode continuity chain preview. It reads only the explicit SceneContinuity relations
// (source_scene_id) of the episode's current generation — never Scene order, names or file
// presence — and reports, per Scene, which upstream Scene it waits for and whether submission
// would pass today. It is read-only: nothing is submitted, configured or invalidated.

type ContinuityChainItem struct {
	SceneID       uint   `json:"scene_id"`
	Order         int    `json:"order"`
	Title         string `json:"title"`
	Mode          string `json:"mode"`
	ReferenceForm string `json:"reference_form,omitempty"`
	SourceSceneID *uint  `json:"source_scene_id,omitempty"`
	ChainID       uint   `json:"chain_id"` // root Scene of the chain
	Depth         int    `json:"depth"`    // 0 for a root; a Scene can start once its source is done
	VideoStatus   string `json:"video_status"`
	// State: done / generating / ready / waiting_source / needs_action / invalid_source.
	State   string `json:"state"`
	WaitFor *uint  `json:"wait_for_scene_id,omitempty"`
	Message string `json:"message,omitempty"`
}

type ContinuityChain struct {
	ChainID  uint   `json:"chain_id"`
	SceneIDs []uint `json:"scene_ids"`
}

type ContinuityChainPlan struct {
	EpisodeN   int                   `json:"episode_n"`
	Generation uint                  `json:"generation"`
	Items      []ContinuityChainItem `json:"items"`
	Chains     []ContinuityChain     `json:"chains"`
	StartNow   []uint                `json:"start_now"` // ready Scenes with no unfinished upstream
}

func sceneVideoGenerating(status string) bool {
	return status == "video_creating" || status == "video_pending" || status == "video_running"
}

// EpisodeContinuityChain builds the read-only chain plan for one episode.
func (s *ProjectService) EpisodeContinuityChain(projectID uint, episodeN int) (*ContinuityChainPlan, error) {
	generation, err := latestEpisodeGeneration(s.db, projectID, episodeN)
	if err != nil {
		return nil, err
	}
	plan := &ContinuityChainPlan{EpisodeN: episodeN, Generation: generation, Items: []ContinuityChainItem{}, Chains: []ContinuityChain{}, StartNow: []uint{}}
	var scenes []models.Scene
	if err := s.db.Where("project_id = ? AND episode_n = ? AND generation = ? AND (archived = ? OR archived IS NULL)", projectID, episodeN, generation, false).
		Order("`order`, id").Find(&scenes).Error; err != nil {
		return nil, err
	}
	if len(scenes) == 0 {
		return plan, nil
	}
	byID := make(map[uint]*models.Scene, len(scenes))
	ids := make([]uint, 0, len(scenes))
	for i := range scenes {
		byID[scenes[i].ID] = &scenes[i]
		ids = append(ids, scenes[i].ID)
	}
	var cfgs []models.SceneContinuity
	if err := s.db.Where("scene_id IN ?", ids).Find(&cfgs).Error; err != nil {
		return nil, err
	}
	cfgOf := make(map[uint]*models.SceneContinuity, len(cfgs))
	for i := range cfgs {
		if cfgs[i].Mode != "" && cfgs[i].Mode != models.ContinuityModeIndependent {
			cfgOf[cfgs[i].SceneID] = &cfgs[i]
		}
	}

	// Resolve each Scene's chain root and depth by walking explicit source links. A source
	// outside this episode generation, or a cycle, makes the Scene its own invalid root.
	type link struct {
		root    uint
		depth   int
		invalid string
	}
	links := map[uint]link{}
	var resolve func(id uint, seen map[uint]bool) link
	resolve = func(id uint, seen map[uint]bool) link {
		if l, ok := links[id]; ok {
			return l
		}
		cfg := cfgOf[id]
		if cfg == nil {
			links[id] = link{root: id}
			return links[id]
		}
		if cfg.SourceSceneID == nil || byID[*cfg.SourceSceneID] == nil {
			links[id] = link{root: id, invalid: "来源场景不在本集当前版本中（可能已删除、归档或属于旧版本），请重新设置连续性"}
			return links[id]
		}
		if seen[id] {
			links[id] = link{root: id, invalid: "连续性来源形成环，请重新设置连续性"}
			return links[id]
		}
		seen[id] = true
		parent := resolve(*cfg.SourceSceneID, seen)
		if l, ok := links[id]; ok { // set by cycle detection further down the walk
			return l
		}
		links[id] = link{root: parent.root, depth: parent.depth + 1}
		return links[id]
	}
	for _, id := range ids {
		resolve(id, map[uint]bool{})
	}

	chainIndex := map[uint]int{}
	for _, sc := range scenes {
		l := links[sc.ID]
		item := ContinuityChainItem{SceneID: sc.ID, Order: sc.Order, Title: sc.Title, Mode: models.ContinuityModeIndependent,
			ChainID: l.root, Depth: l.depth, VideoStatus: sc.Status}
		if cfg := cfgOf[sc.ID]; cfg != nil {
			item.Mode, item.ReferenceForm, item.SourceSceneID = cfg.Mode, cfg.ReferenceForm, cfg.SourceSceneID
		}
		switch {
		case l.invalid != "":
			item.State, item.Message = "invalid_source", l.invalid
		case sc.Status == "video_ready":
			item.State = "done"
		case sceneVideoGenerating(sc.Status):
			item.State = "generating"
		case item.SourceSceneID != nil && byID[*item.SourceSceneID].Status != "video_ready":
			item.State, item.WaitFor = "waiting_source", item.SourceSceneID
			item.Message = fmt.Sprintf("等待场景%d出片并确认尾帧", byID[*item.SourceSceneID].Order)
		default:
			// Same checks as real submission, applied to a copy so nothing is written.
			probe := sc
			var err error
			if s.continuity != nil {
				err = s.continuity.PrepareScene(&probe)
			}
			if err != nil {
				item.State, item.Message = "needs_action", err.Error()
				if item.SourceSceneID != nil {
					item.WaitFor = item.SourceSceneID
				}
			} else {
				item.State = "ready"
				plan.StartNow = append(plan.StartNow, sc.ID)
			}
		}
		plan.Items = append(plan.Items, item)
		if idx, ok := chainIndex[l.root]; ok {
			plan.Chains[idx].SceneIDs = append(plan.Chains[idx].SceneIDs, sc.ID)
		} else {
			chainIndex[l.root] = len(plan.Chains)
			plan.Chains = append(plan.Chains, ContinuityChain{ChainID: l.root, SceneIDs: []uint{sc.ID}})
		}
	}
	return plan, nil
}

func (s *Service) HandleEpisodeContinuityChain(c *gin.Context) {
	p, ok := s.loadProject(c)
	if !ok {
		return
	}
	n, err := strconv.Atoi(c.Param("number"))
	if err != nil || n < 1 {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid episode number"})
		return
	}
	plan, err := s.Projects.EpisodeContinuityChain(p.ID, n)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, plan)
}
