import test from 'node:test'
import assert from 'node:assert/strict'
import { readFileSync } from 'node:fs'
const source = readFileSync(new URL('../src/views/ProjectEditor.vue', import.meta.url), 'utf8')
const code = source.slice(source.indexOf("let editorScreenTextLoadedKey = ''"), source.indexOf('function screenTextKindLabel'))
function fixture() {
  const calls = [], state = { ep: 1 }, cues = { value: [] }, preflights = []
  const api = {
    screenTextCues: (pid, ep) => new Promise(resolve => calls.push({ pid, ep, resolve })),
    characters: async () => ({ data: [] }), approvedFonts: async () => ({ data: { fonts: [] } })
  }
  const run = new Function('api', 'id', 'activeEpN', 'screenTextCues', 'characters', 'approvedFonts', 'loadScreenTextPreflight', 'toast', `${code}; return loadScreenTextCues`)(api, () => 1, { get value() { return state.ep } }, cues, { value: [] }, { value: [] }, async () => preflights.push(state.ep), { error: assert.fail })
  return { calls, state, cues, preflights, run }
}
test('剪辑台连续100次刷新仅加载一次功能文字和预检，保存强制刷新', async () => {
  const f = fixture()
  const pending = Array.from({ length: 100 }, () => f.run())
  assert.equal(f.calls.length, 1)
  f.calls[0].resolve({ data: [{ id: 1 }] })
  await Promise.all(pending)
  for (let i = 0; i < 100; i++) await f.run()
  assert.equal(f.calls.length, 1)
  assert.equal(f.preflights.length, 1)
  const refresh = f.run(true)
  assert.equal(f.calls.length, 2)
  f.calls[1].resolve({ data: [{ id: 2 }] })
  await refresh
  assert.deepEqual(f.cues.value, [{ id: 2 }])
})
test('换集忽略旧功能文字响应', async () => {
  const f = fixture(), old = f.run()
  f.state.ep = 2
  const current = f.run()
  f.calls[1].resolve({ data: [{ id: 22 }] })
  await current
  f.calls[0].resolve({ data: [{ id: 11 }] })
  await old
  assert.deepEqual(f.cues.value, [{ id: 22 }])
  assert.deepEqual(f.preflights, [2])
})
test('功能文字写入后刷新且API共用缓存，所有页面重复调用不能绕过', () => {
  const api = readFileSync(new URL('../src/api/index.js', import.meta.url), 'utf8')
  assert.match(source, /await loadScreenTextCues\(true\)/)
  assert.match(api, /screenTextCues: \(id, episodeN\) => screenTextRequests.list\(id, episodeN\)/)
  assert.match(api, /screenTextRequests.invalidate\(id\)/)
  for (const name of ['createScreenTextCue', 'updateScreenTextCue', 'deleteScreenTextCue']) assert.match(api, new RegExp(`${name}: .*mutateScreenText`))
})
