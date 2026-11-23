import { it, expect, vi } from 'vitest';
import { SceneOperations, SceneSaveGuard, canMoveEntry, materialRevision } from './sceneSafety';
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
