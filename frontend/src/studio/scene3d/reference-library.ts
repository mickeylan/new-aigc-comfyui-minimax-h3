/**
 * 3D 参考图（白模 / 法线 / 深度）存素材库时的命名与限额。
 *
 * 只算字符串与计数，能被 node --test 直接跑；上传本身留在编辑器里
 * （POST /api/materials，type=image），后端不用改。
 */

/** 序列一次最多存多少帧进素材库：素材库是平铺图片列表，几百帧会把库刷爆。 */
export const SEQUENCE_LIBRARY_MAX_FRAMES = 60;

/** 文件名里的时间戳：20261122-171234（本地时间，不含冒号/空格）。 */
export function fileNameStamp(date: Date): string {
  const pad = (value: number) => String(value).padStart(2, "0");
  return (
    `${date.getFullYear()}${pad(date.getMonth() + 1)}${pad(date.getDate())}` +
    `-${pad(date.getHours())}${pad(date.getMinutes())}${pad(date.getSeconds())}`
  );
}

/** 单帧参考图：`clay-20261122-171234.png`。pass 为空时退回 `ref`。 */
export function referenceFileName(pass: string, stamp: string): string {
  const label = pass.trim() || "ref";
  return `${label}-${stamp}.png`;
}

/** 序列里的一帧：`clay-seq-20261122-171234-007.png`（帧号三位，和 zip 内一致）。 */
export function sequenceFrameFileName(pass: string, stamp: string, index: number): string {
  const label = pass.trim() || "ref";
  const frame = String(Math.max(0, Math.round(index))).padStart(3, "0");
  return `${label}-seq-${stamp}-${frame}.png`;
}

/** 序列能不能整段存库；不能就返回要告诉用户的原因。 */
export function sequenceLibraryBlocker(count: number, cap = SEQUENCE_LIBRARY_MAX_FRAMES): string | null {
  if (count <= 0) return "序列还没有帧，先导出一次";
  if (count > cap) {
    return `序列有 ${count} 帧，超过一次存库上限 ${cap} 帧：先换低帧率或缩短机位路径再存`;
  }
  return null;
}