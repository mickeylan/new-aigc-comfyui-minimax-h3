import { describe, expect, it } from 'vitest';
import * as THREE from 'three';
import { createEnvironmentTerrain, defaultSceneEnvironment, groundHeightAt, proveWalkableLine, validateSceneEnvironment } from './ground-path';
import { editEnvironmentTerrain, type TerrainBrush } from './environmentEdit';
import { createEnvironmentTerrainMesh, disposeEnvironmentTerrainMesh, terrainSurfaceTexture } from './environmentTerrain';

const flat = () => createEnvironmentTerrain({ size: 40, segments: 64, amplitude: 0 });
const river: TerrainBrush = { mode: 'river', x: -10, z: 0, endX: 10, endZ: 0, radius: 3, depth: 2, height: 0, strength: 1, surface: 2 };
const center = 32 * 65 + 32;

describe('完整河道与地表渲染', () => {
  it('显式河道贯穿中点，河床、水位与地表独立且严格往返，干岸不变，水域不可走', () => {
    const source = flat(), next = editEnvironmentTerrain(source, river);
    expect(source.waterLevels).toBeUndefined(); expect(source.heights[center]).toBe(0);
    expect(next.heights[center]).toBe(-2); expect(next.waterLevels![center]).toBe(0);
    expect(next.surfaces[center]).toBe(2); expect(next.heights[0]).toBe(0); expect(next.waterLevels![0]).toBeNull();
    expect(validateSceneEnvironment(JSON.parse(JSON.stringify({ ...defaultSceneEnvironment(), terrain: next }))).terrain).toEqual(next);
    expect(proveWalkableLine(next, { x: -9, z: 0 }, { x: 9, z: 0 }).reason).toContain('不支持涉水');
    expect(proveWalkableLine(next, { x: -9, z: 6 }, { x: 9, z: 6 }).ok).toBe(true);
    for (const patch of [{ endX: 21 }, { endZ: NaN }, { depth: 0 }, { depth: 41 }, { height: -40, depth: 1 }]) expect(() => editEnvironmentTerrain(source, { ...river, ...patch })).toThrow();
  });
  it('抽水、覆盖地表与抬高河床都清理失效水位，不修改旧稿', () => {
    const source = editEnvironmentTerrain(flat(), river);
    for (const patch of [{ mode: 'drain' }, { mode: 'surface', surface: 15 }, { mode: 'flatten', height: 3 }] as const) {
      const edited = editEnvironmentTerrain(source, { ...river, ...patch, x: 0 });
      expect(edited.waterLevels![center]).toBeNull(); expect(source.waterLevels![center]).toBe(0);
      expect(validateSceneEnvironment({ ...defaultSceneEnvironment(), terrain: edited }).terrain).toEqual(edited);
    }
  });
  it('真实河床与独立水面同时物化、地表有贴图及物理材质，采样与三角形一致', () => {
    const terrain = editEnvironmentTerrain(flat(), river), mesh = createEnvironmentTerrainMesh(terrain);
    const water = mesh.getObjectByName('__TerrainWater__') as THREE.Mesh;
    expect(water.geometry.index!.count).toBeGreaterThan(0);
    expect(mesh.geometry.index!.count).toBe(64 * 64 * 6);
    expect(mesh.geometry.getAttribute('position').getY(center)).toBe(-2);
    expect(water.geometry.getAttribute('position').getY(center)).toBe(0);
    expect(groundHeightAt(terrain, 0, 0)).toBe(-2);
    const materials = mesh.material as THREE.MeshStandardMaterial[];
    expect(materials.every(material => !!material.map && material.roughness > 0)).toBe(true);
    expect(mesh.geometry.groups.reduce((sum, group) => sum + group.count, 0)).toBe(64 * 64 * 6);
    let disposed = 0;
    mesh.geometry.addEventListener('dispose', () => disposed++); water.geometry.addEventListener('dispose', () => disposed++);
    materials.forEach(material => material.map!.addEventListener('dispose', () => disposed++));
    disposeEnvironmentTerrainMesh(mesh); expect(disposed).toBe(2 + materials.length);
    console.log(`TERRAIN_WATER_RENDER=PASS triangles=${mesh.geometry.index!.count / 3} water=${water.geometry.index!.count / 3} materials=${materials.length}`);
  });
  it('贴图由版本化算法、地表和种子稳定决定，并区分草地与砖缝', () => {
    const a = terrainSurfaceTexture(0, 7), b = terrainSurfaceTexture(0, 7), c = terrainSurfaceTexture(16, 7), d = terrainSurfaceTexture(0, 8);
    expect(a.image.data).toEqual(b.image.data); expect(a.image.data).not.toEqual(c.image.data); expect(a.image.data).not.toEqual(d.image.data);
    [a, b, c, d].forEach(texture => texture.dispose());
  });
});
