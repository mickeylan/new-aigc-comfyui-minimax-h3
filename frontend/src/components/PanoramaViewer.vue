<template>
  <div ref="root" class="pano-viewer" :class="{ dragging }" tabindex="0" @keydown="onKey">
    <canvas ref="canvas" @pointerdown="onDown" @pointermove="onMove" @pointerup="onUp" @pointercancel="onUp" @wheel.prevent="onWheel" @dblclick="reset" />
    <div v-if="state === 'loading'" class="pano-status">全景图加载中…</div>
    <div v-else-if="state === 'error'" class="pano-status pano-error">{{ error }}</div>
    <template v-else>
      <div class="pano-label">{{ label }} · 视场 {{ Math.round(view.fov) }}°</div>
      <div class="pano-hint">拖动环顾 · 滚轮缩放 · 方向键微调 · 双击回正前方<span v-if="downscaled"> · 已按显卡上限缩小到 {{ downscaled }}</span></div>
    </template>
  </div>
</template>
<script setup>
import { computed, onBeforeUnmount, onMounted, reactive, ref, watch } from 'vue'
import { PANORAMA_DEFAULT_FOV, clampFov, clampPitch, normalizeYaw, panoramaTextureSize, panoramaViewLabel } from '../utils/panorama.js'

// 2:1 ERP 全景图贴在球面内侧逐像素反投影；只读查看，不改动全景图和拆出的视角。
const props = defineProps({ src: { type: String, required: true } })
const root = ref(null)
const canvas = ref(null)
const state = ref('loading')
const error = ref('')
const downscaled = ref('')
const dragging = ref(false)
const view = reactive({ yaw: 0, pitch: 0, fov: PANORAMA_DEFAULT_FOV })
const label = computed(() => panoramaViewLabel(view.yaw, view.pitch))

// 与 utils/panorama.js 的 panoramaSampleUV 逐行对应，方向约定同后端 erpToPerspective。
const FRAG = `precision highp float;
varying vec2 vPos;
uniform sampler2D uTex;
uniform float uYaw, uPitch, uTan, uAspect;
void main() {
  float gx = vPos.x * uTan;
  float gy = -vPos.y * uTan / uAspect;
  float Y = gy * cos(uPitch) - sin(uPitch);
  float Z = gy * sin(uPitch) + cos(uPitch);
  float dx = gx * cos(uYaw) + Z * sin(uYaw);
  float dz = -gx * sin(uYaw) + Z * cos(uYaw);
  float lon = atan(dx, dz);
  float lat = atan(Y, length(vec2(dx, dz)));
  gl_FragColor = texture2D(uTex, vec2(fract(lon / 6.28318530718 + 0.5), lat / 3.14159265359 + 0.5));
}`
const VERT = `attribute vec2 aPos; varying vec2 vPos; void main() { vPos = aPos; gl_Position = vec4(aPos, 0.0, 1.0); }`

let gl = null
let program = null
let texture = null
let uniforms = {}
let frame = 0
let resizeObserver = null
let pointer = null
let loadToken = 0

function compile(type, source) {
  const shader = gl.createShader(type)
  gl.shaderSource(shader, source)
  gl.compileShader(shader)
  if (!gl.getShaderParameter(shader, gl.COMPILE_STATUS)) throw new Error(gl.getShaderInfoLog(shader) || '着色器编译失败')
  return shader
}

function initGL() {
  gl = canvas.value.getContext('webgl', { antialias: false, preserveDrawingBuffer: false })
  if (!gl) throw new Error('浏览器不支持 WebGL，无法 360° 查看，请点「查看原图」')
  program = gl.createProgram()
  gl.attachShader(program, compile(gl.VERTEX_SHADER, VERT))
  gl.attachShader(program, compile(gl.FRAGMENT_SHADER, FRAG))
  gl.linkProgram(program)
  if (!gl.getProgramParameter(program, gl.LINK_STATUS)) throw new Error(gl.getProgramInfoLog(program) || '着色器链接失败')
  gl.useProgram(program)
  const buf = gl.createBuffer()
  gl.bindBuffer(gl.ARRAY_BUFFER, buf)
  gl.bufferData(gl.ARRAY_BUFFER, new Float32Array([-1, -1, 1, -1, -1, 1, 1, 1]), gl.STATIC_DRAW)
  const loc = gl.getAttribLocation(program, 'aPos')
  gl.enableVertexAttribArray(loc)
  gl.vertexAttribPointer(loc, 2, gl.FLOAT, false, 0, 0)
  for (const name of ['uTex', 'uYaw', 'uPitch', 'uTan', 'uAspect']) uniforms[name] = gl.getUniformLocation(program, name)
  gl.uniform1i(uniforms.uTex, 0)
}

function loadImage(src) {
  return new Promise((resolve, reject) => {
    const img = new Image()
    img.decoding = 'async'
    img.onload = () => resolve(img)
    img.onerror = () => reject(new Error('全景图加载失败'))
    img.src = src
  })
}

async function load(src) {
  const token = ++loadToken
  state.value = 'loading'
  downscaled.value = ''
  try {
    const img = await loadImage(src)
    if (token !== loadToken || !gl) return
    const ratio = img.naturalWidth / img.naturalHeight
    if (ratio < 1.95 || ratio > 2.05) throw new Error(`不是 2:1 的 ERP 全景图（${img.naturalWidth}×${img.naturalHeight}），无法 360° 查看，请点「查看原图」`)
    const size = panoramaTextureSize(img.naturalWidth, img.naturalHeight, gl.getParameter(gl.MAX_TEXTURE_SIZE))
    let source = img
    if (size.scaled) {
      source = document.createElement('canvas')
      source.width = size.width
      source.height = size.height
      source.getContext('2d').drawImage(img, 0, 0, size.width, size.height)
      downscaled.value = `${size.width}×${size.height}`
    }
    if (!texture) texture = gl.createTexture()
    gl.bindTexture(gl.TEXTURE_2D, texture)
    // WebGL1 的非 2 次幂纹理只能 CLAMP + 无 mipmap；水平环绕由着色器 fract 处理。
    gl.texParameteri(gl.TEXTURE_2D, gl.TEXTURE_WRAP_S, gl.CLAMP_TO_EDGE)
    gl.texParameteri(gl.TEXTURE_2D, gl.TEXTURE_WRAP_T, gl.CLAMP_TO_EDGE)
    gl.texParameteri(gl.TEXTURE_2D, gl.TEXTURE_MIN_FILTER, gl.LINEAR)
    gl.texParameteri(gl.TEXTURE_2D, gl.TEXTURE_MAG_FILTER, gl.LINEAR)
    gl.texImage2D(gl.TEXTURE_2D, 0, gl.RGB, gl.RGB, gl.UNSIGNED_BYTE, source)
    if (gl.getError() !== gl.NO_ERROR) throw new Error('显卡无法载入这张全景图，请点「查看原图」')
    state.value = 'ready'
    requestDraw()
  } catch (e) {
    if (token !== loadToken) return
    error.value = e.message || '全景图加载失败'
    state.value = 'error'
  }
}

function resize() {
  const el = canvas.value
  if (!el) return
  const dpr = Math.min(window.devicePixelRatio || 1, 2)
  const w = Math.max(1, Math.round(el.clientWidth * dpr))
  const h = Math.max(1, Math.round(el.clientHeight * dpr))
  if (el.width !== w || el.height !== h) {
    el.width = w
    el.height = h
  }
  requestDraw()
}

function requestDraw() {
  if (!frame) frame = requestAnimationFrame(draw)
}

function draw() {
  frame = 0
  if (!gl || state.value !== 'ready') return
  const el = canvas.value
  gl.viewport(0, 0, el.width, el.height)
  gl.uniform1f(uniforms.uYaw, (view.yaw * Math.PI) / 180)
  gl.uniform1f(uniforms.uPitch, (view.pitch * Math.PI) / 180)
  gl.uniform1f(uniforms.uTan, Math.tan((view.fov * Math.PI) / 360))
  gl.uniform1f(uniforms.uAspect, el.width / el.height)
  gl.drawArrays(gl.TRIANGLE_STRIP, 0, 4)
}

// 拖动时画面跟着鼠标走：向右拖 = 向左看。每像素对应的角度随视场缩放。
function degreesPerPixel() {
  return view.fov / Math.max(1, canvas.value.clientWidth)
}

function setView(yaw, pitch, fov = view.fov) {
  view.yaw = normalizeYaw(yaw)
  view.pitch = clampPitch(pitch)
  view.fov = clampFov(fov)
  requestDraw()
}

function onDown(e) {
  if (e.button !== 0) return
  pointer = { id: e.pointerId, x: e.clientX, y: e.clientY }
  dragging.value = true
  canvas.value.setPointerCapture(e.pointerId)
  root.value?.focus({ preventScroll: true })
}

function onMove(e) {
  if (!pointer || e.pointerId !== pointer.id) return
  const k = degreesPerPixel()
  setView(view.yaw - (e.clientX - pointer.x) * k, view.pitch + (e.clientY - pointer.y) * k)
  pointer.x = e.clientX
  pointer.y = e.clientY
}

function onUp(e) {
  if (!pointer || e.pointerId !== pointer.id) return
  pointer = null
  dragging.value = false
}

function onWheel(e) {
  setView(view.yaw, view.pitch, view.fov * Math.exp(e.deltaY * 0.001))
}

function onKey(e) {
  const step = e.shiftKey ? 15 : 5
  const moves = { ArrowLeft: [-step, 0], ArrowRight: [step, 0], ArrowUp: [0, step], ArrowDown: [0, -step] }
  if (moves[e.key]) {
    e.preventDefault()
    setView(view.yaw + moves[e.key][0], view.pitch + moves[e.key][1])
  } else if (e.key === '+' || e.key === '=') setView(view.yaw, view.pitch, view.fov - 5)
  else if (e.key === '-') setView(view.yaw, view.pitch, view.fov + 5)
}

function reset() {
  setView(0, 0, PANORAMA_DEFAULT_FOV)
}

onMounted(() => {
  try {
    initGL()
  } catch (e) {
    error.value = e.message
    state.value = 'error'
    return
  }
  resizeObserver = new ResizeObserver(resize)
  resizeObserver.observe(canvas.value)
  resize()
  load(props.src)
})

watch(() => props.src, (src) => {
  if (!gl) return
  reset()
  load(src)
})

onBeforeUnmount(() => {
  loadToken++
  if (frame) cancelAnimationFrame(frame)
  resizeObserver?.disconnect()
  if (gl) {
    if (texture) gl.deleteTexture(texture)
    gl.getExtension('WEBGL_lose_context')?.loseContext()
  }
  gl = null
})
</script>
<style scoped>
.pano-viewer { position: relative; width: 100%; height: 100%; min-height: 240px; overflow: hidden; border-radius: 10px; background: #000; outline: none; }
.pano-viewer canvas { display: block; width: 100%; height: 100%; cursor: grab; touch-action: none; }
.pano-viewer.dragging canvas { cursor: grabbing; }
.pano-status { position: absolute; inset: 0; display: flex; align-items: center; justify-content: center; padding: 24px; color: #ddd; font-size: 13px; text-align: center; }
.pano-error { color: #ff9a9a; }
.pano-label, .pano-hint { position: absolute; left: 10px; padding: 3px 8px; border-radius: 6px; background: rgba(0, 0, 0, 0.55); color: #fff; font-size: 12px; pointer-events: none; }
.pano-label { top: 10px; font-weight: 600; }
.pano-hint { bottom: 10px; color: #ddd; }
</style>
