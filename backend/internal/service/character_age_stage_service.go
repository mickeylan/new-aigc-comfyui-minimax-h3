package service

import (
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"path/filepath"
	"strings"
	"time"

	"comfyui-console/internal/models"
	"gorm.io/gorm"
)

// AgeStagePortraitQwenEdit 以角色主标准像为 <image1>、经 Qwen-Image-2.1 多图编辑生成阶段四视图。
// AgeStagePortraitH3Edit 用 MiniMax H3-Image 多图编辑做同一件事，提示词用 <Picture 1> 指代主标准像。
// 两者都必需参考图：Moss 与 Krea2 只支持文生图，不能带参考图。
const (
	AgeStagePortraitQwenEdit = "qwen_edit"
	AgeStagePortraitH3Edit   = "minimax_h3_edit"
)

// 阶段四视图固定 16:9 横幅（H3-Image 要求宽高为 32 的倍数）。
const (
	ageStageSheetWidth  = 2048
	ageStageSheetHeight = 1152
)

type AgeStageInput struct {
	Name           string `json:"name"`
	Age            *int   `json:"age"`
	Appearance     string `json:"appearance"`
	PortraitEngine string `json:"portrait_engine"`
}

// normalizeAgeStagePortraitEngine 阶段四视图必须带参考图，只用多图编辑（Qwen 或 MiniMax H3-Image）。
// 旧取值（qwen_image_2_1、moss、krea2）一律改为 Qwen 多图编辑，与改动前的行为一致。
func normalizeAgeStagePortraitEngine(value string) (string, error) {
	switch strings.TrimSpace(value) {
	case "", AgeStagePortraitQwenEdit, ImageEngineQwen21, ImageEngineMoss, ImageEngineKrea2:
		return AgeStagePortraitQwenEdit, nil
	case AgeStagePortraitH3Edit:
		return AgeStagePortraitH3Edit, nil
	}
	return "", fmt.Errorf("不支持的年龄阶段四视图引擎")
}

// ageStageReferenceToken 是提示词里指代主标准像的写法：Qwen 多图编辑用 <image1>，
// MiniMax H3-Image 按官方多图编辑约定用 <Picture 1>。
func ageStageReferenceToken(engine string) string {
	if engine == AgeStagePortraitH3Edit {
		return "<Picture 1>"
	}
	return "<image1>"
}

// ageStageSheetLayout 判断提示词是否是四视图版式；旧版 9:16 阶段标准像提示词不再复用。
func ageStageSheetLayout(prompt string) bool {
	return strings.Contains(prompt, "由左到右")
}

func ageStageApproved(stage *models.CharacterAgeStage) bool {
	return stage.AuditStatus == models.LookStatusApproved || stage.AuditStatus == models.LookStatusPublished
}

// ageStageLabel 用于界面与提示词，如“童年（6岁）”。
func ageStageLabel(stage *models.CharacterAgeStage) string {
	if stage.Age != nil && *stage.Age > 0 {
		return fmt.Sprintf("%s（%d岁）", stage.Name, *stage.Age)
	}
	return stage.Name
}

func (s *CharacterLookService) loadAgeStage(db *gorm.DB, projectID, characterID, stageID uint) (*models.CharacterAgeStage, error) {
	var stage models.CharacterAgeStage
	if err := db.Where("id = ? AND project_id = ? AND character_id = ?", stageID, projectID, characterID).First(&stage).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, fmt.Errorf("年龄阶段不存在或不属于当前角色")
		}
		return nil, err
	}
	return &stage, nil
}

func (s *CharacterLookService) ListAgeStages(projectID, characterID uint) ([]models.CharacterAgeStage, error) {
	var stages []models.CharacterAgeStage
	err := s.db.Where("project_id = ? AND character_id = ?", projectID, characterID).Order("CASE WHEN age IS NULL THEN 1 ELSE 0 END, age, id").Find(&stages).Error
	return stages, err
}

func (s *CharacterLookService) SaveAgeStage(projectID, characterID, stageID uint, req AgeStageInput) (*models.CharacterAgeStage, error) {
	req.Name, req.Appearance = strings.TrimSpace(req.Name), strings.TrimSpace(req.Appearance)
	if req.Name == "" {
		return nil, fmt.Errorf("年龄阶段名称不能为空")
	}
	if req.Appearance == "" {
		return nil, fmt.Errorf("请填写该阶段的外貌描述（年龄、脸型、发型、身高体型）")
	}
	if req.Age != nil && (*req.Age < 1 || *req.Age > 120) {
		return nil, fmt.Errorf("年龄必须在1到120之间")
	}
	engine, err := normalizeAgeStagePortraitEngine(req.PortraitEngine)
	if err != nil {
		return nil, err
	}
	var count int64
	if err := s.db.Model(&models.Character{}).Where("id = ? AND project_id = ?", characterID, projectID).Count(&count).Error; err != nil {
		return nil, err
	}
	if count != 1 {
		return nil, fmt.Errorf("角色不存在")
	}
	if stageID == 0 {
		stage := models.CharacterAgeStage{ProjectID: projectID, CharacterID: characterID, Name: req.Name, Age: req.Age, Appearance: req.Appearance, PortraitEngine: engine, AuditStatus: models.LookStatusDraft, Version: 1}
		if err := s.db.Create(&stage).Error; err != nil {
			return nil, fmt.Errorf("保存年龄阶段失败（同一角色阶段名不能重复）: %w", err)
		}
		return &stage, nil
	}
	stage, err := s.loadAgeStage(s.db, projectID, characterID, stageID)
	if err != nil {
		return nil, err
	}
	appearanceChanged := stage.Appearance != req.Appearance || !sameOptionalInt(stage.Age, req.Age)
	updates := map[string]any{"name": req.Name, "age": req.Age, "appearance": req.Appearance, "portrait_engine": engine}
	oldEngine, _ := normalizeAgeStagePortraitEngine(stage.PortraitEngine)
	if appearanceChanged || oldEngine != engine {
		// 提示词由外貌与来源推导；旧提示词不再代表当前设计，审核需重新确认。
		updates["portrait_prompt"] = ""
		updates["audit_status"] = models.LookStatusDraft
		updates["audit_note"] = ""
		updates["version"] = gorm.Expr("version + 1")
	}
	if err := s.db.Model(stage).Updates(updates).Error; err != nil {
		return nil, fmt.Errorf("保存年龄阶段失败（同一角色阶段名不能重复）: %w", err)
	}
	if appearanceChanged || stage.Name != req.Name {
		if err := s.markAgeStageScenesStale(stage.ID, "角色年龄阶段外貌已修改"); err != nil {
			return nil, err
		}
	}
	return s.loadAgeStage(s.db, projectID, characterID, stage.ID)
}

// AgeStageExpandInput 是“AI 扩写”的输入：用户在编辑阶段时写下的简要形象描述。
type AgeStageExpandInput struct {
	Name  string `json:"name"`
	Age   *int   `json:"age"`
	Brief string `json:"brief"`
}

// ExpandAgeStageAppearance 按用户输入扩写该年龄的人物形象描述，只返回文本供用户修改，不保存。
// 用户写明的内容为准；主档案只用于延续发色、瞳色等天生特征，不沿用主档案的年龄与体型。
// 第二个返回值是实际使用的项目 Skill（名称与版本），供界面展示。
func (s *CharacterLookService) ExpandAgeStageAppearance(projectID, characterID uint, req AgeStageExpandInput) (string, string, error) {
	brief := strings.TrimSpace(req.Brief)
	if brief == "" {
		return "", "", fmt.Errorf("请先写几句该年龄的形象要点，再让 AI 扩写")
	}
	if req.Age != nil && (*req.Age < 1 || *req.Age > 120) {
		return "", "", fmt.Errorf("年龄必须在1到120之间")
	}
	if s.textProvider == nil || s.skills == nil {
		return "", "", fmt.Errorf("文本生成服务未配置")
	}
	var project models.Project
	var ch models.Character
	if err := s.db.First(&project, projectID).Error; err != nil {
		return "", "", err
	}
	if err := s.db.Where("id = ? AND project_id = ?", characterID, projectID).First(&ch).Error; err != nil {
		return "", "", fmt.Errorf("角色不存在")
	}
	age := "未指定（按用户描述判断）"
	if req.Age != nil {
		age = fmt.Sprintf("%d岁", *req.Age)
	}
	classical := classicalChineseSetting(&project, &ch)
	system := `你是影视角色设定师。把用户对同一角色某个年龄阶段的简要描述，扩写成可直接用于生成角色四视图的中文人物形象描述。
规则：
1. 用户写明的内容是权威，必须全部保留且不得改写或矛盾；只补全用户没写的部分。
2. 依次写：年龄与性别、脸型与五官、发型发色、身高头身比与体态、神态气质，最后以“基础服饰：”写一套符合该年龄与时代背景的日常服装和鞋履。
3. 年龄感、头身比和面部成熟度必须严格符合该年龄；本任务给出的阶段年龄优先于任何“成年年龄”要求。角色主档案只用于延续发色、瞳色、肤色、痣痣等天生特征，不得沿用主档案的年龄、体型、发型和服装。
4. 不写姓名、剧情、动作、姿势、镜头、构图、背景、画风，不加武器、法宝或剧情道具。
5. 只输出一段纯文本，不超过300字，不要标题、列表、引号或解释。`
	if classical {
		system += "\n" + ageStageClassicalDressRule
	}
	user := fmt.Sprintf("项目题材：%s\n项目画风：%s\n角色性别：%s\n角色身份：%s\n角色主档案外貌（仅供延续天生特征）：%s\n阶段名称：%s\n该阶段年龄：%s\n用户描述：%s",
		project.Genre, project.Style, ch.Gender, ch.Role, ch.Appearance, strings.TrimSpace(req.Name), age, brief)
	// 女性优先用“成年女性角色美术增强”；阶段年龄或用户描述明确未满18岁时改用通用角色 Skill。
	female := portraitGenderIdentity(&ch) == "女性"
	if req.Age != nil && *req.Age < 18 {
		female = false
	} else if minAge, _, ok := explicitCharacterAgeRange(brief); ok && minAge < 18 {
		female = false
	}
	raw, skillLabel, err := chatWithCharacterDesignSkill(s.skills, s.textProvider, projectID, female, system, user)
	if err != nil {
		return "", "", fmt.Errorf("AI扩写失败: %w", err)
	}
	text := strings.Trim(strings.TrimSpace(stripThinkBlocks(raw)), "\"“”`")
	if text == "" {
		return "", "", fmt.Errorf("AI扩写未返回内容，请重试或手动填写")
	}
	if classical {
		if word := ageStageModernDressWord(text, brief); word != "" {
			return "", "", fmt.Errorf("AI扩写写出了与古典题材不符的“%s”，已丢弃，请重试或手动填写", word)
		}
	}
	return text, skillLabel, nil
}

// ageStageClassicalDressRule 约束古典修仙、仙侠、武侠题材的阶段服饰：只用中国古代形制与平底鞋履。
const ageStageClassicalDressRule = "6. 本项目为中国古典题材：服装必须是中国古代形制（如交领或对襟襦裙、长衫、袍服、褙子），下装长及脚踝；鞋履只能是中国古代平底布履、绣鞋或靴。禁止短裙、超短裙、热裤、丝袜、现代服装，禁止高跟鞋、运动鞋和现代皮鞋。"

// ageStageClassicalNegative 是古典题材阶段四视图的负面提示词。
const ageStageClassicalNegative = "短裙，超短裙，迷你裙，热裤，丝袜，高跟鞋，运动鞋，现代皮鞋，现代服装，露腿，竖版构图"

// ageStageModernDressWords 是古典题材下扩写结果不允许出现的现代服饰词（用户自己写明的除外）。
var ageStageModernDressWords = []string{"高跟", "短裙", "超短裙", "迷你裙", "热裤", "丝袜", "连裤袜", "西装", "牛仔", "T恤", "运动鞋", "皮鞋"}

func ageStageModernDressWord(text, brief string) string {
	for _, word := range ageStageModernDressWords {
		if strings.Contains(text, word) && !strings.Contains(brief, word) {
			return word
		}
	}
	return ""
}

func sameOptionalInt(a, b *int) bool {
	if a == nil || b == nil {
		return a == nil && b == nil
	}
	return *a == *b
}

func (s *CharacterLookService) DeleteAgeStage(projectID, characterID, stageID uint) error {
	stage, err := s.loadAgeStage(s.db, projectID, characterID, stageID)
	if err != nil {
		return err
	}
	var outfits int64
	if err := s.db.Model(&models.CharacterOutfit{}).Where("age_stage_id = ?", stage.ID).Count(&outfits).Error; err != nil {
		return err
	}
	if outfits > 0 {
		return fmt.Errorf("该年龄阶段下还有%d套服装；请先删除或改挂这些套装", outfits)
	}
	if stage.PortraitTaskID != "" && s.tasks != nil {
		_ = s.tasks.CancelTask(stage.PortraitTaskID)
	}
	return s.db.Delete(stage).Error
}

func (s *CharacterLookService) ApproveAgeStage(projectID, characterID, stageID uint) error {
	stage, err := s.loadAgeStage(s.db, projectID, characterID, stageID)
	if err != nil {
		return err
	}
	if strings.TrimSpace(stage.Portrait) == "" {
		return fmt.Errorf("请先生成或上传该阶段四视图")
	}
	return s.db.Model(stage).Updates(map[string]any{"audit_status": models.LookStatusApproved, "audit_note": ""}).Error
}

// AgeStagePortraitPrompt 返回当前保存的提示词（可能是用户改过的）与系统默认稿。
// 旧版阶段标准像提示词不是四视图版式，不再作为已保存稿返回。
func (s *CharacterLookService) AgeStagePortraitPrompt(projectID, characterID, stageID uint) (saved, draft string, err error) {
	stage, err := s.loadAgeStage(s.db, projectID, characterID, stageID)
	if err != nil {
		return "", "", err
	}
	var project models.Project
	var ch models.Character
	if err := s.db.First(&project, projectID).Error; err != nil {
		return "", "", err
	}
	if err := s.db.Where("id = ? AND project_id = ?", characterID, projectID).First(&ch).Error; err != nil {
		return "", "", err
	}
	saved = stage.PortraitPrompt
	if !ageStageSheetLayout(saved) {
		saved = ""
	}
	engine, err := normalizeAgeStagePortraitEngine(stage.PortraitEngine)
	if err != nil {
		return "", "", err
	}
	// 换了引擎后旧稿的绑定写法不再适用，按未保存处理，界面显示对新引擎生成的默认稿。
	if saved != "" && checkAgeStageReferenceBindingFor(saved, engine) != nil {
		saved = ""
	}
	return saved, ageStageSheetPromptFor(&project, &ch, stage, engine), nil
}

// ageStageSheetPrompt 先绑定主标准像参考，再接与角色四视图同一套 Moss 结构（人物外貌、服饰、画风，
// “由左到右”四格版式）。四视图自带正面面部特写，不再单独生成阶段标准像。
// 参考图只负责同一人物的脸部特征与画风；年龄、体型、发型与服装以阶段外貌为准，外貌未写服装/鞋履时才补默认值。
func ageStageSheetPrompt(project *models.Project, ch *models.Character, stage *models.CharacterAgeStage) string {
	return ageStageSheetPromptFor(project, ch, stage, AgeStagePortraitQwenEdit)
}

// ageStageSheetPromptFor 按所选引擎生成默认稿，只有参考图绑定写法随引擎不同。
func ageStageSheetPromptFor(project *models.Project, ch *models.Character, stage *models.CharacterAgeStage, engine string) string {
	token := ageStageReferenceToken(engine)
	binding := fmt.Sprintf("%s是角色「%s」的主标准像，仅用于保持同一人物的脸部特征（眼型、眉形、鼻型、脸部轮廓的延续）与项目画风；不沿用%s中的年龄、头身比例、发型与服装。", token, ch.Name, token)
	return binding + ageStageSheetBody(project, ch, stage)
}

func ageStageSheetBody(project *models.Project, ch *models.Character, stage *models.CharacterAgeStage) string {
	// 阶段外貌里写明的鞋履交给鞋履锁定，避免被按时代背景推断的默认鞋覆盖。
	view := models.Character{Name: ch.Name, Role: ch.Role, Gender: ch.Gender, Appearance: stage.Appearance, WardrobeDetail: ageStageClause(stage.Appearance, "鞋", "靴", "履", "屐")}
	age := 0
	if stage.Age != nil {
		age = *stage.Age
	}
	// 年龄来自阶段数据，只写一次（阶段外貌已写明同一年龄时不再前置）；锁定句用“该年龄”指代，不重复数字。
	const ageLock = "年龄感、身高头身比例与面部成熟度严格符合该年龄"
	appearance := strings.TrimSpace(stage.Appearance)
	parts := []string{}
	if prefix := ageLeadPrefix(appearance, ch.Gender, age); prefix != "" {
		parts = append(parts, prefix, ageLock, appearance)
	} else if _, _, written := explicitCharacterAgeRange(appearance); age > 0 || written {
		parts = append(parts, appearance, ageLock)
	} else {
		parts = append(parts, appearance)
	}
	classical := classicalChineseSetting(project, &view)
	if ageStageClause(stage.Appearance, "服", "衣", "衫", "裙", "袍", "裤", "褂", "甲") == "" {
		if classical {
			parts = append(parts, "服饰：符合该年龄的中国古代形制朴素日常服装，下装长及脚踝，款式简洁，不加额外配饰")
		} else {
			parts = append(parts, "服饰：符合该年龄与时代背景的朴素日常基础服装，款式简洁，不加额外配饰")
		}
	}
	if classical {
		parts = append(parts, "服装与鞋履均为中国古代形制，不出现短裙、丝袜、高跟鞋或任何现代服饰")
	}
	if style := strings.TrimSpace(project.Style); style != "" {
		parts = append(parts, style+"风格")
	}
	return normalizePortraitStyle(joinPromptClauses(parts), project) + "。" + characterSheetLayoutTail(project, &view)
}

// ageStageClause 返回外貌中第一个包含任一关键词的分句，没有时返回空串。
func ageStageClause(text string, words ...string) string {
	for _, clause := range strings.FieldsFunc(text, func(r rune) bool { return r == '，' || r == '。' || r == '；' || r == ',' || r == '\n' }) {
		for _, word := range words {
			if strings.Contains(clause, word) {
				return strings.TrimSpace(clause)
			}
		}
	}
	return ""
}

// ageStageGenderNoun 按年龄给出主体称谓，未知性别时用“人物”。
func ageStageGenderNoun(gender string, age int) string {
	female, male := strings.Contains(gender, "女"), strings.Contains(gender, "男")
	switch {
	case female && age < 12:
		return "女孩"
	case male && age < 12:
		return "男孩"
	case female && age < 18:
		return "少女"
	case male && age < 18:
		return "少年"
	case female:
		return "女性"
	case male:
		return "男性"
	}
	return "人物"
}

// SaveAgeStagePortraitPrompt 只保存人工审阅后的阶段四视图提示词，不提交生成、不改动阶段四视图与审核状态。
func (s *CharacterLookService) SaveAgeStagePortraitPrompt(projectID, characterID, stageID uint, prompt string) error {
	stage, err := s.loadAgeStage(s.db, projectID, characterID, stageID)
	if err != nil {
		return err
	}
	prompt = strings.TrimSpace(prompt)
	if prompt == "" {
		return fmt.Errorf("提示词不能为空")
	}
	if err := checkAgeStageReferenceBindingFor(prompt, stage.PortraitEngine); err != nil {
		return err
	}
	return s.db.Model(&models.CharacterAgeStage{}).Where("id = ?", stage.ID).Update("portrait_prompt", prompt).Error
}

// checkAgeStageReferenceBindingFor 提示词必须用所选引擎的写法绑定主标准像。
func checkAgeStageReferenceBindingFor(prompt, engine string) error {
	token := ageStageReferenceToken(engine)
	if !strings.Contains(prompt, token) {
		return fmt.Errorf("阶段四视图以主标准像为参考，提示词必须保留 %s", token)
	}
	return nil
}

// ageStageTemplate 返回阶段四视图使用的模板代码。两个引擎都只用一张参考图（主标准像）。
func ageStageTemplate(engine string) string {
	if engine == AgeStagePortraitH3Edit {
		return TemplateH3ImageEdit
	}
	return TemplateQwen21MultiEdit
}

// ageStageParams 按引擎给出出图参数：两者都固定 16:9 横幅，不跟随竖版主标准像。
// H3-Image 走本地宽高参数、无负向提示词（负向由条件零化代替），古典服饰限制只能写在正向提示词里。
func ageStageParams(engine string, project *models.Project, ch *models.Character) map[string]any {
	if engine == AgeStagePortraitH3Edit {
		return map[string]any{"width": ageStageSheetWidth, "height": ageStageSheetHeight}
	}
	params := qwenPortraitParams()
	params["aspect_ratio"] = qwenAspectRatio("16:9")
	params["use_aspect_ratio"] = true
	if classicalChineseSetting(project, ch) {
		params["negative_prompt"] = ageStageClassicalNegative
	}
	return params
}

// StartAgeStagePortrait 以角色主标准像为参考提交阶段四视图任务。prompt 为空时使用已保存稿或默认稿；提交前由用户审阅。
func (s *CharacterLookService) StartAgeStagePortrait(projectID, characterID, stageID uint, prompt string) error {
	if s.tasks == nil {
		return fmt.Errorf("年龄阶段四视图生成依赖 ComfyUI 任务服务")
	}
	stage, err := s.loadAgeStage(s.db, projectID, characterID, stageID)
	if err != nil {
		return err
	}
	if stage.PortraitTaskID != "" {
		return fmt.Errorf("该阶段四视图正在生成，请勿重复提交")
	}
	var project models.Project
	var ch models.Character
	if err := s.db.First(&project, projectID).Error; err != nil {
		return err
	}
	if err := s.db.Where("id = ? AND project_id = ?", characterID, projectID).First(&ch).Error; err != nil {
		return err
	}
	if strings.TrimSpace(ch.Portrait) == "" {
		return fmt.Errorf("阶段四视图以角色「%s」的主标准像为参考，请先生成或上传主标准像；也可直接上传阶段四视图", ch.Name)
	}
	engine, err := normalizeAgeStagePortraitEngine(stage.PortraitEngine)
	if err != nil {
		return err
	}
	prompt = strings.TrimSpace(prompt)
	// 已保存稿是给另一个引擎写的（绑定写法不同）时不能直接提交，按未填写处理。
	if prompt == "" && ageStageSheetLayout(stage.PortraitPrompt) && checkAgeStageReferenceBindingFor(stage.PortraitPrompt, engine) == nil {
		prompt = strings.TrimSpace(stage.PortraitPrompt)
	}
	if prompt == "" {
		prompt = ageStageSheetPromptFor(&project, &ch, stage, engine)
	}
	if err := checkAgeStageReferenceBindingFor(prompt, engine); err != nil {
		return err
	}
	// 两个引擎都固定 16:9 横幅：Qwen 打开 use_aspect_ratio，H3-Image 直接给宽高，都不跟随竖版主标准像。
	params := ageStageParams(engine, &project, &ch)
	files := map[string][]FileMeta{"ref_images": {{TaskID: fmt.Sprint(projectID), Name: ch.Portrait}}}
	templateCode := ageStageTemplate(engine)
	tpl, err := enabledTemplateByCode(s.db, templateCode)
	if err != nil {
		return err
	}
	task, err := s.tasks.CreateTask(CreateTaskReq{TemplateID: tpl.ID, Prompt: prompt, Params: params, Files: files})
	if err != nil {
		return err
	}
	res := s.db.Model(&models.CharacterAgeStage{}).Where("id = ? AND portrait_task_id = ''", stage.ID).
		Updates(map[string]any{"portrait_task_id": task.TaskID, "portrait_error": "", "portrait_prompt": prompt})
	if res.Error != nil || res.RowsAffected == 0 {
		_ = s.tasks.CancelTask(task.TaskID)
		if res.Error != nil {
			return res.Error
		}
		return fmt.Errorf("该阶段四视图任务已存在")
	}
	log.Printf("[age-stage-submit] project=%d character=%d stage=%d template=%s ref=%s", projectID, characterID, stage.ID, templateCode, ch.Portrait)
	go func() {
		if err := s.tasks.Execute(task.TaskID); err != nil && !errors.Is(err, errNoFreeGPU) {
			log.Printf("[age-stage %d] task %s failed: %v", stage.ID, task.TaskID, err)
		}
	}()
	return nil
}

func (s *CharacterLookService) SaveAgeStageUploadedPortrait(projectID, characterID, stageID uint, filename string, data []byte) error {
	if s.upload == nil {
		return fmt.Errorf("上传服务未配置")
	}
	ext := strings.ToLower(filepath.Ext(filename))
	if ext != ".png" && ext != ".jpg" && ext != ".jpeg" && ext != ".webp" {
		return fmt.Errorf("仅支持 PNG/JPG/WebP")
	}
	stage, err := s.loadAgeStage(s.db, projectID, characterID, stageID)
	if err != nil {
		return err
	}
	path, _, err := s.upload.SaveFile(fmt.Sprint(projectID), "image", fmt.Sprintf("age_stage_%d_%d%s", stage.ID, time.Now().UnixNano(), ext), data)
	if err != nil {
		return err
	}
	if stage.PortraitTaskID != "" && s.tasks != nil {
		_ = s.tasks.CancelTask(stage.PortraitTaskID)
	}
	return s.applyAgeStagePortrait(stage.ID, "", filepath.Base(path))
}

const ageStageOutfitOutdatedNote = "年龄阶段四视图已更换；已有定妆照与四视图保留，如需与新阶段形象一致请重新生成"

func ageStagePortraitHistory(raw string) []string {
	var files []string
	if strings.TrimSpace(raw) != "" {
		_ = json.Unmarshal([]byte(raw), &files)
	}
	return files
}

// pushAgeStagePortraitHistory 把被替换的阶段四视图放到历史最前面，并从历史中移除即将成为当前的那张。
func pushAgeStagePortraitHistory(raw, previous, next string) string {
	out := []string{}
	if previous != "" && previous != next {
		out = append(out, previous)
	}
	for _, file := range ageStagePortraitHistory(raw) {
		if file != "" && file != next && file != previous {
			out = append(out, file)
		}
	}
	if len(out) == 0 {
		return ""
	}
	body, _ := json.Marshal(out)
	return string(body)
}

// DeleteAgeStagePortraitHistory 删除历史里一张不满意的阶段四视图：先移除历史记录，再删文件。
// 当前正在使用的那张不许删（要先把它换下来）；文件删除失败只记日志，历史记录照常移除。
func (s *CharacterLookService) DeleteAgeStagePortraitHistory(projectID, characterID, stageID uint, file string) ([]string, error) {
	stage, err := s.loadAgeStage(s.db, projectID, characterID, stageID)
	if err != nil {
		return nil, err
	}
	file = strings.TrimSpace(file)
	if file == "" || strings.ContainsAny(file, `/\`) {
		return nil, fmt.Errorf("要删除的历史四视图文件名无效")
	}
	if file == stage.Portrait {
		return nil, fmt.Errorf("这张是当前使用的四视图，不能删除；请先把其它历史版本恢复为当前")
	}
	history := ageStagePortraitHistory(stage.PortraitHistory)
	remaining := make([]string, 0, len(history))
	found := false
	for _, old := range history {
		if old == file {
			found = true
			continue
		}
		remaining = append(remaining, old)
	}
	if !found {
		return nil, fmt.Errorf("该图片不在此阶段的历史四视图中")
	}
	raw := ""
	if len(remaining) > 0 {
		body, _ := json.Marshal(remaining)
		raw = string(body)
	}
	if err := s.db.Model(&models.CharacterAgeStage{}).Where("id = ?", stage.ID).Update("portrait_history", raw).Error; err != nil {
		return nil, err
	}
	if s.upload != nil {
		if err := s.upload.RemoveFile(fmt.Sprint(projectID), file); err != nil {
			log.Printf("[age-stage %d] 删除历史四视图文件 %s 失败: %v", stage.ID, file, err)
		}
		if err := s.db.Where("task_id = ? AND name = ?", fmt.Sprint(projectID), file).Delete(&models.UploadFile{}).Error; err != nil {
			log.Printf("[age-stage %d] 删除历史四视图上传记录 %s 失败: %v", stage.ID, file, err)
		}
	}
	return remaining, nil
}

// RestoreAgeStagePortrait 把历史中的某张阶段四视图恢复为当前；当前这张进入历史，不删除任何文件。
func (s *CharacterLookService) RestoreAgeStagePortrait(projectID, characterID, stageID uint, file string) error {
	stage, err := s.loadAgeStage(s.db, projectID, characterID, stageID)
	if err != nil {
		return err
	}
	if stage.PortraitTaskID != "" {
		return fmt.Errorf("该阶段四视图正在生成，请完成后再恢复历史版本")
	}
	file = strings.TrimSpace(file)
	for _, old := range ageStagePortraitHistory(stage.PortraitHistory) {
		if old == file && file != "" {
			return s.applyAgeStagePortrait(stage.ID, "", file)
		}
	}
	return fmt.Errorf("该图片不在此阶段的历史四视图中")
}

// applyAgeStagePortrait 写入新阶段四视图，旧图进入历史（不删除文件）。阶段需重新审核；
// 其下套装已有的定妆照/四视图保留，只取消在途任务并提示可重新生成。
// expectTaskID 非空时只在任务仍是当前任务时写入，防止旧任务覆盖新结果。
func (s *CharacterLookService) applyAgeStagePortrait(stageID uint, expectTaskID, file string) error {
	var cancel []string
	err := s.db.Transaction(func(tx *gorm.DB) error {
		var current models.CharacterAgeStage
		if err := tx.First(&current, stageID).Error; err != nil {
			return err
		}
		if expectTaskID != "" && current.PortraitTaskID != expectTaskID {
			return nil
		}
		q := tx.Model(&models.CharacterAgeStage{}).Where("id = ?", stageID)
		if expectTaskID != "" {
			q = q.Where("portrait_task_id = ?", expectTaskID)
		}
		res := q.Updates(map[string]any{"portrait": file, "portrait_history": pushAgeStagePortraitHistory(current.PortraitHistory, current.Portrait, file), "portrait_task_id": "", "portrait_error": "", "audit_status": models.LookStatusDraft, "audit_note": "", "version": gorm.Expr("version + 1")})
		if res.Error != nil || res.RowsAffected == 0 {
			return res.Error
		}
		// 已生成的套装定妆照/四视图属于用户资产，保留不删；只取消以旧阶段形象在途的任务并提示可重新生成。
		var outfits []models.CharacterOutfit
		if err := tx.Where("age_stage_id = ?", stageID).Find(&outfits).Error; err != nil {
			return err
		}
		for _, outfit := range outfits {
			cancel = append(cancel, nonEmptyStrings([]string{outfit.ImageTaskID, outfit.SheetTaskID})...)
			if err := tx.Model(&outfit).Updates(map[string]any{"image_task_id": "", "image_error": ageStageOutfitOutdatedNote, "sheet_task_id": ""}).Error; err != nil {
				return err
			}
		}
		return nil
	})
	if err == nil && s.tasks != nil {
		for _, id := range cancel {
			_ = s.tasks.CancelTask(id)
		}
	}
	return err
}

func (s *CharacterLookService) SyncAgeStagePortraits() {
	if !s.db.Migrator().HasTable(&models.CharacterAgeStage{}) {
		return
	}
	var stages []models.CharacterAgeStage
	if s.db.Where("portrait_task_id != ''").Find(&stages).Error != nil {
		return
	}
	for i := range stages {
		stage := &stages[i]
		fail := func(reason string) {
			s.db.Model(stage).Where("portrait_task_id = ?", stage.PortraitTaskID).Updates(map[string]any{"portrait_task_id": "", "portrait_error": reason})
		}
		var task models.Task
		if s.db.Where("task_id = ?", stage.PortraitTaskID).First(&task).Error != nil {
			fail("任务不存在")
			continue
		}
		if task.Status == "failed" || task.Status == "cancelled" {
			fail(task.Error)
			continue
		}
		if task.Status != "success" {
			continue
		}
		file, _ := resultImageOf(&task)
		if file == "" || task.Port == nil || s.upload == nil {
			fail("任务成功但未返回图片")
			continue
		}
		sub, name := filepath.ToSlash(filepath.Dir(file)), filepath.Base(file)
		if sub == "." {
			sub = ""
		}
		data, err := NewComfyClient(s.tasks.comfyHostForTask(&task), *task.Port).DownloadOutput(name, sub, "output")
		if err != nil {
			fail(err.Error())
			continue
		}
		ext := filepath.Ext(file)
		if ext == "" {
			ext = ".png"
		}
		path, _, err := s.upload.SaveFile(fmt.Sprint(stage.ProjectID), "image", outfitResultFilename("age_stage", stage.ID, stage.PortraitTaskID, ext), data)
		if err != nil {
			fail(err.Error())
			continue
		}
		if err := s.applyAgeStagePortrait(stage.ID, stage.PortraitTaskID, filepath.Base(path)); err != nil {
			log.Printf("[age-stage %d] apply portrait failed: %v", stage.ID, err)
		}
	}
}

// sceneAgeStage 是场景所选套装所属的年龄阶段及该套装名。
type sceneAgeStage struct {
	Stage      models.CharacterAgeStage
	OutfitName string
}

// sceneAgeStagesByCharacter 返回场景开场造型（Shot 1 覆盖优先，见 ADR-0012）属于年龄阶段的角色
// （按角色 ID）。只认已审核套装，与参考图选择一致；查询失败（含旧库无此表）时返回空表，
// 调用方保持原有角色主档案输出。
func sceneAgeStagesByCharacter(db *gorm.DB, sc *models.Scene) map[uint]sceneAgeStage {
	out := map[uint]sceneAgeStage{}
	for _, outfit := range sceneOpeningOutfits(db, sc) {
		if outfit.AgeStageID == 0 {
			continue
		}
		var stage models.CharacterAgeStage
		if db.Where("id = ? AND project_id = ? AND character_id = ?", outfit.AgeStageID, sc.ProjectID, outfit.CharacterID).First(&stage).Error == nil {
			out[outfit.CharacterID] = sceneAgeStage{Stage: stage, OutfitName: outfit.Name}
		}
	}
	return out
}

// ageStageCharacterDescription 替代角色主档案的 Trait/Style：阶段外貌 + 本场套装名。
// 套装细节由套装参考图承担，此处不重复描述服装。
func ageStageCharacterDescription(selected sceneAgeStage) string {
	desc := ageStageLabel(&selected.Stage) + "：" + strings.TrimSpace(selected.Stage.Appearance)
	if name := strings.TrimSpace(selected.OutfitName); name != "" {
		desc += "；本场造型：" + name
	}
	return desc
}

// markAgeStageScenesStale 使选用该阶段套装的场景提示词过期；只断开在途任务，已生成媒体保留。
func (s *CharacterLookService) markAgeStageScenesStale(stageID uint, reason string) error {
	var sceneIDs []uint
	if err := s.db.Model(&models.SceneCharacterOutfit{}).
		Joins("JOIN character_outfits ON character_outfits.id = scene_character_outfits.outfit_id").
		Where("character_outfits.age_stage_id = ?", stageID).Distinct().Pluck("scene_character_outfits.scene_id", &sceneIDs).Error; err != nil {
		return err
	}
	for _, sceneID := range sceneIDs {
		if err := s.markSceneOutfitSourceChanged(sceneID, reason); err != nil {
			return err
		}
	}
	return nil
}

func (s *CharacterLookService) markSceneOutfitSourceChanged(sceneID uint, reason string) error {
	var scene models.Scene
	if err := s.db.First(&scene, sceneID).Error; err != nil {
		return err
	}
	updates := map[string]any{"prompt_stale": true}
	cancel := staleSceneTaskUpdates(s.db, &scene, updates)
	if err := s.db.Model(&models.Scene{}).Where("id = ?", sceneID).Updates(updates).Error; err != nil {
		return err
	}
	cancelSceneTasks(s.tasks, cancel)
	return markSceneMediaSourceChanged(s.db, scene.ProjectID, sceneID, "", reason)
}
