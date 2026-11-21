<template>
  <button class="btn btn-sm btn-secondary" :disabled="!!blockedReason" :title="blockedReason" @click="open">场景参考设计</button>
  <Teleport to="body">
    <div v-if="visible" class="modal-mask location-design-mask" @click.self="close">
      <section class="modal card location-design" role="dialog" aria-modal="true" aria-labelledby="location-design-title">
        <header><h2 id="location-design-title">场景参考设计 · {{ location.name }}</h2><button class="btn btn-ghost" :disabled="busy" aria-label="关闭" @click="close">×</button></header>
        <p class="sub">用一张场景图的空间结构为底，把人物和道具放进这个场景，生成另一张场景画面。结果加入「参考图版本」，当前场景图不会被自动替换。</p>
        <fieldset :disabled="busy">
          <label>编辑引擎<select v-model="config.engine" class="input"><option v-for="e in engines" :key="e.code" :value="e.code" :disabled="!e.available">{{ e.label }} · 最多 {{ e.max_count }} 张{{ e.available ? '' : '（模板不可用）' }}</option></select></label>
          <p v-if="capacityError" class="error-text">{{ capacityError }}。不会自动删除或截断参考图。</p>
          <div class="reference-add"><label class="btn btn-sm btn-ghost">上传参考图<input hidden type="file" multiple accept="image/png,image/jpeg,image/webp" @change="upload"></label><select v-model="selectedFile" class="input"><option value="">选择项目已有图片</option><option v-for="file in files" :key="file" :value="file">{{ file }}</option></select><button class="btn btn-sm" :disabled="!selectedFile" @click="add(selectedFile)">添加</button></div>
          <div v-for="(r, index) in config.references" :key="r.file" class="design-reference">
            <a :href="api.inputUrl(projectId, r.file)" target="_blank" rel="noopener"><img :src="api.inputUrl(projectId, r.file)" :alt="`参考图 ${index+1}`"></a>
            <div>
              <strong>参考图 {{ index+1 }}</strong>
              <select v-model="r.slot" class="input"><option value="scene">场景参考（提供空间结构）</option><option value="character">人物参考（外形与服装）</option><option value="prop">道具参考（材质与造型）</option></select>
              <select v-model="r.presence" class="input"><option value="required">必须画进画面</option><option value="optional">仅作造型参考</option></select>
              <input v-model="r.responsibility" class="input" maxlength="1000" placeholder="参考重点（选填），如：立在第 3 级石阶右侧">
            </div>
            <div class="reference-actions"><button class="btn btn-xs btn-ghost" :disabled="index === 0" @click="move(index,-1)">上移</button><button class="btn btn-xs btn-ghost" :disabled="index === config.references.length-1" @click="move(index,1)">下移</button><button class="btn btn-xs btn-danger" @click="config.references.splice(index,1)">移除</button></div>
          </div>
          <p class="sub">顺序就是提示词里的参考编号（Qwen 用 &lt;imageN&gt;，H3-Image 用 &lt;Picture N&gt;）。移除选择不会删除原始图片。</p>
          <label>场景设计要求<textarea v-model="config.requirements" class="input" rows="3" maxlength="4000" placeholder="例如：黄昏，山门前的石阶上有两个弟子扫雪；人物大小要符合远景比例"></textarea></label>
          <button class="btn" :disabled="!!capacityError" @click="preview">{{ busy ? '处理中…' : 'AI编写提示词并审阅' }}</button>
        </fieldset>
        <template v-if="draft">
          <p class="sub">本次使用 Skill：{{ draft.skill || '项目已停用/无专用 Skill' }}。修改上方参考图、顺序、类型或引擎后须重新预览。</p>
          <textarea v-model="prompt" class="input" rows="9" :disabled="busy" aria-label="待审阅的生图提示词"></textarea>
          <label><input v-model="reviewed" type="checkbox" :disabled="busy">我已核对参考图、职责及提示词</label>
          <button class="btn" :disabled="busy || !reviewed || !prompt.trim()" @click="generate">确认并生成场景图</button>
        </template>
        <details v-if="designs.length"><summary>设计来源记录（最近30次）</summary><details v-for="d in designs" :key="d.id"><summary>#{{ d.id }} · {{ d.engine }} · {{ d.result_variant_id ? '已生成参考图版本 #' + d.result_variant_id : (d.task_id ? `任务 ${d.task_id}` : '未提交') }}</summary><p v-if="d.requirements">{{ d.requirements }}</p><p>Skill：{{ d.skill || '无' }}</p><div class="reference-add"><figure v-for="r in d.references" :key="r.id"><img :src="api.inputUrl(projectId,r.file)" :alt="r.slot || '参考'" width="64" height="64"><figcaption>{{ r.position }} · {{ slotLabel(r.slot) }} · {{ r.presence === 'required' ? '必须入画' : '仅参考' }} · {{ r.responsibility || '综合参考' }}</figcaption></figure></div><pre>{{ d.prompt }}</pre><p v-if="d.error" class="error-text">{{ d.error }}</p></details></details>
        <p v-if="error" class="error-text" role="alert">{{ error }}</p>
      </section>
    </div>
  </Teleport>
</template>

<script setup>
import { computed, reactive, ref, watch } from 'vue'
import { api } from '../api'
import { requestErrorMessage } from '../utils/requestError.js'
const props = defineProps({ projectId: { type: [Number,String], required: true }, location: { type: Object, required: true }, disabled: Boolean })
const emit = defineEmits(['submitted'])
const visible=ref(false), busy=ref(false), error=ref(''), files=ref([]), engines=ref([]), designs=ref([]), selectedFile=ref(''), draft=ref(null), prompt=ref(''), reviewed=ref(false)
const config=reactive({engine:'qwen_edit',requirements:'',references:[]})
const engine=computed(()=>engines.value.find(e=>e.code===config.engine))
// 按钮被禁用时必须给出原因，否则用户点了像没反应。
const blockedReason=computed(()=>props.location.image_task_id?'当前参考图任务正在生成，完成后才能做场景参考设计':props.disabled?'当前有其它资产操作正在进行，请稍候':'')
const capacityError=computed(()=>!engine.value?.available ? '所选模板不可用' : config.references.length<1 ? '至少需要一张参考图（通常先用当前场景图）' : config.references.length>engine.value.max_count ? `所选引擎最多支持${engine.value.max_count}张` : '')
watch(config,()=>{draft.value=null;reviewed.value=false},{deep:true,flush:'sync'})
watch(prompt,()=>{reviewed.value=false},{flush:'sync'})
watch(()=>[props.location.name,props.location.description,props.location.visual_type,props.location.mega_type,props.location.visible_text],()=>{draft.value=null;reviewed.value=false})
function slotLabel(slot){return slot==='character'?'人物':slot==='prop'?'道具':'场景'}
function close(){if(!busy.value)visible.value=false}
async function open(){visible.value=true;busy.value=true;error.value='';try{const {data}=await api.locationDesignOptions(props.projectId,props.location.id);files.value=data.files;engines.value=data.engines;designs.value=data.designs||[];draft.value=null;reviewed.value=false;if(!config.references.length&&props.location.image)config.references.push({file:props.location.image,slot:'scene',presence:'optional',responsibility:'沿用这张场景图的空间结构与光线'})}catch(e){error.value=requestErrorMessage(e)}finally{busy.value=false}}
// 场景图默认只作空间参考；之后加进来的人物与道具默认必须入画。
function defaultSlot(){return config.references.length===0?'scene':(config.references[0].slot==='scene'?'character':'prop')}
function add(file){if(config.references.some(r=>r.file===file)){error.value='这张图片已添加';return}const slot=defaultSlot();config.references.push({file,slot,presence:slot==='scene'?'optional':'required',responsibility:''});selectedFile.value=''}
function move(index,delta){const target=index+delta;if(target<0||target>=config.references.length)return;const [r]=config.references.splice(index,1);config.references.splice(target,0,r)}
async function upload(event){const chosen=[...(event.target.files||[])];event.target.value='';busy.value=true;error.value='';try{for(const file of chosen){if(file.size>25*1024*1024)throw new Error('参考图片不能超过25MB');const form=new FormData();form.append('file',file);const {data}=await api.uploadLocationDesignReference(props.projectId,props.location.id,form);files.value.unshift(data.file);add(data.file)}}catch(e){error.value=requestErrorMessage(e)}finally{busy.value=false}}
async function preview(){busy.value=true;error.value='';draft.value=null;try{const {data}=await api.previewLocationDesign(props.projectId,props.location.id,config);prompt.value=data.prompt;draft.value=data;reviewed.value=false}catch(e){error.value=requestErrorMessage(e)}finally{busy.value=false}}
async function generate(){if(!draft.value||!reviewed.value)return;busy.value=true;error.value='';try{await api.generateLocationDesign(props.projectId,props.location.id,{design_id:draft.value.id,prompt:prompt.value});visible.value=false;draft.value=null;emit('submitted')}catch(e){error.value=requestErrorMessage(e)}finally{busy.value=false}}
</script>

<style scoped>
/* 弹窗 Teleport 到 body：ProjectDetail 的 .modal-mask 是 scoped 规则，作用域哈希对不上这里，
   缺定位时弹窗会掉到文档末尾，看起来像点了没反应，所以本组件自带遮罩样式。 */
.location-design-mask{position:fixed;inset:0;background:rgba(0,0,0,.6);backdrop-filter:blur(10px);display:flex;align-items:center;justify-content:center;padding:24px;z-index:1400}
.location-design pre{white-space:pre-wrap;overflow-wrap:anywhere}.location-design figure{margin:6px;max-width:180px}.location-design{width:min(780px,94vw);max-height:90vh;overflow:auto;padding:22px;display:grid;gap:12px}.location-design header,.reference-add,.design-reference,.reference-actions{display:flex;align-items:center;gap:10px}.location-design header{justify-content:space-between}.location-design fieldset{border:0;margin:0;padding:0;display:grid;gap:12px;min-width:0}.location-design label{display:grid;gap:6px}.reference-add{flex-wrap:wrap}.reference-add select{flex:1;min-width:140px}.design-reference{padding:10px;border:1px solid var(--border);border-radius:8px;align-items:flex-start}.design-reference img{width:64px;height:64px;object-fit:cover;border-radius:6px}.design-reference>div{display:grid;gap:6px;flex:1;min-width:0}.design-reference .reference-actions{flex:0 0 auto}.error-text{color:var(--red);white-space:pre-wrap}.location-design textarea{width:100%;box-sizing:border-box}@media(max-width:600px){.design-reference{flex-wrap:wrap}.design-reference .reference-actions{display:flex;flex-basis:100%}}
</style>