<template>
  <div class="prompt-ref-editor">
    <div class="prompt-ref-mode">
      <button type="button" class="btn btn-sm" :class="mode === 'preview' ? '' : 'btn-secondary'" :disabled="!modelValue" @click="showPreview">预览</button>
      <button type="button" class="btn btn-sm" :class="mode === 'edit' ? '' : 'btn-secondary'" @click="showEdit()">编辑</button>
      <small>预览只用于查看，提交仍是原文标记；双击预览可在该处编辑</small>
    </div>
    <div v-if="mode === 'preview'" class="textarea prompt-ref-preview-box" :style="{ height: boxHeight }" title="双击切回编辑" @dblclick="editAtPoint">
      <PromptReferenceView :text="modelValue" :bindings="bindings" />
    </div>
    <textarea v-else ref="input" class="textarea" :rows="rows" :placeholder="placeholder" :style="measuredHeight ? { height: measuredHeight } : null" :value="modelValue" @input="emit('update:modelValue', $event.target.value)" @blur="rememberSelection" />
  </div>
</template>
<script setup>
import { computed, nextTick, ref } from 'vue'
import PromptReferenceView from './PromptReferenceView.vue'
import { insertAtSelection } from '../utils/promptReferences.js'
// 在同一位置切换「预览 / 编辑」：预览把标记画成缩略图，编辑就是原来的纯文本框。
// 有内容时默认预览；用户手动切换过以后就按用户的选择，不再自动切。
const props = defineProps({ modelValue: { type: String, default: '' }, bindings: { type: Array, default: () => [] }, rows: { type: Number, default: 6 }, placeholder: { type: String, default: '' } })
const emit = defineEmits(['update:modelValue'])
const input = ref(null)
const chosenMode = ref('')
const measuredHeight = ref('')
const selection = ref(null)
const mode = computed(() => chosenMode.value || (props.modelValue ? 'preview' : 'edit'))
// 预览框沿用文本框当前高度（可能被用户拖动过），切换时页面不跳。
const boxHeight = computed(() => measuredHeight.value || `calc(${props.rows} * 1.7em + 22px)`)

function rememberSelection() {
  if (input.value) selection.value = { start: input.value.selectionStart, end: input.value.selectionEnd }
}
function showPreview() {
  if (input.value) { rememberSelection(); measuredHeight.value = `${input.value.offsetHeight}px` }
  chosenMode.value = 'preview'
}
async function showEdit(caret) {
  chosenMode.value = 'edit'
  if (Number.isInteger(caret)) selection.value = { start: caret, end: caret }
  await nextTick()
  if (!input.value) return
  input.value.focus()
  if (selection.value) input.value.setSelectionRange(selection.value.start, selection.value.end)
}
// 双击预览：把点击位置换算回原文偏移；点在缩略图上就放到标记之后。
function editAtPoint(event) {
  let node = null
  let offset = 0
  if (document.caretPositionFromPoint) {
    const pos = document.caretPositionFromPoint(event.clientX, event.clientY)
    if (pos) { node = pos.offsetNode; offset = pos.offset }
  } else if (document.caretRangeFromPoint) {
    const range = document.caretRangeFromPoint(event.clientX, event.clientY)
    if (range) { node = range.startContainer; offset = range.startOffset }
  }
  const el = node && (node.nodeType === Node.TEXT_NODE ? node.parentElement : node)
  const seg = el?.closest?.('[data-start]')
  let caret = props.modelValue.length
  if (seg?.dataset.end) caret = Number(seg.dataset.end)
  else if (seg && node.nodeType === Node.TEXT_NODE) caret = Number(seg.dataset.start) + offset
  window.getSelection()?.removeAllRanges()
  showEdit(Math.min(caret, props.modelValue.length))
}
// 引用条点击：切到编辑并插在上次光标处；从没编辑过就追加到末尾。
async function insert(tag) {
  const live = mode.value === 'edit' && input.value && document.activeElement === input.value
  const sel = live ? { start: input.value.selectionStart, end: input.value.selectionEnd } : selection.value
  const { value, caret } = insertAtSelection(props.modelValue, tag, sel?.start, sel?.end)
  emit('update:modelValue', value)
  await showEdit(caret)
}
defineExpose({ insert })
</script>
<style scoped>
.prompt-ref-mode { display: flex; flex-wrap: wrap; align-items: center; gap: 6px; margin-bottom: 6px; }
.prompt-ref-mode small { color: var(--text-secondary); font-size: 12px; }
.prompt-ref-preview-box { overflow: auto; resize: vertical; min-height: 90px; font-size: 13px; line-height: 1.7; cursor: text; }
</style>
