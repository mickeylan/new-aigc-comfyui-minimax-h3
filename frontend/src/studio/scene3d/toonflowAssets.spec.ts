import { describe, expect, it } from 'vitest';
import { Box3, Vector3 } from 'three';
import { createProceduralAsset, proceduralAssetCounts, proceduralAssetVariants, searchProceduralAssets } from './toonflowAssets';
import { disposeObject } from './toonflow/sceneResources';

describe('Toonflow procedural catalog migration', () => {
  it('counts real main assets separately from derivatives', () => {
    expect(proceduralAssetCounts()).toEqual({ main: 11750, derivatives: 69560, total: 81310 });
  });
  it('searches upstream Chinese aliases and paginates without truncating the total', () => {
    expect(searchProceduralAssets('古建').total).toBe(40);
    const a = searchProceduralAssets('', 0, 24), b = searchProceduralAssets('', 1, 24);
    expect(a.total).toBe(11750);
    expect(a.rows).toHaveLength(24);
    expect(a.rows.some(row => b.rows.some(other => other.id === row.id))).toBe(false);
    expect(() => searchProceduralAssets('', -1)).toThrow();
  });
  it('creates editable real geometry with reproducible bounds and independent resources', () => {
    const id = searchProceduralAssets('古建').rows[0].id;
    const a = createProceduralAsset(id), b = createProceduralAsset(id);
    try {
      const boundsA = new Box3().setFromObject(a), boundsB = new Box3().setFromObject(b);
      expect(boundsA.equals(boundsB)).toBe(true);
      expect(boundsA.getSize(new Vector3()).length()).toBeGreaterThan(0);
      expect(a.children.length).toBeGreaterThan(1);
      expect(a.children[0]).not.toBe(b.children[0]);
      expect(a.userData.proceduralSource).toMatchObject({ generator: 'toonflow', version: 1, assetId: id });
    } finally { disposeObject(a); disposeObject(b); }
  });
  it('constructs upstream recipe derivatives and rejects missing IDs', () => {
    const main = searchProceduralAssets('', 0, 1).rows[0];
    const variants = proceduralAssetVariants(main.id);
    expect(variants).toHaveLength(main.derivatives);
    const object = createProceduralAsset(variants[0].id, main.id);
    try { expect(object.children.length).toBeGreaterThan(0); } finally { disposeObject(object); }
    expect(() => createProceduralAsset('missing')).toThrow('找不到');
    expect(() => createProceduralAsset(variants[0].id)).toThrow('找不到');
  });
});
