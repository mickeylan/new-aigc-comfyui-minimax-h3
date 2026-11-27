import { describe, it, expect, vi } from 'vitest';
import { Group, Scene, Box3, Mesh, BoxGeometry, MeshStandardMaterial } from 'three';
import { webcrypto } from 'node:crypto';
import { captureSceneDesignContext, captureSceneDesignEditKeys, validateSceneDesign, requestSceneDesign, mergeSceneDesignEdits, materializeSceneDesign, commitSceneDesign, searchSceneDesignAssets, type SceneDesignPlan } from './sceneDesign';
import type { StudioExecutionContext } from './studioExecutionContext';
import { ObjectHistory } from './kitbashEditor';
import { disposeObject } from './toonflow/sceneResources';

// Warm the real large catalog during collection, not inside the 5s behavior assertion budget.
await import('./toonflowSets');
await import('./toonflow/assetLibrary');

it('local edit evidence changes after material, pose, geometry and clip edits', () => {
  const root = new Group(), mesh = new Mesh(new BoxGeometry(), new MeshStandardMaterial()); root.add(mesh);
  const entry = { id: 'a', name: '甲', object: root };
  let before = captureSceneDesignEditKeys([entry]).a;
  for (const edit of [() => mesh.material.color.set('red'), () => mesh.position.x++, () => { mesh.geometry.attributes.position.needsUpdate = true; }]) {
    edit(); const after = captureSceneDesignEditKeys([entry]).a; expect(after).not.toBe(before); before = after;
  }
  disposeObject(root);
});

const object = (id = 'a') => ({ id, name: '主角', asset_id: `object:${id}`, position: [0, 0, 0], rotation: [0, 0, 0], scale: [1, 1, 1] });
const context = (): StudioExecutionContext => ({ revision: 'rev', assets: [{ id: 'object:a', name: '主角', clips: [] }, { id: 'primitive:person', name: '胶囊人', clips: [] }], objects: [object()] });
const plan = (): SceneDesignPlan => ({ context_revision: 'rev', summary: '岸边留出通道', questions: [], asset_queries: [], template: null, objects: [], remove_ids: [], facts: ['两人交谈'], assumptions: ['岸边留四米空地'], limitations: ['静态布景'] });
const reply = (value: SceneDesignPlan) => ({ plan: value, persisted: false, applied: false, validation: 'structure_and_catalog_references_only' });

describe('AI建景契约', () => {
  it('only accepts real assets, revision and user quotes', () => {
    expect(validateSceneDesign(plan(), context(), ['两人交谈'])).toEqual(plan());
    for (const change of [
      (p: any) => p.context_revision = 'old', (p: any) => p.facts = ['红衣'],
      (p: any) => p.objects = [{ ...object('new'), asset_id: 'fake' }],
      (p: any) => p.objects = [{ ...object(), name: '替身' }],
      (p: any) => p.objects = [{ ...object('clone'), asset_id: 'object:a' }],
      (p: any) => p.remove_ids = ['missing'], (p: any) => p.remove_ids = ['a', 'a'],
      (p: any) => p.code = 'eval', (p: any) => p.objects = null,
      (p: any) => { p.questions = ['哪里']; p.objects = [object()]; },
      (p: any) => { p.asset_queries = ['树']; p.template = { id: 'forest', position: [0, 0, 0], yaw: 0 }; },
    ]) { const value = plan(); change(value); expect(() => validateSceneDesign(value, context(), ['两人交谈'])).toThrow(); }
  });
  it('captures editable metadata and real primitive assets without huge catalog payload', async () => {
    vi.stubGlobal('crypto', webcrypto);
    const entry = { id: 'a', name: '主角', object: new Group() };
    const snapshot = await captureSceneDesignContext([entry, { id: 'locked', name: '锁定', object: new Group(), locked: true }]);
    expect(snapshot.objects.map(o => o.id)).toEqual(['a']);
    expect(snapshot.assets.some(a => a.id === 'primitive:riggedPerson')).toBe(true);
    expect(snapshot.assets.some(a => a.id.startsWith('catalog:'))).toBe(false);
    expect(snapshot.revision).toMatch(/^[a-f0-9]{64}$/);
    vi.unstubAllGlobals();
  });
  it('search returns only exact real locally reconstructible assets', async () => {
    const found = await searchSceneDesignAssets(['椅子']);
    expect(found.length).toBeGreaterThan(0); expect(found.length).toBeLessThanOrEqual(12);
    expect(new Set(found.map(a => a.id)).size).toBe(found.length);
    expect(await searchSceneDesignAssets(['绝不存在的虚构素材123'])).toEqual([]);
  });
});
describe('用户编辑优先的三方合并', () => {
  it('takes AI untouched fields, preserves manual transform fields, skips user deletions', () => {
    const base = context(), latest = structuredClone(base), p = plan();
    latest.objects[0].position = [7, 0, 0];
    p.objects = [{ ...object(), position: [2, 0, 0], rotation: [0, 1, 0] }];
    const merged = mergeSceneDesignEdits(p, base, latest);
    expect(merged.updates[0].position).toEqual([7, 0, 0]); expect(merged.updates[0].rotation).toEqual([0, 1, 0]);
    expect(merged.preserved).toEqual(['a:position']);
    latest.objects = [];
    expect(mergeSceneDesignEdits(p, base, latest).updates).toEqual([]);
    expect(mergeSceneDesignEdits(p, base, latest).preserved).toEqual(['a']);
  });
  it('AI deletion only removes unchanged editable object', () => {
    const base = context(), latest = structuredClone(base), p = plan(); p.remove_ids = ['a'];
    expect(mergeSceneDesignEdits(p, base, latest).removeIds).toEqual(['a']);
    latest.objects[0].scale = [2, 2, 2];
    expect(mergeSceneDesignEdits(p, base, latest).removeIds).toEqual([]);
    latest.objects = []; expect(mergeSceneDesignEdits(p, base, latest).removeIds).toEqual([]);
  });
  it('does not undo user transforms when AI did not change that field', () => {
    const latest = context(); latest.objects[0].position = [9, 0, 0]; const p = plan(); p.objects = [object()];
    expect(mergeSceneDesignEdits(p, context(), latest).updates).toEqual([]);
  });
});
describe('有限的AI检索设计循环', () => {
  it('passes history and retrieved assets back to AI then returns a detached plan', async () => {
    const search = plan(); search.asset_queries = ['椅子'];
    const post = vi.fn().mockResolvedValueOnce(reply(search)).mockResolvedValueOnce(reply(plan()));
    const result = await requestSceneDesign('两人交谈', [{ role: 'user', content: '客栈里' }], { sceneKey: 'epoch', context: context() }, new AbortController().signal, vi.fn(), post);
    expect(post).toHaveBeenCalledTimes(2); expect(result.snapshot.context.assets.some(a => a.id.startsWith('catalog:'))).toBe(true);
    expect(post.mock.calls[1][0].history[0].content).toBe('客栈里'); expect(result.snapshot.sceneKey).toBe('epoch');
    expect(context().assets.length).toBe(2);
  });
  it('stops repeated queries, never applies malformed or falsely persisted output', async () => {
    const p = plan(); p.asset_queries = ['椅子']; const post = vi.fn().mockResolvedValue(reply(p));
    await expect(requestSceneDesign('两人交谈', [], { sceneKey: '1', context: context() }, new AbortController().signal, vi.fn(), post)).rejects.toThrow('未收敛');
    expect(post).toHaveBeenCalledTimes(2);
    for (const bad of [{ ...reply(plan()), applied: true }, reply({ ...plan(), context_revision: 'wrong' })]) {
      await expect(requestSceneDesign('两人交谈', [], { sceneKey: '1', context: context() }, new AbortController().signal, vi.fn(), vi.fn().mockResolvedValue(bad))).rejects.toThrow();
    }
  });
  it('cancellation before and after provider response never delivers proposal', async () => {
    const controller = new AbortController(); controller.abort(); const post = vi.fn();
    await expect(requestSceneDesign('两人交谈', [], { sceneKey: '1', context: context() }, controller.signal, vi.fn(), post)).rejects.toThrow(); expect(post).not.toHaveBeenCalled();
    const second = new AbortController();
    await expect(requestSceneDesign('两人交谈', [], { sceneKey: '1', context: context() }, second.signal, vi.fn(), async () => { second.abort(); return reply(plan()); })).rejects.toThrow();
  });
});
describe('实际静态几何与原子提交', () => {
  it('builds real rigged/capsule geometry and preserves procedural archive source', async () => {
    const p = plan(); p.objects = [{ ...object('new'), name: '路人', asset_id: 'primitive:person', position: [2, 0, 3] }];
    const entries = await materializeSceneDesign(p, context(), new AbortController().signal);
    expect(entries).toHaveLength(1); expect(entries[0].object.userData.proceduralSource.source.asset.id).toBe('person');
    const bounds = new Box3().setFromObject(entries[0].object);
    expect(bounds.isEmpty()).toBe(false); expect(bounds.min.y).toBeCloseTo(0); expect(entries[0].object.position.x).toBe(2);
    entries.forEach(e => disposeObject(e.object));
  });
  it('automatically materializes a selected full set without user coordinate forms', async () => {
    const p = plan(); p.template = { id: 'ancientStreet', position: [10, 0, 2], yaw: Math.PI / 2 };
    const entries = await materializeSceneDesign(p, context(), new AbortController().signal);
    expect(entries.length).toBeGreaterThan(10); expect(new Set(entries.map(e => e.id)).size).toBe(entries.length);
    expect(entries.every(e => e.id.startsWith('ai-set:') && e.object.userData.proceduralSource)).toBe(true);
    entries.forEach(e => disposeObject(e.object));
  });
  it.each(['inn', 'courtroom'])('uses %s scenery without hidden demonstration cast; explicit people and existing edits survive', async templateId => {
    const { prepareSetDraft, listSetPersonObjects } = await import('./toonflowSets');
    const signal = new AbortController().signal;
    const draft = await prepareSetDraft(templateId, signal), templatePeople = listSetPersonObjects(draft);
    expect(templatePeople.length).toBeGreaterThan(0);
    const personIds = new Set(templatePeople.map(person => person.id));
    const scenery = draft.document.objects.filter(saved => !personIds.has(saved.id) && saved.asset.itemType !== '人物');
    // Separate builds assign fresh instance/brush UUIDs. Compare all authored
    // content, keeping catalog IDs exact and ignoring only generated identities.
    const content = ({ id: _id, ...saved }: any) => ({ ...saved, asset: { ...saved.asset, id: saved.asset.id.replace(/[a-f0-9]{8}-[a-f0-9]{4}-[a-f0-9]{4}-[a-f0-9]{4}-[a-f0-9]{12}$/i, '<instance>') } });
    const p = plan(); p.template = { id: templateId, position: [0, 0, 0], yaw: 0 };
    for (const count of [0, 2]) {
      p.objects = Array.from({ length: count }, (_, i) => ({ ...object(`cast-${i}`), name: ['甲', '乙'][i], asset_id: 'primitive:person', position: [i * 3, 0, 0] }));
      const entries = await materializeSceneDesign(p, context(), signal);
      try {
        const cast = entries.filter(entry => entry.object.userData.proceduralSource.source.asset.itemType === '人物');
        expect(cast.map(entry => [entry.id, entry.name])).toEqual(p.objects.map(value => [value.id, value.name]));
        // Every non-person instance/asset/transform survives, including seats and props.
        expect(entries.slice(0, scenery.length).map(entry => content(entry.object.userData.proceduralSource.source))).toEqual(scenery.map(content));
        expect(entries).toHaveLength(scenery.length + count);
        const existing = { id: 'a', name: '人工人物', object: new Group() }; existing.object.position.set(19, 0, 2);
        const next = commitSceneDesign({ additions: entries, updates: [], removeIds: [], preserved: [] }, [existing], new Scene(), () => {});
        expect(next[0]).toBe(existing); expect(existing.object.position.toArray()).toEqual([19, 0, 2]);
      } finally { entries.forEach(entry => disposeObject(entry.object)); }
    }
    // The separate manual template flow is unchanged and still contains its cast.
    expect(listSetPersonObjects(draft)).toEqual(templatePeople);
    const manual = await prepareSetDraft(templateId, signal);
    expect(listSetPersonObjects(manual)).toHaveLength(templatePeople.length);
  });
  it('commits one checkpoint and undo/redo restores transforms/additions/removals', () => {
    const scene = new Scene(), a = { id: 'a', name: 'a', object: new Group() }, b = { id: 'b', name: 'b', object: new Group() };
    scene.add(a.object, b.object); const history = new ObjectHistory<typeof a>(() => {});
    const addition = { id: 'new', name: 'new', object: new Group() }; const before = [a, b];
    const next = commitSceneDesign({ additions: [addition], updates: [{ ...object(), position: [3, 0, 0] }], removeIds: ['b'], preserved: [] }, before, scene, () => history.checkpoint(before));
    expect(next.map(e => e.id)).toEqual(['a', 'new']); expect(a.object.position.x).toBe(3); expect(b.object.parent).toBeNull();
    expect(history.undo(next)?.map(e => e.id)).toEqual(['a', 'b']); expect(a.object.position.x).toBe(0);
    expect(history.redo(before)?.map(e => e.id)).toEqual(['a', 'new']); expect(a.object.position.x).toBe(3);
  });
  it('rejects conflicts/locks before history and rolls back publication failures', () => {
    const scene = new Scene(), a = { id: 'a', name: 'a', object: new Group(), locked: true }; scene.add(a.object);
    const checkpoint = vi.fn(); const batch = { additions: [], updates: [object()], removeIds: [], preserved: [] };
    expect(() => commitSceneDesign(batch, [a], scene, checkpoint)).toThrow('不可编辑'); expect(checkpoint).not.toHaveBeenCalled();
    a.locked = false;
    expect(() => commitSceneDesign({ ...batch, additions: [{ id: 'a', name: 'collision', object: new Group() }] }, [a], scene, checkpoint)).toThrow('ID冲突');
    const newObject = new Group();
    expect(() => commitSceneDesign({ ...batch, updates: [{ ...object(), position: [5, 0, 0] }], additions: [{ id: 'new', name: 'new', object: newObject }] }, [a], { add: () => { throw Error('publication failed'); } }, checkpoint)).toThrow('publication failed');
    expect(a.object.position.x).toBe(0); expect(a.object.parent).toBe(scene); expect(newObject.parent).toBeNull();
  });
});
