import test from 'node:test'
import assert from 'node:assert/strict'
import { globSync, readFileSync } from 'node:fs'

const store = readFileSync(new URL('../src/stores/toast.js', import.meta.url), 'utf8')
const app = readFileSync(new URL('../src/App.vue', import.meta.url), 'utf8')

test('错误Toast默认常驻并支持复制和手工关闭', () => {
  assert.match(store, /error\(msg, timeout\).*timeout \?\? 0/)
  assert.match(app, /copyToast\(t\.message\)/)
  assert.match(app, /toast\.dismiss\(t\.id\)/)
  assert.match(app, /navigator\.clipboard\.writeText/)
  assert.match(app, /user-select:\s*text/)
})

// 调用点用过 toast.warning，但 store 从未导出它，警告会抛 TypeError 并中断后续代码。
test('警告Toast有实现，且与错误一样常驻可复制', () => {
  assert.match(store, /warning\(msg, timeout\).*timeout \?\? 0/)
  assert.match(app, /t\.type === 'warning'/)
  assert.match(app, /warning: '!'/)
  assert.match(app, /\.toast-warning \.toast-icon/)
})

test('前端不调用 store 未实现的 toast 方法', () => {
  const sources = globSync('../src/**/*.{vue,js}', { cwd: new URL('.', import.meta.url) })
  const implemented = new Set(['show', 'success', 'error', 'warning', 'info', 'dismiss'])
  const missing = []
  for (const file of sources) {
    const text = readFileSync(new URL(file, import.meta.url), 'utf8')
    for (const [, method] of text.matchAll(/\btoast\.([a-zA-Z_$][\w$]*)\s*\(/g)) {
      if (!implemented.has(method)) missing.push(`${file}: toast.${method}()`)
    }
  }
  assert.deepEqual(missing, [])
})
