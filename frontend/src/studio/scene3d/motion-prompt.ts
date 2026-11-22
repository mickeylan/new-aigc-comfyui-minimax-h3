/**
 * 运镜文案 → 场景视频提示词。
 *
 * 首尾帧只锚住两端，中间怎么动全靠提示词里那一句；这句话之前只能靠人复制粘贴，
 * 换个页面就容易漏。这里把「拼句子」和「追加到已有提示词」做成纯函数：
 * 只追加、不覆盖、已有就不重复写，写回后端由调用方负责。
 */

const NOTE_PREFIX = "运镜：";

export type PromptAppend = {
  /** 追加后的完整提示词（没有追加时原样返回去掉首尾空白的版本）。 */
  prompt: string;
  /** 这次真的往后加了东西吗。 */
  added: boolean;
  /** 实际要写进去的那一行；文案为空时是空串。 */
  line: string;
};

/** 去掉句尾的句号/分号，避免拼出「……半圈。。」。 */
function trimTail(text: string): string {
  return text.trim().replace(/[。；;，,]+$/u, "");
}

/** 把运镜文案整理成提示词里的一行：「运镜：相机向右环绕 180°（半圈）；距离推近 40%。」 */
export function motionPromptLine(note: string): string {
  const body = trimTail(note);
  if (!body) return "";
  // 文案本身已经带「运镜」开头时不再套第二层前缀（Agent 的说明有时会自带）。
  const head = body.startsWith("运镜") ? body : `${NOTE_PREFIX}${body}`;
  return `${head}。`;
}

/**
 * 追加到已有提示词末尾：换行分隔，保留原文一个字都不动。
 * 已经包含这句（或包含同一段运镜文案）时返回 added: false，调用方据此跳过写入。
 */
export function appendMotionToPrompt(prompt: string, note: string): PromptAppend {
  const line = motionPromptLine(note);
  const base = prompt.trim();
  if (!line) return { prompt: base, added: false, line: "" };
  const bare = trimTail(note);
  if (base.includes(line) || (bare && base.includes(bare))) {
    return { prompt: base, added: false, line };
  }
  return { prompt: base ? `${base}\n${line}` : line, added: true, line };
}

export type PromptSceneChoice = {
  id: number;
  /** 下拉里显示的标签。 */
  label: string;
  /** 该场当前的完整视频提示词（可能为空）。 */
  prompt: string;
};

type SceneLike = {
  id?: unknown;
  title?: unknown;
  order?: unknown;
  order_num?: unknown;
  episode_n?: unknown;
  episode_number?: unknown;
  video_full_prompt?: unknown;
  video_prompt?: unknown;
};

function asSceneList(project: unknown): SceneLike[] {
  if (!project || typeof project !== "object") return [];
  const scenes = (project as { scenes?: unknown }).scenes;
  if (!Array.isArray(scenes)) return [];
  return scenes.filter((item): item is SceneLike => Boolean(item) && typeof item === "object");
}

function text(value: unknown): string {
  return typeof value === "string" ? value : "";
}

function ordinal(value: unknown, fallback: string): string {
  const num = typeof value === "number" ? value : Number(value);
  return Number.isFinite(num) ? String(num) : fallback;
}

/**
 * GET /api/projects/:id 里挑出能写提示词的场次。
 * 标签带集/场序号，方便在 3D 页里认出「刚才渲的是哪一场」。
 */
export function scenePromptChoices(project: unknown): PromptSceneChoice[] {
  const rows = asSceneList(project);
  const choices: PromptSceneChoice[] = [];
  rows.forEach((row, index) => {
    const id = typeof row.id === "number" ? row.id : Number(row.id);
    if (!Number.isFinite(id)) return;
    const episode = ordinal(row.episode_n ?? row.episode_number, "");
    const order = ordinal(row.order ?? row.order_num, String(index + 1));
    const title = text(row.title).trim();
    const head = episode ? `第 ${episode} 集 · 第 ${order} 场` : `第 ${order} 场`;
    const prompt = text(row.video_full_prompt) || text(row.video_prompt);
    choices.push({ id, label: title ? `${head} ${title}` : head, prompt });
  });
  return choices;
}

export type ProjectChoice = { id: string; label: string };

/** GET /api/projects 的返回里挑出项目下拉项（id 统一成字符串，直接给 <select> 用）。 */
export function projectChoices(projects: unknown): ProjectChoice[] {
  const rows = Array.isArray(projects) ? projects : [];
  const choices: ProjectChoice[] = [];
  rows.forEach((raw) => {
    if (!raw || typeof raw !== "object") return;
    // 列表接口每项是 { project: {...}, ... } 的包装，详情接口是裸 project；两种都认。
    const row = (raw as { project?: unknown }).project ?? raw;
    if (!row || typeof row !== "object") return;
    const idRaw = (row as { id?: unknown }).id;
    const id = typeof idRaw === "number" ? String(idRaw) : text(idRaw);
    if (!id) return;
    const name = (text((row as { title?: unknown }).title) || text((row as { name?: unknown }).name)).trim();
    choices.push({ id, label: name || `项目 ${id}` });
  });
  return choices;
}