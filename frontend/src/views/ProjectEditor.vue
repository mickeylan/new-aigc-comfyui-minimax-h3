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
        <router-link class="btn btn-secondary btn-sm" :to="`/projects/${id()}/episodes/${activeEpN}/voice`">🎙️ 声音工作台</router-link>
        <button class="btn btn-ghost btn-sm" :disabled="epIndex <= 0" @click="switchEp(-1)">← 上一集</button>
        <button class="btn btn-ghost btn-sm" :disabled="epIndex >= epCount - 1" @click="switchEp(1)">下一集 →</button>
        <button class="btn btn-ghost btn-sm" @click="createEpisode">新建集</button>
        <button class="btn btn-ghost btn-sm" @click="showCharacterHistory = true">角色历史</button><button class="btn btn-ghost btn-sm" @click="showGeneratedHistory = true">生成历史管理</button>
        <button class="btn btn-ghost btn-sm" @click="deleteEpisode">删除空集</button>
        <label v-show="workspaceTab==='delivery'" class="merge-opt" title="默认关闭；仅在需要逐句显示Dialogue时开启"><input type="checkbox" v-model="mergeSub" />烧录Dialogue对白字幕（默认关闭）</label>
        <label v-show="workspaceTab==='delivery'" class="merge-opt" title="只烧录已审核的功能文字，不会自动开启对白字幕"><input type="checkbox" v-model="mergeScreenText" />烧录功能字幕/屏幕文字</label>
        <label v-show="workspaceTab==='delivery'" class="merge-opt"><input type="checkbox" v-model="mergeDub" />保留原声</label>
        <label v-show="workspaceTab==='delivery'" class="merge-opt merge-volume">原声 <input type="number" min="0" max="4" step="0.1" v-model.number="nativeVolume" aria-label="原声音量" /></label>
        <label v-show="workspaceTab==='delivery'" class="merge-opt merge-volume">对白 <input type="number" min="0" max="4" step="0.1" v-model.number="dialogueVolume" aria-label="对白音量" /></label>
        <label v-show="workspaceTab==='delivery'" class="merge-opt merge-volume">BGM <input type="number" min="0" max="4" step="0.1" v-model.number="bgmVolume" aria-label="BGM音量" /></label>
        <button v-show="workspaceTab==='delivery'" class="btn btn-lg" :disabled="busy || curMerging || mergeSequence.length < 2" @click="mergeEpisode">
          {{ curMerging ? '合并中…' : '⚡ 合并第' + activeEpN + '集成片' }}
        </button>
      </div>
    </div>

    <nav class="workspace-tabs" aria-label="剪辑台工作区">
      <button v-for="item in workspaceTabs" :key="item.key" type="button" class="workspace-tab" :class="{active:workspaceTab===item.key}" @click="workspaceTab=item.key"><strong>{{item.label}}</strong><small>{{item.description}}</small></button>
    </nav>
    <div class="workspace-context"><span>第{{activeEpN}}集</span><span v-if="selected">当前 Scene {{selected.order}} · {{selected.title||'未命名'}}</span><span v-else>尚未选择 Scene</span></div>

    <section v-show="workspaceTab==='delivery'" class="section">
      <div class="section-head"><div><span class="overline">FINAL CUT ASSEMBLY</span><h2>最终成片编排</h2><p class="sub">直接观看每个片段后决定保留、排除、排序或插入；只影响本次合并，不修改Scene、候选或原视频。</p></div><div class="section-actions"><input ref="mergeVideoUploadInput" type="file" accept="video/mp4,video/webm,video/quicktime,video/x-matroska" hidden @change="uploadMergeVideo"><button class="btn btn-sm btn-secondary" :disabled="mergeVideoUploading" @click="requestMergeVideoUpload(mergeSequence.length,0)">{{mergeVideoUploading?'上传中…':'上传视频并添加到末尾'}}</button><button class="btn btn-sm btn-ghost" @click="resetMergeSequence">按Scene重置</button></div></div>
      <div class="final-cut-grid">
        <article v-for="(clip,index) in mergeSequence" :key="clip.id" class="card final-cut-clip">
          <div class="final-cut-preview"><video v-if="clip.video_url" :src="clip.video_url" controls muted preload="metadata" @loadedmetadata="clip.duration=$event.target.duration"></video><div v-else class="preview-empty">该片段暂无可预览视频</div><span class="final-cut-index">{{index+1}}</span></div>
          <div class="merge-info"><strong>{{clip.label}}</strong><span>{{clip.scene_id?'Scene片段':'插入视频素材'}}<template v-if="clip.duration"> · {{Number(clip.duration).toFixed(1)}}秒</template></span></div>
          <div class="final-cut-actions"><button class="btn btn-xs btn-ghost" :disabled="index===0" @click="moveMergeClip(index,-1)">上移</button><button class="btn btn-xs btn-ghost" :disabled="index===mergeSequence.length-1" @click="moveMergeClip(index,1)">下移</button><button class="btn btn-xs btn-danger" @click="mergeSequence.splice(index,1)">排除</button></div>
          <div class="final-cut-insert"><select class="input" v-model.number="insertMaterialByIndex[index]"><option :value="0">选择已有项目视频</option><option v-for="m in videoMaterials" :key="m.id" :value="m.id">{{m.name||m.path}}</option></select><button class="btn btn-xs btn-primary" :disabled="!insertMaterialByIndex[index]" @click="replaceMergeMaterial(index)">替换为已有</button><button class="btn btn-xs btn-primary" @click="requestMergeVideoUpload(index,0,'replace')">上传并替换</button><button class="btn btn-xs btn-secondary" :disabled="!insertMaterialByIndex[index]" @click="insertMergeMaterial(index,0)">前插已有</button><button class="btn btn-xs btn-secondary" :disabled="!insertMaterialByIndex[index]" @click="insertMergeMaterial(index,1)">后插已有</button><button class="btn btn-xs btn-secondary" @click="requestMergeVideoUpload(index,0)">前插上传</button><button class="btn btn-xs btn-secondary" @click="requestMergeVideoUpload(index,1)">后插上传</button></div>
        </article>
        <div v-if="!mergeSequence.length" class="card empty">当前没有参与最终成片的片段。可按Scene重置或上传视频。</div>
      </div>
    </section>

    <section v-show="workspaceTab==='continuity'" class="section">
      <div class="section-head"><div><span class="overline">CONTINUITY</span><h2>跨集连续性</h2><p class="sub">上一集摘要、末帧、角色出场与下一集钩子。</p></div><div class="section-actions"><button class="btn btn-sm btn-ghost" @click="loadEpisodeContinuity">刷新</button><button class="btn btn-sm btn-secondary" @click="regenerateContinuity">AI重生成</button><button class="btn btn-sm btn-secondary" @click="runCreativeIntent">创作意图澄清</button></div></div>
      <div class="card" v-if="continuity"><p><strong>上一集摘要：</strong><span v-if="continuity.previous_episode?.summary_stale" class="badge warn">已过期</span> {{ continuity.previous_summary || '暂无' }}</p><p><strong>结尾钩子：</strong><span v-if="continuity.previous_episode?.next_hook_stale" class="badge warn">已过期</span> {{ continuity.previous_hook || '暂无' }}</p><p><strong>本集角色：</strong>{{ (continuity.character_appearances || []).map(v => v.name).join('、') || '暂无' }}</p><div class="merge-links" v-if="continuity.last_scene_images?.length"><span v-for="f in continuity.last_scene_images" :key="f">{{ f }}</span></div><pre v-if="intentDraft" class="prompt-preview">{{ intentDraft }}</pre></div>
    </section>

    <section v-show="workspaceTab==='advanced'" class="section">
      <details class="card"><summary><strong>项目 Skill 配置与审计</strong></summary>
        <p class="sub">项目配置仅在 Skill 的 operation 与具体操作契约一致时生效；否则安全回退到系统专用 Skill。</p>
        <div class="shot-grid"><label>阶段<select v-model="skillStage" class="input" @change="onSkillStageChange"><option v-for="s in skillStages" :key="s.value" :value="s.value">{{ s.label }}</option></select></label><label>操作契约<select v-model="skillOperation" class="input" @change="loadSkillPanel"><option v-for="op in stageOperations" :key="op" :value="op">{{ op }}</option></select></label><label>Skill<select v-model="selectedSkillId" class="input"><option value="">使用系统默认</option><option v-for="s in operationSkills" :key="s.id" :value="String(s.id)">{{ s.name }} · v{{ s.version }}</option></select></label></div>
        <div class="section-actions"><button class="btn btn-sm" @click="saveProjectSkill">保存项目配置</button><button class="btn btn-sm btn-ghost" @click="resetProjectSkill">恢复系统默认</button><button class="btn btn-sm btn-ghost" @click="loadSkillAudit">刷新审计</button></div>
        <p class="sub" v-if="effectiveSkill">当前有效：{{ effectiveSkill.name }}（{{ effectiveSkill.operation || effectiveSkill.code }}）</p>
        <div v-if="skillAudits.length" class="history"><div v-for="row in skillAudits" :key="row.id" class="history-item">{{ row.stage }} · {{ row.skill_code }} · {{ row.success ? '成功' : ('失败：' + (row.error || '未知错误')) }} · {{ new Date(row.created_at).toLocaleString() }}</div></div>
      </details>
    </section>

    <section v-show="workspaceTab==='production'" class="section production-board-section">
      <div class="section-head"><div><span class="overline">PRODUCTION BOARD</span><h2>本集生产看板</h2><p class="sub">Scene是唯一生产单元；点击卡片定位，不创建第二套状态。</p></div></div>
      <div class="production-board">
        <div v-for="column in productionColumns" :key="column.key" class="board-column">
          <header><strong>{{column.label}}</strong><span>{{column.scenes.length}}</span></header>
          <button v-for="sc in column.scenes" :key="sc.id" class="board-card" :class="[{active:selected?.id===sc.id},'severity-'+column.severity]" @click="selectScene(sc)"><span>场景{{sc.order}} · {{sc.title||'未命名'}}</span><small>{{sc.shot_count||0}} Shot · {{Number(sc.duration||0).toFixed(1)}}秒</small><small v-if="sc.prompt_stale" class="warn-msg">提示词待更新</small><small v-if="sc.error" class="fail-msg">{{sc.error}}</small></button>
          <p v-if="!column.scenes.length" class="board-empty">无</p>
        </div>
      </div>
      <details class="card preflight"><summary><strong>本集正式生成前检查</strong></summary><div v-for="check in episodePreflight" :key="check.text" :class="check.ok?'check-ok':'check-fail'">{{check.ok?'✓':'✗'}} {{check.text}}</div><div v-if="preflightIssueScenes.length" class="preflight-issues"><button v-for="sc in preflightIssueScenes" :key="sc.id" class="preflight-issue" @click="selectScene(sc)"><strong>场景{{sc.order}} · {{sc.title}}</strong><span v-if="sc.shot_duration_mismatch">Scene {{Number(sc.duration).toFixed(1)}}秒 / Shot {{Number(sc.shot_duration_total).toFixed(1)}}秒</span><span v-else>存在正式生成前待处理项</span></button></div><div class="section-actions"><button v-if="preflightIssueScenes.length" class="btn btn-sm btn-secondary" @click="selectScene(preflightIssueScenes[0])">定位首个异常Scene</button><button v-if="shotDurationMismatch" class="btn btn-sm btn-secondary" @click="previewAndApplyRetime">修复当前Scene配时</button><button v-if="selected?.prompt_stale" class="btn btn-sm btn-secondary" @click="prepareVideoPrompt">重新准备当前视频提示词</button><button v-if="scenes.some(s=>s.continuity_status==='source_invalidated'||s.continuity_error)" class="btn btn-sm btn-secondary" @click="selectScene(scenes.find(s=>s.continuity_status==='source_invalidated'||s.continuity_error));showFrameSelector=true">修复首个连续性异常</button><button v-if="staleDialogueCount" class="btn btn-sm btn-secondary" @click="dubStaleEpisode">仅生成过期配音</button></div></details>
    </section>

    <!-- 时间轴 -->
    <section v-show="workspaceTab==='production'" class="section">
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
        <div class="timeline-scroll" aria-label="场景时间轴横向滚动区" tabindex="0">
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
        <div v-if="selected" class="shot-timeline-wrap">
          <div class="shot-timeline-head"><strong>场景{{selected.order}}内部Shot</strong><span :class="{'fail-msg':shotDurationMismatch}">{{selectedShots.length}}镜 · {{shotDurationTotal.toFixed(1)}}秒 / Scene {{Number(selected.duration||0).toFixed(1)}}秒</span><button v-if="shotDurationMismatch" class="btn btn-xs btn-secondary" @click="previewAndApplyRetime">预览并重新配时</button></div>
          <div class="shot-timeline">
            <div v-for="shot in timelineShots" :key="shot.id" class="shot-clip" :style="{flexGrow:Math.max(1,Number(shot.duration||1))}"><strong>Shot {{shot.order}}</strong><span>{{shot.shot_type}} · {{shot.camera_movement}}</span><small>{{shot._start.toFixed(1)}}–{{shot._end.toFixed(1)}}秒 · {{Number(shot.duration||0).toFixed(1)}}秒</small><div v-if="shot.dialogue_fragments?.length" class="shot-dialogue"><span v-for="fragment in shot.dialogue_fragments" :key="fragment.dialogue_id+'-'+fragment.start_rune">{{fragment.speaker_label||'未指定说话人'}}：{{fragment.text}} <small>[{{fragment.start_rune}},{{fragment.end_rune}})</small></span></div><div class="shot-flags"><i v-if="shot.continues_from_previous">承接</i><i v-if="shot.continues_to_next">延续</i><i v-if="!shot.dialogue_fragments?.length">静音</i></div></div>
            <p v-if="!selectedShots.length" class="board-empty">当前Scene尚无内部Shot。</p>
          </div>
          <div class="screen-text-track-head"><strong>功能字幕/屏幕文字轨（非对白字幕）</strong><button type="button" class="btn btn-xs btn-secondary" @click="editScreenTextCue()">＋ 新增功能字幕</button></div>
          <div class="screen-text-track" aria-label="功能文字时间轴">
            <button v-for="cue in selectedScreenTextCues" :key="cue.id" type="button" class="screen-text-cue" :class="{'cue-disabled':!cue.enabled}" :style="cueTimelineStyle(cue)" @click="editScreenTextCue(cue)" :title="`${cue.start_time}–${cue.end_time}秒 ${cue.text}`"><span>{{screenTextKindLabel(cue.kind)}}</span><strong>{{cue.text}}</strong></button>
            <span v-if="!selectedScreenTextCues.length" class="screen-text-empty">本Scene尚无人物题名、地点、时间或过场文字</span>
          </div>
        </div>
      </div>
    </section>

    <section v-if="selected" v-show="workspaceTab==='continuity'" class="section relation-workbench">
      <div class="continuity-compare card">
        <div class="section-head"><div><span class="overline">CONTINUITY COMPARE</span><h2>连续性对比</h2></div><button class="btn btn-sm btn-secondary" @click="showFrameSelector=true">设置连续性</button></div>
        <div class="compare-grid">
          <figure><figcaption>上一Scene确认尾帧</figcaption><img v-if="videoPromptDetail?.continuity_frame?.image_url" :src="videoPromptDetail.continuity_frame.image_url"><div v-else class="compare-empty">{{previousScene?'尚未选择确认尾帧':'本集第一Scene'}}</div></figure>
          <figure><figcaption>当前Scene分镜图</figcaption><img v-if="selected.image_url" :src="selected.image_url"><div v-else class="compare-empty">尚未生成分镜图</div></figure>
          <figure><figcaption>当前视频真实首帧</figcaption><img v-if="selected.video_actual_first_frame_url" :src="selected.video_actual_first_frame_url"><div v-else class="compare-empty">尚未从成片提取</div><small v-if="!selected.video_actual_first_frame_url&&selected.image_url">提交起始帧为当前分镜图（不是成片实测首帧）</small></figure><figure><figcaption>当前视频真实尾帧</figcaption><img v-if="selected.video_actual_last_frame_url" :src="selected.video_actual_last_frame_url"><div v-else class="compare-empty">尚未从成片提取</div></figure>
        </div>
        <p class="sub">模式：{{videoPromptDetail?.continuity_mode||'independent'}} · 状态：{{videoPromptDetail?.continuity_status||'not_required'}}</p>
      </div>
      <div class="reference-map card">
        <div class="section-head"><div><span class="overline">REFERENCE MAP</span><h2>Picture ↔ Subject关系</h2><p class="sub">以下顺序就是正式H3提交顺序。</p></div></div>
        <div v-for="ref in (videoPromptDetail?.reference_bindings||[])" :key="ref.picture" class="reference-row"><img :src="ref.image_url"><div><strong>&lt;Picture {{ref.picture}}&gt;<template v-if="ref.subject"> → &lt;Subject {{ref.subject}}&gt;</template></strong><p>{{ref.identity}} · {{ref.role}} · {{ref.usage}}</p><small>出现范围：{{referenceShotScope(ref)}}</small></div></div>
        <div v-if="!(videoPromptDetail?.reference_bindings||[]).length" class="board-empty">当前没有有效视频参考图。</div>
      </div>
    </section>

    <div class="editor-split" v-if="selected" v-show="workspaceTab==='production'">
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
            <input ref="sceneVideoUploadInput" type="file" accept="video/mp4,video/webm,video/quicktime,video/x-matroska" hidden @change="uploadCurrentSceneVideo">
            <button class="btn btn-sm btn-secondary" :disabled="sceneVideoReplacing" @click="sceneVideoUploadInput?.click()">{{sceneVideoReplacing?'替换中…':'上传视频作为当前分镜'}}</button>
            <select v-model.number="sceneVideoMaterialID" class="input input-sm"><option :value="0">从项目视频素材选择</option><option v-for="m in videoMaterials" :key="m.id" :value="m.id">{{m.name||m.path}}</option></select>
            <button class="btn btn-sm btn-secondary" :disabled="sceneVideoReplacing||!sceneVideoMaterialID" @click="replaceCurrentSceneVideoFromMaterial">替换当前分镜视频</button>
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
            <label>导演类型</label>
            <select class="input input-sm" v-model="sceneModeInput" @change="saveSceneMode"><option value="">自动判断</option><option value="文戏">文戏</option><option value="武戏">武戏 / 仙术对轰</option><option value="文武混合">文武混合</option></select>
            <button class="btn btn-sm btn-secondary" :disabled="sceneModeSaving" @click="saveSceneMode">{{ sceneModeSaving ? '保存中…' : '保存类型' }}</button>
            <span class="dur-unit">生成分镜与H3提示词时使用对应导演规则</span>
          </div>
          <div class="duration-edit">
            <label>目标时长</label>
            <input type="number" class="input input-sm" v-model.number="durationInput" min="3" :max="h3DurationMax" step="0.5" @change="saveDuration" />
            <span class="dur-unit">秒 · 当前上限 {{h3DurationMax}}<span v-if="durationInput > 15"> · 长视频高显存</span></span>
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
                <span class="dl-char">{{ speakerLabel(d) }}</span>
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
                <span class="dl-char">{{ speakerLabel(s) }}</span>
                <span class="dl-text">{{ s.text }}</span>
              </div>
              <span class="sub-scene">场景{{ s.scene_order }}</span>
            </div>
            <p class="sub-hint">时间轴按配音真实时长自动对齐；调整字幕时间会保存为该对白的时间偏移，并在合并时生效。</p>
          </div>
        </div>
      </section>
    </div>

    <ShotDirectorEditor v-if="selected" v-show="workspaceTab==='production'" :key="`shot-director-${selected.id}`" :project-id="id()" :scene-id="selected.id" :genre="project?.genre || ''" :tone="project?.tone || ''" :scene-title="selected.title || ''" :scene-content="selected.content || ''" :scene-mode="selected.scene_mode || ''" @scene-changed="reloadSelectedScene" @scene-materialized="handleShotMaterialized" />

    <section v-if="selected" v-show="workspaceTab==='production'" ref="screenTextSection" class="section screen-text-section">
      <div class="section-head"><div><span class="overline">SCREEN TEXT</span><h2>功能字幕 / 屏幕文字（非对白字幕）</h2><p class="sub">用于人物出场、地点、时间、过场和“本集完”；不会进入Dialogue、配音或H3提示词。</p><p class="font-compliance" :class="approvedFonts.some(font=>font.verified)?'ok':'warn'">{{approvedFonts.some(font=>font.verified)?`已验证 ${approvedFonts.filter(font=>font.verified).length} 套开源中文字体`:'尚未安装通过许可证与SHA-256校验的中文字体；可编辑预览，但正式发行导出将被阻止'}}</p></div><button class="btn btn-sm btn-secondary" @click="editScreenTextCue()">新增功能文字</button></div>
      <div class="card screen-text-preflight">
        <div class="screen-text-preflight-head"><strong>最终渲染检查</strong><span v-if="screenTextPreflightLoading">检查中…</span><span v-else-if="screenTextPreflight?.ok" class="check-ok">✓ 可正式导出</span><span v-else class="check-fail">✗ 正式导出将被阻止</span><button class="btn btn-xs btn-secondary" @click="loadScreenTextPreflight">重新检查</button></div>
        <p v-if="screenTextPreflight">{{screenTextPreflight.width}}×{{screenTextPreflight.height}} · {{screenTextPreflight.font_family || screenTextPreflight.font_code}} · {{screenTextPreflight.previews?.length || 0}} 条已审核文字</p>
        <div v-for="issue in screenTextPreflight?.issues || []" :key="`${issue.cue_id}-${issue.code}-${issue.message}`" :class="issue.severity==='error'?'check-fail':'screen-text-warning'">{{issue.severity==='error'?'✗':'⚠'}} {{issue.message}}</div>
      </div>
      <div class="screen-text-grid">
        <div class="card screen-text-list">
          <button v-for="cue in selectedScreenTextCues" :key="cue.id" type="button" class="screen-text-row" @click="editScreenTextCue(cue)"><span class="badge badge-gray">{{screenTextKindLabel(cue.kind)}}</span><strong>{{cue.text}}</strong><small>{{cue.shot_id ? `Shot ${selectedShots.find(s=>s.id===cue.shot_id)?.order || '?'} · ` : ''}}{{cue.start_time}}–{{cue.end_time}}秒 · {{cue.writing_mode}}</small></button>
          <div v-if="!selectedScreenTextCues.length" class="empty">当前Scene暂无功能文字。</div>
        </div>
        <form v-if="screenTextEditing" class="card inline-form screen-text-form" @submit.prevent="saveScreenTextCue">
          <label>类型<select v-model="screenTextForm.kind" class="input"><option value="character_intro">人物出场</option><option value="location">地点</option><option value="time_card">时间</option><option value="transition">过场</option><option value="chapter_title">章回标题</option><option value="story_note">剧情提示</option><option value="end_card">本集完</option><option value="custom">自定义</option></select></label>
          <label>绑定内部Shot（可选）<select v-model="screenTextForm.shot_id" class="input" @change="applyScreenTextShotRange"><option :value="null">整个当前分镜Scene</option><option v-for="shot in timelineShots" :key="shot.id" :value="shot.id">Shot {{shot.order}} · {{shot._start.toFixed(1)}}–{{shot._end.toFixed(1)}}秒</option></select></label>
          <label v-if="screenTextForm.kind==='character_intro'">绑定角色<select v-model="screenTextForm.character_id" class="input" required><option :value="null">请选择</option><option v-for="character in characters" :key="character.id" :value="character.id">{{character.name}}</option></select></label>
          <label>排版<select v-model="screenTextForm.writing_mode" class="input"><option value="horizontal-ltr">中文横排</option><option value="vertical-rl">从上到下、列从右到左</option><option value="vertical-lr">从上到下、列从左到右</option><option value="stacked-upright">单列直立</option></select></label>
          <label>位置<select v-model="screenTextForm.anchor" class="input"><option value="top_left">左上</option><option value="top_center">上方居中</option><option value="top_right">右上</option><option value="center">居中</option><option value="left_center">左侧垂直居中</option><option value="right_center">右侧垂直居中</option><option value="bottom_left">左下</option><option value="bottom_center">下侧水平居中</option><option value="bottom_right">右下</option><option value="subject_left">人物左侧</option><option value="subject_right">人物右侧</option><option value="custom">自定义精确位置</option></select></label>
          <label v-if="screenTextForm.anchor==='custom'">水平位置<input v-model.number="screenTextForm.position_x" type="number" min="0.05" max="0.95" step="0.01" class="input" required></label><label v-if="screenTextForm.anchor==='custom'">垂直位置<input v-model.number="screenTextForm.position_y" type="number" min="0.05" max="0.95" step="0.01" class="input" required></label>
          <label>开始秒<input v-model.number="screenTextForm.start_time" type="number" min="0" :max="selected.duration" step="0.1" class="input" required></label><label>结束秒<input v-model.number="screenTextForm.end_time" type="number" min="0.1" :max="selected.duration" step="0.1" class="input" required></label>
          <label class="wide">主文字<input v-model="screenTextForm.text" class="input" maxlength="200" required></label><label class="wide">副标题<input v-model="screenTextForm.subtext" class="input" maxlength="200"></label>
          <label>艺术字预设<select v-model="screenTextForm.style_code" class="input"><option value="xianxia-character-vertical">仙侠人物竖排</option><option value="xianxia-location-vertical">古装地点竖排</option><option value="historical-time-card">古装时间卡</option><option value="ink-transition-card">水墨过场</option><option value="ink-end-card">水墨结束卡</option><option value="modern-horizontal-caption">简洁横排</option><option value="xianxia-gold-title">仙侠金纹题名</option><option value="cold-silver-runes">冷银符文</option><option value="vermilion-seal">朱砂印章</option></select></label><label>字体<select v-model="screenTextForm.font_code" class="input"><option v-for="font in approvedFonts.filter(v=>v.verified)" :key="font.code" :value="font.code">{{font.display_name}} · {{font.family}}</option></select></label><label>主文字字号<input v-model.number="screenTextForm.font_size" type="number" min="1" max="200" step="1" class="input" placeholder="留空使用样式预设"></label><label>副标题字号<input v-model.number="screenTextForm.subtext_size" type="number" min="1" max="120" step="1" class="input" placeholder="留空使用样式预设"></label><label>主文字颜色<input v-model="screenTextForm.primary_color" type="color" class="input color-input"></label><label>副标题颜色<input v-model="screenTextForm.subtext_color" type="color" class="input color-input"></label><label>描边颜色<input v-model="screenTextForm.outline_color" type="color" class="input color-input"></label><label>阴影颜色<input v-model="screenTextForm.shadow_color" type="color" class="input color-input"></label><label>描边粗细<input v-model.number="screenTextForm.outline_size" type="number" min="0" max="20" step="1" class="input"></label><label>阴影距离<input v-model.number="screenTextForm.shadow_size" type="number" min="0" max="20" step="1" class="input"></label>
          <label>动画<select v-model="screenTextForm.animation" class="input"><option value="none">无</option><option value="fade">淡入淡出</option><option value="slide">滑入</option><option value="typewriter">逐字</option><option value="ink_reveal">墨迹显字（当前以慢淡入渲染）</option></select></label>
          <label class="check"><input v-model="screenTextForm.enabled" type="checkbox"> 启用</label>
          <div class="screen-text-preview wide" :style="selected?.image_url?{backgroundImage:`linear-gradient(rgba(0,0,0,.16),rgba(0,0,0,.16)),url(${selected.image_url})`}:{}" @click="setScreenTextPosition" title="点击画面设置自定义位置"><div class="screen-text-safe-zone"></div><img v-if="screenTextForm.id" :src="screenTextRenderPreviewUrl(screenTextForm.id)" class="screen-text-rendered-layer" alt="libass最终透明文字图层"><div v-else class="screen-text-preview-copy" :class="[screenTextForm.writing_mode?.startsWith('vertical')||screenTextForm.writing_mode==='stacked-upright'?'vertical':'horizontal', `style-${screenTextForm.style_code}`]" :style="screenTextPreviewPosition"><strong :style="screenTextForm.font_size>0?{fontSize:`${screenTextForm.font_size}px`}:{}">{{screenTextPreviewText}}</strong><small v-if="screenTextForm.subtext" :style="screenTextForm.subtext_size>0?{fontSize:`${screenTextForm.subtext_size}px`}:{}">{{screenTextForm.subtext}}</small></div></div>
          <p v-if="screenTextForm.id" class="screen-text-render-note wide">当前显示由正式字体与libass规则生成的透明PNG；可直接修改主文字/副标题字号，保存后会强制刷新最终效果。</p>
          <div class="section-actions wide"><button class="btn btn-sm" :disabled="screenTextSaving">{{screenTextSaving?'保存中…':(screenTextForm.id?'保存修改':'添加到当前分镜')}}</button><button v-if="screenTextForm.id" type="button" class="btn btn-sm btn-danger" @click="removeScreenTextCue(screenTextForm);screenTextEditing=false">删除</button><button type="button" class="btn btn-sm btn-ghost" @click="screenTextEditing=false">取消</button></div>
        </form>
      </div>
    </section>

    <section v-show="workspaceTab==='advanced'" class="section">
      <div class="section-head"><div><span class="overline">SHARED ASSETS</span><h2>共享资产引用</h2><p class="sub">创建、修改或删除项目、场景和镜头级素材引用。</p></div><button class="btn btn-sm btn-secondary" @click="editSharedAsset()">引用素材</button></div>
      <div class="card" v-if="sharedAssetReferences.length"><div v-for="refRow in sharedAssetReferences" :key="refRow.id" class="merge-item"><div class="merge-info"><strong>{{ refRow.material?.name || `素材 #${refRow.material_id}` }}</strong><span>{{ refRow.mode }} · {{ refRow.shot_id ? `镜头 #${refRow.shot_id}` : refRow.scene_id ? `场景 #${refRow.scene_id}` : '项目级' }}</span></div><div class="merge-links"><button class="btn btn-sm btn-ghost" @click="editSharedAsset(refRow)">编辑</button><button class="btn btn-sm btn-danger" @click="removeSharedAsset(refRow)">删除</button></div></div></div><div class="card empty" v-else>暂无共享资产引用。</div>
      <form v-if="sharedAssetEditing" class="card inline-form" @submit.prevent="saveSharedAsset"><label>素材<select v-model.number="sharedAssetForm.material_id" class="input" required><option value="">请选择</option><option v-for="m in materials" :key="m.id" :value="m.id">{{ m.name }} · {{ m.type }}</option></select></label><label>模式<select v-model="sharedAssetForm.mode" class="input"><option value="live">实时引用</option><option value="copy">项目副本</option></select></label><label>范围<select v-model="sharedAssetForm.scope" class="input"><option value="project">项目</option><option value="scene" :disabled="!selected">当前场景</option></select></label><div class="section-actions"><button class="btn btn-sm">保存</button><button type="button" class="btn btn-sm btn-ghost" @click="sharedAssetEditing=false">取消</button></div></form>
    </section>

    <section v-show="workspaceTab==='sound'" class="section">
      <div class="section-head"><div><span class="overline">PROJECT SCORE LIBRARY</span><h2>全剧可复用配乐库</h2><p class="sub">为整部剧生成5–8首人物、情感、悬疑、环境、危机或动作主题。审核采用后，每集只需引用、裁剪、循环和淡化，不再重复生成同类音乐。</p></div><button class="btn btn-sm btn-secondary" :disabled="musicCueBusy" @click="draftMusicLibrary">{{musicCueBusy?'生成中…':'AI生成全剧配乐库'}}</button></div>
      <div class="card"><label class="wide">全剧配乐身份、主题与排除项<textarea v-model="musicRequirements" class="textarea" rows="3" placeholder="例如：华语仙侠影视配乐；主题统一；纯器乐；对白下克制；需要主角、姐妹、悬疑、宗门、战斗和温馨主题"></textarea></label></div>
      <div class="card" v-if="musicLibrary.length"><div v-for="track in musicLibrary" :key="`library-${track.id}`" class="music-cue-card"><div class="merge-info"><span class="badge" :class="track.review_status==='approved'?'badge-green':'badge-gray'">{{track.review_status}}</span><strong>{{track.name}} · {{track.theme_code}}</strong><span>{{track.kind}} / {{track.mood}} · 建议 {{track.duration||track.end_time}} 秒</span><span>{{track.tags}}</span></div><label>Structured Caption JSON<textarea v-model="track.caption_json" class="textarea code-textarea" rows="8"></textarea></label><div class="section-actions"><button class="btn btn-sm btn-secondary" @click="saveMusicCue(track)">保存草稿</button><button class="btn btn-sm" :disabled="track.stale" @click="reviewMusicCue(track,'approve')">审核通过</button><button class="btn btn-sm btn-secondary" :disabled="track.review_status!=='approved'||track.stale||musicCandidateBusy" @click="generateMusicCandidate(track)">生成主题音乐候选</button><button class="btn btn-sm" :disabled="track.stale" @click="placeMusicLibraryTrack(track)">铺到本集</button><button class="btn btn-sm btn-ghost" @click="toggleMusicCandidates(track)">{{musicCandidateCueId===track.id?'收起候选':'试听候选'}}</button></div><div v-if="musicCandidateCueId===track.id" class="candidate-list"><article v-for="item in musicCandidates" :key="item.candidate.id" class="candidate" :class="{current:item.candidate.is_current,stale:item.candidate.stale}"><div class="merge-info"><strong>{{item.candidate.model||'Music 3候选'}}</strong><span>{{item.candidate.duration?item.candidate.duration.toFixed(2)+' 秒':'时长待探测'}} · {{item.candidate.is_current?'已采用':item.candidate.review_status}}</span></div><audio v-if="item.audio_url" :src="item.audio_url" controls preload="none"></audio><label v-if="item.candidate.review_status==='pending'" class="checkbox"><input v-model="item._confirmed" type="checkbox">我已完整试听该候选</label><div class="section-actions"><button v-if="item.candidate.review_status==='pending'" class="btn btn-sm btn-secondary" :disabled="item.candidate.stale||!item._confirmed" @click="reviewMusicCandidate(track,item,'accepted')">审核通过</button><button class="btn btn-sm" :disabled="item.candidate.is_current||item.candidate.stale||item.candidate.review_status!=='accepted'" @click="selectMusicCandidate(track,item)">采用为项目曲目</button></div></article></div></div></div><div class="card empty" v-else>尚未生成全剧配乐库。</div>
    </section>

    <section v-show="workspaceTab==='sound'" class="section">
      <div class="section-head"><div><span class="overline">LEGACY EPISODE MUSIC CUE SHEET</span><h2>MiniMax Music 3 配乐提示词</h2><p class="sub">先填写你的配乐设想，由 music-caption-rewriter 生成 Structured Caption；审核通过后可提交真实Music 3候选；生成结果必须完整试听、审核并采用。</p></div><button class="btn btn-sm btn-secondary" :disabled="musicCueBusy" @click="draftMusicCues">{{musicCueBusy?'生成中…':'AI生成配乐方案'}}</button><button class="btn btn-sm btn-ghost" :disabled="musicCueBusy||!musicCues.length" @click="batchApproveMusicCues">批量审核本集</button></div>
      <div class="card"><label class="wide">本集配乐要求、设想与排除项<textarea v-model="musicRequirements" class="textarea" rows="3" placeholder="例如：纯器乐；情感段使用尼龙弦古典吉他指弹；高潮加入低音弦乐；对白期间克制；不要人声、合唱和电子鼓"></textarea></label></div>
      <div class="card" v-if="musicCues.length"><div v-for="cue in musicCues" :key="cue.id" class="music-cue-card"><div class="merge-info"><span class="badge" :class="cue.review_status==='approved'?'badge-green':cue.review_status==='rejected'?'badge-red':'badge-gray'">{{cue.review_status}}</span><strong>{{cue.kind}} · {{cue.start_time}}s → {{cue.end_time}}s</strong><span>{{cue.mood}} / {{cue.intensity}} · {{cue.instrument_note}}</span><span v-if="cue.stale" class="fail-msg">已过期：{{cue.stale_reason}}</span></div><label>本Cue补充要求<textarea v-model="cue.user_requirements" class="textarea" rows="2"></textarea></label><label>本条AI改写要求<textarea v-model="cue._rewriteRequirements" class="textarea" rows="2" placeholder="仅改写本条，不改变时间与归属"></textarea></label><div class="section-actions"><button class="btn btn-sm btn-secondary" :disabled="cue._rewriteBusy" @click="previewMusicCueRewrite(cue)">{{cue._rewriteBusy?'改写中…':'AI改写预览'}}</button><button v-if="cue._rewritePreview" class="btn btn-sm" @click="applyMusicCueRewrite(cue)">应用预览</button></div><label v-if="cue._rewritePreview">AI改写预览（尚未保存）<textarea :value="cue._rewritePreview" class="textarea code-textarea" rows="10" readonly></textarea></label><label>Structured Caption JSON<textarea v-model="cue.caption_json" class="textarea code-textarea" rows="10"></textarea></label><div class="section-actions"><button class="btn btn-sm btn-secondary" @click="saveMusicCue(cue)">保存草稿</button><button class="btn btn-sm" :disabled="cue.stale" @click="reviewMusicCue(cue,'approve')">审核通过</button><button class="btn btn-sm btn-ghost" @click="reviewMusicCue(cue,'reject')">驳回</button><button class="btn btn-sm btn-secondary" :disabled="cue.review_status!=='approved'||cue.stale||musicCandidateBusy" @click="generateMusicCandidate(cue)">生成Music 3候选</button><button class="btn btn-sm btn-ghost" @click="toggleMusicCandidates(cue)">{{musicCandidateCueId===cue.id?'收起候选':'试听候选'}}</button><button class="btn btn-sm btn-danger" @click="removeMusicCue(cue)">删除</button></div><div v-if="musicCandidateCueId===cue.id" class="candidate-list"><p class="sub">每段最长300秒。长Episode应拆成两段并保持相同主题与配器；候选必须完整试听、审核并采用。</p><div v-if="musicCandidateBusy" class="inline-status">正在加载配乐候选…</div><article v-for="item in musicCandidates" :key="item.candidate.id" class="candidate" :class="{current:item.candidate.is_current,stale:item.candidate.stale}"><div class="merge-info"><strong>{{item.candidate.model||item.candidate.provider||'Music 3候选'}}</strong><span>{{item.candidate.duration?item.candidate.duration.toFixed(2)+' 秒':'时长待探测'}} · {{item.candidate.is_current?'已采用':item.candidate.review_status==='accepted'?'已审核':item.candidate.review_status==='rejected'?'已拒绝':item.task_status==='running'?'ComfyUI生成中':item.task_status==='pending'?'等待GPU调度':item.task_status==='failed'?'任务失败':'待审核'}}</span><span v-if="item.task_id">任务 {{item.task_id}}</span></div><audio v-if="item.audio_url" :src="item.audio_url" controls preload="none"></audio><p v-if="item.candidate.error" class="fail-msg">{{item.candidate.error}}</p><p v-if="item.candidate.stale" class="fail-msg">已过期：{{item.candidate.stale_reason}}</p><label v-if="item.candidate.review_status==='pending'" class="checkbox"><input v-model="item._confirmed" type="checkbox">我已完整试听该候选</label><div class="section-actions"><button v-if="item.candidate.review_status==='pending'" class="btn btn-sm btn-secondary" :disabled="item.candidate.stale||!item._confirmed" @click="reviewMusicCandidate(cue,item,'accepted')">审核通过</button><button class="btn btn-sm" :disabled="item.candidate.is_current||item.candidate.stale||item.candidate.review_status!=='accepted'" @click="selectMusicCandidate(cue,item)">采用此版本</button><button v-if="item.candidate.is_current" class="btn btn-sm btn-ghost" @click="unselectMusicCandidate(cue,item)">取消采用</button><button class="btn btn-sm btn-ghost" :disabled="item.candidate.is_current" @click="reviewMusicCandidate(cue,item,'rejected')">拒绝</button><button class="btn btn-sm btn-danger" :disabled="item.candidate.is_current" @click="removeMusicCandidate(cue,item)">删除</button></div></article><p v-if="!musicCandidateBusy&&!musicCandidates.length" class="sub">暂无音乐候选。</p></div></div></div>
      <div class="card empty" v-else>暂无配乐Cue。填写要求后生成；系统通常只规划2–4个主Cue，避免全程铺满音乐。</div>
    </section>

    <section v-show="workspaceTab==='sound'" class="section">
      <div class="section-head"><div><span class="overline">AUDIO LAYERS</span><h2>环境声 / 音效 / BGM</h2><p class="sub">完整编辑范围、淡入淡出、循环、静音与状态。Music 3 完整覆盖手工 BGM 时，手工层仅在合成时让路，不会被修改。</p></div><button class="btn btn-sm btn-ghost" :disabled="audioPreflightLoading" @click="loadAudioPreflight">{{ audioPreflightLoading ? '检查中…' : '刷新音频预检' }}</button><button class="btn btn-sm btn-secondary" @click="editAudioLayer()">新增音频层</button></div>
      <div v-if="audioPreflight" class="card"><div class="merge-info"><span class="badge" :class="audioPreflight.ready ? 'badge-green' : 'badge-red'">{{ audioPreflight.ready ? '音频层就绪' : '音频层需处理' }}</span><span>响度/削波：{{ audioPreflight.loudness_assessed ? '已评估' : '未评估' }}</span><span v-for="(message, index) in audioPreflight.messages" :key="`${message.code}-${message.layer_id}-${index}`" :class="message.level === 'error' ? 'fail-msg' : 'sub'">{{ message.message }}</span><span v-if="!audioPreflight.messages.length" class="sub">未发现音频层问题。</span></div></div>
      <div class="card" v-if="audioLayers.length"><div v-for="layer in audioLayers" :key="layer.id" class="merge-item"><div class="merge-info"><span class="badge badge-gray">{{ layer.kind }}</span><strong>{{ layer.name || layer.file }}</strong><span>{{ layer.start_time }}s → {{ layer.end_time }}s · 源{{ layer.source_start||0 }}s起 · 音量{{ layer.volume }} · 淡入/出 {{ layer.fade_in }}/{{ layer.fade_out }}s<span v-if="layer.loop"> · 循环</span><span v-if="layer.ducking"> · 对白压低</span><span v-if="layer.muted"> · 已静音</span></span></div><div class="merge-links"><button class="btn btn-sm btn-ghost" @click="editAudioLayer(layer)">编辑</button><button class="btn btn-sm btn-danger" @click="removeAudioLayer(layer)">删除</button></div></div></div>
      <div class="card empty" v-else>暂无音频层。</div>
      <form v-if="audioLayerEditing" class="card inline-form audio-form" @submit.prevent="saveAudioLayer"><label>类型<select v-model="audioLayerForm.kind" class="input"><option value="soundscape">环境声</option><option value="sfx">音效</option><option value="bgm">BGM</option></select></label><label>名称<input v-model="audioLayerForm.name" class="input"></label><label>文件<input v-model="audioLayerForm.file" class="input" required></label><label>场景<select v-model="audioLayerForm.scene_id" class="input"><option :value="null">整集</option><option v-for="sc in scenes" :key="sc.id" :value="sc.id">场景 {{ sc.order }} · {{ sc.title }}</option></select></label><label>开始秒<input v-model.number="audioLayerForm.start_time" type="number" min="0" step="0.1" class="input" required></label><label>结束秒<input v-model.number="audioLayerForm.end_time" type="number" min="0.1" step="0.1" class="input" required></label><label>源音乐起点<input v-model.number="audioLayerForm.source_start" type="number" min="0" step="0.1" class="input"></label><label>音量<input v-model.number="audioLayerForm.volume" type="number" min="0" max="2" step="0.1" class="input"></label><label>淡入<input v-model.number="audioLayerForm.fade_in" type="number" min="0" step="0.1" class="input"></label><label>淡出<input v-model.number="audioLayerForm.fade_out" type="number" min="0" step="0.1" class="input"></label><label>状态<select v-model="audioLayerForm.status" class="input"><option value="draft">草稿</option><option value="ready">就绪</option><option value="failed">失败</option></select></label><label class="check"><input v-model="audioLayerForm.loop" type="checkbox"> 循环</label><label class="check"><input v-model="audioLayerForm.ducking" type="checkbox"> 对白期间压低</label><label class="check"><input v-model="audioLayerForm.muted" type="checkbox"> 静音</label><label class="wide">说明<textarea v-model="audioLayerForm.description" class="textarea" rows="2"></textarea></label><div class="section-actions wide"><button class="btn btn-sm">保存</button><button type="button" class="btn btn-sm btn-ghost" @click="audioLayerEditing=false">取消</button></div></form>
    </section>

    <section class="section" v-if="selected" v-show="workspaceTab==='continuity'">
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
    <GeneratedMediaHistoryDrawer :project-id="id()" :open="showGeneratedHistory" @close="showGeneratedHistory=false" @changed="load" />

    <!-- 帧选择弹窗 -->
    <div v-if="showFrameSelector && selected" class="modal-overlay" @click.self="showFrameSelector = false">
      <div class="modal-content modal-large">
        <FrameSelector
          :project-id="Number(id())"
          :scene-id="selected.id"
          :scene-order="selected.order"
          :source-scene-id="previousScene?.id || 0"
          :source-scene-order="previousScene?.order || 0"
          :source-video-ready="Boolean(previousScene?.video_url || previousScene?.video_file)"
          @close="showFrameSelector = false"
          @frame-selected="onFrameSelected"
          @applied="onContinuityApplied"
        />
      </div>
    </div>

    <!-- 合并记录 -->
    <section v-show="workspaceTab==='delivery'" class="section" v-if="editorEpisodeMerges.length">
      <div class="section-head">
        <div>
          <span class="overline">MERGES</span>
          <h2>第{{activeEpN}}集 · 合并记录</h2>
          <p class="sub">每页最多显示 {{editorMergePageSize}} 条，避免同时加载过多成片播放器。</p>
        </div>
      </div>
      <div class="merge-history-grid">
        <article v-for="m in editorVisibleMerges" :key="m.id" class="card merge-history-card">
          <div class="merge-info">
            <span class="badge" :class="mergeBadgeClass(m.status)">{{ mergeStatusText(m.status) }}</span>
            <span class="merge-time">第{{ m.episode_n || 1 }}集 · {{ (m.created_at || '').slice(0, 16).replace('T', ' ') }}</span>
            <span v-if="m.error" class="fail-msg">{{ m.error }}</span>
            <button v-if="m.status==='failed'" class="btn btn-sm btn-secondary" :disabled="m._retrying" @click="retryMergeRecord(m)">{{m._retrying?'重试中…':'按原编排重试'}}</button>
          </div>
          <div v-if="m.status === 'success' && m.output_file" class="merge-result">
            <video :src="outputUrl(m.output_file)" controls preload="metadata" class="merge-history-video"></video>
            <div class="merge-links">
              <a class="btn btn-sm btn-ghost" :href="outputUrl(m.output_file) + '?download=1'">下载成片</a>
              <a v-if="m.subtitle" class="btn btn-sm btn-ghost" :href="outputUrl(m.output_file.replace(/\.mp4$/, '.srt')) + '?download=1'">下载字幕</a>
              <button class="btn btn-sm btn-danger" :disabled="m._deleting" @click="deleteMergeRecord(m)">{{m._deleting?'删除中…':'删除'}}</button>
            </div>
          </div>
        </article>
        <nav v-if="editorMergePageCount>1" class="editor-merge-pagination" aria-label="剪辑台合并记录分页">
          <button type="button" class="btn btn-sm btn-ghost" :disabled="editorMergePage===1" @click="goToEditorMergePage(editorMergePage-1)">← 上一页</button>
          <button v-for="page in editorMergePageItems" :key="`editor-merge-${page}`" type="button" class="btn btn-sm" :class="page===editorMergePage?'btn-primary':'btn-ghost'" @click="goToEditorMergePage(page)">{{page}}</button>
          <button type="button" class="btn btn-sm btn-ghost" :disabled="editorMergePage===editorMergePageCount" @click="goToEditorMergePage(editorMergePage+1)">下一页 →</button>
          <span>第 {{editorMergePage}} / {{editorMergePageCount}} 页 · 共 {{editorEpisodeMerges.length}} 条</span>
        </nav>
      </div>
    </section>
  </div>
</template>

<script setup>
import { ref, reactive, computed, watch, onMounted, onUnmounted, nextTick } from 'vue'
import { useRoute } from 'vue-router'
import { api } from '../api'
import { useToastStore } from '../stores/toast'
import ShotDirectorEditor from '../components/ShotDirectorEditor.vue'
import FrameSelector from '../components/FrameSelector.vue'
import CharacterHistoryDrawer from '../components/CharacterHistoryDrawer.vue'
import GeneratedMediaHistoryDrawer from '../components/GeneratedMediaHistoryDrawer.vue'
import { compatibleSkills, projectSkillConfig, skillOperationOf } from '../utils/directorWorkflow.js'
import { createDraftSafety } from '../utils/draftSafety.js'

const route = useRoute()
const toast = useToastStore()
const workspaceTabs = [
  {key:'production',label:'分镜制作',description:'Scene、Shot、图片与视频'},
  {key:'continuity',label:'连续性与审核',description:'参考关系、首尾帧与候选'},
  {key:'sound',label:'声音与配乐',description:'Music 3、项目曲库与音频层'},
  {key:'delivery',label:'最终成片',description:'编排、字幕、混音与合并'},
  {key:'advanced',label:'高级设置',description:'Skill、共享资产与审计'}
]
const savedWorkspace = localStorage.getItem('project-editor-workspace')
const requestedWorkspace = String(route.query.workspace || '')
const initialWorkspace = workspaceTabs.some(item=>item.key===requestedWorkspace) ? requestedWorkspace : savedWorkspace
const workspaceTab = ref(workspaceTabs.some(item=>item.key===initialWorkspace)?initialWorkspace:'production')
watch(workspaceTab, value => localStorage.setItem('project-editor-workspace', value))

const project = ref(null)
const episodes = ref([])
const scenes = ref([])
const dialogues = ref([])
const subtitles = ref([])
const serverPreflight = ref(null)
const merges = ref([])
const candidates = ref([])
const compareCandidateIds = ref([])
const audioLayers = ref([])
const audioPreflight = ref(null)
const audioPreflightLoading = ref(false)
const musicCues = ref([])
const musicLibrary = ref([])
const musicRequirements = ref('')
const musicCueBusy = ref(false)
const musicCandidateCueId = ref(null)
const musicCandidates = ref([])
const musicCandidateBusy = ref(false)
const mergeSequence = ref([])
const mergeVideoUploadInput = ref(null)
const mergeVideoUploading = ref(false)
const mergeVideoUploadTarget = reactive({index:0,after:0,mode:'insert'})
const sceneVideoUploadInput = ref(null)
const sceneVideoReplacing = ref(false)
const sceneVideoMaterialID = ref(0)
const mergeSequenceEpisode = ref(0)
const insertMaterialByIndex = reactive({})
const audioLayerEditing = ref(false)
const audioLayerForm = reactive({})
const screenTextCues = ref([])
const screenTextPreflight = ref(null)
const screenTextPreflightLoading = ref(false)
const screenTextEditing = ref(false)
const screenTextSaving = ref(false)
const screenTextSection = ref(null)
const screenTextForm = reactive({})
const characters = ref([])
const approvedFonts = ref([])
const sharedAssetReferences = ref([])
const materials = ref([])
const videoMaterials = computed(() => materials.value.filter(m => m.type === 'video'))
const sharedAssetEditing = ref(false)
const sharedAssetForm = reactive({})
const showCharacterHistory = ref(false)
const showGeneratedHistory = ref(false)
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
const selectedShots = ref([])
const videoPromptDetail = ref(null)
let selectedDetailToken = 0
const tab = ref('dub')
const busy = ref(false)
const curMerging = ref(false)
const mergeSub = ref(false)
const mergeScreenText = ref(true)
const mergeDub = ref(true)
const nativeVolume = ref(1)
const dialogueVolume = ref(1)
const bgmVolume = ref(1)
const durationSaving = ref(false)
const durationInput = ref(5)
const h3DurationMax = ref(15)
const sceneModeSaving = ref(false)
const sceneModeInput = ref('')
const dragFrom = ref(null)
const activeEpN = ref(1)
const editorMergePageSize = 6
const editorMergePage = ref(1)
const editorEpisodeMerges = computed(() => merges.value.filter(row => Number(row.episode_n || 1) === activeEpN.value))
const editorMergePageCount = computed(() => Math.max(1, Math.ceil(editorEpisodeMerges.value.length / editorMergePageSize)))
const editorVisibleMerges = computed(() => editorEpisodeMerges.value.slice((editorMergePage.value-1)*editorMergePageSize, editorMergePage.value*editorMergePageSize))
const editorMergePageItems = computed(() => Array.from({length:editorMergePageCount.value},(_,index)=>index+1))
function goToEditorMergePage(page){editorMergePage.value=Math.min(editorMergePageCount.value,Math.max(1,Number(page)||1))}
watch([activeEpN,editorMergePageCount],()=>{editorMergePage.value=1})
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
const previousScene = computed(() => { const i = scenes.value.findIndex(s => s.id === selected.value?.id); return i > 0 ? scenes.value[i - 1] : null })
function productionStage(s) { if(s.status==='failed'||s.error)return'attention'; if(!Number(s.shot_count||0))return'director'; if(!s.image_file)return'image'; if(s.prompt_stale||!String(s.video_full_prompt||'').trim())return'prompt'; if(!s.video_file)return'video'; const dubs=sceneDubs(s); if(dubs.some(d=>d.audio_stale||!d.audio_file))return'audio'; return'ready' }
const productionColumns = computed(() => [
  {key:'director',label:'待导演设计',severity:'normal'},{key:'image',label:'待分镜图',severity:'normal'},{key:'prompt',label:'待视频提示词审核',severity:'warn'},{key:'video',label:'待视频',severity:'normal'},{key:'audio',label:'待配音/音频',severity:'warn'},{key:'ready',label:'已就绪',severity:'success'},{key:'attention',label:'需处理',severity:'error'}
].map(column=>({...column,scenes:scenes.value.filter(s=>productionStage(s)===column.key)})))
const mismatchedScenes = computed(() => scenes.value.filter(s=>s.shot_duration_mismatch))
const shotDurationTotal = computed(() => selectedShots.value.reduce((sum,s)=>sum+Number(s.duration||0),0))
const shotDurationMismatch = computed(() => selectedShots.value.length>0 && Math.abs(shotDurationTotal.value-Number(selected.value?.duration||0))>0.5)
const timelineShots = computed(() => { let cursor=0; return selectedShots.value.map(shot=>{ const start=cursor; cursor+=Number(shot.duration||0); return {...shot,_start:start,_end:cursor} }) })
const selectedScreenTextCues = computed(() => screenTextCues.value.filter(cue => cue.scene_id === selected.value?.id))
const screenTextPreviewText = computed(() => {
  const value = String(screenTextForm.text || '保存后查看最终libass预览')
  return screenTextForm.writing_mode?.startsWith('vertical') || screenTextForm.writing_mode === 'stacked-upright' ? value.replace(/[\s　]+/g, '') : value
})
const screenTextPreviewPosition = computed(() => {
  const selectedFont = approvedFonts.value.find(font => font.code === screenTextForm.font_code)
  const sizes = { fontFamily: selectedFont?.family ? `"${selectedFont.family}",sans-serif` : undefined, fontSize: screenTextForm.font_size > 0 ? `${screenTextForm.font_size}px` : undefined, color: screenTextForm.primary_color || undefined, '--screen-subtext-size': screenTextForm.subtext_size > 0 ? `${screenTextForm.subtext_size}px` : undefined, '--screen-subtext-color': screenTextForm.subtext_color || undefined, WebkitTextStroke: screenTextForm.outline_size > 0 ? `${screenTextForm.outline_size}px ${screenTextForm.outline_color || '#000000'}` : undefined, textShadow: screenTextForm.shadow_size > 0 ? `${screenTextForm.shadow_size}px ${screenTextForm.shadow_size}px ${screenTextForm.shadow_size}px ${screenTextForm.shadow_color || '#000000'}` : undefined }
  if (screenTextForm.anchor === 'custom') return { ...sizes, left: `${Number(screenTextForm.position_x || .5) * 100}%`, top: `${Number(screenTextForm.position_y || .5) * 100}%`, transform: 'translate(-50%,-50%)' }
  const points = { top_left:[.12,.12], top_center:[.5,.12], top_right:[.88,.12], center:[.5,.5], left_center:[.12,.5], right_center:[.88,.5], bottom_left:[.12,.88], bottom_center:[.5,.88], bottom_right:[.88,.88], subject_left:[.25,.5], subject_right:[.75,.5] }
  const transforms = { top_left:'translate(0,0)', top_center:'translate(-50%,0)', top_right:'translate(-100%,0)', left_center:'translate(0,-50%)', right_center:'translate(-100%,-50%)', bottom_left:'translate(0,-100%)', bottom_center:'translate(-50%,-100%)', bottom_right:'translate(-100%,-100%)' }
  const point = points[screenTextForm.anchor] || [.5,.88]
  return { ...sizes, left:`${point[0]*100}%`, top:`${point[1]*100}%`, transform: transforms[screenTextForm.anchor] || 'translate(-50%,-50%)' }
})
const episodePreflight = computed(() => serverPreflight.value?.checks || [])
const preflightIssueScenes = computed(() => {
  const ids = new Set(serverPreflight.value?.issue_scene_ids || [])
  return scenes.value.filter(scene => ids.has(scene.id))
})
const durProgressClass = computed(() => {
  const p = durProgressPercent.value
  if (p > 105) return 'dur-over'
  if (p > 95) return 'dur-ok'
  if (p > 80) return 'dur-near'
  return 'dur-early'
})

function id() { return route.params.id }
function outputUrl(path) { return `/api/output/0/${path}` }
function screenTextRenderPreviewUrl(cueId) {
  const cue = screenTextCues.value.find(row => Number(row.id) === Number(cueId))
  const version = encodeURIComponent(cue?.updated_at || '')
  return `/api/projects/${id()}/screen-text-cues/${cueId}/render-preview?width=1920&height=1080&v=${version}`
}
function fmtDur(d) {
  const s = Math.round(Number(d) || 0)
  const mm = String(Math.floor(s / 60)).padStart(2, '0')
  const ss = String(s % 60).padStart(2, '0')
  return `${mm}:${ss}`
}
function round1(v) { return Math.round(Number(v) * 10) / 10 }
function speakerKind(row){ const type=String(row?.speech_type||'').toLowerCase(); if(type==='narration')return'narration'; if(type==='monologue'||type==='internal_monologue')return'monologue'; if(type==='offscreen_dialogue')return'offscreen'; return String(row?.character||'').trim()?'dialogue':'unknown' }
function speakerLabel(row){ const kind=speakerKind(row); if(kind==='narration')return'旁白'; if(kind==='monologue')return String(row?.character||'').trim()||'内心独白'; if(kind==='offscreen')return `${String(row?.character||'').trim()||'未指定'}（画外）`; if(kind==='unknown')return'未指定说话人'; return row.character }
function referenceShotScope(ref){ if(ref.retention_mode==='unresolved')return'尚未生成/保存正式H3提示词'; if(ref.shot_orders?.length)return ref.shot_orders.map(n=>`Shot ${n}`).join('、'); return'弱参考，不强制入镜' }

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
    const [{ data }, episodeRes, catalogRes] = await Promise.all([api.editorData(id(), activeEpN.value), api.projectEpisodes(id()), api.modelCatalog()])
    const h3 = (catalogRes.data?.items || []).find(item => item.template_code === 'minimax_h3_ref2v')
    h3DurationMax.value = Number(h3?.duration?.max || 15)
    project.value = data.project
    episodes.value = episodeRes.data || []
    scenes.value = data.scenes || []
    if(mergeSequenceEpisode.value!==activeEpN.value) resetMergeSequence()
    dialogues.value = (data.dialogues || []).map(d => ({ ...d }))
    subtitles.value = data.subtitles || []
    serverPreflight.value = data.preflight || { ready: false, checks: [], issue_scene_ids: [] }
    syncDraftTexts()
    const selectedId = selected.value?.id
    selected.value = (selectedId && scenes.value.find(s => s.id === selectedId)) || scenes.value.find(s => s.status === 'video_ready') || scenes.value[0] || null
    if (selected.value) { durationInput.value = selected.value.duration || 5; sceneModeInput.value = selected.value.scene_mode || ''; await Promise.all([loadCandidates(), loadSelectedWorkbench()]) } else { candidates.value = []; selectedShots.value=[]; videoPromptDetail.value=null }
    const episodeNumbers = epNums()
    epIndex.value = Math.max(0, episodeNumbers.indexOf(activeEpN.value))
    await Promise.all([loadMerges(), loadAudioLayers(), loadAudioPreflight(), loadMusicCues(), loadScreenTextCues(), loadSharedAssets(), loadEpisodeContinuity(), loadSkillPanel()])
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
async function retryMergeRecord(row){
  row._retrying=true
  try{await api.retryMerge(id(),row.id);toast.success('已按原编排启动重试');await loadMerges()}
  catch(e){toast.error(e.response?.data?.error||'重试合并失败')}
  finally{row._retrying=false}
}
async function deleteMergeRecord(row){
  if(row.status==='pending'||row.status==='running'){toast.error('进行中的合并记录不能删除');return}
  if(!window.confirm('删除这条合并记录及其成片文件？此操作不可恢复。'))return
  row._deleting=true
  try{await api.deleteMerge(id(),row.id);toast.success('合并记录和成片文件已删除');await loadMerges()}
  catch(e){toast.error(e.response?.data?.error||'删除合并记录失败')}
  finally{row._deleting=false}
}

function clearSceneDrafts() { videoDraftSafety?.dispose(); videoDraftSafety = null; visualBeatDraft.value = ''; polishDraft.value = ''; assetReviewDraft.value = ''; coverageReviewDraft.value = ''; videoPromptDraft.value = '' }
function startVideoDraftSafety() { videoDraftSafety?.dispose(); videoDraftSafety = createDraftSafety({ key: `draft:h3-prompt:${id()}:${selected.value.id}`, getDraft: () => ({ prompt: videoPromptDraft.value, template: videoPromptTemplate.value }), applyDraft: d => { videoPromptDraft.value = d.prompt || ''; videoPromptTemplate.value = d.template || 'minimax_h3_ref2v' }, save: () => saveReviewedVideoPrompt(false) }); videoDraftSafety.start() }
async function previewAndApplyRetime() {
  if(!selected.value)return
  try { const preview=(await api.previewShotRetime(id(),selected.value.id)).data; const summary=(preview.shots||[]).map(s=>`Shot ${s.order}: ${Number(s.duration).toFixed(1)}秒`).join('\n'); if(!confirm(`当前Shot合计${Number(preview.previous_total).toFixed(1)}秒，将按比例调整至Scene ${Number(preview.scene_duration).toFixed(1)}秒：\n${summary}\n\n确认应用？`))return; await api.applyShotRetime(id(),selected.value.id); await loadSelectedWorkbench(); toast.success('Shot配时已与Scene对齐') } catch(e){ toast.error(e.response?.data?.error||'重新配时失败') }
}
async function loadSelectedWorkbench() {
  const sceneId = selected.value?.id
  if (!sceneId) { selectedShots.value=[]; videoPromptDetail.value=null; return }
  const token = ++selectedDetailToken
  const [shotsResult,promptResult] = await Promise.allSettled([api.sceneShots(id(),sceneId),api.sceneVideoPrompt(id(),sceneId)])
  if (token !== selectedDetailToken || selected.value?.id !== sceneId) return
  selectedShots.value = shotsResult.status==='fulfilled' ? (shotsResult.value.data?.shots||[]) : []
  videoPromptDetail.value = promptResult.status==='fulfilled' ? promptResult.value.data : null
}
function selectScene(sc) {
  selected.value = sc
  clearSceneDrafts()
  durationInput.value = sc.duration || 5
  sceneModeInput.value = sc.scene_mode || ''
  tab.value = 'dub'
  loadSelectedWorkbench().catch(()=>{})
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

async function saveSceneMode() {
  if (!selected.value) return
  sceneModeSaving.value = true
  try {
    const sc = selected.value
    const { data } = await api.updateScene(id(), sc.id, {
      title: sc.title || '', content: sc.content || '', image_prompt: sc.image_prompt || '', image_engine: sc.image_engine || 'minimax_h3',
      scene_mode: sceneModeInput.value, video_prompt: sc.video_prompt || '', duration: Number(sc.duration || 5),
      visible_characters: sc.visible_characters || sc.characters || '', voice_characters: sc.voice_characters || '', mentioned_characters: sc.mentioned_characters || '',
      visual_type: sc.visual_type || 'normal', mega_type: sc.mega_type || ''
    })
    Object.assign(selected.value, data)
    sceneModeInput.value = data.scene_mode || ''
    toast.success(`导演类型已设为${data.scene_mode || '自动判断'}；现有完整视频提示词已标记待更新`)
  } catch (e) { toast.error(e.response?.data?.error || '保存导演类型失败') }
  finally { sceneModeSaving.value = false }
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
    const lines = (data.timeline || []).slice(0, 20).map(row => `${Number(row.start).toFixed(1)}–${Number(row.end).toFixed(1)}s ${speakerLabel(row)}：${row.text}`)
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

function resetMergeSequence(){
  mergeSequence.value=scenes.value.filter(s=>s.status==='video_ready').map(s=>({id:`scene-${s.id}`,scene_id:s.id,label:`Scene ${s.order} · ${s.title}`,video_url:s.video_url,duration:s.video_dur||s.duration||0}))
  mergeSequenceEpisode.value=activeEpN.value
}
function moveMergeClip(index,delta){ const target=index+delta;if(target<0||target>=mergeSequence.value.length)return;const [clip]=mergeSequence.value.splice(index,1);mergeSequence.value.splice(target,0,clip) }
function mergeMaterialClip(material){return {id:`material-${material.id}-${Date.now()}-${Math.random()}`,material_id:material.id,label:material.name||material.path,video_url:api.materialUrl(material.path),duration:0}}
function insertMergeMaterial(index,after){
  const material=videoMaterials.value.find(m=>m.id===insertMaterialByIndex[index]);if(!material)return
  mergeSequence.value.splice(index+after,0,mergeMaterialClip(material))
  insertMaterialByIndex[index]=0
}
function replaceMergeMaterial(index){
  const material=videoMaterials.value.find(m=>m.id===insertMaterialByIndex[index]);if(!material)return
  mergeSequence.value.splice(index,1,mergeMaterialClip(material))
  insertMaterialByIndex[index]=0
  toast.success('当前编排片段已原位替换；原Scene和原视频未修改')
}
function requestMergeVideoUpload(index,after,mode='insert'){
  mergeVideoUploadTarget.index=index;mergeVideoUploadTarget.after=after;mergeVideoUploadTarget.mode=mode;mergeVideoUploadInput.value?.click()
}
async function uploadMergeVideo(event){
  const file=event.target.files?.[0];event.target.value='';if(!file)return
  mergeVideoUploading.value=true
  try{
    const {data}=await api.uploadMaterial(file,'video',Number(id()))
    if(!materials.value.some(item=>item.id===data.id))materials.value.push(data)
    if(mergeVideoUploadTarget.mode==='replace'&&mergeVideoUploadTarget.index<mergeSequence.value.length){
      mergeSequence.value.splice(mergeVideoUploadTarget.index,1,mergeMaterialClip(data))
      toast.success('上传视频已原位替换当前编排片段；原Scene和原视频未修改')
    }else{
      const position=Math.min(mergeSequence.value.length,mergeVideoUploadTarget.index+mergeVideoUploadTarget.after)
      mergeSequence.value.splice(position,0,mergeMaterialClip(data))
      toast.success('视频已上传并插入最终成片编排')
    }
  }catch(e){toast.error(e.response?.data?.error||'上传插入视频失败')}
  finally{mergeVideoUploading.value=false}
}

async function mergeEpisode() {
  if(mergeSequenceEpisode.value!==activeEpN.value||!mergeSequence.value.length)resetMergeSequence()
  const ids = [...new Set(mergeSequence.value.filter(c=>c.scene_id).map(c=>c.scene_id))]
  if (mergeSequence.value.length < 2) {
    toast.show('最终成片至少需要 2 个有效视频片段')
    return
  }
  curMerging.value = true
  try {
    await api.mergeAudioScenes(id(), { scene_ids: ids, edit_sequence: mergeSequence.value, episode_n: activeEpN.value, dub: mergeDub.value, subtitles: mergeSub.value, screen_text: mergeScreenText.value, native_volume: nativeVolume.value, dialogue_volume: dialogueVolume.value, bgm_volume: bgmVolume.value })
    toast.show(`第${activeEpN.value}集合并已启动（对白字幕：${mergeSub.value?'开':'关'}；功能文字：${mergeScreenText.value?'开':'关'}）`)
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
async function uploadCurrentSceneVideo(event) {
  const file=event.target.files?.[0];event.target.value='';if(!file||!selected.value)return
  if(!window.confirm('将上传视频设为当前分镜正式视频？原视频和历史候选会保留，但进行中的合并及已提取首尾帧会失效。'))return
  sceneVideoReplacing.value=true
  try{const {data}=await api.uploadSceneVideo(id(),selected.value.id,file);toast.success(data.message||'当前分镜视频已替换');await Promise.all([reloadSelectedScene(),loadMerges()])}
  catch(e){toast.error(e.response?.data?.error||'上传并替换视频失败')}
  finally{sceneVideoReplacing.value=false}
}
async function replaceCurrentSceneVideoFromMaterial(){
  if(!selected.value||!sceneVideoMaterialID.value)return
  if(!window.confirm('将所选项目视频素材设为当前分镜正式视频？原视频和历史候选会保留。'))return
  sceneVideoReplacing.value=true
  try{const {data}=await api.replaceSceneVideoFromMaterial(id(),selected.value.id,sceneVideoMaterialID.value);toast.success(data.message||'当前分镜视频已替换');sceneVideoMaterialID.value=0;await Promise.all([reloadSelectedScene(),loadMerges()])}
  catch(e){toast.error(e.response?.data?.error||'替换视频失败')}
  finally{sceneVideoReplacing.value=false}
}
async function prepareVideoPrompt() { busy.value = true; try { const { data } = await api.regenerateSceneVideoPrompt(id(), selected.value.id, {}); videoPromptDraft.value = data.full_prompt || data.prompt || ''; videoPromptTemplate.value = data.template || 'minimax_h3_ref2v'; startVideoDraftSafety(); toast.success('完整H3提示词已生成，请审核后保存') } catch (e) { toast.error(e.response?.data?.error || '生成视频提示词失败') } finally { busy.value = false } }
async function saveReviewedVideoPrompt(generate) { busy.value = true; try { await api.updateSceneVideoPrompt(id(), selected.value.id, { prompt: videoPromptDraft.value, template: videoPromptTemplate.value }); if (generate) await api.generateSceneVideo(id(), selected.value.id); videoDraftSafety?.markSaved(); videoPromptDraft.value = ''; await reloadSelectedScene(); toast.success(generate ? '已保存审核提示词并提交视频' : '视频提示词审核结果已保存') } catch (e) { toast.error(e.response?.data?.error || '保存视频提示词失败'); throw e } finally { busy.value = false } }
async function setSceneLock(kind, locked) { try { const { data } = await api.updateSceneLocks(id(), selected.value.id, { [`${kind}_locked`]: locked }); Object.assign(selected.value, data); toast.success(locked ? '生成结果已锁定' : '生成结果已解锁') } catch (e) { toast.error(e.response?.data?.error || '更新锁定状态失败') } }
async function reloadSelectedScene() {
  const sid = selected.value?.id
  await load()
  if (!sid) return
  const current = scenes.value.find(scene => scene.id === sid)
  if (current) {
    selected.value = current
    return
  }
  selected.value = scenes.value[0] || null
  if (selected.value) {
    durationInput.value = selected.value.duration || 5
    sceneModeInput.value = selected.value.scene_mode || ''
    await loadSelectedWorkbench()
  }
}
async function handleShotMaterialized(firstSceneId) { await load(); selected.value = scenes.value.find(s => s.id === Number(firstSceneId)) || scenes.value[0] || null; if (selected.value) { durationInput.value = selected.value.duration || 5; await loadCandidates() } }
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

async function loadMusicCues(){
  try { const [{data:cues},{data:library}]=await Promise.all([api.musicCues(id(),activeEpN.value),api.musicLibrary(id())]); musicCues.value=cues||[]; musicLibrary.value=library||[] }
  catch(e){ toast.error(e.response?.data?.error||'加载配乐方案失败') }
}
async function draftMusicLibrary(){
  musicCueBusy.value=true
  try { const {data}=await api.draftMusicLibrary(id(),musicRequirements.value); musicLibrary.value=data.tracks||[]; toast.success('已生成可审核的全剧配乐库') }
  catch(e){ toast.error(e.response?.data?.error||'生成全剧配乐库失败') }
  finally { musicCueBusy.value=false }
}
async function placeMusicLibraryTrack(track){
  const episodeDuration = scenes.value.filter(s=>Number(s.episode_n)===Number(activeEpN.value)).reduce((sum,s)=>sum+Number(s.duration||0),0)
  if(episodeDuration<=0){ toast.error('当前集没有可用时长'); return }
  try {
    await api.placeMusicLibraryTrack(id(),track.id,{episode_n:activeEpN.value,start_time:0,end_time:episodeDuration,volume:0.35,fade_in:1.5,fade_out:2,loop:true})
    await loadAudioLayers()
    toast.success(`已将“${track.name}”作为可循环BGM铺到本集`)
  } catch(e){ toast.error(e.response?.data?.error||'项目曲目铺轨失败') }
}
async function draftMusicCues(){
  musicCueBusy.value=true
  try { const {data}=await api.draftMusicCues(id(),activeEpN.value,musicRequirements.value); musicCues.value=data.cues||[]; toast.success('已生成可审核的MiniMax Music 3配乐提示词') }
  catch(e){ toast.error(e.response?.data?.error||'生成配乐提示词失败') }
  finally { musicCueBusy.value=false }
}
function musicCuePayload(cue){ return {scope:cue.scope||'episode',theme_code:cue.theme_code||'',name:cue.name||'',episode_n:cue.scope==='library'?0:activeEpN.value,scene_id:cue.scene_id||null,shot_id:cue.shot_id||null,kind:cue.kind,start_time:Number(cue.start_time),end_time:Number(cue.end_time),intensity:cue.intensity,mood:cue.mood||'',instrument_note:cue.instrument_note||'',user_requirements:cue.user_requirements||'',duration:Number(cue.duration||0),tags:cue.tags||'',caption_json:cue.caption_json||'',source:cue.source||'manual',order:Number(cue.order||0),enabled:cue.enabled!==false} }
async function previewMusicCueRewrite(cue){
  const token=Symbol('music-cue-rewrite'); cue._rewriteToken=token; cue._rewriteBusy=true
  const sourceCaption=cue.caption_json||''
  try {
    const {data}=await api.rewriteMusicCue(id(),cue.id,cue._rewriteRequirements||'',sourceCaption)
    if(cue._rewriteToken!==token)return
    cue._rewritePreview=data.caption_json; cue._rewriteSourceCaption=sourceCaption
  } catch(e){ if(cue._rewriteToken===token)toast.error(e.response?.data?.error||'AI改写配乐提示词失败') }
  finally { if(cue._rewriteToken===token)cue._rewriteBusy=false }
}
function applyMusicCueRewrite(cue){
  if(!cue._rewritePreview)return
  if(cue.caption_json!==cue._rewriteSourceCaption&&!window.confirm('请求期间你修改了Caption，仍要用AI预览覆盖当前内容吗？'))return
  cue.caption_json=cue._rewritePreview; cue._rewritePreview=''; cue._rewriteSourceCaption=''
  toast.info('已应用到编辑器，点击“保存草稿”后才会写入')
}
async function saveMusicCue(cue){
  try { const {data}=await api.updateMusicCue(id(),cue.id,musicCuePayload(cue)); Object.assign(cue,data); toast.success('配乐提示词已保存为草稿') }
  catch(e){ toast.error(e.response?.data?.error||'保存配乐Cue失败') }
}
async function reviewMusicCue(cue,action){
  let reason=''
  if(action==='reject'){ reason=window.prompt('填写驳回原因')||''; if(!reason.trim())return }
  try { const {data}=await api.reviewMusicCue(id(),cue.id,action,reason); Object.assign(cue,data); toast.success(action==='approve'?'配乐提示词已审核通过':'已驳回配乐提示词') }
  catch(e){ toast.error(e.response?.data?.error||'审核配乐Cue失败') }
}
async function removeMusicCue(cue){
  if(!window.confirm('删除这个配乐Cue？'))return
  try { await api.deleteMusicCue(id(),cue.id); await loadMusicCues() }
  catch(e){ toast.error(e.response?.data?.error||'删除配乐Cue失败') }
}

async function generateMusicCandidate(cue){
  musicCandidateBusy.value=true
  try {
    await api.generateMusicCandidate(id(),cue.id,-1)
    musicCandidateCueId.value=cue.id
    toast.success('Music 3任务已提交；完成后将作为待审核候选，不会自动采用')
    await loadMusicCandidates(cue)
  } catch(e){ toast.error(e.response?.data?.error||'提交Music 3生成失败') }
  finally { musicCandidateBusy.value=false }
}
async function toggleMusicCandidates(cue){
  if(musicCandidateCueId.value===cue.id){ musicCandidateCueId.value=null; musicCandidates.value=[]; return }
  musicCandidateCueId.value=cue.id
  await loadMusicCandidates(cue)
}
async function loadMusicCandidates(cue,quiet=false){
  if(!quiet) musicCandidateBusy.value=true
  const confirmations=new Map(musicCandidates.value.map(item=>[item.candidate.id,!!item._confirmed]))
  try { const {data}=await api.musicCandidates(id(),cue.id); musicCandidates.value=(data||[]).map(item=>({...item,_confirmed:confirmations.get(item.candidate.id)||false})) }
  catch(e){ if(!quiet){musicCandidates.value=[];toast.error(e.response?.data?.error||'加载配乐候选失败')} }
  finally { if(!quiet) musicCandidateBusy.value=false }
}
async function reviewMusicCandidate(cue,item,status){
  let reason=''
  if(status==='rejected'){ reason=window.prompt('填写拒绝原因')||''; if(!reason.trim())return }
  try { await api.reviewMusicCandidate(id(),cue.id,item.candidate.id,{status,reason,expected_source_hash:item.candidate.source_version_hash,expected_file_sha256:item.candidate.file_sha256,confirmed_full_playback:!!item._confirmed}); await loadMusicCandidates(cue); toast.success(status==='accepted'?'候选已审核通过':'候选已拒绝') }
  catch(e){ toast.error(e.response?.data?.error||'审核配乐候选失败') }
}
async function selectMusicCandidate(cue,item){
  try {
    const { data } = await api.selectMusicCandidate(id(),cue.id,item.candidate.id,{expected_source_hash:item.candidate.source_version_hash,expected_file_sha256:item.candidate.file_sha256})
    await loadMusicCandidates(cue)
    await Promise.all([loadAudioLayers(), loadAudioPreflight()])
    toast.success(data?.bgm_layer ? '已采用配乐候选并放入BGM轨' : '已采用配乐候选')
    const overlaps = data?.overlapping_bgm_layers || []
    if (overlaps.length) toast.info(`该时间段有 ${overlaps.length} 条手工BGM层：完整覆盖时合成会让位，部分重叠仍会叠加，请查看音频预检`)
  }
  catch(e){ toast.error(e.response?.data?.error||'采用配乐候选失败') }
}
async function unselectMusicCandidate(cue,item){
  if(!window.confirm('取消采用这条配乐候选？对应的BGM轨会一并移除。'))return
  try { await api.unselectMusicCandidate(id(),cue.id,item.candidate.id); await loadMusicCandidates(cue); await Promise.all([loadAudioLayers(), loadAudioPreflight()]); toast.success('已取消采用，BGM轨已移除') }
  catch(e){ toast.error(e.response?.data?.error||'取消采用失败') }
}
async function batchApproveMusicCues(){
  if(!window.confirm('批量审核通过本集所有待审核的配乐Cue？其中任何一条校验不通过，整批都不会通过。'))return
  try { await api.batchApproveMusicCues(id(),activeEpN.value); await loadMusicCues(); toast.success('本集配乐Cue已批量审核') }
  catch(e){ toast.error(e.response?.data?.error||'批量审核失败') }
}
async function removeMusicCandidate(cue,item){
  if(!window.confirm('删除这个非当前配乐候选记录？音频文件暂不自动清理。'))return
  try { await api.deleteMusicCandidate(id(),cue.id,item.candidate.id); await loadMusicCandidates(cue) }
  catch(e){ toast.error(e.response?.data?.error||'删除配乐候选失败') }
}

async function loadAudioLayers() {
  try { const { data } = await api.audioLayers(id(), activeEpN.value); audioLayers.value = data || [] }
  catch (e) { toast.error(e.response?.data?.error || '加载音频层失败') }
}
async function loadAudioPreflight() {
  audioPreflightLoading.value = true
  try { audioPreflight.value = (await api.audioLayersPreflight(id(), activeEpN.value)).data }
  catch (e) { audioPreflight.value = { ready: false, loudness_assessed: false, messages: [{ level: 'error', code: 'request_failed', message: e.response?.data?.error || '音频层预检失败' }] } }
  finally { audioPreflightLoading.value = false }
}
async function loadScreenTextPreflight() {
  screenTextPreflightLoading.value = true
  try { screenTextPreflight.value = (await api.screenTextPreflight(id(), activeEpN.value)).data }
  catch (e) { screenTextPreflight.value = { ok:false, issues:[{severity:'error',message:e.response?.data?.error || '功能文字导出检查失败'}], previews:[] } }
  finally { screenTextPreflightLoading.value = false }
}
async function loadScreenTextCues() {
  try {
    const [cueRes, characterRes, fontRes] = await Promise.all([api.screenTextCues(id(), activeEpN.value), api.characters(id()), api.approvedFonts()])
    screenTextCues.value = cueRes.data || []
    characters.value = Array.isArray(characterRes.data) ? characterRes.data : (characterRes.data?.characters || [])
    approvedFonts.value = fontRes.data?.fonts || []
    await loadScreenTextPreflight()
  } catch (e) { toast.error(e.response?.data?.error || '加载功能文字失败') }
}
function screenTextKindLabel(kind) { return { character_intro:'人物出场', location:'地点', time_card:'时间', transition:'过场', chapter_title:'章回标题', story_note:'剧情提示', end_card:'本集完', custom:'自定义' }[kind] || kind }
function cueTimelineStyle(cue) {
  const duration = Math.max(0.1, Number(selected.value?.duration || 1))
  const left = Math.max(0, Math.min(100, Number(cue.start_time || 0) / duration * 100))
  const right = Math.max(left, Math.min(100, Number(cue.end_time || 0) / duration * 100))
  return { left: `${left}%`, width: `${Math.max(2, right-left)}%` }
}
async function editScreenTextCue(cue = null) {
  const duration = Math.max(0.1, Number(selected.value?.duration || 5))
  Object.assign(screenTextForm, {
    id: cue?.id || 0, episode_n: activeEpN.value, scene_id: selected.value?.id || null,
    shot_id: cue?.shot_id || null, character_id: cue?.character_id || null,
    kind: cue?.kind || 'character_intro', text: cue?.text || '', subtext: cue?.subtext || '',
    start_time: cue?.start_time ?? 0, end_time: cue?.end_time ?? Math.min(3, duration),
    writing_mode: cue?.writing_mode || 'vertical-rl', anchor: cue?.anchor || 'top_right',
    position_x: cue?.position_x ?? .5, position_y: cue?.position_y ?? .5,
    style_code: cue?.style_code || 'xianxia-character-vertical', font_code: cue?.font_code || 'noto-sans-sc', primary_color: cue?.primary_color || '#F4E4C0', subtext_color: cue?.subtext_color || '#D1B98B', outline_color: cue?.outline_color || '#2B1C12', shadow_color: cue?.shadow_color || '#000000', outline_size: cue?.outline_size || 0, shadow_size: cue?.shadow_size || 0, font_size: cue?.font_size > 0 ? cue.font_size : null, subtext_size: cue?.subtext_size > 0 ? cue.subtext_size : null, animation: cue?.animation || 'fade',
    enabled: cue?.enabled ?? true, order: cue?.order || 0, source: cue?.source || 'manual', review_status: 'approved'
  })
  screenTextEditing.value = true
  await nextTick()
  screenTextSection.value?.scrollIntoView({ behavior: 'smooth', block: 'start' })
}
function applyScreenTextShotRange() {
  if (!screenTextForm.shot_id) return
  const shot = timelineShots.value.find(row => Number(row.id) === Number(screenTextForm.shot_id))
  if (!shot) return
  screenTextForm.start_time = Math.round(shot._start * 10) / 10
  screenTextForm.end_time = Math.round(shot._end * 10) / 10
}
function setScreenTextPosition(event) {
  const rect = event.currentTarget.getBoundingClientRect()
  const x = Math.min(.95, Math.max(.05, (event.clientX - rect.left) / rect.width))
  const y = Math.min(.95, Math.max(.05, (event.clientY - rect.top) / rect.height))
  screenTextForm.anchor = 'custom'
  screenTextForm.position_x = Math.round(x * 1000) / 1000
  screenTextForm.position_y = Math.round(y * 1000) / 1000
}
async function saveScreenTextCue() {
  if (screenTextSaving.value) return
  const { id: cueId, ...payload } = screenTextForm
  if (!String(payload.text || '').trim()) { toast.error('功能文字不能为空'); return }
  if (payload.anchor !== 'custom') { payload.position_x = null; payload.position_y = null }
  payload.font_size = Number(payload.font_size) || 0
  payload.subtext_size = Number(payload.subtext_size) || 0
  screenTextSaving.value = true
  try {
    cueId ? await api.updateScreenTextCue(id(), cueId, payload) : await api.createScreenTextCue(id(), payload)
    screenTextEditing.value = false
    toast.success('功能文字已加入当前分镜；原始Scene视频保持不变，重新合并并开启“烧录功能文字”后写入成片')
    await loadScreenTextCues()
  } catch (e) { toast.error(e.response?.data?.error || '保存功能文字失败') }
  finally { screenTextSaving.value = false }
}
async function removeScreenTextCue(cue) {
  if (!window.confirm(`删除功能文字“${cue.text}”？`)) return
  try { await api.deleteScreenTextCue(id(), cue.id); await loadScreenTextCues() }
  catch (e) { toast.error(e.response?.data?.error || '删除功能文字失败') }
}
function editAudioLayer(layer = null) {
  Object.assign(audioLayerForm, { id: layer?.id || 0, episode_n: activeEpN.value, scene_id: layer?.scene_id || null, kind: layer?.kind || 'bgm', name: layer?.name || '', file: layer?.file || '', description: layer?.description || '', start_time: layer?.start_time ?? 0, end_time: layer?.end_time ?? targetDuration.value, source_start: layer?.source_start ?? 0, volume: layer?.volume ?? 1, fade_in: layer?.fade_in ?? 0, fade_out: layer?.fade_out ?? 0, loop: !!layer?.loop, ducking: layer?.ducking !== false, muted: !!layer?.muted, status: layer?.status || 'ready' })
  audioLayerEditing.value = true
}
async function saveAudioLayer() {
  const { id: layerId, ...payload } = audioLayerForm
  try { layerId ? await api.updateAudioLayer(id(), layerId, payload) : await api.createAudioLayer(id(), payload); audioLayerEditing.value = false; toast.success('音频层已保存'); await Promise.all([loadAudioLayers(), loadAudioPreflight()]) }
  catch (e) { toast.error(e.response?.data?.error || '保存音频层失败') }
}
async function removeAudioLayer(layer) {
  if (!window.confirm(`删除音频层“${layer.name || layer.file}”？`)) return
  try { await api.deleteAudioLayer(id(), layer.id); await Promise.all([loadAudioLayers(), loadAudioPreflight()]) } catch (e) { toast.error(e.response?.data?.error || '删除失败') }
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

function onContinuityApplied(result, openPrompt) {
  toast.show('连续性配置已应用到视频生成参数')
  showFrameSelector.value = false
  load().then(() => { if (openPrompt) prepareVideoPrompt() })
}

function mergeStatusText(s) { return { pending: '等待中', running: '合并中', success: '成片完成', failed: '失败' }[s] || s }
function mergeBadgeClass(s) { return { pending: 'badge-gray', running: 'badge-orange', success: 'badge-green', failed: 'badge-red' }[s] || 'badge-gray' }

async function openRequestedScreenTextCue() {
  const requestedSceneID = Number(route.query.scene || 0)
  if (requestedSceneID) {
    const requested = scenes.value.find(scene => Number(scene.id) === requestedSceneID)
    if (requested) selectScene(requested)
  }
  if (route.query.screen_text !== '1' || !selected.value) return
  workspaceTab.value = 'production'
  await loadSelectedWorkbench()
  await loadScreenTextCues()
  const requestedCueID = Number(route.query.screen_text_cue || 0)
  const existing = requestedCueID
    ? selectedScreenTextCues.value.find(cue => Number(cue.id) === requestedCueID)
    : (selectedScreenTextCues.value.length === 1 ? selectedScreenTextCues.value[0] : null)
  if (existing) await editScreenTextCue(existing)
  else if (!selectedScreenTextCues.value.length) await editScreenTextCue()
  else {
    screenTextEditing.value = false
    toast.info('当前分镜有多条功能文字，请在已有列表中选择要修改的字幕')
  }
  await nextTick()
  screenTextSection.value?.scrollIntoView({ behavior: 'smooth', block: 'start' })
}

let timer = null
onMounted(async () => {
  const requestedEpisode = Number(route.query.episode || 0)
  if (requestedEpisode > 0) activeEpN.value = requestedEpisode
  await load()
  await openRequestedScreenTextCue()
  timer = setInterval(async () => {
    await load()
    if (musicCandidateCueId.value) {
      const cue = musicCues.value.find(item => item.id === musicCandidateCueId.value)
      if (cue) await loadMusicCandidates(cue, true)
    }
  }, 8000)
})
onUnmounted(() => { clearInterval(timer); videoDraftSafety?.dispose() })
watch(videoPromptDraft, () => videoDraftSafety?.schedule())
</script>

<style scoped>
.editor-page { max-width: 1280px; }
.editor-split { display: grid; grid-template-columns: 1.1fr 1fr; gap: 20px; align-items: start; }
@media (max-width: 980px) { .editor-split { grid-template-columns: 1fr; } }

.production-board{display:grid;grid-template-columns:repeat(7,minmax(180px,1fr));gap:12px;overflow-x:auto}.board-column{min-height:150px;padding:10px;border:1px solid var(--border);border-radius:12px;background:var(--surface-secondary)}.board-column header{display:flex;justify-content:space-between;margin-bottom:8px}.board-card{display:grid;width:100%;gap:4px;margin-bottom:7px;padding:9px;text-align:left;border:1px solid var(--border);border-radius:8px;background:var(--card);color:var(--text-primary);cursor:pointer}.board-card.active{border-color:var(--accent);box-shadow:0 0 0 2px var(--accent-soft)}.board-card.severity-warn{border-left:3px solid #f59e0b}.board-card.severity-error{border-left:3px solid #ef4444}.board-card.severity-success{border-left:3px solid #22c55e}.board-card small,.board-empty{color:var(--text-tertiary)}.warn-msg{color:#f59e0b}.preflight{margin-top:14px;padding:14px}.preflight summary{cursor:pointer}.preflight-issues{display:grid;grid-template-columns:repeat(auto-fill,minmax(260px,1fr));gap:7px;margin:12px 0}.preflight-issue{display:flex;justify-content:space-between;gap:8px;padding:8px;border:1px solid #ef4444;border-radius:8px;background:rgba(239,68,68,.06);color:var(--text-primary);cursor:pointer;text-align:left}.preflight-issue span{color:var(--text-secondary);font-size:12px;white-space:nowrap}.check-ok{color:#22c55e;margin-top:7px}.check-fail{color:#ef4444;margin-top:7px}
.timeline-card { padding: 16px; overflow: visible; }
.timeline-scroll {
  width: 100%; overflow-x: scroll; overflow-y: hidden; padding: 2px 0 12px;
  scrollbar-gutter: stable; scrollbar-width: auto; overscroll-behavior-x: contain;
}
.timeline-scroll::-webkit-scrollbar { height: 12px; }
.timeline-scroll::-webkit-scrollbar-track { background: var(--surface-secondary); border-radius: 999px; }
.timeline-scroll::-webkit-scrollbar-thumb { background: var(--text-tertiary); border: 3px solid var(--surface-secondary); border-radius: 999px; }
.timeline-scroll::-webkit-scrollbar-thumb:hover { background: var(--accent); }
.timeline-scroll:focus-visible { outline: 2px solid var(--accent); outline-offset: 4px; border-radius: 8px; }
.timeline { display: flex; gap: 12px; width: max-content; min-width: 100%; }
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
.shot-timeline-wrap{min-width:100%;margin-top:16px;padding-top:14px;border-top:1px solid var(--border)}.shot-timeline-head{display:flex;justify-content:space-between;margin-bottom:8px;color:var(--text-secondary)}.shot-timeline{display:flex;gap:6px;min-width:700px}.shot-clip{display:grid;min-width:120px;padding:9px;border:1px solid var(--border);border-radius:8px;background:var(--surface-secondary);font-size:11px}.shot-clip span,.shot-clip small{color:var(--text-tertiary)}.shot-dialogue{display:grid;gap:3px;margin-top:5px;padding-top:5px;border-top:1px solid var(--border)}.shot-dialogue span{color:var(--text-secondary)}.shot-flags{display:flex;gap:4px;margin-top:5px}.shot-flags i{padding:2px 5px;border-radius:5px;background:var(--accent-soft);color:var(--accent);font-style:normal}.relation-workbench{display:grid;grid-template-columns:1.2fr 1fr;gap:16px}.continuity-compare,.reference-map{padding:16px}.compare-grid{display:grid;grid-template-columns:repeat(2,1fr);gap:10px}.compare-grid figure{margin:0}.compare-grid figcaption{margin-bottom:6px;font-size:12px;color:var(--text-secondary)}.compare-grid img,.compare-empty{width:100%;aspect-ratio:16/9;object-fit:contain;border-radius:8px;background:#111}.compare-empty{display:flex;align-items:center;justify-content:center;color:#aaa}.reference-row{display:grid;grid-template-columns:88px 1fr;gap:10px;align-items:center;padding:8px 0;border-top:1px solid var(--border)}.reference-row img{width:88px;aspect-ratio:16/9;object-fit:cover;border-radius:6px}.reference-row p{margin:4px 0;color:var(--text-secondary);font-size:12px}

.workspace-tabs{position:sticky;top:0;z-index:30;display:grid;grid-template-columns:repeat(5,minmax(130px,1fr));gap:8px;margin:12px 0;padding:10px;border:1px solid var(--border);border-radius:14px;background:color-mix(in srgb,var(--card) 94%,transparent);backdrop-filter:blur(12px)}
.workspace-tab{display:grid;gap:3px;padding:10px 12px;border:1px solid transparent;border-radius:10px;background:transparent;color:var(--text-secondary);text-align:left;cursor:pointer}.workspace-tab strong{font-size:14px}.workspace-tab small{font-size:10px;color:var(--text-tertiary)}.workspace-tab:hover{background:var(--surface-secondary)}.workspace-tab.active{border-color:var(--accent);background:var(--accent-soft);color:var(--accent)}
.workspace-context{display:flex;gap:12px;align-items:center;margin:-4px 0 14px;padding:0 4px;color:var(--text-tertiary);font-size:12px}.workspace-context span+span:before{content:'•';margin-right:12px}
.merge-history-grid{display:grid;grid-template-columns:repeat(3,minmax(0,1fr));gap:14px}.merge-history-card{display:grid;align-content:start;gap:8px;padding:12px;min-width:0}.merge-history-card .merge-info{display:grid;gap:4px;margin:0}.merge-history-card .merge-result{display:grid;gap:8px}.merge-history-video{display:block;width:100%;aspect-ratio:16/9;max-height:230px;object-fit:contain;border-radius:9px;background:#000}.editor-merge-pagination{grid-column:1/-1;display:flex;align-items:center;justify-content:center;flex-wrap:wrap;gap:7px;padding-top:12px;border-top:1px solid var(--border)}.editor-merge-pagination span{margin-left:6px;color:var(--text-tertiary);font-size:12px}
@media(max-width:1100px){.merge-history-grid{grid-template-columns:repeat(2,minmax(0,1fr))}}@media(max-width:700px){.merge-history-grid{grid-template-columns:1fr}}
.final-cut-grid{display:grid;grid-template-columns:repeat(auto-fill,minmax(300px,1fr));gap:14px}.final-cut-clip{display:grid;gap:10px;padding:12px}.final-cut-preview{position:relative;aspect-ratio:16/9;overflow:hidden;border-radius:9px;background:#08090b}.final-cut-preview video{width:100%;height:100%;object-fit:contain;background:#000}.final-cut-preview .preview-empty{height:100%}.final-cut-index{position:absolute;left:8px;top:8px;min-width:28px;padding:4px 7px;border-radius:999px;background:rgba(0,0,0,.75);color:#fff;text-align:center;font-weight:700}.final-cut-actions,.final-cut-insert{display:flex;gap:6px;align-items:center;flex-wrap:wrap}.final-cut-insert .input{min-width:150px;flex:1}.final-cut-clip .merge-info{display:grid;gap:3px}.final-cut-clip .merge-info span{font-size:12px;color:var(--text-tertiary)}
@media(max-width:900px){.workspace-tabs{position:static;display:flex;overflow-x:auto}.workspace-tab{min-width:150px;flex:0 0 auto}.workspace-tab small{display:none}}

.font-compliance{margin:6px 0 0;font-size:12px}.font-compliance.ok{color:#22c55e}.font-compliance.warn{color:#f59e0b}.screen-text-preflight{margin-bottom:14px;padding:14px}.screen-text-preflight-head{display:flex;align-items:center;gap:12px}.screen-text-preflight-head button{margin-left:auto}.screen-text-preflight p{margin:7px 0;color:var(--text-secondary);font-size:12px}.screen-text-warning{color:#f59e0b;font-size:12px}
.screen-text-track-head{display:flex;align-items:center;justify-content:space-between;margin-top:12px;color:var(--text-secondary)}
.screen-text-track{position:relative;min-width:700px;height:54px;margin-top:7px;border:1px solid var(--border);border-radius:8px;background:var(--surface-secondary);overflow:hidden}
.screen-text-cue{position:absolute;top:7px;height:38px;min-width:34px;display:grid;align-content:center;overflow:hidden;padding:3px 7px;border:1px solid var(--accent);border-radius:6px;background:var(--accent-soft);color:var(--text-primary);text-align:left;cursor:pointer}
.screen-text-cue span{font-size:9px;color:var(--accent)}.screen-text-cue strong{font-size:11px;white-space:nowrap;text-overflow:ellipsis;overflow:hidden}.screen-text-cue.cue-disabled{opacity:.45}.screen-text-empty{padding:17px;color:var(--text-tertiary);font-size:12px}
.screen-text-grid{display:grid;grid-template-columns:minmax(260px,.8fr) minmax(420px,1.4fr);gap:16px}.screen-text-list{display:grid;align-content:start;gap:7px;padding:12px}.screen-text-row{display:grid;grid-template-columns:auto 1fr auto;gap:8px;align-items:center;padding:9px;border:1px solid var(--border);border-radius:8px;background:var(--surface-secondary);color:var(--text-primary);cursor:pointer;text-align:left}.screen-text-row small{color:var(--text-tertiary)}.screen-text-form{grid-template-columns:repeat(2,minmax(0,1fr))}.screen-text-preview{position:relative;min-height:260px;overflow:hidden;border:1px dashed var(--border);border-radius:10px;background-color:#121318;background-position:center;background-size:cover;color:#f3dfb4;cursor:crosshair}.screen-text-safe-zone{position:absolute;inset:5%;border:1px dashed rgba(255,255,255,.38);pointer-events:none}.screen-text-preview-copy{position:absolute;display:flex;align-items:center;gap:12px;transform:translate(-50%,-50%);max-width:88%;max-height:88%;filter:drop-shadow(0 2px 3px #000);pointer-events:none}.screen-text-preview-copy.vertical{writing-mode:vertical-rl;text-orientation:upright;display:block;height:88%;max-height:88%;width:max-content}.screen-text-preview-copy.vertical strong,.screen-text-preview-copy.vertical small{display:block;white-space:nowrap;max-height:100%;overflow:visible}.screen-text-preview-copy.horizontal{writing-mode:horizontal-tb}.screen-text-rendered-layer{position:absolute;inset:0;width:100%;height:100%;object-fit:fill;pointer-events:none}.screen-text-render-note{margin:0;color:var(--text-secondary);font-size:12px}.screen-text-preview-copy strong{font-family:"Noto Sans SC",sans-serif;font-size:32px;letter-spacing:.16em}.screen-text-preview-copy small{font-size:var(--screen-subtext-size,16px);color:var(--screen-subtext-color,#d1b98b)}.color-input{min-height:40px;padding:4px}.screen-text-preview-copy.style-xianxia-character-vertical strong{font-size:38px;letter-spacing:.22em}.screen-text-preview-copy.style-xianxia-location-vertical strong{font-size:34px;letter-spacing:.2em}.screen-text-preview-copy.style-historical-time-card strong{font-size:36px}.screen-text-preview-copy.style-ink-transition-card strong{font-size:42px;letter-spacing:.24em}.screen-text-preview-copy.style-ink-end-card strong{font-size:50px;letter-spacing:.28em}.screen-text-preview-copy.style-modern-horizontal-caption strong{font-size:28px;letter-spacing:.08em;color:#fff}
@media (max-width:980px){.screen-text-grid{grid-template-columns:1fr}.screen-text-form{grid-template-columns:1fr}.screen-text-form .wide{grid-column:auto}}

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
.merge-opt input[type="checkbox"] { width: 14px; height: 14px; cursor: pointer; margin: 0; }
.merge-volume { gap: 6px; cursor: default; }
.merge-volume input[type="number"] {
  width: 76px;
  min-width: 76px;
  height: 34px;
  box-sizing: border-box;
  padding: 4px 6px;
  border: 1px solid var(--border);
  border-radius: 8px;
  background: var(--card);
  color: var(--text-primary);
  font: inherit;
  font-variant-numeric: tabular-nums;
  cursor: text;
}
.merge-volume input[type="number"]:focus { outline: 2px solid var(--accent); outline-offset: 1px; }

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
@media (max-width: 760px) { .inline-form, .audio-form,.relation-workbench { grid-template-columns: 1fr; }.compare-grid{grid-template-columns:1fr}.production-board{grid-template-columns:repeat(5,220px)} }
</style>
