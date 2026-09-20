export function createDraftSafety({ key, getDraft, applyDraft, save, storage = globalThis.localStorage, eventTarget = globalThis.window, confirm = globalThis.confirm, debounceMs = 500, setTimer = setTimeout, clearTimer = clearTimeout }) {
  let baseline = JSON.stringify(getDraft())
  let timer
  const serialized = () => JSON.stringify(getDraft())
  const isDirty = () => serialized() !== baseline
  const flush = () => {
    clearTimer(timer)
    timer = undefined
    if (isDirty()) storage.setItem(key, serialized())
    else storage.removeItem(key)
  }
  const schedule = () => { clearTimer(timer); timer = setTimer(flush, debounceMs) }
  const markSaved = () => { baseline = serialized(); clearTimer(timer); timer = undefined; storage.removeItem(key) }
  const discard = () => storage.removeItem(key)
  const beforeUnload = event => { if (!isDirty()) return; flush(); event.preventDefault(); event.returnValue = '' }
  const keydown = event => {
    if (!(event.ctrlKey || event.metaKey) || String(event.key).toLowerCase() !== 's') return
    event.preventDefault()
    if (isDirty()) Promise.resolve(save()).then(markSaved)
  }
  const start = () => {
    const raw = storage.getItem(key)
    if (raw) {
      if (confirm('检测到未保存的本地草稿，是否恢复？')) {
        try { applyDraft(JSON.parse(raw)) } catch { discard() }
      } else discard()
    }
    eventTarget?.addEventListener('beforeunload', beforeUnload)
    eventTarget?.addEventListener('keydown', keydown)
  }
  const dispose = () => { clearTimer(timer); if (isDirty()) flush(); eventTarget?.removeEventListener('beforeunload', beforeUnload); eventTarget?.removeEventListener('keydown', keydown) }
  return { start, dispose, schedule, flush, markSaved, discard, isDirty }
}
