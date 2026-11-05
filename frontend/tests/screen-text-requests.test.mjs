import test from 'node:test'
import assert from 'node:assert/strict'
import { createScreenTextRequests } from '../src/utils/screenTextRequests.js'

const deferred = () => {
  let resolve, reject
  const promise = new Promise((yes, no) => { resolve = yes; reject = no })
  return { promise, resolve, reject }
}

test('100 repeated and concurrent callers share one request and successful cache', async () => {
  let calls = 0
  const pending = deferred()
  const requests = createScreenTextRequests({ fetch: () => { calls++; return pending.promise } })
  const results = Array.from({ length: 100 }, () => requests.list(7, 2))
  await Promise.resolve()
  assert.equal(calls, 1)
  pending.resolve(['cue'])
  assert.deepEqual(await Promise.all(results), Array(100).fill(['cue']))
  assert.deepEqual(await requests.list('7', '2'), ['cue'])
  assert.equal(calls, 1)
})

test('successful values expire after TTL', async () => {
  let clock = 10
  let calls = 0
  const requests = createScreenTextRequests({ fetch: async () => ++calls, now: () => clock, ttl: 60 })
  assert.equal(await requests.list(1, 1), 1)
  clock = 69
  assert.equal(await requests.list(1, 1), 1)
  clock = 70
  assert.equal(await requests.list(1, 1), 2)
})

test('projects and an omitted episode use isolated keys', async () => {
  let calls = 0
  const requests = createScreenTextRequests({ fetch: async (id, episode) => [++calls, id, episode] })
  await requests.list(1)
  await requests.list(1, 'all')
  await requests.list(2)
  await requests.list('1', undefined)
  assert.equal(calls, 3)
})

test('project invalidation during pending request prevents stale cache repopulation', async () => {
  let calls = 0
  const old = deferred()
  const requests = createScreenTextRequests({ fetch: () => ++calls === 1 ? old.promise : Promise.resolve('fresh') })
  const staleResult = requests.list(4, 3)
  await Promise.resolve()
  requests.invalidate(4)
  assert.equal(await requests.list(4, 3), 'fresh')
  old.resolve('stale')
  assert.equal(await staleResult, 'stale')
  assert.equal(await requests.list(4, 3), 'fresh')
  assert.equal(calls, 2)
})

test('episode invalidation leaves other project episodes cached', async () => {
  let calls = 0
  const requests = createScreenTextRequests({ fetch: async () => ++calls })
  await requests.list(1, 1)
  await requests.list(1, 2)
  requests.invalidate('1', '1')
  assert.equal(await requests.list(1, 1), 3)
  assert.equal(await requests.list(1, 2), 2)
})

test('rejections propagate and are cached only for the failure cooldown', async () => {
  let clock = 0
  let calls = 0
  const failure = new Error('offline')
  const requests = createScreenTextRequests({
    fetch: () => { calls++; throw failure },
    now: () => clock,
    errorTtl: 30
  })
  await assert.rejects(requests.list(1, 1), failure)
  await assert.rejects(requests.list(1, 1), failure)
  assert.equal(calls, 1)
  clock = 30
  await assert.rejects(requests.list(1, 1), failure)
  assert.equal(calls, 2)
})

test('cache remains bounded without timers', async () => {
  let calls = 0
  const requests = createScreenTextRequests({ fetch: async () => ++calls })
  for (let episode = 1; episode <= 51; episode++) await requests.list(1, episode)
  await requests.list(1, 1)
  assert.equal(calls, 52)
})
