import { useEffect, useRef, useState } from 'react';
import PrevisEventTimingEditor from './PrevisEventTimingEditor';
import PrevisEventTimeline from './PrevisEventTimeline';
import type { PrevisEvent } from './previsEvents';
import { parseEventSidecar, serializeEventSidecar } from './previsEventSidecar';
type Choice = { id: string; name: string; bones: string[] };
const scalar = (s: string) => { if (!s.trim() || !Number.isFinite(Number(s))) throw Error('请输入明确的有限数'); return Number(s); };
const vector = (s: string): [number, number, number] => { const v = s.split(',').map(scalar); if (v.length !== 3 || v.some(n => !Number.isFinite(n))) throw Error('向量须为三个逗号分隔的有限数'); return v as [number, number, number]; };
export default function PrevisEventsPanel({ disabled, sceneKey, list, read, write, preview, sidecarSource, timelineDuration }: {
  disabled: boolean; sceneKey: string; sidecarSource?: () => string; timelineDuration?: () => number | undefined; list(): Choice[]; read(): PrevisEvent[]; write(events: PrevisEvent[]): void; preview(seconds: number): Promise<Blob>;
}) {
  const [objects, setObjects] = useState<Choice[]>([]), [events, setEvents] = useState<PrevisEvent[]>([]), [error, setError] = useState('');
  const [kind, setKind] = useState<PrevisEvent['kind']>('attach'), [objectId, setObjectId] = useState(''), [targetId, setTargetId] = useState(''), [bone, setBone] = useState('');
  const [start, setStart] = useState('0'), [end, setEnd] = useState('1'), [offset, setOffset] = useState('0,0,0'), [rotation, setRotation] = useState('0,0,0'), [velocity, setVelocity] = useState('0,2,0'), [scale, setScale] = useState('1'), [seconds, setSeconds] = useState('0');
  const [pending, setPending] = useState(false), [image, setImage] = useState('');
  const [sidecarText, setSidecarText] = useState('');
  const [importDraft, setImportDraft] = useState<{ source: string; events: PrevisEvent[]; previous: string } | null>(null);
  const [removal, setRemoval] = useState<{ index: number; snapshot: string; source?: string } | null>(null);
  const [timing, setTiming] = useState<{ index: number; snapshot: string; source?: string; event: PrevisEvent } | null>(null);
  const [duration, setDuration] = useState<number | undefined>();
  const generation = useRef(0);
  const changed = () => { setImage(''); setRemoval(null); setTiming(null); setImportDraft(null); };
  useEffect(() => { if (disabled) { generation.current++; setPending(false); setImage(''); } }, [disabled]);
  useEffect(() => { generation.current++; setObjects([]); setEvents([]); setDuration(undefined); setImage(''); setPending(false); setSidecarText(''); setImportDraft(null); setRemoval(null); setTiming(null); return () => { generation.current++; }; }, [sceneKey]);
  useEffect(() => () => { if (image) URL.revokeObjectURL(image); }, [image]);
  const off = disabled || pending;
  const assertSource = (source: string | undefined, clear: () => void) => {
    try { if (source !== sidecarSource?.()) throw Error('场景或计划来源已变化，请重新读取后准备'); }
    catch (error) { clear(); throw error; }
  };
  const prepareTiming = (index: number) => {
    if (off) return;
    setRemoval(null); setTiming(null);
    try { setTiming({ index, snapshot: JSON.stringify(events), source: sidecarSource?.(), event: structuredClone(events[index]) }); setError(''); }
    catch (e) { setError(String(e)); }
  };
  return <section aria-label="会话附着与事件效果" className="min-w-0 space-y-2 rounded border border-zinc-700 p-2 text-xs [&_label]:block [&_select]:block [&_select]:w-full [&_select]:bg-zinc-800 [&_select]:text-zinc-100 [&_input]:block [&_input]:w-full [&_input]:bg-zinc-800 [&_input]:text-zinc-100 [&_textarea]:w-full [&_textarea]:bg-zinc-800 [&_textarea]:text-zinc-100 [&_button]:block [&_button]:w-full [&_button]:rounded [&_button]:border [&_button]:border-zinc-600 [&_button]:px-1 [&_button]:py-1 [&_button:disabled]:opacity-40 [&_fieldset]:space-y-2 [&_pre]:max-h-48 [&_pre]:overflow-auto">
    <h4>道具附着与事件效果（会话临时，不随存档保存）</h4>
    <p>附着不等于闭合握持；火烟只是确定性粒子参考；飞散仅移动已有独立部件，不拆网格、不模拟燃烧。允许目标本身也在附着的链式附着（按层级先父后子，循环拒绝），但不含关节约束、物理接触或释放后弹道接力。播放与导出复用同一采样器。</p>
    <button disabled={off} onClick={() => { try { const nextObjects = list(), nextEvents = read(), nextDuration = timelineDuration?.(); setObjects(nextObjects); setEvents(nextEvents); setDuration(nextDuration); setError(''); changed(); } catch (e) { setError(String(e)); } }}>读取现有对象和骨骼</button>
    <select aria-label="事件类型" disabled={off} value={kind} onChange={e => setKind(e.target.value as PrevisEvent['kind'])}>{(['attach', 'fire', 'smoke', 'scatter'] as const).map(k => <option key={k} value={k}>{({ attach: '骨骼附着', fire: '火参考', smoke: '烟参考', scatter: '已有部件飞散' })[k]}</option>)}</select>
    <select aria-label="事件对象" disabled={off} value={objectId} onChange={e => setObjectId(e.target.value)}><option value="">选择道具/锚点/部件</option>{objects.map(o => <option key={o.id} value={o.id}>{o.name}</option>)}</select>
    {kind === 'attach' && <><select aria-label="附着演员" disabled={off} value={targetId} onChange={e => { setTargetId(e.target.value); setBone(''); }}><option value="">选择已有演员</option>{objects.filter(o => o.bones.length).map(o => <option key={o.id} value={o.id}>{o.name}</option>)}</select><select aria-label="附着真实骨骼" disabled={off} value={bone} onChange={e => setBone(e.target.value)}><option value="">明确选择骨骼</option>{objects.find(o => o.id === targetId)?.bones.map((b, i) => <option key={i} value={b}>{b}</option>)}</select></>}
    {[[start, setStart, '事件开始全片秒'], [end, setEnd, '事件结束全片秒'], [offset, setOffset, '局部偏移XYZ'], [rotation, setRotation, '局部旋转XYZ弧度'], [scale, setScale, '效果尺度'], [velocity, setVelocity, '飞散初速度XYZ米每秒']].map(([v, set, label]) => <label key={label as string}>{label as string}<input aria-label={label as string} value={v as string} disabled={off} onChange={e => (set as (s: string) => void)(e.target.value)} /></label>)}
    <button disabled={off} onClick={() => { try { const next = [...read(), { kind, objectId, targetId, bone, start: scalar(start), end: scalar(end), offset: vector(offset), rotation: vector(rotation), velocity: vector(velocity), scale: scalar(scale) }]; write(next); setEvents(next); changed(); setError(''); } catch (e) { setError(String(e)); } }}>确认追加临时事件</button>
    <button disabled={off} onClick={() => { try { write([]); setEvents([]); changed(); setError(''); } catch (e) { setError(String(e)); } }}>清除全部会话事件</button>
    <p>已读取{events.length}个临时事件；列表是读取快照，外部变更后须重读。截图仅代表采样时刻，修改场景后须重新取图。</p>
    <PrevisEventTimeline events={events} duration={duration} disabled={off} onPrepare={prepareTiming} />
    <ol aria-label="已读取会话事件列表" className="space-y-2">
      {events.map((event, index) => <li key={index} className="rounded border border-zinc-600 p-2">
        <p>事件{index + 1} · {event.kind} · {event.objectId} · 全片[{event.start}, {event.end})秒</p>
        <pre>{JSON.stringify(event, null, 2)}</pre>
        <button type="button" disabled={off} onClick={() => { setRemoval(null); setTiming(null); try { setRemoval({ index, snapshot: JSON.stringify(events), source: sidecarSource?.() }); setError(''); } catch (e) { setError(String(e)); } }}>准备删除事件{index + 1}（不应用）</button>
        <button type="button" disabled={off} onClick={() => prepareTiming(index)}>准备调整事件{index + 1}区间（不应用）</button>
      </li>)}
    </ol>
    {removal && <div aria-label="待确认单项事件删除">
      <p>待删除事件{removal.index + 1}；仅删读取快照中的这一项，其余事件顺序不变，不是撤销历史。</p>
      <button type="button" disabled={off} onClick={() => { try {
        assertSource(removal.source, () => setRemoval(null));
        const current = read();
        if (JSON.stringify(current) !== removal.snapshot || !Number.isSafeInteger(removal.index) || !current[removal.index]) { setRemoval(null); throw Error('当前事件已变化，请重新读取后准备删除'); }
        const next = current.filter((_, index) => index !== removal.index);
        write(next); setEvents(next); changed(); setError('');
      } catch (e) { setError(String(e)); } }}>确认仅删除事件{removal.index + 1}</button>
      <button type="button" onClick={() => setRemoval(null)}>放弃单项删除</button>
    </div>}
    {timing && <PrevisEventTimingEditor key={`${timing.index}:${timing.snapshot}`} event={timing.event} index={timing.index} disabled={off} onCancel={() => setTiming(null)} onConfirm={(start, end) => { try {
      assertSource(timing.source, () => setTiming(null));
      const current = read();
      if (JSON.stringify(current) !== timing.snapshot || !current[timing.index]) { setTiming(null); throw Error('当前事件已变化，请重新读取后调整区间'); }
      const next = current.map((event, index) => index === timing.index ? { ...event, start, end } : event);
      write(next); setEvents(next); changed(); setError('');
    } catch (e) { setError(String(e)); } }} />}
    {sidecarSource && <fieldset disabled={off}>
      <legend>事件旁档（单独保存JSON，不改场景存档格式）</legend>
      <button onClick={() => { try {
        const text = serializeEventSidecar(sidecarSource(), read());
        const url = URL.createObjectURL(new Blob([text], { type: 'application/json' }));
        try { const link = document.createElement('a'); link.href = url; link.download = 'previs-events.json'; link.click(); } finally { window.setTimeout(() => URL.revokeObjectURL(url), 10000); }
        setError('');
      } catch (e) { setError(String(e)); } }}>下载当前事件旁档JSON</button>
      <textarea aria-label="待导入事件旁档JSON" maxLength={131072} value={sidecarText} onChange={e => { setSidecarText(e.target.value); setImportDraft(null); }} />
      <button onClick={() => { try { const parsed = parseEventSidecar(sidecarText, sidecarSource()); setImportDraft({ ...parsed, previous: JSON.stringify(read()) }); setError(''); } catch (e) { setImportDraft(null); setError(String(e)); } }}>预览旁档导入（不应用）</button>
      {importDraft && <div><p>待确认整体替换：{importDraft.events.length}事件；对象、骨骼和区间将在确认时再次校验。不会追加或按名称猜配。</p>
        <pre>{JSON.stringify(importDraft.events, null, 2)}</pre>
        <button onClick={() => { try {
          if (sidecarSource() !== importDraft.source || JSON.stringify(read()) !== importDraft.previous) throw Error('场景、计划或当前事件已变化，请重新预览旁档');
          write(structuredClone(importDraft.events)); setEvents(structuredClone(importDraft.events)); changed(); setError('');
        } catch (e) { setImportDraft(null); setError(String(e)); } }}>确认替换为旁档事件</button>
      </div>}
    </fieldset>}
    <input aria-label="事件试演全片秒" value={seconds} disabled={off} onChange={e => setSeconds(e.target.value)} />
    <button disabled={off} onClick={async () => { const token = generation.current; setPending(true); setImage(''); try { const eventKey = JSON.stringify(read()), source = sidecarSource?.(); const blob = await preview(scalar(seconds)); if (generation.current !== token) return; if (eventKey !== JSON.stringify(read()) || source !== sidecarSource?.()) throw Error('场景、计划或事件已变化，请重新试演取图'); setImage(URL.createObjectURL(blob)); setError(''); } catch (e) { if (generation.current === token) setError(String(e)); } finally { if (generation.current === token) setPending(false); } }}>试演该时刻并取真实视口图（随后还原）</button>
    {image && <img src={image} alt="临时事件实际视口采样（不代表视觉批准，场景编辑后需重取）" />}
    {error && <p role="alert">{error}</p>}
  </section>;
}
