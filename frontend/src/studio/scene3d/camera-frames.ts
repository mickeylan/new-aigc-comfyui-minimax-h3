/**
 * 首尾帧：3D 编辑器按机位路径的两端渲两帧「视口原样」画面，交给 H3 的
 * 首尾帧模板（minimax_h3_first_last）当起幅/落幅。
 *
 * 为什么不再走 ref_videos：H3 的 ref_videos 是 IMAGE 帧直接进 MiniMaxH3ReferenceToVideo，
 * 实测会把参考片自己的画面一起搬进成片（第 2 秒起整段变成白模那种灰板子）。
 * 首尾帧只传两张定妆图，中间的运动交给模型，泄漏从结构上消失。
 *
 * 这里只放纯数据（能在 node --test 里跑）；取图与上传留在编辑器里。
 */

export interface MovableCamera {
  /** 机位世界坐标 */
  position: [number, number, number];
  /** 看向的点 */
  target: [number, number, number];
  /** 视角（度），越小越「推」 */
  fov: number;
}

/** 起幅 = 第一个机位，落幅 = 最后一个机位：中间那些机位只影响路径，不影响两端画面。 */
export interface FirstLastFrames<T extends MovableCamera> {
  first: T;
  last: T;
}

/**
 * 取机位路径的两端。
 *
 * 少于两个机位就没有「从哪到哪」，返回 null 让调用方提示用户先记录机位；
 * 只读不复制：两端本来就是用户记下的机位对象，调用方拿到的是同一份数据。
 */
export function firstLastCameraFrames<T extends MovableCamera>(keys: T[] | null | undefined): FirstLastFrames<T> | null {
  if (!Array.isArray(keys) || keys.length < 2) return null;
  return { first: keys[0], last: keys[keys.length - 1] };
}

/** 首尾帧文件名：first/last 一眼能看出哪张是起幅。 */
export function cameraFrameFileName(edge: "first" | "last", stamp: string): string {
  const safe = String(stamp ?? "").trim() || "frame";
  return `camera-${edge}-${safe}.png`;
}