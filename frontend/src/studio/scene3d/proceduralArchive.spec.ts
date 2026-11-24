import { describe, expect, it } from 'vitest';
import { Box3, Mesh } from 'three';
import { prepareSetDraft, materializeSetDraft } from './toonflowSets';
import { createRecipeArchive, restoreRecipeObject, validateRecipeArchive } from './proceduralArchive';
import { disposeObject } from './toonflow/sceneResources';

describe('versioned procedural archives', () => {
  it('deduplicates recipes and roundtrips content, material edits and world transforms without geometry bytes', async () => {
    const draft = await prepareSetDraft('ancientStreet', new AbortController().signal);
    const entries = materializeSetDraft(draft);
    try {
      entries[0].object.position.set(23, 2, -14);
      entries[0].object.scale.set(1.2, 2, 0.8);
      entries[0].object.rotation.y = .7;
      entries[0].object.traverse(node => { if ((node as Mesh).isMesh) { const m: any = (node as Mesh).material; if (!Array.isArray(m)) { m.color.set('#ff4433'); m.roughness = .23; m.opacity = .4; m.transparent = true; } } });
      const { archive, references } = createRecipeArchive(entries);
      expect(references.size).toBe(entries.length);
      expect(archive.definitions.length).toBeLessThan(entries.length);
      const text = JSON.stringify(archive);
      expect(text).not.toContain('"attributes"');
      const loaded = JSON.parse(text); validateRecipeArchive(loaded);
      for (const entry of entries) {
        const restored = restoreRecipeObject(loaded, references.get(entry.id)!);
        try {
          restored.position.copy(entry.object.position); restored.quaternion.copy(entry.object.quaternion); restored.scale.copy(entry.object.scale);
          expect(new Box3().setFromObject(restored).min.distanceTo(new Box3().setFromObject(entry.object).min)).toBeLessThan(1e-6);
          const oldMaterials: string[] = [], newMaterials: string[] = [];
          const collect = (root: any, result: string[]) => root.traverse((node: any) => { if (node.isMesh) { for (const m of Array.isArray(node.material) ? node.material : [node.material]) result.push([m.color.getHex(), m.opacity, m.roughness].join('/')); } });
          collect(entry.object, oldMaterials); collect(restored, newMaterials); expect(newMaterials).toEqual(oldMaterials);
        } finally { disposeObject(restored); }
      }
      console.log(`RECIPE_SIZE instances=${entries.length} definitions=${archive.definitions.length} bytes=${new TextEncoder().encode(text).byteLength}`);
    } finally { entries.forEach(entry => disposeObject(entry.object)); }
  }, 60_000);
  it('falls back to GLB for changed geometry and rejects unknown versions/references', async () => {
    const entries = materializeSetDraft(await prepareSetDraft('bedroom', new AbortController().signal));
    try {
      let changed = false;
      entries[0].object.traverse(node => { if (!changed && (node as Mesh).isMesh) { const mesh = node as Mesh; mesh.geometry = mesh.geometry.clone(); mesh.geometry.translate(1, 0, 0); changed = true; } });
      const { archive, references } = createRecipeArchive(entries);
      expect(references.has(entries[0].id)).toBe(false);
      expect(() => validateRecipeArchive({ ...archive, version: 99 })).toThrow('版本');
      expect(() => restoreRecipeObject(archive, 9999)).toThrow('引用');
      const bad = JSON.parse(JSON.stringify(archive)); bad.definitions[0].version = 99;
      expect(() => validateRecipeArchive(bad)).toThrow('版本');
    } finally { entries.forEach(entry => disposeObject(entry.object)); }
  });
});
