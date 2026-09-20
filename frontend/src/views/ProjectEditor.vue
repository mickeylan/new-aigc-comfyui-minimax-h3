<template>
  <div class="page fade-up editor-page" v-if="project">
    <!-- 顶栏 -->
    <div class="project-head">
      <div>
        <div class="head-links">
          <router-link :to="`/projects/${id()}`" class="back">← 返回项目</router-link>
        </div>
        <h1>🎬 剪辑台 · {{ project.title }}</h1>
        <p class="synopsis">第{{ activeEpN }}集 · {{ currentEpisode?.title || '未命名' }} · 目标{{ targetDuration }}秒 / {{ targetScenes }}场 <button class="btn btn-xs btn-ghost" @click="editCurrentEpisode">编辑本集</button></p>
      </div>
      <div class="head-actions">
        <router-link class="btn btn-secondary btn-sm" :to="`/projects/${id()}/episodes/${activeEpN}/screenplay`">编辑本集剧本</router-link>
        <button class="btn btn-ghost btn-sm" :disabled="epIndex <= 0" @click="switchEp(-1)">← 上一集</button>
        <button class="btn btn-ghost btn-sm" :disabled="epIndex >= epCount - 1" @click="switchEp(1)">下一集 →</button>
        <button class="btn btn-ghost btn-sm" @click="createEpisode">新建集</button>
        <button class="btn btn-ghost btn-sm" @click="showCharacterHistory = true">角色历史</button>
        <button class="btn btn-ghost btn-sm" @click="deleteEpisode">删除空集</button>
        <label class="merge-opt"><input type="checkbox" v-model="mergeSub" />烧录字幕</label>
        <label class="merge-opt"><input type="checkbox" v-model="mergeDub" />保留原声</label>
        <label class="merge-opt">原声 <input class="sub-t" type="number" min="0" max="4" step="0.1" v-model.number="nativeVolume" /></label>
        <label class="merge-opt">对白 <input class="sub-t" type="number" min="0" max="4" step="0.1" v-model.number="dialogueVolume" /></label>
        <label class="merge-opt">BGM <input class="sub-t" type="number" min="0" max="4" step="0.1" v-model.number="bgmVolume" /></label>
        <button class="btn btn-lg" :disabled="busy || curMerging || readyVideoCount < 2" @click="mergeEpisode">
          {{ curMerging ? '合并中…' : '⚡ 合并第' + activeEpN + '集成片' }}
        </button>
      </div>
    </div>

    <section class="section">
      <div class="section-head"><div><span class="overline">CONTINUITY</span><h2>跨集连续性</h2><p class="sub">上一集摘要、末帧、角色出场与下一集钩子。</p></div><div class="section-actions"><button class="btn btn-sm btn-ghost" @click="loadEpisodeContinuity">刷新</button><button class="btn btn-sm btn-secondary" @click="regenerateContinuity">AI重生成</button><button class="btn btn-sm btn-secondary" @click="runCreativeIntent">创作意图澄清</button></div></div>
      <div class="card" v-if="continuity"><p><strong>上一集摘要：</strong><span v-if="continuity.previous_episode?.summary_stale" class="badge warn">已过期</span> {{ continuity.previous_summary || '暂无' }}</p><p><strong>结尾钩子：</strong><span v-if="continuity.previous_episode?.next_hook_stale" class="badge warn">已过期</span> {{ continuity.previous_hook || '暂无' }}</p><p><strong>本集角色：</strong>{{ (continuity.character_appearances || []).map(v => v.name).join('、') || '暂无' }}</p><div class="merge-links" v-if="continuity.last_scene_images?.length"><span v-for="f in continuity.last_scene_images" :key="f">{{ f }}</span></div><pre v-if="intentDraft" class="prompt-preview">{{ intentDraft }}</pre></div>
    </section>

    <section class="section">
      <details class="card"><summary><strong>项目 Skill 配置与审计</strong></summary>
        <p class="sub">项目配置仅在 Skill 的 operation 与具体操作契约一致时生效；否则安全回退到系统专用 Skill。</p>
        <div class="shot-grid"><label>阶段<select v-model="skillStage" class="input" @change="onSkillStageChange"><option v-for="s in skillStages" :key="s.value" :value="s.value">{{ s.label }}</option></select></label><label>操作契约<select v-model="skillOperation" class="input" @change="loadSkillPanel"><option v-for="op in stageOperations" :key="op" :value="op">{{ op }}</option></select></label><label>Skill<select v-model="selectedSkillId" class="input"><option value="">使用系统默认</option><option v-for="s in operationSkills" :key="s.id" :value="String(s.id)">{{ s.name }} · v{{ s.version }}</option></select></label></div>
        <div class="section-actions"><button class="btn btn-sm" @click="saveProjectSkill">保存项目配置</button><button class="btn btn-sm btn-ghost" @click="resetProjectSkill">恢复系统默认</button><button class="btn btn-sm btn-ghost" @click="loadSkillAudit">刷新审计</button></div>
        <p class="sub" v-if="effectiveSkill">当前有效：{{ effectiveSkill.name }}（{{ effectiveSkill.operation || effectiveSkill.code }}）</p>
        <div v-if="skillAudits.length" class="history"><div v-for="row in skillAudits" :key="row.id" class="history-item">{{ row.stage }} · {{ row.skill_code }} · {{ row.success ? '成功' : ('失败：' + (row.error || '未知错误')) }} · {{ new Date(row.created_at).toLocaleString() }}</div></div>
      </details>
    </section>

    <!-- 时间轴 -->
    <section class="section">
      <div class="section-head">
        <div>
          <span class="overline">TIMELINE</span>
          <h2>时间轴（{{ scenes.length }} 个场景）
            <span v-if="targetDuration" class="dur-progress" :class="durProgressClass">
              {{ fmtDur(accumulatedDuration) }} / {{ fmtDur(targetDuration) }} ({{ durProgressPercent }}%)
            </span>
          </h2>
          <p class="sub">点击场景选中，可拖拽调整顺序；右侧面板编辑配音与字幕</p>
        </div>
      </div>
      <div class="card timeline-card">
        <div class="timeline">
          <div v-for="(sc, i) in scenes" :key="sc.id"
            class="tl-clip" :class="{ 'tl-active': selected?.id === sc.id }"
            :draggable="true"
            @dragstart="dragFrom = i" @dragover.prevent @drop="dropScene(i)"
            @click="selectScene(sc)">
            <div class="tl-thumb">
              <video v-if="sc.video_url" :src="sc.video_url" preload="metadata" muted></video>
              <img v-else-if="sc.image_url" :src="sc.image_url" />
              <span v-else class="tl-empty">🎞️</span>
              <span class="tl-dur">{{ fmtDur(sc.video_dur || sc.duration || 5) }}</span>
            </div>
            <div class="tl-info">
              <span class="tl-n">场景 {{ sc.order }}</span>
              <span class="tl-title">{{ sc.title || '未命名' }}</span>
            </div>
            <div class="tl-tools">
              <button class="tl-btn" title="左移" :disabled="i === 0" @click.stop="moveScene(i, -1)">◀</button>
              <button class="tl-btn" title="右移" :disabled="i === scenes.length - 1" @click.stop="moveScene(i, 1)">▶</button>
            </div>
          </div>
          <div v-if="!scenes.length" class="tl-empty-hint">该集暂无场景，请先在项目页生成分镜</div>
        </div>
      </div>
    </section>

    <div class="editor-split" v-if="selected">
      <!-- 左：视频预览 -->
      <section class="section preview-section">
        <div class="section-head">
          <div>
            <span class="overline">PREVIEW</span>
            <h2>场景 {{ selected.order }} · {{ selected.title || '未命名' }}</h2>
            <p class="sub">{{ selected.content }}</p>
          </div>
          <div class="section-actions">
            <button class="btn btn-sm btn-ghost" @click="moveSceneEpisode">移动到其他集</button>
            <button class="btn btn-sm btn-secondary" @click="runFaithfulPolish">忠实润色</button>
            <details class="director-tools"><summary class="btn btn-sm btn-ghost">高级导演工具</summary><div class="director-tools-menu"><button class="btn btn-sm btn-secondary" :disabled="busy" @click="runVisualBeats">视觉节拍</button><button class="btn btn-sm btn-secondary" :disabled="busy || !selected?.image_file" @click="runFirstFramePolish">看图润色</button><button class="btn btn-sm btn-secondary" :disabled="busy" @click="runAssetContinuityReview">资产连续性审查</button><button class="btn btn-sm btn-secondary" :disabled="busy" @click="runCoverageReview">镜头覆盖审查</button></div></details>
            <button class="btn btn-sm" :disabled="busy || selected.image_locked" @click="generateSelectedImage">{{ selected.image_locked ? '分镜图已锁定' : '生成分镜图' }}</button>
            <button class="btn btn-sm btn-ghost" @click="setSceneLock('image', !selected.image_locked)">{{ selected.image_locked ? '解锁分镜图' : '锁定分镜图' }}</button>
            <button class="btn btn-sm" :disabled="busy || !selected.image_file || selected.video_locked" @click="prepareVideoPrompt">{{ selected.video_locked ? '视频已锁定' : '准备并审核视频提示词' }}</button>
            <button class="btn btn-sm btn-ghost" @click="setSceneLock('video', !selected.video_locked)">{{ selected.video_locked ? '解锁视频' : '锁定视频' }}</button>
            <a v-if="selected.video_url" class="btn btn-sm btn-ghost" :href="selected.video_url + '?download=1'">下载视频</a>
            <button class="btn btn-sm btn-secondary" @click="showFrameSelector = true">连续性设置</button>
          </div>
        </div>
        <div class="card preview-card">
          <video v-if="selected.video_url" :key="selected.video_url" :src="selected.video_url" controls preload="metadata" class="editor-video"></video>
          <img v-else-if="selected.image_url" :key="selected.image_url" :src="selected.image_url" class="editor-image" alt="当前场景分镜图" />
          <div v-else class="preview-empty">
            <span class="ph-icon">🎞️</span>
            <p>该场景尚未生成分镜图或视频</p>
          </div>
          <div v-if="polishDraft || visualBeatDraft || assetReviewDraft || coverageReviewDraft" class="workbench scene-skill-draft">
            <div v-if="polishDraft"><strong>忠实润色草稿</strong><pre class="prompt-preview">{{ polishDraft }}</pre><button class="btn btn-sm" @click="applyPolishDraft">应用到Scene起始帧提示词</button></div>
            <div v-if="visualBeatDraft"><strong>视觉节拍草稿</strong><pre class="prompt-preview">{{ visualBeatDraft }}</pre><button class="btn btn-sm" @click="applyVisualBeats">导入Shot导演层</button></div>
            <div v-if="assetReviewDraft"><strong>资产连续性审查</strong><pre class="prompt-preview">{{ assetReviewDraft }}</pre></div>
            <div v-if="coverageReviewDraft"><strong>镜头覆盖审查</strong><pre class="prompt-preview">{{ coverageReviewDraft }}</pre></div>
          </div>
          <div v-if="videoPromptDraft" class="workbench"><strong>最终H3提交提示词（保存后才能生成视频）</strong><textarea v-model="videoPromptDraft" class="textarea" rows="10"/><div class="section-actions"><button class="btn btn-sm" @click="saveReviewedVideoPrompt(false)">保存审核结果</button><button class="btn btn-sm" @click="saveReviewedVideoPrompt(true)">保存并生成视频</button></div></div>
          <div class="duration-edit">
            <label>目标时长</label>
            <input type="number" class="input input-sm" v-model.number="durationInput" min="3" max="15" step="0.5" @change="saveDuration" />
            <span class="dur-unit">秒</span>
            <button class="btn btn-sm btn-secondary" :disabled="durationSaving" @click="saveDuration">{{ durationSaving ? '保存中…' : '保存' }}</button>
          </div>
        </div>
      </section>

      <!-- 右：配音 + 字幕 -->
      <section class="section edit-section">
        <div class="section-head">
          <div>
            <span class="overline">AUDIO & SUBTITLE</span>
            <h2>配音 · 字幕</h2>
          </div>
          <div class="section-actions">
            <button class="btn btn-sm btn-ghost" :disabled="busy" @click="previewEpisodeDub">预览本集时间线</button>
            <button class="btn btn-sm btn-secondary" :disabled="busy || !staleDialogueCount" @click="dubStaleEpisode">仅生成过期配音 ({{ staleDialogueCount }})</button>
            <button class="btn btn-sm btn-secondary" :disabled="busy || sceneDubs(selected).length === 0" @click="dubScene(selected)">
              生成/重试本场景配音
            </button>
          </div>
        </div>
        <div class="card edit-card">
          <div class="tabs">
            <button class="tab" :class="{ active: tab === 'dub' }" @click="tab = 'dub'">🎙️ 配音</button>
            <button class="tab" :class="{ active: tab === 'sub' }" @click="tab = 'sub'">📝 字幕</button>
          </div>

          <!-- 配音面板 -->
          <div v-if="tab === 'dub'" class="dub-panel">
            <div v-if="!sceneDubs(selected).length" class="panel-empty">
              <p>该场景暂无对白。可在项目页剧本中补充对白后重新生成。</p>
            </div>
            <div v-for="d in sceneDubs(selected)" :key="d.id" class="dub-item" :class="'st-' + d.status">
              <div class="dub-row">
                <span class="dl-char">{{ d.character || '旁白' }}</span>
                <span class="dl-order">#{{ d.order }}</span>
                <span class="dub-state" :class="{ fail: d.status === 'failed' }">
                  {{ d.status === 'ready' ? '✅ 已合成' : d.status === 'synthesizing' ? '合成中…' : d.status === 'failed' ? '失败' : '待合成' }}
                </span>
                <span v-if="d.audio_stale" class="fail-msg" :title="d.audio_stale_reason">需更新</span>
              </div>
              <textarea v-model="d._text" class="textarea dub-text" rows="2" @blur="saveDub(d)"></textarea>
              <div class="dub-voice-row">
                <select v-model="d._voice" class="input input-sm" @change="saveDub(d)">
                  <option value="">默认（按设置/角色映射）</option>
                  <optgroup label="👩 女声">
                    <option value="Cherry">Cherry · 甜美清澈</option>
                    <option value="Serena">Serena · 温柔成熟</option>
                    <option value="Chelsie">Chelsie · 清爽知性</option>
                  </optgroup>
                  <optgroup label="👨 男声">
                    <option value="Ethan">Ethan · 沉稳磁性</option>
                  </optgroup>
                </select>
              </div>
              <div class="dub-qa-grid">
                <label>语速<input type="number" class="input input-sm" min="0.5" max="2" step="0.1" v-model.number="d.speed" @change="saveDub(d)" /></label>
                <label>音高<input type="number" class="input input-sm" min="-12" max="12" step="1" v-model.number="d.pitch" @change="saveDub(d)" /></label>
                <label>音量<input type="number" class="input input-sm" min="0" max="4" step="0.1" v-model.number="d.volume" @change="saveDub(d)" /></label>
                <label>偏移(s)<input type="number" class="input input-sm" min="-60" max="60" step="0.1" v-model.number="d.offset" @change="saveDub(d)" /></label>
                <label class="dub-emotion">情绪<input class="input input-sm" v-model="d.emotion" placeholder="如：克制、愤怒" @change="saveDub(d)" /></label>
              </div>
              <div class="dub-actions">
                <audio v-if="d.audio_file" :src="d.audio_file" controls preload="none" class="dl-audio"></audio>
                <button class="btn btn-sm btn-secondary" :disabled="busy" @click="redub(d)">🔊 重新合成</button>
                <button class="btn btn-sm btn-ghost" :disabled="busy || !d.audio_file" @click="applyDubPreview(d)">应用试听</button>
                <button class="btn btn-sm btn-ghost" :disabled="busy || !d.previous_audio_file" @click="revertDubAudio(d)">回退音频</button>
              </div>
            </div>
          </div>

          <!-- 字幕面板 -->
          <div v-else class="sub-panel">
            <div v-if="!subtitles.length" class="panel-empty"><p>该集暂无字幕（需要对白）</p></div>
            <div v-for="(s, i) in subtitles" :key="i" class="sub-item" :class="{ 'sub-active': selected && s.scene_id === selected.id }">
              <div class="sub-time">
                <input type="number" class="input input-sm sub-t" step="0.1" :value="round1(s.start)" @change="shiftSub(i, +$event.target.value - s.start)" />
                <span>→</span>
                <input type="number" class="input input-sm sub-t" step="0.1" :value="round1(s.end)" @change="shiftSub(i, +$event.target.value - s.end)" />
              </div>
              <div class="sub-body">
                <span class="dl-char">{{ s.character || '旁白' }}</span>
                <span class="dl-text">{{ s.text }}</span>
              </div>
              <span class="sub-scene">场景{{ s.scene_order }}</span>
            </div>
            <p class="sub-hint">时间轴按配音真实时长自动对齐；调整字幕时间会保存为该对白的时间偏移，并在合并时生效。</p>
          </div>
        </div>
      </section>
    </div>

    <ShotDirectorEditor v-if="selected" :project-id="id()" :scene-id="selected.id" :genre="project?.genre || ''" :tone="project?.tone || ''" :scene-title="selected.title || ''" :scene-content="selected.content || ''" @scene-changed="reloadSelectedScene" />

    <section class="section">
      <div class="section-head"><div><span class="overline">SHARED ASSETS</span><h2>共享资产引用</h2><p class="sub">创建、修改或删除项目、场景和镜头级素材引用。</p></div><button class="btn btn-sm btn-secondary" @click="editSharedAsset()">引用素材</button></div>
      <div class="card" v-if="sharedAssetReferences.length"><div v-for="refRow in sharedAssetReferences" :key="refRow.id" class="merge-item"><div class="merge-info"><strong>{{ refRow.material?.name || `素材 #${refRow.material_id}` }}</strong><span>{{ refRow.mode }} · {{ refRow.shot_id ? `镜头 #${refRow.shot_id}` : refRow.scene_id ? `场景 #${refRow.scene_id}` : '项目级' }}</span></div><div class="merge-links"><button class="btn btn-sm btn-ghost" @click="editSharedAsset(refRow)">编辑</button><button class="btn btn-sm btn-danger" @click="removeSharedAsset(refRow)">删除</button></div></div></div><div class="card empty" v-else>暂无共享资产引用。</div>
      <form v-if="sharedAssetEditing" class="card inline-form" @submit.prevent="saveSharedAsset"><label>素材<select v-model.number="sharedAssetForm.material_id" class="input" required><option value="">请选择</option><option v-for="m in materials" :key="m.id" :value="m.id">{{ m.name }} · {{ m.type }}</option></select></label><label>模式<select v-model="sharedAssetForm.mode" class="input"><option value="live">实时引用</option><option value="copy">项目副本</option></select></label><label>范围<select v-model="sharedAssetForm.scope" class="input"><option value="project">项目</option><option value="scene" :disabled="!selected">当前场景</option></select></label><div class="section-actions"><button class="btn btn-sm">保存</button><button type="button" class="btn btn-sm btn-ghost" @click="sharedAssetEditing=false">取消</button></div></form>
    </section>

    <section class="section">
      <div class="section-head"><div><span class="overline">AUDIO LAYERS</span><h2>环境声 / 音效 / BGM</h2><p class="sub">完整编辑范围、淡入淡出、循环、静音与状态。</p></div><button class="btn btn-sm btn-secondary" @click="editAudioLayer()">新增音频层</button></div>
      <div class="card" v-if="audioLayers.length"><div v-for="layer in audioLayers" :key="layer.id" class="merge-item"><div class="merge-info"><span class="badge badge-gray">{{ layer.kind }}</span><strong>{{ layer.name || layer.file }}</strong><span>{{ layer.start_time }}s → {{ layer.end_time }}s · 音量{{ layer.volume }} · 淡入/出 {{ layer.fade_in }}/{{ layer.fade_out }}s<span v-if="layer.loop"> · 循环</span><span v-if="layer.muted"> · 已静音</span></span></div><div class="merge-links"><button class="btn btn-sm btn-ghost" @click="editAudioLayer(layer)">编辑</button><button class="btn btn-sm btn-danger" @click="removeAudioLayer(layer)">删除</button></div></div></div>
      <div class="card empty" v-else>暂无音频层。</div>
      <form v-if="audioLayerEditing" class="card inline-form audio-form" @submit.prevent="saveAudioLayer"><label>类型<select v-model="audioLayerForm.kind" class="input"><option value="soundscape">环境声</option><option value="sfx">音效</option><option value="bgm">BGM</option></select></label><label>名称<input v-model="audioLayerForm.name" class="input"></label><label>文件<input v-model="audioLayerForm.file" class="input" required></label><label>场景<select v-model="audioLayerForm.scene_id" class="input"><option :value="null">整集</option><option v-for="sc in scenes" :key="sc.id" :value="sc.id">场景 {{ sc.order }} · {{ sc.title }}</option></select></label><label>开始秒<input v-model.number="audioLayerForm.start_time" type="number" min="0" step="0.1" class="input" required></label><label>结束秒<input v-model.number="audioLayerForm.end_time" type="number" min="0.1" step="0.1" class="input" required></label><label>音量<input v-model.number="audioLayerForm.volume" type="number" min="0" max="2" step="0.1" class="input"></label><label>淡入<input v-model.number="audioLayerForm.fade_in" type="number" min="0" step="0.1" class="input"></label><label>淡出<input v-model.number="audioLayerForm.fade_out" type="number" min="0" step="0.1" class="input"></label><label>状态<select v-model="audioLayerForm.status" class="input"><option value="draft">草稿</option><option value="ready">就绪</option><option value="failed">失败</option></select></label><label class="check"><input v-model="audioLayerForm.loop" type="checkbox"> 循环</label><label class="check"><input v-model="audioLayerForm.muted" type="checkbox"> 静音</label><label class="wide">说明<textarea v-model="audioLayerForm.description" class="textarea" rows="2"></textarea></label><div class="section-actions wide"><button class="btn btn-sm">保存</button><button type="button" class="btn btn-sm btn-ghost" @click="audioLayerEditing=false">取消</button></div></form>
    </section>

    <section class="section" v-if="selected">
      <div class="section-head"><div><span class="overline">TAKES</span><h2>生成候选与审核</h2><p class="sub">保留场景图片和视频历史；设为当前不会删除其他候选。</p></div><button class="btn btn-sm btn-ghost" @click="loadCandidates">刷新</button></div>
      <div class="card" v-if="candidates.length">
        <div v-for="candidate in candidates" :key="candidate.id" class="merge-item">
          <label><input type="checkbox" :value="candidate.id" v-model="compareCandidateIds" :disabled="!compareCandidateIds.includes(candidate.id) && compareCandidateIds.length >= 2" /> 比较</label>
          <img v-if="candidate.media_type === 'image'" :src="candidateUrl(candidate)" class="tl-thumb" />
          <video v-else :src="candidateUrl(candidate)" controls preload="metadata" class="merge-video"></video>
          <div class="merge-info"><span class="badge" :class="candidate.is_current ? 'badge-green' : candidate.review_status === 'rejected' ? 'badge-red' : 'badge-gray'">{{ candidate.media_type }} · {{ candidate.is_current ? '当前' : candidate.review_status }}</span><span>{{ candidate.file }}</span><span v-if="candidate.stale" class="fail-msg">已过期：{{ candidate.stale_reason }}</span></div>
          <div class="merge-links"><button class="btn btn-sm btn-secondary" :disabled="candidate.is_current || candidate.stale" @click="selectCandidate(candidate)">设为当前</button><button class="btn btn-sm btn-secondary" @click="retryCandidate(candidate)">同参数重试</button><button class="btn btn-sm btn-secondary" :disabled="candidate.stale" @click="branchCandidate(candidate)">基于候选分支</button><button class="btn btn-sm btn-ghost" :disabled="candidate.review_status === 'rejected'" @click="rejectCandidate(candidate)">拒绝</button><button class="btn btn-sm btn-danger" :disabled="candidate.is_current" @click="removeCandidate(candidate)">删除</button></div>
        </div>
        <div v-if="compareCandidates.length === 2" class="editor-split"><div v-for="c in compareCandidates" :key="c.id" class="preview-card"><img v-if="c.media_type === 'image'" :src="candidateUrl(c)" class="editor-image" /><video v-else :src="candidateUrl(c)" controls class="editor-video"></video><pre class="prompt-preview">{{ c.prompt_snapshot }}</pre></div></div>
      </div>
      <div class="card empty" v-else>当前还没有可审核候选；已有场景结果会在刷新时自动纳入。</div>
    </section>

    <CharacterHistoryDrawer :project-id="id()" :open="showCharacterHistory" @close="showCharacterHistory=false" />

    <!-- 帧选择弹窗 -->
    <div v-if="showFrameSelector && selected" class="modal-overlay" @click.self="showFrameSelector = false">
      <div class="modal-content modal-large">
        <FrameSelector
          :project-id="id()"
          :scene-id="selected.id"
          :scene-order="selected.order"
          @close="showFrameSelector = false"
          @frame-selected="onFrameSelected"
          @applied="onContinuityApplied"
        />
      </div>
    </div>

    <!-- 合并记录 -->
    <section class="section" v-if="merges.length">
      <div class="section-head">
        <div>
          <span class="overline">MERGES</span>
          <h2>合并记录</h2>
        </div>
      </div>
      <div class="card merge-list">
        <div v-for="m in merges" :key="m.id" class="merge-item">
          <div class="merge-info">
            <span class="badge" :class="mergeBadgeClass(m.status)">{{ mergeStatusText(m.status) }}</span>
            <span class="merge-time">第{{ m.episode_n || 1 }}集 · {{ (m.created_at || '').slice(0, 16).replace('T', ' ') }}</span>
            <span v-if="m.error" class="fail-msg">{{ m.error }}</span>
          </div>
          <div v-if="m.status === 'success' && m.output_file" class="merge-result">
            <video :src="outputUrl(m.output_file)" controls preload="metadata" class="merge-video"></video>
            <div class="merge-links">
              <a class="btn btn-sm btn-ghost" :href="outputUrl(m.output_file) + '?download=1'">下载成片</a>
              <a v-if="m.subtitle" class="btn btn-sm btn-ghost" :href="outputUrl(m.output_file.replace(/\.mp4$/, '.srt')) + '?download=1'">下载字幕</a>
            </div>
          </div>
        </div>
      </div>
    </section>
  </div>
</template>

<script setup>
import { ref, reactive, computed, watch, onMounted, onUnmounted } from 'vue'
import { useRoute } from 'vue-router'
import { api } from '../api'
import { useToastStore } from '../stores/toast'
import ShotDirectorEditor from '../components/ShotDirectorEditor.vue'
import FrameSelector from '../components/FrameSelector.vue'
import CharacterHistoryDrawer from '../components/CharacterHistoryDrawer.vue'
import { compatibleSkills, projectSkillConfig, skillOperationOf } from '../utils/directorWorkflow.js'
import { createDraftSafety } from '../utils/draftSafety.js'

const route = useRoute()
const toast = useToastStore()

const project = ref(null)
const episodes = ref([])
const scenes = ref([])
const dialogues = ref([])
const subtitles = ref([])
const merges = ref([])
const candidates = ref([])
const compareCandidateIds = ref([])
const audioLayers = ref([])
const audioLayerEditing = ref(false)
const audioLayerForm = reactive({})
const sharedAssetReferences = ref([])
const materials = ref([])
const sharedAssetEditing = ref(false)
const sharedAssetForm = reactive({})
const showCharacterHistory = ref(false)
const continuity = ref(null)
const intentDraft = ref('')
const visualBeatDraft = ref('')
const polishDraft = ref('')
const assetReviewDraft = ref('')
const coverageReviewDraft = ref('')
const videoPromptDraft = ref('')
const videoPromptTemplate = ref('minimax_h3_ref2v')
let videoDraftSafety
const selected = ref(null)
const tab = ref('dub')
const busy = ref(false)
const curMerging = ref(false)
const mergeSub = ref(true)
const mergeDub = ref(true)
const nativeVolume = ref(1)
const dialogueVolume = ref(1)
const bgmVolume = ref(1)
const durationSaving = ref(false)
const durationInput = ref(5)
const dragFrom = ref(null)
const activeEpN = ref(1)
const epIndex = ref(0)
const epCount = ref(1)
const showFrameSelector = ref(false)
const skillStages = ref([])
const allSkills = ref([])
const projectSkillConfigs = ref([])
const skillStage = ref('storyboard')
const skillOperation = ref('director-visual-beat-decomposition')
const selectedSkillId = ref('')
const effectiveSkill = ref(null)
const skillAudits = ref([])
const stageSkills = computed(() => allSkills.value.filter(s => s.stage === skillStage.value && s.enabled))
const stageOperations = computed(() => [...new Set(stageSkills.value.map(skillOperationOf))].filter(Boolean).sort())
const operationSkills = computed(() => compatibleSkills(allSkills.value, skillStage.value, skillOperation.value))

// 目标时长与累计时长计算
const compareCandidates = computed(() => candidates.value.filter(c => compareCandidateIds.value.includes(c.id)))
const currentEpisode = computed(() => episodes.value.find(row => row.episode?.number === activeEpN.value)?.episode || null)
const targetDuration = computed(() => currentEpisode.value?.target_duration || 180)
const targetScenes = computed(() => currentEpisode.value?.target_scenes || 25)
const accumulatedDuration = computed(() =>
  scenes.value.reduce((sum, s) => sum + (Number(s.video_dur) || Number(s.duration) || 0), 0)
)
const durProgressPercent = computed(() => {
  if (!targetDuration.value) return 0
  return Math.round((accumulatedDuration.value / targetDuration.value) * 100)
})
const staleDialogueCount = computed(() => dialogues.value.filter(d => d.audio_stale || !d.audio_file).length)
const durProgressClass = computed(() => {
  const p = durProgressPercent.value
  if (p > 105) return 'dur-over'
  if (p > 95) return 'dur-ok'
  if (p > 80) return 'dur-near'
  return 'dur-early'
})

function id() { return route.params.id }
function outputUrl(path) { return `/api/output/0/${path}` }
function fmtDur(d) {
  const s = Math.round(Number(d) || 0)
  const mm = String(Math.floor(s / 60)).padStart(2, '0')
  const ss = String(s % 60).padStart(2, '0')
  return `${mm}:${ss}`
}
function round1(v) { return Math.round(Number(v) * 10) / 10 }

function sceneDubs(sc) {
  return dialogues.value.filter(d => d.scene_id === sc.id)
}

function syncDraftTexts() {
  for (const d of dialogues.value) {
    if (d._text === undefined) d._text = d.text
    if (d._voice === undefined) d._voice = d.voice || ''
    if (!d.speed) d.speed = 1
    if (d.volume == null) d.volume = 1
    if (d.pitch == null) d.pitch = 0
    if (d.offset == null) d.offset = 0
  }
}

function epNums() {
  const set = new Set(episodes.value.map(row => row.episode?.number).filter(Boolean))
  if (!set.size) scenes.value.forEach(s => set.add(s.episode_n || 1))
  const arr = [...set].sort((a, b) => a - b)
  epCount.value = Math.max(1, arr.length)
  return arr
}

async function loadSkillPanel() {
  try {
    if (!skillStages.value.length) {
      const [stages, skills, configs] = await Promise.all([api.skillStages(), api.listSkills({ enabled: true }), api.projectSkills(id())])
      skillStages.value = stages.data || []
      allSkills.value = skills.data || []
      projectSkillConfigs.value = configs.data || []
    }
    if (!stageOperations.value.includes(skillOperation.value)) skillOperation.value = stageOperations.value[0] || skillStage.value
    const cfg = projectSkillConfig(projectSkillConfigs.value, skillStage.value, skillOperation.value)
    selectedSkillId.value = cfg?.skill_id ? String(cfg.skill_id) : ''
    const { data } = await api.effectiveSkill(id(), skillStage.value, skillOperation.value)
    effectiveSkill.value = data || null
  } catch (e) { toast.error(e.response?.data?.error || '加载项目 Skill 配置失败') }
}
async function onSkillStageChange() { skillOperation.value = ''; await loadSkillPanel() }
async function saveProjectSkill() {
  try {
    if (!selectedSkillId.value) { await api.resetProjectSkill(id(), skillStage.value, skillOperation.value) }
    else { await api.setProjectSkill(id(), { stage: skillStage.value, operation: skillOperation.value, skill_id: Number(selectedSkillId.value), enabled: true }) }
    projectSkillConfigs.value = (await api.projectSkills(id())).data || []
    await loadSkillPanel(); toast.success('项目 Skill 配置已保存')
  } catch (e) { toast.error(e.response?.data?.error || '保存项目 Skill 配置失败') }
}
async function resetProjectSkill() {
  try { await api.resetProjectSkill(id(), skillStage.value, skillOperation.value); projectSkillConfigs.value = (await api.projectSkills(id())).data || []; await loadSkillPanel(); toast.success('已恢复系统默认 Skill') }
  catch (e) { toast.error(e.response?.data?.error || '重置 Skill 失败') }
}
async function loadSkillAudit() {
  try { skillAudits.value = (await api.skillAuditLogs(id(), { stage: skillStage.value, limit: 20 })).data || [] }
  catch (e) { toast.error(e.response?.data?.error || '加载 Skill 审计失败') }
}

async function load() {
  try {
    const [{ data }, episodeRes] = await Promise.all([api.editorData(id(), activeEpN.value), api.projectEpisodes(id())])
    project.value = data.project
    episodes.value = episodeRes.data || []
    scenes.value = data.scenes || []
    dialogues.value = (data.dialogues || []).map(d => ({ ...d }))
    subtitles.value = data.subtitles || []
    syncDraftTexts()
    const selectedId = selected.value?.id
    selected.value = (selectedId && scenes.value.find(s => s.id === selectedId)) || scenes.value.find(s => s.status === 'video_ready') || scenes.value[0] || null
    if (selected.value) { durationInput.value = selected.value.duration || 5; await loadCandidates() } else candidates.value = []
    const episodeNumbers = epNums()
    epIndex.value = Math.max(0, episodeNumbers.indexOf(activeEpN.value))
    await Promise.all([loadMerges(), loadAudioLayers(), loadSharedAssets(), loadEpisodeContinuity(), loadSkillPanel()])
  } catch (e) {
    toast.error(e.response?.data?.error || '加载剪辑台失败')
  }
}

async function loadMerges() {
  try {
    const { data } = await api.merges(id())
    merges.value = data
  } catch { /* ignore */ }
}

function clearSceneDrafts() { videoDraftSafety?.dispose(); videoDraftSafety = null; visualBeatDraft.value = ''; polishDraft.value = ''; assetReviewDraft.value = ''; coverageReviewDraft.value = ''; videoPromptDraft.value = '' }
function startVideoDraftSafety() { videoDraftSafety?.dispose(); videoDraftSafety = createDraftSafety({ key: `draft:h3-prompt:${id()}:${selected.value.id}`, getDraft: () => ({ prompt: videoPromptDraft.value, template: videoPromptTemplate.value }), applyDraft: d => { videoPromptDraft.value = d.prompt || ''; videoPromptTemplate.value = d.template || 'minimax_h3_ref2v' }, save: () => saveReviewedVideoPrompt(false) }); videoDraftSafety.start() }
function selectScene(sc) {
  selected.value = sc
  clearSceneDrafts()
  durationInput.value = sc.duration || 5
  tab.value = 'dub'
}

function moveScene(i, dir) {
  const j = i + dir
  if (j < 0 || j >= scenes.value.length) return
  const arr = scenes.value.map(s => s.id)
  ;[arr[i], arr[j]] = [arr[j], arr[i]]
  const reordered = scenes.value.slice()
  ;[reordered[i], reordered[j]] = [reordered[j], reordered[i]]
  scenes.value = reordered
  persistOrder(arr)
}

function dropScene(j) {
  if (dragFrom.value === null || dragFrom.value === j) { dragFrom.value = null; return }
  const i = dragFrom.value
  dragFrom.value = null
  const arr = scenes.value.map(s => s.id)
  const [moved] = arr.splice(i, 1)
  arr.splice(j, 0, moved)
  const reordered = scenes.value.slice()
  const [it] = reordered.splice(i, 1)
  reordered.splice(j, 0, it)
  scenes.value = reordered
  persistOrder(arr)
}

async function persistOrder(ids) {
  try {
    await api.reorderScenes(id(), { scene_ids: ids })
    toast.show('场景顺序已保存')
    await load()
  } catch (e) {
    toast.error(e.response?.data?.error || '保存顺序失败')
  }
}

async function saveDuration() {
  if (!selected.value) return
  durationSaving.value = true
  try {
    const d = Number(durationInput.value)
    await api.updateSceneDuration(id(), selected.value.id, { duration: d })
    selected.value.duration = d
    toast.show('场景时长已更新')
  } catch (e) {
    toast.error(e.response?.data?.error || '保存时长失败')
  } finally {
    durationSaving.value = false
  }
}

async function saveDub(d) {
  const payload = {
    text: (d._text || '').trim(), voice: (d._voice || '').trim(),
    speed: Number(d.speed), pitch: Number(d.pitch), volume: Number(d.volume),
    emotion: (d.emotion || '').trim(), offset: Number(d.offset)
  }
  try {
    const { data } = await api.updateDialogue(id(), d.id, payload)
    Object.assign(d, data, { _text: data.text, _voice: data.voice || '' })
    toast.show('对白与试听参数已保存')
  } catch (e) {
    toast.error(e.response?.data?.error || '保存对白失败')
  }
}

async function previewEpisodeDub() {
  busy.value = true
  try {
    const { data } = await api.episodeDubPreview(id(), activeEpN.value)
    const lines = (data.timeline || []).slice(0, 20).map(row => `${Number(row.start).toFixed(1)}–${Number(row.end).toFixed(1)}s ${row.character || '旁白'}：${row.text}`)
    window.alert(`第${activeEpN.value}集配音时间线（${(data.dialogues || []).length}条）\n\n${lines.join('\n') || '暂无对白'}`)
  } catch (e) { toast.error(e.response?.data?.error || '配音预览失败') }
  finally { busy.value = false }
}
async function dubStaleEpisode() {
  busy.value = true
  try {
    const { data } = await api.generateEpisodeDub(id(), activeEpN.value, true)
    toast.show(data.message || `已提交 ${data.count || 0} 条过期配音`)
    setTimeout(load, 3000)
  } catch (e) { toast.error(e.response?.data?.error || '过期配音生成失败') }
  finally { busy.value = false }
}
async function applyDubPreview(d) {
  try { const { data } = await api.applyDialoguePreview(id(), d.id); Object.assign(d, data); toast.success('试听版本已应用') }
  catch (e) { toast.error(e.response?.data?.error || '应用试听失败') }
}
async function revertDubAudio(d) {
  if (!window.confirm('回退到上一版音频？')) return
  try { const { data } = await api.revertDialogueAudio(id(), d.id); Object.assign(d, data); toast.success('已回退上一版音频') }
  catch (e) { toast.error(e.response?.data?.error || '回退失败') }
}

async function redub(d) {
  busy.value = true
  try {
    await api.redubDialogue(id(), d.id)
    d.status = 'synthesizing'
    toast.show('配音重新合成已提交')
    setTimeout(load, 3000)
  } catch (e) {
    toast.error(e.response?.data?.error || '合成失败')
  } finally {
    busy.value = false
  }
}

async function dubScene(sc) {
  busy.value = true
  try {
    const { data } = await api.generateSceneDub(id(), sc.id)
    toast.show(data.message || '配音已提交')
    setTimeout(load, 3000)
  } catch (e) {
    toast.error(e.response?.data?.error || '配音失败')
  } finally {
    busy.value = false
  }
}

async function shiftSub(i, delta) {
  const s = subtitles.value[i]
  const dialogue = dialogues.value.find(d => d.id === s.dialogue_id)
  if (!dialogue || !Number.isFinite(delta)) return
  const offset = round1((Number(dialogue.offset) || 0) + delta)
  try {
    const { data } = await api.updateDialogue(id(), dialogue.id, { offset })
    Object.assign(dialogue, data, { _text: data.text, _voice: data.voice || '' })
    await load()
    toast.show('字幕时间偏移已保存')
  } catch (e) {
    toast.error(e.response?.data?.error || '保存字幕时间失败')
    await load()
  }
}

const readyVideoCount = computed(() => scenes.value.filter(s => s.status === 'video_ready').length)

async function mergeEpisode() {
  const ids = scenes.value.filter(s => s.status === 'video_ready').map(s => s.id)
  if (ids.length < 2) {
    toast.show('该集至少需要 2 个视频就绪的场景')
    return
  }
  curMerging.value = true
  try {
    await api.mergeAudioScenes(id(), { scene_ids: ids, dub: mergeDub.value, subtitles: mergeSub.value, native_volume: nativeVolume.value, dialogue_volume: dialogueVolume.value, bgm_volume: bgmVolume.value })
    toast.show(`第${activeEpN.value}集合并已启动（配音+字幕）`)
    setTimeout(loadMerges, 3000)
  } catch (e) {
    toast.error(e.response?.data?.error || '合并失败')
  } finally {
    curMerging.value = false
  }
}

async function loadEpisodeContinuity() {
  try { const { data } = await api.episodeContinuity(id(), activeEpN.value); continuity.value = data }
  catch { continuity.value = null }
}
async function regenerateContinuity() {
  try { await api.regenerateEpisodeContinuity(id(), activeEpN.value); toast.success('连续性摘要已更新'); await loadEpisodeContinuity() }
  catch (e) { toast.error(e.response?.data?.error || '生成失败') }
}
async function createEpisode() {
  const numbers = epNums(); const number = numbers.length ? Math.max(...numbers) + 1 : 1
  const title = window.prompt('新集标题', `第${number}集`); if (!title) return
  try { await api.createProjectEpisode(id(), { number, title, target_duration: 180, target_scenes: 25 }); toast.success('新集已创建'); await load() }
  catch (e) { toast.error(e.response?.data?.error || '创建失败') }
}
async function deleteEpisode() {
  if (!currentEpisode.value || !window.confirm(`删除空的第${activeEpN.value}集？`)) return
  try { await api.deleteProjectEpisode(id(), activeEpN.value); activeEpN.value = 1; selected.value = null; await load() }
  catch (e) { toast.error(e.response?.data?.error || '只能删除不含场景的集') }
}
async function moveSceneEpisode() {
  if (!selected.value) return
  const number = Number(window.prompt('目标集数', String(activeEpN.value))); if (!number || number === activeEpN.value) return
  try { await api.reassignSceneEpisode(id(), selected.value.id, number); toast.success('场景已移动'); selected.value = null; await load() }
  catch (e) { toast.error(e.response?.data?.error || '移动失败') }
}
async function runVisualBeats() {
  try { const { data } = await api.visualBeatDraft(id(), selected.value.id, { target_duration: selected.value.duration }); visualBeatDraft.value = data.draft; toast.success('已生成视觉节拍草稿（未自动保存）') }
  catch (e) { toast.error(e.response?.data?.error || '生成失败') }
}
async function runFirstFramePolish() {
  busy.value = true
  try {
    const { data: capability } = await api.textProviderCapabilities()
    if (!capability.first_frame_grounded_polish) { toast.error('当前文本模型不支持图片输入，未执行伪多模态润色'); return }
    const { data } = await api.firstFramePolish({ prompt: selected.value.image_prompt || selected.value.content || '', image_path: selected.value.image_file })
    polishDraft.value = data.prompt || ''
    toast.success('已依据实际首帧生成润色草稿（未自动保存）')
  } catch (e) { toast.error(e.response?.data?.error || '看图润色失败') }
  finally { busy.value = false }
}
async function runAssetContinuityReview() {
  try { const { data } = await api.assetContinuityReviewDraft(id(), selected.value.id); assetReviewDraft.value = data.draft; toast.success('已生成资产连续性审查草稿（未自动保存）') }
  catch (e) { toast.error(e.response?.data?.error || '审查失败') }
}
async function runCoverageReview() {
  try { const { data } = await api.coverageReviewDraft(id(), selected.value.id); coverageReviewDraft.value = data.draft; toast.success('已生成镜头覆盖审查草稿（未自动保存）') }
  catch (e) { toast.error(e.response?.data?.error || '审查失败') }
}
async function runFaithfulPolish() {
  const feedback = window.prompt('只描述希望改进的视觉表现', '增强构图和动作可见性'); if (feedback === null) return
  try { const { data } = await api.faithfulPolishDraft(id(), selected.value.id, { original_prompt: selected.value.image_prompt || '', immutable_facts: selected.value.content || '', editable_presentation: '构图、光线、材质、摄影表达', feedback }); polishDraft.value = data.draft; toast.success('已生成忠实润色草稿（未自动保存）') }
  catch (e) { toast.error(e.response?.data?.error || '润色失败') }
}
function parseSkillJSON(text) { const clean = String(text || '').trim().replace(/^```json\s*/i, '').replace(/```$/, '').trim(); return JSON.parse(clean) }
async function applyVisualBeats() {
  try {
    const parsed = parseSkillJSON(visualBeatDraft.value); const rows = Array.isArray(parsed) ? parsed : parsed.shots
    if (!Array.isArray(rows) || !rows.length) throw new Error('草稿中没有shots数组')
    const shots = rows.map((s, i) => ({ act_type: ['setup','rising','midpoint','falling','resolution'][Math.min(4, Math.floor(i * 5 / rows.length))], shot_type: s.shot_type || '中景', camera_angle: s.camera_angle || '平视', camera_movement: s.camera_movement || '固定', duration: Math.max(0.1, Number(s.duration) || 2), description: s.description || '', dialogue: Array.isArray(s.dialogues) ? s.dialogues.map(d => d.text || d).join('\n') : (s.dialogue || ''), emotion: s.emotion || '', prompt_subject: '', prompt_action: '', prompt_camera: '', prompt_lighting: '', prompt_style: '', negative_prompt: '' }))
    if (!window.confirm(`将以${shots.length}个镜头替换当前Shot导演设计。Scene剧情正文和起始帧提示词不会被覆盖，是否继续？`)) return
    await api.replaceSceneShots(id(), selected.value.id, shots); visualBeatDraft.value = ''; await reloadSelectedScene(); toast.success('视觉节拍已导入Shot导演层')
  } catch (e) { toast.error(e.response?.data?.error || e.message || '视觉节拍草稿格式无效') }
}
async function applyPolishDraft() { try { await api.updateScene(id(), selected.value.id, { image_prompt: polishDraft.value }); polishDraft.value = ''; await reloadSelectedScene(); toast.success('润色草稿已应用到Scene起始帧提示词') } catch (e) { toast.error(e.response?.data?.error || '应用失败') } }
async function generateSelectedImage() { busy.value = true; try { await api.generateSceneImage(id(), selected.value.id); toast.success('分镜图任务已提交，已组合Scene事实、Shot导演设计、参考图与负向约束'); await reloadSelectedScene() } catch (e) { toast.error(e.response?.data?.error || '提交分镜图失败') } finally { busy.value = false } }
async function prepareVideoPrompt() { busy.value = true; try { const { data } = await api.regenerateSceneVideoPrompt(id(), selected.value.id, {}); videoPromptDraft.value = data.full_prompt || data.prompt || ''; videoPromptTemplate.value = data.template || 'minimax_h3_ref2v'; startVideoDraftSafety(); toast.success('完整H3提示词已生成，请审核后保存') } catch (e) { toast.error(e.response?.data?.error || '生成视频提示词失败') } finally { busy.value = false } }
async function saveReviewedVideoPrompt(generate) { busy.value = true; try { await api.updateSceneVideoPrompt(id(), selected.value.id, { prompt: videoPromptDraft.value, template: videoPromptTemplate.value }); if (generate) await api.generateSceneVideo(id(), selected.value.id); videoDraftSafety?.markSaved(); videoPromptDraft.value = ''; await reloadSelectedScene(); toast.success(generate ? '已保存审核提示词并提交视频' : '视频提示词审核结果已保存') } catch (e) { toast.error(e.response?.data?.error || '保存视频提示词失败'); throw e } finally { busy.value = false } }
async function setSceneLock(kind, locked) { try { const { data } = await api.updateSceneLocks(id(), selected.value.id, { [`${kind}_locked`]: locked }); Object.assign(selected.value, data); toast.success(locked ? '生成结果已锁定' : '生成结果已解锁') } catch (e) { toast.error(e.response?.data?.error || '更新锁定状态失败') } }
async function reloadSelectedScene() { const sid = selected.value?.id; await load(); if (sid) selected.value = scenes.value.find(s => s.id === sid) || selected.value }
async function runCreativeIntent() {
  const goal = window.prompt('你希望澄清的创作目标', project.value?.synopsis || ''); if (!goal) return
  try {
    const questions = await api.creativeIntent(id(), { action: 'questions', user_goal: goal, answers: '' })
    const answers = window.prompt(`请回答以下问题（可合并回答）：\n${questions.data.draft}`, '')
    if (answers === null) { intentDraft.value = questions.data.draft; return }
    const brief = await api.creativeIntent(id(), { action: 'brief', user_goal: goal, answers })
    intentDraft.value = brief.data.draft; toast.success('Intent Brief已生成（不会自动执行）')
  } catch (e) { toast.error(e.response?.data?.error || '意图澄清失败') }
}

async function loadSharedAssets() {
  try {
    const [refs, mats] = await Promise.all([api.sharedAssetReferences(id()), api.materials({ project_id: id() })])
    sharedAssetReferences.value = refs.data || []
    materials.value = mats.data || []
  } catch { sharedAssetReferences.value = [] }
}
function editSharedAsset(row = null) {
  Object.assign(sharedAssetForm, { id: row?.id || 0, material_id: row?.material_id || '', mode: row?.mode || 'live', scope: row?.scene_id ? 'scene' : 'project' })
  sharedAssetEditing.value = true
}
async function saveSharedAsset() {
  const payload = { material_id: Number(sharedAssetForm.material_id), mode: sharedAssetForm.mode, scene_id: sharedAssetForm.scope === 'scene' ? selected.value?.id : null, shot_id: null }
  try { sharedAssetForm.id ? await api.updateSharedAssetReference(id(), sharedAssetForm.id, payload) : await api.createSharedAssetReference(id(), payload); sharedAssetEditing.value = false; toast.success('共享资产引用已保存'); await loadSharedAssets() }
  catch (e) { toast.error(e.response?.data?.error || '保存引用失败') }
}
async function removeSharedAsset(row) {
  if (!window.confirm(`删除共享资产引用“${row.material?.name || row.material_id}”？`)) return
  try { await api.deleteSharedAssetReference(id(), row.id); await loadSharedAssets() } catch (e) { toast.error(e.response?.data?.error || '删除引用失败') }
}

async function loadAudioLayers() {
  try { const { data } = await api.audioLayers(id(), activeEpN.value); audioLayers.value = data || [] }
  catch (e) { toast.error(e.response?.data?.error || '加载音频层失败') }
}
function editAudioLayer(layer = null) {
  Object.assign(audioLayerForm, { id: layer?.id || 0, episode_n: activeEpN.value, scene_id: layer?.scene_id || null, kind: layer?.kind || 'bgm', name: layer?.name || '', file: layer?.file || '', description: layer?.description || '', start_time: layer?.start_time ?? 0, end_time: layer?.end_time ?? targetDuration.value, volume: layer?.volume ?? 1, fade_in: layer?.fade_in ?? 0, fade_out: layer?.fade_out ?? 0, loop: !!layer?.loop, muted: !!layer?.muted, status: layer?.status || 'ready' })
  audioLayerEditing.value = true
}
async function saveAudioLayer() {
  const { id: layerId, ...payload } = audioLayerForm
  try { layerId ? await api.updateAudioLayer(id(), layerId, payload) : await api.createAudioLayer(id(), payload); audioLayerEditing.value = false; toast.success('音频层已保存'); await loadAudioLayers() }
  catch (e) { toast.error(e.response?.data?.error || '保存音频层失败') }
}
async function removeAudioLayer(layer) {
  if (!window.confirm(`删除音频层“${layer.name || layer.file}”？`)) return
  try { await api.deleteAudioLayer(id(), layer.id); await loadAudioLayers() } catch (e) { toast.error(e.response?.data?.error || '删除失败') }
}

async function loadCandidates() {
  if (!selected.value) { candidates.value = []; return }
  try { const { data } = await api.sceneCandidates(id(), selected.value.id); candidates.value = data || [] }
  catch (e) { toast.error(e.response?.data?.error || '加载候选失败') }
}
function candidateUrl(candidate) {
  if (candidate.video_input_file) return `/api/input/${id()}/${candidate.video_input_file}`
  if (candidate.media_type === 'image') return `/api/input/${id()}/${String(candidate.file).split(/[\\/]/).pop()}`
  return `/api/output/${candidate.video_gpu ?? 0}/${candidate.file}`
}
async function retryCandidate(candidate) {
  try { await api.retryCandidate(id(), candidate.id); toast.success('已使用候选快照提交重试') }
  catch (e) { toast.error(e.response?.data?.error || '候选不可重试') }
}
async function branchCandidate(candidate) {
  try { await api.branchCandidate(id(), candidate.id); toast.success('已从该候选创建生成分支'); setTimeout(loadCandidates, 1500) }
  catch (e) { toast.error(e.response?.data?.error || '创建分支失败') }
}
async function selectCandidate(candidate) {
  try { await api.selectCandidate(id(), candidate.id); toast.success('已设为当前候选'); await load() }
  catch (e) { toast.error(e.response?.data?.error || '选择候选失败') }
}
async function removeCandidate(candidate) {
  if (!window.confirm(`永久删除候选“${candidate.file}”？文件也可能被清理。`)) return
  try { await api.deleteCandidate(id(), candidate.id); compareCandidateIds.value = compareCandidateIds.value.filter(v => v !== candidate.id); toast.success('候选已删除'); await loadCandidates() }
  catch (e) { toast.error(e.response?.data?.error || '候选无法删除') }
}
async function rejectCandidate(candidate) {
  const reason = window.prompt('请输入拒绝原因', candidate.review_reason || '')
  if (reason === null) return
  try { await api.reviewCandidate(id(), candidate.id, 'rejected', reason.trim()); toast.success('候选已拒绝'); await loadCandidates() }
  catch (e) { toast.error(e.response?.data?.error || '审核候选失败') }
}

async function editCurrentEpisode() {
  const episode = currentEpisode.value
  if (!episode) return
  const title = window.prompt('本集标题', episode.title || '')
  if (title === null) return
  const duration = Number(window.prompt('目标时长（秒）', String(episode.target_duration || 180)))
  const sceneCount = Number(window.prompt('目标场景数', String(episode.target_scenes || 25)))
  if (!title.trim() || duration <= 0 || sceneCount < 1) { toast.error('标题、目标时长或场景数无效'); return }
  try {
    await api.updateProjectEpisode(id(), episode.number, { title: title.trim(), target_duration: duration, target_scenes: Math.round(sceneCount) })
    toast.success('本集信息已保存')
    await load()
  } catch (e) { toast.error(e.response?.data?.error || '保存本集信息失败') }
}

function switchEp(dir) {
  const arr = epNums()
  epIndex.value = Math.max(0, Math.min(arr.length - 1, epIndex.value + dir))
  activeEpN.value = arr[epIndex.value] || 1
  selected.value = null
  load()
}

// 帧选择回调
function onFrameSelected(frame) {
  toast.show(`已选择衔接帧 #${frame.frame_index}`)
}

function onContinuityApplied(result) {
  toast.show('连续性配置已应用到视频生成参数')
  showFrameSelector.value = false
  load() // 刷新场景数据
}

function mergeStatusText(s) { return { pending: '等待中', running: '合并中', success: '成片完成', failed: '失败' }[s] || s }
function mergeBadgeClass(s) { return { pending: 'badge-gray', running: 'badge-orange', success: 'badge-green', failed: 'badge-red' }[s] || 'badge-gray' }

let timer = null
onMounted(() => {
  load()
  timer = setInterval(load, 8000)
})
onUnmounted(() => { clearInterval(timer); videoDraftSafety?.dispose() })
watch(videoPromptDraft, () => videoDraftSafety?.schedule())
</script>

<style scoped>
.editor-page { max-width: 1280px; }
.editor-split { display: grid; grid-template-columns: 1.1fr 1fr; gap: 20px; align-items: start; }
@media (max-width: 980px) { .editor-split { grid-template-columns: 1fr; } }

.timeline-card { padding: 16px; overflow-x: auto; }
.timeline { display: flex; gap: 12px; min-width: max-content; }
.tl-clip {
  width: 168px; flex: 0 0 auto; border-radius: 12px; border: 2px solid var(--border);
  overflow: hidden; cursor: pointer; background: var(--card); transition: all 0.2s; position: relative;
}
.tl-clip:hover { border-color: var(--accent); }
.tl-clip.tl-active { border-color: var(--accent); box-shadow: 0 0 0 3px var(--accent-soft); }
.tl-thumb { position: relative; aspect-ratio: 16/9; background: #000; }
.tl-thumb video, .tl-thumb img { width: 100%; height: 100%; object-fit: cover; display: block; }
.tl-empty { position: absolute; inset: 0; display: flex; align-items: center; justify-content: center; font-size: 26px; }
.tl-dur {
  position: absolute; right: 6px; bottom: 6px; font-size: 11px; font-weight: 700; color: #fff;
  background: rgba(0, 0, 0, 0.65); padding: 2px 6px; border-radius: 6px;
}
.tl-info { padding: 8px 10px; display: flex; flex-direction: column; gap: 2px; }
.tl-n { font-size: 11px; font-weight: 700; color: var(--accent); }
.tl-title { font-size: 12px; color: var(--text-secondary); white-space: nowrap; overflow: hidden; text-overflow: ellipsis; }
.tl-tools { position: absolute; top: 6px; left: 6px; display: flex; gap: 4px; }
.tl-btn {
  width: 22px; height: 22px; border-radius: 6px; border: none; cursor: pointer; font-size: 10px;
  background: rgba(0, 0, 0, 0.6); color: #fff;
}
.tl-btn:disabled { opacity: 0.35; cursor: default; }
.tl-empty-hint { padding: 40px; color: var(--text-tertiary); }

.dur-progress { font-size: 14px; font-weight: 600; margin-left: 8px; }
.dur-progress.dur-early { color: var(--text-secondary); }
.dur-progress.dur-near { color: #f59e0b; }
.dur-progress.dur-ok { color: #22c55e; }
.dur-progress.dur-over { color: #ef4444; }

.director-tools { position: relative; }
.director-tools > summary { list-style: none; cursor: pointer; }
.director-tools > summary::-webkit-details-marker { display: none; }
.director-tools-menu { position: absolute; right: 0; top: calc(100% + 6px); z-index: 20; min-width: 180px; display: grid; gap: 6px; padding: 8px; border: 1px solid var(--border); border-radius: 10px; background: var(--card); box-shadow: 0 8px 24px rgba(0,0,0,.18); }
.preview-card { padding: 16px; }
.editor-video { width: 100%; border-radius: 12px; background: #000; max-height: 420px; }
.editor-image { display: block; width: 100%; max-height: 520px; object-fit: contain; border-radius: 12px; background: #000; }
.preview-empty { height: 220px; display: flex; flex-direction: column; align-items: center; justify-content: center; gap: 8px; color: var(--text-tertiary); }
.duration-edit { display: flex; align-items: center; gap: 8px; margin-top: 14px; font-size: 13px; color: var(--text-secondary); }
.dur-unit { color: var(--text-tertiary); }

.edit-card { padding: 0; }
.tabs { display: flex; border-bottom: 1px solid var(--border); }
.tab {
  flex: 1; padding: 12px; border: none; background: transparent; cursor: pointer;
  font-size: 14px; font-weight: 600; color: var(--text-secondary); border-bottom: 2px solid transparent;
}
.tab.active { color: var(--accent); border-bottom-color: var(--accent); }
.dub-panel, .sub-panel { padding: 14px; max-height: 520px; overflow-y: auto; }
.dub-item { border: 1px solid var(--border); border-radius: 12px; padding: 10px; margin-bottom: 10px; }
.dub-row { display: flex; align-items: center; gap: 8px; margin-bottom: 8px; }
.dub-state { font-size: 11px; padding: 2px 8px; border-radius: 980px; background: var(--accent-soft); color: var(--accent); }
.dub-state.fail { background: rgba(220, 38, 38, 0.1); color: var(--red); }
.dub-text { font-size: 13px; }
.dub-voice-row { margin: 8px 0; }
.dub-qa-grid { display: grid; grid-template-columns: repeat(4, minmax(0, 1fr)); gap: 6px; margin: 8px 0; }
.dub-qa-grid label { color: var(--text-tertiary); font-size: 10px; }
.dub-qa-grid .input { width: 100%; margin-top: 3px; }
.dub-qa-grid .dub-emotion { grid-column: span 4; }
.dub-actions { display: flex; align-items: center; gap: 8px; flex-wrap: wrap; }
.dub-actions .dl-audio { flex: 1; min-width: 200px; }

.sub-item {
  display: flex; align-items: center; gap: 10px; padding: 8px 10px; border-radius: 10px;
  border: 1px solid var(--border); margin-bottom: 8px; font-size: 13px;
}
.sub-item.sub-active { border-color: var(--accent); background: var(--accent-soft); }
.sub-time { display: flex; align-items: center; gap: 4px; flex: 0 0 auto; }
.sub-t { width: 64px; }
.sub-body { flex: 1; min-width: 0; display: flex; gap: 8px; align-items: center; }
.sub-scene { font-size: 11px; color: var(--text-tertiary); flex: 0 0 auto; }
.sub-hint { font-size: 12px; color: var(--text-tertiary); margin-top: 8px; }
.panel-empty { padding: 30px; text-align: center; color: var(--text-tertiary); }
.merge-opt { display: inline-flex; align-items: center; gap: 4px; font-size: 13px; color: var(--text-secondary); cursor: pointer; white-space: nowrap; }
.merge-opt input { width: 14px; height: 14px; cursor: pointer; margin: 0; }

/* 弹窗样式 */
.modal-overlay {
  position: fixed;
  inset: 0;
  background: rgba(0, 0, 0, 0.7);
  display: flex;
  align-items: center;
  justify-content: center;
  z-index: 1000;
}

.modal-content {
  background: var(--card);
  border-radius: 12px;
  max-height: 90vh;
  overflow-y: auto;
  box-shadow: 0 8px 32px rgba(0, 0, 0, 0.4);
}

.modal-large {
  width: 90vw;
  max-width: 1000px;
}
.inline-form { margin-top: 12px; padding: 16px; display: grid; grid-template-columns: repeat(3, minmax(0, 1fr)); gap: 12px; }
.inline-form label { display: grid; gap: 5px; color: var(--text-secondary); font-size: 12px; }
.audio-form { grid-template-columns: repeat(4, minmax(0, 1fr)); }
.audio-form .wide { grid-column: 1 / -1; }
.audio-form .check { display: flex; align-items: center; }
@media (max-width: 760px) { .inline-form, .audio-form { grid-template-columns: 1fr 1fr; } }
</style>
