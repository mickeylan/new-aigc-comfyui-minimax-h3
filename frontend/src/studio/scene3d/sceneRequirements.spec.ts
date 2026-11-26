import { BoxGeometry, Group, InstancedMesh, Mesh, MeshStandardMaterial, Plane, Texture } from 'three';
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
