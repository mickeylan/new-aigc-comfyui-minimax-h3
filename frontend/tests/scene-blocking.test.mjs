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

test('站位提示词必须先预览编辑再显式确认且显示两类应用状态', () => {
  assert.match(detail, /按此站位更新分镜图提示词/)
  assert.match(detail, /按此站位更新视频完整执行稿/)
  assert.match(detail, /完整视频执行稿.*预览（可编辑，尚未保存）/)
  assert.match(detail, /v-model="blockingPromptPreview\.prompt"/)
  assert.match(detail, /确认保存此/)
  assert.match(detail, /blocking_image_applied\?'已应用':'未应用'/)
  assert.match(detail, /blocking_video_applied\?'已应用':'未应用'/)
  assert.match(detail, /保存为实际生成使用的 video_full_prompt/)
  assert.match(detail, /local\.video_full_prompt=blockingPromptPreview\.prompt/)
  assert.match(detail, /local\.video_prompt=data\.action_prompt/)
  assert.match(api, /previewSceneBlockingPrompt/)
  assert.match(api, /applySceneBlockingPrompt/)
  assert.match(detail, /完整视频执行稿已确认保存，未启动生成任务/)
})
