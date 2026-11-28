import { validateScriptShotPlan, type ScriptShotPlan } from './scriptShotPlan';
export function observationFrames(plan: ScriptShotPlan, temporal = false) {
  validateScriptShotPlan(plan);
  const frames: { shotId: string; seconds: number; localSeconds: number }[] = [];
  for (const shot of plan.shots) {
    const duration = shot.end - shot.start, times = new Set([shot.start]);
    if (temporal) {
      times.add(shot.start + duration / 2); times.add(shot.end - Math.min(1 / 24, duration / 2));
      for (const actor of shot.execution?.actors ?? []) for (const beat of actor.beats) {
        for (const local of [beat.start, beat.end]) { const absolute = shot.start + local; if (absolute >= shot.start && absolute < shot.end) times.add(absolute); }
      }
    }
    for (const seconds of [...times].sort((a, b) => a - b)) {
      frames.push({ shotId: shot.id, seconds, localSeconds: seconds - shot.start });
      if (frames.length > 96) throw Error('观察截图超过96张预算，请拆分计划；不会截断证据');
    }
  }
  return frames;
}
const escape = (value: string) => value.replace(/[&<>"']/g, c => ({ '&': '&amp;', '<': '&lt;', '>': '&gt;', '"': '&quot;', "'": '&#39;' })[c]!);
export type ObservationFrame = { file: string; shotId: string; seconds: number; localSeconds: number; width: number; height: number; activeBeats?: { targetId: string; targetName: string; beatIndex: number; animation: string }[] };
export function observationReviewHTML(plan: ScriptShotPlan, frames: readonly ObservationFrame[]) {
  const names = new Set<string>();
  const cards = frames.map(frame => {
    if (!/^observation-\d{3}\.png$/.test(frame.file) || names.has(frame.file)) throw Error('观察截图文件映射无效'); names.add(frame.file);
    const shot = plan.shots.find(s => s.id === frame.shotId);
    if (!shot || !Number.isFinite(frame.seconds) || frame.seconds < shot.start || frame.seconds >= shot.end || frame.localSeconds !== frame.seconds - shot.start) throw Error('观察截图时间映射无效');
    const dialogue = shot.dialogue.filter(d => frame.seconds >= d.start && frame.seconds < d.end).map(d => `${d.speaker}：${d.line}`).join('\n');
    const active = (frame.activeBeats ?? []).map(b => `${b.targetName} (${b.targetId}) · 段${b.beatIndex + 1} · ${b.animation}`).join('\n');
    return `<figure><figcaption>镜${shot.order} · ${escape(shot.id)} · 全片${frame.seconds.toFixed(3)}秒 / 镜内${frame.localSeconds.toFixed(3)}秒</figcaption><img loading="lazy" src="${frame.file}" alt="真实视口观察"><pre>${escape(dialogue)}</pre><pre>${escape(active)}</pre></figure>`;
  }).join('\n');
  return '<!doctype html><html lang="zh"><meta charset="utf-8"><meta http-equiv="Content-Security-Policy" content="default-src \'none\'; img-src \'self\' data:; style-src \'unsafe-inline\'"><title>时序观察审阅</title><style>body{background:#18181b;color:#eee;font:16px sans-serif;margin:24px}main{display:grid;grid-template-columns:repeat(auto-fit,minmax(280px,1fr));gap:16px}figure{margin:0;border:1px solid #666;padding:12px}img{width:100%;height:auto}pre{white-space:pre-wrap;overflow-wrap:anywhere}</style><h1>预演时序证据审阅</h1><p>visuallyApproved=false。有限离散截图，不证明动作完成、连续无遮挡或闭合握持。活动动作是计划元数据；对白只读无音频。本页无模型调用，不修改计划。</p><main>' + cards + '</main></html>';
}
