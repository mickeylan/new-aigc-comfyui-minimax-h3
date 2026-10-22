import test from 'node:test'
import assert from 'node:assert/strict'
import fs from 'node:fs'

const page=fs.readFileSync(new URL('../src/views/Extensibility.vue',import.meta.url),'utf8')
const router=fs.readFileSync(new URL('../src/router/index.js',import.meta.url),'utf8')
const api=fs.readFileSync(new URL('../src/api/index.js',import.meta.url),'utf8')
const catalog=fs.readFileSync(new URL('../src/views/ModelCatalog.vue',import.meta.url),'utf8')

test('扩展能力有只读状态页且不提供生成适配器提交入口',()=>{
  assert.match(router,/path: '\/extensibility'/)
  assert.match(page,/所有扩展默认关闭/)
  assert.match(page,/没有真实模板和校验通过的适配器时不会开放提交/)
  assert.match(api,/extensibilityStatus/)
  assert.match(api,/generationAdapters/)
  assert.doesNotMatch(api,/submitGenerationAdapter/)
})

test('模型目录展示可选能力声明但保留模板派生事实',()=>{
  assert.match(catalog,/声明式能力附加信息/)
  assert.match(catalog,/item\.capability\.provider_family/)
  assert.match(catalog,/item\.supports_first_frame/)
})
