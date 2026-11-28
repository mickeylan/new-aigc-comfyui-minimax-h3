import React, { act } from 'react';
import { createRoot } from 'react-dom/client';
import { expect, it, vi } from 'vitest';
import PrevisSeekPanel from './PrevisSeekPanel';
import type { ScriptShotPlan } from './scriptShotPlan';
const plan = { shots: [{ start: 0, end: 2 }] } as ScriptShotPlan;
async function fixture(run: (host: HTMLDivElement, render: (props?: object) => Promise<void>, seek: ReturnType<typeof vi.fn>, stop: ReturnType<typeof vi.fn>) => Promise<void>) {
  (globalThis as any).IS_REACT_ACT_ENVIRONMENT = true;
  const host = document.createElement('div'); document.body.append(host); const root = createRoot(host), onSeek = vi.fn(), onStop = vi.fn();
  const render = async (props = {}) => { await act(async () => root.render(<PrevisSeekPanel plan={plan} disabled={false} holding={false} onSeek={onSeek} onStop={onStop} {...props} />)); };
  try { await render(); await run(host, render, onSeek, onStop); } finally { await act(async () => root.unmount()); host.remove(); }
}
const click = async (host: HTMLElement, text: string) => { await act(async () => (Array.from(host.querySelectorAll('button')).find(b => b.textContent === text)!).click()); };
it('requests explicit frame time without changing plan or claiming sampling success', async () => {
  const before = JSON.stringify(plan);
  await fixture(async (host, _render, seek) => {
    expect(host.querySelector('input')?.max).toBe('47'); expect(seek).not.toHaveBeenCalled();
    await click(host, '定位当前请求帧'); expect(seek).toHaveBeenLastCalledWith(0);
    await click(host, '定位下一帧'); expect(seek).toHaveBeenLastCalledWith(1/24);
    expect(host.textContent).toContain('不是采样成功证据');
    await click(host, '定位上一帧'); expect(seek).toHaveBeenLastCalledWith(0);
    expect(JSON.stringify(plan)).toBe(before);
  });
});
it('allows reseek while holding and explicit restoration; disables requests during ordinary play or busy', async () => {
  await fixture(async (host, render, seek, stop) => {
    await render({ holding: true }); await click(host, '定位下一帧'); expect(seek).toHaveBeenLastCalledWith(1/24);
    await click(host, '停止定位并还原'); expect(stop).toHaveBeenCalledTimes(1);
    await render({ disabled: true, holding: true }); await click(host, '定位下一帧'); expect(seek).toHaveBeenCalledTimes(1);
    expect(host.querySelector('input')?.disabled).toBe(true); await click(host, '停止定位并还原'); expect(stop).toHaveBeenCalledTimes(2);
  });
});
it('allows cancellation before any sampler has published holding state', async () => {
  await fixture(async (host, _render, seek, stop) => {
    await click(host, '定位当前请求帧'); expect(seek).toHaveBeenCalledWith(0);
    await click(host, '停止定位并还原'); expect(stop).toHaveBeenCalledTimes(1);
  });
});
it('dragging the final frame requests an in-range time, not the shot endpoint', async () => {
  await fixture(async (host, _render, seek) => {
    const range = host.querySelector('input')!;
    await act(async () => {
      Object.getOwnPropertyDescriptor(HTMLInputElement.prototype, 'value')!.set!.call(range, '47');
      range.dispatchEvent(new Event('input', { bubbles: true })); range.dispatchEvent(new Event('change', { bubbles: true }));
    });
    expect(seek).toHaveBeenLastCalledWith(47/24); expect(47/24).toBeLessThan(2);
  });
});
it('resets requested frame when applied plan changes and does not automatically sample', async () => {
  await fixture(async (host, render, seek) => {
    await click(host, '定位下一帧'); await render({ plan: structuredClone(plan) });
    expect(host.querySelector('input')?.value).toBe('0'); expect(seek).toHaveBeenCalledTimes(1);
  });
});
it.each([0, -1, NaN, Infinity])('disables malformed duration %s', async end => {
  await fixture(async (host, render, seek) => {
    await render({ plan: { shots: [{ start: 0, end }] } }); expect(host.querySelector('input')?.disabled).toBe(true);
    await click(host, '定位当前请求帧'); expect(seek).not.toHaveBeenCalled();
  });
});
it('one short shot has just frame zero and no endpoint frame', async () => {
  await fixture(async (host, render, seek) => {
    await render({ plan: { shots: [{ start: 0, end: .01 }] } }); expect(host.querySelector('input')?.max).toBe('0');
    await click(host, '定位下一帧'); expect(seek).not.toHaveBeenCalled();
    await click(host, '定位当前请求帧'); expect(seek).toHaveBeenLastCalledWith(0);
  });
});
