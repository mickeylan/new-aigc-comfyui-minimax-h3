import test from 'node:test'
import assert from 'node:assert/strict'
import { readFileSync } from 'node:fs'
const source = readFileSync(new URL('../src/views/ProjectEditor.vue', import.meta.url), 'utf8')

test('离开剪辑台后在途刷新仍使用原项目ID，不能读到新路由的空ID', () => {
  const code = source.slice(source.indexOf('const editorProjectId'), source.indexOf('function outputUrl'))
  const route = { params: { id: '7' } }
  const id = new Function('route', `${code}; return id`)(route)
  route.params = {} // 已跳转到 /materials
  assert.equal(id(), '7')
  assert.doesNotMatch(source, /function id\(\) \{ return route\.params\.id \}/)
})

test('首次加载期间离开剪辑台不再启动轮询，后续子请求也停止', () => {
  const mounted = source.slice(source.indexOf('onMounted(async () => {\n  const requestedEpisode'), source.indexOf('onUnmounted('))
  assert.ok(mounted.indexOf('if (editorDisposed) return') > 0 && mounted.indexOf('if (editorDisposed) return') < mounted.indexOf('setInterval'))
  assert.match(source, /onUnmounted\(\(\) => \{ editorDisposed = true;/)
  const load = source.slice(source.indexOf('async function load() {'), source.indexOf('async function loadMerges'))
  assert.ok(load.indexOf('if (editorDisposed) return') < load.indexOf('loadCandidates()'))
})
