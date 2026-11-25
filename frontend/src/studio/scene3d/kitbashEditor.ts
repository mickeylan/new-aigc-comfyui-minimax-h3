import * as THREE from 'three';
import { clonePosedContent, isPoseEditorHelper } from './posing/poseExport';
import { serializeBonePose, applyBonePose, type BonePoseNode } from './posing/bonePose';
export type EditableEntry = { id: string; object: THREE.Object3D; locked?: boolean };
type State<T> = { entry: T; position: number[]; quaternion: number[]; scale: number[]; visible: boolean; locked: boolean; pose: BonePoseNode | null };
/** Small transform snapshots retain resource owners, never duplicate media bytes per edit. */
export class ObjectHistory<T extends EditableEntry> {
  private past: State<T>[][] = []; private future: State<T>[][] = [];
  private retained = new Set<T>();
  constructor(private dispose: (entry: T) => void, private limit = 50) {}
  snapshot(entries: T[]) { return entries.map(entry => ({ entry, position: entry.object.position.toArray(), quaternion: entry.object.quaternion.toArray(), scale: entry.object.scale.toArray(), visible: entry.object.visible, locked: !!entry.locked, pose: serializeBonePose(entry.object) })); }
  checkpoint(entries: T[]) { this.past.push(this.snapshot(entries)); this.future = []; if (this.past.length > this.limit) this.past.shift(); entries.forEach(e => this.retained.add(e)); }
  private restore(states: State<T>[]) { return states.map(s => { const o = s.entry.object; o.position.fromArray(s.position); o.quaternion.fromArray(s.quaternion); o.scale.fromArray(s.scale); o.visible = s.visible; s.entry.locked = s.locked; if (s.pose) applyBonePose(o, s.pose); o.updateMatrixWorld(true); return s.entry; }); }
  undo(entries: T[]) { const state = this.past.pop(); if (!state) return null; entries.forEach(e => this.retained.add(e)); this.future.push(this.snapshot(entries)); return this.restore(state); }
  redo(entries: T[]) { const state = this.future.pop(); if (!state) return null; entries.forEach(e => this.retained.add(e)); this.past.push(this.snapshot(entries)); return this.restore(state); }
  collect(entries: T[]) { const live = new Set([...entries, ...this.past.flat().map(s => s.entry), ...this.future.flat().map(s => s.entry)]); for (const e of this.retained) if (!live.has(e)) { this.dispose(e); this.retained.delete(e); } }
  clear(entries: T[]) { this.past = []; this.future = []; this.collect(entries); this.retained.clear(); }
}
export function duplicateMesh(root: THREE.Object3D) {
  const copy = clonePosedContent(root), textures = new Map<THREE.Texture, THREE.Texture>();
  copy.traverse(o => { const m = o as THREE.Mesh; if (!m.isMesh) return; m.geometry = m.geometry.clone();
    const duplicate = (material: THREE.Material) => { const c = material.clone(); for (const [key, value] of Object.entries(c)) if (value instanceof THREE.Texture) { let t = textures.get(value); if (!t) { t = value.clone(); textures.set(value, t); } (c as any)[key] = t; } return c; };
    m.material = Array.isArray(m.material) ? m.material.map(duplicate) : duplicate(m.material);
  }); copy.position.x += .5; copy.position.z += .5; return copy;
}
/**
 * 拖放落点：先打场景里已有的网格，再打地形（有地形时地面不再是 y=0 平面），
 * 最后才退回无限水平面，避免把物件放到半空或山里。
 */
export function surfaceDrop(
  ray: THREE.Raycaster,
  entries: Array<EditableEntry & { splat?: unknown }>,
  terrain?: THREE.Object3D | null,
) {
  const targets: THREE.Object3D[] = [];
  for (const e of entries) if (e.object.visible && !e.splat) e.object.traverse(o => { if ((o as THREE.Mesh).isMesh && !isPoseEditorHelper(o)) { let p: THREE.Object3D | null = o; let visible = true; while (p) { if (!p.visible || isPoseEditorHelper(p)) visible = false; p = p.parent; } if (visible) targets.push(o); } });
  if (terrain?.visible) targets.push(terrain);
  const hit = ray.intersectObjects(targets, false)[0];
  return hit ? hit.point.clone() : ray.ray.intersectPlane(new THREE.Plane(new THREE.Vector3(0, 1, 0), 0), new THREE.Vector3());
}
