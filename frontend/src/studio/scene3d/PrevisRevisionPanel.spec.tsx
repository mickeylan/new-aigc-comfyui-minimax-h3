import React, { act } from 'react';
import { createRoot, type Root } from 'react-dom/client';
import { afterEach, expect, it, vi } from 'vitest';
import PrevisRevisionPanel from './PrevisRevisionPanel';
import { buildRevisionProposals } from './previsRevision';
import { validateScriptShotPlan, type ScriptShotPlan } from './scriptShotPlan';
vi.mock('./toonflowSets', () => ({ listSetTemplates: () => [] }));
let host: HTMLDivElement, root: Root | undefined;
afterEach(async () => { if (root) await act(async () => root!.unmount()); root = undefined; host?.remove(); });
const plan = (target: [number, number, number] = [0, 1, 0], x = 9): ScriptShotPlan => validateScriptShotPlan({ version: 1, sceneId: 's', heading: '', location: '', sourceFingerprint: 'src', sceneFingerprint: 'rev', diagnostics: [], action: [],
  shots: [{ id: 's1', order: 1, start: 0, end: 4, camera: { position: [0, 3, 10], target, fov: 50 }, dialogue: [],
    execution: { version: 1, actors: [{ targetId: 'a', targetName: 'A', beats: [{ start: 0, end: 4, from: { x, z: 0 }, to: { x, z: 0 }, yaw: 0, animation: { index: 0, name: 'idle', duration: 2 } }] }] } }] });
const frame = (x = 9) => ({ file: 'observation-001.png', shotId: 's1', seconds: 0, localSeconds: 0, camera: { position: [0, 3, 10] as [number, number, number], target: [0, 1, 0] as [number, number, number], fov: 50 }, width: 640, height: 360, x });
const reportOf = (x = 9) => ({ observations: [{ file: 'observation-001.png', shotId: 's1', seconds: 0, localSeconds: 0, camera: { position: [0, 3, 10] as [number, number, number], target: [0, 1, 0] as [number, number, number], fov: 50 }, width: 640, height: 360 }] });
const button = (label: string) => host.querySelector(`[aria-label="${label}"]`) as HTMLButtonElement;
it('lists deterministic framing proposals, applies only on an explicit click, and can dismiss a row', async () => {
  vi.stubGlobal('IS_REACT_ACT_ENVIRONMENT', true);
  host = document.createElement('div'); document.body.append(host); root = createRoot(host);
  const current = plan(), apply = vi.fn(), expected = buildRevisionProposals(current, reportOf());
  await act(async () => root!.render(<PrevisRevisionPanel report={reportOf()} plan={current} disabled={false} onApply={apply} />));
  expect(host.textContent).toContain('不读像素、不调用模型、不自动应用');
  expect(host.textContent).toContain('s1:0:a'.replace('s1:0:a', 'observation-001.png'));
  expect(host.textContent).toContain('对准后预测NDC');
  expect(apply).not.toHaveBeenCalled();
  await act(async () => button(`应用修订提案 ${expected[0].id}`).click());
  expect(apply).toHaveBeenCalledTimes(1);
  expect(apply.mock.calls[0][0]).toMatchObject({ kind: 'camera-pan', shotId: 's1', target: [9, 1, 0], requiresHumanConfirmation: true });
  await act(async () => button(`忽略修订提案 ${expected[0].id}`).click());
  expect(host.textContent).toContain('没有可计算的取景越界提案');
});
it('reports an apply failure without hiding the plan, and renders nothing without a report or plan', async () => {
  host = document.createElement('div'); document.body.append(host); root = createRoot(host);
  const failing = vi.fn(() => { throw Error('提案已过期：机位目标已变化，请重新观察'); });
  await act(async () => root!.render(<PrevisRevisionPanel report={reportOf()} plan={plan()} disabled={false} onApply={failing} />));
  await act(async () => button(`应用修订提案 ${buildRevisionProposals(plan(), reportOf())[0].id}`).click());
  expect(host.querySelector('[role="alert"]')!.textContent).toContain('提案已过期');
  await act(async () => root!.render(<PrevisRevisionPanel report={null} plan={plan()} disabled onApply={failing} />));
  expect(host.textContent).toBe('');
});
it('shows the non-resolved wording and disables both buttons while busy', async () => {
  host = document.createElement('div'); document.body.append(host); root = createRoot(host);
  const narrow = plan(); narrow.shots[0].camera!.fov = 15; narrow.shots[0].execution!.actors![0].beats[0].from = { x: 0, z: 5 }; narrow.shots[0].execution!.actors![0].beats[0].to = { x: 0, z: 5 };
  const report = { observations: [{ ...reportOf().observations[0] }] };
  await act(async () => root!.render(<PrevisRevisionPanel report={report} plan={narrow} disabled onApply={vi.fn()} />));
  expect(host.textContent).toContain('未自动改焦距或机位');
  const id = buildRevisionProposals(narrow, report)[0].id;
  expect(button(`应用修订提案 ${id}`).disabled).toBe(true); expect(button(`忽略修订提案 ${id}`).disabled).toBe(true);
});