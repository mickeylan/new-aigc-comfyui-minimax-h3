// 素材库 → 3D 背景（图片平面 / 天空盒）的纯逻辑。
// 「图片平面」「导入天空盒」原先只能走 <input type=file>，脚本化和远程操作都进不去；
// 这里把「从素材库挑一张图当背景」抽成可测的纯函数，接线在 Scene3DStage.tsx。

/** 素材库里的一张图（只取用得上的字段）。 */
export interface LibraryImage {
  id: number;
  name: string;
  path: string;
}

const IMAGE_EXTENSIONS = [".png", ".jpg", ".jpeg", ".webp", ".avif", ".gif"];

/** 兜底：老后端不带 type 字段时，按扩展名判断能不能当纹理。 */
export function looksLikeImage(nameOrPath: string) {
  const trimmed = nameOrPath.split("?")[0].toLowerCase();
  return IMAGE_EXTENSIONS.some((extension) => trimmed.endsWith(extension));
}

/**
 * 素材 path 形如「materials/obj.png」，后端把字节挂在 /api/input 下；
 * 只切第一个斜杠，文件名里再有斜杠也不影响。
 */
export function materialUrl(path: string) {
  const trimmed = String(path ?? "").replace(/^[\\/]+/, "");
  if (!trimmed) return "";
  const slash = trimmed.indexOf("/");
  if (slash < 0) return `/api/input/${trimmed}`;
  return `/api/input/${trimmed.slice(0, slash)}/${trimmed.slice(slash + 1)}`;
}

interface MaterialRow {
  id?: unknown;
  name?: unknown;
  path?: unknown;
  type?: unknown;
}

function toImage(row: MaterialRow): LibraryImage | null {
  if (!row || typeof row !== "object") return null;
  const name = typeof row.name === "string" ? row.name.trim() : "";
  const path = typeof row.path === "string" ? row.path.trim() : "";
  if (!name || !path) return null;
  // 有 type 就按 type 判；没有 type（老后端）才退回扩展名。
  if (typeof row.type === "string" && row.type) {
    if (row.type !== "image") return null;
  } else if (!looksLikeImage(path || name)) {
    return null;
  }
  const id = typeof row.id === "number" && Number.isFinite(row.id) ? row.id : 0;
  return { id, name, path };
}

/**
 * 把 /api/materials 的响应整理成下拉选项：只留图片、按 id 倒序（新存的在前）、
 * 同名只留一张、最多 limit 条。后端可能直接返回数组，也可能包一层 {items: []}。
 */
export function imageMaterialOptions(payload: unknown, limit = 40) {
  const rows: MaterialRow[] = Array.isArray(payload)
    ? (payload as MaterialRow[])
    : ((payload as { items?: MaterialRow[] } | null)?.items ?? []);
  const seen = new Set<string>();
  const options: LibraryImage[] = [];
  for (const row of rows) {
    const image = toImage(row);
    if (!image || seen.has(image.name)) continue;
    seen.add(image.name);
    options.push(image);
  }
  options.sort((left, right) => right.id - left.id);
  return Number.isFinite(limit) && limit > 0 ? options.slice(0, limit) : options;
}

/** 图片平面进「场景对象」后显示的名字：带上「平面」前缀，和 GLB 名区分开。 */
export function imagePlaneName(image: LibraryImage) {
  const base = String(image?.name ?? "").trim();
  return base ? `planar-${base}` : "planar-image";
}

/** 天空盒标签：全景图和普通照片都可能被选中，名字原样展示，避免谎称是等距柱状图。 */
export function skyboxLabel(image: LibraryImage) {
  const base = String(image?.name ?? "").trim();
  return base || "素材库图片";
}

/** 下拉框选中值 → 图片（找不到返回 null，避免把「请选择」当成一张图）。 */
export function findImage(options: LibraryImage[], value: string | number | null | undefined) {
  const id = Number(value);
  if (!Number.isFinite(id) || id <= 0) return null;
  return options.find((image) => image.id === id) ?? null;
}