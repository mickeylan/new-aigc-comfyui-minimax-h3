import test from 'node:test'
import assert from 'node:assert/strict'
import { readFileSync } from 'node:fs'

const detail = readFileSync(new URL('../src/views/ProjectDetail.vue', import.meta.url), 'utf8')
const api = readFileSync(new URL('../src/api/index.js', import.meta.url), 'utf8')

test('视频重新生成展示来源、完整稿与中文预览的对白数量', () => {
  assert.match(detail, /data\.dialogue_count/)
  assert.match(detail, /data\.prompt_dialogue_count/)
  assert.match(detail, /videoTaskProposedZH\.value\.match/)
  assert.match(detail, /\{\{ videoTaskDetail\.dialogue_generation_note \}\}/)
})

test('翻译前完整稿在翻译请求前保存供核验，翻译警告和策略详情可见', () => {
  const regenerate = detail.slice(detail.indexOf('async function regenerateVideoPrompt()'), detail.indexOf('async function uploadVideoFrame'))
  assert.ok(regenerate.indexOf('generated_before_translation: generated') < regenerate.indexOf('await api.translateSceneVideoPrompt'))
  assert.match(detail, /\{\{videoTaskDetail\.generated_before_translation\}\}/)
  assert.match(detail, /generation_translation_warnings\.join/)
  assert.match(detail, /e\.response\.data\.detail/)
})

test('对白分析预览保持为本地草稿，只有应用才持久化', () => {
  assert.match(api, /analyzeSceneDialogue/)
  assert.match(api, /applySceneDialogue/)
  const analyze = detail.match(/async function analyzeDialogue\(\)[\s\S]*?\nasync function applyDialogueAnalysis/)?.[0] || ''
  assert.match(analyze, /setDialogueAnalysis\(data\.analysis\)/)
  assert.doesNotMatch(analyze, /dialogue_analysis_json\s*=/)
})

test('对白异步分析和应用均校验捕获的scene id', () => {
  assert.match(detail, /const sid = dialogueAnalysisScene\.value\.id;[\s\S]*?analyzeSceneDialogue\(id\(\), sid\)/)
  assert.match(detail, /applySceneDialogue\(id\(\), sid,[\s\S]*?if \(dialogueAnalysisScene\.value\?\.id !== sid\) return/)
  assert.ok((detail.match(/dialogueAnalysisScene\.value\?\.id !== sid/g) || []).length >= 2)
})
