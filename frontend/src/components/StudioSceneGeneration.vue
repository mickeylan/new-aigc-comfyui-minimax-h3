<template>
  <section class="studio-scene-generation card" aria-label="AI布景与场景生成">
    <div class="field-label-actions"><strong>同一 AI 布景 → 场景图 / 分镜图 / 视频</strong><router-link class="btn btn-sm btn-secondary" target="_blank" :to="{ path: '/studio/scene', query: { project_id: projectId, scene_id: sceneId } }">打开本场 AI 布景与取景</router-link></div>
    <p class="field-hint">在 Studio 对话建景、确认设计，再用「机位出图」按导演 Shot 自动取景，或导出逐镜视频。图像仍走现有待审候选，视频仍从「3D结构参考视频」选择；不改生成模板、四视图或镜序，不会自动生成。</p>
    <button type="button" class="btn btn-sm btn-ghost" :disabled="loading || adopting" @click="loadSources">刷新布景来源与候选</button>
    <p v-if="error" role="alert" class="warning">{{ error }}</p>
    <p v-if="imageSource" class="field-hint">当前场景图同源版本 {{ imageSource.revision.slice(0,12) }}</p>
    <p v-if="videoSource" class="field-hint">所选3D视频同源版本 {{ videoSource.revision.slice(0,12) }}</p>
    <p :class="comparison.status === 'different' ? 'warning' : 'field-hint'" role="status">{{ comparison.message }}</p>
    <p class="field-hint">版本是本地来源记录，不是服务端签名或剧情质量证明；离开 Studio 后无法据此证明布景未再编辑。改景后请重新捕获/导出，旧产物只供审阅。</p>
    <details v-if="studioCandidates.length" open><summary>本场 Studio 取景候选（{{ studioCandidates.length }}）</summary>
      <div v-for="candidate in studioCandidates" :key="candidate.id" class="studio-source-candidate">
        <img :src="api.inputUrl(projectId, candidate.file.split(/[\\/]/).pop())" :alt="`Studio待审场景图候选 #${candidate.id}`" loading="lazy" />
        <span>#{{ candidate.id }} · {{ candidate.is_current ? '当前场景图' : candidate.review_status || '待审' }}{{ candidate.stale ? ' · 已过期，请重新核对' : '' }} · {{ sourceLabel(candidate) }}</span>
        <button v-if="allowAdopt && !candidate.is_current && !candidate.stale && candidate.review_status !== 'rejected'" type="button" class="btn btn-sm btn-secondary" :disabled="loading || adopting" @click="adopt(candidate)">审阅后采纳为本场景图</button>
      </div>
    </details>
    <p v-else-if="!loading" class="field-hint">暂无 Studio 取景候选。普通场景图生成仍可照常使用；已有布景请在 Studio 人工选择存档恢复，不会自动加载或覆盖。</p>
  </section>
</template>

<script setup>
import { computed, onBeforeUnmount, ref, watch } from 'vue'
import { api } from '../api'
import { materialList } from '../utils/materialList.js'
import { candidateStoryboardSource, materialStoryboardSource, provenanceObject, storyboardComparison } from '../utils/storyboardProvenance.js'
const props = defineProps({ projectId: { type: [Number, String], required: true }, sceneId: { type: [Number, String], required: true }, imageFile: { type: String, default: '' }, videoFile: { type: String, default: '' }, allowAdopt: { type: Boolean, default: false } })
const emit = defineEmits(['scene-changed'])
const candidates = ref([]), materials = ref([]), loading = ref(false), adopting = ref(false), error = ref('')
let epoch = 0, mounted = true
onBeforeUnmount(() => { mounted = false; epoch++ })
const belongs = candidate => provenanceObject(candidate.provenance_json)?.source === 'studio_location' && Number(candidate.project_id) === Number(props.projectId) && candidate.entity_type === 'scene' && Number(candidate.entity_id) === Number(props.sceneId)
const studioCandidates = computed(() => candidates.value.filter(candidate => candidate.media_type === 'image' && typeof candidate.file === 'string' && belongs(candidate)))
const currentImage = computed(() => candidates.value.find(candidate => candidate.media_type === 'image' && candidate.is_current && candidate.file?.split(/[\\/]/).pop() === props.imageFile?.split(/[\\/]/).pop()))
const imageSource = computed(() => candidateStoryboardSource(currentImage.value, props.projectId, props.sceneId))
const videoSource = computed(() => materialStoryboardSource(materials.value.find(material => material.name === props.videoFile), props.projectId, props.sceneId))
const comparison = computed(() => storyboardComparison(imageSource.value, videoSource.value))
function sourceLabel(candidate) { const source = candidateStoryboardSource(candidate, props.projectId, props.sceneId); return source ? `同源版本 ${source.revision.slice(0,12)}` : '手工取景或旧来源，未核验同源' }
async function loadSources() {
  const owner = ++epoch, project = Number(props.projectId), scene = Number(props.sceneId)
  loading.value = true; error.value = ''; candidates.value = []; materials.value = []
  try {
    const [images, videos] = await Promise.all([api.sceneCandidates(project, scene, 'image'), api.materials({ type: 'video' })])
    if (!mounted || owner !== epoch) return
    candidates.value = Array.isArray(images.data) ? images.data : []
    materials.value = materialList(videos.data).filter(material => material.project_id == null || Number(material.project_id) === project)
  } catch (cause) { if (mounted && owner === epoch) error.value = cause.response?.data?.error || '来源读取失败；不影响原有生成，请刷新后核对。' }
  finally { if (mounted && owner === epoch) loading.value = false }
}
watch(() => [props.projectId, props.sceneId], () => { void loadSources() }, { immediate: true })
async function adopt(candidate) {
  if (!props.allowAdopt || loading.value || adopting.value || candidate.stale || candidate.review_status === 'rejected' || !studioCandidates.value.includes(candidate)) return
  if (!window.confirm(`将候选 #${candidate.id} 采纳为本场景图？这会按既有规则使下游视频/衔接失效，不会生成任务。请先完整审阅。`)) return
  const owner = epoch, project = Number(props.projectId)
  adopting.value = true; error.value = ''
  try {
    await api.selectCandidate(project, candidate.id)
    if (!mounted || owner !== epoch) return
    emit('scene-changed')
    await loadSources()
  } catch (cause) { if (mounted && owner === epoch) error.value = cause.response?.data?.error || '采纳结果未确认，请刷新核对，不会自动重试。' }
  finally { if (mounted) adopting.value = false }
}
</script>

<style scoped>
.studio-scene-generation { margin: 12px 0; padding: 12px; }
.studio-source-candidate { display: flex; flex-wrap: wrap; gap: 10px; align-items: center; margin-top: 10px; }
.studio-source-candidate img { width: 160px; max-height: 100px; object-fit: contain; }
</style>
