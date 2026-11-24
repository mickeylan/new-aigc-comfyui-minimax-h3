import { it, expect, vi } from 'vitest';
import { SceneOperations, SceneSaveGuard, buildSceneFingerprint, canMoveEntry, materialRevision, sameSceneState } from './sceneSafety';
import { MeshBasicMaterial, Texture, DoubleSide } from 'three';
it('render recompilation is not an edit, while actual material changes are revisions', () => {
  const m = new MeshBasicMaterial({ map: new Texture() }), initial = materialRevision(m);
  m.needsUpdate = true; m.needsUpdate = true;
  expect(m.version).toBe(2); expect(materialRevision(m)).toEqual(initial);
  for (const edit of [() => m.color.set('#ff0000'), () => { m.opacity = .4; }, () => { m.side = DoubleSide; }, () => { m.alphaTest = .2; }, () => { m.transparent = true; }, () => { m.map = new Texture(); }]) {
    const before = materialRevision(m); edit(); expect(materialRevision(m)).not.toEqual(before);
  }
  m.dispose(); m.map?.dispose();
});
it('finishing an older or nested job never clears another busy operation', () => { const emit=vi.fn(), jobs=new SceneOperations(emit), a=jobs.start('a'),b=jobs.start('b');a.finish();expect(emit).toHaveBeenLastCalledWith('b');a.update('stale');expect(emit).toHaveBeenLastCalledWith('b');b.update('loading');expect(emit).toHaveBeenLastCalledWith('loading');b.finish();expect(emit).toHaveBeenLastCalledWith(null);b.finish();expect(emit).toHaveBeenLastCalledWith(null); });
it('only an unchanged successfully saved revision clears dirty state',()=>{const g=new SceneSaveGuard();g.markSaved('empty');expect(g.isDirty('edited')).toBe(true);expect(()=>g.assertUnchanged('before','after')).toThrow('重新保存');expect(g.isDirty('edited')).toBe(true);g.assertUnchanged('edited','edited');g.markSaved('edited');expect(g.isDirty('edited')).toBe(false);expect(g.isDirty('changed during upload')).toBe(true);});
it('hidden or locked entries cannot move in any preview or export',()=>{for(const locked of [false,true]) for(const visible of [false,true]) expect(canMoveEntry({locked,object:{visible}})).toBe(!locked&&visible);});
it('500ms poll compares structure and media values without holding a copy of the payload',()=>{
  const guard = new SceneSaveGuard();
  const payload = 'data:image/png;base64,' + 'A'.repeat(1_000_000);
  const media: (string | null | undefined)[] = [payload];
  guard.markSaved('full-before', { structure: 'v1', media });
  expect(guard.isDirtyState({ structure: 'v1', media: [payload] })).toBe(false);
  // Same length and prefix, different tail: structure cannot see it, the media comparison must.
  expect(guard.isDirtyState({ structure: 'v1', media: ['data:image/png;base64,' + 'B'.repeat(1_000_000)] })).toBe(true);
  expect(guard.isDirtyState({ structure: 'v2', media: [payload] })).toBe(true);
  // Caller-owned array grows after the save: the stored baseline must not grow with it.
  media.push(payload);
  expect(guard.isDirtyState({ structure: 'v1', media: [payload, payload] })).toBe(true);
  expect(guard.isDirty('full-before')).toBe(false);
  // A legacy one-argument markSaved still gates the full-string comparison.
  guard.markSaved('full-after');
  expect(guard.isDirty('full-after')).toBe(false);
  expect(guard.isDirty('edited')).toBe(true);
});

const mediaURL = (payload: string) => `data:image/png;base64,${payload}`;

it('500 ms 轮询不重新序列化内嵌媒体，只有完整性闸门才拼正文', () => {
  const payload = 'A'.repeat(2_000_000);
  const source = mediaURL(payload);
  const rows = [{ id: 'plane', source, p: [0, 0, 0] }, { id: 'mesh', source: undefined, p: [1, 0, 0] }];
  const spy = vi.spyOn(JSON, 'stringify');
  const fingerprint = buildSceneFingerprint({ epoch: 1, meta: '{}' }, rows);
  const afterStructure = spy.mock.calls.length;
  const structureLength = fingerprint.structure.length;
  expect(afterStructure).toBe(1);
  expect(structureLength).toBeLessThan(400);
  expect(fingerprint.structure).toContain(String(source.length));
  expect(fingerprint.media[0]).toBe(source);
  expect(fingerprint.media[1]).toBeUndefined();
  // 轮询路径到此为止：没有再发生第二次序列化。
  expect(spy.mock.calls.length).toBe(afterStructure);

  const full = fingerprint.full();
  expect(spy.mock.calls.length).toBe(afterStructure + 1);
  expect(full.length).toBeGreaterThan(source.length);
  expect(full).toContain(source);
  // 同一快照内重复读取不再重新拼接。
  expect(fingerprint.full()).toBe(full);
  expect(spy.mock.calls.length).toBe(afterStructure + 1);
  spy.mockRestore();
});

it('媒体按值、其余状态按结构比较：同长度换内容也算修改', () => {
  const a = mediaURL('AAA');
  const snap = (over: Partial<{ source: string; p: number[]; epoch: number }> = {}) =>
    buildSceneFingerprint({ epoch: over.epoch ?? 1 }, [{ id: 'plane', source: over.source ?? a, p: over.p ?? [0, 0, 0] }]);

  const saved = snap();
  expect(sameSceneState(saved, snap())).toBe(true);
  // 内容相同的新字符串：语义上未修改。
  expect(sameSceneState(saved, snap({ source: mediaURL('AAA') }))).toBe(true);
  // 长度、前缀都相同，只有最后一个字符不同——结构层看不出，媒体比较必须拦住。
  expect(sameSceneState(saved, snap({ source: mediaURL('AAB') }))).toBe(false);
  expect(sameSceneState(saved, snap({ p: [1, 0, 0] }))).toBe(false);
  expect(sameSceneState(saved, snap({ epoch: 2 }))).toBe(false);
  expect(sameSceneState(saved, buildSceneFingerprint({ epoch: 1 }, []))).toBe(false);
  expect(sameSceneState(null, saved)).toBe(false);
});

it('完整指纹不修改传入的行快照', () => {
  const rows = [{ id: 'plane', source: mediaURL('AAA') }, { id: 'mesh' }];
  const before = JSON.stringify(rows);
  const fingerprint = buildSceneFingerprint({ epoch: 1 }, rows);
  fingerprint.full();
  expect(JSON.stringify(rows)).toBe(before);
  expect(rows[0].source).toBe(mediaURL('AAA'));
});
