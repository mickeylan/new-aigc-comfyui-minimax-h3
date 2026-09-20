<template>
  <div class="page screenplay-page">
    <header class="screenplay-head">
      <div>
        <router-link :to="`/projects/${projectId}`" class="back">← 返回项目</router-link>
        <span class="overline">STRUCTURED SCREENPLAY</span>
        <h1>第{{ episodeN }}集 · 结构化剧本</h1>
        <p class="sub">Scene 是剧情与生成权威；保存后同步对白、导演拆镜及后续图片/视频流水线。</p>
      </div>
      <div class="head-actions">
        <button class="btn btn-ghost" :disabled="busy" @click="load">重新载入</button>
        <button class="btn" :disabled="busy || !dirty || !scenes.length" @click="save">{{ busy ? '保存中…' : '保存并同步流水线' }}</button>
      </div>
    </header>

    <section class="creative-actions card">
      <div>
        <strong>上游创作更新</strong>
        <p>修改项目创意后，先生成创作方案，再重新生成本集剧本；两步都会调用现有项目流水线。</p>
      </div>
      <div class="head-actions">
        <button class="btn btn-secondary" :disabled="busy || generatingPlan || !project?.synopsis" @click="generateCreativePlan">
          {{ generatingPlan ? '方案生成中…' : (project?.plan ? '1. 重新生成创作方案' : '1. 生成创作方案') }}
        </button>
        <button class="btn" :disabled="busy || generatingScript || !project?.synopsis" @click="regenerateEpisodeScript">
          {{ generatingScript ? '剧本生成中…' : `2. 重新生成第${episodeN}集剧本` }}
        </button>
      </div>
    </section>

    <section class="pipeline card">
      <strong>下游生产链路</strong>
      <span class="active">剧本块</span><b>→</b><span>Scene / Dialogue</span><b>→</b><span>AI导演 Shot</span><b>→</b><span>分镜图</span><b>→</b><span>视频 / 配音</span><b>→</b><span>合并</span>
      <router-link class="btn btn-sm btn-secondary" :to="`/projects/${projectId}/editor?episode=${episodeN}`">进入导演与剪辑台</router-link>
    </section>

    <div v-if="loading" class="card empty">正在加载剧本…</div>
    <div v-else-if="!scenes.length" class="card empty">
      本集暂无场景。<button class="btn btn-sm" @click="addScene">创建第一场</button>
    </div>

    <main v-else class="scene-list">
      <article v-for="(scene, si) in scenes" :key="scene.key" class="scene-card card">
        <aside class="scene-index">{{ String(si + 1).padStart(2, '0') }}</aside>
        <div class="scene-body">
          <div class="scene-toolbar">
            <label class="block-label">场景标题</label>
            <div class="row-actions">
              <button class="btn btn-xs btn-ghost" :disabled="si === 0" @click="moveScene(si, -1)">上移</button>
              <button class="btn btn-xs btn-ghost" :disabled="si === scenes.length - 1" @click="moveScene(si, 1)">下移</button>
              <button class="btn btn-xs btn-danger" @click="removeScene(si)">删除场景</button>
            </div>
          </div>
          <input v-model="scene.heading" class="input heading-input" placeholder="例如：INT. 玉霄宫·内殿 - 清晨" @input="touch" />

          <div v-for="(block, bi) in scene.elements" :key="block.key" class="script-block" :class="`block-${block.type}`">
            <div class="block-head">
              <select v-model="block.type" class="block-type" @change="touch">
                <option value="action">动作</option>
                <option value="character">角色</option>
                <option value="parenthetical">表演提示</option>
                <option value="dialogue">对白</option>
                <option value="transition">转场</option>
              </select>
              <div class="row-actions">
                <button class="btn btn-xs btn-ghost" :disabled="bi === 0" @click="moveBlock(scene, bi, -1)">↑</button>
                <button class="btn btn-xs btn-ghost" :disabled="bi === scene.elements.length - 1" @click="moveBlock(scene, bi, 1)">↓</button>
                <button class="btn btn-xs btn-ghost" @click="removeBlock(scene, bi)">删除</button>
              </div>
            </div>
            <input v-if="block.type === 'character'" v-model="block.text" class="input character-input" placeholder="说话角色姓名" @input="touch" />
            <input v-else-if="block.type === 'parenthetical'" v-model="block.text" class="input parenthetical-input" placeholder="例如：压低声音、急促地" @input="touch" />
            <input v-else-if="block.type === 'transition'" v-model="block.text" class="input transition-input" placeholder="例如：CUT TO" @input="touch" />
            <textarea v-else v-model="block.text" class="textarea block-text" :placeholder="block.type === 'dialogue' ? '逐字对白；只有这里会进入结构化发声内容' : '可见动作、环境与剧情事实'" @input="touch" />
          </div>

          <div class="add-blocks">
            <button class="btn btn-sm btn-ghost" @click="addBlock(scene, 'action')">＋动作</button>
            <button class="btn btn-sm btn-ghost" @click="addDialogue(scene)">＋角色对白</button>
            <button class="btn btn-sm btn-ghost" @click="addNarration(scene)">＋旁白</button>
            <button class="btn btn-sm btn-ghost" @click="addBlock(scene, 'transition')">＋转场</button>
          </div>
        </div>
      </article>
    </main>

    <button v-if="scenes.length" class="btn btn-secondary add-scene" @click="addScene">＋新增场景</button>

    <footer class="save-bar" v-if="dirty">
      <span>有未保存修改</span>
      <button class="btn" :disabled="busy" @click="save">保存并同步流水线</button>
    </footer>
  </div>
</template>

<script setup>
import { onBeforeUnmount, onMounted, ref } from 'vue'
import { onBeforeRouteLeave, useRoute } from 'vue-router'
import { api } from '../api'
import { useToastStore } from '../stores/toast'

const route = useRoute()
const toast = useToastStore()
const projectId = Number(route.params.id)
const episodeN = Number(route.params.episode)
const project = ref(null)
const scenes = ref([])
const loading = ref(true)
const busy = ref(false)
const generatingPlan = ref(false)
const generatingScript = ref(false)
const dirty = ref(false)
let keySeq = 0
const key = () => `draft-${Date.now()}-${++keySeq}`
const hydrate = data => (data.scenes || []).map(scene => ({ key: key(), heading: scene.heading || '', elements: (scene.elements || []).map(block => ({ ...block, key: key() })) }))
const payload = () => ({ format: 'fountain', title: `第${episodeN}集`, scenes: scenes.value.map(scene => ({ heading: scene.heading.trim(), elements: scene.elements.filter(b => b.text.trim()).map(({ type, text }) => ({ type, text: text.trim() })) })) })
const touch = () => { dirty.value = true }

async function load() {
  if (dirty.value && !window.confirm('重新载入会丢失未保存修改，继续吗？')) return
  loading.value = true
  try {
    const [screenplayRes, projectRes] = await Promise.all([api.episodeScreenplay(projectId, episodeN), api.project(projectId)])
    scenes.value = hydrate(screenplayRes.data)
    project.value = projectRes.data.project || projectRes.data
    dirty.value = false
  }
  catch (e) { toast.error(e.response?.data?.error || '加载结构化剧本失败') }
  finally { loading.value = false }
}
function addScene() { scenes.value.push({ key: key(), heading: `SCENE ${scenes.value.length + 1}`, elements: [{ key: key(), type: 'action', text: '' }] }); touch() }
function removeScene(index) { if (window.confirm('删除此场会在保存时清理其分镜、对白及派生媒体。系统会先建立修订快照，确定继续吗？')) { scenes.value.splice(index, 1); touch() } }
function moveScene(index, delta) { const next = index + delta; if (next < 0 || next >= scenes.value.length) return; const [item] = scenes.value.splice(index, 1); scenes.value.splice(next, 0, item); touch() }
function addBlock(scene, type) { scene.elements.push({ key: key(), type, text: '' }); touch() }
function addDialogue(scene) { scene.elements.push({ key: key(), type: 'character', text: '' }, { key: key(), type: 'dialogue', text: '' }); touch() }
function addNarration(scene) { scene.elements.push({ key: key(), type: 'character', text: '旁白' }, { key: key(), type: 'dialogue', text: '' }); touch() }
function removeBlock(scene, index) { scene.elements.splice(index, 1); touch() }
function moveBlock(scene, index, delta) { const next = index + delta; if (next < 0 || next >= scene.elements.length) return; const [item] = scene.elements.splice(index, 1); scene.elements.splice(next, 0, item); touch() }
async function save() {
  const body = payload()
  if (!body.scenes.length || body.scenes.some(s => !s.heading)) { toast.error('每个场景都必须填写场景标题'); return }
  busy.value = true
  try {
    await api.applyScreenplayImport(projectId, episodeN, body)
    dirty.value = false
    toast.success('剧本已保存，并同步到 Scene、Dialogue 与后续生成流水线')
    await load()
  } catch (e) { toast.error(e.response?.data?.error || '保存结构化剧本失败') }
  finally { busy.value = false }
}
async function generateCreativePlan() {
  if (dirty.value) { toast.error('请先保存或放弃当前剧本修改，再更新创作方案'); return }
  if (project.value?.plan && !window.confirm('重新生成创作方案会更新角色与分集规划。之后还需重新生成本集剧本，确定继续吗？')) return
  busy.value = true; generatingPlan.value = true
  try {
    const { data } = await api.generatePlan(projectId)
    project.value = data.project || project.value
    toast.success('创作方案已生成。现在可执行第2步：重新生成本集剧本')
  } catch (e) { toast.error(e.response?.data?.error || '生成创作方案失败') }
  finally { generatingPlan.value = false; busy.value = false }
}
async function regenerateEpisodeScript() {
  if (dirty.value) { toast.error('请先保存或放弃当前剧本修改，再重新生成剧本'); return }
  if (!window.confirm(`重新生成第${episodeN}集剧本会替换本集 Scene、Shot、Dialogue，并使旧媒体失效；系统会先保存安全版本。确定继续吗？`)) return
  busy.value = true; generatingScript.value = true
  try {
    await api.generateScript(projectId, episodeN)
    await load()
    toast.success(`第${episodeN}集剧本已按最新创作方案重新生成`)
  } catch (e) { toast.error(e.response?.data?.error || '重新生成剧本失败') }
  finally { generatingScript.value = false; busy.value = false }
}
function beforeUnload(event) { if (!dirty.value) return; event.preventDefault(); event.returnValue = '' }
onMounted(() => { window.addEventListener('beforeunload', beforeUnload); load() })
onBeforeUnmount(() => window.removeEventListener('beforeunload', beforeUnload))
onBeforeRouteLeave(() => !dirty.value || window.confirm('有未保存的剧本修改，确定离开吗？'))
</script>

<style scoped>
.screenplay-page{max-width:1180px;margin:0 auto;padding-bottom:90px}.screenplay-head{display:flex;justify-content:space-between;gap:24px;align-items:flex-end;margin-bottom:20px}.screenplay-head h1{margin:8px 0}.head-actions,.row-actions,.add-blocks{display:flex;gap:8px;align-items:center;flex-wrap:wrap}.creative-actions{display:flex;justify-content:space-between;gap:20px;align-items:center;margin-bottom:14px;border-left:4px solid #6257d9}.creative-actions p{margin:5px 0 0;color:#8f98aa}.pipeline{display:flex;gap:10px;align-items:center;flex-wrap:wrap;margin-bottom:20px}.pipeline span{padding:5px 9px;border-radius:999px;background:var(--surface-2,#20242d)}.pipeline .active{color:#fff;background:#6257d9}.pipeline .btn{margin-left:auto}.scene-list{display:grid;gap:18px}.scene-card{display:grid;grid-template-columns:64px 1fr;padding:0;overflow:hidden}.scene-index{padding:24px 12px;background:#151821;color:#8f98aa;font-size:22px;font-weight:700;text-align:center}.scene-body{padding:22px}.scene-toolbar,.block-head{display:flex;justify-content:space-between;gap:12px;align-items:center}.block-label{font-size:12px;color:#8f98aa;text-transform:uppercase;letter-spacing:.08em}.heading-input{font-weight:700;font-size:17px;margin:8px 0 18px}.script-block{border-left:3px solid #4f596b;padding:10px 0 10px 14px;margin:10px 0}.block-dialogue{border-color:#6257d9;margin-left:12%}.block-character{border-color:#ce8b42;margin-left:20%;max-width:55%}.block-parenthetical{border-color:#8f98aa;margin-left:17%;max-width:65%}.block-transition{border-color:#4ea77c;margin-left:35%}.block-type{background:transparent;color:inherit;border:0;font-weight:700}.block-text{min-height:86px;margin-top:8px}.character-input,.parenthetical-input,.transition-input{margin-top:8px}.add-blocks{padding-top:10px;border-top:1px solid rgba(255,255,255,.08)}.add-scene{display:block;margin:20px auto}.empty{text-align:center;padding:50px}.save-bar{position:fixed;left:50%;bottom:20px;transform:translateX(-50%);z-index:10;display:flex;gap:20px;align-items:center;padding:12px 18px;border-radius:12px;background:#191d27;box-shadow:0 10px 35px #0008}.back{display:block;margin-bottom:8px}@media(max-width:760px){.screenplay-head{align-items:stretch;flex-direction:column}.scene-card{grid-template-columns:42px 1fr}.scene-body{padding:14px}.block-dialogue,.block-character,.block-parenthetical,.block-transition{margin-left:0;max-width:none}.pipeline b{display:none}}
</style>
