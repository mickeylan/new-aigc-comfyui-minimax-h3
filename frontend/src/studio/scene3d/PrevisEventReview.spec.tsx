import React, { act } from 'react';
import { createRoot } from 'react-dom/client';
import { expect, it, vi } from 'vitest';
import EventsPanel from './PrevisEventsPanel';
import type { PrevisEvent } from './previsEvents';
import { serializeEventSidecar } from './previsEventSidecar';
const event = (kind: PrevisEvent['kind'] = 'fire'): PrevisEvent => ({ kind, objectId: 'prop<script>', start: 0, end: 2, offset: [0,0,0], rotation: [0,0,0], velocity: [0,2,0], scale: 1 });
async function fixture(run: (f: { host: HTMLDivElement; render: (props?: object) => Promise<void>; click: (text: string) => Promise<void>; write: ReturnType<typeof vi.fn>; setEvents: (e: PrevisEvent[]) => void; getEvents: () => PrevisEvent[]; source: (s: string) => void; preview: ReturnType<typeof vi.fn>; createURL: ReturnType<typeof vi.fn>; revoke: ReturnType<typeof vi.fn> }) => Promise<void>) {
  (globalThis as any).IS_REACT_ACT_ENVIRONMENT = true;
  let current = [event(), event(), event('smoke')], source = 's';
  const host = document.createElement('div'); document.body.append(host); const root = createRoot(host);
  const write = vi.fn((events: PrevisEvent[]) => { current = structuredClone(events); }), preview = vi.fn(async () => new Blob(['png']));
  const createURL = vi.spyOn(URL, 'createObjectURL').mockReturnValue('blob:review'), revoke = vi.spyOn(URL, 'revokeObjectURL').mockImplementation(() => {});
  const render = async (props = {}) => { await act(async () => root.render(<EventsPanel disabled={false} sceneKey="s" list={() => []} read={() => structuredClone(current)} write={write} preview={preview} sidecarSource={() => source} {...props} />)); };
  const click = async (text: string) => { const button = [...host.querySelectorAll('button')].find(b => b.textContent === text); expect(button).toBeDefined(); await act(async () => button!.click()); };
  try { await render(); await run({ host, render, click, write, setEvents: e => { current = e; }, getEvents: () => current, source: s => { source = s; }, preview, createURL, revoke }); }
  finally { await act(async () => root.unmount()); host.remove(); createURL.mockRestore(); revoke.mockRestore(); }
}
it.each(['delete', 'timing'])('rejects %s after source changes even when events stay identical', async kind => {
  await fixture(async ({ host, click, source, write }) => {
    await click('读取现有对象和骨骼');
    await click(kind === 'delete' ? '准备删除事件1（不应用）' : '准备调整事件1区间（不应用）');
    if (kind === 'timing') await input(host, '调整事件新结束全片秒', '3');
    source('revised-plan-same-events');
    await click(kind === 'delete' ? '确认仅删除事件1' : '确认仅调整事件1区间');
    expect(write).not.toHaveBeenCalled(); expect(host.textContent).toContain('来源已变化');
    expect(host.querySelector(kind === 'delete' ? '[aria-label="待确认单项事件删除"]' : '[aria-label="待确认事件区间调整"]')).toBeNull();
  });
});
it.each(['delete', 'timing'])('clears %s draft on source read failure at preparation or confirmation', async kind => {
  await fixture(async ({ host, click, render, write }) => {
    let fail = false; const sidecarSource = () => { if (fail) throw Error('source unavailable'); return 'stable'; };
    await render({ sidecarSource }); await click('读取现有对象和骨骼');
    const prepare = kind === 'delete' ? '准备删除事件1（不应用）' : '准备调整事件1区间（不应用）';
    const selector = kind === 'delete' ? '[aria-label="待确认单项事件删除"]' : '[aria-label="待确认事件区间调整"]';
    await click(prepare); fail = true; await click(prepare);
    expect(host.querySelector(selector)).toBeNull(); expect(host.textContent).toContain('source unavailable'); expect(write).not.toHaveBeenCalled();
    fail = false; await click(prepare); if (kind === 'timing') await input(host, '调整事件新结束全片秒', '3');
    fail = true; await click(kind === 'delete' ? '确认仅删除事件1' : '确认仅调整事件1区间');
    expect(host.querySelector(selector)).toBeNull(); expect(write).not.toHaveBeenCalled();
  });
});
it.each(['delete', 'timing'])('keeps %s compatible with hosts without a source callback', async kind => {
  await fixture(async ({ click, render, write, host }) => {
    await render({ sidecarSource: undefined }); await click('读取现有对象和骨骼');
    await click(kind === 'delete' ? '准备删除事件1（不应用）' : '准备调整事件1区间（不应用）');
    if (kind === 'timing') await input(host, '调整事件新结束全片秒', '3');
    await click(kind === 'delete' ? '确认仅删除事件1' : '确认仅调整事件1区间'); expect(write).toHaveBeenCalledTimes(1);
  });
});
it('timeline selection prepares the exact duplicate without writes or samples, then reuses source protection', async () => {
  await fixture(async ({ host, click, render, write, preview, source }) => {
    await render({ timelineDuration: () => 4 }); await click('读取现有对象和骨骼');
    expect(host.querySelector('[aria-label="事件区间时间轴"]')?.textContent).toContain('0 — 4秒');
    await click('准备删除事件1（不应用）');
    await act(async () => host.querySelector<HTMLButtonElement>('[aria-label="时间轴准备调整事件2区间"]')!.click());
    expect(host.querySelector('[aria-label="待确认单项事件删除"]')).toBeNull();
    expect(host.querySelector('[aria-label="待确认事件区间调整"]')?.textContent).toContain('事件2');
    expect(write).not.toHaveBeenCalled(); expect(preview).not.toHaveBeenCalled();
    await input(host, '调整事件新结束全片秒', '3'); source('new-plan'); await click('确认仅调整事件2区间');
    expect(write).not.toHaveBeenCalled(); expect(host.textContent).toContain('来源已变化');
  });
});
it('keeps timeline scale as a read snapshot and clears it on scene switch', async () => {
  await fixture(async ({ host, render, click }) => {
    let duration = 4; const timelineDuration = () => duration;
    await render({ timelineDuration }); await click('读取现有对象和骨骼');
    duration = 8; await render({ timelineDuration });
    expect(host.querySelector('[aria-label="事件区间时间轴"]')?.textContent).toContain('0 — 4秒');
    await click('读取现有对象和骨骼'); expect(host.querySelector('[aria-label="事件区间时间轴"]')?.textContent).toContain('0 — 8秒');
    await render({ timelineDuration, sceneKey: 'other' }); expect(host.querySelector('[aria-label="事件区间时间轴"]')?.textContent).toContain('无有效计划时长');
  });
});
it('does not publish partial read results when timeline duration throws', async () => {
  await fixture(async ({ host, render, click, write }) => {
    await render({ timelineDuration: () => { throw Error('duration unavailable'); } }); await click('读取现有对象和骨骼');
    expect(host.querySelectorAll('ol li')).toHaveLength(0); expect(host.textContent).toContain('duration unavailable'); expect(write).not.toHaveBeenCalled();
  });
});
it('reads escaped full event details, then explicitly deletes just one duplicate while preserving order', async () => {
  await fixture(async ({ host, click, write, getEvents }) => {
    await click('读取现有对象和骨骼'); expect(write).not.toHaveBeenCalled();
    expect(host.querySelectorAll('ol li')).toHaveLength(3); expect(host.querySelector('ol script')).toBeNull(); expect(host.querySelector('ol')?.textContent).toContain('prop<script>'); expect(host.querySelector('ol pre')?.textContent).toContain('velocity');
    await click('准备删除事件2（不应用）'); expect(write).not.toHaveBeenCalled();
    await click('确认仅删除事件2'); expect(write).toHaveBeenCalledExactlyOnceWith([event(), event('smoke')]);
    expect(getEvents()).toEqual([event(), event('smoke')]); expect(host.querySelectorAll('ol li')).toHaveLength(2); expect(host.querySelector('[aria-label="待确认单项事件删除"]')).toBeNull();
  });
});
it.each(['replace', 'reorder', 'append', 'clear'])('refuses stale deletion after external %s with zero writes', async mode => {
  await fixture(async ({ host, click, write, setEvents }) => {
    await click('读取现有对象和骨骼'); await click('准备删除事件1（不应用）');
    if (mode === 'replace') setEvents([event('smoke')]); if (mode === 'reorder') setEvents([event('smoke'), event(), event()]);
    if (mode === 'append') setEvents([event(), event(), event('smoke'), event()]); if (mode === 'clear') setEvents([]);
    await click('确认仅删除事件1'); expect(write).not.toHaveBeenCalled(); expect(host.textContent).toContain('当前事件已变化'); expect(host.querySelector('[aria-label="待确认单项事件删除"]')).toBeNull();
  });
});
it('keeps displayed and actual events unchanged on host validation failure', async () => {
  await fixture(async ({ host, click, write, getEvents }) => {
    await click('读取现有对象和骨骼'); await click('准备删除事件2（不应用）'); write.mockImplementationOnce(() => { throw Error('host blocked'); });
    await click('确认仅删除事件2'); expect(getEvents()).toHaveLength(3); expect(host.querySelectorAll('ol li')).toHaveLength(3); expect(host.textContent).toContain('host blocked');
    await click('放弃单项删除'); expect(host.querySelector('[aria-label="待确认单项事件删除"]')).toBeNull();
  });
});
it('blocks confirmation while busy and clears drafts on reread and scene switch', async () => {
  await fixture(async ({ host, click, render, write }) => {
    await click('读取现有对象和骨骼'); await click('准备删除事件1（不应用）'); await render({ disabled: true }); await click('确认仅删除事件1'); expect(write).not.toHaveBeenCalled();
    await click('放弃单项删除'); await render(); await click('准备删除事件1（不应用）'); await click('读取现有对象和骨骼'); expect(host.querySelector('[aria-label="待确认单项事件删除"]')).toBeNull();
    await click('准备删除事件1（不应用）'); await render({ sceneKey: 'other' }); expect(host.querySelectorAll('ol li')).toHaveLength(0); expect(host.querySelector('[aria-label="待确认单项事件删除"]')).toBeNull();
  });
});
it.each(['events', 'source', 'scene', 'disabled'])('discards delayed preview after %s changes without allocating a URL', async mode => {
  await fixture(async ({ host, click, preview, createURL, setEvents, source, render }) => {
    let finish!: (blob: Blob) => void; preview.mockImplementationOnce(() => new Promise(resolve => { finish = resolve; }));
    await click('试演该时刻并取真实视口图（随后还原）');
    if (mode === 'events') setEvents([]); if (mode === 'source') source('changed'); if (mode === 'scene') await render({ sceneKey: 'other' }); if (mode === 'disabled') await render({ disabled: true });
    await act(async () => { finish(new Blob(['png'])); }); expect(createURL).not.toHaveBeenCalled(); expect(host.querySelector('img')).toBeNull();
    if (mode === 'events' || mode === 'source') expect(host.textContent).toContain('请重新试演取图');
  });
});
it('invalidates the sidecar draft on single deletion and removal draft on full clear', async () => {
  await fixture(async ({ host, click, write, getEvents }) => {
    await click('读取现有对象和骨骼');
    const textarea = host.querySelector('textarea')!;
    await act(async () => { Object.getOwnPropertyDescriptor(HTMLTextAreaElement.prototype, 'value')!.set!.call(textarea, serializeEventSidecar('s', [event()])); textarea.dispatchEvent(new Event('input', { bubbles: true })); });
    await click('预览旁档导入（不应用）'); expect(host.textContent).toContain('待确认整体替换：1事件');
    await click('准备删除事件2（不应用）'); await click('确认仅删除事件2');
    expect(host.textContent).not.toContain('确认替换为旁档事件'); expect(write).toHaveBeenCalledTimes(1); expect(getEvents()).toHaveLength(2);
    await click('准备删除事件1（不应用）'); await click('清除全部会话事件'); expect(getEvents()).toEqual([]); expect(host.querySelector('[aria-label="待确认单项事件删除"]')).toBeNull();
  });
});
async function input(host: HTMLElement, label: string, value: string) {
  const element = host.querySelector(`[aria-label="${label}"]`) as HTMLInputElement;
  await act(async () => { Object.getOwnPropertyDescriptor(HTMLInputElement.prototype, 'value')!.set!.call(element, value); element.dispatchEvent(new Event('input', { bubbles: true })); });
}
it('changes only the selected duplicate interval after explicit confirmation and releases previous image', async () => {
  await fixture(async ({ host, click, write, getEvents, revoke }) => {
    await click('读取现有对象和骨骼'); await click('试演该时刻并取真实视口图（随后还原）');
    const before = structuredClone(getEvents()); await click('准备调整事件2区间（不应用）');
    await input(host, '调整事件新开始全片秒', '.5'); await input(host, '调整事件新结束全片秒', '1.5'); expect(write).not.toHaveBeenCalled();
    expect(host.textContent).toContain('飞散弹道/火烟相位会变化'); await click('确认仅调整事件2区间');
    expect(getEvents()).toEqual([before[0], { ...before[1], start: .5, end: 1.5 }, before[2]]); expect(write).toHaveBeenCalledTimes(1);
    expect(host.querySelector('[aria-label="待确认事件区间调整"]')).toBeNull(); expect(host.querySelector('img')).toBeNull(); expect(revoke).toHaveBeenCalledWith('blob:review');
  });
});
it.each([['', '2'], ['-1', '2'], ['2', '2'], ['3', '2'], ['0', '601'], ['0', '2']])('rejects blank/invalid/unchanged interval %s to %s without writing', async (start, end) => {
  await fixture(async ({ host, click, write }) => {
    await click('读取现有对象和骨骼'); await click('准备调整事件1区间（不应用）');
    await input(host, '调整事件新开始全片秒', start); await input(host, '调整事件新结束全片秒', end); await click('确认仅调整事件1区间'); expect(write).not.toHaveBeenCalled();
  });
});
it('rejects expired retiming snapshot and cannot accidentally retime a replacement event', async () => {
  await fixture(async ({ host, click, setEvents, write }) => {
    await click('读取现有对象和骨骼'); await click('准备调整事件2区间（不应用）'); await input(host, '调整事件新结束全片秒', '3');
    setEvents([event('smoke')]); await click('确认仅调整事件2区间'); expect(write).not.toHaveBeenCalled(); expect(host.textContent).toContain('当前事件已变化'); expect(host.querySelector('[aria-label="待确认事件区间调整"]')).toBeNull();
  });
});
it('keeps form and actual events on retiming failure, blocks while busy and clears on scene change', async () => {
  await fixture(async ({ host, click, write, getEvents, render }) => {
    await click('读取现有对象和骨骼'); await click('准备调整事件1区间（不应用）'); await input(host, '调整事件新结束全片秒', '3');
    write.mockImplementationOnce(() => { throw Error('超出全片区间'); }); await click('确认仅调整事件1区间');
    expect(host.textContent).toContain('超出全片区间'); expect(getEvents()[0].end).toBe(2); expect(host.querySelector('[aria-label="调整事件新结束全片秒"]')?.getAttribute('value')).toBe('3');
    await render({ disabled: true }); await click('确认仅调整事件1区间'); expect(write).toHaveBeenCalledTimes(1);
    await render({ sceneKey: 'other' }); expect(host.querySelector('[aria-label="待确认事件区间调整"]')).toBeNull();
  });
});
it('makes deletion and timing drafts mutually exclusive and cancels without writes', async () => {
  await fixture(async ({ host, click, write }) => {
    await click('读取现有对象和骨骼'); await click('准备删除事件1（不应用）'); await click('准备调整事件2区间（不应用）');
    expect(host.querySelector('[aria-label="待确认单项事件删除"]')).toBeNull();
    await click('准备删除事件3（不应用）'); expect(host.querySelector('[aria-label="待确认事件区间调整"]')).toBeNull();
    await click('准备调整事件1区间（不应用）'); await click('放弃区间调整'); expect(write).not.toHaveBeenCalled(); expect(host.querySelector('[aria-label="待确认事件区间调整"]')).toBeNull();
  });
});
it.each(['delete', 'disabled', 'reread'])('releases previous image URL after %s', async mode => {
  await fixture(async ({ host, click, render, revoke }) => {
    await click('读取现有对象和骨骼'); await click('试演该时刻并取真实视口图（随后还原）'); expect(host.querySelector('img')).not.toBeNull();
    if (mode === 'delete') { await click('准备删除事件1（不应用）'); await click('确认仅删除事件1'); }
    if (mode === 'disabled') await render({ disabled: true }); if (mode === 'reread') await click('读取现有对象和骨骼');
    expect(host.querySelector('img')).toBeNull(); expect(revoke).toHaveBeenCalledWith('blob:review');
  });
});
