package service

import (
	"encoding/json"
	"fmt"
	"reflect"
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
	return out
}

func candidate(kind string, id uint, variant, label, category, image string) SceneReferenceCandidate {
	r := SceneReferenceSelection{SourceType: kind, SourceID: id, Variant: variant, UseKrea2: true, UseH3: true}
	return SceneReferenceCandidate{SceneReferenceSelection: r, Key: referenceKey(r), Label: label, Category: category, Image: image}
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

func (s *ProjectService) SaveSceneReferences(sc *models.Scene, refs []SceneReferenceSelection) error {
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
	return s.db.Model(sc).Updates(map[string]any{
		"reference_images_json": string(data), "image_file": "", "image_task_id": "",
		"video_file": "", "video_input_file": "", "video_task_id": "", "video_gpu": nil,
		"status": "pending", "error": "",
	}).Error
}

func (s *ProjectService) selectedSceneReferenceFiles(sc *models.Scene, target string) ([]FileMeta, []string, bool) {
	selected, explicit := parseSceneReferences(sc)
	if !explicit {
		return nil, nil, false
	}
	byKey := map[string]SceneReferenceCandidate{}
	for _, c := range s.sceneReferenceCandidates(sc) {
		byKey[c.Key] = c
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
		refs = append(refs, FileMeta{TaskID: fmt.Sprint(sc.ProjectID), Name: c.Image})
		pictureNumber := len(refs)
		if target == "h3" {
			pictureNumber++
		}
		lines = append(lines, fmt.Sprintf("- <Picture %d>：%s", pictureNumber, c.Label))
	}
	return refs, lines, true
}
