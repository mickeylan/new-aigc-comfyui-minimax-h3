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
import { estimateChineseDialogueDuration, SPEAKER_SWITCH_SECONDS } from '../../utils/dialogueDuration.js';
import { validateCharacterMotionSettings, type CharacterMotionSettings } from './characterMotion';
import type { ShotNavigation } from './shotNavigation';

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

// ---------------------------------------------------------------------------
// 剧本镜头与台词时间轴：把已审阅的场次草稿拆成可复核、可改时长的镜头表。
// 只做确定性拆分与时间校验：不猜机位、不推断表演、不改台词、不联网、不调用模型。
// 机位由用户在现有视口里显式记录；镜头表随现有场景存档保存，旧存档没有该字段仍可载入。
// ---------------------------------------------------------------------------
export const SCRIPT_SHOT_LIMIT = 64;
export const SCRIPT_TIMELINE_MAX_SECONDS = 86400;
export const SCRIPT_SHOT_MIN_SECONDS = 0.2;
const SCRIPT_CAMERA_COORDINATE_LIMIT = 10000;
const SCRIPT_CAMERA_FOV_MIN = 15;
const SCRIPT_CAMERA_FOV_MAX = 90;
const SHOT_TIME_TOLERANCE = 1e-6;
/** 单镜走位的最短秒数：再短只会是瞬移，不能被当成走位。 */
export const SCRIPT_MOTION_MIN_SECONDS = 0.1;
/** 走位坐标与布景站位共用同一范围，避免两套坐标口径。 */
export const SCRIPT_MOTION_COORDINATE_LIMIT = 1000;
const SCRIPT_MOTION_MIN_DISTANCE = 1e-3;
export type ScriptShotCamera = { position: [number, number, number]; target: [number, number, number]; fov: number };
export type ScriptTimedLine = { index: number; speaker: string; line: string; start: number; end: number };
/** 人工选定的已有骨骼 clip：按索引+名称+时长严格匹配，不按文件名猜动作语义。 */
export type ScriptShotMotionAnimation = { index: number; name: string; duration: number };
/** 一镜内一个角色的 XZ 直线走位；Y 由采样器跟随地形（无地形时保留当前高度），不可走坡度/水域或静态障碍显式拒绝，不自动绕行。 */
export type ScriptShotMotion = {
  targetId: string;
  targetName: string;
  start: { x: number; z: number };
  end: { x: number; z: number };
  /** 走完全程的秒数，必须 ≤ 本镜时长；走完后停在终点。 */
  duration: number;
  animation?: ScriptShotMotionAnimation | null;
  /** Explicit opt-in; missing fields retain legacy line-reject / single-clip behavior. */
  navigation?: ShotNavigation;
  characterMotion?: CharacterMotionSettings;
};
export type ScriptShotMotionTarget = {
  id: string;
  name: string;
  visible: boolean;
  locked: boolean;
  animations: ScriptShotMotionAnimation[];
};
export type ScriptShot = {
  id: string;
  order: number;
  start: number;
  end: number;
  /** 未指定机位时保留 null，不借用其它镜头或默认机位。 */
  camera: ScriptShotCamera | null;
  /** 人工绑定的一镜走位；旧镜头表没有该字段，按未绑定处理。 */
  motion?: ScriptShotMotion | null;
  dialogue: ScriptTimedLine[];
};
export type ScriptShotPlan = {
  version: 1;
  sceneId: string;
  heading: string;
  location: string;
  /** 生成时的剧本原文指纹；原文改了就失效，不自动沿用旧镜头表。 */
  sourceFingerprint: string;
  sceneFingerprint: string;
  /** 动作行按原文保留；它们不能与台词逐句对应，因此统一挂在计划上，不冒充某镜的时长。 */
  action: string[];
  shots: ScriptShot[];
  diagnostics: string[];
};

const shotTime = (value: number) => Math.round(value * 100) / 100;
const shotText = (value: unknown, label: string, max: number) => {
  if (typeof value !== 'string' || value.length > max) throw new Error(`${label}无效`);
  return value;
};
const shotVector = (value: unknown, label: string, limit: number): [number, number, number] => {
  if (!Array.isArray(value) || value.length !== 3 || value.some(item => typeof item !== 'number' || !Number.isFinite(item))) throw new Error(`${label}不是有限坐标`);
  const point = value as number[];
  if (point.some(item => Math.abs(item) > limit)) throw new Error(`${label}超出 ±${limit} 米`);
  return [point[0]!, point[1]!, point[2]!];
};

/** 校验一份镜头表结构；版本、时间、连续性、台词顺序或机位不成立都显式报错，不做静默修补。 */
export function validateScriptShotPlan(value: unknown): ScriptShotPlan {
  const plan = value as Partial<ScriptShotPlan> | null | undefined;
  if (!plan || typeof plan !== 'object') throw new Error('镜头表格式无效');
  if (plan.version !== 1) throw new Error('不支持的镜头计划版本');
  const sceneId = shotText(plan.sceneId, '镜头表的场次标识', 160);
  if (!sceneId.trim()) throw new Error('镜头表缺少场次标识');
  shotText(plan.heading, '镜头表的场次标题', 500);
  shotText(plan.location, '镜头表的地点', 500);
  if (!shotText(plan.sourceFingerprint, '镜头表的剧本来源', 200).trim()) throw new Error('镜头表缺少剧本来源指纹');
  if (!shotText(plan.sceneFingerprint, '镜头表的场次指纹', 200).trim()) throw new Error('镜头表缺少场次指纹');
  if (!Array.isArray(plan.action) || plan.action.some(item => typeof item !== 'string')) throw new Error('镜头表的动作行无效');
  if (!Array.isArray(plan.diagnostics) || plan.diagnostics.some(item => typeof item !== 'string')) throw new Error('镜头表的诊断信息无效');
  if (!Array.isArray(plan.shots) || plan.shots.length === 0) throw new Error('镜头表没有镜头');
  if (plan.shots.length > SCRIPT_SHOT_LIMIT) throw new Error(`镜头数超过 ${SCRIPT_SHOT_LIMIT} 个上限，请拆分场次`);
  const ids = new Set<string>();
  let previousEnd = 0;
  let previousLine = -1;
  plan.shots.forEach((raw, index) => {
    const label = `第 ${index + 1} 镜`;
    const shot = raw as Partial<ScriptShot> | null | undefined;
    if (!shot || typeof shot !== 'object') throw new Error(`${label}格式无效`);
    const id = shotText(shot.id, `${label}的标识`, 160);
    if (!id.trim()) throw new Error(`${label}缺少标识`);
    if (ids.has(id)) throw new Error(`${label}的标识重复`);
    ids.add(id);
    if (shot.order !== index + 1) throw new Error(`${label}的序号不连续`);
    const start = shot.start; const end = shot.end;
    if (typeof start !== 'number' || !Number.isFinite(start) || typeof end !== 'number' || !Number.isFinite(end)) throw new Error(`${label}的时间不是有限数`);
    if (start < -SHOT_TIME_TOLERANCE) throw new Error(`${label}的开始时间不能为负`);
    if (end - start < SCRIPT_SHOT_MIN_SECONDS - SHOT_TIME_TOLERANCE) throw new Error(`${label}短于 ${SCRIPT_SHOT_MIN_SECONDS} 秒`);
    if (end > SCRIPT_TIMELINE_MAX_SECONDS + SHOT_TIME_TOLERANCE) throw new Error(`${label}超过 ${SCRIPT_TIMELINE_MAX_SECONDS} 秒上限`);
    if (Math.abs(start - previousEnd) > SHOT_TIME_TOLERANCE) throw new Error(`${label}与上一镜不连续，请按时间顺序排列且不留空档`);
    previousEnd = end;
    if (!Array.isArray(shot.dialogue)) throw new Error(`${label}的台词列表无效`);
    shot.dialogue.forEach(rawLine => {
      const line = rawLine as Partial<ScriptTimedLine> | null | undefined;
      if (!line || typeof line !== 'object') throw new Error(`${label}的台词格式无效`);
      if (!Number.isInteger(line.index) || (line.index as number) <= previousLine) throw new Error(`${label}的台词顺序无效`);
      previousLine = line.index as number;
      if (typeof line.speaker !== 'string' || !line.speaker.trim() || line.speaker.length > 64) throw new Error(`${label}的说话人无效`);
      if (typeof line.line !== 'string' || line.line.length > 20000) throw new Error(`${label}的台词原文无效`);
      if (typeof line.start !== 'number' || !Number.isFinite(line.start) || typeof line.end !== 'number' || !Number.isFinite(line.end)) throw new Error(`${label}的台词时间不是有限数`);
      if (line.start < start - SHOT_TIME_TOLERANCE || line.end < line.start - SHOT_TIME_TOLERANCE || line.end > end + SHOT_TIME_TOLERANCE) throw new Error(`${label}的台词时间超出镜头范围`);
    });
    if (shot.camera === undefined) throw new Error(`${label}缺少机位字段（未指定时应为 null）`);
    if (shot.camera !== null) validateScriptShotCamera(shot.camera);
    // 走位是可选字段：旧档没有就保持没有；有但不成立必须显式失败，不静默忽略。
    if (shot.motion !== undefined && shot.motion !== null) validateScriptShotMotion(shot.motion, end - start, `${label}的走位`);
  });
  return plan as ScriptShotPlan;
}

const motionPoint = (value: unknown, label: string) => {
  const point = value as { x?: unknown; z?: unknown } | null | undefined;
  if (!point || typeof point !== 'object' || typeof point.x !== 'number' || typeof point.z !== 'number' || !Number.isFinite(point.x) || !Number.isFinite(point.z)) throw new Error(`${label}不是有限坐标`);
  if (Math.abs(point.x) > SCRIPT_MOTION_COORDINATE_LIMIT || Math.abs(point.z) > SCRIPT_MOTION_COORDINATE_LIMIT) throw new Error(`${label}超出 ±${SCRIPT_MOTION_COORDINATE_LIMIT} 米`);
  return { x: point.x, z: point.z };
};

/** 校验人工走位：目标 ID、起止 XZ、时长与可选 clip 引用都必须成立，不做默认补全。 */
export function validateScriptShotMotion(value: unknown, shotDuration: number, label = '走位'): ScriptShotMotion {
  const motion = value as Partial<ScriptShotMotion> | null | undefined;
  if (!motion || typeof motion !== 'object') throw new Error(`${label}格式无效`);
  const targetId = shotText(motion.targetId, `${label}的目标标识`, 160);
  if (!targetId.trim()) throw new Error(`${label}缺少目标标识`);
  const targetName = shotText(motion.targetName, `${label}的目标名称`, 160);
  if (!targetName.trim()) throw new Error(`${label}缺少目标名称`);
  const start = motionPoint(motion.start, `${label}的起点`);
  const end = motionPoint(motion.end, `${label}的终点`);
  if (Math.hypot(end.x - start.x, end.z - start.z) < SCRIPT_MOTION_MIN_DISTANCE) throw new Error(`${label}的起点与终点重合`);
  if (typeof motion.duration !== 'number' || !Number.isFinite(motion.duration)) throw new Error(`${label}的时长不是有限数`);
  const duration = shotTime(motion.duration);
  if (duration < SCRIPT_MOTION_MIN_SECONDS - SHOT_TIME_TOLERANCE) throw new Error(`${label}短于 ${SCRIPT_MOTION_MIN_SECONDS} 秒`);
  if (duration > shotDuration + SHOT_TIME_TOLERANCE) throw new Error(`${label}的时长超出本镜 ${shotTime(shotDuration)} 秒`);
  let animation: ScriptShotMotionAnimation | null = null;
  if (motion.animation !== undefined && motion.animation !== null) {
    const clip = motion.animation as Partial<ScriptShotMotionAnimation> | null;
    if (!clip || typeof clip !== 'object') throw new Error(`${label}的骨骼动作格式无效`);
    if (!Number.isSafeInteger(clip.index) || (clip.index as number) < 0) throw new Error(`${label}的骨骼动作索引无效`);
    const name = shotText(clip.name, `${label}的骨骼动作名称`, 200);
    if (!name.trim()) throw new Error(`${label}缺少骨骼动作名称`);
    if (typeof clip.duration !== 'number' || !Number.isFinite(clip.duration) || clip.duration <= 0) throw new Error(`${label}的骨骼动作时长无效`);
    animation = { index: clip.index as number, name, duration: clip.duration };
  }
  const characterMotion = motion.characterMotion === undefined ? undefined : validateCharacterMotionSettings(motion.characterMotion);
  const navigation = motion.navigation;
  if (navigation !== undefined && (!navigation || navigation.version !== 1 || navigation.mode !== 'avoid' ||
    Object.keys(navigation).some(key => !['version', 'mode', 'radius', 'speed'].includes(key)) ||
    typeof navigation.radius !== 'number' || !Number.isFinite(navigation.radius) || navigation.radius <= 0 || navigation.radius > 10 ||
    typeof navigation.speed !== 'number' || !Number.isFinite(navigation.speed) || navigation.speed <= 0 || navigation.speed > 100)) throw Error(`${label}的避障配置无效`);
  return { targetId, targetName, start, end, duration, ...(animation ? { animation } : {}),
    ...(characterMotion ? { characterMotion } : {}), ...(navigation ? { navigation: { ...navigation } } : {}) };
}

/** 人工绑定某一镜的走位；只改计划数据，不移动对象、不生成表演。 */
export function setScriptShotMotion(plan: ScriptShotPlan, shotId: string, motion: ScriptShotMotion): ScriptShotPlan {
  const base = cloneShotPlan(validateScriptShotPlan(plan));
  const index = shotAt(base, shotId);
  const shot = base.shots[index]!;
  base.shots[index] = { ...shot, motion: validateScriptShotMotion(motion, shot.end - shot.start, `第 ${shot.order} 镜的走位`) };
  return validateScriptShotPlan(base);
}

/** 明确清空某一镜的走位；清空后保持未绑定，不借用相邻镜头。 */
export function clearScriptShotMotion(plan: ScriptShotPlan, shotId: string): ScriptShotPlan {
  const base = cloneShotPlan(validateScriptShotPlan(plan));
  const index = shotAt(base, shotId);
  base.shots[index] = { ...base.shots[index]!, motion: null };
  return validateScriptShotPlan(base);
}

/** 机位只接受视口真实记录的取景：有限坐标、不与注视点重合、视角在 15–90 度之间。 */
export function validateScriptShotCamera(value: unknown): ScriptShotCamera {
  const camera = value as Partial<ScriptShotCamera> | null | undefined;
  if (!camera || typeof camera !== 'object') throw new Error('镜头机位格式无效');
  const position = shotVector(camera.position, '镜头机位', SCRIPT_CAMERA_COORDINATE_LIMIT);
  const target = shotVector(camera.target, '镜头注视点', SCRIPT_CAMERA_COORDINATE_LIMIT);
  if (Math.hypot(position[0] - target[0], position[1] - target[1], position[2] - target[2]) <= SHOT_TIME_TOLERANCE) throw new Error('镜头机位与注视点不能重合');
  if (typeof camera.fov !== 'number' || !Number.isFinite(camera.fov) || camera.fov < SCRIPT_CAMERA_FOV_MIN || camera.fov > SCRIPT_CAMERA_FOV_MAX) throw new Error(`镜头视角须在 ${SCRIPT_CAMERA_FOV_MIN}–${SCRIPT_CAMERA_FOV_MAX} 度之间`);
  return { position, target, fov: camera.fov };
}

/**
 * 生成确定性镜头表：每句台词一镜，按原文顺序连续排时间。
 * 时长复用既有台词估算（每秒 9 字、标点停顿另计、换说话人加停顿），可逐镜改写；
 * 机位一律留空由人工记录；没有台词、超过 64 镜或超出总时长上限都显式报错，不截断。
 */
export function buildScriptShotPlan(draft: ScriptSceneDraft): ScriptShotPlan {
  const dialogue = draft.review.dialogue;
  if (!Array.isArray(dialogue) || dialogue.length === 0) throw new Error('本场没有台词，无法从剧本确定镜头时长；请先写台词，或手工搭建镜头');
  if (dialogue.length > SCRIPT_SHOT_LIMIT) throw new Error(`本场 ${dialogue.length} 句台词超过 ${SCRIPT_SHOT_LIMIT} 个镜头上限，请先拆分场次`);
  const shots: ScriptShot[] = [];
  let cursor = 0;
  let previousSpeaker = '';
  dialogue.forEach((line, index) => {
    const speech = shotTime(estimateChineseDialogueDuration(line.line));
    const switchPause = index > 0 && line.speaker !== previousSpeaker ? SPEAKER_SWITCH_SECONDS : 0;
    const seconds = Math.max(SCRIPT_SHOT_MIN_SECONDS, shotTime(speech + switchPause));
    const start = shotTime(cursor);
    const end = shotTime(start + seconds);
    shots.push({
      id: `shot-${index + 1}`,
      order: index + 1,
      start,
      end,
      camera: null,
      // 台词与说话人按原文保留；开始时间就是本镜开始，结束时间按估算，不做改写。
      dialogue: [{ index, speaker: line.speaker, line: line.line, start, end: shotTime(start + speech) }],
    });
    cursor = end;
    previousSpeaker = line.speaker;
  });
  if (cursor > SCRIPT_TIMELINE_MAX_SECONDS + SHOT_TIME_TOLERANCE) throw new Error(`镜头表总时长 ${cursor} 秒超过 ${SCRIPT_TIMELINE_MAX_SECONDS} 秒上限，请拆分场次`);
  const action = [...draft.review.action];
  const diagnostics = [
    ...draft.review.diagnostics,
    ...draft.review.conflicts,
    '镜头时长按原文汉字数估算（每秒 9 字，标点停顿另计），可逐镜改写；不会自动补机位。',
  ];
  if (action.length) diagnostics.push(`${action.length} 行动作只作画面说明，未参与自动计时，也不推断表演。`);
  return validateScriptShotPlan({
    version: 1,
    sceneId: draft.sceneId,
    heading: draft.review.heading,
    location: draft.review.location,
    sourceFingerprint: draft.review.sourceFingerprint,
    sceneFingerprint: draft.review.sceneFingerprint,
    action,
    shots,
    diagnostics,
  });
}

const cloneShotPlan = (plan: ScriptShotPlan) => JSON.parse(JSON.stringify(plan)) as ScriptShotPlan;
function shotAt(plan: ScriptShotPlan, shotId: string) {
  const index = plan.shots.findIndex(shot => shot.id === shotId);
  if (index < 0) throw new Error(`镜头 ${shotId} 不在当前镜头表中`);
  return index;
}

/**
 * 改写某一镜时长：后续镜头整体顺延，台词只随镜头平移，不缩放、不截断。
 * 新时长装不下本镜台词、不是有限数或会让总时长越界都显式报错。
 */
export function updateScriptShotDuration(plan: ScriptShotPlan, shotId: string, seconds: number): ScriptShotPlan {
  const base = cloneShotPlan(validateScriptShotPlan(plan));
  const index = shotAt(base, shotId);
  if (typeof seconds !== 'number' || !Number.isFinite(seconds)) throw new Error('镜头时长必须是有限秒数');
  const duration = shotTime(seconds);
  if (duration < SCRIPT_SHOT_MIN_SECONDS) throw new Error(`镜头时长不能短于 ${SCRIPT_SHOT_MIN_SECONDS} 秒`);
  const shot = base.shots[index]!;
  const speech = shot.dialogue.reduce((longest, line) => Math.max(longest, shotTime(line.end - line.start)), 0);
  if (duration < speech - SHOT_TIME_TOLERANCE) throw new Error(`第 ${shot.order} 镜的台词需要 ${speech} 秒，镜头时长不得短于台词；请先调整台词或拆分镜头`);
  const delta = shotTime(duration - (shot.end - shot.start));
  const total = base.shots[base.shots.length - 1]!.end + delta;
  if (total > SCRIPT_TIMELINE_MAX_SECONDS + SHOT_TIME_TOLERANCE) throw new Error(`改时长后总时长 ${shotTime(total)} 秒超过 ${SCRIPT_TIMELINE_MAX_SECONDS} 秒上限，请拆分场次`);
  base.shots[index] = { ...shot, end: shotTime(shot.start + duration) };
  for (let next = index + 1; next < base.shots.length; next++) {
    const item = base.shots[next]!;
    base.shots[next] = {
      ...item,
      start: shotTime(item.start + delta),
      end: shotTime(item.end + delta),
      dialogue: item.dialogue.map(line => ({ ...line, start: shotTime(line.start + delta), end: shotTime(line.end + delta) })),
    };
  }
  return validateScriptShotPlan(base);
}

/** 记录人工确认的机位；只保存视口真实取景，不生成或猜测别的机位。 */
export function setScriptShotCamera(plan: ScriptShotPlan, shotId: string, camera: ScriptShotCamera): ScriptShotPlan {
  const base = cloneShotPlan(validateScriptShotPlan(plan));
  const index = shotAt(base, shotId);
  base.shots[index] = { ...base.shots[index]!, camera: validateScriptShotCamera(camera) };
  return validateScriptShotPlan(base);
}

/** 明确清空某一镜的机位；清空后保持 null，不借用默认机位。 */
export function clearScriptShotCamera(plan: ScriptShotPlan, shotId: string): ScriptShotPlan {
  const base = cloneShotPlan(validateScriptShotPlan(plan));
  const index = shotAt(base, shotId);
  base.shots[index] = { ...base.shots[index]!, camera: null };
  return validateScriptShotPlan(base);
}

/** 来源失效检查：剧本原文或所选场次变了就必须重新生成，不能沿用旧镜头表继续应用。 */
export function assertScriptShotPlanSource(plan: ScriptShotPlan, draft: ScriptSceneDraft): ScriptShotPlan {
  const valid = validateScriptShotPlan(plan);
  if (valid.sceneId !== draft.sceneId) throw new Error(`镜头表属于 ${valid.sceneId}，当前草稿是 ${draft.sceneId}；请先选择同一场次`);
  if (valid.sourceFingerprint !== draft.review.sourceFingerprint || valid.sceneFingerprint !== draft.review.sceneFingerprint) throw new Error('剧本原文或本场内容已改变，镜头表已失效，请重新生成');
  return valid;
}

/** 镜头表摘要：镜数、总时长与未指定机位的镜数，供界面如实显示。 */
export function summarizeScriptShotPlan(plan: ScriptShotPlan) {
  const valid = validateScriptShotPlan(plan);
  return { shots: valid.shots.length, seconds: valid.shots[valid.shots.length - 1]!.end, missingCameras: valid.shots.filter(shot => !shot.camera).length };
}

/**
 * 连续逐镜播放的就绪检查：任何一镜没有人工记录的机位都不播放。
 * 这里不借用别的镜头、不生成默认机位，只把缺失的镜次原样报出来。
 */
export function assertScriptShotPlaybackReady(plan: ScriptShotPlan): ScriptShotPlan {
  const valid = validateScriptShotPlan(plan);
  const missing = valid.shots.filter(shot => !shot.camera);
  if (missing.length > 0) {
    throw new Error(`镜头表还有 ${missing.length} 镜未指定机位（第 ${missing.map(shot => shot.order).join('、')} 镜），不能连续逐镜播放；请先在视口逐镜记录机位，不会自动补机位`);
  }
  return valid;
}

export type ScriptShotMotionSample = {
  shotId: string;
  order: number;
  targetId: string;
  targetName: string;
  /** 0–1；走完后为 1，不再越过终点。 */
  progress: number;
  /** 平地直线上的 XZ；Y 不在计划里，调用方保留对象当前高度。 */
  position: { x: number; z: number };
  /** 面朝行进方向，模型默认面朝 +Z。 */
  yaw: number;
  /** 本镜人工选择的骨骼 clip；未选择为 null。 */
  animation: ScriptShotMotionAnimation | null;
  /** 已钳到 clip 时长的绝对采样秒数；未选择 clip 为 0。 */
  animationSeconds: number;
  /** 走位时长已走完（停在终点）。 */
  finished: boolean;
};

/** 在指定镜内按绝对秒数确定性采样走位；不做缓动、避障或地形抬升。 */
export function sampleScriptShotMotionAt(shot: ScriptShot, seconds: number): ScriptShotMotionSample | null {
  const motion = shot.motion;
  if (!motion) return null;
  const local = Math.min(Math.max(0, Number.isFinite(seconds) ? seconds : 0) - shot.start, shot.end - shot.start, motion.duration);
  const progress = motion.duration > 0 ? Math.min(1, Math.max(0, local / motion.duration)) : 1;
  const animation = motion.animation ?? null;
  return {
    shotId: shot.id,
    order: shot.order,
    targetId: motion.targetId,
    targetName: motion.targetName,
    progress,
    position: {
      x: motion.start.x + (motion.end.x - motion.start.x) * progress,
      z: motion.start.z + (motion.end.z - motion.start.z) * progress,
    },
    yaw: Math.atan2(motion.end.x - motion.start.x, motion.end.z - motion.start.z),
    animation,
    animationSeconds: animation ? Math.min(Math.max(0, local), animation.duration) : 0,
    finished: progress >= 1 - SHOT_TIME_TOLERANCE,
  };
}

/** 按时间轴取当前镜的走位；没有绑定返回 null。 */
export function sampleScriptShotMotion(plan: ScriptShotPlan, seconds: number): ScriptShotMotionSample | null {
  const valid = validateScriptShotPlan(plan);
  if (typeof seconds !== 'number' || !Number.isFinite(seconds) || seconds < 0) throw new Error('采样时间必须是有限非负数');
  const total = valid.shots[valid.shots.length - 1]!.end;
  const time = Math.min(seconds, total);
  const found = valid.shots.findIndex(shot => time < shot.end);
  const shot = valid.shots[found < 0 ? valid.shots.length - 1 : found]!;
  return sampleScriptShotMotionAt(shot, time);
}

export type ScriptShotPlaybackSample = {
  /** 当前镜在 shots 数组里的下标。 */
  index: number;
  shot: ScriptShot;
  camera: ScriptShotCamera;
  /** 当前时刻落在的台词；镜头内的停顿或空台词为 null，不借用相邻台词。 */
  line: ScriptTimedLine | null;
  /** 当前镜的人工走位采样；未绑定为 null。 */
  motion: ScriptShotMotionSample | null;
  /** 已钳制到 [0, 总时长] 的播放时间（秒）。 */
  seconds: number;
};

/**
 * 按镜头表时间轴取当前镜与当前台词：到镜次边界就切到下一镜，最后一镜保持到总时长结束。
 * 缺机位或时间不是有限非负数都显式报错，不静默沿用上一镜的机位。
 */
export function sampleScriptShotPlayback(plan: ScriptShotPlan, seconds: number): ScriptShotPlaybackSample {
  const valid = validateScriptShotPlan(plan);
  if (typeof seconds !== 'number' || !Number.isFinite(seconds) || seconds < 0) throw new Error('播放时间必须是有限非负数');
  const total = valid.shots[valid.shots.length - 1]!.end;
  const time = Math.min(seconds, total);
  // 镜头时间连续且首尾相接：第一个 end 大于当前时间的就是当前镜；到总时长时停在最后一镜。
  const found = valid.shots.findIndex(shot => time < shot.end);
  const index = found < 0 ? valid.shots.length - 1 : found;
  const shot = valid.shots[index]!;
  if (!shot.camera) throw new Error(`第 ${shot.order} 镜未指定机位，不能切换机位；不会自动补机位`);
  const line = shot.dialogue.find(item => time >= item.start && time < item.end) ?? null;
  return { index, shot, camera: shot.camera, line, motion: sampleScriptShotMotionAt(shot, time), seconds: time };
}

/**
 * 播/存前把计划里的走位目标对到当前场景：目标必须存在、可见、未锁定，可选骨骼 clip 必须仍按索引匹配。
 * 只校验并原样返回计划，不改对象、不选默认目标。
 */
export function assertScriptShotMotionTargets(plan: ScriptShotPlan, targets: readonly ScriptShotMotionTarget[]): ScriptShotPlan {
  const valid = validateScriptShotPlan(plan);
  const byId = new Map<string, ScriptShotMotionTarget>();
  for (const target of targets) {
    if (!target || typeof target !== 'object' || typeof target.id !== 'string' || !target.id.trim()) throw new Error('场景对象列表格式无效');
    byId.set(target.id, target);
  }
  for (const shot of valid.shots) {
    const motion = shot.motion;
    if (!motion) continue;
    const label = `第 ${shot.order} 镜的走位目标`;
    const target = byId.get(motion.targetId);
    if (!target) throw new Error(`${label} ${motion.targetId}（${motion.targetName}）在当前场景中不存在，请重新绑定`);
    if (target.visible === false) throw new Error(`${label}「${target.name || motion.targetName}」已隐藏，不能走位；请先显示或改绑`);
    if (target.locked) throw new Error(`${label}「${target.name || motion.targetName}」已锁定，不能走位；请先解锁或改绑`);
    for (const reference of [motion.animation, motion.characterMotion?.blend?.animation]) {
      if (!reference) continue;
      const clip = Array.isArray(target.animations) ? target.animations[reference.index] : undefined;
      if (!clip || clip.name !== reference.name || clip.duration !== reference.duration) {
        throw new Error(`${label}「${target.name || motion.targetName}」的骨骼动作「${reference.name}」已变化，请重新选择`);
      }
    }
  }
  return valid;
}

// ---------------------------------------------------------------------------
// 逐镜有序参考包：镜头表 + 人工机位的派生输出（只算清单，不渲染、不联网、不写场景）
// ---------------------------------------------------------------------------
/** 参考图 pass 与视口既有白模/法线/深度三种材质一一对应。 */
export const SCRIPT_REFERENCE_PASSES = ['clay', 'normal', 'depth'] as const;
export type ScriptReferencePass = (typeof SCRIPT_REFERENCE_PASSES)[number];
export const SCRIPT_REFERENCE_MANIFEST_FILE = 'manifest.json';

export type ScriptReferenceFile = { pass: ScriptReferencePass; name: string };
export type ScriptReferenceShot = {
  shotId: string;
  order: number;
  /** 固定采样时刻：取本镜开始秒数；镜内机位固定，所以这就是本镜取景。 */
  sampleSeconds: number;
  /** 人工在视口记录的机位原值，不做取整或替换。 */
  camera: ScriptShotCamera;
  dialogue: ScriptTimedLine[];
  /** 包内文件名，顺序 = pass 顺序；文件名前缀 = 镜序，便于按序展开。 */
  files: ScriptReferenceFile[];
};
export type ScriptReferencePack = {
  version: 1;
  kind: 'script-shot-reference-pack';
  sceneId: string;
  heading: string;
  location: string;
  /** 剧本原文指纹与场次指纹：来源变了，旧包即失效。 */
  sourceFingerprint: string;
  sceneFingerprint: string;
  exportedAt: string;
  /** 固定为视口人工记录；这里不产生、也不推断机位。 */
  cameraSource: 'viewport-recorded';
  passes: ScriptReferencePass[];
  manifestFile: string;
  shots: ScriptReferenceShot[];
  notes: string[];
};

const referenceFileName = (order: number, pass: ScriptReferencePass) => `shot-${String(order).padStart(2, '0')}-${pass}.png`;

/** pass 列表只接受白模/法线/深度，且不能为空、不能重复；不做默认补齐以外的猜测。 */
export function normalizeScriptReferencePasses(value: unknown): ScriptReferencePass[] {
  const list = value === undefined ? [...SCRIPT_REFERENCE_PASSES] : value;
  if (!Array.isArray(list) || list.length === 0) throw new Error('参考包至少要选一种参考图（白模/法线/深度）');
  const result: ScriptReferencePass[] = [];
  list.forEach((item) => {
    if (typeof item !== 'string' || !(SCRIPT_REFERENCE_PASSES as readonly string[]).includes(item)) throw new Error(`不支持的参考图类型：${String(item)}`);
    if (result.includes(item as ScriptReferencePass)) throw new Error(`参考图类型重复：${item}`);
    result.push(item as ScriptReferencePass);
  });
  return result;
}

/** 导出前的就绪检查：任何一镜缺人工机位都拒绝整包，不借用别的镜头。 */
export function assertScriptShotReferenceReady(plan: ScriptShotPlan): ScriptShotPlan {
  const ready = assertScriptShotPlaybackReady(plan);
  return ready;
}

/**
 * 生成逐镜参考包清单：镜序、每镜固定采样秒数、人工机位、台词、文件名与来源指纹全部先确定。
 * 不渲染、不读实时播放头、不改输入镜头表；任一步不成立就抛错，调用方不得产出部分包。
 */
export function buildScriptShotReferencePack(
  plan: ScriptShotPlan,
  options: { passes?: unknown; now?: Date } = {},
): ScriptReferencePack {
  const ready = assertScriptShotReferenceReady(plan);
  const passes = normalizeScriptReferencePasses(options.passes);
  const now = options.now ?? new Date();
  if (!(now instanceof Date) || !Number.isFinite(now.getTime())) throw new Error('参考包时间戳无效');
  const shots = ready.shots.map((shot, index) => {
    // 用既有时间轴采样确认「镜序 ↔ 机位」一一对应；采样时刻就是本镜开始秒数。
    const sample = sampleScriptShotPlayback(ready, shot.start);
    if (sample.index !== index || sample.shot.id !== shot.id) throw new Error(`第 ${shot.order} 镜的采样与镜序不一致，已停止导出`);
    return {
      shotId: shot.id,
      order: shot.order,
      sampleSeconds: sample.seconds,
      camera: {
        position: [...sample.camera.position] as [number, number, number],
        target: [...sample.camera.target] as [number, number, number],
        fov: sample.camera.fov,
      },
      dialogue: shot.dialogue.map(line => ({ ...line })),
      files: passes.map(pass => ({ pass, name: referenceFileName(shot.order, pass) })),
    };
  });
  return {
    version: 1,
    kind: 'script-shot-reference-pack',
    sceneId: ready.sceneId,
    heading: ready.heading,
    location: ready.location,
    sourceFingerprint: ready.sourceFingerprint,
    sceneFingerprint: ready.sceneFingerprint,
    exportedAt: now.toISOString(),
    cameraSource: 'viewport-recorded',
    passes,
    manifestFile: SCRIPT_REFERENCE_MANIFEST_FILE,
    shots,
    notes: [
      '参考包是已确认镜头表与人工记录机位的派生输出：不生成角色表演、不编码视频、不上传素材库、不写业务库。',
      '采样时刻取每镜开始秒数；镜内机位固定，因此该时刻即本镜取景。采样后视口还原到导出前机位。',
      '白模/法线/深度均为本地渲染 PNG；深度为近黑远白的 8 位线性灰度，范围按模型包围盒计算。',
      '是否可把某张图当控制输入，必须在目标工作流实测，不能按普通参考图对待。',
      ...(ready.shots.some(shot => shot.motion) ? ['镜头表含人工走位；本函数仅构造清单，实际走位与骨姿的逐镜采样由导出入口负责并记录。'] : []),
    ],
  };
}

/** manifest 正文：缩进 JSON，逐字保留机位浮点值与指纹，便于外部核对。 */
export function scriptShotReferenceManifestText(pack: ScriptReferencePack): string {
  if (!pack || pack.version !== 1 || pack.kind !== 'script-shot-reference-pack') throw new Error('参考包清单版本无效');
  return JSON.stringify(pack, null, 2);
}
