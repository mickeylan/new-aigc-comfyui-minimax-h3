// Derived from Toonflow-app; Copyright (c) 2026 HBAI-Ltd; MIT. See ../../THIRD_PARTY_NOTICES.md.
import { InstancedMesh, Line, Mesh, Texture } from "three";
import type { BufferGeometry, Material, Object3D } from "three";

const geometryUsers = new WeakMap<BufferGeometry, number>();
const sharedMeshes = new WeakSet<Mesh>();
const retainedGeometry = new WeakMap<Mesh, BufferGeometry>();
const geometryPool = new Map<string, BufferGeometry[]>();
const poolKeys = new WeakMap<BufferGeometry, string>();

function sameGeometry(a: BufferGeometry, b: BufferGeometry): boolean {
  if (JSON.stringify([a.groups, a.drawRange, Object.keys(a.morphAttributes)]) !== JSON.stringify([b.groups, b.drawRange, Object.keys(b.morphAttributes)])) return false;
  if (Object.keys(a.morphAttributes).length || Object.keys(b.morphAttributes).length) return false;
  const attributes = (g: BufferGeometry) => [g.index, ...Object.keys(g.attributes).sort().map(key => g.attributes[key])];
  if (Object.keys(a.attributes).sort().join('|') !== Object.keys(b.attributes).sort().join('|')) return false;
  const aa = attributes(a), bb = attributes(b);
  return aa.length === bb.length && aa.every((attr, i) => {
    const other = bb[i]; if (!attr || !other) return attr === other;
    if ('isInterleavedBufferAttribute' in attr || 'isInterleavedBufferAttribute' in other) return false;
    if (attr.itemSize !== other.itemSize || attr.normalized !== other.normalized || attr.array.constructor !== other.array.constructor || attr.array.length !== other.array.length) return false;
    return attr.array.every((value, index) => value === other.array[index]);
  });
}

/** Only immutable generated geometry is pooled. Materials, bones and object transforms remain independent.
 * Exact attribute comparison guards against recipe/name collisions; final release evicts the pool entry.
 */
export function shareObjectGeometry(object: Object3D, sourceKey: string) {
  let index = 0;
  object.traverse(child => {
    if (!(child instanceof Mesh) || (child as any).isSkinnedMesh || child instanceof InstancedMesh || sharedMeshes.has(child)) return;
    const key = `${sourceKey}:${index++}`;
    const candidates = geometryPool.get(key) ?? [];
    const existing = candidates.find(geometry => sameGeometry(geometry, child.geometry));
    if (existing) { const previous = child.geometry; child.geometry = existing; if (previous !== existing) previous.dispose(); }
    else { candidates.push(child.geometry); geometryPool.set(key, candidates); poolKeys.set(child.geometry, key); }
    retainMeshGeometry(child);
  });
}

/** Releases the owned geometry even if the caller replaced mesh.geometry with a private edit. */
export function releaseMeshGeometry(mesh: Mesh): BufferGeometry[] {
  const owned = retainedGeometry.get(mesh);
  const disposable: BufferGeometry[] = [];
  if (sharedMeshes.delete(mesh) && owned) {
    retainedGeometry.delete(mesh);
    const remaining = (geometryUsers.get(owned) ?? 1) - 1;
    if (remaining) geometryUsers.set(owned, remaining);
    else {
      geometryUsers.delete(owned); disposable.push(owned);
      const key = poolKeys.get(owned);
      if (key) { const rest = (geometryPool.get(key) ?? []).filter(item => item !== owned); if (rest.length) geometryPool.set(key, rest); else geometryPool.delete(key); poolKeys.delete(owned); }
    }
  }
  if (mesh.geometry !== owned && !geometryUsers.has(mesh.geometry)) disposable.push(mesh.geometry);
  return disposable;
}

export function retainMeshGeometry(mesh: Mesh) {
  if (sharedMeshes.has(mesh)) return;
  sharedMeshes.add(mesh);
  retainedGeometry.set(mesh, mesh.geometry);
  geometryUsers.set(mesh.geometry, (geometryUsers.get(mesh.geometry) ?? 0) + 1);
}

export function disposeObject(object: Object3D) {
  const geometries = new Set<BufferGeometry>();
  const materials = new Set<Material>();
  const textures = new Set<Texture>();
  object.traverse(child => {
    if (!(child instanceof Mesh || child instanceof Line)) return;
    if (child instanceof InstancedMesh) child.dispose();
    if (child instanceof Mesh) releaseMeshGeometry(child).forEach(geometry => geometries.add(geometry));
    else if (!geometryUsers.has(child.geometry)) geometries.add(child.geometry);
    for (const material of Array.isArray(child.material) ? child.material : [child.material]) {
      materials.add(material);
      for (const value of Object.values(material)) if (value instanceof Texture) textures.add(value);
    }
    if (child instanceof Mesh && "skeleton" in child) {
      const skeleton = child.skeleton as { dispose?: () => void } | undefined;
      skeleton?.dispose?.();
    }
  });
  geometries.forEach(geometry => geometry.dispose());
  materials.forEach(material => material.dispose());
  const bitmaps = new Set<ImageBitmap>();
  textures.forEach(texture => {
    if (typeof ImageBitmap !== "undefined" && texture.source.data instanceof ImageBitmap) bitmaps.add(texture.source.data);
    texture.dispose();
  });
  bitmaps.forEach(bitmap => bitmap.close());
}
