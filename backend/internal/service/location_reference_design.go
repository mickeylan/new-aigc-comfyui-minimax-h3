package service

import (
	"encoding/json"
	"fmt"
	"log"
	"path/filepath"
	"strings"
	"time"

	"comfyui-console/internal/models"
	"gorm.io/gorm"
)

// 场景参考设计：以若干张参考图（场景图 / 人物形象 / 道具图）为基础，组合出一张新的场景画面。
// 结果注册为该场景 asset_image 的未选中变体，Asset.Image 仍然只有一个被选中的输出。
const (
	locationDesignSlotScene     = "scene"
	locationDesignSlotCharacter = "character"
	locationDesignSlotProp      = "prop"

	locationDesignPresentRequired = "required" // 必须出现在画面里
	locationDesignPresentOptional = "optional" // 只作造型/材质参考
)

type locationDesignInput struct {
	Engine       string                               `json:"engine"`
	Requirements string                               `json:"requirements"`
	References   []models.LocationReferenceDesignFile `json:"references"`
}

type locationDesignEngine struct {
	Code      string `json:"code"`
	Label     string `json:"label"`
	MaxCount  int    `json:"max_count"`
	Available bool   `json:"available"`
}

// 与参考人脸设计共用同一批多图编辑模板；参考容量由模板定义，不在前端写死。
func (s *ProjectService) locationDesignEngines() []locationDesignEngine {
	engines := []locationDesignEngine{{Code: AgeStagePortraitQwenEdit, Label: "Qwen-Image-2.1"}, {Code: AgeStagePortraitH3Edit, Label: "MiniMax H3-Image"}}
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
				engines[i].Available = in.MaxCount >= 1
			}
		}
	}
	return engines
}

func locationDesignProfileHash(p *models.Project, a *models.Asset) string {
	raw, _ := json.Marshal([]string{
		p.Genre, p.Style, p.AspectRatio,
		a.Kind, a.Name, a.Description, a.VisualType, a.MegaType, a.VisibleText,
	})
	return sha256Hex(raw)
}

func validateLocationDesignInput(in locationDesignInput, limit int) error {
	if len(in.References) < 1 || len(in.References) > limit {
		return fmt.Errorf("参考图需要1至%d张，不能静默截断", limit)
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
		switch r.Slot {
		case locationDesignSlotScene, locationDesignSlotCharacter, locationDesignSlotProp:
		default:
			return fmt.Errorf("参考类型只能是场景、人物或道具")
		}
		switch r.Presence {
		case locationDesignPresentRequired, locationDesignPresentOptional:
		default:
			return fmt.Errorf("请注明每张参考是必须入画还是仅作参考")
		}
		if len([]rune(r.Responsibility)) > 1000 {
			return fmt.Errorf("单图参考重点不能超过1000字")
		}
	}
	if len([]rune(in.Requirements)) > 4000 {
		return fmt.Errorf("设计要求不能超过4000字")
	}
	return nil
}

func locationDesignSlotLabel(slot string) string {
	switch slot {
	case locationDesignSlotScene:
		return "场景参考"
	case locationDesignSlotCharacter:
		return "人物参考"
	case locationDesignSlotProp:
		return "道具参考"
	}
	return "参考"
}

func locationDesignPresenceLabel(presence string) string {
	if presence == locationDesignPresentRequired {
		return "必须出现在画面中"
	}
	return "仅作造型与材质参考，不要求入画"
}

// 引擎决定参考图编号写法，因此提示词里的编号说明也按引擎写死，避免模型混用两种记号。
func locationDesignSystem(engine string, count int) string {
	token := fmt.Sprintf("<image1>…<image%d>", count)
	if engine == AgeStagePortraitH3Edit {
		token = fmt.Sprintf("<Picture 1>…<Picture %d>", count)
	}
	return "你为场景参考图编写多图编辑提示词。参考图编号在本引擎写作 " + token + "，必须逐个出现在提示词里并对应各自的类型与职责。只输出一段中文生图提示词，不输出JSON或解释。" +
		"标注必须入画的人物与道具要写进画面并说明与场景的位置关系，标注仅作参考的只描述借用的造型或材质。严格服从目标场景描述、画风与画幅。" +
		"不得声称已经观察到图片中的具体细节，未提供的内容不要假装从图片分析得出。"
}

// chatLocationDesignPrompt 复用多图编辑提示词的程序化 Skill；H3-Image 没有专门的编辑 Skill，
// 只有项目显式配置了阶段 Skill 时才附加，否则直接用基础提示词，避免套用 Qwen 的操作规则。
func (s *ProjectService) chatLocationDesignPrompt(pid uint, engine, system, user string) (string, string, error) {
	params := map[string]string{"request": user}
	if engine == AgeStagePortraitQwenEdit {
		out, provenance, err := s.skills.ChatWithConfiguredOrFallbackSkillTraced(pid, models.SkillStageQwenImageEdit, QwenImage21EditProgram, s.textProvider, system, user, params)
		if provenance == nil {
			return out, "", err
		}
		return out, fmt.Sprintf("%s v%d", provenance.Code, provenance.Version), err
	}
	if skill, err := s.skills.GetEffectiveSkill(pid, models.SkillStageQwenImageEdit); err == nil && skill != nil {
		out, err := s.skills.ChatWithSkill(pid, models.SkillStageQwenImageEdit, s.textProvider, system, user, params)
		return out, fmt.Sprintf("%s v%d", skill.Name, skill.Version), err
	}
	out, err := s.textProvider.Chat(system, user)
	return out, "", err
}

func locationDesignRequest(p *models.Project, a *models.Asset, engine string, in locationDesignInput) string {
	var b strings.Builder
	fmt.Fprintf(&b, "目标场景：%s；类型：场景资产\n场景描述（权威）：%s\n", a.Name, a.Description)
	if visualType, megaType := normalizeAssetVisual(a.Kind, a.VisualType, a.MegaType); visualType == "megastructure" {
		fmt.Fprintf(&b, "场景类别：巨构场景（%s），需要明确尺度参照与广角远景构图。\n", megaType)
	}
	if text := strings.TrimSpace(a.VisibleText); text != "" {
		fmt.Fprintf(&b, "画面必须逐字呈现的可读文字：%s\n", text)
	}
	fmt.Fprintf(&b, "项目题材与画风：%s；%s；画面比例：%s\n用户要求（权威）：%s\n", p.Genre, p.Style, p.AspectRatio, in.Requirements)
	b.WriteString("设计意图：以场景参考的空间结构、建筑与光影为底，把标注为必须入画的人物与道具放进这个空间，保持他们各自的身份、比例与材质；仅作参考的图片只借造型、色彩或氛围。\n")
	for i, r := range in.References {
		token := fmt.Sprintf("<image%d>", i+1)
		if engine == AgeStagePortraitH3Edit {
			token = fmt.Sprintf("<Picture %d>", i+1)
		}
		fmt.Fprintf(&b, "%s：%s，%s", token, locationDesignSlotLabel(r.Slot), locationDesignPresenceLabel(r.Presence))
		if responsibility := strings.TrimSpace(r.Responsibility); responsibility != "" {
			fmt.Fprintf(&b, "；参考重点：%s", responsibility)
		}
		b.WriteString("。\n")
	}
	b.WriteString("输出一张完整的场景画面：构图有明确前景、中景与远景层次，空间结构清晰可读，光线方向统一；人物按正常人体比例与地面接触自然站立或活动，不复制参考图里的原背景、原构图或多余人物；不出现拼贴、分割线、边框、文字标签与水印。目标场景的文字要求优先。")
	return b.String()
}

// PreviewLocationDesign 让文本模型写一版提示词供人工审阅；不建任务、不改场景图。
func (s *ProjectService) PreviewLocationDesign(pid, aid uint, in locationDesignInput) (*models.LocationReferenceDesign, error) {
	var a models.Asset
	if err := s.db.Where("id=? AND project_id=?", aid, pid).First(&a).Error; err != nil {
		return nil, err
	}
	if a.Kind != AssetKindLocation {
		return nil, fmt.Errorf("场景参考设计只用于场景资产")
	}
	var p models.Project
	if err := s.db.First(&p, pid).Error; err != nil {
		return nil, err
	}
	if s.textProvider == nil || s.skills == nil || s.upload == nil {
		return nil, fmt.Errorf("场景参考设计需要文本模型、提示词Skill与图片上传服务")
	}
	var engine *locationDesignEngine
	for _, e := range s.locationDesignEngines() {
		if e.Code == in.Engine {
			copy := e
			engine = &copy
		}
	}
	if engine == nil || !engine.Available {
		return nil, fmt.Errorf("所选多图编辑模板不可用")
	}
	if err := validateLocationDesignInput(in, engine.MaxCount); err != nil {
		return nil, err
	}
	refs := make([]models.LocationReferenceDesignFile, len(in.References))
	for i, r := range in.References {
		if err := validateProjectImageUpload(s.db, pid, r.File); err != nil {
			return nil, err
		}
		hash := s.fileSHA256(FileMeta{TaskID: fmt.Sprint(pid), Name: r.File})
		if hash == "" {
			return nil, fmt.Errorf("参考图无法读取：%s", r.File)
		}
		refs[i] = models.LocationReferenceDesignFile{ProjectID: pid, File: r.File, SHA256: hash, Slot: r.Slot, Presence: r.Presence, Responsibility: strings.TrimSpace(r.Responsibility), Position: i + 1}
	}
	in.References = refs
	prompt, skill, err := s.chatLocationDesignPrompt(pid, in.Engine, locationDesignSystem(in.Engine, len(refs)), locationDesignRequest(&p, &a, in.Engine, in))
	if err != nil {
		return nil, err
	}
	prompt = strings.TrimSpace(stripThinkBlocks(prompt))
	if err := checkFaceDesignTokens(in.Engine, prompt, len(refs)); err != nil {
		return nil, err
	}
	params := map[string]any{}
	if in.Engine == AgeStagePortraitQwenEdit {
		params = map[string]any{
			"aspect_ratio": qwenAspectRatio(p.AspectRatio), "use_aspect_ratio": true,
			"megapixels": 2.5, "multiple": 8, "reference_resolution": 1024,
			"steps": 25, "cfg": 1, "negative_prompt": "",
		}
	} else {
		width, height := assetImageSize(&p, AssetKindLocation)
		params = map[string]any{"width": width, "height": height}
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
	params["_location_design_template_hash"] = sha256Hex([]byte(tpl.WorkflowJSON + "\n" + tpl.InputsJSON))
	raw, _ := json.Marshal(params)
	row := models.LocationReferenceDesign{ProjectID: pid, AssetID: aid, Engine: in.Engine, Requirements: in.Requirements, Prompt: prompt, ParamsJSON: string(raw), ProfileHash: locationDesignProfileHash(&p, &a), Skill: skill, References: refs}
	if err := s.db.Create(&row).Error; err != nil {
		return nil, err
	}
	return &row, nil
}

// StartLocationDesign 提交审阅过的提示词；参考图、引擎与模板都不允许在这一步偷换。
func (s *ProjectService) StartLocationDesign(pid, aid, did uint, prompt string) (string, error) {
	if s.tasks == nil {
		return "", fmt.Errorf("任务服务不可用")
	}
	var row models.LocationReferenceDesign
	if err := s.db.Preload("References", func(db *gorm.DB) *gorm.DB { return db.Order("position ASC") }).Where("id=? AND project_id=? AND asset_id=?", did, pid, aid).First(&row).Error; err != nil {
		return "", err
	}
	if row.TaskID != "" {
		return "", fmt.Errorf("该设计已提交，请重新预览后生成")
	}
	if row.ResultVariantID != 0 {
		return "", fmt.Errorf("该设计已生成过场景图，请在参考图版本里查看；需要新图请重新预览")
	}
	var a models.Asset
	if err := s.db.Where("id=? AND project_id=?", aid, pid).First(&a).Error; err != nil {
		return "", err
	}
	if a.Kind != AssetKindLocation {
		return "", fmt.Errorf("场景参考设计只用于场景资产")
	}
	var p models.Project
	if err := s.db.First(&p, pid).Error; err != nil {
		return "", err
	}
	if row.ProfileHash != locationDesignProfileHash(&p, &a) {
		return "", fmt.Errorf("场景资料已改变，请重新预览审阅")
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
	if params["_location_design_template_hash"] != sha256Hex([]byte(tpl.WorkflowJSON+"\n"+tpl.InputsJSON)) {
		return "", fmt.Errorf("模板已改变，请重新预览审阅")
	}
	params["_location_design_id"] = row.ID
	task, err := s.tasks.CreateTask(CreateTaskReq{TemplateID: tpl.ID, Prompt: prompt, Params: params, Files: files})
	if err != nil {
		return "", err
	}
	res := s.db.Model(&models.LocationReferenceDesign{}).Where("id=? AND task_id='' AND result_variant_id=0", row.ID).Updates(map[string]any{"task_id": task.TaskID, "prompt": prompt, "error": ""})
	if res.Error != nil || res.RowsAffected != 1 {
		_ = s.tasks.CancelTask(task.TaskID)
		if res.Error != nil {
			return "", res.Error
		}
		return "", fmt.Errorf("设计已被其他请求提交")
	}
	go func() { _ = s.tasks.Execute(task.TaskID) }()
	s.pushProject(nil)
	return task.TaskID, nil
}

// syncLocationDesigns 把已结束的任务结果落成场景图的未选中变体；不覆盖当前选中的场景图。
// 返回是否写入了数据库，便于调用方与测试判断。
func (s *ProjectService) syncLocationDesigns() bool {
	var rows []models.LocationReferenceDesign
	if err := s.db.Where("task_id != ''").Find(&rows).Error; err != nil {
		return false
	}
	changed := false
	for i := range rows {
		row := &rows[i]
		var task models.Task
		if err := s.db.Where("task_id = ?", row.TaskID).First(&task).Error; err != nil {
			res := s.db.Model(row).Where("task_id = ?", row.TaskID).Updates(map[string]any{"task_id": "", "error": "场景参考设计任务不存在，请重新预览生成"})
			changed = changed || res.RowsAffected > 0
			continue
		}
		switch task.Status {
		case "failed", "cancelled":
			msg := strings.TrimSpace(task.Error)
			if msg == "" {
				msg = "场景参考设计任务已" + map[string]string{"failed": "失败", "cancelled": "取消"}[task.Status]
			}
			res := s.db.Model(row).Where("task_id = ?", task.TaskID).Updates(map[string]any{"task_id": "", "error": msg})
			changed = changed || res.RowsAffected > 0
		case "success":
			if !s.saveLocationDesignResult(row, &task) {
				continue
			}
			changed = true
		}
	}
	if changed {
		s.pushProject(nil)
	}
	return changed
}

// saveLocationDesignResult 下载任务输出、存进项目 input 目录并注册为未选中变体。
func (s *ProjectService) saveLocationDesignResult(row *models.LocationReferenceDesign, task *models.Task) bool {
	fail := func(msg string) bool {
		s.db.Model(row).Where("task_id = ?", task.TaskID).Updates(map[string]any{"task_id": "", "error": msg})
		return true
	}
	file, _ := resultImageOf(task)
	if file == "" {
		return fail("场景参考设计任务成功但未返回可用图片")
	}
	if task.Port == nil || s.upload == nil || s.tasks == nil {
		return fail("场景参考设计缺少下载或保存条件")
	}
	subfolder, filename := filepath.ToSlash(filepath.Dir(file)), filepath.Base(file)
	if subfolder == "." {
		subfolder = ""
	}
	client := NewComfyClient(s.tasks.comfyHostForTask(task), *task.Port)
	data, err := client.DownloadOutput(filename, subfolder, "output")
	if err != nil {
		return fail("读取场景参考设计结果失败: " + err.Error())
	}
	ext := filepath.Ext(file)
	if ext == "" {
		ext = detectImageExt(data)
	}
	name := fmt.Sprintf("loc_design_%d_%d%s", row.AssetID, time.Now().UnixNano(), ext)
	path, _, err := s.upload.SaveFile(fmt.Sprint(row.ProjectID), "image", name, data)
	if err != nil {
		return fail("保存场景参考设计结果失败: " + err.Error())
	}
	variant, err := NewAssetVariantService(s.db).Register(models.AssetVariant{
		ProjectID: row.ProjectID, EntityType: VariantAssetImage, EntityID: row.AssetID,
		File: filepath.Base(path), Prompt: row.Prompt,
		Provenance: fmt.Sprintf("location_design:%d;task=%s", row.ID, task.TaskID),
	})
	if err != nil {
		log.Printf("[location-design %d] register variant failed: %v", row.ID, err)
		return fail("注册场景参考图版本失败: " + err.Error())
	}
	s.db.Model(row).Where("task_id = ?", task.TaskID).Updates(map[string]any{"task_id": "", "error": "", "result_variant_id": variant.ID})
	return true
}
