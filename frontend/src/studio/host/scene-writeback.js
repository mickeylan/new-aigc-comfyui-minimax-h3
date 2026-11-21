// 画布写回分镜：同一个浏览器里的 2D 画布结果要变成场景的候选媒体。
// 这里只负责协议（multipart 字段、错误提取），DOM/Blob 由调用方给，
// 方便在 node:test 里用假 fetch 断言请求体。
const DEFAULT_IMAGE_NAME = "canvas.png";

async function readError(response) {
  try {
    const payload = await response.json();
    if (payload && payload.error) return String(payload.error);
  } catch {
    // 非 JSON 响应：退回状态码说明。
  }
  return `写回失败（HTTP ${response.status}）`;
}

function fileFrom(blob, filename) {
  if (typeof File === "function") return new File([blob], filename, { type: blob.type || "image/png" });
  return blob;
}

/**
 * 根据分镜上下文生成写回函数；上下文不完整时返回 null（画布不显示写回按钮）。
 * - mode=sceneImage（默认）：POST /scenes/:sid/image/upload，as_candidate=1，另存候选。
 * - mode=frame：POST /scenes/:sid/continuity/frames/replace，替换当前选中的衔接帧
 *   （后端保留 original_image_file，可回看原图）。
 */
export function buildSceneWriteback(context, { fetchImpl, apiBase = "/api" } = {}) {
  if (!context || !context.projectId || !context.sceneId) return null;
  const doFetch = fetchImpl || ((...args) => fetch(...args));
  const pid = encodeURIComponent(String(context.projectId));
  const sid = encodeURIComponent(String(context.sceneId));

  if (context.mode === "frame") {
    if (!context.frameId) return null;
    return async (blob, filename = DEFAULT_IMAGE_NAME) => {
      const form = new FormData();
      form.append("file", fileFrom(blob, filename));
      const response = await doFetch(`${apiBase}/projects/${pid}/scenes/${sid}/continuity/frames/replace`, {
        method: "POST",
        body: form,
      });
      if (!response.ok) throw new Error(await readError(response));
      const payload = await response.json().catch(() => ({}));
      return payload.message || "已替换衔接帧（原图保留）";
    };
  }

  return async (blob, filename = DEFAULT_IMAGE_NAME) => {
    const form = new FormData();
    form.append("file", fileFrom(blob, filename));
    form.append("as_candidate", "1");
    const response = await doFetch(`${apiBase}/projects/${pid}/scenes/${sid}/image/upload`, {
      method: "POST",
      body: form,
    });
    if (!response.ok) throw new Error(await readError(response));
    const payload = await response.json().catch(() => ({}));
    return payload.message || "已保存为新的分镜图候选";
  };
}