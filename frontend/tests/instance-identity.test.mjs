import test from 'node:test'
import assert from 'node:assert/strict'
import { readFileSync } from 'node:fs'

const view = readFileSync(new URL('../src/views/Instances.vue', import.meta.url), 'utf8')

test('实例操作使用稳定 instance id 并展示 host:port', () => {
  assert.match(view, /api\.startInstance\(inst\.id\)/)
  assert.match(view, /api\.stopInstance\(inst\.id\)/)
  assert.match(view, /api\.restartInstance\(inst\.id\)/)
  assert.match(view, /inst\.host \|\| '默认主机'/)
})
