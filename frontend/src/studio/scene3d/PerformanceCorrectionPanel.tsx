import { useEffect, useState } from 'react';
import type { ScriptShotMotionAnimation, ScriptShotMotionTarget, ScriptShotPlan } from './scriptShotPlan';
import type { ExecutionClipCorrection, ExecutionClipRetime } from './performanceCorrection';

/** Only chooses an existing actor/beat and an actually attached clip. No model call,
 * auto choreography, clip retiming, contact editing or viewport mutation. */
export default function PerformanceCorrectionPanel({ plan, targets, disabled: busy, stale = false, onDirty, onStage, onRetime }: {
  plan: ScriptShotPlan; targets: ScriptShotMotionTarget[]; disabled: boolean; stale?: boolean;
  onDirty: (dirty: boolean) => void; onStage: (correction: ExecutionClipCorrection) => void; onRetime?: (correction: ExecutionClipRetime) => void;
}) {
  const disabled = busy || stale;
  const [shotId, setShotId] = useState(''), [targetId, setTargetId] = useState(''), [beatIndex, setBeatIndex] = useState('');
  const [zoom, setZoom] = useState(1), [cursor, setCursor] = useState(0);
  const [snap, setSnap] = useState('none');
  useEffect(() => { setZoom(1); setCursor(0); }, [plan, shotId]);
  const [animation, setAnimation] = useState<ScriptShotMotionAnimation | null>(null);
  const [reason, setReason] = useState(''), [error, setError] = useState('');
  const [split, setSplit] = useState(false), [splitAt, setSplitAt] = useState('');
  const [afterAnimation, setAfterAnimation] = useState<ScriptShotMotionAnimation | null>(null);
  const [boundary, setBoundary] = useState(false), [boundaryAt, setBoundaryAt] = useState('');
  const [retime, setRetime] = useState(false), [sourceIn, setSourceIn] = useState('0'), [rate, setRate] = useState('1');
  const dirty = animation !== null || reason !== '' || split || boundary || retime;
  const reset = () => { setAnimation(null); setReason(''); setError(''); setSplit(false); setSplitAt(''); setAfterAnimation(null); setBoundary(false); setBoundaryAt(''); setRetime(false); setSourceIn('0'); setRate('1'); onDirty(false); };
  useEffect(() => { setShotId(''); setTargetId(''); setBeatIndex(''); reset(); }, [plan]);
  const shots = plan.shots.filter(s => s.execution?.actors?.length);
  const shot = shots.find(s => s.id === shotId), actors = shot?.execution?.actors ?? [];
  const duration = shot ? shot.end - shot.start : 0;
  const validDuration = Number.isFinite(duration) && duration > 0;
  const actor = actors.find(a => a.targetId === targetId), beat = beatIndex === '' ? undefined : actor?.beats[Number(beatIndex)];
  const nextBeat = actor?.beats[Number(beatIndex) + 1];
  const boundaryUsable = !!beat && !!nextBeat && beat.end === nextBeat.start && beat.from.x === beat.to.x && beat.from.z === beat.to.z && nextBeat.from.x === nextBeat.to.x && nextBeat.from.z === nextBeat.to.z && beat.to.x === nextBeat.from.x && beat.to.z === nextBeat.from.z && beat.yaw === nextBeat.yaw;
  const boundaryValid = boundaryUsable && boundaryAt.trim() !== '' && Number.isFinite(Number(boundaryAt)) && Number(boundaryAt) - beat!.start > 1e-6 && nextBeat!.end - Number(boundaryAt) > 1e-6 && Math.abs(Number(boundaryAt) - beat!.end) > 1e-6;
  const target = targets.find(t => t.id === targetId);
  const clips = target?.animations ?? [];
  const usable = !!target && target.visible !== false && !target.locked;
  const retimeValid = !!beat && reason.trim() !== '' && sourceIn.trim() !== '' && rate.trim() !== '' && Number.isFinite(Number(sourceIn)) && Number.isFinite(Number(rate)) && Number(sourceIn) >= 0 && Number(sourceIn) < beat.animation.duration && Number(rate) >= .25 && Number(rate) <= 4 && Number(sourceIn) + (beat.end - beat.start) * Number(rate) <= beat.animation.duration + 1e-6;
  const stageRetime = () => {
    if (disabled || !usable || !beat || !retime || !retimeValid || !onRetime) return;
    try { onRetime({ shotId, targetId, beatIndex: Number(beatIndex), timing: { sourceIn: Number(sourceIn), rate: Number(rate) }, reason }); setError(''); }
    catch (cause) { setError(cause instanceof Error ? cause.message : String(cause)); }
  };
  const stage = () => {
    if (disabled || !usable || !beat || !reason.trim() || (boundary ? !boundaryValid : !animation || (split && (!afterAnimation || !splitAt.trim())))) return;
    try { onStage({ shotId, targetId, beatIndex: Number(beatIndex), animation: boundary ? beat.animation : animation!, reason,
      ...(boundary ? { boundary: { at: Number(boundaryAt) } } : split ? { split: { at: Number(splitAt), afterAnimation: afterAnimation! } } : {}) }); setError(''); }
    catch (cause) { setError(cause instanceof Error ? cause.message : String(cause)); }
  };
  return <section aria-label="逐段骨骼动作纠正" className="space-y-1 rounded border border-zinc-700 p-2 [&_label]:block">
    <h4>逐段骨骼动作纠正（人工）</h4>
    <p>替换已有角色、已有动作段的骨骼片段，也可人工拆成两段；保留原总区间、来源索引、线性位置轨迹、朝向、机位与接触约束。不按名称猜剧情；通用片段不是抓紧、扑胸或剧情已完成的证据。</p>
    <p>接触目前仅整镜腕表面贴合，不支持接近/释放或闭合握指；更换动作后仍须通过蒙皮、角色间距及环境预检。动作短于区间时停在末帧，不自动拉伸或循环。</p>
    {!shots.length ? <p>没有可纠正的逐段骨骼执行；不会把静止机位或旧走位伪装成表演。</p> : <>
      <label>镜头<select aria-label="纠正镜头" className="w-full bg-zinc-800" value={shotId} disabled={disabled || dirty} onChange={e => { setShotId(e.target.value); setTargetId(''); setBeatIndex(''); setError(''); }}><option value="">请选择镜头</option>{shots.map(s => <option key={s.id} value={s.id}>第{s.order}镜 · {s.id}</option>)}</select></label>
      <label>角色<select aria-label="纠正角色" className="w-full bg-zinc-800" value={targetId} disabled={disabled || dirty || !shot} onChange={e => { setTargetId(e.target.value); setBeatIndex(''); setError(''); }}><option value="">请选择已有执行角色</option>{actors.map(a => <option key={a.targetId} value={a.targetId}>{a.targetName} · {a.targetId}</option>)}</select></label>
      <label>动作段（镜内秒）<select aria-label="纠正动作段" className="w-full bg-zinc-800" value={beatIndex} disabled={disabled || dirty || !actor} onChange={e => { setBeatIndex(e.target.value); setError(''); }}><option value="">请选择动作段</option>{actor?.beats.map((b, i) => <option key={i} value={String(i)}>段{i + 1} · {b.start}–{b.end}秒 · {b.animation.name}{b.sourceIndex === undefined ? ' · 无原时间轴索引' : ` · 原段${b.sourceIndex + 1}`}</option>)}</select></label>
      {shot && validDuration && <section aria-label="镜内动作时间轴" className="space-y-2 overflow-x-auto rounded border border-zinc-600 p-2">
        <p>镜内 0–{shot.end - shot.start} 秒（全片 {shot.start}–{shot.end} 秒）。点选片段后在下方替换或拆分；条块不改变时长、不播放。</p>
        <label>时间轴缩放<select aria-label="时间轴缩放" className="w-full bg-zinc-800" disabled={disabled || dirty} value={zoom} onChange={e => setZoom(Number(e.target.value))}>{[1, 2, 4, 8].map(n => <option key={n} value={n}>{n}倍</option>)}</select></label>
        <label>游标吸附<select aria-label="时间游标吸附" className="w-full bg-zinc-800" disabled={disabled || dirty} value={snap} onChange={e => setSnap(e.target.value)}><option value="none">无</option><option value="tenth">0.1秒</option><option value="beat">动作边界</option></select></label>
        <label>审阅时间游标（不播放）<input type="range" aria-label="审阅时间游标" min={0} max={duration} step="0.001" disabled={disabled || dirty} value={cursor} onChange={e => {
          let value = Number(e.target.value);
          if (snap === 'tenth') value = Math.round(value * 10) / 10;
          if (snap === 'beat') { const edges = [0, duration, ...actors.flatMap(a => a.beats.flatMap(b => [b.start, b.end]))]; value = edges.reduce((best, edge) => Math.abs(edge - value) < Math.abs(best - value) ? edge : best, edges[0]); }
          setCursor(Math.max(0, Math.min(duration, value)));
        }} /></label>
        <p role="status">审阅游标：镜内{cursor.toFixed(3)}秒 · 全片{(shot.start + cursor).toFixed(3)}秒；只读标尺，不改变视口。</p>
        <p aria-label="游标活动动作">{actors.map(row => `${row.targetName}：${row.beats.filter(b => cursor >= b.start && cursor < b.end).map(b => b.animation.name).join('、') || '无活动段'}`).join('；')}</p>
        <div className="overflow-x-auto"><div style={{ width: `${zoom * 100}%` }}>
        {actors.map(row => {
          const live = targets.find(t => t.id === row.targetId);
          const unavailable = !live || live.visible === false || live.locked;
          return <div key={row.targetId} aria-label={`动作轨道 ${row.targetName}`}>
            <p>{row.targetName}{unavailable ? ' · 不可编辑' : ''}</p>
            <div className="relative h-12 min-w-80 bg-zinc-900">
              <span aria-hidden="true" className="pointer-events-none absolute top-0 z-10 h-12 border-l-2 border-amber-300" style={{ left: `${cursor / duration * 100}%` }} />
              {row.beats.map((item, index) => <button key={index} type="button"
                aria-label={`选择 ${row.targetName} 段${index + 1} ${item.start}–${item.end}秒`}
                aria-pressed={targetId === row.targetId && beatIndex === String(index)}
                disabled={disabled || dirty || unavailable}
                title={`${item.start}–${item.end}秒 · ${item.animation.name}${item.sourceIndex === undefined ? ' · 无原时间轴索引' : ` · 原段${item.sourceIndex + 1}`}`}
                style={{ left: `${item.start / duration * 100}%`, width: `${(item.end - item.start) / duration * 100}%` }}
                className={`absolute top-0 h-12 overflow-hidden border px-1 text-left text-xs disabled:opacity-40 ${targetId === row.targetId && beatIndex === String(index) ? 'border-amber-300 bg-amber-900' : 'border-zinc-500 bg-zinc-700'}`}
                onClick={() => { if (disabled || dirty || unavailable) return; setTargetId(row.targetId); setBeatIndex(String(index)); setError(''); }}>
                {item.start}–{item.end}s · {item.animation.name}
              </button>)}
            </div>
          </div>;
        })}
        </div></div>
      </section>}
      {beat && <p>当前动作：{beat.animation.name} · {beat.animation.duration}秒；区间{beat.start}–{beat.end}秒（当前计划）。可单独暂存相接静止段分界修改，人工选择不构成原文事实。</p>}
      {actor && !usable && <p role="alert">执行角色缺失、隐藏或锁定，请先恢复可用状态。</p>}
      <label><input type="checkbox" aria-label="调整静止动作分界" checked={boundary} disabled={disabled || !usable || !boundaryUsable || split || animation !== null} onChange={e => { setBoundary(e.target.checked); setBoundaryAt(beat ? String(beat.end) : ''); setError(''); onDirty(e.target.checked || reason !== '' || split || animation !== null); }} />调整所选段与下一段的静止动作分界（保留片段）</label>
      {boundary && beat && nextBeat && <div className="space-y-1">
        <p>原分界{beat.end}秒；仅同位置同朝向静止相邻段。两段并集{beat.start}–{nextBeat.end}秒不变，片段各自从零播放，不拉伸、不循环、不改对白；切换可能跳姿，需重新审阅。</p>
        <label>新分界镜内秒<input type="number" step="any" aria-label="静止动作新分界秒" className="w-full bg-zinc-800" value={boundaryAt} disabled={disabled || !usable || !boundaryUsable} onChange={e => { setBoundaryAt(e.target.value); setError(''); }} /></label>
        <input type="range" aria-label="拖动静止动作分界" min={beat.start} max={nextBeat.end} step="0.001" value={boundaryAt === '' || !Number.isFinite(Number(boundaryAt)) ? beat.end : Number(boundaryAt)} disabled={disabled || !usable || !boundaryUsable} onChange={e => { setBoundaryAt(e.target.value); setError(''); }} />
        <p role="status">待暂存：段{Number(beatIndex) + 1} {beat.start}–{boundaryAt}秒；段{Number(beatIndex) + 2} {boundaryAt}–{nextBeat.end}秒。尚未修改计划或视口。</p>
        {!boundaryValid && <p>分界须改变且严格在两段并集内部，两侧须超过0.000001秒。</p>}
      </div>}
      <label><input type="checkbox" aria-label="拆分动作段" checked={split} disabled={disabled || !beat || !usable || boundary} onChange={e => {
        setSplit(e.target.checked); setSplitAt(''); setAfterAnimation(null); setError(''); onDirty(e.target.checked || animation !== null || reason !== '');
      }} />将所选动作段拆成两段</label>
      <label><input type="checkbox" aria-label="调整片段源区间与速率" checked={retime} disabled={disabled || !beat || !usable || boundary || split || animation !== null} onChange={e => {
        setRetime(e.target.checked); setSourceIn(String(beat?.timing?.sourceIn ?? 0)); setRate(String(beat?.timing?.rate ?? 1)); setError(''); onDirty(e.target.checked || reason !== '' || animation !== null || split || boundary);
      }} />调整所选段的片段入点与播放速率（不改位置轨迹）</label>
      {retime && beat && <div className="space-y-1">
        <p>片段{beat.animation.name}共{beat.animation.duration}秒；本段区间{beat.start}–{beat.end}秒（{beat.end - beat.start}秒）。入点{sourceIn || '0'}秒起、按{rate || '1'}倍播放，采样停在片段末帧但不循环；人物位置仍按区间进度线性移动，不会因此走得更快。带该设置的片段暂不支持替换、拆分或移动分界。</p>
        <label>片段入点（秒）<input type="number" step="any" min={0} aria-label="片段入点秒" className="w-full bg-zinc-800" value={sourceIn} disabled={disabled || !usable} onChange={e => { setSourceIn(e.target.value); setError(''); }} /></label>
        <label>播放速率（0.25–4）<input type="number" step="any" min={.25} max={4} aria-label="片段播放速率" className="w-full bg-zinc-800" value={rate} disabled={disabled || !usable} onChange={e => { setRate(e.target.value); setError(''); }} /></label>
        {!retimeValid && <p>入点须≥0且小于片段时长，速率0.25–4，且入点+区间×速率不超过片段时长。</p>}
        <p role="status">待暂存：段{Number(beatIndex) + 1} 入点{sourceIn || '0'}秒 · {rate || '1'}倍；尚未修改计划或视口。</p>
        <button type="button" disabled={disabled || !retimeValid} onClick={stageRetime}>暂存源区间与速率</button>
      </div>}
      {split && <>
        <label>分界（镜内秒）<input type="number" step="any" aria-label="拆分分界秒" className="w-full bg-zinc-800" value={splitAt} disabled={disabled || !beat} onChange={e => { setSplitAt(e.target.value); setError(''); }} /></label>
        <p>请显式选择前后两段片段，均各自从零秒播放；不自动续接原片段、拉伸、循环或混合，分界可能跳姿，必须审阅。线性根轨迹在分界插值，整镜接触不会释放。</p>
      </>}
      <label>{split ? '前段实际挂载片段' : '实际挂载片段'}<select aria-label="纠正骨骼片段" className="w-full bg-zinc-800" value={animation ? String(animation.index) : ''} disabled={disabled || !beat || !usable || boundary} onChange={e => {
        const clip = e.target.value === '' ? undefined : clips.find(c => c.index === Number(e.target.value));
        setAnimation(clip ? { index: clip.index, name: clip.name, duration: clip.duration } : null); setError(''); onDirty(!!clip || reason !== '' || split);
      }}><option value="">请选择替换片段</option>{clips.map(c => <option key={`${c.index}:${c.name}:${c.duration}`} value={String(c.index)}>{c.index} · {c.name} · {c.duration.toFixed(2)}秒</option>)}</select></label>
      {split && <label>后段实际挂载片段<select aria-label="后段骨骼片段" className="w-full bg-zinc-800" value={afterAnimation ? String(afterAnimation.index) : ''} disabled={disabled || !beat || !usable} onChange={e => {
        const clip = e.target.value === '' ? undefined : clips.find(c => c.index === Number(e.target.value));
        setAfterAnimation(clip ? { index: clip.index, name: clip.name, duration: clip.duration } : null); setError('');
      }}><option value="">请选择后段片段</option>{clips.map(c => <option key={`${c.index}:${c.name}:${c.duration}`} value={String(c.index)}>{c.index} · {c.name} · {c.duration.toFixed(2)}秒</option>)}</select></label>}
      <label>人工纠正理由<textarea aria-label="人工动作纠正理由" className="w-full bg-zinc-800" rows={2} maxLength={1000} value={reason} disabled={disabled || !beat} onChange={e => { setReason(e.target.value); setError(''); onDirty(animation !== null || e.target.value !== '' || split || boundary); }} /></label>
      {dirty && <p role="status">存在未暂存动作修改；先暂存或放弃，再重新确认镜头表。不会自动应用、播放、保存或导出。</p>}
      <button type="button" disabled={disabled || !beat || !usable || retime || !reason.trim() || (boundary ? !boundaryValid : !animation || (split && (!afterAnimation || !splitAt.trim())))} onClick={stage}>暂存动作纠正</button>
      <button type="button" disabled={busy || !dirty} onClick={reset}>放弃本次动作修改</button>
    </>}
    {error && <p role="alert">{error}</p>}
  </section>;
}
