import type { Object3D } from 'three';

export type StudioExecutionContext = {
  revision: string;
  assets: { id: string; name: string; clips: { id: string; name: string; duration: number }[] }[];
  objects: { id: string; asset_id: string; name: string; position: number[]; rotation: number[]; scale: number[] }[];
};
export type ExecutionEntry = { id: string; name: string; object: Object3D; locked?: boolean; splat?: unknown };

/** Metadata only; no geometry building, scene mutation, clip attachment or network. */
export async function captureStudioExecutionContext(entries: readonly ExecutionEntry[]): Promise<StudioExecutionContext> {
  // Capture every entry before the first await, including unselectable states in the revision.
  const captured = entries.map(entry => ({
    id: entry.id, asset_id: `object:${entry.id}`, name: entry.name,
    position: entry.object.position.toArray(), rotation: [entry.object.rotation.x, entry.object.rotation.y, entry.object.rotation.z], scale: entry.object.scale.toArray(),
    visible: entry.object.visible, locked: !!entry.locked, splat: !!entry.splat,
    geometry: entry.object.uuid,
    clips: entry.object.animations.map((clip, index) => ({ id: `${index}:${clip.uuid}`, name: clip.name || `(unnamed clip ${index})`, duration: clip.duration })),
  }));
  if (new Set(captured.map(item => item.id)).size !== captured.length) throw Error('当前场景对象ID重复');
  const { catalogAssets } = await import('./toonflow/assetCatalog');
  const usable = captured.filter(item => item.visible && !item.locked && !item.splat);
  const assets: StudioExecutionContext['assets'] = catalogAssets.map(asset => ({ id: `catalog:${asset.id}`, name: `${asset.name} · ${asset.theme || ''} · ${asset.category}`, clips: [] }));
  // Only advertise attached clips as executable on an existing object. Catalog names
  // alone do not prove a clip can bind to a new skeleton.
  assets.push(...usable.map(item => ({ id: item.asset_id, name: item.name, clips: item.clips })));
  const objects = usable.map(({ id, asset_id, name, position, rotation, scale }) => ({ id, asset_id, name, position, rotation, scale }));
  const raw = new TextEncoder().encode(JSON.stringify({ captured, assets }));
  const digest = await crypto.subtle.digest('SHA-256', raw);
  const revision = Array.from(new Uint8Array(digest), byte => byte.toString(16).padStart(2, '0')).join('');
  return { revision, assets, objects };
}
