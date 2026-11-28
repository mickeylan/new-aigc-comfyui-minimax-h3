import { assertScriptShotMotionTargets, validateScriptShotPlan, type ScriptShotMotionAnimation, type ScriptShotMotionTarget, type ScriptShotPlan } from './scriptShotPlan';
import type { ExecutionClipTiming } from './shotExecution';

export type ExecutionClipRetime = { shotId: string; targetId: string; beatIndex: number; timing: ExecutionClipTiming; reason: string };

export type ExecutionClipCorrection = { shotId: string; targetId: string; beatIndex: number; animation: ScriptShotMotionAnimation; reason: string;
  /** Explicit phase edit: both clips restart at local zero, no blending or retiming. */
  split?: { at: number; afterAnimation: ScriptShotMotionAnimation };
  /** Shared boundary of two contiguous stationary beats. Clips remain unchanged. */
  boundary?: { at: number };
};
const RECORD_KIND = 'manual-performance-correction';

/** Data-only correction of an EXISTING beat, optionally split into two phases.
 * Overall source interval, linear root trajectory, camera and contacts stay intact.
 * Clip restarts are intentional edits, not continuity or contact-release proofs. Geometric approval
 * still belongs to the production application/sampling preflight, never this helper. */
export function correctExecutionBeatClip(plan: ScriptShotPlan, correction: ExecutionClipCorrection, targets: readonly ScriptShotMotionTarget[]): ScriptShotPlan {
  const base = structuredClone(validateScriptShotPlan(plan));
  if (new Set(targets.map(t => t.id)).size !== targets.length) throw Error('真实对象目录身份重复，请重新读取');
  assertScriptShotMotionTargets(base, targets);
  const shot = base.shots.find(s => s.id === correction.shotId);
  const actor = shot?.execution?.actors?.find(a => a.targetId === correction.targetId);
  const beat = Number.isSafeInteger(correction.beatIndex) && correction.beatIndex >= 0 ? actor?.beats[correction.beatIndex] : undefined;
  if (!shot || !actor || !beat) throw Error('所选镜头、执行角色或动作段已变化，请重新选择');
  // A retimed beat no longer means "clip from zero"; replacing, splitting or moving its boundary
  // would silently invalidate the authored window, so those edits are rejected instead.
  if (beat.timing) throw Error('带源区间或速率的片段暂不支持替换片段、拆分或移动分界');
  const reason = typeof correction.reason === 'string' ? correction.reason.trim() : '';
  if (!reason || reason.length > 1000) throw Error('请填写1–1000字人工纠正理由，不得当作原文事实');
  const target = targets.find(t => t.id === actor.targetId);
  const resolve = (wanted: ScriptShotMotionAnimation) => {
    const clip = Number.isSafeInteger(wanted?.index) && wanted.index >= 0 ? target?.animations[wanted.index] : undefined;
    if (!clip || clip.index !== wanted.index || clip.name !== wanted.name || clip.duration !== wanted.duration) throw Error('所选骨骼动作资源已变化，请重新读取并选择');
    return { index: clip.index, name: clip.name, duration: clip.duration };
  };
  const replacement = resolve(correction.animation);
  if (!correction.split && !correction.boundary && beat.animation.index === replacement.index && beat.animation.name === replacement.name && beat.animation.duration === replacement.duration) throw Error('请选择与当前动作不同的骨骼片段');
  // Append-only, bounded audit trail; never discard old corrections to make room.
  const count = base.diagnostics.filter(text => { try { return JSON.parse(text)?.kind === RECORD_KIND; } catch { return false; } }).length;
  if (count >= 128) throw Error('人工动作纠正记录已达128项，请另存并拆分场次');
  const record = { kind: RECORD_KIND, version: 1, shotId: shot.id, targetId: actor.targetId, beatIndex: correction.beatIndex,
    start: beat.start, end: beat.end, ...(beat.sourceIndex === undefined ? {} : { sourceIndex: beat.sourceIndex }), previous: beat.animation, replacement, reason,
    sourceFingerprint: base.sourceFingerprint, sceneFingerprint: base.sceneFingerprint };
  if (correction.boundary) {
    if (correction.split || JSON.stringify(beat.animation) !== JSON.stringify(replacement)) throw Error('调整分界不能同时拆段或替换片段');
    const next = actor.beats[correction.beatIndex + 1], at = correction.boundary.at;
    if (next?.timing) throw Error('下一段带源区间或速率，暂不支持移动分界');
    if (!next || beat.end !== next.start || beat.from.x !== beat.to.x || beat.from.z !== beat.to.z || next.from.x !== next.to.x || next.from.z !== next.to.z || beat.to.x !== next.from.x || beat.to.z !== next.from.z || beat.yaw !== next.yaw) throw Error('仅支持同位置同朝向且严格相接的两段静止动作分界');
    if (!Number.isFinite(at) || at - beat.start <= 1e-6 || next.end - at <= 1e-6 || Math.abs(at - beat.end) <= 1e-6) throw Error('新分界须不同于原分界且位于两段并集内部，两侧均须超过0.000001秒');
    const previousBeats = structuredClone([beat, next]);
    beat.end = at; next.start = at;
    base.diagnostics.push(JSON.stringify({ ...record, operation: 'move-stationary-boundary', boundaryAt: at, previousBeats, replacementBeats: [beat, next] }));
  } else if (correction.split) {
    const { at, afterAnimation } = correction.split;
    if (!Number.isFinite(at) || at - beat.start <= 1e-6 || beat.end - at <= 1e-6) throw Error('拆分分界须位于原动作段内部，两侧均须超过0.000001秒');
    const after = resolve(afterAnimation);
    const t = (at - beat.start) / (beat.end - beat.start);
    const middle = { x: beat.from.x + (beat.to.x - beat.from.x) * t, z: beat.from.z + (beat.to.z - beat.from.z) * t };
    const replacementBeats = [{ ...beat, end: at, to: middle, animation: replacement }, { ...beat, start: at, from: { ...middle }, animation: after }];
    base.diagnostics.push(JSON.stringify({ ...record, operation: 'split-beat', splitAt: at, previousBeat: beat, replacementBeats }));
    actor.beats.splice(correction.beatIndex, 1, ...replacementBeats);
  } else {
    base.diagnostics.push(JSON.stringify(record));
    beat.animation = replacement;
  }
  return assertScriptShotMotionTargets(validateScriptShotPlan(base), targets);
}

/** Authored source window / rate for ONE existing beat. Never re-splits the beat, never moves the
 * root trajectory, camera or contacts, and never guesses a clip from its name. */
export function retimeExecutionBeat(plan: ScriptShotPlan, correction: ExecutionClipRetime, targets: readonly ScriptShotMotionTarget[]): ScriptShotPlan {
  const base = structuredClone(validateScriptShotPlan(plan));
  if (new Set(targets.map(t => t.id)).size !== targets.length) throw Error('真实对象目录身份重复，请重新读取');
  assertScriptShotMotionTargets(base, targets);
  const shot = base.shots.find(s => s.id === correction.shotId);
  const actor = shot?.execution?.actors?.find(a => a.targetId === correction.targetId);
  const beat = Number.isSafeInteger(correction.beatIndex) && correction.beatIndex >= 0 ? actor?.beats[correction.beatIndex] : undefined;
  if (!shot || !actor || !beat) throw Error('所选镜头、执行角色或动作段已变化，请重新选择');
  const reason = typeof correction.reason === 'string' ? correction.reason.trim() : '';
  if (!reason || reason.length > 1000) throw Error('请填写1–1000字人工纠正理由，不得当作原文事实');
  const wanted = correction.timing as Record<string, unknown> | null | undefined;
  if (!wanted || typeof wanted !== 'object' || Array.isArray(wanted) || Object.keys(wanted).some(key => !['sourceIn', 'rate'].includes(key))) throw Error('源区间与速率格式无效');
  const sourceIn = wanted.sourceIn === undefined ? 0 : wanted.sourceIn, rate = wanted.rate === undefined ? 1 : wanted.rate;
  if (typeof sourceIn !== 'number' || !Number.isFinite(sourceIn) || sourceIn < 0 || sourceIn >= 86400) throw Error('片段入点须为 0–86400 秒内的有限数');
  if (typeof rate !== 'number' || !Number.isFinite(rate) || rate < .25 || rate > 4) throw Error('播放速率须在 0.25–4 之间');
  const clip = beat.animation, target = targets.find(t => t.id === actor.targetId);
  const resource = Number.isSafeInteger(clip.index) && clip.index >= 0 ? target?.animations[clip.index] : undefined;
  if (!resource || resource.index !== clip.index || resource.name !== clip.name || resource.duration !== clip.duration) throw Error('所选骨骼动作资源已变化，请重新读取并选择');
  const span = beat.end - beat.start;
  if (sourceIn >= clip.duration) throw Error('片段入点须小于片段时长');
  if (sourceIn + span * rate > clip.duration + 1e-6) throw Error(`源区间与速率会让片段超出 ${clip.duration} 秒；本段 ${span} 秒，最多可放慢到 ${Math.floor((clip.duration - sourceIn) / span * 1000) / 1000} 倍或把入点提前`);
  const timing: ExecutionClipTiming = { ...(wanted.sourceIn === undefined ? {} : { sourceIn }), ...(wanted.rate === undefined ? {} : { rate }) };
  const current = { sourceIn: beat.timing?.sourceIn ?? 0, rate: beat.timing?.rate ?? 1 };
  if (current.sourceIn === sourceIn && current.rate === rate) throw Error('请调整片段入点或速率');
  const count = base.diagnostics.filter(text => { try { return JSON.parse(text)?.kind === RECORD_KIND; } catch { return false; } }).length;
  if (count >= 128) throw Error('人工动作纠正记录已达128项，请另存并拆分场次');
  base.diagnostics.push(JSON.stringify({ kind: RECORD_KIND, version: 1, operation: 'retime-beat', shotId: shot.id, targetId: actor.targetId,
    beatIndex: correction.beatIndex, start: beat.start, end: beat.end, ...(beat.sourceIndex === undefined ? {} : { sourceIndex: beat.sourceIndex }),
    animation: clip, previousTiming: beat.timing ?? null, timing, reason, sourceFingerprint: base.sourceFingerprint, sceneFingerprint: base.sceneFingerprint }));
  if (Object.keys(timing).length) beat.timing = timing; else delete beat.timing;
  return assertScriptShotMotionTargets(validateScriptShotPlan(base), targets);
}
