import { assertScriptShotMotionTargets, validateScriptShotPlan, type ScriptShotMotionAnimation, type ScriptShotMotionTarget, type ScriptShotPlan } from './scriptShotPlan';

export type ExecutionClipCorrection = { shotId: string; targetId: string; beatIndex: number; animation: ScriptShotMotionAnimation; reason: string;
  /** Explicit phase edit: both clips restart at local zero, no blending or retiming. */
  split?: { at: number; afterAnimation: ScriptShotMotionAnimation };
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
  const reason = typeof correction.reason === 'string' ? correction.reason.trim() : '';
  if (!reason || reason.length > 1000) throw Error('请填写1–1000字人工纠正理由，不得当作原文事实');
  const target = targets.find(t => t.id === actor.targetId);
  const resolve = (wanted: ScriptShotMotionAnimation) => {
    const clip = Number.isSafeInteger(wanted?.index) && wanted.index >= 0 ? target?.animations[wanted.index] : undefined;
    if (!clip || clip.index !== wanted.index || clip.name !== wanted.name || clip.duration !== wanted.duration) throw Error('所选骨骼动作资源已变化，请重新读取并选择');
    return { index: clip.index, name: clip.name, duration: clip.duration };
  };
  const replacement = resolve(correction.animation);
  if (!correction.split && beat.animation.index === replacement.index && beat.animation.name === replacement.name && beat.animation.duration === replacement.duration) throw Error('请选择与当前动作不同的骨骼片段');
  // Append-only, bounded audit trail; never discard old corrections to make room.
  const count = base.diagnostics.filter(text => { try { return JSON.parse(text)?.kind === RECORD_KIND; } catch { return false; } }).length;
  if (count >= 128) throw Error('人工动作纠正记录已达128项，请另存并拆分场次');
  const record = { kind: RECORD_KIND, version: 1, shotId: shot.id, targetId: actor.targetId, beatIndex: correction.beatIndex,
    start: beat.start, end: beat.end, ...(beat.sourceIndex === undefined ? {} : { sourceIndex: beat.sourceIndex }), previous: beat.animation, replacement, reason,
    sourceFingerprint: base.sourceFingerprint, sceneFingerprint: base.sceneFingerprint };
  if (correction.split) {
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
