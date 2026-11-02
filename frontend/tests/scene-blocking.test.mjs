import test from 'node:test'
import assert from 'node:assert/strict'
import { readFileSync } from 'node:fs'
const detail = readFileSync(new URL('../src/views/ProjectDetail.vue', import.meta.url), 'utf8')
const api = readFileSync(new URL('../src/api/index.js', import.meta.url), 'utf8')

test('Scene卡提供可读中文站位表单与保存入口', () => {
  assert.match(detail, /scene-blocking-entry/)
  assert.match(detail, /原文事实（每行一条）/)
  assert.match(detail, /站位建议（每行一条）/)
  assert.match(detail, /v-model="character\.name"/)
  for (const field of ['position', 'facing', 'gaze', 'visibility']) assert.match(detail, new RegExp(`v-model="character\\.${field}"`))
  assert.match(detail, /addBlockingCharacter/)
  assert.match(detail, /removeBlockingCharacter/)
  for (const field of ['camera', 'start', 'end', 'summary']) assert.match(detail, new RegExp(`v-model="blockingDraft\\.${field}"`))
  assert.match(detail, /显式保存/)
  assert.match(api, /analyzeSceneBlocking/)
  assert.match(api, /saveSceneBlocking/)
})

test('站位异步请求捕获sceneId并忽略已关闭或切换场景的响应', () => {
  assert.match(detail, /const sceneId = blockingScene\.value\.id;[\s\S]*?analyzeSceneBlocking\(id\(\), sceneId\)/)
  assert.match(detail, /const sceneId = blockingScene\.value\.id;[\s\S]*?inheritSceneBlocking\(id\(\), sceneId\)/)
  assert.match(detail, /saveSceneBlocking\(id\(\),sceneId,value\); if \(blockingScene\.value\?\.id !== sceneId\) return/)
  assert.ok((detail.match(/blockingScene\.value\?\.id !== sceneId/g) || []).length >= 3)
  assert.match(detail, /local\.blocking_json=JSON\.stringify\(data\.blocking\);local\.prompt_stale=true/)
  assert.doesNotMatch(detail, /local\.(image_file|video_file)\s*=/)
})
