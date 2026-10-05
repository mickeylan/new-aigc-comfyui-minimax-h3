import test from 'node:test'
import assert from 'node:assert/strict'
import { readFileSync } from 'node:fs'

const view = readFileSync(new URL('../src/views/EpisodeVoiceStudio.vue', import.meta.url), 'utf8')
const router = readFileSync(new URL('../src/router/index.js', import.meta.url), 'utf8')
const api = readFileSync(new URL('../src/api/index.js', import.meta.url), 'utf8')
const screenplay = readFileSync(new URL('../src/views/EpisodeScreenplay.vue', import.meta.url), 'utf8')

test('声音工作台路由、剧本入口与聚合接口已接通', () => {
  assert.match(router, /episodes\/:episode\/voice/)
  assert.match(router, /EpisodeVoiceStudio\.vue/)
  assert.match(screenplay, /进入声音工作台/)
  assert.match(screenplay, /episodes\/\$\{episodeN\}\/voice/)
  assert.match(api, /episodeVoiceStudio:/)
  assert.match(api, /episodes\/\$\{ep\}\/voice-studio/)
})

test('声音工作台提供角色摘要、筛选、进度及逐句表演参数编辑', () => {
  assert.match(view, /本集角色与音色/)
  assert.match(view, /本地 IndexTTS-2\.5 CUDA/)
  assert.match(view, /本地配音暂不可用/)
  assert.match(view, /ttsUnavailable/)
  assert.match(view, /其他功能不受影响/)
  assert.match(view, /progressPercent/)
  assert.match(view, /statusFilter/)
  assert.match(view, /roleFilter/)
  assert.match(view, /v-model\.number="d\.speed"/)
  assert.match(view, /v-model="d\.emotion"/)
  assert.match(view, /v-model="d\.delivery"/)
  assert.match(view, /updateDialogue\(projectId, d\.id/)
})

test('声音工作台支持单句、批量生成及音频播放', () => {
  assert.match(view, /redubDialogue\(projectId, d\.id\)/)
  assert.match(view, /generateEpisodeDub\(projectId, episodeN, staleOnly\)/)
  assert.match(view, /生成待更新/)
  assert.match(view, /全部重新生成/)
  assert.match(view, /<audio v-if="audioSource\(d\)"/)
})

test('SRT 预览使用 file multipart 字段并展示诊断与角色映射', () => {
  assert.match(api, /previewEpisodeSRT:/)
  assert.match(api, /fd\.append\('file', file\)/)
  assert.match(api, /episodes\/\$\{ep\}\/dub\/srt\/preview/)
  assert.match(api, /applyEpisodeSRT:/)
  assert.match(api, /episodes\/\$\{ep\}\/dub\/srt\/apply/)
  assert.match(api, /multipart\/form-data/)
  assert.match(view, /SRT 文件预览/)
  assert.match(view, /应用配音参数/)
  assert.match(view, /不覆盖结构化 Dialogue 原文/)
  assert.match(view, /srtDiagnostics/)
  assert.match(view, /roleMappings/)
  assert.match(view, /srtCues/)
})

test('声音工作台保留配音候选并支持试听审核和采用', () => {
  assert.match(api, /dialogueAudioCandidates:/)
  assert.match(api, /reviewDialogueAudioCandidate:/)
  assert.match(api, /selectDialogueAudioCandidate:/)
  assert.match(view, /试听候选/)
  assert.match(view, /采用此版本/)
  assert.match(view, /人工拒绝/)
})

test('声音工作台提供混音提交、SRT 导出及剪辑台入口', () => {
  assert.match(view, /mergeAudioScenes/)
  assert.match(view, /native_volume/)
  assert.match(view, /dialogue_volume/)
  assert.match(view, /bgm_volume/)
  assert.match(view, /api\.srtUrl\(projectId, episodeN\)/)
  assert.match(view, /进入剪辑台/)
})

test('声音工作台读取持久化批次状态并支持失败项重试', () => {
  assert.match(api, /episodeDubBatches:/)
  assert.match(api, /episodes\/\$\{ep\}\/dub\/batches/)
  assert.match(api, /retryEpisodeDubBatch:/)
  assert.match(api, /dub\/batches\/\$\{batchId\}\/retry/)
  assert.match(view, /批量任务/)
  assert.match(view, /任务状态由服务端持久保存/)
  assert.match(view, /api\.episodeDubBatches\(projectId, episodeN\)/)
  assert.match(view, /api\.retryEpisodeDubBatch\(projectId, episodeN, id\)/)
  assert.match(view, /dubBatches\.value\.some\(batchActive\)/)
})

test('声音工作台提供对白 WAV 与角色分轨导出并展示下载文件', () => {
  assert.match(api, /exportEpisodeDialogue:/)
  assert.match(api, /episodes\/\$\{ep\}\/dub\/export/)
  assert.match(api, /exportEpisodeDialogueStems:/)
  assert.match(api, /dub\/stems\/export/)
  assert.match(view, /api\.exportEpisodeDialogue\(projectId, episodeN\)/)
  assert.match(view, /api\.exportEpisodeDialogueStems\(projectId, episodeN\)/)
  assert.match(view, /data\?\.stems/)
  assert.match(view, /data\?\.dialogue/)
  assert.match(view, /导出对白 WAV/)
  assert.match(view, /导出角色分轨/)
})
