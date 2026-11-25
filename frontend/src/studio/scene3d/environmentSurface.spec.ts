import { describe, expect, it } from 'vitest';
import { createEnvironmentTerrain, defaultSceneEnvironment, validateSceneEnvironment } from './ground-path';
import * as THREE from 'three';
import { editEnvironmentTerrain, type TerrainBrush } from './environmentEdit';
import { createEnvironmentTerrainMesh, disposeEnvironmentTerrainMesh } from './environmentTerrain';
import { terrainSurfaceOptions } from './toonflow/terrainSurfaces';

const flat = () => createEnvironmentTerrain({ size: 40, segments: 64, amplitude: 0 });
const checked = (terrain: unknown) => validateSceneEnvironment({ ...defaultSceneEnvironment(), terrain }).terrain!;

describe('地表混合与颜色契约', () => {
  it('可选字段严格往返、深复制，旧档不补猜测字段', () => {
    const terrain = flat(), count = terrain.heights.length;
    const source = { ...terrain, surfaceBlend: { surfaces: Array(count).fill(15), weights: Array(count).fill(0.35) }, surfaceColors: { '15': '#Ab12Cd' }, waterLevels: Array(count).fill(null), waterColors: { '0': '#0011FF' } };
    source.surfaces[0] = 2; source.heights[0] = -1; source.waterLevels[0] = 0;
    const copy = checked(JSON.parse(JSON.stringify(source)));
    expect(copy).toEqual(source); expect(checked(terrain)).toEqual(terrain);
    copy.surfaceBlend!.weights[0] = 1; copy.surfaceBlend!.surfaces[0] = 4;
    copy.surfaceColors!['15'] = '#ffffff'; copy.waterColors!['0'] = '#ffffff';
    expect(source.surfaceBlend.weights[0]).toBe(0.35); expect(source.surfaceBlend.surfaces[0]).toBe(15);
    expect(source.surfaceColors['15']).toBe('#Ab12Cd'); expect(source.waterColors['0']).toBe('#0011FF');
  });
  it('非法长度/权重/ID/颜色/稀疏键/干地水色与未支持现代层原子拒绝', () => {
    const terrain = flat(), count = terrain.heights.length;
    const blend = { surfaces: Array(count).fill(0), weights: Array(count).fill(0.5) };
    for (const patch of [
      { surfaceBlend: { ...blend, weights: [0] } }, { surfaceBlend: { ...blend, extra: 1 } },
      { surfaceBlend: { ...blend, weights: Array(count).fill(NaN) } },
      { surfaceBlend: { ...blend, weights: Array(count).fill(1.01) } },
      { surfaceBlend: { ...blend, surfaces: Array(count).fill(59) } },
      { surfaceColors: { '38': '#ffffff' } }, { surfaceColors: { '015': '#ffffff' } },
      { surfaceColors: { '15': '#abc' } }, { surfaceColors: { '59': '#ffffff' } },
      { waterColors: { '0': '#ffffff' } }, { waterColors: { [count]: '#ffffff' } },
      { surfacePaint: { segments: 64, data: '' } },
    ]) expect(() => checked({ ...terrain, ...patch })).toThrow();
    expect(terrain).toEqual(flat());
    console.log('TERRAIN_COLOR_CONTRACT=PASS legacy=compatible invalid=atomic');
  });
  const brush: TerrainBrush = { mode: 'blend', x: 0, z: 0, radius: 5, strength: 0.5, height: 0, surface: 15 };
  const center = 32 * 65 + 32;
  it('混合笔刷保留基础、径向覆盖且不累加第三层；替换和河道清理旧混合', () => {
    const source = flat(), blend = editEnvironmentTerrain(source, brush);
    expect(blend.surfaces).toEqual(source.surfaces); expect(source.surfaceBlend).toBeUndefined();
    expect(blend.surfaceBlend!.weights[center]).toBe(0.5); expect(blend.surfaceBlend!.weights[center + 1]).toBeCloseTo(0.4375);
    expect(blend.surfaceBlend!.weights[0]).toBe(0);
    const replaced = editEnvironmentTerrain(blend, { ...brush, surface: 5, strength: 0.2 });
    expect(replaced.surfaceBlend!.surfaces[center]).toBe(5); expect(replaced.surfaceBlend!.weights[center]).toBe(0.2);
    expect(blend.surfaceBlend!.surfaces[center]).toBe(15);
    for (const mode of ['surface', 'river'] as const) {
      const next = editEnvironmentTerrain(blend, { ...brush, mode });
      expect(next.surfaceBlend!.weights[center]).toBe(0); expect(next.surfaceBlend!.surfaces[center]).toBe(next.surfaces[center]);
      expect(checked(next)).toEqual(next);
    }
    expect(checked(blend)).toEqual(blend);
  });
  it('调色板是全局覆盖、可重置且不污染撤销基线，原始地面不覆盖', () => {
    const source = flat(), colored = editEnvironmentTerrain(source, { ...brush, mode: 'surfaceColor', color: '#AA0022' });
    expect(colored.surfaceColors).toEqual({ '15': '#aa0022' }); expect(source.surfaceColors).toBeUndefined();
    expect(editEnvironmentTerrain(colored, { ...brush, mode: 'surfaceColor' }).surfaceColors).toEqual({});
    expect(colored.surfaceColors).toEqual({ '15': '#aa0022' });
    for (const patch of [{ surface: 38, color: '#ffffff' }, { color: '#fff' }]) expect(() => editEnvironmentTerrain(source, { ...brush, mode: 'surfaceColor', ...patch })).toThrow();
  });
  it('水色仅修改圆内已有水、默认色移除覆盖；抽水/抬床/替换同步清理', () => {
    const river = editEnvironmentTerrain(flat(), { ...brush, mode: 'river', strength: 1, depth: 2 });
    const colored = editEnvironmentTerrain(river, { ...brush, mode: 'waterColor', color: '#FF0099' });
    expect(colored.waterColors![center]).toBe('#ff0099'); expect(colored.waterColors!['0']).toBeUndefined(); expect(river.waterColors).toBeUndefined();
    const reset = editEnvironmentTerrain(colored, { ...brush, mode: 'waterColor', color: '#60abbc' });
    expect(reset.waterColors).toEqual({}); expect(colored.waterColors![center]).toBe('#ff0099');
    for (const patch of [{ mode: 'drain' }, { mode: 'flatten', height: 3 }, { mode: 'surface', surface: 15 }] as const) {
      const next = editEnvironmentTerrain(colored, { ...brush, strength: 1, ...patch });
      expect(next.waterColors![center]).toBeUndefined(); expect(next.waterLevels![center]).toBeNull(); expect(checked(next)).toEqual(next);
    }
    expect(() => editEnvironmentTerrain(flat(), { ...brush, mode: 'waterColor', color: '#ffffff' })).toThrow('没有改变');
    expect(checked(colored)).toEqual(colored);
  });
  it('混合材质编码精确权重与线性调色板、物理通道；水面顶点色不二次染色且释放纹理', () => {
    let terrain = editEnvironmentTerrain(flat(), { ...brush, mode: 'river', strength: 1, depth: 2 });
    terrain = editEnvironmentTerrain(terrain, brush);
    terrain = editEnvironmentTerrain(terrain, { ...brush, mode: 'surfaceColor', color: '#ff0000' });
    terrain = editEnvironmentTerrain(terrain, { ...brush, mode: 'waterColor', color: '#00ff00' });
    const mesh = createEnvironmentTerrainMesh(terrain), material = (mesh.material as THREE.MeshStandardMaterial[])[0];
    expect(mesh.geometry.groups).toHaveLength(1); expect(material.name).toBe('terrain-two-layer-v1');
    const shader: any = { uniforms: {}, vertexShader: THREE.ShaderLib.standard.vertexShader, fragmentShader: THREE.ShaderLib.standard.fragmentShader };
    material.onBeforeCompile(shader, {} as any);
    const field = shader.uniforms.terrainField.value, palette = shader.uniforms.terrainPalette.value;
    expect(Array.from(field.image.data.slice(center * 4, center * 4 + 4))).toEqual([2, 15, 128, 255]);
    expect(Array.from(palette.image.data.slice(15 * 4, 15 * 4 + 3))).toEqual([1, 0, 0]);
    expect(palette.image.data[15 * 4 + 3]).toBeCloseTo(terrainSurfaceOptions[15].roughness);
    expect(shader.fragmentShader).toContain('roughnessFactor *= terrainRough'); expect(shader.fragmentShader).toContain('metalnessFactor *= terrainMetal'); expect(shader.fragmentShader).toContain('totalEmissiveRadiance += terrainGlow');
    const water = mesh.getObjectByName('__TerrainWater__') as THREE.Mesh;
    expect(water.geometry.getAttribute('color').getY(center)).toBe(1);
    expect((water.material as THREE.MeshStandardMaterial).color.getHexString()).toBe('ffffff');
    let disposed = 0; [field, palette].forEach(t => t.addEventListener('dispose', () => disposed++));
    disposeEnvironmentTerrainMesh(mesh); expect(disposed).toBe(2);
    console.log('TERRAIN_BLEND_EDIT_RENDER=PASS blend=immutable palette=global water=local disposed=2');
  });
});
