import test from 'node:test'
import assert from 'node:assert/strict'
import { readFileSync } from 'node:fs'

const create = readFileSync(new URL('../src/views/ProjectNew.vue', import.meta.url), 'utf8')
const projects = readFileSync(new URL('../src/views/Projects.vue', import.meta.url), 'utf8')
const detail = readFileSync(new URL('../src/views/ProjectDetail.vue', import.meta.url), 'utf8')

test('项目创建防止重复提交并携带幂等令牌', () => {
  assert.match(create, /if \(creating\.value\) return/)
  assert.match(create, /create_token: createToken\.value/)
  assert.match(create, /randomUUID/)
})

test('项目列表和详情均提供可反馈错误的删除入口', () => {
  assert.match(projects, /@click\.stop="removeProject\(item\.project\)"/)
  assert.match(projects, /删除项目失败/)
  assert.match(detail, /删除项目失败/)
})
