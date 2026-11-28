import { Group, type Object3D, type Bone } from 'three';
import type { ScriptShotPlan } from './scriptShotPlan';
import { setSessionEvents, sessionEvents, type PrevisEvent } from './previsEvents';
const MAX_BYTES = 128 * 1024;
/** Stable content binding, deliberately excluding runtime UUID and scene epoch. */
export function captureEventSidecarSource(plan: ScriptShotPlan, entries: readonly { id: string; name: string; object: Object3D }[]) {
  if (new Set(entries.map(e => e.id)).size !== entries.length) throw Error('事件旁档对象身份重复');
  return JSON.stringify({ plan, objects: entries.map(e => {
    const bones: string[] = []; e.object.traverse(o => { if ((o as Bone).isBone) bones.push(o.name); });
    return { id: e.id, name: e.name, position: e.object.position.toArray(), quaternion: e.object.quaternion.toArray(), scale: e.object.scale.toArray(), bones };
  }) });
}
function record(value: unknown, keys: string[]): Record<string, unknown> {
  if (!value || typeof value !== 'object' || Array.isArray(value) || Object.keys(value).some(k => !keys.includes(k))) throw Error('事件旁档包含未知字段或无效对象');
  return value as Record<string, unknown>;
}
function text(value: unknown, max: number): asserts value is string {
  if (typeof value !== 'string' || !value.trim() || value.length > max) throw Error('事件旁档身份字段无效');
}
export function validateSidecarEvents(value: unknown): PrevisEvent[] {
  if (!Array.isArray(value) || value.length > 32) throw Error('事件旁档最多32事件');
  for (const raw of value) {
    const e = record(raw, ['kind', 'objectId', 'targetId', 'bone', 'start', 'end', 'offset', 'rotation', 'scale', 'velocity']);
    text(e.objectId, 160);
    if (e.targetId !== undefined && (typeof e.targetId !== 'string' || e.targetId.length > 160)) throw Error('附着身份无效');
    if (e.bone !== undefined && (typeof e.bone !== 'string' || e.bone.length > 200)) throw Error('骨骼身份无效');
  }
  // Reuse the runtime contract, including overlap and unsupported dependencies.
  const temporary = new Group(); setSessionEvents(temporary, value as PrevisEvent[]);
  return sessionEvents(temporary);
}
export function serializeEventSidecar(source: string, events: PrevisEvent[]): string {
  text(source, MAX_BYTES);
  const result = JSON.stringify({ version: 1, kind: 'previs-event-sidecar', source, events: validateSidecarEvents(events) }, null, 2);
  if (new TextEncoder().encode(result).length > MAX_BYTES) throw Error('事件旁档超过128KiB');
  return result;
}
export function parseEventSidecar(input: string, source: string) {
  if (new TextEncoder().encode(input).length > MAX_BYTES) throw Error('事件旁档超过128KiB');
  const document = record(JSON.parse(input), ['version', 'kind', 'source', 'events']);
  if (document.version !== 1 || document.kind !== 'previs-event-sidecar') throw Error('事件旁档版本不支持');
  text(document.source, MAX_BYTES); text(source, MAX_BYTES);
  if (document.source !== source) throw Error('事件旁档与当前场景/镜头表不一致，拒绝猜配');
  return { source, events: validateSidecarEvents(document.events) };
}
