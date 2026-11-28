import React, { act } from 'react';
import { createRoot } from 'react-dom/client';
import { expect, it, vi } from 'vitest';
import PrevisPlaybackTimeline, { type PrevisPlaybackState } from './PrevisPlaybackTimeline';
import type { ScriptShotPlan } from './scriptShotPlan';
const plan = { shots: [{ id: 'before', order: 1, start: 0, end: 4 }, { id: 's', order: 2, start: 4, end: 8, execution: { actors: [{ targetId: 'a', targetName: '甲', beats: [{ start: 0, end: 2, animation: { name: 'idle' } }, { start: 2, end: 4, animation: { name: 'talk' } }] }, { targetId: 'b', targetName: '乙', beats: [{ start: 1, end: 4, animation: { name: 'walk' } }] }] } }] } as ScriptShotPlan;
async function fixture(run: (render: (p: PrevisPlaybackState | null, applied?: ScriptShotPlan) => Promise<void>, host: HTMLDivElement) => Promise<void>) {
  (globalThis as any).IS_REACT_ACT_ENVIRONMENT = true;
  const host = document.createElement('div'); document.body.append(host); const root = createRoot(host);
  const render = async (playback: PrevisPlaybackState | null, applied = plan) => { await act(async () => root.render(<PrevisPlaybackTimeline plan={applied} playback={playback} />)); };
  try { await run(render, host); } finally { await act(async () => root.unmount()); host.remove(); }
}
const state = (seconds = 6): PrevisPlaybackState => ({ plan, seconds, index: 1, shots: 2 });
it('shows sampler absolute/local time and aligned multiple actor tracks without a clock or edits', async () => {
  const before = JSON.stringify(plan); const fetchSpy = vi.spyOn(globalThis, 'fetch');
  try { await fixture(async (render, host) => {
    await render(state());
    expect(host.textContent).toContain('全片6.000秒 · 镜内2.000秒'); expect(host.textContent).toContain('甲：talk'); expect(host.textContent).toContain('乙：walk');
    expect(host.querySelector('[role="progressbar"]')?.getAttribute('aria-valuenow')).toBe('2');
    expect((host.querySelector('[role="progressbar"] span') as HTMLElement).style.left).toBe('50%');
    expect(host.querySelectorAll('button,input')).toHaveLength(0);
    await act(async () => { await new Promise(resolve => setTimeout(resolve, 20)); });
    expect(host.textContent).toContain('全片6.000秒'); expect(JSON.stringify(plan)).toBe(before); expect(fetchSpy).not.toHaveBeenCalled();
    await render(state(5)); expect(host.textContent).toContain('甲：idle');
    await render(null); expect(host.innerHTML).toBe('');
  }); } finally { fetchSpy.mockRestore(); }
});
it.each(['different-plan', 'no-sample', 'nan', 'outside', 'end', 'index', 'count'])('hides invalid or stale playback: %s', async kind => {
  await fixture(async (render, host) => {
    const s = state();
    if (kind === 'different-plan') s.plan = structuredClone(plan);
    if (kind === 'no-sample') delete s.seconds;
    if (kind === 'nan') s.seconds = NaN;
    if (kind === 'outside') s.seconds = 3;
    if (kind === 'end') s.seconds = 8;
    if (kind === 'index') s.index = 1.5;
    if (kind === 'count') s.shots = 3;
    await render(s); expect(host.innerHTML).toBe('');
  });
});
