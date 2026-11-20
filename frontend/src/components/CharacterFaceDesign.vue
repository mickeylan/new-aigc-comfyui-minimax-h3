<template>
  <button class="btn btn-sm btn-secondary" :disabled="!!blockedReason" :title="blockedReason" @click="open">参考人脸设计</button>
  <Teleport to="body">
    <div v-if="visible" class="modal-mask face-design-mask" @click.self="close">
      <section class="modal card face-design" role="dialog" aria-modal="true" aria-labelledby="face-design-title">
        <header><h2 id="face-design-title">参考人脸设计 · {{ character.name }}</h2><button class="btn btn-ghost" :disabled="busy" aria-label="关闭" @click="close">×</button></header>
        <p class="sub">综合参考设计独立的新角色，不是机械叠脸或遗传预测。只参考面部，年龄、服装和画风以角色资料为准。结果沿用标准像历史，可恢复旧图。</p>
        <fieldset :disabled="busy">
          <label>设计方式<select v-model="config.preset" class="input"><option value="dual_parent">双亲参考 · 设计子女相貌</option><option value="multi_face_fusion">多脸融合 · 设计新人物</option></select></label>
          <label>编辑引擎<select v-model="config.engine" class="input"><option v-for="e in engines" :key="e.code" :value="e.code" :disabled="!e.available">{{ e.label }} · 最多 {{ e.max_count }} 张{{ e.available ? '' : '（模板不可用）' }}</option></select></label>
          <p v-if="capacityError" class="error-text">{{ capacityError }}。不会自动删除或截断参考图。</p>
          <div class="reference-add"><label class="btn btn-sm btn-ghost">上传参考图<input hidden type="file" multiple accept="image/png,image/jpeg,image/webp" @change="upload"></label><select v-model="selectedFile" class="input"><option value="">选择项目已有图片</option><option v-for="file in files" :key="file" :value="file">{{ file }}</option></select><button class="btn btn-sm" :disabled="!selectedFile" @click="add(selectedFile)">添加</button></div>
          <div v-for="(r, index) in config.references" :key="r.file" class="face-reference">
            <a :href="api.inputUrl(projectId, r.file)" target="_blank" rel="noopener"><img :src="api.inputUrl(projectId, r.file)" :alt="`参考图 ${index+1}`"></a>
            <div><strong>参考图 {{ index+1 }}</strong><select v-if="config.preset === 'dual_parent'" v-model="r.slot" class="input"><option value="father">父亲长相参考</option><option value="mother">母亲长相参考</option></select><input v-model="r.responsibility" class="input" maxlength="1000" placeholder="参考重点（选填），如：眼型偏这张图"></div>
            <div class="reference-actions"><button class="btn btn-xs btn-ghost" :disabled="index === 0" @click="move(index,-1)">上移</button><button class="btn btn-xs btn-ghost" :disabled="index === config.references.length-1" @click="move(index,1)">下移</button><button class="btn btn-xs btn-danger" @click="config.references.splice(index,1)">移除</button></div>
          </div>
          <p class="sub">顺序用于参考编号，不代表精确融合权重。移除选择不会删除原始图片。</p>
          <label>整体设计要求<textarea v-model="config.requirements" class="input" rows="3" maxlength="4000" placeholder="例如：眼型偏母亲、鼻形偏父亲；或希望新脸与所有参考人物明显不同"></textarea></label>
          <button class="btn" :disabled="!!capacityError || !validSlots" @click="preview">{{ busy ? '处理中…' : 'AI编写提示词并审阅' }}</button>
        </fieldset>
        <template v-if="draft">
          <p class="sub">本次使用 Skill：{{ draft.skill || '项目已停用' }}。修改上方参考图、顺序、职责或引擎后须重新预览。</p>
          <textarea v-model="prompt" class="input" rows="9" :disabled="busy" aria-label="待审阅的生图提示词"></textarea>
          <label><input v-model="reviewed" type="checkbox" :disabled="busy">我已核对参考图、职责及提示词</label>
          <button class="btn" :disabled="busy || !reviewed || !prompt.trim()" @click="generate">确认并生成标准像</button>
        </template>
        <details v-if="designs.length"><summary>设计来源记录（最近30次）</summary><details v-for="d in designs" :key="d.id"><summary>#{{ d.id }} · {{ d.preset === 'dual_parent' ? '双亲参考' : '多脸融合' }} · {{ d.engine }} · {{ d.task_id ? `任务 ${d.task_id}` : '未提交' }}</summary><p>{{ d.requirements }}</p><p>Skill：{{ d.skill }}</p><div class="reference-add"><figure v-for="r in d.references" :key="r.id"><img :src="api.inputUrl(projectId,r.file)" :alt="r.slot || '融合参考'" width="64" height="64"><figcaption>{{ r.position }} · {{ r.slot }} · {{ r.responsibility || '综合参考' }}</figcaption></figure></div><pre>{{ d.prompt }}</pre></details></details>
        <p v-if="error" class="error-text" role="alert">{{ error }}</p>
      </section>
    </div>
  </Teleport>
</template>

<script setup>
import { computed, reactive, ref, watch } from 'vue'
import { api } from '../api'
import { requestErrorMessage } from '../utils/requestError.js'
const props = defineProps({ projectId: { type: [Number,String], required: true }, character: { type: Object, required: true }, disabled: Boolean })
const emit = defineEmits(['submitted'])
const visible=ref(false), busy=ref(false), error=ref(''), files=ref([]), engines=ref([]), designs=ref([]), selectedFile=ref(''), draft=ref(null), prompt=ref(''), reviewed=ref(false)
const config=reactive({preset:'dual_parent',engine:'qwen_edit',requirements:'',references:[]})
const engine=computed(()=>engines.value.find(e=>e.code===config.engine))
// 按钮被禁用时必须给出原因：此前禁用条件只在 disabled 属性里，点不动也没有任何提示。
const blockedReason=computed(()=>props.character.profile_status!=='approved'?'请先审核通过角色档案，才能用参考人脸设计':props.character.portrait_task_id?'标准像任务正在生成，完成后才能用参考人脸设计':props.disabled?'当前有其它角色操作正在进行，请稍候':'')
const capacityError=computed(()=>!engine.value?.available ? '所选模板不可用' : config.references.length<2 ? '至少需要两张不同参考图' : config.preset==='dual_parent' && config.references.length!==2 ? '双亲参考需要两张图片' : config.references.length>engine.value.max_count ? `所选引擎最多支持${engine.value.max_count}张` : '')
const validSlots=computed(()=>config.preset!=='dual_parent' || config.references.filter(r=>r.slot==='father').length===1 && config.references.filter(r=>r.slot==='mother').length===1)
watch(config,()=>{draft.value=null;reviewed.value=false},{deep:true,flush:'sync'})
watch(prompt,()=>{reviewed.value=false},{flush:'sync'})
watch(()=>[props.character.appearance,props.character.gender,props.character.trait,props.character.wardrobe_detail,props.character.profile_status],()=>{draft.value=null;reviewed.value=false})
function close(){if(!busy.value)visible.value=false}
async function open(){visible.value=true;busy.value=true;error.value='';try{const {data}=await api.faceDesignOptions(props.projectId,props.character.id);files.value=data.files;engines.value=data.engines;designs.value=data.designs||[];draft.value=null;reviewed.value=false}catch(e){error.value=requestErrorMessage(e)}finally{busy.value=false}}
function add(file){if(config.references.some(r=>r.file===file)){error.value='这张图片已添加';return}config.references.push({file,slot:config.references.length===0?'father':'mother',responsibility:''});selectedFile.value=''}
function move(index,delta){const target=index+delta;if(target<0||target>=config.references.length)return;const [r]=config.references.splice(index,1);config.references.splice(target,0,r)}
async function upload(event){const chosen=[...(event.target.files||[])];event.target.value='';busy.value=true;error.value='';try{for(const file of chosen){if(file.size>25*1024*1024)throw new Error('参考图片不能超过25MB');const form=new FormData();form.append('file',file);const {data}=await api.uploadFaceDesignReference(props.projectId,props.character.id,form);files.value.unshift(data.file);add(data.file)}}catch(e){error.value=requestErrorMessage(e)}finally{busy.value=false}}
async function preview(){busy.value=true;error.value='';draft.value=null;try{const {data}=await api.previewFaceDesign(props.projectId,props.character.id,config);prompt.value=data.prompt;draft.value=data;reviewed.value=false}catch(e){error.value=requestErrorMessage(e)}finally{busy.value=false}}
async function generate(){if(!draft.value||!reviewed.value)return;busy.value=true;error.value='';try{await api.generateFaceDesign(props.projectId,props.character.id,{design_id:draft.value.id,prompt:prompt.value});visible.value=false;draft.value=null;emit('submitted')}catch(e){error.value=requestErrorMessage(e)}finally{busy.value=false}}
</script>

<style scoped>
/* 弹窗 Teleport 到 body，ProjectDetail 里 .modal-mask 是 scoped 规则，作用域哈希对不上这里，
   缺了定位时整个弹窗会掉到文档末尾（页面底部）等于点了没反应，所以本组件必须自带遮罩样式。 */
.face-design-mask{position:fixed;inset:0;background:rgba(0,0,0,.6);backdrop-filter:blur(10px);display:flex;align-items:center;justify-content:center;padding:24px;z-index:1400}
.face-design pre{white-space:pre-wrap;overflow-wrap:anywhere}.face-design figure{margin:6px;max-width:180px}.face-design{width:min(760px,94vw);max-height:90vh;overflow:auto;padding:22px;display:grid;gap:12px}.face-design header,.reference-add,.face-reference,.reference-actions{display:flex;align-items:center;gap:10px}.face-design header{justify-content:space-between}.face-design fieldset{border:0;margin:0;padding:0;display:grid;gap:12px;min-width:0}.face-design label{display:grid;gap:6px}.reference-add{flex-wrap:wrap}.reference-add select{flex:1;min-width:140px}.face-reference{padding:10px;border:1px solid var(--border);border-radius:8px}.face-reference img{width:64px;height:64px;object-fit:cover;border-radius:6px}.face-reference>div{display:grid;gap:6px;flex:1;min-width:0}.face-reference .reference-actions{flex:0 0 auto}.error-text{color:var(--red);white-space:pre-wrap}.face-design textarea{width:100%;box-sizing:border-box}@media(max-width:600px){.face-reference{flex-wrap:wrap}.face-reference .reference-actions{display:flex;flex-basis:100%}}
</style>
