import test from 'node:test'
import assert from 'node:assert/strict'
import { readFileSync } from 'node:fs'

const stage = readFileSync(new URL('../src/studio/scene3d/Scene3DStage.tsx', import.meta.url), 'utf8')

// 视口 3840 宽时每帧 200–370 KB；当视频控制图 1280 宽已经够，体积小一大截。
test('序列导出可选分辨率：临时缩画布，导完还原', () => {
  assert.match(stage, /const SEQUENCE_WIDTH_OPTIONS = \[0, 1280, 960, 720\];/)
  assert.match(stage, /const \[sequenceWidth, setSequenceWidth\] = useState\(0\);/)
  // 宽度为 0 时不动画布，保持原来「视口原尺寸」的行为
  assert.match(stage, /if \(renderer && sequenceWidth > 0\) \{/)
  assert.match(stage, /const height = Math\.max\(1, Math\.round\(sequenceWidth \/ savedAspect\)\);/)
  assert.match(stage, /renderer\.setSize\(sequenceWidth, height, false\);/)
  assert.match(stage, /camera\.aspect = sequenceWidth \/ height;/)
  // 还原：尺寸与投影都要放回去，否则编辑器停在导出分辨率上
  assert.match(stage, /const savedSize = renderer \? renderer\.getSize\(new THREE\.Vector2\(\)\) : null;/)
  assert.match(stage, /renderer\.setSize\(savedSize\.x, savedSize\.y, false\);/)
  assert.match(stage, /camera\.aspect = savedAspect;/)
  // UI 下拉 + 依赖
  assert.match(stage, /title="序列导出的画面宽度：原尺寸最大也最清楚，小一点体积小很多"/)
  assert.match(stage, /moverId, sequenceFps, sequenceWidth, archiveProjectId\],/)
})

test('分辨率选项文案：0 就是跟随视口', () => {
  assert.match(stage, /0: "视口原尺寸",/)
  assert.match(stage, /1280: "1280 宽",/)
  assert.match(stage, /960: "960 宽",/)
  assert.match(stage, /720: "720 宽",/)
})