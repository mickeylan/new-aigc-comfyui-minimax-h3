import type { ScriptShotCamera, ScriptShotMotionAnimation } from './scriptShotPlan';
import { validateWristContactBinding, type WristContactBinding } from './characterMotion';

/** Authored playback window of an existing clip. Defaults mean the legacy "from zero, real time" behaviour.
 * sourceIn is the clip second to start from; rate scales playback without touching the root trajectory. */
export type ExecutionClipTiming = { sourceIn?: number; rate?: number };
export type ExecutionBeat = { start: number; end: number; from: { x: number; z: number }; to: { x: number; z: number }; yaw: number; animation: ScriptShotMotionAnimation; sourceIndex?: number; timing?: ExecutionClipTiming };
export type ExecutionActor = { targetId: string; targetName: string; beats: ExecutionBeat[] };
/** First contact version is a held wrist surface contact for the ENTIRE shot.
 * Approach/release, closed fingers and balance recovery are not implied. */
export type ExecutionWristContact = { version: 1; policy: 'whole-skin-separating-plane-v1'; sourceId: string; targetId: string; start: number; end: number; binding: WristContactBinding; normal: [number, number, number] };
export type ShotExecution = { version: 1; camera?: { keys: (ScriptShotCamera & { at: number })[] }; actors?: ExecutionActor[]; contacts?: ExecutionWristContact[] };
// Same 1e-6-second boundary convention as ScriptShotPlan; accumulated cuts are not exact decimals.
const TIME_TOLERANCE = 1e-6;
const fail = (): never => { throw Error('镜内执行配置无效（版本、区间、资源或坐标超出支持范围）'); };
function object(value: unknown, keys: string[]): Record<string, any> {
  if (!value || typeof value !== 'object' || Array.isArray(value) || Object.keys(value).some(key => !keys.includes(key))) fail();
  return value as Record<string, any>;
}
function finite(value: unknown, min = -1000, max = 1000): number {
  if (typeof value !== 'number' || !Number.isFinite(value) || value < min || value > max) fail();
  return value as number;
}
function text(value: unknown, max = 160): string {
  if (typeof value !== 'string' || !value.trim() || value.length > max) fail();
  return value as string;
}
function point(value: unknown) { const p = object(value, ['x', 'z']); return { x: finite(p.x), z: finite(p.z) }; }
function vector(value: unknown): [number, number, number] {
  if (!Array.isArray(value) || value.length !== 3) fail();
  return (value as unknown[]).map(v => finite(v)) as [number, number, number];
}
/** Minimum squared separation of two linear trajectories over a unit interval. */
function separation(a: number[], b: number[]) {
  const delta = b.map((v, i) => v - a[i]);
  const denominator = delta.reduce((n, v) => n + v * v, 0);
  const t = denominator ? Math.max(0, Math.min(1, -a.reduce((n, v, i) => n + v * delta[i], 0) / denominator)) : 0;
  return a.reduce((n, v, i) => n + (v + delta[i] * t) ** 2, 0);
}
export function validateShotExecution(value: unknown, duration: number, clearance = .7): ShotExecution {
  const e = object(value, ['version', 'camera', 'actors', 'contacts']);
  if (e.version !== 1 || !Number.isFinite(duration) || duration <= 0) fail();
  const result: ShotExecution = { version: 1 };
  if (e.camera !== undefined) {
    const c = object(e.camera, ['keys']);
    if (!Array.isArray(c.keys) || c.keys.length < 2 || c.keys.length > 64) fail();
    const keys = c.keys.map((value: unknown) => {
      const k = object(value, ['at', 'position', 'target', 'fov']);
      return { at: finite(k.at, 0, duration + TIME_TOLERANCE), position: vector(k.position), target: vector(k.target), fov: finite(k.fov, 15, 90) };
    });
    if (keys[0].at !== 0 || Math.abs(keys[keys.length - 1].at - duration) > TIME_TOLERANCE) fail();
    for (let i = 1; i < keys.length; i++) {
      const a = keys[i - 1], b = keys[i];
      if (a.at >= b.at || separation(a.position.map((v: number, j: number) => v - a.target[j]), b.position.map((v: number, j: number) => v - b.target[j])) <= 1e-12) fail();
    }
    result.camera = { keys };
  }
  if (e.actors !== undefined) {
    if (!Array.isArray(e.actors) || !e.actors.length || e.actors.length > 32) fail();
    let count = 0;
    const ids = new Set<string>();
    result.actors = e.actors.map((value: unknown) => {
      const a = object(value, ['targetId', 'targetName', 'beats']);
      const targetId = text(a.targetId), targetName = text(a.targetName);
      if (ids.has(targetId)) fail(); ids.add(targetId);
      if (!Array.isArray(a.beats) || !a.beats.length || (count += a.beats.length) > 128) fail();
      const beats: ExecutionBeat[] = a.beats.map((value: unknown) => {
        const b = object(value, ['start', 'end', 'from', 'to', 'yaw', 'animation', 'sourceIndex', 'timing']);
        const start = finite(b.start, 0, duration + TIME_TOLERANCE), end = finite(b.end, 0, duration + TIME_TOLERANCE);
        if (end <= start) fail();
        const c = object(b.animation, ['index', 'name', 'duration']);
        const index = finite(c.index, 0, Number.MAX_SAFE_INTEGER);
        if (!Number.isSafeInteger(index) || (b.sourceIndex !== undefined && !Number.isSafeInteger(b.sourceIndex))) fail();
        const clip = { index, name: text(c.name, 200), duration: finite(c.duration, Number.MIN_VALUE, 86400) };
        let timing: ExecutionClipTiming | undefined;
        if (b.timing !== undefined) {
          const t = object(b.timing, ['sourceIn', 'rate']);
          const sourceIn = t.sourceIn === undefined ? 0 : finite(t.sourceIn, 0, 86400);
          const rate = t.rate === undefined ? 1 : finite(t.rate, .25, 4);
          // The authored window must land inside the clip; a frozen last frame is never implicit.
          if (sourceIn >= clip.duration || sourceIn + (end - start) * rate > clip.duration + TIME_TOLERANCE) fail();
          timing = { ...(t.sourceIn === undefined ? {} : { sourceIn }), ...(t.rate === undefined ? {} : { rate }) };
        }
        return { start, end, from: point(b.from), to: point(b.to), yaw: finite(b.yaw, -2 * Math.PI, 2 * Math.PI), animation: clip, ...(b.sourceIndex === undefined ? {} : { sourceIndex: finite(b.sourceIndex, 0, 127) }), ...(timing ? { timing } : {}) };
      });
      for (let i = 1; i < beats.length; i++) {
        const a = beats[i - 1], b = beats[i];
        if (b.start < a.end || a.to.x !== b.from.x || a.to.z !== b.from.z) fail();
      }
      return { targetId, targetName, beats };
    });
    if (e.contacts !== undefined) {
      if (!Array.isArray(e.contacts) || !e.contacts.length || e.contacts.length > 16) fail();
      const participants = new Set<string>();
      result.contacts = e.contacts.map((value: unknown) => {
        const c = object(value, ['version', 'policy', 'sourceId', 'targetId', 'start', 'end', 'binding', 'normal']);
        const sourceId = text(c.sourceId), targetId = text(c.targetId), normal = vector(c.normal);
        const start = finite(c.start, 0, duration), end = finite(c.end, 0, duration + TIME_TOLERANCE);
        if (c.version !== 1 || c.policy !== 'whole-skin-separating-plane-v1' || sourceId === targetId || !ids.has(sourceId) || !ids.has(targetId) || participants.has(sourceId) || participants.has(targetId) || start !== 0 || Math.abs(end - duration) > TIME_TOLERANCE || Math.abs(normal.reduce((s, n) => s + n * n, 0) - 1) > 1e-6) fail();
        participants.add(sourceId); participants.add(targetId);
        return { version: 1, policy: c.policy, sourceId, targetId, start, end, binding: validateWristContactBinding(c.binding), normal };
      });
    }
    // Piecewise-linear relative motion: include every beat boundary and held gap.
    const times = [...new Set([0, duration, ...result.actors.flatMap(a => a.beats.flatMap(b => [b.start, b.end]))])].sort((a, b) => a - b);
    for (let k = 1; k < times.length; k++) {
      const a = sampleExecutionActors(result, times[k - 1]), b = sampleExecutionActors(result, times[k]);
      for (let i = 0; i < a.length; i++) for (let j = i + 1; j < a.length; j++) {
        const distanceSquared = separation([a[i].position.x - a[j].position.x, a[i].position.z - a[j].position.z], [b[i].position.x - b[j].position.x, b[i].position.z - b[j].position.z]);
        const posedProofRequired = result.contacts?.some(c => [c.sourceId, c.targetId].includes(a[i].targetId) && [c.sourceId, c.targetId].includes(a[j].targetId));
        // The .7m floor NEVER changes. A declared pair can defer only the extra
        // coarse editing-envelope clearance to a mandatory whole-skin pose proof
        // in the shared sampler. This validator alone is NOT geometric approval.
        if (distanceSquared <= .7 ** 2 || (!posedProofRequired && distanceSquared <= Math.max(.7, clearance) ** 2)) throw Error('镜内执行角色间距不足，无法证明安全');
      }
    }
  }
  if ((!result.camera && !result.actors) || (e.contacts !== undefined && !result.contacts)) fail();
  return result;
}
export function sampleExecutionCamera(execution: ShotExecution | undefined, seconds: number): ScriptShotCamera | null {
  const keys = execution?.camera?.keys;
  if (!keys) return null;
  const b = keys.find(k => k.at >= seconds) ?? keys[keys.length - 1];
  const a = keys[Math.max(0, keys.indexOf(b) - 1)];
  const t = b.at === a.at ? 0 : Math.max(0, Math.min(1, (seconds - a.at) / (b.at - a.at)));
  const lerp = (x: number, y: number) => x + (y - x) * t;
  return { position: a.position.map((v, i) => lerp(v, b.position[i])) as [number, number, number], target: a.target.map((v, i) => lerp(v, b.target[i])) as [number, number, number], fov: lerp(a.fov, b.fov) };
}
export function sampleExecutionActors(execution: ShotExecution | undefined, seconds: number) {
  return (execution?.actors ?? []).map(actor => {
    const beat = [...actor.beats].reverse().find(b => seconds >= b.start) ?? actor.beats[0];
    const local = Math.max(0, Math.min(beat.end - beat.start, seconds - beat.start));
    const progress = local / (beat.end - beat.start);
    return { targetId: actor.targetId, targetName: actor.targetName, progress, position: { x: beat.from.x + (beat.to.x - beat.from.x) * progress, z: beat.from.z + (beat.to.z - beat.from.z) * progress }, yaw: beat.yaw, animation: beat.animation, animationSeconds: Math.min(beat.animation.duration, (beat.timing?.sourceIn ?? 0) + local * (beat.timing?.rate ?? 1)), finished: progress === 1 };
  });
}
