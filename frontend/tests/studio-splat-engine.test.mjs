import test from 'node:test'
import assert from 'node:assert/strict'
import { readFileSync } from 'node:fs'

const stage = readFileSync(new URL('../src/studio/scene3d/Scene3DStage.tsx', import.meta.url), 'utf8')

test('图生 3D 世界按抠图引擎选模板，默认 RMBG-3.0', () => {
  assert.match(stage, /const SPLAT_MATTE_TEMPLATES = \{\s*rmbg3: "image_to_3d_triposplat",\s*birefnet: "image_to_3d_triposplat_birefnet",/)
  assert.match(stage, /useState<SplatMatteEngine>\("rmbg3"\)/)
})

test('顶栏按钮旁边有抠图方式下拉，两个引擎都能选', () => {
  assert.match(stage, /<option value="rmbg3">RMBG-3\.0 抠图<\/option>/)
  assert.match(stage, /<option value="birefnet">BiRefNet 抠图<\/option>/)
  assert.match(stage, /setSplatMatteEngine\(event\.target\.value as SplatMatteEngine\)/)
  // 下拉要跟「AI 图生 3D 世界」按钮挨在一起，不能飘到别处
  assert.match(stage, /AI 图生 3D 世界\s*<\/button>\s*<select[\s\S]{0,400}splatMatteEngine/)
})

test('提交任务时把选中的引擎传下去', () => {
  assert.match(stage, /createImageToSplatTask\(upload, splatMatteEngine\)/)
  assert.match(stage, /findTemplateId\(SPLAT_MATTE_TEMPLATES\[matteEngine\]\)/)
})

test('缺 BiRefNet 变体模板时退回默认 RMBG-3 模板而不是直接失败', () => {
  assert.match(stage, /if \(matteEngine === "rmbg3"\) throw err;/)
  assert.match(stage, /templateId = await findTemplateId\(SPLAT_MATTE_TEMPLATES\.rmbg3\)/)
})