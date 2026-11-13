package service

import (
	"encoding/json"
	"errors"
	"fmt"
	"reflect"
	"sort"
	"strings"

	"comfyui-console/internal/models"
	"gorm.io/gorm"
)

type SceneReferenceSelection struct {
	SourceType     string `json:"source_type"`
	SourceID       uint   `json:"source_id"`
	Variant        string `json:"variant"`
	AssetVariantID uint   `json:"asset_variant_id,omitempty"`
	UseKrea2       bool   `json:"use_krea2"`
	UseH3          bool   `json:"use_h3"`
	BindCharacter  string `json:"bind_character,omitempty"`
}

type SceneReferenceCandidate struct {
	SceneReferenceSelection
	Key      string `json:"key"`
	Label    string `json:"label"`
	Category string `json:"category"`
	Image    string `json:"image"`
}

func referenceKey(r SceneReferenceSelection) string {
	base := fmt.Sprintf("%s:%d:%s", r.SourceType, r.SourceID, r.Variant)
	if r.AssetVariantID == 0 {
		return base
	}
	return fmt.Sprintf("%s:%d", base, r.AssetVariantID)
}

func (s *ProjectService) sceneReferenceCandidates(sc *models.Scene) []SceneReferenceCandidate {
	pid := sc.ProjectID
	out := make([]SceneReferenceCandidate, 0)
	var chars []models.Character
	s.db.Where("project_id = ?", pid).Order("id").Find(&chars)
	aliasesByCanonical := map[string][]string{}
	if s.db.Migrator().HasTable(&models.CharacterAliasCandidate{}) {
		var aliases []models.CharacterAliasCandidate
		s.db.Where("project_id = ? AND status = ?", pid, "confirmed").Order("id").Find(&aliases)
		for _, alias := range aliases {
			canonical, value := strings.TrimSpace(alias.CanonicalName), strings.TrimSpace(alias.Alias)
			if canonical != "" && value != "" && canonical != value {
				aliasesByCanonical[canonical] = append(aliasesByCanonical[canonical], value)
			}
		}
	}
	characterLabel := func(name, suffix string) string {
		label := "角色「" + name + "」" + suffix
		for _, alias := range aliasesByCanonical[name] {
			label += "；别名「" + alias + "」"
		}
		return label
	}
	for _, ch := range chars {
		if ch.Portrait != "" {
			out = append(out, candidate("character", ch.ID, "portrait", characterLabel(ch.Name, "标准像"), "character", ch.Portrait))
		}
		if ch.Sheet != "" {
			out = append(out, candidate("character", ch.ID, "sheet", characterLabel(ch.Name, "四视图"), "character", ch.Sheet))
		}
	}
	// Merges can retain a source character's selected file as a historical variant.
	// Address it explicitly so rewriting the owner ID never silently changes the image.
	if s.db.Migrator().HasTable(&models.AssetVariant{}) {
		charactersByID := make(map[uint]models.Character, len(chars))
		for _, ch := range chars {
			charactersByID[ch.ID] = ch
		}
		var variants []models.AssetVariant
		s.db.Where("project_id = ? AND entity_type IN ?", pid, []string{VariantCharacterPortrait, VariantCharacterSheet}).Order("id").Find(&variants)
		seenVariantFiles := map[string]bool{}
		for _, v := range variants {
			ch, exists := charactersByID[v.EntityID]
			if !exists {
				continue
			}
			variant, suffix, currentFile := "portrait", "标准照", ch.Portrait
			if v.EntityType == VariantCharacterSheet {
				variant, suffix, currentFile = "sheet", "四视图", ch.Sheet
			}
			// The Character row is the current projection and is already listed above.
			// Merge preservation can materialize that same file as an AssetVariant; showing
			// both creates two visually identical portrait/sheet choices.
			if strings.TrimSpace(v.File) == "" || strings.TrimSpace(v.File) == strings.TrimSpace(currentFile) {
				continue
			}
			fileKey := fmt.Sprintf("%d:%s:%s", v.EntityID, variant, strings.TrimSpace(v.File))
			if seenVariantFiles[fileKey] {
				continue
			}
			seenVariantFiles[fileKey] = true
			ref := candidate("character", v.EntityID, variant, characterLabel(ch.Name, suffix)+"（历史版本）", "character", v.File)
			ref.AssetVariantID = v.ID
			ref.Key = referenceKey(ref.SceneReferenceSelection)
			out = append(out, ref)
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
	if s.db.Migrator().HasTable(&models.CharacterMergeRedirect{}) {
		var redirects []models.CharacterMergeRedirect
		s.db.Where("project_id = ?", projectID).Find(&redirects)
		next := map[uint]uint{}
		bySource := map[uint]models.CharacterMergeRedirect{}
		for _, redirect := range redirects {
			next[redirect.SourceCharacterID] = redirect.TargetCharacterID
			bySource[redirect.SourceCharacterID] = redirect
		}
		for i := range refs {
			if refs[i].SourceType != "character" {
				continue
			}
			seen := map[uint]bool{}
			for next[refs[i].SourceID] != 0 && !seen[refs[i].SourceID] {
				seen[refs[i].SourceID] = true
				redirect := bySource[refs[i].SourceID]
				if refs[i].AssetVariantID == 0 {
					if refs[i].Variant == "portrait" {
						refs[i].AssetVariantID = redirect.SourcePortraitVariantID
					} else if refs[i].Variant == "sheet" {
						refs[i].AssetVariantID = redirect.SourceSheetVariantID
					}
				}
				refs[i].SourceID = redirect.TargetCharacterID
			}
		}
	}
	// Collapse AssetVariant rows that duplicate the current Character projection, and
	// canonicalize duplicate historical rows created by older merge implementations.
	if s.db.Migrator().HasTable(&models.AssetVariant{}) {
		var characters []models.Character
		s.db.Where("project_id = ?", projectID).Find(&characters)
		charactersByID := map[uint]models.Character{}
		for _, character := range characters {
			charactersByID[character.ID] = character
		}
		var variants []models.AssetVariant
		s.db.Where("project_id = ? AND entity_type IN ?", projectID, []string{VariantCharacterPortrait, VariantCharacterSheet}).Order("id").Find(&variants)
		variantsByID := map[uint]models.AssetVariant{}
		canonicalByFile := map[string]uint{}
		for _, variant := range variants {
			variantsByID[variant.ID] = variant
			kind := "portrait"
			if variant.EntityType == VariantCharacterSheet {
				kind = "sheet"
			}
			key := fmt.Sprintf("%d:%s:%s", variant.EntityID, kind, strings.TrimSpace(variant.File))
			if canonicalByFile[key] == 0 {
				canonicalByFile[key] = variant.ID
			}
		}
		for i := range refs {
			if refs[i].SourceType != "character" || refs[i].AssetVariantID == 0 {
				continue
			}
			variant, exists := variantsByID[refs[i].AssetVariantID]
			character, characterExists := charactersByID[refs[i].SourceID]
			if !exists || !characterExists || variant.EntityID != refs[i].SourceID {
				continue
			}
			currentFile, kind := character.Portrait, "portrait"
			if refs[i].Variant == "sheet" {
				currentFile, kind = character.Sheet, "sheet"
			}
			if strings.TrimSpace(variant.File) == strings.TrimSpace(currentFile) {
				refs[i].AssetVariantID = 0
				continue
			}
			key := fmt.Sprintf("%d:%s:%s", variant.EntityID, kind, strings.TrimSpace(variant.File))
			refs[i].AssetVariantID = canonicalByFile[key]
		}
	}
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

func partitionSceneReferences(refs []SceneReferenceSelection, candidates []SceneReferenceCandidate) (valid, invalid []SceneReferenceSelection) {
	allowed := make(map[string]bool, len(candidates))
	for _, candidate := range candidates {
		allowed[candidate.Key] = true
	}
	valid = make([]SceneReferenceSelection, 0, len(refs))
	invalid = make([]SceneReferenceSelection, 0)
	for _, reference := range refs {
		if allowed[referenceKey(reference)] {
			valid = append(valid, reference)
		} else {
			invalid = append(invalid, reference)
		}
	}
	return valid, invalid
}

func (s *ProjectService) SaveSceneReferences(sc *models.Scene, refs []SceneReferenceSelection) error {
	refs = s.normalizeSceneReferences(sc.ProjectID, refs)
	candidates := s.sceneReferenceCandidates(sc)
	allowed := make(map[string]bool, len(candidates))
	for _, c := range candidates {
		allowed[c.Key] = true
	}
	allowedBindings := map[string]bool{}
	for _, raw := range []string{sc.Characters, sc.VisibleCharacters, sc.VoiceCharacters, sc.MentionedCharacters} {
		for _, name := range parseSceneCharacters(raw) {
			allowedBindings[name] = true
		}
	}
	seen, bound, krea, h3 := map[string]bool{}, map[string]bool{}, 0, 0
	for i := range refs {
		refs[i].BindCharacter = strings.TrimSpace(refs[i].BindCharacter)
		ref := refs[i]
		key := referenceKey(ref)
		if !allowed[key] {
			return fmt.Errorf("参考图不存在、未审核或不属于当前项目: %s", key)
		}
		if seen[key] {
			return fmt.Errorf("参考图重复: %s", key)
		}
		seen[key] = true
		if ref.BindCharacter != "" {
			if ref.SourceType != "character" && ref.SourceType != "look" && ref.SourceType != "outfit" {
				return fmt.Errorf("只有人物、造型或套装参考图可以绑定角色: %s", key)
			}
			if !allowedBindings[ref.BindCharacter] {
				return fmt.Errorf("参考图绑定角色不属于当前Scene: %s", ref.BindCharacter)
			}
			if bound[ref.BindCharacter] {
				return fmt.Errorf("角色%s重复绑定了多张身份参考图", ref.BindCharacter)
			}
			bound[ref.BindCharacter] = true
		}
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
	updates := map[string]any{
		"reference_images_json": string(data),
		"prompt_stale":          strings.TrimSpace(sc.VideoFullPrompt) != "",
	}
	cancelTasks := staleSceneTaskUpdates(s.db, sc, updates)
	if err := s.db.Model(sc).Updates(updates).Error; err != nil {
		return err
	}
	cancelSceneTasks(s.tasks, cancelTasks)
	return markSceneMediaSourceChanged(s.db, sc.ProjectID, sc.ID, "", "场景参考图已修改")
}

// Source edits are advisory, not a media adoption action. Preserve is_current so
// an existing user-selected candidate cannot become deletable or disappear from selection.
func markSceneMediaSourceChanged(db *gorm.DB, projectID, sceneID uint, mediaType, reason string) error {
	q := db.Model(&models.GenerationCandidate{}).Where("project_id = ? AND entity_type = ? AND entity_id = ?", projectID, "scene", sceneID)
	if mediaType != "" {
		q = q.Where("media_type = ?", mediaType)
	}
	return q.Updates(map[string]any{"stale": true, "stale_reason": reason}).Error
}

// staleSceneTaskUpdates disconnects only in-flight work so an old task cannot write
// results generated from superseded references. Completed media and its provenance stay intact.
func staleSceneTaskUpdates(db interface {
	Where(query interface{}, args ...interface{}) *gorm.DB
}, sc *models.Scene, updates map[string]any) []string {
	var cancel []string
	for _, state := range []struct {
		taskID, taskField, output string
	}{{sc.ImageTaskID, "image_task_id", sc.ImageFile}, {sc.VideoTaskID, "video_task_id", sc.VideoFile}} {
		if state.taskID == "" {
			continue
		}
		var task models.Task
		err := db.Where("task_id = ?", state.taskID).First(&task).Error
		active := err == nil && task.Status != "success" && task.Status != "failed" && task.Status != "cancelled"
		if errors.Is(err, gorm.ErrRecordNotFound) && strings.TrimSpace(state.output) == "" {
			active = true
		}
		if active {
			updates[state.taskField] = ""
			cancel = append(cancel, state.taskID)
		}
	}
	if len(cancel) > 0 {
		switch {
		case strings.TrimSpace(sc.VideoFile) != "":
			updates["status"] = "video_ready"
		case strings.TrimSpace(sc.ImageFile) != "":
			updates["status"] = "image_ready"
		default:
			updates["status"] = "pending"
		}
	}
	return cancel
}

func cancelSceneTasks(tasks *TaskService, taskIDs []string) {
	if tasks == nil {
		return
	}
	for _, taskID := range taskIDs {
		_ = tasks.CancelTask(taskID)
	}
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
	// 显式参考列表是用户最终决定：不自动补回被取消的标准像或旧四视图。
	// 这个顺序对分镜图和后续视频必须完全一致：两份提示词共用同一套 <Picture N>
	// 编号，任何一侧单独重排都会让同一个编号在两份提示词里指向不同的人物。
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
	// 稳定排序：同一优先级内保留用户在界面上用 ↑↓ 调整的相对顺序。
	sort.SliceStable(selected, func(i, j int) bool { return priority(selected[i]) < priority(selected[j]) })
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
		label := c.Label
		if r.BindCharacter != "" && (r.SourceType == "character" || r.SourceType == "look" || r.SourceType == "outfit") {
			label = fmt.Sprintf("角色「%s」（由%s显式绑定，仅用于该Scene的身份外观）", r.BindCharacter, c.Label)
		}
		lines = append(lines, fmt.Sprintf("- <Picture %d>：%s", len(refs), label))
	}
	return refs, lines, true
}
