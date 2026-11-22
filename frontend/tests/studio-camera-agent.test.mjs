import test from 'node:test'
import assert from 'node:assert/strict'
import { readFileSync } from 'node:fs'

const {
  parseCameraBrief,
  answerCameraQuestion,
  questionsFor,
  describeMove,
  planCameraMoves,
} = await import('../src/studio/scene3d/camera-agent.ts')

const stage = readFileSync(new URL('../src/studio/scene3d/Scene3DStage.tsx', import.meta.url), 'utf8')

const anchor = { position: [0, 2, 8], target: [0, 1, 0], fov: 45 }

test('一圈：认出环绕并给出圈数，不再追问', () => {
  const intent = parseCameraBrief('镜头绕角色转一圈')
  assert.equal(intent.moves.length, 1)
  assert.equal(intent.moves[0].kind, 'orbit')
  assert.equal(intent.moves[0].turns, 1)
  assert.deepEqual(intent.questions, [])
})

test('半圈 / 四分之一圈 / N 度都换算成圈数', () => {
  assert.equal(parseCameraBrief('绕半圈').moves[0].turns, 0.5)
  assert.equal(parseCameraBrief('环绕四分之一圈').moves[0].turns, 0.25)
  assert.equal(parseCameraBrief('环绕 90°').moves[0].turns, 0.25)
})

test('只说「环绕」没给圈数 → 追问一圈多少', () => {
  const intent = parseCameraBrief('环绕主角')
  assert.equal(intent.moves[0].kind, 'orbit')
  assert.equal(intent.moves[0].turns, undefined)
  assert.deepEqual(intent.questions, ['环绕多少？（半圈 / 一圈 / 90°）'])
})

test('推近带百分比；拉远记成负数', () => {
  assert.equal(parseCameraBrief('推近 40%').moves[0].percent, 40)
  assert.equal(parseCameraBrief('镜头拉远 30%').moves[0].kind, 'dolly')
  assert.equal(parseCameraBrief('镜头拉远 30%').moves[0].percent, -30)
})

test('句子里的一个百分比属于这句话的动作，不会被别的段抢走', () => {
  const intent = parseCameraBrief('先绕半圈，再推近 20%')
  assert.deepEqual(
    intent.moves.map((move) => move.kind),
    ['orbit', 'dolly'],
  )
  assert.equal(intent.moves[0].turns, 0.5)
  assert.equal(intent.moves[1].percent, 20)
})

test('升降按米记，下降记成负数；摇镜按度记', () => {
  assert.equal(parseCameraBrief('升空 3 米').moves[0].meters, 3)
  assert.equal(parseCameraBrief('机位下降 2 米').moves[0].meters, -2)
  const pan = parseCameraBrief('原地右摇 45°')
  assert.equal(pan.moves[0].kind, 'pan')
  assert.equal(pan.moves[0].degrees, 45)
  assert.equal(pan.moves[0].direction, 1)
})

test('希区柯克优先于推近：同一句不重复算两段', () => {
  const intent = parseCameraBrief('希区柯克变焦，推近 30%')
  assert.deepEqual(
    intent.moves.map((move) => move.kind),
    ['hitchcock'],
  )
})

test('一次只问一个：先填圈数，再轮到百分比', () => {
  let intent = parseCameraBrief('环绕再推近')
  assert.equal(intent.questions.length, 2)
  intent = answerCameraQuestion(intent, '半圈')
  assert.equal(intent.moves[0].turns, 0.5)
  assert.deepEqual(intent.questions, ['推近还是拉远、多少？（如 40%）'])
  intent = answerCameraQuestion(intent, '30%')
  assert.equal(intent.moves[1].percent, 30)
  assert.deepEqual(intent.questions, [])
})

test('答非所问时不瞎猜：问题原样留着', () => {
  const intent = parseCameraBrief('环绕主角')
  const after = answerCameraQuestion(intent, '好看一点')
  assert.deepEqual(after.questions, intent.questions)
  assert.equal(after.moves[0].turns, undefined)
})

test('认不出动作时给一句示范，不产出机位', () => {
  const intent = parseCameraBrief('随便来点炫的')
  assert.deepEqual(intent.moves, [])
  assert.match(intent.questions[0], /没认出动作/)
  const plan = planCameraMoves(intent, anchor)
  assert.deepEqual(plan.keys, [])
})

test('圈数有了就能出机位：环绕半圈给 1 + 8 个（含起幅），末点绕到对面', () => {
  const intent = parseCameraBrief('绕半圈，4 秒')
  const plan = planCameraMoves(intent, anchor)
  assert.deepEqual(plan.problems, [])
  assert.equal(plan.keys.length, 9)
  assert.deepEqual(plan.keys[0].position, anchor.position)
  const last = plan.keys[plan.keys.length - 1]
  assert.ok(Math.abs(last.position[0]) < 0.01, `末点应绕到 Z 轴对面，实际 x=${last.position[0]}`)
  assert.ok(last.position[2] < -7.9, `末点 z 应到 -8 附近，实际 ${last.position[2]}`)
})

test('紧跟环绕的推近从环绕落点起步，不跳回起点', () => {
  const intent = parseCameraBrief('绕半圈，然后推近 50%')
  const plan = planCameraMoves(intent, anchor)
  assert.deepEqual(plan.problems, [])
  const orbitEnd = plan.keys[8].position
  assert.ok(Math.abs(orbitEnd[2] + 8) < 0.05)
  const dollyEnd = plan.keys[plan.keys.length - 1]
  // 从 z≈-8 推近一半 → z≈-4（仍朝目标）
  assert.ok(Math.abs(dollyEnd.position[2] + 4) < 0.2, `推近终点 z≈-4，实际 ${dollyEnd.position[2]}`)
  assert.ok(Math.abs(dollyEnd.fov - 45) < 0.001, '普通推近不改视角')
})

test('希区柯克会改视角：推近 50% 时视角变大', () => {
  const intent = parseCameraBrief('希区柯克变焦，推近 50%')
  const plan = planCameraMoves(intent, anchor)
  assert.deepEqual(plan.problems, [])
  const last = plan.keys[plan.keys.length - 1]
  assert.ok(last.fov > 45.5, `视角应变大，实际 ${last.fov}`)
})

test('升降只改高度，摇镜只转视线：位置与目标各动一个', () => {
  const crane = planCameraMoves(parseCameraBrief('升空 3 米'), anchor)
  const craneLast = crane.keys[crane.keys.length - 1]
  assert.ok(Math.abs(craneLast.position[1] - 5) < 0.01)
  assert.ok(Math.abs(craneLast.position[0]) < 0.01 && Math.abs(craneLast.position[2] - 8) < 0.01)

  const pan = planCameraMoves(parseCameraBrief('原地右摇 45°'), anchor)
  const panLast = pan.keys[pan.keys.length - 1]
  assert.deepEqual(panLast.position, [0, 2, 8])
  // 面向 -Z 时向右摇 = 视点往 +X 移（与「向右环绕」同一个符号约定）
  assert.ok(panLast.target[0] > 2, `右摇 45° 后视点应偏到 +x，实际 ${panLast.target[0]}`)
})

test('跟拍没有 A/B 路径时拒绝出机位并说清要先点什么', () => {
  const plan = planCameraMoves(parseCameraBrief('跟拍 6 秒'), anchor, null)
  assert.deepEqual(plan.keys, [])
  assert.match(plan.problems[0], /A、B/)
})

test('跟拍有路径时沿路径采样，机位与视点一起走', () => {
  const path = (t) => [0, 0, 4 - 8 * t]
  const plan = planCameraMoves(parseCameraBrief('跟拍 6 秒'), anchor, path)
  assert.deepEqual(plan.problems, [])
  const last = plan.keys[plan.keys.length - 1]
  // 相机保持与起点相同的偏移（x=0, z=+4），视点落到 B
  assert.ok(Math.abs(last.target[2] + 4) < 0.05, `视点应到 B（z≈-4），实际 ${last.target[2]}`)
  assert.ok(Math.abs(last.position[2]) < 0.05, `相机应比 B 后 4 米，实际 z=${last.position[2]}`)
})

test('预览说明按段给人话，时长缺省带默认值', () => {
  assert.equal(describeMove({ kind: 'orbit', turns: 0.5 }), '向右环绕 0.5 圈（4 秒）')
  assert.equal(describeMove({ kind: 'dolly', percent: -30 }), '拉远 30%（3 秒）')
  assert.equal(describeMove({ kind: 'crane', meters: 2 }), '升高 2 米（3 秒）')
  assert.match(describeMove({ kind: 'hitchcock', percent: 20 }), /希区柯克/)
})

test('问题清单直接来自缺失参数，顺序跟段序一致', () => {
  const intent = parseCameraBrief('环绕，再升空，再跟拍')
  assert.deepEqual(questionsFor(intent.moves), intent.questions)
  assert.equal(intent.questions.length, 3)
})

test('编辑器接线：Agent 面板存在并可预览/采用/撤销', () => {
  assert.match(stage, /camera-agent/)
  assert.match(stage, /相机 Agent/)
  assert.match(stage, /parseCameraBrief/)
  assert.match(stage, /planCameraMoves/)
  assert.match(stage, /answerCameraQuestion/)
  assert.match(stage, /撤销/)
})