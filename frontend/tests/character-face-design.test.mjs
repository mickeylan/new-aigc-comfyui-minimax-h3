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
