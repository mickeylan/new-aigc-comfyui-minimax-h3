import { z } from 'zod';
import { Box3, Mesh, MeshStandardMaterial, type Object3D } from 'three';
import { measureCollisionBounds } from './toonflow/sceneCollision';

// An explicitly authored review, NOT a novel analyser. Exact quotation proves only
// source membership, not entailment or completeness. Numbers/axes are staging choices.
const text = z.string().trim().min(1).max(8000);
const id = z.string().trim().min(1).max(160);
const basis = z.discriminatedUnion('kind', [
  z.object({ kind: z.literal('fact'), start: z.number().int().nonnegative(), end: z.number().int().positive(), quote: z.string().min(1).max(8000).refine(s => !!s.trim()) }).strict(),
  z.object({ kind: z.literal('assumption'), reason: text }).strict(),
]);
const common = { id, subject: id, reference: id, stagingNote: text, basis };
const schema = z.object({
  version: z.literal(1), sourceRevision: text,
  sourceContent: z.string().min(1).max(1024 * 1024).refine(s => !!s.trim()),
  entities: z.array(z.object({ id, label: text, count: z.number().int().min(1).max(4096), objectIds: z.array(id).min(1).max(4096), basis }).strict()).min(1).max(128),
  relations: z.array(z.discriminatedUnion('kind', [
    z.object({ ...common, kind: z.literal('near'), maxMetres: z.number().finite().min(0).max(1000) }).strict(),
    z.object({ ...common, kind: z.literal('opposite-sides'), other: id, axis: z.enum(['x', 'z']) }).strict(),
  ])).max(256),
  unresolved: z.array(text).max(128),
}).strict();
export type SceneRequirements = z.infer<typeof schema>;
export type RequirementEntry = { id: string; object: Object3D; splat?: unknown };

export function validateSceneRequirements(value: unknown): SceneRequirements {
  const review = schema.parse(value);
  const ids = new Set<string>(), objects = new Set<string>(), relationIds = new Set<string>();
  for (const entity of review.entities) {
    if (ids.has(entity.id)) throw Error('建景要求的物件ID重复');
    ids.add(entity.id);
    if (entity.objectIds.length !== entity.count) throw Error(`建景要求数量与绑定不一致：${entity.label}`);
    for (const objectId of entity.objectIds) {
      if (objects.has(objectId)) throw Error('建景要求不能重复或跨物件共用对象ID');
      objects.add(objectId);
      if (objects.size > 4096) throw Error('建景要求映射超过4096对象');
    }
  }
  let pairs = 0;
  for (const relation of review.relations) {
    if (relationIds.has(relation.id)) throw Error('建景空间关系ID重复');
    relationIds.add(relation.id);
    const referenced = [relation.subject, relation.reference, ...(relation.kind === 'opposite-sides' ? [relation.other] : [])];
    if (new Set(referenced).size !== referenced.length || referenced.some(id => !ids.has(id))) throw Error('建景空间关系引用缺失或自指');
    if (relation.kind === 'near') pairs += review.entities.find(e => e.id === relation.subject)!.count * review.entities.find(e => e.id === relation.reference)!.count;
    if (pairs > 65536) throw Error('建景距离比较超过65536对预算，请拆分要求');
  }
  for (const item of [...review.entities, ...review.relations]) if (item.basis.kind === 'fact') {
    const { start, end, quote } = item.basis;
    // Offsets are UTF-16 [start,end), exactly as JS selectionStart/slice. No trim,
    // whitespace normalization, fuzzy match or substitution from director fields.
    if (end <= start || end > review.sourceContent.length || review.sourceContent.slice(start, end) !== quote) throw Error(`建景原文证据定位不匹配：${item.id}`);
  }
  return review;
}

/** Fail closed on unsupported deformations rather than issuing a false geometry proof.
 * Bounds include all rendered descendants/instances, not merely the object's root.
 */
function visibleBounds(entry: RequirementEntry): Box3 {
  if (entry.splat) throw Error(`建景要求暂不支持泼溅几何：${entry.id}`);
  for (let node: Object3D | null = entry.object; node; node = node.parent) if (!node.visible) throw Error(`建景要求对象不可见：${entry.id}`);
  function check(node: Object3D) {
    if (!node.visible) return;
    if (node instanceof Mesh) {
      const materials = Array.isArray(node.material) ? node.material : [node.material];
      if (materials.some(m => m.visible && m.opacity > 0)) {
        if (materials.some(m => !(m instanceof MeshStandardMaterial) || m.displacementMap || Object.hasOwn(m, 'onBeforeCompile')) || Object.hasOwn(node, 'onBeforeRender')) throw Error(`建景要求不支持自定义着色/位移：${entry.id}`);
        if (materials.some(m => m.clippingPlanes?.length || m.alphaTest > 0 || m.alphaHash || (m as MeshStandardMaterial).alphaMap || (m.transparent && (m as MeshStandardMaterial).map))) throw Error(`建景要求不支持材质裁切/透明贴图：${entry.id}`);
        if (node.morphTargetInfluences?.some(v => v !== 0) || (node as any).morphTexture || (node as any).isSkinnedMesh || (node as any).isBatchedMesh) throw Error(`建景要求仅支持静态布景：${entry.id}`);
        const p = node.geometry.getAttribute('position');
        if (!p) throw Error(`建景要求对象缺少顶点：${entry.id}`);
        for (let i = 0; i < p.count; i++) if (![p.getX(i), p.getY(i), p.getZ(i)].every(Number.isFinite)) throw Error(`建景要求顶点不是有限数：${entry.id}`);
      }
    }
    node.children.forEach(check);
  }
  check(entry.object);
  const box = measureCollisionBounds(entry.object, new Box3());
  if (box.isEmpty() || ![...box.min.toArray(), ...box.max.toArray()].every(Number.isFinite)) throw Error(`建景要求对象没有有效可见几何：${entry.id}`);
  return box;
}

/** Declared coverage only, not full novel fidelity, water/obstacle clearance or physics.
 * No transform mutation; safe before transactional application and before capture.
 */
export function assertSceneRequirements(value: unknown, entries: readonly RequirementEntry[]) {
  const review = validateSceneRequirements(value);
  if (review.unresolved.length) throw Error(`建景要求仍未解决：${review.unresolved.join('；')}`);
  const byId = new Map(entries.map(e => [e.id, e]));
  if (byId.size !== entries.length) throw Error('建景要求场景对象ID重复');
  const objectBounds = new Map<string, Box3>();
  const bounds = new Map(review.entities.map(entity => {
    const box = new Box3();
    for (const id of entity.objectIds) {
      const entry = byId.get(id);
      if (!entry) throw Error(`建景要求缺少对象：${entity.label}（${id}）`);
      const measured = visibleBounds(entry); objectBounds.set(id, measured); box.union(measured);
    }
    return [entity.id, box] as const;
  }));
  for (const relation of review.relations) {
    const a = bounds.get(relation.subject)!, b = bounds.get(relation.reference)!;
    if (relation.kind === 'near') {
      // All mapped objects must satisfy proximity; an aggregate AABB could span a
      // hole and falsely report zero separation. Never use that for "near".
      const subject = review.entities.find(e => e.id === relation.subject)!;
      const reference = review.entities.find(e => e.id === relation.reference)!;
      for (const sid of subject.objectIds) {
        const s = objectBounds.get(sid)!;
        const gap = Math.min(...reference.objectIds.map(rid => {
          const r = objectBounds.get(rid)!;
          return Math.hypot(...(['x', 'y', 'z'] as const).map(axis => Math.max(0, s.min[axis] - r.max[axis], r.min[axis] - s.max[axis])));
        }));
        if (gap > relation.maxMetres) throw Error(`建景距离超出人工约束：${relation.id}（${gap.toFixed(3)}米）`);
      }
    } else {
      const c = bounds.get(relation.other)!, axis = relation.axis;
      if (!((a.max[axis] < b.min[axis] && c.min[axis] > b.max[axis]) || (c.max[axis] < b.min[axis] && a.min[axis] > b.max[axis]))) throw Error(`建景对象未处于参照物两侧：${relation.id}`);
    }
  }
  return { policy: 'declared-scene-requirements-v1' as const, coverage: 'declared-only' as const,
    entities: review.entities.length, relations: review.relations.length,
    assumptions: [...review.entities, ...review.relations].filter(e => e.basis.kind === 'assumption').length };
}
