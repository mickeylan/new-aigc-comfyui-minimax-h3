import test from 'node:test'
import assert from 'node:assert/strict'
import { resolveExportDestinations } from '../src/studio/video-editor/lib/export/destinations.js'

// 真实缺陷复现：浏览器下载 sink 没有媒体库，但面板默认勾选 library，
// 导出结束固定显示 "Library upload failed"，用户会误以为导出失败。

test('宿主没有媒体库时，即使界面勾着也不交给 sink', () => {
  const d = resolveExportDestinations({ supportsLibrary: false, saveToDisk: true, saveToLibrary: true })
  assert.deepEqual(d, { disk: true, library: false, scene: false, canExport: true })
})

test('supportsLibrary 缺省视为不支持', () => {
  const d = resolveExportDestinations({ saveToDisk: true, saveToLibrary: true })
  assert.equal(d.library, false)
  assert.equal(d.canExport, true)
})

test('宿主支持媒体库时按用户勾选传递', () => {
  assert.deepEqual(
    resolveExportDestinations({ supportsLibrary: true, saveToDisk: false, saveToLibrary: true }),
    { disk: false, library: true, scene: false, canExport: true },
  )
  assert.deepEqual(
    resolveExportDestinations({ supportsLibrary: true, saveToDisk: true, saveToLibrary: false }),
    { disk: true, library: false, scene: false, canExport: true },
  )
})

test('两个目的地都关掉时禁止导出', () => {
  const d = resolveExportDestinations({ supportsLibrary: true, saveToDisk: false, saveToLibrary: false })
  assert.equal(d.canExport, false)
})

test('不支持媒体库且用户取消存盘时，cannot export 而不是静默失败', () => {
  const d = resolveExportDestinations({ supportsLibrary: false, saveToDisk: false, saveToLibrary: true })
  assert.deepEqual(d, { disk: false, library: false, scene: false, canExport: false })
})