// One-shot hand-off from a Vue route (task result, material card) to the
// video editor island. sessionStorage survives the router navigation but
// not a new tab; the editor removes the entry as soon as it reads it so a
// refresh does not import the same clip twice.
const KEY = "studio.initialMedia";

export function stashStudioMedia(entries) {
  const media = (entries || []).filter((entry) => entry && entry.url && entry.name);
  if (media.length === 0) return false;
  sessionStorage.setItem(KEY, JSON.stringify(media));
  return true;
}

export function takeStudioMedia() {
  const raw = sessionStorage.getItem(KEY);
  if (!raw) return [];
  sessionStorage.removeItem(KEY);
  try {
    const parsed = JSON.parse(raw);
    return Array.isArray(parsed) ? parsed : [];
  } catch {
    return [];
  }
}

// 分镜上下文：编辑器要往哪个 Scene 写回、退出后回到哪个路由。
// 与媒体分开存，避免「只带上下文不带素材」和「只带素材」的调用互相覆盖。
// mode/sceneImage：画布写回分镜图；mode/frame + frameId：画布写回上一镜衔接帧；
// imageUrl 是画布打开时要载入的原始画面。
const SCENE_KEY = "studio.sceneContext";

export function stashStudioScene(context) {
  if (!context || !context.sceneId || !context.projectId) return false;
  const mode = context.mode === "frame" || context.mode === "sceneImage" ? context.mode : "";
  sessionStorage.setItem(
    SCENE_KEY,
    JSON.stringify({
      projectId: String(context.projectId),
      sceneId: Number(context.sceneId),
      episodeN: context.episodeN != null ? Number(context.episodeN) : null,
      sceneLabel: context.sceneLabel ? String(context.sceneLabel) : "",
      returnTo: context.returnTo ? String(context.returnTo) : "",
      mode,
      frameId: mode === "frame" && context.frameId ? Number(context.frameId) : null,
      imageUrl: context.imageUrl ? String(context.imageUrl) : "",
    }),
  );
  return true;
}

export function takeStudioScene() {
  const raw = sessionStorage.getItem(SCENE_KEY);
  if (!raw) return null;
  sessionStorage.removeItem(SCENE_KEY);
  try {
    const parsed = JSON.parse(raw);
    if (!parsed || !parsed.projectId || !parsed.sceneId) return null;
    if (parsed.frameId == null) parsed.frameId = null;
    if (typeof parsed.imageUrl !== "string") parsed.imageUrl = "";
    return parsed;
  } catch {
    return null;
  }
}