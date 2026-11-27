import { describe, expect, it } from 'vitest';
import { extractSceneryCandidates, SCENERY_SOURCE_LIMIT, SCENERY_MENTION_LIMIT } from './sceneryCandidates';

describe('offline source scenery candidates (never an executable review)', () => {
  it('keeps every occurrence and exact raw UTF-16 spans including emoji, CRLF and whitespace', () => {
    const source = '😀\r\n两块  黑石在水潭附近。\r\n黑石在水潭对岸。';
    const result = extractSceneryCandidates(source);
    expect(result.status).toBe('review-required');
    expect(result.coverage).toBe('lexical-candidates-only');
    expect(result.mentions.map(m => m.label)).toEqual(['黑石', '水潭', '黑石', '水潭']);
    expect(result.mentions[0]).toMatchObject({ quantity: 2, classifier: '块', evidence: { start: 4, end: 10, quote: '两块  黑石' } });
    expect(result.mentions[2].noun.start).toBe(source.lastIndexOf('黑石'));
    expect(result.mentions[2].id).not.toBe(result.mentions[0].id);
    for (const item of [...result.mentions, ...result.relations]) {
      expect(source.slice(item.evidence.start, item.evidence.end)).toBe(item.evidence.quote);
      expect(source.slice(item.context.start, item.context.end)).toBe(item.context.quote);
    }
    for (const mention of result.mentions) expect(source.slice(mention.noun.start, mention.noun.end)).toBe(mention.label);
  });
  it('does not invent one instance for a bare noun or add repeated mentions together', () => {
    const r = extractSceneryCandidates('黑石。黑石。');
    expect(r.mentions).toHaveLength(2);
    expect(r.mentions.every(m => m.quantity === null && m.concerns.join(' ').includes('数量未明'))).toBe(true);
    expect(r.issues.join(' ')).toContain('身份');
    expect(r).not.toHaveProperty('entities');
    expect(r).not.toHaveProperty('unresolved');
  });
  it('retains classifiers rather than treating one bamboo grove as one bamboo plant', () => {
    const r = extractSceneryCandidates('一片竹林，两棵树，12座木屋，二十一块黑石。');
    expect(r.mentions.map(m => [m.label, m.quantity, m.classifier])).toEqual([
      ['竹林', 1, '片'], ['树', 2, '棵'], ['木屋', 12, '座'], ['黑石', 21, '块'],
    ]);
  });
  it.each(['几块黑石', '数块黑石', '两三块黑石', '两到三块黑石', '2-3块黑石', '十几块黑石', '半块黑石', '零块黑石', '99999块黑石', '约两块黑石', '至少两块黑石', '每处两块黑石', '一百块黑石'])('does not assert a scene count for %s', source => {
    const m = extractSceneryCandidates(source).mentions[0];
    expect(m.quantity).toBeNull();
    expect(m.concerns.join(' ')).toContain('数量');
    expect(m.evidence.quote).toBe(source);
  });
  it.each(['没有两块黑石。', '如果有两块黑石。', '也许有两块黑石。', '“有两块黑石。”', '原先有两块黑石。'])('flags non-asserted or temporal context: %s', source => {
    const m = extractSceneryCandidates(source).mentions[0];
    expect(m.quantity).toBeNull();
    expect(m.concerns.join(' ')).toContain('语境');
  });
  it('flags differing counts without deciding whether mentions refer to the same entity', () => {
    const r = extractSceneryCandidates('两块黑石。三块黑石。');
    expect(r.mentions.map(m => m.quantity)).toEqual([2, 3]);
    expect(r.mentions.every(m => m.concerns.join(' ').includes('数量或身份冲突'))).toBe(true);
  });
  it('extracts only explicit local relation endpoints, without metres, axes, road points or third entities', () => {
    const r = extractSceneryCandidates('黑石 在 水潭 附近。小路通向军营。瀑布在水潭对岸。');
    expect(r.relations.map(v => [v.kind, v.evidence.quote])).toEqual([
      ['near-reference', '黑石 在 水潭 附近'], ['leads-to', '小路通向军营'], ['across-reference', '瀑布在水潭对岸'],
    ]);
    for (const relation of r.relations) {
      expect(r.mentions.some(m => m.id === relation.subject)).toBe(true);
      expect(r.mentions.some(m => m.id === relation.reference)).toBe(true);
      expect(relation).not.toHaveProperty('maxMetres'); expect(relation).not.toHaveProperty('axis');
      expect(relation).not.toHaveProperty('points'); expect(relation).not.toHaveProperty('other');
    }
  });
  it('does not connect endpoints across punctuation, pronouns, adjectives or unstated reference objects', () => {
    const r = extractSceneryCandidates('黑石。他在水潭附近。小路通向那里。对岸有瀑布。黑石在清澈的水潭附近。');
    expect(r.relations).toEqual([]);
    expect(r.issues.join(' ')).toContain('关系');
  });
  it('keeps negated relations visibly uncertain rather than geometric facts', () => {
    const r = extractSceneryCandidates('如果黑石在水潭附近。');
    expect(r.relations[0].concerns.join(' ')).toContain('语境');
    expect(extractSceneryCandidates('黑石不在水潭附近。').relations).toEqual([]);
  });
  it('always reports limited vocabulary, also for zero matches or empty source', () => {
    for (const source of ['', '他转过身，轻轻握住她的手。']) {
      const r = extractSceneryCandidates(source);
      expect(r.mentions).toEqual([]); expect(r.relations).toEqual([]);
      expect(r.issues.join(' ')).toContain('词表'); expect(r.issues.join(' ')).toContain('未识别');
    }
  });
  it('uses longest nonoverlapping vocabulary terms without collapsing distinct noun names', () => {
    expect(extractSceneryCandidates('石桥旁有桥，石头边有黑石。').mentions.map(m => m.label)).toEqual(['石桥', '桥', '石头', '黑石']);
  });
  it.each(['1.5块黑石', '-2块黑石', '负两块黑石', '二点五块黑石', '二分之一块黑石', '第2块黑石', '2,500块黑石'])('does not misread a numeric suffix as an exact count: %s', source => {
    expect(extractSceneryCandidates(source).mentions[0].quantity).toBeNull();
  });
  it.each(['至少有两块黑石', '约有两块黑石', '最多有三座木屋', '每座木屋需要两块黑石', '近两块黑石', '不足两块黑石', '不少于两块黑石', '多于两块黑石', '2, 500块黑石', '2 500块黑石', '要是门口有两块黑石', '他说门口有两块黑石', '后来只有两块黑石'])('does not present qualified or distributed counts as definite: %s', source => {
    const r = extractSceneryCandidates(source);
    const m = r.mentions.at(-1)!;
    expect(m.quantity).toBeNull(); expect(m.concerns.length).toBeGreaterThan(0);
  });
  it.each(['1.5块黑石', '-2块黑石', '第2块黑石', '二分之一块黑石', '2,500块黑石', '2, 500块黑石'])('highlights the rejected numeric context, never a misleading crisp suffix: %s', source => {
    const m = extractSceneryCandidates(source).mentions[0];
    expect(m.quantity).toBeNull(); expect(m.evidence).toEqual({ start: 0, end: source.length, quote: source });
  });
  it('bounds repeated output evidence, not just input and mention count', () => {
    for (const source of ['黑石'.repeat(200) + 'x'.repeat(64000), ('附近' + 'x'.repeat(120) + '，').repeat(512)]) {
      const r = extractSceneryCandidates(source);
      expect(r.status).toBe('unavailable'); expect(r.mentions).toEqual([]); expect(r.relations).toEqual([]);
      expect(r.issues.join(' ')).toContain('输出证据');
    }
  });
  it.each(['小路通向军营和水潭附近。', '军营和小路通向水潭。', '瀑布在水潭对面。'])('surfaces unparsed spatial phrases even beside a successfully parsed relation: %s', source => {
    expect(extractSceneryCandidates(source).issues.join(' ')).toContain('空间关系句式或端点未识别');
  });
  it('carries uncertain sentence context across commas, newlines and quoted sentences', () => {
    for (const source of ['如果下雨，两块黑石在水潭附近。', '他说：“天亮了。两块黑石在水潭附近。”', '如果下雨，\r\n两块黑石在水潭附近。']) {
      const r = extractSceneryCandidates(source);
      expect(r.mentions[0].quantity).toBeNull();
      expect(r.relations[0].concerns.join(' ')).toContain('语境');
    }
  });
  it('bounds numeric matching and relation-only clauses without returning partial results', () => {
    const r = extractSceneryCandidates('1'.repeat(60000) + '块黑石');
    expect(r.mentions[0].quantity).toBeNull();
    const overflow = extractSceneryCandidates('对岸。'.repeat(513));
    expect(overflow.status).toBe('unavailable'); expect(overflow.mentions).toEqual([]);
  });
  it('rejects oversize input and excessive candidates atomically instead of claiming truncated coverage', () => {
    for (const source of ['水潭' + 'x'.repeat(SCENERY_SOURCE_LIMIT), '黑石。'.repeat(SCENERY_MENTION_LIMIT + 1)]) {
      const r = extractSceneryCandidates(source);
      expect(r.status).toBe('unavailable'); expect(r.mentions).toEqual([]); expect(r.relations).toEqual([]);
      expect(r.issues.join(' ')).toContain('预算');
    }
  });
});
