import { describe, expect, it } from 'vitest';
import * as THREE from 'three';
import { buildScriptShotPlan, parseScriptScenes, scriptFingerprint, type ScriptSceneDraft } from './toonflowSets';
import { buildScriptPerformancePlan, captureScriptPerformanceContext, suggestScriptCast, type PerformanceCast } from './scriptPerformance';
import { assertScriptShotApplicationSafe, createScriptShotSampler } from './scriptShotSampler';
import { createRiggedPerson } from './toonflow/riggedPerson';
import { defaultSceneEnvironment, createEnvironmentTerrain } from './ground-path';

function fixture(body = '张三：等一等。\n（张三走近李四。）\n李四：请说。\n（张三离开。）') {
  const source = `1. 外景 古街 日\r\n${body.replace(/\n/g, '\r\n')}`;
  const index = parseScriptScenes(source), scene = index.scenes[0];
  const draft = { sceneId: scene.id, review: { ...scene, sourceFingerprint: index.fingerprint,
    sceneFingerprint: scriptFingerprint(`${scene.heading}\n${scene.action.join('\n')}\n${scene.dialogue.map(l => `${l.speaker}:${l.line}`).join('\n')}`), diagnostics: [], conflicts: [] } } as unknown as ScriptSceneDraft;
  const entries = scene.characters.map((name, i) => {
    const object = createRiggedPerson('#ccc'); object.position.x = i * 8;
    return { id: `actor-${i}`, name, object };
  });
  const context = captureScriptPerformanceContext(entries);
  const cast: PerformanceCast[] = context.targets.map((target, i) => ({ character: scene.characters[i], targetId: target.id,
    idle: { ...target.animations.find(c => c.name === 'cc0Idle')! }, talk: { ...target.animations.find(c => c.name === 'cc0Talk')! }, walk: { ...target.animations.find(c => c.name === 'cc0Walk')! } }));
  const base = buildScriptShotPlan(draft);
  const build = (settings = { speed: 1.35, approachDistance: 4, departureDistance: 4 }) => buildScriptPerformancePlan(base, scene, source, cast, context, settings);
  return { source, scene, base, entries, context, cast, build };
}

describe('literal script performance proposals', () => {
  it('suggests distinct editable neutral skeletal placements, never source facts or name-based reuse', () => {
    const choices = suggestScriptCast(['张三', '李四', '王五']);
    expect(choices.map(c => c.character)).toEqual(['张三', '李四', '王五']);
    expect(choices.every(c => c.target.kind === 'placeholder' && c.target.rig === 'skeletal')).toBe(true);
    expect(new Set(choices.map(c => JSON.stringify(c.target))).size).toBe(3);
    expect(suggestScriptCast(['张三', '李四', '王五'])).toEqual(choices);
    expect(() => suggestScriptCast(Array.from({ length: 33 }, (_, i) => `人${i}`))).toThrow('32');
    expect(() => suggestScriptCast(['张三', '张三'])).toThrow('重复');
  });
  it('preserves source chronology and exact dialogue; inserts separate action cuts, holds listeners, and does not mutate inputs', () => {
    const f = fixture(), before = JSON.stringify([f.base, f.cast, f.context]);
    const result = f.build(), shots = result.shots;
    expect(shots).toHaveLength(4);
    expect(shots.map(s => s.dialogue.map(l => l.line))).toEqual([['等一等。'], [], ['请说。'], []]);
    expect(shots.every(s => s.camera === null && s.execution?.actors?.length === 2)).toBe(true);
    expect(shots[0].execution!.actors![0].beats[0].animation.name).toBe('cc0Talk');
    expect(shots[0].execution!.actors![1].beats[0].animation.name).toBe('cc0Idle');
    const move = shots[1].execution!.actors![0].beats;
    expect(move[0].from).toEqual({ x: 0, z: 0 }); expect(move.at(-1)!.to).toEqual({ x: 4, z: 0 });
    expect(move.length).toBeGreaterThan(1); // real clip repetitions, not a frozen walk tail
    expect(shots[2].execution!.actors![0].beats[0].from).toEqual({ x: 4, z: 0 });
    expect(shots[3].execution!.actors![0].beats.at(-1)!.to.x).toBeCloseTo(0);
    expect(result.action).toEqual(f.base.action); expect(JSON.stringify([f.base, f.cast, f.context])).toBe(before);
    const record = JSON.parse(result.diagnostics.at(-1)!);
    expect(record.kind).toBe('script-performance-proposal'); expect(record.coverage).toBe('literal-stage-cues-only');
    expect(record.unresolved).toEqual([]); expect(record.events.map((e: any) => e.line)).toEqual([2, 3, 4, 5]);
    expect(record.settings).toEqual({ speed: 1.35, approachDistance: 4, departureDistance: 4 });
    expect(record.events[1].quote).toBe('（张三走近李四。）');
  });
  it.each(['张三没有走到李四面前。', '如果张三走到李四面前。', '（张三说：“张三走到李四面前。”）', '张三抓住李四手腕。', '张三扑进李四怀里。', '他走到李四面前。', '张三走到门前。', '张三走近李四，然后抓腕。'])('retains unsupported/uncertain action as unresolved instead of faking performance: %s', action => {
    const f = fixture(`张三：等。\n${action}\n李四：好。`), result = f.build();
    expect(result.shots).toHaveLength(2); expect(result.action).toContain(action);
    const record = JSON.parse(result.diagnostics.at(-1)!);
    expect(record.unresolved).toHaveLength(1); expect(record.unresolved[0].quote).toBe(action);
    expect(result.shots.flatMap(s => s.execution!.actors!.flatMap(a => a.beats)).every(b => b.from.x === b.to.x && b.from.z === b.to.z)).toBe(true);
  });
  it('keeps repeated identical lines in order and rejects mismatched source without partial output', () => {
    const f = fixture('张三：等。\n（张三走近李四。）\n李四：好。\n（张三走近李四。）');
    const result = f.build(), record = JSON.parse(result.diagnostics.at(-1)!);
    expect(record.unresolved).toHaveLength(1); expect(record.unresolved[0].reason).toContain('距离');
    expect(() => buildScriptPerformancePlan(f.base, f.scene, f.source.replace('等。', '改。'), f.cast, f.context, { speed: 1.35, approachDistance: 4, departureDistance: 4 })).toThrow('原文');
  });
  it('uses live world coordinates instead of unshifted set placement', () => {
    const f = fixture(); f.entries.forEach(e => { e.object.position.x += 10; e.object.position.z += 20; });
    const result = buildScriptPerformancePlan(f.base, f.scene, f.source, f.cast, captureScriptPerformanceContext(f.entries), { speed: 1.35, approachDistance: 4, departureDistance: 4 });
    expect(result.shots[0].execution!.actors![0].beats[0].from).toEqual({ x: 10, z: 20 });
    expect(result.shots[1].execution!.actors![0].beats.at(-1)!.to).toEqual({ x: 14, z: 20 });
  });
  it('places a front approach in the other actor facing direction, not merely somewhere nearby', () => {
    const f = fixture('张三：等。\n（张三走到李四面前。）\n李四：好。');
    const result = f.build(), last = result.shots[1].execution!.actors![0].beats.at(-1)!;
    expect(last.to.x).toBeCloseTo(8); expect(last.to.z).toBeCloseTo(4);
    expect(Math.abs(last.yaw)).toBeCloseTo(Math.PI);
    // Rotating the listener to +X would route straight THROUGH their body.
    // Reject that case; do not weaken the spacing gate to test endpoint semantics.
    f.context.targets[1].yaw = Math.PI / 2;
    expect(() => f.build()).toThrow('间距');
  });
  it('refuses parented entries rather than mislabeling local coordinates as world coordinates', () => {
    const f = fixture(), parent = new THREE.Group(); parent.position.x = 10; parent.add(f.entries[0].object);
    expect(() => captureScriptPerformanceContext(f.entries)).toThrow('直接挂载');
  });
  it('rejects headingless input explicitly without pretending to skip its first line', () => {
    const f = fixture(); f.scene.heading = ''; f.base.heading = '';
    expect(() => f.build()).toThrow('显式场次');
  });
  it('does not mistake a legitimate English character name for voice-over', () => {
    expect(() => fixture('Vova：等。').build()).not.toThrow();
  });
  it.each(['画外音', 'OS', 'V.O.'])('refuses a voice-over qualifier stripped by the basic dialogue parser: %s', qualifier => {
    expect(() => fixture(`张三（${qualifier}）：等。`).build()).toThrow('画外音');
  });
  it('retains ambiguous character endpoint parsing as a gap instead of taking the last match', () => {
    const f = fixture('甲：等。\n乙走近丙：嗯。\n甲走近乙：好。\n丙：是。\n（甲走近乙走近丙。）');
    const record = JSON.parse(f.build().diagnostics.at(-1)!);
    expect(record.unresolved[0].reason).toContain('多种角色解析');
  });
  it.each(['missing', 'locked', 'hidden', 'clip-name', 'clip-duration', 'duplicate-id', 'duplicate-cast', 'missing-cast'])('rejects invalid or stale identities/resources: %s', mode => {
    const f = fixture();
    if (mode === 'missing') f.context.targets.pop();
    if (mode === 'locked') f.context.targets[0].locked = true;
    if (mode === 'hidden') f.context.targets[0].visible = false;
    if (mode === 'clip-name') f.context.targets[0].animations[f.cast[0].talk.index].name = 'changed';
    if (mode === 'clip-duration') f.context.targets[0].animations[f.cast[0].talk.index].duration += 1;
    if (mode === 'duplicate-id') f.context.targets.push(f.context.targets[0]);
    if (mode === 'duplicate-cast') f.cast[1].targetId = f.cast[0].targetId;
    if (mode === 'missing-cast') f.cast.pop();
    expect(() => f.build()).toThrow();
  });
  it('does not overwrite existing motion or execution and enforces bounded input/settings/output', () => {
    const f = fixture(); f.base.shots[0].execution = { version: 1, actors: [{ targetId: 'actor-0', targetName: '张三', beats: [{ start: 0, end: .2, from: { x: 0, z: 0 }, to: { x: 0, z: 0 }, yaw: 0, animation: f.cast[0].talk }] }] };
    expect(() => f.build()).toThrow('已有');
    delete f.base.shots[0].execution;
    expect(() => f.build({ speed: 0, approachDistance: 4, departureDistance: 4 })).toThrow();
    expect(() => f.build({ speed: 1, approachDistance: .1, departureDistance: 4 })).toThrow();
    expect(() => buildScriptPerformancePlan(f.base, f.scene, ' '.repeat(65537), f.cast, f.context, { speed: 1, approachDistance: 4, departureDistance: 4 })).toThrow('65536');
    f.cast.forEach(c => { c.idle.duration = .0001; c.talk.duration = .0001; c.walk.duration = .0001; });
    f.context.targets.forEach(t => t.animations.forEach(c => { c.duration = .0001; }));
    expect(() => f.build()).toThrow('128');
  });
  it('rejects scenes from another selection and narration rather than inventing a body for voice-over', () => {
    const f = fixture(); f.scene.id = 'other'; expect(() => f.build()).toThrow('场次');
    const narration = fixture('旁白：天亮了。'); expect(() => narration.build()).toThrow('旁白');
  });
  it('captures a synchronous immutable identity/transform/resource revision, excluding camera, with upright pi heading', () => {
    const f = fixture(), first = captureScriptPerformanceContext(f.entries);
    f.entries[0].object.rotation.y = Math.PI;
    const second = captureScriptPerformanceContext(f.entries);
    expect(second.revision).not.toBe(first.revision); expect(Math.abs(second.targets[0].yaw)).toBeCloseTo(Math.PI);
    f.entries[0].object.position.x = 20;
    expect(captureScriptPerformanceContext(f.entries).revision).not.toBe(second.revision);
    expect(first.targets[0].position.x).toBe(0);
  });
  it('runs generated beats through actual rig sampling, preserves baseline, and keeps obstacle/water gates', () => {
    const f = fixture(), result = f.build();
    const plan = { ...result, shots: result.shots.map(s => ({ ...s, camera: { position: [0, 4, 16] as [number, number, number], target: [4, 1, 0] as [number, number, number], fov: 45 } })) };
    expect(() => assertScriptShotApplicationSafe(plan, f.entries, null)).not.toThrow();
    const baseline = f.entries.map(e => e.object.position.toArray());
    const sampler = createScriptShotSampler(plan, { camera: new THREE.PerspectiveCamera(), controls: { target: new THREE.Vector3(), enabled: true, update() {} }, entries: () => f.entries, environment: null });
    sampler.sampleAt(.1); const a = f.entries[0].object.getObjectByName('leftUpperArm')!.quaternion.clone();
    sampler.sampleAt(.4); expect(f.entries[0].object.getObjectByName('leftUpperArm')!.quaternion.angleTo(a)).toBeGreaterThan(.001);
    const walking = plan.shots[1]; sampler.sampleAt(walking.start + (walking.end - walking.start) / 2);
    expect(f.entries[0].object.position.x).toBeCloseTo(2); sampler.stop();
    expect(f.entries.map(e => e.object.position.toArray())).toEqual(baseline);
    const wall = new THREE.Mesh(new THREE.BoxGeometry(1, 3, 1)); wall.position.set(2, 1.5, 0);
    expect(() => assertScriptShotApplicationSafe(plan, [...f.entries, { id: 'wall', name: '墙', object: wall }], null)).toThrow('墙');
    const terrain = createEnvironmentTerrain({ size: 40, segments: 64, amplitude: 0 }); terrain.waterLevels = terrain.heights.map(() => 1);
    expect(() => assertScriptShotApplicationSafe(plan, f.entries, { ...defaultSceneEnvironment(), terrain })).toThrow('水域');
    expect(f.entries.map(e => e.object.position.toArray())).toEqual(baseline);
  });
});
