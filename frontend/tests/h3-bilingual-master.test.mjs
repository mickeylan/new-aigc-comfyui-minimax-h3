import test from 'node:test'
import assert from 'node:assert/strict'
import { readFileSync } from 'node:fs'

const detail = readFileSync(new URL('../src/views/ProjectDetail.vue', import.meta.url), 'utf8')
const api = readFileSync(new URL('../src/api/index.js', import.meta.url), 'utf8')

test('H3 bilingual API exposes translate, master save and confirm separately', () => {
  assert.match(api, /translateSceneVideoPrompt:.*prompt\/translate/)
  assert.match(api, /updateSceneVideoPromptMaster:.*prompt\/master/)
  assert.match(api, /confirmSceneVideoPrompt:.*prompt\/confirm/)
})

test('Chinese master workflow cannot overwrite legacy English through old PUT', () => {
  const saveMaster = detail.slice(detail.indexOf('async function saveChineseMaster'), detail.indexOf('async function translateChineseToEnglish'))
  assert.match(saveMaster, /updateSceneVideoPromptMaster/)
  assert.doesNotMatch(saveMaster, /updateSceneVideoPrompt\(/)
  assert.match(detail, /转换为中文对照/)
  assert.match(detail, /应用为中文主稿/)
  assert.match(detail, /确认保存英文执行稿/)
})

test('preview invalidation keeps English text but revokes confirmation', () => {
  assert.match(detail, /watch\(\[videoTaskMasterZH, videoTaskTemplate, videoTaskFirstFrame, videoTaskLastFrame\]/)
  const invalidate = detail.slice(detail.indexOf('function invalidateVideoPromptPreview'), detail.indexOf('watch([videoTaskMasterZH'))
  assert.match(invalidate, /videoTaskSourceHash\.value = ''/)
  assert.match(invalidate, /persistVideoPromptDraft\(\)/)
  assert.doesNotMatch(invalidate, /videoTaskExecPreview\.value = ''/)
  assert.match(detail, /promptState\.master_prompt_zh && promptState\.exec_status !== 'confirmed'/)
  assert.match(detail, /epoch !== videoPromptRequestEpoch \|\| videoTaskScene\.value\?\.id !== sceneId/)
})

test('ordinary Chinese master save does not discard the local English draft', () => {
  const saveMaster = detail.slice(detail.indexOf('async function saveChineseMaster'), detail.indexOf('async function translateChineseToEnglish'))
  assert.match(saveMaster, /invalidateVideoPromptPreview\(\)/)
  assert.doesNotMatch(saveMaster, /clearVideoPromptDraft|removeItem|videoTaskExecPreview\.value\s*=/)
})

test('reopening restores a local draft as unconfirmed instead of replacing confirmed server execution text', () => {
  const open = detail.slice(detail.indexOf('async function viewVideoPrompt'), detail.indexOf('async function regenerateVideoPrompt'))
  assert.match(open, /const localDraft = readVideoPromptDraft\(sc\.id\)/)
  assert.match(open, /localDraft\?\.execPrompt \? \{ exec_status: 'unconfirmed' \}/)
  assert.match(open, /videoTaskExecPreview\.value = localDraft\?\.execPrompt \|\| preview\.exec_prompt/)
  assert.match(open, /localDraft\?\.execPrompt \? \(localDraft\.sourceHash \|\| ''\) : \(preview\.exec_status === 'confirmed'/)
})

test('local English drafts are isolated by project and scene and cleared only after confirm', () => {
  assert.match(detail, /`h3-video-prompt-draft:\$\{id\(\)\}:\$\{sceneId\}`/)
  assert.match(detail, /localStorage\.setItem\(videoPromptDraftKey\(sceneId\)/)
  const confirm = detail.slice(detail.indexOf('async function confirmEnglishAndGenerate'), detail.indexOf('async function saveVideoPrompt'))
  assert.match(confirm, /confirmSceneVideoPrompt[\s\S]*clearVideoPromptDraft\(sceneId\)/)
})

test('regeneration always proposes Chinese without replacing the current draft', () => {
  const regenerate = detail.slice(detail.indexOf('async function regenerateVideoPrompt'), detail.indexOf('async function uploadVideoFrame'))
  assert.match(regenerate, /translateSceneVideoPrompt/)
  assert.match(regenerate, /videoTaskProposedZH\.value =/)
  assert.doesNotMatch(regenerate, /videoTaskPromptDraft\.value = generated/)
  assert.doesNotMatch(regenerate, /if \(isBilingualVideoPrompt\.value\)/)
})

test('async bilingual transitions retain immutable scene inputs and epoch guards', () => {
  const view = detail.slice(detail.indexOf('async function viewVideoPrompt'), detail.indexOf('async function regenerateVideoPrompt'))
  assert.match(view, /api\.task[\s\S]*if \(epoch !== videoPromptRequestEpoch\) return[\s\S]*videoTaskDetail\.value =/)

  const translate = detail.slice(detail.indexOf('async function translateChineseToEnglish'), detail.indexOf('async function confirmEnglishAndGenerate'))
  assert.match(translate, /const epoch = \+\+videoPromptRequestEpoch/)
  assert.match(translate, /const masterPromptZH = videoTaskMasterZH\.value/)
  assert.match(translate, /const template = videoTaskTemplate\.value/)
  assert.match(translate, /const firstFrameImg = videoTaskFirstFrame\.value/)
  assert.match(translate, /const lastFrameImg = videoTaskLastFrame\.value/)
  assert.match(translate, /updateSceneVideoPromptMaster[\s\S]*if \(epoch !== videoPromptRequestEpoch \|\| videoTaskScene\.value\?\.id !== sceneId\) return[\s\S]*translateSceneVideoPrompt/)
  assert.match(translate, /finally \{ if \(epoch === videoPromptRequestEpoch\) translatingVideoPrompt\.value = false \}/)

  const confirm = detail.slice(detail.indexOf('async function confirmEnglishAndGenerate'), detail.indexOf('async function saveVideoPrompt'))
  assert.match(confirm, /const sceneId = videoTaskScene\.value\.id/)
  assert.match(confirm, /confirmSceneVideoPrompt[\s\S]*if \(epoch !== videoPromptRequestEpoch \|\| videoTaskScene\.value\?\.id !== sceneId\) return[\s\S]*generateSceneVideo\(id\(\), sceneId\)/)
})
