// 移植自 ArtCraft（MIT / Apache-2.0 双许可）；来源与依赖声明见 ../THIRD_PARTY_NOTICES.md。
// 资产库面板（kitbashing）：从素材库拖模型进 3D 场景摆位。
import { useCallback, useEffect, useRef, useState } from "react";
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

/**
 * 缩略图块。没缩略图的素材（例：从命令行丢进素材目录的模型）
 * 不能把 <img> 指向 .glb——浏览器只会渲成空白格，所以直接给占位块；
 * 有缩略图但加载失败时同样退回占位块。
 */
function AssetThumbnail({ asset }: { asset: LibraryAsset }) {
  const [failed, setFailed] = useState(!asset.thumb_path);
  if (failed) {
    return (
      <span
        aria-hidden
        className="flex h-8 w-8 shrink-0 items-center justify-center rounded border border-white/10 bg-white/5 text-[10px] text-white/30"
      >
        3D
      </span>
    );
  }
  return (
    <img
      src={thumbnailUrl(asset)}
      alt=""
      loading="lazy"
      className="h-8 w-8 shrink-0 rounded border border-white/10 object-cover"
      onError={() => setFailed(true)}
    />
  );
}

export default function AssetLibraryPanel({ onPlace, onError }: AssetLibraryPanelProps) {
  const [items, setItems] = useState<LibraryAsset[]>([]);
  const [loading, setLoading] = useState(false);
  const [query, setQuery] = useState('');
  const [category, setCategory] = useState('');
  const requestRef = useRef(0);
  const onErrorRef = useRef(onError); onErrorRef.current = onError;
  /** 素材越存越多，面板一次只拉一页；旧后端不认这两个参数会退回全量。 */
  const [page, setPage] = useState(1);
  const [hasMore, setHasMore] = useState(false);

  const fetchPage = useCallback(async (target: number) => {
    // React 岛内不引 axios 实例，素材库直接打 fetch（3D 编辑器存入素材库也是这么做的）。
    const response = await fetch(
      `/api/materials?type=3d&page=${target}&page_size=${ASSET_PAGE_SIZE}&q=${encodeURIComponent(query)}&category=${encodeURIComponent(category)}`,
    );
    if (!response.ok) throw new Error(`读取素材库失败（HTTP ${response.status}）`);
    const payload = await response.json();
    // 后端可能直接返回数组，也可能包一层 {items: []}，两种都认。
    const list: LibraryAsset[] = Array.isArray(payload) ? payload : (payload.items ?? []);
    return list;
  }, [query, category]);

  const refresh = useCallback(async () => {
    const ticket = ++requestRef.current;
    setLoading(true);
    try {
      const list = await fetchPage(1);
      if (ticket !== requestRef.current) return;
      setItems(list);
      setPage(1);
      setHasMore(list.length >= ASSET_PAGE_SIZE);
    } catch (err) {
      if (ticket === requestRef.current) onErrorRef.current(err instanceof Error ? err.message : String(err));
    } finally {
      if (ticket === requestRef.current) setLoading(false);
    }
  }, [fetchPage]);

  const loadMore = useCallback(async () => {
    const ticket = ++requestRef.current;
    setLoading(true);
    try {
      const next = page + 1;
      const list = await fetchPage(next);
      if (ticket !== requestRef.current) return;
      // 追加而不是替换：上一页已经在列表里了。
      setItems((current) => [...current, ...list]);
      setPage(next);
      setHasMore(list.length >= ASSET_PAGE_SIZE);
    } catch (err) {
      if (ticket === requestRef.current) onErrorRef.current(err instanceof Error ? err.message : String(err));
    } finally {
      if (ticket === requestRef.current) setLoading(false);
    }
  }, [fetchPage, page]);

  // Query transitions invalidate even a response that arrives before the debounce expires.
  useEffect(() => {
    requestRef.current++; setItems([]); setPage(1); setHasMore(false); setLoading(true);
    const timer = setTimeout(() => void refresh(), 250);
    return () => { clearTimeout(timer); requestRef.current++; };
  }, [refresh]);

  return (
    <div className="space-y-1.5">
      <input aria-label="搜索全部3D素材" placeholder="搜索全部3D素材" value={query} onChange={e => { requestRef.current++; setQuery(e.target.value); }} className="w-full rounded bg-black/30 px-2 py-1 text-xs" />
      <select aria-label="素材格式分类" value={category} onChange={e => { requestRef.current++; setCategory(e.target.value); }} className="w-full rounded bg-black/30 text-xs"><option value="">全部格式</option><option value="model">网格 GLB / GLTF / OBJ</option><option value="splat">高斯泼溅</option></select>
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
                <AssetThumbnail asset={asset} />
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