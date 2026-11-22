import test from 'node:test'
import assert from 'node:assert/strict'
import { readFileSync } from 'node:fs'

const stage = readFileSync(new URL('../src/studio/scene3d/Scene3DStage.tsx', import.meta.url), 'utf8')

// 推拉镜头：两个机位视角不同，一路变焦过去（fov 越小越推近）。
test('机位带视角（fov）：推拉随路径插值，导出后还原', () => {
  // 15° 长焦到 90° 广角；缺省/非法值回 45°（相机初始视角）
  assert.match(stage, /const DEFAULT_CAMERA_FOV = 45;/)
  assert.match(stage, /const CAMERA_FOV_LIMITS = \{ min: 15, max: 90 \};/)
  assert.match(stage, /function normalizeCameraFov\(value: unknown\) \{/)
  assert.match(
    stage,
    /Math\.min\(CAMERA_FOV_LIMITS\.max, Math\.max\(CAMERA_FOV_LIMITS\.min, fov\)\)/,
  )
  // 取样时两个机位的视角一起插值
  assert.match(stage, /fov: fromFov \+ \(toFov - fromFov\) \* ratio,/)
  // 回放与序列导出都真的改相机视角（不改 projectionMatrix 不生效）
  assert.match(stage, /camera\.fov = pose\.fov;/)
  assert.match(stage, /camera\.fov = sample\.fov;/)
  assert.match(stage, /camera\.updateProjectionMatrix\(\);/)
  // 导出结束要连视角一起还原，否则编辑器停在最后一帧的焦段
  assert.match(stage, /const savedFov = camera\.fov;/)
  assert.match(stage, /camera\.fov = savedFov;/)
  // 存档读写都要归一化：老存档没这个字段也能读
  assert.match(stage, /fov: normalizeCameraFov\(key\.fov\)/)
  // 记录机位时把当前视角一起记下来
  assert.match(stage, /fov: normalizeCameraFov\(camera\.fov\),/)
  // UI：每个机位一个视角输入
  assert.match(stage, /title="这个机位的视角（度）：越小越推近，越大越拉远"/)
})

test('取样插值：视角相同时不产生跳变，且只在变化时更新投影矩阵', () => {
  // 视角相同则不更新（避免每帧重建投影矩阵）
  assert.match(stage, /if \(Math\.abs\(camera\.fov - pose\.fov\) > 1e-4\) \{/)
  assert.match(stage, /if \(Math\.abs\(camera\.fov - sample\.fov\) > 1e-4\) \{/)
  // 端点：from 与 to 的视角都来自机位自身，缺省回 45°
  assert.match(stage, /const fromFov = normalizeCameraFov\(active\.from\.fov\);/)
  assert.match(stage, /const toFov = normalizeCameraFov\(active\.to\.fov\);/)
})