// A view-only filter: preserve original objects, selection keys and ordering.
export function filterReferenceCandidates(candidates, query) {
  const needle = String(query || '').trim().normalize('NFKC').toLocaleLowerCase()
  if (!needle) return candidates
  return candidates.filter(item => [item.label, item.name, item.character_name, item.asset_name]
    .some(value => String(value || '').normalize('NFKC').toLocaleLowerCase().includes(needle)))
}
