import type { ScriptShotPlan } from './scriptShotPlan';
export type PrevisPlaybackState = { index: number; shots: number; line?: string | null; seconds?: number; plan?: ScriptShotPlan };
/** Displays only actual sampler output for the applied plan, never a draft or UI clock. */
export default function PrevisPlaybackTimeline({ plan, playback }: { plan?: ScriptShotPlan | null; playback?: PrevisPlaybackState | null }) {
  if (!plan || !playback || playback.plan !== plan || !Number.isFinite(playback.seconds) || !Number.isInteger(playback.index) || playback.shots !== plan.shots.length) return null;
  const shot = plan.shots[playback.index], seconds = playback.seconds!;
  if (!shot || !Number.isFinite(shot.start) || !Number.isFinite(shot.end) || shot.end <= shot.start || seconds < shot.start || seconds >= shot.end) return null;
  const local = seconds - shot.start, duration = shot.end - shot.start, actors = shot.execution?.actors ?? [];
  return <section aria-label="已应用计划真实播放头" className="space-y-2 rounded border border-cyan-700 p-2">
    <h4>真实播放头（只读）</h4>
    <p>第{shot.order}镜 · 全片{seconds.toFixed(3)}秒 · 镜内{local.toFixed(3)}秒</p>
    <p className="text-zinc-400">来自共享采样器，显示约10Hz更新；不是审阅游标，不可拖动，不改动作或对白。</p>
    <div role="progressbar" aria-label="真实镜内播放进度" aria-valuemin={0} aria-valuemax={duration} aria-valuenow={local} className="relative h-3 bg-zinc-900">
      <span className="absolute h-3 border-l-2 border-cyan-300" style={{ left: `${local / duration * 100}%` }} />
    </div>
    {actors.map(actor => <div key={actor.targetId} aria-label={`真实播放轨道 ${actor.targetName}`}>
      <p>{actor.targetName}：{actor.beats.filter(b => local >= b.start && local < b.end).map(b => b.animation.name).join('、') || '无活动段'}</p>
      <div className="relative h-8 bg-zinc-900">
        {actor.beats.map((beat, i) => <span key={i} className={`absolute h-8 overflow-hidden border px-1 text-xs ${local >= beat.start && local < beat.end ? 'border-cyan-300 bg-cyan-900' : 'border-zinc-500 bg-zinc-700'}`} style={{ left: `${beat.start / duration * 100}%`, width: `${(beat.end - beat.start) / duration * 100}%` }}>{beat.start}–{beat.end}s · {beat.animation.name}</span>)}
        <span aria-hidden="true" className="pointer-events-none absolute z-10 h-8 border-l-2 border-cyan-300" style={{ left: `${local / duration * 100}%` }} />
      </div>
    </div>)}
    {!actors.length && <p>本镜无骨骼动作轨道；播放头仍代表实际机位采样时间。</p>}
  </section>;
}
