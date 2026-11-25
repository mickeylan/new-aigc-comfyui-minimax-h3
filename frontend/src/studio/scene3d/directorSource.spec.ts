import { describe, expect, it } from 'vitest';
import { snapshotDirectorSource } from './directorSource';

const scene = () => ({ id: 12, project_id: 7, title: '客栈雨夜', content: '他推开门，停在门边。女子没有回头。', location: '客栈', director_source_stale: false, updated_at: '2026-11-25T20:00:00+08:00' });
const shots = () => [{ id: 81, scene_id: 12, order: 1, duration: 3, dialogue: '', speech_presentation: 'silent_visual', performance: '右手推开门，迈一步后停下', blocking: '门口与窗边相望', camera_angle: '平视', camera_movement: '缓推', director_source: 'ai', director_source_sha256: 'source-hash', dialogue_ranges: [], action_timeline: [{ start: 0, end: 3, subject: '男子', action: '推门迈步后停下', state: '门开，人停在门边', camera: '缓推' }], future_field: { exact: ['不得删掉的新字段'] } }];

describe('existing director source boundary', () => {
  it('uses existing prose and silent action shots without requiring script headings, speech or a new model call', async () => {
    const source = await snapshotDirectorSource(7, scene(), shots());
    expect(source.kind).toBe('existing-director-source');
    expect(source.scene).toEqual(scene());
    expect(source.shots).toEqual(shots());
    expect(source.revision).toMatch(/^[a-f0-9]{64}$/);
    expect(source).not.toHaveProperty('plan');
    expect(source.shots[0]).not.toHaveProperty('motion');
  });
  it('retains multiple existing shots and exact fractional durations, never resplitting per dialogue line', async () => {
    const first = { ...shots()[0], duration: 2.345, action_timeline: [] };
    const second = { ...shots()[0], id: 82, order: 2, duration: 4.5, action_timeline: [], dialogue: '甲：进来。\n乙：不。' };
    const source = await snapshotDirectorSource(7, scene(), [first, second]);
    expect(source.shots).toEqual([first, second]);
  });
  it('copies before async hashing and never mutates or shares source records', async () => {
    const input = shots(); const original = structuredClone(input);
    const pending = snapshotDirectorSource(7, scene(), input);
    input[0].future_field.exact[0] = 'late edit';
    const source = await pending;
    expect(source.shots).toEqual(original);
    (source.shots[0].future_field as { exact: string[] }).exact[0] = 'snapshot edit';
    expect(input[0].future_field.exact).toEqual(['late edit']);
  });
  it('uses stable key order but invalidates revision for semantic, provenance and scene changes', async () => {
    const base = await snapshotDirectorSource(7, scene(), shots());
    const reordered = Object.fromEntries(Object.entries(scene()).reverse());
    expect((await snapshotDirectorSource(7, reordered, shots())).revision).toBe(base.revision);
    for (const patch of [{ performance: '转身' }, { director_source_sha256: 'changed' }, { future_field: { exact: ['changed'] } }]) {
      expect((await snapshotDirectorSource(7, scene(), [{ ...shots()[0], ...patch }])).revision).not.toBe(base.revision);
    }
    expect((await snapshotDirectorSource(7, { ...scene(), content: '改后的正文' }, shots())).revision).not.toBe(base.revision);
  });
  it('rejects stale, unknown-status, archived or cross-project scene sources', async () => {
    for (const patch of [{ director_source_stale: true }, { director_source_stale: undefined }, { archived: true }, { project_id: 8 }]) {
      await expect(snapshotDirectorSource(7, { ...scene(), ...patch }, shots())).rejects.toThrow();
    }
  });
  it('rejects absent, cross-scene, duplicated or reordered shots rather than repairing them', async () => {
    for (const input of [[], [{ ...shots()[0], scene_id: 13 }], [shots()[0], { ...shots()[0], order: 2 }], [{ ...shots()[0], order: 2 }]]) {
      await expect(snapshotDirectorSource(7, scene(), input)).rejects.toThrow();
    }
  });
  it('rejects invalid durations and excessive shot counts', async () => {
    for (const duration of [0, -1, 0.1, NaN, Infinity, 86401]) {
      await expect(snapshotDirectorSource(7, scene(), [{ ...shots()[0], duration }])).rejects.toThrow();
    }
    await expect(snapshotDirectorSource(7, scene(), Array.from({ length: 65 }, (_, i) => ({ ...shots()[0], id: 100 + i, order: i + 1 })))).rejects.toThrow(/超过64/);
  });
  it('rejects broken action intervals but preserves empty legacy/null timelines', async () => {
    const step = shots()[0].action_timeline[0];
    for (const action_timeline of [[{ ...step, start: 1 }], [{ ...step, end: 2 }], [{ ...step, end: 4 }], [{ ...step, end: NaN }], [{ ...step, action: null }]]) {
      await expect(snapshotDirectorSource(7, scene(), [{ ...shots()[0], action_timeline }])).rejects.toThrow();
    }
    const result = await snapshotDirectorSource(7, scene(), [{ ...shots()[0], action_timeline: null }]);
    expect(result.shots[0].action_timeline).toBeNull();
  });
  it('rejects data that would be silently lost by JSON serialization, cycles and excessive payloads', async () => {
    const cyclic: Record<string, unknown> = {}; cyclic.self = cyclic;
    for (const future_field of [undefined, NaN, new Date(), [ , 'sparse'], cyclic]) {
      await expect(snapshotDirectorSource(7, scene(), [{ ...shots()[0], future_field }])).rejects.toThrow();
    }
    await expect(snapshotDirectorSource(7, { ...scene(), content: '汉'.repeat(750000) }, shots())).rejects.toThrow(/2MiB/);
  });
});
