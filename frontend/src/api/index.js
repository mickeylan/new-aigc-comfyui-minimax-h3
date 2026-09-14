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
  tasks: (params) => http.get('/tasks', { params }),
  clearTasks: () => http.delete('/tasks'),
  task: (id) => http.get(`/tasks/${id}`),
  createTask: (data) => http.post('/tasks', data),
  cancelTask: (id) => http.post(`/tasks/${id}/cancel`),
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
  updateProject: (id, data) => http.put(`/projects/${id}`, data),
  deleteProject: (id) => http.delete(`/projects/${id}`),
  generateScript: (id, episodeN) => http.post(`/projects/${id}/script?episode_n=${episodeN || 1}`, null, { timeout: 300000 }),
  renderScriptFromText: (id, episodeN, script) => http.post(`/projects/${id}/script/render`, { episode_n: episodeN, script }, { timeout: 300000 }),
  expandScript: (id, episodeN, script) => http.post(`/projects/${id}/script/expand`, { episode_n: episodeN, script }, { timeout: 300000 }),
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
  generateSceneImage: (id, sid) => http.post(`/projects/${id}/scenes/${sid}/image`),
  generateSceneVideo: (id, sid) => http.post(`/projects/${id}/scenes/${sid}/video`),
  cancelSceneVideo: (id, sid) => http.post(`/projects/${id}/scenes/${sid}/video/cancel`),
  mergeScenes: (id, payload) => http.post(`/projects/${id}/merge`, payload),
  mergeAllScenes: (id) => http.post(`/projects/${id}/merge-all`),
  merges: (id) => http.get(`/projects/${id}/merges`),
  // 角色资产
  characters: (id) => http.get(`/projects/${id}/characters`),
  createCharacter: (id, data) => http.post(`/projects/${id}/characters`, data),
  updateCharacter: (id, cid, data) => http.put(`/projects/${id}/characters/${cid}`, data),
  deleteCharacter: (id, cid) => http.delete(`/projects/${id}/characters/${cid}`),
  generateCharacterPortrait: (id, cid) => http.post(`/projects/${id}/characters/${cid}/portrait`),
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
  // 视觉资产（kind: prop=道具 / location=场景）
  assets: (id, kind) => http.get(`/projects/${id}/assets/${kind}`),
  createAsset: (id, kind, data) => http.post(`/projects/${id}/assets/${kind}`, data),
  updateAsset: (id, kind, aid, data) => http.put(`/projects/${id}/assets/${kind}/${aid}`, data),
  deleteAsset: (id, kind, aid) => http.delete(`/projects/${id}/assets/${kind}/${aid}`),
  generateAssetImage: (id, kind, aid) => http.post(`/projects/${id}/assets/${kind}/${aid}/image`),
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
  resetProjectSkill: (id, stage) => http.delete(`/projects/${id}/skills/${stage}`),
  effectiveSkill: (id, stage) => http.get(`/projects/${id}/skills/effective?stage=${stage}`),
  skillAuditLogs: (id, params) => http.get(`/projects/${id}/skills/audit`, { params }),
}

export function wsUrl() {
  const proto = location.protocol === 'https:' ? 'wss' : 'ws'
  return `${proto}://${location.host}/api/ws`
}
