package api

import (
	"strings"

	"github.com/gin-gonic/gin"

	"comfyui-console/internal/config"
	"comfyui-console/internal/service"
	"comfyui-console/internal/static"
)

type Router struct {
	cfg *config.Config
	svc *service.Service
}

func NewRouter(cfg *config.Config, svc *service.Service) *gin.Engine {
	r := gin.New()
	r.Use(gin.Logger(), gin.Recovery())

	r.GET("/api/health", func(c *gin.Context) { c.JSON(200, gin.H{"ok": true}) })
	r.GET("/api/release", svc.HandleGetRelease)

	// 实例管理
	r.GET("/api/instances", svc.HandleListInstances)
	r.POST("/api/instances/:id/start", svc.HandleInstanceStart)
	r.POST("/api/instances/:id/stop", svc.HandleInstanceStop)
	r.POST("/api/instances/:id/restart", svc.HandleInstanceRestart)
	r.POST("/api/instances/start-all", svc.HandleStartAll)
	r.POST("/api/instances/stop-all", svc.HandleStopAll)
	r.POST("/api/instances/restart-all", svc.HandleRestartAll)

	// GPU 监控
	r.GET("/api/gpus", svc.HandleListGPUs)

	// 模板
	r.GET("/api/templates", svc.HandleListTemplates)

	// 任务
	r.GET("/api/tasks", svc.HandleListTasks)
	r.DELETE("/api/tasks", svc.HandleClearTasks)
	r.GET("/api/tasks/:id", svc.HandleGetTask)
	r.POST("/api/tasks", svc.HandleCreateTask)
	r.POST("/api/tasks/:id/cancel", svc.HandleCancelTask)
	r.POST("/api/tasks/:id/requeue", svc.HandleRequeueTask)
	r.POST("/api/tasks/cancel-all", svc.HandleCancelAllTasks)
	r.POST("/api/tasks/:id/rerun", svc.HandleRerunTask)

	// 文件
	r.POST("/api/upload", svc.HandleUpload)
	r.GET("/api/output/:gpu/*path", svc.HandleOutputFile)
	r.GET("/api/media/:gpu/*path", svc.HandleMediaInfo)

	// 实时推送
	r.GET("/api/ws", svc.HandleWS)

	// 平台设置（火山引擎）
	r.GET("/api/settings", svc.HandleGetSettings)
	r.PUT("/api/settings", svc.HandleUpdateSettings)
	r.POST("/api/settings/test-text", svc.HandleTestText)
	r.POST("/api/settings/test-image", svc.HandleTestImage)
	r.POST("/api/settings/test-tts", svc.HandleTestTTS)

	// 漫剧项目
	r.GET("/api/projects", svc.HandleListProjects)
	r.POST("/api/projects", svc.HandleCreateProject)
	r.GET("/api/projects/:id", svc.HandleGetProject)
	r.PUT("/api/projects/:id", svc.HandleUpdateProject)
	r.DELETE("/api/projects/:id", svc.HandleDeleteProject)
	r.POST("/api/projects/:id/script", svc.HandleGenerateScript)
	r.POST("/api/projects/:id/script/render", svc.HandleRenderScriptFromText)
	r.POST("/api/projects/:id/script/expand", svc.HandleExpandScript)
	r.POST("/api/projects/:id/plan", svc.HandleGeneratePlan)
	r.PUT("/api/projects/:id/plan/episodes", svc.HandleUpdatePlanEpisodes)
	r.POST("/api/projects/:id/generate", svc.HandleGenerateProject)
	r.POST("/api/projects/:id/images", svc.HandleGenerateAllImages)
	r.POST("/api/projects/:id/videos", svc.HandleGenerateAllVideos)
	r.PATCH("/api/projects/:id/scenes/:sid", svc.HandleUpdateScene)
	r.POST("/api/projects/:id/scenes/:sid/image", svc.HandleGenerateSceneImage)
	r.POST("/api/projects/:id/scenes/:sid/video", svc.HandleGenerateSceneVideo)
	r.POST("/api/projects/:id/scenes/:sid/video/cancel", svc.HandleCancelSceneVideo)
	r.POST("/api/projects/:id/merge", svc.HandleCreateMerge)
	r.POST("/api/projects/:id/merge-all", svc.HandleCreateAllMerges)
	r.GET("/api/projects/:id/merges", svc.HandleListMerges)
	// 对白配音与字幕
	r.GET("/api/projects/:id/scenes/:sid/dialogues", svc.HandleListSceneDialogues)
	r.POST("/api/projects/:id/scenes/:sid/dub", svc.HandleGenerateSceneDub)
	r.POST("/api/projects/:id/dub", svc.HandleGenerateProjectDub)
	r.GET("/api/projects/:id/srt", svc.HandleEpisodeSRT)
	// 剪辑台
	r.GET("/api/projects/:id/editor", svc.HandleEditorData)
	r.PUT("/api/projects/:id/dialogues/:did", svc.HandleUpdateDialogue)
	r.POST("/api/projects/:id/dialogues/:did/dub", svc.HandleRedubDialogue)
	r.PUT("/api/projects/:id/editor/order", svc.HandleReorderScenes)
	r.PATCH("/api/projects/:id/scenes/:sid/duration", svc.HandleUpdateSceneDuration)
	// 角色资产
	r.GET("/api/projects/:id/characters", svc.HandleListCharacters)
	r.POST("/api/projects/:id/characters", svc.HandleCreateCharacter)
	r.POST("/api/projects/:id/characters/portraits", svc.HandleGenerateAllPortraits)
	r.PUT("/api/projects/:id/characters/:cid", svc.HandleUpdateCharacter)
	r.DELETE("/api/projects/:id/characters/:cid", svc.HandleDeleteCharacter)
	r.POST("/api/projects/:id/characters/:cid/portrait", svc.HandleGenerateCharacterPortrait)
	r.POST("/api/projects/:id/characters/:cid/portrait/upload", svc.HandleUploadCharacterPortrait)
	r.POST("/api/projects/:id/characters/:cid/sheet", svc.HandleGenerateCharacterSheet)

	// 角色档案（LumxAI 风格结构化角色提示词 + 审核工作流）
	r.POST("/api/projects/:id/characters/:cid/profile", svc.HandleGenerateCharacterProfile)                // LLM 生成完整角色档案
	r.POST("/api/projects/:id/characters/:cid/profile/generate-prompt", svc.HandleGenerateReferencePrompt) // 单独生成参考像提示词
	r.PUT("/api/projects/:id/characters/:cid/profile", svc.HandleUpdateCharacterProfile)                   // 手动更新角色档案字段
	r.POST("/api/projects/:id/characters/:cid/profile/approve", svc.HandleApproveCharacterProfile)         // 审核通过
	r.POST("/api/projects/:id/characters/:cid/profile/reject", svc.HandleRejectCharacterProfile)           // 审核驳回
	r.POST("/api/projects/:id/characters/:cid/profile/reset", svc.HandleResetCharacterProfile)             // 重置为草稿
	// 角色语音（预设音色 / 参考语音复刻）
	r.POST("/api/projects/:id/characters/:cid/voice/upload", svc.HandleUploadCharacterVoice)
	r.POST("/api/projects/:id/characters/:cid/voice/clone", svc.HandleCloneCharacterVoice)
	r.POST("/api/projects/:id/characters/:cid/voice/clear", svc.HandleClearCharacterVoice)
	// 视觉资产（道具 / 场景，跨分镜一致性参考图）
	r.GET("/api/projects/:id/assets/:kind", svc.HandleListAssets)
	r.POST("/api/projects/:id/assets/:kind", svc.HandleCreateAsset)
	r.POST("/api/projects/:id/assets/:kind/images", svc.HandleGenerateAllAssetImages)
	r.PUT("/api/projects/:id/assets/:kind/:aid", svc.HandleUpdateAsset)
	r.DELETE("/api/projects/:id/assets/:kind/:aid", svc.HandleDeleteAsset)
	r.POST("/api/projects/:id/assets/:kind/:aid/image", svc.HandleGenerateAssetImage)
	r.POST("/api/projects/:id/assets/:kind/:aid/image/upload", svc.HandleUploadAssetImage)
	r.POST("/api/projects/:id/assets/:kind/:aid/sheet", svc.HandleGeneratePropSheet)
	r.GET("/api/input/:taskid/*path", svc.HandleInputFile)

	// 创作技能管理
	r.GET("/api/skills", svc.HandleListSkills)                        // 列出所有技能
	r.GET("/api/skills/stages", svc.HandleListSkillStages)            // 获取所有可用阶段
	r.GET("/api/skills/stats", svc.HandleSkillStats)                  // 技能统计
	r.GET("/api/skills/:id", svc.HandleGetSkill)                      // 获取单个技能
	r.POST("/api/skills", svc.HandleCreateSkill)                      // 创建自定义技能
	r.PUT("/api/skills/:id", svc.HandleUpdateSkill)                   // 更新技能
	r.DELETE("/api/skills/:id", svc.HandleDeleteSkill)                // 删除自定义技能
	r.POST("/api/skills/:id/upgrade", svc.HandleUpgradeSkill)         // 升级系统技能
	r.GET("/api/skills/history/:code", svc.HandleSkillVersionHistory) // 获取版本历史
	r.POST("/api/skills/:id/preview", svc.HandlePreviewSkillPrompt)   // 预览提示词装配

	// 项目级技能配置
	r.GET("/api/projects/:id/skills", svc.HandleGetProjectSkills)            // 获取项目技能配置
	r.POST("/api/projects/:id/skills", svc.HandleSetProjectSkill)            // 设置项目技能
	r.DELETE("/api/projects/:id/skills/:stage", svc.HandleResetProjectSkill) // 重置项目技能
	r.GET("/api/projects/:id/skills/effective", svc.HandleGetEffectiveSkill) // 获取有效技能
	r.GET("/api/projects/:id/skills/audit", svc.HandleGetSkillAuditLogs)     // 获取审计日志

	// 素材库
	r.GET("/api/materials", svc.HandleListMaterials)
	r.POST("/api/materials", svc.HandleUploadMaterial)
	r.DELETE("/api/materials/:id", svc.HandleDeleteMaterial)

	// 小说改编项目（source_type=novel）
	r.POST("/api/projects/novel", svc.HandleCreateNovelProject)                    // 创建小说改编项目
	r.POST("/api/projects/:id/novel/upload", svc.HandleUploadNovel)                // 上传小说文件（TXT/MD）
	r.GET("/api/projects/:id/novel/import-status", svc.HandleGetNovelImportStatus) // 获取导入状态
	r.GET("/api/projects/:id/novel/chapters", svc.HandleListChapters)              // 章节列表
	r.GET("/api/projects/:id/novel/chapters/:cid", svc.HandleGetChapter)           // 章节详情
	r.PUT("/api/projects/:id/novel/chapters/:cid", svc.HandleUpdateChapter)        // 更新章节（标题/顺序）
	r.PUT("/api/projects/:id/novel/chapters/reorder", svc.HandleReorderChapters)   // 批量更新章节顺序
	r.POST("/api/projects/:id/novel/chapters/:cid/split", svc.HandleSplitChapter)  // 拆分章节
	r.POST("/api/projects/:id/novel/chapters/merge", svc.HandleMergeChapters)      // 合并章节
	r.GET("/api/projects/:id/novel/tasks", svc.HandleListChapterTasks)             // 章节任务列表
	r.POST("/api/projects/:id/novel/chapters/analyze", svc.HandleAnalyzeNovelChapters)
	r.POST("/api/projects/:id/novel/chapters/:cid/retry", svc.HandleRetryNovelChapter)
	r.GET("/api/projects/:id/novel/arcs", svc.HandleListNovelArcs)
	r.POST("/api/projects/:id/novel/arcs/generate", svc.HandleGenerateNovelArcs)
	r.GET("/api/projects/:id/novel/aliases", svc.HandleListNovelAliases)
	r.PUT("/api/projects/:id/novel/aliases/:aid", svc.HandleUpdateNovelAlias)
	r.GET("/api/projects/:id/story-bible", svc.HandleGetStoryBible)
	r.POST("/api/projects/:id/story-bible/generate", svc.HandleGenerateStoryBible)
	r.PUT("/api/projects/:id/story-bible", svc.HandleUpdateStoryBible)
	r.POST("/api/projects/:id/story-bible/approve", svc.HandleApproveStoryBible)
	r.GET("/api/projects/:id/novel/jobs", svc.HandleListNovelJobs)
	r.POST("/api/projects/:id/novel/jobs/:jid/retry", svc.HandleRetryNovelJob)
	r.POST("/api/projects/:id/novel/jobs/:jid/cancel", svc.HandleCancelNovelJob)
	r.GET("/api/projects/:id/adaptation-strategy", svc.HandleGetAdaptationStrategy)
	r.PUT("/api/projects/:id/adaptation-strategy", svc.HandleSaveAdaptationStrategy)
	r.GET("/api/projects/:id/adaptations", svc.HandleListAdaptations)
	r.POST("/api/projects/:id/adaptations/generate", svc.HandleGenerateAdaptations)
	r.PUT("/api/projects/:id/adaptations/:episode", svc.HandleUpdateAdaptation)
	r.POST("/api/projects/:id/adaptations/:episode/approve", svc.HandleApproveAdaptations)
	r.POST("/api/projects/:id/adaptations/approve", svc.HandleApproveAdaptations)
	r.GET("/api/projects/:id/adaptations/:episode/context", svc.HandleAdaptationContext)
	r.POST("/api/projects/:id/adaptations/:episode/script", svc.HandleGenerateAdaptationScript)
	r.POST("/api/projects/:id/adaptations/scripts", svc.HandleBatchGenerateAdaptationScripts)
	r.POST("/api/projects/:id/adaptations/:episode/review", svc.HandleReviewAdaptation)
	r.POST("/api/projects/:id/adaptations/review", svc.HandleBatchReviewAdaptations)
	r.GET("/api/projects/:id/novel/usage", svc.HandleNovelUsage)
	r.PUT("/api/projects/:id/source-type", svc.HandleChangeProjectSourceType) // 转换项目来源类型

	// Scene 下的导演镜头层（手动结构化数据，不按字符比例切分）
	r.POST("/api/projects/:id/scenes/:sid/shots", svc.HandleCreateShots)
	r.GET("/api/projects/:id/scenes/:sid/shots", svc.HandleGetSceneShots)
	r.PUT("/api/projects/:id/shots/:shid", svc.HandleUpdateShot)
	r.DELETE("/api/projects/:id/shots/:shid", svc.HandleDeleteShot)

	// 提示词工作台
	r.POST("/api/prompts/build", svc.HandleBuildPrompt)         // 构建提示词
	r.POST("/api/prompts/optimize", svc.HandleOptimizePrompt)   // 优化提示词
	r.POST("/api/prompts/translate", svc.HandleTranslatePrompt) // 翻译提示词
	r.GET("/api/prompts/history", svc.HandleGetPromptHistory)   // 提示词历史
	r.POST("/api/prompts/rollback", svc.HandleRollbackPrompt)   // 回滚提示词

	// 风格预设
	r.GET("/api/presets", svc.HandleListPresets)                    // 列出预设
	r.GET("/api/presets/categories", svc.HandleGetPresetCategories) // 获取分类
	r.GET("/api/presets/recommend", svc.HandleGetRecommendations)   // 获取推荐（查询参数）
	r.GET("/api/presets/:id", svc.HandleGetPreset)                  // 获取单个预设
	r.POST("/api/presets/:id/apply", svc.HandleApplyPreset)         // 应用预设
	r.POST("/api/presets", svc.HandleCreatePreset)                  // 创建自定义预设
	r.PUT("/api/presets/:id", svc.HandleUpdatePreset)               // 更新预设
	r.DELETE("/api/presets/:id", svc.HandleDeletePreset)            // 删除预设

	// 前端静态资源 (SPA)
	r.NoRoute(func(c *gin.Context) {
		if strings.HasPrefix(c.Request.URL.Path, "/api/") {
			c.JSON(404, gin.H{"error": "not found"})
			return
		}
		static.Handler().ServeHTTP(c.Writer, c.Request)
	})

	return r
}
