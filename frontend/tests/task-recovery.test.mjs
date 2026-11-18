import test from 'node:test'
import assert from 'node:assert/strict'
import { readFileSync } from 'node:fs'
import { connectTaskUpdates, taskRecoveryText } from '../src/utils/taskRecovery.js'

test('provider evidence distinguishes unknown, submitting, awaiting output and stop request', () => {
  assert.match(taskRecoveryText({status:'running', provider_outcome:'unknown'}), /未知/)
  assert.match(taskRecoveryText({status:'queued', provider_outcome:'submitting'}), /提交中/)
  assert.match(taskRecoveryText({status:'running', provider_outcome:'awaiting_result'}), /待取回/)
  assert.match(taskRecoveryText({status:'running', provider_outcome:'cancel_requested'}), /待确认/)
  assert.equal(taskRecoveryText({status:'failed',provider_outcome:'unknown'}), '')
})

test('reconnect only reloads reads and disposes callbacks', async () => {
  const sockets = [], timers = [], connected = []
  let reads = 0, messages = 0
  class Socket { constructor() { sockets.push(this) } close() { this.closed = true } }
  const stop = connectTaskUpdates({ url: ()=>'ws://fake', WebSocketImpl: Socket,
    reload: () => { reads++ }, onMessage:()=>{ messages++ }, onConnection:v=>connected.push(v),
    schedule:fn=>{timers.push(fn);return timers.length}, unschedule:()=>{} })
  sockets[0].onopen(); await Promise.resolve()
  assert.equal(reads,1)
  sockets[0].onclose(); assert.equal(connected.at(-1),false)
  timers[0](); assert.equal(sockets.length,2)
  sockets[0].onmessage({}); assert.equal(messages,0)
  sockets[1].onopen(); await Promise.resolve(); assert.equal(reads,2)
  sockets[1].onmessage({}); assert.equal(messages,1)
  const oldMessage = sockets[1].onmessage, oldOpen = sockets[1].onopen
  stop(); oldMessage({}); oldOpen(); await Promise.resolve()
  assert.equal(messages,1); assert.equal(reads,2); assert.equal(sockets[1].closed,true)
})

test('task pages expose explicit recovery and guard stale loads', () => {
  for (const file of ['Tasks','TaskDetail']) {
    const source = readFileSync(new URL(`../src/views/${file}.vue`, import.meta.url),'utf8')
    assert.match(source,/api\.reconcileTask\(/)
    assert.match(source,/connectTaskUpdates\(/)
    assert.match(source,/epoch !== loadEpoch/)
    assert.match(source,/disposed = true; loadEpoch\+\+/)
    assert.match(source,/不会.*提交|不重复提交/)
  }
  const detail = readFileSync(new URL('../src/views/TaskDetail.vue',import.meta.url),'utf8')
  assert.match(detail,/id !== route\.params\.id/)
  assert.match(detail,/route\.params\.id !== id \|\| epoch !== loadEpoch/)
})
