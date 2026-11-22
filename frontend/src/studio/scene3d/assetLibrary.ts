// 移植自 ArtCraft（MIT / Apache-2.0 双许可）；来源与依赖声明见 ../THIRD_PARTY_NOTICES.md。
// 资产库（kitbashing）的纯逻辑：路径拼装、格式判定、拖放 MIME。
// 不碰 DOM 与 three，便于单元测试；面板与场景接线分别在 AssetLibraryPanel.tsx / Scene3DStage.tsx。

/** 自定义拖放类型：只在 3D 编辑器内部传递，避免和浏览器「拖文件进来」混淆。 */
export const ASSET_DRAG_MIME = "application/x-studio-3d-asset";

export interface LibraryAsset {
  id: number;
  name: string;
  path: string;
  thumb_path?: string;
  type?: string;
}

/**
 * 素材 path 可能是「子目录/文件名」也可能是纯文件名；后端把字节挂在 /api/input 下。
 * 只切第一个斜杠，文件名里再有斜杠也不影响。
 */
export function assetUrl(path: string) {
  const trimmed = path.replace(/^[\\/]+/, "");
  const slash = trimmed.indexOf("/");
  if (slash < 0) return `/api/input/${trimmed}`;
  return `/api/input/${trimmed.slice(0, slash)}/${trimmed.slice(slash + 1)}`;
}

/** 缩略图优先用后端截的那一帧，没有就退回模型文件本身（浏览器会当图片加载失败处理）。 */
export function thumbnailUrl(asset: LibraryAsset) {
  return assetUrl(asset.thumb_path || asset.path);
}

export type AssetKind = "glb" | "gltf" | "obj" | "splat" | "unsupported";

/**
 * 3D 编辑器直接吃的格式：模型 glb/gltf/obj，高斯泼溅 spz/splat/ksplat（Spark 自己解码）。
 * fbx 与网格 .ply 仍需先转 GLB；.ply 不进 splat 是因为普通网格 PLY 会被 Spark 当泼溅解错。
 * 面板上要明说而不是静默失败。
 */
export function assetKind(nameOrPath: string): AssetKind {
  const extension = nameOrPath.split("?")[0].split(".").pop()?.toLowerCase();
  if (extension === "glb") return "glb";
  if (extension === "gltf") return "gltf";
  if (extension === "obj") return "obj";
  if (extension === "spz" || extension === "splat" || extension === "ksplat") return "splat";
  return "unsupported";
}

export const SUPPORTED_ASSET_KINDS: AssetKind[] = ["glb", "gltf", "obj", "splat"];

export function isSupportedAsset(nameOrPath: string) {
  return assetKind(nameOrPath) !== "unsupported";
}