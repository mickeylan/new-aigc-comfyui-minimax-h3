import { useEffect, useState } from 'react';
import type { ScriptShotMotionAnimation, ScriptShotMotionTarget, ScriptShotPlan } from './scriptShotPlan';
import type { ExecutionClipCorrection } from './performanceCorrection';

/** Only chooses an existing actor/beat and an actually attached clip. No model call,
 * auto choreography, clip retiming, contact editing or viewport mutation. */
export default function PerformanceCorrectionPanel({ plan, targets, disabled: busy, stale = false, onDirty, onStage }: {
  plan: ScriptShotPlan; targets: ScriptShotMotionTarget[]; disabled: boolean; stale?: boolean;
  onDirty: (dirty: boolean) => void; onStage: (correction: ExecutionClipCorrection) => void;
}) {
  const disabled = busy || stale;
  const [shotId, setShotId] = useState(''), [targetId, setTargetId] = useState(''), [beatIndex, setBeatIndex] = useState('');
  const [animation, setAnimation] = useState<ScriptShotMotionAnimation | null>(null);
  const [reason, setReason] = useState(''), [error, setError] = useState('');
  const [split, setSplit] = useState(false), [splitAt, setSplitAt] = useState('');
  const [afterAnimation, setAfterAnimation] = useState<ScriptShotMotionAnimation | null>(null);
  const dirty = animation !== null || reason !== '' || split;
  const reset = () => { setAnimation(null); setReason(''); setError(''); setSplit(false); setSplitAt(''); setAfterAnimation(null); onDirty(false); };
  useEffect(() => { setShotId(''); setTargetId(''); setBeatIndex(''); reset(); }, [plan]);
  const shots = plan.shots.filter(s => s.execution?.actors?.length);
  const shot = shots.find(s => s.id === shotId), actors = shot?.execution?.actors ?? [];
  const actor = actors.find(a => a.targetId === targetId), beat = beatIndex === '' ? undefined : actor?.beats[Number(beatIndex)];
  const target = targets.find(t => t.id === targetId);
  const clips = target?.animations ?? [];
  const usable = !!target && target.visible !== false && !target.locked;
  const stage = () => {
    if (disabled || !beat || !animation || (split && (!afterAnimation || !splitAt.trim()))) return;
    try { onStage({ shotId, targetId, beatIndex: Number(beatIndex), animation, reason,
      ...(split ? { split: { at: Number(splitAt), afterAnimation: afterAnimation! } } : {}) }); setError(''); }
    catch (cause) { setError(cause instanceof Error ? cause.message : String(cause)); }
  };
  return <section aria-label="逐段骨骼动作纠正" className="space-y-1 rounded border border-zinc-700 p-2">
    <h4>逐段骨骼动作纠正（人工）</h4>
    <p>替换已有角色、已有动作段的骨骼片段，也可人工拆成两段；保留原总区间、来源索引、线性位置轨迹、朝向、机位与接触约束。不按名称猜剧情；通用片段不是抓紧、扑胸或剧情已完成的证据。</p>
    <p>接触目前仅整镜腕表面贴合，不支持接近/释放或闭合握指；更换动作后仍须通过蒙皮、角色间距及环境预检。动作短于区间时停在末帧，不自动拉伸或循环。</p>
    {!shots.length ? <p>没有可纠正的逐段骨骼执行；不会把静止机位或旧走位伪装成表演。</p> : <>
      <label>镜头<select aria-label="纠正镜头" className="w-full bg-zinc-800" value={shotId} disabled={disabled || dirty} onChange={e => { setShotId(e.target.value); setTargetId(''); setBeatIndex(''); setError(''); }}><option value="">请选择镜头</option>{shots.map(s => <option key={s.id} value={s.id}>第{s.order}镜 · {s.id}</option>)}</select></label>
      <label>角色<select aria-label="纠正角色" className="w-full bg-zinc-800" value={targetId} disabled={disabled || dirty || !shot} onChange={e => { setTargetId(e.target.value); setBeatIndex(''); setError(''); }}><option value="">请选择已有执行角色</option>{actors.map(a => <option key={a.targetId} value={a.targetId}>{a.targetName} · {a.targetId}</option>)}</select></label>
      <label>动作段（镜内秒）<select aria-label="纠正动作段" className="w-full bg-zinc-800" value={beatIndex} disabled={disabled || dirty || !actor} onChange={e => { setBeatIndex(e.target.value); setError(''); }}><option value="">请选择动作段</option>{actor?.beats.map((b, i) => <option key={i} value={String(i)}>段{i + 1} · {b.start}–{b.end}秒 · {b.animation.name}{b.sourceIndex === undefined ? ' · 无原时间轴索引' : ` · 原段${b.sourceIndex + 1}`}</option>)}</select></label>
      {beat && <p>当前动作：{beat.animation.name} · {beat.animation.duration}秒；区间{beat.start}–{beat.end}秒（只读）。坐标/时长不在此修改，人工选择不构成原文事实。</p>}
      {actor && !usable && <p role="alert">执行角色缺失、隐藏或锁定，请先恢复可用状态。</p>}
      <label><input type="checkbox" aria-label="拆分动作段" checked={split} disabled={disabled || !beat || !usable} onChange={e => {
        setSplit(e.target.checked); setSplitAt(''); setAfterAnimation(null); setError(''); onDirty(e.target.checked || animation !== null || reason !== '');
      }} />将所选动作段拆成两段</label>
      {split && <>
        <label>分界（镜内秒）<input type="number" step="any" aria-label="拆分分界秒" className="w-full bg-zinc-800" value={splitAt} disabled={disabled || !beat} onChange={e => { setSplitAt(e.target.value); setError(''); }} /></label>
        <p>请显式选择前后两段片段，均各自从零秒播放；不自动续接原片段、拉伸、循环或混合，分界可能跳姿，必须审阅。线性根轨迹在分界插值，整镜接触不会释放。</p>
      </>}
      <label>{split ? '前段实际挂载片段' : '实际挂载片段'}<select aria-label="纠正骨骼片段" className="w-full bg-zinc-800" value={animation ? String(animation.index) : ''} disabled={disabled || !beat || !usable} onChange={e => {
        const clip = e.target.value === '' ? undefined : clips.find(c => c.index === Number(e.target.value));
        setAnimation(clip ? { index: clip.index, name: clip.name, duration: clip.duration } : null); setError(''); onDirty(!!clip || reason !== '' || split);
      }}><option value="">请选择替换片段</option>{clips.map(c => <option key={`${c.index}:${c.name}:${c.duration}`} value={String(c.index)}>{c.index} · {c.name} · {c.duration.toFixed(2)}秒</option>)}</select></label>
      {split && <label>后段实际挂载片段<select aria-label="后段骨骼片段" className="w-full bg-zinc-800" value={afterAnimation ? String(afterAnimation.index) : ''} disabled={disabled || !beat || !usable} onChange={e => {
        const clip = e.target.value === '' ? undefined : clips.find(c => c.index === Number(e.target.value));
        setAfterAnimation(clip ? { index: clip.index, name: clip.name, duration: clip.duration } : null); setError('');
      }}><option value="">请选择后段片段</option>{clips.map(c => <option key={`${c.index}:${c.name}:${c.duration}`} value={String(c.index)}>{c.index} · {c.name} · {c.duration.toFixed(2)}秒</option>)}</select></label>}
      <label>人工纠正理由<textarea aria-label="人工动作纠正理由" className="w-full bg-zinc-800" rows={2} maxLength={1000} value={reason} disabled={disabled || !beat} onChange={e => { setReason(e.target.value); setError(''); onDirty(animation !== null || e.target.value !== '' || split); }} /></label>
      {dirty && <p role="status">存在未暂存动作修改；先暂存或放弃，再重新确认镜头表。不会自动应用、播放、保存或导出。</p>}
      <button type="button" disabled={disabled || !beat || !usable || !animation || !reason.trim() || (split && (!afterAnimation || !splitAt.trim()))} onClick={stage}>暂存动作纠正</button>
      <button type="button" disabled={busy || !dirty} onClick={reset}>放弃本次动作修改</button>
    </>}
    {error && <p role="alert">{error}</p>}
  </section>;
}
