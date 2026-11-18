// 与后端 erpToPerspective（asset_panorama.go）同一约定：yaw 0 = 全景图正中，正值向右转，
// 水平视场按画布宽度计算；pitch 是后端没有的抬头/低头，正值抬头。PanoramaViewer 的着色器逐行对应 panoramaSampleUV。
export const PANORAMA_DEFAULT_FOV = 75
export const PANORAMA_MIN_FOV = 30
export const PANORAMA_MAX_FOV = 100
export const PANORAMA_MAX_PITCH = 85

export function normalizeYaw(yaw) {
  const y = (((yaw % 360) + 540) % 360) - 180
  return y === -180 ? 180 : y
}

export function clampPitch(pitch) {
  return Math.min(PANORAMA_MAX_PITCH, Math.max(-PANORAMA_MAX_PITCH, pitch))
}

export function clampFov(fov) {
  return Math.min(PANORAMA_MAX_FOV, Math.max(PANORAMA_MIN_FOV, fov))
}

// 方向说法与拆出视角的版本标签一致（正前方 / 向右 60° / 正后方）。
export function panoramaViewLabel(yaw, pitch = 0) {
  let y = Math.round(normalizeYaw(yaw))
  if (y === -180) y = 180
  const dir = y === 0 ? '正前方' : y === 180 ? '正后方' : y > 0 ? `向右 ${y}°` : `向左 ${-y}°`
  const p = Math.round(pitch)
  return p === 0 ? dir : `${dir} · ${p > 0 ? '仰' : '俯'} ${Math.abs(p)}°`
}

// 超出显卡最大纹理尺寸时等比缩小，否则 WebGL 上传失败只会黑屏。
export function panoramaTextureSize(width, height, maxTexture) {
  const scale = Math.min(1, maxTexture / width, maxTexture / height)
  return { width: Math.max(1, Math.floor(width * scale)), height: Math.max(1, Math.floor(height * scale)), scaled: scale < 1 }
}

// x、y 为画布归一化坐标 [-1,1]（y 向上），返回 ERP 纹理坐标（v=0 为图片顶边，u 未取模）。
export function panoramaSampleUV(yawDeg, pitchDeg, fovDeg, aspect, x, y) {
  const t = Math.tan((fovDeg * Math.PI) / 360)
  const gx = x * t
  const gy = (-y * t) / aspect
  const p = (pitchDeg * Math.PI) / 180
  const w = (yawDeg * Math.PI) / 180
  const X = gx
  const Y = gy * Math.cos(p) - Math.sin(p)
  const Z = gy * Math.sin(p) + Math.cos(p)
  const dx = X * Math.cos(w) + Z * Math.sin(w)
  const dz = -X * Math.sin(w) + Z * Math.cos(w)
  const lon = Math.atan2(dx, dz)
  const lat = Math.atan2(Y, Math.hypot(dx, dz))
  return { u: lon / (2 * Math.PI) + 0.5, v: lat / Math.PI + 0.5 }
}
