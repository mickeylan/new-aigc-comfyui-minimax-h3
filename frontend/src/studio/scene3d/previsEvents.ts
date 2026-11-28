import * as THREE from 'three';
export type PrevisEvent = { kind: 'attach' | 'fire' | 'smoke' | 'scatter'; objectId: string; start: number; end: number; targetId?: string; bone?: string; offset: [number, number, number]; rotation: [number, number, number]; scale: number; velocity: [number, number, number] };
type Entry = { id: string; object: THREE.Object3D; locked?: boolean; splat?: unknown };
const sessions = new WeakMap<THREE.Object3D, PrevisEvent[]>();
export const sessionEvents = (scene: THREE.Object3D | null) => structuredClone(scene ? sessions.get(scene) ?? [] : []);
export function setSessionEvents(scene: THREE.Object3D, events: PrevisEvent[]) {
  if (!Array.isArray(events) || events.length > 32) throw Error('会话事件上限32');
  for (const e of events) {
    if (!['attach', 'fire', 'smoke', 'scatter'].includes(e.kind) || !e.objectId || !Number.isFinite(e.start) || !Number.isFinite(e.end) || e.start < 0 || e.end <= e.start || e.end > 600 || !Number.isFinite(e.scale) || e.scale <= 0 || e.scale > 10 || [e.offset, e.rotation, e.velocity].some(v => !Array.isArray(v) || v.length !== 3 || v.some(n => !Number.isFinite(n) || Math.abs(n) > 100))) throw Error('事件时间或局部变换无效');
    if (e.kind === 'attach' && (!e.targetId || e.targetId === e.objectId || !e.bone)) throw Error('附着需要不同目标和真实骨骼名');
  }
  for (let i = 0; i < events.length; i++) for (let j = 0; j < i; j++) {
    const a = events[i], b = events[j];
    if (['attach', 'scatter'].includes(a.kind) && ['attach', 'scatter'].includes(b.kind) && a.objectId === b.objectId && Math.max(a.start, b.start) < Math.min(a.end, b.end)) throw Error('同一道具的附着/飞散区间重叠');
  }
  if (events.some(e => e.kind === 'attach' && events.some(other => other.kind === 'scatter' && other.objectId === e.targetId && Math.max(e.start, other.start) < Math.min(e.end, other.end)))) throw Error('附着目标为飞散部件，暂不支持该组合');
  attachSamplingOrder(events);
  sessions.set(scene, structuredClone(events));
}
/** Attach chain: an attach may target an object that is itself attached at the same
 * time. Depth order (then external index) keeps positions stable regardless of list
 * order; overlapping cycles are rejected. Non-overlapping attaches are independent.
 * Scatter is ballistic, never a parent. */
export function attachSamplingOrder(events: readonly PrevisEvent[]): number[] {
  const attached = events.map((event, index) => ({ event, index })).filter(item => item.event.kind === 'attach');
  const dependencies = new Map<number, number[]>(attached.map(item => [item.index, attached
    .filter(other => other.index !== item.index && other.event.objectId === item.event.targetId && Math.max(item.event.start, other.event.start) < Math.min(item.event.end, other.event.end))
    .map(other => other.index)]));
  const depth = new Map<number, number>();
  const resolve = (index: number, stack: ReadonlySet<number>): number => {
    const known = depth.get(index);
    if (known !== undefined) return known;
    if (stack.has(index)) throw Error('附着目标形成循环依赖');
    const next = new Set(stack).add(index);
    let value = 0;
    for (const dependency of dependencies.get(index) ?? []) value = Math.max(value, resolve(dependency, next) + 1);
    depth.set(index, value);
    return value;
  };
  return attached.map(item => item.index).sort((a, b) => resolve(a, new Set()) - resolve(b, new Set()) || a - b);
}
export function createPrevisEventRuntime(entries: readonly Entry[], duration: number, actorIds: ReadonlySet<string>, activate?: (entry: Entry) => void) {
  const scene = entries[0]?.object.parent, events = sessionEvents(scene ?? null);
  const snapshots = new Map<THREE.Object3D, { position: THREE.Vector3; quaternion: THREE.Quaternion; scale: THREE.Vector3; parent: THREE.Object3D | null }>();
  const bindings = events.map(event => {
    if (event.end > duration) throw Error('事件超出全片区间');
    const entry = entries.find(e => e.id === event.objectId);
    if (!entry || entry.locked || !entry.object.visible || entry.splat || entry.object.parent !== scene) throw Error('事件对象不可编辑或层级不支持');
    if (['attach', 'scatter'].includes(event.kind) && actorIds.has(entry.id)) throw Error('执行演员不能同时作为道具或飞散部件');
    if (['attach', 'scatter'].includes(event.kind) && !snapshots.has(entry.object)) snapshots.set(entry.object, { position: entry.object.position.clone(), quaternion: entry.object.quaternion.clone(), scale: entry.object.scale.clone(), parent: entry.object.parent });
    let bone: THREE.Bone | undefined, handoff: PrevisEvent | undefined;
    if (event.kind === 'attach') {
      const target = entries.find(e => e.id === event.targetId);
      if (!target || target.locked || !target.object.visible || target.object.parent !== scene || target.splat) throw Error('附着目标不可用');
      const matches: THREE.Bone[] = []; target.object.traverse(o => { if ((o as THREE.Bone).isBone && o.name === event.bone) matches.push(o as THREE.Bone); });
      if (matches.length !== 1) throw Error('附着骨骼缺失或重名'); bone = matches[0];
    }
    if (event.kind === 'scatter') {
      // Handoff is inferred from an exact attach-end/scatter-start boundary, never a new field.
      const sources = events.filter(other => other.kind === 'attach' && other.objectId === event.objectId && Math.abs(other.end - event.start) <= 1e-9);
      if (sources.length > 1) throw Error('飞散接力的附着来源不唯一');
      handoff = sources[0];
      if (handoff) {
        const target = entries.find(e => e.id === handoff!.targetId);
        if (!target || target.locked || !target.object.visible || target.object.parent !== scene || target.splat) throw Error('飞散接力的附着目标不可用');
        const matches: THREE.Bone[] = []; target.object.traverse(o => { if ((o as THREE.Bone).isBone && o.name === handoff!.bone) matches.push(o as THREE.Bone); });
        if (matches.length !== 1) throw Error('飞散接力骨骼缺失或重名');
      }
    }
    return { event, entry, bone, handoff };
  });
  // Same-object effects depend on this sample's root transform, not list order.
  // Preserve the external list (sidecar/provenance) and each phase's relative order.
  // Attach chains additionally run parent-first: a child attach reads the world
  // matrix its parent attach produced earlier in this same sample.
  const samplingBindings = [
    ...attachSamplingOrder(events).map(index => bindings[index]),
    ...bindings.filter(({ event }) => event.kind === 'scatter'),
    ...bindings.filter(({ event }) => event.kind === 'fire' || event.kind === 'smoke'),
  ];
  const particles: THREE.Points[] = [], touched = new Set<THREE.Object3D>();
  let stopped = false;
  const restore = () => {
    particles.splice(0).forEach(p => { p.removeFromParent(); p.geometry.dispose(); (p.material as THREE.Material).dispose(); });
    for (const [object, state] of snapshots) if (touched.has(object) && object.parent === state.parent) { object.position.copy(state.position); object.quaternion.copy(state.quaternion); object.scale.copy(state.scale); object.updateMatrixWorld(true); }
  };
  return { events, restore, stop() { if (stopped) return; restore(); touched.clear(); stopped = true; },
    sample(seconds: number) {
      if (stopped || !Number.isFinite(seconds) || seconds < 0 || seconds > duration) throw Error('事件采样时间无效或会话已停止');
      restore();
      for (const { event: e, entry, bone, handoff } of samplingBindings) {
        if (seconds < e.start || seconds >= e.end) continue;
        if (entry.locked || !entry.object.visible || entry.object.parent !== scene) throw Error('事件对象已变化');
        const t = seconds - e.start, root = entry.object;
        if (e.kind === 'attach') {
          const target = entries.find(item => item.id === e.targetId);
          const matches: THREE.Object3D[] = []; target?.object.traverse(o => { if (o.name === e.bone && (o as THREE.Bone).isBone) matches.push(o); });
          if (!bone?.parent || !scene || !target || target.locked || !target.object.visible || target.object.parent !== scene || matches.length !== 1 || matches[0] !== bone) throw Error('附着骨骼已移除或目标已变化');
          activate?.(entry); touched.add(root); bone.updateWorldMatrix(true, false); scene.updateWorldMatrix(true, false);
          const local = new THREE.Matrix4().compose(new THREE.Vector3(...e.offset), new THREE.Quaternion().setFromEuler(new THREE.Euler(...e.rotation)), new THREE.Vector3(e.scale, e.scale, e.scale));
          const matrix = new THREE.Matrix4().copy(scene.matrixWorld).invert().multiply(bone.matrixWorld).multiply(local);
          matrix.decompose(root.position, root.quaternion, root.scale); root.updateMatrixWorld(true);
        } else if (e.kind === 'scatter') {
          activate?.(entry); touched.add(root); const original = snapshots.get(root)!;
          // Handoff: launch from the CURRENT parent-bone world pose at the release
          // boundary. Velocity stays the authored initial velocity; no momentum is inferred.
          const launch = new THREE.Matrix4().compose(original.position, original.quaternion, new THREE.Vector3(1, 1, 1));
          if (handoff) {
            const target = entries.find(item => item.id === handoff.targetId);
            const matches: THREE.Object3D[] = []; target?.object.traverse(o => { if (o.name === handoff.bone && (o as THREE.Bone).isBone) matches.push(o); });
            if (!scene || !target || target.locked || !target.object.visible || target.object.parent !== scene || matches.length !== 1) throw Error('飞散接力的附着骨骼已移除或目标已变化');
            const sourceBone = matches[0] as THREE.Bone; sourceBone.updateWorldMatrix(true, false); scene.updateWorldMatrix(true, false);
            launch.copy(scene.matrixWorld).invert().multiply(sourceBone.matrixWorld);
          }
          const position = new THREE.Vector3(), quaternion = new THREE.Quaternion(), scale = new THREE.Vector3();
          launch.decompose(position, quaternion, scale);
          root.position.copy(position).add(new THREE.Vector3(...e.velocity).multiplyScalar(t)); root.position.y -= 4.905 * t * t;
          root.quaternion.copy(quaternion).multiply(new THREE.Quaternion().setFromEuler(new THREE.Euler(...e.rotation.map(v => v * t) as [number, number, number]))); root.updateMatrixWorld(true);
        } else {
          const vertices = new Float32Array(32 * 3);
          for (let i = 0; i < 32; i++) { const phase = (t * .7 + i / 32) % 1, angle = i * 2.399963; vertices[i * 3] = Math.cos(angle) * phase * e.scale * .25; vertices[i * 3 + 1] = phase * e.scale; vertices[i * 3 + 2] = Math.sin(angle) * phase * e.scale * .25; }
          const geometry = new THREE.BufferGeometry(); geometry.setAttribute('position', new THREE.BufferAttribute(vertices, 3));
          const points = new THREE.Points(geometry, new THREE.PointsMaterial({ color: e.kind === 'fire' ? '#ff7b22' : '#777777', size: .08 * e.scale, transparent: true, opacity: e.kind === 'fire' ? .8 : .35, depthWrite: false }));
          points.position.copy(root.getWorldPosition(new THREE.Vector3())).add(new THREE.Vector3(...e.offset));
          if (scene) { points.position.applyMatrix4(scene.matrixWorld.clone().invert()); scene.add(points); particles.push(points); }
        }
      }
    },
  };
}
