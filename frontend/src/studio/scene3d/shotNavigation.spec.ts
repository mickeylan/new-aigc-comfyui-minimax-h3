import { describe, expect, it } from 'vitest';
import * as THREE from 'three';
import { compileShotNavigation, type ShotSceneEntry } from './shotNavigation';
import type { ScriptShotPlan, ScriptShotMotion } from './toonflowSets';
import { createEnvironmentTerrain, defaultSceneEnvironment, proveWalkableLine } from './ground-path';

const entry = (id: string, x: number, z: number, w = 0.4, d = 0.4): ShotSceneEntry => {
  const object = new THREE.Mesh(new THREE.BoxGeometry(w, 1, d), new THREE.MeshBasicMaterial());
  object.position.set(x, 0.5, z);
  return { id, name: id, object };
};
const motion = (id: string, x: number, z: number, ex: number, ez: number): ScriptShotMotion => ({ targetId: id, targetName: id, start: { x, z }, end: { x: ex, z: ez }, duration: 10, navigation: { version: 1, mode: 'avoid', radius: 0.25, speed: 5 } } as ScriptShotMotion);
const plan = (...motions: ScriptShotMotion[]): ScriptShotPlan => ({ version: 1, sceneId: 'scene', heading: '', location: '', sourceFingerprint: 'source', sceneFingerprint: 'scene', action: [], diagnostics: [], shots: motions.map((m, i) => ({ id: `s${i}`, order: i + 1, start: i * 10, end: (i + 1) * 10, camera: null, dialogue: [], motion: m })) });

describe('compileShotNavigation', () => {
  it('detours around a thin locked wall with continuous edge proofs', () => {
    const entries = [entry('a', -3, 0), { ...entry('wall', 0, 0, 0.01, 3), locked: true }];
    const result = compileShotNavigation(plan(motion('a', -3, 0, 3, 0)), entries, null);
    const shot = result.proof.shots[0]!;
    expect(shot.points.length).toBeGreaterThan(2);
    for (let i = 1; i < shot.points.length; i++) expect(proveWalkableLine(null, shot.points[i - 1]!, shot.points[i]!, { obstacles: shot.blockers, obstacleMargin: 0 }).ok).toBe(true);
    expect(result.sample('s0', 10).position).toEqual({ x: 3, z: 0 });
    expect(() => JSON.stringify(result.proof)).not.toThrow();
  });
  it('reserves future actors at original positions and earlier actors at endpoints', () => {
    const entries = [entry('a', -3, 0), entry('b', 0, 0)];
    const result = compileShotNavigation(plan(motion('a', -3, 0, 3, 0), motion('b', 0, 0, 5, 0)), entries, null);
    expect(result.proof.shots[0]!.points.length).toBeGreaterThan(2); // future b at origin
    expect(result.proof.shots[1]!.points.length).toBeGreaterThan(2); // earlier a at x=3
    // A completed mover may have turned along its routed final edge. Reserve its
    // yaw-invariant envelope, not the smaller original unrotated square.
    const halfDiagonal = Math.hypot(0.2, 0.2);
    // Both obstacle and mover need their actual yaw-invariant radius; a user's
    // smaller declared radius cannot shrink either geometry envelope.
    expect(result.proof.shots[1]!.blockers[0]!.min[0]).toBeCloseTo(3 - 2 * halfDiagonal);
    expect(result.proof.shots[1]!.blockers[0]!.max[0]).toBeCloseTo(3 + 2 * halfDiagonal);
  });
  it('releases vacated actor occupancy in a later time interval', () => {
    const result = compileShotNavigation(plan(motion('a', 0, 0, 0, 3), motion('b', -3, 0, 3, 0)), [entry('a', 0, 0), entry('b', -3, 0)], null);
    expect(result.proof.shots[1]!.points).toHaveLength(2);
  });
  it('rejects occupied endpoints, impossible speed, discontinuous starts and invalid options', () => {
    const entries = [entry('a', -3, 0), entry('wall', 3, 0)];
    expect(() => compileShotNavigation(plan(motion('a', -3, 0, 3, 0)), entries, null)).toThrow('No safe route');
    const m = motion('a', -3, 0, 3, 0) as ScriptShotMotion & { navigation: { speed: number } };
    m.navigation.speed = 0.01;
    expect(() => compileShotNavigation(plan(m), entries.slice(0, 1), null)).toThrow('speed/duration');
    expect(() => compileShotNavigation(plan(motion('a', 0, 0, 3, 0)), entries, null)).toThrow('reserved');
    m.navigation.speed = NaN;
    expect(() => compileShotNavigation(plan(m), entries, null)).toThrow('避障配置无效');
  });
  it('rejects routes beyond terrain limits', () => {
    const env = defaultSceneEnvironment();
    env.terrain = { size: 4, resolution: 2, heights: Array(9).fill(0), surfaces: Array(9).fill(0) } as NonNullable<typeof env.terrain>;
    expect(() => compileShotNavigation(plan(motion('a', 0, 0, 5, 0)), [entry('a', 0, 0)], env)).toThrow('No safe route');
  });
  it('cannot shrink a wide actor into a user-declared tiny proxy', () => {
    const entries=[entry('a',-3,0,3,0.2),entry('wall',3,1.1,0.1,0.1)];
    const m=motion('a',-3,0,3,0); m.navigation!.radius=0.1;
    expect(()=>compileShotNavigation(plan(m),entries,null)).toThrow('No safe route');
  });
  it('rejects wet cells between the old three parallel tracks anywhere in the swept disc', () => {
    const env=defaultSceneEnvironment(); env.terrain=createEnvironmentTerrain({size:40,segments:64,amplitude:0});
    env.terrain.waterLevels=Array(env.terrain.heights.length).fill(null); env.terrain.waterLevels[33*65+27]=0;
    const m=motion('a',-3,0,3,0); m.navigation!.radius=1;
    expect(proveWalkableLine(env.terrain,m.start,m.end).ok).toBe(true);
    expect(()=>compileShotNavigation(plan(m),[entry('a',-3,0)],env)).toThrow('No safe route');
  });
  it('is random-access deterministic and never mutates plan or editing objects', () => {
    const entries = [entry('a', -3, 0), entry('wall', 0, 0, 0.01, 3)];
    const p = plan(motion('a', -3, 0, 3, 0)), before = JSON.stringify(p);
    const matrices = entries.map(e => e.object.matrixWorld.toArray());
    const result = compileShotNavigation(p, entries, null), expected = result.sample('s0', 4);
    result.sample('s0', 9); result.sample('s0', 0);
    expect(result.sample('s0', 4)).toEqual(expected);
    expect(JSON.stringify(p)).toBe(before);
    expect(entries.map(e => e.object.matrixWorld.toArray())).toEqual(matrices);
    entries[1]!.object.position.x = 99;
    expect(result.sample('s0', 4)).toEqual(expected);
  });
  it('does not compile or reinterpret legacy navigation-absent lines', () => {
    const m = motion('a', -3, 0, 3, 0) as ScriptShotMotion & { navigation?: unknown };
    delete m.navigation;
    const result = compileShotNavigation(plan(m), [entry('a', -3, 0), entry('wall', 0, 0)], null);
    expect(result.proof.shots).toEqual([]);
    expect(() => result.sample('s0', 1)).toThrow('Unknown avoid shot');
  });
  it('excludes hidden and splat obstacles but not locked obstacles', () => {
    const hidden = entry('hidden', 0, 0); hidden.object.visible = false;
    const result = compileShotNavigation(plan(motion('a', -3, 0, 3, 0)), [entry('a', -3, 0), hidden, { ...entry('splat', 1, 0), splat: {} }], null);
    expect(result.proof.shots[0]!.points).toHaveLength(2);
  });
});
