<template>
  <section class="design-questions">
    <button type="button" class="btn btn-sm btn-secondary" :disabled="disabled" @click="open = !open">{{ open ? '收起设计问答' : '先回答几个设计问题（可跳过）' }}</button>
    <div v-if="open">
      <p class="field-hint">只将你确认的答案加入本次想法，不自动存为长期方向；留空不会被当成已确认设定。</p>
      <label>希望如何处理已有设定？
        <select v-model="approach" class="input" :disabled="disabled">
          <option value="">暂不指定</option>
          <option>尽量沿用，只改我明确指出的部分</option>
          <option>允许提出新方案，但新增细节要标为 AI 建议</option>
        </select>
      </label>
      <label>{{ kind === 'location' ? '希望的空间与氛围' : kind === 'prop' ? '希望的形制与材质' : '希望调整的人物特征' }}
        <textarea v-model="focus" class="textarea" rows="2" :disabled="disabled" placeholder="可留空，不猜测参考图细节" />
      </label>
      <fieldset :disabled="disabled"><legend>优先关注哪些方面？（可多选）</legend><label v-for="item in ['轮廓结构', '材质细节', '色彩氛围', '身份辨识度']" :key="item"><input v-model="priorities" type="checkbox" :value="item" /> {{ item }}</label></fieldset>
      <label>哪些内容必须保留？<textarea v-model="keep" class="textarea" rows="2" :disabled="disabled" /></label>
      <label>有哪些不能出现的设计？<textarea v-model="avoid" class="textarea" rows="2" :disabled="disabled" /></label>
      <div class="actions">
        <button type="button" class="btn btn-sm" :disabled="disabled || !answer" @click="submit">确认答案并加入本次想法</button>
        <button type="button" class="btn btn-sm btn-ghost" :disabled="disabled" @click="skip">跳过本组问答</button>
      </div>
    </div>
    <p v-if="status" class="field-hint" role="status">{{ status }}</p>
  </section>
</template>
<script setup>
import { computed, ref } from 'vue'
const props = defineProps({ kind: { type: String, default: '' }, disabled: Boolean })
const emit = defineEmits(['answer'])
const open = ref(false)
const approach = ref('')
const focus = ref('')
const priorities = ref([])
const keep = ref('')
const avoid = ref('')
const status = ref('')
const answer = computed(() => [['修改方式', approach.value], ['设计重点', focus.value], ['优先关注', priorities.value.join('、')], ['必须保留', keep.value], ['不得出现', avoid.value]].filter(([, value]) => value.trim()).map(([label, value]) => `${label}：${value.trim()}`).join('\n'))
function clear() { priorities.value = []; approach.value = ''; focus.value = ''; keep.value = ''; avoid.value = ''; open.value = false }
function submit() {
  if (props.disabled || !answer.value) return
  emit('answer', answer.value)
  status.value = '答案已加入本次想法，尚未生成或保存。可在想法框中继续修改。'
  clear()
}
function skip() {
  if (props.disabled) return
  clear()
  status.value = '已跳过问答；不代表认可任何方案。'
}
</script>
<style scoped>
.design-questions label { display: block; margin: 8px 0; font-size: 13px; }
.design-questions .input, .design-questions .textarea { display: block; width: 100%; margin-top: 4px; }
</style>
