import test from 'node:test'
import assert from 'node:assert/strict'
import { readFileSync } from 'node:fs'

const source = readFileSync(new URL('../src/views/ProjectDetail.vue', import.meta.url), 'utf8')
const resourceCode = source.slice(source.indexOf('async function loadScreenTextCues('), source.indexOf('function refreshScreenTextOnFocus'))
function fixture() {
  const activeEpN = { value: 1 }, screenTextCues = { value: [] }, calls = []
  let projectID = 1, disposed = false
  const api = { screenTextCues: (pid, ep) => new Promise((resolve, reject) => calls.push({ pid, ep, resolve, reject })) }
  const run = new Function('api', 'id', 'activeEpN', 'screenTextCues', 'isDisposed', `
    let screenTextLoadedKey='', screenTextRequestedKey='', screenTextRetryAt=0;
    const screenTextRequests=new Map();
    ${resourceCode.replaceAll('detailDisposed', 'isDisposed()')}
    return loadScreenTextCues;
  `)(api, () => projectID, activeEpN, screenTextCues, () => disposed)
  return { run, activeEpN, screenTextCues, calls, setProject: value => { projectID = value }, dispose: () => { disposed = true } }
}

test('功能文字多次轮询与并发刷新只请求一次，显式刷新才重取', async () => {
  const f = fixture()
  const first = f.run(), duplicate = f.run()
  assert.equal(f.calls.length, 1)
  f.calls[0].resolve({ data: [{ id: 11 }] })
  await Promise.all([first, duplicate])
  for (let i = 0; i < 20; i++) await f.run()
  assert.equal(f.calls.length, 1)
  const forced = f.run(true)
  assert.equal(f.calls.length, 2)
  f.calls[1].resolve({ data: [{ id: 12 }] })
  await forced
  assert.deepEqual(f.screenTextCues.value, [{ id: 12 }])
})

test('换集或换项目忽略旧响应，返回原集也会重新加载', async () => {
  const f = fixture()
  const old = f.run()
  f.activeEpN.value = 2
  const current = f.run()
  f.calls[1].resolve({ data: [{ id: 22 }] })
  await current
  f.calls[0].resolve({ data: [{ id: 11 }] })
  await old
  assert.deepEqual(f.screenTextCues.value, [{ id: 22 }])
  f.activeEpN.value = 1
  const back = f.run()
  assert.equal(f.calls.length, 3)
  f.calls[2].resolve({ data: [{ id: 13 }] })
  await back
  f.setProject(2)
  const other = f.run()
  assert.equal(f.calls[3].pid, 2)
  f.dispose()
  f.calls[3].resolve({ data: [{ id: 99 }] })
  await other
  assert.deepEqual(f.screenTextCues.value, [])
})

test('请求失败保留已有内容，普通轮询不会立即重复重试', async () => {
  const f = fixture()
  const initial = f.run()
  f.calls[0].resolve({ data: [{ id: 11 }] })
  await initial
  const force = f.run(true)
  f.calls[1].reject(new Error('offline'))
  await force
  for (let i = 0; i < 20; i++) await f.run()
  assert.equal(f.calls.length, 2)
  assert.deepEqual(f.screenTextCues.value, [{ id: 11 }])
})

test('旧WebSocket事件不再定时消费，项目请求合并且隐藏页不轮询', () => {
  assert.doesNotMatch(source, /wsTimer|if \(store\.lastEvent \|\| store\.lastProjectUpdate\)/)
  assert.match(source, /if \(projectLoadInFlight\) \{ projectLoadQueued = true; return \}/)
  assert.match(source, /projectLoadInFlight = false/)
  assert.match(source, /event\.project_id != null && Number\(event\.project_id\) !== Number\(id\(\)\)/)
  assert.match(source, /setInterval\(\(\) => \{ if \(document\.visibilityState !== 'hidden'\) load\(\) \}, 5000\)/)
  assert.match(source, /removeEventListener\('visibilitychange', refreshScreenTextOnFocus\)/)
})
