// 剪辑导出到本平台素材库的 sink。
//
// 浏览器默认 sink 只能触发下载；这里补上「存到媒体库」：把导出产物
// POST /api/materials（type=video，可选 project_id），素材库里马上能
// 看到这条成片。磁盘下载仍交给注入的 diskSink，两边都按目的地各自
// 上报进度，UI 才能分别显示「Saved to disk / Saved to media library」。

import { resolveExportDestinations } from "../video-editor/lib/export/destinations.js";

/**
 * @param {{
 *   fetchImpl?: typeof fetch,
 *   projectId?: string | number | null,
 *   diskSink: { accept: (artifact: any, options?: any) => Promise<any> },
 *   endpoint?: string,
 * }} options
 */
export function createComfyExportSink(options) {
  const { diskSink } = options;
  if (!diskSink || typeof diskSink.accept !== "function") {
    throw new Error("createComfyExportSink 需要 diskSink");
  }
  const fetchImpl = options.fetchImpl ?? ((...args) => globalThis.fetch(...args));
  const endpoint = options.endpoint ?? "/api/materials";
  // 编辑器在没有路由参数时会生成本地 id（local-<uuid>），后端只认数字主键；
  // 非数字一律不入库到项目，素材仍然会进素材库，只是不挂项目。
  const rawProjectId = options.projectId;
  const projectId =
    typeof rawProjectId === "number"
      ? String(rawProjectId)
      : /^\d+$/.test(String(rawProjectId ?? ""))
        ? String(rawProjectId)
        : null;

  return {
    supportsLibrary: true,

    async accept(artifact, sinkOptions = {}) {
      const onProgress = sinkOptions.onProgress;
      const destinations = resolveExportDestinations({
        supportsLibrary: true,
        saveToDisk: sinkOptions.saveToDisk !== false,
        saveToLibrary: sinkOptions.saveToLibrary === true,
      });

      if (destinations.disk) {
        await diskSink.accept(artifact, {
          saveToDisk: true,
          saveToLibrary: false,
          onProgress,
        });
      }

      if (destinations.library) {
        onProgress?.({ destination: "library", status: "pending" });
        try {
          const form = new FormData();
          form.append("type", "video");
          if (projectId) form.append("project_id", String(projectId));
          form.append("file", artifact.blob, artifact.filename);
          const response = await fetchImpl(endpoint, {
            method: "POST",
            body: form,
          });
          if (!response.ok) {
            throw new Error(`素材入库失败：${response.status}`);
          }
          onProgress?.({ destination: "library", status: "success" });
        } catch (error) {
          onProgress?.({
            destination: "library",
            status: "error",
            error: error instanceof Error ? error.message : "素材入库失败",
          });
          throw error;
        }
      }

      return null;
    },
  };
}