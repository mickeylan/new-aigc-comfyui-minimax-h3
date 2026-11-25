import React, { act } from 'react';
import { createRoot } from 'react-dom/client';
import { afterEach, expect, it, vi } from 'vitest';
import * as THREE from 'three';
import ShotEnhancementPanel from './ShotEnhancementPanel';
import type { ScriptShotPlan } from './toonflowSets';

(globalThis as any).IS_REACT_ACT_ENVIRONMENT = true;
const cleanups: (() => Promise<void>)[] = [];
afterEach(async () => { for (const cleanup of cleanups.splice(0)) await cleanup(); });
async function setup(authored = false) {
  const object = new THREE.Mesh(new THREE.BoxGeometry(1, 2, 1), new THREE.MeshBasicMaterial());
  object.position.y = 1; object.animations = [new THREE.AnimationClip('Walk', 2, [])];
  const bone = new THREE.Bone(); bone.name = 'ActualHip'; object.add(bone);
  const locked = object.clone(); locked.position.x = 20;
  const entries = [{ id: 'actor', name: 'Actor', object }, { id: 'locked', name: 'Locked', object: locked, locked: true }];
  const plan: ScriptShotPlan = { version: 1, sceneId: 'scene', heading: 'heading', location: 'location', sourceFingerprint: 'source', sceneFingerprint: 'scene', action: [], diagnostics: [], shots: [{ id: 's1', order: 1, start: 0, end: 4, camera: null, dialogue: [{ index: 0, speaker: 'Alice', line: 'Hello', start: 0, end: 4 }], ...(authored ? { motion: { targetId: 'actor', targetName: 'Actor', start: { x: 0, z: 0 }, end: { x: 0, z: 1 }, duration: 4 } } : {}) }] };
  const onApply = vi.fn(), host = document.createElement('div'); document.body.append(host);
  const root = createRoot(host);
  const render = async (changes = {}) => act(async () => root.render(<ShotEnhancementPanel plan={plan} entries={entries} environment={null} disabled={false} onApply={onApply} {...changes} />));
  await render(); cleanups.push(async () => { await act(async () => root.unmount()); host.remove(); });
  const node = (label: string) => host.querySelector(`[aria-label="${label}"]`) as HTMLInputElement;
  const change = async (label: string, value: string) => act(async () => {
    const el = node(label); const proto = el.tagName === 'TEXTAREA' ? HTMLTextAreaElement.prototype : el.tagName === 'SELECT' ? HTMLSelectElement.prototype : HTMLInputElement.prototype;
    Object.getOwnPropertyDescriptor(proto, 'value')!.set!.call(el, value);
    el.dispatchEvent(new Event(el.tagName === 'SELECT' ? 'change' : 'input', { bubbles: true }));
  });
  const button = (text: string) => [...host.querySelectorAll('button')].find(b => b.textContent === text)!;
  const click = async (el: HTMLElement) => act(async () => el.click());
  const generate = async () => { await change('Alice 角色', 'actor'); await change('Alice clip', '0'); await click(button('生成导演草稿')); };
  return { plan, entries, host, node, change, click, button, generate, render, onApply };
}
it('requires explicit clip and human confirmation; drafting does not mutate or apply', async () => {
  const t = await setup(), before = JSON.stringify(t.plan);
  expect(t.host.querySelector('option[value="locked"]')!.hasAttribute('disabled')).toBe(true);
  await t.change('Alice 角色', 'actor'); await t.click(t.button('生成导演草稿'));
  expect(t.host.querySelector('[role="alert"]')!.textContent).toContain('clip');
  await t.generate(); expect(t.host.textContent).toContain('机位 1'); expect(t.host.textContent).toContain('ActualHip');
  expect(t.onApply).not.toHaveBeenCalled(); expect(JSON.stringify(t.plan)).toBe(before);
  expect(t.button('确认应用导演草稿').disabled).toBe(true);
  await t.click(t.node('人工确认草稿')); await t.click(t.button('确认应用导演草稿'));
  expect(t.onApply).toHaveBeenCalledTimes(1); expect(t.onApply.mock.calls[0][0].shots[0].motion.animation.name).toBe('Walk');
});
it('invalidates confirmed drafts on source, entries, and disabled changes', async () => {
  const t = await setup();
  for (const changes of [{ plan: structuredClone(t.plan) }, { entries: [...t.entries] }, { disabled: true }]) {
    await t.render(); await t.generate(); await t.click(t.node('人工确认草稿'));
    await t.render(changes); expect(t.button('确认应用导演草稿').disabled).toBe(true);
    await t.click(t.button('确认应用导演草稿')); expect(t.onApply).not.toHaveBeenCalled();
  }
});
it('validates per-shot JSON and navigation, then applies a cloned authored plan only explicitly', async () => {
  const t = await setup(true), before = JSON.stringify(t.plan);
  await t.change('本镜', 's1'); await t.change('角色增强 JSON', '{"version":2}'); await t.click(t.button('应用本镜增强'));
  expect(t.host.querySelector('[role="alert"]')!.textContent).toContain('Invalid'); expect(t.onApply).not.toHaveBeenCalled();
  await t.change('角色增强 JSON', '{"version":1}'); await t.click(t.node('本镜避障')); await t.change('避障半径', '-1');
  await t.click(t.button('应用本镜增强')); expect(t.onApply).not.toHaveBeenCalled();
  await t.change('避障半径', '0.2'); await t.change('避障速度', '3'); await t.click(t.button('应用本镜增强'));
  expect(t.onApply).toHaveBeenCalledTimes(1);
  const next = t.onApply.mock.calls[0][0]; expect(next).not.toBe(t.plan);
  expect(next.shots[0].motion.characterMotion).toEqual({ version: 1 });
  expect(next.shots[0].motion.navigation).toEqual({ version: 1, mode: 'avoid', radius: 0.2, speed: 3 });
  expect(JSON.stringify(t.plan)).toBe(before);
});
