import * as THREE from 'three';
import { validateScriptShotPlan, type ScriptShotPlan, type ScriptShotMotion } from './scriptShotPlan';
import { proveWalkableLine, WALK_MAX_SLOPE, type GroundObstacle, type SceneEnvironment } from './ground-path';

export type ShotSceneEntry = { id: string; name: string; object: THREE.Object3D; locked?: boolean; splat?: unknown };
export type ShotNavigation = { version: 1; mode: 'avoid'; radius: number; speed: number };
type Point = { x: number; z: number };
type Motion = ScriptShotMotion & { navigation?: ShotNavigation };
const distance = (a: Point, b: Point) => Math.hypot(a.x - b.x, a.z - b.z);

/** Bounds without touching editing-scene matrices or shared geometry caches.
 * Local-XZ movement requires identity ancestors; transformed parents reject explicitly. */
export function snapshotShotBounds(object: THREE.Object3D): THREE.Box3 {
  for (let parent = object.parent; parent; parent = parent.parent) {
    if (parent.position.lengthSq() > 1e-12 || parent.quaternion.angleTo(new THREE.Quaternion()) > 1e-6 || parent.scale.distanceTo(new THREE.Vector3(1, 1, 1)) > 1e-6) throw Error('Shot movement requires untransformed ancestors');
  }
  const clone = object.clone(true);
  clone.traverse(node => {
    const mesh = node as THREE.Mesh;
    if (mesh.geometry) mesh.geometry = mesh.geometry.clone();
  });
  try { clone.updateMatrixWorld(true); return new THREE.Box3().setFromObject(clone); }
  finally { clone.traverse(node => { (node as THREE.Mesh).geometry?.dispose(); }); }
}

/** Snapshot-only sequential reservations. One actor moves per shot: every other actor
 * occupies its original position until its first shot and its last endpoint thereafter.
 * Thus each visibility edge is a continuous swept test against the EXACT occupancy
 * for that time interval, not a globally actor-excluded static approximation.
 * seconds in sample() is ABSOLUTE timeline time. Absent navigation is not compiled;
 * callers must retain their legacy line-reject validator and sampler for those shots.
 * Bounds: 128 entries, 512 visibility vertices, 200000 edge proofs/session.
 */
export function compileShotNavigation(plan: ScriptShotPlan, entries: readonly ShotSceneEntry[], environment: SceneEnvironment | null) {
  validateScriptShotPlan(plan);
  const hasNavigation = plan.shots.some(s => s.motion?.navigation);
  if (!hasNavigation) return { proof: { version: 1, policy: 'sequential-time-reservations-visibility-v1', checks: 0, shots: [] }, sample(_id: string, _seconds: number): {position: Point; yaw: number} { throw Error('Unknown avoid shot or invalid sample time'); } };
  if (entries.length > 128) throw Error('Navigation entry budget exceeded');
  const terrain = environment?.terrain ? structuredClone(environment.terrain) : null;
  const snapshots = entries.map(entry => {
    const box = snapshotShotBounds(entry.object);
    const p = entry.object.position;
    return { id: entry.id, name: entry.name, locked: entry.locked, excluded: !!entry.splat || !entry.object.visible,
      position: { x: p.x, z: p.z }, box, clips: entry.object.animations.map(c => ({ name: c.name, duration: c.duration })) };
  });
  if (new Set(snapshots.map(e => e.id)).size !== snapshots.length) throw Error('Duplicate navigation target');
  const positions = new Map(snapshots.map(e => [e.id, { ...e.position }]));
  const routes = new Map<string, { points: Point[]; length: number; start: number; duration: number }>();
  const proofs: { shotId: string; targetId: string; start: number; end: number; points: Point[]; length: number; blockers: GroundObstacle[] }[] = [];
  let checks = 0, terrainChecks = 0;
  // Over-approximate every touched cell of the swept disc. Check both exact
  // triangle gradients and all wet vertices, not three sparse parallel tracks.
  const terrainSafe = (a: Point, b: Point, radius: number) => {
    if (!terrain) return true;
    const half = terrain.size / 2, cell = terrain.size / terrain.segments, width = terrain.segments + 1;
    if ([a, b].some(p => Math.abs(p.x) + radius > half || Math.abs(p.z) + radius > half)) return false;
    const col = (x: number) => Math.max(0, Math.min(terrain.segments - 1, Math.floor((x + half) / cell)));
    const row = col;
    for (let z = row(Math.min(a.z,b.z)-radius); z <= row(Math.max(a.z,b.z)+radius); z++) for (let x = col(Math.min(a.x,b.x)-radius); x <= col(Math.max(a.x,b.x)+radius); x++) {
      if (++terrainChecks > 1_000_000) throw Error('Navigation terrain-cell budget exceeded');
      const cx = (x + 0.5) * cell - half, cz = (z + 0.5) * cell - half;
      const dx = b.x-a.x, dz=b.z-a.z, lengthSq=dx*dx+dz*dz;
      const t = lengthSq ? Math.max(0,Math.min(1,((cx-a.x)*dx+(cz-a.z)*dz)/lengthSq)) : 0;
      if (Math.hypot(cx-a.x-dx*t,cz-a.z-dz*t) > radius + cell * Math.SQRT2 / 2) continue;
      const i=z*width+x, ids=[i,i+1,i+width,i+width+1], h=ids.map(index=>terrain.heights[index]);
      if (ids.some(index=>typeof terrain.waterLevels?.[index]==='number') || !h.every(Number.isFinite)) return false;
      if (Math.hypot((h[1]-h[0])/cell,(h[2]-h[0])/cell)>WALK_MAX_SLOPE || Math.hypot((h[3]-h[2])/cell,(h[3]-h[1])/cell)>WALK_MAX_SLOPE) return false;
    }
    return true;
  };
  for (const shot of plan.shots) {
    const motion = shot.motion as Motion | null | undefined;
    if (!motion) continue;
    const actor = snapshots.find(e => e.id === motion.targetId);
    if (!actor || actor.excluded || actor.locked || actor.name !== motion.targetName) throw Error('Invalid navigation target');
    if (motion.animation) {
      const clip = actor.clips[motion.animation.index];
      if (!clip || clip.name !== motion.animation.name || clip.duration !== motion.animation.duration) throw Error('Missing navigation animation reference');
    }
    const current = positions.get(actor.id)!;
    const nav = motion.navigation;
    if (nav && distance(current, motion.start) > 1e-5) throw Error('Navigation start must match reserved actor position');
    if (nav && (nav.version !== 1 || nav.mode !== 'avoid' || !Number.isFinite(nav.radius) || nav.radius <= 0 || nav.radius > 10 || !Number.isFinite(nav.speed) || nav.speed <= 0 || nav.speed > 100)) throw Error('Invalid navigation settings');
    const bodyRadius = actor.box.isEmpty() ? 0 : Math.hypot(Math.max(Math.abs(actor.box.min.x-actor.position.x),Math.abs(actor.box.max.x-actor.position.x)),Math.max(Math.abs(actor.box.min.z-actor.position.z),Math.abs(actor.box.max.z-actor.position.z)));
    const margin = nav ? Math.max(nav.radius, bodyRadius) : 0;
    const obstacles: GroundObstacle[] = snapshots.flatMap(e => {
      if (e.id === actor.id || e.excluded || e.box.isEmpty()) return [];
      const p = positions.get(e.id)!;
      const dx = p.x - e.position.x, dz = p.z - e.position.z;
      // Completed movers may have turned: reserve their full yaw-invariant envelope.
      const moved = proofs.some(proof => proof.targetId === e.id) || plan.shots.slice(0, shot.order - 1).some(s => s.motion?.targetId === e.id);
      const r = Math.hypot(Math.max(Math.abs(e.box.min.x - e.position.x), Math.abs(e.box.max.x - e.position.x)), Math.max(Math.abs(e.box.min.z - e.position.z), Math.abs(e.box.max.z - e.position.z)));
      return [{ name: e.name, min: [moved ? p.x - r - margin : e.box.min.x + dx - margin, moved ? p.z - r - margin : e.box.min.z + dz - margin] as [number, number], max: [moved ? p.x + r + margin : e.box.max.x + dx + margin, moved ? p.z + r + margin : e.box.max.z + dz + margin] as [number, number] }];
    });
    if (nav) {
      const points: Point[] = [{ ...motion.start }, { ...motion.end }];
      for (const b of obstacles) for (const x of [b.min[0] - 0.001, b.max[0] + 0.001]) for (const z of [b.min[1] - 0.001, b.max[1] + 0.001]) points.push({ x, z });
      if (points.length > 512) throw Error('Navigation vertex budget exceeded');
      const costs = points.map(() => Infinity), previous = points.map(() => -1), done = new Set<number>();
      costs[0] = 0;
      for (;;) {
        let u = -1;
        for (let i = 0; i < points.length; i++) if (!done.has(i) && (u < 0 || costs[i]! < costs[u]!)) u = i;
        if (u < 0 || !Number.isFinite(costs[u])) throw Error(`No safe route for ${shot.id}`);
        if (u === 1) break;
        done.add(u);
        for (let v = 0; v < points.length; v++) {
          if (done.has(v)) continue;
          const cost = costs[u]! + distance(points[u]!, points[v]!);
          if (cost >= costs[v]!) continue;
          if (++checks > 200000) throw Error('Navigation edge budget exceeded');
          const a = points[u]!, b = points[v]!;
          if (!proveWalkableLine(terrain, a, b, { obstacles, obstacleMargin: 0 }).ok || !terrainSafe(a,b,margin)) continue;
          costs[v] = cost; previous[v] = u;
        }
      }
      const path: Point[] = [];
      for (let i = 1; i >= 0; i = previous[i]!) path.unshift({ ...points[i]! });
      const length = costs[1]!;
      if (length > nav.speed * motion.duration + 1e-6) throw Error(`No safe route within speed/duration for ${shot.id}`);
      routes.set(shot.id, { points: path, length, start: shot.start, duration: motion.duration });
      proofs.push({ shotId: shot.id, targetId: actor.id, start: shot.start, end: shot.end, points: path, length, blockers: obstacles });
    }
    positions.set(actor.id, { ...motion.end });
  }
  return {
    proof: { version: 1, policy: 'sequential-time-reservations-visibility-v1', checks, shots: structuredClone(proofs) },
    sample(shotId: string, seconds: number) {
      const route = routes.get(shotId);
      if (!route || !Number.isFinite(seconds)) throw Error('Unknown avoid shot or invalid sample time');
      let remaining = Math.max(0, Math.min(1, (seconds - route.start) / route.duration)) * route.length;
      for (let i = 1; i < route.points.length; i++) {
        const a = route.points[i - 1]!, b = route.points[i]!, len = distance(a, b);
        if (remaining <= len || i === route.points.length - 1) {
          const t = len ? Math.min(1, remaining / len) : 0;
          return { position: { x: a.x + (b.x - a.x) * t, z: a.z + (b.z - a.z) * t }, yaw: Math.atan2(b.x - a.x, b.z - a.z) };
        }
        remaining -= len;
      }
      throw Error('Empty navigation route');
    },
  };
}
