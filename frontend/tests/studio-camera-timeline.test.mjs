import test from 'node:test'
import assert from 'node:assert/strict'
import { readFileSync } from 'node:fs'

const stage = readFileSync(new URL('../src/studio/scene3d/Scene3DStage.tsx', import.meta.url), 'utf8')

// 机位序列以前只活在内存里：刷新、关页面、换场景就没了。v3 起进存档。
test('相机关键帧带时长与缓动，存进 v3 存档并能读回', () => {
  const type = stage.match(/interface CameraKey \{([\s\S]*?)\n\}/)
  assert.ok(type, '缺 CameraKey 类型')
  assert.match(type[1], /duration: number;/, '关键帧要带段时长')
  assert.match(type[1], /easing: CameraEasing;/, '关键帧要带缓动')
  assert.match(type[1], /fov: number;/, '关键帧要带视角（推拉）')

  // 存档里写的是同一份结构，不是只写位置
  const saved = stage.match(/cameraKeys: cameraKeys\.map\(\(key\) => \(\{([\s\S]*?)\}\)\),/)
  assert.ok(saved, 'saveScene 没写 cameraKeys')
  assert.match(saved[1], /duration: normalizeCameraDuration\(key\.duration\)/)
  assert.match(saved[1], /easing: normalizeCameraEasing\(key\.easing\)/)

  // 载入时老存档没这个字段要清空 + 补默认，不能让上一个场景的机位串进来
  assert.match(stage, /setCameraKeys\(\s*\n\s*\(doc\.cameraKeys \?\? \[\]\)\.map/)
  // fov 是后加的字段：载入时也过一遍 normalizeCameraFov（老存档没有就回 45°）
  assert.match(stage, /duration: normalizeCameraDuration\(key\.duration\),\s*\n\s*easing: normalizeCameraEasing\(key\.easing\),\s*\n\s*fov: normalizeCameraFov\(key\.fov\),\s*\n\s*\}\)\),\s*\n\s*\);\s*\n\s*const camera = cameraRef\.current;/)
})

test('段时长/缓动有默认值与上下限，坏数据不会把相机甩飞', () => {
  assert.match(stage, /const DEFAULT_CAMERA_SEGMENT_SECONDS = 1\.6;/)
  assert.match(stage, /const DEFAULT_CAMERA_EASING: CameraEasing = "easeInOut";/)
  assert.match(stage, /const CAMERA_SEGMENT_LIMITS = \{ min: 0\.2, max: 20 \};/)
  // 归一化：非数字/<=0 回默认，越界夹到区间
  const fn = stage.match(/function normalizeCameraDuration\(value: unknown\) \{([\s\S]*?)\n\}/)
  assert.ok(fn, '缺 normalizeCameraDuration')
  assert.match(fn[1], /if \(!Number\.isFinite\(seconds\) \|\| seconds <= 0\) return DEFAULT_CAMERA_SEGMENT_SECONDS;/)
  assert.match(fn[1], /Math\.min\(CAMERA_SEGMENT_LIMITS\.max, Math\.max\(CAMERA_SEGMENT_LIMITS\.min, seconds\)\)/)
  // 未知缓动值也回默认，不能因为存档里写了别的字符串就崩
  assert.match(stage, /function normalizeCameraEasing\(value: unknown\): CameraEasing \{/)
})

test('四档缓动齐备，回放按每一段自己的时长与曲线走', () => {
  for (const name of ['linear', 'easeInOut', 'easeIn', 'easeOut']) {
    assert.match(stage, new RegExp(`\\b${name}: \\{ label:`), `缺缓动 ${name}`)
  }
  // 回放不再用统一 1.6 秒，而是逐段取时长、逐段取缓动函数；
  // 取样抽成 cameraPathSegments / cameraPathDuration 后，回放与序列导出共用同一条路径。
  assert.match(stage, /function cameraPathSegments\(keys: CameraKey\[\]\) \{\n  return keys\.slice\(0, -1\)\.map\(\(from, index\) => \(\{/)
  assert.match(stage, /seconds: normalizeCameraDuration\(from\.duration\),/)
  assert.match(stage, /ease: CAMERA_EASINGS\[normalizeCameraEasing\(from\.easing\)\]\.fn,/)
  assert.match(stage, /return cameraPathSegments\(keys\)\.reduce\(\(sum, item\) => sum \+ item\.seconds, 0\);/)
  // 回放走同一个取样函数，不再各写一份插值
  assert.match(stage, /const pose = sampleCameraPath\(cameraKeys, elapsed\);/)
  // 超过总时长时停在最后一段末尾
  assert.match(stage, /\/\*\* 按时间从路径上取一个机位；超过总时长停在最后一段末尾。 \*\//)
  assert.match(stage, /const local = Math\.min\(1, Math\.max\(0, \(elapsed - cursor\) \/ active\.seconds\)\);/)
  assert.doesNotMatch(stage, /const segment = 1\.6;/, '不该再留硬编码的单段时长')
})

test('右栏能改时长/缓动、上下移、删除单个机位', () => {
  assert.match(stage, /const updateCameraKey = useCallback\(/)
  assert.match(stage, /const moveCameraKey = useCallback\(\(id: string, delta: -1 \| 1\) => \{/)
  assert.match(stage, /const removeCameraKey = useCallback\(\(id: string\) => \{/)
  // 上移/下移是交换位置，不是重录
  assert.match(stage, /\[next\[index\], next\[target\]\] = \[next\[target\], next\[index\]\];/)
  // UI：时长输入 + 缓动下拉 + 三个按钮 + 总时长提示
  assert.match(stage, /value=\{key\.duration\}/)
  assert.match(stage, /value=\{key\.easing\}/)
  assert.match(stage, /title="上移"/)
  assert.match(stage, /title="下移"/)
  assert.match(stage, /title="删除这个机位"/)
  assert.match(stage, /时长与缓动跟场景一起存档（v3）。/)
})