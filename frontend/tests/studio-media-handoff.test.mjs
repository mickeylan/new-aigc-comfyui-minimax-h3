import test from 'node:test'
import assert from 'node:assert/strict'

// sessionStorage 在 Node 里不存在：给 initial-media.js 一个最小的实现即可，
// 它只在函数体里读写，不在模块顶层触碰。
test('结果交接只生效一次，并在下次读取时清空', async () => {
  const store = new Map()
  globalThis.sessionStorage = {
    getItem: (key) => (store.has(key) ? store.get(key) : null),
    setItem: (key, value) => store.set(key, String(value)),
    removeItem: (key) => store.delete(key),
    clear: () => store.clear(),
  }
  const { stashStudioMedia, takeStudioMedia } = await import('../src/studio/initial-media.js')

  assert.equal(stashStudioMedia([]), false)
  assert.deepEqual(takeStudioMedia(), [])

  assert.equal(stashStudioMedia([{ url: '/api/output/0/a.mp4', name: 'a.mp4', kind: 'video' }, null, { name: 'no-url' }]), true)
  assert.deepEqual(takeStudioMedia(), [{ url: '/api/output/0/a.mp4', name: 'a.mp4', kind: 'video' }])
  // 同一批素材不能被刷新后的第二次挂载重复导入。
  assert.deepEqual(takeStudioMedia(), [])

  store.set('studio.initialMedia', '{not json')
  assert.deepEqual(takeStudioMedia(), [])
  assert.equal(store.size, 0)
})