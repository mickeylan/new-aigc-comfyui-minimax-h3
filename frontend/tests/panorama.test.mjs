import test from 'node:test'
import assert from 'node:assert/strict'
import { readFileSync } from 'node:fs'
import { clampFov, clampPitch, normalizeYaw, panoramaSampleUV, panoramaTextureSize, panoramaViewLabel } from '../src/utils/panorama.js'

const viewer = readFileSync(new URL('../src/components/PanoramaViewer.vue', import.meta.url), 'utf8')

// 后端 erpToPerspective（asset_panorama.go）的逐行转写，像素坐标 → ERP 纹理坐标（未减 0.5 像素偏移）。
function backendUV(yawDeg, fovDeg, outW, outH, px, py) {
  const f = outW / 2 / Math.tan((fovDeg * Math.PI) / 360)
  const gx = px - outW / 2
  const gy = py - outH / 2
  const s = Math.sin((yawDeg * Math.PI) / 180)
  const c = Math.cos((yawDeg * Math.PI) / 180)
  const dx = gx * c + f * s
  const dz = -gx * s + f * c
  const lon = Math.atan2(dx, dz)
  const lat = Math.atan2(gy, Math.hypot(dx, dz))
  return { u: lon / (2 * Math.PI) + 0.5, v: lat / Math.PI + 0.5 }
}

const wrap = (u) => ((u % 1) + 1) % 1

test('俯仰为 0 时与后端拆视角的投影逐点一致，拆出的视角和 360° 查看看到的是同一画面', () => {
  const outW = 1280
  const outH = 720
  for (const yaw of [0, 60, 90, 180, 300, -45]) {
    for (const [px, py] of [[640, 360], [0, 0], [1280, 720], [100, 600], [1100, 50]]) {
      const x = (px / outW) * 2 - 1
      const y = 1 - (py / outH) * 2
      const got = panoramaSampleUV(yaw, 0, 75, outW / outH, x, y)
      const want = backendUV(yaw, 75, outW, outH, px, py)
      const du = Math.abs(wrap(got.u) - wrap(want.u))
      assert.ok(Math.min(du, 1 - du) < 1e-9, `yaw=${yaw} (${px},${py}) u ${got.u} vs ${want.u}`)
      assert.ok(Math.abs(got.v - want.v) < 1e-9, `yaw=${yaw} (${px},${py}) v ${got.v} vs ${want.v}`)
    }
  }
})

test('方向约定：yaw 0 看全景图正中，正值向右，180 看左右接缝；抬头看图片上方', () => {
  const center = (yaw, pitch = 0) => panoramaSampleUV(yaw, pitch, 75, 16 / 9, 0, 0)
  assert.deepEqual(center(0), { u: 0.5, v: 0.5 })
  assert.ok(Math.abs(center(90).u - 0.75) < 1e-12)
  assert.ok(Math.abs(center(-90).u - 0.25) < 1e-12)
  assert.ok(Math.abs(wrap(center(180).u)) < 1e-12 || Math.abs(wrap(center(180).u) - 1) < 1e-12)
  // v=0 是图片顶边（WebGL 未翻转上传时第一行在 t=0）。
  assert.ok(Math.abs(center(0, 30).v - (0.5 - 30 / 180)) < 1e-12)
  assert.ok(Math.abs(center(0, -30).v - (0.5 + 30 / 180)) < 1e-12)
  // 画面右边缘看到的在中心右侧。
  assert.ok(panoramaSampleUV(0, 0, 75, 16 / 9, 1, 0).u > 0.5)
  assert.ok(panoramaSampleUV(0, 0, 75, 16 / 9, 0, 1).v < 0.5)
})

test('方向标签与拆出视角的版本标签说法一致', () => {
  assert.equal(panoramaViewLabel(0), '正前方')
  assert.equal(panoramaViewLabel(60), '向右 60°')
  assert.equal(panoramaViewLabel(300), '向左 60°')
  assert.equal(panoramaViewLabel(180), '正后方')
  assert.equal(panoramaViewLabel(-180), '正后方')
  assert.equal(panoramaViewLabel(-179.7), '正后方')
  assert.equal(panoramaViewLabel(45, 12.4), '向右 45° · 仰 12°')
  assert.equal(panoramaViewLabel(0, -20), '正前方 · 俯 20°')
})

test('视角取值范围：yaw 环绕、俯仰不越过两极、视场限制在 30–100°', () => {
  assert.equal(normalizeYaw(370), 10)
  assert.equal(normalizeYaw(-190), 170)
  assert.equal(normalizeYaw(180), 180)
  assert.equal(normalizeYaw(-180), 180)
  assert.equal(clampPitch(120), 85)
  assert.equal(clampPitch(-120), -85)
  assert.equal(clampFov(10), 30)
  assert.equal(clampFov(150), 100)
})

test('超过显卡最大纹理尺寸时等比缩小，不超过时原样上传', () => {
  assert.deepEqual(panoramaTextureSize(5632, 2816, 4096), { width: 4096, height: 2048, scaled: true })
  assert.deepEqual(panoramaTextureSize(8192, 4096, 16384), { width: 8192, height: 4096, scaled: false })
  assert.deepEqual(panoramaTextureSize(8192, 4096, 8192), { width: 8192, height: 4096, scaled: false })
})

test('着色器与 panoramaSampleUV 是同一套公式，水平方向在着色器内环绕', () => {
  for (const line of [
    'float gx = vPos.x * uTan;',
    'float gy = -vPos.y * uTan / uAspect;',
    'float Y = gy * cos(uPitch) - sin(uPitch);',
    'float Z = gy * sin(uPitch) + cos(uPitch);',
    'float dx = gx * cos(uYaw) + Z * sin(uYaw);',
    'float dz = -gx * sin(uYaw) + Z * cos(uYaw);',
    'float lon = atan(dx, dz);',
    'float lat = atan(Y, length(vec2(dx, dz)));',
  ]) assert.ok(viewer.includes(line), line)
  assert.match(viewer, /fract\(lon \/ 6\.28318530718 \+ 0\.5\)/)
  assert.match(viewer, /panoramaTextureSize\(img\.naturalWidth, img\.naturalHeight, gl\.getParameter\(gl\.MAX_TEXTURE_SIZE\)\)/)
})
