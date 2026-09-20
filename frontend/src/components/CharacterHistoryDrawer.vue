<template>
  <div v-if="open" class="drawer-mask" @click.self="$emit('close')">
    <aside class="drawer" aria-label="角色历史">
      <header><div><span class="overline">CHARACTER HISTORY</span><h2>角色历史</h2></div><button class="btn btn-sm btn-ghost" @click="$emit('close')">关闭</button></header>
      <div v-if="loading" class="empty">加载中…</div>
      <div v-else-if="!rows.length" class="empty">暂无角色历史。</div>
      <article v-for="row in rows" :key="row.character.id" class="history-card">
        <h3>{{ row.character.name }}</h3>
        <p v-if="row.aliases?.length" class="sub">别名：{{ row.aliases.join('、') }}</p>
        <dl><dt>最近审核造型</dt><dd>{{ row.last_approved_look?.name || '无' }}</dd><dt>最近审核套装</dt><dd>{{ row.last_approved_outfit?.name || '无' }}</dd><dt>最近出场</dt><dd>{{ lastSeen(row.last_seen) }}</dd><dt>音色</dt><dd>{{ row.voice_config?.voice || row.voice_config?.voice_id || '默认' }}</dd></dl>
        <details><summary>出场记录（{{ sceneCount(row) }} 场）</summary><div v-for="ep in row.episodes" :key="ep.number" class="episode"><strong>第{{ ep.number }}集</strong><p v-for="scene in ep.scenes" :key="scene.id">场景{{ scene.order }} · {{ scene.title || '未命名' }}（{{ scene.shots?.length || 0 }}镜）</p></div></details>
        <p v-if="row.props?.length" class="sub">关联道具：{{ row.props.map(v => v.name).join('、') }}</p>
      </article>
    </aside>
  </div>
</template>
<script setup>
import { ref, watch } from 'vue'
import { api } from '../api'
import { useToastStore } from '../stores/toast'
const props = defineProps({ projectId: { type: [String, Number], required: true }, open: Boolean })
defineEmits(['close'])
const toast = useToastStore(), rows = ref([]), loading = ref(false)
const sceneCount = row => (row.episodes || []).reduce((n, ep) => n + (ep.scenes?.length || 0), 0)
const lastSeen = value => value ? `第${value.episode_n}集 · 场景${value.scene_order}${value.shot_order ? ` · 镜头${value.shot_order}` : ''}` : '无'
async function load() { loading.value = true; try { rows.value = (await api.characterHistory(props.projectId)).data || [] } catch (e) { toast.error(e.response?.data?.error || '加载角色历史失败') } finally { loading.value = false } }
watch(() => props.open, value => { if (value) load() }, { immediate: true })
</script>
<style scoped>
.drawer-mask{position:fixed;inset:0;background:rgba(0,0,0,.45);z-index:1100}.drawer{position:absolute;right:0;top:0;height:100%;width:min(520px,92vw);overflow:auto;background:var(--card);padding:22px;box-shadow:-8px 0 30px rgba(0,0,0,.25)}header{display:flex;justify-content:space-between;align-items:center}.history-card{border:1px solid var(--border);border-radius:12px;padding:14px;margin:12px 0}.history-card h3{margin:0}dl{display:grid;grid-template-columns:auto 1fr;gap:6px 12px;font-size:13px}dt{color:var(--text-tertiary)}dd{margin:0}.episode{border-top:1px solid var(--border);padding-top:8px;margin-top:8px}.episode p{margin:5px 0;color:var(--text-secondary);font-size:12px}.empty{padding:30px;text-align:center;color:var(--text-tertiary)}
</style>
