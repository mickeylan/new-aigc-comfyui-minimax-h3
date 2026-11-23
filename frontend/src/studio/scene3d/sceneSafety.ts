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
  isDirty(current: string) { return this.saved !== null && current !== this.saved; }
  markSaved(current: string) { this.saved = current; }
  assertUnchanged(before: string, after: string) { if (before !== after) throw new Error('保存期间场景发生变化，请重新保存（未标记为已保存）'); }
}
// GPU recompilation increments Material.version even when no user-visible property changed.
export function materialRevision(material: { uuid: string; color?: { getHex(): number }; opacity: number; roughness?: number; metalness?: number; side: number; alphaTest: number; transparent: boolean; map?: { uuid: string } | null }) {
  return [material.uuid, material.color?.getHex(), material.opacity, material.roughness, material.metalness, material.side, material.alphaTest, material.transparent, material.map?.uuid];
}
export const canMoveEntry = (entry: { locked?: boolean; object: { visible: boolean } }) => !entry.locked && entry.object.visible;
