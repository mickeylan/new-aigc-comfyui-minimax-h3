export function setDialogueItemExcluded(item, excluded) {
  if (!item) return
  item.excluded = Boolean(excluded)
}

export function snapshotDialogueExclusions(items = []) {
  return new Map(items.map(item => [`${item.index}\u0000${item.text}`, Boolean(item.excluded)]))
}

export function overlayDialogueExclusions(items = [], snapshot = new Map()) {
  return items.map(item => {
    const key = `${item.index}\u0000${item.text}`
    return snapshot.has(key) ? { ...item, excluded: snapshot.get(key) } : { ...item }
  })
}
