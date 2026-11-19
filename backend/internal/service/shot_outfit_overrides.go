package service

import (
	"fmt"
	"regexp"
	"sort"
	"strings"

	"comfyui-console/internal/models"
	"gorm.io/gorm"
)

// 镜头级造型覆盖（ADR-0012）：Shot 1 的覆盖决定开场造型；后续镜头的换装以额外参考图和
// 时间线换装句进入 H3 视频提示词。没有任何镜头覆盖时，所有输出与此前逐字一致。

var approvedOutfitStatuses = []models.CharacterLookStatus{models.LookStatusApproved, models.LookStatusPublished}

// sceneLevelOutfits 是场景级选择的已审核套装（不含镜头覆盖）。
func sceneLevelOutfits(db *gorm.DB, sc *models.Scene) []models.CharacterOutfit {
	var outfits []models.CharacterOutfit
	_ = db.Model(&models.CharacterOutfit{}).
		Joins("JOIN scene_character_outfits ON scene_character_outfits.outfit_id = character_outfits.id").
		Where("scene_character_outfits.scene_id = ? AND character_outfits.project_id = ? AND character_outfits.audit_status IN ?", sc.ID, sc.ProjectID, approvedOutfitStatuses).
		Preload("Character").Order("scene_character_outfits.id").Find(&outfits).Error
	return outfits
}

// sceneDirectorShotList 与 currentSceneDirectorShots 同一权威：导演镜头过期时不读镜头覆盖。
func sceneDirectorShotList(db *gorm.DB, sc *models.Scene) []models.Shot {
	if sc == nil || sc.DirectorSourceStale || !db.Migrator().HasTable(&models.Shot{}) {
		return nil
	}
	var shots []models.Shot
	if db.Where("scene_id = ?", sc.ID).Order("order_num, id").Find(&shots).Error != nil {
		return nil
	}
	return shots
}

// shotOutfitOverrides 返回镜头 ID → 角色 ID → 已审核且属于该角色的覆盖套装。
func shotOutfitOverrides(db *gorm.DB, sc *models.Scene, shots []models.Shot) map[uint]map[uint]models.CharacterOutfit {
	out := map[uint]map[uint]models.CharacterOutfit{}
	if len(shots) == 0 || !db.Migrator().HasTable(&models.ShotCharacterOutfit{}) {
		return out
	}
	shotIDs := make([]uint, 0, len(shots))
	for _, shot := range shots {
		shotIDs = append(shotIDs, shot.ID)
	}
	var rows []struct {
		ShotID   uint
		OutfitID uint
	}
	if err := db.Table("shot_character_outfits").
		Select("shot_character_outfits.shot_id AS shot_id, character_outfits.id AS outfit_id").
		Joins("JOIN character_outfits ON character_outfits.id = shot_character_outfits.outfit_id AND character_outfits.character_id = shot_character_outfits.character_id").
		Where("shot_character_outfits.shot_id IN ? AND character_outfits.project_id = ? AND character_outfits.audit_status IN ?", shotIDs, sc.ProjectID, approvedOutfitStatuses).
		Order("shot_character_outfits.id").Scan(&rows).Error; err != nil || len(rows) == 0 {
		return out
	}
	outfitIDs := make([]uint, 0, len(rows))
	for _, row := range rows {
		outfitIDs = append(outfitIDs, row.OutfitID)
	}
	var outfits []models.CharacterOutfit
	if db.Preload("Character").Where("id IN ?", outfitIDs).Find(&outfits).Error != nil {
		return out
	}
	byID := make(map[uint]models.CharacterOutfit, len(outfits))
	for _, outfit := range outfits {
		byID[outfit.ID] = outfit
	}
	for _, row := range rows {
		outfit, ok := byID[row.OutfitID]
		if !ok {
			continue
		}
		if out[row.ShotID] == nil {
			out[row.ShotID] = map[uint]models.CharacterOutfit{}
		}
		out[row.ShotID][outfit.CharacterID] = outfit
	}
	return out
}

// sceneOpeningOutfits 是 0.00 秒（Shot 1）实际使用的造型：Shot 1 覆盖替代该角色的场景级选择。
func sceneOpeningOutfits(db *gorm.DB, sc *models.Scene) []models.CharacterOutfit {
	outfits := sceneLevelOutfits(db, sc)
	shots := sceneDirectorShotList(db, sc)
	if len(shots) == 0 {
		return outfits
	}
	first := shotOutfitOverrides(db, sc, shots[:1])[shots[0].ID]
	if len(first) == 0 {
		return outfits
	}
	replaced := map[uint]bool{}
	for i, outfit := range outfits {
		if override, ok := first[outfit.CharacterID]; ok {
			outfits[i] = override
			replaced[outfit.CharacterID] = true
		}
	}
	added := make([]models.CharacterOutfit, 0, len(first))
	for characterID, override := range first {
		if !replaced[characterID] {
			added = append(added, override)
		}
	}
	sort.Slice(added, func(i, j int) bool { return added[i].CharacterID < added[j].CharacterID })
	return append(outfits, added...)
}

// shotOutfitChange 是某角色从 ShotNo（1 起）开始换上的造型；Outfit 为 nil 表示换回角色主形象。
type shotOutfitChange struct {
	ShotNo    int
	Character models.Character
	Outfit    *models.CharacterOutfit
}

// sceneShotOutfitChanges 逐镜比较每个角色的有效造型（镜头覆盖 > 场景选择 > 主形象），
// 返回 Shot 2 起的换装与导演镜头数。只处理画面中可见的角色。
func (s *ProjectService) sceneShotOutfitChanges(sc *models.Scene) ([]shotOutfitChange, int) {
	shots := sceneDirectorShotList(s.db, sc)
	if len(shots) < 2 {
		return nil, len(shots)
	}
	overrides := shotOutfitOverrides(s.db, sc, shots)
	if len(overrides) == 0 {
		return nil, len(shots)
	}
	level := map[uint]models.CharacterOutfit{}
	for _, outfit := range sceneLevelOutfits(s.db, sc) {
		level[outfit.CharacterID] = outfit
	}
	tracked := map[uint]bool{}
	for _, byCharacter := range overrides {
		for characterID := range byCharacter {
			tracked[characterID] = true
		}
	}
	ids := make([]uint, 0, len(tracked))
	for id := range tracked {
		ids = append(ids, id)
	}
	sort.Slice(ids, func(i, j int) bool { return ids[i] < ids[j] })
	var characters []models.Character
	if s.db.Where("project_id = ? AND id IN ?", sc.ProjectID, ids).Order("id").Find(&characters).Error != nil {
		return nil, len(shots)
	}
	visible := map[string]bool{}
	for _, canonical := range s.canonicalCharacterNames(sc.ProjectID, visibleSceneCharacterNames(sc)) {
		visible[canonical] = true
	}
	outfitID := func(o *models.CharacterOutfit) uint {
		if o == nil {
			return 0
		}
		return o.ID
	}
	var changes []shotOutfitChange
	for _, ch := range characters {
		if !visible[ch.Name] {
			continue
		}
		effective := func(shot models.Shot) *models.CharacterOutfit {
			if o, ok := overrides[shot.ID][ch.ID]; ok {
				return &o
			}
			if o, ok := level[ch.ID]; ok {
				return &o
			}
			return nil
		}
		prev := effective(shots[0])
		for i := 1; i < len(shots); i++ {
			cur := effective(shots[i])
			if outfitID(cur) != outfitID(prev) {
				changes = append(changes, shotOutfitChange{ShotNo: i + 1, Character: ch, Outfit: cur})
			}
			prev = cur
		}
	}
	sort.SliceStable(changes, func(i, j int) bool { return changes[i].ShotNo < changes[j].ShotNo })
	return changes, len(shots)
}

func shotOutfitChangeFile(c shotOutfitChange) (string, string) {
	if c.Outfit != nil {
		if c.Outfit.Sheet != "" {
			return c.Outfit.Sheet, "套装四视图"
		}
		return c.Outfit.Image, "竖版套装图"
	}
	if c.Character.Sheet != "" {
		return c.Character.Sheet, "四视图"
	}
	return c.Character.Portrait, "标准像"
}

// 换装参考行刻意不含“角色「”：它是已定义人物的后续外观，不能被识别成新的 Subject。
func shotOutfitChangeLabel(c shotOutfitChange, kind string) string {
	what := "换回主形象"
	if c.Outfit != nil {
		what = "换穿造型套装「" + c.Outfit.Name + "」"
	}
	return fmt.Sprintf("换装参考：「%s」自 Shot %d 起%s（%s；同一人物，开场不使用）", c.Character.Name, c.ShotNo, what, kind)
}

// appendShotOutfitReferences 把后续镜头换上的造型追加在已有参考之后（分镜图之前），
// 不改变分镜图与视频共用的既有 <Picture N> 编号。
func (s *ProjectService) appendShotOutfitReferences(sc *models.Scene, pid string, refs []FileMeta, lines []string, limit int) ([]FileMeta, []string) {
	changes, _ := s.sceneShotOutfitChanges(sc)
	if len(changes) == 0 {
		return refs, lines
	}
	present := map[string]bool{}
	for _, ref := range refs {
		present[ref.Name] = true
	}
	for _, c := range changes {
		file, kind := shotOutfitChangeFile(c)
		if file == "" || present[file] {
			continue
		}
		if len(refs) >= limit {
			break
		}
		refs = append(refs, FileMeta{TaskID: pid, Name: file})
		lines = append(lines, fmt.Sprintf("- <Picture %d>：%s", len(refs), shotOutfitChangeLabel(c, kind)))
		present[file] = true
	}
	return refs, lines
}

var shotOutfitReferenceShotPattern = regexp.MustCompile(`自 Shot ([0-9]+) 起`)

var shotOutfitSentencePattern = regexp.MustCompile(`\s*From this shot on, 「[^」]*」 (?:wears the outfit 「[^」]*」|returns to the default look)(?: shown in <Picture [0-9]+>)? \(same person and face\)\.`)

// applyShotOutfitChanges 先删除旧换装句，再按当前数据在对应 [Shot N] 后重写，重复调用逐字幂等。
// 提示词镜头数与导演镜头数不一致（导演合并/增加了切点）时不插入。
func (s *ProjectService) applyShotOutfitChanges(sc *models.Scene, prompt string) string {
	section := prompt
	if detail := h3PromptSection(prompt, "detailed_description:"); detail != "" {
		section = detail
	} else if detail := h3IntegratedDescription(prompt); detail != "" {
		section = detail
	}
	updated := shotOutfitSentencePattern.ReplaceAllString(section, "")
	changes, shotCount := s.sceneShotOutfitChanges(sc)
	if len(changes) > 0 && len(distinctH3ShotNumbers(updated)) == shotCount {
		pictures := map[string]int{}
		if strings.Contains(prompt, "<Picture") {
			refs, _ := s.sceneVideoReferenceFiles(sc, fmt.Sprint(sc.ProjectID))
			for i, ref := range refs {
				if _, ok := pictures[ref.Name]; !ok {
					pictures[ref.Name] = i + 1
				}
			}
		}
		byShot := map[int][]string{}
		for _, c := range changes {
			shown := ""
			if file, _ := shotOutfitChangeFile(c); pictures[file] > 0 {
				shown = fmt.Sprintf(" shown in <Picture %d>", pictures[file])
			}
			sentence := fmt.Sprintf("From this shot on, 「%s」 returns to the default look%s (same person and face).", c.Character.Name, shown)
			if c.Outfit != nil {
				sentence = fmt.Sprintf("From this shot on, 「%s」 wears the outfit 「%s」%s (same person and face).", c.Character.Name, c.Outfit.Name, shown)
			}
			byShot[c.ShotNo] = append(byShot[c.ShotNo], sentence)
		}
		shotNos := make([]int, 0, len(byShot))
		for n := range byShot {
			shotNos = append(shotNos, n)
		}
		sort.Sort(sort.Reverse(sort.IntSlice(shotNos)))
		for _, n := range shotNos {
			for _, m := range h3TimelineMarkerPattern.FindAllStringSubmatchIndex(updated, -1) {
				if number := updated[m[2]:m[3]]; number != fmt.Sprint(n) {
					continue
				}
				rest := updated[m[1]:]
				if rest != "" && !strings.HasPrefix(rest, " ") && !strings.HasPrefix(rest, "\n") {
					rest = " " + rest
				}
				updated = updated[:m[1]] + " " + strings.Join(byShot[n], " ") + rest
				break
			}
		}
	}
	if updated == section {
		return prompt
	}
	if section == prompt {
		return updated
	}
	return strings.Replace(prompt, section, updated, 1)
}
