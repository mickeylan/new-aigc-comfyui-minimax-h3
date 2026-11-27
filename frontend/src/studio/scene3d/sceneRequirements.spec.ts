import { Bone, BoxGeometry, Float32BufferAttribute, Group, InstancedMesh, Matrix4, Mesh, MeshStandardMaterial, Plane, Skeleton, SkinnedMesh, Texture, Uint16BufferAttribute } from 'three';
import { createEnvironmentTerrain, defaultSceneEnvironment } from './ground-path';
import { expect, it } from 'vitest';
import { assertSceneRequirements, validateSceneRequirements } from './sceneRequirements';
import { validateScriptShotPlan } from './scriptShotPlan';
import { assertScriptShotApplicationSafe, createScriptShotSampler } from './scriptShotSampler';
import { PerspectiveCamera, Vector3 } from 'three';

const content = '军营外的小路通向水潭。黑石在水潭近岸，瀑布在水潭对岸。';
const evidence = (quote: string) => ({ kind: 'fact' as const, start: content.indexOf(quote), end: content.indexOf(quote) + quote.length, quote });
function fixture() {
  const entry = (id: string, x: number, z: number, w = 1, d = 1) => {
    const object = new Group(); object.add(new Mesh(new BoxGeometry(w, 1, d), new MeshStandardMaterial())); object.position.set(x, 0, z);
    return { id, name: id, object };
  };
  const entries = [entry('rock', 0, 6), entry('pool', 0, 0, 8, 8), entry('falls', 0, -6)];
  const review = { version: 1 as const, sourceRevision: 'reviewed-revision', sourceContent: content, entities: [
    { id: 'stone', label: '黑石', count: 1, objectIds: ['rock'], basis: evidence('黑石') },
    { id: 'water', label: '水潭', count: 1, objectIds: ['pool'], basis: evidence('水潭') },
    { id: 'waterfall', label: '瀑布', count: 1, objectIds: ['falls'], basis: evidence('瀑布') },
  ], relations: [
    { id: 'near-bank', kind: 'near' as const, subject: 'stone', reference: 'water', maxMetres: 2, stagingNote: '两米为人工布景距离，不是小说数字', basis: evidence('黑石在水潭近岸') },
    { id: 'across', kind: 'opposite-sides' as const, subject: 'stone', reference: 'water', other: 'waterfall', axis: 'z' as const, stagingNote: 'Z轴为人工布景朝向', basis: evidence('瀑布在水潭对岸') },
  ], unresolved: [] as string[] };
  return { entries, review };
}
it('checks declared requirements against actual visible geometry, not roots, without mutating scene or review', () => {
  const f = fixture(), before = JSON.stringify(f.review), positions = f.entries.map(e => e.object.position.toArray());
  const r = assertSceneRequirements(f.review, f.entries);
  expect(r).toMatchObject({ policy: 'declared-scene-requirements-v1', entities: 3, relations: 2, coverage: 'declared-only' });
  expect(JSON.stringify(f.review)).toBe(before); expect(f.entries.map(e => e.object.position.toArray())).toEqual(positions);
  expect(validateSceneRequirements(JSON.parse(before))).toEqual(f.review);
});
it('requires exact raw source spans and never accepts director-added text as novel evidence', () => {
  for (const change of [
    (r: any) => r.entities[0].basis.quote = '导演描述黑石',
    (r: any) => r.entities[0].basis.start++,
    (r: any) => r.entities[0].basis.end--,
    (r: any) => r.entities[0].basis.quote = ' ',
    (r: any) => r.sourceContent = '另一版小说',
  ]) { const f = fixture(); change(f.review); expect(() => validateSceneRequirements(f.review)).toThrow(); }
});
it('keeps explicit staging assumptions distinct from facts and requires a reason', () => {
  const f = fixture(); (f.review.entities[0] as any).basis = { kind: 'assumption', reason: '原文未写石头尺寸，人工选现有素材' };
  expect(assertSceneRequirements(f.review, f.entries).assumptions).toBe(1);
  (f.review.entities[0] as any).basis.reason = ' '; expect(() => validateSceneRequirements(f.review)).toThrow();
});
it('rejects unknown fields, duplicate or shared object identities, invalid counts and unbound relations', () => {
  for (const change of [
    (r: any) => r.execute_code = 'bad',
    (r: any) => r.entities[0].basis.kind = 'inferred-fact',
    (r: any) => r.entities[0].count = 2,
    (r: any) => r.entities[0].objectIds = ['rock', 'rock'],
    (r: any) => r.entities[1].objectIds = ['rock'],
    (r: any) => r.entities[1].id = 'stone',
    (r: any) => r.relations[1].other = 'absent',
    (r: any) => r.relations[0].reference = 'stone',
    (r: any) => r.relations[0].maxMetres = NaN,
    (r: any) => r.relations[0].stagingNote = '',
    (r: any) => r.relations[1].axis = 'screen-left',
  ]) { const f = fixture(); change(f.review); expect(() => validateSceneRequirements(f.review)).toThrow(); }
});
it('fails on missing/hidden/empty/splat geometry instead of declaring presence by object name', () => {
  for (const change of [
    (f: any) => f.entries.pop(),
    (f: any) => f.entries[0].object.visible = false,
    (f: any) => f.entries[0].object.children[0].visible = false,
    (f: any) => f.entries[0].object.clear(),
    (f: any) => f.entries[0].splat = {},
    (f: any) => f.entries.push(f.entries[0]),
  ]) { const f = fixture(); change(f); expect(() => assertSceneRequirements(f.review, f.entries)).toThrow(); }
});
it('rejects same-bank or overlapping geometry even when root signs suggest opposite banks', () => {
  const f = fixture(); f.entries[2].object.position.z = 6; expect(() => assertSceneRequirements(f.review, f.entries)).toThrow('两侧');
  f.entries[2].object.position.z = -6; f.entries[2].object.children[0].scale.z = 20;
  expect(() => assertSceneRequirements(f.review, f.entries)).toThrow('两侧');
});
it('near uses true world bounds distance and rejects a gap larger than the authored maximum', () => {
  const f = fixture(); f.entries[0].object.position.z = 10; expect(() => assertSceneRequirements(f.review, f.entries)).toThrow('距离');
  f.entries[0].object.position.z = 6; f.entries[0].object.children[0].position.z = 10;
  expect(() => assertSceneRequirements(f.review, f.entries)).toThrow('距离');
});
function shotPlan() {
  return { version: 1, sceneId: 's', heading: '潭边', location: '水潭', sourceFingerprint: 'hash', sceneFingerprint: 'reviewed-revision', action: [], diagnostics: [],
    shots: [{ id: 's1', order: 1, start: 0, end: 1, camera: { position: [0, 2, 9], target: [0, 0, 0], fov: 45 }, motion: null, dialogue: [] }], sceneRequirements: fixture().review };
}
it('persists optional scene requirements in shot plans, rejects stale/invalid reviews but accepts legacy plans', () => {
  const p = shotPlan(); expect(validateScriptShotPlan(JSON.parse(JSON.stringify(p)))).toEqual(p);
  p.sceneRequirements.sourceRevision = 'different'; expect(() => validateScriptShotPlan(p)).toThrow('来源');
  p.sceneRequirements.sourceRevision = 'reviewed-revision'; p.sceneRequirements.entities[0].basis.quote = '假的'; expect(() => validateScriptShotPlan(p)).toThrow('证据');
  const { sceneRequirements, ...legacy } = p; expect(validateScriptShotPlan(legacy)).toEqual(legacy);
});
it('checks staged proposed transforms before application and restores them after rejection', () => {
  const f = fixture(), p = validateScriptShotPlan(shotPlan());
  expect(() => assertScriptShotApplicationSafe(p, f.entries, null)).not.toThrow();
  expect(() => assertScriptShotApplicationSafe(p, f.entries, null, [{ id: 'falls', position: [0, 0, 6], rotation: [0, 0, 0], scale: [1, 1, 1] }])).toThrow('两侧');
  expect(f.entries[2].object.position.z).toBe(-6);
});
it('rejects later editing drift before viewport/reference/video sampler ownership without moving camera', () => {
  const f = fixture(), camera = new PerspectiveCamera(), controls = { target: new Vector3(), enabled: true, update() {} };
  camera.position.set(1, 2, 3); f.entries[2].object.position.z = 6;
  expect(() => createScriptShotSampler(validateScriptShotPlan(shotPlan()), { camera, controls, entries: () => f.entries, environment: null })).toThrow('两侧');
  expect(camera.position.toArray()).toEqual([1, 2, 3]); expect(controls.enabled).toBe(true);
});
it('fails unresolved requirements closed, not partial success', () => {
  const f = fixture(); f.review.unresolved.push('军营到小路的连通性还没验证');
  expect(() => assertSceneRequirements(f.review, f.entries)).toThrow('军营到小路');
});
it('refuses material clipping and alpha cutouts that bounds cannot prove visible', () => {
  for (const change of [
    (m: MeshStandardMaterial) => m.clippingPlanes = [new Plane(new Vector3(1, 0, 0), -100)],
    (m: MeshStandardMaterial) => m.alphaTest = 1,
    (m: MeshStandardMaterial) => m.alphaHash = true,
    (m: MeshStandardMaterial) => m.alphaMap = new Texture(),
    (m: MeshStandardMaterial) => { m.transparent = true; m.map = new Texture(); },
  ]) {
    const f = fixture(); change((f.entries[0].object.children[0] as Mesh).material as MeshStandardMaterial);
    expect(() => assertSceneRequirements(f.review, f.entries)).toThrow('裁切');
  }
});
it('refuses per-instance morph weights rather than measuring the undeformed mesh', () => {
  const f = fixture(), geometry = new BoxGeometry(1, 1, 1);
  const morphed = geometry.attributes.position.clone();
  for (let i = 0; i < morphed.count; i++) morphed.setZ(i, morphed.getZ(i) + 100);
  geometry.morphAttributes.position = [morphed];
  const donor = new Mesh(geometry, new MeshStandardMaterial()); donor.morphTargetInfluences![0] = 1;
  const instances = new InstancedMesh(geometry, donor.material, 1); instances.setMorphAt(0, donor);
  f.entries[0].object.clear().add(instances);
  expect(() => assertSceneRequirements(f.review, f.entries)).toThrow('静态布景');
});
it('rejects unsupported displaced or non-finite geometry rather than issuing a false spatial proof', () => {
  for (const change of [
    (m: Mesh) => (m.material as MeshStandardMaterial).displacementMap = new Texture(),
    (m: Mesh) => m.geometry.attributes.position.setX(0, NaN),
  ]) { const f = fixture(); change(f.entries[0].object.children[0] as Mesh); expect(() => assertSceneRequirements(f.review, f.entries)).toThrow(); }
});

function routeFixture() {
  const mesh = (w: number, h: number, d: number, x: number, y: number) => {
    const m = new Mesh(new BoxGeometry(w, h, d), new MeshStandardMaterial()); m.position.set(x, y, 0); return m;
  };
  const road = new Group(); road.add(mesh(4, .1, 1, 0, -.05));
  const entries = [{ id: 'road', name: '小路', object: road },
    { id: 'camp', name: '军营', object: mesh(1, 1, 1, -3, .5) },
    { id: 'bank', name: '潭边', object: mesh(1, 1, 1, 3, .5) }];
  const review = { version: 1 as const, sourceRevision: 'reviewed-revision', sourceContent: content,
    entities: entries.map(e => ({ id: e.id, label: e.name, count: 1, objectIds: [e.id], basis: evidence(e.id === 'road' ? '小路' : e.id === 'camp' ? '军营' : '水潭') })),
    relations: [{ id: 'camp-to-bank', kind: 'path-connected' as const, subject: 'road', reference: 'camp', other: 'bank',
      points: [[-2, 0, 0], [2, 0, 0]] as [number, number, number][], maxEndpointGapMetres: .5,
      stagingNote: '世界坐标折线和半米端点接近为人工布景，不证明路宽或人物可通行', basis: evidence('军营外的小路通向水潭') }], unresolved: [] };
  return { entries, review, road, mesh };
}
it('proves a declared route on rendered triangles, keeps assumptions/source and does not mutate transforms', () => {
  const f = routeFixture(), before = JSON.stringify(f.review), positions = f.entries.map(e => e.object.position.toArray());
  const proof = assertSceneRequirements(f.review, f.entries, null);
  expect(proof.paths).toMatchObject([{ id: 'camp-to-bank', segments: 1, support: 'continuous-triangle-centreline', clearanceMetres: .35 }]);
  expect(validateSceneRequirements(JSON.parse(before))).toEqual(f.review);
  expect(JSON.stringify(f.review)).toBe(before); expect(f.entries.map(e => e.object.position.toArray())).toEqual(positions);
});
it('rejects sub-sample road gaps inside an aggregate AABB instead of sampling across the hole', () => {
  const f = routeFixture(); f.road.clear();
  // 1mm gap at x=.123; a 0.5m sample grid and the union AABB both miss it.
  f.road.add(f.mesh(2.1225, .1, 1, -.93875, -.05), f.mesh(1.8765, .1, 1, 1.06175, -.05));
  expect(() => assertSceneRequirements(f.review, f.entries, null)).toThrow('路面不连续');
});
it('requires true endpoint surface proximity and never attaches across hollow landmark bounds', () => {
  const f = routeFixture(); f.entries[1].object.position.x = -4;
  expect(() => assertSceneRequirements(f.review, f.entries, null)).toThrow('端点');
  const hollow = new Group(); const a = f.mesh(1, 1, 1, -3, .5), b = a.clone(); a.position.z = -3; b.position.z = 3;
  hollow.add(a, b); f.entries[1].object = hollow as any;
  expect(() => assertSceneRequirements(f.review, f.entries, null)).toThrow('端点');
});
it('binds actual transformed/instanced route geometry and rejects hidden or clipped support', () => {
  const f = routeFixture(); f.road.clear();
  const instances = new InstancedMesh(new BoxGeometry(4, .1, 1), new MeshStandardMaterial(), 1);
  instances.setMatrixAt(0, new Matrix4().makeTranslation(0, -.05, 0)); f.road.add(instances);
  expect(() => assertSceneRequirements(f.review, f.entries, null)).not.toThrow();
  f.road.position.z = 2; expect(() => assertSceneRequirements(f.review, f.entries, null)).toThrow('路面不连续');
  f.road.position.z = 0; instances.geometry.setDrawRange(0, 0);
  expect(() => assertSceneRequirements(f.review, f.entries, null)).toThrow();
});
it('keeps water, slope and third-object clearance gates for declared roads', () => {
  const f = routeFixture(), environment = defaultSceneEnvironment(); environment.terrain = createEnvironmentTerrain({ size: 20, segments: 64, amplitude: 0 });
  expect(() => assertSceneRequirements(f.review, f.entries, environment)).not.toThrow();
  environment.terrain.waterLevels = Array(4225).fill(null); environment.terrain.waterLevels[2112] = .1; environment.terrain.surfaces[2112] = 2;
  expect(() => assertSceneRequirements(f.review, f.entries, environment)).toThrow('水域');
  environment.terrain.waterLevels[2112] = null;
  f.entries.push({ id: 'blocker', name: '挡路石', object: f.mesh(.2, 1, .2, .123, .5) } as any);
  expect(() => assertSceneRequirements(f.review, f.entries, environment)).toThrow('障碍');
  f.entries.pop(); f.road.rotation.z = Math.PI / 3;
  expect(() => assertSceneRequirements(f.review, f.entries, environment)).toThrow('路面不连续');
});
it('rejects ambiguous route references, zero-length segments, malformed coordinates and unbounded work', () => {
  for (const edit of [
    (r: any) => r.relations[0].other = 'absent',
    (r: any) => r.relations[0].other = 'road',
    (r: any) => r.relations[0].points[1] = [-2, 0, 0],
    (r: any) => r.relations[0].points[1][0] = NaN,
    (r: any) => r.relations[0].points[1] = [2, 0],
    (r: any) => r.relations[0].points = Array(130).fill([0, 0, 0]),
    (r: any) => r.relations[0].maxEndpointGapMetres = 999,
  ]) { const f = routeFixture(); edit(f.review); expect(() => validateSceneRequirements(f.review)).toThrow(); }
});
it('passes current terrain into application and shared viewport/reference/video requirements gates', () => {
  const f = routeFixture(), p = validateScriptShotPlan({ ...shotPlan(), sceneRequirements: f.review });
  const environment = defaultSceneEnvironment(); environment.terrain = createEnvironmentTerrain({ size: 20, segments: 64, amplitude: 0 });
  environment.terrain.waterLevels = Array(4225).fill(null); environment.terrain.waterLevels[2112] = .1; environment.terrain.surfaces[2112] = 2;
  expect(() => assertScriptShotApplicationSafe(p, f.entries, environment)).toThrow('水域');
  const camera = new PerspectiveCamera(), controls = { target: new Vector3(), enabled: true, update() {} };
  camera.position.set(1, 2, 3);
  expect(() => createScriptShotSampler(p, { camera, controls, entries: () => f.entries, environment })).toThrow('水域');
  expect(camera.position.toArray()).toEqual([1, 2, 3]); expect(controls.enabled).toBe(true);
});
it('restores staged road transforms on rejection and rechecks geometry drift before sampler ownership', () => {
  const f = routeFixture(), p = validateScriptShotPlan({ ...shotPlan(), sceneRequirements: f.review });
  expect(validateScriptShotPlan(JSON.parse(JSON.stringify(p)))).toEqual(p);
  expect(() => assertScriptShotApplicationSafe(p, f.entries, null, [{ id: 'road', position: [0, 0, 2], rotation: [0, 0, 0], scale: [1, 1, 1] }])).toThrow('路面不连续');
  expect(f.road.position.toArray()).toEqual([0, 0, 0]);
  const camera = new PerspectiveCamera(), controls = { target: new Vector3(), enabled: true, update() {} };
  const sampler = createScriptShotSampler(p, { camera, controls, entries: () => f.entries, environment: null });
  expect(sampler.walkProof.sceneRequirements?.paths).toHaveLength(1); sampler.stop();
  f.road.children[0].scale.x = .9;
  expect(() => createScriptShotSampler(p, { camera, controls, entries: () => f.entries, environment: null })).toThrow('路面不连续');
  expect(controls.enabled).toBe(true);
});
it('refuses colourless declared scenery as well as road evidence', () => {
  const f = fixture(); ((f.entries[0].object.children[0] as Mesh).material as MeshStandardMaterial).colorWrite = false;
  expect(() => assertSceneRequirements(f.review, f.entries)).toThrow('不可见');
});
it('bounds vertex preprocessing before scanning geometry even with a tiny draw range', () => {
  const f = routeFixture(), geometry = (f.road.children[0] as Mesh).geometry;
  geometry.setAttribute('position', new Float32BufferAttribute(new Float32Array(300000 * 3), 3)); geometry.setDrawRange(0, 6);
  expect(() => assertSceneRequirements(f.review, f.entries, null)).toThrow('顶点预算');
});
it('does not accumulate per-seam tolerance into a meaningful unsupported length', () => {
  const f = routeFixture(); f.road.clear();
  const count = 100, cell = 4 / count, gap = .0000005;
  for (let i = 0; i < count; i++) f.road.add(f.mesh(cell - (i === count - 1 ? 0 : gap), .1, 1, -2 + cell * i + (cell - (i === count - 1 ? 0 : gap)) / 2, -.05));
  expect(() => assertSceneRequirements(f.review, f.entries, null)).toThrow('路面不连续');
});
it('refuses colourless road evidence even when it still writes depth', () => {
  const f = routeFixture(); ((f.road.children[0] as Mesh).material as MeshStandardMaterial).colorWrite = false;
  expect(() => assertSceneRequirements(f.review, f.entries, null)).toThrow('不可见');
});
it('allows surface-touching endpoints but still rejects entering or crossing endpoint landmarks', () => {
  const f = routeFixture(); f.road.children[0].scale.x = 1.25;
  f.review.relations[0].points = [[-2.5, 0, 0], [2.5, 0, 0]]; f.review.relations[0].maxEndpointGapMetres = 0;
  expect(() => assertSceneRequirements(f.review, f.entries, null)).not.toThrow();
  f.road.children[0].scale.x = 1.5;
  f.review.relations[0].points = [[-3, 0, 0], [3, 0, 0]];
  f.review.relations[0].maxEndpointGapMetres = .5;
  expect(() => assertSceneRequirements(f.review, f.entries, null)).toThrow('障碍');
});
it('uses the current skin pose as an obstacle without requiring unrelated characters to be static scenery evidence', () => {
  const f = routeFixture(), geometry = new BoxGeometry(.5, 1, .5), count = geometry.attributes.position.count;
  geometry.setAttribute('skinIndex', new Uint16BufferAttribute(new Uint16Array(count * 4), 4));
  const weights = new Float32Array(count * 4); for (let i = 0; i < count; i++) weights[i * 4] = 1;
  geometry.setAttribute('skinWeight', new Float32BufferAttribute(weights, 4));
  const actor = new SkinnedMesh(geometry, new MeshStandardMaterial()), bone = new Bone(); actor.add(bone); actor.bind(new Skeleton([bone])); actor.position.set(0, .5, 100);
  f.entries.push({ id: 'actor', name: '远处角色', object: actor } as any);
  expect(() => assertSceneRequirements(f.review, f.entries, null)).not.toThrow();
  actor.position.z = 0;
  expect(() => assertSceneRequirements(f.review, f.entries, null)).toThrow('障碍');
});
it('rejects floating, hidden, transparent, mirrored and over-budget route geometry', () => {
  for (const edit of [
    (f: ReturnType<typeof routeFixture>) => { f.road.position.y = .5; f.review.relations[0].points.forEach(p => p[1] = .5); },
    (f: ReturnType<typeof routeFixture>) => f.road.children[0].visible = false,
    (f: ReturnType<typeof routeFixture>) => ((f.road.children[0] as Mesh).material as MeshStandardMaterial).opacity = .5,
    (f: ReturnType<typeof routeFixture>) => f.road.scale.x = -1,
    (f: ReturnType<typeof routeFixture>) => { const m = new InstancedMesh(new BoxGeometry(4, .1, 1), new MeshStandardMaterial(), 4097); f.road.clear().add(m); },
  ]) { const f = routeFixture(); edit(f); expect(() => assertSceneRequirements(f.review, f.entries, null)).toThrow(); }
});
