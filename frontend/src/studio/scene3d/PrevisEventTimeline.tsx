import type { PrevisEvent } from './previsEvents';

/** Read snapshot only. Choosing a row prepares E5; it never edits or samples. */
export default function PrevisEventTimeline({ events, duration, disabled, onPrepare }: {
  events: PrevisEvent[]; duration?: number; disabled: boolean; onPrepare(index: number): void;
}) {
  const hasScale = Number.isFinite(duration) && duration! > 0;
  return <section aria-label="事件区间时间轴" className="min-w-0 space-y-2 rounded border border-zinc-600 p-2">
    <h5>事件区间时间轴（读取快照，点选仅准备调整）</h5>
    <p>{hasScale ? `全片标尺：0 — ${duration}秒；每行同一比例，区间左闭右开。` : '无有效计划时长，仅展示事件区间，不表示全片比例。'}</p>
    {!events.length && <p>尚无已读取事件。</p>}
    {events.map((event, index) => {
      const valid = hasScale && Number.isFinite(event.start) && Number.isFinite(event.end) && event.start >= 0 && event.end > event.start && event.end <= duration!;
      return <div key={index} className="min-w-0 space-y-1">
        <button type="button" disabled={disabled} className="break-all text-left" onClick={() => onPrepare(index)} aria-label={`时间轴准备调整事件${index + 1}区间`}>
          事件{index + 1} · {event.kind} · {event.objectId} · [{String(event.start)}, {String(event.end)})秒
        </button>
        {valid ? <div aria-label={`事件${index + 1}全片比例轨道`} className="relative h-3 overflow-hidden rounded bg-zinc-800">
          <span data-event-bar={index + 1} className="absolute inset-y-0 bg-cyan-400" style={{ left: `${event.start / duration! * 100}%`, width: `${(event.end - event.start) / duration! * 100}%` }} />
        </div> : hasScale && <p>区间非法或超出全片，未绘制比例；原值未修改。</p>}
      </div>;
    })}
  </section>;
}
