package models

import (
	"encoding/json"
	"time"
)

// Instance 表示一个 ComfyUI 实例（每 GPU 一个）
type Instance struct {
	ID            uint      `gorm:"primaryKey" json:"id"`
	GPUIndex      int       `gorm:"column:gpu_index;uniqueIndex" json:"gpu_index"` // 平台内逻辑槽位，不作为实例身份
	Host          string    `gorm:"column:host;uniqueIndex:idx_instance_endpoint" json:"host"`
	Port          int       `gorm:"column:port;uniqueIndex:idx_instance_endpoint" json:"port"`
	Managed       bool      `gorm:"column:managed;default:true" json:"managed"`
	Status        string    `gorm:"column:status" json:"status"` // stopped/starting/running/error
	PID           int       `gorm:"column:pid" json:"pid"`
	EnableManager bool      `gorm:"column:enable_manager" json:"enable_manager"`
	QueueLen      int       `gorm:"column:queue_len" json:"queue_len"`
	VRAMFree      int64     `gorm:"column:vram_free" json:"vram_free"`
	VRAMTotal     int64     `gorm:"column:vram_total" json:"vram_total"`
	Util          float64   `gorm:"column:util" json:"util"`
	Temp          float64   `gorm:"column:temp" json:"temp"`
	Power         float64   `gorm:"column:power" json:"power"`
	LastChecked   time.Time `gorm:"column:last_checked" json:"last_checked"`
	CreatedAt     time.Time `json:"created_at"`
	UpdatedAt     time.Time `json:"updated_at"`
}

// Template 工作流模板
type Template struct {
	ID           uint      `gorm:"primaryKey" json:"id"`
	Name         string    `json:"name"`
	Code         string    `gorm:"uniqueIndex" json:"code"`
	Description  string    `json:"description"`
	WorkflowJSON string    `gorm:"type:text" json:"workflow_json"`
	InputsJSON   string    `gorm:"type:text" json:"inputs_json"`
	IsSystem     bool      `json:"is_system"`
	Enabled      bool      `json:"enabled"`
	CreatedAt    time.Time `json:"created_at"`
	UpdatedAt    time.Time `json:"updated_at"`
}

// UploadFile 已上传素材文件
type UploadFile struct {
	ID        uint      `gorm:"primaryKey" json:"id"`
	TaskID    string    `gorm:"column:task_id;index" json:"task_id"`
	Type      string    `json:"type"` // image/video/audio
	Name      string    `json:"name"`
	Path      string    `json:"path"`
	Size      int64     `json:"size"`
	CreatedAt time.Time `json:"created_at"`
}

// Task 生成任务
type Task struct {
	ID                uint       `gorm:"primaryKey" json:"id"`
	TaskID            string     `gorm:"column:task_id;uniqueIndex" json:"task_id"`
	TemplateID        uint       `gorm:"column:template_id;index:idx_task_status_template,priority:2" json:"template_id"`
	TemplateName      string     `gorm:"column:template_name" json:"template_name"`
	Prompt            string     `json:"prompt"`
	ParamsJSON        string     `gorm:"type:text" json:"params_json"`
	InputsJSON        string     `gorm:"type:text" json:"inputs_json"`
	ProvenanceJSON    string     `gorm:"column:provenance_json;type:text" json:"provenance_json"`
	ParentCandidateID *uint      `gorm:"column:parent_candidate_id;index" json:"parent_candidate_id,omitempty"`
	InstanceID        *uint      `gorm:"column:instance_id;index" json:"instance_id"`
	GPUIndex          *int       `gorm:"column:gpu_index" json:"gpu_index"`
	Port              *int       `gorm:"column:port" json:"port"`
	ComfyPromptID     string     `gorm:"column:comfy_prompt_id" json:"comfy_prompt_id"`
	Status            string     `gorm:"index:idx_task_status_updated,priority:1;index:idx_task_status_template,priority:1" json:"status"` // pending/queued/running/success/failed/cancelled
	Progress          float64    `json:"progress"`
	CurrentNode       string     `gorm:"column:current_node" json:"current_node"`
	Error             string     `json:"error"`
	ResultFiles       string     `gorm:"column:result_files;type:text" json:"result_files"`
	CreatedAt         time.Time  `json:"created_at"`
	UpdatedAt         time.Time  `gorm:"index:idx_task_status_updated,priority:2" json:"updated_at"`
	StartedAt         *time.Time `gorm:"column:started_at" json:"started_at"`
	FinishedAt        *time.Time `gorm:"column:finished_at" json:"finished_at"`
}

// PlaygroundRun is a project-free generation snapshot backed by a normal Task.
type PlaygroundRun struct {
	ID        uint            `gorm:"primaryKey" json:"id"`
	Mode      string          `gorm:"index" json:"mode"`
	Template  string          `gorm:"index" json:"template"`
	Prompt    string          `gorm:"type:text" json:"prompt"`
	Params    json.RawMessage `gorm:"type:text" json:"params"`
	Files     json.RawMessage `gorm:"type:text" json:"files"`
	TaskID    string          `gorm:"column:task_id;uniqueIndex" json:"task_id"`
	Status    string          `gorm:"index" json:"status"`
	Result    json.RawMessage `gorm:"type:text" json:"result"`
	Starred   bool            `gorm:"index;default:false" json:"starred"`
	CreatedAt time.Time       `gorm:"index" json:"created_at"`
}

// Event 任务事件日志
type Event struct {
	ID        uint      `gorm:"primaryKey" json:"id"`
	TaskID    string    `gorm:"column:task_id;index" json:"task_id"`
	Type      string    `json:"type"` // created/submitted/progress/executing/completed/failed/error
	Node      string    `json:"node"`
	Progress  float64   `json:"progress"`
	Message   string    `json:"message"`
	CreatedAt time.Time `json:"created_at"`
}

// Setting 平台设置（key-value，如火山引擎 API Key / 模型 ID 等）
type Setting struct {
	Key       string    `gorm:"primaryKey" json:"key"`
	Value     string    `json:"value"`
	UpdatedAt time.Time `json:"updated_at"`
}

// ProjectSourceType 项目来源类型常量
type ProjectSourceType string

const (
	ProjectSourceOutline ProjectSourceType = "outline" // 梗概项目：故事创意 → 创作方案 → 剧本 → 分镜画面 → 视频
	ProjectSourceNovel   ProjectSourceType = "novel"   // 小说改编项目：上传 TXT/MD → 章节识别 → 拆分合并 → 剧本生成
)

// NovelImportStatus 小说导入状态
type NovelImportStatus string

const (
	NovelImportPending    NovelImportStatus = "pending"    // 待处理
	NovelImportProcessing NovelImportStatus = "processing" // 处理中
	NovelImportCompleted  NovelImportStatus = "completed"  // 已完成
	NovelImportFailed     NovelImportStatus = "failed"     // 失败
)

// Project 漫剧项目：选题 → 创作方案 → 剧本 → 分镜画面 → 视频 → 合并成片
type Project struct {
	ID              uint              `gorm:"primaryKey" json:"id"`
	Title           string            `json:"title"`
	SourceType      ProjectSourceType `gorm:"column:source_type;default:outline" json:"source_type"` // 项目来源类型：outline(梗概)/novel(小说改编)
	Genre           string            `json:"genre"`                                                 // 题材（可选，支持组合如"科幻+悬疑"）
	Style           string            `json:"style"`                                                 // 画风（可选）
	Synopsis        string            `json:"synopsis"`                                              // 故事创意/一句话梗概
	Audience        string            `json:"audience"`                                              // 受众：女频/男频/全龄
	Tone            string            `json:"tone"`                                                  // 基调：爽/甜/虐/燃/搞笑/悬疑
	Ending          string            `json:"ending"`                                                // 结局：HE/BE/OE
	Episodes        int               `json:"episodes"`                                              // 目标集数（用于节奏规划）
	AspectRatio     string            `json:"aspect_ratio"`                                          // 画幅：16:9 横屏 / 9:16 竖屏 / 1:1 方形（默认 16:9）
	Plan            string            `gorm:"type:text" json:"plan"`                                 // 创作方案 JSON（short-drama 方法论产物）
	Script          string            `gorm:"type:text" json:"script"`                               // 最近一集剧本（兼容旧数据）
	Scripts         string            `gorm:"type:text" json:"scripts"`                              // 按集剧本 JSON map[int]string（episode_n -> 剧本正文）
	VisualBible     string            `gorm:"type:text" json:"visual_bible"`                         // 角色外观与统一画风基准
	Status          string            `json:"status"`                                                // draft/plan_done/script_done/producing/ready/finished/failed
	Error           string            `json:"error"`
	Generation      uint              `gorm:"default:0" json:"generation"`                               // 当前生成版本，防止旧任务回写新分镜
	PipelineStage   string            `gorm:"column:pipeline_stage;index" json:"pipeline_stage"`         // plan/script/images/videos/merge/finished/failed
	PipelineEpisode int               `gorm:"column:pipeline_episode;default:1" json:"pipeline_episode"` // 当前一键生成流水线的目标集数
	AutoGenerate    bool              `gorm:"column:auto_generate" json:"auto_generate"`
	StopAfterScript bool              `gorm:"column:stop_after_script" json:"-"` // 自动流水线生成完第一集剧本后停止（后续由人工处理）

	// 小说导入相关字段（仅 source_type=novel 时有效）
	NovelFilePath  string            `gorm:"column:novel_file_path" json:"novel_file_path,omitempty"`             // 原始小说文件路径
	NovelFileHash  string            `gorm:"column:novel_file_hash;size:64" json:"novel_file_hash,omitempty"`     // 文件内容哈希（检测重复上传）
	NovelWordCount int               `gorm:"column:novel_word_count" json:"novel_word_count,omitempty"`           // 总字数
	ImportStatus   NovelImportStatus `gorm:"column:import_status;default:pending" json:"import_status,omitempty"` // 导入状态
	ImportError    string            `gorm:"column:import_error;type:text" json:"import_error,omitempty"`         // 导入错误信息

	CreatedAt time.Time `json:"created_at"`
	UpdatedAt time.Time `json:"updated_at"`
}

// Episode 是项目内可持久化的集级聚合。Scene.EpisodeN 在兼容期继续保留。
type Episode struct {
	ID                   uint      `gorm:"primaryKey" json:"id"`
	ProjectID            uint      `gorm:"column:project_id;uniqueIndex:idx_episode_project_number" json:"project_id"`
	Number               int       `gorm:"column:episode_number;uniqueIndex:idx_episode_project_number" json:"number"`
	Title                string    `json:"title"`
	TargetDuration       float64   `gorm:"column:target_duration;default:180" json:"target_duration"`
	TargetScenes         int       `gorm:"column:target_scenes;default:25" json:"target_scenes"`
	Status               string    `gorm:"default:draft;index" json:"status"`
	Summary              string    `gorm:"type:text" json:"summary"`
	SummaryInputHash     string    `gorm:"column:summary_input_hash;size:64" json:"summary_input_hash"`
	SummaryStale         bool      `gorm:"column:summary_stale;default:false" json:"summary_stale"`
	NextHook             string    `gorm:"column:next_hook;type:text" json:"next_hook"`
	NextHookInputHash    string    `gorm:"column:next_hook_input_hash;size:64" json:"next_hook_input_hash"`
	NextHookStale        bool      `gorm:"column:next_hook_stale;default:false" json:"next_hook_stale"`
	CharacterAppearances string    `gorm:"column:character_appearances;type:text" json:"character_appearances"`
	Version              int       `gorm:"default:1" json:"version"`
	CreatedAt            time.Time `json:"created_at"`
	UpdatedAt            time.Time `json:"updated_at"`
}

// Scene 分镜场景（项目内顺序片段）
type Scene struct {
	ID                     uint      `gorm:"primaryKey" json:"id"`
	ProjectID              uint      `gorm:"column:project_id;index;uniqueIndex:idx_scene_project_generation_episode_order" json:"project_id"`
	EpisodeN               int       `gorm:"column:episode_n;default:1;uniqueIndex:idx_scene_project_generation_episode_order" json:"episode_n"` // 所属集数（从 1 开始）
	Order                  int       `gorm:"uniqueIndex:idx_scene_project_generation_episode_order" json:"order"`                                // 场景序号（从 1 开始）
	Generation             uint      `gorm:"uniqueIndex:idx_scene_project_generation_episode_order" json:"generation"`
	Title                  string    `json:"title"`                                                               // 场景标题
	Content                string    `json:"content"`                                                             // 场景正文（作为视频提示词）
	ImagePrompt            string    `json:"image_prompt"`                                                        // 文生图提示词
	ImageEngine            string    `gorm:"column:image_engine;default:minimax_h3" json:"image_engine"`          // minimax_h3/qwen_image_2_1
	NegativePrompt         string    `gorm:"column:negative_prompt;type:text" json:"negative_prompt"`             // 镜头层聚合的负面提示词
	ReferenceImagesJSON    string    `gorm:"column:reference_images_json;type:text" json:"-"`                     // 用户指定的有序参考图及 Krea2/H3 用途
	Duration               float64   `gorm:"default:5" json:"duration"`                                           // 场景目标时长（秒）
	Characters             string    `json:"characters"`                                                          // 兼容字段：旧数据中的出场角色名
	VisibleCharacters      string    `gorm:"column:visible_characters" json:"visible_characters"`                 // 画面中实际可见角色，逗号分隔；仅此类要求视觉参考
	VoiceCharacters        string    `gorm:"column:voice_characters" json:"voice_characters"`                     // 仅发声但不在画面中的角色，逗号分隔
	MentionedCharacters    string    `gorm:"column:mentioned_characters" json:"mentioned_characters"`             // 仅在剧情、对白或独白中被提及的角色
	CharacterRolesSet      bool      `gorm:"column:character_roles_set;default:false" json:"character_roles_set"` // 是否已明确完成可见/发声/提及分类
	LocationName           string    `gorm:"column:location_name" json:"location"`                                // 场景地点名（对应 location 资产，用于环境一致性注入）
	Props                  string    `json:"props"`                                                               // 出场关键道具名（逗号分隔），用于道具一致性注入
	VisualType             string    `gorm:"column:visual_type;default:normal" json:"visual_type"`                // normal/megastructure
	MegaType               string    `gorm:"column:mega_type" json:"mega_type"`                                   // architecture/creature/geological/mechanical/surreal
	ImageFile              string    `json:"image_file"`                                                          // 首帧图文件名（input/<project_id>/ 下）
	ImageLocked            bool      `gorm:"column:image_locked;default:false" json:"image_locked"`               // 禁止生成端点覆盖当前图片
	VideoLocked            bool      `gorm:"column:video_locked;default:false" json:"video_locked"`               // 禁止生成端点覆盖当前视频
	ImageToken             string    `gorm:"column:image_token" json:"-"`                                         // 单次生成令牌，防止并发或过期结果回写
	ImageTaskID            string    `gorm:"column:image_task_id;index" json:"image_task_id"`                     // 关联 Krea2 分镜画面任务
	VideoTaskID            string    `gorm:"column:video_task_id;index" json:"video_task_id"`                     // 关联视频生成任务
	VideoGPU               *int      `gorm:"column:video_gpu" json:"video_gpu"`
	VideoFile              string    `gorm:"column:video_file" json:"video_file"`                         // ComfyUI 输出相对路径（合并使用）
	VideoInputFile         string    `gorm:"column:video_input_file" json:"video_input_file"`             // 下载到项目 input 目录的浏览器可播放副本
	VideoPrompt            string    `gorm:"column:video_prompt;type:text" json:"video_prompt"`           // 用户可编辑的动作正文
	VideoFullPrompt        string    `gorm:"column:video_full_prompt;type:text" json:"video_full_prompt"` // 用户审核后最终提交的完整 H3 提示词
	VideoTemplate          string    `gorm:"column:video_template" json:"video_template"`                 // 视频模板（minimax_h3_i2v/ref2v/t2v/first_last 等；空则自动选择）
	VideoFirstFrameImg     string    `gorm:"column:video_first_frame_img" json:"video_first_frame_img"`   // 首尾帧模板的首帧图文件名
	VideoLastFrameImg      string    `gorm:"column:video_last_frame_img" json:"video_last_frame_img"`     // 首尾帧模板的尾帧图文件名
	Status                 string    `gorm:"index" json:"status"`                                         // pending/image_pending/image_ready/video_pending/video_running/video_ready/failed
	Error                  string    `json:"error"`
	PromptStale            bool      `gorm:"column:prompt_stale;default:false" json:"prompt_stale"` // 来源变化后需重新确认完整视频提示词
	ImageRetries           int       `gorm:"column:image_retries" json:"image_retries"`             // 画面生成已重试次数
	VideoRetries           int       `gorm:"column:video_retries" json:"video_retries"`             // 视频生成已重试次数
	ImageCandidateParentID *uint     `gorm:"column:image_candidate_parent_id" json:"-"`             // 当前图片任务的候选分支来源
	VideoCandidateParentID *uint     `gorm:"column:video_candidate_parent_id" json:"-"`             // 当前视频任务的候选分支来源
	ShotCount              int       `gorm:"column:shot_count;default:0" json:"shot_count"`         // 镜头数量
	CreatedAt              time.Time `json:"created_at"`
	UpdatedAt              time.Time `json:"updated_at"`
}

// Chapter 小说章节（仅 source_type=novel 的项目使用）
type Chapter struct {
	ID              uint      `gorm:"primaryKey" json:"id"`
	ProjectID       uint      `gorm:"column:project_id;index" json:"project_id"`
	Order           int       `gorm:"column:chapter_order;index" json:"order"`                     // 章节顺序（从 1 开始）
	Title           string    `json:"title"`                                                       // 章节标题（可人工修改）
	Content         string    `gorm:"type:text" json:"content"`                                    // 章节正文
	WordCount       int       `gorm:"column:word_count" json:"word_count"`                         // 字数统计
	IdentifiedBy    string    `gorm:"column:identified_by;default:rule" json:"identified_by"`      // 识别方式：rule(规则)/manual(手动)
	ManuallyEdited  bool      `gorm:"column:manually_edited;default:false" json:"manually_edited"` // 是否经人工修改
	ContentHash     string    `gorm:"column:content_hash;size:64;index" json:"content_hash"`
	Summary         string    `gorm:"type:text" json:"summary"`
	AnalysisJSON    string    `gorm:"column:analysis_json;type:text" json:"analysis_json"`
	AnalysisStatus  string    `gorm:"column:analysis_status;default:pending;index" json:"analysis_status"`
	AnalysisVersion int       `gorm:"column:analysis_version;default:0" json:"analysis_version"`
	AnalysisHash    string    `gorm:"column:analysis_hash;size:64" json:"analysis_hash"`
	AnalysisError   string    `gorm:"column:analysis_error;type:text" json:"analysis_error"`
	CreatedAt       time.Time `json:"created_at"`
	UpdatedAt       time.Time `json:"updated_at"`
}

// ChapterTaskType 章节任务类型
type ChapterTaskType string

const (
	ChapterTaskSplit   ChapterTaskType = "split"   // 拆分章节
	ChapterTaskMerge   ChapterTaskType = "merge"   // 合并章节
	ChapterTaskRetitle ChapterTaskType = "retitle" // 重命名章节
)

// ChapterTaskStatus 章节任务状态
type ChapterTaskStatus string

const (
	ChapterTaskPending    ChapterTaskStatus = "pending"    // 待处理
	ChapterTaskProcessing ChapterTaskStatus = "processing" // 处理中
	ChapterTaskCompleted  ChapterTaskStatus = "completed"  // 已完成
	ChapterTaskFailed     ChapterTaskStatus = "failed"     // 失败
)

// ChapterTask 章节操作任务（拆分/合并/重命名）
type ChapterTask struct {
	ID               uint              `gorm:"primaryKey" json:"id"`
	ProjectID        uint              `gorm:"column:project_id;index" json:"project_id"`
	TaskType         ChapterTaskType   `gorm:"column:task_type;index" json:"task_type"` // 任务类型
	Status           ChapterTaskStatus `gorm:"column:task_status;default:pending" json:"status"`
	SourceChapterID  *uint             `gorm:"column:source_chapter_id" json:"source_chapter_id,omitempty"`   // 来源章节ID（拆分/重命名）
	TargetChapterIDs string            `gorm:"column:target_chapter_ids" json:"target_chapter_ids,omitempty"` // 目标章节ID列表，逗号分隔（合并）
	NewTitle         string            `gorm:"column:new_title" json:"new_title,omitempty"`                   // 新标题（重命名）
	SplitPoints      string            `gorm:"column:split_points;type:text" json:"split_points,omitempty"`   // 拆分点位置 JSON
	ResultIDs        string            `gorm:"column:result_ids" json:"result_ids,omitempty"`                 // 结果章节ID列表
	Error            string            `gorm:"column:task_error;type:text" json:"error,omitempty"`
	CreatedAt        time.Time         `json:"created_at"`
	UpdatedAt        time.Time         `json:"updated_at"`
}

// CharacterProfileStatus 角色档案审核状态
type CharacterProfileStatus string

const (
	ProfileStatusDraft    CharacterProfileStatus = "draft"    // 草稿（AI 生成，待审核）
	ProfileStatusApproved CharacterProfileStatus = "approved" // 已审核通过
	ProfileStatusRejected CharacterProfileStatus = "rejected" // 已驳回，需修改
)

// Character 角色卡：项目内可复用的人物资产，统一外貌/服装设定以保证跨场景一致性
// 扩展支持 LumxAI 风格的结构化角色档案，包含详细外貌描述、性格设定、背景故事、关系图谱
type Character struct {
	ID             uint   `gorm:"primaryKey" json:"id"`
	ProjectID      uint   `gorm:"column:project_id;index;uniqueIndex:idx_character_project_name" json:"project_id"`
	Name           string `gorm:"uniqueIndex:idx_character_project_name" json:"name"`          // 项目内唯一
	Role           string `json:"role"`                                                        // 身份：主角/女主/反派/配角…
	Trait          string `gorm:"type:text" json:"trait"`                                      // 外貌特征（发型/五官/体型）
	Style          string `gorm:"type:text" json:"style"`                                      // 服装造型
	Portrait       string `json:"portrait"`                                                    // 标准参考像文件名（input/<project_id>/ 下）
	PortraitEngine string `gorm:"column:portrait_engine;default:krea2" json:"portrait_engine"` // krea2/qwen_image_2_1
	PortraitTaskID string `gorm:"column:portrait_task_id;index" json:"portrait_task_id"`       // Krea2 标准像生成任务
	PortraitError  string `gorm:"type:text" json:"portrait_error"`                             // 标准像生成错误
	Sheet          string `json:"sheet"`                                                       // 角色四视图文件名（input/<project_id>/ 下）
	SheetTaskID    string `gorm:"column:sheet_task_id;index" json:"sheet_task_id"`             // Krea2 四视图生成任务
	SheetError     string `gorm:"column:sheet_error;type:text" json:"sheet_error"`             // 四视图生成错误
	Voice          string `json:"voice"`                                                       // 预设 TTS 音色 ID（角色级，配音优先于平台角色映射）
	VoiceRef       string `gorm:"column:voice_ref" json:"voice_ref"`                           // 参考语音文件名（input/<project_id>/ 下）
	VoiceID        string `gorm:"column:voice_id" json:"voice_id"`                             // 参考语音注册的复刻音色 ID（阿里云 qwen-voice-enrollment）
	VoiceModel     string `gorm:"column:voice_model" json:"voice_model"`                       // 复刻音色绑定的合成模型（须与注册时 target_model 一致）
	Source         string `json:"source"`                                                      // auto(方案抽取) / manual(手动新建)

	// --- LumxAI 风格结构化角色档案字段 ---
	Appearance      string                 `gorm:"type:text" json:"appearance"`                               // 详细外貌描述（发型/发色/脸型/眉眼/鼻嘴/肤色/体型/特殊标记）
	Personality     string                 `gorm:"type:text" json:"personality"`                              // 性格特点（MBTI/行为模式/情绪表达习惯）
	Background      string                 `gorm:"type:text" json:"background"`                               // 背景故事（出身/经历/动机/目标）
	Relationships   string                 `gorm:"type:text" json:"relationships"`                            // 关系图谱（与其他角色的关系描述）
	Emotions        string                 `gorm:"type:text" json:"emotions"`                                 // 情绪表达方式（喜怒哀乐的表现形式）
	Habits          string                 `gorm:"type:text" json:"habits"`                                   // 习惯动作（小动作/口头禅/特殊习惯）
	WardrobeDetail  string                 `gorm:"type:text" json:"wardrobe_detail"`                          // 服装细节（材质/颜色/配饰/随时间变化的造型）
	LightingMood    string                 `gorm:"column:lighting_mood" json:"lighting_mood"`                 // 光影氛围偏好（适合该角色的打光风格）
	ColorPalette    string                 `gorm:"column:color_palette" json:"color_palette"`                 // 角色色调（主色/辅色/点缀色）
	ReferencePrompt string                 `gorm:"type:text" json:"reference_prompt"`                         // 生成的参考像提示词（高质量单人标准像）
	ProfileStatus   CharacterProfileStatus `gorm:"column:profile_status;default:draft" json:"profile_status"` // 档案审核状态
	ReviewNote      string                 `gorm:"type:text" json:"review_note"`                              // 审核意见（驳回原因或备注）
	ProfileVersion  int                    `gorm:"column:profile_version;default:0" json:"profile_version"`   // 档案版本号（用于追踪修改历史）

	CreatedAt time.Time `json:"created_at"`
	UpdatedAt time.Time `json:"updated_at"`
}

// Asset 视觉资产卡：道具（prop）/场景（location）参考图，项目内可复用，跨分镜保持一致
type Asset struct {
	ID          uint      `gorm:"primaryKey" json:"id"`
	ProjectID   uint      `gorm:"column:project_id;index;uniqueIndex:idx_asset_project_kind_name" json:"project_id"`
	Kind        string    `gorm:"column:kind;uniqueIndex:idx_asset_project_kind_name" json:"kind"` // prop(道具) / location(场景)
	Name        string    `gorm:"uniqueIndex:idx_asset_project_kind_name" json:"name"`             // 项目内同类别唯一
	Description string    `gorm:"type:text" json:"description"`                                    // 外观描述（道具：形状/材质/颜色/细节；场景：空间/建筑/光线氛围）
	VisualType  string    `gorm:"column:visual_type;default:normal" json:"visual_type"`            // 场景资产：normal/megastructure
	MegaType    string    `gorm:"column:mega_type" json:"mega_type"`                               // 场景资产巨构类别
	ImageEngine string    `gorm:"column:image_engine;default:krea2" json:"image_engine"`           // krea2/qwen_image_2_1
	VisibleText string    `gorm:"column:visible_text;type:text" json:"visible_text"`               // 必须逐字呈现在图中的可读文字
	Image       string    `json:"image"`                                                           // 参考图文件名（input/<project_id>/ 下）
	ImageTaskID string    `gorm:"column:image_task_id;index" json:"image_task_id"`                 // Krea2 参考图生成任务
	ImageError  string    `gorm:"column:image_error;type:text" json:"image_error"`                 // 参考图生成错误
	Sheet       string    `json:"sheet"`                                                           // 道具四视图文件名（input/<project_id>/ 下；location 不使用）
	SheetTaskID string    `gorm:"column:sheet_task_id;index" json:"sheet_task_id"`                 // Krea2 道具四视图任务
	SheetError  string    `gorm:"column:sheet_error;type:text" json:"sheet_error"`                 // 道具四视图错误
	Source      string    `json:"source"`                                                          // auto(方案抽取) / manual(手动新建)
	CreatedAt   time.Time `json:"created_at"`
	UpdatedAt   time.Time `json:"updated_at"`
}

// AssetVariant preserves alternate generated/uploaded images for a project-owned visual entity.
// EntityType identifies the target field (character_portrait, character_sheet,
// character_look, asset_image, or asset_sheet); EntityID is the owning row ID.
type AssetVariant struct {
	ID         uint      `gorm:"primaryKey" json:"id"`
	ProjectID  uint      `gorm:"column:project_id;index:idx_asset_variant_entity" json:"project_id"`
	EntityType string    `gorm:"column:entity_type;size:40;index:idx_asset_variant_entity" json:"entity_type"`
	EntityID   uint      `gorm:"column:entity_id;index:idx_asset_variant_entity" json:"entity_id"`
	File       string    `gorm:"column:file;not null" json:"file"`
	Prompt     string    `gorm:"type:text" json:"prompt"`
	Provenance string    `gorm:"type:text" json:"provenance"`
	Selected   bool      `gorm:"default:false;index" json:"selected"`
	Favorite   bool      `gorm:"default:false;index" json:"favorite"`
	CreatedAt  time.Time `json:"created_at"`
	UpdatedAt  time.Time `json:"updated_at"`
}

// MergeTask 视频合并任务（把多个场景视频合并剪辑成片）
type MergeTask struct {
	ID                     uint      `gorm:"primaryKey" json:"id"`
	ProjectID              uint      `gorm:"column:project_id;index" json:"project_id"`
	EpisodeN               int       `gorm:"column:episode_n;default:1" json:"episode_n"` // 所属集数
	Title                  string    `json:"title"`
	SceneOrder             string    `json:"scene_order"`                                        // 按序合并的场景 ID（逗号分隔）
	SceneVideoFingerprints string    `gorm:"column:scene_video_fingerprints;type:text" json:"-"` // 提交时的视频输入快照，防止过期合并结果发布
	InputSnapshot          string    `gorm:"column:input_snapshot;type:text" json:"-"`           // 所有合并输入与选项的不可变快照
	Generation             uint      `gorm:"index" json:"generation"`                            // 本集权威场景版本（不等同于项目全局版本）
	RequestedDub           bool      `gorm:"column:requested_dub;default:false" json:"requested_dub"`
	RequestedSubtitles     bool      `gorm:"column:requested_subtitles;default:false" json:"requested_subtitles"`
	Status                 string    `json:"status"`                        // pending/running/success/failed
	OutputFile             string    `json:"output_file"`                   // 合并输出文件（相对 output_workers/gpu0/ 路径）
	Subtitle               bool      `gorm:"default:false" json:"subtitle"` // 是否生成了配音字幕（SRT 与成片同名）
	NativeVolume           float64   `gorm:"column:native_volume;default:1" json:"native_volume"`
	DialogueVolume         float64   `gorm:"column:dialogue_volume;default:1" json:"dialogue_volume"`
	BGMVolume              float64   `gorm:"column:bgm_volume;default:1" json:"bgm_volume"`
	DialogueMix            bool      `gorm:"column:dialogue_mix;default:false" json:"dialogue_mix"`
	Error                  string    `json:"error"`
	CreatedAt              time.Time `json:"created_at"`
	UpdatedAt              time.Time `json:"updated_at"`
}

// Material 素材库：生成的图片/视频自动入库，也支持手动上传管理
type Material struct {
	ID        uint      `gorm:"primaryKey" json:"id"`
	Name      string    `json:"name"`
	Type      string    `gorm:"index" json:"type"`                         // image/video/audio
	Source    string    `json:"source"`                                    // scene(场景生成)/upload(手动上传)
	ProjectID *uint     `gorm:"column:project_id;index" json:"project_id"` // 关联项目（可选）
	SceneID   *uint     `gorm:"column:scene_id" json:"scene_id"`           // 关联场景（可选）
	Path      string    `json:"path"`                                      // input 相对路径（task_id/filename）
	Prompt    string    `json:"prompt"`                                    // 生成提示词（可选）
	Size      int64     `json:"size"`
	CreatedAt time.Time `json:"created_at"`
}

// ScriptRevision 保存某一集完整剧本层级的不可变 JSON 快照。
type ScriptRevision struct {
	ID               uint      `gorm:"primaryKey" json:"id"`
	ProjectID        uint      `gorm:"column:project_id;index:idx_script_revision_project_created" json:"project_id"`
	EpisodeN         int       `gorm:"column:episode_n;default:1;index" json:"episode_n"`
	Reason           string    `gorm:"size:120" json:"reason"`
	SourceRevisionID *uint     `gorm:"column:source_revision_id;index" json:"source_revision_id,omitempty"`
	SnapshotJSON     string    `gorm:"column:snapshot_json;type:text" json:"-"`
	CreatedAt        time.Time `gorm:"index:idx_script_revision_project_created,sort:desc" json:"created_at"`
}

// Dialogue 场景对白：用于 TTS 配音与 SRT 字幕生成
type Dialogue struct {
	ID                 uint      `gorm:"primaryKey" json:"id"`
	SceneID            uint      `gorm:"column:scene_id;index" json:"scene_id"`
	ProjectID          uint      `gorm:"column:project_id;index" json:"project_id"`
	Order              int       `json:"order"`                                                             // 场景内句序（从 1 开始）
	Character          string    `json:"character"`                                                         // 对白/内心独白为角色名；旁白可为“旁白”
	SpeechType         string    `gorm:"column:speech_type;default:dialogue" json:"speech_type"`            // dialogue/narration/monologue
	H3VoiceDescription string    `gorm:"column:h3_voice_description;type:text" json:"h3_voice_description"` // H3 说话者声音身份
	Text               string    `gorm:"type:text" json:"text"`                                             // 仅可发声原文；空说话人且无明确类型时不发声
	Voice              string    `json:"voice"`                                                             // TTS 音色（voice_type）
	Position           float64   `gorm:"default:0" json:"position"`                                         // 场景内起始位置（秒；0 表示自动顺排）
	Offset             float64   `gorm:"default:0" json:"offset"`                                           // 相对自动/指定位置的微调秒数，可为负
	Speed              float64   `gorm:"default:1" json:"speed"`                                            // 播放/合并速度（0.5~2）
	Pitch              float64   `gorm:"default:0" json:"pitch"`                                            // 音高半音（-12~12）
	Volume             float64   `gorm:"default:1" json:"volume"`                                           // 单句增益（0~4）
	Emotion            string    `json:"emotion"`                                                           // 供应商支持时传入，否则仅持久化供 QA
	Delivery           string    `gorm:"type:text" json:"delivery"`                                         // 表演/语气说明
	AudioFile          string    `json:"audio_file"`                                                        // 当前合成/已应用预览音频
	PreviousAudioFile  string    `gorm:"column:previous_audio_file" json:"previous_audio_file"`             // 最近一次替换前的音频，可一键回退
	PreviousAudioHash  string    `gorm:"column:previous_audio_hash;size:64" json:"-"`
	AudioRevision      int       `gorm:"column:audio_revision;default:0" json:"audio_revision"`
	AudioHash          string    `gorm:"column:audio_hash;size:64" json:"audio_hash"` // 生成当前音频时的输入摘要
	AudioToken         string    `gorm:"column:audio_token;size:64;index" json:"-"`   // 防止迟到合成覆盖新输入
	AudioStale         bool      `gorm:"column:audio_stale;default:false" json:"audio_stale"`
	AudioStaleReason   string    `gorm:"column:audio_stale_reason" json:"audio_stale_reason"`
	Status             string    `json:"status"` // pending/synthesizing/ready/failed
	Error              string    `json:"error"`
	CreatedAt          time.Time `json:"created_at"`
	UpdatedAt          time.Time `json:"updated_at"`
}

// Skill 创作技能模板：提供阶段化提示词装配，支持系统内置与项目级覆盖
// Skill 仅作为提示词模板，不拥有执行权限（无网络/Shell/文件/数据库访问）
type Skill struct {
	ID             uint      `gorm:"primaryKey" json:"id"`
	Name           string    `gorm:"index" json:"name"`                                           // 技能名称（版本之间共享 Name）
	Code           string    `gorm:"index;uniqueIndex:idx_skill_code_version" json:"code"`        // 技能代码（与 Version 组成唯一版本标识）
	Version        int       `gorm:"default:1;uniqueIndex:idx_skill_code_version" json:"version"` // 版本号（每次更新递增）
	Description    string    `gorm:"type:text" json:"description"`                                // 技能描述与用途说明
	Stage          string    `gorm:"column:stage;index" json:"stage"`                             // 适用阶段：plan/character/storyboard/image_prompt/video_prompt/review
	PromptTemplate string    `gorm:"type:text" json:"prompt_template"`                            // 提示词模板（支持 {{param}} 占位符）
	SystemPrompt   string    `gorm:"type:text" json:"system_prompt"`                              // 系统提示词片段（追加到主 system prompt）
	Operation      string    `gorm:"column:operation;index" json:"operation"`                     // 精确操作契约；空值向后兼容为 Code
	IsSystem       bool      `gorm:"column:is_system;default:false" json:"is_system"`             // 是否系统内置（系统技能不可删除，只可升级版本）
	Enabled        bool      `gorm:"default:true" json:"enabled"`                                 // 是否启用
	SortOrder      int       `gorm:"default:0" json:"sort_order"`                                 // 排序顺序（同一阶段内）
	ParentID       *uint     `gorm:"column:parent_id" json:"parent_id"`                           // 父技能 ID（用于版本追踪，null 表示无父版本）
	CreatedAt      time.Time `json:"created_at"`
	UpdatedAt      time.Time `json:"updated_at"`
}

// SkillStage 技能适用阶段常量
const (
	SkillStagePlan              = "plan"                       // 创作方案阶段
	SkillStageCharacter         = "character"                  // 角色设定阶段
	SkillStageStoryboard        = "storyboard"                 // 分镜剧本阶段
	SkillStageImagePrompt       = "image_prompt"               // 画面提示词阶段
	SkillStageVideoPrompt       = "video_prompt"               // 视频提示词阶段
	SkillStageReview            = "review"                     // 审核/复审阶段
	SkillStageChapterAnalysis   = "chapter_analysis"           // 小说章节结构化分析
	SkillStageArcMerge          = "arc_merge"                  // 小说剧情单元归并
	SkillStageStoryBible        = "story_bible"                // 小说故事圣经综合
	SkillStageAdaptationPlan    = "adaptation_plan"            // 小说分集映射
	SkillStageEpisodeAdaptation = "episode_adaptation"         // 小说单集剧本改编
	SkillStageContinuityReview  = "continuity_review"          // 小说集间连续性复审
	SkillStageMegastructure     = "megastructure_prompt"       // Krea2 巨构场景提示词增强
	SkillStageCreativeIntent    = "creative_intent"            // 用户显式触发的创作意图澄清
	SkillStageQwenImageT2I      = "qwen_image_t2i_prompt"      // Qwen-Image-2.1 文生图提示词
	SkillStageQwenImageEdit     = "qwen_image_edit_prompt"     // Qwen-Image-2.1 多图编辑提示词
	SkillStageQwenCharacterCard = "qwen_character_card_prompt" // Qwen-Image-2.1 角色设定卡提示词
)

// ProjectSkillConfig 项目级技能配置：支持项目选择特定技能或覆盖系统默认
type ProjectSkillConfig struct {
	ID              uint      `gorm:"primaryKey" json:"id"`
	ProjectID       uint      `gorm:"column:project_id;uniqueIndex:idx_proj_stage_operation" json:"project_id"`
	Stage           string    `gorm:"column:stage;uniqueIndex:idx_proj_stage_operation" json:"stage"`
	Operation       string    `gorm:"column:operation;uniqueIndex:idx_proj_stage_operation" json:"operation"`
	SkillID         *uint     `gorm:"column:skill_id" json:"skill_id"`                 // 选中技能 ID（nil 表示使用系统默认）
	Enabled         bool      `gorm:"default:true" json:"enabled"`                     // 该阶段是否启用技能注入
	VersionSnapshot int       `gorm:"column:version_snapshot" json:"version_snapshot"` // 生成时锁定的技能版本
	CreatedAt       time.Time `json:"created_at"`
	UpdatedAt       time.Time `json:"updated_at"`

	// 关联
	Skill *Skill `gorm:"foreignKey:SkillID" json:"skill,omitempty"`
}

// SkillAuditLog 技能使用审计日志：记录每次生成使用的技能版本
// StoryArc is a 5-10 chapter structured merge product.
type StoryArc struct {
	ID             uint   `gorm:"primaryKey" json:"id"`
	ProjectID      uint   `gorm:"column:project_id;uniqueIndex:idx_story_arc_project_no" json:"project_id"`
	ArcNo          int    `gorm:"column:arc_no;uniqueIndex:idx_story_arc_project_no" json:"arc_no"`
	Title          string `json:"title"`
	ChapterStart   int    `gorm:"column:chapter_start" json:"chapter_start"`
	ChapterEnd     int    `gorm:"column:chapter_end" json:"chapter_end"`
	Summary        string `gorm:"type:text" json:"summary"`
	AnalysisJSON   string `gorm:"column:analysis_json;type:text" json:"analysis_json"`
	SourceVersions string `gorm:"column:source_versions;type:text" json:"source_versions"`
	Status         string `gorm:"default:draft;index" json:"status"`
	Version        int    `gorm:"default:1" json:"version"`
	Error          string `gorm:"type:text" json:"error"`

	// Batch planning fields
	BatchID       *uint      `gorm:"column:batch_id;index" json:"batch_id,omitempty"`                 // 所属批次
	ReviewStatus  string     `gorm:"column:review_status;default:pending;index" json:"review_status"` // pending/approved/rejected
	ReviewerNotes string     `gorm:"column:reviewer_notes;type:text" json:"reviewer_notes"`           // 审核意见
	ReviewerID    *uint      `gorm:"column:reviewer_id" json:"reviewer_id,omitempty"`                 // 审核人
	ReviewedAt    *time.Time `gorm:"column:reviewed_at" json:"reviewed_at,omitempty"`                 // 审核时间

	CreatedAt time.Time `json:"created_at"`
	UpdatedAt time.Time `json:"updated_at"`
}

// StoryBible is the editable, explicitly approved whole-book synthesis.
type StoryBible struct {
	ID                uint      `gorm:"primaryKey" json:"id"`
	ProjectID         uint      `gorm:"column:project_id;uniqueIndex" json:"project_id"`
	Premise           string    `gorm:"type:text" json:"premise"`
	WorldRules        string    `gorm:"type:text" json:"world_rules"`
	MainPlot          string    `gorm:"type:text" json:"main_plot"`
	Subplots          string    `gorm:"type:text" json:"subplots"`
	TimelineJSON      string    `gorm:"column:timeline_json;type:text" json:"timeline_json"`
	RelationshipsJSON string    `gorm:"column:relationships_json;type:text" json:"relationships_json"`
	ClueLedgerJSON    string    `gorm:"column:clue_ledger_json;type:text" json:"clue_ledger_json"`
	LocationBibleJSON string    `gorm:"column:location_bible_json;type:text" json:"location_bible_json"`
	PropBibleJSON     string    `gorm:"column:prop_bible_json;type:text" json:"prop_bible_json"`
	AdaptationRules   string    `gorm:"type:text" json:"adaptation_rules"`
	SourceVersions    string    `gorm:"column:source_versions;type:text" json:"source_versions"`
	Status            string    `gorm:"default:draft;index" json:"status"`
	Version           int       `gorm:"default:1" json:"version"`
	Error             string    `gorm:"type:text" json:"error"`
	CreatedAt         time.Time `json:"created_at"`
	UpdatedAt         time.Time `json:"updated_at"`
}

// PlanningBatch represents a rolling batch of episodes (5-20) for phased adaptation.
// Each batch must be reviewed and approved before the next batch can be generated.
type PlanningBatch struct {
	ID             uint   `gorm:"primaryKey" json:"id"`
	ProjectID      uint   `gorm:"column:project_id;uniqueIndex:idx_batch_project_no" json:"project_id"`
	BatchNo        int    `gorm:"column:batch_no;uniqueIndex:idx_batch_project_no" json:"batch_no"` // 批次序号（从 1 开始）
	Title          string `json:"title"`                                                            // 批次标题，如"第一卷 相遇篇"
	EpisodeStart   int    `gorm:"column:episode_start" json:"episode_start"`                        // 本批次起始集数
	EpisodeEnd     int    `gorm:"column:episode_end" json:"episode_end"`                            // 本批次结束集数
	EpisodeCount   int    `gorm:"column:episode_count" json:"episode_count"`                        // 本批次集数
	ArcRange       string `gorm:"column:arc_range" json:"arc_range"`                                // 关联的故事弧编号/ID
	ChapterStart   int    `gorm:"column:chapter_start" json:"chapter_start"`                        // 原文章节起点
	ChapterEnd     int    `gorm:"column:chapter_end" json:"chapter_end"`                            // 原文章节终点
	Summary        string `gorm:"type:text" json:"summary"`                                         // 批次整体摘要
	GenerationJSON string `gorm:"column:generation_json;type:text" json:"generation_json"`          // 生成参数及结果快照

	// Status tracking
	Status        string     `gorm:"default:draft;index" json:"status"`                               // draft/generating/review/approved/produced/cancelled
	ReviewStatus  string     `gorm:"column:review_status;default:pending;index" json:"review_status"` // pending/approved/rejected
	ReviewerNotes string     `gorm:"column:reviewer_notes;type:text" json:"reviewer_notes"`
	ReviewerID    *uint      `gorm:"column:reviewer_id" json:"reviewer_id,omitempty"`
	ReviewedAt    *time.Time `gorm:"column:reviewed_at" json:"reviewed_at,omitempty"`

	// Batch progress
	TotalEpisodes int `gorm:"column:total_episodes;default:0" json:"total_episodes"` // 目标集数
	ApprovedCount int `gorm:"column:approved_count;default:0" json:"approved_count"` // 已审核通过集数
	ProducedCount int `gorm:"column:produced_count;default:0" json:"produced_count"` // 已进入生产的集数

	// Rolling control
	PreviousBatchID *uint `gorm:"column:previous_batch_id;index" json:"previous_batch_id,omitempty"` // 前一批次（用于连续性）
	NextBatchID     *uint `gorm:"column:next_batch_id;index" json:"next_batch_id,omitempty"`         // 下一批次（预留）
	CanRollNext     bool  `gorm:"column:can_roll_next;default:false" json:"can_roll_next"`           // 是否可以滚动生成下一批

	// Generation lock
	Generation uint      `gorm:"column:generation;default:0" json:"generation"` // 生成版本，防止旧任务回写
	Version    int       `gorm:"default:1" json:"version"`
	Error      string    `gorm:"type:text" json:"error"`
	CreatedAt  time.Time `json:"created_at"`
	UpdatedAt  time.Time `json:"updated_at"`

	// Associations
	PreviousBatch *PlanningBatch `gorm:"foreignKey:PreviousBatchID" json:"previous_batch,omitempty"`
	NextBatch     *PlanningBatch `gorm:"foreignKey:NextBatchID" json:"next_batch,omitempty"`
}

// BatchEpisode represents the association between a PlanningBatch and EpisodeAdaptation.
type BatchEpisode struct {
	ID             uint       `gorm:"primaryKey" json:"id"`
	BatchID        uint       `gorm:"column:batch_id;uniqueIndex:idx_batch_episode_unique" json:"batch_id"`
	EpisodeAdaptID uint       `gorm:"column:episode_adapt_id;uniqueIndex:idx_batch_episode_unique" json:"episode_adapt_id"`
	EpisodeN       int        `gorm:"column:episode_n;index" json:"episode_n"` // 集号
	BatchOrder     int        `gorm:"column:batch_order" json:"batch_order"`   // 批次内顺序
	Status         string     `gorm:"default:draft;index" json:"status"`       // draft/approved/produced
	ReviewNotes    string     `gorm:"column:review_notes;type:text" json:"review_notes"`
	ReviewerID     *uint      `gorm:"column:reviewer_id" json:"reviewer_id,omitempty"`
	ReviewedAt     *time.Time `gorm:"column:reviewed_at" json:"reviewed_at,omitempty"`
	CreatedAt      time.Time  `json:"created_at"`
	UpdatedAt      time.Time  `json:"updated_at"`

	// Associations
	Batch        *PlanningBatch     `gorm:"foreignKey:BatchID" json:"batch,omitempty"`
	EpisodeAdapt *EpisodeAdaptation `gorm:"foreignKey:EpisodeAdaptID" json:"episode_adapt,omitempty"`
}

// BatchStateSnapshot is the reviewed end-of-batch world/character/clue state injected into the next batch.
type BatchStateSnapshot struct {
	ID                  uint       `gorm:"primaryKey" json:"id"`
	ProjectID           uint       `gorm:"column:project_id;index" json:"project_id"`
	BatchID             uint       `gorm:"column:batch_id;uniqueIndex" json:"batch_id"`
	PreviousSnapshotID  *uint      `gorm:"column:previous_snapshot_id;index" json:"previous_snapshot_id,omitempty"`
	CharacterStatesJSON string     `gorm:"column:character_states_json;type:text" json:"character_states_json"`
	RelationshipsJSON   string     `gorm:"column:relationships_json;type:text" json:"relationships_json"`
	WorldStateJSON      string     `gorm:"column:world_state_json;type:text" json:"world_state_json"`
	ClueStateJSON       string     `gorm:"column:clue_state_json;type:text" json:"clue_state_json"`
	ReviewJSON          string     `gorm:"column:review_json;type:text" json:"review_json"`
	Status              string     `gorm:"default:draft;index" json:"status"`
	Version             int        `gorm:"default:1" json:"version"`
	ApprovedAt          *time.Time `gorm:"column:approved_at" json:"approved_at,omitempty"`
	CreatedAt           time.Time  `json:"created_at"`
	UpdatedAt           time.Time  `json:"updated_at"`
}

// StoryClue is the queryable current clue lifecycle; approved snapshots preserve historical states.
type StoryClue struct {
	ID                 uint      `gorm:"primaryKey" json:"id"`
	ProjectID          uint      `gorm:"column:project_id;uniqueIndex:idx_story_clue_project_key" json:"project_id"`
	ClueKey            string    `gorm:"column:clue_key;uniqueIndex:idx_story_clue_project_key" json:"clue_key"`
	Title              string    `json:"title"`
	Description        string    `gorm:"type:text" json:"description"`
	Status             string    `gorm:"default:open;index" json:"status"`
	OpenedEpisode      int       `gorm:"column:opened_episode" json:"opened_episode"`
	LastTouchedEpisode int       `gorm:"column:last_touched_episode" json:"last_touched_episode"`
	ResolvedEpisode    int       `gorm:"column:resolved_episode" json:"resolved_episode"`
	Version            int       `gorm:"default:1" json:"version"`
	CreatedAt          time.Time `json:"created_at"`
	UpdatedAt          time.Time `json:"updated_at"`
}

// AdaptationStrategy stores the user-approved constraints used to create episode mappings.
type AdaptationStrategy struct {
	ID                  uint      `gorm:"primaryKey" json:"id"`
	ProjectID           uint      `gorm:"column:project_id;uniqueIndex" json:"project_id"`
	Mode                string    `gorm:"default:cinematic" json:"mode"`
	ChapterStart        int       `gorm:"column:chapter_start" json:"chapter_start"`
	ChapterEnd          int       `gorm:"column:chapter_end" json:"chapter_end"`
	TargetEpisodes      int       `gorm:"column:target_episodes" json:"target_episodes"`
	TargetDuration      float64   `gorm:"column:target_duration;default:180" json:"target_duration"`
	TargetScenes        int       `gorm:"column:target_scenes;default:25" json:"target_scenes"`
	MustKeepJSON        string    `gorm:"column:must_keep_json;type:text" json:"must_keep_json"`
	DroppableJSON       string    `gorm:"column:droppable_json;type:text" json:"droppable_json"`
	AllowCharacterMerge bool      `gorm:"column:allow_character_merge" json:"allow_character_merge"`
	AllowEndingChange   bool      `gorm:"column:allow_ending_change" json:"allow_ending_change"`
	PlatformRules       string    `gorm:"column:platform_rules;type:text" json:"platform_rules"`
	Status              string    `gorm:"default:draft;index" json:"status"`
	Version             int       `gorm:"default:1" json:"version"`
	CreatedAt           time.Time `json:"created_at"`
	UpdatedAt           time.Time `json:"updated_at"`
}

// EpisodeAdaptation maps source chapters and events to one production episode.
type EpisodeAdaptation struct {
	ID               uint      `gorm:"primaryKey" json:"id"`
	ProjectID        uint      `gorm:"column:project_id;uniqueIndex:idx_adaptation_project_episode" json:"project_id"`
	EpisodeN         int       `gorm:"column:episode_n;uniqueIndex:idx_adaptation_project_episode" json:"episode_n"`
	BatchID          *uint     `gorm:"column:batch_id;index" json:"batch_id,omitempty"` // 所属批次（滚动批次模式）
	Title            string    `json:"title"`
	ChapterStart     int       `gorm:"column:chapter_start" json:"chapter_start"`
	ChapterEnd       int       `gorm:"column:chapter_end" json:"chapter_end"`
	SourceChapterIDs string    `gorm:"column:source_chapter_ids;type:text" json:"source_chapter_ids"`
	TargetDuration   float64   `gorm:"column:target_duration;default:180" json:"target_duration"`
	TargetScenes     int       `gorm:"column:target_scenes;default:25" json:"target_scenes"`
	AdaptationGoal   string    `gorm:"column:adaptation_goal;type:text" json:"adaptation_goal"`
	MustKeepEvents   string    `gorm:"column:must_keep_events;type:text" json:"must_keep_events"`
	OptionalEvents   string    `gorm:"column:optional_events;type:text" json:"optional_events"`
	OmittedEvents    string    `gorm:"column:omitted_events;type:text" json:"omitted_events"`
	OpeningState     string    `gorm:"column:opening_state;type:text" json:"opening_state"`
	EndingState      string    `gorm:"column:ending_state;type:text" json:"ending_state"`
	Hook             string    `gorm:"type:text" json:"hook"`
	SourceDigest     string    `gorm:"column:source_digest;size:64;index" json:"source_digest"`
	SourceTracesJSON string    `gorm:"column:source_traces_json;type:text" json:"source_traces_json"`
	Script           string    `gorm:"type:text" json:"script"`
	ReviewJSON       string    `gorm:"column:review_json;type:text" json:"review_json"`
	ContinuityStatus string    `gorm:"column:continuity_status;default:pending;index" json:"continuity_status"`
	OverrideReason   string    `gorm:"column:override_reason;type:text" json:"override_reason"`
	Status           string    `gorm:"default:draft;index" json:"status"`
	Version          int       `gorm:"default:1" json:"version"`
	CreatedAt        time.Time `json:"created_at"`
	UpdatedAt        time.Time `json:"updated_at"`
}

// CharacterAliasCandidate never mutates Character records until explicitly confirmed.
type CharacterAliasCandidate struct {
	ID            uint      `gorm:"primaryKey" json:"id"`
	ProjectID     uint      `gorm:"column:project_id;index;uniqueIndex:idx_alias_project_alias" json:"project_id"`
	CanonicalName string    `gorm:"column:canonical_name;index" json:"canonical_name"`
	Alias         string    `gorm:"uniqueIndex:idx_alias_project_alias" json:"alias"`
	ChapterID     uint      `gorm:"column:chapter_id;index" json:"chapter_id"`
	Status        string    `gorm:"default:pending;index" json:"status"`
	CreatedAt     time.Time `json:"created_at"`
	UpdatedAt     time.Time `json:"updated_at"`
}

// NovelJob persists resumable Phase B work and its locked Skill version.
type NovelJob struct {
	ID           uint       `gorm:"primaryKey" json:"id"`
	ProjectID    uint       `gorm:"column:project_id;index" json:"project_id"`
	Type         string     `gorm:"index" json:"type"`
	ScopeStart   int        `gorm:"column:scope_start" json:"scope_start"`
	ScopeEnd     int        `gorm:"column:scope_end" json:"scope_end"`
	Status       string     `gorm:"index" json:"status"`
	Progress     float64    `json:"progress"`
	Total        int        `json:"total"`
	Completed    int        `json:"completed"`
	SkillID      *uint      `gorm:"column:skill_id" json:"skill_id"`
	SkillVersion int        `gorm:"column:skill_version" json:"skill_version"`
	Token        string     `gorm:"size:64" json:"-"`
	Error        string     `gorm:"type:text" json:"error"`
	CreatedAt    time.Time  `json:"created_at"`
	UpdatedAt    time.Time  `json:"updated_at"`
	FinishedAt   *time.Time `json:"finished_at"`
}

type SkillAuditLog struct {
	ID           uint      `gorm:"primaryKey" json:"id"`
	ProjectID    uint      `gorm:"column:project_id;index" json:"project_id"`
	Stage        string    `gorm:"column:stage;index" json:"stage"`             // 阶段
	SkillID      uint      `gorm:"column:skill_id" json:"skill_id"`             // 使用的技能 ID
	SkillName    string    `gorm:"column:skill_name" json:"skill_name"`         // 技能名称快照
	SkillVersion int       `gorm:"column:skill_version" json:"skill_version"`   // 使用的技能版本
	SkillCode    string    `gorm:"column:skill_code" json:"skill_code"`         // 技能代码快照
	InputHash    string    `gorm:"column:input_hash;size:64" json:"input_hash"` // 输入内容哈希（用于复现）
	OutputLength int       `gorm:"column:output_length" json:"output_length"`   // 输出长度
	DurationMS   int64     `gorm:"column:duration_ms" json:"duration_ms"`       // 生成耗时（毫秒）
	Success      bool      `gorm:"default:true" json:"success"`                 // 是否成功
	Error        string    `gorm:"type:text" json:"error"`                      // 错误信息（如有）
	CreatedAt    time.Time `json:"created_at"`
}

type ShotActType string
type ShotTransitionType string

const (
	ShotActSetup      ShotActType = "setup"
	ShotActRising     ShotActType = "rising"
	ShotActMidpoint   ShotActType = "midpoint"
	ShotActFalling    ShotActType = "falling"
	ShotActResolution ShotActType = "resolution"

	ShotTransitionCut      ShotTransitionType = "cut"
	ShotTransitionDissolve ShotTransitionType = "dissolve"
	ShotTransitionFade     ShotTransitionType = "fade"
	ShotTransitionWipe     ShotTransitionType = "wipe"
	ShotTransitionMatchCut ShotTransitionType = "match_cut"
)

// ShotDialogueRange binds a Shot to an exact rune range of canonical structured Dialogue.
// Dialogue remains authoritative; the range only records performance continuity across cuts.
type ShotDialogueRange struct {
	DialogueID uint   `json:"dialogue_id"`
	GroupKey   string `json:"group_key"`
	StartRune  int    `json:"start_rune"`
	EndRune    int    `json:"end_rune"`
}

// Shot 是 Scene 下的导演镜头层。ActType 表示五幕叙事位置，五段提示词描述单镜画面。
type Shot struct {
	ID                    uint                `gorm:"primaryKey" json:"id"`
	SceneID               uint                `gorm:"column:scene_id;index;uniqueIndex:idx_shot_scene_order" json:"scene_id"`
	Order                 int                 `gorm:"column:order_num;uniqueIndex:idx_shot_scene_order" json:"order"`
	ActType               ShotActType         `gorm:"column:act_type;index" json:"act_type"`
	ShotType              string              `gorm:"column:shot_type" json:"shot_type"`
	CameraAngle           string              `gorm:"column:camera_angle" json:"camera_angle"`
	CameraMovement        string              `gorm:"column:camera_movement" json:"camera_movement"`
	TransitionType        ShotTransitionType  `gorm:"column:transition_type;index" json:"transition_type"`
	TransitionNote        string              `gorm:"column:transition_note;type:text" json:"transition_note"`
	StartState            string              `gorm:"column:start_state;type:text" json:"start_state"`
	EndState              string              `gorm:"column:end_state;type:text" json:"end_state"`
	Duration              float64             `gorm:"default:1.5" json:"duration"`
	Description           string              `gorm:"type:text" json:"description"`
	Dialogue              string              `gorm:"type:text" json:"dialogue"` // compatibility display cache; structured Dialogue remains authoritative
	DialogueRanges        []ShotDialogueRange `gorm:"column:dialogue_ranges_json;type:text;serializer:json" json:"dialogue_ranges"`
	ContinuesFromPrevious bool                `gorm:"-" json:"continues_from_previous"`
	ContinuesToNext       bool                `gorm:"-" json:"continues_to_next"`
	Emotion               string              `gorm:"type:text" json:"emotion"`
	PromptSubject         string              `gorm:"column:prompt_subject;type:text" json:"prompt_subject"`
	PromptAction          string              `gorm:"column:prompt_action;type:text" json:"prompt_action"`
	PromptCamera          string              `gorm:"column:prompt_camera;type:text" json:"prompt_camera"`
	PromptLighting        string              `gorm:"column:prompt_lighting;type:text" json:"prompt_lighting"`
	PromptStyle           string              `gorm:"column:prompt_style;type:text" json:"prompt_style"`
	NegativePrompt        string              `gorm:"column:negative_prompt;type:text" json:"negative_prompt"`
	CreatedAt             time.Time           `json:"created_at"`
	UpdatedAt             time.Time           `json:"updated_at"`
}

// PromptVersion 提示词版本历史
type PromptVersion struct {
	ID         uint      `gorm:"primaryKey" json:"id"`
	ProjectID  uint      `gorm:"column:project_id;index" json:"project_id"`
	EntityType string    `gorm:"column:entity_type;index" json:"entity_type"`   // scene/shot/skill
	EntityID   uint      `gorm:"column:entity_id;index" json:"entity_id"`       // 关联实体 ID
	Content    string    `gorm:"column:content;type:text" json:"content"`       // 版本内容
	Action     string    `gorm:"column:action" json:"action"`                   // build/optimize/translate/manual/rollback
	State      string    `gorm:"column:state;default:draft;index" json:"state"` // draft/applied
	Metadata   string    `gorm:"column:metadata;type:text" json:"metadata"`     // JSON: model_used, duration, tokens
	CreatedAt  time.Time `json:"created_at"`
}

// StylePresetCategory 风格预设分类
type StylePresetCategory string

const (
	StylePresetCinematic  StylePresetCategory = "cinematic"  // 电影感
	StylePresetAnimated   StylePresetCategory = "animated"   // 动画风
	StylePresetRealistic  StylePresetCategory = "realistic"  // 写实风
	StylePresetArtistic   StylePresetCategory = "artistic"   // 艺术风
	StylePresetCommercial StylePresetCategory = "commercial" // 商业风
)

// StylePreset 风格预设：提供可复用的提示词片段与推荐理由
type StylePreset struct {
	ID             uint                `gorm:"primaryKey" json:"id"`
	Name           string              `gorm:"column:name" json:"name"`                                   // 预设名称
	Category       StylePresetCategory `gorm:"column:category" json:"category"`                           // 分类
	Subcategory    string              `gorm:"column:subcategory" json:"subcategory"`                     // 子分类
	PromptTail     string              `gorm:"column:prompt_tail;type:text" json:"prompt_tail"`           // 追加提示词
	NegativeTail   string              `gorm:"column:negative_tail;type:text" json:"negative_tail"`       // 负面提示词
	Reason         string              `gorm:"column:reason;type:text" json:"reason"`                     // 推荐理由
	UseCases       string              `gorm:"column:use_cases;type:text" json:"use_cases"`               // 适用场景
	SceneTypes     string              `gorm:"column:scene_types;type:text" json:"scene_types"`           // 适用镜头类型
	Parameters     string              `gorm:"column:parameters;type:text" json:"parameters"`             // JSON: temperature, guidance_scale
	PreviewURL     string              `gorm:"column:preview_url" json:"preview_url"`                     // 预览图
	Tags           string              `gorm:"column:tags" json:"tags"`                                   // 逗号分隔标签
	IsRecommended  bool                `gorm:"column:is_recommended;default:false" json:"is_recommended"` // 是否推荐
	RecommendedFor string              `gorm:"column:recommended_for;type:text" json:"recommended_for"`   // 推荐用于
	UsageCount     int                 `gorm:"column:usage_count;default:0" json:"usage_count"`           // 使用次数
	IsSystem       bool                `gorm:"column:is_system;default:false" json:"is_system"`           // 是否系统预设
	CreatedAt      time.Time           `json:"created_at"`
	UpdatedAt      time.Time           `json:"updated_at"`
}

// CharacterLookStatus 角色造型审核状态
type CharacterLookStatus string

const (
	LookStatusDraft     CharacterLookStatus = "draft"     // 草稿（AI 生成，待审核）
	LookStatusApproved  CharacterLookStatus = "approved"  // 已审核通过
	LookStatusRejected  CharacterLookStatus = "rejected"  // 已驳回，需修改
	LookStatusPublished CharacterLookStatus = "published" // 已发布，可用于生成
)

// CharacterLook 角色造型资产：项目内可复用的角色外观变体（服装/发型/配饰等）
// 独立于 Character.profile，支持按场景/镜头选择的造型变体，覆盖服装、鞋履、发型、发饰、首饰、包等
type CharacterLook struct {
	ID            uint                `gorm:"primaryKey" json:"id"`
	ProjectID     uint                `gorm:"column:project_id;index;uniqueIndex:idx_look_project_character_name" json:"project_id"`
	CharacterID   uint                `gorm:"column:character_id;index;uniqueIndex:idx_look_project_character_name" json:"character_id"`
	Name          string              `gorm:"uniqueIndex:idx_look_project_character_name" json:"name"`     // 项目内同名角色下唯一
	Category      string              `json:"category"`                                                    // 造型分类：clothing(服装)/shoes(鞋履)/hair(发型)/hair_accessory(发饰)/jewelry(首饰)/bag(包)/full(完整造型)
	Description   string              `gorm:"type:text" json:"description"`                                // 造型详细描述（AI 扩写或手动编辑）
	Image         string              `json:"image"`                                                       // 参考图文件名（input/<project_id>/ 下）
	ImageTaskID   string              `gorm:"column:image_task_id;index" json:"image_task_id"`             // Krea2 参考图生成任务
	ImageError    string              `gorm:"column:image_error;type:text" json:"image_error"`             // 参考图生成错误
	Prompt        string              `gorm:"type:text" json:"prompt"`                                     // 参考图提示词
	Priority      int                 `gorm:"default:0" json:"priority"`                                   // 优先级（数字越大优先级越高，用于 MiniMax H3 排序）
	IsShotRelated bool                `gorm:"column:is_shot_related;default:false" json:"is_shot_related"` // 是否镜头相关（MiniMax H3 仅纳入为 true 的造型）
	IsDefault     bool                `gorm:"column:is_default;default:false;index" json:"is_default"`     // 角色标准像阶段自动建立的默认造型
	AuditStatus   CharacterLookStatus `gorm:"column:audit_status;default:draft" json:"audit_status"`       // 审核状态
	AuditNote     string              `gorm:"column:audit_note;type:text" json:"audit_note"`               // 审核意见
	AuditVersion  int                 `gorm:"column:audit_version;default:0" json:"audit_version"`         // 审核版本号
	Source        string              `json:"source"`                                                      // auto(AI 扩写)/manual(手动新建)
	Version       int                 `gorm:"default:1" json:"version"`                                    // 版本号
	CreatedAt     time.Time           `json:"created_at"`
	UpdatedAt     time.Time           `json:"updated_at"`

	// 关联
	Character *Character `gorm:"foreignKey:CharacterID" json:"character,omitempty"`
}

// SceneCharacterLook 场景-造型关联表：建立 Scene 与 CharacterLook 的多对多关系
type SceneCharacterLook struct {
	ID         uint      `gorm:"primaryKey" json:"id"`
	SceneID    uint      `gorm:"column:scene_id;index;uniqueIndex:idx_scl_scene_look" json:"scene_id"`
	LookID     uint      `gorm:"column:look_id;index;uniqueIndex:idx_scl_scene_look" json:"look_id"`
	Order      int       `gorm:"default:0" json:"order"`                              // 场景内造型顺序
	IsFeatured bool      `gorm:"column:is_featured;default:false" json:"is_featured"` // 是否为主推造型（画面重点）
	CreatedAt  time.Time `json:"created_at"`

	// 关联
	Scene *Scene         `gorm:"foreignKey:SceneID" json:"scene,omitempty"`
	Look  *CharacterLook `gorm:"foreignKey:LookID" json:"look,omitempty"`
}

// ShotCharacterLook 镜头-造型关联表：建立 Shot 与 CharacterLook 的多对多关系
type ShotCharacterLook struct {
	ID         uint      `gorm:"primaryKey" json:"id"`
	ShotID     uint      `gorm:"column:shot_id;index;uniqueIndex:idx_shcl_shot_look" json:"shot_id"`
	LookID     uint      `gorm:"column:look_id;index;uniqueIndex:idx_shcl_shot_look" json:"look_id"`
	Order      int       `gorm:"default:0" json:"order"`                              // 镜头内造型顺序
	IsFeatured bool      `gorm:"column:is_featured;default:false" json:"is_featured"` // 是否为主推造型
	CreatedAt  time.Time `json:"created_at"`

	// 关联
	Shot *Shot          `gorm:"foreignKey:ShotID" json:"shot,omitempty"`
	Look *CharacterLook `gorm:"foreignKey:LookID" json:"look,omitempty"`
}

// ========== 视频分镜连续性 ==========

const (
	ContinuityModeIndependent = "independent"
	ContinuityModeContinue    = "continue_from_previous"
	ContinuityModeBridge      = "bridge_to_storyboard"
)

// FrameCandidateType 帧候选类型
type FrameCandidateType string

const (
	FrameCandidateCandidate  FrameCandidateType = "candidate"   // 待选帧（从视频提取）
	FrameCandidateSelected   FrameCandidateType = "selected"    // 已选帧（人工选择作为衔接帧）
	FrameCandidateVideoFirst FrameCandidateType = "video_first" // 成片实际首帧
	FrameCandidateVideoLast  FrameCandidateType = "video_last"  // 成片实际尾帧
)

// FrameCandidate 是某次视频任务末尾提取的候选衔接帧。
type FrameCandidate struct {
	ID                uint               `gorm:"primaryKey" json:"id"`
	ProjectID         uint               `gorm:"index" json:"project_id"`
	SceneID           uint               `gorm:"index;uniqueIndex:idx_scene_video_frame" json:"scene_id"`
	VideoTaskID       string             `gorm:"index;uniqueIndex:idx_scene_video_frame" json:"video_task_id"`
	Type              FrameCandidateType `gorm:"default:candidate" json:"type"`
	FrameIndex        int                `gorm:"uniqueIndex:idx_scene_video_frame" json:"frame_index"`
	TimestampMS       int64              `json:"timestamp_ms"`
	ImageFile         string             `json:"image_file"` // input/<projectID> 下的当前有效图片
	OriginalImageFile string             `json:"original_image_file,omitempty"`
	Source            string             `gorm:"default:extracted" json:"source"` // extracted/manual_upload
	SelectedAt        *time.Time         `json:"selected_at,omitempty"`
	CreatedAt         time.Time          `json:"created_at"`
}

// SceneContinuity 是当前 Scene 对上一 Scene 的显式依赖。
// GenerationCandidate preserves immutable Scene image/video takes and their reproducible inputs.
type GenerationCandidate struct {
	ID                uint       `gorm:"primaryKey" json:"id"`
	ProjectID         uint       `gorm:"index;uniqueIndex:idx_candidate_task_media" json:"project_id"`
	EntityType        string     `gorm:"index" json:"entity_type"`
	EntityID          uint       `gorm:"index" json:"entity_id"`
	MediaType         string     `gorm:"index;uniqueIndex:idx_candidate_task_media" json:"media_type"`
	TaskID            string     `gorm:"index;uniqueIndex:idx_candidate_task_media" json:"task_id"`
	File              string     `json:"file"`
	VideoInputFile    string     `gorm:"column:video_input_file" json:"video_input_file,omitempty"`
	VideoGPU          *int       `gorm:"column:video_gpu" json:"video_gpu,omitempty"`
	PromptSnapshot    string     `gorm:"type:text" json:"prompt_snapshot"`
	ReferencesJSON    string     `gorm:"type:text" json:"references_json"`
	ParamsJSON        string     `gorm:"type:text" json:"params_json"`
	ProvenanceJSON    string     `gorm:"type:text" json:"provenance_json"`
	ReviewStatus      string     `gorm:"default:pending;index" json:"review_status"`
	ReviewReason      string     `gorm:"type:text" json:"review_reason"`
	ParentCandidateID *uint      `gorm:"index" json:"parent_candidate_id,omitempty"`
	IsCurrent         bool       `gorm:"default:false;index" json:"is_current"`
	Stale             bool       `gorm:"default:false;index" json:"stale"`
	StaleReason       string     `json:"stale_reason"`
	ReviewedAt        *time.Time `json:"reviewed_at,omitempty"`
	CreatedAt         time.Time  `json:"created_at"`
	UpdatedAt         time.Time  `json:"updated_at"`
}

// AudioLayer stores non-dialogue soundscape/SFX/BGM independently from H3 native audio.
type AudioLayer struct {
	ID          uint      `gorm:"primaryKey" json:"id"`
	ProjectID   uint      `gorm:"index" json:"project_id"`
	EpisodeN    int       `gorm:"index" json:"episode_n"`
	SceneID     *uint     `gorm:"index" json:"scene_id,omitempty"`
	Kind        string    `gorm:"index" json:"kind"`
	Name        string    `json:"name"`
	File        string    `json:"file"`
	Description string    `gorm:"type:text" json:"description"`
	StartTime   float64   `json:"start_time"`
	EndTime     float64   `json:"end_time"`
	Volume      float64   `gorm:"default:1" json:"volume"`
	FadeIn      float64   `json:"fade_in"`
	FadeOut     float64   `json:"fade_out"`
	Loop        bool      `json:"loop"`
	Muted       bool      `json:"muted"`
	Status      string    `gorm:"default:draft;index" json:"status"`
	Stale       bool      `gorm:"default:false" json:"stale"`
	StaleReason string    `json:"stale_reason"`
	CreatedAt   time.Time `json:"created_at"`
	UpdatedAt   time.Time `json:"updated_at"`
}

// PromptPolicyOverride stores an inherited prompt policy at one project hierarchy scope.
type PromptPolicyOverride struct {
	ID        uint      `gorm:"primaryKey" json:"id"`
	ProjectID uint      `gorm:"index;uniqueIndex:idx_prompt_policy_scope" json:"project_id"`
	EpisodeID *uint     `gorm:"index;uniqueIndex:idx_prompt_policy_scope" json:"episode_id,omitempty"`
	SceneID   *uint     `gorm:"index;uniqueIndex:idx_prompt_policy_scope" json:"scene_id,omitempty"`
	ShotID    *uint     `gorm:"index;uniqueIndex:idx_prompt_policy_scope" json:"shot_id,omitempty"`
	PolicyKey string    `gorm:"size:100;index;uniqueIndex:idx_prompt_policy_scope" json:"policy_key"`
	Content   string    `gorm:"type:text" json:"content"`
	Version   int       `gorm:"default:1" json:"version"`
	CreatedAt time.Time `json:"created_at"`
	UpdatedAt time.Time `json:"updated_at"`
}

// CharacterMotionReference is a reusable, user-uploaded dynamic video reference.
// It remains separate from static H3 reference images and does not imply a generation provider.
type CharacterMotionReference struct {
	ID          uint      `gorm:"primaryKey" json:"id"`
	ProjectID   uint      `gorm:"index" json:"project_id"`
	CharacterID uint      `gorm:"index" json:"character_id"`
	Name        string    `json:"name"`
	VideoPath   string    `json:"video_path"`
	AudioPath   string    `json:"audio_path,omitempty"`
	Selected    bool      `gorm:"index" json:"selected"`
	CreatedAt   time.Time `json:"created_at"`
	UpdatedAt   time.Time `json:"updated_at"`
}

// SharedAssetReference makes global/project assets explicit and prevents deletion while referenced.
type SharedAssetReference struct {
	ID         uint      `gorm:"primaryKey" json:"id"`
	MaterialID uint      `gorm:"index;uniqueIndex:idx_shared_asset_ref" json:"material_id"`
	ProjectID  uint      `gorm:"index;uniqueIndex:idx_shared_asset_ref" json:"project_id"`
	SceneID    *uint     `gorm:"index;uniqueIndex:idx_shared_asset_ref" json:"scene_id,omitempty"`
	ShotID     *uint     `gorm:"index;uniqueIndex:idx_shared_asset_ref" json:"shot_id,omitempty"`
	Mode       string    `gorm:"default:live" json:"mode"`
	LocalFile  string    `json:"local_file,omitempty"`
	CreatedAt  time.Time `json:"created_at"`
}

type SceneContinuity struct {
	ID                uint            `gorm:"primaryKey" json:"id"`
	SceneID           uint            `gorm:"uniqueIndex" json:"scene_id"`
	Mode              string          `gorm:"default:independent;index" json:"mode"`
	SourceMode        string          `gorm:"default:auto_previous" json:"source_mode"`
	SourceSceneID     *uint           `gorm:"index" json:"source_scene_id,omitempty"`
	SelectedFrameID   *uint           `gorm:"index" json:"selected_frame_id,omitempty"`
	SourceVideoTaskID string          `gorm:"index" json:"source_video_task_id"`
	Status            string          `gorm:"default:not_required;index" json:"status"`
	Error             string          `gorm:"type:text" json:"error"`
	Version           int             `gorm:"default:1" json:"version"`
	AnalysisStatus    string          `gorm:"default:not_started" json:"analysis_status"`
	AnalysisJSON      string          `gorm:"type:text" json:"analysis_json,omitempty"`
	AnalysisModel     string          `json:"analysis_model,omitempty"`
	AnalysisVersion   int             `gorm:"default:0" json:"analysis_version"`
	SelectedFrame     *FrameCandidate `gorm:"foreignKey:SelectedFrameID" json:"selected_frame,omitempty"`
	CreatedAt         time.Time       `json:"created_at"`
	UpdatedAt         time.Time       `json:"updated_at"`
}
