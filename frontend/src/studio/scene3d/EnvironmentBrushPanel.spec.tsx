import React, { act } from 'react';
import { createRoot, type Root } from 'react-dom/client';
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest';
import EnvironmentBrushPanel from './EnvironmentBrushPanel';
import { createEnvironmentTerrain } from './ground-path';
import type { SceneTerrain } from './toonflow/sceneDocument';
let host: HTMLDivElement, root: Root, terrain: SceneTerrain;
const center = 32 * 65 + 32;
const button = (name: string) => [...host.querySelectorAll('button')].find(b => b.textContent?.startsWith(name))!;
async function click(name: string) { await act(async () => button(name).click()); }
async function change(label: string, value: string) {
  await act(async () => {
    const element = host.querySelector(`[aria-label="${label}"]`) as HTMLInputElement;
    Object.getOwnPropertyDescriptor(element.tagName === 'SELECT' ? HTMLSelectElement.prototype : HTMLInputElement.prototype, 'value')!.set!.call(element, value);
    element.dispatchEvent(new Event(element.tagName === 'SELECT' ? 'change' : 'input', { bubbles: true }));
  });
}
function render(disabled = false) { root.render(<EnvironmentBrushPanel terrain={terrain} disabled={disabled} onChange={next => { terrain = next; render(); }} />); }
beforeEach(async () => {
  (globalThis as any).IS_REACT_ACT_ENVIRONMENT = true;
  terrain = createEnvironmentTerrain({ size: 40, segments: 64, amplitude: 0 });
  host = document.createElement('div'); document.body.append(host); root = createRoot(host);
  vi.stubGlobal('fetch', vi.fn(() => { throw Error('must not use network'); }));
  await act(async () => render());
});
afterEach(async () => { await act(async () => root.unmount()); host.remove(); vi.unstubAllGlobals(); });
describe('混合与颜色人工入口', () => {
  it('现代绘图明确激活、59项选择、严格参数、持久化与逐笔撤销', async () => {
    const original = terrain;
    await change('地形笔刷操作', 'surfacePaint');
    expect(host.textContent).toContain('完整59种程序材质库');
    expect(host.querySelectorAll('[aria-label="地形地表"] option')).toHaveLength(59);
    expect(terrain).toBe(original); expect(terrain.surfacePaint).toBeUndefined();
    await change('地形地表', '58'); await change('地形颜色', '#ff0000');
    await change('地形绘图flow', '0.8'); await change('地形绘图hardness', '0.6');
    await click('应用地形笔刷');
    const first = terrain, snapshot = JSON.stringify(first);
    expect(first.surfacePaint!.data.length).toBeGreaterThan(20);
    expect(first.surfacePaint!.styles).toEqual([{ surface: 58, color: '#ff0000' }]);
    await change('地形颜色', '#00ff00'); await click('应用地形笔刷');
    expect(JSON.stringify(first)).toBe(snapshot); expect(terrain.surfacePaint!.styles).toHaveLength(2);
    await click('撤销笔刷'); expect(terrain).toBe(first);
    await change('地形绘图hardness', '1.1'); await click('应用地形笔刷');
    expect(terrain).toBe(first); expect(host.querySelector('[role="alert"]')).not.toBeNull();
    await click('撤销笔刷'); expect(terrain).toBe(original);
    expect(fetch).not.toHaveBeenCalled();
  });
  it('显式混合、全局颜色、撤销和重置共用不可变历史，不联网', async () => {
    const original = terrain;
    await change('地形笔刷操作', 'blend');
    expect(host.textContent).toContain('圆外原有地表边界和纹理外观也可能改变');
    await click('应用地形笔刷'); expect(host.textContent).not.toContain('首笔会将');
    const blended = terrain; expect(blended.surfaceBlend!.weights[center]).toBe(0.5); expect(original.surfaceBlend).toBeUndefined();
    await change('地形笔刷操作', 'surfaceColor'); await change('地形颜色', '#ff0000'); await click('应用地形笔刷');
    expect(terrain.surfaceColors!['15']).toBe('#ff0000'); expect(host.textContent).toContain('整片地形');
    await click('撤销笔刷'); expect(terrain).toBe(blended);
    await click('应用地形笔刷'); await click('重置所选地表色'); expect(terrain.surfaceColors).toEqual({});
    await click('撤销笔刷'); expect(terrain.surfaceColors!['15']).toBe('#ff0000');
    await change('地形地表', '38'); const before = terrain; await click('应用地形笔刷');
    expect(terrain).toBe(before); expect(host.querySelector('[role="alert"]')?.textContent).toContain('原始地面38');
    expect(fetch).not.toHaveBeenCalled();
  });
  it('无水时水色明确拒绝，挖湖后水色可改、撤销与干地恢复不留非法覆盖', async () => {
    await change('地形笔刷操作', 'waterColor'); await change('地形颜色', '#00ff00'); await click('应用地形笔刷');
    expect(host.querySelector('[role="alert"]')?.textContent).toContain('没有改变');
    await change('地形笔刷操作', 'river'); await click('应用地形笔刷');
    await change('地形笔刷操作', 'waterColor'); await click('应用地形笔刷'); expect(terrain.waterColors![center]).toBe('#00ff00');
    const colored = terrain;
    await change('地形笔刷操作', 'drain'); await click('应用地形笔刷'); expect(terrain.waterColors![center]).toBeUndefined();
    await click('撤销笔刷'); expect(terrain).toBe(colored); expect(fetch).not.toHaveBeenCalled();
  });
  it('外部载入清空旧撤销栈；禁用状态禁止全部笔刷提交', async () => {
    await click('应用地形笔刷'); expect(button('撤销笔刷').disabled).toBe(false);
    terrain = createEnvironmentTerrain({ size: 40, segments: 64, amplitude: 1 });
    await act(async () => render()); expect(button('撤销笔刷').disabled).toBe(true);
    const before = terrain; await act(async () => render(true)); await click('应用地形笔刷'); expect(terrain).toBe(before);
  });
});
