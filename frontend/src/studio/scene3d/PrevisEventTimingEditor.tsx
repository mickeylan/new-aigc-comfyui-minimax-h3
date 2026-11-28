import { useState } from 'react';
import type { PrevisEvent } from './previsEvents';
/** Mount with a snapshot key; this form never mutates the event or estimates duration. */
export default function PrevisEventTimingEditor({ event, index, disabled, onConfirm, onCancel }: {
  event: PrevisEvent; index: number; disabled: boolean;
  onConfirm: (start: number, end: number) => void; onCancel: () => void;
}) {
  const [start, setStart] = useState(String(event.start)), [end, setEnd] = useState(String(event.end));
  const a = Number(start), b = Number(end);
  const valid = start.trim() !== '' && end.trim() !== '' && Number.isFinite(a) && Number.isFinite(b) && a >= 0 && b > a && b <= 600 && (a !== event.start || b !== event.end);
  return <fieldset aria-label="待确认事件区间调整" className="space-y-2 rounded border border-amber-700 p-2">
    <legend>调整事件{index + 1}的全片区间（尚未应用）</legend>
    <p>{event.kind} · {event.objectId} · 原全片[{event.start}, {event.end})秒</p>
    <p>只改此事件区间，不改镜长或对白。事件按新起点重新计时：飞散弹道/火烟相位会变化，附着结束回编辑姿态；不保证释放连续性或物理效果。</p>
    <label>新开始全片秒<input type="number" step="any" aria-label="调整事件新开始全片秒" value={start} disabled={disabled} onChange={e => setStart(e.target.value)} /></label>
    <label>新结束全片秒<input type="number" step="any" aria-label="调整事件新结束全片秒" value={end} disabled={disabled} onChange={e => setEnd(e.target.value)} /></label>
    {!valid && <p>请输入不同于原值的有限半开区间，0≤开始&lt;结束≤600；还需宿主全片时长与重叠校验。</p>}
    <button type="button" disabled={disabled || !valid} onClick={() => { if (!disabled && valid) onConfirm(a, b); }}>确认仅调整事件{index + 1}区间</button>
    <button type="button" onClick={onCancel}>放弃区间调整</button>
  </fieldset>;
}
