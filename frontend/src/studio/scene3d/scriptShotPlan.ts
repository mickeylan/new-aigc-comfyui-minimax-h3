// Shot data contract: no runtime dependency on set templates or procedural catalogs.
import { estimateChineseDialogueDuration, SPEAKER_SWITCH_SECONDS } from '../../utils/dialogueDuration.js';
import { validateCharacterMotionSettings, type CharacterMotionSettings } from './characterMotion';
import type { ShotNavigation } from './shotNavigation';
import { validateShotExecution, sampleExecutionCamera, sampleExecutionActors, type ShotExecution } from './shotExecution';
import type { ScriptSceneDraft } from './toonflowSets';

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
  execution?: ShotExecution;
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
    if (shot.execution !== undefined) validateShotExecution(shot.execution, end - start);
  });
  const executionIds = new Set(plan.shots.flatMap(shot => (shot.execution?.actors ?? []).map(actor => actor.targetId)));
  if (executionIds.size > 32) throw Error('整份执行计划超过32个角色，请拆分场次');
  if (executionIds.size && plan.shots.some(shot => shot.motion)) throw Error('Execution actors cannot mix with legacy motion anywhere in a plan');
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
  return { shots: valid.shots.length, seconds: valid.shots[valid.shots.length - 1]!.end, missingCameras: valid.shots.filter(shot => !shot.camera && !shot.execution?.camera).length };
}

/**
 * 连续逐镜播放的就绪检查：任何一镜没有人工记录的机位都不播放。
 * 这里不借用别的镜头、不生成默认机位，只把缺失的镜次原样报出来。
 */
export function assertScriptShotPlaybackReady(plan: ScriptShotPlan): ScriptShotPlan {
  const valid = validateScriptShotPlan(plan);
  const missing = valid.shots.filter(shot => !shot.camera && !shot.execution?.camera);
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
  /** Present only for the opt-in execution extension. */
  actors?: ReturnType<typeof sampleExecutionActors>;
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
  const camera = sampleExecutionCamera(shot.execution, time - shot.start) ?? shot.camera;
  if (!camera) throw new Error(`第 ${shot.order} 镜未指定机位，不能切换机位；不会自动补机位`);
  const line = shot.dialogue.find(item => time >= item.start && time < item.end) ?? null;
  return { index, shot, camera, line, motion: sampleScriptShotMotionAt(shot, time), seconds: time,
    ...(shot.execution?.actors ? { actors: sampleExecutionActors(shot.execution, time - shot.start) } : {}) };
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
    for (const actor of shot.execution?.actors ?? []) {
      const target = byId.get(actor.targetId);
      if (!target || target.visible === false || target.locked) throw Error(`Execution actor unavailable: ${actor.targetId}`);
      for (const beat of actor.beats) {
        const clip = target.animations?.[beat.animation.index];
        if (!clip || clip.name !== beat.animation.name || clip.duration !== beat.animation.duration) throw Error(`Execution clip resource changed: ${actor.targetId}`);
      }
    }
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
