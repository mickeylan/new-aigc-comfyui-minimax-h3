<template>
  <div class="prompt-ref-view"><template v-for="seg in segments" :key="seg.start"><span v-if="seg.type === 'text'" :data-start="seg.start">{{ seg.text }}</span><span v-else-if="seg.issue" class="prompt-ref-chip invalid" :data-start="seg.start" :data-end="seg.end" :title="seg.issue">{{ seg.raw }}</span><span v-else class="prompt-ref-chip" :data-start="seg.start" :data-end="seg.end" :title="chipTitle(seg)"><img :src="seg.binding.image_url" alt="" loading="lazy">{{ referenceDisplayName(seg.binding) }}<span class="prompt-ref-pop"><img :src="seg.binding.image_url" alt="" loading="lazy"><span>{{ seg.raw }} · {{ referenceRoleLabel(seg.binding.role) }}「{{ referenceDisplayName(seg.binding) }}」</span></span></span></template></div>
</template>
<script setup>
import { computed } from 'vue'
import { parsePromptReferences, referenceDisplayName, referenceRoleLabel } from '../utils/promptReferences.js'
// 只渲染显示：原文中的 <Subject N>/<Picture N> 画成「缩略图 + 名字」，提交文本不受影响。
// data-start/data-end 是片段在原文中的位置，供双击切回编辑时定位光标。
const props = defineProps({ text: { type: String, default: '' }, bindings: { type: Array, default: () => [] } })
const segments = computed(() => {
  let start = 0
  return parsePromptReferences(props.text, props.bindings).map(seg => {
    const end = start + (seg.type === 'text' ? seg.text : seg.raw).length
    const item = { ...seg, start, end }
    start = end
    return item
  })
})
function chipTitle(seg) { return `${seg.raw} · ${seg.binding.usage || referenceRoleLabel(seg.binding.role)}` }
</script>
<style scoped>
.prompt-ref-view { white-space: pre-wrap; overflow-wrap: anywhere; }
.prompt-ref-chip { position: relative; display: inline-flex; align-items: center; gap: 3px; margin: 0 1px; padding: 0 5px 0 2px; border-radius: 5px; background: var(--accent-soft); color: var(--accent); font-weight: 600; white-space: nowrap; vertical-align: baseline; }
.prompt-ref-chip > img { width: 16px; height: 16px; border-radius: 3px; object-fit: cover; }
.prompt-ref-chip.invalid { padding: 0 5px; background: rgba(255, 69, 58, 0.12); color: var(--red); text-decoration: wavy underline; cursor: help; }
.prompt-ref-pop { display: none; position: absolute; left: 0; top: calc(100% + 4px); z-index: 20; width: 180px; padding: 6px; border: 1px solid var(--border); border-radius: 8px; background: var(--surface, #fff); box-shadow: 0 6px 20px rgba(0, 0, 0, 0.18); color: var(--text-secondary); font-weight: 400; white-space: normal; }
.prompt-ref-pop img { display: block; width: 100%; max-height: 180px; object-fit: contain; border-radius: 5px; margin-bottom: 4px; }
.prompt-ref-chip:hover .prompt-ref-pop { display: block; }
</style>
