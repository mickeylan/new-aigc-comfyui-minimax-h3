// Offline lexical leads, NOT facts, entity resolution, a semantic parser or an
// executable SceneRequirements review. Only raw source text enters this module.
export const SCENERY_SOURCE_LIMIT = 65536; // UTF-16 code units; reject, never truncate.
export const SCENERY_MENTION_LIMIT = 256;
const places = ['军营', '营地', '水潭', '瀑布', '竹林', '树林', '森林', '河流', '河岸', '湖泊', '湖岸', '山谷', '山洞', '山坡', '山崖', '悬崖', '古镇', '村庄', '街道', '庭院', '客栈', '寺庙', '码头', '小路', '石路', '道路', '河', '湖'];
const objects = ['黑石', '岩石', '巨石', '石头', '石桥', '木桥', '桥', '木屋', '房屋', '帐篷', '围墙', '城墙', '石阶', '台阶', '竹子', '竹', '树木', '树', '木门', '门', '窗户', '桌子', '椅子', '木箱', '栅栏', '水井', '路灯'];
export const SCENERY_VOCABULARY: readonly string[] = Object.freeze([...places, ...objects].sort((a, b) => b.length - a.length));
const placeSet = new Set(places);
const vocabulary = new RegExp(SCENERY_VOCABULARY.join('|'), 'gu');
export type SourceSpan = { start: number; end: number; quote: string };
export type SceneryMention = {
  id: string; label: string; kind: 'place' | 'object'; noun: SourceSpan; evidence: SourceSpan; context: SourceSpan;
  quantity: number | null; classifier: string | null; concerns: string[];
};
export type SceneryRelation = {
  id: string; kind: 'near-reference' | 'across-reference' | 'leads-to'; subject: string; reference: string;
  evidence: SourceSpan; context: SourceSpan; concerns: string[];
};
export type SceneryCandidates = {
  status: 'review-required' | 'unavailable'; coverage: 'lexical-candidates-only';
  mentions: SceneryMention[]; relations: SceneryRelation[]; issues: string[];
};
const disclaimer = '有限词表与句式，仅提供离线原文候选；不证明事实、身份、全文覆盖或场景已建成。';
// A deliberately conservative flag: some benign clauses will also need review.
// Unflagged clauses are STILL only candidates, never asserted scene facts.
const uncertainContext = /没有|并无|未见|不存在|不在|不是|并非|不通|未通|不要|不再|已无|也许|可能|或|假如|如果|要是|若|仿佛|好像|似乎|据说|声称|他说|她说|人说|说过|希望|梦见|计划|将要|原先|曾经|过去|以前|原本|后来|如今|移走|搬走|消失|拆除|[“”「」『』"]/u;
const quantityContext = /大约|约有|至少|至多|最多|最少|超过|不到|不满|不足|不少于|不多于|多于|少于|各|每|需要|仅剩|只剩|近\s*(?:有\s*)?[0-9零〇一二两三四五六七八九十百千万]/u;
const spatialCue = /通向|通往|连接|附近|近岸|对岸|对面|旁边|边上|周围|中间|之间|东侧|西侧|南侧|北侧/gu;
const numberToken = '[0-9零〇一二两三四五六七八九十百千万几数多余半]';
const quantitySuffix = new RegExp(`(${numberToken}+(?:[到至—\\-~～、或]${numberToken}+)?)(\\s*)([块座条片棵株个间栋顶处口面扇把张只排段丛])(\\s*)$`, 'u');
const qualifier = /(?:约|大约|大概|至少|至多|最多|最少|超过|不到|不满|各|每处|每边|每侧|每|仅剩|只剩)\s*$/u;
function literalNumber(token: string): number | null {
  if (/^[1-9][0-9]*$/u.test(token)) { const n = Number(token); return n <= 4096 ? n : null; }
  const normal = token.replaceAll('两', '二'), digits = '一二三四五六七八九';
  if (normal.length === 1 && digits.includes(normal)) return digits.indexOf(normal) + 1;
  if (!/^(?:[二三四五六七八九]?十)(?:[一二三四五六七八九])?$/u.test(normal)) return null;
  const [ten, one] = normal.split('十');
  return (ten ? digits.indexOf(ten) + 1 : 1) * 10 + (one ? digits.indexOf(one) + 1 : 0);
}

export function extractSceneryCandidates(source: string): SceneryCandidates {
  const result: SceneryCandidates = { status: 'review-required', coverage: 'lexical-candidates-only', mentions: [], relations: [], issues: [disclaimer] };
  const unavailable = (why: string): SceneryCandidates => ({ ...result, status: 'unavailable', mentions: [], relations: [], issues: [disclaimer, why] });
  if (source.length > SCENERY_SOURCE_LIMIT) return unavailable(`原文超过${SCENERY_SOURCE_LIMIT} UTF-16单元预算，请分场审阅；未截取或分析部分原文。`);
  const span = (start: number, end: number): SourceSpan => ({ start, end, quote: source.slice(start, end) });
  // Budget repeated evidence as rendered/serialized, not only unique source bytes.
  let outputUnits = 1024;
  const reserve = (units: number) => (outputUnits += units) <= 262144;
  const outputOverflow = () => unavailable('输出证据超过262144 UTF-16单元预算，请分场审阅；未保留不完整结果。');
  // Sentence context spans commas/newlines. Quote state also spans sentence ends;
  // neither boundary is evidence that a hypothetical/reported scene became real.
  const quoted = new Uint8Array(source.length), closing: string[] = [];
  const openQuotes: Record<string, string> = { '“': '”', '「': '」', '『': '』' };
  for (let i = 0; i < source.length; i++) {
    const char = source[i];
    if (char === '"') { if (closing.at(-1) === '"') closing.pop(); else closing.push('"'); quoted[i] = 1; }
    else if (openQuotes[char]) { closing.push(openQuotes[char]); quoted[i] = 1; }
    else { quoted[i] = closing.length ? 1 : 0; if (closing.at(-1) === char) closing.pop(); }
  }
  const sentences = Array.from(source.matchAll(/[^。！？!?]+/gu), s => ({ context: span(s.index, s.index + s[0].length), risky: uncertainContext.test(s[0]) }));
  let sentenceIndex = 0, clauseCount = 0;
  // Delimiters stay in the original string. All indices refer to that string.
  for (const clause of source.matchAll(/[^，,。！？!?；;\r\n]+/gu)) {
    if (++clauseCount > 512) return unavailable('分句超过512项预算，请分场审阅；未保留不完整结果。');
    const offset = clause.index, raw = clause[0];
    while (sentences[sentenceIndex].context.end <= offset) sentenceIndex++;
    const { context, risky: sentenceRisk } = sentences[sentenceIndex];
    const risky = sentenceRisk || !!quoted.subarray(offset, offset + raw.length).some(v => v === 1);
    const mentions: SceneryMention[] = [];
    for (const match of raw.matchAll(vocabulary)) {
      if (result.mentions.length >= SCENERY_MENTION_LIMIT) return unavailable(`物件候选超过${SCENERY_MENTION_LIMIT}项预算，请分场审阅；未保留不完整结果。`);
      const local = match.index, start = offset + local, label = match[0], prefix = raw.slice(0, local);
      // Bound the suffix search, including malformed huge numeric tokens. Do not
      // reinterpret a clipped suffix, decimal, fraction, ordinal or signed number.
      const tailStart = Math.max(0, prefix.length - 96);
      const quantityMatch = quantitySuffix.exec(prefix.slice(tailStart));
      const concerns: string[] = [];
      let quantity: number | null = null, classifier: string | null = null, evidenceStart = start;
      if (quantityMatch) {
        const quantityStart = tailStart + quantityMatch.index;
        classifier = quantityMatch[3]; evidenceStart = offset + quantityStart;
        quantity = literalNumber(quantityMatch[1]);
        const before = prefix.slice(0, quantityStart), q = qualifier.exec(before);
        const partial = /(?:[0-9零〇一二两三四五六七八九十百千万负第点.+\-−－±/／%％]|分之)\s*$/u.test(before);
        const splitNumber = /^[\s]*$/u.test(before) && /[0-9][,，]\s*$/u.test(source.slice(context.start, offset + quantityStart));
        if (partial || splitNumber) { quantity = null; evidenceStart = context.start; }
        if (q) { quantity = null; evidenceStart = offset + q.index; }
        if (quantity === null) concerns.push('数量含模糊、范围、限定或不支持数词，须人工确认');
      } else concerns.push('数量未明，不能默认一个');
      if (quantityMatch && quantityContext.test(context.quote)) { quantity = null; evidenceStart = context.start; concerns.push('数量语境有范围、近似或分配限定，须人工确认'); }
      if (risky) { quantity = null; if (quantityMatch) evidenceStart = context.start; concerns.push('语境可能含否定、假设、转述、对白或时间变化，不能直接认作当前布景事实'); }
      const mention: SceneryMention = { id: `mention-${start}`, label, kind: placeSet.has(label) ? 'place' : 'object', noun: span(start, start + label.length), evidence: span(evidenceStart, start + label.length), context, quantity, classifier, concerns };
      if (!reserve(mention.evidence.quote.length + context.quote.length + label.length + 512)) return outputOverflow();
      mentions.push(mention); result.mentions.push(mention);
    }
    const clauseRelations: SceneryRelation[] = [];
    for (let i = 1; i < mentions.length; i++) {
      const subject = mentions[i - 1], reference = mentions[i];
      const between = source.slice(subject.noun.end, reference.noun.start), after = source.slice(reference.noun.end, offset + raw.length);
      let kind: SceneryRelation['kind'] | null = null, end = reference.noun.end;
      if (/^\s*(?:通向|通往|连接)\s*$/u.test(between)) kind = 'leads-to';
      else if (/^\s*(?:在|位于)\s*$/u.test(between)) {
        const suffix = /^\s*(附近|近岸|对岸)/u.exec(after);
        if (suffix) { kind = suffix[1] === '对岸' ? 'across-reference' : 'near-reference'; end += suffix[0].length; }
      }
      if (!kind) continue;
      const relation: SceneryRelation = { id: `relation-${subject.noun.start}-${reference.noun.start}`, kind, subject: subject.id, reference: reference.id,
        evidence: span(subject.noun.start, end), context, concerns: [
          '端点仅为原文提及，不解析代词或合并身份；距离、轴、路径和第三主体均未确定',
          ...(risky ? ['语境有歧义，关系未确认'] : []),
          ...(kind === 'leads-to' ? ['通向不证明道路可通行'] : kind === 'across-reference' ? ['对岸不自动证明两主体分居两侧'] : ['附近/近岸不提供米数']),
        ] };
      if (!reserve(relation.evidence.quote.length + context.quote.length + 512)) return outputOverflow();
      clauseRelations.push(relation); result.relations.push(relation);
    }
    const cues = [...raw.matchAll(spatialCue)];
    const uncoveredCue = cues.some(cue => !clauseRelations.some(r => r.evidence.start <= offset + cue.index && r.evidence.end >= offset + cue.index + cue[0].length));
    const unboundMention = mentions.some(m => !clauseRelations.some(r => r.subject === m.id || r.reference === m.id));
    if (cues.length && (uncoveredCue || unboundMention)) {
      if (!reserve(context.quote.length + 200)) return outputOverflow();
      result.issues.push(`空间关系句式或端点未识别，须人工补充：UTF-16 [${context.start},${context.end}) ${context.quote}`);
    }
  }
  const groups = new Map<string, SceneryMention[]>();
  for (const mention of result.mentions) { const group = groups.get(mention.label) ?? []; group.push(mention); groups.set(mention.label, group); }
  for (const group of groups.values()) if (group.length > 1) {
    if (!reserve(group[0].label.length + 512 * group.length)) return outputOverflow();
    result.issues.push(`“${group[0].label}”重复提及：身份未合并，数量不能相加；请人工判断是同一地点/物件、不同实例或时间变化。`);
    if (new Set(group.flatMap(m => m.quantity === null ? [] : [m.quantity])).size > 1) for (const mention of group) mention.concerns.push('重复提及有数量或身份冲突，不能自动择一或相加');
  }
  if (!result.mentions.length) result.issues.push('未识别词表内地点/物件；零命中不代表原文没有布景要求，请人工通读补齐。');
  if (!result.relations.length) result.issues.push('未识别支持句式的完整空间关系，不能推定不存在关系。');
  result.issues.push('未识别词表外名词、代词指代、跨句关系、尺寸和完整性；请逐句人工核对。');
  return result;
}
