import type { SceneTerrain } from './toonflow/sceneDocument';
import { TERRAIN_LIMITS } from './ground-path';
import { terrainSurfaceOptions } from './toonflow/terrainSurfaces';
import { flushTerrainPaint, paintTerrainSurface, validateTerrainPaint } from './toonflow/terrainPaint';
const TERRAIN_SURFACE_MAX_ID = Math.max(...terrainSurfaceOptions.map(surface => surface.id));

export interface TerrainBrush {
  mode: 'raise' | 'lower' | 'flatten' | 'smooth' | 'surface' | 'river' | 'drain' | 'blend' | 'surfaceColor' | 'waterColor' | 'surfacePaint';
  flow?: number;
  hardness?: number;
  /** Missing surfaceColor resets the global palette entry; waterColor requires an explicit color. */
  color?: string;
  /** River is a capsule between the explicit start and end; absent end creates a lake. */
  endX?: number;
  endZ?: number;
  depth?: number;
  x: number;
  z: number;
  radius: number;
  strength: number;
  height: number;
  surface: number;
}

/** One explicit, immutable stroke. Smoothing reads the original grid, never a half-edited neighbour. */
export function editEnvironmentTerrain(terrain: SceneTerrain, brush: TerrainBrush): SceneTerrain {
  if (!['raise', 'lower', 'flatten', 'smooth', 'surface', 'river', 'drain', 'blend', 'surfaceColor', 'waterColor', 'surfacePaint'].includes(brush.mode)) throw Error('未知地形笔刷');
  if (brush.color !== undefined && !/^#[0-9a-f]{6}$/i.test(brush.color)) throw Error('颜色须为六位十六进制颜色');
  if (brush.mode === 'waterColor' && brush.color === undefined) throw Error('请指定水面颜色');
  if (![brush.x, brush.z, brush.radius, brush.strength, brush.height, brush.surface].every(Number.isFinite)) throw Error('笔刷参数必须是有限数');
  if (brush.radius < terrain.size / terrain.segments / 2 || brush.radius > terrain.size) throw Error('笔刷半径须在半个网格至地形边长之间');
  if (Math.abs(brush.x) > terrain.size / 2 || Math.abs(brush.z) > terrain.size / 2) throw Error('笔刷中心在地形外');
  if (brush.strength <= 0 || brush.strength > 1) throw Error('笔刷强度须大于 0 且不超过 1');
  if (brush.height < TERRAIN_LIMITS.minHeight || brush.height > TERRAIN_LIMITS.maxHeight) throw Error('笔刷高度超出地形范围');
  if (!Number.isInteger(brush.surface) || brush.surface < 0 || brush.surface > TERRAIN_SURFACE_MAX_ID) throw Error('地表编号无效');
  const endX = brush.endX ?? brush.x, endZ = brush.endZ ?? brush.z, depth = brush.depth ?? 1;
  if (brush.mode === 'river' && (![endX, endZ, depth].every(Number.isFinite) || Math.abs(endX) > terrain.size / 2 || Math.abs(endZ) > terrain.size / 2 || depth <= 0 || depth > 40 || brush.height - depth < TERRAIN_LIMITS.minHeight)) throw Error('河道终点、水位或深度无效（深度须大于0且不超过40米，河床不得低于地形下限）');
  const next: SceneTerrain = { ...terrain, heights: [...terrain.heights], surfaces: [...terrain.surfaces], ...(terrain.waterLevels ? { waterLevels: [...terrain.waterLevels] } : {}),
    ...(terrain.surfaceBlend ? { surfaceBlend: { surfaces: [...terrain.surfaceBlend.surfaces], weights: [...terrain.surfaceBlend.weights] } } : {}),
    ...(terrain.surfaceColors ? { surfaceColors: { ...terrain.surfaceColors } } : {}), ...(terrain.waterColors ? { waterColors: { ...terrain.waterColors } } : {}) };
  if (brush.mode === 'surfacePaint') {
    const flow = brush.flow ?? 1, hardness = brush.hardness ?? 0.35;
    if (![flow, hardness].every(value => Number.isFinite(value) && value >= 0 && value <= 1) || flow === 0 || brush.surface === 38) throw Error('绘图流量、硬度或材质无效（原始地面38不可绘制）');
    if (terrain.surfacePaint) {
      const error = validateTerrainPaint(terrain.surfacePaint);
      if (error) throw Error(error);
      next.surfacePaint = { ...terrain.surfacePaint, styles: terrain.surfacePaint.styles?.map(style => ({ ...style })) };
    }
    if (!paintTerrainSurface(next, brush, brush.surface, brush.radius, brush.strength, 1 / 8, { color: brush.color, flow, hardness })) throw Error('笔刷没有改变绘图');
    flushTerrainPaint(next);
    return next;
  }
  if (brush.mode === 'surfaceColor') {
    if (brush.surface === 38) throw Error('原始地面38不支持覆盖调色板');
    next.surfaceColors ??= {};
    const color = brush.color?.toLowerCase();
    if (next.surfaceColors[brush.surface] === color) throw Error('地表色没有改变');
    if (color === undefined) delete next.surfaceColors[brush.surface]; else next.surfaceColors[brush.surface] = color;
    return next;
  }
  if (brush.mode === 'river') next.waterLevels ??= terrain.heights.map(() => null);
  if (brush.mode === 'blend') next.surfaceBlend ??= { surfaces: [...terrain.surfaces], weights: terrain.heights.map(() => 0) };
  const width = terrain.segments + 1, cell = terrain.size / terrain.segments;
  let changed = false;
  const clearBlend = (index: number) => {
    if (!next.surfaceBlend) return;
    changed ||= next.surfaceBlend.weights[index] !== 0 || next.surfaceBlend.surfaces[index] !== next.surfaces[index];
    next.surfaceBlend.surfaces[index] = next.surfaces[index]; next.surfaceBlend.weights[index] = 0;
  };
  for (let row = 0; row < width; row++) for (let col = 0; col < width; col++) {
    const x = col * cell - terrain.size / 2, z = row * cell - terrain.size / 2;
    const dx = endX - brush.x, dz = endZ - brush.z, length2 = dx * dx + dz * dz;
    const t = brush.mode === 'river' && length2 > 0 ? Math.max(0, Math.min(1, ((x - brush.x) * dx + (z - brush.z) * dz) / length2)) : 0;
    const distance = Math.hypot(x - brush.x - t * dx, z - brush.z - t * dz);
    if (distance >= brush.radius) continue;
    const index = row * width + col, weight = (1 - distance / brush.radius) * brush.strength;
    if (brush.mode === 'blend') {
      changed ||= next.surfaceBlend!.surfaces[index] !== brush.surface || next.surfaceBlend!.weights[index] !== weight;
      next.surfaceBlend!.surfaces[index] = brush.surface; next.surfaceBlend!.weights[index] = weight;
      continue;
    }
    if (brush.mode === 'waterColor') {
      if (typeof next.waterLevels?.[index] !== 'number') continue;
      const color = brush.color!.toLowerCase(), override = color === '#60abbc' ? undefined : color;
      changed ||= next.waterColors?.[index] !== override;
      next.waterColors ??= {};
      if (override === undefined) delete next.waterColors[index]; else next.waterColors[index] = override;
      continue;
    }
    if (brush.mode === 'river') {
      const height = Math.min(terrain.heights[index] + (brush.height - depth - terrain.heights[index]) * weight, brush.height - 0.02);
      changed ||= next.heights[index] !== height || next.surfaces[index] !== 2 || next.waterLevels![index] !== brush.height;
      next.heights[index] = height; next.surfaces[index] = 2; next.waterLevels![index] = brush.height;
      clearBlend(index);
      continue;
    }
    if (brush.mode === 'drain') {
      changed ||= next.waterLevels?.[index] != null || next.surfaces[index] === 2;
      if (next.waterLevels) next.waterLevels[index] = null;
      if (next.surfaces[index] === 2) { next.surfaces[index] = 0; clearBlend(index); }
      continue;
    }
    if (brush.mode === 'surface') {
      changed ||= next.surfaces[index] !== brush.surface;
      next.surfaces[index] = brush.surface; clearBlend(index);
      continue;
    }
    const current = terrain.heights[index];
    let target = current;
    if (brush.mode === 'raise') target += 2;
    if (brush.mode === 'lower') target -= 2;
    if (brush.mode === 'flatten') target = brush.height;
    if (brush.mode === 'smooth') {
      let sum = 0, count = 0;
      for (let r = Math.max(0, row - 1); r <= Math.min(width - 1, row + 1); r++)
        for (let c = Math.max(0, col - 1); c <= Math.min(width - 1, col + 1); c++) { sum += terrain.heights[r * width + c]; count++; }
      target = sum / count;
    }
    const height = Math.max(TERRAIN_LIMITS.minHeight, Math.min(TERRAIN_LIMITS.maxHeight, current + (target - current) * weight));
    changed ||= height !== current;
    next.heights[index] = height;
  }
  // Painting dry ground or raising a riverbed drains that vertex, never leaves an invalid archive.
  next.waterLevels?.forEach((level, index) => {
    if (level !== null && (next.surfaces[index] !== 2 || level <= next.heights[index] + 0.01)) next.waterLevels![index] = null;
  });
  if (next.waterColors) for (const key of Object.keys(next.waterColors)) {
    if (typeof next.waterLevels?.[Number(key)] !== 'number') { delete next.waterColors[key]; changed = true; }
  }
  if (!changed) throw Error('笔刷没有改变任何顶点，请调整中心、半径或参数');
  return next;
}
