import test from 'node:test'
import assert from 'node:assert/strict'
import { readFileSync } from 'node:fs'

const detail = readFileSync(new URL('../src/views/ProjectDetail.vue', import.meta.url), 'utf8')
const api = readFileSync(new URL('../src/api/index.js', import.meta.url), 'utf8')

test('storyboard sheet anchors are drafted first and only submitted on explicit confirmation', () => {
  assert.match(api, /storyboardSheetAnchors: \(id, sid\) => http\.post\(`\/projects\/\$\{id\}\/scenes\/\$\{sid\}\/storyboard-sheet\/anchors`/)
  assert.match(api, /generateStoryboardSheet: \(id, sid, data\) => http\.post\(`\/projects\/\$\{id\}\/scenes\/\$\{sid\}\/storyboard-sheet`, data\)/)
  assert.match(detail, /@click="openStoryboardSheet\(sc\)">故事版总图<\/button>/)
  assert.match(detail, /@click="submitStoryboardSheet">\{\{ sheetGenerating \? '出图中…' : '确认锚点并出图' \}\}/)
  const draft = detail.slice(detail.indexOf('async function draftStoryboardAnchors'), detail.indexOf('async function submitStoryboardSheet'))
  // Drafting anchors never starts an image task.
  assert.doesNotMatch(draft, /generateStoryboardSheet/)
})

test('the storyboard sheet is labelled review-only and never wired into references', () => {
  assert.match(detail, /总图只供审阅，不会作为 H3 参考图，也不会进入任何视频提交/)
  const block = detail.slice(detail.indexOf('const sheetScene = ref(null)'), detail.indexOf('async function submitStoryboardSheet'))
  assert.doesNotMatch(block, /selectedSceneReferences|updateScene|use_h3/)
})
