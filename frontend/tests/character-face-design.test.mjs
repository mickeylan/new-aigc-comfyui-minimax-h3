import test from 'node:test'
import assert from 'node:assert/strict'
import fs from 'node:fs'
import { createRequire } from 'node:module'

const read = path => fs.readFileSync(new URL(path, import.meta.url), 'utf8')
const source = read('../src/components/CharacterFaceDesign.vue')
const detail = read('../src/views/ProjectDetail.vue')
const require = createRequire(new URL('../package.json', import.meta.url))

test('两个参考人脸预设共用一个可编译的组件和标准像入口', () => {
  const { parse, compileScript, compileTemplate } = require('@vue/compiler-sfc')
  const { descriptor, errors } = parse(source)
  assert.deepEqual(errors, [])
  assert.ok(compileScript(descriptor, { id: 'face-design' }).content)
  assert.deepEqual(compileTemplate({ source: descriptor.template.content, filename: 'CharacterFaceDesign.vue', id: 'face-design' }).errors, [])
  assert.match(source, /value="dual_parent"/)
  assert.match(source, /value="multi_face_fusion"/)
  assert.match(detail, /<CharacterFaceDesign :project-id="id\(\)" :character="ch"/)
})

test('参考图可上传或选择项目图片，有序职责变动必须重新审阅', () => {
  assert.match(source, /api\.uploadFaceDesignReference/)
  assert.match(source, /选择项目已有图片/)
  assert.match(source, /v-model="r\.responsibility"/)
  assert.match(source, /watch\(config,.*draft\.value=null;reviewed\.value=false.*deep:true/)
  assert.match(source, /watch\(prompt,.*reviewed\.value=false/)
  assert.match(source, /@click="move\(index,-1\)"/)
  assert.match(source, /@click="move\(index,1\)"/)
})

test('引擎容量来自服务端且不静默丢参考图，提交须人工确认', () => {
  assert.match(source, /e\.max_count/)
  assert.match(source, /不会自动删除或截断参考图/)
  assert.match(source, /:disabled="busy \|\| !reviewed \|\| !prompt\.trim\(\)"/)
  assert.match(source, /design_id:draft\.value\.id,prompt:prompt\.value/)
  assert.doesNotMatch(source, /max_count\s*:\s*(3|16|100)/)
})

test('设计来源记录展示具体参考图片、顺序、职责与任务', () => {
  assert.match(source, /设计来源记录/)
  for (const field of ['r.file', 'r.position', 'r.responsibility', 'd.task_id', 'd.prompt']) assert.ok(source.includes(field))
  assert.match(source, /移除选择不会删除原始图片/)
})

test('Teleport 到 body 的弹窗自带遮罩定位，不会掉到页面底部', () => {
  // ProjectDetail 的 .modal-mask 是 scoped 规则，作用域哈希对不上 Teleport 出来的节点，
  // 缺失定位时弹窗会渲染在文档末尾，表现为“点了没反应”。
  const mask = source.match(/\.face-design-mask\{([^}]*)\}/)
  assert.ok(mask, '组件必须自己定义 .face-design-mask')
  assert.match(mask[1], /position:fixed/)
  assert.match(mask[1], /inset:0/)
  assert.match(mask[1], /display:flex/)
  assert.match(mask[1], /z-index:1400/)
})

test('按钮禁用时必须给出原因，不能点了毫无反应', () => {
  assert.match(source, /:disabled="!!blockedReason" :title="blockedReason"/)
  assert.match(source, /const blockedReason=computed/)
  assert.match(source, /请先审核通过角色档案/)
  assert.match(source, /标准像任务正在生成/)
  assert.match(source, /当前有其它角色操作正在进行/)
  assert.doesNotMatch(source, /:disabled="disabled \|\| character\.profile_status !== 'approved' \|\| !!character\.portrait_task_id"/)
})
