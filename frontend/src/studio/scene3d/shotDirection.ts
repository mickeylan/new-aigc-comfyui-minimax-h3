import * as THREE from 'three';
import { validateScriptShotPlan, type ScriptShotPlan, type ScriptShotMotionAnimation } from './scriptShotPlan';
import { groundHeightAt, type SceneEnvironment } from './ground-path';
import { snapshotShotBounds, type ShotSceneEntry, type ShotNavigation } from './shotNavigation';

export type ShotDirectionGuide = {
  speakerTargets: Readonly<Record<string, string>>;
  animationRefs: Readonly<Record<string, ScriptShotMotionAnimation>>;
  framing: 'wide' | 'medium' | 'close';
  /** Radians, world XZ. Alternation is opt-in, never inferred from dialogue. */
  azimuth: number;
  alternateAzimuth?: boolean;
  distance: number;
  travel: number;
  avoid: boolean;
  navigation?: ShotNavigation;
};

/** Deterministic suggestions, no scene mutation. One distinct explicitly mapped subject
 * per shot; multi-subject shots reject rather than silently choosing a speaker.
 * Existing cameras/motions are retained; dialogue/source/unknown JSON fields survive.
 * References must match actual Object3D animation resources, not semantic guesses.
 */
export function draftShotDirection(plan: ScriptShotPlan, entries: readonly ShotSceneEntry[], guide: ShotDirectionGuide, environment: SceneEnvironment | null): ScriptShotPlan {
  validateScriptShotPlan(plan);
  if (!['wide', 'medium', 'close'].includes(guide.framing) || !Number.isFinite(guide.azimuth) || !Number.isFinite(guide.distance) || guide.distance <= 0 || guide.distance > 1000 || !Number.isFinite(guide.travel) || guide.travel <= 0 || guide.travel > 1000 || typeof guide.avoid !== 'boolean') throw Error('Invalid direction guide/framing');
  if (!guide.speakerTargets || !guide.animationRefs || (guide.alternateAzimuth !== undefined && typeof guide.alternateAzimuth !== 'boolean')) throw Error('Missing explicit direction guide');
  if (new Set(entries.map(e => e.id)).size !== entries.length) throw Error('Duplicate direction target');
  const result = structuredClone(plan);
  const positions = new Map(entries.map(e => [e.id, { x: e.object.position.x, z: e.object.position.z }]));
  result.shots.forEach((shot, index) => {
    const ids = new Set(shot.dialogue.map(line => {
      if (!Object.hasOwn(guide.speakerTargets, line.speaker) || !guide.speakerTargets[line.speaker]) throw Error(`Missing speaker mapping: ${line.speaker}`);
      return guide.speakerTargets[line.speaker]!;
    }));
    if (ids.size !== 1) throw Error('Direction requires exactly one explicitly mapped subject per shot');
    const id = [...ids][0]!;
    const entry = entries.find(e => e.id === id);
    if (!entry || entry.locked || entry.splat || !entry.object.visible) throw Error(`Invalid direction target: ${id}`);
    const ref = Object.hasOwn(guide.animationRefs, id) ? guide.animationRefs[id] : undefined;
    const clip = ref && entry.object.animations[ref.index];
    if (!ref || !Number.isSafeInteger(ref.index) || ref.index < 0 || !clip || clip.name !== ref.name || clip.duration !== ref.duration || ref.duration <= 0) throw Error(`Missing animation reference: ${id}`);
    const box = snapshotShotBounds(entry.object);
    if (box.isEmpty() || ![...box.min.toArray(), ...box.max.toArray()].every(Number.isFinite)) throw Error('Direction subject has no finite bounds');
    const start = positions.get(id)!;
    const angle = guide.azimuth + (guide.alternateAzimuth && index % 2 ? Math.PI : 0);
    if (!shot.motion) {
      const duration = shot.end - shot.start;
      const end = { x: start.x + Math.sin(angle) * guide.travel, z: start.z + Math.cos(angle) * guide.travel };
      const nav = guide.navigation ?? { version: 1, mode: 'avoid', radius: Math.max(0.1, Math.hypot(Math.max(Math.abs(box.min.x-entry.object.position.x),Math.abs(box.max.x-entry.object.position.x)), Math.max(Math.abs(box.min.z-entry.object.position.z),Math.abs(box.max.z-entry.object.position.z)))), speed: Math.max(1, guide.travel * 3 / duration) };
      if (guide.avoid && (nav.version !== 1 || nav.mode !== 'avoid' || !Number.isFinite(nav.radius) || nav.radius <= 0 || nav.radius > 10 || !Number.isFinite(nav.speed) || nav.speed <= 0 || nav.speed > 100)) throw Error('Invalid direction navigation');
      shot.motion = { targetId: id, targetName: entry.name, start: { ...start }, end, duration, animation: { ...ref }, ...(guide.avoid ? { navigation: { ...nav } } : {}) };
    }
    if (!shot.camera) {
      const center = box.getCenter(new THREE.Vector3());
      const height = box.max.y - box.min.y;
      const target: [number, number, number] = [center.x + start.x - entry.object.position.x, center.y, center.z + start.z - entry.object.position.z];
      const fov = { wide: 55, medium: 40, close: 28 }[guide.framing];
      const d = Math.max(guide.distance, height / (2 * Math.tan(fov * Math.PI / 360)) * 1.2);
      const x = target[0] + Math.sin(angle) * d, z = target[2] + Math.cos(angle) * d;
      shot.camera = { target, position: [x, Math.max(target[1] + height * 0.2, groundHeightAt(environment?.terrain, x, z) + 0.3), z], fov };
    }
    positions.set(shot.motion.targetId, { ...shot.motion.end });
  });
  return validateScriptShotPlan(result);
}
