import { useEffect, useMemo, useRef, useState } from 'react';
import { projectChoices, scenePromptChoices, type ProjectChoice } from './motion-prompt';
import { snapshotDirectorSource, type DirectorSourceSnapshot } from './directorSource';
import type { StudioExecutionContext } from './studioExecutionContext';
import { validateSceneRequirements, type SceneRequirements } from './sceneRequirements';
import { extractSceneryCandidates, SCENERY_VOCABULARY, type SourceSpan } from './sceneryCandidates';

type SceneChoice = { id: number; label: string };
async function readJSON(url: string, signal: AbortSignal): Promise<any> {
  const response = await fetch(url, { method: 'GET', signal });
  const value = await response.json();
  if (!response.ok) throw Error(value?.error || `读取失败 HTTP ${response.status}`);
  return value;
}
function findScene(project: any, id: number) {
  if (!Array.isArray(project?.scenes)) throw Error('项目详情缺少场次列表');
  const scene = project.scenes.find((row: any) => row?.id === id);
  if (!scene) throw Error('所选场次已移除，请重新选择');
  return scene;
}

/** Read/plan first; editor application is a separate, explicitly confirmed transaction. */
export type ApplyDirectorExecution = (plan: unknown, source: DirectorSourceSnapshot, isCurrent: () => boolean, requirements?: SceneRequirements) => Promise<void>;
export default function DirectorSourcePanel({ disabled, captureExecutionContext, onApplyExecution }: { disabled: boolean; captureExecutionContext?: () => Promise<StudioExecutionContext>; onApplyExecution?: ApplyDirectorExecution }) {
  const [projects, setProjects] = useState<ProjectChoice[]>([]);
  const [scenes, setScenes] = useState<SceneChoice[]>([]);
  const [project, setProject] = useState('');
  const [scene, setScene] = useState('');
  const [snapshot, setSnapshot] = useState<DirectorSourceSnapshot | null>(null);
  const [error, setError] = useState('');
  const [pending, setPending] = useState(false);
  const [execution, setExecution] = useState<any>(null);
  const [acknowledged, setAcknowledged] = useState(false);
  const [executionApplied, setExecutionApplied] = useState(false);
  const [requirementsJSON, setRequirementsJSON] = useState('');
  const candidates = useMemo(() => snapshot ? extractSceneryCandidates(snapshot.scene.content) : null, [snapshot]);
  const [sourceHighlight, setSourceHighlight] = useState<SourceSpan | null>(null);
  const sourceView = useRef<HTMLPreElement | null>(null);
  const locateSource = (evidence: SourceSpan) => setSourceHighlight({ ...evidence });
  useEffect(() => {
    const view = sourceView.current;
    if (!view || !sourceHighlight) return;
    const details = view.closest('details'); if (details) details.open = true;
    view.focus(); view.querySelector('mark')?.scrollIntoView?.({ block: 'nearest' });
  }, [sourceHighlight]);
  const requirementState = useMemo((): { review?: SceneRequirements; error?: string } => {
    if (!requirementsJSON.trim()) return {};
    try {
      if (requirementsJSON.length > 2 * 1024 * 1024) throw Error('建景要求超过2MiB，请拆分审校');
      const review = validateSceneRequirements(JSON.parse(requirementsJSON));
      if (!snapshot || review.sourceRevision !== snapshot.revision || review.sourceContent !== snapshot.scene.content) throw Error('建景要求来源已变化或原文不一致，请重新审校');
      if (review.unresolved.length) throw Error(`建景要求仍未解决：${review.unresolved.join('；')}`);
      return { review };
    } catch (cause) { return { error: `建景要求无效：${cause instanceof Error ? cause.message : String(cause)}` }; }
  }, [requirementsJSON, snapshot]);
  const loading = useRef<AbortController | null>(null);
  const generation = useRef(0);
  const invalidate = () => { generation.current++; loading.current?.abort(); setSnapshot(null); setSourceHighlight(null); setExecution(null); setAcknowledged(false); setExecutionApplied(false); setRequirementsJSON(''); setError(''); setPending(false); };

  useEffect(() => {
    const controller = new AbortController();
    readJSON('/api/projects', controller.signal).then(value => { if (!controller.signal.aborted) setProjects(projectChoices(value)); })
      .catch(cause => { if (!controller.signal.aborted) setError(String(cause.message || cause)); });
    return () => { controller.abort(); loading.current?.abort(); generation.current++; };
  }, []);
  useEffect(() => {
    const controller = new AbortController();
    if (project) readJSON(`/api/projects/${project}`, controller.signal).then(value => {
      if (!controller.signal.aborted) setScenes(scenePromptChoices(value));
    }).catch(cause => { if (!controller.signal.aborted) setError(String(cause.message || cause)); });
    return () => controller.abort();
  }, [project]);
  useEffect(() => { if (disabled) invalidate(); }, [disabled]);

  async function inspect() {
    if (!project || !scene || disabled || pending) return;
    invalidate();
    const ticket = generation.current;
    const controller = new AbortController(); loading.current = controller;
    const current = () => !controller.signal.aborted && generation.current === ticket;
    setPending(true);
    try {
      const base = `/api/projects/${project}`;
      const before = findScene(await readJSON(base, controller.signal), Number(scene));
      const shots = await readJSON(`${base}/scenes/${scene}/shots`, controller.signal);
      const after = findScene(await readJSON(base, controller.signal), Number(scene));
      // The API has no transactional snapshot endpoint. At least reject observed
      // scene revision changes across reads; this is not an atomic DB guarantee.
      const first = await snapshotDirectorSource(Number(project), before, shots);
      const latest = await snapshotDirectorSource(Number(project), after, shots);
      if (first.revision !== latest.revision) throw Error('读取期间场次已变化，请重新读取导演来源');
      if (current()) setSnapshot(latest);
    } catch (cause) {
      if (current()) setError(cause instanceof Error ? cause.message : String(cause));
    } finally { if (current()) setPending(false); }
  }

  async function planExecution() {
    if (!snapshot || !captureExecutionContext || disabled || pending) return;
    const ticket = ++generation.current;
    loading.current?.abort();
    const controller = new AbortController(); loading.current = controller;
    const current = () => !controller.signal.aborted && generation.current === ticket;
    setPending(true); setError(''); setExecution(null); setAcknowledged(false); setExecutionApplied(false); setRequirementsJSON('');
    try {
      const base = `/api/projects/${project}/scenes/${scene}/studio-execution`;
      const authoritative = await readJSON(`${base}/source`, controller.signal);
      // Use the already reviewed scene with the authoritative shot payload to compare
      // every shot field without treating project-only computed Scene fields as edits.
      const checked = await snapshotDirectorSource(Number(project), snapshot.scene, authoritative.source?.shots);
      if (checked.revision !== snapshot.revision || authoritative.source?.scene?.content !== snapshot.scene.content || authoritative.source?.scene?.updated_at !== snapshot.scene.updated_at || authoritative.source?.scene?.director_source_stale !== false) throw Error('导演来源已变化，请重新读取并审阅');
      const context = await captureExecutionContext();
      if (!current()) return;
      const body = JSON.stringify({ source_hash: authoritative.source_hash, context });
      if (new TextEncoder().encode(body).byteLength > 2 * 1024 * 1024) throw Error('完整资源快照超过2MiB，本次不发送、不截断目录');
      const response = await fetch(`${base}/plan`, { method: 'POST', signal: controller.signal, headers: { 'Content-Type': 'application/json' }, body });
      const value = await response.json();
      if (!response.ok) throw Error(value?.error || `规划失败 HTTP ${response.status}`);
      if (!current()) return;
      const latest = await captureExecutionContext();
      if (!current()) return;
      if (latest.revision !== context.revision) throw Error('规划期间3D场景或资源已变化，草稿已丢弃');
      if (!value.plan || value.plan.source_hash !== authoritative.source_hash || value.plan.context_revision !== context.revision || value.persisted !== false || value.applied !== false) throw Error('执行草稿缺少一致的来源或未应用状态');
      setExecution(value);
    } catch (cause) { if (current()) setError(cause instanceof Error ? cause.message : String(cause)); }
    finally { if (current()) setPending(false); }
  }

  async function applyExecution() {
    if (!execution?.plan || !snapshot || !onApplyExecution || !acknowledged || disabled || pending || executionApplied || requirementState.error) return;
    const ticket = ++generation.current;
    const current = () => generation.current === ticket;
    setPending(true); setError(''); setAcknowledged(false);
    try {
      if (requirementState.review) await onApplyExecution(execution.plan, snapshot, current, requirementState.review);
      else await onApplyExecution(execution.plan, snapshot, current);
      if (current()) setExecutionApplied(true);
    } catch (cause) { if (current()) setError(cause instanceof Error ? cause.message : String(cause)); }
    finally { if (current()) setPending(false); }
  }

  return <section aria-label="已有导演结果来源" className="space-y-2 border-t border-zinc-700 pt-2">
    <h3>小说预演 · 复用已有导演结果</h3>
    <p>复用已有小说理解与导演结果。规划按钮只调用文本模型生成待审3D执行草稿，不提交图像或视频，不改当前场景。</p>
    <label>来源项目<select aria-label="导演来源项目" value={project} disabled={disabled} onChange={event => { invalidate(); setProject(event.target.value); setScene(''); setScenes([]); }} className="w-full bg-zinc-800">
      <option value="">请选择项目</option>{projects.map(item => <option key={item.id} value={item.id}>{item.label}</option>)}
    </select></label>
    <label>来源场次<select aria-label="导演来源场次" value={scene} disabled={disabled || !project} onChange={event => { invalidate(); setScene(event.target.value); }} className="w-full bg-zinc-800">
      <option value="">请选择场次</option>{scenes.map(item => <option key={item.id} value={item.id}>{item.label}</option>)}
    </select></label>
    <button type="button" disabled={disabled || pending || !project || !scene} onClick={() => void inspect()}>读取已有导演结果</button>
    {pending && <p role="status">正在读取导演来源…</p>}
    {error && <p role="alert">{error}</p>}
    {snapshot && <div aria-label="导演来源快照" className="space-y-1">
      <p role="status">已读取 {snapshot.shots.length} 镜 · {snapshot.shots.reduce((sum, shot) => sum + shot.duration, 0).toFixed(2)} 秒 · {executionApplied ? '已由单独确认应用到编辑场景' : '未修改当前3D场景'}</p>
      <p>{snapshot.scene.title} · {snapshot.scene.location}</p>
      <p className="whitespace-pre-wrap">{snapshot.scene.content}</p>
      <p className="break-all">来源修订：{snapshot.revision}</p>
      {candidates && <section aria-label="离线原文建景候选" className="space-y-2 border border-zinc-700 p-2">
        <h4>离线原文候选 · 未完成审校</h4>
        <p>{candidates.status === 'unavailable' ? '本次无法分析；请人工审阅原文。' : `${candidates.mentions.length} 次提及 · ${candidates.relations.length} 条关系候选`}</p>
        <p>只扫描本次快照的场次正文，不读标题、地点栏或导演补充。不调用模型，不修改场景或人工建景 JSON，不清空未解决项。候选不等于事实，也不代表全文已理解。</p>
        <p>数量为“数词＋量词＋词表名词”的字面线索（中文一至九十九、阿拉伯正整数至4096），不代表场景实例数；一片竹林不是一株竹子。重复提及不合并、不累加。</p>
        <details><summary>规则词表与覆盖边界</summary><p>{SCENERY_VOCABULARY.join('、')}</p><p>仅局部明确端点支持“在/位于…附近/近岸/对岸”和“通向/通往/连接”。词表外名词、指代、跨句关系及复杂数量须人工补充；当前没有语义完整性保证。</p></details>
        <details><summary>原文定位（只读 UTF-16 [start,end)）</summary>
          <pre aria-label="候选原文定位" ref={sourceView} tabIndex={-1} className="max-h-60 overflow-auto whitespace-pre-wrap break-all">{sourceHighlight ? <>{snapshot.scene.content.slice(0, sourceHighlight.start)}<mark data-source-start={sourceHighlight.start} data-source-end={sourceHighlight.end}>{snapshot.scene.content.slice(sourceHighlight.start, sourceHighlight.end)}</mark>{snapshot.scene.content.slice(sourceHighlight.end)}</> : snapshot.scene.content}</pre>
        </details>
        <details open><summary>地点与物件提及（未绑定实际对象）</summary>
          <ol className="max-h-80 space-y-2 overflow-auto">{candidates.mentions.map((item, index) => <li key={item.id} data-scenery-mention={item.label}>
            <p>{index + 1}. {item.label} · {item.kind === 'place' ? '地点词' : '物件词'} · 字面数量：{item.quantity === null ? '未确认' : `${item.quantity} ${item.classifier}`}</p>
            <p>UTF-16 [{item.evidence.start},{item.evidence.end}) <code className="whitespace-pre-wrap">{item.evidence.quote}</code></p>
            <button type="button" disabled={disabled} onClick={() => locateSource(item.evidence)}>定位提及 {index + 1}</button>
            {item.concerns.map(concern => <p key={concern}>{concern}</p>)}
            <details><summary>原始语境</summary><pre className="whitespace-pre-wrap break-all">{item.context.quote}</pre></details>
          </li>)}</ol>
        </details>
        <details><summary>空间关系候选（不是几何证明）</summary>
          <ol className="max-h-80 space-y-2 overflow-auto">{candidates.relations.map((item, index) => <li key={item.id}>
            <p>{index + 1}. {item.kind === 'leads-to' ? '通向线索' : item.kind === 'across-reference' ? '对岸线索' : '附近/近岸线索'} · UTF-16 [{item.evidence.start},{item.evidence.end})</p>
            <code className="whitespace-pre-wrap">{item.evidence.quote}</code>
            <button type="button" disabled={disabled} onClick={() => locateSource(item.evidence)}>定位关系 {index + 1}</button>
            {item.concerns.map(concern => <p key={concern}>{concern}</p>)}
          </li>)}</ol>
        </details>
        <details open><summary>缺项与歧义提示（未完成）</summary><ul>{candidates.issues.map((issue, index) => <li key={index} className="whitespace-pre-wrap break-all">{issue}</li>)}</ul></details>
        <p>确认语义、数量和身份后，仍须在高级人工建景审校中绑定真实对象并填写距离/坐标/推定理由；本区不会生成可执行要求或更改应用确认。</p>
      </section>}
      {snapshot.shots.map(shot => <details key={shot.id}>
        <summary>镜 {shot.order} · ID {shot.id} · {shot.duration} 秒 · {String(shot.shot_type || '未标注景别')}</summary>
        <pre className="whitespace-pre-wrap break-all">{JSON.stringify(shot, null, 2)}</pre>
      </details>)}
      <p>{executionApplied ? '已接入布景与镜头表；请播放审阅并保存。' : '布景与角色资产、动作clip和空间机位尚未绑定；这里的动作与机位文字不代表已经可播放。'}执行草稿只通过结构和目录引用校验，不代表剧情、碰撞或视觉验收通过。</p>
      <button type="button" disabled={disabled || pending || !captureExecutionContext} onClick={() => void planExecution()}>生成待审3D执行草稿（文本模型）</button>
      {!captureExecutionContext && <p>视口尚未提供真实资源快照，不能规划。</p>}
      {execution !== null && <div aria-label="待审3D执行草稿">
        <p role="status">{executionApplied ? '已确认应用到编辑场景 · 未自动播放、保存或导出' : '已生成待审执行草稿 · 未应用到场景 · 尚未绑定播放'}</p>
        <pre className="whitespace-pre-wrap break-all">{JSON.stringify(execution, null, 2)}</pre>
        {onApplyExecution && !executionApplied && <>
          <details aria-label="人工建景审校入口">
            <summary>人工建景要求（高级 JSON，可选）</summary>
            <p>这不是小说自动抽取。逐项填写物件数量和草稿中的对象ID；fact 使用上方原文的 UTF-16 [start,end) 精确引用，assumption 必须写 reason。near 的 maxMetres、opposite-sides 的 x/z 轴均为人工布景选择，须写 stagingNote；未解决项不得清空冒充完成。</p>
            <p>道路可声明 path-connected：subject 绑定路面物件，reference/other 为起终点地点；points 按顺序填写世界坐标 [x,y,z]。检查实际路面是否支撑连续中心线及端点是否接近地点表面，拒绝断口和第三方障碍；地形/水域为取样检查，不证明路宽、整身通行、涉水或全文覆盖。坐标、端点距离须写人工 stagingNote；不会自动加路或替你补齐关系。</p>
            <pre aria-label="道路连通关系格式示例" className="whitespace-pre-wrap break-all">{JSON.stringify({ id: 'camp-to-bank', kind: 'path-connected', subject: 'road', reference: 'camp', other: 'bank', points: [[-2, 0, 0], [2, 0, 0]], maxEndpointGapMetres: .5, stagingNote: '仅格式示例，须替换为本场景实测坐标、端点距离及物件映射', basis: { kind: 'assumption', reason: '示例不是小说事实，请逐条核对原文后替换' } }, null, 2)}</pre>
            <button type="button" disabled={disabled || pending} onClick={() => {
              if (requirementsJSON.trim()) return;
              setRequirementsJSON(JSON.stringify({ version: 1, sourceRevision: snapshot.revision, sourceContent: snapshot.scene.content,
                entities: [{ id: 'entity-1', label: '待填写物件', count: 1, objectIds: ['待填写对象ID'], basis: { kind: 'assumption', reason: '待填写布景推定理由；有原文证据则改为fact及start/end/quote' } }], relations: [], unresolved: ['请逐项核对原文、物件及空间关系，完成前保留此项'] }, null, 2));
              setAcknowledged(false);
            }}>初始化建景审校模板（未完成）</button>
            <label>完整建景要求 JSON<textarea aria-label="人工建景要求 JSON" value={requirementsJSON} disabled={disabled || pending} rows={14} className="w-full bg-zinc-800 font-mono" onChange={event => { setRequirementsJSON(event.target.value); setAcknowledged(false); }} /></label>
            <p>事实依据格式：{JSON.stringify({ kind: 'fact', start: 0, end: snapshot.scene.content.length, quote: snapshot.scene.content })}。原文匹配只证明引用存在，不证明物件含义或全文无遗漏。</p>
          </details>
          {requirementState.error && <p role="alert" className="whitespace-pre-wrap break-all">{requirementState.error}</p>}
          <p role="status">{requirementState.review ? `已声明 ${requirementState.review.entities.length} 项物件、${requirementState.review.relations.length} 项关系；应用时检查真实几何，不代表全文完整还原` : '未附有效原文建景要求，场景忠实度未核验'}</p>
          <label><input type="checkbox" aria-label="确认应用导演执行草稿" checked={acknowledged} disabled={disabled || pending} onChange={event => setAcknowledged(event.target.checked)} />已审阅原文依据、导演补充与阻塞；确认追加布景、更新复用对象并替换当前镜头表。存在执行阻塞时不会应用。</label>
          <button type="button" disabled={disabled || pending || !acknowledged || !!requirementState.error} onClick={() => void applyExecution()}>确认应用到3D编辑场景</button>
        </>}
      </div>}
    </div>}
  </section>;
}
