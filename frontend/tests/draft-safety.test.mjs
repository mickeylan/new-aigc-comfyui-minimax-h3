import test from 'node:test'
import assert from 'node:assert/strict'
import { createDraftSafety } from '../src/utils/draftSafety.js'

function harness(initial) {
  const values = new Map(initial ? [['draft', JSON.stringify(initial)]] : [])
  const handlers = {}
  return {
    storage: { getItem: k => values.get(k) ?? null, setItem: (k, v) => values.set(k, v), removeItem: k => values.delete(k) },
    target: { addEventListener: (n, fn) => { handlers[n] = fn }, removeEventListener: n => { delete handlers[n] } },
    values, handlers
  }
}

test('draft safety restores an accepted local draft and discards a rejected one', () => {
  let state = { text: 'server' }
  const h = harness({ text: 'local' })
  const safety = createDraftSafety({ key: 'draft', getDraft: () => state, applyDraft: d => { state = d }, save() {}, storage: h.storage, eventTarget: h.target, confirm: () => true })
  safety.start()
  assert.equal(state.text, 'local')
  safety.dispose()

  const rejected = harness({ text: 'old' })
  createDraftSafety({ key: 'draft', getDraft: () => state, applyDraft: d => { state = d }, save() {}, storage: rejected.storage, eventTarget: rejected.target, confirm: () => false }).start()
  assert.equal(rejected.storage.getItem('draft'), null)
})

test('draft safety debounces persistence, guards unload, and handles Cmd+S', async () => {
  let state = { text: 'a' }; let queued; let saves = 0
  const h = harness()
  const safety = createDraftSafety({ key: 'draft', getDraft: () => state, applyDraft() {}, save: async () => { saves++ }, storage: h.storage, eventTarget: h.target, confirm: () => false, setTimer: fn => { queued = fn; return 1 }, clearTimer() {} })
  safety.start(); state.text = 'b'; safety.schedule(); queued()
  assert.equal(JSON.parse(h.storage.getItem('draft')).text, 'b')
  const unload = { preventDefault() { this.prevented = true } }; h.handlers.beforeunload(unload)
  assert.equal(unload.prevented, true); assert.equal(unload.returnValue, '')
  const key = { key: 's', metaKey: true, preventDefault() { this.prevented = true } }; h.handlers.keydown(key)
  await Promise.resolve(); await Promise.resolve()
  assert.equal(key.prevented, true); assert.equal(saves, 1); assert.equal(h.storage.getItem('draft'), null)
  safety.dispose()
})
