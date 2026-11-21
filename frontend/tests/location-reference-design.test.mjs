import test from 'node:test'
import assert from 'node:assert/strict'
import fs from 'node:fs'
import { createRequire } from 'node:module'

const read = path => fs.readFileSync(new URL(path, import.meta.url), 'utf8')
const source = read('../src/components/LocationReferenceDesign.vue')
const detail = read('../src/views/ProjectDetail.vue')
const api = read('../src/api/index.js')
const require = createRequire(new URL('../package.json', import.meta.url))

test('场景参考设计是可编译组件，只挂在场景资产卡片上', () => {
  const { parse, compileScript, compileTemplate } = require('@vue/compiler-sfc')
  const { descriptor, errors } = parse(source)
  assert.deepEqual(errors, [])
  assert.ok(compileScript(descriptor, { id: 'location-design' }).content)
  assert.deepEqual(compileTemplate({ source: descriptor.template.content, filename: 'LocationReferenceDesign.vue', id: 'location-design' }).errors, [])
  assert.match(detail, /<LocationReferenceDesign v-if="a\.kind === 'location'" :project-id="id\(\)" :location="a"/)
})

test('参考图按顺序编号，场景默认仅参考、人物道具默认必须入画', () => {
  assert.match(source, /function defaultSlot\(\)\{return config\.references\.length===0\?'scene':/)
  assert.match(source, /presence:slot==='scene'\?'optional':'required'/)
  assert.match(source, /value="scene"/)
  assert.match(source, /value="character"/)
  assert.match(source, /value="prop"/)
  assert.match(source, /Qwen 用 &lt;imageN&gt;，H3-Image 用 &lt;Picture N&gt;/)
  assert.match(source, /@click="move\(index,-1\)"/)
  assert.match(source, /@click="move\(index,1\)"/)
  assert.match(source, /这张图片已添加/)
})

test('容量与引擎由服务端给出，不静默丢参考图，提交必须勾选审阅', () => {
  assert.match(source, /e\.max_count/)
  assert.match(source, /不会自动删除或截断参考图/)
  assert.match(source, /:disabled="busy \|\| !reviewed \|\| !prompt\.trim\(\)"/)
  assert.match(source, /design_id:draft\.value\.id,prompt:prompt\.value/)
  assert.match(source, /我已核对参考图、职责及提示词/)
  assert.doesNotMatch(source, /max_count\s*:\s*(3|16|100)/)
})

test('修改参考图、顺序或引擎后必须重新预览', () => {
  assert.match(source, /watch\(config,.*draft\.value=null;reviewed\.value=false.*deep:true/)
  assert.match(source, /watch\(prompt,.*reviewed\.value=false/)
  assert.match(source, /须重新预览/)
})

test('Teleport 到 body 的弹窗自带遮罩定位', () => {
  const mask = source.match(/\.location-design-mask\{([^}]*)\}/)
  assert.ok(mask, '组件必须自己定义 .location-design-mask')
  assert.match(mask[1], /position:fixed/)
  assert.match(mask[1], /inset:0/)
  assert.match(mask[1], /display:flex/)
  assert.match(mask[1], /z-index:1400/)
})

test('按钮禁用时必须给出原因，结果进参考图版本不替换当前图', () => {
  assert.match(source, /:disabled="!!blockedReason" :title="blockedReason"/)
  assert.match(source, /请参考图任务正在生成|参考图任务正在生成/)
  assert.match(source, /当前场景图不会被自动替换/)
  assert.match(source, /result_variant_id/)
  for (const call of ['locationDesignOptions', 'uploadLocationDesignReference', 'previewLocationDesign', 'generateLocationDesign']) {
    assert.match(api, new RegExp(`${call}: \\(id, aid`))
  }
  for (const path of ['location-design`', 'location-design/references`', 'location-design/preview`', 'location-design/generate`']) {
    assert.ok(api.includes(path), `api/index.js 缺少 ${path}`)
  }
})