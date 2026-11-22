/**
 * 3D 场景存档的请求格式与展示格式。
 *
 * 后端只存元数据，JSON 落在 data/scene-archives/<id>.json；
 * 接口见 backend/internal/service/scene_archive_handlers.go。
 */
export const SCENE_ARCHIVE_MAX_BYTES = 32 * 1024 * 1024;
export const SCENE_ARCHIVE_MAX_LABEL = "32 MB";

export interface SceneArchiveItem {
  id: number;
  projectId: number | null;
  name: string;
  size: number;
  sizeLabel: string;
  createdAt: string;
  createdLabel: string;
}

/** 存档体积：小于 1 KB 按字节，KB/MB 保留一位小数。 */
export function archiveSizeLabel(bytes: number): string {
  const value = Number.isFinite(bytes) && bytes > 0 ? bytes : 0;
  if (value < 1024) return `${Math.round(value)} B`;
  if (value < 1024 * 1024) return `${(value / 1024).toFixed(1)} KB`;
  return `${(value / (1024 * 1024)).toFixed(1)} MB`;
}

/** 把后端返回的 ISO 时间压成本地「YYYY-MM-DD HH:mm」，解析不了就留空。 */
export function archiveTimeLabel(iso: string): string {
  const ms = Date.parse(iso);
  if (!Number.isFinite(ms)) return "";
  const date = new Date(ms);
  const pad = (value: number) => String(value).padStart(2, "0");
  return `${date.getFullYear()}-${pad(date.getMonth() + 1)}-${pad(date.getDate())} ${pad(date.getHours())}:${pad(date.getMinutes())}`;
}

/**
 * 解析 GET /api/scene-archives 的响应。既认 {items:[…]}，也认裸数组；
 * 缺 name/size 的坏行直接跳过，不让一条脏数据把整个列表打空。
 */
export function parseSceneArchiveList(payload: unknown): SceneArchiveItem[] {
  const container = payload as { items?: unknown } | unknown[] | null | undefined;
  const rows = Array.isArray(container)
    ? container
    : Array.isArray((container as { items?: unknown })?.items)
      ? ((container as { items: unknown[] }).items)
      : [];
  const items: SceneArchiveItem[] = [];
  for (const raw of rows) {
    if (!raw || typeof raw !== "object") continue;
    const row = raw as Record<string, unknown>;
    const id = Number(row.id);
    if (!Number.isFinite(id) || id <= 0) continue;
    const rawName = typeof row.name === "string" ? row.name.trim() : "";
    const size = Number(row.size);
    const bytes = Number.isFinite(size) && size > 0 ? size : 0;
    const createdAt = typeof row.created_at === "string" ? row.created_at : "";
    items.push({
      id,
      projectId: typeof row.project_id === "number" ? row.project_id : null,
      name: rawName || `存档 ${id}`,
      size: bytes,
      sizeLabel: archiveSizeLabel(bytes),
      createdAt,
      createdLabel: archiveTimeLabel(createdAt),
    });
  }
  return items;
}

/** 下载/上传共用的文件名，和后端只存 id 的实现无关，仅用于展示与下载名。 */
export function sceneArchiveFileName(at: number): string {
  const ms = Number.isFinite(at) && at > 0 ? Math.round(at) : Date.now();
  return `scene-${ms}.json`;
}

/**
 * 上传前的体积闸门。浏览器端先挡一次，省得把 40 MB 传完才收到 413；
 * 返回空串表示可以传，否则是给用户看的原因。
 */
export function sceneArchiveBlocker(json: string): string {
  const bytes = new TextEncoder().encode(json).length;
  if (bytes === 0) return "场景存档是空的，先在视口里放点东西再存。";
  if (bytes > SCENE_ARCHIVE_MAX_BYTES) {
    return `场景存档 ${archiveSizeLabel(bytes)}，超过后端 ${SCENE_ARCHIVE_MAX_LABEL} 上限：删掉几个对象再存。`;
  }
  return "";
}

/**
 * 组 multipart body：file 字段带 JSON 正文，name / project_id 作为普通字段。
 * 后端优先用 file 的 filename，但显式带上 name 才能在文件名被浏览器改写时保持一致。
 */
export function sceneArchiveFormBody(
  json: string,
  name: string,
  projectId?: number | null,
): FormData {
  const fileName = name || sceneArchiveFileName(Date.now());
  const form = new FormData();
  form.append("file", new Blob([json], { type: "application/json" }), fileName);
  form.append("name", fileName);
  if (typeof projectId === "number" && Number.isFinite(projectId) && projectId > 0) {
    form.append("project_id", String(projectId));
  }
  return form;
}