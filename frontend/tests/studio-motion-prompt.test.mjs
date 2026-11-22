import test from 'node:test'
import assert from 'node:assert/strict'
import { readFileSync } from 'node:fs'

const { motionPromptLine, appendMotionToPrompt, scenePromptChoices, projectChoices } = await import(
  '../src/studio/scene3d/motion-prompt.ts'
)

const stage = readFileSync(new URL('../src/studio/scene3d/Scene3DStage.tsx', import.meta.url), 'utf8')

const NOTE = '相机向右环绕 180°（半圈）；距离推近 40%'

test('运镜文案拼成一行：加「运镜：」前缀、句尾补句号', () => {
  assert.equal(motionPromptLine(NOTE), `运镜：${NOTE}。`)
})

test('句尾已有句号/分号时不叠标点', () => {
  assert.equal(motionPromptLine('相机推近 40%。'), '运镜：相机推近 40%。')
  assert.equal(motionPromptLine('相机推近 40%；'), '运镜：相机推近 40%。')
})

test('文案自带「运镜」开头就不再套第二层前缀', () => {
  assert.equal(motionPromptLine('运镜：相机上升 2 米'), '运镜：相机上升 2 米。')
})

test('空文案得到空行，追加时不改动原文', () => {
  assert.equal(motionPromptLine('   '), '')
  const result = appendMotionToPrompt('原提示词。', '  ')
  assert.deepEqual(result, { prompt: '原提示词。', added: false, line: '' })
})

test('追加到已有提示词末尾：换行分隔，原文一个字不动', () => {
  const result = appendMotionToPrompt('中国宫殿庭院，低多边形少年。', NOTE)
  assert.equal(result.added, true)
  assert.equal(result.line, `运镜：${NOTE}。`)
  assert.equal(result.prompt, `中国宫殿庭院，低多边形少年。\n运镜：${NOTE}。`)
  assert.ok(result.prompt.startsWith('中国宫殿庭院，低多边形少年。'))
})

test('提示词为空时追加结果就是这一行', () => {
  const result = appendMotionToPrompt('   ', NOTE)
  assert.equal(result.added, true)
  assert.equal(result.prompt, `运镜：${NOTE}。`)
})

test('已经有同一句时不重复写（added=false，提示词原样）', () => {
  const first = appendMotionToPrompt('场景描述。', NOTE)
  const again = appendMotionToPrompt(first.prompt, NOTE)
  assert.equal(again.added, false)
  assert.equal(again.prompt, first.prompt)
})

test('提示词里已有不带前缀的同一段运镜文案时也判定为重复', () => {
  const existing = `场景描述。\n相机向右环绕 180°（半圈）；距离推近 40%。`
  const result = appendMotionToPrompt(existing, NOTE)
  assert.equal(result.added, false)
  assert.equal(result.prompt, existing)
})

test('不同的运镜文案不算重复，照常追加', () => {
  const existing = `场景描述。\n运镜：相机向左环绕 90°（四分之一圈）。`
  const result = appendMotionToPrompt(existing, NOTE)
  assert.equal(result.added, true)
  assert.ok(result.prompt.includes('相机向左环绕 90°'))
  assert.ok(result.prompt.includes('相机向右环绕 180°'))
})

test('场次选择项带集/场序号与标题，读 video_full_prompt 优先', () => {
  const project = {
    scenes: [
      { id: 51, episode_number: 1, order_num: 1, title: '庭院起幅', video_full_prompt: '完整稿', video_prompt: '执行稿' },
      { id: 52, episode_number: 2, order_num: 7, title: '', video_prompt: '只有执行稿' },
    ],
  }
  const choices = scenePromptChoices(project)
  assert.deepEqual(choices, [
    { id: 51, label: '第 1 集 · 第 1 场 庭院起幅', prompt: '完整稿' },
    { id: 52, label: '第 2 集 · 第 7 场', prompt: '只有执行稿' },
  ])
})

test('真实接口的字段名（episode_n / order）也认', () => {
  const project = {
    scenes: [{ id: 51, episode_n: 1, order: 1, title: '场景1：云上天地界', video_full_prompt: '原稿' }],
  }
  assert.deepEqual(scenePromptChoices(project), [
    { id: 51, label: '第 1 集 · 第 1 场 场景1：云上天地界', prompt: '原稿' },
  ])
})

test('缺少 scenes / id 时返回空表，不抛错', () => {
  assert.deepEqual(scenePromptChoices(null), [])
  assert.deepEqual(scenePromptChoices({}), [])
  assert.deepEqual(scenePromptChoices({ scenes: [{ title: '没有 id' }] }), [])
})

test('项目下拉项把数字 id 转成字符串，名字缺省给「项目 N」', () => {
  assert.deepEqual(projectChoices([{ id: 1, name: '漫剧 A' }, { id: 2 }]), [
    { id: '1', label: '漫剧 A' },
    { id: '2', label: '项目 2' },
  ])
  assert.deepEqual(projectChoices(null), [])
})

test('项目列表接口的 { project: {…} } 包装也认，标题取 title', () => {
  assert.deepEqual(
    projectChoices([{ project: { id: 1, title: '舒寒' }, scenes: [] }, { project: { id: 3, title: '别的项目' } }]),
    [
      { id: '1', label: '舒寒' },
      { id: '3', label: '别的项目' },
    ],
  )
})

test('3D 页接线：运镜文案块里有「写进场景提示词」，追加只 PUT 提示词', () => {
  assert.ok(stage.includes('写进场景提示词'))
  assert.ok(stage.includes('appendMotionToPrompt'))
  assert.ok(stage.includes('scenePromptChoices'))
  assert.ok(stage.includes('/video/prompt`'))
  // 只追加 prompt，不改模板与首尾帧（模板/首尾帧是另一件事，得人来确认）。
  const put = stage.slice(stage.indexOf('appendNoteToScene'))
  assert.ok(put.includes('body: JSON.stringify({ prompt:'))
  assert.ok(!put.includes('first_frame_img'))
  assert.ok(!put.includes('ref3d_video'))
})