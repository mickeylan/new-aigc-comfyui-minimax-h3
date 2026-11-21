import test from 'node:test'
import assert from 'node:assert/strict'
import { buildSceneWriteback } from '../src/studio/host/scene-writeback.js'

function recorder(response) {
  const calls = []
  return {
    calls,
    fetchImpl: async (url, init) => {
      calls.push({ url, init })
      return response
    },
  }
}

const okResponse = (body) => ({ ok: true, status: 200, json: async () => body })

test('分镜图写回走 image/upload 并带 as_candidate=1', async () => {
  const { calls, fetchImpl } = recorder(okResponse({ message: '已保存为新的分镜图候选并设为当前；旧版本可在候选中切回' }))
  const writeBack = buildSceneWriteback({ projectId: '3', sceneId: 12, mode: 'sceneImage' }, { fetchImpl })
  const message = await writeBack(new Blob(['png'], { type: 'image/png' }), 'canvas.png')

  assert.equal(calls.length, 1)
  assert.equal(calls[0].url, '/api/projects/3/scenes/12/image/upload')
  assert.equal(calls[0].init.method, 'POST')
  const form = calls[0].init.body
  assert.equal(form.get('as_candidate'), '1')
  assert.equal(form.get('file').name, 'canvas.png')
  assert.match(message, /新的分镜图候选/)
})

test('衔接帧写回走 frames/replace，不带 as_candidate', async () => {
  const { calls, fetchImpl } = recorder(okResponse({}))
  const writeBack = buildSceneWriteback({ projectId: 3, sceneId: 12, mode: 'frame', frameId: 88 }, { fetchImpl })
  const message = await writeBack(new Blob(['png'], { type: 'image/png' }))

  assert.equal(calls[0].url, '/api/projects/3/scenes/12/continuity/frames/replace')
  const form = calls[0].init.body
  assert.equal(form.get('as_candidate'), null)
  assert.equal(form.get('file').name, 'canvas.png')
  assert.equal(message, '已替换衔接帧（原图保留）')
})

test('上下文不完整时不给写回入口', () => {
  assert.equal(buildSceneWriteback(null), null)
  assert.equal(buildSceneWriteback({ sceneId: 1 }), null)
  assert.equal(buildSceneWriteback({ projectId: 1 }), null)
  // 衔接帧模式缺 frameId：后端只会替换「当前选中帧」，没有帧就无法安全写回。
  assert.equal(buildSceneWriteback({ projectId: 1, sceneId: 2, mode: 'frame' }), null)
})

test('后端拒绝时把错误原文抛给用户', async () => {
  const { fetchImpl } = recorder({ ok: false, status: 400, json: async () => ({ error: '仅支持有效的 PNG/JPG/WebP 图片' }) })
  const writeBack = buildSceneWriteback({ projectId: 1, sceneId: 2 }, { fetchImpl })
  await assert.rejects(() => writeBack(new Blob(['x'])), /仅支持有效的 PNG\/JPG\/WebP 图片/)
})

test('非 JSON 错误响应也能给出可读提示', async () => {
  const { fetchImpl } = recorder({ ok: false, status: 502, json: async () => { throw new Error('not json') } })
  const writeBack = buildSceneWriteback({ projectId: 1, sceneId: 2 }, { fetchImpl })
  await assert.rejects(() => writeBack(new Blob(['x'])), /HTTP 502/)
})