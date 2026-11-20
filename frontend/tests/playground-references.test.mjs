import test from 'node:test'
import assert from 'node:assert/strict'
import { readFileSync } from 'node:fs'
import {
  MAX_PLAYGROUND_REFERENCES,
  appendReferences,
  moveReference,
  referenceLabel,
  referenceSlots,
  removeReference,
} from '../src/utils/playgroundReferences.js'

const view = readFileSync(new URL('../src/views/Playground.vue', import.meta.url), 'utf8')
const meta = (name, taskId = 'playground-1') => ({ task_id: taskId, name })

test('追加参考图保留原有顺序，序号即 <imageN>', () => {
  const first = appendReferences([], [meta('sheet.png')])
  assert.deepEqual(first.rows.map(r => r.name), ['sheet.png'])
  const second = appendReferences(first.rows, [meta('net.png'), meta('prop.png')])
  assert.deepEqual(second.rows.map(r => r.name), ['sheet.png', 'net.png', 'prop.png'])
  assert.equal(second.skipped, 0)
  assert.deepEqual(referenceSlots(second.rows).map(s => s.label), ['<image1>', '<image2>', '<image3>'])
  assert.equal(referenceLabel(0), '<image1>')
  assert.equal(referenceLabel(15), '<image16>')
})

test('追加不会覆盖上一批（旧实现是覆盖，用户无法指定 <image1>）', () => {
  const existing = [meta('a.png')]
  const next = appendReferences(existing, [meta('b.png')])
  assert.equal(existing.length, 1, '入参数组不能被就地改写')
  assert.deepEqual(next.rows.map(r => r.name), ['a.png', 'b.png'])
})

test('参考图数量受模板上限约束，超出的部分被丢弃', () => {
  const full = Array.from({ length: 16 }, (_, i) => meta(`r${i}.png`))
  const over = appendReferences(full, [meta('extra.png')], MAX_PLAYGROUND_REFERENCES)
  assert.equal(over.rows.length, MAX_PLAYGROUND_REFERENCES)
  assert.equal(over.skipped, 1)
  const partial = appendReferences([meta('a.png')], [meta('b.png'), meta('c.png')], 2)
  assert.deepEqual(partial.rows.map(r => r.name), ['a.png', 'b.png'])
  assert.equal(partial.skipped, 1)
})

test('上移下移删除只改变顺序，越界调用保持原样', () => {
  const rows = [meta('a.png'), meta('b.png'), meta('c.png')]
  assert.deepEqual(moveReference(rows, 2, -1).map(r => r.name), ['a.png', 'c.png', 'b.png'])
  assert.deepEqual(moveReference(rows, 0, 1).map(r => r.name), ['b.png', 'a.png', 'c.png'])
  assert.deepEqual(moveReference(rows, 0, -1).map(r => r.name), ['a.png', 'b.png', 'c.png'])
  assert.deepEqual(moveReference(rows, 2, 1).map(r => r.name), ['a.png', 'b.png', 'c.png'])
  assert.deepEqual(removeReference(rows, 1).map(r => r.name), ['a.png', 'c.png'])
  assert.deepEqual(removeReference(rows, 9).map(r => r.name), ['a.png', 'b.png', 'c.png'])
  assert.deepEqual(rows.map(r => r.name), ['a.png', 'b.png', 'c.png'], '入参数组不能被就地改写')
})

test('缩略图地址走 /api/input/<bucket>/<name>，可直接预览', () => {
  const slots = referenceSlots([{ task_id: 'playground-1712345678', name: 'net.png' }])
  assert.equal(slots[0].url, '/api/input/playground-1712345678/net.png')
  assert.deepEqual(referenceSlots(undefined), [])
})

test('生成试验场界面显式展示参考图顺序并可调整', () => {
  assert.match(view, /顺序即提示词里的 &lt;image1&gt;…&lt;imageN&gt;/)
  assert.match(view, /第 1 张是 &lt;image1&gt;，第 2 张是 &lt;image2&gt;/)
  assert.match(view, /MiniMax H3 编辑模板写 &lt;Picture 1&gt;、&lt;Picture 2&gt;，顺序相同/)
  assert.match(view, /最多 \{\{ maxReferences \}\} 张/)
  assert.match(view, /v-for="\(slot, i\) in refSlots"/)
  assert.match(view, /\{\{ slot\.label \}\}/)
  assert.match(view, /@click="moveRef\(i, -1\)">上移/)
  assert.match(view, /@click="moveRef\(i, 1\)">下移/)
  assert.match(view, /@click="removeRef\(i\)">删除/)
  assert.match(view, /appendReferences\(files\.value\.ref_images,\s*rows,\s*limit\)/)
  assert.doesNotMatch(view, /files\.value=\{\s*\.\.\.files\.value,\s*\[slot\]:rows\s*\}/, '参考图必须追加而不是覆盖')
  assert.match(view, /maxReferences = computed/)
})