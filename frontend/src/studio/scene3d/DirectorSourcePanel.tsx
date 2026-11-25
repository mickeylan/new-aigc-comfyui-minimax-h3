import { useEffect, useRef, useState } from 'react';
import { projectChoices, scenePromptChoices, type ProjectChoice } from './motion-prompt';
import { snapshotDirectorSource, type DirectorSourceSnapshot } from './directorSource';
import type { StudioExecutionContext } from './studioExecutionContext';

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
export type ApplyDirectorExecution = (plan: unknown, source: DirectorSourceSnapshot, isCurrent: () => boolean) => Promise<void>;
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
  const loading = useRef<AbortController | null>(null);
  const generation = useRef(0);
  const invalidate = () => { generation.current++; loading.current?.abort(); setSnapshot(null); setExecution(null); setAcknowledged(false); setExecutionApplied(false); setError(''); setPending(false); };

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
    setPending(true); setError(''); setExecution(null); setAcknowledged(false); setExecutionApplied(false);
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
    if (!execution?.plan || !snapshot || !onApplyExecution || !acknowledged || disabled || pending || executionApplied) return;
    const ticket = ++generation.current;
    const current = () => generation.current === ticket;
    setPending(true); setError(''); setAcknowledged(false);
    try {
      await onApplyExecution(execution.plan, snapshot, current);
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
          <label><input type="checkbox" aria-label="确认应用导演执行草稿" checked={acknowledged} disabled={disabled || pending} onChange={event => setAcknowledged(event.target.checked)} />已审阅原文依据、导演补充与阻塞；确认追加布景、更新复用对象并替换当前镜头表。存在执行阻塞时不会应用。</label>
          <button type="button" disabled={disabled || pending || !acknowledged} onClick={() => void applyExecution()}>确认应用到3D编辑场景</button>
        </>}
      </div>}
    </div>}
  </section>;
}
