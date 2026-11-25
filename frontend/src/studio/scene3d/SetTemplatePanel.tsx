import { useEffect, useRef, useState } from 'react';
import type { ScriptCharacterBinding, ScriptSceneDraft, ScriptSceneIndex, ScriptShot, ScriptShotCamera, ScriptShotMotion, ScriptShotMotionTarget, ScriptShotPlan, SetDraft } from './toonflowSets';
type Engine = typeof import('./toonflowSets');
type Entries = ReturnType<Engine['materializeSetDraft']>;
type BindingChoice = { kind: 'unbound' } | { kind: 'set-person'; objectId: string } | { kind: 'placeholder'; x: number; z: number; rig?: 'skeletal' };

const TIME_LABELS: Record<string, string> = { day: '日', dawn: '晨', dusk: '黄昏', night: '夜', unknown: '未标注时间' };
const POSE_LABELS: Record<string, string> = { person: '站', seatedPerson: '坐', kneelingPerson: '跪' };
const bindingValue = (choice: BindingChoice) => choice.kind === 'unbound' ? '' : choice.kind === 'placeholder' ? (choice.rig === 'skeletal' ? 'skeletal' : 'placeholder') : `set:${choice.objectId}`;
type MotionForm = { targetId: string; startX: string; startZ: string; endX: string; endZ: string; duration: string; animationIndex: string };
const motionForm = (motion?: ScriptShotMotion | null): MotionForm => ({
  targetId: motion?.targetId ?? '',
  startX: String(motion?.start.x ?? 0), startZ: String(motion?.start.z ?? 0),
  endX: String(motion?.end.x ?? 0), endZ: String(motion?.end.z ?? 0),
  duration: String(motion?.duration ?? 1), animationIndex: motion?.animation ? String(motion.animation.index) : '',
});

/** 一镜走位的人工输入：对象、起止 XZ、时长与可选骨骼 clip 都必须由人选定。 */
function ShotMotionEditor({ shot, targets, disabled, pending, onSet, onClear }: {
  shot: ScriptShot;
  targets: ScriptShotMotionTarget[];
  disabled: boolean;
  pending: boolean;
  onSet: (motion: ScriptShotMotion) => void;
  onClear: () => void;
}) {
  const [form, setForm] = useState<MotionForm>(() => motionForm(shot.motion));
  const [error, setError] = useState('');
  useEffect(() => { setForm(motionForm(shot.motion)); setError(''); }, [shot.id, shot.motion]);
  const target = targets.find(item => item.id === form.targetId);
  const animations = target?.animations ?? [];
  const patch = (part: Partial<MotionForm>) => setForm(current => ({ ...current, ...part }));
  const apply = () => {
    const clip = form.animationIndex === '' ? undefined : animations.find(item => item.index === Number(form.animationIndex));
    if (form.animationIndex !== '' && !clip) { setError('所选骨骼动作已变化，请重新选择'); return; }
    setError('');
    onSet({
      targetId: form.targetId,
      targetName: target?.name ?? '',
      start: { x: Number(form.startX), z: Number(form.startZ) },
      end: { x: Number(form.endX), z: Number(form.endZ) },
      duration: Number(form.duration),
      ...(clip ? { animation: { index: clip.index, name: clip.name, duration: clip.duration } } : {}),
    });
  };
  return <div className="space-y-1 rounded border border-zinc-800 p-1" aria-label={`镜 ${shot.order} 走位`}>
    {error && <p role="alert">{error}</p>}
    <p className="text-zinc-400">{shot.motion ? `已绑定走位：${shot.motion.targetName} · ${shot.motion.start.x},${shot.motion.start.z} → ${shot.motion.end.x},${shot.motion.end.z} · ${shot.motion.duration.toFixed(2)} 秒${shot.motion.animation ? ` · 动作 ${shot.motion.animation.name}` : ' · 无骨骼动作'}` : '未绑定走位（不会自动走位）'}</p>
    <label className="flex items-center gap-1">
      <span className="w-16 shrink-0">对象</span>
      <select aria-label={`镜 ${shot.order} 走位对象`} value={form.targetId} disabled={disabled || pending} onChange={event => patch({ targetId: event.target.value, animationIndex: '' })} className="w-full bg-zinc-800 p-1">
        <option value="">未选择</option>
        {targets.map(item => <option key={item.id} value={item.id}>{item.name}{item.visible ? '' : '（隐藏）'}{item.locked ? '（锁定）' : ''}</option>)}
      </select>
    </label>
    <div className="flex flex-wrap gap-1">
      <label>起点X<input aria-label={`镜 ${shot.order} 走位起点X`} type="number" step="0.1" value={form.startX} disabled={disabled || pending} onChange={event => patch({ startX: event.target.value })} className="w-16 bg-zinc-800" /></label>
      <label>起点Z<input aria-label={`镜 ${shot.order} 走位起点Z`} type="number" step="0.1" value={form.startZ} disabled={disabled || pending} onChange={event => patch({ startZ: event.target.value })} className="w-16 bg-zinc-800" /></label>
      <label>终点X<input aria-label={`镜 ${shot.order} 走位终点X`} type="number" step="0.1" value={form.endX} disabled={disabled || pending} onChange={event => patch({ endX: event.target.value })} className="w-16 bg-zinc-800" /></label>
      <label>终点Z<input aria-label={`镜 ${shot.order} 走位终点Z`} type="number" step="0.1" value={form.endZ} disabled={disabled || pending} onChange={event => patch({ endZ: event.target.value })} className="w-16 bg-zinc-800" /></label>
    </div>
    <div className="flex flex-wrap gap-1">
      <label>时长（秒）<input aria-label={`镜 ${shot.order} 走位时长`} type="number" step="0.1" min={0.1} value={form.duration} disabled={disabled || pending} onChange={event => patch({ duration: event.target.value })} className="w-16 bg-zinc-800" /></label>
      <label className="flex-1">骨骼动作<select aria-label={`镜 ${shot.order} 骨骼动作`} value={form.animationIndex} disabled={disabled || pending || animations.length === 0} onChange={event => patch({ animationIndex: event.target.value })} className="w-full bg-zinc-800 p-1">
        <option value="">不绑定骨骼动作</option>
        {animations.map(clip => <option key={`${clip.index}-${clip.name}`} value={String(clip.index)}>{clip.name} · {clip.duration.toFixed(2)} 秒</option>)}
      </select></label>
    </div>
    <p className="text-zinc-500">XZ 直线位移，有地形时 Y 跟随高度场，否则保留当前高度；坡度或水域不可走、遇静态障碍时显式拒绝，不自动绕行。骨骼动作只作局部预演，目标须由人选定，不按名字猜。</p>
    <div className="flex gap-2">
      <button type="button" aria-label={`镜 ${shot.order} 应用走位`} disabled={disabled || pending || !form.targetId} onClick={apply}>应用走位</button>
      <button type="button" aria-label={`镜 ${shot.order} 清除走位`} disabled={disabled || pending || !shot.motion} onClick={onClear}>清除走位</button>
    </div>
  </div>;
}

export default function SetTemplatePanel({ disabled, onApply, onCaptureCamera, onPreviewShot, onApplyShots, appliedPlan, onPlayShots, onStopShots, shotPlayback, onExportShotPack, listMotionTargets }: {
  disabled: boolean;
  onApply: (draft: SetDraft, entries: Entries, x: number, z: number) => void;
  /** 记录当前视口机位；场景没接视口时保持 undefined，界面不会假装能记机位。 */
  onCaptureCamera?: () => ScriptShotCamera;
  /** 用现有视口相机预览某一镜，不新建第二个预览器。 */
  onPreviewShot?: (camera: ScriptShotCamera) => void;
  /** 把镜头表接入场景状态，随现有存档保存。 */
  onApplyShots?: (plan: ScriptShotPlan) => void;
  /** 场景里已保存的镜头表；只用于复核，不自动替换正在编辑的计划。 */
  appliedPlan?: ScriptShotPlan | null;
  /** 按已保存镜头表连续逐镜播放；场景持有视口，并负责停止与还原播放前的机位。 */
  onPlayShots?: (plan: ScriptShotPlan) => void;
  /** 停止播放并还原播放前的视口机位。 */
  onStopShots?: () => void;
  /** 当前播放到的镜次（下标）与总镜数；null 表示未播放。 */
  shotPlayback?: { index: number; shots: number; line?: string | null } | null;
  /** 按已保存镜头表在视口逐镜渲参考图并本地打包下载；场景负责采样与还原视口，失败不得产出部分包。返回 false 表示没有产出下载，界面保留本次确认。 */
  onExportShotPack?: (plan: ScriptShotPlan) => void | boolean | Promise<void | boolean>;
  /** 当前场景可被人工选作走位目标的真实对象；不提供就无法在应用/播放前校验目标。 */
  listMotionTargets?: () => ScriptShotMotionTarget[];
}) {
  const [engine, setEngine] = useState<Engine | null>(null);
  const [templateId, setTemplateId] = useState('ancientStreet');
  const [draft, setDraft] = useState<SetDraft | null>(null);
  const [error, setError] = useState('');
  const [pending, setPending] = useState(false);
  const [x, setX] = useState(0); const [z, setZ] = useState(0);
  const controller = useRef<AbortController | null>(null);
  const [scriptText, setScriptText] = useState('');
  const [index, setIndex] = useState<ScriptSceneIndex | null>(null);
  const [sceneId, setSceneId] = useState('');
  const [scriptTemplateId, setScriptTemplateId] = useState('');
  const [scriptDraft, setScriptDraft] = useState<ScriptSceneDraft | null>(null);
  const [bindings, setBindings] = useState<Record<string, BindingChoice>>({});
  const [acknowledged, setAcknowledged] = useState(false);
  const [shotPlan, setShotPlan] = useState<ScriptShotPlan | null>(null);
  const [shotAcknowledged, setShotAcknowledged] = useState(false);
  const [shotNote, setShotNote] = useState('');
  const [packAcknowledged, setPackAcknowledged] = useState(false);
  const [packPending, setPackPending] = useState(false);
  const scriptController = useRef<AbortController | null>(null);
  // 已保存镜头表一换（重新接入或载入别的存档），上次的参考包确认就作废。
  useEffect(() => { setPackAcknowledged(false); }, [appliedPlan]);
  useEffect(() => {
    let active = true;
    import('./toonflowSets').then(value => { if (active) setEngine(value); }).catch(cause => { if (active) setError(String(cause)); });
    return () => { active = false; controller.current?.abort(); scriptController.current?.abort(); };
  }, []);
  /** 两条入口共用：先物化条目，再交给场景原子提交；提交失败时回收已建几何。 */
  function commit(target: SetDraft, centerX: number, centerZ: number) {
    if (!engine) throw new Error('布景模块尚未加载完成，请稍后重试');
    const entries = engine.materializeSetDraft(target);
    try { onApply(target, entries, centerX, centerZ); }
    catch (cause) {
      entries.forEach(entry => entry.object.traverse((child: any) => {
        child.geometry?.dispose();
        const materials = Array.isArray(child.material) ? child.material : child.material ? [child.material] : [];
        materials.forEach((material: any) => material.dispose());
      }));
      throw cause;
    }
  }
  const centerBlocker = () => (![x, z].every(Number.isFinite) || Math.max(Math.abs(x), Math.abs(z)) > 1000 ? '布景中心坐标须在 -1000 到 1000 米内' : '');
  /** 剧本、场次、模板或草稿一换，旧镜头表就作废；不悄悄沿用。 */
  function resetShots() { setShotPlan(null); setShotAcknowledged(false); setShotNote(''); }
  async function prepare() {
    if (!engine || disabled || pending) return;
    controller.current?.abort();
    const operation = new AbortController(); controller.current = operation;
    setDraft(null); setError(''); setPending(true);
    try {
      const value = await engine.prepareSetDraft(templateId, operation.signal);
      if (!operation.signal.aborted) setDraft(value);
    } catch (cause) { if (!operation.signal.aborted) setError(cause instanceof Error ? cause.message : String(cause)); }
    finally { if (!operation.signal.aborted) setPending(false); }
  }
  function apply() {
    if (!engine || !draft || disabled || pending) return;
    const blocker = centerBlocker();
    if (blocker) { setError(blocker); return; }
    setError(''); setPending(true);
    try {
      // The receiver takes ownership only after an atomic scene commit.
      commit(draft, x, z);
      setDraft(null);
    } catch (cause) { setError(cause instanceof Error ? cause.message : String(cause)); }
    finally { setPending(false); }
  }
  function parseScript() {
    if (!engine || disabled || pending) return;
    setError(''); setIndex(null); setSceneId(''); setScriptDraft(null); setBindings({}); setAcknowledged(false); resetShots();
    if (!scriptText.trim()) { setError('请先粘贴或输入剧本原文'); return; }
    try {
      const parsed = engine.parseScriptScenes(scriptText);
      setIndex(parsed);
      if (!parsed.explicitHeadings) setError(parsed.diagnostics.join('；') || '剧本没有可识别的场次标记');
    } catch (cause) { setError(cause instanceof Error ? cause.message : String(cause)); }
  }
  async function prepareScript() {
    if (!engine || !index || disabled || pending) return;
    scriptController.current?.abort();
    const operation = new AbortController(); scriptController.current = operation;
    setScriptDraft(null); setBindings({}); setAcknowledged(false); resetShots(); setError(''); setPending(true);
    try {
      const value = await engine.buildScriptSceneDraft(index, sceneId, operation.signal, scriptTemplateId || undefined);
      if (!operation.signal.aborted) {
        setScriptDraft(value);
        // 每个角色初始都是“未绑定”，由人工逐项选择，不预填、不按姓名匹配。
        setBindings(Object.fromEntries(value.review.characters.map(character => [character, { kind: 'unbound' } as BindingChoice])));
      }
    } catch (cause) { if (!operation.signal.aborted) setError(cause instanceof Error ? cause.message : String(cause)); }
    finally { if (!operation.signal.aborted) setPending(false); }
  }
  function setCharacterBinding(character: string, value: string) {
    setAcknowledged(false);
    const choice: BindingChoice = value === '' ? { kind: 'unbound' } : (value === 'placeholder' || value === 'skeletal') ? { kind: 'placeholder', x: 0, z: 0, ...(value === 'skeletal' ? { rig: 'skeletal' as const } : {}) } : { kind: 'set-person', objectId: value.slice(4) };
    setBindings(current => ({ ...current, [character]: choice }));
  }
  function movePlaceholder(character: string, axis: 'x' | 'z', value: number) {
    setAcknowledged(false);
    setBindings(current => {
      const choice = current[character];
      return choice?.kind === 'placeholder' ? { ...current, [character]: { ...choice, [axis]: value } } : current;
    });
  }
  const scriptPersons = scriptDraft && engine ? engine.listSetPersonObjects(scriptDraft.setDraft) : [];
  // 场景对象列表由视口按需提供；只用于人工下拉，不按名称自动选中。
  const motionTargets = listMotionTargets?.() ?? [];
  const unboundCharacters = scriptDraft ? scriptDraft.review.characters.filter(character => (bindings[character]?.kind ?? 'unbound') === 'unbound') : [];
  /** 生成镜头表：每句台词一镜，机位全部留空由人工记录，不猜机位也不推断表演。 */
  function generateShots() {
    if (!engine || !scriptDraft || disabled || pending) return;
    try {
      setShotPlan(engine.buildScriptShotPlan(scriptDraft));
      setShotAcknowledged(false); setShotNote(''); setError('');
    } catch (cause) { setError(cause instanceof Error ? cause.message : String(cause)); }
  }
  function editShotDuration(shotId: string, seconds: number) {
    if (!engine || !shotPlan) return;
    try {
      // 改时长只顺延后续镜头；装不下台词或越界都会报错，不压缩台词。
      setShotPlan(engine.updateScriptShotDuration(shotPlan, shotId, seconds));
      setShotAcknowledged(false); setError('');
    } catch (cause) { setError(cause instanceof Error ? cause.message : String(cause)); }
  }
  function captureShotCamera(shotId: string) {
    if (!engine || !shotPlan) return;
    try {
      if (!onCaptureCamera) throw new Error('当前场景没有接入视口，无法记录机位');
      setShotPlan(engine.setScriptShotCamera(shotPlan, shotId, onCaptureCamera()));
      setShotAcknowledged(false); setError('');
    } catch (cause) { setError(cause instanceof Error ? cause.message : String(cause)); }
  }
  function previewShot(camera: ScriptShotCamera | null) {
    if (!camera) { setError('这一镜还没有记录机位，无法预览'); return; }
    try {
      if (!onPreviewShot) throw new Error('当前场景没有接入视口预览');
      onPreviewShot(camera); setError('');
    } catch (cause) { setError(cause instanceof Error ? cause.message : String(cause)); }
  }
  function clearShotCamera(shotId: string) {
    if (!engine || !shotPlan) return;
    try { setShotPlan(engine.clearScriptShotCamera(shotPlan, shotId)); setShotAcknowledged(false); setError(''); }
    catch (cause) { setError(cause instanceof Error ? cause.message : String(cause)); }
  }
  /** 人工绑定一镜走位：目标、坐标、时长与骨骼 clip 都由界面显式传入，模块只校验不猜测。 */
  function bindShotMotion(shotId: string, motion: ScriptShotMotion) {
    if (!engine || !shotPlan) return;
    if (!motion.targetName) { setError('请先选择场景里的走位对象；不会按角色姓名或对象名称自动猜'); return; }
    try {
      setShotPlan(engine.setScriptShotMotion(shotPlan, shotId, motion));
      setShotAcknowledged(false); setError('');
    } catch (cause) { setError(cause instanceof Error ? cause.message : String(cause)); }
  }
  function unbindShotMotion(shotId: string) {
    if (!engine || !shotPlan) return;
    try { setShotPlan(engine.clearScriptShotMotion(shotPlan, shotId)); setShotAcknowledged(false); setError(''); }
    catch (cause) { setError(cause instanceof Error ? cause.message : String(cause)); }
  }
  /** 计划含走位时必须先对当前场景对象校验；缺目标、隐藏、锁定或 clip 变化都在应用/播放前报出来。 */
  function verifyMotionTargets(plan: ScriptShotPlan): ScriptShotPlan {
    if (!engine || !plan.shots.some(shot => shot.motion)) return plan;
    if (!listMotionTargets) throw new Error('当前场景没有接入对象列表，无法校验走位目标');
    return engine.assertScriptShotMotionTargets(plan, listMotionTargets());
  }
  /** 只播放已保存在场景里的镜头表；缺任何一镜机位都由模块显式拒绝，不借用别的镜头。 */
  function playAppliedShots() {
    if (!engine || !appliedPlan || disabled || pending) return;
    try {
      const ready = verifyMotionTargets(engine.assertScriptShotPlaybackReady(appliedPlan));
      if (!onPlayShots) throw new Error('当前场景没有接入视口，无法连续逐镜播放');
      onPlayShots(ready); setError('');
    } catch (cause) { setError(cause instanceof Error ? cause.message : String(cause)); }
  }
  /**
   * 逐镜参考包：只以场景里已确认的镜头表为准，先用既有就绪/来源检查，再交给视口采样导出。
   * 缺机位、来源变更或没有视口都在这里拦住，不产出一半的包。
   */
  async function exportShotPack() {
    if (!engine || !appliedPlan || disabled || pending || packPending) return;
    if (!packAcknowledged) { setError('请先核对逐镜机位、采样时刻与导出清单并勾选确认'); return; }
    try {
      const ready = engine.assertScriptShotReferenceReady(appliedPlan);
      if (scriptDraft) engine.assertScriptShotPlanSource(ready, scriptDraft);
      if (!onExportShotPack) throw new Error('当前场景没有接入视口，无法导出逐镜参考包');
      setError(''); setPackPending(true);
      const produced = await onExportShotPack(ready);
      // 没有产出下载时不假装成功，也不得替用户重新勾上确认。
      if (produced !== false) setPackAcknowledged(false);
    } catch (cause) { setError(cause instanceof Error ? cause.message : String(cause)); }
    finally { setPackPending(false); }
  }
  function loadAppliedShots() {
    if (!appliedPlan) return;
    setShotPlan(appliedPlan); setShotAcknowledged(false); setError('');
    const mismatch = scriptDraft && (appliedPlan.sceneId !== scriptDraft.sceneId || appliedPlan.sourceFingerprint !== scriptDraft.review.sourceFingerprint);
    setShotNote(mismatch
      ? `该镜头表来自存档（${appliedPlan.sceneId}），与当前草稿不同源；仅作复核，应用会被来源检查拦下。`
      : `已载入存档镜头表（${appliedPlan.shots.length} 镜）；改时长或机位后需重新勾选确认。`);
  }
  function applyShots() {
    if (!engine || !shotPlan || disabled || pending) return;
    if (!onApplyShots) { setError('当前场景没有接入镜头表接口，镜头计划无法应用'); return; }
    if (!shotAcknowledged) { setError('请先核对镜头顺序、台词与未指定机位的镜头并勾选确认'); return; }
    setError('');
    try {
      // 有当前草稿时必须先比对来源；没有草稿就如实提示无法比对。
      const checked = scriptDraft ? engine.assertScriptShotPlanSource(shotPlan, scriptDraft) : shotPlan;
      const verified = verifyMotionTargets(checked);
      onApplyShots(verified);
      setShotNote(`已接入 ${verified.shots.length} 镜（${verified.shots.filter(shot => shot.motion).length} 镜含人工走位）；保存场景后随存档保留。未指定机位 ${verified.shots.filter(shot => !shot.camera).length} 镜。`);
      setShotAcknowledged(false);
    } catch (cause) { setError(cause instanceof Error ? cause.message : String(cause)); }
  }
  function applyScript() {
    if (!engine || !scriptDraft || disabled || pending) return;
    if (!acknowledged) { setError('请先核对场次、地点、模板、角色绑定与站位并勾选确认'); return; }
    if (unboundCharacters.length) { setError(`请先为每个出场角色显式选择布景人物或中性占位人：${unboundCharacters.join('、')}`); return; }
    const blocker = centerBlocker();
    if (blocker) { setError(blocker); return; }
    setError(''); setPending(true);
    try {
      const choices = scriptDraft.review.characters.map(character => {
        const choice = bindings[character];
        if (!choice || choice.kind === 'unbound') throw new Error(`角色「${character}」尚未绑定`);
        const target: ScriptCharacterBinding['target'] = choice.kind === 'placeholder'
          ? { kind: 'placeholder', position: { x: choice.x, z: choice.z }, ...(choice.rig ? { rig: choice.rig } : {}) }
          : { kind: 'set-person', objectId: choice.objectId };
        return { character, target };
      });
      const bound = engine.applyScriptCharacterBindings(scriptDraft, choices);
      commit(bound.setDraft, x, z);
      // 镜头计划独立于布景草稿：追加布景后仍可继续记录机位并应用。
      setScriptDraft(null); setAcknowledged(false); setBindings({});
    } catch (cause) { setError(cause instanceof Error ? cause.message : String(cause)); }
    finally { setPending(false); }
  }
  const playingShot = appliedPlan && shotPlayback ? appliedPlan.shots[shotPlayback.index] ?? null : null;
  const playingLine = shotPlayback?.line ?? '（停顿／无台词）';
  return <section aria-label="整套布景" className="space-y-2 border-t border-zinc-700 p-2 text-xs">
    <h3>整套布景 · {engine?.listSetTemplates().length ?? '加载中'} 套</h3>
    <p className="text-zinc-400">先生成草稿，确认后追加到当前场景，不替换已有对象。当前仅应用静态布景；模板时间、天气、群演动线尚未接入。</p>
    <select aria-label="布景模板" value={templateId} disabled={!engine || disabled || pending} onChange={event => { setTemplateId(event.target.value); setDraft(null); }} className="w-full bg-zinc-800 p-2">
      {engine?.listSetTemplates().map(row => <option key={row.id} value={row.id}>{row.genre} · {row.name} · {row.width}×{row.depth}米</option>)}
    </select>
    <button type="button" disabled={!engine || disabled || pending} onClick={prepare}>生成布景草稿</button>
    {draft && <div className="space-y-2">
      <p role="status">{draft.name} · {draft.document.objects.length} 个可编辑实例（不是模型部件数）</p>
      <label>中心 X（米）<input aria-label="布景中心X" type="number" min={-1000} max={1000} value={x} onChange={event => { setX(event.target.valueAsNumber); setAcknowledged(false); }} className="w-20 bg-zinc-800" /></label>
      <label>中心 Z（米）<input aria-label="布景中心Z" type="number" min={-1000} max={1000} value={z} onChange={event => { setZ(event.target.valueAsNumber); setAcknowledged(false); }} className="w-20 bg-zinc-800" /></label>
      <p>请核对中心位置，追加布景不自动避让已有物体；可在场景里逐件编辑或一次撤销。</p>
      <button type="button" disabled={disabled || pending} onClick={apply}>确认追加布景</button>
      <button type="button" disabled={pending} onClick={() => setDraft(null)}>丢弃草稿</button>
    </div>}
    <div className="space-y-2 border-t border-zinc-700 pt-2">
      <h3>从剧本搭景</h3>
      <p className="text-zinc-400">确定性解析场次并挑选真实存在的模板，不调用大模型、不联网；多场剧本必须显式选场，不默认取第一场。</p>
      <textarea aria-label="剧本原文" rows={4} value={scriptText} disabled={disabled} placeholder={'1. 内景 客栈 大堂 夜\n张三：门外有人。'} onChange={event => { controller.current?.abort(); scriptController.current?.abort(); setDraft(null); setPending(false); setScriptText(event.target.value); setIndex(null); setSceneId(''); setScriptDraft(null); setBindings({}); setScriptTemplateId(''); setAcknowledged(false); resetShots(); }} className="w-full bg-zinc-800 p-2 font-mono" />
      <button type="button" disabled={!engine || disabled || pending} onClick={parseScript}>解析剧本场次</button>
      {index && <div className="space-y-1">
        <p role="status">识别到 {index.scenes.length} 场；请显式选择要搭的场次。</p>
        {index.scenes.map(scene => <label key={scene.id} className="flex gap-1">
          <input type="radio" name="script-scene" aria-label={`场次 ${scene.id}`} checked={sceneId === scene.id} disabled={disabled || pending} onChange={() => { setSceneId(scene.id); setScriptDraft(null); setScriptTemplateId(''); setBindings({}); setAcknowledged(false); resetShots(); }} />
          <span>{scene.id} · {scene.heading || '未标注场次'} · {scene.location || '未标注地点'} · {TIME_LABELS[scene.timeOfDay]} · {scene.characters.length} 人 · {scene.dialogue.length} 句</span>
        </label>)}
        <label>该场布景（可人工改选）<select aria-label="剧本布景模板" value={scriptTemplateId} disabled={disabled || pending} onChange={event => { setScriptTemplateId(event.target.value); setScriptDraft(null); setBindings({}); setAcknowledged(false); resetShots(); }} className="w-full bg-zinc-800 p-2">
          <option value="">按地点规则推荐（无匹配则明确报错）</option>
          {engine?.listSetTemplates().map(row => <option key={row.id} value={row.id}>{row.genre} · {row.name}</option>)}
        </select></label>
        <button type="button" disabled={!sceneId || disabled || pending} onClick={prepareScript}>生成该场草稿</button>
      </div>}
      {scriptDraft && <div className="space-y-1">
        <p role="status">{scriptDraft.sceneId} → {scriptDraft.templateName}（{scriptDraft.templateId}）· 布景 {scriptDraft.review.instanceCount} 个实例 + 新增 {Object.values(bindings).filter(choice => choice.kind === 'placeholder').length} 个占位人（不是模型部件数；已有布景人物不重复添加）</p>
        <p>模板来源：{scriptDraft.recommendation.matchedBy === 'manual' ? '人工指定' : '地点规则推荐'}。当前仅搭景，不会自动把台词变成角色表演或机位。</p>
        {scriptDraft.review.characters.length > 0 && <p>出场：{scriptDraft.review.characters.join('、')}</p>}
        {scriptDraft.review.dialogue.map((line, position) => <p key={`${line.speaker}-${position}`} className="text-zinc-400">{line.speaker}：{line.line}</p>)}
        {scriptDraft.review.characters.length > 0 ? <div className="space-y-1 border-t border-zinc-700 pt-1">
          <p>角色绑定：复用布景人物，或显式新建胶囊人/24骨蒙皮人物并填写站位；不按姓名猜素材。</p>
          {scriptDraft.review.characters.map(character => {
            const choice: BindingChoice = bindings[character] ?? { kind: 'unbound' };
            return <div key={character} className="space-y-1">
              <label className="flex items-center gap-1">
                <span className="w-20 shrink-0">{character}</span>
                <select aria-label={`角色 ${character} 绑定`} value={bindingValue(choice)} disabled={disabled || pending} onChange={event => setCharacterBinding(character, event.target.value)} className="w-full bg-zinc-800 p-1">
                  <option value="">未绑定</option>
                  <option value="placeholder">中性占位人（胶囊人）＋指定站位</option>
                  <option value="skeletal">24骨蒙皮人物（37动作）＋指定站位</option>
                  {scriptPersons.map(person => <option key={person.id} value={`set:${person.id}`}>布景人物：{person.name} · {POSE_LABELS[person.shape] ?? person.shape} · {person.position.x.toFixed(1)},{person.position.z.toFixed(1)}</option>)}
                </select>
              </label>
              {choice.kind === 'placeholder' && <div className="flex gap-2 pl-20">
                <label>站位 X（相对中心，米）<input aria-label={`角色 ${character} 站位X`} type="number" step="0.1" min={-1000} max={1000} value={choice.x} disabled={disabled || pending} onChange={event => movePlaceholder(character, 'x', event.target.valueAsNumber)} className="w-20 bg-zinc-800" /></label>
                <label>站位 Z（相对中心，米）<input aria-label={`角色 ${character} 站位Z`} type="number" step="0.1" min={-1000} max={1000} value={choice.z} disabled={disabled || pending} onChange={event => movePlaceholder(character, 'z', event.target.valueAsNumber)} className="w-20 bg-zinc-800" /></label>
              </div>}
            </div>;
          })}
          <p role="status">{unboundCharacters.length ? `已绑定 ${scriptDraft.review.characters.length - unboundCharacters.length}/${scriptDraft.review.characters.length} 个角色；还需绑定：${unboundCharacters.join('、')}` : `已绑定 ${scriptDraft.review.characters.length} 个角色（站位相对布景中心）`}</p>
          <p className="text-zinc-400">占位人不是最终角色模型；24骨人物含37个可选片段，追加后可人工逐镜选动作。不按台词猜表演、不写提示词。</p>
        </div> : <p className="text-zinc-400">本场没有检出出场角色，无需角色绑定。</p>}
        {scriptDraft.review.diagnostics.map((text, position) => <p key={`diagnostic-${position}`} role="note" className="text-amber-400">{text}</p>)}
        {scriptDraft.review.conflicts.map((text, position) => <p key={`conflict-${position}`} role="alert" className="text-amber-400">{text}</p>)}
        <label>中心 X（米）<input aria-label="剧本布景中心X" type="number" min={-1000} max={1000} value={x} onChange={event => { setX(event.target.valueAsNumber); setAcknowledged(false); }} className="w-20 bg-zinc-800" /></label>
        <label>中心 Z（米）<input aria-label="剧本布景中心Z" type="number" min={-1000} max={1000} value={z} onChange={event => { setZ(event.target.valueAsNumber); setAcknowledged(false); }} className="w-20 bg-zinc-800" /></label>
        <p className="text-zinc-400">台词与出场只作审阅参考；追加后可逐件编辑或一次撤销。按钮在角色未绑定或未勾选核对时保持禁用。</p>
        <label className="flex gap-1">
          <input type="checkbox" aria-label="确认场次与模板" checked={acknowledged} disabled={disabled || pending} onChange={event => setAcknowledged(event.target.checked)} />
          <span>已核对场次、地点、模板、角色绑定与实例数</span>
        </label>
        <button type="button" disabled={!acknowledged || unboundCharacters.length > 0 || disabled || pending} onClick={applyScript}>确认追加该场布景</button>
        <button type="button" disabled={pending} onClick={() => { setScriptDraft(null); setBindings({}); setAcknowledged(false); }}>丢弃剧本草稿</button>
        <button type="button" disabled={disabled || pending} onClick={generateShots}>生成镜头计划（每句一镜）</button>
        <p className="text-zinc-400">镜头计划只拆台词与时间；机位一律留空，需在视口里逐镜记录。</p>
      </div>}
      {(shotPlan || appliedPlan) && <div className="space-y-2 border-t border-zinc-700 pt-2" aria-label="镜头表">
        <h3>镜头表与台词时间轴</h3>
        {appliedPlan && appliedPlan !== shotPlan && <div className="space-y-1">
          <p role="status">场景中已保存镜头表：{appliedPlan.shots.length} 镜 · {appliedPlan.shots[appliedPlan.shots.length - 1]!.end.toFixed(2)} 秒 · {appliedPlan.shots.filter(shot => !shot.camera).length} 镜未指定机位 · {appliedPlan.shots.filter(shot => shot.motion).length} 镜含人工走位（{appliedPlan.sceneId}）</p>
          <button type="button" disabled={disabled || pending} onClick={loadAppliedShots}>载入场景镜头表审阅</button>
        </div>}
        {appliedPlan && <div className="space-y-1 rounded border border-zinc-700 p-1" aria-label="连续逐镜播放">
          <p className="text-zinc-400">连续逐镜播放：按已保存镜头表的时间轴切换人工记录的视口机位，并在下方显示本镜台词。缺任一镜机位就直接拒绝，不补造机位；已人工绑定的走位、骨骼片段或胶囊步态同步预览，不自动编排表演或生成视频。</p>
          <div className="flex flex-wrap gap-2">
            <button type="button" disabled={disabled || pending || !onPlayShots || Boolean(shotPlayback)} onClick={playAppliedShots}>连续逐镜播放</button>
            <button type="button" disabled={!shotPlayback || !onStopShots} onClick={onStopShots}>停止并还原视口</button>
          </div>
          {shotPlayback && <p role="status">正在播放第 {playingShot?.order ?? shotPlayback.index + 1} 镜 / 共 {shotPlayback.shots} 镜{playingLine ? ` · 当前台词：${playingLine}` : ''}</p>}
        </div>}
        {appliedPlan && <div className="space-y-1 rounded border border-zinc-700 p-1" aria-label="逐镜参考包">
          <p className="text-zinc-400">逐镜参考包：按已保存镜头表的镜序，在每镜开始秒数用人工记录的机位分别渲染白模/法线/深度三张 PNG，连同 manifest.json 打成一个 zip 本地下载。缺任一镜机位或有镜头取图失败都拒绝整包，不补造机位、不导出半份；与逐镜视频共用时间轴，取样人工走位与骨姿，保留前镜走位终点；采样后视口和人物还原。这只是网格结构参考素材：包含泼溅或视频平面的场景暂不支持，会明确拒绝而非省略。清单记录导出时场景标识；导出途中场景变化则整包作废。不编码视频、不上传素材库、不写业务库。</p>
          <p role="note">预计 {appliedPlan.shots.length} 镜 × 3 张 = {appliedPlan.shots.length * 3} 张 PNG + manifest.json。导出期间视口会依次切到各镜机位，结束后回到当前取景。</p>
          <label className="flex gap-1">
            <input type="checkbox" aria-label="确认导出逐镜参考包" checked={packAcknowledged} disabled={disabled || pending || packPending} onChange={event => setPackAcknowledged(event.target.checked)} />
            <span>我已核对逐镜机位与采样时刻（每镜开始秒数），同意在本地导出参考包</span>
          </label>
          <button type="button" disabled={!packAcknowledged || disabled || pending || packPending || !onExportShotPack} onClick={() => { void exportShotPack(); }}>导出逐镜参考包 ZIP</button>
        </div>}
        {shotPlan && <div className="space-y-1">
          <p role="status">共 {shotPlan.shots.length} 镜 · 总时长 {shotPlan.shots[shotPlan.shots.length - 1]!.end.toFixed(2)} 秒 · 未指定机位 {shotPlan.shots.filter(shot => !shot.camera).length} 镜 · 含人工走位 {shotPlan.shots.filter(shot => shot.motion).length} 镜{scriptDraft ? '' : ' · 本面板没有当前草稿可比对来源'}</p>
          <p className="text-zinc-400">走位按人工选定的场景对象、起止 XZ 与时长做直线移动，Y 跟随启用的地形高差；越界或陡坡拒绝，不做障碍避让。可选已有骨骼 clip 只作局部预演；无 clip 的胶囊人按行进距离摆步态。不会按角色姓名猜对象，也不会按文件名猜动作。</p>
          {shotPlan.shots.map(shot => <div key={shot.id} className="space-y-1 border-t border-zinc-800 pt-1">
            <p role="status">{shot.id} · 第 {shot.order} 镜 · {shot.start.toFixed(2)}–{shot.end.toFixed(2)} 秒 · {shot.camera ? '已记录机位' : '未指定机位'}</p>
            {shot.dialogue.map(line => <p key={`${shot.id}-${line.index}`} className="text-zinc-400">{line.speaker}：{line.line || '（无台词）'}（{line.start.toFixed(2)}–{line.end.toFixed(2)} 秒）</p>)}
            <div className="flex flex-wrap gap-2">
              <label>时长（秒）<input aria-label={`镜 ${shot.order} 时长`} type="number" step="0.1" min={0.2} value={Number((shot.end - shot.start).toFixed(2))} disabled={disabled || pending} onChange={event => { const value = event.target.valueAsNumber; if (Number.isFinite(value)) editShotDuration(shot.id, value); }} className="w-20 bg-zinc-800" /></label>
              <button type="button" aria-label={`用当前视口机位 ${shot.id}`} disabled={disabled || pending || !onCaptureCamera} onClick={() => captureShotCamera(shot.id)}>用当前视口机位</button>
              <button type="button" aria-label={`预览镜 ${shot.order}`} disabled={disabled || !shot.camera || !onPreviewShot} onClick={() => previewShot(shot.camera)}>预览</button>
              <button type="button" aria-label={`清除机位 镜 ${shot.order}`} disabled={disabled || pending || !shot.camera} onClick={() => clearShotCamera(shot.id)}>清除机位</button>
            </div>
            <p className="text-zinc-400">{shot.camera ? `机位 ${shot.camera.position.map(value => value.toFixed(2)).join(',')} → ${shot.camera.target.map(value => value.toFixed(2)).join(',')} · ${shot.camera.fov.toFixed(0)}°` : '未指定机位（不会自动补）'}</p>
            <ShotMotionEditor shot={shot} targets={motionTargets} disabled={disabled} pending={pending}
              onSet={motion => bindShotMotion(shot.id, motion)} onClear={() => unbindShotMotion(shot.id)} />
          </div>)}
          {shotPlan.action?.length > 0 && <div>
            <p className="text-zinc-400">动作行（按原文保留，不计时）：</p>
            {shotPlan.action.map((text, position) => <p key={`shot-action-${position}`} className="text-zinc-400">{text}</p>)}
          </div>}
          {shotPlan.diagnostics?.map((text, position) => <p key={`shot-diagnostic-${position}`} role="note" className="text-amber-400">{text}</p>)}
          {shotNote && <p role="status" className="text-amber-400">{shotNote}</p>}
          <label className="flex gap-1">
            <input type="checkbox" aria-label="确认镜头表" checked={shotAcknowledged} disabled={disabled || pending} onChange={event => setShotAcknowledged(event.target.checked)} />
            <span>已核对镜头顺序、台词时长与未指定机位的镜头；机位只来自我记录的视口</span>
          </label>
          <button type="button" disabled={!shotAcknowledged || disabled || pending || !onApplyShots} onClick={applyShots}>确认应用镜头表</button>
          <button type="button" disabled={pending} onClick={() => { resetShots(); }}>丢弃镜头计划</button>
        </div>}
      </div>}
    </div>
    {pending && <p role="status">正在构建布景…</p>}
    {error && <p role="alert" className="text-red-400">{error}</p>}
  </section>;
}