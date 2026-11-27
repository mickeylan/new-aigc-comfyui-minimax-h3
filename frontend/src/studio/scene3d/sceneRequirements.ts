import { z } from 'zod';
import { BackSide, Box3, DoubleSide, InstancedMesh, Matrix4, Mesh, MeshStandardMaterial, Triangle, Vector3, type Object3D } from 'three';
import { measureCollisionBounds } from './toonflow/sceneCollision';
import { GROUND_OBSTACLE_MARGIN, WALK_MAX_SLOPE, groundHeightAt, proveWalkableLine, validateSceneEnvironment, type SceneEnvironment } from './ground-path';

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
    z.object({ ...common, kind: z.literal('path-connected'), other: id,
      points: z.array(z.tuple([z.number().finite().min(-1000).max(1000), z.number().finite().min(-1000).max(1000), z.number().finite().min(-1000).max(1000)])).min(2).max(128),
      maxEndpointGapMetres: z.number().finite().min(0).max(2) }).strict(),
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
  let pairs = 0, segments = 0;
  for (const relation of review.relations) {
    if (relationIds.has(relation.id)) throw Error('建景空间关系ID重复');
    relationIds.add(relation.id);
    const referenced = [relation.subject, relation.reference, ...(relation.kind !== 'near' ? [relation.other] : [])];
    if (new Set(referenced).size !== referenced.length || referenced.some(id => !ids.has(id))) throw Error('建景空间关系引用缺失或自指');
    if (relation.kind === 'near') pairs += review.entities.find(e => e.id === relation.subject)!.count * review.entities.find(e => e.id === relation.reference)!.count;
    if (pairs > 65536) throw Error('建景距离比较超过65536对预算，请拆分要求');
    if (relation.kind === 'path-connected') {
      segments += relation.points.length - 1;
      if (segments > 512) throw Error('建景道路超过512段预算，请拆分要求');
      for (let i = 1; i < relation.points.length; i++) {
        const a = relation.points[i - 1], b = relation.points[i];
        if (Math.hypot(b[0] - a[0], b[2] - a[2]) < 1e-6) throw Error('建景道路不能包含零长度XZ段');
      }
    }
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
function visibleBounds(entry: RequirementEntry, posedObstacle = false): Box3 {
  if (entry.splat) throw Error(`建景要求暂不支持泼溅几何：${entry.id}`);
  for (let node: Object3D | null = entry.object; node; node = node.parent) if (!node.visible) throw Error(`建景要求对象不可见：${entry.id}`);
  function check(node: Object3D) {
    if (!node.visible) return;
    if (node instanceof Mesh) {
      const materials = Array.isArray(node.material) ? node.material : [node.material];
      if (materials.some(m => m.visible && m.opacity > 0)) {
        if (!posedObstacle && materials.some(m => m.visible && m.opacity > 0 && !m.colorWrite)) throw Error(`建景要求对象材质不可见：${entry.id}`);
        if (materials.some(m => !(m instanceof MeshStandardMaterial) || m.displacementMap || Object.hasOwn(m, 'onBeforeCompile')) || Object.hasOwn(node, 'onBeforeRender')) throw Error(`建景要求不支持自定义着色/位移：${entry.id}`);
        if (materials.some(m => m.clippingPlanes?.length || m.alphaTest > 0 || m.alphaHash || (m as MeshStandardMaterial).alphaMap || (m.transparent && (m as MeshStandardMaterial).map))) throw Error(`建景要求不支持材质裁切/透明贴图：${entry.id}`);
        if ((!posedObstacle && (node.morphTargetInfluences?.some(v => v !== 0) || (node as any).isSkinnedMesh)) || (node as any).morphTexture || (node as any).isBatchedMesh) throw Error(`建景要求仅支持静态布景：${entry.id}`);
        const p = node.geometry.getAttribute('position');
        if (!p) throw Error(`建景要求对象缺少顶点：${entry.id}`);
        for (let i = 0; i < p.count; i++) if (!(posedObstacle ? node.getVertexPosition(i, new Vector3()).toArray() : [p.getX(i), p.getY(i), p.getZ(i)]).every(Number.isFinite)) throw Error(`建景要求顶点不是有限数：${entry.id}`);
      }
    }
    node.children.forEach(check);
  }
  check(entry.object);
  const box = measureCollisionBounds(entry.object, new Box3());
  if (box.isEmpty() || ![...box.min.toArray(), ...box.max.toArray()].every(Number.isFinite)) throw Error(`建景要求对象没有有效可见几何：${entry.id}`);
  return box;
}

type PathRelation = Extract<SceneRequirements['relations'][number], { kind: 'path-connected' }>;
type SurfaceTriangle = { triangle: Triangle; upward: boolean };

/** Only rendered, opaque static triangles count. No root boxes or editable navigation
 * metadata as road evidence. Share a per-proof budget across instances and relations. */
function visibleTriangles(entry: RequirementEntry, budget: { triangles: number }): SurfaceTriangle[] {
  // Entity geometry was already validated before any relation proof.
  const result: SurfaceTriangle[] = [];
  entry.object.updateWorldMatrix(true, true);
  const visit = (node: Object3D) => {
    if (!node.visible) return;
    if (node instanceof Mesh) {
      const geometry = node.geometry, position = geometry.getAttribute('position'), index = geometry.index;
      const total = index?.count ?? position.count;
      const materials = Array.isArray(node.material) ? node.material : [node.material];
      const groups = Array.isArray(node.material) ? geometry.groups : [{ start: 0, count: total, materialIndex: 0 }];
      const count = node instanceof InstancedMesh ? node.count : 1;
      if (!Number.isInteger(count) || count < 0 || count > 4096) throw Error('建景道路实例预算无效');
      for (const group of groups) {
        const material = materials[group.materialIndex ?? 0] as MeshStandardMaterial;
        if (!material?.visible || material.opacity <= 0) continue;
        if (!material.colorWrite) throw Error(`建景道路路面或端点不可见：${entry.id}`);
        if (material.opacity !== 1 || material.wireframe) throw Error(`建景道路不支持透明/线框路面或端点：${entry.id}`);
        const start = Math.max(0, group.start, geometry.drawRange.start), end = Math.min(total, group.start + group.count, geometry.drawRange.start + geometry.drawRange.count);
        if (end <= start) continue;
        if (!Number.isInteger(start) || start % 3 || !Number.isInteger(end) || (end - start) % 3) throw Error('建景道路三角绘制范围无效');
        const added = count * (end - start) / 3;
        budget.triangles += added;
        if (budget.triangles > 65536) throw Error('建景道路超过65536三角预算');
        for (let instance = 0; instance < count; instance++) {
          const matrix = new Matrix4();
          if (node instanceof InstancedMesh) node.getMatrixAt(instance, matrix);
          matrix.premultiply(node.matrixWorld);
          if (!matrix.elements.every(Number.isFinite) || matrix.determinant() <= 1e-12) throw Error('建景道路不支持镜像/退化变换');
          for (let i = start; i < end; i += 3) {
            const vertex = (cursor: number) => {
              const id = index?.getX(cursor) ?? cursor;
              if (!Number.isInteger(id) || id < 0 || id >= position.count) throw Error('建景道路三角索引无效');
              const v = new Vector3().fromBufferAttribute(position, id).applyMatrix4(matrix);
              if (!v.toArray().every(Number.isFinite)) throw Error('建景道路三角坐标无效');
              return v;
            };
            const triangle = new Triangle(vertex(i), vertex(i + 1), vertex(i + 2));
            const normal = triangle.getNormal(new Vector3());
            const facing = material.side === DoubleSide || (material.side === BackSide ? normal.y < 0 : normal.y > 0);
            result.push({ triangle, upward: facing && Math.abs(normal.y) > 1e-8 && Math.hypot(normal.x, normal.z) / Math.abs(normal.y) <= WALK_MAX_SLOPE });
          }
        }
      }
    }
    node.children.forEach(visit);
  };
  visit(entry.object);
  if (!result.length) throw Error(`建景道路缺少可见三角：${entry.id}`);
  return result;
}

/** Clip the WHOLE segment to each triangle in XZ and to its surface-height band.
 * Union of intervals proves centreline support, not road width/body clearance.
 * Fixed 1 micrometre seam tolerance is not an authored gap allowance. */
function supportedInterval(triangle: Triangle, start: Vector3, end: Vector3): [number, number] | null {
  const { a, b, c } = triangle;
  const determinant = (b.x - a.x) * (c.z - a.z) - (b.z - a.z) * (c.x - a.x);
  if (Math.abs(determinant) < 1e-12) return null;
  const weights = (p: Vector3) => {
    const u = ((p.x - a.x) * (c.z - a.z) - (p.z - a.z) * (c.x - a.x)) / determinant;
    const v = ((b.x - a.x) * (p.z - a.z) - (b.z - a.z) * (p.x - a.x)) / determinant;
    return [1 - u - v, u, v];
  };
  const s = weights(start), e = weights(end);
  let low = 0, high = 1;
  const clip = (from: number, to: number) => {
    if (from < 0 && to < 0) return false;
    if (from < 0) low = Math.max(low, -from / (to - from));
    if (to < 0) high = Math.min(high, from / (from - to));
    return low <= high;
  };
  for (let i = 0; i < 3; i++) if (!clip(s[i], e[i])) return null;
  const height = (w: number[]) => a.y * w[0] + b.y * w[1] + c.y * w[2];
  const hs = start.y - height(s), he = end.y - height(e);
  if (!clip(.02 - hs, .02 - he) || !clip(.02 + hs, .02 + he)) return null;
  return [low, high];
}

function assertConnectedPath(relation: PathRelation, review: SceneRequirements, entries: readonly RequirementEntry[],
  triangles: (id: string) => SurfaceTriangle[], environment: SceneEnvironment | null, budget: { checks: number }) {
  const entity = (id: string) => review.entities.find(e => e.id === id)!;
  const routeIds = new Set(entity(relation.subject).objectIds);
  const surfaces = [...routeIds].flatMap(triangles).filter(t => t.upward);
  const points = relation.points.map(p => new Vector3(...p));
  for (const [point, id] of [[points[0], relation.reference], [points[points.length - 1], relation.other]] as const) {
    let distance = Infinity;
    for (const objectId of entity(id).objectIds) for (const { triangle } of triangles(objectId)) {
      if (++budget.checks > 1_000_000) throw Error('建景道路超过1000000次比较预算');
      distance = Math.min(distance, triangle.closestPointToPoint(point, new Vector3()).distanceTo(point));
    }
    if (distance > relation.maxEndpointGapMetres + 1e-6) throw Error(`建景道路端点未接近目标可见表面：${relation.id}`);
  }
  if (entries.length > 256) throw Error('建景道路障碍检查超过256对象预算');
  const firstIds = new Set(entity(relation.reference).objectIds), lastIds = new Set(entity(relation.other).objectIds);
  const obstacles = entries.flatMap(entry => {
    if (routeIds.has(entry.id)) return [];
    for (let node: Object3D | null = entry.object; node; node = node.parent) if (!node.visible) return [];
    // Characters are not scenery evidence: reserve their CURRENT posed bounds.
    // Unknown shader displacement/splats still fail closed, even when far away.
    entry.object.updateWorldMatrix(true, true);
    const box = visibleBounds(entry, true);
    return [{ name: entry.id, first: firstIds.has(entry.id), last: lastIds.has(entry.id), min: [box.min.x, box.min.z] as [number, number], max: [box.max.x, box.max.z] as [number, number] }];
  });
  for (let i = 1; i < points.length; i++) {
    const a = points[i - 1], b = points[i], length = Math.hypot(b.x - a.x, b.z - a.z);
    const intervals: [number, number][] = [];
    for (const surface of surfaces) {
      if (++budget.checks > 1_000_000) throw Error('建景道路超过1000000次比较预算');
      const interval = supportedInterval(surface.triangle, a, b);
      if (interval) intervals.push(interval);
    }
    intervals.sort((a, b) => a[0] - b[0]);
    let covered = 0, unsupported = 0;
    for (const [start, end] of intervals) {
      unsupported += Math.max(0, start - covered) * length;
      if (unsupported > 1e-6) break;
      covered = Math.max(covered, end);
    }
    if (!intervals.length || unsupported + (1 - covered) * length > 1e-6) throw Error(`建景道路路面不连续或坡度不支持：${relation.id} 第${i}段`);
    const proof = proveWalkableLine(environment?.terrain, a, b, { obstacles: obstacles.filter(o => !o.first && !o.last) });
    if (!proof.ok) throw Error(`建景道路${relation.id}：${proof.reason}`);
    // Landmark surface contact at the FIRST/LAST point is allowed, not a blanket
    // exclusion: every interior part is still checked against the endpoint boxes.
    // This is road connectivity only; existing actor margins remain independent.
    for (const obstacle of obstacles.filter(o => o.first || o.last)) {
      if (++budget.checks > 1_000_000) throw Error('建景道路超过1000000次比较预算');
      const start = obstacle.first && i === 1 ? a.clone().lerp(b, Math.min(1e-6 / length, .25)) : a;
      const end = obstacle.last && i === points.length - 1 ? b.clone().lerp(a, Math.min(1e-6 / length, .25)) : b;
      const endpointProof = proveWalkableLine(null, start, end, { obstacles: [obstacle], obstacleMargin: 0 });
      if (!endpointProof.ok) throw Error(`建景道路${relation.id}：${endpointProof.reason}`);
    }
    // Explicitly sampled ground anchoring, separate from continuous mesh support.
    const samples = Math.ceil(length / .25);
    if (samples > 4096) throw Error('建景道路离地检查超过4096点预算');
    for (let j = 0; j <= samples; j++) {
      const point = a.clone().lerp(b, j / samples);
      if (Math.abs(point.y - groundHeightAt(environment?.terrain, point.x, point.z)) > .1) throw Error(`建景道路离地或埋地：${relation.id}`);
    }
  }
  return { id: relation.id, segments: points.length - 1, support: 'continuous-triangle-centreline' as const,
    clearanceMetres: GROUND_OBSTACLE_MARGIN, clearanceScope: 'third-party-xz-boxes' as const,
    endpointContact: 'surface-proximity-interior-box-reject' as const,
    terrain: 'sampled-ground-line' as const, maxSurfaceHeightErrorMetres: .02 };
}

/** Declared coverage only, not full novel fidelity or physics. Optional roads prove
 * continuous static centreline support, with separate sampled terrain checks.
 * No transform mutation; safe before transactional application and before capture. */
export function assertSceneRequirements(value: unknown, entries: readonly RequirementEntry[], environment: SceneEnvironment | null = null) {
  const review = validateSceneRequirements(value);
  if (review.relations.some(r => r.kind === 'path-connected')) {
    if (entries.length > 256) throw Error('建景道路障碍检查超过256对象预算');
    // Bound preprocessing BEFORE full vertex/bounds scans, including non-road
    // obstacles and every instance. A tiny drawRange must not bypass this limit.
    let nodes = 0, vertices = 0;
    const preflight = (node: Object3D) => {
      if (!node.visible) return;
      if (++nodes > 8192) throw Error('建景道路超过8192节点预算');
      if (node instanceof Mesh) {
        const count = node instanceof InstancedMesh ? node.count : 1;
        if (!Number.isInteger(count) || count < 0 || count > 4096) throw Error('建景道路实例预算无效');
        const size = node.geometry.getAttribute('position')?.count ?? 0;
        vertices += size * Math.max(1, count);
        if (!Number.isInteger(vertices) || vertices > 262144) throw Error('建景道路超过262144顶点预算');
        if ((node.geometry.index?.count ?? 0) > 393216 || node.geometry.groups.length > 4096) throw Error('建景道路索引/绘制组超过预算');
      }
      node.children.forEach(preflight);
    };
    for (const entry of entries) preflight(entry.object);
  }
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
  const budget = { triangles: 0, checks: 0 }, triangleCache = new Map<string, SurfaceTriangle[]>();
  const triangles = (id: string) => {
    if (!triangleCache.has(id)) triangleCache.set(id, visibleTriangles(byId.get(id)!, budget));
    return triangleCache.get(id)!;
  };
  const paths: ReturnType<typeof assertConnectedPath>[] = [];
  const checkedEnvironment = review.relations.some(r => r.kind === 'path-connected') && environment ? validateSceneEnvironment(environment) : null;
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
    } else if (relation.kind === 'path-connected') {
      paths.push(assertConnectedPath(relation, review, entries, triangles, checkedEnvironment, budget));
    } else {
      const c = bounds.get(relation.other)!, axis = relation.axis;
      if (!((a.max[axis] < b.min[axis] && c.min[axis] > b.max[axis]) || (c.max[axis] < b.min[axis] && a.min[axis] > b.max[axis]))) throw Error(`建景对象未处于参照物两侧：${relation.id}`);
    }
  }
  return { policy: 'declared-scene-requirements-v1' as const, coverage: 'declared-only' as const,
    entities: review.entities.length, relations: review.relations.length, ...(paths.length ? { paths } : {}),
    assumptions: [...review.entities, ...review.relations].filter(e => e.basis.kind === 'assumption').length };
}
