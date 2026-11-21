// 剪辑导出到本平台素材库的 sink。
//
// 浏览器默认 sink 只能触发下载；这里补上「存到媒体库」：把导出产物
// POST /api/materials（type=video，可选 project_id），素材库里马上能
// 看到这条成片。磁盘下载仍交给注入的 diskSink，两边都按目的地各自
// 上报进度，UI 才能分别显示各条保存结果。
//
// 从某个分镜（Scene）打开剪辑器时，再提供「写回分镜」：把成片作为新的
// 分镜视频候选写回该场景，旧版本由后端保留为历史候选，可随时切回。

import { resolveExportDestinations } from "../video-editor/lib/export/destinations.js";

async function describeFailure(response, fallback) {
  try {
    const data = await response.json();
    if (data && data.error) return String(data.error);
  } catch {
    // 非 JSON 错误体，退回状态码信息。
  }
  return `${fallback}：${response.status}`;
}

/**
 * @param {{
 *   fetchImpl?: typeof fetch,
 *   projectId?: string | number | null,
 *   scene?: { projectId: string | number, sceneId: number } | null,
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
  // 只有带着分镜上下文（从某个 Scene 打开）时才提供「写回分镜」。
  const scene =
    options.scene && options.scene.projectId && options.scene.sceneId
      ? {
          projectId: String(options.scene.projectId),
          sceneId: Number(options.scene.sceneId),
        }
      : null;

  return {
    supportsLibrary: true,
    // 导出面板据此显示「写回分镜（另存候选）」；没有分镜上下文时不显示。
    supportsScene: scene !== null,

    async accept(artifact, sinkOptions = {}) {
      const onProgress = sinkOptions.onProgress;
      const destinations = resolveExportDestinations({
        supportsLibrary: true,
        supportsScene: scene !== null,
        saveToDisk: sinkOptions.saveToDisk !== false,
        saveToLibrary: sinkOptions.saveToLibrary === true,
        saveToScene: sinkOptions.saveToScene === true,
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
            throw new Error(await describeFailure(response, "素材入库失败"));
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

      if (destinations.scene) {
        onProgress?.({ destination: "scene", status: "pending" });
        try {
          const form = new FormData();
          // as_candidate：后端会新建分镜视频候选并设为当前，旧版本保留可切回。
          form.append("as_candidate", "1");
          form.append("file", artifact.blob, artifact.filename);
          const response = await fetchImpl(
            `/api/projects/${scene.projectId}/scenes/${scene.sceneId}/video/upload`,
            { method: "POST", body: form },
          );
          if (!response.ok) {
            throw new Error(await describeFailure(response, "写回分镜失败"));
          }
          onProgress?.({ destination: "scene", status: "success" });
        } catch (error) {
          onProgress?.({
            destination: "scene",
            status: "error",
            error: error instanceof Error ? error.message : "写回分镜失败",
          });
          throw error;
        }
      }

      return null;
    },
  };
}