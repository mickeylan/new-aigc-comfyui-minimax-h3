import test from 'node:test'
import assert from 'node:assert/strict'
import { createComfyExportSink } from '../src/studio/host/export-sink.js'

const artifact = {
  blob: new Blob([new Uint8Array([1, 2, 3])], { type: 'video/mp4' }),
  filename: 'Untitled project.mp4',
  mime: 'video/mp4',
  durationMs: 3000,
}

function fakeDiskSink() {
  const calls = []
  return {
    calls,
    async accept(_artifact, options) {
      calls.push(options?.saveToDisk === true)
      options?.onProgress?.({ destination: 'disk', status: 'success' })
      return 'Untitled project.mp4'
    },
  }
}

function fetchStub({ ok = true, status = 200 } = {}) {
  const calls = []
  const impl = async (url, init) => {
    calls.push({ url, init })
    return { ok, status, json: async () => ({ id: 7 }) }
  }
  return { calls, impl }
}

test('两个目的地都选时，磁盘与素材库各跑一次并各自上报进度', async () => {
  const disk = fakeDiskSink()
  const fetcher = fetchStub()
  const sink = createComfyExportSink({ diskSink: disk, fetchImpl: fetcher.impl, projectId: '12' })
  const events = []

  assert.equal(sink.supportsLibrary, true, '接了素材库后必须声明支持，否则面板会隐藏该选项')
  await sink.accept(artifact, {
    saveToDisk: true,
    saveToLibrary: true,
    onProgress: (e) => events.push(`${e.destination}:${e.status}`),
  })

  assert.deepEqual(disk.calls, [true])
  assert.equal(fetcher.calls.length, 1)
  assert.equal(fetcher.calls[0].url, '/api/materials')
  assert.equal(fetcher.calls[0].init.method, 'POST')
  const form = fetcher.calls[0].init.body
  assert.equal(form.get('type'), 'video')
  assert.equal(form.get('project_id'), '12')
  assert.equal(form.get('file').name, 'Untitled project.mp4')
  assert.deepEqual(events, ['disk:success', 'library:pending', 'library:success'])
})

test('只选磁盘时不碰素材库接口', async () => {
  const disk = fakeDiskSink()
  const fetcher = fetchStub()
  const sink = createComfyExportSink({ diskSink: disk, fetchImpl: fetcher.impl, projectId: '3' })

  await sink.accept(artifact, { saveToDisk: true, saveToLibrary: false })
  assert.equal(fetcher.calls.length, 0)
})

test('非数字项目 id 不入库到项目，素材仍然进素材库', async () => {
  const fetcher = fetchStub()
  const sink = createComfyExportSink({
    diskSink: fakeDiskSink(),
    fetchImpl: fetcher.impl,
    projectId: 'local-2f1c-uuid',
  })

  await sink.accept(artifact, { saveToDisk: false, saveToLibrary: true })
  const form = fetcher.calls[0].init.body
  assert.equal(form.get('project_id'), null, 'local-* 不是后端认识的数字主键')
  assert.equal(form.get('type'), 'video')
})

test('素材入库失败时上报 error 并抛出，不吞掉', async () => {
  const fetcher = fetchStub({ ok: false, status: 500 })
  const sink = createComfyExportSink({ diskSink: fakeDiskSink(), fetchImpl: fetcher.impl })
  const events = []

  await assert.rejects(
    () =>
      sink.accept(artifact, {
        saveToDisk: false,
        saveToLibrary: true,
        onProgress: (e) => events.push(`${e.destination}:${e.status}`),
      }),
    /素材入库失败：500/,
  )
  assert.deepEqual(events, ['library:pending', 'library:error'])
})

test('缺少 diskSink 时立刻报错，避免静默丢掉磁盘下载', () => {
  assert.throws(() => createComfyExportSink({}), /需要 diskSink/)
})