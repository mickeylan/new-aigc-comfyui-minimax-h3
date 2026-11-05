import test from 'node:test'
import assert from 'node:assert/strict'
import { readFileSync } from 'node:fs'

const view = readFileSync(new URL('../src/views/ProjectDetail.vue', import.meta.url), 'utf8')
const apiSource = readFileSync(new URL('../src/api/index.js', import.meta.url), 'utf8')

test('manual scene merge API has unique preview/apply contracts', () => {
  assert.match(apiSource, /previewSceneMerge: \(id, sceneIds\) => http\.post\(`\/projects\/\$\{id\}\/scenes\/merge\/preview`, \{ scene_ids: sceneIds \}\)/)
  assert.match(apiSource, /applySceneMerge: \(id, sceneIds, sourceHash\) => http\.post\(`\/projects\/\$\{id\}\/scenes\/merge\/apply`, \{ scene_ids: sceneIds, source_hash: sourceHash \}\)/)
  assert.equal((apiSource.match(/previewSceneMerge:/g) || []).length, 1)
  assert.equal((apiSource.match(/applySceneMerge:/g) || []).length, 1)
})

test('project detail limits selection to same episode, sorted adjacent available scenes', () => {
  assert.match(view, /const currentScenes = computed\(\(\) => scenes\.value[\s\S]*?\.filter\(s => \(s\.episode_n \|\| 1\) === activeEpN\.value\)[\s\S]*?\.sort\(\(a, b\) => a\.order - b\.order\)/)
  assert.match(view, /const availableScenes = computed\(\(\) => currentScenes\.value\.filter\(sc => Number\(sc\.duration \|\| 0\) >= 5 && !isWorking\(sc\)\)\)/)
  assert.match(view, /sc\.order === rows\[index - 1\]\.order \+ 1/)
  assert.match(view, /watch\(activeEpN,[\s\S]*?selectedMergeSceneIDs\.value = \[\]/)
  assert.doesNotMatch(view.slice(view.indexOf('const availableScenes'), view.indexOf('const selectedMergeScenes')), /shot/i)
})

test('preview then apply request pipeline carries immutable hash and refreshes without media generation', async () => {
  const calls = []
  const api = {
    previewSceneMerge: async (pid, ids) => { calls.push(['preview', pid, ids]); return { data: { scene_ids: ids, source_hash: 'hard-hash', content: 'A\n\nB' } } },
    applySceneMerge: async (pid, ids, hash) => { calls.push(['apply', pid, ids, hash]); return { data: { scene: { id: 9 } } } }
  }
  const preview = (await api.previewSceneMerge(7, [2, 3])).data
  await api.applySceneMerge(7, preview.scene_ids, preview.source_hash)
  calls.push(['load'])
  assert.deepEqual(calls, [['preview', 7, [2, 3]], ['apply', 7, [2, 3], 'hard-hash'], ['load']])
  assert.match(view, /await api\.applySceneMerge\(projectID, sceneIDs, sourceHash\)[\s\S]*?await load\(\)[\s\S]*?selectedMergeSceneIDs\.value = \[\]/)
  assert.doesNotMatch(view.slice(view.indexOf('async function applySelectedSceneMerge'), view.indexOf('watch(activeEpN')), /allImages|allVideos|generateSceneImage|generateSceneVideo/)
})

test('merge confirmation is explicit, read-only, and stale responses are ignored', () => {
  assert.match(view, /合并内容（预览，只读，不进行 AI 重写）/)
  assert.match(view, /:value="sceneMergePreview\.content"[\s\S]*?readonly/)
  assert.match(view, /mergeBusy \|\| !sceneMergeConfirmed/)
  assert.match(view, /原分镜将归档且媒体保留；新分镜不带图片或视频/)
  assert.match(view, /requestEpoch !== sceneMergeEpoch \|\| projectID !== id\(\) \|\| episodeN !== activeEpN\.value/)
  assert.match(view, /mergeError\.value = error\.response\?\.data\?\.error/)
})
