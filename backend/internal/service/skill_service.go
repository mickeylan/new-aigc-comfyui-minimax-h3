package service

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"strings"
	"time"

	"gorm.io/gorm"

	"comfyui-console/internal/models"
)

// SkillService 创作技能管理服务
// Skill 仅作为提示词与输出规范，不拥有执行权限
type SkillService struct {
	db *gorm.DB
}

// NewSkillService 创建技能服务
func NewSkillService(db *gorm.DB) *SkillService {
	return &SkillService{db: db}
}

// InitSystemSkills 初始化系统内置技能（幂等）
func (s *SkillService) InitSystemSkills() error {
	systemSkills := []models.Skill{
		// plan 阶段技能
		{
			Name:           "短剧创作方案",
			Code:           "short-drama-plan",
			Version:        1,
			Description:    "基于短剧方法论生成完整创作方案：剧名、三幕结构、角色设定、分集目录、节奏/卡点/爽感矩阵",
			Stage:          models.SkillStagePlan,
			PromptTemplate: "请基于以下信息生成完整的创作方案：\n{{project_info}}\n\n要求：\n1. 生成 3 个候选剧名\n2. 规划三幕结构（入局/纠缠/决战）\n3. 设计主要角色（3-5 个）\n4. 制定分集目录（{{episode_count}} 集）\n5. 规划节奏曲线与付费卡点",
			SystemPrompt:   "你是一位专业的微短剧编剧，精通短视频平台的爆款短剧创作方法论。参考以下方法论文档：\n{{references}}\n\n创作原则：\n1. 三幕结构：入局（1-20集）/ 纠缠（21-60集）/ 决战（61-{{episode_count}}集）\n2. 节奏曲线：起势/攀升/风暴/决战配比合理\n3. 钩子设计：悬念钩/反转钩/情绪钩/信息钩/危机钩\n4. 付费卡点：占全集 10-15%，标注卡点集与悬念设计\n5. 爽感矩阵：打脸/逆袭/甜宠/虐心/燃/搞笑/感动",
			IsSystem:       true,
			Enabled:        true,
			SortOrder:      1,
		},
		// character 阶段技能
		{
			Name:           "角色设定标准",
			Code:           "character-standard",
			Version:        1,
			Description:    "生成结构化角色档案：外貌特征、性格设定、背景故事、关系图谱、服装细节、光影氛围",
			Stage:          models.SkillStageCharacter,
			PromptTemplate: "请为以下角色生成完整的结构化档案：\n{{character_info}}\n\n要求生成以下维度：\n1. 外貌特征（发型/五官/体型/特殊标记）\n2. 性格特点（MBTI/行为模式/情绪表达）\n3. 背景故事（出身/经历/动机/目标）\n4. 关系图谱（与其他角色的关系）\n5. 情绪表达（喜怒哀乐的表现形式）\n6. 习惯动作（口头禅/小动作）\n7. 服装细节（材质/颜色/配饰）\n8. 光影氛围（适合的打光风格）",
			SystemPrompt:   "你是一位专业的漫剧角色设计师，参考 LumxAI 人物维度规范：\n- appearance: 详细外貌描述（发型/发色/脸型/眉眼/鼻嘴/肤色/体型/特殊标记）\n- personality: MBTI 性格类型、核心性格标签、行为模式\n- background: 出身背景、成长经历、关键事件、角色动机\n- relationships: 关系图谱，关系动态变化\n- emotions: 情绪下的具体表现方式\n- habits: 常见小动作、口头禅、标志性行为\n- wardrobe_detail: 日常服装、重要场合造型、配饰\n- lighting_mood: 打光风格、主光源方向、氛围偏好",
			IsSystem:       true,
			Enabled:        true,
			SortOrder:      1,
		},
		// storyboard 阶段技能
		{
			Name:           "分镜剧本生成",
			Code:           "storyboard-generate",
			Version:        1,
			Description:    "基于创作方案和剧情提示词生成专业分镜：场景、画面描述、视频提示词、对白",
			Stage:          models.SkillStageStoryboard,
			PromptTemplate: "请生成第 {{episode_n}} 集的分镜剧本：\n\n剧情提示词：\n{{story_prompt}}\n\n角色设定：\n{{characters}}\n\n要求：\n1. 生成 {{scene_count}} 个场景\n2. 每个场景包含：场景标题、场景正文（视频提示词）、图生图画面提示词\n3. 每个场景输出出场角色列表\n4. 包含对白 dialogues（character/text/voice）\n5. 场景时长 3-8 秒",
			SystemPrompt:   "你是一位专业的分镜编剧，遵循以下规范：\n\n场景格式（每场输出 JSON）：\n{\n  \"title\": \"场景标题\",\n  \"content\": \"场景正文（视频提示词，中文）\",\n  \"image_prompt\": \"图生图画面提示词（英文，适合 AI 生图）\",\n  \"duration\": 5,\n  \"characters\": [\"角色1\", \"角色2\"],\n  \"dialogues\": [\n    {\"character\": \"角色1\", \"text\": \"台词1\"},\n    {\"character\": \"角色2\", \"text\": \"台词2\"}\n  ]\n}\n\n质量要求：\n- 景别变化：全景、中景、近景、特写交替使用\n- 每集 3-5 个场次\n- 结尾必须有悬念钩子\n- 付费卡点集结尾制造强悬念",
			IsSystem:       true,
			Enabled:        true,
			SortOrder:      1,
		},
		// image_prompt 阶段技能
		{
			Name:           "画面提示词优化",
			Code:           "image-prompt-optimize",
			Version:        1,
			Description:    "优化分镜画面提示词，确保角色一致性和画风统一",
			Stage:          models.SkillStageImagePrompt,
			PromptTemplate: "请优化以下画面提示词：\n\n原始提示词：\n{{original_prompt}}\n\n角色设定（必须严格遵守）：\n{{character_definitions}}\n\n画风要求：\n{{style_requirements}}",
			SystemPrompt:   "你是专业的 AI 生图提示词工程师，遵循以下原则：\n\n1. 角色一致性：注入权威 trait/style，不允许 LLM 漂移\n2. 景别规范：居中构图、中景、半身像\n3. 质量标签：masterpiece, best quality, highly detailed\n4. 负面提示词：lowres, bad anatomy, blurry, worst quality\n5. 画风统一：与项目整体风格保持一致\n\n输出格式：优化后的英文提示词",
			IsSystem:       true,
			Enabled:        true,
			SortOrder:      1,
		},
		// video_prompt 阶段技能
		{
			Name:           "视频提示词增强",
			Code:           "video-prompt-enhance",
			Version:        1,
			Description:    "增强视频生成提示词，优化镜头语言和动作描述",
			Stage:          models.SkillStageVideoPrompt,
			PromptTemplate: "请增强以下视频提示词：\n\n场景内容：\n{{scene_content}}\n\n角色动作：\n{{character_actions}}\n\n时长要求：{{duration}} 秒",
			SystemPrompt:   "你是专业的视频生成提示词工程师：\n\n1. 镜头语言：WIDE SHOT / MEDIUM SHOT / CLOSE-UP / EXTREME CLOSE-UP\n2. 动作描述：具体、连贯、可生成\n3. 摄像机运动：static / pan / tilt / dolly / tracking\n4. 光影氛围：自然光/人工光/戏剧光\n5. 输出格式：英文提示词，适合 MiniMax H3 i2v 模型",
			IsSystem:       true,
			Enabled:        true,
			SortOrder:      1,
		},
		// review 阶段技能
		{
			Name:           "剧本质量审核",
			Code:           "quality-review",
			Version:        1,
			Description:    "对已完成的剧本进行质量检查：节奏、爽点、台词、格式、连贯性",
			Stage:          models.SkillStageReview,
			PromptTemplate: "请对第 {{episode_n}} 集剧本进行质量检查：\n\n剧本内容：\n{{script_content}}\n\n检查维度：\n1. 节奏：开场是否够快、有无拖沓段落\n2. 爽点：数量是否足够、强度是否达标\n3. 台词：角色区分度、是否口语化\n4. 格式：场景头完整性、景别标注\n5. 连贯性：与前后集是否矛盾",
			SystemPrompt:   "你是一位专业的剧本审核专家，按以下维度评分（每项 1-10）：\n\n| 维度 | 检查内容 |\n|------|---------|\n| 节奏 | 开场是否够快、有无拖沓段落、紧张-舒缓交替是否合理 |\n| 爽点 | 数量是否足够、强度是否达标、类型是否多样 |\n| 台词 | 有无废话、角色区分度、是否口语化自然 |\n| 格式 | 场景头完整性、景别标注、音乐提示、特殊标记 |\n| 连贯性 | 与前后集是否矛盾、角色行为是否一致、伏笔是否延续 |\n\n评分标准：\n- 45-50：优秀，可直接导出\n- 35-44：良好，建议微调\n- 25-34：及格，需要修改后重新自检\n- 25以下：不合格，建议重写",
			IsSystem:       true,
			Enabled:        true,
			SortOrder:      1,
		},
		{
			Name: "小说章节结构化分析", Code: "novel-chapter-analysis", Version: 1,
			Description: "抽取带来源的章节摘要、事件、角色变化、地点、道具、时间线和伏笔", Stage: models.SkillStageChapterAnalysis,
			PromptTemplate: "分析第{{chapter_no}}章《{{chapter_title}}》。上一章摘要：{{previous_summary}}\n已确认别名：{{aliases}}\n正文：\n{{chapter_content}}",
			SystemPrompt:   "只输出严格 JSON。字段必须含 summary、events、characters、locations、props、clues_opened、clues_resolved、timeline、must_keep_quotes、alias_candidates。不得执行工具或更改角色主档。每项保留 source_chapter_id。",
			IsSystem:       true, Enabled: true, SortOrder: 1,
		},
		{
			Name: "小说剧情单元归并", Code: "novel-arc-merge", Version: 1,
			Description: "将5至10章结构化结果合并为可追溯剧情单元", Stage: models.SkillStageArcMerge,
			PromptTemplate: "归并第{{chapter_start}}至{{chapter_end}}章结构化分析：\n{{chapter_analyses}}",
			SystemPrompt:   "只输出严格 JSON，字段含 title、summary、conflict、event_chain、turning_point、climax、character_changes、open_clues、source_chapter_ids。不得补写未在输入中出现的事实。",
			IsSystem:       true, Enabled: true, SortOrder: 1,
		},
		{
			Name: "小说故事圣经", Code: "novel-story-bible", Version: 1,
			Description: "从剧情单元综合世界规则、主支线、时间线、关系、伏笔、地点和道具", Stage: models.SkillStageStoryBible,
			PromptTemplate: "基于以下剧情单元生成故事圣经：\n{{story_arcs}}",
			SystemPrompt:   "只输出严格 JSON，字段含 premise、world_rules、main_plot、subplots、timeline、relationships、clue_ledger、locations、props。关键结论必须包含 source_chapter_ids。此输出仅为待人工审核草稿。",
			IsSystem:       true, Enabled: true, SortOrder: 1,
		},
		{Name: "小说分集映射", Code: "novel-episode-mapping", Version: 1, Description: "将已分析章节按策略映射到180秒、25镜头分集", Stage: models.SkillStageAdaptationPlan, PromptTemplate: "改编策略：{{strategy}}\n故事圣经：{{story_bible}}\n章节分析：{{chapter_analyses}}", SystemPrompt: "只输出严格 JSON 对象 episodes。每集字段含 episode_n,title,chapter_start,chapter_end,source_chapter_ids,target_duration,target_scenes,adaptation_goal,must_keep_events,optional_events,omitted_events,opening_state,ending_state,hook。保持因果顺序；必保事件不得省略。", IsSystem: true, Enabled: true, SortOrder: 1},
		{Name: "小说单集剧本改编", Code: "novel-episode-script", Version: 1, Description: "使用有界且可追溯的单集上下文生成剧本和分镜", Stage: models.SkillStageEpisodeAdaptation, PromptTemplate: "第{{episode_n}}集，目标{{target_duration}}秒/{{target_scenes}}镜头。上下文：{{episode_context}}", SystemPrompt: "只使用输入事实并输出严格 JSON，字段含 script,opening_state,ending_state,visual_bible,scenes。scenes 数量和总时长须满足目标，每场含 title,content,image_prompt,duration,characters,location,props,dialogues。", IsSystem: true, Enabled: true, SortOrder: 1},
		{Name: "小说连续性复审", Code: "novel-continuity-review", Version: 1, Description: "比较前集结束、本集开场和剧本因果连续性", Stage: models.SkillStageContinuityReview, PromptTemplate: "前集结束：{{previous_ending}}\n本集开场：{{opening_state}}\n本集结束：{{ending_state}}\n剧本：{{script}}", SystemPrompt: "只输出严格 JSON：passed(bool), conflicts(array), recommendations(array)。地点、伤势、持有物、知识、关系、时间或未解释因果冲突必须判定失败。", IsSystem: true, Enabled: true, SortOrder: 1},
		{Name: "Krea2 巨构场景增强", Code: "krea2-megastructure-prompt", Version: 1, Description: "用结构、尺度参照、大气透视和镜头语言强化宏大场景，不改变剧情主体", Stage: models.SkillStageMegastructure, PromptTemplate: "巨构类别：{{mega_type}}。原始分镜：{{original_prompt}}。项目画风：{{style_requirements}}。角色约束：{{character_definitions}}。", SystemPrompt: "按主体结构、尺度锚定、材质表面、环境、大气光、镜头和媒介七层表达。保留原主体、人物、动作、颜色及空间关系，不得擅自增加剧情角色或关键道具。至少使用一个可辨认尺度参照物、近清远雾的大气分层、两个结构重复单元以及明确重量感；使用14至35mm低角度或合适远景，使主体占画面70%至90%或延伸出画框。尺寸形容词不超过两个，使用自然语言长句，禁止标签堆砌和元语言。", IsSystem: true, Enabled: true, SortOrder: 1},
		{Name: "资产连续性检查", Code: "reference-asset-continuity", Version: 1, Description: "内置自参考模板：在分镜生成前核对角色、场景和道具的连续性决策", Stage: models.SkillStageStoryboard, PromptTemplate: "资产圣经：{{asset_bible}}\n当前分镜：{{scene_content}}\n前一镜结束状态：{{previous_end_state}}", SystemPrompt: "逐项核对角色外观与服装、持有道具、地点布局、时间和伤势状态。只使用输入中已有资产；冲突时明确列出冲突，不得自行改写权威资产。", IsSystem: true, Enabled: true, SortOrder: 20},
		{Name: "单镜起止状态导演提示", Code: "reference-shot-state-prompt", Version: 1, Description: "内置自参考模板：把单镜动作、注意力交接与起止状态组织为可执行视频提示词", Stage: models.SkillStageVideoPrompt, PromptTemplate: "镜头描述：{{scene_content}}\n起始状态：{{start_state}}\n结束状态：{{end_state}}\n时长：{{duration}}秒", SystemPrompt: "按主体、动作、摄影机、光线氛围、视觉风格五段输出。动作在时长内必须可完成；明确视线或注意力交接；结束状态必须能作为下一镜的连续性输入。", IsSystem: true, Enabled: true, SortOrder: 20},
		{Name: "分镜覆盖度审查", Code: "reference-coverage-review", Version: 1, Description: "内置自参考模板：检查建立镜头、动作、反应、细节与转场覆盖", Stage: models.SkillStageReview, PromptTemplate: "剧本目标：{{story_goal}}\n待审分镜：{{storyboard}}", SystemPrompt: "检查每个剧情信息是否有可见画面承载，并评估建立镜头、主要动作、角色反应、关键细节和转场是否齐全。输出缺失项、重复项及最小补镜建议；不要为了数量机械补镜。", IsSystem: true, Enabled: true, SortOrder: 20},
		{Name: "导演视觉节拍拆镜", Code: "director-visual-beat-decomposition", Version: 1, Description: "将剧情节拍拆成可拍摄单镜：一镜一个主要可见动作，明确可见角色、发声者和时长", Stage: models.SkillStageStoryboard, PromptTemplate: "剧情事实：{{story_content}}\n已确认角色：{{characters}}\n地点与道具：{{asset_context}}\n目标时长：{{target_duration}}秒", SystemPrompt: "你是导演分镜师。把输入拆成最少数量的可执行镜头：每镜只承载一个主要可见动作或反应；同一句中存在多个连续动作时按必要节拍拆分；对白原文不得改写；明确区分画面可见角色、只发声角色和仅被提及角色；不得把剧情说明、心理描写或动作描述改成旁白或独白。景别和机位使用受控常用词；时长必须足以完成动作。输出严格JSON，字段为shots数组，每项仅含description,shot_type,camera_angle,camera_movement,duration,visible_characters,voice_characters,dialogues。", IsSystem: true, Enabled: true, SortOrder: 30},
		{Name: "导演单镜执行包", Code: "director-shot-packet", Version: 1, Description: "把单镜整理为五段导演提示词，并显式约束调度、连续性、声音和结束状态", Stage: models.SkillStageVideoPrompt, PromptTemplate: "镜头事实：{{scene_content}}\n角色与资产：{{asset_context}}\n起始状态：{{start_state}}\n结束状态：{{end_state}}\n时长：{{duration}}秒", SystemPrompt: "只输出当前单镜的五段JSON：subject,action,camera,lighting,style。保持人物身份、空间关系、道具和剧情事实；主体段说明画面位置和调度，动作段只写时长内可完成的主要动作与必要反应，摄影机段只使用一种主运镜，光线段保持时间和光源连续，风格段只描述媒介与质感。不得新增剧情、人物、对白、心理旁白或不在输入中的关键道具；结束状态必须可供下一镜连续使用。", IsSystem: true, Enabled: true, SortOrder: 30},
		{Name: "忠实最小改动润色", Code: "director-faithful-prompt-polish", Version: 1, Description: "在不改变剧情事实和用户导演决策的前提下，只增强可见、可执行的画面表达", Stage: models.SkillStageImagePrompt, PromptTemplate: "用户原稿：{{original_prompt}}\n不可变事实：{{immutable_facts}}\n允许增强项：{{editable_presentation}}\n用户反馈：{{feedback}}", SystemPrompt: "把不可变事实与可增强表现严格分开。不可变事实包括角色、动作、对白原文、地点、道具、空间关系、镜头目标和用户明确指定的风格；只可增强构图清晰度、动作可见性、光线层次、材质和摄影表达。修订时只修改用户反馈涉及的部分，其他内容逐项保留。禁止新增剧情、情绪结论、人物关系、旁白、独白、对白或资产。只输出润色后的提示词，不输出解释。", IsSystem: true, Enabled: true, SortOrder: 30},
	}

	for _, skill := range systemSkills {
		var latest models.Skill
		err := s.db.Where("code = ?", skill.Code).Order("version DESC, id DESC").First(&latest).Error
		switch {
		case err == gorm.ErrRecordNotFound:
			if err := s.db.Create(&skill).Error; err != nil {
				return fmt.Errorf("init skill %s failed: %w", skill.Code, err)
			}
		case err != nil:
			return fmt.Errorf("query skill %s failed: %w", skill.Code, err)
		case latest.Version < skill.Version:
			skill.ParentID = &latest.ID
			if err := s.db.Create(&skill).Error; err != nil {
				return fmt.Errorf("upgrade skill %s failed: %w", skill.Code, err)
			}
		}
	}
	return nil
}

// ListSkills 列出所有技能（支持按阶段筛选）
func (s *SkillService) ListSkills(stage string, enabledOnly bool) ([]models.Skill, error) {
	var skills []models.Skill
	query := s.db.Model(&models.Skill{})
	if stage != "" {
		query = query.Where("stage = ?", stage)
	}
	if enabledOnly {
		query = query.Where("enabled = ?", true)
	}
	query = query.Order("stage, sort_order, id")
	if err := query.Find(&skills).Error; err != nil {
		return nil, err
	}
	return skills, nil
}

// GetSkill 获取单个技能
func (s *SkillService) GetSkill(id uint) (*models.Skill, error) {
	var skill models.Skill
	if err := s.db.First(&skill, id).Error; err != nil {
		return nil, err
	}
	return &skill, nil
}

// GetSkillByCode 按代码获取技能
func (s *SkillService) GetSkillByCode(code string) (*models.Skill, error) {
	var skill models.Skill
	if err := s.db.Where("code = ?", code).Order("version DESC, id DESC").First(&skill).Error; err != nil {
		return nil, err
	}
	return &skill, nil
}

// CreateSkill 创建自定义技能（系统技能不可通过此接口创建）
func (s *SkillService) CreateSkill(input models.Skill) (*models.Skill, error) {
	if input.ID != 0 || input.Version != 0 || input.IsSystem || input.ParentID != nil ||
		!input.CreatedAt.IsZero() || !input.UpdatedAt.IsZero() {
		return nil, fmt.Errorf("创建技能包含服务端管理字段")
	}
	input.Name = strings.TrimSpace(input.Name)
	input.Code = strings.TrimSpace(input.Code)
	input.Stage = strings.TrimSpace(input.Stage)
	if input.Name == "" {
		return nil, fmt.Errorf("技能名称不能为空")
	}
	if input.Code == "" {
		return nil, fmt.Errorf("技能代码不能为空")
	}
	if input.Stage == "" {
		return nil, fmt.Errorf("适用阶段不能为空")
	}
	if !isValidStage(input.Stage) {
		return nil, fmt.Errorf("无效的阶段: %s", input.Stage)
	}
	input.Version = 1
	if err := s.db.Create(&input).Error; err != nil {
		return nil, err
	}
	return &input, nil
}

// UpdateSkill 更新自定义技能（系统技能只允许更新 prompt_template 和 system_prompt）
func (s *SkillService) UpdateSkill(id uint, updates map[string]any) (*models.Skill, error) {
	var skill models.Skill
	if err := s.db.First(&skill, id).Error; err != nil {
		return nil, err
	}
	allowed := map[string]bool{"description": true, "enabled": true, "sort_order": true}
	if !skill.IsSystem {
		allowed["name"] = true
		allowed["stage"] = true
		allowed["prompt_template"] = true
		allowed["system_prompt"] = true
	}
	for field, value := range updates {
		if !allowed[field] {
			return nil, fmt.Errorf("不允许修改字段: %s", field)
		}
		switch field {
		case "name", "stage", "description", "prompt_template", "system_prompt":
			if _, ok := value.(string); !ok {
				return nil, fmt.Errorf("字段 %s 类型无效", field)
			}
		case "enabled":
			if _, ok := value.(bool); !ok {
				return nil, fmt.Errorf("字段 enabled 类型无效")
			}
		case "sort_order":
			switch n := value.(type) {
			case int:
				// Service callers may already provide the native type.
			case float64:
				if n != float64(int(n)) {
					return nil, fmt.Errorf("字段 sort_order 类型无效")
				}
				updates[field] = int(n)
			default:
				return nil, fmt.Errorf("字段 sort_order 类型无效")
			}
		}
	}
	if name, ok := updates["name"]; ok {
		value, ok := name.(string)
		if !ok || strings.TrimSpace(value) == "" {
			return nil, fmt.Errorf("技能名称不能为空")
		}
		updates["name"] = strings.TrimSpace(value)
	}
	if stage, ok := updates["stage"]; ok {
		value, ok := stage.(string)
		if !ok || !isValidStage(strings.TrimSpace(value)) {
			return nil, fmt.Errorf("无效的阶段")
		}
		updates["stage"] = strings.TrimSpace(value)
	}
	if len(updates) == 0 {
		return &skill, nil
	}
	// 自定义 Skill 的内容修改创建不可变新版本，已有项目继续锁定原 SkillID/VersionSnapshot。
	versioned := !skill.IsSystem && (updates["name"] != nil || updates["stage"] != nil || updates["description"] != nil || updates["prompt_template"] != nil || updates["system_prompt"] != nil)
	if versioned {
		var latest models.Skill
		if err := s.db.Where("code = ?", skill.Code).Order("version DESC, id DESC").First(&latest).Error; err != nil {
			return nil, err
		}
		clone := latest
		clone.ID = 0
		clone.Version = latest.Version + 1
		clone.ParentID = &latest.ID
		clone.CreatedAt = time.Time{}
		clone.UpdatedAt = time.Time{}
		for field, value := range updates {
			switch field {
			case "name":
				clone.Name = value.(string)
			case "stage":
				clone.Stage = value.(string)
			case "description":
				clone.Description = value.(string)
			case "prompt_template":
				clone.PromptTemplate = value.(string)
			case "system_prompt":
				clone.SystemPrompt = value.(string)
			case "enabled":
				clone.Enabled = value.(bool)
			case "sort_order":
				clone.SortOrder = value.(int)
			}
		}
		if err := s.db.Create(&clone).Error; err != nil {
			return nil, err
		}
		return &clone, nil
	}
	if err := s.db.Model(&skill).Updates(updates).Error; err != nil {
		return nil, err
	}
	s.db.First(&skill, id)
	return &skill, nil
}

// DeleteSkill 删除自定义技能（系统技能不可删除）
func (s *SkillService) DeleteSkill(id uint) error {
	var skill models.Skill
	if err := s.db.First(&skill, id).Error; err != nil {
		return err
	}
	if skill.IsSystem {
		return fmt.Errorf("系统内置技能不可删除")
	}
	return s.db.Delete(&skill).Error
}

// UpgradeSkill 升级系统技能（创建新版本）
func (s *SkillService) UpgradeSkill(id uint, newVersion int, newPromptTemplate, newSystemPrompt string) (*models.Skill, error) {
	var skill models.Skill
	if err := s.db.First(&skill, id).Error; err != nil {
		return nil, err
	}
	if !skill.IsSystem {
		return nil, fmt.Errorf("自定义技能不支持版本升级，请直接更新")
	}

	var latest models.Skill
	if err := s.db.Where("code = ?", skill.Code).Order("version DESC, id DESC").First(&latest).Error; err != nil {
		return nil, err
	}
	if newVersion <= latest.Version {
		return nil, fmt.Errorf("新版本必须大于当前最新版本 %d", latest.Version)
	}

	// 始终从最新版本派生，避免从历史版本产生分叉。
	newSkill := models.Skill{
		Name:           latest.Name,
		Code:           latest.Code,
		Version:        newVersion,
		Description:    latest.Description,
		Stage:          latest.Stage,
		PromptTemplate: newPromptTemplate,
		SystemPrompt:   newSystemPrompt,
		IsSystem:       true,
		Enabled:        latest.Enabled,
		SortOrder:      latest.SortOrder,
		ParentID:       &latest.ID,
	}
	if err := s.db.Create(&newSkill).Error; err != nil {
		return nil, err
	}
	return &newSkill, nil
}

// GetAvailableStages 获取所有可用阶段
func (s *SkillService) GetAvailableStages() []map[string]string {
	return []map[string]string{
		{"value": models.SkillStagePlan, "label": "创作方案"},
		{"value": models.SkillStageCharacter, "label": "角色设定"},
		{"value": models.SkillStageStoryboard, "label": "分镜剧本"},
		{"value": models.SkillStageImagePrompt, "label": "画面提示词"},
		{"value": models.SkillStageVideoPrompt, "label": "视频提示词"},
		{"value": models.SkillStageReview, "label": "审核复审"},
		{"value": models.SkillStageChapterAnalysis, "label": "章节分析"},
		{"value": models.SkillStageArcMerge, "label": "剧情单元"},
		{"value": models.SkillStageStoryBible, "label": "故事圣经"},
		{"value": models.SkillStageAdaptationPlan, "label": "分集改编规划"},
		{"value": models.SkillStageEpisodeAdaptation, "label": "小说单集改编"},
		{"value": models.SkillStageContinuityReview, "label": "连续性复审"},
		{"value": models.SkillStageMegastructure, "label": "巨构画面提示词"},
	}
}

// ---------- 项目级技能配置 ----------

// GetProjectConfig 获取项目的技能配置
func (s *SkillService) GetProjectConfig(projectID uint) ([]models.ProjectSkillConfig, error) {
	var configs []models.ProjectSkillConfig
	if err := s.db.Preload("Skill").Where("project_id = ?", projectID).Find(&configs).Error; err != nil {
		return nil, err
	}
	return configs, nil
}

// GetProjectStageConfig 获取项目特定阶段的技能配置
func (s *SkillService) GetProjectStageConfig(projectID uint, stage string) (*models.ProjectSkillConfig, error) {
	var config models.ProjectSkillConfig
	err := s.db.Preload("Skill").Where("project_id = ? AND stage = ?", projectID, stage).First(&config).Error
	if err == gorm.ErrRecordNotFound {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return &config, nil
}

// SetProjectSkillConfig 设置项目特定阶段的技能
func (s *SkillService) SetProjectSkillConfig(projectID uint, stage string, skillID *uint, enabled bool) (*models.ProjectSkillConfig, error) {
	if !isValidStage(stage) {
		return nil, fmt.Errorf("无效的阶段: %s", stage)
	}
	if skillID != nil {
		var skill models.Skill
		if err := s.db.First(&skill, *skillID).Error; err != nil {
			return nil, fmt.Errorf("技能不存在: %w", err)
		}
		if skill.Stage != stage {
			return nil, fmt.Errorf("技能阶段不匹配：期望 %s，实际 %s", stage, skill.Stage)
		}
		if enabled && !skill.Enabled {
			return nil, fmt.Errorf("技能已被全局禁用")
		}
	}

	var config models.ProjectSkillConfig
	err := s.db.Where("project_id = ? AND stage = ?", projectID, stage).First(&config).Error

	versionSnapshot := 0
	if skillID != nil {
		var skill models.Skill
		if err := s.db.First(&skill, *skillID).Error; err == nil {
			versionSnapshot = skill.Version
		}
	}

	if err == gorm.ErrRecordNotFound {
		// 新建
		config = models.ProjectSkillConfig{
			ProjectID:       projectID,
			Stage:           stage,
			SkillID:         skillID,
			Enabled:         enabled,
			VersionSnapshot: versionSnapshot,
		}
		if err := s.db.Create(&config).Error; err != nil {
			return nil, err
		}
	} else if err == nil {
		// 更新
		updates := map[string]any{
			"skill_id":         skillID,
			"enabled":          enabled,
			"version_snapshot": versionSnapshot,
		}
		if err := s.db.Model(&config).Updates(updates).Error; err != nil {
			return nil, err
		}
	} else {
		return nil, err
	}

	// Reload with skill
	s.db.Preload("Skill").First(&config, config.ID)
	return &config, nil
}

// ResetProjectStageConfig 重置项目阶段技能配置（使用系统默认）
func (s *SkillService) ResetProjectStageConfig(projectID uint, stage string) error {
	return s.db.Where("project_id = ? AND stage = ?", projectID, stage).Delete(&models.ProjectSkillConfig{}).Error
}

// GetEffectiveSkill 获取项目阶段的有效技能（项目配置优先，否则系统默认）
func (s *SkillService) GetEffectiveSkill(projectID uint, stage string) (*models.Skill, error) {
	// 1. 先查项目配置
	config, err := s.GetProjectStageConfig(projectID, stage)
	if err != nil {
		return nil, err
	}
	if config != nil {
		if !config.Enabled {
			return nil, nil
		}
		if config.SkillID != nil {
			var skill models.Skill
			if err := s.db.Where("id = ? AND enabled = ?", *config.SkillID, true).First(&skill).Error; err == gorm.ErrRecordNotFound {
				return nil, fmt.Errorf("项目配置的技能不可用")
			} else if err != nil {
				return nil, err
			}
			return &skill, nil
		}
	}

	// 2. 查每个 code 的最新系统版本，再按 sort_order 选择默认
	var skill models.Skill
	err = s.db.Where("stage = ? AND enabled = ? AND is_system = ?", stage, true, true).
		Where("NOT EXISTS (SELECT 1 FROM skills newer WHERE newer.code = skills.code AND newer.version > skills.version)").
		Order("sort_order ASC, version DESC, id ASC").
		First(&skill).Error
	if err == gorm.ErrRecordNotFound {
		return nil, fmt.Errorf("阶段 %s 没有可用的系统技能", stage)
	}
	if err != nil {
		return nil, err
	}
	return &skill, nil
}

// ApplyPrompt 将项目阶段的有效 Skill 作为受控附加上下文装配到现有提示词。
func (s *SkillService) ApplyPrompt(projectID uint, stage, baseSystem, baseUser string, params map[string]string) (string, string, *models.Skill, error) {
	skill, err := s.GetEffectiveSkill(projectID, stage)
	if err != nil {
		return "", "", nil, err
	}
	if skill == nil {
		return baseSystem, baseUser, nil, nil
	}
	system, user := s.AssemblePrompt(skill, params)
	if strings.TrimSpace(system) != "" {
		baseSystem += "\n\n【当前项目 Skill 规则】\n" + system
	}
	if strings.TrimSpace(user) != "" {
		baseUser += "\n\n【当前项目 Skill 补充要求】\n" + user
	}
	return baseSystem, baseUser, skill, nil
}

func (s *SkillService) latestEnabledSkillByCode(code string) (*models.Skill, error) {
	var skill models.Skill
	err := s.db.Where("code = ? AND enabled = ?", code, true).Order("version DESC, id DESC").First(&skill).Error
	if err != nil {
		return nil, err
	}
	return &skill, nil
}

// ChatWithConfiguredOrFallbackSkill uses an explicitly configured project skill when present;
// otherwise it uses the named curated fallback. Every real invocation is audited.
func (s *SkillService) ChatWithConfiguredOrFallbackSkill(projectID uint, stage, fallbackCode string, provider TextProvider, baseSystem, baseUser string, params map[string]string) (string, error) {
	var skill *models.Skill
	config, err := s.GetProjectStageConfig(projectID, stage)
	if err != nil {
		return "", err
	}
	if config != nil && !config.Enabled {
		return provider.Chat(baseSystem, baseUser)
	}
	if config != nil && config.SkillID != nil {
		configured, err := s.GetSkill(*config.SkillID)
		if err != nil || !configured.Enabled {
			return "", fmt.Errorf("项目配置的技能不可用")
		}
		skill = configured
	} else {
		skill, err = s.latestEnabledSkillByCode(fallbackCode)
		if err != nil {
			return "", err
		}
	}
	system, user := s.AssemblePrompt(skill, params)
	if strings.TrimSpace(baseSystem) != "" {
		system = baseSystem + "\n\n" + system
	}
	if strings.TrimSpace(baseUser) != "" {
		user = baseUser + "\n\n" + user
	}
	started := time.Now()
	output, chatErr := provider.Chat(system, user)
	errText := ""
	if chatErr != nil {
		errText = chatErr.Error()
	}
	_ = s.LogSkillUsage(projectID, stage, skill, user, len(output), time.Since(started).Milliseconds(), chatErr == nil, errText)
	return output, chatErr
}

// ChatWithSkill 执行阶段化文本生成，并记录成功或失败的 Skill 版本审计。
func (s *SkillService) ChatWithSkill(projectID uint, stage string, provider TextProvider, baseSystem, baseUser string, params map[string]string) (string, error) {
	system, user, skill, err := s.ApplyPrompt(projectID, stage, baseSystem, baseUser, params)
	if err != nil {
		return "", err
	}
	started := time.Now()
	output, chatErr := provider.Chat(system, user)
	if skill != nil {
		errText := ""
		if chatErr != nil {
			errText = chatErr.Error()
		}
		_ = s.LogSkillUsage(projectID, stage, skill, user, len(output), time.Since(started).Milliseconds(), chatErr == nil, errText)
	}
	return output, chatErr
}

// ---------- 技能使用审计 ----------

// LogSkillUsage 记录技能使用（审计日志）
func (s *SkillService) LogSkillUsage(projectID uint, stage string, skill *models.Skill, input string, outputLength int, durationMS int64, success bool, errMsg string) error {
	inputHash := ""
	if input != "" {
		h := sha256.Sum256([]byte(input))
		inputHash = hex.EncodeToString(h[:])
	}

	log := models.SkillAuditLog{
		ProjectID:    projectID,
		Stage:        stage,
		SkillID:      skill.ID,
		SkillName:    skill.Name,
		SkillVersion: skill.Version,
		SkillCode:    skill.Code,
		InputHash:    inputHash,
		OutputLength: outputLength,
		DurationMS:   durationMS,
		Success:      success,
		Error:        errMsg,
	}
	return s.db.Create(&log).Error
}

// GetSkillAuditLogs 获取技能使用审计日志
func (s *SkillService) GetSkillAuditLogs(projectID uint, stage string, limit int) ([]models.SkillAuditLog, error) {
	var logs []models.SkillAuditLog
	query := s.db.Where("project_id = ?", projectID)
	if stage != "" {
		query = query.Where("stage = ?", stage)
	}
	if limit > 0 {
		query = query.Limit(limit)
	}
	query = query.Order("created_at DESC")
	if err := query.Find(&logs).Error; err != nil {
		return nil, err
	}
	return logs, nil
}

// ---------- 提示词装配 ----------

// AssemblePrompt 装配阶段提示词（替换占位符）
func (s *SkillService) AssemblePrompt(skill *models.Skill, params map[string]string) (systemPrompt, userPrompt string) {
	systemPrompt = skill.SystemPrompt
	userPrompt = skill.PromptTemplate

	// 替换占位符
	for key, value := range params {
		placeholder := "{{" + key + "}}"
		systemPrompt = strings.ReplaceAll(systemPrompt, placeholder, value)
		userPrompt = strings.ReplaceAll(userPrompt, placeholder, value)
	}

	return systemPrompt, userPrompt
}

// ---------- 内部辅助 ----------

func isValidStage(stage string) bool {
	stages := []string{
		models.SkillStagePlan,
		models.SkillStageCharacter,
		models.SkillStageStoryboard,
		models.SkillStageImagePrompt,
		models.SkillStageVideoPrompt,
		models.SkillStageReview,
		models.SkillStageChapterAnalysis,
		models.SkillStageArcMerge,
		models.SkillStageStoryBible,
		models.SkillStageAdaptationPlan,
		models.SkillStageEpisodeAdaptation,
		models.SkillStageContinuityReview,
		models.SkillStageMegastructure,
	}
	for _, s := range stages {
		if s == stage {
			return true
		}
	}
	return false
}

// SkillVersionHistory 获取技能的版本历史
func (s *SkillService) SkillVersionHistory(code string) ([]models.Skill, error) {
	var skills []models.Skill
	if err := s.db.Where("code = ?", code).Order("version DESC").Find(&skills).Error; err != nil {
		return nil, err
	}
	return skills, nil
}

// CountSkillsByStage 统计各阶段技能数量
func (s *SkillService) CountSkillsByStage() (map[string]int, error) {
	type StageCount struct {
		Stage string
		Count int
	}
	var counts []StageCount
	if err := s.db.Model(&models.Skill{}).
		Select("stage, count(*) as count").
		Group("stage").
		Find(&counts).Error; err != nil {
		return nil, err
	}
	result := make(map[string]int)
	for _, c := range counts {
		result[c.Stage] = c.Count
	}
	return result, nil
}
