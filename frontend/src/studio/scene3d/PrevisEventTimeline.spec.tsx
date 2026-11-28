import React, { act } from 'react';
import { createRoot } from 'react-dom/client';
import { expect, it, vi } from 'vitest';
import Timeline from './PrevisEventTimeline';
import type { PrevisEvent } from './previsEvents';
const event = (start = 1, end = 3): PrevisEvent => ({ kind: 'fire', objectId: '<script>道具', start, end, offset: [0,0,0], rotation: [0,0,0], velocity: [0,2,0], scale: 1 });
async function fixture(props: object, test: (host: HTMLDivElement, prepare: ReturnType<typeof vi.fn>) => Promise<void> | void) {
  (globalThis as any).IS_REACT_ACT_ENVIRONMENT = true;
  const host = document.createElement('div'), root = createRoot(host), prepare = vi.fn();
  try { await act(async () => root.render(<Timeline events={[event(), event()]} duration={4} disabled={false} onPrepare={prepare} {...props} />)); await test(host, prepare); }
  finally { await act(async () => root.unmount()); }
}
it('places overlapping duplicates on independent equal-scale rows and preserves identity', async () => {
  await fixture({}, async (host, prepare) => {
    const bars = host.querySelectorAll<HTMLElement>('[data-event-bar]'); expect(bars).toHaveLength(2);
    for (const bar of bars) { expect(bar.style.left).toBe('25%'); expect(bar.style.width).toBe('50%'); }
    expect(host.textContent).toContain('0 — 4秒'); expect(host.querySelector('script')).toBeNull();
    await act(async () => host.querySelectorAll('button')[1].click()); expect(prepare).toHaveBeenCalledExactlyOnceWith(1);
  });
});
it.each([undefined, NaN, Infinity, 0, -1])('does not invent a duration for %s', async duration => {
  await fixture({ duration }, host => { expect(host.querySelector('[data-event-bar]')).toBeNull(); expect(host.textContent).toContain('无有效计划时长'); expect(host.textContent).toContain('[1, 3)'); });
});
it.each([[NaN, 3], [1, Infinity], [-1, 3], [2, 2], [3, 2], [1, 5]])('does not clamp invalid interval %s..%s', async (start, end) => {
  await fixture({ events: [event(start, end)] }, host => { expect(host.querySelector('[data-event-bar]')).toBeNull(); expect(host.textContent).toContain('原值未修改'); expect(host.textContent).toContain(`[${String(start)}, ${String(end)})`); });
});
it('supports full half-open span including zero start and exact end', async () => {
  await fixture({ events: [event(0,4)] }, host => { const bar = host.querySelector<HTMLElement>('[data-event-bar]')!; expect(bar.style.left).toBe('0%'); expect(bar.style.width).toBe('100%'); });
});
it('disables selection when busy', async () => {
  await fixture({ disabled: true }, async (host, prepare) => { await act(async () => host.querySelector('button')!.click()); expect(prepare).not.toHaveBeenCalled(); });
});
it('shows empty state without fabricated bars', async () => {
  await fixture({ events: [] }, host => { expect(host.textContent).toContain('尚无已读取事件'); expect(host.querySelector('button')).toBeNull(); });
});
