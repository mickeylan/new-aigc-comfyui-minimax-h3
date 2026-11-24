import { describe, expect, it } from 'vitest';
import { existsSync, readFileSync } from 'node:fs';
import { resolve } from 'node:path';
import {
  applyScriptCharacterBindings, buildScriptSceneDraft, buildScriptShotPlan, listSetPersonObjects, listSetTemplates, materializeSetDraft,
  parseScriptScenes, recommendSetTemplate, scriptFingerprint, selectScriptScene, validateSetDraft, assertScriptShotPlanSource,
  clearScriptShotCamera, setScriptShotCamera, summarizeScriptShotPlan, updateScriptShotDuration, validateScriptShotCamera, validateScriptShotPlan,
  setScriptShotMotion, clearScriptShotMotion, validateScriptShotMotion, sampleScriptShotMotion, sampleScriptShotMotionAt, assertScriptShotMotionTargets,
  SCRIPT_MOTION_MIN_SECONDS, SCRIPT_MOTION_COORDINATE_LIMIT,
  assertScriptShotPlaybackReady, sampleScriptShotPlayback,
  assertScriptShotReferenceReady, buildScriptShotReferencePack, normalizeScriptReferencePasses, scriptShotReferenceManifestText,
  SCRIPT_REFERENCE_MANIFEST_FILE, SCRIPT_REFERENCE_PASSES,
  SCRIPT_SHOT_LIMIT, SCRIPT_SHOT_MIN_SECONDS, SCRIPT_TIMELINE_MAX_SECONDS,
} from './toonflowSets';
import { estimateChineseDialogueDuration, SPEAKER_SWITCH_SECONDS } from '../../utils/dialogueDuration.js';
import { disposeObject } from './toonflow/sceneResources';

describe('Script scene parsing', () => {
  it('splits Chinese numbered scene headings and keeps interior/time', () => {
    const text = [
      '第一集',
      '',
      '1. 内景 客栈 大堂 夜',
      '张三（压低声音）：门外有人。',
      '李四：先别开门。',
      '',
      '2. 外景 古街 日',
      '人群从牌坊下走过。',
    ].join('\n');
    const index = parseScriptScenes(text);
    expect(index.explicitHeadings).toBe(true);
    expect(index.scenes.map(scene => scene.id)).toEqual(['scene-1', 'scene-2']);
    expect(index.scenes[0]).toMatchObject({ interior: true, timeOfDay: 'night' });
    expect(index.scenes[0].location).toContain('客栈');
    expect(index.scenes[1]).toMatchObject({ interior: false, timeOfDay: 'day' });
    expect(index.scenes[1].location).toContain('古街');
  });

  it('accepts 第N场, 场景N: and INT./EXT. forms', () => {
    const forms = ['第3场 四合院 黄昏', '场景二：咖啡馆 清晨', 'INT. COFFEE SHOP - NIGHT', 'EXT. STREET - DAY'];
    const expected = [
      { interior: null, timeOfDay: 'dusk' },
      { interior: null, timeOfDay: 'dawn' },
      { interior: true, timeOfDay: 'night' },
      { interior: false, timeOfDay: 'day' },
    ];
    forms.forEach((heading, index) => {
      const parsed = parseScriptScenes(`${heading}\n有人说话。`);
      expect(parsed.explicitHeadings, heading).toBe(true);
      expect(parsed.scenes[0].interior, heading).toBe(expected[index]!.interior);
      expect(parsed.scenes[0].timeOfDay, heading).toBe(expected[index]!.timeOfDay);
    });
  });

  it('does not treat a numbered dialogue line as a heading', () => {
    const index = parseScriptScenes('1. 你好，我到了。\n2. 知道了。');
    expect(index.explicitHeadings).toBe(false);
    expect(index.scenes).toHaveLength(1);
  });

  it('accepts a bare numbered heading only when it ends with a time word', () => {
    const withTime = parseScriptScenes('1. 客栈 夜\n张三：来了。');
    expect(withTime.explicitHeadings).toBe(true);
    expect(withTime.scenes[0].location).toBe('客栈');
    expect(withTime.scenes[0].timeOfDay).toBe('night');
    // 同样以数字开头但不以时间词结尾的行仍是台词/动作，不当场次标题。
    expect(parseScriptScenes('1. 他一个人走了\n李四：什么？').explicitHeadings).toBe(false);
  });

  it('reports missing headings instead of inventing scenes or a location', () => {
    const index = parseScriptScenes('张三：走吧。\n两人离开。');
    expect(index.explicitHeadings).toBe(false);
    expect(index.scenes).toHaveLength(1);
    expect(index.scenes[0].location).toBe('');
    // 只给「没分场」这一条诊断，不编造地点，也不产生其它场次。
    expect(index.diagnostics).toHaveLength(1);
    expect(index.diagnostics[0]).toContain('场次标记');
  });

  it('extracts speakers in first-appearance order and preserves dialogue verbatim', () => {
    const index = parseScriptScenes('1. 内景 客厅 日\n李四： 先坐。\n张三（笑）：我不坐，  你先说。\n李四：好。');
    const scene = index.scenes[0];
    expect(scene.characters).toEqual(['李四', '张三']);
    expect(scene.dialogue[1]).toEqual({ speaker: '张三', line: '我不坐，  你先说。' });
    expect(scene.action).toEqual([]);
  });

  it('keeps returning speakers in each selected scene, not only their first scene', () => {
    const parsed = parseScriptScenes('1. 内景 客栈 夜\n张三：走。\n李四：好。\n2. 外景 古街 日\n李四：到了。\n张三：停。\n李四：等一下。');
    expect(parsed.scenes[0].characters).toEqual(['张三', '李四']);
    expect(parsed.scenes[1].characters).toEqual(['李四', '张三']);
    expect(parsed.scenes[1].dialogue).toHaveLength(3);
  });

  it('produces byte-identical output for the same script', () => {
    const text = '1. 内景 客栈 夜\n张三：走。\n2. 外景 古街 日\n李四：跟上。';
    expect(JSON.stringify(parseScriptScenes(text))).toBe(JSON.stringify(parseScriptScenes(text)));
    expect(parseScriptScenes(text).fingerprint).toBe(scriptFingerprint(text));
    expect(parseScriptScenes(text).fingerprint).not.toBe(scriptFingerprint(`${text}\n多一行`));
  });
});

describe('Script scene selection and template choice', () => {
  const script = [
    '1. 内景 客栈 大堂 夜',
    '张三：门外有人。',
    '2. 外景 古街 日',
    '人群走过。',
    '3. 外景 月球表面 日',
    '没有人。',
  ].join('\n');

  it('requires an explicit scene id instead of silently taking the first scene', () => {
    const index = parseScriptScenes(script);
    expect(() => selectScriptScene(index, '')).toThrow('显式选择');
    expect(() => selectScriptScene(index, 'scene-9')).toThrow('不在本次剧本中');
    // 报错必须列出全部可选场次，让用户自己挑。
    expect(() => selectScriptScene(index, '')).toThrow('scene-1');
    expect(() => selectScriptScene(index, '')).toThrow('scene-3');
  });

  it('maps locations only to templates that really exist in the catalog', () => {
    const ids = new Set(listSetTemplates().map(row => row.id));
    expect(recommendSetTemplate({ location: '客栈 大堂', interior: true })?.templateId).toBe('inn');
    expect(recommendSetTemplate({ location: '古街', interior: false })?.templateId).toBe('ancientStreet');
    expect(recommendSetTemplate({ location: '咖啡馆', interior: null })?.templateId).toBe('cafe');
    for (const location of ['客栈', '古街', '咖啡馆', '教室', '卧室', '客厅', '军营', '王座', '四合院', '报告厅', '办公室', '现代街道']) {
      const picked = recommendSetTemplate({ location, interior: null });
      expect(picked && ids.has(picked.templateId), location).toBe(true);
    }
  });

  it('returns null rather than substituting a default template', () => {
    expect(recommendSetTemplate({ location: '月球表面', interior: null })).toBeNull();
    expect(recommendSetTemplate({ location: '', interior: null })).toBeNull();
  });

  it('flags interior/outdoor conflicts for human confirmation', () => {
    expect(recommendSetTemplate({ location: '古街', interior: true })?.conflicts.join('')).toContain('室外');
    expect(recommendSetTemplate({ location: '客栈', interior: false })?.conflicts.join('')).toContain('室内');
    expect(recommendSetTemplate({ location: '客栈', interior: true })?.conflicts).toEqual([]);
  });

  it('builds the requested scene only and reuses the validated set draft', async () => {
    const index = parseScriptScenes(script);
    const draft = await buildScriptSceneDraft(index, 'scene-2', new AbortController().signal);
    expect(draft.templateId).toBe('ancientStreet');
    expect(draft.sceneId).toBe('scene-2');
    expect(draft.requiresConfirmation).toBe(true);
    expect(draft.review.instanceCount).toBe(draft.setDraft.document.objects.length);
    expect(draft.review.instanceCount).toBeGreaterThan(0);
    expect(draft.review.sourceFingerprint).toBe(index.fingerprint);
    expect(() => validateSetDraft(draft.setDraft)).not.toThrow();
    expect(draft.setDraft.templateId).toBe(draft.templateId);
  }, 120_000);

  it('refuses scenes without headings and scenes without a matching template', async () => {
    const noHeading = parseScriptScenes('张三：走吧。');
    await expect(buildScriptSceneDraft(noHeading, 'scene-1', new AbortController().signal)).rejects.toThrow('场次标记');
    const index = parseScriptScenes(script);
    await expect(buildScriptSceneDraft(index, 'scene-3', new AbortController().signal)).rejects.toThrow('没有匹配的内置布景模板');
    await expect(buildScriptSceneDraft(index, '', new AbortController().signal)).rejects.toThrow('显式选择');
  }, 60_000);

  it('lets the human explicitly override an unmatched location, retaining conflicts and original text', async () => {
    const index = parseScriptScenes('1. 外景 月球表面 夜\n张三：这不是客栈。');
    await expect(buildScriptSceneDraft(index, 'scene-1', new AbortController().signal)).rejects.toThrow('没有匹配');
    const draft = await buildScriptSceneDraft(index, 'scene-1', new AbortController().signal, 'bedroom');
    expect(draft.recommendation.matchedBy).toBe('manual');
    expect(draft.templateId).toBe('bedroom');
    expect(draft.review.location).toBe('月球表面');
    expect(draft.review.dialogue).toEqual([{ speaker: '张三', line: '这不是客栈。' }]);
    expect(draft.review.conflicts.join('')).toContain('外景');
    await expect(buildScriptSceneDraft(index, 'scene-1', new AbortController().signal, 'invented')).rejects.toThrow('模板不存在');
  }, 60_000);

  it('honors cancellation before building any geometry', async () => {
    const index = parseScriptScenes(script);
    const controller = new AbortController(); controller.abort();
    await expect(buildScriptSceneDraft(index, 'scene-1', controller.signal)).rejects.toThrow();
  });
});

describe('Script character binding to set people or neutral placeholders', () => {
  const script = '1. 内景 现代卧室 夜\n张三：你回来了。\n李四：嗯。';
  const build = () => buildScriptSceneDraft(parseScriptScenes(script), 'scene-1', new AbortController().signal);

  it('binds a real set person and adds one neutral capsule placeholder with an explicit standing position', async () => {
    const draft = await build();
    const persons = listSetPersonObjects(draft.setDraft);
    expect(persons.length).toBeGreaterThanOrEqual(1);
    // 只列真实人物；整块内景、家具等不是人物候选。
    expect(persons.every(person => ['person', 'seatedPerson', 'kneelingPerson'].includes(person.shape))).toBe(true);
    expect(draft.review.characters).toEqual(['张三', '李四']);
    const before = JSON.stringify(draft);
    const bound = applyScriptCharacterBindings(draft, [
      { character: '张三', target: { kind: 'set-person', objectId: persons[0]!.id } },
      { character: '李四', target: { kind: 'placeholder', position: { x: 0.4, z: -1.1 } } },
    ]);
    expect(JSON.stringify(draft)).toBe(before);
    const objects = bound.setDraft.document.objects;
    expect(objects).toHaveLength(draft.setDraft.document.objects.length + 1);
    const reused = objects.find(object => object.id === persons[0]!.id)!;
    // 绑定是有意的重命名；原素材、姿态与站位保持不变。
    expect(reused.name).toBe('张三');
    expect(reused.asset.id).toBe(draft.setDraft.document.objects.find(object => object.id === persons[0]!.id)!.asset.id);
    expect(reused.position).toEqual(persons[0]!.position);
    const placeholder = objects.find(object => object.name === '李四')!;
    expect(placeholder.id).not.toBe('');
    // 每个实例身份唯一，追加到同一布景时不会与已有对象撞号。
    expect(new Set(objects.map(object => object.id)).size).toBe(objects.length);
    expect(placeholder.name).toBe('李四');
    expect(placeholder.asset).toMatchObject({ id: 'person', kind: 'primitive', shape: 'person' });
    expect(placeholder.position).toEqual({ x: 0.4, y: 0, z: -1.1 });
    expect(placeholder.height).toBe(0);
    expect(placeholder.visible).toBe(true);
    expect(placeholder.gravityEnabled).toBe(true);
    expect(bound.review.instanceCount).toBe(objects.length);
    expect(bound.review.bindings).toEqual([
      { character: '张三', kind: 'set-person', objectId: persons[0]!.id, sourceName: persons[0]!.name, position: persons[0]!.position },
      { character: '李四', kind: 'placeholder', objectId: placeholder.id, sourceName: '胶囊人', position: { x: 0.4, y: 0, z: -1.1 } },
    ]);
    // 仍是可校验的 SetDraft，可直接走既有物化与 v7 存档路径。
    expect(() => validateSetDraft(bound.setDraft)).not.toThrow();
    const entries = materializeSetDraft(bound.setDraft);
    try {
      expect(entries).toHaveLength(objects.length);
      expect(entries.some(entry => entry.id === placeholder.id && entry.name === '李四')).toBe(true);
      expect(entries.every(entry => entry.object.userData.proceduralSource?.generator === 'toonflow-set')).toBe(true);
    } finally { entries.forEach(entry => disposeObject(entry.object)); }
    // 再次应用同一份绑定仍只追加一个占位人，身份不与上一次重复，输入草稿不被修改。
    const again = applyScriptCharacterBindings(draft, [
      { character: '张三', target: { kind: 'set-person', objectId: persons[0]!.id } },
      { character: '李四', target: { kind: 'placeholder', position: { x: 0.4, z: -1.1 } } },
    ]);
    expect(again.setDraft.document.objects).toHaveLength(objects.length);
    expect(new Set(again.setDraft.document.objects.map(object => object.id)).size).toBe(objects.length);
    expect(again.review.bindings?.[1]?.objectId).not.toBe(placeholder.id);
    expect(JSON.stringify(draft)).toBe(before);
  }, 60_000);

  it('does not invent bindings or modify a scene with no speaking characters', async () => {
    const silent = await buildScriptSceneDraft(parseScriptScenes('1. 内景 现代卧室 日\n她推开门。'), 'scene-1', new AbortController().signal);
    expect(silent.review.characters).toEqual([]);
    expect(applyScriptCharacterBindings(silent, [])).toBe(silent);
    expect(() => applyScriptCharacterBindings(silent, [{ character: '张三', target: { kind: 'placeholder', position: { x: 0, z: 0 } } }])).toThrow('没有检出');
  }, 60_000);

  it('rejects missing, duplicated, non-person, overlapping or out-of-range bindings instead of choosing for the user', async () => {
    const draft = await build();
    const persons = listSetPersonObjects(draft.setDraft);
    const [first, second] = draft.review.characters;
    const nonPerson = draft.setDraft.document.objects.find(object => !['person', 'seatedPerson', 'kneelingPerson'].includes(object.asset.shape ?? ''))!;
    expect(() => applyScriptCharacterBindings(draft, [
      { character: first!, target: { kind: 'set-person', objectId: persons[0]!.id } },
    ])).toThrow('每个出场角色');
    expect(() => applyScriptCharacterBindings(draft, [
      { character: first!, target: { kind: 'set-person', objectId: persons[0]!.id } },
      { character: second!, target: { kind: 'set-person', objectId: persons[0]!.id } },
    ])).toThrow('同一个布景人物');
    expect(() => applyScriptCharacterBindings(draft, [
      { character: '王五', target: { kind: 'set-person', objectId: persons[0]!.id } },
      { character: second!, target: { kind: 'placeholder', position: { x: 1, z: 1 } } },
    ])).toThrow('不在本场出场名单中');
    expect(() => applyScriptCharacterBindings(draft, [
      { character: first!, target: { kind: 'set-person', objectId: nonPerson.id } },
      { character: second!, target: { kind: 'placeholder', position: { x: 1, z: 1 } } },
    ])).toThrow('不是可摆放人物');
    expect(() => applyScriptCharacterBindings(draft, [
      { character: first!, target: { kind: 'set-person', objectId: persons[0]!.id } },
      { character: second!, target: { kind: 'placeholder', position: { x: persons[0]!.position.x, z: persons[0]!.position.z } } },
    ])).toThrow('重合');
    expect(() => applyScriptCharacterBindings(draft, [
      { character: first!, target: { kind: 'placeholder', position: { x: 1001, z: 0 } } },
      { character: second!, target: { kind: 'placeholder', position: { x: 1, z: 1 } } },
    ])).toThrow('站位');
    expect(() => applyScriptCharacterBindings(draft, [
      { character: first!, target: { kind: 'placeholder', position: { x: Number.NaN, z: 0 } } },
      { character: second!, target: { kind: 'placeholder', position: { x: 1, z: 1 } } },
    ])).toThrow('站位');
  }, 60_000);
});

describe('Script scene module boundaries', () => {
  it('does not call a model, network or the scene apply path', () => {
    const frontend = resolve(existsSync('src/studio/scene3d') ? '.' : 'frontend');
    const source = readFileSync(resolve(frontend, 'src/studio/scene3d/toonflowSets.ts'), 'utf8');
    for (const forbidden of ['fetch(', 'XMLHttpRequest', 'axios', 'crypto.randomUUID', 'Date.now', 'aiScene', 'kiln']) {
      expect(source.includes(forbidden), forbidden).toBe(false);
    }
  });
});

describe('Script shot list and dialogue timeline', () => {
  const lines = [{ speaker: '张三', line: '门外有人。' }, { speaker: '李四', line: '先别开门。' }];
  const roundTime = (value: number) => Math.round(value * 100) / 100;
  /** 轻量草稿：镜头表只读审阅字段与来源指纹，不需要真的物化布景几何。 */
  const shotDraft = (dialogue: { speaker: string; line: string }[], action: string[] = [], patch: Record<string, any> = {}) => ({
    version: 1, requiresConfirmation: true, sceneId: 'scene-1', templateId: 'bedroom', templateName: '卧室',
    recommendation: { templateId: 'bedroom', templateName: '卧室', matchedBy: 'manual', matchedKeyword: '', conflicts: [] },
    setDraft: { version: 1, templateId: 'bedroom', name: '卧室', document: { version: 1, objects: [] } },
    review: {
      heading: '1. 内景 卧室 夜', location: '卧室', interior: true, timeOfDay: 'night', characters: [], dialogue, action,
      instanceCount: 0, diagnostics: [], conflicts: [], sourceFingerprint: 'fnv1a-00000001-10', sceneFingerprint: 'fnv1a-00000002-10',
    },
    ...patch,
  } as any);

  it('splits one shot per dialogue line, keeps the text verbatim and leaves every camera unassigned', () => {
    const draft = shotDraft(lines);
    const plan = buildScriptShotPlan(draft);
    expect(plan.version).toBe(1);
    expect(plan.sceneId).toBe('scene-1');
    expect(plan.sourceFingerprint).toBe(draft.review.sourceFingerprint);
    expect(plan.sceneFingerprint).toBe(draft.review.sceneFingerprint);
    expect(plan.shots.map(shot => shot.id)).toEqual(['shot-1', 'shot-2']);
    expect(plan.shots[0]).toMatchObject({ order: 1, start: 0, end: 0.59, camera: null });
    // 台词与说话人逐字保留，只补时间，不改写。
    expect(plan.shots[0]!.dialogue).toEqual([{ index: 0, speaker: '张三', line: '门外有人。', start: 0, end: 0.59 }]);
    expect(plan.shots[1]!.start).toBe(plan.shots[0]!.end);
    // 时长复用既有台词估算：每秒 9 字 + 标点停顿，换说话人再加停顿。
    const second = roundTime(estimateChineseDialogueDuration(lines[1]!.line) + SPEAKER_SWITCH_SECONDS);
    // 起止是十进制秒，相减有浮点尾差；行为断言用 1e-6 秒容差，不做四舍五入掩盖。
    expect(plan.shots[1]!.end - plan.shots[1]!.start).toBeCloseTo(second, 6);
    expect(plan.shots.every(shot => shot.camera === null)).toBe(true);
    // 同一份草稿永远得到同一份镜头表（不含随机数或时间戳）。
    expect(JSON.stringify(buildScriptShotPlan(shotDraft(lines)))).toBe(JSON.stringify(plan));
    expect(validateScriptShotPlan(JSON.parse(JSON.stringify(plan)))).toEqual(plan);
  });

  it('keeps action lines verbatim at plan level and never invents a performance duration for them', () => {
    const plan = buildScriptShotPlan(shotDraft(lines, ['两人对视。', '门被推开。']));
    expect(plan.action).toEqual(['两人对视。', '门被推开。']);
    expect(plan.diagnostics.join('')).toContain('未参与自动计时');
    expect(plan.shots.reduce((sum, shot) => sum + (shot.end - shot.start), 0)).toBeCloseTo(plan.shots.at(-1)!.end, 6);
  });

  it('gives an empty line the minimum shot length instead of zero or a dropped shot', () => {
    const plan = buildScriptShotPlan(shotDraft([{ speaker: '张三', line: '' }]));
    expect(plan.shots[0]!.end - plan.shots[0]!.start).toBe(SCRIPT_SHOT_MIN_SECONDS);
    expect(plan.shots[0]!.dialogue[0]).toMatchObject({ start: 0, end: 0 });
  });

  it('refuses missing dialogue, an oversized shot list and malformed plans instead of truncating or replacing them', () => {
    expect(() => buildScriptShotPlan(shotDraft([]))).toThrow('没有台词');
    const many = Array.from({ length: SCRIPT_SHOT_LIMIT + 1 }, (_, index) => ({ speaker: '张三', line: `第${index}句。` }));
    expect(() => buildScriptShotPlan(shotDraft(many))).toThrow(`${SCRIPT_SHOT_LIMIT}`);
    const plan = buildScriptShotPlan(shotDraft(lines));
    expect(() => validateScriptShotPlan({ ...plan, version: 2 })).toThrow('版本');
    expect(() => validateScriptShotPlan({ ...plan, shots: [{ ...plan.shots[0], start: 1, end: 1.59 }] })).toThrow('不连续');
    expect(() => validateScriptShotPlan({ ...plan, shots: [plan.shots[0], { ...plan.shots[1], id: 'shot-1' }] })).toThrow('重复');
    expect(() => validateScriptShotPlan({ ...plan, shots: [{ ...plan.shots[0], dialogue: [{ ...plan.shots[0]!.dialogue[0]!, end: 9 }] }, plan.shots[1]] })).toThrow('超出镜头范围');
    // 旧版本没有该字段可以；有了却缺机位字段就是坏数据，不能被当成“未指定”糊过去。
    expect(() => validateScriptShotPlan({ ...plan, shots: [{ ...plan.shots[0], camera: undefined }, plan.shots[1]] })).toThrow('机位字段');
    expect(SCRIPT_TIMELINE_MAX_SECONDS).toBe(86400);
  });

  it('rewrites one shot duration by shifting later shots, never scaling or truncating dialogue', () => {
    const plan = buildScriptShotPlan(shotDraft(lines));
    const before = JSON.stringify(plan);
    const span = (shot: typeof plan.shots[number]) => shot.dialogue[0]!.end - shot.dialogue[0]!.start;
    const secondDuration = plan.shots[1]!.end - plan.shots[1]!.start;
    const edited = updateScriptShotDuration(plan, 'shot-1', 5);
    expect(JSON.stringify(plan)).toBe(before);
    expect(edited.shots[0]!.end).toBe(5);
    expect(edited.shots[1]!.start).toBe(5);
    expect(edited.shots[1]!.end - edited.shots[1]!.start).toBeCloseTo(secondDuration, 6);
    expect(edited.shots[1]!.dialogue[0]!.start).toBe(5);
    expect(span(edited.shots[1]!)).toBeCloseTo(span(plan.shots[1]!), 6);
    expect(() => updateScriptShotDuration(plan, 'shot-2', 0.2)).toThrow('不得短于台词');
    expect(() => updateScriptShotDuration(plan, 'shot-1', Number.NaN)).toThrow('有限');
    expect(() => updateScriptShotDuration(plan, 'shot-9', 1)).toThrow('不在当前镜头表');
    expect(() => updateScriptShotDuration(plan, 'shot-1', 0)).toThrow('不能短于');
    expect(() => updateScriptShotDuration(plan, 'shot-1', SCRIPT_TIMELINE_MAX_SECONDS)).toThrow('上限');
  });

  it('accepts only an explicitly recorded viewport framing and keeps cleared shots unassigned', () => {
    const plan = buildScriptShotPlan(shotDraft(lines));
    const camera = { position: [2, 1.6, 4] as [number, number, number], target: [0, 1.2, 0] as [number, number, number], fov: 35 };
    const withCamera = setScriptShotCamera(plan, 'shot-1', camera);
    expect(withCamera.shots[0]!.camera).toEqual(camera);
    expect(summarizeScriptShotPlan(withCamera)).toEqual({ shots: 2, seconds: plan.shots[1]!.end, missingCameras: 1 });
    expect(clearScriptShotCamera(withCamera, 'shot-1').shots[0]!.camera).toBeNull();
    expect(plan.shots[0]!.camera).toBeNull();
    expect(() => validateScriptShotCamera({ ...camera, position: [0, 1.2, 0] })).toThrow('重合');
    expect(() => validateScriptShotCamera({ ...camera, fov: 5 })).toThrow('15–90');
    expect(() => validateScriptShotCamera({ ...camera, position: [20000, 0, 0] })).toThrow('±10000');
    expect(() => validateScriptShotCamera({ ...camera, target: [0, Number.NaN, 0] })).toThrow('有限坐标');
  });

  it('invalidates the plan when the script text or the selected scene changes', () => {
    const draft = shotDraft(lines);
    const plan = buildScriptShotPlan(draft);
    expect(assertScriptShotPlanSource(plan, draft)).toBe(plan);
    const changed = shotDraft(lines, [], { review: { ...draft.review, sourceFingerprint: 'fnv1a-deadbeef-11' } });
    expect(() => assertScriptShotPlanSource(plan, changed)).toThrow('失效');
    const otherScene = shotDraft(lines, [], { sceneId: 'scene-2', review: { ...draft.review, sceneFingerprint: 'fnv1a-deadbeef-12' } });
    expect(() => assertScriptShotPlanSource(plan, otherScene)).toThrow('同一场次');
  });

  it('refuses continuous playback while any shot has no recorded camera, naming every missing shot', () => {
    const plan = buildScriptShotPlan(shotDraft(lines));
    const before = JSON.stringify(plan);
    expect(() => assertScriptShotPlaybackReady(plan)).toThrow('还有 2 镜未指定机位（第 1、2 镜）');
    expect(JSON.stringify(plan)).toBe(before);
    const cameraA = { position: [2, 1.6, 4] as [number, number, number], target: [0, 1.2, 0] as [number, number, number], fov: 35 };
    const cameraB = { position: [-3, 2.2, 5] as [number, number, number], target: [0, 1.1, 0] as [number, number, number], fov: 50 };
    const firstOnly = setScriptShotCamera(plan, 'shot-1', cameraA);
    // 缺第二镜机位时仍要阻止，且只报真正缺失的那一镜，不把已记录的机位算成缺失。
    expect(() => assertScriptShotPlaybackReady(firstOnly)).toThrow('还有 1 镜未指定机位（第 2 镜）');
    const ready = setScriptShotCamera(firstOnly, 'shot-2', cameraB);
    expect(assertScriptShotPlaybackReady(ready)).toBe(ready);
    expect(ready.shots.map(shot => shot.camera)).toEqual([cameraA, cameraB]);
  });

  it('samples the shot and line from the confirmed timeline, cuts at boundaries and never borrows a camera', () => {
    const plan = buildScriptShotPlan(shotDraft(lines));
    const cameraA = { position: [2, 1.6, 4] as [number, number, number], target: [0, 1.2, 0] as [number, number, number], fov: 35 };
    const cameraB = { position: [-3, 2.2, 5] as [number, number, number], target: [0, 1.1, 0] as [number, number, number], fov: 50 };
    const ready = setScriptShotCamera(setScriptShotCamera(plan, 'shot-1', cameraA), 'shot-2', cameraB);
    const first = sampleScriptShotPlayback(ready, 0.1);
    expect(first.index).toBe(0);
    expect(first.shot.order).toBe(1);
    expect(first.camera).toEqual(cameraA);
    expect(first.line).toMatchObject({ speaker: '张三', line: '门外有人。' });
    // 边界属于下一镜：不把上一镜的机位或台词延长。
    const boundary = sampleScriptShotPlayback(ready, plan.shots[0]!.end);
    expect(boundary.index).toBe(1);
    expect(boundary.camera).toEqual(cameraB);
    expect(boundary.line).toMatchObject({ speaker: '李四', line: '先别开门。' });
    // 台词结束后镜头内的停顿没有当前台词，也不借相邻台词。
    const pause = sampleScriptShotPlayback(ready, plan.shots[1]!.end);
    expect(pause.index).toBe(1);
    expect(pause.line).toBeNull();
    // 超过总时长钳制在最后一镜，不越界也不回绕。
    expect(sampleScriptShotPlayback(ready, 999).seconds).toBe(plan.shots[1]!.end);
    expect(() => sampleScriptShotPlayback(ready, -1)).toThrow('有限非负数');
    expect(() => sampleScriptShotPlayback(ready, Number.NaN)).toThrow('有限非负数');
    // 即使绕过就绪检查直接采样，缺机位也显式失败，不沿用上一镜相机。
    const firstOnly = setScriptShotCamera(plan, 'shot-1', cameraA);
    expect(() => sampleScriptShotPlayback(firstOnly, plan.shots[1]!.start)).toThrow('第 2 镜未指定机位');
  });

  it('builds an ordered reference pack manifest from recorded cameras without touching the plan', () => {
    const draft = shotDraft(lines);
    const plan = buildScriptShotPlan(draft);
    const cameraA = { position: [2, 1.6, 4] as [number, number, number], target: [0, 1.2, 0] as [number, number, number], fov: 35 };
    const cameraB = { position: [-3, 2.2, 5] as [number, number, number], target: [0, 1.1, 0] as [number, number, number], fov: 50 };
    const ready = setScriptShotCamera(setScriptShotCamera(plan, 'shot-1', cameraA), 'shot-2', cameraB);
    const before = JSON.stringify(ready);
    const pack = buildScriptShotReferencePack(ready, { now: new Date('2026-11-24T15:18:00.000Z') });
    expect(JSON.stringify(ready)).toBe(before);
    expect(pack).toMatchObject({
      version: 1, kind: 'script-shot-reference-pack', sceneId: 'scene-1',
      sourceFingerprint: draft.review.sourceFingerprint, sceneFingerprint: draft.review.sceneFingerprint,
      exportedAt: '2026-11-24T15:18:00.000Z', cameraSource: 'viewport-recorded',
      manifestFile: SCRIPT_REFERENCE_MANIFEST_FILE, passes: [...SCRIPT_REFERENCE_PASSES],
    });
    // 镜序、镜头 ID、固定采样秒数与人工机位逐镜写进清单；第 2 镜在第 1 镜结束处采样。
    expect(pack.shots.map(shot => [shot.order, shot.shotId, shot.sampleSeconds, shot.camera])).toEqual([
      [1, 'shot-1', 0, cameraA],
      [2, 'shot-2', plan.shots[0]!.end, cameraB],
    ]);
    // 文件名前缀即镜序，按清单顺序展开就是包内顺序。
    expect(pack.shots.flatMap(shot => shot.files.map(file => file.name))).toEqual([
      'shot-01-clay.png', 'shot-01-normal.png', 'shot-01-depth.png',
      'shot-02-clay.png', 'shot-02-normal.png', 'shot-02-depth.png',
    ]);
    expect(pack.shots[1]!.dialogue).toEqual(plan.shots[1]!.dialogue);
    const parsed = JSON.parse(scriptShotReferenceManifestText(pack));
    expect(parsed.shots[1].camera).toEqual(cameraB);
    expect(parsed.shots[1].sampleSeconds).toBe(pack.shots[1]!.sampleSeconds);
    expect(parsed.notes.join('')).toContain('不生成角色表演');
  });

  it('refuses a partial or guessed reference pack instead of shipping what it has', () => {
    const draft = shotDraft(lines);
    const plan = buildScriptShotPlan(draft);
    const cameraA = { position: [2, 1.6, 4] as [number, number, number], target: [0, 1.2, 0] as [number, number, number], fov: 35 };
    const cameraB = { position: [-3, 2.2, 5] as [number, number, number], target: [0, 1.1, 0] as [number, number, number], fov: 50 };
    // 缺任一镜机位就整包拒绝，而不是只导出有 机位的那几镜。
    expect(() => buildScriptShotReferencePack(plan)).toThrow('未指定机位');
    expect(() => assertScriptShotReferenceReady(plan)).toThrow('第 1、2 镜');
    const ready = setScriptShotCamera(setScriptShotCamera(plan, 'shot-1', cameraA), 'shot-2', cameraB);
    expect(() => buildScriptShotReferencePack(ready, { passes: [] })).toThrow('至少');
    expect(() => buildScriptShotReferencePack(ready, { passes: ['clay', 'clay'] })).toThrow('重复');
    expect(() => buildScriptShotReferencePack(ready, { passes: ['lineart'] })).toThrow('不支持');
    expect(() => buildScriptShotReferencePack(ready, { now: new Date(Number.NaN) })).toThrow('时间戳');
    expect(() => normalizeScriptReferencePasses(['depth'])).not.toThrow();
    // 单 pass 时只列该 pass，不偷偷补其它张。
    const clayOnly = buildScriptShotReferencePack(ready, { passes: ['clay'] });
    expect(clayOnly.shots.flatMap(shot => shot.files.map(file => file.pass))).toEqual(['clay', 'clay']);
    // 来源变更由既有来源检查拦下；清单版本不认识也不假装能读。
    const changed = shotDraft(lines, [], { review: { ...draft.review, sourceFingerprint: 'fnv1a-deadbeef-33' } });
    expect(() => assertScriptShotPlanSource(ready, changed)).toThrow('失效');
    expect(() => scriptShotReferenceManifestText({ ...clayOnly, version: 2 } as never)).toThrow('版本');
  });

  it('binds one explicit per-shot walk target and rejects invalid targets, ranges and durations', () => {
    const plan = buildScriptShotPlan(shotDraft(lines));
    const long = updateScriptShotDuration(plan, 'shot-1', 2);
    const motion = { targetId: 'actor-1', targetName: '张三', start: { x: 0, z: 0 }, end: { x: 4, z: 2 }, duration: 1.5 };
    const moved = setScriptShotMotion(long, 'shot-1', motion);
    expect(moved.shots[0]!.motion).toEqual(motion);
    expect(long.shots[0]!.motion).toBeUndefined();
    expect(clearScriptShotMotion(moved, 'shot-1').shots[0]!.motion).toBeNull();
    expect(validateScriptShotMotion(motion, 2)).toEqual(motion);
    expect(() => setScriptShotMotion(plan, 'shot-9', motion)).toThrow('不在当前镜头表');
    expect(() => setScriptShotMotion(long, 'shot-1', { ...motion, duration: 9 })).toThrow('超出本镜 2 秒');
    expect(() => setScriptShotMotion(long, 'shot-1', { ...motion, duration: SCRIPT_MOTION_MIN_SECONDS / 2 })).toThrow('短于');
    expect(() => setScriptShotMotion(long, 'shot-1', { ...motion, end: { ...motion.start } })).toThrow('重合');
    expect(() => setScriptShotMotion(long, 'shot-1', { ...motion, start: { x: SCRIPT_MOTION_COORDINATE_LIMIT + 1, z: 0 } })).toThrow('超出');
    expect(() => setScriptShotMotion(long, 'shot-1', { ...motion, start: { x: Number.NaN, z: 0 } })).toThrow('有限坐标');
    expect(() => setScriptShotMotion(long, 'shot-1', { ...motion, targetId: '' })).toThrow('目标标识');
    expect(() => setScriptShotMotion(long, 'shot-1', { ...motion, targetName: '' })).toThrow('目标名称');
    expect(() => setScriptShotMotion(long, 'shot-1', { ...motion, animation: { index: -1, name: 'walk', duration: 1 } })).toThrow('索引');
    expect(() => setScriptShotMotion(long, 'shot-1', { ...motion, animation: { index: 0, name: 'walk', duration: 0 } })).toThrow('时长无效');
    // 旧镜头表没有 motion 字段仍可读；有了坏数据必须显式失败，不静默丢掉。
    const legacy = JSON.parse(JSON.stringify(long)); delete legacy.shots[0].motion;
    expect(validateScriptShotPlan(legacy).shots[0]!.motion).toBeUndefined();
    expect(() => validateScriptShotPlan({ ...long, shots: [{ ...long.shots[0], motion: { ...motion, duration: 9 } }, long.shots[1]] })).toThrow('超出本镜');
    // 缩短本镜时长不能把已绑定的走位截断或压缩。
    expect(() => updateScriptShotDuration(moved, 'shot-1', 1)).toThrow('走位');
  });

  it('samples the bound walk deterministically on a flat straight line and never borrows a neighbour shot', () => {
    const plan = updateScriptShotDuration(buildScriptShotPlan(shotDraft(lines)), 'shot-1', 2);
    const motion = {
      targetId: 'actor-1', targetName: '张三', start: { x: 0, z: 0 }, end: { x: 4, z: 2 }, duration: 2,
      animation: { index: 0, name: 'walk', duration: 1 },
    };
    const moved = setScriptShotMotion(plan, 'shot-1', motion);
    const start = sampleScriptShotMotion(moved, 0)!;
    expect(start).toMatchObject({ shotId: 'shot-1', targetId: 'actor-1', progress: 0, position: { x: 0, z: 0 }, animationSeconds: 0, finished: false });
    expect(start.yaw).toBeCloseTo(Math.atan2(4, 2), 12);
    const mid = sampleScriptShotMotion(moved, 1)!;
    expect(mid.progress).toBeCloseTo(0.5, 12);
    expect(mid.position).toEqual({ x: 2, z: 1 });
    // 骨骼动作按自己的 clip 时长取样，走位继续时冻结在 clip 末尾，不拖慢走位。
    expect(mid.animationSeconds).toBe(1);
    const end = sampleScriptShotMotionAt(moved.shots[0]!, 2)!;
    expect(end).toMatchObject({ progress: 1, position: { x: 4, z: 2 }, finished: true });
    // 镜次边界属于下一镜；下一镜没绑定走位就是 null，不借用上一镜。
    expect(sampleScriptShotMotion(moved, moved.shots[0]!.end)).toBeNull();
    expect(sampleScriptShotMotionAt(moved.shots[1]!, moved.shots[1]!.end)).toBeNull();
    expect(() => sampleScriptShotMotion(moved, -1)).toThrow('有限非负数');
    expect(() => sampleScriptShotMotion(moved, Number.NaN)).toThrow('有限非负数');
    // 播放采样把同一秒的机位、台词与走位放进同一个 sample，避免界面各算各的。
    const cameraA = { position: [2, 1.6, 4] as [number, number, number], target: [0, 1.2, 0] as [number, number, number], fov: 35 };
    const cameraB = { position: [-3, 2.2, 5] as [number, number, number], target: [0, 1.1, 0] as [number, number, number], fov: 50 };
    const ready = setScriptShotCamera(setScriptShotCamera(moved, 'shot-1', cameraA), 'shot-2', cameraB);
    expect(sampleScriptShotPlayback(ready, 1).motion).toMatchObject({ targetId: 'actor-1', position: { x: 2, z: 1 } });
    expect(sampleScriptShotPlayback(ready, 2.5).motion).toBeNull();
  });

  it('validates the bound target against the actual scene before playback and never guesses a replacement', () => {
    const plan = updateScriptShotDuration(buildScriptShotPlan(shotDraft(lines)), 'shot-1', 2);
    const motion = {
      targetId: 'actor-1', targetName: '张三', start: { x: 0, z: 0 }, end: { x: 4, z: 2 }, duration: 1.5,
      animation: { index: 0, name: 'walk', duration: 1 },
    };
    const moved = setScriptShotMotion(plan, 'shot-1', motion);
    const targets = [{ id: 'actor-1', name: '张三', visible: true, locked: false, animations: [{ index: 0, name: 'walk', duration: 1 }] }];
    expect(assertScriptShotMotionTargets(moved, targets)).toBe(moved);
    // 没有走位的旧计划不需要任何目标，不能被空的场景对象列表拦住。
    expect(assertScriptShotMotionTargets(plan, [])).toBe(plan);
    expect(() => assertScriptShotMotionTargets(moved, [])).toThrow('不存在');
    expect(() => assertScriptShotMotionTargets(moved, [{ ...targets[0]!, visible: false }])).toThrow('隐藏');
    expect(() => assertScriptShotMotionTargets(moved, [{ ...targets[0]!, locked: true }])).toThrow('锁定');
    expect(() => assertScriptShotMotionTargets(moved, [{ ...targets[0]!, animations: [{ index: 0, name: 'walk', duration: 1.2 }] }])).toThrow('已变化');
    expect(() => assertScriptShotMotionTargets(moved, [{ ...targets[0]!, animations: [] }])).toThrow('已变化');
  });
});