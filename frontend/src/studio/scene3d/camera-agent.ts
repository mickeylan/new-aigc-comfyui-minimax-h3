// 相机 Agent：把一句中文的运镜要求解析成结构化机位，缺参数就追问，出预览再采用。
// 只编排相机 —— 不改角色、不改场景、不碰提示词。纯函数，不碰 three/DOM，便于直接跑测试。
//
// 为什么先做「只排相机」：H3 首尾帧实测证明「中间怎么动」必须写清楚；
// 人能说清「绕到背面再推近」，但不想一个个机位手点。规则表足够覆盖现有镜头语法，
// 不必先接一个大模型。

import type { GroundPoint } from "./ground-path.js";

export type CameraMoveKind = "orbit" | "dolly" | "crane" | "pan" | "follow" | "hitchcock";

export type Vec3 = [number, number, number];

/** 一段运镜；数字缺省表示「用户没说」，由 questions 追问，不替用户瞎猜。 */
export interface CameraMove {
  kind: CameraMoveKind;
  /** 环绕圈数（1 = 一圈，0.5 = 半圈）。 */
  turns?: number;
  /** 环绕/摇镜方向：1 向右，-1 向左。 */
  direction?: 1 | -1;
  /** 推近/拉远的幅度（百分数；推近为正）。 */
  percent?: number;
  /** 升降多少米（正 = 升）。 */
  meters?: number;
  /** 摇镜角度（度）。 */
  degrees?: number;
  /** 这一段用几秒。 */
  seconds?: number;
  /** 采样几个机位。 */
  segments?: number;
  /** 环绕收尾时的抬高/压低角度（度）。 */
  pitch?: number;
}

export interface CameraIntent {
  /** 用户原话。 */
  text: string;
  moves: CameraMove[];
  /** 依次要问的问题；空数组表示可以出预览了。 */
  questions: string[];
  /** 认不出来但被忽略的片段，原样留着给人看。 */
  ignored: string[];
}

export interface CameraAnchor {
  position: Vec3;
  target: Vec3;
  fov: number;
}

/**
 * 跟拍要的路径：只给一个 t∈[0,1] → 世界坐标的采样函数。
 * 不直接依赖 ground-path，模块保持无依赖（测试里直接塞一条直线即可）。
 */
export type FollowPathSample = (t: number) => GroundPoint;

/** 与组件里的 CameraKey 同形（不含 id），由调用方补 id。 */
export interface CameraKeyDraft {
  position: Vec3;
  target: Vec3;
  duration: number;
  easing: "easeInOut";
  fov: number;
}

const CN_DIGITS: Record<string, number> = {
  零: 0, 一: 1, 二: 2, 两: 2, 三: 3, 四: 4, 五: 5, 六: 6, 七: 7, 八: 8, 九: 9, 十: 10,
};

/** 只认 0–99 的中文/阿拉伯数字，够描述圈数与秒数。 */
function readNumber(raw: string | undefined): number | null {
  if (!raw) return null;
  const arabic = Number(raw);
  if (Number.isFinite(arabic)) return arabic;
  if (raw === "十") return 10;
  if (raw.length === 1) return CN_DIGITS[raw] ?? null;
  if (raw.length === 2 && raw[0] === "十") return 10 + (CN_DIGITS[raw[1]] ?? 0);
  if (raw.length === 2 && raw[1] === "十") return (CN_DIGITS[raw[0]] ?? 0) * 10;
  return null;
}

const NUM = "[0-9零一二两三四五六七八九十]+";

// 注意：这四个正则不能用 /g —— 同一个对象反复 test() 会带着 lastIndex 跳着命中。
const TURN_WORDS: Array<[RegExp, number]> = [
  [/(?:四分之一|1\/4)\s*圈/, 0.25],
  [/(?:半|1\/2)\s*圈/, 0.5],
  [/(?:一|1)\s*整?圈/, 1],
];

/** 圈数：明确说「半圈/一圈/N 圈/N 度」都收；都没有返回 null 交给追问。 */
function readTurns(text: string): number | null {
  for (const [pattern, turns] of TURN_WORDS) {
    if (pattern.test(text)) return turns;
  }
  const circle = new RegExp(`(${NUM})\\s*圈`).exec(text);
  if (circle) {
    const value = readNumber(circle[1]);
    if (value !== null && value > 0) return value;
  }
  const degree = new RegExp(`(${NUM})\\s*(?:度|°|deg)`).exec(text);
  if (degree) {
    const value = readNumber(degree[1]);
    if (value !== null && value > 0) return value / 360;
  }
  return null;
}

function readPercent(text: string): number | null {
  const match = new RegExp(`(${NUM})\\s*(?:%|％|百分之)`, "g").exec(text);
  if (!match) return null;
  const value = readNumber(match[1]);
  return value !== null && value > 0 ? value : null;
}

function readMeters(text: string): number | null {
  const match = new RegExp(`(${NUM})\\s*(?:米|m\\b)`, "i").exec(text);
  if (!match) return null;
  const value = readNumber(match[1]);
  return value !== null && value > 0 ? value : null;
}

function readSeconds(text: string): number | null {
  const match = new RegExp(`(${NUM})\\s*秒`).exec(text);
  if (!match) return null;
  const value = readNumber(match[1]);
  return value !== null && value > 0 ? value : null;
}

function readDirection(text: string, kind: CameraMoveKind): 1 | -1 | undefined {
  if (/向左|往左|左摇|左转|逆时针/.test(text)) return -1;
  if (/向右|往右|右摇|右转|顺时针/.test(text)) return 1;
  if (kind === "pan" && /左/.test(text)) return -1;
  if (kind === "pan" && /右/.test(text)) return 1;
  return undefined;
}

const MOVE_WORDS: Array<{ kind: CameraMoveKind; pattern: RegExp }> = [
  { kind: "hitchcock", pattern: /希区柯克|眩晕|变焦推拉|滑动变焦/ },
  { kind: "orbit", pattern: /环绕|绕(?:一|半|着|到|过|圈|目标|角色|对象)|转(?:一圈|半圈|到|过)|旋转/ },
  { kind: "follow", pattern: /跟拍|跟随|跟着|跟踪/ },
  { kind: "dolly", pattern: /推近|推进|推上去|推镜|拉远|后拉|后退|拉开/ },
  { kind: "crane", pattern: /升高|升空|上升|升起|摇臂|俯冲|下降|降低|下压/ },
  { kind: "pan", pattern: /摇镜|左摇|右摇|横摇|平移|转动镜头/ },
];

const DEFAULT_SECONDS: Record<CameraMoveKind, number> = {
  orbit: 4,
  dolly: 3,
  crane: 3,
  pan: 3,
  follow: 4,
  hitchcock: 3,
};

const DEFAULT_SEGMENTS: Record<CameraMoveKind, number> = {
  orbit: 8,
  dolly: 6,
  crane: 6,
  pan: 6,
  follow: 8,
  hitchcock: 6,
};

/** 「先绕半圈，然后推近 40%」按连词切段，逐段认动作，顺序即播放顺序。 */
export function parseCameraBrief(text: string): CameraIntent {
  const source = (text ?? "").trim();
  if (!source) return { text: "", moves: [], questions: ["先说一句运镜：从哪个角度、怎么动、到哪儿结束？"], ignored: [] };

  const parts = source
    .split(/[，,。;；]|然后|接着|之后|再(?=[绕推拉升降摇跟])/)
    .map((part) => part.trim())
    .filter(Boolean);

  const moves: CameraMove[] = [];
  const ignored: string[] = [];
  const globalSeconds = readSeconds(source);

  for (const part of parts) {
    const hit = MOVE_WORDS.find((word) => word.pattern.test(part));
    if (!hit) {
      if (part !== source) ignored.push(part);
      continue;
    }
    if (moves.some((move) => move.kind === hit.kind)) continue;
    const move: CameraMove = { kind: hit.kind };
    const direction = readDirection(part, hit.kind);
    if (direction) move.direction = direction;

    if (hit.kind === "orbit") {
      const turns = readTurns(part);
      if (turns !== null) move.turns = turns;
      const pitch = /俯拍|抬高|升空/.test(part) ? 15 : /仰拍|压低/.test(part) ? -15 : undefined;
      if (pitch !== undefined) move.pitch = pitch;
    }
    if (hit.kind === "dolly" || hit.kind === "hitchcock") {
      const percent = readPercent(part);
      if (percent !== null) move.percent = /拉远|后拉|后退|拉开/.test(part) ? -percent : percent;
    }
    if (hit.kind === "crane") {
      const meters = readMeters(part);
      if (meters !== null) move.meters = /下降|降低|下压/.test(part) ? -meters : meters;
    }
    if (hit.kind === "pan") {
      const degrees = new RegExp(`(${NUM})\\s*(?:度|°)`).exec(part);
      const value = degrees ? readNumber(degrees[1]) : null;
      if (value !== null) move.degrees = value;
    }
    const seconds = readSeconds(part);
    if (seconds !== null) move.seconds = seconds;
    moves.push(move);
  }

  if (moves.length === 0) {
    return {
      text: source,
      moves: [],
      questions: ["没认出动作。可以这样写：「从正面绕到背后半圈，然后推近 40%」。"],
      ignored,
    };
  }

  // 希区柯克本身就是「推近/拉远 + 改视角」，后面再写一个推近只是补百分比，
  // 不能真排成两段（那样会先变焦推一次、再普通推一次）。
  const hitchcock = moves.find((move) => move.kind === "hitchcock");
  if (hitchcock) {
    const dollies = moves.filter((move) => move.kind === "dolly");
    for (const dolly of dollies) {
      if (hitchcock.percent === undefined) hitchcock.percent = dolly.percent;
      if (hitchcock.seconds === undefined) hitchcock.seconds = dolly.seconds;
    }
    if (dollies.length > 0) {
      for (let i = moves.length - 1; i >= 0; i -= 1) {
        if (moves[i].kind === "dolly") moves.splice(i, 1);
      }
    }
  }

  if (globalSeconds !== null) {
    for (const move of moves) if (move.seconds === undefined) move.seconds = globalSeconds;
  }

  return { text: source, moves, questions: questionsFor(moves), ignored };
}

/** 每段只问一个最关键的缺失参数，一次问一个，避免一口气甩一堆。 */
export function questionsFor(moves: CameraMove[]): string[] {
  const questions: string[] = [];
  for (const move of moves) {
    if (move.kind === "orbit" && move.turns === undefined) {
      questions.push("环绕多少？（半圈 / 一圈 / 90°）");
    }
    if ((move.kind === "dolly" || move.kind === "hitchcock") && move.percent === undefined) {
      questions.push("推近还是拉远、多少？（如 40%）");
    }
    if (move.kind === "crane" && move.meters === undefined) {
      questions.push("升降多少米？（如 2 米）");
    }
    if (move.kind === "pan" && move.degrees === undefined) {
      questions.push("摇多少度？（如 45°）");
    }
    if (move.kind === "follow" && move.seconds === undefined) {
      questions.push("跟拍几秒？（如 6 秒）");
    }
  }
  return questions;
}

/**
 * 用一句补充回答填上最靠前的那个缺口。
 * 认不出来的回答原样返回，问题不动 —— 宁可再问一次，也不猜一个数。
 */
export function answerCameraQuestion(intent: CameraIntent, answer: string): CameraIntent {
  if (intent.questions.length === 0) return intent;
  const moves = intent.moves.map((move) => ({ ...move }));
  const text = (answer ?? "").trim();
  const seconds = readSeconds(text);

  for (const move of moves) {
    if (move.kind === "orbit" && move.turns === undefined) {
      const turns = readTurns(text);
      if (turns === null) return intent;
      move.turns = turns;
      if (seconds !== null && move.seconds === undefined) move.seconds = seconds;
      return { ...intent, moves, questions: questionsFor(moves) };
    }
    if ((move.kind === "dolly" || move.kind === "hitchcock") && move.percent === undefined) {
      const percent = readPercent(text);
      if (percent === null) return intent;
      move.percent = /拉远|后拉|后退|拉开/.test(text) ? -percent : percent;
      if (seconds !== null && move.seconds === undefined) move.seconds = seconds;
      return { ...intent, moves, questions: questionsFor(moves) };
    }
    if (move.kind === "crane" && move.meters === undefined) {
      const meters = readMeters(text);
      if (meters === null) return intent;
      move.meters = /下降|降低|下压/.test(text) ? -meters : meters;
      return { ...intent, moves, questions: questionsFor(moves) };
    }
    if (move.kind === "pan" && move.degrees === undefined) {
      const degrees = new RegExp(`(${NUM})\\s*(?:度|°)`).exec(text);
      const value = degrees ? readNumber(degrees[1]) : null;
      if (value === null) return intent;
      move.degrees = value;
      return { ...intent, moves, questions: questionsFor(moves) };
    }
    if (move.kind === "follow" && move.seconds === undefined) {
      if (seconds === null) return intent;
      move.seconds = seconds;
      return { ...intent, moves, questions: questionsFor(moves) };
    }
  }
  return intent;
}

/** 把一段运镜说成人话，用于预览确认。 */
export function describeMove(move: CameraMove): string {
  const seconds = move.seconds ?? DEFAULT_SECONDS[move.kind];
  switch (move.kind) {
    case "orbit":
      return `${move.direction === -1 ? "向左" : "向右"}环绕 ${move.turns ?? 1} 圈（${seconds} 秒）`;
    case "dolly":
      return `${(move.percent ?? 0) >= 0 ? "推近" : "拉远"} ${Math.abs(move.percent ?? 0)}%（${seconds} 秒）`;
    case "crane":
      return `${(move.meters ?? 0) >= 0 ? "升高" : "下降"} ${Math.abs(move.meters ?? 0)} 米（${seconds} 秒）`;
    case "pan":
      return `原地${move.direction === -1 ? "向左" : "向右"}摇 ${move.degrees ?? 0}°（${seconds} 秒）`;
    case "follow":
      return `沿 A→B 路径跟拍（${seconds} 秒）`;
    case "hitchcock":
      return `希区柯克变焦（距离 ${(move.percent ?? 0) >= 0 ? "推近" : "拉远"} ${Math.abs(move.percent ?? 0)}% 同时改视角，${seconds} 秒）`;
  }
}

function add(a: Vec3, b: Vec3): Vec3 {
  return [a[0] + b[0], a[1] + b[1], a[2] + b[2]];
}

function sub(a: Vec3, b: Vec3): Vec3 {
  return [a[0] - b[0], a[1] - b[1], a[2] - b[2]];
}

function scale(a: Vec3, k: number): Vec3 {
  return [a[0] * k, a[1] * k, a[2] * k];
}

function rotateY(point: Vec3, center: Vec3, radians: number): Vec3 {
  const dx = point[0] - center[0];
  const dz = point[2] - center[2];
  const cos = Math.cos(radians);
  const sin = Math.sin(radians);
  return [center[0] + dx * cos - dz * sin, point[1], center[2] + dx * sin + dz * cos];
}

const MIN_SEGMENTS = 2;
const MAX_SEGMENTS = 24;

function segmentsOf(move: CameraMove): number {
  const wanted = move.segments ?? DEFAULT_SEGMENTS[move.kind];
  return Math.min(MAX_SEGMENTS, Math.max(MIN_SEGMENTS, Math.round(wanted)));
}

function secondsOf(move: CameraMove): number {
  const wanted = move.seconds ?? DEFAULT_SECONDS[move.kind];
  return Math.min(60, Math.max(0.2, wanted));
}

export interface CameraPlan {
  /** 逐段的机位（首段包含起幅），按顺序连起来就是整条路径。 */
  keys: CameraKeyDraft[];
  /** 给人确认的逐段说明。 */
  notes: string[];
  /** 出不了图的原因；非空时 keys 为空。 */
  problems: string[];
}

/**
 * 按顺序把每段运镜串起来：每段都从上一段的落点起步。
 * follow 需要地面路径；没有路径时给问题，不猜一条假路径。
 */
export function planCameraMoves(
  intent: CameraIntent,
  anchor: CameraAnchor,
  followPath?: FollowPathSample | null,
): CameraPlan {
  const notes: string[] = [];
  const problems: string[] = [];
  const keys: CameraKeyDraft[] = [];
  if (!intent.moves.length) return { keys, notes, problems: ["还没有可用的运镜"] };
  if (intent.questions.length) return { keys, notes, problems: intent.questions };

  let position: Vec3 = [...anchor.position] as Vec3;
  let target: Vec3 = [...anchor.target] as Vec3;
  let fov = anchor.fov;

  const push = (draft: Omit<CameraKeyDraft, "easing">) => {
    keys.push({ ...draft, easing: "easeInOut" });
    position = [...draft.position] as Vec3;
    target = [...draft.target] as Vec3;
    fov = draft.fov;
  };

  push({ position, target, duration: secondsOf(intent.moves[0]), fov });

  for (const move of intent.moves) {
    const seconds = secondsOf(move);
    const segments = segmentsOf(move);
    const stepSeconds = seconds / segments;
    notes.push(describeMove(move));

    if (move.kind === "orbit") {
      const centerXZ: Vec3 = [target[0], target[1], target[2]];
      const radius = Math.hypot(position[0] - centerXZ[0], position[2] - centerXZ[2]);
      if (radius < 1e-3) {
        problems.push("机位和目标重合，绕不了圈：先把相机挪开一点");
        break;
      }
      const turns = (move.direction === -1 ? -1 : 1) * (move.turns ?? 1);
      const startAngle = Math.atan2(position[0] - centerXZ[0], position[2] - centerXZ[2]);
      const height0 = position[1];
      const pitchRadians = ((move.pitch ?? 0) * Math.PI) / 180;
      for (let i = 1; i <= segments; i += 1) {
        const t = i / segments;
        const angle = startAngle + turns * Math.PI * 2 * t;
        const height = height0 * (1 - t) + (target[1] + (height0 - target[1]) * Math.cos(pitchRadians)) * t;
        push({
          position: [centerXZ[0] + radius * Math.sin(angle), height, centerXZ[2] + radius * Math.cos(angle)],
          target: [...target] as Vec3,
          duration: stepSeconds,
          fov,
        });
      }
      continue;
    }

    if (move.kind === "dolly" || move.kind === "hitchcock") {
      const percent = move.percent ?? 0;
      const ratio = Math.max(0.05, 1 - percent / 100);
      const from = [...position] as Vec3;
      const distance0 = Math.hypot(...(sub(from, target) as [number, number, number]));
      const fov0 = fov;
      const tanHalf0 = Math.tan(((fov0 * Math.PI) / 360));
      for (let i = 1; i <= segments; i += 1) {
        const t = i / segments;
        const k = 1 + (ratio - 1) * t;
        const point = add(target, scale(sub(from, target), k));
        // 希区柯克：距离变、视角跟着变，主体大小不变；普通推拉视角不动。
        let nextFov = fov0;
        if (move.kind === "hitchcock") {
          nextFov = Math.min(90, Math.max(15, (2 * Math.atan(tanHalf0 / k)) * (180 / Math.PI)));
        }
        push({ position: point, target: [...target] as Vec3, duration: stepSeconds, fov: nextFov });
      }
      continue;
    }

    if (move.kind === "crane") {
      const meters = move.meters ?? 0;
      const from = [...position] as Vec3;
      for (let i = 1; i <= segments; i += 1) {
        const t = i / segments;
        push({
          position: [from[0], from[1] + meters * t, from[2]],
          target: [...target] as Vec3,
          duration: stepSeconds,
          fov,
        });
      }
      continue;
    }

    if (move.kind === "pan") {
      const degrees = (move.direction === -1 ? -1 : 1) * (move.degrees ?? 0);
      const from = [...target] as Vec3;
      for (let i = 1; i <= segments; i += 1) {
        const t = i / segments;
        push({
          position: [...position] as Vec3,
          target: rotateY(from, position, ((degrees * t) * Math.PI) / 180),
          duration: stepSeconds,
          fov,
        });
      }
      continue;
    }

    if (move.kind === "follow") {
      if (!followPath) {
        problems.push("跟拍要先有地面路径：先在场景里点 A、B 两个落点");
        break;
      }
      const pathStart = followPath(0);
      const offsetXZ: Vec3 = [position[0] - pathStart[0], 0, position[2] - pathStart[2]];
      const height = position[1];
      const targetHeight = target[1];
      for (let i = 1; i <= segments; i += 1) {
        const point = followPath(i / segments);
        push({
          position: [point[0] + offsetXZ[0], height, point[2] + offsetXZ[2]],
          target: [point[0], targetHeight, point[2]],
          duration: stepSeconds,
          fov,
        });
      }
      continue;
    }
  }

  // 有硬问题就一张机位都不给：半条路径比没有路径更容易让人误用。
  if (problems.length > 0) return { keys: [], notes, problems };
  return { keys, notes, problems };
}