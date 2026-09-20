<template>
  <div class="page fade-up rolling-page">
    <div class="head-links"><router-link :to="`/projects/${id}/story-bible`" class="back">← 故事圣经</router-link></div>
    <header class="page-head">
      <div><span class="overline">ROLLING ADAPTATION</span><h1>长篇滚动改编规划</h1><p class="sub">全剧可以是上千集；每次只展开5–20集，审核后再滚动下一批。</p></div>
      <router-link :to="`/projects/${id}`" class="btn btn-ghost">返回项目</router-link>
    </header>

    <div v-if="error" class="card error">{{ error }}</div>
    <section class="stats card">
      <div><input v-model.number="totalTarget" type="number" min="1" max="100000" class="input target-input"><span>全剧预计总集数</span><button class="btn btn-xs btn-ghost" @click="saveTarget">保存</button></div>
      <div><b>{{ batches.length }}</b><span>已建立批次</span></div>
      <div><b>{{ approvedEnd }}</b><span>已审核至第几集</span></div>
      <div><b>5–20</b><span>单次规划集数</span></div>
    </section>

    <section class="card create-batch">
      <div class="section-head"><div><h2>新建滚动批次</h2><p class="sub">选择原文章节和故事弧，只生成当前窗口，不重写已审核批次。</p></div></div>
      <div class="grid six">
        <label>批次标题<input v-model="form.title" class="input" placeholder="例如：第一卷·七玄门"></label>
        <label>起始集<input v-model.number="form.episode_start" type="number" min="1" class="input"></label>
        <label>结束集<input v-model.number="form.episode_end" type="number" :min="form.episode_start" class="input"></label>
        <label>起始章节<input v-model.number="form.chapter_start" type="number" min="1" class="input"></label>
        <label>结束章节<input v-model.number="form.chapter_end" type="number" :min="form.chapter_start" class="input"></label>
        <label>故事弧编号<input v-model="form.arc_range" class="input" placeholder="例如 1-3"></label>
      </div>
      <div class="batch-hint">本批 {{ batchCount }} 集。建议每批10集；范围必须为5–20集，且不能与已有批次重叠。</div>
      <button class="btn" :disabled="busy || batchCount < 5 || batchCount > 20" @click="createBatch">创建批次</button>
    </section>

    <section class="arcs-section">
      <div class="section-head"><div><h2>故事弧 / 分卷</h2><p class="sub">来自章节分析的可审核分卷，批次通过编号引用。</p></div><div class="actions"><button class="btn btn-sm btn-ghost" @click="createArc">新增故事弧</button><button class="btn btn-sm btn-ghost" :disabled="selectedArcIds.length<2" @click="mergeArcs">合并选中</button></div></div>
      <div class="arc-grid"><article v-for="arc in arcs" :key="arc.id" class="card arc"><header><label><input type="checkbox" :value="arc.id" v-model="selectedArcIds"> <strong>弧{{arc.arc_no}} · {{arc.title}}</strong></label><span class="badge">{{arc.review_status || arc.status}}</span></header><small>章节 {{arc.chapter_start}}–{{arc.chapter_end}}</small><p>{{arc.summary}}</p><div class="actions"><button class="btn btn-xs btn-secondary" :disabled="busy || arc.review_status==='approved'" @click="reviewArc(arc,'approved')">审核通过</button><button class="btn btn-xs btn-ghost" @click="editArc(arc)">编辑</button><button class="btn btn-xs btn-ghost" @click="splitArc(arc)">拆分</button><button class="btn btn-xs btn-danger" @click="removeArc(arc)">删除</button></div></article></div>
    </section>

    <section>
      <div class="section-head"><div><h2>规划批次</h2><p class="sub">AI输出先进入审核态；通过后才创建/更新Episode，并允许下一批。</p></div></div>
      <div v-if="!batches.length" class="card empty">尚未创建批次。</div>
      <article v-for="batch in batches" :key="batch.id" class="card batch" :class="`status-${batch.status}`">
        <header><div><h3>批次{{batch.batch_no}} · {{batch.title}}</h3><p>第{{batch.episode_start}}–{{batch.episode_end}}集 · 原文章节{{batch.chapter_start}}–{{batch.chapter_end}} · 故事弧{{batch.arc_range || '自动匹配'}}</p></div><span class="badge">{{ statusLabel(batch.status) }}</span></header>
        <p v-if="batch.summary" class="summary">{{batch.summary}}</p>
        <div v-if="generating === batch.id" class="progress"><div><strong>AI正在生成本批分集映射</strong><span>{{elapsed}}秒</span></div><div class="track"><span :style="{width:progress+'%'}"></span></div><small>正在加载故事圣经、故事弧、章节分析和上一批结束状态，并校验集数连续性。</small></div>
        <div class="actions">
          <button class="btn btn-sm" :disabled="busy || batch.status==='approved'" @click="generate(batch)">{{batch.status==='review'?'重新生成草稿':'生成本批方案'}}</button>
          <button class="btn btn-sm btn-secondary" :disabled="busy || batch.status!=='review'" @click="generateSnapshot(batch)">生成批次状态快照</button>
          <button class="btn btn-sm btn-secondary" :disabled="busy || batch.status!=='review'" @click="review(batch,'approve')">审核通过并应用Episode</button>
          <button class="btn btn-sm btn-ghost" :disabled="busy || batch.status!=='review'" @click="review(batch,'reject')">退回修改</button>
          <button class="btn btn-sm btn-ghost" @click="toggleDetail(batch)">{{detail?.batch?.id===batch.id?'收起':'查看分集'}}</button>
        </div>
        <div v-if="detail?.batch?.id===batch.id" class="episode-list">
          <article v-for="ep in detail.episodes" :key="ep.id">
            <div><input v-model="ep.title" class="input"><span>第{{ep.episode_n}}集 · 章节{{ep.chapter_start}}–{{ep.chapter_end}}</span></div>
            <textarea v-model="ep.adaptation_goal" class="textarea" rows="2" placeholder="本集改编目标"></textarea>
            <div class="state-grid"><label>开始状态<textarea v-model="ep.opening_state" class="textarea" rows="2"></textarea></label><label>结束状态<textarea v-model="ep.ending_state" class="textarea" rows="2"></textarea></label><label>结尾钩子<textarea v-model="ep.hook" class="textarea" rows="2"></textarea></label></div>
            <div class="actions"><button class="btn btn-xs btn-secondary" :disabled="batch.status==='approved'||busy" @click="saveEpisode(ep)">保存映射</button><button class="btn btn-xs" :disabled="ep.status!=='approved'||busy" @click="script(ep)">生成本集剧本</button><router-link class="btn btn-xs btn-ghost" :to="`/projects/${id}/episodes/${ep.episode_n}/screenplay`">结构化编辑</router-link></div>
          </article>
          <section v-if="snapshot?.batch_id===batch.id" class="snapshot-editor">
            <h4>批次结束状态快照 <span class="badge">{{snapshot.status}}</span></h4>
            <label>角色状态（修为/能力/所在地/道具）<textarea v-model="snapshot.character_states_json" class="textarea" rows="5"></textarea></label>
            <label>关系状态<textarea v-model="snapshot.relationships_json" class="textarea" rows="3"></textarea></label>
            <label>世界状态<textarea v-model="snapshot.world_state_json" class="textarea" rows="3"></textarea></label>
            <label>伏笔状态<textarea v-model="snapshot.clue_state_json" class="textarea" rows="5"></textarea></label>
            <div class="actions"><button class="btn btn-sm btn-secondary" @click="saveSnapshot(batch)">保存快照</button><button class="btn btn-sm" @click="approveSnapshot(batch)">连续性审核并通过</button></div>
          </section>
        </div>
      </article>
    </section>
  </div>
</template>

<script setup>
import { computed,onBeforeUnmount,onMounted,reactive,ref } from 'vue'
import { useRoute } from 'vue-router'
import { api } from '../api'
const id=useRoute().params.id,project=ref(null),arcs=ref([]),selectedArcIds=ref([]),batches=ref([]),detail=ref(null),snapshot=ref(null),busy=ref(false),error=ref(''),totalTarget=ref(0),generating=ref(0),elapsed=ref(0),progress=ref(0)
let timer=null
const nextStart=computed(()=>batches.value.length?Math.max(...batches.value.map(v=>v.episode_end))+1:1)
const form=reactive({title:'',episode_start:1,episode_end:10,chapter_start:1,chapter_end:8,arc_range:''})
const batchCount=computed(()=>Number(form.episode_end)-Number(form.episode_start)+1)
const approvedEnd=computed(()=>Math.max(0,...batches.value.filter(v=>v.status==='approved'||v.status==='produced').map(v=>v.episode_end)))
const statusLabel=s=>({draft:'待生成',generating:'生成中',review:'待审核',approved:'已审核',produced:'已生产',cancelled:'已取消'}[s]||s)
async function load(){try{const [p,a,b]=await Promise.all([api.project(id),api.novelArcs(id),api.planningBatches(id)]);project.value=p.data.project||p.data;arcs.value=a.data.arcs||a.data||[];batches.value=b.data.batches||[];totalTarget.value=b.data.total_episode_target||0;if(!form.title){form.episode_start=nextStart.value;form.episode_end=nextStart.value+9;form.title=`第${batches.value.length+1}批`}}catch(e){error.value=e.response?.data?.error||e.message}}
async function run(fn){busy.value=true;error.value='';try{await fn();await load()}catch(e){error.value=e.response?.data?.error||e.message}finally{busy.value=false}}
const createBatch=()=>run(async()=>{await api.createPlanningBatch(id,{...form});form.title='';detail.value=null})
const saveTarget=()=>run(()=>api.updatePlanningTarget(id,totalTarget.value))
async function reviewArc(arc,status){const notes=window.prompt(status==='approved'?'审核意见（可选）':'退回原因','');if(notes===null)return;await run(()=>api.reviewNovelArc(id,arc.id,status,notes))}
async function createArc(){const title=window.prompt('故事弧标题','新故事弧');if(!title)return;const range=window.prompt('章节范围，例如 1-20','1-20');if(!range)return;const [chapter_start,chapter_end]=range.split('-').map(Number);await run(()=>api.createNovelArc(id,{title,summary:'',chapter_start,chapter_end}))}
async function editArc(arc){const title=window.prompt('标题',arc.title);if(title===null)return;const summary=window.prompt('摘要',arc.summary||'');if(summary===null)return;await run(()=>api.updateNovelArc(id,arc.id,{title,summary,chapter_start:arc.chapter_start,chapter_end:arc.chapter_end}))}
async function splitArc(arc){const split_chapter=Number(window.prompt(`在章节 ${arc.chapter_start+1}–${arc.chapter_end} 的哪章拆分`,String(arc.chapter_start+1)));if(!split_chapter)return;await run(()=>api.splitNovelArc(id,arc.id,{split_chapter,second_title:`${arc.title}（下）`}))}
async function removeArc(arc){if(!window.confirm(`删除故事弧「${arc.title}」？`))return;await run(()=>api.deleteNovelArc(id,arc.id))}
async function mergeArcs(){const title=window.prompt('合并后的标题','合并故事弧');if(!title)return;await run(()=>api.mergeNovelArcs(id,{arc_ids:selectedArcIds.value,title}));selectedArcIds.value=[]}
async function generate(batch){busy.value=true;generating.value=batch.id;elapsed.value=0;progress.value=4;timer=setInterval(()=>{elapsed.value++;progress.value=Math.min(92,4+Math.round(88*(1-Math.exp(-elapsed.value/45))))},1000);try{detail.value=(await api.generatePlanningBatch(id,batch.id)).data;await load()}catch(e){error.value=e.response?.data?.error||e.message}finally{clearInterval(timer);timer=null;generating.value=0;busy.value=false}}
async function review(batch,action){const notes=window.prompt(action==='approve'?'审核意见（可选）':'退回原因','') ;if(notes===null)return;await run(()=>api.reviewPlanningBatch(id,batch.id,action,notes));detail.value=null}
async function toggleDetail(batch){if(detail.value?.batch?.id===batch.id){detail.value=null;snapshot.value=null;return}try{detail.value=(await api.planningBatch(id,batch.id)).data;try{snapshot.value=(await api.batchSnapshot(id,batch.id)).data}catch{snapshot.value=null}}catch(e){error.value=e.response?.data?.error||e.message}}
const saveEpisode=ep=>run(()=>api.updateAdaptation(id,ep.episode_n,{title:ep.title,adaptation_goal:ep.adaptation_goal,opening_state:ep.opening_state,ending_state:ep.ending_state,hook:ep.hook}))
async function generateSnapshot(batch){await run(async()=>{snapshot.value=(await api.generateBatchSnapshot(id,batch.id)).data;detail.value=(await api.planningBatch(id,batch.id)).data})}
const saveSnapshot=batch=>run(async()=>{snapshot.value=(await api.saveBatchSnapshot(id,batch.id,{character_states_json:snapshot.value.character_states_json,relationships_json:snapshot.value.relationships_json,world_state_json:snapshot.value.world_state_json,clue_state_json:snapshot.value.clue_state_json})).data})
async function approveSnapshot(batch){const override=window.prompt('连续性不一致时的人工覆盖理由（无则留空）','');if(override===null)return;await run(async()=>{snapshot.value=(await api.reviewBatchSnapshot(id,batch.id,true,override)).data})}
const script=ep=>run(()=>api.generateAdaptationScript(id,ep.episode_n))
onMounted(load);onBeforeUnmount(()=>clearInterval(timer))
</script>

<style scoped>
.rolling-page{max-width:1200px;margin:auto}.page-head,.section-head,.batch header,.arc header,.progress>div:first-child,.episode-list article>div:first-child{display:flex;justify-content:space-between;gap:16px;align-items:center}.stats{display:grid;grid-template-columns:repeat(4,1fr);margin:18px 0;padding:18px}.stats div{display:grid;text-align:center;gap:5px}.stats b{font-size:26px;color:#8c83ff}.stats span,.batch small,.arc small{color:#8f98aa}.create-batch{padding:20px;margin-bottom:24px}.grid{display:grid;gap:12px}.six{grid-template-columns:2fr repeat(5,1fr)}.grid label{display:grid;gap:6px}.batch-hint{margin:12px 0;color:#8f98aa}.arc-grid{display:grid;grid-template-columns:repeat(3,1fr);gap:12px;margin-bottom:24px}.arc,.batch{padding:18px}.batch{margin-bottom:14px}.batch header h3{margin:0}.summary{padding:10px;background:#ffffff0a;border-radius:8px}.actions{display:flex;gap:8px;flex-wrap:wrap;margin-top:12px}.progress{padding:12px;margin:12px 0;background:#6257d914;border-radius:8px}.track{height:8px;margin:10px 0;background:#ffffff14;border-radius:99px;overflow:hidden}.track span{display:block;height:100%;background:linear-gradient(90deg,#6257d9,#52b8e8)}.episode-list{display:grid;gap:9px;margin-top:14px}.episode-list article{padding:12px;border:1px solid #ffffff14;border-radius:8px}.episode-list p{margin:7px 0}.state-grid{display:grid;grid-template-columns:repeat(3,1fr);gap:8px}.state-grid label,.snapshot-editor label{display:grid;gap:5px}.snapshot-editor{padding:16px;border:1px solid #6257d955;border-radius:10px;display:grid;gap:10px}.target-input{text-align:center;font-size:22px;font-weight:700}.empty{text-align:center;padding:35px}.error{padding:14px;color:var(--red)}@media(max-width:900px){.six{grid-template-columns:1fr 1fr}.arc-grid,.stats{grid-template-columns:1fr 1fr}}@media(max-width:560px){.six,.arc-grid,.stats{grid-template-columns:1fr}.page-head{display:block}}
</style>
