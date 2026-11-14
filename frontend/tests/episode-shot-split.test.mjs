import test from 'node:test'
import assert from 'node:assert/strict'
import { readFileSync } from 'node:fs'
import { directorDraftShotPayload, episodeShotSplitSkipReason, splitEpisodeShots } from '../src/utils/episodeShotSplit.js'

const scene = (id, extra = {}) => ({ id, content: `第${id}场正文`, status: 'pending', shot_count: 0, ...extra })

test('整集拆镜跳过已有Shot、已出片、生成中和空正文的场景', () => {
  assert.equal(episodeShotSplitSkipReason(scene(1)), '')
  assert.equal(episodeShotSplitSkipReason(scene(2, { shot_count: 3 })), '已有导演Shot')
  assert.equal(episodeShotSplitSkipReason(scene(3, { status: 'video_ready' })), '视频已就绪')
  assert.equal(episodeShotSplitSkipReason(scene(4, { status: 'video_running' })), '正在生成')
  assert.equal(episodeShotSplitSkipReason(scene(5, { content: '  ' })), '场景正文为空')
})

test('草稿镜头去掉审计字段并补齐对白呈现字段', () => {
  const payload = directorDraftShotPayload({ shots: [{ description: 'A', checks: ['x'], speech_presentation: 'visible_speaker_lipsync' }, { description: 'B' }] })
  assert.deepEqual(payload, [
    { description: 'A', speech_presentation: 'visible_speaker_lipsync', listener_character: '' },
    { description: 'B', speech_presentation: '', listener_character: '' },
  ])
})

test('逐场生成并携带台词快照保存；单场失败不中断整集', async () => {
  const saves = []
  const progress = []
  const result = await splitEpisodeShots({
    scenes: [scene(1), scene(2, { shot_count: 2 }), scene(3), scene(4)],
    generate: async sc => {
      if (sc.id === 3) throw new Error('provider failed')
      return { data: { draft: { shots: [{ description: `${sc.id}-1` }, { description: `${sc.id}-2` }] }, dialogue_snapshot: `snap-${sc.id}` } }
    },
    save: async (sc, shots, snapshot) => { saves.push([sc.id, shots.length, snapshot]) },
    onProgress: p => progress.push([p.done, p.total]),
  })
  assert.deepEqual(saves, [[1, 2, 'snap-1'], [4, 2, 'snap-4']])
  assert.deepEqual(result.saved.map(r => r.scene.id), [1, 4])
  assert.deepEqual(result.skipped.map(r => [r.scene.id, r.reason]), [[2, '已有导演Shot']])
  assert.deepEqual(result.failed.map(r => [r.scene.id, r.error.message]), [[3, 'provider failed']])
  assert.deepEqual(progress, [[0, 3], [1, 3], [2, 3], [3, 3]])
})

test('空草稿记为失败且不保存；取消后不再请求', async () => {
  let saves = 0
  const empty = await splitEpisodeShots({ scenes: [scene(1)], generate: async () => ({ data: { draft: { shots: [] } } }), save: async () => { saves++ } })
  assert.equal(saves, 0)
  assert.equal(empty.failed[0].error.message, '导演草稿没有镜头')
  let calls = 0
  let stop = false
  const stopped = await splitEpisodeShots({ scenes: [scene(1), scene(2)], generate: async () => { calls++; stop = true; return { data: { draft: { shots: [{ description: 'A' }] } } } }, save: async () => {}, cancelled: () => stop })
  assert.equal(calls, 1)
  assert.equal(stopped.cancelled, true)
})

test('分镜工作区提供整集按剧情拆镜入口并复用单场导演草稿接口', () => {
  const source = readFileSync(new URL('../src/views/ProjectDetail.vue', import.meta.url), 'utf8')
  assert.match(source, /splitEpisodeShots\(/)
  assert.match(source, /api\.sceneDirectorDraft\(id\(\), sc\.id, \{ mode: 'standard' \}\)/)
  assert.match(source, /api\.replaceSceneShots\(id\(\), sc\.id, shots, snapshot\)/)
  assert.match(source, /按剧情拆镜/)
})

test('剪辑台视觉节拍复用导演草稿接口并携带台词快照保存', () => {
  const source = readFileSync(new URL('../src/views/ProjectEditor.vue', import.meta.url), 'utf8')
  const run = source.slice(source.indexOf('async function runVisualBeats'), source.indexOf('async function runFirstFramePolish'))
  assert.match(run, /api\.sceneDirectorDraft\(id\(\), selected\.value\.id, \{ mode: 'standard' \}\)/)
  assert.doesNotMatch(run, /api\.visualBeatDraft/)
  const apply = source.slice(source.indexOf('async function applyVisualBeats'), source.indexOf('async function applyPolishDraft'))
  assert.match(apply, /directorDraftShotPayload\(visualBeatResult\?\.draft\)/)
  assert.match(apply, /visualBeatResult\?\.dialogue_snapshot/)
})
