// A monotonic revision rejects ABA edits and requests completed after disposal.
export function createDesignRequestGate() {
  let revision = 0
  let disposed = false
  return {
    capture: () => revision,
    invalidate() { revision++ },
    accepts(token) { return !disposed && token === revision },
    dispose() { disposed = true; revision++ }
  }
}

export function selectedCodePoints(text, startUTF16, endUTF16) {
  if (!Number.isInteger(startUTF16) || !Number.isInteger(endUTF16) || startUTF16 < 0 || endUTF16 <= startUTF16 || endUTF16 > text.length) return null
  const selection = text.slice(startUTF16, endUTF16)
  const start = Array.from(text.slice(0, startUTF16)).length
  if (Array.from(text).slice(start, start + Array.from(selection).length).join('') !== selection) return null
  return { selection, selection_start: start }
}
