import test from 'node:test'
import assert from 'node:assert/strict'
import { readFileSync } from 'node:fs'

const source = readFileSync(new URL('../src/views/Skills.vue', import.meta.url), 'utf8')

test('Skill 卡片详情有可见弹窗和键盘入口', () => {
  assert.match(source, /@click="openSkill\(s\)"/)
  assert.match(source, /@keydown\.enter\.prevent="openSkill\(s\)"/)
  assert.match(source, /<Teleport to="body">/)
  assert.match(source, /class="skill-modal-mask"/)
  assert.match(source, /role="dialog"/)
  assert.match(source, /\.skill-modal-mask\s*\{[^}]*position:\s*fixed/s)
})

test('流水线 Skill 详情展示输入输出、执行步骤和 API', () => {
  for (const label of ['输入', '输出', '模型 / 工具', '关键参数', '触发方式', '执行步骤', '相关 API']) {
    assert.ok(source.includes(label), `详情缺少 ${label}`)
  }
})
