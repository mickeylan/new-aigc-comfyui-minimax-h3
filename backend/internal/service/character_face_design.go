package service

import (
	"encoding/json"
	"fmt"
	"strings"

	"comfyui-console/internal/models"
	"gorm.io/gorm"
)

type faceDesignInput struct {
	Preset       string                          `json:"preset"`
	Engine       string                          `json:"engine"`
	Requirements string                          `json:"requirements"`
	References   []models.CharacterFaceReference `json:"references"`
}

type faceDesignEngine struct {
	Code      string `json:"code"`
	Label     string `json:"label"`
	MaxCount  int    `json:"max_count"`
	Available bool   `json:"available"`
}

// Template definitions, not duplicated UI constants, own reference capacity.
func (s *ProjectService) faceDesignEngines() []faceDesignEngine {
	engines := []faceDesignEngine{{Code: AgeStagePortraitQwenEdit, Label: "Qwen-Image-2.1"}, {Code: AgeStagePortraitH3Edit, Label: "MiniMax H3-Image"}}
	for i := range engines {
		tpl, err := enabledTemplateByCode(s.db, ageStageTemplate(engines[i].Code))
		if err != nil {
			continue
		}
		var inputs []TemplateInput
		if json.Unmarshal([]byte(tpl.InputsJSON), &inputs) != nil {
			continue
		}
		for _, in := range inputs {
			if in.Key == "ref_images" {
				engines[i].MaxCount = in.MaxCount
				engines[i].Available = in.MaxCount >= 2
			}
		}
	}
	return engines
}

func faceDesignProfileHash(p *models.Project, ch *models.Character) string {
	// Only authoritative design facts: the selected output/task does not change a draft.
	raw, _ := json.Marshal([]string{p.Style, p.Genre, ch.Name, ch.Gender, ch.Role, ch.Appearance, ch.Trait, ch.WardrobeDetail, ch.ColorPalette, ch.LightingMood, string(ch.ProfileStatus)})
	return sha256Hex(raw)
}

func validateFaceDesignInput(in faceDesignInput, limit int) error {
	if in.Preset != "dual_parent" && in.Preset != "multi_face_fusion" {
		return fmt.Errorf("不支持的人脸设计预设")
	}
	if len(in.References) < 2 || len(in.References) > limit {
		return fmt.Errorf("参考图需要2至%d张，不能静默截断", limit)
	}
	if in.Preset == "dual_parent" {
		if len(in.References) != 2 {
			return fmt.Errorf("双亲参考需要父亲、母亲各一张")
		}
		slots := map[string]int{}
		for _, r := range in.References {
			slots[r.Slot]++
		}
		if slots["father"] != 1 || slots["mother"] != 1 {
			return fmt.Errorf("请明确指定父亲和母亲参考图")
		}
	}
	seen := map[string]bool{}
	for _, r := range in.References {
		if _, err := safeFileSegment(r.File, "reference file"); err != nil {
			return err
		}
		if seen[r.File] {
			return fmt.Errorf("请使用不同的参考图片")
		}
		seen[r.File] = true
		if len([]rune(r.Responsibility)) > 1000 {
			return fmt.Errorf("单图参考重点不能超过1000字")
		}
	}
	if len([]rune(in.Requirements)) > 4000 {
		return fmt.Errorf("设计要求不能超过4000字")
	}
	return nil
}

func faceDesignRequest(p *models.Project, ch *models.Character, in faceDesignInput) string {
	var b strings.Builder
	fmt.Fprintf(&b, "目标角色：%s；性别：%s；身份：%s\n目标外貌与年龄：%s\n补充特征：%s\n项目题材与画风：%s；%s\n服装：%s\n用户要求（权威）：%s\n", ch.Name, ch.Gender, ch.Role, ch.Appearance, ch.Trait, p.Genre, p.Style, ch.WardrobeDetail, in.Requirements)
	if in.Preset == "dual_parent" {
		b.WriteString("设计意图：根据父亲与母亲长相参考，艺术化设计他们子女的独立相貌；不是遗传预测，年龄以目标角色资料为准，不默认幼童。\n")
	} else {
		b.WriteString("设计意图：综合不同参考人物的面部结构，设计独立的新人物，避免复制任一参考脸，不是机械叠脸或平均脸。\n")
	}
	for i, r := range in.References {
		token := fmt.Sprintf("<image%d>", i+1)
		if in.Engine == AgeStagePortraitH3Edit {
			token = fmt.Sprintf("<Picture %d>", i+1)
		}
		role := "融合参考"
		if in.Preset == "dual_parent" {
			if r.Slot == "father" {
				role = "父亲长相参考"
			} else {
				role = "母亲长相参考"
			}
		}
		fmt.Fprintf(&b, "%s：%s；只参考面部，不继承年龄、服装、妆容、背景。参考重点：%s。\n", token, role, r.Responsibility)
	}
	b.WriteString("只生成目标角色一个人，正面肩部以上标准像，完整头发与头饰，清晰五官、自然中性表情、纯净背景；不出现参考人物、拼贴、文字。生成全新画布而非保留第一张图的原人物或背景。不要编造未分析的参考图具体五官；以图片本身和用户职责说明为准。目标年龄未知则不编造数字年龄。")
	return b.String()
}

func (s *ProjectService) PreviewFaceDesign(pid, cid uint, in faceDesignInput) (*models.CharacterFaceDesign, error) {
	var ch models.Character
	if err := s.db.Where("id=? AND project_id=?", cid, pid).First(&ch).Error; err != nil {
		return nil, err
	}
	var p models.Project
	if err := s.db.First(&p, pid).Error; err != nil {
		return nil, err
	}
	if ch.ProfileStatus != models.ProfileStatusApproved {
		return nil, fmt.Errorf("请先审核角色档案")
	}
	if s.textProvider == nil || s.skills == nil {
		return nil, fmt.Errorf("参考人脸设计需要文本模型和通用角色Skill")
	}
	var engine *faceDesignEngine
	for _, e := range s.faceDesignEngines() {
		if e.Code == in.Engine {
			copy := e
			engine = &copy
		}
	}
	if engine == nil || !engine.Available {
		return nil, fmt.Errorf("所选多图编辑模板不可用")
	}
	if err := validateFaceDesignInput(in, engine.MaxCount); err != nil {
		return nil, err
	}
	refs := make([]models.CharacterFaceReference, len(in.References))
	for i, r := range in.References {
		if err := validateProjectImageUpload(s.db, pid, r.File); err != nil {
			return nil, err
		}
		hash := s.fileSHA256(FileMeta{TaskID: fmt.Sprint(pid), Name: r.File})
		if hash == "" {
			return nil, fmt.Errorf("参考图无法读取：%s", r.File)
		}
		refs[i] = models.CharacterFaceReference{ProjectID: pid, File: r.File, SHA256: hash, Slot: r.Slot, Responsibility: strings.TrimSpace(r.Responsibility), Position: i + 1}
	}
	in.References = refs
	system := "你为角色标准像编写参考人脸设计提示词。只输出一段中文生图提示词，不输出JSON或解释。必须逐一保留输入中的参考图片编号与职责；只有一个新角色，区分于锁定原人物身份的换装。严格服从目标年龄、画风与参考职责。不得声称已经观察到图片中的具体五官，未提供的内容不要假装从参考图分析得出。"
	if classicalChineseSetting(&p, &ch) {
		system += "\n" + ageStageClassicalDressRule
	}
	useBeauty := femaleBeautyDesignApplies(&ch)
	if age, _, ok := explicitCharacterAgeRange(in.Requirements); ok && age < 18 {
		useBeauty = false
	}
	for _, word := range []string{"未成年", "女童", "幼女", "小女孩", "女婴"} {
		if strings.Contains(in.Requirements, word) {
			useBeauty = false
		}
	}
	prompt, skill, err := chatWithCharacterDesignSkill(s.skills, s.textProvider, pid, useBeauty, system, faceDesignRequest(&p, &ch, in))
	if err != nil {
		return nil, err
	}
	prompt = strings.TrimSpace(stripThinkBlocks(prompt))
	if err := checkFaceDesignTokens(in.Engine, prompt, len(refs)); err != nil {
		return nil, err
	}
	params := qwenPortraitParams()
	if in.Engine == AgeStagePortraitQwenEdit {
		params["use_aspect_ratio"] = true
	} else {
		params = map[string]any{"width": 1152, "height": 2048}
	}
	tpl, err := enabledTemplateByCode(s.db, ageStageTemplate(in.Engine))
	if err != nil {
		return nil, err
	}
	files := map[string][]FileMeta{"ref_images": {}}
	for _, ref := range refs {
		files["ref_images"] = append(files["ref_images"], FileMeta{TaskID: fmt.Sprint(pid), Name: ref.File})
	}
	if err := normalizeTemplateFiles(tpl, params, files); err != nil {
		return nil, err
	}
	params["_face_design_template_hash"] = sha256Hex([]byte(tpl.WorkflowJSON + "\n" + tpl.InputsJSON))
	raw, _ := json.Marshal(params)
	row := models.CharacterFaceDesign{ProjectID: pid, CharacterID: cid, Preset: in.Preset, Engine: in.Engine, Requirements: in.Requirements, Prompt: prompt, ParamsJSON: string(raw), ProfileHash: faceDesignProfileHash(&p, &ch), Skill: skill, References: refs}
	if err := s.db.Create(&row).Error; err != nil {
		return nil, err
	}
	return &row, nil
}

func checkFaceDesignTokens(engine, prompt string, count int) error {
	if strings.TrimSpace(prompt) == "" || len([]rune(prompt)) > 12000 {
		return fmt.Errorf("提示词为空或超过12000字")
	}
	for i := 1; i <= count; i++ {
		token := fmt.Sprintf("<image%d>", i)
		if engine == AgeStagePortraitH3Edit {
			token = fmt.Sprintf("<Picture %d>", i)
		}
		if !strings.Contains(prompt, token) {
			return fmt.Errorf("提示词缺少参考职责编号%s", token)
		}
	}
	return nil
}

// A reviewed record is consumed once; prompt edits are accepted, but refs/engine
// cannot be swapped at submit. A fresh preview is required after changing inputs.
func (s *ProjectService) StartFaceDesign(pid, cid, did uint, prompt string) (string, error) {
	if s.tasks == nil {
		return "", fmt.Errorf("任务服务不可用")
	}
	var row models.CharacterFaceDesign
	if err := s.db.Preload("References", func(db *gorm.DB) *gorm.DB { return db.Order("position ASC") }).Where("id=? AND project_id=? AND character_id=?", did, pid, cid).First(&row).Error; err != nil {
		return "", err
	}
	if row.TaskID != "" {
		return "", fmt.Errorf("该设计已提交，请重新预览后生成")
	}
	var ch models.Character
	if err := s.db.Where("id=? AND project_id=?", cid, pid).First(&ch).Error; err != nil {
		return "", err
	}
	var p models.Project
	if err := s.db.First(&p, pid).Error; err != nil {
		return "", err
	}
	if ch.ProfileStatus != models.ProfileStatusApproved || row.ProfileHash != faceDesignProfileHash(&p, &ch) {
		return "", fmt.Errorf("角色资料已改变，请重新预览审阅")
	}
	if ch.PortraitTaskID != "" {
		return "", fmt.Errorf("已有标准像任务，请先完成或重置")
	}
	if err := checkFaceDesignTokens(row.Engine, prompt, len(row.References)); err != nil {
		return "", err
	}
	files := map[string][]FileMeta{"ref_images": {}}
	for _, r := range row.References {
		if err := validateProjectImageUpload(s.db, pid, r.File); err != nil {
			return "", err
		}
		ref := FileMeta{TaskID: fmt.Sprint(pid), Name: r.File}
		if r.SHA256 == "" || s.fileSHA256(ref) != r.SHA256 {
			return "", fmt.Errorf("参考图内容改变或丢失，请重新选择：%s", r.File)
		}
		files["ref_images"] = append(files["ref_images"], ref)
	}
	tpl, err := enabledTemplateByCode(s.db, ageStageTemplate(row.Engine))
	if err != nil {
		return "", err
	}
	params := map[string]any{}
	if err := json.Unmarshal([]byte(row.ParamsJSON), &params); err != nil {
		return "", err
	}
	if params["_face_design_template_hash"] != sha256Hex([]byte(tpl.WorkflowJSON+"\n"+tpl.InputsJSON)) {
		return "", fmt.Errorf("模板已改变，请重新预览审阅")
	}
	params["_face_design_id"] = row.ID
	task, err := s.tasks.CreateTask(CreateTaskReq{TemplateID: tpl.ID, Prompt: prompt, Params: params, Files: files})
	if err != nil {
		return "", err
	}
	err = s.db.Transaction(func(tx *gorm.DB) error {
		var current models.Character
		var project models.Project
		if err := tx.Where("id=? AND project_id=?", cid, pid).First(&current).Error; err != nil {
			return err
		}
		if err := tx.First(&project, pid).Error; err != nil {
			return err
		}
		if faceDesignProfileHash(&project, &current) != row.ProfileHash {
			return fmt.Errorf("角色资料已改变，请重新审阅")
		}
		res := tx.Model(&models.CharacterFaceDesign{}).Where("id=? AND task_id=''", row.ID).Updates(map[string]any{"task_id": task.TaskID, "prompt": prompt})
		if res.Error != nil {
			return res.Error
		}
		if res.RowsAffected != 1 {
			return fmt.Errorf("设计已被其他请求提交")
		}
		res = tx.Model(&models.Character{}).Where("id=? AND project_id=? AND portrait_task_id=''", cid, pid).Updates(map[string]any{"portrait_task_id": task.TaskID, "portrait_error": ""})
		if res.Error != nil {
			return res.Error
		}
		if res.RowsAffected != 1 {
			return fmt.Errorf("角色已有生成任务")
		}
		return nil
	})
	if err != nil {
		_ = s.tasks.CancelTask(task.TaskID)
		return "", err
	}
	go func() { _ = s.tasks.Execute(task.TaskID) }()
	s.pushProject(nil)
	return task.TaskID, nil
}
