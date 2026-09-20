<template>
  <div class="page fade-up">
    <section class="skills-hero">
      <div class="hero-copy">
        <span class="hero-eyebrow">DRAMA SKILLS</span>
        <h1>漫剧生成 Skill</h1>
        <p>平台集成的漫剧创作全链路 skill：每个环节的方法论、模型、输入输出、关键参数与触发方式。点击任意环节查看详细说明。</p>
      </div>
      <div class="hero-orb" aria-hidden="true"><span>🧩</span></div>
    </section>

    <!-- 标签页切换 -->
    <div class="tabs-bar card">
      <button class="tab-btn" :class="{ active: activeTab === 'pipeline' }" @click="activeTab = 'pipeline'">
        📋 流水线技能
      </button>
      <button class="tab-btn" :class="{ active: activeTab === 'management' }" @click="activeTab = 'management'">
        ⚙️ 技能管理
      </button>
    </div>

    <div v-if="errorMessage" class="card error-message" role="alert">
      <span>{{ errorMessage }}</span>
      <button class="btn btn-sm btn-ghost" @click="errorMessage = ''">✕</button>
    </div>

    <!-- 流水线技能视图 -->
    <template v-if="activeTab === 'pipeline'">
      <div class="pipeline-bar card">
        <template v-for="(s, i) in pipelineSkills" :key="s.key">
          <button type="button" class="pipe-node" @click="openSkill(s)" :title="'查看' + s.name" :aria-label="`查看${s.name}详情`">
            <span class="pipe-n">{{ i + 1 }}</span>
            <span>{{ s.short }}</span>
          </button>
          <span v-if="i < pipelineSkills.length - 1" class="pipe-arrow">→</span>
        </template>
      </div>

      <div class="skill-grid">
        <article v-for="s in pipelineSkills" :key="s.key" class="card skill-card" role="button" tabindex="0" @click="openSkill(s)" @keydown.enter.prevent="openSkill(s)" @keydown.space.prevent="openSkill(s)">
          <div class="skill-head">
            <span class="skill-icon">{{ s.icon }}</span>
            <div>
              <h2>{{ s.name }}</h2>
              <span class="skill-stage">{{ s.stage }}</span>
            </div>
            <span class="skill-open">详情 ›</span>
          </div>
          <p class="skill-what">{{ s.what }}</p>
          <div class="skill-flow">
            <div class="flow-row"><span class="flow-k">输入</span><span class="flow-v">{{ s.input }}</span></div>
            <div class="flow-row"><span class="flow-k">输出</span><span class="flow-v">{{ s.output }}</span></div>
          </div>
          <div class="skill-meta">
            <span class="meta-row"><b>模型/工具</b>{{ s.model }}</span>
            <span class="meta-row"><b>关键参数</b>{{ s.params }}</span>
            <span class="meta-row"><b>怎么触发</b>{{ s.how }}</span>
          </div>
        </article>
      </div>

      <div class="card method-card">
        <h2>📖 创作方法论（short-drama skill）</h2>
        <p>创作方案环节集成的专业短剧方法论，<code>readRef()</code> 按阶段组装进系统提示词：</p>
        <div class="method-grid">
          <div class="method-item"><b>三幕结构</b>入局 / 纠缠 / 决战，规划全剧骨架与集数分配</div>
          <div class="method-item"><b>节奏曲线</b>起势 / 攀升 / 风暴 / 决战的节奏配比</div>
          <div class="method-item"><b>钩子设计</b>悬念钩 / 反转钩 / 情绪钩 / 信息钩 / 危机钩</div>
          <div class="method-item"><b>付费卡点</b>占全集 10-15%，标注卡点集与悬念设计</div>
          <div class="method-item"><b>爽感矩阵</b>打脸 / 逆袭 / 甜宠 / 虐心 / 燃 / 搞笑 / 感动</div>
          <div class="method-item"><b>四层反派</b>小反派 / 中反派 / 大反派 / 隐藏反派</div>
        </div>
        <p class="method-note">来源：<code>0xsline/short-drama</code>（<code>backend/internal/service/shortdrama/references/*.md</code>）。</p>
      </div>

      <div class="card method-card">
        <h2>🔑 一致性与调度机制</h2>
        <div class="method-grid">
          <div class="method-item"><b>角色一致</b>Character Bible 权威 trait 注入画面提示词 + i2v 首帧锁定（首帧含角色）</div>
          <div class="method-item"><b>画幅一致</b>项目级 16:9 / 9:16 / 1:1，画面与视频按画幅生成，避免首帧变形</div>
          <div class="method-item"><b>GPU 独占</b>每个视频任务独占一个 GPU，避免抢占；并发数可配（默认 4）</div>
          <div class="method-item"><b>持久化</b>流水线状态机 + 重启恢复（卡死任务自动 reconcile）</div>
        </div>
      </div>
    </template>

    <!-- 技能管理视图 -->
    <template v-if="activeTab === 'management'">
      <!-- 管理工具栏 -->
      <div class="card mgmt-toolbar">
        <div class="toolbar-left">
          <select v-model="filterStage" class="select-filter">
            <option value="">全部阶段</option>
            <option v-for="s in availableStages" :key="s.value" :value="s.value">{{ s.label }}</option>
          </select>
          <label class="checkbox-label">
            <input type="checkbox" v-model="showEnabledOnly" />
            仅显示启用
          </label>
        </div>
        <button class="btn btn-primary" @click="showCreateForm = true" v-if="!showCreateForm">
          + 新建自定义技能
        </button>
      </div>

      <!-- 新建/编辑表单 -->
      <div v-if="showCreateForm" class="card skill-form">
        <h3>{{ editingSkillId ? '编辑自定义技能' : '新建自定义技能' }}</h3>
        <div class="form-grid">
          <div class="form-row">
            <label>技能名称 *</label>
            <input type="text" v-model="formData.name" placeholder="例如：古风角色设定" />
          </div>
          <div class="form-row">
            <label>技能代码 *</label>
            <input type="text" v-model="formData.code" placeholder="例如：ancient-style-character" />
          </div>
          <div class="form-row">
            <label>适用阶段 *</label>
            <select v-model="formData.stage">
              <option value="">选择阶段</option>
              <option v-for="s in availableStages" :key="s.value" :value="s.value">{{ s.label }}</option>
            </select>
          </div>
          <div class="form-row">
            <label>操作契约 *</label>
            <input type="text" v-model="formData.operation" placeholder="例如：director-shot-packet；必须与调用操作一致" />
          </div>
          <div class="form-row">
            <label>描述</label>
            <textarea v-model="formData.description" placeholder="技能用途说明"></textarea>
          </div>
          <div class="form-row full-width">
            <label>提示词模板</label>
            <textarea v-model="formData.prompt_template" placeholder="支持 {{param}} 占位符" rows="4"></textarea>
          </div>
          <div class="form-row full-width">
            <label>系统提示词</label>
            <textarea v-model="formData.system_prompt" placeholder="追加到主系统提示词的内容" rows="4"></textarea>
          </div>
        </div>
        <div class="form-actions">
          <button class="btn btn-ghost" @click="cancelForm">取消</button>
          <button class="btn btn-primary" @click="saveSkill">{{ editingSkillId ? '保存' : '创建' }}</button>
        </div>
      </div>

      <!-- 技能列表 -->
      <div class="skill-mgmt-list">
        <div v-for="stage in groupedSkills" :key="stage.value" class="stage-group card">
          <h3 class="stage-title">
            <span class="stage-badge">{{ stage.label }}</span>
            <span class="stage-count">{{ stage.skills.length }} 个技能</span>
          </h3>
          <div class="stage-skills">
            <div v-for="s in stage.skills" :key="s.id" class="skill-item" :class="{ disabled: !s.enabled }">
              <div class="skill-info">
                <div class="skill-name-row">
                  <span class="skill-name">{{ s.name }}</span>
                  <span v-if="s.is_system" class="system-badge">系统</span>
                  <span class="version-badge">v{{ s.version }}</span>
                </div>
                <div class="skill-desc">{{ s.description || '无描述' }}</div>
                <div class="skill-code">{{ s.code }}</div>
              </div>
              <div class="skill-actions">
                <label class="toggle-switch" :title="s.enabled ? '已启用' : '已禁用'">
                  <input type="checkbox" :checked="s.enabled" @change="toggleSkill(s)" />
                  <span class="toggle-slider"></span>
                </label>
                <button class="btn btn-sm btn-ghost" @click="viewSkill(s)" title="查看详情">👁</button>
                <button class="btn btn-sm btn-ghost" @click="editSkill(s)" v-if="!s.is_system" title="编辑">✎</button>
                <button class="btn btn-sm btn-ghost danger" @click="deleteSkill(s)" v-if="!s.is_system" title="删除">🗑</button>
              </div>
            </div>
            <div v-if="stage.skills.length === 0" class="empty-stage">该阶段暂无技能</div>
          </div>
        </div>
      </div>

      <!-- 统计信息 -->
      <div class="card stats-bar">
        <div class="stat-item">
          <span class="stat-value">{{ totalSkills }}</span>
          <span class="stat-label">总技能数</span>
        </div>
        <div class="stat-item">
          <span class="stat-value">{{ systemSkillCount }}</span>
          <span class="stat-label">系统技能</span>
        </div>
        <div class="stat-item">
          <span class="stat-value">{{ customSkillCount }}</span>
          <span class="stat-label">自定义技能</span>
        </div>
        <div class="stat-item">
          <span class="stat-value">{{ enabledSkillCount }}</span>
          <span class="stat-label">已启用</span>
        </div>
      </div>
    </template>

    <!-- 详情弹窗挂到 body，避免 page 动画 transform 截断 fixed 定位和层级。 -->
    <Teleport to="body">
    <div v-if="detail" class="skill-modal-mask" @click.self="closeDetail" @keydown.esc="closeDetail">
      <div ref="detailDialog" class="skill-modal card skill-detail" role="dialog" aria-modal="true" :aria-labelledby="`skill-detail-${detail.key || detail.id || 'current'}`" tabindex="-1">
        <div class="detail-head">
          <span class="skill-icon">{{ detail.icon || '📋' }}</span>
          <div>
            <h2 :id="`skill-detail-${detail.key || detail.id || 'current'}`">{{ detail.name || detail.code }}</h2>
            <span class="skill-stage">{{ getStageLabel(detail.stage) }}</span>
          </div>
          <button class="btn btn-sm btn-ghost close" @click="closeDetail" aria-label="关闭详情">✕</button>
        </div>

        <div v-if="!detail.presentation" class="detail-meta">
          <span class="meta-tag">{{ detail.is_system ? '系统技能' : '自定义技能' }}</span>
          <span class="meta-tag">v{{ detail.version }}</span>
          <span class="meta-tag" :class="{ 'enabled': detail.enabled, 'disabled': !detail.enabled }">
            {{ detail.enabled ? '已启用' : '已禁用' }}
          </span>
        </div>

        <div class="detail-section">
          <h4>描述</h4>
          <p>{{ detail.description || '无' }}</p>
        </div>

        <template v-if="detail.presentation"><div class="detail-section"><h4>输入</h4><p>{{ detail.input }}</p></div><div class="detail-section"><h4>输出</h4><p>{{ detail.output }}</p></div><div class="detail-section"><h4>模型 / 工具</h4><p>{{ detail.model }}</p></div><div class="detail-section"><h4>关键参数</h4><p>{{ detail.params }}</p></div><div class="detail-section"><h4>触发方式</h4><p>{{ detail.how }}</p></div><div class="detail-section" v-if="detail.steps?.length"><h4>执行步骤</h4><ol><li v-for="step in detail.steps" :key="step">{{ step }}</li></ol></div><div class="detail-section" v-if="detail.apis?.length"><h4>相关 API</h4><pre class="code-block">{{ detail.apis.map(v => `${v.method} ${v.path}`).join('\n') }}</pre></div></template>

        <div class="detail-section" v-if="detail.prompt_template">
          <h4>提示词模板</h4>
          <pre class="code-block">{{ detail.prompt_template }}</pre>
        </div>

        <div class="detail-section" v-if="detail.system_prompt">
          <h4>系统提示词</h4>
          <pre class="code-block">{{ detail.system_prompt }}</pre>
        </div>

        <div v-if="!detail.presentation" class="detail-section">
          <h4>使用记录</h4>
          <p>全局技能页不提供项目审计记录，请在具体项目中查看。</p>
        </div>

        <div class="modal-actions">
          <button class="btn btn-ghost" @click="closeDetail">关闭</button>
        </div>
      </div>
    </div>
    </Teleport>
  </div>
</template>

<script setup>
import { ref, computed, nextTick, onMounted, onUnmounted } from 'vue'
import { api } from '../api/index.js'

// 状态
const activeTab = ref('pipeline')
const skills = ref([]) // API 获取的技能
const availableStages = ref([])
const filterStage = ref('')
const showEnabledOnly = ref(false)
const showCreateForm = ref(false)
const detail = ref(null)
const detailDialog = ref(null)
let detailOpener = null
const editingSkillId = ref(null)
const errorMessage = ref('')

// 表单数据
const formData = ref({
  name: '',
  code: '',
  stage: '',
  operation: '',
  description: '',
  prompt_template: '',
  system_prompt: ''
})

// 静态流水线技能定义
const pipelineSkills = [
  {
    key: 'plan', name: '创作方案', short: '创作方案', icon: '📐', stage: '第 1 步 · 策划',
    what: '按 short-drama 方法论生成剧名、三幕结构、角色档案、分集目录、节奏/卡点/爽感矩阵。',
    input: '故事创意、题材、画风、受众、基调、结局、目标集数',
    output: '创作方案 JSON（剧名/三幕/角色 trait+style/分集目录/节奏）+ 自动抽取角色卡',
    model: '火山文生文 deepseek-v4（Responses API）',
    params: '系统提示词注入 6 份方法论参考文档；角色数量 3-5；分集 5-60',
    how: '项目详情 → 生成创作方案',
    steps: ['读取 short-drama 方法论参考文档', 'LLM 输出创作方案 JSON', '解析并抽取角色卡', '自动触发角色标准像生成'],
    apis: [
      { method: 'POST', path: '/api/projects/:id/plan' },
      { method: 'PUT', path: '/api/projects/:id/plan/episodes' },
    ],
  },
  {
    key: 'character', name: '角色资产', short: '角色资产', icon: '🧑‍🎨', stage: '第 2 步 · 角色',
    what: '角色卡（trait/style/标准像），跨场景注入权威设定保证人物一致。',
    input: '角色的外貌 trait、服装 style',
    output: '角色标准像 jpg + 角色卡',
    model: '火山文生图 doubao-seedream-5-0',
    params: '画幅匹配、限流 3 并发',
    how: '角色区块 → 一键生成标准像',
    steps: ['从创作方案抽取角色', '按项目画幅与角色 trait/style 拼文生图提示词', '标准像写入 input/<pid>/'],
    apis: [
      { method: 'POST', path: '/api/projects/:id/characters/portraits' },
    ],
  },
  {
    key: 'script', name: '剧本分镜', short: '剧本分镜', icon: '📜', stage: '第 3 步 · 剧本',
    what: '依据创作方案渲染分镜：6-10 场景，每场含画面/视频提示词、对白。',
    input: '创作方案 + 当前集剧情提示词',
    output: '分镜场景（title/content/image_prompt/duration/characters/dialogues）',
    model: '火山文生文 deepseek-v4',
    params: '场景数 6-10、时长 3-8s',
    how: '重新生成剧本 / AI 扩写',
    steps: ['读取当前集剧情提示词', 'LLM 输出分镜 JSON', '校验场景数 6-10'],
    apis: [
      { method: 'POST', path: '/api/projects/:id/script?episode_n=N' },
    ],
  },
  {
    key: 'image', name: '分镜画面', short: '分镜画面', icon: '🖼️', stage: '第 4 步 · 画面',
    what: '按画幅比例图生图：以出场角色标准人像图为底，作为视频首帧。',
    input: '场景 image_prompt + 角色标准人像图 + 画风',
    output: '分镜画面 jpg',
    model: '火山图生图 doubao-seedream-5-0',
    params: '横 2560×1440 / 竖 1440×2560 / 方 1920²',
    how: '场景区 → 生成全部画面',
    steps: ['按项目画幅计算图像尺寸', '注入出场角色标准像参考', '调用 seedream 图生图'],
    apis: [
      { method: 'POST', path: '/api/projects/:id/images' },
    ],
  },
  {
    key: 'video', name: '场景视频', short: '场景视频', icon: '🎬', stage: '第 5 步 · 视频',
    what: 'i2v 图生视频：首帧锁定起点，对白注入 prompt 生成人声音轨。',
    input: '首帧图 + 视频 prompt',
    output: '场景视频 mp4',
    model: '本地 L40 · MiniMax H3 i2v',
    params: '画幅分辨率、steps=20、cfg=1.0、fps=24',
    how: '场景区 → 生成全部视频',
    steps: ['读取场景对白并注入 prompt', 'i2v 生成视频', 'GPU 独占 + 并发限流'],
    apis: [
      { method: 'POST', path: '/api/projects/:id/videos' },
    ],
  },
  {
    key: 'merge', name: '合并成片', short: '合并成片', icon: '✂️', stage: '第 6 步 · 成片',
    what: 'ffmpeg concat 拼接视频 + SRT 字幕烧录，输出成片。',
    input: '就绪的场景视频列表',
    output: '成片 mp4 + SRT',
    model: '远程 ffmpeg libx264/AAC',
    params: 'CRF 18、preset medium',
    how: '合并区 → 合并该集',
    steps: ['校验场景视频就绪', 'ffmpeg concat 视频流', '烧录字幕'],
    apis: [
      { method: 'POST', path: '/api/projects/:id/merge' },
    ],
  },
]

// 计算属性
const filteredSkills = computed(() => {
  let result = skills.value
  if (filterStage.value) {
    result = result.filter(s => s.stage === filterStage.value)
  }
  if (showEnabledOnly.value) {
    result = result.filter(s => s.enabled === true)
  }
  return result
})

const groupedSkills = computed(() => availableStages.value
  .filter(stage => !filterStage.value || stage.value === filterStage.value)
  .map(stage => ({
    ...stage,
    skills: filteredSkills.value.filter(s => s.stage === stage.value)
  })))

const totalSkills = computed(() => skills.value.length)
const systemSkillCount = computed(() => skills.value.filter(s => s.is_system).length)
const customSkillCount = computed(() => skills.value.filter(s => !s.is_system).length)
const enabledSkillCount = computed(() => skills.value.filter(s => s.enabled).length)

// 方法
function backendError(error) {
  return error?.response?.data?.error || error?.response?.data?.message || error?.message || String(error)
}

async function fetchSkills() {
  try {
    const res = await api.listSkills()
    skills.value = res.data
  } catch (e) {
    errorMessage.value = `获取技能列表失败：${backendError(e)}`
  }
}

async function fetchStages() {
  try {
    const res = await api.skillStages()
    availableStages.value = res.data
  } catch (e) {
    errorMessage.value = `获取技能阶段失败：${backendError(e)}`
    // 使用默认阶段
    availableStages.value = [
      { value: 'plan', label: '创作方案' },
      { value: 'character', label: '角色设定' },
      { value: 'storyboard', label: '分镜剧本' },
      { value: 'image_prompt', label: '画面提示词' },
      { value: 'video_prompt', label: '视频提示词' },
      { value: 'review', label: '审核复审' },
    ]
  }
}

async function saveSkill() {
  if (!formData.value.name || !formData.value.code || !formData.value.stage) {
    errorMessage.value = '请填写必填项'
    return
  }
  try {
    if (editingSkillId.value) {
      const { code, ...updates } = formData.value
      await api.updateSkill(editingSkillId.value, updates)
    } else {
      await api.createSkill(formData.value)
    }
    cancelForm()
    await fetchSkills()
  } catch (e) {
    errorMessage.value = `${editingSkillId.value ? '编辑' : '创建'}失败：${backendError(e)}`
  }
}

async function toggleSkill(skill) {
  try {
    const enabled = !skill.enabled
    await api.updateSkill(skill.id, { enabled })
    skill.enabled = enabled
  } catch (e) {
    errorMessage.value = `更新失败：${backendError(e)}`
  }
}

async function editSkill(skill) {
  formData.value = {
    name: skill.name,
    code: skill.code,
    stage: skill.stage,
    operation: skill.operation || skill.code,
    description: skill.description,
    prompt_template: skill.prompt_template,
    system_prompt: skill.system_prompt
  }
  editingSkillId.value = skill.id
  showCreateForm.value = true
}

async function deleteSkill(skill) {
  if (!confirm(`确定删除技能 "${skill.name}" 吗？`)) return
  try {
    await api.deleteSkill(skill.id)
    await fetchSkills()
  } catch (e) {
    errorMessage.value = `删除失败：${backendError(e)}`
  }
}

function showDetail(skill) {
  detailOpener = document.activeElement
  detail.value = skill
  nextTick(() => detailDialog.value?.focus())
}

function closeDetail() {
  detail.value = null
  nextTick(() => detailOpener?.focus?.())
}

function viewSkill(skill) {
  showDetail(skill)
}

function cancelForm() {
  formData.value = {
    name: '',
    code: '',
    stage: '',
    description: '',
    prompt_template: '',
    system_prompt: ''
  }
  editingSkillId.value = null
  showCreateForm.value = false
}

function openSkill(skill) {
  showDetail({
    ...skill,
    description: skill.what,
    presentation: true
  })
}

function getStageLabel(stage) {
  const stageMap = {
    'plan': '创作方案',
    'character': '角色设定',
    'storyboard': '分镜剧本',
    'image_prompt': '画面提示词',
    'video_prompt': '视频提示词',
    'review': '审核复审'
  }
  return stageMap[stage] || stage
}

// 初始化
function handleGlobalEscape(event) { if (event.key === 'Escape' && detail.value) closeDetail() }

onMounted(async () => {
  window.addEventListener('keydown', handleGlobalEscape)
  await fetchStages()
  await fetchSkills()
})
onUnmounted(() => window.removeEventListener('keydown', handleGlobalEscape))
</script>

<style scoped>
.skills-hero { display: flex; align-items: center; justify-content: space-between; gap: 24px; padding: 36px 0 18px; }
.hero-eyebrow { font-size: 12px; font-weight: 700; letter-spacing: 1.5px; color: var(--accent); }
.skills-hero h1 { margin: 8px 0 8px; font-size: 32px; }
.skills-hero p { color: var(--text-secondary); margin: 0; font-size: 14px; max-width: 660px; line-height: 1.6; }
.hero-orb { width: 96px; height: 96px; border-radius: 24px; background: var(--accent-soft); display: flex; align-items: center; justify-content: center; flex: 0 0 auto; }
.hero-orb span { font-size: 36px; }

/* 标签页 */
.tabs-bar { display: flex; gap: 8px; padding: 12px 16px; margin-bottom: 16px; }
.error-message { display: flex; align-items: center; justify-content: space-between; gap: 12px; margin-bottom: 16px; padding: 12px 16px; color: #dc2626; border-color: rgba(239, 68, 68, 0.35); }
.tab-btn { padding: 8px 16px; border: none; background: transparent; color: var(--text-secondary); font-size: 14px; cursor: pointer; border-radius: 8px; transition: all 0.2s; }
.tab-btn:hover { background: var(--accent-soft); color: var(--accent); }
.tab-btn.active { background: var(--accent); color: #fff; font-weight: 600; }

/* 流水线视图 */
.pipeline-bar { display: flex; align-items: center; gap: 8px; padding: 14px 18px; margin: 16px 0 24px; flex-wrap: wrap; }
.pipe-node { display: inline-flex; align-items: center; gap: 7px; font: inherit; font-size: 13px; font-weight: 600; padding: 6px 12px; border: 0; border-radius: 980px; background: var(--accent-soft); color: var(--accent); cursor: pointer; transition: all 0.2s; }
.pipe-node:hover { background: var(--accent); color: #fff; }
.pipe-n { width: 18px; height: 18px; border-radius: 50%; background: var(--accent); color: #fff; display: inline-flex; align-items: center; justify-content: center; font-size: 11px; }
.pipe-node:hover .pipe-n { background: rgba(255, 255, 255, 0.25); }
.pipe-arrow { color: var(--text-tertiary); }
.skill-grid { display: grid; grid-template-columns: repeat(auto-fill, minmax(340px, 1fr)); gap: 16px; }
.skill-card { padding: 20px; cursor: pointer; transition: all 0.2s; }
.skill-card:hover { transform: translateY(-2px); box-shadow: 0 8px 24px rgba(0, 0, 0, 0.08); border-color: var(--accent); }
.skill-head { display: flex; align-items: center; gap: 12px; margin-bottom: 12px; }
.skill-icon { width: 42px; height: 42px; border-radius: 12px; background: var(--accent-soft); display: flex; align-items: center; justify-content: center; font-size: 20px; flex: 0 0 auto; }
.skill-head h2 { margin: 0; font-size: 16px; }
.skill-stage { font-size: 11px; color: var(--text-tertiary); font-weight: 600; }
.skill-open { margin-left: auto; font-size: 12px; color: var(--accent); font-weight: 600; flex: 0 0 auto; }
.skill-what { margin: 0 0 12px; font-size: 13px; color: var(--text-secondary); line-height: 1.6; }
.skill-flow { display: flex; flex-direction: column; gap: 5px; margin-bottom: 12px; padding: 10px 12px; border-radius: 10px; background: rgba(0, 0, 0, 0.03); }
.flow-row { display: flex; gap: 8px; font-size: 12px; line-height: 1.5; }
.flow-k { flex: 0 0 36px; color: var(--accent); font-weight: 700; }
.flow-v { color: var(--text-secondary); }
.skill-meta { display: flex; flex-direction: column; gap: 6px; padding-top: 12px; border-top: 1px solid var(--border); }
.meta-row { font-size: 12px; color: var(--text-secondary); line-height: 1.5; }
.meta-row b { display: inline-block; min-width: 70px; color: var(--text-tertiary); font-weight: 600; }
.method-card { padding: 24px; margin-top: 20px; }
.method-card h2 { margin: 0 0 8px; font-size: 18px; }
.method-card > p { margin: 0 0 16px; font-size: 13px; color: var(--text-secondary); }
.method-grid { display: grid; grid-template-columns: repeat(auto-fill, minmax(280px, 1fr)); gap: 12px; }
.method-item { font-size: 13px; color: var(--text-secondary); padding: 10px 12px; border-radius: 10px; background: rgba(0, 0, 0, 0.035); line-height: 1.5; }
.method-item b { color: var(--accent); margin-right: 4px; }
.method-note { margin: 16px 0 0; font-size: 12px; color: var(--text-tertiary); }
.method-note code { background: rgba(0, 0, 0, 0.05); padding: 1px 6px; border-radius: 4px; font-size: 11px; }

/* 管理视图 */
.mgmt-toolbar { display: flex; align-items: center; justify-content: space-between; padding: 12px 16px; margin-bottom: 16px; }
.toolbar-left { display: flex; gap: 12px; align-items: center; }
.select-filter { padding: 6px 12px; border: 1px solid var(--border); border-radius: 8px; font-size: 13px; }
.checkbox-label { display: flex; align-items: center; gap: 6px; font-size: 13px; cursor: pointer; }

.skill-form { padding: 20px; margin-bottom: 16px; }
.skill-form h3 { margin: 0 0 16px; font-size: 16px; }
.form-grid { display: grid; grid-template-columns: 1fr 1fr; gap: 16px; }
.form-row { display: flex; flex-direction: column; gap: 6px; }
.form-row.full-width { grid-column: span 2; }
.form-row label { font-size: 12px; font-weight: 600; color: var(--text-tertiary); }
.form-row input, .form-row select, .form-row textarea { padding: 8px 12px; border: 1px solid var(--border); border-radius: 8px; font-size: 13px; }
.form-row textarea { resize: vertical; font-family: monospace; }
.form-actions { display: flex; justify-content: flex-end; gap: 8px; margin-top: 16px; }

.skill-mgmt-list { display: flex; flex-direction: column; gap: 16px; }
.stage-group { padding: 16px; }
.stage-title { display: flex; align-items: center; gap: 12px; margin: 0 0 12px; font-size: 14px; }
.stage-badge { background: var(--accent-soft); color: var(--accent); padding: 4px 10px; border-radius: 980px; font-weight: 600; }
.stage-count { color: var(--text-tertiary); font-size: 12px; }
.stage-skills { display: flex; flex-direction: column; gap: 8px; }
.skill-item { display: flex; align-items: center; justify-content: space-between; padding: 12px; border: 1px solid var(--border); border-radius: 10px; transition: all 0.2s; }
.skill-item:hover { border-color: var(--accent); }
.skill-item.disabled { opacity: 0.5; }
.skill-info { flex: 1; }
.skill-name-row { display: flex; align-items: center; gap: 8px; margin-bottom: 4px; }
.skill-name { font-weight: 600; font-size: 14px; }
.system-badge { background: var(--accent); color: #fff; padding: 2px 6px; border-radius: 4px; font-size: 10px; font-weight: 600; }
.version-badge { color: var(--text-tertiary); font-size: 11px; }
.skill-desc { font-size: 12px; color: var(--text-secondary); margin-bottom: 2px; }
.skill-code { font-size: 11px; color: var(--text-tertiary); font-family: monospace; }
.skill-actions { display: flex; align-items: center; gap: 6px; }
.empty-stage { text-align: center; padding: 20px; color: var(--text-tertiary); font-size: 13px; }

/* Toggle Switch */
.toggle-switch { position: relative; display: inline-block; width: 36px; height: 20px; cursor: pointer; }
.toggle-switch input { opacity: 0; width: 0; height: 0; }
.toggle-slider { position: absolute; inset: 0; background: var(--border); border-radius: 20px; transition: 0.2s; }
.toggle-slider::before { content: ''; position: absolute; width: 16px; height: 16px; left: 2px; bottom: 2px; background: white; border-radius: 50%; transition: 0.2s; }
.toggle-switch input:checked + .toggle-slider { background: var(--accent); }
.toggle-switch input:checked + .toggle-slider::before { transform: translateX(16px); }

/* 统计栏 */
.stats-bar { display: flex; justify-content: space-around; padding: 16px; margin-top: 16px; }
.stat-item { text-align: center; }
.stat-value { display: block; font-size: 24px; font-weight: 700; color: var(--accent); }
.stat-label { font-size: 12px; color: var(--text-tertiary); }

/* 详情弹窗 */
.skill-modal-mask { position: fixed; inset: 0; z-index: 1200; display: flex; align-items: center; justify-content: center; padding: 24px; background: rgba(0, 0, 0, 0.58); backdrop-filter: blur(8px); }
.skill-modal { width: min(680px, calc(100vw - 32px)); padding: 24px; outline: none; box-shadow: 0 24px 80px rgba(0, 0, 0, 0.32); }
.skill-detail { max-height: min(85vh, 820px); overflow-y: auto; }
.skill-detail ol { margin: 0; padding-left: 20px; color: var(--text-secondary); font-size: 13px; line-height: 1.8; }
.detail-head { display: flex; align-items: center; gap: 12px; margin-bottom: 8px; }
.detail-head h2 { margin: 0; font-size: 18px; }
.detail-head .close { margin-left: auto; }
.detail-meta { display: flex; gap: 8px; margin-bottom: 16px; }
.meta-tag { padding: 4px 8px; border-radius: 6px; font-size: 11px; background: var(--accent-soft); color: var(--accent); }
.meta-tag.enabled { background: rgba(16, 185, 129, 0.1); color: #059669; }
.meta-tag.disabled { background: rgba(239, 68, 68, 0.1); color: #dc2626; }
.detail-section { margin-bottom: 14px; }
.detail-section h4 { margin: 0 0 6px; font-size: 12px; color: var(--text-tertiary); font-weight: 700; }
.detail-section p { margin: 0; font-size: 13px; color: var(--text-secondary); }
.code-block { background: rgba(0,0,0,0.05); padding: 12px; border-radius: 8px; font-size: 12px; font-family: monospace; white-space: pre-wrap; margin: 0; max-height: 200px; overflow-y: auto; }
.audit-list { display: flex; flex-direction: column; gap: 6px; }
.audit-item { display: flex; gap: 12px; font-size: 12px; padding: 6px 0; border-bottom: 1px solid var(--border); }
.audit-time { color: var(--text-tertiary); }
.audit-stage { color: var(--text-secondary); }
.audit-result { margin-left: auto; }
.audit-result.success { color: #059669; }
.modal-actions { display: flex; justify-content: flex-end; margin-top: 16px; }
@media (max-width: 780px) { .skills-hero { flex-direction: column; align-items: flex-start; } .form-grid { grid-template-columns: 1fr; } .form-row.full-width { grid-column: span 1; } }
</style>
