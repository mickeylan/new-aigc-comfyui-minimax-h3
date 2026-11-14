import test from 'node:test'
import assert from 'node:assert/strict'
import { readFileSync } from 'node:fs'
import { directorDraftShotPayload, episodeShotSplitSkipReason, shotSplitBatchActive, shotSplitBatchOpen, shotSplitReport, shotSplitStatusLabel } from '../src/utils/episodeShotSplit.js'

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

test('后端拆镜批次按结果分组并关联场景；已删除场景仍按id显示', () => {
  const detail = { batch: { id: 9, status: 'running' }, items: [
    { id: 1, scene_id: 1, order: 1, status: 'saved', shot_count: 2 },
    { id: 2, scene_id: 2, order: 2, status: 'skipped', skip_reason: '已有导演Shot' },
    { id: 3, scene_id: 3, order: 3, status: 'failed', error: 'provider failed' },
    { id: 4, scene_id: 4, order: 4, status: 'running' },
    { id: 5, scene_id: 99, order: 5, status: 'queued' },
    { id: 6, scene_id: 6, order: 6, status: 'cancelled' },
  ] }
  const report = shotSplitReport(detail, [scene(1), scene(2), scene(3), scene(4)])
  assert.deepEqual(report.saved.map(r => [r.scene.id, r.item.shot_count]), [[1, 2]])
  assert.deepEqual(report.skipped.map(r => r.item.skip_reason), ['已有导演Shot'])
  assert.deepEqual(report.failed.map(r => r.item.error), ['provider failed'])
  assert.deepEqual(report.pending.map(r => r.scene.id), [4, 99])
  assert.equal(report.pending[1].scene.order, 5)
})

test('拆镜批次状态：只有排队/运行中需要轮询，暂停仍占用入口', () => {
  assert.equal(shotSplitBatchActive({ status: 'running' }), true)
  assert.equal(shotSplitBatchActive({ status: 'paused' }), false)
  assert.equal(shotSplitBatchOpen({ status: 'paused' }), true)
  assert.equal(shotSplitBatchOpen({ status: 'failed' }), false)
  assert.equal(shotSplitBatchOpen(null), false)
  assert.equal(shotSplitStatusLabel('paused'), '已暂停')
})

test('分镜工作区通过后端批次按剧情拆镜并轮询进度，不再在浏览器内逐场循环', () => {
  const source = readFileSync(new URL('../src/views/ProjectDetail.vue', import.meta.url), 'utf8')
  assert.match(source, /api\.createShotSplitBatch\(id\(\), activeEpN\.value\)/)
  assert.match(source, /api\.getShotSplitBatch\(/)
  assert.match(source, /api\.controlShotSplitBatch\(id\(\), batch\.episode_n, batch\.id, action\)/)
  assert.match(source, /clearTimeout\(shotSplitTimer\)/)
  assert.doesNotMatch(source, /splitEpisodeShots\(/)
  assert.match(source, /按剧情拆镜/)
  const api = readFileSync(new URL('../src/api/index.js', import.meta.url), 'utf8')
  assert.match(api, /\/episodes\/\$\{number\}\/shot-split\/\$\{bid\}\/\$\{action\}/)
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
