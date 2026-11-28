import { describe, expect, it } from 'vitest';
import * as THREE from 'three';
import { applyRevisionProposal, buildRevisionProposals, type ObservationPoint } from './previsRevision';
import { validateScriptShotPlan, type ScriptShotPlan, type ScriptShotCamera } from './scriptShotPlan';
const camera = (target: [number, number, number]): ScriptShotCamera => ({ position: [0, 3, 10], target, fov: 50 });
const plan = (target: [number, number, number] = [0, 1, 0], position: [number, number, number] = [0, 0]): ScriptShotPlan => ({ version: 1, sceneId: 's', heading: '', location: '',
  sourceFingerprint: 'src', sceneFingerprint: 'rev', diagnostics: [], action: [], shots: [{ id: 's1', order: 1, start: 0, end: 4, camera: camera(target),
    dialogue: [], execution: { version: 1, actors: [{ targetId: 'a', targetName: 'A', beats: [{ start: 0, end: 4, from: { x: position[0], z: position[1] }, to: { x: position[0], z: position[1] }, yaw: 0, animation: { index: 0, name: 'idle', duration: 2 } }] }] } }] });
const frame = (over: Partial<ObservationPoint> = {}): ObservationPoint => ({ file: 'observation-001.png', shotId: 's1', seconds: 0, localSeconds: 0, camera: camera([0, 1, 0]), width: 640, height: 360, ...over });
const actorOut = plan([0, 1, 0], [9, 0]);
// world-space NDC probe reused for the human-facing expectation only
const ndc = (c: ScriptShotCamera, point: [number, number, number]) => {
  const view = new THREE.PerspectiveCamera(c.fov, 640 / 360, .1, 10000); view.position.set(...c.position); view.lookAt(new THREE.Vector3(...c.target)); view.updateMatrixWorld(true);
  const v = new THREE.Vector3(...point).project(view); return [v.x, v.y];
};
describe('observation-derived revision proposals', () => {
  it('proposes a horizontal aim only when the actor actually leaves the frame', () => {
    const out = buildRevisionProposals(actorOut, { observations: [frame()] });
    expect(out).toHaveLength(1);
    const proposal = out[0];
    expect(proposal).toMatchObject({ kind: 'camera-pan', shotId: 's1', frameFile: 'observation-001.png', requiresHumanConfirmation: true, applied: false, previousTarget: [0, 1, 0] });
    expect(Math.abs(proposal.evidence.ndc[0])).toBeGreaterThan(.95);
    expect(proposal.target).toEqual([9, 1, 0]);
    expect(proposal.predictedNdc[0]).toBeCloseTo(0, 3);
    expect(proposal.resolved).toBe(true);
    expect(proposal.note).toContain('水平对准');
  });
  it('stays silent for an in-frame actor, a missing execution cast, or a frame outside the shot', () => {
    expect(buildRevisionProposals(plan(), { observations: [frame()] })).toEqual([]);
    const bare = plan(); delete bare.shots[0].execution;
    expect(buildRevisionProposals(bare, { observations: [frame()] })).toEqual([]);
    expect(buildRevisionProposals(actorOut, { observations: [] })).toEqual([]);
    expect(buildRevisionProposals(actorOut, { observations: [frame({ seconds: 9, localSeconds: 9 })] })).toEqual([]);
    expect(buildRevisionProposals(actorOut, { observations: [frame({ width: 0 })] })).toEqual([]);
  });
  it('says the aim is not enough instead of silently changing fov or position', () => {
    const narrow = plan([0, 1, 0], [0, 5]); narrow.shots[0].camera!.fov = 15;
    const out = buildRevisionProposals(narrow, { observations: [frame()] });
    expect(out[0].resolved).toBe(false);
    expect(out[0].note).toContain('未自动改焦距或机位');
    expect(out[0].target).toEqual([0, 1, 5]);
  });
  it('pins one stable id per shot, second and actor and rejects more than 64 proposals', () => {
    const once = buildRevisionProposals(actorOut, { observations: [frame(), frame({ file: 'observation-002.png', seconds: 2, localSeconds: 2 })] });
    expect(once.map(p => p.id)).toEqual(['s1:0:a', 's1:2:a']);
    const frames = (n: number) => Array.from({ length: n }, (_, i) => frame({ file: `observation-${String(i + 1).padStart(3, '0')}.png`, seconds: i * .05, localSeconds: i * .05 }));
    expect(buildRevisionProposals(actorOut, { observations: frames(64) })).toHaveLength(64);
    expect(() => buildRevisionProposals(actorOut, { observations: frames(65) })).toThrow('64');
  });
  it('applies to the aimed shot only, keeps everything else, and returns a validated plan', () => {
    const original = actorOut, before = structuredClone(original), [proposal] = buildRevisionProposals(original, { observations: [frame()] });
    const next = applyRevisionProposal(original, proposal);
    expect(original).toEqual(before);
    expect(next.shots[0].camera!.target).toEqual([9, 1, 0]);
    expect(next.shots[0].camera!.position).toEqual(original.shots[0].camera!.position);
    expect(next.shots[0].camera!.fov).toBe(original.shots[0].camera!.fov);
    expect(next.shots[0].execution).toEqual(original.shots[0].execution);
    expect(validateScriptShotPlan(next)).toEqual(next);
    expect(ndc(next.shots[0].camera!, [9, 0, 0])[0]).toBeCloseTo(0, 3);
  });
  it('rejects a stale proposal, a missing shot and a malformed target without mutating the plan', () => {
    const original = actorOut, before = structuredClone(original), [proposal] = buildRevisionProposals(original, { observations: [frame()] });
    const moved = plan([2, 1, 0], [9, 0]);
    expect(() => applyRevisionProposal(moved, proposal)).toThrow('提案已过期');
    const renamed = plan([0, 1, 0], [9, 0]); renamed.shots[0].id = 'other';
    expect(() => applyRevisionProposal(renamed, proposal)).toThrow('已不存在');
    expect(() => applyRevisionProposal(original, { ...proposal, target: [Infinity, 1, 0] })).toThrow('无效');
    expect(() => applyRevisionProposal(original, { ...proposal, kind: 'auto-story' as never })).toThrow('类型无效');
    expect(original).toEqual(before);
  });
});