const MAX_ENTRIES = 50
const ALL_EPISODES = null

function normalize(value) {
  return String(value)
}

function normalizeEpisode(episode) {
  return episode === undefined || episode === null || episode === ''
    ? ALL_EPISODES
    : normalize(episode)
}

/**
 * Share screen-text list requests between component instances.
 * `fetch` has the same `(projectId, episode)` shape as api.screenTextCues.
 */
export function createScreenTextRequests({
  fetch,
  now = Date.now,
  ttl = 60000,
  errorTtl = 30000
}) {
  if (typeof fetch !== 'function') throw new TypeError('fetch must be a function')

  const entries = new Map()
  const keyFor = (id, episode) => JSON.stringify([normalize(id), normalizeEpisode(episode)])
  const projectFor = id => normalize(id)

  function removeOldest() {
    while (entries.size > MAX_ENTRIES) entries.delete(entries.keys().next().value)
  }

  function list(id, episode) {
    const key = keyFor(id, episode)
    const existing = entries.get(key)
    const time = now()

    if (existing && (existing.pending || time < existing.expiresAt)) {
      // Map insertion order doubles as bounded LRU order without changing TTL.
      entries.delete(key)
      entries.set(key, existing)
      return existing.promise
    }
    if (existing) entries.delete(key)

    const entry = {
      project: projectFor(id),
      pending: true,
      expiresAt: Infinity,
      promise: null
    }
    entry.promise = Promise.resolve()
      .then(() => fetch(id, episode))
      .then(
        value => {
          if (entries.get(key) === entry) {
            entry.pending = false
            entry.expiresAt = now() + ttl
          }
          return value
        },
        error => {
          if (entries.get(key) === entry) {
            entry.pending = false
            entry.expiresAt = now() + errorTtl
          }
          throw error
        }
      )

    entries.set(key, entry)
    removeOldest()
    return entry.promise
  }

  function invalidate(id, episode) {
    const project = projectFor(id)
    if (episode !== undefined) {
      entries.delete(keyFor(id, episode))
      return
    }
    for (const [key, entry] of entries) {
      if (entry.project === project) entries.delete(key)
    }
  }

  return { list, invalidate }
}
