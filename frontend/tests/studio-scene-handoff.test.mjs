import test from 'node:test'
import assert from 'node:assert/strict'
import { resolveExportDestinations } from '../src/studio/video-editor/lib/export/destinations.js'
import { createComfyExportSink } from '../src/studio/host/export-sink.js'

// node:test 没有 sessionStorage，用一个最小的内存实现跑交接协议。
function installSessionStorage() {
  const store = new Map()
  globalThis.sessionStorage = {
    getItem: (key) => (store.has(key) ? store.get(key) : null),
    setItem: (key, value) => store.set(key, String(value)),
    removeItem: (key) => store.delete(key),
  }
  return store
}

test('分镜上下文一次性交接并在读取后清空', async () => {
  installSessionStorage()
  const { stashStudioScene, takeStudioScene } = await import('../src/studio/initial-media.js')
  assert.equal(stashStudioScene({ sceneId: 7, projectId: 3, episodeN: 2, sceneLabel: '场景 7 · 门', returnTo: '/projects/3/editor?episode=2' }), true)
  const taken = takeStudioScene()
  assert.deepEqual(taken, {
    projectId: '3',
    sceneId: 7,
    episodeN: 2,
    sceneLabel: '场景 7 · 门',
    returnTo: '/projects/3/editor?episode=2',
    mode: '',
    frameId: null,
    imageUrl: '',
  })
  // 二次读取必须为空：刷新页面不能重复写回同一个分镜。
  assert.equal(takeStudioScene(), null)
  // 缺 project/scene 的上下文一律不落盘，避免写出 /scenes/undefined。
  assert.equal(stashStudioScene({ sceneId: 7 }), false)
  assert.equal(stashStudioScene(null), false)
})

// 画布需要知道写回目标与原始画面；衔接帧模式还要带上 frameId。
test('画布上下文带上模式、帧与原始画面地址', async () => {
  installSessionStorage()
  const { stashStudioScene, takeStudioScene } = await import('../src/studio/initial-media.js')
  assert.equal(
    stashStudioScene({ projectId: 3, sceneId: 12, mode: 'frame', frameId: 88, imageUrl: '/api/input/3/frame.png' }),
    true,
  )
  const frame = takeStudioScene()
  assert.equal(frame.mode, 'frame')
  assert.equal(frame.frameId, 88)
  assert.equal(frame.imageUrl, '/api/input/3/frame.png')

  assert.equal(stashStudioScene({ projectId: 3, sceneId: 12, mode: 'sceneImage', frameId: 99, imageUrl: '/a.png' }), true)
  const image = takeStudioScene()
  assert.equal(image.mode, 'sceneImage')
  // 分镜图模式不记帧号，避免写回时误走衔接帧接口。
  assert.equal(image.frameId, null)
  // 未知模式退回空模式（画布仍可打开，只是不显示写回按钮）。
  assert.equal(stashStudioScene({ projectId: 3, sceneId: 12, mode: 'unknown' }), true)
  assert.equal(takeStudioScene().mode, '')
})

test('写回分镜只在宿主声明 supportsScene 时可用', () => {
  const onlyScene = resolveExportDestinations({ supportsScene: true, saveToDisk: false, saveToLibrary: false, saveToScene: true })
  assert.deepEqual(onlyScene, { disk: false, library: false, scene: true, canExport: true })
  const withoutContext = resolveExportDestinations({ supportsScene: false, saveToDisk: false, saveToLibrary: false, saveToScene: true })
  assert.equal(withoutContext.scene, false)
  assert.equal(withoutContext.canExport, false)
  // 未勾选写回时不去动分镜。
  const optedOut = resolveExportDestinations({ supportsScene: true, saveToScene: false })
  assert.equal(optedOut.scene, false)
  assert.equal(optedOut.canExport, true)
})

test('带分镜上下文时导出会 POST 到 scene video upload 并带 as_candidate', async () => {
  const calls = []
  const sink = createComfyExportSink({
    diskSink: { accept: async () => null },
    projectId: '3',
    scene: { projectId: '3', sceneId: 7 },
    fetchImpl: async (url, init) => {
      calls.push({ url, init })
      return { ok: true, status: 201, json: async () => ({ ok: true, candidate_id: 11 }) }
    },
  })
  assert.equal(sink.supportsLibrary, true)
  assert.equal(sink.supportsScene, true)

  const events = []
  const artifact = { blob: new Blob(['x'], { type: 'video/mp4' }), filename: '精修版.mp4', mime: 'video/mp4' }
  await sink.accept(artifact, {
    saveToDisk: false,
    saveToLibrary: false,
    saveToScene: true,
    onProgress: (event) => events.push(event),
  })

  assert.equal(calls.length, 1)
  assert.equal(calls[0].url, '/api/projects/3/scenes/7/video/upload')
  const form = calls[0].init.body
  assert.equal(form.get('as_candidate'), '1')
  assert.equal(form.get('file').name, '精修版.mp4')
  assert.equal(form.get('type'), null, '写回分镜不应带素材库的 type 字段')
  assert.deepEqual(events, [
    { destination: 'scene', status: 'pending' },
    { destination: 'scene', status: 'success' },
  ])
})

test('没有分镜上下文时不提供写回，也不会误传分镜请求', async () => {
  const calls = []
  const sink = createComfyExportSink({
    diskSink: { accept: async () => null },
    projectId: '3',
    fetchImpl: async (url) => {
      calls.push(url)
      return { ok: true, status: 200, json: async () => ({}) }
    },
  })
  assert.equal(sink.supportsScene, false)
  const artifact = { blob: new Blob(['x'], { type: 'video/mp4' }), filename: 'clip.mp4', mime: 'video/mp4' }
  const events = []
  await sink.accept(artifact, { saveToDisk: false, saveToScene: true, onProgress: (event) => events.push(event) })
  assert.deepEqual(calls, [])
  assert.deepEqual(events, [])
})

test('写回失败会把后端错误原样报给用户', async () => {
  const sink = createComfyExportSink({
    diskSink: { accept: async () => null },
    scene: { projectId: 3, sceneId: 7 },
    fetchImpl: async () => ({ ok: false, status: 400, json: async () => ({ error: '仅支持有效的 MP4/WebM/MOV/MKV 视频' }) }),
  })
  const events = []
  await assert.rejects(
    () => sink.accept({ blob: new Blob(['x']), filename: 'bad.mp4', mime: 'video/mp4' }, { saveToDisk: false, saveToScene: true, onProgress: (event) => events.push(event) }),
    /仅支持有效的 MP4\/WebM\/MOV\/MKV 视频/,
  )
  assert.deepEqual(events.at(-1), { destination: 'scene', status: 'error', error: '仅支持有效的 MP4/WebM/MOV/MKV 视频' })
})