import test from 'node:test'
import assert from 'node:assert/strict'
import { readFileSync } from 'node:fs'

const view = readFileSync(new URL('../src/views/ProjectDetail.vue', import.meta.url), 'utf8')
const apiSource = readFileSync(new URL('../src/api/index.js', import.meta.url), 'utf8')

function extractFunction(name) {
  const functionStart = view.indexOf(`function ${name}(`)
  assert.notEqual(functionStart, -1, `${name} must exist`)
  const start = view.slice(Math.max(0, functionStart - 6), functionStart) === 'async ' ? functionStart - 6 : functionStart
  const open = view.indexOf('{', start)
  let depth = 0
  for (let i = open; i < view.length; i++) {
    if (view[i] === '{') depth++
    if (view[i] === '}' && --depth === 0) return view.slice(start, i + 1)
  }
  throw new Error(`unterminated ${name}`)
}

function deferred() {
  let resolve
  const promise = new Promise(done => { resolve = done })
  return { promise, resolve }
}

function harness(api, confirm = () => true) {
  const state = {
    editingScene: { value: { id: 7, content: '旧正文', image_file: 'keep.png', video_file: 'keep.mp4' } },
    sceneForm: { content: '当前动作和逐字对白', image_prompt: '未保存画面草稿' },
    dialogues: { value: [{ id: 3, scene_id: 7, character: '', speech_type: 'unknown', text: '原句' }, { id: 4, scene_id: 8, character: '乙', speech_type: 'dialogue', text: '另一场' }] },
    storyPreview: { value: null }, storyPreviewBusy: { value: false }, storyApplyBusy: { value: false }, storyPreviewError: { value: '' },
    savingScene: { value: false },
    scenes: { value: [{ id: 7, content: '旧正文', image_file: 'keep.png', video_file: 'keep.mp4', references: ['keep'] }] },
    sceneShotDirector: { value: { syncAuthoritativeSceneContent: content => { state.synced = content } } },
    toast: { success: message => { state.success = message }, warning: () => {} },
  }
  const context = { ...state, api, id: () => '12', nextTick: async () => {}, window: { confirm } }
  const source = `let storyRequestEpoch = 0; ${extractFunction('closeStoryPreview')} ${extractFunction('previewIntegratedStory')} ${extractFunction('applyIntegratedStory')} return { closeStoryPreview, previewIntegratedStory, applyIntegratedStory }`
  return { state, actions: new Function(...Object.keys(context), source)(...Object.values(context)) }
}

test('story preview API contract is direct and separate from AI expansion', () => {
  assert.match(apiSource, /previewSceneStory:.*\/story\/preview/)
  assert.match(apiSource, /applySceneStory:.*\/story\/apply/)
  assert.match(view, /按原文整合对白/)
  assert.match(view, /sound_effect/)
  assert.match(view, /unknown.*必须审阅/)
  assert.match(view, /:value="dialogue\.text" readonly/)
})

test('closing invalidates a late story preview response', async () => {
  const pending = deferred()
  const { state, actions } = harness({ previewSceneStory: () => pending.promise })
  const request = actions.previewIntegratedStory()
  actions.closeStoryPreview()
  pending.resolve({ data: { content: '迟到内容', dialogues: [], source_hash: 'hash', warnings: [] } })
  await request
  assert.equal(state.storyPreview.value, null)
  assert.equal(state.storyPreviewBusy.value, false)
})

test('preview and apply are blocked while scene save or story apply is in flight', async () => {
  let previewCalls = 0, applyCalls = 0, confirmCalls = 0
  const { state, actions } = harness({
    previewSceneStory: async () => { previewCalls++; return { data: {} } },
    applySceneStory: async () => { applyCalls++; return { data: {} } }
  }, () => { confirmCalls++; return true })
  state.storyPreview.value = { projectId: '12', sceneId: 7, sourceContent: state.sceneForm.content, content: '预览', dialogues: [] }
  state.savingScene.value = true
  await actions.previewIntegratedStory()
  await actions.applyIntegratedStory()
  assert.equal(previewCalls, 0)
  assert.equal(applyCalls, 0)
  state.savingScene.value = false
  state.storyApplyBusy.value = true
  actions.closeStoryPreview()
  await actions.previewIntegratedStory()
  await actions.applyIntegratedStory()
  assert.equal(state.storyPreview.value.content, '预览')
  assert.equal(previewCalls, 0)
  assert.equal(applyCalls, 0)
  assert.equal(confirmCalls, 0)
})

test('apply patches only authoritative content and preserves media and other form drafts', async () => {
  let body
  const { state, actions } = harness({ applySceneStory: async (_pid, _sid, payload) => { body = payload; return { data: { content: '整合后正文', dialogues: payload.dialogues } } } })
  state.storyPreview.value = { projectId: '12', sceneId: 7, sourceContent: state.sceneForm.content, content: '可编辑预览', source_hash: 'source-hash', warnings: [], dialogues: [{ id: 3, speaker: '甲', speech_type: 'offscreen_dialogue', text: '原句', ignored: true }] }
  await actions.applyIntegratedStory()
  assert.deepEqual(body, { content: '可编辑预览', dialogues: [{ id: 3, speaker: '甲', speech_type: 'offscreen_dialogue', text: '原句' }], source_hash: 'source-hash' })
  assert.equal(state.scenes.value[0].content, '整合后正文')
  assert.equal(state.scenes.value[0].image_file, 'keep.png')
  assert.equal(state.scenes.value[0].video_file, 'keep.mp4')
  assert.deepEqual(state.scenes.value[0].references, ['keep'])
  assert.equal(state.sceneForm.image_prompt, '未保存画面草稿')
  assert.equal(state.scenes.value[0].prompt_stale, true)
  assert.equal(state.dialogues.value[0].character, '甲')
  assert.equal(state.dialogues.value[0].speech_type, 'offscreen_dialogue')
  assert.equal(state.dialogues.value[1].character, '乙')
  assert.equal(state.synced, '整合后正文')
  assert.equal(state.storyPreview.value, null)
})
