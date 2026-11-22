// 移植自 ArtCraft（MIT / Apache-2.0 双许可）；来源与依赖声明见 ../THIRD_PARTY_NOTICES.md。
// 资产库面板（kitbashing）：从素材库拖模型进 3D 场景摆位。
import { useCallback, useEffect, useState } from "react";
import {
  ASSET_DRAG_MIME,
  assetKind,
  isSupportedAsset,
  thumbnailUrl,
  type LibraryAsset,
} from "./assetLibrary.js";

export interface AssetLibraryPanelProps {
  /** 点「放入场景」时调用：把资产放到原点，用户再拖 gizmo 摆位。 */
  onPlace: (asset: LibraryAsset) => void;
  onError: (message: string) => void;
}

/** 一次拉多少个素材。24 大致是一屏半，再多就让人滚得心烦。 */
const ASSET_PAGE_SIZE = 24;

export default function AssetLibraryPanel({ onPlace, onError }: AssetLibraryPanelProps) {
  const [items, setItems] = useState<LibraryAsset[]>([]);
  const [loading, setLoading] = useState(false);
  const [loaded, setLoaded] = useState(false);
  /** 素材越存越多，面板一次只拉一页；旧后端不认这两个参数会退回全量。 */
  const [page, setPage] = useState(1);
  const [hasMore, setHasMore] = useState(false);

  const fetchPage = useCallback(async (target: number) => {
    // React 岛内不引 axios 实例，素材库直接打 fetch（3D 编辑器存入素材库也是这么做的）。
    const response = await fetch(
      `/api/materials?type=3d&page=${target}&page_size=${ASSET_PAGE_SIZE}`,
    );
    if (!response.ok) throw new Error(`读取素材库失败（HTTP ${response.status}）`);
    const payload = await response.json();
    // 后端可能直接返回数组，也可能包一层 {items: []}，两种都认。
    const list: LibraryAsset[] = Array.isArray(payload) ? payload : (payload.items ?? []);
    return list;
  }, []);

  const refresh = useCallback(async () => {
    setLoading(true);
    try {
      const list = await fetchPage(1);
      setItems(list);
      setPage(1);
      setHasMore(list.length >= ASSET_PAGE_SIZE);
      setLoaded(true);
    } catch (err) {
      onError(err instanceof Error ? err.message : String(err));
    } finally {
      setLoading(false);
    }
  }, [fetchPage, onError]);

  const loadMore = useCallback(async () => {
    setLoading(true);
    try {
      const next = page + 1;
      const list = await fetchPage(next);
      // 追加而不是替换：上一页已经在列表里了。
      setItems((current) => [...current, ...list]);
      setPage(next);
      setHasMore(list.length >= ASSET_PAGE_SIZE);
    } catch (err) {
      onError(err instanceof Error ? err.message : String(err));
    } finally {
      setLoading(false);
    }
  }, [fetchPage, onError, page]);

  // 首次展开时拉一次；之后靠「刷新」按钮。
  useEffect(() => {
    if (!loaded && !loading) void refresh();
  }, [loaded, loading, refresh]);

  return (
    <div className="space-y-1.5">
      <div className="flex items-center justify-between px-1">
        <span className="text-[11px] text-white/40">
          {loading ? "正在读取…" : `已加载 ${items.length} 个 3D 素材`}
        </span>
        <button
          type="button"
          onClick={() => void refresh()}
          disabled={loading}
          className="rounded px-1.5 py-0.5 text-[11px] text-white/50 hover:bg-white/10 disabled:opacity-40"
        >
          刷新
        </button>
      </div>
      {items.length === 0 && !loading ? (
        <p className="px-1 text-[11px] leading-4 text-white/35">
          素材库里还没有 3D 模型。先在 3D 编辑器里「存入素材库」，或到素材库页上传 GLB。
        </p>
      ) : (
        <div className="max-h-56 space-y-1 overflow-y-auto pr-0.5">
          {items.map((asset) => {
            const supported = isSupportedAsset(asset.path || asset.name);
            return (
              <div
                key={asset.id}
                draggable={supported}
                onDragStart={(event) => {
                  if (!supported) return;
                  event.dataTransfer.effectAllowed = "copy";
                  event.dataTransfer.setData(ASSET_DRAG_MIME, JSON.stringify(asset));
                }}
                className={`flex items-center gap-2 rounded-md px-1.5 py-1 text-xs ${
                  supported
                    ? "cursor-grab text-white/75 hover:bg-white/10 active:cursor-grabbing"
                    : "cursor-not-allowed text-white/30"
                }`}
                title={
                  supported
                    ? "拖到画面里摆放，或点「放入」放到原点"
                    : `暂不支持 ${assetKind(asset.path || asset.name) === "unsupported" ? "这个格式" : assetKind(asset.path || asset.name)}，请先转成 GLB`
                }
              >
                <img
                  src={thumbnailUrl(asset)}
                  alt=""
                  loading="lazy"
                  className="h-8 w-8 shrink-0 rounded border border-white/10 object-cover"
                  onError={(event) => {
                    (event.currentTarget as HTMLImageElement).style.visibility = "hidden";
                  }}
                />
                <span className="min-w-0 flex-1 truncate">{asset.name}</span>
                {supported ? (
                  <button
                    type="button"
                    onClick={() => onPlace(asset)}
                    className="shrink-0 rounded bg-white/5 px-1.5 py-0.5 text-[11px] text-white/60 hover:bg-white/15"
                  >
                    放入
                  </button>
                ) : (
                  <span className="shrink-0 text-[10px] text-white/25">需转 GLB</span>
                )}
              </div>
            );
          })}
          {hasMore ? (
            <button
              type="button"
              onClick={() => void loadMore()}
              disabled={loading}
              className="w-full rounded-md bg-white/5 px-1.5 py-1 text-[11px] text-white/60 hover:bg-white/15 disabled:opacity-40"
            >
              {loading ? "正在读取…" : "加载更多"}
            </button>
          ) : null}
        </div>
      )}
    </div>
  );
}