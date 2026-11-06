import test from 'node:test'
import assert from 'node:assert/strict'
import { readFileSync } from 'node:fs'

const source = readFileSync(new URL('../src/views/ProjectDetail.vue', import.meta.url), 'utf8')
const saveFunction = source.slice(source.indexOf('async function saveScene(close = true)'), source.indexOf('function aspectLabel'))

test('失效历史参考图被隔离并在下次显式保存时清理', () => {
  assert.match(source, /invalidSceneReferences = ref\(\[\]\)/)
  assert.match(source, /data\.invalid_references \|\| \[\]/)
  assert.match(source, /canonicalSceneReferences\(\[\.\.\.selectedSceneReferences\.value, \.\.\.invalidSceneReferences\.value\]\)/)
  assert.match(source, /已隔离.*条失效旧参考图/)
})

function fixture() {
  const original = { id: 7, content: '旧故事', image_prompt: '', image_file: 'keep.png', video_file: 'keep.mp4' }
  const editingScene = { value: original }
  const scenes = { value: [original] }
  const sceneForm = { content: 'AI扩写后的新故事', duration: 8, image_prompt: '' }
  const synced = [], calls = []
  let complete
  const request = new Promise(resolve => { complete = resolve })
  const context = {
    editingScene, scenes, sceneForm,
    selectedSceneReferences: { value: [] }, selectedSceneOutfits: { value: {} },
    sceneReferencesBaseline: { value: '[]' }, sceneOutfitsBaseline: { value: '[]' },
    canonicalSceneReferences: JSON.stringify, sceneOutfitAssignments: () => [],
    savingScene: { value: false }, storyApplyBusy: { value: false }, sceneError: { value: '' },
    api: { updateScene: async (...args) => { calls.push(args); return request }, updateSceneReferences: () => assert.fail('unchanged references written'), updateSceneOutfits: () => assert.fail('unchanged outfits written') },
    id: () => 1, nextTick: async () => {}, sceneWorkspaceBaseline: { value: '' }, sceneWorkspaceState: () => '{}',
    sceneShotDirector: { value: { syncAuthoritativeSceneContent: content => synced.push(content) } },
    closeSceneWorkspace: () => assert.fail('new or retained workspace closed'), toast: { show() {} }
  }
  const execute = new Function(...Object.keys(context), `let loadRequestEpoch=0; ${saveFunction}; return saveScene(false)`)
  return { ...context, calls, synced, complete, run: () => execute(...Object.values(context)) }
}

test('故事应用或已有保存进行中时不发送普通场景 PATCH', async () => {
  const f = fixture()
  f.storyApplyBusy.value = true
  assert.equal(await f.run(), false)
  f.storyApplyBusy.value = false
  f.savingScene.value = true
  assert.equal(await f.run(), false)
  assert.equal(f.calls.length, 0)
})

test('执行真实保存函数：新正文写入请求、场景列表与同场景导演，保留媒体', async () => {
  const f = fixture()
  const pending = f.run()
  assert.equal(f.calls[0][2].content, 'AI扩写后的新故事')
  // Editing while an in-flight request runs must not change its submitted snapshot.
  f.sceneForm.content = '稍后手动修改'
  f.complete({ data: { ok: true } })
  assert.equal(await pending, true)
  assert.equal(f.editingScene.value.content, 'AI扩写后的新故事')
  assert.equal(f.scenes.value[0].content, 'AI扩写后的新故事')
  assert.deepEqual(f.synced, ['AI扩写后的新故事'])
  assert.equal(f.editingScene.value.video_file, 'keep.mp4')
  assert.equal(f.editingScene.value.image_file, 'keep.png')
})

test('保存期间切换场景：更新原场景但不覆盖新编辑窗口或导演', async () => {
  const f = fixture()
  const pending = f.run()
  f.editingScene.value = { id: 8, content: '另一个场景' }
  f.complete({ data: { ok: true } })
  assert.equal(await pending, true)
  assert.equal(f.editingScene.value.id, 8)
  assert.equal(f.editingScene.value.content, '另一个场景')
  assert.equal(f.scenes.value[0].content, 'AI扩写后的新故事')
  assert.deepEqual(f.synced, [])
})
