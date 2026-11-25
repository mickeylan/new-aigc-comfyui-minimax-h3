import { readFileSync, existsSync } from 'node:fs';
import { resolve } from 'node:path';
import { describe, expect, it, vi } from 'vitest';
const frontend = resolve(existsSync('src/studio/scene3d') ? '.' : 'frontend');
const source = readFileSync(resolve(frontend, 'src/studio/scene3d/Scene3DStage.tsx'), 'utf8');
describe('纯网格不执行空Spark遍历', () => {
  it('生产帧循环按登记列表判断，新增/隐藏splat保持原路径，移除最后一项关闭；不改模型树', () => {
    const start = source.indexOf('    const animate = () => {');
    const end = source.indexOf('\n    animate();', start);
    expect(start).toBeGreaterThan(0); expect(end).toBeGreaterThan(start);
    const entries = Array.from({ length: 1577 }, () => ({ object: { visible: true, traverse: () => { throw Error('must not scan mesh subtree'); } } }));
    const scope: any = {
      frame: 0, guardFrames: 0, guardSince: performance.now(), animationTime: performance.now(), performance,
      requestAnimationFrame: vi.fn(() => 1), weatherEffectsRef: { current: { sampleAt: vi.fn() } }, animationRef: { current: null },
      splatDetailRef: { current: 'manual' }, objectsRef: { current: entries }, controls: { update: vi.fn() }, fkRef: { current: null },
      spark: { visible: true }, scene: {}, camera: {}, renderer: { render: vi.fn() },
    };
    const animate = new Function('scope', `with(scope) { ${source.slice(start, end)}; return animate; }`)(scope);
    const rendered: boolean[] = []; scope.renderer.render.mockImplementation(() => rendered.push(scope.spark.visible));
    animate(); expect(scope.spark.visible).toBe(false);
    const splat = { splat: {}, object: { visible: true } }; scope.objectsRef.current = [...entries, splat];
    animate(); expect(scope.spark.visible).toBe(true);
    splat.object.visible = false; animate(); expect(scope.spark.visible).toBe(true);
    scope.objectsRef.current = entries; animate(); expect(scope.spark.visible).toBe(false);
    expect(rendered).toEqual([false, true, true, false]); expect(scope.controls.update).toHaveBeenCalledTimes(4);
    expect(scope.requestAnimationFrame).toHaveBeenCalledTimes(4); expect(scope.weatherEffectsRef.current.sampleAt).toHaveBeenCalledTimes(4);
    expect(entries).toHaveLength(1577);
    console.log('MESH_ONLY_SPARK_GUARD=PASS empty=off mesh=off splat=on hidden=on removed=off');
  });
});
