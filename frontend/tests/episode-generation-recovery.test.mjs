import test from 'node:test'
import assert from 'node:assert/strict'
import { generateWithEpisodeRecovery } from '../src/utils/episodeGenerationRecovery.js'
const conflict = () => Object.assign(new Error('conflict'), { response: { data: { error: '第3集映射与前集实际剧情冲突：来源对白已在前集全部使用' } } })
const preview = { token: 'snapshot', start_episode: 3, unplanned_episode: 10, items: [{ episode_n: 3, previous_title: '神力反杀', title: '血仇与远行', adaptation_goal: '战后远行', mapping: { opening_state: '三人已死', ending_state: '离开荒地' } }] }
test('真实生成入口：冲突后预览、确认、CAS应用，再按同一集号生成一次', async () => {
  let calls = 0
  const requests = []
  const result = await generateWithEpisodeRecovery({ episode: 3,
    generate: async () => { calls++; if(calls === 1) throw conflict(); return 'episode3' },
    reflow: async data => { requests.push(data); return {data: preview} },
    confirm: text => { assert.match(text, /保留第1–2集/); assert.match(text, /血仇与远行/); assert.match(text, /第10集留作待规划/); return true }
  })
  assert.equal(result, 'episode3'); assert.equal(calls, 2)
  assert.deepEqual(requests, [{start_episode:3}, {start_episode:3,token:'snapshot',apply:true}])
})
test('拒绝确认不应用、不再次生成', async () => {
  const requests = []; let calls = 0
  await assert.rejects(generateWithEpisodeRecovery({ episode:3,generate:async()=>{calls++;throw conflict()},reflow:async data=>{requests.push(data);return {data:preview}},confirm:()=>false }))
  assert.equal(calls,1); assert.equal(requests.length,1)
})
test('非规划冲突不修复；CAS失败不生成；重复冲突不循环', async () => {
  let reflows = 0
  await assert.rejects(generateWithEpisodeRecovery({episode:3,generate:async()=>{throw new Error('provider failed')},reflow:()=>{reflows++},confirm:()=>true}), /provider failed/)
  assert.equal(reflows,0)
  let calls=0
  await assert.rejects(generateWithEpisodeRecovery({episode:3,generate:async()=>{calls++;throw conflict()},reflow:async data=>{if(data.apply)throw new Error('stale');return {data:preview}},confirm:()=>true}),/stale/)
  assert.equal(calls,1)
  calls=0
  await assert.rejects(generateWithEpisodeRecovery({episode:3,generate:async()=>{calls++;throw conflict()},reflow:async()=>({data:preview}),confirm:()=>true}))
  assert.equal(calls,2)
})
