import { expect, it } from 'vitest';
import { parseEventSidecar, serializeEventSidecar } from './previsEventSidecar';
import type { PrevisEvent } from './previsEvents';
const events = (): PrevisEvent[] => [{ kind: 'fire', objectId: '道具', start: 0, end: 2, offset: [0, 0, 0], rotation: [0, 0, 0], scale: 1, velocity: [0, 0, 0] }];
it('roundtrips exact event data without mutating or aliasing inputs', () => {
  const source = '{"plan":"same","objects":["stable-id"]}', input = events(), before = structuredClone(input);
  const result = parseEventSidecar(serializeEventSidecar(source, input), source);
  expect(result).toEqual({ source, events: input }); result.events[0].offset[0] = 7; expect(input).toEqual(before);
  expect(parseEventSidecar(serializeEventSidecar(source, []), source).events).toEqual([]);
});
it.each(['version', 'kind', 'source', 'extra', 'eventExtra', 'null', 'numeric', 'identity', 'count'])('rejects %s malformed or foreign data', mode => {
  const doc: any = JSON.parse(serializeEventSidecar('same', events()));
  if (mode === 'version') doc.version = 2;
  if (mode === 'kind') doc.kind = 'other';
  if (mode === 'source') doc.source = 'foreign';
  if (mode === 'extra') doc.execute = 'not allowed';
  if (mode === 'eventExtra') doc.events[0].script = 'not allowed';
  if (mode === 'null') doc.events[0] = null;
  if (mode === 'numeric') doc.events[0].start = '0';
  if (mode === 'identity') doc.events[0].objectId = 1;
  if (mode === 'count') doc.events = Array.from({ length: 33 }, events).flat();
  expect(() => parseEventSidecar(JSON.stringify(doc), 'same')).toThrow();
});
it('enforces UTF8 byte budget before JSON parse or download', () => {
  expect(() => parseEventSidecar('汉'.repeat(45000), 'same')).toThrow('128KiB');
  expect(() => serializeEventSidecar('汉'.repeat(45000), [])).toThrow('128KiB');
});
