package service

import (
	"encoding/json"
	"fmt"
	"reflect"
	"sort"
	"strings"

	"comfyui-console/internal/models"
)

type SceneReferenceSelection struct {
	SourceType string `json:"source_type"`
	SourceID   uint   `json:"source_id"`
	Variant    string `json:"variant"`
	UseKrea2   bool   `json:"use_krea2"`
	UseH3      bool   `json:"use_h3"`
}

type SceneReferenceCandidate struct {
	SceneReferenceSelection
	Key      string `json:"key"`
	Label    string `json:"label"`
	Category string `json:"category"`
	Image    string `json:"image"`
}

func referenceKey(r SceneReferenceSelection) string {
	return fmt.Sprintf("%s:%d:%s", r.SourceType, r.SourceID, r.Variant)
}

func (s *ProjectService) sceneReferenceCandidates(sc *models.Scene) []SceneReferenceCandidate {
	pid := sc.ProjectID
	out := make([]SceneReferenceCandidate, 0)
	var chars []models.Character
	s.db.Where("project_id = ?", pid).Order("id").Find(&chars)
	for _, ch := range chars {
		if ch.Portrait != "" {
			out = append(out, candidate("character", ch.ID, "portrait", "角色「"+ch.Name+"」标准像", "character", ch.Portrait))
		}
		if ch.Sheet != "" {
			out = append(out, candidate("character", ch.ID, "sheet", "角色「"+ch.Name+"」四视图", "character", ch.Sheet))
		}
	}
	var looks []models.CharacterLook
	s.db.Where("project_id = ? AND image != '' AND audit_status IN ?", pid, []models.CharacterLookStatus{models.LookStatusApproved, models.LookStatusPublished}).Order("priority DESC, id").Find(&looks)
	for _, look := range looks {
		out = append(out, candidate("look", look.ID, "image", "造型「"+look.Name+"」（"+LookCategoryLabel(look.Category)+"）", "look", look.Image))
	}
	var outfits []models.CharacterOutfit
	s.db.Preload("Character").Where("project_id = ? AND audit_status IN ?", pid, []models.CharacterLookStatus{models.LookStatusApproved, models.LookStatusPublished}).Order("character_id, id").Find(&outfits)
	for _, outfit := range outfits {
		image, variant := outfit.Sheet, "sheet"
		if image == "" {
			image, variant = outfit.Image, "image"
		}
		if image == "" {
			continue
		}
		characterName := "角色"
		if outfit.Character != nil {
			characterName = outfit.Character.Name
		}
		out = append(out, candidate("outfit", outfit.ID, variant, "角色「"+characterName+"」新形象「"+outfit.Name+"」", "outfit", image))
	}
	var assets []models.Asset
	s.db.Where("project_id = ?", pid).Order("kind, id").Find(&assets)
	for _, a := range assets {
		if a.Image != "" {
			out = append(out, candidate("asset", a.ID, "image", AssetKindLabel(a.Kind)+"「"+a.Name+"」参考图", a.Kind, a.Image))
		}
		if a.Kind == AssetKindProp && a.Sheet != "" {
			out = append(out, candidate("asset", a.ID, "sheet", "道具「"+a.Name+"」四视图", a.Kind, a.Sheet))
		}
	}
	shared, _ := NewSharedAssetReferenceService(s.db).ListResolvedForScene(pid, sc.ID)
	for _, item := range shared {
		id, variant, scope := item.Material.ID, "global-live", "全局"
		if item.Reference != nil {
			id, variant, scope = item.Reference.ID, fmt.Sprintf("ref-%d-%s", item.Reference.ID, item.Mode), "项目"
			if item.Reference.SceneID != nil {
				scope = "场景"
			}
			if item.Reference.ShotID != nil {
				scope = "镜头"
			}
		}
		out = append(out, candidate("shared_material", id, variant, fmt.Sprintf("共享素材「%s」· %s/%s", item.Material.Name, scope, item.Mode), "shared", item.Path))
	}
	return out
}

func candidate(kind string, id uint, variant, label, category, image string) SceneReferenceCandidate {
	r := SceneReferenceSelection{SourceType: kind, SourceID: id, Variant: variant, UseKrea2: true, UseH3: true}
	return SceneReferenceCandidate{SceneReferenceSelection: r, Key: referenceKey(r), Label: label, Category: category, Image: image}
}

func fileMetaForSharedMaterial(projectID uint, path string) FileMeta {
	path = strings.TrimLeft(strings.ReplaceAll(strings.TrimSpace(path), "\\", "/"), "/")
	if i := strings.Index(path, "/"); i > 0 && i < len(path)-1 {
		return FileMeta{TaskID: path[:i], Name: path[i+1:]}
	}
	return FileMeta{TaskID: fmt.Sprint(projectID), Name: path}
}

func parseSceneReferences(sc *models.Scene) ([]SceneReferenceSelection, bool) {
	if strings.TrimSpace(sc.ReferenceImagesJSON) == "" {
		return nil, false
	}
	var refs []SceneReferenceSelection
	if json.Unmarshal([]byte(sc.ReferenceImagesJSON), &refs) != nil {
		return nil, false
	}
	return refs, true
}

// normalizeSceneReferences makes a selected complete outfit authoritative for that character.
// A complete outfit image/sheet already contains identity and styling, so keeping the old
// character portrait/sheet creates two competing Subjects for the same person.
func (s *ProjectService) normalizeSceneReferences(projectID uint, refs []SceneReferenceSelection) []SceneReferenceSelection {
	outfitIDs := make([]uint, 0)
	for _, ref := range refs {
		if ref.SourceType == "outfit" {
			outfitIDs = append(outfitIDs, ref.SourceID)
		}
	}
	if len(outfitIDs) == 0 {
		return refs
	}
	var outfits []models.CharacterOutfit
	s.db.Where("project_id = ? AND id IN ?", projectID, outfitIDs).Find(&outfits)
	replacedCharacters := map[uint]bool{}
	for _, outfit := range outfits {
		replacedCharacters[outfit.CharacterID] = true
	}
	filtered := make([]SceneReferenceSelection, 0, len(refs))
	for _, ref := range refs {
		if ref.SourceType == "character" && replacedCharacters[ref.SourceID] {
			continue
		}
		filtered = append(filtered, ref)
	}
	return filtered
}

func (s *ProjectService) SaveSceneReferences(sc *models.Scene, refs []SceneReferenceSelection) error {
	refs = s.normalizeSceneReferences(sc.ProjectID, refs)
	candidates := s.sceneReferenceCandidates(sc)
	allowed := make(map[string]bool, len(candidates))
	for _, c := range candidates {
		allowed[c.Key] = true
	}
	seen, krea, h3 := map[string]bool{}, 0, 0
	for _, ref := range refs {
		key := referenceKey(ref)
		if !allowed[key] {
			return fmt.Errorf("参考图不存在、未审核或不属于当前项目: %s", key)
		}
		if seen[key] {
			return fmt.Errorf("参考图重复: %s", key)
		}
		seen[key] = true
		if ref.UseKrea2 {
			krea++
		}
		if ref.UseH3 {
			h3++
		}
	}
	if krea > maxSceneReferenceImages {
		return fmt.Errorf("MiniMax H3 SelfLift 分镜候选最多 %d 张参考图", maxSceneReferenceImages)
	}
	if h3 > maxSceneReferenceImages-1 {
		return fmt.Errorf("H3 除起始帧外最多 %d 张参考图", maxSceneReferenceImages-1)
	}
	// 场景编辑器保存其他字段时也会回传当前参考图。引用未变化时不能
	// 清空已经生成的分镜图，否则只改时长也会误删画面。
	if current, explicit := parseSceneReferences(sc); explicit && reflect.DeepEqual(current, refs) {
		return nil
	}
	data, _ := json.Marshal(refs)
	if err := s.db.Model(sc).Updates(map[string]any{
		"reference_images_json": string(data), "prompt_stale": strings.TrimSpace(sc.VideoFullPrompt) != "", "image_file": "", "image_task_id": "",
		"video_file": "", "video_input_file": "", "video_task_id": "", "video_gpu": nil,
		"status": "pending", "error": "",
	}).Error; err != nil {
		return err
	}
	return MarkSceneCandidatesStale(s.db, sc.ProjectID, sc.ID, "", "场景参考图已修改")
}

func (s *ProjectService) selectedSceneReferenceFiles(sc *models.Scene, target string) ([]FileMeta, []string, bool) {
	selected, explicit := parseSceneReferences(sc)
	if !explicit {
		return nil, nil, false
	}
	selected = s.normalizeSceneReferences(sc.ProjectID, selected)
	byKey := map[string]SceneReferenceCandidate{}
	for _, c := range s.sceneReferenceCandidates(sc) {
		byKey[c.Key] = c
	}
	// 场景已分配的新形象是权威造型选择，即使用户保存过显式参考图列表也必须自动补入。
	selectedKeys := make(map[string]bool, len(selected))
	for _, r := range selected {
		selectedKeys[referenceKey(r)] = true
	}
	assignedOutfitList := s.sceneCharacterOutfits(sc)
	assignedOutfits := map[uint]models.CharacterOutfit{}
	for _, outfit := range assignedOutfitList {
		assignedOutfits[outfit.CharacterID] = outfit
	}
	if len(assignedOutfits) > 0 {
		filtered := selected[:0]
		for _, ref := range selected {
			if ref.SourceType == "character" {
				if _, replacedByOutfit := assignedOutfits[ref.SourceID]; replacedByOutfit {
					continue
				}
			}
			filtered = append(filtered, ref)
		}
		selected = filtered
		selectedKeys = make(map[string]bool, len(selected))
		for _, ref := range selected {
			selectedKeys[referenceKey(ref)] = true
		}
	}
	for _, outfit := range assignedOutfitList {
		variant := "sheet"
		if outfit.Sheet == "" {
			variant = "image"
		}
		ref := SceneReferenceSelection{SourceType: "outfit", SourceID: outfit.ID, Variant: variant, UseKrea2: true, UseH3: true}
		if !selectedKeys[referenceKey(ref)] {
			selected = append(selected, ref)
			selectedKeys[referenceKey(ref)] = true
		}
	}
	if target == "krea2" { // legacy field name; this target is MiniMax H3 SelfLift scene generation
		// 可见角色的身份参考是场景生成的必需输入。旧项目可能显式列表里只有场景图，
		// 此时自动补四视图（标准像兜底），避免 H3 在没有人物参考时自行脑补。
		selectedCharacterIDs := map[uint]bool{}
		selectedOutfitIDs := make([]uint, 0)
		for _, r := range selected {
			if r.SourceType == "character" && r.UseKrea2 {
				selectedCharacterIDs[r.SourceID] = true
			}
			if r.SourceType == "outfit" && r.UseKrea2 {
				selectedOutfitIDs = append(selectedOutfitIDs, r.SourceID)
			}
		}
		if len(selectedOutfitIDs) > 0 {
			var selectedOutfits []models.CharacterOutfit
			s.db.Where("project_id = ? AND id IN ?", sc.ProjectID, selectedOutfitIDs).Find(&selectedOutfits)
			for _, outfit := range selectedOutfits {
				selectedCharacterIDs[outfit.CharacterID] = true
			}
		}
		assignedOutfits := s.sceneOutfitsByCharacter(sc)
		for _, ch := range s.sceneCharacterPortraits(sc) {
			if selectedCharacterIDs[ch.ID] {
				continue
			}
			if _, selectedOutfit := assignedOutfits[ch.ID]; selectedOutfit {
				continue
			}
			variant := "sheet"
			if ch.Sheet == "" {
				variant = "portrait"
			}
			ref := SceneReferenceSelection{SourceType: "character", SourceID: ch.ID, Variant: variant, UseKrea2: true}
			if !selectedKeys[referenceKey(ref)] {
				selected = append(selected, ref)
				selectedKeys[referenceKey(ref)] = true
			}
		}

		priority := func(r SceneReferenceSelection) int {
			// SelfLift 对前序图片权重更敏感：人物身份图必须先于环境图，
			// 否则模型容易复刻空场景而忽略动作主体。
			if r.SourceType == "character" || r.SourceType == "outfit" {
				return 0
			}
			if r.SourceType == "look" {
				return 1
			}
			c, ok := byKey[referenceKey(r)]
			if ok && c.Category == AssetKindLocation {
				return 2
			}
			return 3
		}
		sort.SliceStable(selected, func(i, j int) bool { return priority(selected[i]) < priority(selected[j]) })
	}
	refs, lines := []FileMeta{}, []string{}
	for _, r := range selected {
		if (target == "krea2" && !r.UseKrea2) || (target == "h3" && !r.UseH3) {
			continue
		}
		c, ok := byKey[referenceKey(r)]
		if !ok {
			continue
		}
		// 显式勾选是用户的最终决定。不要再按Shot/Characters元数据静默丢弃，
		// 否则界面显示已选三张，实际提示词和任务却只提交两张。
		// 自动补入的角色仍由 sceneCharacterPortraits 按镜头可见性筛选。
		if r.SourceType == "shared_material" {
			refs = append(refs, fileMetaForSharedMaterial(sc.ProjectID, c.Image))
		} else {
			refs = append(refs, FileMeta{TaskID: fmt.Sprint(sc.ProjectID), Name: c.Image})
		}
		lines = append(lines, fmt.Sprintf("- <Picture %d>：%s", len(refs), c.Label))
	}
	return refs, lines, true
}
