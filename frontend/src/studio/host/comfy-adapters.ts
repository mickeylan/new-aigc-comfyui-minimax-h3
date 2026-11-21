import {
  createDefaultAdapters,
  type AssetGalleryAdapter,
  type MediaHandle,
  type MediaKind,
  type MediaPickerSelection,
  type MediaSourceAdapter,
  type ResolvedMedia,
  type VideoEditorAdapters,
} from "../video-editor/index";
import { api } from "../../api";
import { createComfyExportSink } from "./export-sink.js";
import {
  parseRunResults,
  runResultKind,
  runResultUrl,
} from "../../utils/playgroundResults.js";

/**
 * Media handles minted by this host carry their resolved URL and mime
 * inline: `comfy|<urlencoded-url>|<mime>`. The editor stores handles in
 * the project document, so the URL has to stay valid across reloads;
 * blob URLs from the default adapter are only good for the current tab.
 */
const HANDLE_PREFIX = "comfy|";

interface GalleryItem {
  handle: MediaHandle;
  name: string;
  kind: MediaKind;
  url: string;
  source: string;
  createdAt: number;
}

export function createComfyAdapters(
  options: { projectId?: string; scene?: { projectId: string | number; sceneId: number } | null } = {},
): VideoEditorAdapters {
  const base = createDefaultAdapters();
  return {
    ...base,
    mediaSource: createComfyMediaSource(base.mediaSource),
    assetGallery: createComfyAssetGallery(),
    // 磁盘下载沿用默认 sink，另加「存到素材库」→ POST /api/materials
    // 与「写回分镜」→ POST /api/projects/:id/scenes/:sid/video/upload。
    exportSink: createComfyExportSink({
      diskSink: base.exportSink,
      projectId: options.projectId,
      scene: options.scene ?? null,
    }),
  };
}

export function createComfyMediaSource(
  base: MediaSourceAdapter,
): MediaSourceAdapter {
  return {
    async resolveMedia(handle) {
      const inline = parseHandle(handle.id);
      if (inline) {
        return inline;
      }
      return base.resolveMedia(handle);
    },
    uploadLocalFile(file) {
      return base.uploadLocalFile(file);
    },
    releaseResolved(resolved) {
      // HTTP-backed URLs must not be revoked; only hand blob URLs that
      // the wrapped local-file adapter created back to it.
      if (resolved.url.startsWith("blob:")) {
        base.releaseResolved?.(resolved);
      }
    },
    deleteHandle(handle) {
      return base.deleteHandle?.(handle) ?? Promise.resolve();
    },
  };
}

export function createComfyAssetGallery(): AssetGalleryAdapter {
  return {
    openPicker({ kinds }) {
      return openComfyPicker(kinds);
    },
  };
}

function parseHandle(id: string): ResolvedMedia | null {
  if (!id.startsWith(HANDLE_PREFIX)) {
    return null;
  }
  const rest = id.slice(HANDLE_PREFIX.length);
  const separator = rest.lastIndexOf("|");
  if (separator < 0) {
    return null;
  }
  return {
    url: decodeURIComponent(rest.slice(0, separator)),
    mime: rest.slice(separator + 1),
  };
}

function makeHandle(url: string, mime: string, kind: MediaKind): MediaHandle {
  return { id: `${HANDLE_PREFIX}${encodeURIComponent(url)}|${mime}`, kind };
}

function normalizeKind(value: unknown): MediaKind | null {
  const text = String(value || "").toLowerCase();
  if (text.startsWith("video")) return "video";
  if (text.startsWith("audio")) return "audio";
  if (text.startsWith("image")) return "image";
  return null;
}

export function mimeForMedia(kind: MediaKind, filename: string): string {
  const ext = String(filename).split(".").pop()?.toLowerCase() || "";
  if (kind === "video") {
    if (ext === "webm") return "video/webm";
    if (ext === "mov") return "video/quicktime";
    if (ext === "mkv") return "video/x-matroska";
    return "video/mp4";
  }
  if (kind === "audio") {
    if (ext === "wav") return "audio/wav";
    if (ext === "mp3") return "audio/mpeg";
    if (ext === "ogg") return "audio/ogg";
    if (ext === "flac") return "audio/flac";
    if (ext === "m4a") return "audio/mp4";
    return "audio/mpeg";
  }
  if (ext === "jpg" || ext === "jpeg") return "image/jpeg";
  if (ext === "webp") return "image/webp";
  if (ext === "gif") return "image/gif";
  if (ext === "avif") return "image/avif";
  return "image/png";
}

async function loadGalleryItems(kinds: MediaKind[]): Promise<{
  items: GalleryItem[];
  problems: string[];
}> {
  const problems: string[] = [];
  const items: GalleryItem[] = [];
  const seen = new Set<string>();

  const [materialsResult, tasksResult] = await Promise.allSettled([
    api.materials(),
    api.tasks({ limit: 80 }),
  ]);

  if (materialsResult.status === "fulfilled") {
    const payload = materialsResult.value?.data;
    const rows = Array.isArray(payload) ? payload : payload?.items ?? [];
    for (const row of rows) {
      const kind = normalizeKind(row?.type);
      if (!kind || !kinds.includes(kind) || !row?.path) continue;
      const url = api.materialUrl(row.path);
      if (seen.has(url)) continue;
      seen.add(url);
      items.push({
        handle: makeHandle(url, mimeForMedia(kind, row.name || row.path), kind),
        name: row.name || String(row.path).split("/").pop() || "素材",
        kind,
        url,
        source: row.project_id ? "素材库 · 项目素材" : "素材库",
        createdAt: Date.parse(row.created_at) || 0,
      });
    }
  } else {
    problems.push("素材库加载失败");
  }

  if (tasksResult.status === "fulfilled") {
    const payload = tasksResult.value?.data;
    const rows = Array.isArray(payload)
      ? payload
      : payload?.items ?? payload?.tasks ?? payload?.data ?? [];
    for (const task of rows) {
      if (task?.status && task.status !== "success") continue;
      const files = parseRunResults(task?.result_files);
      for (const file of files) {
        const kind = runResultKind(file);
        if (kind !== "video" && kind !== "image" && kind !== "audio") continue;
        if (!kinds.includes(kind)) continue;
        const url = runResultUrl(task, file);
        if (!url || seen.has(url)) continue;
        seen.add(url);
        const filename = String(file?.filename || "").split("/").pop() || "结果";
        items.push({
          handle: makeHandle(url, mimeForMedia(kind, filename), kind),
          name: filename,
          kind,
          url,
          source: task?.template_name ? `生成结果 · ${task.template_name}` : "生成结果",
          createdAt: Date.parse(task?.created_at) || 0,
        });
      }
    }
  } else {
    problems.push("生成结果加载失败");
  }

  items.sort((a, b) => b.createdAt - a.createdAt);
  return { items, problems };
}

function openComfyPicker(kinds: MediaKind[]): Promise<MediaPickerSelection[]> {
  return new Promise((resolve) => {
    const selected = new Map<string, GalleryItem>();

    const overlay = document.createElement("div");
    overlay.className = "comfy-gallery-overlay";
    overlay.innerHTML = `
      <div class="comfy-gallery-panel" role="dialog" aria-modal="true" aria-label="从 ComfyStudio 导入素材">
        <div class="comfy-gallery-head">
          <div>
            <div class="comfy-gallery-title">从 ComfyStudio 导入</div>
            <div class="comfy-gallery-sub">生成结果与素材库中的视频、图片、音频</div>
          </div>
          <button type="button" class="comfy-gallery-close" aria-label="关闭">×</button>
        </div>
        <div class="comfy-gallery-body">
          <div class="comfy-gallery-loading">正在加载素材…</div>
        </div>
        <div class="comfy-gallery-foot">
          <span class="comfy-gallery-count">未选择素材</span>
          <div class="comfy-gallery-actions">
            <button type="button" class="comfy-gallery-cancel">取消</button>
            <button type="button" class="comfy-gallery-ok" disabled>导入</button>
          </div>
        </div>
      </div>
    `;

    document.body.appendChild(overlay);

    const body = overlay.querySelector(".comfy-gallery-body") as HTMLElement;
    const count = overlay.querySelector(".comfy-gallery-count") as HTMLElement;
    const confirm = overlay.querySelector(".comfy-gallery-ok") as HTMLButtonElement;
    const closeButton = overlay.querySelector(".comfy-gallery-close") as HTMLButtonElement;
    const cancelButton = overlay.querySelector(".comfy-gallery-cancel") as HTMLButtonElement;

    const finish = (result: MediaPickerSelection[]) => {
      document.removeEventListener("keydown", onKeyDown);
      overlay.remove();
      resolve(result);
    };

    const onKeyDown = (event: KeyboardEvent) => {
      if (event.key === "Escape") finish([]);
    };
    document.addEventListener("keydown", onKeyDown);

    const refreshCount = () => {
      const total = selected.size;
      count.textContent = total ? `已选择 ${total} 项` : "未选择素材";
      confirm.disabled = total === 0;
      confirm.textContent = total ? `导入 ${total} 项` : "导入";
    };

    const toggle = (item: GalleryItem, card: HTMLElement) => {
      if (selected.has(item.url)) {
        selected.delete(item.url);
        card.classList.remove("is-selected");
      } else {
        selected.set(item.url, item);
        card.classList.add("is-selected");
      }
      refreshCount();
    };

    const makeCard = (item: GalleryItem) => {
      const card = document.createElement("button");
      card.type = "button";
      card.className = "comfy-gallery-card";
      card.title = `${item.name}\n${item.source}`;

      const thumb = document.createElement("div");
      thumb.className = "comfy-gallery-thumb";
      if (item.kind === "image") {
        const image = document.createElement("img");
        image.loading = "lazy";
        image.src = item.url;
        image.alt = "";
        thumb.appendChild(image);
      } else if (item.kind === "video") {
        const video = document.createElement("video");
        video.src = item.url;
        video.muted = true;
        video.playsInline = true;
        video.preload = "metadata";
        thumb.appendChild(video);
        const badge = document.createElement("span");
        badge.className = "comfy-gallery-badge";
        badge.textContent = "VIDEO";
        thumb.appendChild(badge);
      } else {
        const badge = document.createElement("span");
        badge.className = "comfy-gallery-audio";
        badge.textContent = "♪";
        thumb.appendChild(badge);
      }

      const name = document.createElement("div");
      name.className = "comfy-gallery-name";
      name.textContent = item.name;

      const source = document.createElement("div");
      source.className = "comfy-gallery-source";
      source.textContent = item.source;

      card.append(thumb, name, source);
      return card;
    };

    const makeSection = (title: string, items: GalleryItem[]) => {
      if (items.length === 0) return null;
      const section = document.createElement("section");
      section.className = "comfy-gallery-section";
      const heading = document.createElement("div");
      heading.className = "comfy-gallery-section-title";
      heading.textContent = `${title} · ${items.length}`;
      section.appendChild(heading);

      const grid = document.createElement("div");
      grid.className = "comfy-gallery-grid";
      for (const item of items) {
        const card = makeCard(item);
        card.addEventListener("click", () => toggle(item, card));
        grid.appendChild(card);
      }
      section.appendChild(grid);
      return section;
    };

    const render = (items: GalleryItem[], problems: string[]) => {
      body.textContent = "";
      if (problems.length) {
        const warning = document.createElement("div");
        warning.className = "comfy-gallery-warning";
        warning.textContent = problems.join("；");
        body.appendChild(warning);
      }
      if (items.length === 0) {
        const empty = document.createElement("div");
        empty.className = "comfy-gallery-loading";
        empty.textContent = "没有可导入的素材。先在 ComfyStudio 里生成或上传文件。";
        body.appendChild(empty);
        return;
      }
      const generated = items.filter((item) => item.source.startsWith("生成结果"));
      const library = items.filter((item) => !item.source.startsWith("生成结果"));
      const sections = [
        makeSection("生成结果", generated),
        makeSection("素材库", library),
      ].filter(Boolean) as HTMLElement[];
      for (const section of sections) body.appendChild(section);
    };

    closeButton.addEventListener("click", () => finish([]));
    cancelButton.addEventListener("click", () => finish([]));
    overlay.addEventListener("click", (event) => {
      if (event.target === overlay) finish([]);
    });
    confirm.addEventListener("click", () => {
      finish([...selected.values()].map((item) => ({ handle: item.handle, name: item.name })));
    });

    loadGalleryItems(kinds)
      .then(({ items, problems }) => render(items, problems))
      .catch((error) => {
        console.error("ComfyStudio gallery failed:", error);
        render([], ["无法连接 ComfyStudio 后端"]);
      });
  });
}