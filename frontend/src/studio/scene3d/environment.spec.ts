import { describe, expect, it } from 'vitest';
import { editEnvironmentTerrain, type TerrainBrush } from './environmentEdit';
import { createEnvironmentEffects } from './environmentEffects';
import {
  WALK_MAX_SLOPE,
  clampGroundPointToTerrain,
  createEnvironmentTerrain,
  defaultSceneEnvironment,
  environmentLight,
  groundHeightAt,
  groundPathPointOnTerrain,
  proveWalkableLine,
  summarizeEnvironment,
  terrainBounds,
  terrainSlopeAt,
  validateSceneEnvironment,
  type SceneEnvironment,
} from './ground-path';

const env = (patch: Partial<SceneEnvironment> = {}): SceneEnvironment => ({
  ...defaultSceneEnvironment(),
  ...patch,
});

describe('人工地形笔刷与天气特效', () => {
  const brush: TerrainBrush = { mode: 'raise', x: 0, z: 0, radius: 3, strength: 1, height: 4, surface: 15 };
  const flat = () => createEnvironmentTerrain({ size: 40, segments: 64, amplitude: 0 });
  it('抬升、降低、整平、涂地表只改变圆内，不污染原稿，结果能严格往返', () => {
    const source = flat(), center = 32 * 65 + 32;
    const raised = editEnvironmentTerrain(source, brush);
    expect(source.heights[center]).toBe(0);
    expect(raised.heights[center]).toBe(2);
    expect(raised.heights[0]).toBe(source.heights[0]);
    expect(editEnvironmentTerrain(source, { ...brush, mode: 'lower' }).heights[center]).toBe(-2);
    expect(editEnvironmentTerrain(source, { ...brush, mode: 'flatten' }).heights[center]).toBe(4);
    const painted = editEnvironmentTerrain(raised, { ...brush, mode: 'surface' });
    expect(painted.surfaces[center]).toBe(15);
    expect(painted.heights).toEqual(raised.heights);
    expect(validateSceneEnvironment(JSON.parse(JSON.stringify(env({ terrain: painted })))).terrain).toEqual(painted);
  });
  it('平滑用修改前邻域，高度钳制且参数错误与无变化明确拒绝', () => {
    const source = flat(), center = 32 * 65 + 32;
    source.heights[center] = 40;
    const smoothed = editEnvironmentTerrain(source, { ...brush, mode: 'smooth' });
    expect(smoothed.heights[center]).toBeCloseTo(40 / 9);
    expect(editEnvironmentTerrain(source, brush).heights[center]).toBe(40);
    for (const patch of [{ x: 21 }, { radius: 0 }, { strength: 0 }, { height: 41 }, { surface: 59 }, { z: NaN }])
      expect(() => editEnvironmentTerrain(source, { ...brush, ...patch })).toThrow();
    expect(() => editEnvironmentTerrain(flat(), { ...brush, mode: 'flatten', height: 0 })).toThrow('没有改变');
  });
  it.each(['rain', 'snow'] as const)('%s 同一秒可复现、不按帧累积，风力改变位移且释放资源', condition => {
    const effect = createEnvironmentEffects(env({ weather: { condition, intensity: 1, wind: 0.5 } }));
    const positions = () => Array.from(effect.object.geometry.attributes.position.array);
    const first = positions(); effect.sampleAt(2); expect(positions()).not.toEqual(first);
    effect.sampleAt(0); expect(positions()).toEqual(first);
    const calm = createEnvironmentEffects(env({ weather: { condition, intensity: 1, wind: 0 } }));
    calm.sampleAt(2); effect.sampleAt(2); expect(positions()).not.toEqual(Array.from(calm.object.geometry.attributes.position.array));
    let disposed = 0; effect.object.geometry.addEventListener('dispose', () => disposed++);
    effect.dispose(); calm.dispose(); expect(disposed).toBe(1);
  });
  it('晴天/零强度不产生粒子，非法时间拒绝', () => {
    const effect = createEnvironmentEffects(env());
    expect(effect.object.geometry.attributes.position.count).toBe(0);
    expect(() => effect.sampleAt(NaN)).toThrow('时间'); effect.dispose();
    const rain = createEnvironmentEffects(env({ weather: { condition: 'rain', intensity: 0, wind: 1 } }));
    expect(rain.object.geometry.attributes.position.count).toBe(0); rain.dispose();
  });
});

describe('场景环境契约', () => {
  it('默认环境可往返 JSON 并通过校验', () => {
    const parsed = validateSceneEnvironment(JSON.parse(JSON.stringify(defaultSceneEnvironment())));
    expect(parsed).toEqual(defaultSceneEnvironment());
    expect(parsed.terrain).toBeNull();
  });

  it('未知版本、未知字段、越界数值都显式拒绝', () => {
    expect(() => validateSceneEnvironment({ ...env(), version: 2 })).toThrow('不支持的环境版本');
    expect(() => validateSceneEnvironment({ ...env(), extra: 1 })).toThrow('未支持的字段');
    expect(() => validateSceneEnvironment({ ...env(), hour: 24.5 })).toThrow('环境小时');
    expect(() => validateSceneEnvironment({ ...env(), latitude: 80 })).toThrow('环境纬度');
    expect(() => validateSceneEnvironment({ ...env(), weather: { condition: 'smog', intensity: 0.5, wind: 0 } }))
      .toThrow('天气类型无效');
    expect(() => validateSceneEnvironment({ ...env(), weather: { condition: 'rain', intensity: 1.5, wind: 0 } }))
      .toThrow('天气强度');
  });

  it('地形字段严格校验长度、范围与未知字段', () => {
    const terrain = createEnvironmentTerrain({ size: 40, segments: 64, seed: 7, amplitude: 3 });
    const accepted = validateSceneEnvironment({ ...env(), terrain });
    expect(accepted.terrain).toEqual(terrain);
    expect(accepted.terrain).not.toBe(terrain);
    expect(() => validateSceneEnvironment({ ...env(), terrain: { ...terrain, heights: terrain.heights.slice(1) } }))
      .toThrow('地形高度数组长度');
    expect(() => validateSceneEnvironment({ ...env(), terrain: { ...terrain, unknownTerrainField: [] } }))
      .toThrow('未支持的字段');
    expect(() => validateSceneEnvironment({ ...env(), terrain: { ...terrain, waterLevels: [] } }))
      .toThrow('水面数组长度');
    const count = terrain.heights.length;
    const waterLevels = Array(count).fill(null); waterLevels[0] = terrain.heights[0] + 1;
    const waterTerrain = { ...terrain, surfaces: [...terrain.surfaces], waterLevels };
    expect(() => validateSceneEnvironment(env({ terrain: waterTerrain }))).toThrow('地表为水体');
    waterTerrain.surfaces[0] = 2;
    expect(validateSceneEnvironment(JSON.parse(JSON.stringify(env({ terrain: waterTerrain })))).terrain).toEqual(waterTerrain);
    waterLevels[0] = terrain.heights[0];
    expect(() => validateSceneEnvironment(env({ terrain: waterTerrain }))).toThrow('河床上方');
    waterLevels[0] = NaN;
    expect(() => validateSceneEnvironment(env({ terrain: waterTerrain }))).toThrow('水面高度');
    expect(() => validateSceneEnvironment({ ...env(), terrain: { ...terrain, heights: terrain.heights.map(() => 41) } }))
      .toThrow('地形高度');
  });
});

describe('确定性天气/时间光照', () => {
  it('同一份环境永远给出同一束光（与调用顺序无关）', () => {
    const first = environmentLight(env({ hour: 8.25, weather: { condition: 'rain', intensity: 0.8, wind: 0.3 } }));
    const second = environmentLight(env({ hour: 8.25, weather: { condition: 'rain', intensity: 0.8, wind: 0.3 } }));
    expect(second).toEqual(first);
    expect(JSON.stringify(second)).toBe(JSON.stringify(first));
  });

  it('赤道正午太阳在头顶，午夜落到地平线以下且没有直射光', () => {
    const noon = environmentLight(env({ hour: 12, latitude: 0 }));
    expect(noon.sunDirection[1]).toBeCloseTo(1, 6);
    expect(noon.sunIntensity).toBeGreaterThan(0);
    expect(noon.daylight).toBeCloseTo(1, 6);
    const midnight = environmentLight(env({ hour: 0, latitude: 0 }));
    expect(midnight.sunDirection[1]).toBeLessThan(0);
    expect(midnight.sunIntensity).toBe(0);
    expect(midnight.ambientIntensity).toBeLessThan(noon.ambientIntensity);
  });

  it('上午/下午的太阳在东/西两侧，方位随小时单调变化', () => {
    const morning = environmentLight(env({ hour: 8, latitude: 30 }));
    const evening = environmentLight(env({ hour: 16, latitude: 30 }));
    expect(morning.sunDirection[0]).toBeGreaterThan(0);
    expect(evening.sunDirection[0]).toBeLessThan(0);
    const later = environmentLight(env({ hour: 9, latitude: 30 }));
    expect(later.sunElevation).toBeGreaterThan(morning.sunElevation);
  });

  it('天气越重雾越浓、直射越弱；风只记录不改变光照', () => {
    const clear = environmentLight(env({ weather: { condition: 'clear', intensity: 0.5, wind: 0.2 } }));
    const fog = environmentLight(env({ weather: { condition: 'fog', intensity: 0.9, wind: 0.2 } }));
    expect(fog.fogDensity).toBeGreaterThan(clear.fogDensity);
    expect(fog.sunIntensity).toBeLessThan(clear.sunIntensity);
    const windier = environmentLight(env({ weather: { condition: 'clear', intensity: 0.5, wind: 0.9 } }));
    expect(windier.fogDensity).toBe(clear.fogDensity);
    expect(windier.sunIntensity).toBe(clear.sunIntensity);
  });

  it('摘要文本包含阶段、太阳高度与地形口径', () => {
    const text = summarizeEnvironment(env({ hour: 12, terrain: createEnvironmentTerrain({ size: 40, segments: 64 }) }));
    expect(text).toContain('白天');
    expect(text).toContain('地形 40×40 米');
    expect(summarizeEnvironment(env())).toContain('平地（无地形）');
  });
});

describe('地形高度场与地面权威', () => {
  it('同一组参数生成同一份高度，不同种子不同', () => {
    const a = createEnvironmentTerrain({ size: 80, segments: 64, seed: 11, amplitude: 6 });
    const b = createEnvironmentTerrain({ size: 80, segments: 64, seed: 11, amplitude: 6 });
    const c = createEnvironmentTerrain({ size: 80, segments: 64, seed: 12, amplitude: 6 });
    expect(a).toEqual(b);
    expect(a.heights).not.toEqual(c.heights);
    expect(a.heights).toHaveLength((64 + 1) ** 2);
    expect(a.heights.every(value => Number.isFinite(value))).toBe(true);
  });

  it('中心平垫保证原点可站人，外围有起伏', () => {
    const terrain = createEnvironmentTerrain({ size: 80, segments: 64, seed: 5, amplitude: 8 });
    expect(groundHeightAt(terrain, 0, 0)).toBe(0);
    expect(terrain.heights.some(value => Math.abs(value) > 0.01)).toBe(true);
    const bounds = terrainBounds(terrain);
    expect(bounds.minHeight).toBeLessThanOrEqual(0);
    expect(bounds.maxHeight).toBeGreaterThan(0);
  });

  it('没有地形时地面高度恒为 0，落点沿用旧平地钳制', () => {
    expect(groundHeightAt(null, 30, -30)).toBe(0);
    // 旧行为只钳 XZ、保留传入的 Y；地形版才会把 Y 压到地面上。
    expect(clampGroundPointToTerrain(null, [100, 0, -100])).toEqual([9.5, 0, -9.5]);
  });

  it('有地形时落点钳到地形内圈并落在地面上', () => {
    const terrain = createEnvironmentTerrain({ size: 40, segments: 64, seed: 3, amplitude: 4 });
    const half = terrain.size / 2 - 0.5;
    const clamped = clampGroundPointToTerrain(terrain, [100, 0, -100]);
    expect(clamped[0]).toBe(half);
    expect(clamped[2]).toBe(-half);
    expect(clamped[1]).toBeCloseTo(groundHeightAt(terrain, half, -half) + 0.02, 6);
    expect(groundHeightAt(terrain, 100, 100)).toBe(0);
  });

  it('坡度在平垫上为 0，在起伏处大于 0', () => {
    const terrain = createEnvironmentTerrain({ size: 80, segments: 64, seed: 9, amplitude: 10 });
    expect(terrainSlopeAt(terrain, 0, 0)).toBeCloseTo(0, 6);
    let maxSlope = 0;
    for (let x = -30; x <= 30; x += 2) maxSlope = Math.max(maxSlope, terrainSlopeAt(terrain, x, 12));
    expect(maxSlope).toBeGreaterThan(0);
  });
});

describe('走位边界证明', () => {
  it('静态障碍用连续相交检测拒绝薄墙和障碍内起终点，平地也不能穿模；不改人工路径', () => {
    const start = { x: -5, z: 0 }, end = { x: 5, z: 0 };
    const obstacles = [{ name: '薄墙', min: [0.011, -1] as [number, number], max: [0.012, 1] as [number, number] }];
    for (const terrain of [null, createEnvironmentTerrain({ size: 40, amplitude: 0 })]) {
      const proof = proveWalkableLine(terrain, start, end, { obstacles });
      expect(proof.ok).toBe(false); expect(proof.reason).toContain('薄墙'); expect(proof.reason).toContain('不会自动绕行');
    }
    expect(proveWalkableLine(null, { x: 0.0115, z: 0 }, end, { obstacles }).ok).toBe(false);
    expect(proveWalkableLine(null, start, { x: 0.0115, z: 0 }, { obstacles }).ok).toBe(false);
    expect(proveWalkableLine(null, { x: -5, z: 2 }, { x: 5, z: 2 }, { obstacles }).ok).toBe(true);
    expect(start).toEqual({ x: -5, z: 0 }); expect(end).toEqual({ x: 5, z: 0 });
    expect(() => proveWalkableLine(null, start, end, { obstacles: [{ min: [NaN, 0], max: [1, 1] }] })).toThrow('包围盒无效');
    expect(() => proveWalkableLine(null, start, end, { obstacleMargin: -1 })).toThrow('边距');
  });
  const ramp = () => {
    // 合成一块沿 +X 线性抬升的坡：0 → 每米 1.5 米，远超默认坡度上限。
    const segments = 64;
    const size = 80;
    const width = segments + 1;
    const heights: number[] = [];
    for (let row = 0; row < width; row += 1) {
      for (let column = 0; column < width; column += 1) {
        const x = column * (size / segments) - size / 2;
        heights.push(x <= 0 ? 0 : Math.min(40, x * 1.5));
      }
    }
    return { size, segments, heights, surfaces: Array(width * width).fill(38), seed: 1 };
  };

  it('没有地形时退化为平地直线，永远可走', () => {
    const proof = proveWalkableLine(null, { x: -5, z: 0 }, { x: 5, z: 0 });
    expect(proof.ok).toBe(true);
    expect(proof.reason).toContain('未启用地形');
    expect(proof.samples).toBe(2);
    expect(proof.climb).toBe(0);
  });

  it('地形内平垫上的直线可走，并给出采样点与高差', () => {
    const terrain = createEnvironmentTerrain({ size: 80, segments: 64, seed: 21, amplitude: 6 });
    const proof = proveWalkableLine(terrain, { x: -3, z: 0 }, { x: 3, z: 0 });
    expect(proof.ok).toBe(true);
    expect(proof.samples).toBeGreaterThanOrEqual(2);
    expect(proof.maxSlope).toBeLessThanOrEqual(WALK_MAX_SLOPE);
    expect(proof.climb).toBeCloseTo(proof.endY - proof.startY, 9);
  });

  it('终点超出地形范围时拒绝并说明边界', () => {
    const terrain = createEnvironmentTerrain({ size: 40, segments: 64, seed: 4, amplitude: 2 });
    const proof = proveWalkableLine(terrain, { x: 0, z: 0 }, { x: 40, z: 0 });
    expect(proof.ok).toBe(false);
    expect(proof.reason).toContain('地形范围外');
  });

  it('超过坡度上限的坡给出具体采样点与坡度，不放宽成可走', () => {
    const terrain = ramp();
    const proof = proveWalkableLine(terrain, { x: -2, z: 0 }, { x: 10, z: 0 });
    expect(proof.ok).toBe(false);
    expect(proof.reason).toContain('坡度');
    expect(proof.reason).toContain(`上限 ${WALK_MAX_SLOPE.toFixed(2)}`);
    expect(proof.maxSlope).toBeGreaterThan(WALK_MAX_SLOPE);
  });

  it('地面路径取样在地形上跟随高度，无地形时保持原插值', () => {
    const path = { start: [-4, 0.02, 0] as [number, number, number], end: [4, 0.02, 0] as [number, number, number] };
    expect(groundPathPointOnTerrain(null, path, 0.5)).toEqual([0, 0.02, 0]);
    const terrain = createEnvironmentTerrain({ size: 80, segments: 64, seed: 31, amplitude: 9 });
    const onHill = groundPathPointOnTerrain(terrain, path, 1);
    expect(onHill[0]).toBe(4);
    expect(onHill[1]).toBeCloseTo(groundHeightAt(terrain, 4, 0) + 0.02, 6);
  });

  it('可自定义上限，但非法上限显式报错', () => {
    const terrain = ramp();
    expect(proveWalkableLine(terrain, { x: 1, z: 0 }, { x: 3, z: 0 }, { maxSlope: 5 }).ok).toBe(true);
    expect(() => proveWalkableLine(terrain, { x: 0, z: 0 }, { x: 1, z: 0 }, { maxSlope: 0 })).toThrow('坡度上限');
    expect(() => proveWalkableLine(terrain, { x: Number.NaN, z: 0 }, { x: 1, z: 0 })).toThrow('不是有限 XZ 坐标');
  });
});

describe('与 Toonflow 地形模型的等价性', () => {
  // ground-path.ts 不 import terrainModel（要能被 node --test 直接跑），
  // 所以高度的三角插值与地表编号上限在两边各有一份：这里把两份钉在一起。
  it('同一份高度场上，本文件的取样与 getTerrainHeight 完全一致', async () => {
    const { getTerrainHeight } = await import('./toonflow/terrainModel');
    const terrain = createEnvironmentTerrain({ size: 60, segments: 64, seed: 991, amplitude: 8 });
    let checked = 0;
    for (let index = 0; index < 97; index += 1) {
      const x = -31 + index * 0.64;
      const z = 29 - index * 0.6;
      expect(groundHeightAt(terrain, x, z)).toBeCloseTo(getTerrainHeight(terrain, x, z), 12);
      checked += 1;
    }
    // 越界：两边都返回 0，不能让落点飞到地形外
    expect(groundHeightAt(terrain, 999, 0)).toBe(0);
    expect(getTerrainHeight(terrain, 999, 0)).toBe(0);
    expect(checked).toBe(97);
  });

  it('地表编号上限与 terrainSurfaceMaxId 同步', async () => {
    const { terrainSurfaceMaxId } = await import('./toonflow/terrainSurfaces');
    const surface = terrainSurfaceMaxId;
    // 分段数下限是 64：height/surface 数组长度必须是 (segments+1)²。
    const segments = 64;
    const width = segments + 1;
    const heights = Array(width * width).fill(0);
    const terrain = {
      size: 20, segments, seed: 1,
      heights,
      surfaces: Array(width * width).fill(surface),
    };
    expect(validateSceneEnvironment({ ...env(), terrain }).terrain?.surfaces[0]).toBe(surface);
    expect(() => validateSceneEnvironment({
      ...env(),
      terrain: { ...terrain, surfaces: Array(width * width).fill(surface + 1) },
    })).toThrow('地形地表');
  });
});