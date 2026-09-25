import test from 'node:test'
import assert from 'node:assert/strict'
import { readFileSync } from 'node:fs'

const store = readFileSync(new URL('../src/stores/toast.js', import.meta.url), 'utf8')
const app = readFileSync(new URL('../src/App.vue', import.meta.url), 'utf8')

test('错误Toast默认常驻并支持复制和手工关闭', () => {
  assert.match(store, /error\(msg, timeout\).*timeout \?\? 0/)
  assert.match(app, /copyToast\(t\.message\)/)
  assert.match(app, /toast\.dismiss\(t\.id\)/)
  assert.match(app, /navigator\.clipboard\.writeText/)
  assert.match(app, /user-select:\s*text/)
})
