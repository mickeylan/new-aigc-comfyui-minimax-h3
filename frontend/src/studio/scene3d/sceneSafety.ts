/**
 * 场景脏标记指纹。
 *
 * 500ms 轮询不能重新序列化内嵌媒体：图片/视频平面带的是 base64 data URL（可达数 MiB），
 * 泼溅带的是 blob URL。媒体字节在图层创建时写入一次，之后没有就地改写的入口，
 * 所以轮询只记录媒体来源本身，按值比较；未修改时两侧是同一个字符串实例，
 * V8 会先比引用，不搬运正文。
 *
 * 完整序列化仍然保留给保存、离开页面、换场景这些一次性闸门；
 * 轮询只走 structure + media，不拼接媒体正文。
 */
export interface SceneState {
  /** 媒体正文换成字节长度后的结构序列化结果。 */
  structure: string;
  /** 按对象顺序排列的媒体来源；只比较值，不复制、不重新序列化正文。 */
  media: (string | null | undefined)[];
}

export interface SceneFingerprint extends SceneState {
  /** 含媒体正文的完整序列化。仅用于保存/离开闸门，首次调用后才真正拼接。 */
  full: () => string;
}

/** 一行对象快照。`source` 是图片/视频平面的媒体来源（data URL），其他对象没有。 */
export type SceneRow = Record<string, unknown> & { source?: string | null };

/** 同一份快照的结构与媒体来源都相等才算未修改。 */
export function sameSceneState(saved: SceneState | null, current: SceneState): boolean {
  if (!saved || saved.structure !== current.structure || saved.media.length !== current.media.length) return false;
  for (let index = 0; index < saved.media.length; index += 1) {
    if (saved.media[index] !== current.media[index]) return false;
  }
  return true;
}

/**
 * 一次遍历同时产出两层指纹：
 * - structure：媒体正文只留长度（可发现替换，但不搬运多 MiB 字符串）；
 * - media：原样保留来源，供 `sameSceneState` 比较，不搬运正文；
 * - full()：需要时才拼接完整 JSON，结果在该快照内缓存。
 */
export function buildSceneFingerprint(header: unknown, rows: readonly SceneRow[]): SceneFingerprint {
  const structure = JSON.stringify({
    header,
    objects: rows.map((row) => ({ ...row, source: typeof row.source === "string" ? row.source.length : null })),
  });
  const media = rows.map((row) => row.source);
  let payload: string | null = null;
  return {
    structure,
    media,
    full: () => (payload ??= JSON.stringify({ header, objects: rows.map((row) => ({ ...row, source: row.source ?? null })) })),
  };
}

/** Scene operation identity: nested and concurrent jobs cannot clear each other's busy state. */
export class SceneOperations {
  private jobs = new Map<symbol, string>();
  constructor(private publish: (label: string | null) => void) {}
  start(label: string) {
    const id = Symbol(label); this.jobs.set(id, label); this.emit();
    return { update: (next: string) => { if (this.jobs.has(id)) { this.jobs.set(id, next); this.emit(); } }, finish: () => { this.jobs.delete(id); this.emit(); } };
  }
  private emit() { this.publish([...this.jobs.values()].at(-1) ?? null); }
}
export class SceneSaveGuard {
  private saved: string | null = null;
  private savedState: SceneState | null = null;
  isDirty(current: string) { return this.saved !== null && current !== this.saved; }
  /** 500ms 轮询用的便宜比较：不重新序列化媒体正文，结构或媒体来源变化才算脏。 */
  isDirtyState(current: SceneState) { return this.savedState !== null && !sameSceneState(this.savedState, current); }
  markSaved(current: string, state: SceneState = { structure: current, media: [] }) {
    this.saved = current;
    // 复制一层数组：调用方之后增删自己的媒体列表不能悄悄改掉已保存基线。
    this.savedState = { structure: state.structure, media: [...state.media] };
  }
  assertUnchanged(before: string, after: string) { if (before !== after) throw new Error('保存期间场景发生变化，请重新保存（未标记为已保存）'); }
}
// GPU recompilation increments Material.version even when no user-visible property changed.
export function materialRevision(material: { uuid: string; color?: { getHex(): number }; opacity: number; roughness?: number; metalness?: number; side: number; alphaTest: number; transparent: boolean; map?: { uuid: string } | null }) {
  return [material.uuid, material.color?.getHex(), material.opacity, material.roughness, material.metalness, material.side, material.alphaTest, material.transparent, material.map?.uuid];
}
export const canMoveEntry = (entry: { locked?: boolean; object: { visible: boolean } }) => !entry.locked && entry.object.visible;
