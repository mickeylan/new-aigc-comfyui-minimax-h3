import test from 'node:test'
import assert from 'node:assert/strict'
import { readFileSync } from 'node:fs'

const read = (p) => readFileSync(new URL(p, import.meta.url), 'utf8')
const stage3d = read('../src/studio/scene3d/Scene3DStage.tsx')

// Character Identity Transfer：不新增模板，复用 Qwen-Image 2.1 多图编辑。
// 关键是两张参考图的顺序——prompt 用 <image1>/<image2> 指代，
// 顺序错了脸和机位就装反。
test('身份迁移复用 qwen_image_2_1_multi_edit，不新建模板', () => {
  assert.match(
    stage3d,
    /findTemplateId\("qwen_image_2_1_multi_edit"\)/,
    '应复用现成的多图编辑模板',
  )
  assert.doesNotMatch(
    stage3d,
    /image_identity_transfer/,
    '不该为身份迁移单开模板',
  )
})

test('ref_images 顺序固定：image1 是 3D 快照，image2 是角色主形象', () => {
  const block = stage3d.match(
    /ref_images:\s*\[([\s\S]{0,200}?)\]/,
  )
  assert.ok(block, '缺少 ref_images 参数')
  const order = block[1]
  const snapshotAt = order.indexOf('options.snapshot.task_id')
  const characterAt = order.indexOf('options.character.task_id')
  assert.ok(snapshotAt >= 0, '快照必须进 ref_images')
  assert.ok(characterAt >= 0, '角色主形象必须进 ref_images')
  assert.ok(snapshotAt < characterAt, '快照要排在前面，才会是 <image1>')
})

test('出图前要求先写提示词，并说明两个占位指代', () => {
  assert.match(stage3d, /identityPrompt\.trim\(\)/, '空提示词不该提交任务')
  assert.match(stage3d, /<image1>/, '应说明 <image1> 指什么')
  assert.match(stage3d, /<image2>/, '应说明 <image2> 指什么')
})

test('3D 身份迁移快照来自当前相机的干净WebGL取景', () => {
  assert.match(stage3d, /const snapshotBlob = await captureCurrentView\(\)/)
  assert.match(stage3d, /const shot = await captureViewportAt\(/)
  assert.match(stage3d, /context\.drawImage\(renderer\.domElement, 0, 0, width, height\)/)
  assert.match(stage3d, /hide\(gridRef\.current\)/)
  assert.match(stage3d, /hide\(transformRef\.current\?\.getHelper/)
})

test('默认提示词是已批准的那一段，逐字不变', () => {
  const block = stage3d.match(/const DEFAULT_IDENTITY_PROMPT = `([\s\S]*?)`/)
  assert.ok(block, '缺少默认提示词常量')
  const prompt = block[1].replace(/\s+/g, ' ').trim()
  assert.equal(
    prompt,
    '以<image2>的人物身份替换<image1>中的主体，保持<image1>的机位、构图、 景别与光影不变；脸型、五官、发型与服装以<image2>为准； 输出写实摄影风格，不要 3D 渲染感、不要塑料皮肤。',
    '默认提示词被改动了，改措辞要先重新获批',
  )
  assert.match(stage3d, /useState\(DEFAULT_IDENTITY_PROMPT\)/, '输入框默认值应取该常量')
})

test('结果回场景当图片参考板', () => {
  assert.match(
    stage3d,
    /await addPlane\(url, `identity-\$\{task\.task_id\}\.png`, "image"\)/,
    '生成结果应作为图片平面进场景',
  )
})