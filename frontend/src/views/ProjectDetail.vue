<template>
  <div class="page fade-up" v-if="project">
    <!-- 项目头 -->
    <div class="project-head">
      <div>
        <div class="head-links">
          <router-link to="/projects" class="back">← 全部项目</router-link>
        </div>
        <h1>{{ project.title }}</h1>
        <p class="synopsis">{{ project.synopsis }}</p>
        <div class="meta-tags">
          <span v-if="project.genre" class="tag">{{ project.genre }}</span>
          <span v-if="project.style" class="tag tag-orange">{{ project.style }}</span>
          <span v-if="project.aspect_ratio" class="tag tag-gray">{{ aspectLabel(project.aspect_ratio) }}</span>
        </div>
      </div>
      <div class="head-actions">
        <span class="badge" :class="projectBadgeClass">{{ projectStatusText }}</span>
        <button class="btn btn-ghost btn-sm" :disabled="busy" @click="openEditProject">✎ 编辑信息</button>
        <router-link v-if="project.source_type === 'novel'" :to="`/projects/${id()}/novel`" class="btn btn-secondary btn-sm">📚 小说章节</router-link>
        <button v-if="!project.plan" class="btn btn-secondary btn-sm" :disabled="busy || generatingPlan || !project.synopsis" @click="generatePlan">
          {{ generatingPlan ? '方案生成中…' : '📋 生成创作方案' }}
        </button>
        <button class="btn btn-ghost btn-sm" :disabled="busy || !project.synopsis" @click="regenerateScript">
          {{ generatingScript ? '剧本生成中…' : ('🔄 重生第' + activeEpN + '集剧本') }}
        </button>
        <button class="btn btn-sm" :disabled="busy || pipelineActive || !project.synopsis" @click="startPipeline">
          {{ pipelineActive ? `第${project.pipeline_episode || activeEpN}集生成中…` : `⚡ 一键生成(第${activeEpN}集全流程)` }}
        </button>
        <button class="btn btn-ghost btn-sm" :disabled="busy" @click="dubAll">🎤 全集配音</button>
        <router-link :to="`/projects/${id()}/editor`" class="btn btn-secondary btn-sm">🎬 剪辑台</router-link>
        <a class="btn btn-ghost btn-sm" :href="api.srtUrl(id(), activeEpN)" target="_blank">📜 第{{ activeEpN }}集字幕</a>
        <button class="btn btn-danger btn-sm" @click="removeProject">🗑 删除</button>
      </div>
    </div>

    <div v-if="project.status === 'failed'" class="notice project-failure-notice" role="alert">
      <div>
        <strong>项目生成异常</strong>
        <p>{{ project.error || '后端未保存具体错误信息。可重新执行失败步骤；若仍失败，请查看对应场景或任务详情。' }}</p>
      </div>
      <button class="btn btn-ghost btn-sm" :disabled="busy || pipelineActive" @click="startPipeline">重试当前集流程</button>
    </div>

    <!-- 创作方案（short-drama 方法论两阶段：先方案后分镜） -->
    <section class="section" v-if="project.plan || !project.plan === false">
      <div class="section-head">
        <div>
          <span class="overline">CREATIVE PLAN</span>
          <h2>创作方案</h2>
          <p class="sub" v-if="plan">剧名「{{ plan.title }}」· {{ plan.logline }}</p>
        </div>
        <div v-if="project.plan" class="section-actions">
          <button class="btn btn-ghost btn-sm" @click="showPlan = !showPlan">{{ showPlan ? '收起' : '展开' }}</button>
        </div>
      </div>
      <div class="card plan-card" v-if="showPlan || !project.plan">
        <template v-if="plan">
          <div class="plan-grid">
            <div class="plan-block">
              <h4>三幕结构</h4>
              <p v-for="a in plan.acts" :key="a.name" class="plan-line"><b>{{ a.name }}</b>（{{ a.range }}）：{{ a.event }}</p>
            </div>
            <div class="plan-block">
              <h4>主要角色</h4>
              <p v-for="ch in plan.characters" :key="ch.name" class="plan-line"><b>{{ ch.name }}</b>（{{ ch.role }}）：{{ ch.arc }}</p>
            </div>
            <div class="plan-block">
              <h4>四层反派</h4>
              <p v-for="v in plan.villains" :key="v.layer + v.name" class="plan-line"><b>{{ v.layer }}</b> {{ v.name }}：{{ v.motif }}</p>
            </div>
            <div class="plan-block">
              <h4>节奏 / 卡点 / 爽点</h4>
              <p class="plan-line">{{ plan.rhythm }}</p>
              <p class="plan-line">{{ plan.paywall }}</p>
              <p class="plan-line">{{ plan.satisfaction }}</p>
            </div>
            <div class="plan-block plan-episodes">
              <h4>分集目录（{{ epEdits.length }} 集）
                <button class="btn btn-sm btn-secondary" :disabled="epSaving || epDirtyCount === 0" @click="saveEpisodes">
                  {{ epSaving ? '保存中…' : '保存分集修改' }}
                </button>
                <span v-if="epDirtyCount > 0" class="ep-dirty-hint">{{ epDirtyCount }} 集未保存</span>
              </h4>
              <p class="plan-hint">点击集数或「进入」切换该集分镜场景；可修改每集标题与剧情提示词，保存后点击「重新生成剧本」使新提示词生效</p>
              <div v-for="e in epEdits" :key="e.n" class="plan-episode-row"
                :class="{ 'ep-active': activeEpN === e.n, 'ep-dirty-row': isEpDirty(e) }">
                <span class="ep-n ep-link" :title="'查看第' + e.n + '集分镜'" @click="selectEp(e.n)">第{{ e.n }}集</span>
                <input v-model="e.title" class="input input-sm ep-title" :class="{ 'ep-dirty': isEpDirty(e) }" placeholder="集标题" @click.stop />
                <span v-if="e.tag" class="ep-tag">{{ e.tag }}</span>
                <textarea v-model="e.brief" rows="1" class="textarea textarea-sm ep-brief" :class="{ 'ep-dirty': isEpDirty(e) }" placeholder="剧情提示词" @click.stop />
                <span class="ep-duration" :title="'目标时长：' + e.target_duration + '秒，目标镜头：' + e.target_scenes + '个'">
                  {{ fmtSec(e.target_duration) }} / {{ e.target_scenes }}镜
                </span>
                <input type="number" v-model.number="e.target_duration" class="input input-xs ep-dur" min="60" max="600" step="10" @click.stop :class="{ 'ep-dirty': isEpDirty(e) }" title="目标时长（秒）" />
                <input type="number" v-model.number="e.target_scenes" class="input input-xs ep-scene-count" min="10" max="50" step="5" @click.stop :class="{ 'ep-dirty': isEpDirty(e) }" title="目标镜头数" />
                <button class="btn btn-sm btn-ghost ep-enter" @click.stop="selectEp(e.n)">进入 ▶</button>
              </div>
            </div>
          </div>
        </template>
        <div v-else class="plan-empty">
          点击「生成创作方案」，AI 将按专业短剧方法论（题材指南/节奏曲线/钩子设计/付费卡点/爽感矩阵/四层反派）产出剧名、三幕结构、角色档案与分集目录，随后「重新生成剧本」将基于方案渲染分镜场景。
        </div>
      </div>
    </section>

    <!-- 资产库（角色 / 道具 / 场景 + 角色音色）：跨分镜、跨集一致性 -->
    <section class="section" v-if="characters.length || assets.length || project.plan !== undefined">
      <div class="section-head">
        <div>
          <span class="overline">ASSET BIBLE</span>
          <h2>角色 · 道具 · 场景</h2>
          <p class="sub">统一角色外貌、道具与场景环境，并可锁定角色音色；分镜画面生成时自动注入设定与参考图，保证跨集一致</p>
        </div>
        <div class="section-actions">
          <button v-if="assetTab === 'char'" class="btn btn-ghost btn-sm" :disabled="busy || approvedCharsWithoutPortrait === 0" @click="allPortraits">
            一键生成已审核标准像 ({{ approvedCharsWithoutPortrait }})
          </button>
          <button v-else class="btn btn-ghost btn-sm" :disabled="busy || assetsWithoutImage === 0" @click="allAssetImages">
            一键 Krea2 生成{{ assetKindLabel }}参考图 ({{ assetsWithoutImage }})
          </button>
          <button v-if="assetTab === 'char'" class="btn btn-secondary btn-sm" @click="openCreateCharacter">＋ 新建角色</button>
          <button v-else class="btn btn-secondary btn-sm" @click="openCreateAsset">＋ 新建{{ assetKindLabel }}</button>
        </div>
      </div>
      <div class="asset-tabs">
        <button class="asset-tab" :class="{ active: assetTab === 'char' }" @click="assetTab = 'char'">👤 角色 ({{ characters.length }})</button>
        <button class="asset-tab" :class="{ active: assetTab === 'prop' }" @click="assetTab = 'prop'">🎒 道具 ({{ propAssets.length }})</button>
        <button class="asset-tab" :class="{ active: assetTab === 'location' }" @click="assetTab = 'location'">🏞 场景 ({{ locationAssets.length }})</button>
      </div>

      <!-- 角色卡片 -->
      <div v-show="assetTab === 'char'">
        <div v-if="characters.length" class="character-grid">
          <div v-for="ch in characters" :key="ch.id" class="card character-card">
            <div class="char-portrait" @click="viewCharPortrait(ch)">
              <img v-if="ch.portrait" :src="charPortraitUrl(ch)" alt="角色标准像" />
              <div v-else class="char-portrait-ph">{{ (ch.name || '?').slice(0, 1) }}</div>
            </div>
            <div class="char-body">
              <div class="char-name-row">
                <span class="char-name">{{ ch.name }}</span>
                <span v-if="ch.role" class="char-role">{{ ch.role }}</span>
                <span v-if="ch.source === 'auto'" class="tag tag-gray">方案抽取</span>
              </div>
              <p v-if="ch.trait" class="char-trait">🎨 {{ ch.trait }}</p>
              <p v-if="ch.style" class="char-style">👔 {{ ch.style }}</p>
              <span v-if="ch.voice_id" class="char-voice" title="已用参考语音注册复刻音色，配音音色全剧一致">🎤 复刻音色（参考语音）</span>
              <span v-else-if="ch.voice" class="char-voice">🎵 音色：{{ ch.voice }}</span>
              <span class="char-appear">出场 {{ characterCounts[ch.id] || 0 }} 场</span>
              <span v-if="ch.portrait_task_id" class="char-voice">⏳ Krea2 标准像生成中</span>
              <span v-if="ch.portrait_error" class="fail-msg">{{ ch.portrait_error }}</span>
              <span v-if="ch.sheet_task_id" class="char-voice">⏳ 角色四视图生成中</span>
              <span v-if="ch.sheet_error" class="fail-msg">{{ ch.sheet_error }}</span>
              <div class="char-actions">
                <button class="btn btn-sm btn-secondary" :disabled="busy || !!ch.portrait_task_id || ch.profile_status !== 'approved' || !ch.reference_prompt" @click="genPortrait(ch)"
                  :title="ch.profile_status !== 'approved' ? '请先审核通过角色档案' : !ch.reference_prompt ? '请先生成或填写参考像提示词' : ch.portrait_task_id ? '已有任务，不会重复提交；请先检查生成结果' : ''">
                  {{ ch.portrait_task_id ? '已有标准像任务' : ch.portrait ? 'Krea2 重生成标准像' : 'Krea2 生成标准像' }}
                </button>
                <button v-if="ch.portrait_task_id" class="btn btn-sm btn-ghost" :disabled="ch._recovering" @click="recoverPortrait(ch)">{{ch._recovering?'检查中…':'检查生成结果'}}</button>
                <button v-if="ch.portrait_task_id" class="btn btn-sm btn-danger" :disabled="ch._resetting" @click="resetPortrait(ch)">{{ch._resetting?'重置中…':'重置生成状态'}}</button>
                <button class="btn btn-sm btn-ghost" :disabled="busy || ch._uploading" @click="uploadPortrait(ch)">
                  {{ ch._uploading ? '上传中…' : '上传图片替换' }}
                </button>
                <button class="btn btn-sm btn-secondary" :disabled="busy || !ch.portrait || !!ch.sheet_task_id" @click="genCharacterSheet(ch)"
                  :title="!ch.portrait ? '请先生成或上传标准像' : '下游分镜与 H3 视频将优先使用四视图'">
                  {{ ch.sheet_task_id ? '四视图生成中…' : ch.sheet ? '重生成四视图' : '生成四视图' }}
                </button>
                <button v-if="ch.sheet" class="btn btn-sm btn-ghost" @click="viewCharacterSheet(ch)">查看四视图</button>
                <router-link :to="`/projects/${id()}/characters/${ch.id}/looks`" class="btn btn-sm btn-secondary">造型资产</router-link>
                <button class="btn btn-sm btn-ghost" @click="openEditCharacter(ch)">编辑</button>
                <button class="btn btn-sm btn-danger" @click="removeCharacter(ch)">删除</button>
              </div>
              <div class="char-actions">
                <button class="btn btn-sm btn-ghost" :disabled="busy || ch._voiceUploading" @click="uploadVoice(ch)"
                  :title="'上传 10~20 秒清晰人声，注册为该角色的复刻音色（全剧配音一致）'">
                  {{ ch._voiceUploading ? '注册中…' : (ch.voice_ref ? '↻ 重传参考语音' : '🎤 上传参考语音') }}
                </button>
                <button v-if="ch.voice_ref && !ch.voice_id" class="btn btn-sm btn-ghost" :disabled="busy || ch._voiceUploading" @click="retryCloneVoice(ch)">
                  重试注册音色
                </button>
                <button v-if="ch.voice || ch.voice_id" class="btn btn-sm btn-ghost" :disabled="busy" @click="clearVoice(ch)">清除语音</button>
              </div>
            </div>
          </div>
        </div>
        <div v-else class="card empty-inline">
          <template v-if="pipelineActive || generatingPlan">AI 正在分析故事并生成角色草稿，请稍候。生成后你可以审核、修改、删除或补充角色。</template>
          <template v-else>暂无角色。请先生成创作方案，AI 会从故事内容自动生成角色草稿；「新建角色」仅用于审核后的人工补充。</template>
        </div>
      </div>

      <!-- 道具 / 场景卡片 -->
      <div v-show="assetTab !== 'char'">
        <div v-if="currentAssets.length" class="character-grid">
          <div v-for="a in currentAssets" :key="a.id" class="card character-card">
            <div class="char-portrait" @click="viewAssetImage(a)">
              <img v-if="a.image" :src="assetImageUrl(a)" :alt="assetKindLabel + '参考图'" />
              <div v-else class="char-portrait-ph">{{ assetTab === 'prop' ? '🎒' : '🏞' }}</div>
            </div>
            <div class="char-body">
              <div class="char-name-row">
                <span class="char-name">{{ a.name }}</span>
                <span v-if="a.source === 'auto'" class="tag tag-gray">方案抽取</span>
              </div>
              <p v-if="a.description" class="char-trait">📝 {{ a.description }}</p>
              <span class="char-appear">出场 {{ assetCounts[a.id] || 0 }} 场</span>
              <span v-if="a.image_task_id" class="char-voice">⏳ Krea2 参考图生成中</span>
              <span v-if="a.image_error" class="fail-msg">{{ a.image_error }}</span>
              <span v-if="a.kind === 'prop' && a.sheet_task_id" class="char-voice">⏳ 道具四视图生成中</span>
              <span v-if="a.kind === 'prop' && a.sheet_error" class="fail-msg">{{ a.sheet_error }}</span>
              <div class="char-actions">
                <button class="btn btn-sm btn-secondary" :disabled="busy || !!a.image_task_id" @click="genAssetImage(a)">
                  {{ a.image_task_id ? 'Krea2 生成中…' : a.image ? 'Krea2 重生成参考图' : 'Krea2 生成参考图' }}
                </button>
                <button class="btn btn-sm btn-ghost" :disabled="busy || a._uploading" @click="uploadAssetImage(a)">
                  {{ a._uploading ? '上传中…' : '上传图片替换' }}
                </button>
                <button v-if="a.kind === 'prop'" class="btn btn-sm btn-secondary" :disabled="busy || !a.image || !!a.sheet_task_id" @click="genPropSheet(a)"
                  :title="!a.image ? '请先生成或上传道具参考图' : '下游分镜与 H3 视频将优先使用四视图'">
                  {{ a.sheet_task_id ? '四视图生成中…' : a.sheet ? '重生成四视图' : '生成四视图' }}
                </button>
                <button v-if="a.kind === 'prop' && a.sheet" class="btn btn-sm btn-ghost" @click="viewPropSheet(a)">查看四视图</button>
                <button class="btn btn-sm btn-ghost" @click="openEditAsset(a)">编辑</button>
                <button class="btn btn-sm btn-danger" @click="removeAsset(a)">删除</button>
              </div>
            </div>
          </div>
        </div>
        <div v-else class="card empty-inline">
          暂无{{ assetKindLabel }}。生成创作方案后会自动抽取关键{{ assetKindLabel }}（分镜引用的{{ assetKindLabel }}也会自动建卡），分镜画面生成时会自动以{{ assetKindLabel }}参考图锁定外观，保证全剧一致；也可点击「新建{{ assetKindLabel }}」手动添加。
        </div>
      </div>
    </section>

    <!-- 流程步骤条 -->
    <div class="steps-bar card">
      <div class="step" :class="{ done: !!project.plan, active: !project.plan }">
        <span class="step-n">1</span><span>创作方案</span>
      </div>
      <span class="step-arrow">→</span>
      <div class="step" :class="{ done: allRefsReady, active: !!project.plan && !allRefsReady }" :title="allRefsReady ? '角色/道具/场景参考图就绪，画面将锁定人物与道具环境一致' : '建议先生成参考图（角色标准像、道具图、场景图），画面生成时才能锁定人物、道具与环境一致'">
        <span class="step-n">2</span><span>参考图（角色/道具/场景）</span>
      </div>
      <span class="step-arrow">→</span>
      <div class="step" :class="{ done: scenes.length > 0 && imageCount === scenes.length }">
        <span class="step-n">3</span><span>分镜画面</span>
      </div>
      <span class="step-arrow">→</span>
      <div class="step" :class="{ done: scenes.length > 0 && videoReadyCount === scenes.length }">
        <span class="step-n">4</span><span>场景视频</span>
      </div>
      <span class="step-arrow">→</span>
      <div class="step" :class="{ done: merges.some(m => m.status === 'success') }">
        <span class="step-n">5</span><span>合并成片</span>
      </div>
      <span class="step-status" v-if="project.status === 'ready' || project.status === 'finished'">
        <span class="dot green"></span>{{ project.status === 'finished' ? '成片已完成' : '全部视频就绪，可合并成片' }}
      </span>
    </div>

    <!-- 剧本（当前集，可编辑后 AI 重新生成分镜） -->
    <section id="episode-workspace" class="section" v-if="project.script || project.scripts">
      <div class="section-head">
        <div>
          <span class="overline">SCENARIO</span>
          <h2>第{{ activeEpN }}集 剧本 <span v-if="currentEpTitle && !currentEpTitle.includes('第')">· {{ currentEpTitle }}</span></h2>
          <p class="sub">修改本集剧本正文后点击「保存并 AI 重新生成分镜」，将按新剧本重建该集的分镜场景</p>
        </div>
        <div class="section-actions">
          <button class="btn btn-secondary btn-sm" :disabled="busy || expandingScript || !scriptDraft.trim()" @click="aiExpand">
            {{ expandingScript ? 'AI 扩写中…' : '✨ AI 扩写' }}
          </button>
          <button class="btn btn-secondary btn-sm" :disabled="busy || scriptRendering || !scriptDraft.trim()" @click="saveAndRender">
            {{ scriptRendering ? 'AI 生成中…' : '💾 保存并 AI 重新生成分镜' }}
          </button>
          <button class="btn btn-ghost btn-sm" @click="showScript = !showScript">
            {{ showScript ? '收起' : '展开' }}
          </button>
        </div>
      </div>
      <div class="card script-card" v-if="showScript">
        <textarea v-model="scriptDraft" class="textarea script-edit" rows="10"
          :placeholder="'编辑第' + activeEpN + '集剧本正文…'" @input="scriptDirty = true" />
        <div class="field-hint">推荐格式：`【动作】画面描述`、`【对白｜角色名】原文`、`【旁白】原文`、`【内心独白｜角色名】原文`。旧格式和常见自然写法会尽量兼容，但不会把普通心理或氛围描写擅自转换成发声。</div>
        <div class="script-edit-bar">
          <span v-if="scriptDirty" class="script-dirty-hint">已修改，未保存</span>
          <span v-else-if="!scriptDraft.trim()" class="script-dirty-hint">该集剧本尚未生成，可直接编写或点击「重新生成剧本」</span>
          <button class="btn btn-sm btn-secondary" :disabled="busy || scriptRendering || !scriptDirty" @click="saveAndRender">
            {{ scriptRendering ? 'AI 生成中…' : '💾 保存并 AI 重新生成分镜' }}
          </button>
        </div>
      </div>
    </section>

    <!-- 场景工作区（按当前集显示） -->
    <section class="section" v-if="currentScenes.length">
      <div class="section-head">
        <div>
          <span class="overline">STORYBOARD</span>
          <h2>第{{ activeEpN }}集 · {{ currentEpTitle }} <span class="count">{{ curReadyText }}</span></h2>
        </div>
        <div class="section-actions">
          <button class="btn btn-secondary" :disabled="busy || curPendingScenes === 0" @click="allImages">
            <span class="live-dot"></span> 生成该集全部画面 ({{ curPendingScenes }})
          </button>
          <button class="btn" :disabled="busy || curImageReadyScenes === 0" @click="allVideos">
            🎬 生成该集全部视频 ({{ curImageReadyScenes }})
          </button>
          <button class="btn btn-primary" :disabled="busy || curVideoReadyScenes === 0" @click="autoMergeEpisode">
            ⚡ 自动合并该集成片
          </button>
        </div>
      </div>

      <div class="scene-grid">
        <div v-for="sc in pagedScenes" :key="sc.id" class="card scene-card" :class="{ 'scene-active': isWorking(sc) }">
          <div class="scene-head">
            <span class="scene-order" :class="orderColor(sc.order)">{{ sc.order }}</span>
            <div class="scene-title">
              <div>{{ sc.title || '场景 ' + sc.order }}</div>
              <span class="badge" :class="sceneBadgeClass(sc)">{{ sceneStatusText(sc) }}</span>
            </div>
            <button class="icon-btn" title="编辑场景" @click="openEditScene(sc)">✎</button>
          </div>

          <div class="scene-preview">
            <img v-if="sc.image_file" :src="imageUrl(sc)" class="scene-img" alt="分镜画面"
              @click="viewImage(sc)" />
            <div v-else class="scene-placeholder">
              <span class="ph-icon">🎨</span>
              <span v-if="sc.status === 'image_pending'">画面生成中…（约 30 秒）</span>
              <span v-else-if="sc.status === 'failed' && sc.error && sc.error.includes('画面')">画面生成失败</span>
              <span v-else>{{ sc.content ? '等待生成画面' : '等待生成剧本' }}</span>
              <span v-if="sc.status === 'image_pending'" class="ph-bar">
                <span class="ph-bar-fill"></span>
              </span>
            </div>
          </div>

          <p class="scene-content">{{ sc.content }}</p>
          <p v-if="sc.image_prompt" class="scene-prompt">🎨 {{ sc.image_prompt }}</p>
          <div v-if="sceneDialogues(sc).length" class="scene-dialogues">
            <div v-for="d in sceneDialogues(sc)" :key="d.id" class="dialogue-row">
              <span class="dl-char">{{ d.character || '旁白' }}</span>
              <span class="dl-text">{{ d.text }}</span>
              <audio v-if="d.audio_file && d.status === 'ready'" :src="dubAudioUrl(d)" controls preload="none" class="dl-audio" />
              <span v-else-if="d.status === 'synthesizing'" class="dl-state">合成中…</span>
              <span v-else-if="d.status === 'failed'" class="dl-state dl-fail" :title="d.error">失败</span>
            </div>
          </div>

          <div class="scene-actions">
            <!-- 画面：无图生成，有图重新生成（始终可单独触发） -->
            <template v-if="!sc.image_file">
              <span v-if="sc.status === 'image_pending'" class="working">
                <span class="dot blue pulse"></span>画面生成中…
              </span>
              <template v-else>
                <button class="btn btn-sm btn-secondary" :disabled="busy || sc._working" @click="genImage(sc)">
                  {{ sc._working ? '生成中…' : (sc.status === 'failed' && sc.image_retries > 0 ? '重试画面' : '生成画面') }}
                </button>
                <label class="btn btn-sm btn-ghost" :class="{ disabled: busy || sc._working }">
                  上传分镜图<input type="file" accept="image/png,image/jpeg,image/webp" :disabled="busy || sc._working" hidden @change="e => uploadSceneImage(sc, e)" />
                </label>
              </template>
            </template>
            <template v-else>
              <button class="btn btn-sm btn-secondary" :disabled="busy || sc._working || isVideoWorking(sc)" @click="genImage(sc)">
                {{ sc._working ? '生成中…' : '重新生成画面' }}
              </button>
              <label class="btn btn-sm btn-ghost" :class="{ disabled: busy || sc._working || isVideoWorking(sc) }">
                替换分镜图<input type="file" accept="image/png,image/jpeg,image/webp" :disabled="busy || sc._working || isVideoWorking(sc)" hidden @change="e => uploadSceneImage(sc, e)" />
              </label>
              <!-- 视频：画面就绪即可单独触发；就绪后仍可重新生成 -->
              <span v-if="isVideoWorking(sc)" class="working">
                <span class="dot blue pulse"></span>{{ videoProgress(sc) }}
                <button class="btn btn-sm btn-danger stop-video" :disabled="sc._stopping" @click="stopVideo(sc)">
                  {{ sc._stopping ? '停止中…' : '⏹ 停止' }}
                </button>
              </span>
              <button v-else class="btn btn-sm" :class="{ 'btn-ghost': sc.status === 'video_ready' }"
                :disabled="busy || sc._working" @click="genVideo(sc)">
                {{ sc.status === 'video_ready' ? '重新生成视频' : (sc.video_retries > 0 ? '重试视频' : '生成视频') }}
              </button>
              <button class="btn btn-sm btn-ghost" @click="viewImage(sc)">查看画面</button>
              <button class="btn btn-sm btn-ghost" @click="viewVideoPrompt(sc)">编辑视频提示词</button>
              <button class="btn btn-sm btn-secondary" @click="openContinuity(sc)">连续性设置</button>
            </template>
            <span v-if="sc.status === 'failed' && sc.error" class="fail-msg">{{ sc.error }}</span>
          </div>

          <div v-if="sc.video_input_file || (sc.video_file && sc.video_gpu !== null && sc.video_gpu !== undefined)" class="video-box">
            <video :src="videoUrl(sc)" controls preload="metadata" class="scene-video"></video>
            <a class="btn btn-sm btn-ghost download" :href="videoUrl(sc) + '?download=1'">下载</a>
          </div>
        </div>
      </div>

      <!-- 场景分页 -->
      <div v-if="currentScenes.length > scenePageSize" class="pager">
        <button class="btn btn-sm btn-ghost" :disabled="scenePage <= 1" @click="scenePage--">← 上一页</button>
        <span class="pager-info">第 {{ scenePage }} / {{ scenePageCount }} 页 · 共 {{ currentScenes.length }} 个场景</span>
        <button class="btn btn-sm btn-ghost" :disabled="scenePage >= scenePageCount" @click="scenePage++">下一页 →</button>
      </div>
    </section>

    <!-- 合并成片（当前集） -->
    <section class="section" v-if="curVideoReadyCount > 0">
      <div class="section-head">
        <div>
          <span class="overline">MERGE & EDIT</span>
          <h2>第{{ activeEpN }}集 · 合并成片</h2>
          <p class="sub">按该集场景序号顺序拼接，输出完整成片，可下载</p>
        </div>
        <div class="section-actions">
          <button class="btn btn-ghost btn-sm" :disabled="busy" @click="mergeAll">🎬 整剧一键合并</button>
          <label class="merge-opt"><input type="checkbox" v-model="mergeSub" />烧录字幕</label>
          <label class="merge-opt"><input type="checkbox" v-model="mergeDub" />保留原声</label>
          <button class="btn btn-lg" :disabled="busy || curVideoReadyCount < 2 || curMerging" @click="autoMergeEpisode">
            ⚡ 合并第{{ activeEpN }}集 {{ curVideoReadyCount }} 个场景
          </button>
        </div>
      </div>

      <div class="card merge-card">
        <div class="merge-select">
          <span class="merge-hint">默认不烧录字幕；可按需勾选“烧录字幕”。各场景 H3 原音轨可独立选择是否保留。</span>
          <button v-for="sc in curReadyScenes" :key="sc.id" class="merge-chip active" disabled>
            <span class="chip-n">{{ sc.order }}</span>
            <span class="chip-title">{{ sc.title || '场景' }}</span>
            <span class="chip-check">✓</span>
          </button>
          <span class="merge-hint" v-if="curReadyScenes.length >= 2">合并顺序：场景 {{ curReadyScenes.map(s => s.order).join(' → ') }}</span>
        </div>

        <div v-if="merges.length" class="merge-list">
          <div v-for="m in visibleMerges" :key="m.id" class="merge-item">
            <div class="merge-info">
              <span class="badge" :class="mergeBadgeClass(m.status)">{{ mergeStatusText(m) }}</span>
              <span class="merge-time">第{{ m.episode_n || 1 }}集 · {{ m.created_at?.slice(0, 16).replace('T', ' ') }}</span>
              <span v-if="m.error" class="fail-msg">{{ m.error }}</span>
            </div>
            <div v-if="m.status === 'success' && m.output_file" class="merge-result">
              <video :src="outputUrl(0, m.output_file)" controls preload="metadata" class="merge-video"></video>
              <div class="merge-links">
                <a class="btn btn-sm btn-ghost" :href="outputUrl(0, m.output_file) + '?download=1'">下载成片</a>
                <a class="btn btn-sm btn-ghost" :href="outputUrl(0, m.output_file.replace(/\.mp4$/, '.srt')) + '?download=1'" v-if="m.subtitle">下载字幕</a>
              </div>
            </div>
            <div v-else-if="m.status === 'running' || m.status === 'pending'" class="merge-working">
              <span class="dot blue pulse"></span>正在拼接视频，请稍候…
            </div>
          </div>
          <button v-if="merges.length > mergeShowLimit" class="btn btn-sm btn-ghost merge-more" @click="showAllMerges = !showAllMerges">
            {{ showAllMerges ? '收起合并历史' : `展开全部合并历史（${merges.length} 条）` }}
          </button>
        </div>
      </div>
    </section>

    <!-- 场景编辑弹窗 -->
    <div v-if="editingScene" class="modal-mask" @click.self="editingScene = null">
      <div class="modal card scene-edit-modal">
        <h2>编辑场景 {{ editingScene.order }}</h2>
        <div class="field">
          <label>场景标题</label>
          <input v-model="sceneForm.title" class="input" />
        </div>
        <div class="field">
          <label>分镜时长（秒）</label>
          <input v-model.number="sceneForm.duration" class="input" type="number" min="3" max="15" step="0.1" />
          <div class="field-hint">允许 3–15 秒；该数值控制实际生成帧数，修改后需要重新生成视频。</div>
        </div>
        <div class="field">
          <label>场景正文（视频提示词）</label>
          <textarea v-model="sceneForm.content" class="textarea" rows="3"
            placeholder="描述画面动作、镜头运动、对白…" />
        </div>
        <div class="field"><label>画面实际出场人物</label><input v-model="sceneForm.visible_characters" class="input" placeholder="逗号分隔；只有这些人物需要四视图" /><div class="field-hint">仅填写本镜最终画面中真实可见的人物。回忆、照片或倒影中确实被画出时也算可见。</div></div>
        <div class="field"><label>仅发声人物</label><input v-model="sceneForm.voice_characters" class="input" placeholder="画外对白或内心独白发声者，逗号分隔" /></div>
        <div class="field"><label>仅被提及人物</label><input v-model="sceneForm.mentioned_characters" class="input" placeholder="只在剧情、对白或独白中被提到，逗号分隔" /><div class="field-hint">仅发声和仅被提及人物不会要求四视图，也不会作为画面主体。</div></div>
        <div class="field">
          <label>视觉类型</label>
          <select v-model="sceneForm.visual_type" class="input">
            <option value="normal">普通场景</option>
            <option value="megastructure">巨构场景（Krea2 专项增强）</option>
          </select>
          <div v-if="sceneForm.visual_type === 'megastructure'" class="field">
            <label>巨构类别</label>
            <select v-model="sceneForm.mega_type" class="input">
              <option value="architecture">建筑巨构</option><option value="creature">巨兽/生物</option><option value="geological">自然/地质</option><option value="mechanical">机械/载具</option><option value="surreal">超现实混合</option>
            </select>
          </div>
          <div class="field-hint">巨构模式强化尺度参照、大气分层、结构可读性、重量感和镜头构图，不改变剧情主体。</div>
        </div>
        <div class="field">
          <div class="field-label-actions"><label>H3 起始帧画面提示词</label><button class="btn btn-sm btn-secondary" :disabled="redesigningScenePrompt || !sceneForm.content.trim()" @click="redesignScenePrompt">{{ redesigningScenePrompt ? '生成中…' : '生成 H3 起始帧提示词' }}</button></div>
          <textarea v-model="sceneForm.image_prompt" class="textarea" rows="6"
            placeholder="根据当前剧情、镜头设计和已选参考图，生成简洁的 H3 起始帧画面、动作、摄影机与光线描述。" />
          <div class="field-hint">人物身份与造型由参考图控制；提示词只描述当前镜头中实际出现的主体、动作、构图和光线。修改后需保存并重新生成画面。</div>
        </div>
        <div class="field"><label>视频动作正文（可手工修改）</label><textarea v-model="sceneForm.video_prompt" class="textarea" rows="6" placeholder="填写可见动作、结束状态和运镜；留空则自动生成。首帧、连续性和 H3 六段契约由系统固定保护。" /></div>
        <div class="field">
          <label>本场角色造型套装</label>
          <div v-if="!sceneCharactersForOutfits().length" class="field-hint">本场未识别到项目角色。</div>
          <div v-for="ch in sceneCharactersForOutfits()" :key="ch.id" class="outfit-select-row"><strong>{{ch.name}}</strong><select v-model.number="selectedSceneOutfits[ch.id]" class="input"><option :value="0">未选择（使用默认）</option><option v-for="o in sceneOutfitOptions[ch.id]||[]" :key="o.id" :value="o.id" :disabled="o.audit_status!=='approved'&&o.audit_status!=='published'">{{o.name}} · {{o.audit_status}}</option></select><router-link :to="`/projects/${id()}/characters/${ch.id}/looks`" class="btn btn-sm btn-ghost">管理造型</router-link></div>
          <div class="field-hint">每个角色在一个场景中选择一套完整造型；镜头未单独覆盖时继承本场选择。</div>
        </div>
        <div class="field">
          <label>参考图片</label>
          <div v-if="!sceneReferenceCandidates.length" class="field-hint">暂无可用图片，请先生成或上传人物四视图、造型、场景或道具参考图。</div>
          <div class="reference-picker"><div v-for="ref in sceneReferenceCandidates" :key="ref.key" class="reference-option" :class="{ selected: referenceIndex(ref) >= 0 }"><img :src="api.inputUrl(id(), ref.image)" @click="toggleSceneReference(ref)"><div><strong>{{ referenceIndex(ref) >= 0 ? `${selectedSceneReferences[referenceIndex(ref)].use_krea2 ? '用于生成分镜图' : '不用于分镜图'} / ${selectedSceneReferences[referenceIndex(ref)].use_h3 ? '用于后续视频参考' : '不用于后续视频'}` : '未选择' }}</strong><span>{{ ref.label }}</span><div v-if="referenceIndex(ref) >= 0" class="reference-flags"><label><input v-model="selectedSceneReferences[referenceIndex(ref)].use_krea2" type="checkbox"> 生成分镜图时使用</label><label><input v-model="selectedSceneReferences[referenceIndex(ref)].use_h3" type="checkbox"> 后续视频时使用</label><button type="button" @click="moveReference(referenceIndex(ref), -1)">↑</button><button type="button" @click="moveReference(referenceIndex(ref), 1)">↓</button></div></div></div></div>
          <div class="field-hint"><strong>生成分镜图：</strong>SelfLift 使用场景、当前出场人物四视图、造型和道具参考图，最多 9 张；出场人物四视图会自动补入。<br><strong>生成后续视频：</strong>已确认的分镜图作为起始帧；这里只选择需要额外提供给视频模型的人物、造型或道具参考图，最多 8 张。</div>
        </div>
        <div v-if="sceneError" class="notice error-notice">{{ sceneError }}</div>
        <div class="modal-actions">
          <button class="btn btn-ghost" @click="editingScene = null">取消</button>
          <button class="btn" :disabled="savingScene" @click="saveScene">
            {{ savingScene ? '保存中…' : '保存' }}
          </button>
        </div>
      </div>
    </div>

    <div v-if="videoTaskDetail" class="modal-mask" @click.self="videoTaskDetail = null">
      <div class="modal card video-task-modal"><h2>编辑视频提示词</h2><div v-if="videoTaskDetail.continuity_frame" class="continuity-prompt-source"><img :src="videoTaskDetail.continuity_frame.image_url" /><div><b>已使用上一镜末尾帧</b><p>连续性模式：{{ continuityModeLabel(videoTaskDetail.continuity_mode) }}</p><p>{{ formatFrameTime(videoTaskDetail.continuity_frame.timestamp_ms) }}</p></div></div><div v-else-if="videoTaskDetail.continuity_mode && videoTaskDetail.continuity_mode !== 'independent'" class="warning">连续性已配置，但上一镜末尾帧尚未就绪。</div><div class="field-row"><div class="field"><label>视频模板</label><select v-model="videoTaskTemplate" class="input"><option value="minimax_h3_ref2v">多图参考生视频（推荐，有场景参考图时优先）</option><option value="minimax_h3_i2v">图生视频（首帧硬锚定）</option><option value="minimax_h3_first_last">首尾帧生视频（需指定首帧和尾帧图片）</option></select></div><div class="field"><label>尺寸 / 时长 / 帧率</label><p class="info-text">{{ videoTaskDetail.width }}×{{ videoTaskDetail.height }} / {{ videoTaskDetail.duration }}秒 / {{ videoTaskDetail.fps }} FPS</p></div></div><div v-if="videoTaskTemplate === 'minimax_h3_first_last'" class="field-row"><div class="field"><label>首帧图片</label><select v-model="videoTaskFirstFrame" class="input"><option value="">请选择…</option><option v-if="videoTaskScene.image_file" :value="videoTaskScene.image_file">分镜画面（当前场景）</option></select></div><div class="field"><label>尾帧图片</label><select v-model="videoTaskLastFrame" class="input"><option value="">请选择…</option><option v-if="videoTaskScene.image_file" :value="videoTaskScene.image_file">分镜画面（当前场景）</option></select></div></div><div class="field-label-actions"><label>最终提交给 H3 的完整提示词（可直接修改）</label><button class="btn btn-sm btn-secondary" :disabled="regeneratingVideoPrompt" @click="regenerateVideoPrompt">{{ regeneratingVideoPrompt ? 'AI 生成中…' : 'AI 重新生成完整提示词' }}</button></div><textarea v-model="videoTaskPromptDraft" class="textarea" rows="20" /><div v-if="!videoTaskDetail.has_saved_prompt" class="warning">当前没有已保存的视频提示词。编辑窗口不会自动生成；请手工填写，或明确点击“AI 重新生成完整提示词”。</div><div v-if="videoTaskDetail.prompt_issues?.length" class="warning">参考图已变化：{{ videoTaskDetail.prompt_issues.join('；') }}。系统不会自动覆盖，请手工调整或明确点击 AI 重新生成。</div><div class="field-hint">这里原样显示已保存的最终六段 Ref2VA 提示词；打开编辑窗口不会自动生成或重建。<b v-if="videoTaskTemplate === 'minimax_h3_first_last'">　⚠ 首尾帧模板已选择，请确认首帧和尾帧图片已正确指定。</b></div><details open><summary>当前将提交的完整提示词</summary><pre class="task-prompt">{{ previewVideoFullPrompt }}</pre></details><details v-if="videoTaskDetail.history_prompt"><summary>上次实际提交的提示词</summary><pre class="task-prompt">{{ videoTaskDetail.history_prompt }}</pre></details><details v-if="videoTaskDetail.params_json"><summary>上次实际参数与输入图片</summary><pre class="task-prompt">{{ formatTaskParams(videoTaskDetail.params_json) }}</pre></details><div class="modal-actions"><button class="btn btn-ghost" @click="videoTaskDetail = null">取消</button><button class="btn btn-secondary" :disabled="savingVideoPrompt" @click="saveVideoPrompt(false)">保存提示词</button><button class="btn" :disabled="savingVideoPrompt" @click="saveVideoPrompt(true)">{{ savingVideoPrompt ? '处理中…' : '保存最新提示词并生成视频' }}</button></div></div>
    </div>

    <!-- 项目信息编辑弹窗 -->
    <div v-if="editingProject" class="modal-mask" @click.self="editingProject = false">
      <div class="modal card">
        <h2>编辑项目</h2>
        <div class="field">
          <label>项目名称</label>
          <input v-model="projectForm.title" class="input" />
        </div>
        <div class="field-row">
          <div class="field">
            <label>题材</label>
            <input v-model="projectForm.genre" class="input" placeholder="如：科幻+悬疑" />
          </div>
          <div class="field">
            <label>画风</label>
            <input v-model="projectForm.style" class="input" placeholder="如：国漫 / 赛博朋克 / 水墨" />
          </div>
        </div>
        <div class="field-row">
          <div class="field">
            <label>目标受众</label>
            <select v-model="projectForm.audience" class="input">
              <option value="">不限</option>
              <option>女频</option>
              <option>男频</option>
              <option>全龄</option>
            </select>
          </div>
          <div class="field">
            <label>故事基调</label>
            <select v-model="projectForm.tone" class="input">
              <option value="">不限</option>
              <option>爽</option>
              <option>甜</option>
              <option>虐</option>
              <option>燃</option>
              <option>搞笑</option>
              <option>悬疑</option>
            </select>
          </div>
        </div>
        <div class="field-row">
          <div class="field">
            <label>结局类型</label>
            <select v-model="projectForm.ending" class="input">
              <option value="">不限</option>
              <option value="HE">HE（大团圆）</option>
              <option value="BE">BE（悲剧）</option>
              <option value="OE">OE（开放式）</option>
            </select>
          </div>
          <div class="field">
            <label>目标集数</label>
            <select v-model="projectForm.episodes" class="input">
              <option :value="0">不指定</option>
              <option :value="5">5 集</option>
              <option :value="10">10 集</option>
              <option :value="20">20 集</option>
              <option :value="40">40 集</option>
              <option :value="60">60 集</option>
            </select>
          </div>
        </div>
        <div class="field">
          <label>画幅 <span class="optional">影响视频与画面比例</span></label>
          <select v-model="projectForm.aspect_ratio" class="input">
            <option value="16:9">横屏 16:9（YouTube / 横屏）</option>
            <option value="9:16">竖屏 9:16（短剧 / 抖音 / 快手）</option>
            <option value="1:1">方形 1:1</option>
          </select>
          <div class="field-hint">切换画幅后需重新生成画面与视频</div>
        </div>
        <div class="field">
          <label>故事创意</label>
          <textarea v-model="projectForm.synopsis" class="textarea" rows="3" />
          <div class="field-hint">修改创意后请先「生成创作方案」，再「重新生成剧本」更新分镜</div>
        </div>
        <div v-if="projectError" class="notice error-notice">{{ projectError }}</div>
        <div class="modal-actions">
          <button class="btn btn-ghost" @click="editingProject = false">取消</button>
          <button class="btn" :disabled="savingProject" @click="saveProject">
            {{ savingProject ? '保存中…' : '保存' }}
          </button>
        </div>
      </div>
    </div>

    <!-- 角色新建/编辑弹窗 -->
    <div v-if="editingCharacter" class="modal-mask" @click.self="editingCharacter = null">
      <div class="modal card modal-lg">
        <h2>{{ editingCharacter === 'new' ? '新建角色' : '编辑角色' }} <span v-if="editingCharacter !== 'new' && editingCharacter.name">「{{ editingCharacter.name }}」</span></h2>

        <!-- 角色编辑标签页（仅已保存的角色显示档案标签） -->
        <div v-if="editingCharacter !== 'new'" class="char-edit-tabs">
          <button class="char-edit-tab" :class="{ active: charProfileTab === 'basic' }" @click="charProfileTab = 'basic'">基础信息</button>
          <button class="char-edit-tab" :class="{ active: charProfileTab === 'profile' }" @click="charProfileTab = 'profile'">
            详细档案 <span v-if="editingCharacter.profile_status" :class="'badge ' + profileStatusClass(editingCharacter.profile_status)">{{ profileStatusText(editingCharacter.profile_status) }}</span>
          </button>
          <button class="char-edit-tab" :class="{ active: charProfileTab === 'prompt' }" @click="charProfileTab = 'prompt'">参考像提示词</button>
        </div>

        <!-- 基础信息标签页 -->
        <div v-if="charProfileTab === 'basic'">
          <div class="field">
            <label>角色名 <span class="req">必填</span></label>
            <input v-model="charForm.name" class="input" placeholder="如：林夏" />
          </div>
          <div class="field">
            <label>身份 <span class="optional">可选</span></label>
            <input v-model="charForm.role" class="input" placeholder="主角 / 女主 / 反派 / 配角…" />
          </div>
          <div class="field">
            <label>外貌特征 <span class="optional">用于保证人物一致</span></label>
            <textarea v-model="charForm.trait" class="textarea" rows="3"
              placeholder="发型、五官、体型、年龄感、肤色…" />
          </div>
          <div class="field">
            <label>服装造型</label>
            <textarea v-model="charForm.style" class="textarea" rows="2"
              placeholder="标志性服装、配饰、主色调…" />
          </div>
          <div class="field">
            <label>配音音色 <span class="optional">可选，预设音色 ID</span></label>
            <input v-model="charForm.voice" class="input" list="voice-presets" placeholder="如 Cherry / Ethan；留空使用平台设置的音色映射" />
            <datalist id="voice-presets">
              <option v-for="v in voicePresets" :key="v" :value="v" />
            </datalist>
            <div class="field-hint">角色级音色优先于平台设置的角色音色映射；上传参考语音复刻的音色优先级最高（在角色卡片上传）</div>
          </div>
        </div>

        <!-- 详细档案标签页（LumxAI 风格） -->
        <div v-if="charProfileTab === 'profile'">
          <div class="profile-hint">
            <p>💡 AI 将从故事内容中提取角色的详细特征，生成包含外貌、性格、背景、关系等多维度的结构化档案。</p>
            <div class="profile-actions">
              <button class="btn btn-secondary btn-sm" :disabled="generatingProfile || editingCharacter === 'new'" @click="generateCharacterProfile">
                {{ generatingProfile ? 'AI 生成中…' : '🤖 AI 生成完整档案' }}
              </button>
            </div>
          </div>

          <div class="field">
            <label>外貌描述</label>
            <textarea v-model="charProfileForm.appearance" class="textarea" rows="4"
              placeholder="发型（形状/长度/颜色/质感）、脸型、眉眼（形状/眼神特点）、鼻型、唇形、肤色、身材、特殊标记…" />
          </div>
          <div class="field">
            <label>性格特点</label>
            <textarea v-model="charProfileForm.personality" class="textarea" rows="3"
              placeholder="MBTI 性格类型、核心性格标签、行为模式、情绪表达习惯…" />
          </div>
          <div class="field">
            <label>背景故事</label>
            <textarea v-model="charProfileForm.background" class="textarea" rows="4"
              placeholder="出身背景、成长经历、关键事件、角色动机、个人目标与欲望…" />
          </div>
          <div class="field">
            <label>关系图谱</label>
            <textarea v-model="charProfileForm.relationships" class="textarea" rows="3"
              placeholder="与其他角色的关系描述（亲子/恋人/朋友/敌人等），关系动态变化…" />
          </div>
          <div class="field-row">
            <div class="field">
              <label>情绪表达</label>
              <textarea v-model="charProfileForm.emotions" class="textarea" rows="2"
                placeholder="喜怒哀乐的表现形式，面部表情和肢体语言特点…" />
            </div>
            <div class="field">
              <label>习惯动作</label>
              <textarea v-model="charProfileForm.habits" class="textarea" rows="2"
                placeholder="小动作、口头禅、紧张/放松时的标志性行为…" />
            </div>
          </div>
          <div class="field">
            <label>服装细节</label>
            <textarea v-model="charProfileForm.wardrobe_detail" class="textarea" rows="3"
              placeholder="材质、颜色、款式、重要配饰、随时间变化的造型演变…" />
          </div>
          <div class="field-row">
            <div class="field">
              <label>光影氛围</label>
              <input v-model="charProfileForm.lighting_mood" class="input"
                placeholder="柔和/硬朗/戏剧性、暖色调/冷色调…" />
            </div>
            <div class="field">
              <label>角色色调</label>
              <input v-model="charProfileForm.color_palette" class="input"
                placeholder="主色调、辅色调、点缀色…" />
            </div>
          </div>

          <!-- 审核工作流 -->
          <div class="profile-review" v-if="editingCharacter !== 'new'">
            <h4>审核工作流</h4>
            <div class="review-status">
              <span :class="'badge ' + profileStatusClass(editingCharacter.profile_status)">
                {{ profileStatusText(editingCharacter.profile_status) }}
              </span>
              <span v-if="editingCharacter.review_note" class="review-note">{{ editingCharacter.review_note }}</span>
            </div>
            <div class="review-actions">
              <button class="btn btn-sm btn-secondary" :disabled="busy || editingCharacter === 'new'" @click="saveCharacterProfile">
                {{ busy ? '保存中…' : '💾 保存档案' }}
              </button>
              <button class="btn btn-sm btn-primary" :disabled="busy" @click="approveCharacterProfile('审核通过')">
                ✓ 审核通过
              </button>
              <button class="btn btn-sm btn-danger" :disabled="busy" @click="showRejectDialog">
                ✗ 驳回修改
              </button>
              <button class="btn btn-sm btn-ghost" :disabled="busy" @click="resetCharacterProfile">
                重置为草稿
              </button>
            </div>
          </div>
        </div>

        <!-- 参考像提示词标签页 -->
        <div v-if="charProfileTab === 'prompt'">
          <div class="profile-hint">
            <p>💡 基于角色档案生成适合当前角色标准像流程的高质量单人参考像提示词。</p>
            <div class="profile-actions">
              <button class="btn btn-secondary btn-sm" :disabled="generatingPrompt || editingCharacter === 'new'" @click="generateReferencePrompt">
                {{ generatingPrompt ? '生成中…' : '生成参考像提示词' }}
              </button>
            </div>
          </div>

          <div class="field">
            <label>参考像提示词</label>
            <textarea v-model="charProfileForm.reference_prompt" class="textarea" rows="8"
              placeholder="系统将根据角色档案生成高质量的参考像提示词，也可手动编辑…" />
            <div class="field-hint">提示词应包含人物正面/3/4侧面照描述、精确外貌特征、服装造型、场景环境、光影氛围、构图方式、画风描述。</div>
          </div>

          <div v-if="charProfileForm.reference_prompt" class="prompt-preview">
            <h4>提示词预览</h4>
            <pre class="prompt-text">{{ charProfileForm.reference_prompt }}</pre>
          </div>
        </div>

        <div class="modal-actions">
          <button class="btn btn-ghost" @click="editingCharacter = null">关闭</button>
          <button class="btn" :disabled="busy || !charForm.name.trim()" @click="saveCharacter">
            {{ busy ? '保存中…' : '保存基础信息' }}
          </button>
        </div>
      </div>
    </div>

    <!-- 驳回原因弹窗 -->
    <div v-if="showRejectModal" class="modal-mask" @click.self="showRejectModal = false">
      <div class="modal card">
        <h2>驳回角色档案</h2>
        <div class="field">
          <label>驳回原因 <span class="req">必填</span></label>
          <textarea v-model="rejectReason" class="textarea" rows="4"
            placeholder="请说明需要修改的内容…" />
        </div>
        <div class="modal-actions">
          <button class="btn btn-ghost" @click="showRejectModal = false">取消</button>
          <button class="btn btn-danger" :disabled="busy || !rejectReason.trim()" @click="confirmReject">
            {{ busy ? '提交中…' : '确认驳回' }}
          </button>
        </div>
      </div>
    </div>

    <!-- 道具/场景新建/编辑弹窗 -->
    <div v-if="editingAsset" class="modal-mask" @click.self="editingAsset = null">
      <div class="modal card">
        <h2>{{ editingAsset === 'new' ? `新建${assetKindLabel}` : `编辑${assetKindLabel}` }}</h2>
        <div class="field">
          <label>{{ assetKindLabel }}名 <span class="req">必填</span></label>
          <input v-model="assetForm.name" class="input" :placeholder="assetTab === 'prop' ? '如：青铜古镜' : '如：云隐宗大殿'" />
          <div class="field-hint">名称须与剧本分镜中引用的{{ assetKindLabel }}名完全一致，才能自动匹配参考图</div>
        </div>
        <div class="field">
          <div class="field-label-actions"><label>简单说明 / AI 设计结果 <span class="optional">用于保证{{ assetKindLabel }}一致</span></label><button class="btn btn-sm btn-secondary" :disabled="redesigningAsset || !assetForm.name.trim() || !assetForm.description.trim()" @click="redesignAssetDescription">{{ redesigningAsset ? 'AI 设计中…' : 'AI 重新设计' }}</button></div>
          <textarea v-model="assetForm.description" class="textarea" rows="6"
            :placeholder="assetTab === 'prop' ? '简单写：青玉玉佩，金色云纹；AI 会补全形状、材质、比例和辨识细节。' : '简单写：云隐宗大殿，宏伟、晨雾；AI 会补全空间布局、建筑、陈设、材质和光线。'" />
        </div>
        <div v-if="assetError" class="notice error-notice">{{ assetError }}</div>
        <div class="modal-actions">
          <button class="btn btn-ghost" @click="editingAsset = null">取消</button>
          <button class="btn" :disabled="busy || !assetForm.name.trim()" @click="saveAsset">
            {{ busy ? '保存中…' : '保存' }}
          </button>
        </div>
      </div>
    </div>

    <!-- 视频分镜连续性弹窗 -->
    <div v-if="continuityScene" class="modal-mask" @click.self="closeContinuity">
      <div class="modal card continuity-modal">
        <FrameSelector
          :project-id="Number(id())"
          :scene-id="continuityScene.id"
          :scene-order="continuityScene.order"
          :source-scene-id="continuitySourceScene?.id || 0"
          :source-scene-order="continuitySourceScene?.order || 0"
          :source-video-ready="!!(continuitySourceScene?.video_input_file || continuitySourceScene?.video_file)"
          @close="closeContinuity"
          @frame-selected="onContinuityFrameSelected"
          @applied="onContinuityApplied"
        />
      </div>
    </div>

    <!-- 图片查看弹窗 -->
    <div v-if="viewer" class="modal-mask viewer-mask" tabindex="-1" ref="viewerMask" @click.self="closeViewer" @keydown.esc="closeViewer">
      <div class="viewer-panel" role="dialog" aria-modal="true" aria-label="图片预览">
        <div class="viewer-head">
          <span>图片预览</span>
          <button class="viewer-close" type="button" @click="closeViewer" aria-label="关闭图片预览" title="关闭 (Esc)">✕</button>
        </div>
        <div class="viewer-body">
          <img :src="viewer" alt="预览图片" class="viewer-img" />
        </div>
        <div class="viewer-foot">
          <span class="viewer-hint">点击遮罩或按 Esc 关闭</span>
          <div class="viewer-actions">
            <a :href="viewer + '?download=1'" class="btn btn-sm btn-download" @click.stop>下载原图</a>
            <button class="btn btn-sm btn-ghost btn-close" type="button" @click="closeViewer">关闭</button>
          </div>
        </div>
      </div>
    </div>
  </div>

  <div v-else-if="loadError" class="page empty">
    <div class="empty-icon">!</div>
    <div>{{ loadError }}</div>
    <button class="btn btn-sm" @click="load">重新加载</button>
  </div>
  <div v-else class="page empty">
    <div class="empty-icon">✦</div>
    <div>加载中…</div>
  </div>
</template>

<script setup>
import { computed, nextTick, onBeforeUnmount, onMounted, reactive, ref, watch } from 'vue'
import { useRoute, useRouter } from 'vue-router'
import { api } from '../api'
import { useAppStore } from '../stores/app'
import { useToastStore } from '../stores/toast'
import FrameSelector from '../components/FrameSelector.vue'

const route = useRoute()
const router = useRouter()
const store = useAppStore()
const toast = useToastStore()

const project = ref(null)
const scenes = ref([])
const merges = ref([])
const characters = ref([])
const characterCounts = ref({})
const dialogues = ref([])
const dubMergeOn = ref(true)
const mergeSub = ref(false)
const mergeDub = ref(true)
const curEpDubReady = computed(() => {
  const ids = currentScenes.value.map(s => s.id)
  const dubs = dialogues.value.filter(d => ids.includes(d.scene_id))
  return dubs.length > 0 && dubs.every(d => d.status === 'ready')
})
const editingCharacter = ref(null)
const charForm = reactive({ name: '', role: '', trait: '', style: '', voice: '' })

// 角色档案（LumxAI 风格）状态
const charProfileTab = ref('basic') // basic / profile / prompt
const charProfileForm = reactive({
  appearance: '', personality: '', background: '', relationships: '',
  emotions: '', habits: '', wardrobe_detail: '', lighting_mood: '',
  color_palette: '', reference_prompt: ''
})
const generatingProfile = ref(false)
const generatingPrompt = ref(false)
const profileStatusClass = (status) => ({
  draft: 'badge-gray', approved: 'badge-green', rejected: 'badge-red'
}[status] || 'badge-gray')
const profileStatusText = (status) => ({
  draft: '草稿', approved: '已审核', rejected: '已驳回'
}[status] || status)
const showRejectModal = ref(false)
const rejectReason = ref('')
function showRejectDialog() {
  rejectReason.value = ''
  showRejectModal.value = true
}
async function confirmReject() {
  showRejectModal.value = false
  await rejectCharacterProfile(rejectReason.value)
}

// 道具/场景资产 + 角色语音
const assetTab = ref('char') // char / prop / location
const assets = ref([])
const assetCounts = ref({})
const editingAsset = ref(null)
const assetError = ref('')
const assetForm = reactive({ name: '', description: '' })
const redesigningAsset = ref(false)
const voicePresets = ['Cherry', 'Ethan', 'Chelsie', 'Serena', 'Nofish', 'Dylan', 'Jada', 'Peter', 'Sunny', 'Luna']
const showScript = ref(false)
const showPlan = ref(false)
const viewer = ref(null)
const continuityScene = ref(null)
const continuitySourceScene = ref(null)
const videoTaskDetail = ref(null)
const videoTaskScene = ref(null)
const videoTaskPromptDraft = ref('')
const videoTaskTemplate = ref('minimax_h3_ref2v')
const videoTaskFirstFrame = ref('')
const videoTaskLastFrame = ref('')
const savingVideoPrompt = ref(false)
const regeneratingVideoPrompt = ref(false)
const viewerMask = ref(null)
const busy = ref(false)
const generatingScript = ref(false)
const generatingPlan = ref(false)
const scriptRendering = ref(false)
const expandingScript = ref(false)
const scriptDraft = ref('')
const scriptDirty = ref(false)
const videoProgressMap = ref({})
const editingScene = ref(null)
const editingProject = ref(false)
const savingScene = ref(false)
const redesigningScenePrompt = ref(false)
const savingProject = ref(false)
const sceneError = ref('')
const projectError = ref('')
const loadError = ref('')
const sceneForm = reactive({ title: '', content: '', duration: 5, image_prompt: '', video_prompt: '', visible_characters: '', voice_characters: '', mentioned_characters: '', visual_type: 'normal', mega_type: 'architecture' })
const sceneReferenceCandidates = ref([])
const selectedSceneReferences = ref([])
const sceneOutfitOptions = ref({})
const selectedSceneOutfits = ref({})
const projectForm = reactive({ title: '', genre: '', style: '', synopsis: '', audience: '', tone: '', ending: '', episodes: 10, aspect_ratio: '16:9' })
let timer = null
let wsTimer = null
let pollTimer = null

const id = () => route.params.id

const pendingScenes = computed(() => scenes.value.filter(s => s.status === 'pending').length)
const imageReadyScenes = computed(() => scenes.value.filter(s => s.status === 'image_ready').length)
const imageCount = computed(() => scenes.value.filter(s => s.image_file).length)
const videoReadyCount = computed(() => scenes.value.filter(s => s.status === 'video_ready').length)
const allPortraitsReady = computed(() => characters.value.length > 0 && characters.value.every(c => c.portrait))
const approvedCharsWithoutPortrait = computed(() => characters.value.filter(c => !c.portrait && c.profile_status === 'approved' && c.reference_prompt).length)
const propAssets = computed(() => assets.value.filter(a => a.kind === 'prop'))
const locationAssets = computed(() => assets.value.filter(a => a.kind === 'location'))
const currentAssets = computed(() => (assetTab.value === 'location' ? locationAssets.value : propAssets.value))
const assetKindLabel = computed(() => (assetTab.value === 'location' ? '场景' : '道具'))
const assetsWithoutImage = computed(() => currentAssets.value.filter(a => !a.image && !a.image_task_id).length)
const allAssetImagesReady = computed(() => assets.value.length === 0 || assets.value.every(a => a.image))
const allRefsReady = computed(() => allPortraitsReady.value && allAssetImagesReady.value)
const readyText = computed(() => `${videoReadyCount.value}/${scenes.value.length} 视频就绪`)
const pipelineActive = computed(() => ['plan', 'plan_running', 'script', 'script_running', 'script_manual', 'images', 'videos', 'merge'].includes(project.value?.pipeline_stage))
const projectStatusText = computed(() => ({
  draft: '草稿', plan_done: '方案就绪', script_done: '剧本就绪', producing: '制作中', ready: '全部就绪', finished: '成片完成', failed: '异常'
}[project.value.status] || project.value.status))
const projectBadgeClass = computed(() => ({
  draft: 'badge-gray', plan_done: 'badge-purple', script_done: 'badge-blue', producing: 'badge-orange', ready: 'badge-green', finished: 'badge-green', failed: 'badge-red'
}[project.value.status] || 'badge-gray'))
const plan = computed(() => {
  if (!project.value?.plan) return null
  try { return JSON.parse(project.value.plan) } catch (_) { return null }
})

const epEdits = ref([])
const epSaving = ref(false)
const epDirtyCount = computed(() => epEdits.value.filter(isEpDirty).length)
function trackEpisodes() {
  const p = plan.value
  if (!p || !p.episodes || p.episodes.length === 0) return
  epEdits.value = p.episodes.map(e => ({
    n: e.n, title: e.title || '', brief: e.brief || '', tag: e.tag || '',
    origTitle: e.title || '', origBrief: e.brief || '',
    // 目标时长/镜头数（兼容旧项目缺失字段）
    target_duration: e.target_duration || 180,
    target_scenes: e.target_scenes || 25,
    origTargetDuration: e.target_duration || 180,
    origTargetScenes: e.target_scenes || 25
  }))
  if (!activeEpN.value || !p.episodes.some(e => e.n === activeEpN.value)) {
    activeEpN.value = p.episodes[0].n
    syncEpisodeQuery(activeEpN.value)
  }
}
function isEpDirty(e) {
  return e.origTitle !== e.title || e.origBrief !== e.brief || e.origTargetDuration !== e.target_duration || e.origTargetScenes !== e.target_scenes
}
async function saveEpisodes() {
  if (epEdits.value.length === 0) return
  const eps = epEdits.value.map(e => ({
    n: e.n, title: e.title.trim(), brief: e.brief.trim(),
    target_duration: e.target_duration, target_scenes: e.target_scenes
  }))
  epSaving.value = true
  try {
    const { data } = await api.updatePlanEpisodes(id(), eps)
    project.value = data.project
    trackEpisodes()
    toast.show('分集提示词已保存，重新生成剧本后生效')
  } catch (e) {
    toast.show(e.response?.data?.error || '保存失败')
  } finally {
    epSaving.value = false
  }
}

// ---------- 按集工作区 ----------
const queryEpisode = Number.parseInt(String(route.query.episode || ''), 10)
const activeEpN = ref(Number.isInteger(queryEpisode) && queryEpisode > 0 ? queryEpisode : 1)
const curMerging = ref(false)
async function selectEp(n) {
  const episodeN = Number(n)
  if (!Number.isInteger(episodeN) || episodeN <= 0) return
  if (episodeN !== activeEpN.value && scriptDirty.value && !confirm(`第${activeEpN.value}集剧本有未保存修改，切换后将丢弃？`)) return
  activeEpN.value = episodeN
  syncEpisodeQuery(episodeN)
  scriptDirty.value = false
  scenePage.value = 1
  await load()
  await nextTick()
  // 即使该集尚无分镜，也滚动到剧本工作区，让“进入”操作有明确反馈。
  const target = document.querySelector('.scene-grid') || document.querySelector('#episode-workspace')
  target?.scrollIntoView({ behavior: 'smooth', block: 'start' })
}
function syncEpisodeQuery(n) {
  if (String(route.query.episode || '') === String(n)) return
  router.replace({ query: { ...route.query, episode: String(n) } })
}
const currentScenes = computed(() => scenes.value
  .filter(s => (s.episode_n || 1) === activeEpN.value)
  .sort((a, b) => a.order - b.order))
// 场景分页
const scenePageSize = 6
const scenePage = ref(1)
const scenePageCount = computed(() => Math.max(1, Math.ceil(currentScenes.value.length / scenePageSize)))
const pagedScenes = computed(() => {
  const start = (scenePage.value - 1) * scenePageSize
  return currentScenes.value.slice(start, start + scenePageSize)
})
// 合并历史折叠
const mergeShowLimit = 3
const showAllMerges = ref(false)
const visibleMerges = computed(() => showAllMerges.value ? merges.value : merges.value.slice(0, mergeShowLimit))
const currentEpTitle = computed(() => {
  const e = epEdits.value.find(x => x.n === activeEpN.value)
  return e ? (e.title || `第${e.n}集`) : `第${activeEpN.value}集`
})
const curPendingScenes = computed(() => currentScenes.value.filter(s => s.status === 'pending').length)
const curImageReadyScenes = computed(() => currentScenes.value.filter(s => s.status === 'image_ready').length)
const curVideoReadyCount = computed(() => currentScenes.value.filter(s => s.status === 'video_ready').length)
const curReadyText = computed(() => `${curVideoReadyCount.value}/${currentScenes.value.length} 视频就绪`)
const curReadyScenes = computed(() => currentScenes.value.filter(s => s.status === 'video_ready'))
async function autoMergeEpisode() {
  const ids = curReadyScenes.value.map(s => s.id)
  if (ids.length < 2) {
    toast.show('该集至少需要 2 个视频就绪的场景才能合并')
    return
  }
  curMerging.value = true
  try {
    await api.mergeScenes(id(), { scene_ids: ids, dub: mergeDub.value, subtitles: mergeSub.value })
    await load()
    toast.show(`第${activeEpN.value}集合并任务已启动，完成后可下载成片`)
  } catch (e) {
    toast.show('合并失败：' + (e.response?.data?.error || e.message))
  } finally {
    curMerging.value = false
  }
}

function isWorking(sc) {
  return sc.status === 'video_creating' || sc.status === 'video_pending' || sc.status === 'video_running' || sc.status === 'image_pending'
}
function isVideoWorking(sc) {
  return sc.status === 'video_creating' || sc.status === 'video_pending' || sc.status === 'video_running'
}
function sceneStatusText(sc) {
  return {
    pending: '待画面', image_pending: '画面生成中', image_ready: '画面就绪', video_pending: '视频排队',
    video_creating: '创建视频任务', video_running: '视频生成中', video_ready: '视频就绪', failed: '失败'
  }[sc.status] || sc.status
}
function sceneBadgeClass(sc) {
  return {
    pending: 'badge-gray', image_pending: 'badge-blue', image_ready: 'badge-blue', video_pending: 'badge-orange',
    video_creating: 'badge-orange', video_running: 'badge-orange', video_ready: 'badge-green', failed: 'badge-red'
  }[sc.status] || 'badge-gray'
}
function mergeStatusText(m) {
  return { pending: '等待中', running: '合并中', success: '成片完成', failed: '失败' }[m.status] || m.status
}
function mergeBadgeClass(m) {
  return { pending: 'badge-gray', running: 'badge-orange', success: 'badge-green', failed: 'badge-red' }[m.status] || 'badge-gray'
}
function orderColor(n) {
  return ['', 'icon-blue', 'icon-purple', 'icon-teal', 'icon-orange', 'icon-green', 'icon-pink', 'icon-indigo', 'icon-brown'][n % 8 + 1]
}
function fmtSec(s) {
  // 格式化为 mm:ss 或 ss（秒）
  const sec = Math.round(Number(s) || 0)
  if (sec < 60) return sec + 's'
  return Math.floor(sec / 60) + ':' + String(sec % 60).padStart(2, '0')
}
function videoProgress(sc) {
  const p = videoProgressMap.value[sc.video_task_id]
  return p !== undefined ? `视频生成中 ${Math.min(99, Math.floor(p))}%` : '视频生成中…'
}
function imageUrl(sc) {
  return api.inputUrl(project.value.id, sc.image_file)
}
function openContinuity(sc) {
  continuityScene.value = sc
  continuitySourceScene.value = scenes.value
    .filter(candidate => candidate.episode_n === sc.episode_n && candidate.generation === sc.generation && candidate.order < sc.order)
    .sort((a, b) => b.order - a.order)[0] || null
}
function closeContinuity() {
  continuityScene.value = null
  continuitySourceScene.value = null
}
function onContinuityFrameSelected(frame) {
  toast.success(frame.source === 'manual_upload' ? '高清衔接帧已替换' : `已选择第 ${frame.frame_index + 1} 张衔接帧`)
}
async function onContinuityApplied(_continuity, openPrompt = false) {
  const sceneId = continuityScene.value?.id
  closeContinuity()
  await load()
  toast.success('连续性生成方式已保存')
  if (openPrompt && sceneId) {
    const scene = scenes.value.find(item => item.id === sceneId)
    if (scene) await viewVideoPrompt(scene)
  }
}

async function viewVideoPrompt(sc) {
  try {
    const { data: preview } = await api.sceneVideoPrompt(id(), sc.id)
    let history = null
    if (sc.video_task_id) {
      try { history = (await api.task(sc.video_task_id)).data } catch { /* 历史任务不可用不影响生成前编辑 */ }
    }
    videoTaskDetail.value = { ...preview, history_prompt: history?.prompt || '', params_json: history?.params_json || '' }
    videoTaskScene.value = sc
    videoTaskPromptDraft.value = preview.prompt || ''
    videoTaskTemplate.value = preview.template || sc.video_template || 'minimax_h3_ref2v'
    videoTaskFirstFrame.value = preview.first_frame_img || sc.video_first_frame_img || sc.image_file || ''
    videoTaskLastFrame.value = preview.last_frame_img || sc.video_last_frame_img || ''
  } catch (e) { toast.error(e.response?.data?.error || '生成视频提示词失败') }
}
async function regenerateVideoPrompt() {
  if (!videoTaskScene.value) return
  regeneratingVideoPrompt.value = true
  try {
    const { data } = await api.regenerateSceneVideoPrompt(id(), videoTaskScene.value.id)
    videoTaskPromptDraft.value = data.full_prompt || data.prompt || ''
    videoTaskDetail.value = { ...videoTaskDetail.value, full_prompt: videoTaskPromptDraft.value }
    toast.success('AI 已重新生成完整 H3 提示词，可继续修改后保存')
  } catch (e) { toast.error(e.response?.data?.error || 'AI 重新生成失败') }
  finally { regeneratingVideoPrompt.value = false }
}
async function saveVideoPrompt(generateAfterSave = false) {
  if (videoTaskTemplate.value === 'minimax_h3_first_last' && (!videoTaskFirstFrame.value || !videoTaskLastFrame.value)) {
    toast.show('首尾帧模板需要同时指定首帧图片和尾帧图片')
    return
  }
  savingVideoPrompt.value = true
  try {
    const { data } = await api.updateSceneVideoPrompt(id(), videoTaskScene.value.id, { prompt: videoTaskPromptDraft.value, template: videoTaskTemplate.value, first_frame_img: videoTaskFirstFrame.value, last_frame_img: videoTaskLastFrame.value })
    videoTaskPromptDraft.value = data.video_full_prompt || videoTaskPromptDraft.value.trim()
    videoTaskScene.value.video_full_prompt = videoTaskPromptDraft.value
    videoTaskScene.value.video_template = videoTaskTemplate.value
    videoTaskScene.value.video_first_frame_img = videoTaskFirstFrame.value
    videoTaskScene.value.video_last_frame_img = videoTaskLastFrame.value
    if (generateAfterSave) {
      await api.generateSceneVideo(id(), videoTaskScene.value.id)
      toast.success('已保存最新提示词并创建视频任务')
      refreshSoon()
    } else {
      toast.success('视频提示词和模板已保存，下次生成视频时生效')
    }
    videoTaskDetail.value = null
  } catch (e) { toast.error(e.response?.data?.error || '保存视频提示词失败') }
  finally { savingVideoPrompt.value = false }
}
const previewVideoFullPrompt = computed(() => String(videoTaskPromptDraft.value || '').trim())
function continuityModeLabel(mode) { return mode === 'bridge_to_storyboard' ? '上一镜末尾帧 → 当前分镜图' : '从上一镜末尾帧继续' }
function formatFrameTime(ms) { const value = Number(ms || 0); return value < 0 ? `${(value / 1000).toFixed(3)} 秒（距上一镜结尾）` : `${(value / 1000).toFixed(3)} 秒` }
function formatTaskParams(raw) { try { return JSON.stringify(JSON.parse(raw || '{}'), null, 2) } catch { return raw || '' } }

function videoUrl(sc) {
  return sc.video_input_file ? api.inputUrl(project.value.id, sc.video_input_file) : api.outputUrl(sc.video_gpu, sc.video_file)
}
function outputUrl(gpu, path) {
  return api.outputUrl(gpu, path)
}
function viewImage(sc) {
  viewer.value = imageUrl(sc)
  nextTick(() => viewerMask.value?.focus())
}
function closeViewer() {
  viewer.value = null
}

async function load() {
  try {
    const { data } = await api.project(id())
    loadError.value = ''
    project.value = data.project
    if (epDirtyCount.value === 0) trackEpisodes()
    const cur = (data.project.scripts ? JSON.parse(data.project.scripts) : {})[activeEpN.value] || ''
    if (!scriptDirty.value) {
      scriptDraft.value = cur
    }
    merges.value = data.merges || []
    const previousCharacters = new Map(characters.value.map(character => [character.id, character]))
    const nextCharacters = data.characters || []
    for (const character of nextCharacters) {
      const previous = previousCharacters.get(character.id)
      if (previous?.portrait_task_id && !character.portrait_task_id && character.portrait && character.portrait !== previous.portrait) {
        toast.show(`角色「${character.name}」标准像生成完成`)
      } else if (previous?.portrait_task_id && !character.portrait_task_id && character.portrait_error) {
        toast.show(`角色「${character.name}」标准像处理失败：${character.portrait_error}`)
      }
      if (previous?.sheet_task_id && !character.sheet_task_id && character.sheet && character.sheet !== previous.sheet) {
        toast.success(`角色「${character.name}」四视图生成完成`)
      } else if (previous?.sheet_task_id && !character.sheet_task_id && character.sheet_error) {
        toast.error(`角色「${character.name}」四视图生成失败：${character.sheet_error}`)
      }
    }
    characters.value = nextCharacters
    characterCounts.value = data.character_counts || {}
    const previousAssets = new Map(assets.value.map(asset => [asset.id, asset]))
    const nextAssets = data.assets || []
    for (const asset of nextAssets) {
      const previous = previousAssets.get(asset.id)
      const label = asset.kind === 'location' ? '场景' : '道具'
      if (previous?.image_task_id && !asset.image_task_id && asset.image && asset.image !== previous.image) {
        toast.success(`${label}「${asset.name}」Krea2 参考图生成完成`)
      } else if (previous?.image_task_id && !asset.image_task_id && asset.image_error) {
        toast.error(`${label}「${asset.name}」Krea2 参考图生成失败：${asset.image_error}`)
      }
      if (asset.kind === 'prop' && previous?.sheet_task_id && !asset.sheet_task_id && asset.sheet && asset.sheet !== previous.sheet) {
        toast.success(`道具「${asset.name}」四视图生成完成`)
      } else if (asset.kind === 'prop' && previous?.sheet_task_id && !asset.sheet_task_id && asset.sheet_error) {
        toast.error(`道具「${asset.name}」四视图生成失败：${asset.sheet_error}`)
      }
    }
    assets.value = nextAssets
    assetCounts.value = data.asset_counts || {}
    dialogues.value = data.dialogues || []
    scenes.value = (data.scenes || []).map(s => {
      s._working = isWorking(s)
      return s
    })
    if (data.scenes?.some(s => isVideoWorking(s))) {
      fetchProgress()
    }
  } catch (e) {
    loadError.value = e.response?.data?.error || '项目加载失败，请检查服务连接'
  }
}

async function fetchProgress() {
  try {
    const { data } = await api.tasks({ size: 50 })
    const map = {}
    ;(data.items || []).forEach(t => { map[t.task_id] = t.progress })
    videoProgressMap.value = map
  } catch (_) {
  }
}

function refreshSoon() {
  clearTimeout(timer)
  timer = setTimeout(load, 400)
}

async function regenerateScript() {
  busy.value = true
  generatingScript.value = true
  try {
    await api.generateScript(id(), activeEpN.value)
    await load()
  } catch (e) {
    toast.show('生成剧本失败：' + (e.response?.data?.error || e.message))
  } finally {
    busy.value = false
    generatingScript.value = false
  }
}

// 保存编辑后的剧本，并调用 AI 重新渲染该集分镜场景
async function saveAndRender() {
  if (!scriptDraft.value.trim()) return
  scriptRendering.value = true
  try {
    await api.renderScriptFromText(id(), activeEpN.value, scriptDraft.value.trim())
    scriptDirty.value = false
    await load()
    toast.show(`第${activeEpN.value}集分镜场景已按新剧本重新生成，可在下方生成画面`)
  } catch (e) {
    toast.show('重新生成分镜失败：' + (e.response?.data?.error || e.message))
  } finally {
    scriptRendering.value = false
  }
}

// AI 扩写剧本正文：根据当前内容丰富场景/对白/冲突，回填编辑框（不自动保存，用户确认后可保存重生分镜）
async function aiExpand() {
  if (!scriptDraft.value.trim()) return
  expandingScript.value = true
  try {
    const { data } = await api.expandScript(id(), activeEpN.value, scriptDraft.value.trim())
    if (data.script && data.script.trim()) {
      scriptDraft.value = data.script
      scriptDirty.value = true
      toast.success('AI 扩写完成，确认满意后点「保存并 AI 重新生成分镜」')
    } else {
      toast.show('AI 扩写返回为空，请重试')
    }
  } catch (e) {
    toast.error('扩写失败：' + (e.response?.data?.error || e.message))
  } finally {
    expandingScript.value = false
  }
}

async function generatePlan() {
  busy.value = true
  generatingPlan.value = true
  try {
    const { data } = await api.generatePlan(id())
    project.value = data.project
    showPlan.value = true
    await load() // 刷新角色列表（方案抽取的角色）
  } catch (e) {
    toast.show('生成方案失败：' + (e.response?.data?.error || e.message))
  } finally {
    busy.value = false
    generatingPlan.value = false
  }
}

// ---------- 角色资产 ----------
function charPortraitUrl(ch) {
  return api.inputUrl(project.value.id, ch.portrait)
}
function viewCharPortrait(ch) {
  if (ch.portrait) {
    viewer.value = charPortraitUrl(ch)
    nextTick(() => viewerMask.value?.focus())
  }
}
async function allPortraits() {
  busy.value = true
  try {
    const { data } = await api.generateAllPortraits(id())
    toast.show(data.message || '已提交')
    refreshSoon()
  } catch (e) {
    toast.show(e.response?.data?.error || '生成失败')
  } finally {
    busy.value = false
  }
}
async function genPortrait(ch) {
  try {
    await api.generateCharacterPortrait(id(), ch.id)
    refreshSoon()
  } catch (e) {
    toast.show(e.response?.data?.error || '生成失败')
  }
}
async function recoverPortrait(ch) {
  ch._recovering = true
  try {
    const { data } = await api.recoverCharacterPortrait(id(), ch.id)
    const current = data.character || {}
    if (current.portrait) toast.success('已找到并回写标准像')
    else if (current.portrait_error) toast.error(current.portrait_error)
    else toast.show(`任务状态：${data.task?.status || '未知'}，尚未发现可用图片`)
    await load()
  } catch (e) { toast.error(e.response?.data?.error || '检查生成结果失败') }
  finally { ch._recovering = false }
}
async function resetPortrait(ch) {
  if (!confirm(`重置角色“${ch.name}”的标准像生成状态？旧任务将被取消。`)) return
  ch._resetting = true
  try { await api.resetCharacterPortrait(id(), ch.id); await load(); toast.success('生成状态已重置，可以重新生图') }
  catch (e) { toast.error(e.response?.data?.error || '重置失败') }
  finally { ch._resetting = false }
}
async function genCharacterSheet(ch) {
  try {
    const { data } = await api.generateCharacterSheet(id(), ch.id)
    toast.show(data.message || '角色四视图任务已提交')
    await load()
  } catch (e) {
    toast.error(e.response?.data?.error || '角色四视图任务提交失败')
  }
}
function viewCharacterSheet(ch) {
  if (ch.sheet) {
    viewer.value = api.inputUrl(project.value.id, ch.sheet)
    nextTick(() => viewerMask.value?.focus())
  }
}
async function uploadPortrait(ch) {
  const input = document.createElement('input')
  input.type = 'file'
  input.accept = 'image/*'
  input.onchange = async () => {
    const f = input.files && input.files[0]
    if (!f) return
    if (!/^image\/(jpeg|png|webp)$/.test(f.type)) {
      toast.show('请上传 JPG/PNG/WebP 图片')
      return
    }
    if (f.size > 10 * 1024 * 1024) {
      toast.show('图片不能超过 10MB')
      return
    }
    ch._uploading = true
    try {
      await api.uploadCharacterPortrait(id(), ch.id, f)
      toast.show('照片已设为角色标准像')
      await load()
    } catch (e) {
      toast.show(e.response?.data?.error || '上传失败')
    } finally {
      ch._uploading = false
    }
  }
  input.click()
}
function openCreateCharacter() {
  Object.assign(charForm, { name: '', role: '', trait: '', style: '', voice: '' })
  editingCharacter.value = 'new'
}
function openEditCharacter(ch) {
  Object.assign(charForm, { name: ch.name, role: ch.role, trait: ch.trait, style: ch.style, voice: ch.voice || '' })
  // 加载 LumxAI 风格档案字段
  Object.assign(charProfileForm, {
    appearance: ch.appearance || '',
    personality: ch.personality || '',
    background: ch.background || '',
    relationships: ch.relationships || '',
    emotions: ch.emotions || '',
    habits: ch.habits || '',
    wardrobe_detail: ch.wardrobe_detail || '',
    lighting_mood: ch.lighting_mood || '',
    color_palette: ch.color_palette || '',
    reference_prompt: ch.reference_prompt || ''
  })
  charProfileTab.value = 'basic'
  editingCharacter.value = ch
}
async function saveCharacter() {
  if (!charForm.name.trim()) return
  busy.value = true
  try {
    if (editingCharacter.value === 'new') {
      await api.createCharacter(id(), { ...charForm })
    } else {
      await api.updateCharacter(id(), editingCharacter.value.id, { ...charForm })
    }
    editingCharacter.value = null
    await load()
  } catch (e) {
    toast.show(e.response?.data?.error || '保存失败')
  } finally {
    busy.value = false
  }
}
async function removeCharacter(ch) {
  if (!confirm(`确定删除角色「${ch.name}」？已生成标准像将被移除（不影响已有分镜）。`)) return
  try {
    await api.deleteCharacter(id(), ch.id)
    await load()
  } catch (e) {
    toast.show(e.response?.data?.error || '删除失败')
  }
}

// ---------- 角色档案（LumxAI 风格） ----------
async function generateCharacterProfile() {
  if (!editingCharacter.value || editingCharacter.value === 'new') {
    toast.show('请先保存角色后再生成档案')
    return
  }
  generatingProfile.value = true
  try {
    const { data } = await api.generateCharacterProfile(id(), editingCharacter.value.id)
    // 更新当前编辑的角色数据
    editingCharacter.value = data.character
    Object.assign(charProfileForm, {
      appearance: data.character.appearance || '',
      personality: data.character.personality || '',
      background: data.character.background || '',
      relationships: data.character.relationships || '',
      emotions: data.character.emotions || '',
      habits: data.character.habits || '',
      wardrobe_detail: data.character.wardrobe_detail || '',
      lighting_mood: data.character.lighting_mood || '',
      color_palette: data.character.color_palette || '',
      reference_prompt: data.character.reference_prompt || ''
    })
    // 更新角色列表中的数据
    const idx = characters.value.findIndex(c => c.id === data.character.id)
    if (idx !== -1) characters.value[idx] = data.character
    toast.show(data.message || '角色档案已生成')
    await load()
  } catch (e) {
    toast.show(e.response?.data?.error || '生成档案失败')
  } finally {
    generatingProfile.value = false
  }
}

async function generateReferencePrompt() {
  if (!editingCharacter.value || editingCharacter.value === 'new') {
    toast.show('请先保存角色后再生成参考像提示词')
    return
  }
  generatingPrompt.value = true
  try {
    // 提示词必须基于当前表单；先保存完整档案，避免使用数据库中的旧内容。
    const saved = await api.updateCharacterProfile(id(), editingCharacter.value.id, { ...charProfileForm })
    editingCharacter.value = saved.data.character
    const { data } = await api.generateReferencePrompt(id(), editingCharacter.value.id)
    charProfileForm.reference_prompt = data.prompt
    editingCharacter.value = data.character
    const idx = characters.value.findIndex(c => c.id === data.character.id)
    if (idx !== -1) characters.value[idx] = data.character
    toast.show(data.message || '参考像提示词已生成')
    await load()
  } catch (e) {
    toast.show(e.response?.data?.error || '生成提示词失败')
  } finally {
    generatingPrompt.value = false
  }
}

async function saveCharacterProfile() {
  if (!editingCharacter.value || editingCharacter.value === 'new') {
    toast.show('请先保存角色后再编辑档案')
    return
  }
  busy.value = true
  try {
    const { data } = await api.updateCharacterProfile(id(), editingCharacter.value.id, { ...charProfileForm })
    editingCharacter.value = data.character
    const idx = characters.value.findIndex(c => c.id === data.character.id)
    if (idx !== -1) characters.value[idx] = data.character
    toast.show('角色档案已保存')
    await load()
  } catch (e) {
    toast.show(e.response?.data?.error || '保存失败')
  } finally {
    busy.value = false
  }
}

async function approveCharacterProfile(note = '') {
  if (!editingCharacter.value || editingCharacter.value === 'new') return
  busy.value = true
  try {
    // 审核必须针对当前表单内容，先完整保存，避免批准数据库中的旧版本。
    const saved = await api.updateCharacterProfile(id(), editingCharacter.value.id, { ...charProfileForm })
    editingCharacter.value = saved.data.character
    const { data } = await api.approveCharacterProfile(id(), editingCharacter.value.id, note)
    editingCharacter.value = data.character
    const idx = characters.value.findIndex(c => c.id === data.character.id)
    if (idx !== -1) characters.value[idx] = data.character
    toast.show(data.message || '角色档案已审核通过')
    await load()
  } catch (e) {
    toast.show(e.response?.data?.error || '审核失败')
  } finally {
    busy.value = false
  }
}

async function rejectCharacterProfile(reason) {
  if (!editingCharacter.value || editingCharacter.value === 'new') return
  if (!reason || !reason.trim()) {
    toast.show('请提供驳回原因')
    return
  }
  busy.value = true
  try {
    const { data } = await api.rejectCharacterProfile(id(), editingCharacter.value.id, reason)
    editingCharacter.value = data.character
    const idx = characters.value.findIndex(c => c.id === data.character.id)
    if (idx !== -1) characters.value[idx] = data.character
    toast.show(data.message || '角色档案已驳回')
    await load()
  } catch (e) {
    toast.show(e.response?.data?.error || '操作失败')
  } finally {
    busy.value = false
  }
}

async function resetCharacterProfile() {
  if (!editingCharacter.value || editingCharacter.value === 'new') return
  busy.value = true
  try {
    const { data } = await api.resetCharacterProfile(id(), editingCharacter.value.id)
    editingCharacter.value = data.character
    const idx = characters.value.findIndex(c => c.id === data.character.id)
    if (idx !== -1) characters.value[idx] = data.character
    toast.show(data.message || '角色档案已重置为草稿')
    await load()
  } catch (e) {
    toast.show(e.response?.data?.error || '操作失败')
  } finally {
    busy.value = false
  }
}

// ---------- 角色语音（预设音色 / 参考语音复刻） ----------
async function uploadVoice(ch) {
  const input = document.createElement('input')
  input.type = 'file'
  input.accept = 'audio/*'
  input.onchange = async () => {
    const f = input.files && input.files[0]
    if (!f) return
    if (!/\.(mp3|wav|m4a|aac)$/i.test(f.name)) {
      toast.show('请上传 MP3/WAV/M4A/AAC 音频（建议 10~20 秒清晰人声）')
      return
    }
    if (f.size > 10 * 1024 * 1024) {
      toast.show('参考语音不能超过 10MB')
      return
    }
    ch._voiceUploading = true
    try {
      const { data } = await api.uploadCharacterVoice(id(), ch.id, f)
      if (data.warning) toast.show(data.warning)
      else toast.show(data.message || '参考语音已上传')
      await load()
    } catch (e) {
      toast.show(e.response?.data?.error || '上传失败')
    } finally {
      ch._voiceUploading = false
    }
  }
  input.click()
}
async function retryCloneVoice(ch) {
  ch._voiceUploading = true
  try {
    const { data } = await api.cloneCharacterVoice(id(), ch.id)
    toast.show(data.message || '复刻音色注册成功')
    await load()
  } catch (e) {
    toast.show(e.response?.data?.error || '注册失败')
  } finally {
    ch._voiceUploading = false
  }
}
async function clearVoice(ch) {
  if (!confirm(`确定清除「${ch.name}」的语音配置（预设音色与参考语音复刻）？`)) return
  try {
    await api.clearCharacterVoice(id(), ch.id)
    await load()
  } catch (e) {
    toast.show(e.response?.data?.error || '清除失败')
  }
}

// ---------- 道具/场景资产 ----------
function assetImageUrl(a) {
  return api.inputUrl(project.value.id, a.image)
}
function viewAssetImage(a) {
  if (a.image) {
    viewer.value = assetImageUrl(a)
    nextTick(() => viewerMask.value?.focus())
  }
}
async function genPropSheet(a) {
  try {
    const { data } = await api.generatePropSheet(id(), a.id)
    toast.show(data.message || '道具四视图任务已提交')
    await load()
  } catch (e) {
    toast.error(e.response?.data?.error || '道具四视图任务提交失败')
  }
}
function viewPropSheet(a) {
  if (a.sheet) {
    viewer.value = api.inputUrl(project.value.id, a.sheet)
    nextTick(() => viewerMask.value?.focus())
  }
}
async function allAssetImages() {
  busy.value = true
  try {
    const { data } = await api.generateAllAssetImages(id(), assetTab.value)
    toast.show(data.message || '已提交')
    refreshSoon()
  } catch (e) {
    toast.show(e.response?.data?.error || '生成失败')
  } finally {
    busy.value = false
  }
}
async function genAssetImage(a) {
  try {
    const { data } = await api.generateAssetImage(id(), a.kind, a.id)
    toast.show(data.message || 'Krea2 参考图任务已提交')
    await load()
  } catch (e) {
    toast.error(e.response?.data?.error || 'Krea2 参考图任务提交失败')
  }
}
function uploadAssetImage(a) {
  const input = document.createElement('input')
  input.type = 'file'
  input.accept = 'image/*'
  input.onchange = async () => {
    const f = input.files && input.files[0]
    if (!f) return
    if (!/^image\/(jpeg|png|webp)$/.test(f.type)) {
      toast.show('请上传 JPG/PNG/WebP 图片')
      return
    }
    if (f.size > 10 * 1024 * 1024) {
      toast.show('图片不能超过 10MB')
      return
    }
    a._uploading = true
    try {
      const { data } = await api.uploadAssetImage(id(), a.kind, a.id, f)
      // 上传结果优先于仍在运行的生成任务，先更新本地状态以免误报为 Krea2 完成。
      a.image = data.image || a.image
      a.image_task_id = ''
      a.image_error = ''
      toast.show('图片已设为参考图')
      await load()
    } catch (e) {
      toast.show(e.response?.data?.error || '上传失败')
    } finally {
      a._uploading = false
    }
  }
  input.click()
}
function openCreateAsset() {
  assetError.value = ''
  Object.assign(assetForm, { name: '', description: '' })
  editingAsset.value = 'new'
}
function openEditAsset(a) {
  assetError.value = ''
  Object.assign(assetForm, { name: a.name, description: a.description })
  editingAsset.value = a
}
async function redesignAssetDescription() {
  if (!assetForm.name.trim() || !assetForm.description.trim()) return
  redesigningAsset.value = true
  assetError.value = ''
  try {
    const kind = editingAsset.value === 'new' ? assetTab.value : editingAsset.value.kind
    const { data } = await api.redesignAssetDescription(id(), kind, { name: assetForm.name.trim(), brief: assetForm.description.trim() })
    assetForm.description = data.description
    toast.success(`${kind === 'location' ? '场景' : '道具'}设定已由 AI 重新设计，请确认后保存`)
  } catch (e) {
    assetError.value = e.response?.data?.error || 'AI 重新设计失败'
  } finally {
    redesigningAsset.value = false
  }
}

async function saveAsset() {
  if (!assetForm.name.trim()) return
  busy.value = true
  try {
    if (editingAsset.value === 'new') {
      await api.createAsset(id(), assetTab.value, { ...assetForm })
    } else {
      await api.updateAsset(id(), editingAsset.value.kind, editingAsset.value.id, { ...assetForm })
    }
    editingAsset.value = null
    await load()
  } catch (e) {
    assetError.value = e.response?.data?.error || '保存失败'
  } finally {
    busy.value = false
  }
}
async function removeAsset(a) {
  if (!confirm(`确定删除${a.kind === 'location' ? '场景' : '道具'}「${a.name}」？已生成参考图将被移除（不影响已有分镜）。`)) return
  try {
    await api.deleteAsset(id(), a.kind, a.id)
    await load()
  } catch (e) {
    toast.show(e.response?.data?.error || '删除失败')
  }
}

// ---------- 对白配音与字幕 ----------
function sceneDialogues(sc) {
  return dialogues.value.filter(d => d.scene_id === sc.id)
}
function dubAudioUrl(d) {
  return api.dubAudioUrl(project.value.id, d.audio_file)
}
async function dubScene(sc) {
  try {
    const { data } = await api.generateSceneDub(id(), sc.id)
    toast.show(data.message || '配音已提交')
    refreshSoon()
  } catch (e) {
    toast.error(e.response?.data?.error || '配音失败')
  }
}
async function dubAll() {
  busy.value = true
  try {
    const { data } = await api.generateProjectDub(id())
    toast.show(data.message || '配音已提交')
    refreshSoon()
  } catch (e) {
    toast.error(e.response?.data?.error || '配音失败')
  } finally {
    busy.value = false
  }
}

async function mergeAll() {
  busy.value = true
  try {
    const { data } = await api.mergeAllScenes(id(), { dub: mergeDub.value, subtitles: mergeSub.value })
    toast.show(data.message || '整剧合并已启动')
    refreshSoon()
  } catch (e) {
    toast.error(e.response?.data?.error || '整剧合并失败')
  } finally {
    busy.value = false
  }
}

async function stopVideo(sc) {
  if (!confirm(`确定停止场景 ${sc.order} 的视频生成？已生成画面会保留，可稍后重新生成视频。`)) return
  sc._stopping = true
  try {
    await api.cancelSceneVideo(id(), sc.id)
    refreshSoon()
  } catch (e) {
    toast.show('停止失败：' + (e.response?.data?.error || e.message))
    sc._stopping = false
  }
}

async function startPipeline() {
  busy.value = true
  try {
    const { data } = await api.generateProject(id(), activeEpN.value)
    toast.show(data.message || '完整生成流程已启动')
    await load()
  } catch (e) {
    toast.show('启动完整生成失败：' + (e.response?.data?.error || e.message))
  } finally {
    busy.value = false
  }
}

async function genImage(sc) {
  sc._working = true
  try {
    await api.generateSceneImage(id(), sc.id)
    refreshSoon()
  } catch (e) {
    toast.show('生成画面失败：' + (e.response?.data?.error || e.message))
    sc._working = false
  }
}

async function uploadSceneImage(sc, event) {
  const file = event.target.files?.[0]
  event.target.value = ''
  if (!file || busy.value || sc._working || isVideoWorking(sc)) return
  sc._working = true
  try {
    const { data } = await api.uploadSceneImage(id(), sc.id, file)
    toast.show(data.message || '分镜图已上传')
    await load()
  } catch (e) {
    toast.show('上传分镜图失败：' + (e.response?.data?.error || e.message))
  } finally {
    sc._working = false
  }
}

async function genVideo(sc) {
  sc._working = true
  try {
    await api.generateSceneVideo(id(), sc.id)
    refreshSoon()
  } catch (e) {
    toast.show('创建视频任务失败：' + (e.response?.data?.error || e.message))
    sc._working = false
  }
}

async function allImages() {
  busy.value = true
  try {
    const { data } = await api.generateAllImages(id(), activeEpN.value)
    currentScenes.value.forEach(s => {
      if (s.status === 'pending') s._working = true
    })
    toast.show(`第${activeEpN.value}集：` + (data.message || '已提交'))
  } catch (e) {
    toast.show(e.response?.data?.error || '提交失败')
  } finally {
    busy.value = false
    refreshSoon()
  }
}

async function allVideos() {
  busy.value = true
  try {
    const { data } = await api.generateAllVideos(id(), activeEpN.value)
    toast.show(`第${activeEpN.value}集：` + (data.message || '已提交'))
  } catch (e) {
    toast.show(e.response?.data?.error || '提交失败')
  } finally {
    busy.value = false
    refreshSoon()
  }
}

async function removeProject() {
  if (!confirm(`确定删除项目「${project.value.title}」？该操作不可恢复。`)) return
  await api.deleteProject(id())
  router.push('/projects')
}

// ---------- 编辑 ----------

async function openEditScene(sc) {
  sceneError.value = ''
  editingScene.value = sc
  Object.assign(sceneForm, {
    title: sc.title, content: sc.content, duration: Number(sc.duration) || 5, image_prompt: sc.image_prompt, video_prompt: sc.video_prompt || '',
    visible_characters: sc.character_roles_set ? (sc.visible_characters || '') : (sc.characters || ''),
    voice_characters: sc.voice_characters || '', mentioned_characters: sc.mentioned_characters || '',
    visual_type: sc.visual_type || 'normal', mega_type: sc.mega_type || 'architecture'
  })
  try {
    const sceneChars = sceneCharactersForOutfits(sc)
    const [{ data }, { data: videoPrompt }, { data: assigned }, outfitLists] = await Promise.all([
      api.sceneReferences(id(), sc.id),
      api.sceneVideoPrompt(id(), sc.id),
      api.sceneOutfits(id(), sc.id),
      Promise.all(sceneChars.map(ch => api.characterOutfits(id(), ch.id).then(r=>({cid:ch.id,rows:r.data||[]}))))
    ])
    sceneReferenceCandidates.value = data.candidates || []
    selectedSceneReferences.value = (data.references || []).map(x => ({ ...x }))
    sceneOutfitOptions.value = Object.fromEntries(outfitLists.map(x=>[x.cid,x.rows]))
    selectedSceneOutfits.value = Object.fromEntries((assigned||[]).map(x=>[x.character_id,x.outfit_id]))
    sceneForm.video_prompt = videoPrompt.prompt || ''
  } catch (e) {
    sceneReferenceCandidates.value = []
    selectedSceneReferences.value = []
    sceneError.value = e.response?.data?.error || '加载参考图库失败'
  }
}

function sceneCharactersForOutfits(sc=editingScene.value) { const names=String(sc?.characters||'').split(/[，,、]/).map(x=>x.trim()).filter(Boolean); return characters.value.filter(ch=>names.includes(ch.name)) }
function referenceKey(ref) { return `${ref.source_type}:${ref.source_id}:${ref.variant}` }
function referenceIndex(ref) { const key = referenceKey(ref); return selectedSceneReferences.value.findIndex(x => referenceKey(x) === key) }
function toggleSceneReference(ref) { const i = referenceIndex(ref); if (i >= 0) selectedSceneReferences.value.splice(i, 1); else selectedSceneReferences.value.push({ source_type: ref.source_type, source_id: ref.source_id, variant: ref.variant, use_krea2: true, use_h3: true }) }
function moveReference(index, delta) { const target = index + delta; if (target < 0 || target >= selectedSceneReferences.value.length) return; const [item] = selectedSceneReferences.value.splice(index, 1); selectedSceneReferences.value.splice(target, 0, item) }

async function redesignScenePrompt() {
  if (!editingScene.value || !sceneForm.content.trim()) return
  redesigningScenePrompt.value = true
  sceneError.value = ''
  try {
    await api.updateSceneReferences(id(), editingScene.value.id, selectedSceneReferences.value)
    const { data } = await api.redesignScenePrompt(id(), editingScene.value.id, sceneForm.content.trim())
    sceneForm.image_prompt = data.prompt
    toast.success('场景画面提示词已由 AI 重新设计，请确认后保存')
  } catch (e) {
    sceneError.value = e.response?.data?.error || 'AI 重新设计失败'
  } finally {
    redesigningScenePrompt.value = false
  }
}

async function saveScene() {
  savingScene.value = true
  sceneError.value = ''
  try {
    await api.updateScene(id(), editingScene.value.id, {
      title: sceneForm.title,
      content: sceneForm.content,
      duration: Number(sceneForm.duration),
      image_prompt: sceneForm.image_prompt,
      video_prompt: sceneForm.video_prompt,
      visible_characters: sceneForm.visible_characters,
      voice_characters: sceneForm.voice_characters,
      mentioned_characters: sceneForm.mentioned_characters,
      visual_type: sceneForm.visual_type,
      mega_type: sceneForm.mega_type
    })
    await api.updateSceneReferences(id(), editingScene.value.id, selectedSceneReferences.value)
    await api.updateSceneOutfits(id(), editingScene.value.id, Object.entries(selectedSceneOutfits.value).filter(([,oid])=>Number(oid)>0).map(([cid,oid])=>({character_id:Number(cid),outfit_id:Number(oid)})))
    const promptChanged = sceneForm.image_prompt !== editingScene.value.image_prompt
    editingScene.value = null
    await load()
    if (promptChanged) toast.show('画面提示词已修改，场景已重置，请重新生成画面')
  } catch (e) {
    sceneError.value = e.response?.data?.error || '保存失败'
  } finally {
    savingScene.value = false
  }
}

function aspectLabel(a) {
  return { '16:9': '横屏 16:9', '9:16': '竖屏 9:16', '1:1': '方形 1:1' }[a] || a || '横屏 16:9'
}
function openEditProject() {
  projectError.value = ''
  editingProject.value = true
  Object.assign(projectForm, {
    title: project.value.title,
    genre: project.value.genre,
    style: project.value.style,
    synopsis: project.value.synopsis,
    audience: project.value.audience || '',
    tone: project.value.tone || '',
    ending: project.value.ending || '',
    episodes: project.value.episodes || 0,
    aspect_ratio: project.value.aspect_ratio || '16:9'
  })
}

async function saveProject() {
  savingProject.value = true
  projectError.value = ''
  try {
    const { data } = await api.updateProject(id(), projectForm)
    project.value = data
    editingProject.value = false
  } catch (e) {
    projectError.value = e.response?.data?.error || '保存失败'
  } finally {
    savingProject.value = false
  }
}

// ---------- 实时刷新 ----------

watch(
  () => store.lastEvent,
  (t) => {
    if (t && scenes.value.some(s => s.video_task_id === t.task_id)) {
      videoProgressMap.value[t.task_id] = t.progress
    }
    refreshSoon()
  }
)

watch(
  () => store.lastProjectUpdate,
  () => refreshSoon()
)

onMounted(() => {
  load()
  wsTimer = setInterval(() => {
    if (store.lastEvent || store.lastProjectUpdate) refreshSoon()
  }, 1000)
  pollTimer = setInterval(load, 5000)
})

onBeforeUnmount(() => {
  clearInterval(wsTimer)
  clearInterval(pollTimer)
  clearTimeout(timer)
})
</script>

<style scoped>
.project-head {
  display: flex;
  justify-content: space-between;
  align-items: flex-start;
  gap: 20px;
  padding: 14px 0;
  position: sticky;
  top: var(--nav-h);
  z-index: 50;
  background: var(--bg);
  border-bottom: 1px solid var(--border);
}
.head-links .back { color: var(--text-secondary); font-size: 13px; text-decoration: none; }
.back:hover { color: var(--accent); }
.project-head h1 { margin: 10px 0 6px; font-size: 30px; }
.synopsis { margin: 0 0 8px; color: var(--text-secondary); font-size: 13px; max-width: 720px; line-height: 1.5; display: -webkit-box; -webkit-line-clamp: 2; -webkit-box-orient: vertical; overflow: hidden; }
.meta-tags { display: flex; gap: 8px; }
.tag {
  font-size: 11px; font-weight: 600; padding: 3px 10px; border-radius: 980px;
  background: var(--accent-soft); color: var(--accent);
}
.tag-orange { background: rgba(255, 159, 10, 0.14); color: #c47f00; }
.head-actions { display: flex; align-items: center; gap: 10px; flex: 0 0 auto; flex-wrap: wrap; }
.steps-bar { display: flex; align-items: center; gap: 12px; padding: 14px 20px; margin: 18px 0 26px; flex-wrap: wrap; }
.step { display: flex; align-items: center; gap: 8px; font-size: 13px; font-weight: 600; color: var(--text-secondary); }
.step-n {
  width: 22px; height: 22px; border-radius: 50%; background: rgba(0, 0, 0, 0.08);
  display: flex; align-items: center; justify-content: center; font-size: 12px; color: var(--text-tertiary);
}
.step.done { color: var(--text); }
.step.done .step-n { background: var(--gradient); color: #fff; }
.step-arrow { color: var(--text-tertiary); }
.step-status { margin-left: auto; display: inline-flex; align-items: center; gap: 6px; font-size: 12px; font-weight: 700; color: var(--green); }
.section { margin-bottom: 30px; }
.section-head { display: flex; justify-content: space-between; align-items: flex-end; margin-bottom: 14px; gap: 16px; flex-wrap: wrap; }
.overline { font-size: 11px; letter-spacing: 1.4px; color: var(--text-tertiary); font-weight: 700; }
.section-head h2 { margin: 2px 0 0; font-size: 21px; }
.section-head .count { font-size: 13px; color: var(--text-secondary); font-weight: 500; margin-left: 6px; }
.section-head .sub { margin: 4px 0 0; font-size: 13px; color: var(--text-secondary); }
.section-actions { display: flex; gap: 10px; }
.live-dot { display: inline-block; width: 7px; height: 7px; border-radius: 50%; background: var(--accent); margin-right: 2px; }
.script-card { padding: 20px; }
.script-text { white-space: pre-wrap; font-family: inherit; font-size: 13.5px; line-height: 1.8; color: var(--text); margin: 0; }
.plan-card { padding: 20px; }
.plan-grid { display: grid; grid-template-columns: repeat(auto-fit, minmax(300px, 1fr)); gap: 18px; }
.plan-block h4 { margin: 0 0 8px; font-size: 13px; color: var(--accent); }
.plan-line { margin: 0 0 6px; font-size: 13px; line-height: 1.6; color: var(--text-secondary); }
.plan-line b { color: var(--text); }
.plan-episodes { grid-column: 1 / -1; }
.plan-empty { color: var(--text-tertiary); font-size: 13px; line-height: 1.7; }
.plan-hint { margin: 0 0 10px; font-size: 12px; color: var(--text-tertiary); }
.script-edit { font-size: 13px; line-height: 1.7; color: var(--text); min-height: 220px; }
.script-edit-bar { display: flex; align-items: center; gap: 10px; margin-top: 8px; }
.script-dirty-hint { font-size: 12px; color: var(--accent); }
.plan-episode-row { display: flex; align-items: center; gap: 8px; margin-bottom: 8px; padding: 4px 8px; border-radius: 8px; border: 1px solid transparent; cursor: default; }
.plan-episode-row:hover { background: var(--accent-soft); }
.plan-episode-row.ep-active { border-color: var(--accent); background: var(--accent-soft); }
.plan-episode-row.ep-dirty-row { border-style: dashed; border-color: var(--accent); }
.plan-episode-row .ep-n { flex: 0 0 52px; font-size: 13px; color: var(--text-secondary); font-weight: 600; }
.plan-episode-row .ep-link { cursor: pointer; }
.plan-episode-row .ep-link:hover { color: var(--accent); text-decoration: underline; }
.plan-episode-row .ep-title { flex: 0 0 180px; }
.plan-episode-row .ep-brief { flex: 1; }
.plan-episode-row .ep-tag { flex: 0 0 auto; font-size: 12px; }
.plan-episode-row .ep-duration { flex: 0 0 60px; font-size: 12px; color: var(--text-secondary); text-align: center; }
.plan-episode-row .ep-dur { flex: 0 0 64px; font-size: 12px; }
.plan-episode-row .ep-scene-count { flex: 0 0 52px; font-size: 12px; }
.plan-episode-row .ep-enter { flex: 0 0 auto; padding: 2px 10px; font-size: 12px; }
.input-sm { padding: 5px 8px; font-size: 13px; min-height: 30px; }
.textarea-sm { padding: 5px 8px; font-size: 13px; min-height: 30px; resize: vertical; }
.ep-dirty { border-color: var(--accent) !important; }
.ep-dirty-hint { margin-left: 8px; font-size: 12px; color: var(--accent); }
.plan-episodes h4 { display: flex; align-items: center; }
.plan-episodes h4 .btn { margin-left: 10px; }
.stop-video { margin-left: 8px; padding: 2px 10px; font-size: 12px; }
.character-grid { display: grid; grid-template-columns: repeat(auto-fill, minmax(280px, 1fr)); gap: 16px; }
.asset-tabs { display: flex; gap: 8px; margin-bottom: 14px; flex-wrap: wrap; }
.asset-tab {
  padding: 7px 16px; border-radius: 980px; border: 1.5px solid var(--border); background: transparent;
  font-size: 13px; font-weight: 600; color: var(--text-secondary); cursor: pointer; transition: all 0.2s;
}
.asset-tab:hover { border-color: var(--accent); color: var(--accent); }
.asset-tab.active { border-color: var(--accent); background: var(--accent-soft); color: var(--accent); }
.char-voice { margin-top: 6px; font-size: 12px; color: var(--accent); font-weight: 600; }
.character-card { padding: 16px; display: flex; gap: 14px; align-items: flex-start; }
.char-portrait { flex: 0 0 88px; width: 88px; height: 88px; border-radius: 14px; overflow: hidden; background: rgba(0, 0, 0, 0.04); cursor: zoom-in; }
.char-portrait img { width: 100%; height: 100%; object-fit: cover; display: block; }
.char-portrait-ph { width: 100%; height: 100%; display: flex; align-items: center; justify-content: center; font-size: 34px; font-weight: 700; color: #fff; background: var(--gradient); }
.char-body { flex: 1; min-width: 0; display: flex; flex-direction: column; }
.char-name-row { display: flex; align-items: center; gap: 8px; flex-wrap: wrap; }
.char-name { font-weight: 700; font-size: 15px; }
.char-role { font-size: 11px; padding: 2px 9px; border-radius: 980px; background: var(--accent-soft); color: var(--accent); }
.char-trait, .char-style { margin: 4px 0 0; font-size: 12px; color: var(--text-secondary); line-height: 1.5; display: -webkit-box; -webkit-line-clamp: 2; -webkit-box-orient: vertical; overflow: hidden; }
.char-appear { margin-top: 6px; font-size: 12px; color: var(--text-tertiary); }
.char-actions { display: flex; gap: 6px; flex-wrap: wrap; margin-top: 8px; }
.empty-inline { padding: 18px; color: var(--text-tertiary); font-size: 13px; text-align: center; }
.field .req { color: var(--red); margin-left: 4px; font-weight: 400; }
.field .optional { color: var(--text-tertiary); font-weight: 400; margin-left: 4px; }
.tag-gray { background: rgba(0, 0, 0, 0.06); color: var(--text-secondary); }
.scene-grid { display: grid; grid-template-columns: repeat(auto-fill, minmax(340px, 1fr)); gap: 16px; }
.scene-card { padding: 18px; display: flex; flex-direction: column; }
.scene-active { border-color: var(--accent); box-shadow: 0 0 0 3px var(--accent-soft); }
.scene-head { display: flex; align-items: center; gap: 12px; margin-bottom: 12px; }
.scene-order {
  width: 30px; height: 30px; border-radius: 9px; color: #fff; font-weight: 700; font-size: 14px;
  display: flex; align-items: center; justify-content: center; flex: 0 0 auto;
}
.icon-blue { background: var(--accent); }
.icon-purple { background: var(--purple); }
.icon-teal { background: var(--teal); }
.icon-orange { background: var(--orange); }
.icon-green { background: var(--green); }
.icon-pink { background: #ff6482; }
.icon-indigo { background: #5856d6; }
.icon-brown { background: #a2845e; }
.scene-title { flex: 1; display: flex; align-items: center; justify-content: space-between; gap: 8px; min-width: 0; }
.scene-title div { font-weight: 700; font-size: 14px; overflow: hidden; text-overflow: ellipsis; white-space: nowrap; }
.icon-btn {
  border: none; background: transparent; color: var(--text-tertiary); font-size: 14px; cursor: pointer;
  padding: 4px 6px; border-radius: 6px; transition: all 0.2s; flex: 0 0 auto;
}
.icon-btn:hover { color: var(--accent); background: var(--accent-soft); }
.scene-preview { border-radius: 12px; overflow: hidden; background: rgba(0, 0, 0, 0.04); margin-bottom: 10px; aspect-ratio: 16/9; }
.scene-img { width: 100%; height: 100%; object-fit: cover; display: block; cursor: zoom-in; }
.scene-placeholder {
  height: 100%; display: flex; flex-direction: column; align-items: center; justify-content: center; gap: 8px;
  color: var(--text-tertiary); font-size: 13px;
}
.ph-icon { font-size: 26px; }
.ph-bar { width: 120px; height: 4px; border-radius: 2px; background: rgba(0, 0, 0, 0.08); overflow: hidden; }
.ph-bar-fill {
  display: block; width: 40%; height: 100%; border-radius: 2px;
  background: var(--gradient); animation: slide 1.2s ease-in-out infinite;
}
@keyframes slide { 0% { transform: translateX(-100%); } 100% { transform: translateX(320%); } }
.scene-content {
  margin: 0 0 6px; font-size: 13px; color: var(--text-secondary); line-height: 1.6;
  display: -webkit-box; -webkit-line-clamp: 4; -webkit-box-orient: vertical; overflow: hidden;
}
.scene-prompt {
  margin: 0 0 10px; font-size: 12px; color: var(--text-tertiary); line-height: 1.6;
  display: -webkit-box; -webkit-line-clamp: 3; -webkit-box-orient: vertical; overflow: hidden;
}
.scene-actions { display: flex; align-items: center; gap: 8px; flex-wrap: wrap; margin-top: auto; }
.scene-dialogues { margin: 0 0 10px; display: flex; flex-direction: column; gap: 5px; }
.dialogue-row { display: flex; align-items: center; gap: 8px; font-size: 12px; padding: 5px 9px; border-radius: 8px; background: rgba(0, 0, 0, 0.03); }
.dl-char { flex: 0 0 auto; font-weight: 700; color: var(--accent); }
.dl-text { flex: 1; min-width: 0; color: var(--text-secondary); line-height: 1.4; }
.dl-audio { flex: 0 0 auto; height: 26px; max-width: 180px; }
.dl-state { flex: 0 0 auto; font-size: 11px; color: var(--text-tertiary); }
.dl-fail { color: var(--red); }
.working { display: inline-flex; align-items: center; gap: 6px; font-size: 12px; font-weight: 600; color: var(--accent); }
.ready { font-size: 12px; font-weight: 700; color: var(--green); }
.fail-msg { font-size: 12px; color: var(--red); }
.video-box { position: relative; margin-top: 12px; border-radius: 12px; overflow: hidden; background: #000; }
.scene-video { width: 100%; display: block; max-height: 260px; }
.download { position: absolute; right: 8px; bottom: 8px; background: rgba(0, 0, 0, 0.55); color: #fff; border-color: transparent; }
.merge-card { padding: 20px; }
.merge-select { display: flex; flex-wrap: wrap; align-items: center; gap: 8px; margin-bottom: 16px; }
.merge-hint { font-size: 12px; color: var(--text-tertiary); margin-left: 6px; }
.merge-chip {
  display: inline-flex; align-items: center; gap: 8px; padding: 7px 12px; border-radius: 980px;
  border: 1.5px solid var(--border); background: transparent; font-size: 13px; cursor: pointer;
  transition: all 0.2s; color: var(--text-secondary);
}
.merge-chip .chip-check { opacity: 0; color: var(--green); font-weight: 700; }
.merge-chip.active { border-color: var(--accent); background: var(--accent-soft); color: var(--text); }
.merge-chip.active .chip-check { opacity: 1; }
.chip-n {
  width: 20px; height: 20px; border-radius: 50%; background: rgba(0, 0, 0, 0.08);
  display: flex; align-items: center; justify-content: center; font-size: 11px; font-weight: 700;
}
.merge-chip.active .chip-n { background: var(--accent); color: #fff; }
.merge-list { border-top: 1px solid var(--border); padding-top: 14px; display: flex; flex-direction: column; gap: 16px; }
.merge-more { align-self: flex-start; margin-top: 4px; }
.pager { display: flex; align-items: center; justify-content: center; gap: 14px; margin-top: 16px; }
.pager-info { font-size: 13px; color: var(--text-secondary); }
.merge-info { display: flex; align-items: center; gap: 10px; margin-bottom: 8px; }
.merge-time { font-size: 12px; color: var(--text-tertiary); }
.merge-result { display: flex; gap: 12px; align-items: flex-start; }
.merge-video { width: min(480px, 100%); border-radius: 12px; }
.merge-working { display: flex; align-items: center; gap: 8px; font-size: 13px; color: var(--text-secondary); }
.merge-opt { display: inline-flex; align-items: center; gap: 4px; font-size: 13px; color: var(--text-secondary); cursor: pointer; white-space: nowrap; }
.merge-opt input { width: 14px; height: 14px; cursor: pointer; margin: 0; }
.modal-mask {
  position: fixed; inset: 0; background: rgba(0, 0, 0, 0.6); backdrop-filter: blur(10px);
  display: flex; align-items: center; justify-content: center; z-index: 200; padding: 24px;
}
/* 图片查看：整图可见、无需滚动 */
.viewer-mask { overflow: hidden; padding: 32px; }
.viewer-panel {
  width: min(860px, calc(100vw - 64px)); max-height: min(82vh, 700px);
  display: grid; grid-template-rows: auto minmax(0, 1fr) auto;
  overflow: hidden; border-radius: 18px; background: var(--card-solid);
  box-shadow: 0 28px 80px rgba(0, 0, 0, 0.42);
}
.viewer-head {
  min-height: 52px; padding: 0 12px 0 18px; display: flex; align-items: center; justify-content: space-between;
  color: var(--text); font-size: 14px; font-weight: 650; border-bottom: 1px solid var(--border);
}
.viewer-body { min-height: 0; padding: 16px; display: flex; align-items: center; justify-content: center; background: #111; }
.viewer-img {
  display: block; max-width: 100%; max-height: min(62vh, 560px); border-radius: 10px; object-fit: contain;
}
.viewer-close {
  width: 36px; height: 36px; flex: 0 0 36px; border-radius: 10px;
  border: 1px solid var(--border); background: var(--bg); color: var(--text);
  font-size: 18px; cursor: pointer; display: flex; align-items: center; justify-content: center; transition: 0.2s;
}
.viewer-close:hover { background: var(--red); border-color: var(--red); color: #fff; }
.viewer-foot {
  min-height: 58px; padding: 10px 14px 10px 18px; display: flex; align-items: center; justify-content: space-between; gap: 16px;
  border-top: 1px solid var(--border);
}
.viewer-hint { font-size: 12px; color: var(--text-tertiary); }
.viewer-actions { display: flex; align-items: center; gap: 10px; }
.viewer-foot .btn-download { background: var(--accent); color: #fff; border-color: transparent; }
.viewer-foot .btn-close { min-width: 68px; }
.modal { width: min(520px, calc(100vw - 32px)); padding: 28px; animation: pop 0.25s ease; max-height: 90vh; overflow-y: auto; }
.modal h2 { margin: 0 0 18px; font-size: 19px; }
.field label { display: block; font-size: 13px; font-weight: 600; margin-bottom: 6px; }
.field + .field { margin-top: 14px; }
.field-row { display: grid; grid-template-columns: 1fr 1fr; gap: 14px; margin-top: 14px; }
.field-row + .field { margin-top: 14px; }
.field-hint { font-size: 12px; color: var(--text-tertiary); margin-top: 5px; }
.notice { border-radius: 12px; padding: 10px 14px; font-size: 13px; margin-top: 14px; }
.error-notice { background: rgba(255, 69, 58, 0.1); color: var(--red); }
.project-failure-notice { display: flex; align-items: center; justify-content: space-between; gap: 16px; margin: 0 0 20px; background: rgba(255, 69, 58, 0.1); color: var(--red); border: 1px solid rgba(255, 69, 58, 0.22); }
.project-failure-notice p { margin: 4px 0 0; white-space: pre-wrap; color: inherit; }
.modal-actions { display: flex; justify-content: flex-end; gap: 10px; margin-top: 22px; }
@keyframes pop { from { transform: scale(0.94); opacity: 0; } to { transform: scale(1); opacity: 1; } }

/* 角色档案编辑样式 */
.modal-lg { max-width: 800px; }
.char-edit-tabs {
  display: flex;
  gap: 8px;
  margin-bottom: 20px;
  border-bottom: 1px solid var(--border);
  padding-bottom: 12px;
}
.char-edit-tab {
  padding: 8px 16px;
  border-radius: 8px;
  border: 1.5px solid var(--border);
  background: transparent;
  font-size: 13px;
  font-weight: 600;
  color: var(--text-secondary);
  cursor: pointer;
  transition: all 0.2s;
  display: flex;
  align-items: center;
  gap: 6px;
}
.char-edit-tab:hover { border-color: var(--accent); color: var(--accent); }
.char-edit-tab.active { border-color: var(--accent); background: var(--accent-soft); color: var(--accent); }
.profile-hint {
  background: var(--accent-soft);
  border-radius: 10px;
  padding: 14px 16px;
  margin-bottom: 16px;
  font-size: 13px;
  color: var(--text-secondary);
  line-height: 1.6;
}
.profile-hint p { margin: 0 0 10px; }
.profile-actions { display: flex; gap: 10px; flex-wrap: wrap; }
.field-row { display: flex; gap: 16px; }
.field-row .field { flex: 1; }
.profile-review {
  background: rgba(0, 0, 0, 0.03);
  border-radius: 10px;
  padding: 16px;
  margin-top: 20px;
}
.profile-review h4 { margin: 0 0 12px; font-size: 14px; color: var(--text); }
.review-status {
  display: flex;
  align-items: center;
  gap: 12px;
  margin-bottom: 12px;
}
.review-note { font-size: 13px; color: var(--text-secondary); }
.review-actions { display: flex; gap: 8px; flex-wrap: wrap; }
.prompt-preview {
  background: rgba(0, 0, 0, 0.03);
  border-radius: 10px;
  padding: 16px;
  margin-top: 12px;
}
.prompt-preview h4 { margin: 0 0 10px; font-size: 14px; color: var(--text); }
.prompt-text {
  white-space: pre-wrap;
  font-size: 12px;
  line-height: 1.6;
  color: var(--text-secondary);
  margin: 0;
  max-height: 300px;
  overflow-y: auto;
  background: rgba(0, 0, 0, 0.04);
  padding: 12px;
  border-radius: 6px;
}
.scene-edit-modal { width: min(920px, 94vw); max-height: calc(100vh - 48px); overflow-y: auto; }
.continuity-modal { width: min(1000px, 94vw); padding: 0; max-height: calc(100vh - 48px); overflow-y: auto; }
.video-task-modal { width: min(900px, 94vw); max-height: calc(100vh - 48px); overflow-y: auto; }
.continuity-prompt-source { display: flex; align-items: center; gap: 14px; margin: 12px 0 18px; padding: 12px; border: 1px solid var(--border); border-radius: 8px; background: var(--surface-secondary); }
.continuity-prompt-source img { width: 180px; aspect-ratio: 16 / 9; object-fit: cover; border-radius: 6px; }
.continuity-prompt-source p { margin: 4px 0 0; color: var(--text-secondary); font-size: 13px; }
.task-prompt { white-space: pre-wrap; overflow-wrap: anywhere; background: var(--surface-secondary); padding: 14px; border-radius: 8px; font-size: 12px; line-height: 1.6; }
.field-label-actions { display: flex; justify-content: space-between; align-items: center; gap: 10px; }
.reference-picker { display: grid; grid-template-columns: repeat(auto-fill, minmax(180px, 1fr)); gap: 8px; margin-top: 8px; }
.reference-option { display: grid; grid-template-columns: auto 46px 1fr; align-items: center; gap: 8px; padding: 8px; border: 1px solid var(--border); border-radius: 8px; font-size: 12px; }
.reference-option img { width: 46px; height: 46px; object-fit: cover; border-radius: 6px; }
.outfit-select-row { display: grid; grid-template-columns: minmax(90px, auto) minmax(220px, 1fr) auto; align-items: center; gap: 10px; margin-top: 8px; }
.badge-green { background: rgba(34, 197, 94, 0.15); color: #16a34a; }
.badge-red { background: rgba(239, 68, 68, 0.12); color: #dc2626; }

@media (max-width: 780px) {
  .project-head { flex-direction: column; }
  .scene-grid { grid-template-columns: 1fr; }
  .viewer-mask { padding: 12px; }
  .viewer-panel { width: calc(100vw - 24px); max-height: calc(100vh - 24px); }
  .viewer-img { max-height: calc(100vh - 176px); }
  .viewer-hint { display: none; }
  .modal-lg { max-width: 100%; }
  .field-row { flex-direction: column; }
}
</style>
