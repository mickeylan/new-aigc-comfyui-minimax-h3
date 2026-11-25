// Toonflow-app adapter; upstream MIT terms: ../THIRD_PARTY_NOTICES.md.
import { Box3, Group, MathUtils, Mesh, Vector3 } from 'three';
import { createSceneDocument, createSceneObject, type SceneAsset, type SceneDocument, type SceneObject } from './toonflow/sceneDocument';
import { buildSetTemplate, setTemplates } from './toonflow/setTemplates';
import { resolveAssetModel } from './toonflow/assetModelResolver';
import { createModelGroup } from './toonflow/modelGeometry';
import { builtinAssets, usesNativeScale } from './toonflow/assetLibrary';
import { getAssetPlacementScale } from './toonflow/assetMetadata';
import { measureCollisionBounds } from './toonflow/sceneCollision';
import { disposeObject, shareObjectGeometry } from './toonflow/sceneResources';
import { createToonflowPrimitive } from './toonflowPrimitives';

export type SetDraft = { version: 1; templateId: string; name: string; document: SceneDocument };
export const SET_DRAFT_VERSION = 1;
/** 单份布景草稿的对象上限：损坏或被篡改的草稿不得在物化时撑爆内存。 */
export const SET_DRAFT_MAX_OBJECTS = 4096;
export type SetAssetReference = { assetId: string; parentId?: string };
export type SetDraftReuseCounts = { instances: number; assets: number; perAsset: { assetId: string; instances: number }[] };

const isText = (value: unknown): value is string => typeof value === 'string' && value.trim() !== '';
const isPoint = (value: unknown) => {
  const point = value as { x?: unknown; y?: unknown; z?: unknown } | null;
  return !!point && typeof point === 'object' && (['x', 'y', 'z'] as const).every(axis => typeof point[axis] === 'number' && Number.isFinite(point[axis]));
};

/**
 * 物化前校验草稿。来源可能是存档文件或更早版本的插件，因此这里只信任结构，不信任内容：
 * 版本、模板、对象数量、稳定ID、有限变换和素材可重建来源都必须成立，否则显式报错，
 * 绝不悄悄丢弃对象或换成别的模型。
 */
export function validateSetDraft(value: unknown): SetDraft {
  const draft = value as Partial<SetDraft> | null | undefined;
  if (!draft || typeof draft !== 'object') throw new Error('布景草稿格式无效');
  if (draft.version !== SET_DRAFT_VERSION) throw new Error('不支持的布景配方版本');
  if (!isText(draft.templateId) || draft.templateId!.length > 160) throw new Error('布景草稿缺少模板标识');
  if (!setTemplates.some(template => template.id === draft.templateId)) throw new Error('布景草稿的模板不存在，请更新插件后重试');
  if (!isText(draft.name) || draft.name!.length > 160) throw new Error('布景草稿名称无效');
  const objects = (draft.document as { objects?: unknown } | undefined)?.objects;
  if (!Array.isArray(objects) || objects.length === 0) throw new Error('布景草稿没有可编辑对象');
  if (objects.length > SET_DRAFT_MAX_OBJECTS) throw new Error(`布景草稿对象超过 ${SET_DRAFT_MAX_OBJECTS} 个上限，请拆分场景`);
  const ids = new Set<string>();
  objects.forEach((raw, index) => {
    const label = `第 ${index + 1} 个对象`;
    const object = raw as Partial<SceneObject> | null;
    if (!object || typeof object !== 'object') throw new Error(`${label}格式无效`);
    if (!isText(object.id) || object.id!.length > 160) throw new Error(`${label}缺少稳定ID`);
    if (ids.has(object.id!)) throw new Error(`${label}的稳定ID重复：${object.id}`);
    ids.add(object.id!);
    if (typeof object.name !== 'string' || object.name.length > 160) throw new Error(`${label}名称无效`);
    if (!isPoint(object.position) || !isPoint(object.rotation) || !isPoint(object.scale)) throw new Error(`${label}的变换不是有限数`);
    if (typeof object.color !== 'string') throw new Error(`${label}颜色无效`);
    if (typeof object.visible !== 'boolean') throw new Error(`${label}显隐无效`);
    if (object.height !== undefined && !(typeof object.height === 'number' && Number.isFinite(object.height))) throw new Error(`${label}高度无效`);
    const asset = object.asset as Partial<SceneAsset> | null | undefined;
    if (!asset || typeof asset !== 'object' || !isText(asset.id)) throw new Error(`${label}缺少素材标识`);
    if (asset.kind === 'primitive') {
      // riggedPerson 等特殊形状仍由物化阶段给出专门的错误，这里只要求形状存在。
      if (!isText(asset.shape)) throw new Error(`${label}的基础形状缺失`);
    } else if (asset.kind === 'model') {
      if (!asset.model && !asset.recipe && !asset.blueprint && !isText(asset.url) && !isText(asset.path)) {
        throw new Error(`${label}的素材缺少可重建来源，不能只存变换`);
      }
    } else throw new Error(`${label}的素材类型无效`);
  });
  return draft as SetDraft;
}

/** 草稿里需要能重建的素材引用；衍生素材记录父ID，便于按父ID重新解析而不是按名字猜。 */
export function setDraftAssetReferences(draft: SetDraft): SetAssetReference[] {
  const seen = new Map<string, SetAssetReference>();
  for (const object of validateSetDraft(draft).document.objects) {
    const parentId = object.asset.variantOf ?? object.asset.recipe?.sourceId;
    const key = `${object.asset.id}\u0000${parentId ?? ''}`;
    if (!seen.has(key)) seen.set(key, parentId ? { assetId: object.asset.id, parentId } : { assetId: object.asset.id });
  }
  return [...seen.values()];
}

/** 真实实例/素材计数：实例数不等于模型数，衍生素材也不等于独立模型。 */
export function setDraftReuseCounts(draft: SetDraft): SetDraftReuseCounts {
  const objects = validateSetDraft(draft).document.objects;
  const counts = new Map<string, number>();
  for (const object of objects) counts.set(object.asset.id, (counts.get(object.asset.id) ?? 0) + 1);
  const perAsset = [...counts.entries()]
    .map(([assetId, instances]) => ({ assetId, instances }))
    .sort((a, b) => b.instances - a.instances || a.assetId.localeCompare(b.assetId));
  return { instances: objects.length, assets: perAsset.length, perAsset };
}

export function listSetTemplates() {
  return setTemplates.map(({ id, name, genre, half }) => ({ id, name, genre, width: half[0] * 2, depth: half[1] * 2 }));
}
export async function prepareSetDraft(templateId: string, signal: AbortSignal): Promise<SetDraft> {
  const template = setTemplates.find(value => value.id === templateId);
  if (!template) throw new Error('找不到布景模板，请重新选择');
  signal.throwIfAborted();
  const document = await buildSetTemplate(template, createSceneDocument(), signal);
  signal.throwIfAborted();
  return validateSetDraft({ version: SET_DRAFT_VERSION, templateId, name: template.name, document });
}

export function materializeSetObject(saved: SceneObject): Group {
  const asset = saved.asset;
  const definition = asset.model ?? resolveAssetModel(asset);
  if (!definition && asset.kind !== 'primitive') throw new Error(`素材 ${asset.name} 不支持本地构建`);
  const model = definition ? createModelGroup(definition, undefined, true) : createToonflowPrimitive(asset, saved.color);
  try {
    const content = new Group();
    if (definition) {
      // Same pivot and metre scale as the upstream layout footprint, NOT Studio's import normalization.
      model.updateMatrixWorld(true);
      const bounds = measureCollisionBounds(model, new Box3());
      if (bounds.isEmpty()) throw new Error(`素材 ${asset.name} 没有几何体`);
      const pivot = new Group(); pivot.add(model);
      const center = bounds.getCenter(new Vector3());
      if (asset.contentTransform) pivot.position.copy(asset.contentTransform.offset);
      else pivot.position.set(-center.x, -bounds.min.y, -center.z);
      content.add(pivot);
      content.scale.setScalar(asset.contentTransform?.scale ?? (usesNativeScale(asset) ? 1 : getAssetPlacementScale(asset, bounds.getSize(new Vector3()))));
    } else content.add(model);
    const object = new Group(); object.add(content); object.name = saved.name;
    object.animations = model.animations.map(clip => clip.clone());
    object.position.copy(saved.position);
    object.rotation.set(MathUtils.degToRad(saved.rotation.x), MathUtils.degToRad(saved.rotation.y), MathUtils.degToRad(saved.rotation.z));
    object.scale.copy(saved.scale); object.visible = saved.visible;
    object.updateMatrixWorld(true);
    if (saved.height !== undefined) object.position.y += saved.height - measureCollisionBounds(object, new Box3()).min.y;
    object.traverse(child => { if (child instanceof Mesh) { child.castShadow = true; child.receiveShadow = true; } });
    object.userData.proceduralSource = { generator: 'toonflow-set', version: 1, source: JSON.parse(JSON.stringify(saved)) };
    shareObjectGeometry(object, JSON.stringify(asset));
    return object;
  } catch (error) { disposeObject(model); throw error; }
}
export function materializeSetDraft(draft: SetDraft) {
  // 先整份校验再建几何：坏草稿不能一半进了视口、一半报错。
  const validated = validateSetDraft(draft);
  const result: { id: string; name: string; object: Group }[] = [];
  try {
    for (const saved of validated.document.objects) result.push({ id: saved.id, name: saved.name, object: materializeSetObject(saved) });
    return result;
  } catch (error) { result.forEach(entry => disposeObject(entry.object)); throw error; }
}

// ---------------------------------------------------------------------------
// 剧本选场：布景草稿的第二个来源（模板目录 / 剧本原文）。
// 只做确定性解析与模板选择：不调用大模型、不联网、不写场景、不按姓名猜素材。
// ---------------------------------------------------------------------------
export type ScriptTimeOfDay = 'day' | 'dawn' | 'dusk' | 'night' | 'unknown';
export type ScriptDialogueLine = { speaker: string; line: string };
export type ScriptScene = {
  /** 稳定、可重现的场次 ID（不含随机数、不含时间戳）。 */
  id: string;
  order: number;
  heading: string;
  interior: boolean | null;
  location: string;
  timeOfDay: ScriptTimeOfDay;
  characters: string[];
  dialogue: ScriptDialogueLine[];
  action: string[];
  /** 原始行号区间（1 基，含首尾），便于人工回查原文。 */
  lineStart: number;
  lineEnd: number;
};
export type ScriptSceneIndex = {
  scenes: ScriptScene[];
  diagnostics: string[];
  /** 是否真的解析到显式场次标记；false 时不把整篇剧本当成已分场。 */
  explicitHeadings: boolean;
  fingerprint: string;
};

const CN_NUMBER = '0-9一二三四五六七八九十百零〇';
const TIME_KEYWORDS: Record<string, ScriptTimeOfDay> = {
  清晨: 'dawn', 早晨: 'dawn', 凌晨: 'dawn', 黎明: 'dawn', 晨: 'dawn',
  黄昏: 'dusk', 傍晚: 'dusk', 夕阳: 'dusk', 落日: 'dusk', 暮: 'dusk',
  深夜: 'night', 午夜: 'night', 夜晚: 'night', 晚上: 'night', 夜: 'night', 晚: 'dusk',
  白天: 'day', 日间: 'day', 中午: 'day', 午后: 'day', 下午: 'day', 上午: 'day', 早上: 'day', 正午: 'day', 日: 'day', 昼: 'day',
  NIGHT: 'night', EVENING: 'dusk', DUSK: 'dusk', DAWN: 'dawn', MORNING: 'dawn', DAY: 'day', NOON: 'day',
};
const SCRIPT_SEPARATORS = /[\s\-—–·,，、:：()（）/|]+/;
const SPEAKER_WITH_NOTE = /^([^\s:：()（）]{1,24})\s*[（(][^）)]{0,60}[）)]\s*[:：]\s*(.*)$/;
const SPEAKER_ONLY = /^([^\s:：()（）]{1,24})\s*[:：]\s*(.*)$/;
const NOT_A_SPEAKER = /^(内景|外景|内外景|第|场景|注|旁白说明|字幕)/;

/** 行首标记：内景/外景、第N场、场景N、INT/EXT。可连续剥离，例如「1. 内景 客栈」。 */
function stripHeadingPrefix(line: string): { rest: string; interior?: boolean } | null {
  const text = line.trim();
  let match: RegExpMatchArray | null;
  if ((match = text.match(new RegExp(`^第\\s*[${CN_NUMBER}]+\\s*场\\s*(.*)$`)))) return { rest: match[1]!.trim() };
  if ((match = text.match(new RegExp(`^场\\s*景?\\s*[${CN_NUMBER}]+\\s*[.、:：]?\\s*(.*)$`)))) return { rest: match[1]!.trim() };
  if ((match = text.match(/^(内景|外景|内外景)\s*[.、:：]?\s*(.*)$/))) {
    return { rest: match[2]!.trim(), interior: match[1] === '内景' ? true : match[1] === '外景' ? false : undefined };
  }
  if ((match = text.match(/^(INT\.?\/EXT\.?|INT\.?|EXT\.?)\s+(.+)$/i))) {
    const marker = match[1]!.toUpperCase().replace(/\./g, '');
    return { rest: match[2]!.trim(), interior: marker === 'INT' ? true : marker === 'EXT' ? false : undefined };
  }
  if ((match = text.match(new RegExp(`^[${CN_NUMBER}]+\\s*[.、,)）]\\s*(.*)$`)))) {
    const rest = match[1]!.trim();
    // 只有剩下的内容看起来是场次行时才承认这是编号，避免把「1. 你好」这类台词当标题。
    // 中文没有 \b 词边界，这里用前缀判断加「末词元是时间词」的形态判断。
    if (!rest || /^(内景|外景|内外景)/.test(rest) || /^(INT|EXT)\b/i.test(rest)) return { rest };
    const tokens = rest.split(SCRIPT_SEPARATORS).filter(Boolean);
    if (tokens.length >= 2 && TIME_KEYWORDS[tokens[tokens.length - 1]!]) return { rest };
    return null;
  }
  return null;
}

function readHeading(line: string): { heading: string; interior: boolean | null; location: string; timeOfDay: ScriptTimeOfDay } | null {
  let interior: boolean | null = null;
  let rest = line.trim();
  let matched = false;
  for (let pass = 0; pass < 3; pass++) {
    const stripped = stripHeadingPrefix(rest);
    if (!stripped) break;
    matched = true;
    rest = stripped.rest;
    if (stripped.interior !== undefined) interior = stripped.interior;
  }
  if (!matched) return null;
  const tokens = rest.split(SCRIPT_SEPARATORS).filter(Boolean);
  let timeOfDay: ScriptTimeOfDay = 'unknown';
  for (let index = tokens.length - 1; index >= 0; index--) {
    // 只认完整词元，避免把「夜市」「夜店」这类地名里的字当成时间。
    const value = TIME_KEYWORDS[tokens[index]!];
    if (!value) continue;
    timeOfDay = value;
    tokens.splice(index, 1);
    break;
  }
  return { heading: line.trim(), interior, location: tokens.join(' '), timeOfDay };
}

function readDialogue(line: string): ScriptDialogueLine | null {
  const text = line.trim();
  const match = text.match(SPEAKER_WITH_NOTE) ?? text.match(SPEAKER_ONLY);
  if (!match) return null;
  const speaker = match[1]!.trim();
  if (!speaker || NOT_A_SPEAKER.test(speaker)) return null;
  // 台词按原文保留，不做改写、截断或规范化。
  return { speaker, line: match[2] ?? '' };
}

/** 变更检测用指纹：FNV-1a 32 位，不是加密哈希，只用于判断剧本是否被改过。 */
export function scriptFingerprint(text: string) {
  const normalized = text.replace(/\r\n?/g, '\n').replace(/[ \t]+$/gm, '').trim();
  let hash = 0x811c9dc5;
  for (let index = 0; index < normalized.length; index++) {
    hash ^= normalized.charCodeAt(index);
    hash = Math.imul(hash, 0x01000193) >>> 0;
  }
  return `fnv1a-${hash.toString(16).padStart(8, '0')}-${normalized.length}`;
}

/**
 * 解析剧本场次。逐行扫描，场次标记之外的原文只归入对白/动作，不改写。
 * 解析不到显式标记时只产出 1 个未标注场次并给出诊断，不假装已经分场，也不猜地点。
 */
export function parseScriptScenes(text: string): ScriptSceneIndex {
  const source = String(text ?? '').replace(/\r\n?/g, '\n');
  const lines = source.split('\n');
  const diagnostics: string[] = [];
  const scenes: ScriptScene[] = [];
  const preamble: string[] = [];
  const characterOrder = new Map<string, number>();

  lines.forEach((line, index) => {
    const trimmed = line.trim();
    const parsed = trimmed ? readHeading(line) : null;
    if (parsed) {
      scenes.push({
        id: `scene-${scenes.length + 1}`, order: scenes.length + 1, heading: parsed.heading,
        interior: parsed.interior, location: parsed.location, timeOfDay: parsed.timeOfDay,
        characters: [], dialogue: [], action: [], lineStart: index + 1, lineEnd: index + 1,
      });
      return;
    }
    if (!trimmed) return;
    if (scenes.length === 0) { preamble.push(trimmed); return; }
    const scene = scenes[scenes.length - 1]!;
    scene.lineEnd = index + 1;
    const dialogue = readDialogue(trimmed);
    if (dialogue) {
      scene.dialogue.push(dialogue);
      // Characters are scoped to this scene; a returning speaker must not disappear
      // just because they spoke in an earlier scene.
      if (!scene.characters.includes(dialogue.speaker)) scene.characters.push(dialogue.speaker);
      return;
    }
    scene.action.push(trimmed);
  });

  const explicitHeadings = scenes.length > 0;
  if (!explicitHeadings) {
    const scene: ScriptScene = {
      id: 'scene-1', order: 1, heading: '', interior: null, location: '', timeOfDay: 'unknown',
      characters: [], dialogue: [], action: [], lineStart: 1, lineEnd: Math.max(1, lines.length),
    };
    for (const line of preamble) {
      const dialogue = readDialogue(line);
      if (dialogue) {
        scene.dialogue.push(dialogue);
        if (!characterOrder.has(dialogue.speaker)) { characterOrder.set(dialogue.speaker, characterOrder.size); scene.characters.push(dialogue.speaker); }
      } else scene.action.push(line);
    }
    scenes.push(scene);
    diagnostics.push('剧本没有可识别的场次标记（如「1. 内景 客栈 夜」「第1场」「场景三：」「INT./EXT.」），未拆分场次');
  } else {
    if (preamble.length) diagnostics.push(`场次标记之前有 ${preamble.length} 行文字（标题/人物表/梗概），未并入任何场次`);
    for (const scene of scenes) {
      if (!scene.location) diagnostics.push(`第 ${scene.order} 场没有解析到地点，模板需人工选择`);
    }
  }
  return { scenes, diagnostics, explicitHeadings, fingerprint: scriptFingerprint(source) };
}

export type TemplateRecommendation = {
  templateId: string;
  templateName: string;
  matchedBy: 'location-keyword' | 'template-name' | 'manual';
  matchedKeyword: string;
  /** 内景/外景与模板类型是否冲突；有值时必须在界面上让用户确认。 */
  conflicts: string[];
};

// 只写真实存在于 setTemplates 的 ID；命中后仍会与目录核对，缺模板不做替换。
const LOCATION_RULES: { keywords: string[]; templateId: string }[] = [
  { keywords: ['夜市'], templateId: 'nightMarket' },
  { keywords: ['审讯', '问讯'], templateId: 'interrogation' },
  { keywords: ['法庭', '庭审', '审判', '公审'], templateId: 'courtroom' },
  { keywords: ['客栈', '酒楼', '饭庄', '驿站', '茶楼', '茶馆', '酒肆'], templateId: 'inn' },
  { keywords: ['四合院', '宅院', '府邸', '院落', '院子', '衙门', '大院'], templateId: 'courtyard' },
  { keywords: ['军营', '大营', '校场', '营帐', '战场', '帅帐'], templateId: 'camp' },
  { keywords: ['王座', '大殿', '金銮', '朝堂', '宫廷'], templateId: 'throneRoom' },
  { keywords: ['古街', '街市', '集市', '集镇', '坊市', '古镇', '市集', '老街'], templateId: 'ancientStreet' },
  { keywords: ['办公室', '办公区', '工位', '公司', '写字楼'], templateId: 'office' },
  { keywords: ['报告厅', '阶梯教室', '礼堂', '讲座'], templateId: 'lectureHall' },
  { keywords: ['教室', '课室', '课堂'], templateId: 'classroom' },
  { keywords: ['卧室', '卧房', '寝室', '闺房'], templateId: 'bedroom' },
  { keywords: ['客厅', '起居室', '会客室'], templateId: 'livingRoom' },
  { keywords: ['咖啡', '酒吧', '餐厅', '饭馆', '餐吧', '茶馆店'], templateId: 'cafe' },
  { keywords: ['街道', '马路', '路口', '十字', '大街'], templateId: 'modernStreet' },
];
const OUTDOOR_TEMPLATES = new Set(['ancientStreet', 'nightMarket', 'modernStreet', 'camp']);

/** 依据地点关键词挑模板；挑不到就返回 null，绝不退回到默认模板。 */
export function recommendSetTemplate(scene: Pick<ScriptScene, 'location' | 'interior'>): TemplateRecommendation | null {
  const location = scene.location ?? '';
  for (const rule of LOCATION_RULES) {
    const keyword = rule.keywords.find(value => location.includes(value));
    if (!keyword) continue;
    const template = setTemplates.find(value => value.id === rule.templateId);
    if (!template) continue;
    const conflicts: string[] = [];
    if (scene.interior === true && OUTDOOR_TEMPLATES.has(template.id)) conflicts.push(`剧本标为内景，候选模板「${template.name}」是室外布景`);
    if (scene.interior === false && !OUTDOOR_TEMPLATES.has(template.id)) conflicts.push(`剧本标为外景，候选模板「${template.name}」是室内布景`);
    return { templateId: template.id, templateName: template.name, matchedBy: 'location-keyword', matchedKeyword: keyword, conflicts };
  }
  const tokens = location.split(SCRIPT_SEPARATORS).filter(Boolean);
  for (const template of setTemplates) {
    if (!tokens.includes(template.name)) continue;
    return { templateId: template.id, templateName: template.name, matchedBy: 'template-name', matchedKeyword: template.name, conflicts: [] };
  }
  return null;
}

export type ScriptSceneDraft = {
  version: 1;
  /** 草稿必须由人工确认后才会进入场景；界面不得自动应用。 */
  requiresConfirmation: true;
  sceneId: string;
  templateId: string;
  templateName: string;
  recommendation: TemplateRecommendation;
  setDraft: SetDraft;
  review: {
    heading: string;
    location: string;
    interior: boolean | null;
    timeOfDay: ScriptTimeOfDay;
    characters: string[];
    dialogue: ScriptDialogueLine[];
    action: string[];
    instanceCount: number;
    diagnostics: string[];
    conflicts: string[];
    sourceFingerprint: string;
    sceneFingerprint: string;
    /** 仅记录人工确认过的角色→布景人物/占位人绑定；未绑定时不写。 */
    bindings?: ScriptCharacterPlacement[];
  };
};

/** 显式取某一场；不给 ID、ID 不存在或剧本未分场都要报错，不默认取第一场。 */
export function selectScriptScene(index: ScriptSceneIndex, sceneId: string): ScriptScene {
  const available = index.scenes.map(scene => `${scene.id}（${scene.heading || '未标注'}）`).join('、');
  if (!sceneId) throw new Error(`请先显式选择要搭的场次：${available || '剧本没有场次'}`);
  const scene = index.scenes.find(value => value.id === sceneId);
  if (!scene) throw new Error(`所选场次 ${sceneId} 不在本次剧本中，请重新选择：${available}`);
  return scene;
}

/**
 * 生成可审阅的确定性草稿：选场 → 挑真实模板 → 复用既有布景构建与校验。
 * 不做角色↔素材的语义推断，不改写台词，不自动应用，也不新增模型来源。
 */
export async function buildScriptSceneDraft(index: ScriptSceneIndex, sceneId: string, signal: AbortSignal, manualTemplateId?: string): Promise<ScriptSceneDraft> {
  if (!index.explicitHeadings) throw new Error('剧本没有可识别的场次标记，无法确定场景地点；请先补「1. 内景 客栈 夜」这类场次行');
  const scene = selectScriptScene(index, sceneId);
  signal.throwIfAborted();
  let recommendation = recommendSetTemplate(scene);
  if (manualTemplateId !== undefined) {
    const template = setTemplates.find(value => value.id === manualTemplateId);
    if (!template) throw new Error('人工选择的布景模板不存在，请重新选择');
    const outdoor = OUTDOOR_TEMPLATES.has(template.id);
    const conflicts = scene.interior === true && outdoor ? [`剧本标为内景，人工模板「${template.name}」是室外布景`] : scene.interior === false && !outdoor ? [`剧本标为外景，人工模板「${template.name}」请核对是否适合室外`] : [];
    recommendation = { templateId: template.id, templateName: template.name, matchedBy: 'manual', matchedKeyword: '', conflicts };
  }
  if (!recommendation) throw new Error(`第 ${scene.order} 场「${scene.location || '未标注地点'}」没有匹配的内置布景模板，请人工选择模板或手动搭建`);
  signal.throwIfAborted();
  const setDraft = await prepareSetDraft(recommendation.templateId, signal);
  signal.throwIfAborted();
  return {
    version: 1,
    requiresConfirmation: true,
    sceneId: scene.id,
    templateId: recommendation.templateId,
    templateName: recommendation.templateName,
    recommendation,
    setDraft,
    review: {
      heading: scene.heading,
      location: scene.location,
      interior: scene.interior,
      timeOfDay: scene.timeOfDay,
      characters: [...scene.characters],
      dialogue: scene.dialogue.map(line => ({ ...line })),
      action: [...scene.action],
      instanceCount: setDraft.document.objects.length,
      diagnostics: [...index.diagnostics],
      conflicts: [...recommendation.conflicts],
      sourceFingerprint: index.fingerprint,
      sceneFingerprint: scriptFingerprint(`${scene.heading}\n${scene.action.join('\n')}\n${scene.dialogue.map(line => `${line.speaker}:${line.line}`).join('\n')}`),
    },
  };
}

// ---------------------------------------------------------------------------
// 剧本角色绑定：把已解析的出场角色显式绑到布景里已有的人物，或新建中性占位人并给定站位。
// 只改这份布景草稿（复用 SetDraft 与 v7 存档），不推断长相、不换素材、不写提示词、不新增应用通道。
// ---------------------------------------------------------------------------
export const SCRIPT_CHARACTER_LIMIT = 64;
export const STAGING_LIMIT = 1000;
/** 布景里可被剧本角色复用、且能由本地基础几何物化的人物形状。 */
const PLACEABLE_PERSON_SHAPES = new Set(['person', 'seatedPerson', 'kneelingPerson', 'riggedPerson']);
export type SetPersonSummary = { id: string; name: string; shape: string; position: { x: number; y: number; z: number } };
export type ScriptCharacterTarget =
  | { kind: 'set-person'; objectId: string }
  | { kind: 'placeholder'; position: { x: number; z: number }; rig?: 'skeletal' };
export type ScriptCharacterBinding = { character: string; target: ScriptCharacterTarget };
export type ScriptCharacterPlacement = {
  character: string;
  kind: 'set-person' | 'placeholder';
  objectId: string;
  /** 布景人物记原人物名；占位人记真实素材名（内置胶囊人），不冒充最终角色模型。 */
  sourceName: string;
  position: { x: number; y: number; z: number };
};

/** 列出布景草稿里真实存在、可由本地几何建出的人物；顺序即文档顺序，不做语义猜测。 */
export function listSetPersonObjects(draft: SetDraft): SetPersonSummary[] {
  return validateSetDraft(draft).document.objects
    .filter(object => object.asset.kind === 'primitive' && PLACEABLE_PERSON_SHAPES.has(object.asset.shape ?? ''))
    .map(object => ({ id: object.id, name: object.name, shape: object.asset.shape!, position: { ...object.position } }));
}

function isStagingPoint(value: unknown): value is { x: number; z: number } {
  const point = value as { x?: unknown; z?: unknown } | null | undefined;
  if (!point || typeof point !== 'object') return false;
  const { x, z } = point;
  return typeof x === 'number' && typeof z === 'number' && Number.isFinite(x) && Number.isFinite(z) && Math.max(Math.abs(x), Math.abs(z)) <= STAGING_LIMIT;
}

/**
 * 把人工给定的角色绑定落到布景草稿上：绑定已有布景人物时重命名该实例，绑定中性占位人时追加一个
 * 内置胶囊人。必须覆盖本场每个出场角色且一一对应；缺项、重复、指向非人物、站位越界或重合都显式报错，
 * 不做默认分配。返回新草稿，输入草稿不被修改。
 */
export function applyScriptCharacterBindings(draft: ScriptSceneDraft, bindings: readonly ScriptCharacterBinding[]): ScriptSceneDraft {
  const characters = draft.review.characters;
  if (characters.length > SCRIPT_CHARACTER_LIMIT) throw new Error(`本场出场角色超过 ${SCRIPT_CHARACTER_LIMIT} 人，请先拆分场次`);
  if (characters.length === 0) {
    if (bindings.length) throw new Error('本场没有检出出场角色，不能绑定不存在的角色');
    return draft;
  }
  if (bindings.length !== characters.length) throw new Error(`请为每个出场角色显式选择布景人物或中性占位人：${characters.join('、')}`);
  const base = validateSetDraft(JSON.parse(JSON.stringify(draft.setDraft)));
  const byId = new Map(base.document.objects.map(object => [object.id, object]));
  const seenCharacters = new Set<string>();
  const usedObjects = new Set<string>();
  const usedStanding = new Set<string>();
  const placements: ScriptCharacterPlacement[] = [];
  const additions: SceneObject[] = [];
  const claimStanding = (character: string, position: { x: number; z: number }) => {
    // 只用XZ判重合：两人站在同一格会让“站位”变成一句空话。
    const key = `${position.x.toFixed(2)}:${position.z.toFixed(2)}`;
    if (usedStanding.has(key)) throw new Error(`角色「${character}」的站位与另一个角色重合，请调整站位`);
    usedStanding.add(key);
  };
  for (const binding of bindings) {
    const character = binding?.character;
    if (typeof character !== 'string' || !characters.includes(character)) throw new Error(`角色「${typeof character === 'string' ? character : ''}」不在本场出场名单中`);
    if (seenCharacters.has(character)) throw new Error(`角色「${character}」重复绑定`);
    seenCharacters.add(character);
    const target = binding.target;
    if (!target || typeof target !== 'object') throw new Error(`角色「${character}」没有选择布景人物或中性占位人`);
    if (target.kind === 'set-person') {
      const object = typeof target.objectId === 'string' ? byId.get(target.objectId) : undefined;
      if (!object) throw new Error(`角色「${character}」要绑定的布景人物不存在，请重新生成草稿`);
      if (!(object.asset.kind === 'primitive' && PLACEABLE_PERSON_SHAPES.has(object.asset.shape ?? ''))) throw new Error(`角色「${character}」要绑定的对象不是可摆放人物`);
      if (usedObjects.has(object.id)) throw new Error(`角色「${character}」与另一个角色绑定了同一个布景人物，请分开`);
      claimStanding(character, object.position);
      usedObjects.add(object.id);
      const sourceName = object.name;
      object.name = character;
      placements.push({ character, kind: 'set-person', objectId: object.id, sourceName, position: { ...object.position } });
      continue;
    }
    if (target.kind !== 'placeholder') throw new Error(`角色「${character}」的绑定类型无效`);
    if (!isStagingPoint(target.position)) throw new Error(`角色「${character}」的站位须为 ±${STAGING_LIMIT} 米内的有限坐标`);
    claimStanding(character, target.position);
    if (target.rig !== undefined && target.rig !== 'skeletal') throw new Error('不支持的占位人物骨架');
    const asset = builtinAssets.find(item => item.id === (target.rig === 'skeletal' ? 'riggedPerson' : 'person'));
    if (!asset) throw new Error('内置人物素材缺失，无法新建中性占位人');
    const object = createSceneObject(JSON.parse(JSON.stringify(asset)), { x: target.position.x, y: 0, z: target.position.z });
    object.name = character;
    if (byId.has(object.id)) throw new Error(`角色「${character}」的占位人身份与已有对象冲突，请重新生成草稿`);
    additions.push(object);
    byId.set(object.id, object);
    usedObjects.add(object.id);
    placements.push({ character, kind: 'placeholder', objectId: object.id, sourceName: asset.name, position: { ...object.position } });
  }
  const document: SceneDocument = { ...base.document, objects: [...base.document.objects, ...additions] };
  const setDraft = validateSetDraft({ ...base, document });
  return {
    ...draft,
    setDraft,
    review: { ...draft.review, instanceCount: setDraft.document.objects.length, bindings: placements },
  };
}

// Compatibility exports; runtime shot consumers import scriptShotPlan directly.
export * from './scriptShotPlan';
