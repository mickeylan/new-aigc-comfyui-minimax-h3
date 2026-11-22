import test from 'node:test'
import assert from 'node:assert/strict'
import { readFileSync } from 'node:fs'

import { materialList } from '../src/utils/materialList.js'

const detail = readFileSync(new URL('../src/views/ProjectDetail.vue', import.meta.url), 'utf8')

// 2026-11-22 修：GET /api/materials 返回裸数组，而弹窗只读 data.items，
// 于是「3D 结构参考视频」下拉框在真机里永远是空的（后端与 UI 都在，就是选不到）。
test('素材列表两种响应都解析：裸数组 / {items}', () => {
  const rows = [{ id: 40, name: 'clay-seq-a.mp4' }]
  assert.deepEqual(materialList(rows), rows)
  assert.deepEqual(materialList({ items: rows, total: 1 }), rows)
  assert.deepEqual(materialList({ items: null }), [])
  assert.deepEqual(materialList(null), [])
  assert.deepEqual(materialList(undefined), [])
  assert.deepEqual(materialList(''), [])
})

test('3D 结构参考视频下拉用 materialList 灌数据，不再只认 items', () => {
  assert.match(detail, /api\.materials\(\{ type: 'video', page_size: 100 \}\)/)
  assert.match(detail, /videoRef3DOptions\.value = materialList\(data\)/)
  assert.doesNotMatch(detail, /videoRef3DOptions\.value = data\?\.items \|\| \[\]/)
})

test('下拉选项提交的是素材文件名（后端按 materials/<name> 拼路径）', () => {
  assert.match(detail, /<option v-for="m in videoRef3DOptions" :key="m\.id" :value="m\.name">/)
})