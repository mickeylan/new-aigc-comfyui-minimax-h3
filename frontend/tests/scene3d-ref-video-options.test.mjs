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
  assert.match(detail, /api\.materials\(\{ type: 'video' \}\)/)
  assert.match(detail, /videoRef3DOptions\.value = materialList\(data\)/)
  assert.doesNotMatch(detail, /videoRef3DOptions\.value = data\?\.items \|\| \[\]/)
})

test('参考视频超过100条仍完整且只包括当前项目与共享，拒绝过期响应', async () => {
  const start = detail.indexOf('    const referenceProjectID = Number(id())')
  const end = detail.indexOf('    await nextTick()', start)
  const source = detail.slice(start, end)
  const run = new Function('api', 'id', 'epoch', 'videoPromptRequestEpoch', 'videoRef3DOptions', 'materialList', source)
  const rows = Array.from({ length: 125 }, (_, i) => ({ id: i + 1, project_id: 7 }))
  rows.push({ id: 126, project_id: null }, { id: 127, project_id: 8 })
  for (const data of [rows, { items: rows }]) {
    const options = { value: [] }
    run({ materials: async params => { assert.deepEqual(params, { type: 'video' }); return { data } } }, () => 7, 1, 1, options, materialList)
    await new Promise(resolve => setImmediate(resolve))
    assert.equal(options.value.length, 126)
    assert.ok(!options.value.some(row => row.project_id === 8))
  }
  let resolve
  let project = 7
  const options = { value: [] }
  run({ materials: () => new Promise(r => { resolve = r }) }, () => project, 1, 1, options, materialList)
  project = 8
  options.value = [{ id: 'new-project' }]
  resolve({ data: rows })
  await new Promise(resolve => setImmediate(resolve))
  assert.deepEqual(options.value, [{ id: 'new-project' }])
})

test('下拉选项提交的是素材文件名（后端按 materials/<name> 拼路径）', () => {
  assert.match(detail, /<option v-for="m in videoRef3DOptions" :key="m\.id" :value="m\.name">/)
})