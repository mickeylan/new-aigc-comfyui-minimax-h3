import axios from 'axios'

const http = axios.create({
  baseURL: '/api',
  timeout: 120000
})

export const api = {
  instances: () => http.get('/instances'),
  startInstance: (id) => http.post(`/instances/${id}/start`),
  stopInstance: (id) => http.post(`/instances/${id}/stop`),
  restartInstance: (id) => http.post(`/instances/${id}/restart`),
  startAll: () => http.post('/instances/start-all'),
  stopAll: () => http.post('/instances/stop-all'),
  restartAll: () => http.post('/instances/restart-all'),
  gpus: () => http.get('/gpus'),
  templates: () => http.get('/templates'),
  modelCatalog: () => http.get('/model-catalog'),
  playgroundRuns: (params = {}) => http.get('/playground/runs', { params }),
  playgroundRun: (id) => http.get(`/playground/runs/${id}`),
  createPlaygroundRun: (data) => http.post('/playground/runs', data),
  starPlaygroundRun: (id, starred) => http.patch(`/playground/runs/${id}/star`, { starred }),
  promotePlaygroundRun: (id, resultIndex = 0) => http.post(`/playground/runs/${id}/promote`, { result_index: resultIndex }),
  tasks: (params) => http.get('/tasks', { params }),
  clearTasks: () => http.delete('/tasks'),
  task: (id) => http.get(`/tasks/${id}`),
  createTask: (data) => http.post('/tasks', data),
  cancelTask: (id) => http.post(`/tasks/${id}/cancel`),
  taskDiagnostics: (id) => http.get(`/tasks/${id}/diagnostics`),
  requeueTask: (id) => http.post(`/tasks/${id}/requeue`),
  cancelAllTasks: () => http.post('/tasks/cancel-all'),
  rerunTask: (id) => http.post(`/tasks/${id}/rerun`),
  upload: (file, type, taskId) => {
    const fd = new FormData()
    fd.append('file', file)
    fd.append('type', type)
    fd.append('task_id', taskId)
    return http.post('/upload', fd, {
      headers: { 'Content-Type': 'multipart/form-data' },
      timeout: 300000
    })
  },
  outputUrl: (gpu, path) => `/api/output/${gpu}/${path}`,
  mediaInfo: (gpu, path) => http.get(`/media/${gpu}/${path}`),
  task: (taskId) => http.get(`/tasks/${taskId}`),

  // 平台设置（火山引擎）
  settings: () => http.get('/settings'),
  saveSettings: (data) => http.put('/settings', data),
  testText: () => http.post('/settings/test-text'),
  testImage: () => http.post('/settings/test-image'),
  testTTS: () => http.post('/settings/test-tts'),

  // 漫剧项目
  projects: () => http.get('/projects'),
  createProject: (data) => http.post('/projects', data),
  project: (id) => http.get(`/projects/${id}`),
  projectEpisodes: (id) => http.get(`/projects/${id}/episodes`),
  createProjectEpisode: (id, data) => http.post(`/projects/${id}/episodes`, data),
  updateProjectEpisode: (id, number, data) => http.patch(`/projects/${id}/episodes/${number}`, data),
  deleteProjectEpisode: (id, number) => http.delete(`/projects/${id}/episodes/${number}`),
  episodeScreenplay: (id, number) => http.get(`/projects/${id}/episodes/${number}/screenplay`),
  previewScreenplayImport: (id, number, file, format = '') => {
    const fd = new FormData()
    fd.append('file', file)
    if (format) fd.append('format', format)
    return http.post(`/projects/${id}/episodes/${number}/screenplay/preview`, fd, { headers: { 'Content-Type': 'multipart/form-data' } })
  },
  applyScreenplayImport: (id, number, preview) => http.post(`/projects/${id}/episodes/${number}/screenplay/apply`, preview),
  screenplayExportUrl: (id, number, format) => `/api/projects/${id}/episodes/${number}/screenplay/export/${format}`,
  episodeContinuity: (id, number) => http.get(`/projects/${id}/episodes/${number}/continuity`),
  regenerateEpisodeContinuity: (id, number) => http.post(`/projects/${id}/episodes/${number}/continuity/regenerate`, {}, { timeout: 300000 }),
  reassignSceneEpisode: (id, sid, episodeN) => http.post(`/projects/${id}/scenes/${sid}/reassign-episode`, { episode_n: episodeN }),
  sceneDirectorDraft: (id, sid, data = {}) => http.post(`/projects/${id}/scenes/${sid}/shots/director-draft`, data, { timeout: 300000 }),
  visualBeatDraft: (id, sid, data = {}) => http.post(`/projects/${id}/scenes/${sid}/skills/visual-beats`, data, { timeout: 300000 }),
  faithfulPolishDraft: (id, sid, data) => http.post(`/projects/${id}/scenes/${sid}/skills/faithful-polish`, data, { timeout: 300000 }),
  assetContinuityReviewDraft: (id, sid, data = {}) => http.post(`/projects/${id}/scenes/${sid}/skills/asset-continuity-review`, data, { timeout: 300000 }),
  coverageReviewDraft: (id, sid) => http.post(`/projects/${id}/scenes/${sid}/skills/coverage-review`, {}, { timeout: 300000 }),
  creativeIntent: (id, data) => http.post(`/projects/${id}/skills/creative-intent`, data, { timeout: 300000 }),
  promptPolicyOverrides: (id, policyKey = '') => http.get(`/projects/${id}/prompt-policy-overrides`, { params: policyKey ? { policy_key: policyKey } : {} }),
  effectivePromptPolicy: (id, params) => http.get(`/projects/${id}/prompt-policy-effective`, { params }),
  createPromptPolicyOverride: (id, data) => http.post(`/projects/${id}/prompt-policy-overrides`, data),
  updatePromptPolicyOverride: (id, overrideId, content) => http.put(`/projects/${id}/prompt-policy-overrides/${overrideId}`, { content }),
  deletePromptPolicyOverride: (id, overrideId) => http.delete(`/projects/${id}/prompt-policy-overrides/${overrideId}`),
  reconcileAssetsPreview: (id, data) => http.post(`/projects/${id}/assets/reconciliation/preview`, data),
  applyAssetReconciliation: (id, decisions) => http.post(`/projects/${id}/assets/reconciliation/apply`, { decisions }),
  assetVariants: (id, entityType, entityId) => http.get(`/projects/${id}/asset-variants`, { params: { entity_type: entityType, entity_id: entityId } }),
  selectAssetVariant: (id, variantId) => http.post(`/projects/${id}/asset-variants/${variantId}/select`),
  favoriteAssetVariant: (id, variantId, favorite) => http.patch(`/projects/${id}/asset-variants/${variantId}/favorite`, { favorite }),
  updateProject: (id, data) => http.put(`/projects/${id}`, data),
  deleteProject: (id) => http.delete(`/projects/${id}`),
  generateScript: (id, episodeN) => http.post(`/projects/${id}/script?episode_n=${episodeN || 1}`, null, { timeout: 300000 }),
  renderScriptFromText: (id, episodeN, script) => http.post(`/projects/${id}/script/render`, { episode_n: episodeN, script }, { timeout: 300000 }),
  expandScript: (id, episodeN, script) => http.post(`/projects/${id}/script/expand`, { episode_n: episodeN, script }, { timeout: 300000 }),
  createScriptRevision: (id, episodeN, reason = 'manual') => http.post(`/projects/${id}/script-revisions`, { episode_n: episodeN, reason }),
  scriptRevisions: (id, episodeN) => http.get(`/projects/${id}/script-revisions`, { params: { episode_n: episodeN } }),
  scriptRevision: (id, revisionId) => http.get(`/projects/${id}/script-revisions/${revisionId}`),
  restoreScriptRevision: (id, revisionId) => http.post(`/projects/${id}/script-revisions/${revisionId}/restore`),
  generatePlan: (id) => http.post(`/projects/${id}/plan`, null, { timeout: 300000 }),
  updatePlanEpisodes: (id, episodes) => http.put(`/projects/${id}/plan/episodes`, { episodes }),
  generateProject: (id, episodeN = 1, autoOnly = false) => {
    const query = new URLSearchParams({ episode_n: String(episodeN || 1) })
    if (autoOnly) query.set('auto', '1')
    return http.post(`/projects/${id}/generate?${query}`)
  },
  generateAllImages: (id, episodeN) => http.post(`/projects/${id}/images${episodeN ? `?episode_n=${episodeN}` : ''}`),
  generateAllVideos: (id, episodeN) => http.post(`/projects/${id}/videos${episodeN ? `?episode_n=${episodeN}` : ''}`),
  updateScene: (id, sid, data) => http.patch(`/projects/${id}/scenes/${sid}`, data),
  updateSceneLocks: (id, sid, data) => http.patch(`/projects/${id}/scenes/${sid}/locks`, data),
  redesignScenePrompt: (id, sid, data) => http.post(`/projects/${id}/scenes/${sid}/prompt/redesign`, typeof data === 'string' ? { brief: data } : data, { timeout: 300000 }),
  generateSceneImage: (id, sid) => http.post(`/projects/${id}/scenes/${sid}/image`),
  sceneCandidates: (id, sid, mediaType = '') => http.get(`/projects/${id}/scenes/${sid}/candidates`, { params: mediaType ? { media_type: mediaType } : {} }),
  reviewCandidate: (id, cid, status, reason = '') => http.patch(`/projects/${id}/candidates/${cid}/review`, { status, reason }),
  selectCandidate: (id, cid) => http.post(`/projects/${id}/candidates/${cid}/select`),
  branchCandidate: (id, cid) => http.post(`/projects/${id}/candidates/${cid}/branch`),
  retryCandidate: (id, cid) => http.post(`/projects/${id}/candidates/${cid}/retry`),
  deleteCandidate: (id, cid) => http.delete(`/projects/${id}/candidates/${cid}`),
  uploadSceneImage: (id, sid, file) => {
    const fd = new FormData()
    fd.append('file', file)
    return http.post(`/projects/${id}/scenes/${sid}/image/upload`, fd, {
      headers: { 'Content-Type': 'multipart/form-data' },
      timeout: 60000
    })
  },
  generateSceneVideo: (id, sid) => http.post(`/projects/${id}/scenes/${sid}/video`),
  sceneVideoPrompt: (id, sid) => http.get(`/projects/${id}/scenes/${sid}/video/prompt`),
  regenerateSceneVideoPrompt: (id, sid, payload = {}) => http.post(`/projects/${id}/scenes/${sid}/video/prompt/regenerate`, payload, { timeout: 300000 }),
  updateSceneVideoPrompt: (id, sid, payload) => http.put(`/projects/${id}/scenes/${sid}/video/prompt`, payload),
  uploadSceneVideoFrame: (id, sid, kind, file) => { const form = new FormData(); form.append('file', file); return http.post(`/projects/${id}/scenes/${sid}/video/frame-upload?kind=${kind}`, form, { headers: { 'Content-Type': 'multipart/form-data' }, timeout: 120000 }) },
  cancelSceneVideo: (id, sid) => http.post(`/projects/${id}/scenes/${sid}/video/cancel`),
  mergeScenes: (id, payload) => http.post(`/projects/${id}/merge`, payload),
  mergeAudioScenes: (id, payload) => http.post(`/projects/${id}/audio-merge`, payload),
  mergeAllScenes: (id, payload = {}) => http.post(`/projects/${id}/merge-all`, payload),
  merges: (id) => http.get(`/projects/${id}/merges`),
  audioLayers: (id, episodeN) => http.get(`/projects/${id}/audio-layers`, { params: episodeN ? { episode_n: episodeN } : {} }),
  createAudioLayer: (id, data) => http.post(`/projects/${id}/audio-layers`, data),
  updateAudioLayer: (id, aid, data) => http.put(`/projects/${id}/audio-layers/${aid}`, data),
  deleteAudioLayer: (id, aid) => http.delete(`/projects/${id}/audio-layers/${aid}`),
  // 角色资产
  characters: (id) => http.get(`/projects/${id}/characters`),
  characterHistory: (id, characterId) => http.get(`/projects/${id}/characters/history`, { params: characterId ? { character_id: characterId } : {} }),
  createCharacter: (id, data) => http.post(`/projects/${id}/characters`, data),
  updateCharacter: (id, cid, data) => http.put(`/projects/${id}/characters/${cid}`, data),
  deleteCharacter: (id, cid) => http.delete(`/projects/${id}/characters/${cid}`),
  generateCharacterPortrait: (id, cid) => http.post(`/projects/${id}/characters/${cid}/portrait`),
  recoverCharacterPortrait: (id, cid) => http.post(`/projects/${id}/characters/${cid}/portrait/recover`),
  resetCharacterPortrait: (id, cid) => http.post(`/projects/${id}/characters/${cid}/portrait/reset`),
  generateCharacterSheet: (id, cid) => http.post(`/projects/${id}/characters/${cid}/sheet`),
  uploadCharacterPortrait: (id, cid, file) => {
    const fd = new FormData()
    fd.append('file', file)
    return http.post(`/projects/${id}/characters/${cid}/portrait/upload`, fd, {
      headers: { 'Content-Type': 'multipart/form-data' },
      timeout: 120000
    })
  },
  generateAllPortraits: (id) => http.post(`/projects/${id}/characters/portraits`),

  // 角色档案（LumxAI 风格结构化角色提示词 + 审核工作流）
  generateCharacterProfile: (id, cid) => http.post(`/projects/${id}/characters/${cid}/profile`, null, { timeout: 300000 }),
  generateReferencePrompt: (id, cid) => http.post(`/projects/${id}/characters/${cid}/profile/generate-prompt`, null, { timeout: 300000 }),
  updateCharacterProfile: (id, cid, data) => http.put(`/projects/${id}/characters/${cid}/profile`, data),
  approveCharacterProfile: (id, cid, note) => http.post(`/projects/${id}/characters/${cid}/profile/approve`, { note }),
  rejectCharacterProfile: (id, cid, reason) => http.post(`/projects/${id}/characters/${cid}/profile/reject`, { reason }),
  resetCharacterProfile: (id, cid) => http.post(`/projects/${id}/characters/${cid}/profile/reset`),

  // 角色语音（预设音色 / 参考语音复刻）
  uploadCharacterVoice: (id, cid, file) => {
    const fd = new FormData()
    fd.append('file', file)
    return http.post(`/projects/${id}/characters/${cid}/voice/upload`, fd, {
      headers: { 'Content-Type': 'multipart/form-data' },
      timeout: 120000
    })
  },
  cloneCharacterVoice: (id, cid) => http.post(`/projects/${id}/characters/${cid}/voice/clone`, null, { timeout: 120000 }),
  clearCharacterVoice: (id, cid) => http.post(`/projects/${id}/characters/${cid}/voice/clear`),
  sceneReferences: (id, sid) => http.get(`/projects/${id}/scenes/${sid}/references`),
  updateSceneReferences: (id, sid, references) => http.put(`/projects/${id}/scenes/${sid}/references`, { references }),
  sceneOutfits: (id, sid) => http.get(`/projects/${id}/scenes/${sid}/outfits`),
  updateSceneOutfits: (id, sid, outfits) => http.put(`/projects/${id}/scenes/${sid}/outfits`, { outfits }),
  characterLooks: (id, cid) => http.get(`/projects/${id}/characters/${cid}/looks`),
  characterOutfits: (id, cid) => http.get(`/projects/${id}/characters/${cid}/outfits`),
  characterMotionReferences: (id, cid) => http.get(`/projects/${id}/characters/${cid}/motion-references`),
  uploadCharacterMotionReference: (id, cid, file, audio, name) => { const fd = new FormData(); fd.append('video', file); if (audio) fd.append('audio', audio); if (name) fd.append('name', name); return http.post(`/projects/${id}/characters/${cid}/motion-references`, fd, { headers: { 'Content-Type': 'multipart/form-data' }, timeout: 300000 }) },
  selectCharacterMotionReference: (id, cid, rid) => http.post(`/projects/${id}/characters/${cid}/motion-references/${rid}/select`),
  deleteCharacterMotionReference: (id, cid, rid) => http.delete(`/projects/${id}/characters/${cid}/motion-references/${rid}`),
  shotLooks: (id, shotId) => http.get(`/projects/${id}/shots/${shotId}/looks`),
  updateShotLooks: (id, shotId, looks) => http.put(`/projects/${id}/shots/${shotId}/looks`, { looks }),
  shotOutfits: (id, shotId) => http.get(`/projects/${id}/shots/${shotId}/outfits`),
  updateShotOutfits: (id, shotId, outfits) => http.put(`/projects/${id}/shots/${shotId}/outfits`, { outfits }),
  // 视觉资产（kind: prop=道具 / location=场景）
  assets: (id, kind) => http.get(`/projects/${id}/assets/${kind}`),
  redesignAssetDescription: (id, kind, data) => http.post(`/projects/${id}/assets/${kind}/redesign`, data, { timeout: 300000 }),
  createAsset: (id, kind, data) => http.post(`/projects/${id}/assets/${kind}`, data),
  updateAsset: (id, kind, aid, data) => http.put(`/projects/${id}/assets/${kind}/${aid}`, data),
  deleteAsset: (id, kind, aid) => http.delete(`/projects/${id}/assets/${kind}/${aid}`),
  generateAssetImage: (id, kind, aid) => http.post(`/projects/${id}/assets/${kind}/${aid}/image`),
  generatePropSheet: (id, aid) => http.post(`/projects/${id}/assets/prop/${aid}/sheet`),
  uploadAssetImage: (id, kind, aid, file) => {
    const fd = new FormData()
    fd.append('file', file)
    return http.post(`/projects/${id}/assets/${kind}/${aid}/image/upload`, fd, {
      headers: { 'Content-Type': 'multipart/form-data' },
      timeout: 120000
    })
  },
  generateAllAssetImages: (id, kind) => http.post(`/projects/${id}/assets/${kind}/images`),
  // 对白配音与字幕
  sceneDialogues: (id, sid) => http.get(`/projects/${id}/scenes/${sid}/dialogues`),
  generateSceneDub: (id, sid) => http.post(`/projects/${id}/scenes/${sid}/dub`),
  generateProjectDub: (id) => http.post(`/projects/${id}/dub`),
  generateEpisodeDub: (id, ep, staleOnly = true) => http.post(`/projects/${id}/episodes/${ep}/dub`, null, { params: { stale_only: staleOnly } }),
  episodeDubPreview: (id, ep) => http.get(`/projects/${id}/episodes/${ep}/dub/preview`),
  applyDialoguePreview: (id, did) => http.post(`/projects/${id}/dialogues/${did}/preview/apply`),
  revertDialogueAudio: (id, did) => http.post(`/projects/${id}/dialogues/${did}/audio/revert`),
  srtUrl: (id, ep) => `/api/projects/${id}/srt?episode_n=${ep || 1}`,
  dubAudioUrl: (pid, file) => `/api/input/${pid}/${file}`,
  inputUrl: (taskId, path) => `/api/input/${taskId}/${path}`,
  // 剪辑台
  editorData: (id, ep) => http.get(`/projects/${id}/editor?episode_n=${ep || 1}`),
  updateDialogue: (id, did, data) => http.put(`/projects/${id}/dialogues/${did}`, data),
  redubDialogue: (id, did) => http.post(`/projects/${id}/dialogues/${did}/dub`),
  reorderScenes: (id, data) => http.put(`/projects/${id}/editor/order`, data),
  updateSceneDuration: (id, sid, data) => http.patch(`/projects/${id}/scenes/${sid}/duration`, data),

  // 素材库
  materials: (params) => http.get('/materials', { params }),
  uploadMaterial: (file, type, projectId) => {
    const fd = new FormData()
    fd.append('file', file)
    fd.append('type', type)
    if (projectId) fd.append('project_id', projectId)
    return http.post('/materials', fd, {
      headers: { 'Content-Type': 'multipart/form-data' },
      timeout: 300000
    })
  },
  deleteMaterial: (id) => http.delete(`/materials/${id}`),
  sharedAssetReferences: (id) => http.get(`/projects/${id}/shared-asset-references`),
  effectiveSharedAssets: (id, params = {}) => http.get(`/projects/${id}/shared-assets/effective`, { params }),
  createSharedAssetReference: (id, data) => http.post(`/projects/${id}/shared-asset-references`, data),
  updateSharedAssetReference: (id, rid, data) => http.put(`/projects/${id}/shared-asset-references/${rid}`, data),
  deleteSharedAssetReference: (id, rid) => http.delete(`/projects/${id}/shared-asset-references/${rid}`),
  materialUrl: (path) => {
    const i = path.indexOf('/')
    return i > 0 ? `/api/input/${path.slice(0, i)}/${path.slice(i + 1)}` : `/api/input/${path}`
  },

  // 创作技能管理
  listSkills: (params) => http.get('/skills', { params }),
  getSkill: (id) => http.get(`/skills/${id}`),
  createSkill: (data) => http.post('/skills', data),
  updateSkill: (id, data) => http.put(`/skills/${id}`, data),
  deleteSkill: (id) => http.delete(`/skills/${id}`),
  upgradeSkill: (id, data) => http.post(`/skills/${id}/upgrade`, data),
  skillVersionHistory: (code) => http.get(`/skills/history/${code}`),
  skillStats: () => http.get('/skills/stats'),
  previewSkillPrompt: (id, params) => http.post(`/skills/${id}/preview`, { params }),
  skillStages: () => http.get('/skills/stages'),
  // 项目级技能配置
  projectSkills: (id) => http.get(`/projects/${id}/skills`),
  setProjectSkill: (id, data) => http.post(`/projects/${id}/skills`, data),
  resetProjectSkill: (id, stage, operation) => http.delete(`/projects/${id}/skills/${stage}`, { params: { operation } }),
  effectiveSkill: (id, stage, operation) => http.get(`/projects/${id}/skills/effective`, { params: { stage, operation } }),
  skillAuditLogs: (id, params) => http.get(`/projects/${id}/skills/audit`, { params }),

  // 导演镜头与提示词工作台
  sceneShots: (id, sid) => http.get(`/projects/${id}/scenes/${sid}/shots`),
  replaceSceneShots: (id, sid, shots) => http.post(`/projects/${id}/scenes/${sid}/shots`, { shots }),
  updateShot: (id, shotId, data) => http.put(`/projects/${id}/shots/${shotId}`, data),
  expandShotDirectorPrompt: (id, shotId, data) => http.post(`/projects/${id}/shots/${shotId}/director-expand`, data, { timeout: 300000 }),
  shotStatePromptDraft: (id, shotId, data) => http.post(`/projects/${id}/shots/${shotId}/skills/state-prompt`, data, { timeout: 300000 }),
  shotStyleRecommendations: (id, shotId, limit = 3) => http.get(`/projects/${id}/shots/${shotId}/style-recommendations`, { params: { limit } }),
  applyShotStylePreset: (id, shotId, presetId, preview = false) => http.post(`/projects/${id}/shots/${shotId}/style-preset`, { preset_id: Number(presetId), preview }),
  deleteShot: (id, shotId) => http.delete(`/projects/${id}/shots/${shotId}`),
  buildPrompt: (data) => http.post('/prompts/build', data),
  optimizePrompt: (data) => http.post('/prompts/optimize', data, { timeout: 300000 }),
  textProviderCapabilities: () => http.get('/text-provider/capabilities'),
  firstFramePolish: (data) => http.post('/prompt-workshop/first-frame-polish', data, { timeout: 300000 }),
  translatePrompt: (data) => http.post('/prompts/translate', data, { timeout: 300000 }),
  promptHistory: (params) => http.get('/prompts/history', { params }),
  rollbackPrompt: (data) => http.post('/prompts/rollback', data),
  presets: (params) => http.get('/presets', { params }),
  presetRecommendations: (params) => http.get('/presets/recommend', { params }),
  applyPreset: (presetId, basePrompt) => http.post(`/presets/${presetId}/apply`, { base_prompt: basePrompt }),

  // 长篇小说导入与章节管理
  createNovelProject: (data) => http.post('/projects/novel', data),
  uploadNovel: (id, file) => {
    const fd = new FormData()
    fd.append('file', file)
    return http.post(`/projects/${id}/novel/upload`, fd, { headers: { 'Content-Type': 'multipart/form-data' }, timeout: 300000 })
  },
  novelImportStatus: (id) => http.get(`/projects/${id}/novel/import-status`),
  novelChapters: (id) => http.get(`/projects/${id}/novel/chapters`),
  novelChapter: (id, cid) => http.get(`/projects/${id}/novel/chapters/${cid}`),
  updateNovelChapter: (id, cid, data) => http.put(`/projects/${id}/novel/chapters/${cid}`, data),
  reorderNovelChapters: (id, orders) => http.put(`/projects/${id}/novel/chapters/reorder`, { orders }),
  splitNovelChapter: (id, cid, splitPoints) => http.post(`/projects/${id}/novel/chapters/${cid}/split`, { split_points: splitPoints }),
  mergeNovelChapters: (id, chapterIds, newTitle) => http.post(`/projects/${id}/novel/chapters/merge`, { chapter_ids: chapterIds, new_title: newTitle }),
  analyzeNovelChapters: (id, chapterIds = []) => http.post(`/projects/${id}/novel/chapters/analyze`, { chapter_ids: chapterIds }, { timeout: 300000 }),
  retryNovelChapter: (id, cid) => http.post(`/projects/${id}/novel/chapters/${cid}/retry`, {}, { timeout: 300000 }),
  novelArcs: (id) => http.get(`/projects/${id}/novel/arcs`),
  generateNovelArcs: (id, groupSize = 8) => http.post(`/projects/${id}/novel/arcs/generate`, { group_size: groupSize }, { timeout: 300000 }),
  novelAliases: (id) => http.get(`/projects/${id}/novel/aliases`),
  updateNovelAlias: (id, aid, status) => http.put(`/projects/${id}/novel/aliases/${aid}`, { status }),
  storyBible: (id) => http.get(`/projects/${id}/story-bible`),
  generateStoryBible: (id) => http.post(`/projects/${id}/story-bible/generate`, {}, { timeout: 300000 }),
  updateStoryBible: (id, data) => http.put(`/projects/${id}/story-bible`, data),
  approveStoryBible: (id) => http.post(`/projects/${id}/story-bible/approve`),
  novelJobs: (id) => http.get(`/projects/${id}/novel/jobs`),
  adaptationStrategy: (id) => http.get(`/projects/${id}/adaptation-strategy`),
  saveAdaptationStrategy: (id, data) => http.put(`/projects/${id}/adaptation-strategy`, data),
  adaptations: (id) => http.get(`/projects/${id}/adaptations`),
  generateAdaptations: (id) => http.post(`/projects/${id}/adaptations/generate`, {}, { timeout: 300000 }),
  updateAdaptation: (id, episode, data) => http.put(`/projects/${id}/adaptations/${episode}`, data),
  approveAdaptation: (id, episode) => http.post(`/projects/${id}/adaptations/${episode}/approve`),
  generateAdaptationScript: (id, episode) => http.post(`/projects/${id}/adaptations/${episode}/script`, {}, { timeout: 300000 }),
  reviewAdaptation: (id, episode, overrideReason = '') => http.post(`/projects/${id}/adaptations/${episode}/review`, { override_reason: overrideReason }, { timeout: 300000 }),
  adaptationContext: (id, episode) => http.get(`/projects/${id}/adaptations/${episode}/context`),
  novelUsage: (id) => http.get(`/projects/${id}/novel/usage`),

  // 视频分镜连续性
  extractFrameCandidates: (id, sid) => http.post(`/projects/${id}/scenes/${sid}/continuity/frames/extract`, {}, { timeout: 180000 }),
  getFrameCandidates: (id, sid) => http.get(`/projects/${id}/scenes/${sid}/continuity/frames`),
  selectFrame: (id, sid, frameId) => http.put(`/projects/${id}/scenes/${sid}/continuity/frames/select`, { frame_id: frameId }),
  replaceSelectedFrame: (id, sid, form) => http.post(`/projects/${id}/scenes/${sid}/continuity/frames/replace`, form, { headers: { 'Content-Type': 'multipart/form-data' }, timeout: 180000 }),
  getContinuity: (id, sid) => http.get(`/projects/${id}/scenes/${sid}/continuity`),
  configureContinuity: (id, sid, data) => http.put(`/projects/${id}/scenes/${sid}/continuity`, data),
}

export function wsUrl() {
  const proto = location.protocol === 'https:' ? 'wss' : 'ws'
  return `${proto}://${location.host}/api/ws`
}
