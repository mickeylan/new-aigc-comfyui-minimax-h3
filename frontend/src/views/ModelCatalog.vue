<template>
  <div class="catalog-page">
    <header class="page-head">
      <div>
        <p class="eyebrow">MODEL CATALOG</p>
        <h1>模型能力目录</h1>
        <p class="subtitle">由当前启用的工作流模板实时派生，仅供查看。</p>
      </div>
      <span class="count">{{ items.length }} 个模板</span>
    </header>

    <div v-if="loading" class="state">正在读取模型能力…</div>
    <div v-else-if="error" class="state error">{{ error }} <button class="retry" @click="load">重试</button></div>
    <section v-else class="catalog-grid">
      <article v-for="item in items" :key="item.template_code" class="model-card">
        <div class="card-title">
          <div>
            <span class="mode">{{ modeLabel(item.mode) }}</span>
            <h2>{{ item.name }}</h2>
          </div>
          <span class="code">{{ item.template_code }}</span>
        </div>
        <p class="description">{{ item.description }}</p>

        <div class="capabilities">
          <span :class="{ muted: !item.max_references }">参考图 {{ item.max_references || 0 }}</span>
          <span :class="{ muted: !item.supports_first_frame }">首帧 {{ yesNo(item.supports_first_frame) }}</span>
          <span :class="{ muted: !item.supports_last_frame }">尾帧 {{ yesNo(item.supports_last_frame) }}</span>
          <span :class="{ muted: !item.supports_audio }">音频 {{ yesNo(item.supports_audio) }}</span>
        </div>

        <dl class="facts">
          <div>
            <dt>分辨率</dt>
            <dd>{{ resolutionText(item.resolutions) }}</dd>
          </div>
          <div>
            <dt>时长</dt>
            <dd>{{ durationText(item.duration) }}</dd>
          </div>
        </dl>

        <details v-if="Object.keys(item.default_params || {}).length" class="defaults">
          <summary>默认参数</summary>
          <div class="param-list">
            <span v-for="(value, key) in item.default_params" :key="key"><b>{{ key }}</b> {{ value }}</span>
          </div>
        </details>
      </article>
    </section>
  </div>
</template>

<script setup>
import { onMounted, ref } from 'vue'
import { fetchModelCatalog } from '../api/modelCatalog'

const items = ref([])
const loading = ref(true)
const error = ref('')

const labels = {
  'text-to-video': '文生视频',
  'image-to-video': '图生视频',
  'reference-to-video': '参考图生视频',
  'first-last-frame-to-video': '首尾帧生视频',
  'text-to-image': '文生图',
  'image-to-image': '图生图',
  'reference-to-image': '参考图生图'
}

function modeLabel(mode) { return labels[mode] || mode }
function yesNo(value) { return value ? '支持' : '不支持' }
function number(value) { return value ?? '—' }
function durationText(duration) {
  if (!duration) return '不适用'
  return `${number(duration.min)}–${number(duration.max)} 秒（默认 ${number(duration.default)}）`
}
function resolutionText(resolutions) {
  if (!resolutions?.length) return '由工作流决定'
  return resolutions.map(({ width, height }) => {
    const defaults = `${number(width.default)}×${number(height.default)}`
    return width.min == null || width.max == null || height.min == null || height.max == null
      ? defaults
      : `${defaults}（宽 ${width.min}–${width.max}，高 ${height.min}–${height.max}）`
  }).join('；')
}
async function load() {
  loading.value = true
  error.value = ''
  try {
    const { data } = await fetchModelCatalog()
    items.value = data.items || []
  } catch (err) {
    error.value = err.response?.data?.error || '模型能力目录加载失败'
  } finally {
    loading.value = false
  }
}

onMounted(load)
</script>

<style scoped>
.catalog-page { max-width: 1180px; margin: 0 auto; padding: 38px 24px 64px; }
.page-head { display: flex; justify-content: space-between; align-items: end; gap: 20px; margin-bottom: 24px; }
.eyebrow { margin: 0 0 6px; color: var(--accent); font-size: 11px; font-weight: 700; letter-spacing: .16em; }
h1 { margin: 0; color: var(--text); font-size: clamp(28px, 4vw, 42px); letter-spacing: -.03em; }
.subtitle { margin: 8px 0 0; color: var(--text-secondary); }
.count, .mode, .code, .capabilities span, .param-list span { border-radius: 999px; }
.count { padding: 7px 12px; background: var(--chip-bg); color: var(--text-secondary); font-size: 12px; white-space: nowrap; }
.catalog-grid { display: grid; grid-template-columns: repeat(auto-fit, minmax(330px, 1fr)); gap: 14px; }
.model-card { padding: 20px; border: 1px solid var(--border); border-radius: 18px; background: var(--card-solid); box-shadow: 0 8px 28px rgba(0, 0, 0, .05); }
.card-title { display: flex; justify-content: space-between; align-items: start; gap: 12px; }
.mode { display: inline-block; padding: 4px 9px; background: var(--accent-soft); color: var(--accent); font-size: 11px; font-weight: 650; }
h2 { margin: 10px 0 0; color: var(--text); font-size: 18px; }
.code { max-width: 48%; padding: 5px 8px; background: var(--chip-bg); color: var(--text-tertiary); font: 11px/1.3 ui-monospace, monospace; overflow-wrap: anywhere; }
.description { min-height: 42px; margin: 12px 0 16px; color: var(--text-secondary); font-size: 13px; line-height: 1.55; }
.capabilities { display: flex; flex-wrap: wrap; gap: 6px; }
.capabilities span { padding: 5px 9px; background: rgba(52, 199, 89, .1); color: var(--green); font-size: 11px; }
.capabilities span.muted { background: var(--chip-bg); color: var(--text-tertiary); }
.facts { margin: 16px 0 0; border-top: 1px solid var(--border); }
.facts div { display: grid; grid-template-columns: 64px 1fr; gap: 8px; padding-top: 11px; }
dt { color: var(--text-tertiary); font-size: 12px; } dd { margin: 0; color: var(--text); font-size: 12px; }
.defaults { margin-top: 14px; color: var(--text-secondary); font-size: 12px; }
.defaults summary { cursor: pointer; user-select: none; }
.param-list { display: flex; flex-wrap: wrap; gap: 6px; margin-top: 9px; }
.param-list span { padding: 5px 8px; background: var(--chip-bg); overflow-wrap: anywhere; }
.param-list b { color: var(--text); margin-right: 4px; }
.state { padding: 50px; text-align: center; color: var(--text-secondary); }
.state.error { color: var(--red); }
.retry { margin-left: 8px; border: 0; background: transparent; color: var(--accent); cursor: pointer; }
@media (max-width: 640px) { .catalog-page { padding: 26px 16px 48px; } .page-head { align-items: start; } .catalog-grid { grid-template-columns: 1fr; } .card-title { display: block; } .code { display: inline-block; max-width: 100%; margin-top: 8px; } }
</style>
