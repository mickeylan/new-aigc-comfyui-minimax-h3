import test from 'node:test'
import assert from 'node:assert/strict'
import { createDesignRequestGate, selectedCodePoints } from '../src/utils/designRequest.js'

test('旧请求在编辑、改回原值、卸载后均不能被采用', () => {
  const gate = createDesignRequestGate()
  const old = gate.capture()
  assert.equal(gate.accepts(old), true)
  gate.invalidate()
  gate.invalidate()
  assert.equal(gate.accepts(old), false)
  const fresh = gate.capture()
  assert.equal(gate.accepts(fresh), true)
  gate.dispose()
  assert.equal(gate.accepts(fresh), false)
  assert.equal(gate.accepts(gate.capture()), false)
})

test('浏览器UTF16选区转为码点偏移，精确定位重复词且不裁剪空白', () => {
  const text = ' 前😀青玉，中段青玉。尾部 \n'
  const start = text.lastIndexOf('青玉')
  assert.deepEqual(selectedCodePoints(text, start, start + 2), { selection: '青玉', selection_start: 8 })
  assert.deepEqual(selectedCodePoints(text, 0, text.length), { selection: text, selection_start: 0 })
  for (const [a,b] of [[0,0],[-1,3],[0,text.length+1],[3,4],[2,3]]) assert.equal(selectedCodePoints(text,a,b),null)
})
