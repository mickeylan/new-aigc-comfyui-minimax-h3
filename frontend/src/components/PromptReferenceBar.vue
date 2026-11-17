<template>
  <div class="prompt-ref-bar" :class="{ disabled }">
    <div class="prompt-ref-bar-head"><b>引用（按提交顺序）</b><small>{{ hint || '点击在光标处插入；调整参考图请到场景编辑的参考图设置' }}</small></div>
    <div v-if="!bindings.length" class="prompt-ref-empty">当前没有参考图。</div>
    <div v-else class="prompt-ref-items">
      <button v-for="b in bindings" :key="b.picture" type="button" class="prompt-ref-item" :disabled="disabled" :title="itemTitle(b)" @click="emit('insert', referenceInsertTag(b))"><span class="prompt-ref-no">{{ b.picture }}</span><img :src="b.image_url" alt="" loading="lazy"><span class="prompt-ref-name">{{ referenceDisplayName(b) }}</span><small>{{ b.subject ? '主体' : referenceRoleLabel(b.role) }}</small></button>
    </div>
  </div>
</template>
<script setup>
import { referenceDisplayName, referenceInsertTag, referenceRoleLabel } from '../utils/promptReferences.js'
// 只展示、不增删：编号就是提交顺序，增删参考图会让正文里的全部标记错位。
defineProps({ bindings: { type: Array, default: () => [] }, disabled: { type: Boolean, default: false }, hint: { type: String, default: '' } })
const emit = defineEmits(['insert'])
function itemTitle(b) {
  const shots = (b.shot_orders || []).length ? ` · 出现在 Shot ${b.shot_orders.join('、')}` : ''
  return `${referenceInsertTag(b)} · ${b.usage || referenceRoleLabel(b.role)}${shots}`
}
</script>
<style scoped>
.prompt-ref-bar { margin: 6px 0; padding: 8px; border: 1px solid var(--border); border-radius: 8px; }
.prompt-ref-bar.disabled { opacity: 0.55; }
.prompt-ref-bar-head { display: flex; flex-wrap: wrap; gap: 4px 10px; align-items: baseline; margin-bottom: 6px; font-size: 12px; }
.prompt-ref-bar-head small, .prompt-ref-empty { color: var(--text-secondary); font-size: 12px; }
.prompt-ref-items { display: flex; flex-wrap: wrap; gap: 6px; }
.prompt-ref-item { display: inline-flex; align-items: center; gap: 5px; padding: 3px 8px 3px 3px; border: 1px solid var(--border); border-radius: 6px; background: transparent; color: inherit; font-size: 12px; cursor: pointer; }
.prompt-ref-item:hover:not(:disabled) { border-color: var(--accent); background: var(--accent-soft); }
.prompt-ref-item:disabled { cursor: not-allowed; }
.prompt-ref-item img { width: 28px; height: 28px; border-radius: 4px; object-fit: cover; }
.prompt-ref-no { min-width: 16px; text-align: center; color: var(--text-secondary); font-weight: 600; }
.prompt-ref-name { font-weight: 600; }
.prompt-ref-item small { color: var(--text-secondary); }
</style>
