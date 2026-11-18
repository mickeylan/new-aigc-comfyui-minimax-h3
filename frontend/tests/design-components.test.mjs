import test from 'node:test'
import assert from 'node:assert/strict'
import fs from 'node:fs'
import { pathToFileURL } from 'node:url'
import { createRequire } from 'node:module'
const require = createRequire(new URL('../package.json', import.meta.url))
const { parse, compileScript } = require('@vue/compiler-sfc')
const { createRenderer, reactive, nextTick } = require('vue')

async function component(name, renderTemplate = false) {
  const url = new URL(`../src/components/${name}.vue`, import.meta.url)
  const { descriptor } = parse(fs.readFileSync(url, 'utf8'))
  let source = compileScript(descriptor, { id: name, inlineTemplate: renderTemplate }).content
  source = source.replace(/from ['"]vue['"]/g, `from '${pathToFileURL(require.resolve('vue/dist/vue.runtime.esm-bundler.js')).href}'`)
    .replace(/import \{ api \} from ['"]\.\.\/api['"]/g, 'const api = new Proxy({}, { get: (_, key) => (...args) => globalThis.__designTestAPI[key](...args) })')
    .replace(/import DesignQuestions from [^\n]+/g, 'const DesignQuestions = {}')
    .replace(/from ['"](\.\.\/utils\/[^'"]+)['"]/g, (_, path) => `from '${new URL(path, url).href}'`)
  const module = await import(`data:text/javascript;base64,${Buffer.from(source).toString('base64')}`)
  return renderTemplate ? module.default : { ...module.default, render: () => null }
}
const renderer = createRenderer({
  createElement: tag => ({ tag, tagName: tag.toUpperCase(), children: [], props: {}, options: [], addEventListener() {}, setAttribute() {}, removeAttribute() {} }), createText: text => ({ text }), createComment: () => ({}),
  setText(node, text) { node.text = text }, setElementText(node, text) { node.text = text }, patchProp(node, key, old, value) { node.props[key] = value }, parentNode: node => node.parent || null, nextSibling: () => null,
  insert(child, parent) { child.parent = parent; parent.children.push(child) }, remove(node) { if (node.parent) node.parent.children = node.parent.children.filter(child => child !== node) }
})
function mount(Component, props) {
  const app = renderer.createApp(Component, props)
  const instance = app.mount({ children: [] })
  return { state: instance.$.setupState, app }
}

test('真实模板的两个AI入口空闲可点击、请求中禁用、完成后恢复', async () => {
  let finish
  globalThis.__designTestAPI = { assetDesign: () => new Promise(resolve => { finish = resolve }) }
  const Component = await component('AssetDesignPanel', true)
  const root = { children: [] }
  const app = renderer.createApp(Component, { projectId: 1, kind: 'prop', name: '玉佩' })
  app.config.warnHandler = () => {} // The question child is intentionally stubbed in this focused render.
  app.mount(root)
  const nodes = node => [node, ...(node.children || []).flatMap(nodes)]
  const buttons = () => nodes(root).filter(node => node.tag === 'button' && /AI 生成第一稿|AI 出稿中|先给我 3 个方向|思考中/.test(node.text || ''))
  assert.equal(buttons().length, 2)
  assert.ok(buttons().every(button => button.props.disabled === false))
  buttons()[0].props.onClick()
  await nextTick()
  assert.ok(buttons().every(button => button.props.disabled === true))
  finish({ data: { description: '测试稿', sources: [], changes: [] } })
  await new Promise(resolve => setTimeout(resolve, 0)); await nextTick()
  assert.ok(buttons().every(button => button.props.disabled === false))
  app.unmount()
})

test('角色草稿先预览、不修改表单；采用只改所选字段，支持撤销，手工编辑使旧稿失效', async () => {
  let submitted
  globalThis.__designTestAPI = { async previewCharacterProfile(...args) { submitted = args[2]; return { data: { patch: { appearance: '新发型' } } } } }
  const Component = await component('CharacterProfileDesign')
  const form = reactive({ appearance: '原发型', personality: '沉静', reference_prompt: '已确认提示词' })
  const { state, app } = mount(Component, { projectId: 1, characterId: 2, form, onAdopt: patch => Object.assign(form, patch) })
  state.instruction = '只改发型'
  await state.generate()
  assert.equal(form.appearance, '原发型')
  assert.deepEqual(submitted.fields, ['appearance'])
  assert.equal(submitted.current.reference_prompt, undefined)
  assert.equal(state.history.length, 1)
  state.adopt()
  assert.equal(form.appearance, '新发型')
  assert.equal(form.personality, '沉静')
  assert.equal(form.reference_prompt, '已确认提示词')
  state.rollback()
  assert.equal(form.appearance, '原发型')
  form.appearance = '人工稿'
  state.adopt()
  assert.equal(form.appearance, '人工稿')
  app.unmount()
})

test('请求期间编辑再改回也失效；卸载后响应不会添加草稿', async () => {
  let finish
  globalThis.__designTestAPI = { previewCharacterProfile: () => new Promise(resolve => { finish = resolve }) }
  const Component = await component('CharacterProfileDesign')
  const form = reactive({ appearance: '原稿' })
  const { state, app } = mount(Component, { projectId: 1, characterId: 2, form })
  state.instruction = '修改'
  let pending = state.generate()
  form.appearance = '人工编辑'; form.appearance = '原稿'
  finish({ data: { patch: { appearance: '迟到的结果' } } }); await pending
  assert.equal(state.history.length, 0)
  assert.equal(state.busy, false)
  pending = state.generate()
  app.unmount()
  finish({ data: { patch: { appearance: '关闭后结果' } } }); await pending
  assert.equal(state.history.length, 0)
})

test('场景道具旧响应不会新增稿子或长期方向，局部请求携带精确位置', async () => {
  let finish, submitted
  globalThis.__designTestAPI = { assetDesign: (...args) => { submitted = args[3]; return new Promise(resolve => { finish = resolve }) } }
  const Component = await component('AssetDesignPanel')
  const directions = []
  const { state, app } = mount(Component, { projectId: 1, kind: 'prop', name: '玉佩', assetId: 3, description: '前青玉，中青玉。', 'onUpdate:directions': text => directions.push(text) })
  state.importDescription()
  state.instruction = '只改第二处'
  state.selection = '青玉'; state.selectionStart = 5; state.onlySelection = true
  const pending = state.call('revise', state.request({ current: state.active.description, selection: state.selection, selection_start: state.selectionStart, instruction: state.instruction }), data => { state.pushDraft(data, 'ai'); state.addDirection('旧方向') })
  assert.equal(submitted.selection_start, 5)
  state.instruction = '新方向'; state.instruction = '只改第二处'
  finish({ data: { description: '旧结果', changes: [] } }); await pending
  assert.equal(state.drafts.length, 1)
  assert.deepEqual(directions, [])
  assert.equal(state.busy, '')
  app.unmount()
})

test('问答跳过不等于确认；显式确认才发出文本、单选与多选答案', async () => {
  const Component = await component('DesignQuestions')
  const answers = []
  const { state, app } = mount(Component, { kind: 'prop', onAnswer: text => answers.push(text) })
  state.focus = '青玉'
  state.skip()
  assert.deepEqual(answers, [])
  assert.equal(state.focus, '')
  state.approach = '只改指定部分'; state.focus = '保留玉佩'; state.priorities = ['材质细节', '身份辨识度']
  state.submit(); await nextTick()
  assert.equal(answers.length, 1)
  assert.match(answers[0], /修改方式：只改指定部分/)
  assert.match(answers[0], /优先关注：材质细节、身份辨识度/)
  state.submit()
  assert.equal(answers.length, 1)
  app.unmount()
})
