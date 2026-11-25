import { describe, expect, it, vi } from 'vitest';
import * as THREE from 'three';
import { gzipSync } from 'three/addons/libs/fflate.module.js';
import { createEnvironmentTerrain, defaultSceneEnvironment, validateSceneEnvironment } from './ground-path';
import { editEnvironmentTerrain, type TerrainBrush } from './environmentEdit';
import { createEnvironmentTerrainMesh, disposeEnvironmentTerrainMesh } from './environmentTerrain';
import { getTerrainPaintSample, validateTerrainPaint, getTerrainPaint } from './toonflow/terrainPaint';
import { createTerrainMaterial } from './toonflow/terrainMaterial';
import { terrainSurfaceOptions } from './toonflow/terrainSurfaces';
const flat = () => createEnvironmentTerrain({ size: 40, segments: 64, amplitude: 0 });
const brush: TerrainBrush = { mode: 'surfacePaint', x: 0, z: 0, radius: 5, strength: 0.8, height: 0, surface: 58, color: '#ff0011', flow: 0.7, hardness: 0.4 };
const checked = (terrain: unknown) => validateSceneEnvironment({ ...defaultSceneEnvironment(), terrain }).terrain!;
const encode = (bytes: Uint8Array) => { let text = ''; for (const byte of bytes) text += String.fromCharCode(byte); return btoa(text); };
function shader(material: THREE.MeshStandardMaterial) {
  const result: any = { uniforms: {}, vertexShader: THREE.ShaderLib.standard.vertexShader, fragmentShader: THREE.ShaderLib.standard.fragmentShader };
  material.onBeforeCompile(result, {} as any); return result;
}
describe('modern terrain integration', () => {
  it('flushes durable paint before return, clones records/styles/pixels, preserves legacy layers and undo baselines', () => {
    const legacy = editEnvironmentTerrain(flat(), { ...brush, mode: 'blend', surface: 15 });
    const first = editEnvironmentTerrain(legacy, brush), snapshot = JSON.stringify(first);
    expect(legacy.surfacePaint).toBeUndefined(); expect(first.surfaceBlend).toEqual(legacy.surfaceBlend);
    expect(first.surfacePaint!.data.length).toBeGreaterThan(20);
    expect(validateTerrainPaint(first.surfacePaint!)).toBeUndefined();
    const restored = checked(JSON.parse(snapshot)); expect(restored).toEqual(first);
    const second = editEnvironmentTerrain(first, { ...brush, surface: 39, color: '#00ff00' });
    expect(JSON.stringify(first)).toBe(snapshot);
    expect(second.surfacePaint).not.toBe(first.surfacePaint);
    expect(second.surfacePaint!.styles).not.toBe(first.surfacePaint!.styles);
    expect(second.surfacePaint!.styles![0]).not.toBe(first.surfacePaint!.styles![0]);
    expect(getTerrainPaint(second)!.data).not.toBe(getTerrainPaint(first)!.data);
    expect(getTerrainPaintSample(restored, 0, 0)?.base).toBe(58);
    expect(getTerrainPaintSample(restored, 19, 19)).toBeNull();
    expect(checked(legacy)).toEqual(legacy);
  });
  it('rejects bounds, malformed styles, unknown keys, invalid gzip lengths/CRC/IDs without modifying source', () => {
    const terrain = editEnvironmentTerrain(flat(), brush), snapshot = JSON.stringify(terrain);
    for (const patch of [{ flow: NaN }, { flow: 0 }, { flow: 1.01 }, { hardness: -1 }, { hardness: Infinity }, { surface: 38 }, { surface: 59 }, { color: '#fff' }]) {
      expect(() => editEnvironmentTerrain(terrain, { ...brush, ...patch })).toThrow();
      expect(JSON.stringify(terrain)).toBe(snapshot);
    }
    const paint = terrain.surfacePaint!;
    const bytes = Uint8Array.from(atob(paint.data), c => c.charCodeAt(0)); bytes[bytes.length - 8] ^= 1;
    const invalidId = new Uint8Array(257 * 257 * 4); invalidId[0] = 59;
    const cases = [
      { ...paint, segments: 255 }, { ...paint, data: 'AAAA' }, { ...paint, data: encode(bytes) },
      { ...paint, data: encode(gzipSync(new Uint8Array(4))) },
      { segments: 256, data: encode(gzipSync(invalidId)) },
      { ...paint, styles: [] }, { ...paint, styles: [{ surface: 59, color: '#ffffff' }] },
      { ...paint, styles: [{ surface: 0, color: '#fff' }] },
      { ...paint, styles: [{ surface: 0, color: '#ffffff', extra: 1 }] }, { ...paint, extra: 1 },
    ];
    for (const surfacePaint of cases) expect(() => checked({ ...terrain, surfacePaint })).toThrow();
    expect(JSON.stringify(terrain)).toBe(snapshot);
    // An apparently correct trailer must not allow unbounded inflation.
    const bomb = gzipSync(new Uint8Array(257 * 257 * 4 + 4096));
    new DataView(bomb.buffer, bomb.byteOffset + bomb.length - 4, 4).setUint32(0, 257 * 257 * 4, true);
    expect(validateTerrainPaint({ segments: 256, data: encode(bomb) })).toBeTruthy();
  });
  it('maps all 59 catalog entries to real palette slots and injects upstream detailed/preview GLSL', () => {
    const terrain = editEnvironmentTerrain(flat(), brush), modern = createTerrainMaterial(terrain);
    const full = shader(modern.material), preview = shader(modern.previewMaterial);
    expect(terrainSurfaceOptions.map(s => s.id)).toEqual(Array.from({ length: 59 }, (_, i) => i));
    const palette = full.uniforms.terrainPalette.value;
    expect(palette.image.width).toBe(59); expect(palette.image.height).toBe(2);
    for (const surface of terrainSurfaceOptions) {
      const color = surface.id === 38 ? new THREE.Color('#f5f7fa').lerp(new THREE.Color('#dcdfe6'), 0.12) : new THREE.Color(surface.color);
      for (const [channel, value] of [color.r, color.g, color.b].entries()) expect(palette.image.data[surface.id * 4 + channel]).toBeCloseTo(value);
      expect(palette.image.data[(59 + surface.id) * 4 + 2]).toBeCloseTo(surface.metalness ?? 0);
      expect(palette.image.data[(59 + surface.id) * 4 + 3]).toBeCloseTo(surface.emission ?? 0);
    }
    for (const compiled of [full, preview]) {
      expect(compiled.vertexShader).toContain('terrainLocalPosition');
      expect(compiled.fragmentShader).toContain('TerrainSample terrainSample');
      expect(compiled.fragmentShader).toContain('terrainPaintMap');
      expect(compiled.fragmentShader).toContain('terrainResolve');
      expect(compiled.fragmentShader).not.toContain('undefined');
      expect(compiled.fragmentShader).toContain('metalnessFactor = terrainMetalness');
      expect(compiled.fragmentShader).toContain('totalEmissiveRadiance += terrainEmission');
    }
    expect(full.fragmentShader).toContain('terrainHeightGradient');
    expect(full.fragmentShader.length).toBeGreaterThan(preview.fragmentShader.length);
    modern.dispose();
  });
  it('updates textures, palette and environment; releases replaced resources and all four textures once', () => {
    const first = editEnvironmentTerrain(flat(), brush), modern = createTerrainMaterial(first), compiled = shader(modern.material);
    const oldPaint = compiled.uniforms.terrainPaintMap.value, oldField = compiled.uniforms.terrainSurfaces.value;
    const paintDispose = vi.spyOn(oldPaint, 'dispose'), fieldDispose = vi.spyOn(oldField, 'dispose');
    const resized = editEnvironmentTerrain(createEnvironmentTerrain({ size: 40, segments: 128, amplitude: 0 }), brush);
    modern.update(resized); expect(paintDispose).toHaveBeenCalledOnce(); expect(fieldDispose).toHaveBeenCalledOnce();
    expect(compiled.uniforms.terrainSurfaces.value.image.width).toBe(129);
    modern.setColors({ '15': '#ff0000' }); expect(Array.from(compiled.uniforms.terrainPalette.value.image.data.slice(60, 63))).toEqual([1, 0, 0]);
    modern.setRain(2); expect(compiled.uniforms.terrainRain.value).toBe(1);
    modern.setEnvironment(new THREE.Color('red'), new THREE.Color('blue'), new THREE.Color('white'), new THREE.Vector3(2, 0, 0));
    expect(compiled.uniforms.terrainEnvSunDirection.value.x).toBe(1);
    const textures = ['terrainSurfaces', 'terrainPalette', 'terrainPaintMap', 'terrainStyles'].map(key => vi.spyOn(compiled.uniforms[key].value, 'dispose'));
    const materials = [vi.spyOn(modern.material, 'dispose'), vi.spyOn(modern.previewMaterial, 'dispose')];
    modern.dispose(); modern.dispose(); for (const dispose of [...textures, ...materials]) expect(dispose).toHaveBeenCalledOnce();
  });
  it('only activates full material for paint records, preserving river water and complete mesh disposal', () => {
    const river = editEnvironmentTerrain(flat(), { ...brush, mode: 'river', strength: 1, depth: 2 });
    const painted = editEnvironmentTerrain(river, brush);
    expect(painted.waterLevels).toEqual(river.waterLevels);
    const legacy = createEnvironmentTerrainMesh(river), mesh = createEnvironmentTerrainMesh(painted);
    expect(legacy.userData.terrainMaterial).toBeUndefined();
    const modern = mesh.userData.terrainMaterial; expect(modern).toBeDefined();
    expect(mesh.geometry.groups).toHaveLength(1); expect(mesh.getObjectByName('__TerrainWater__')).toBeDefined();
    const dispose = vi.spyOn(modern, 'dispose'); disposeEnvironmentTerrainMesh(mesh); expect(dispose).toHaveBeenCalledOnce();
    disposeEnvironmentTerrainMesh(legacy);
  });
});
