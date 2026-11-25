// Toonflow-app adapter; upstream MIT copyright/terms: ../THIRD_PARTY_NOTICES.md.
// Keep the large procedural catalog behind dynamic import in the editor.
import { catalogAssets, countAssetDerivatives, getAssetDerivatives, getAssetSearchText, matchesSearchToken } from './toonflow/assetCatalog';
import { resolveAssetModel } from './toonflow/assetModelResolver';
import { createModelGroup } from './toonflow/modelGeometry';
import { createToonflowPrimitive } from './toonflowPrimitives';
import type { SceneAsset } from './toonflow/sceneDocument';

export const TOONFLOW_GENERATOR_VERSION = 1;
export interface ProceduralSource { generator: 'toonflow'; version: 1; assetId: string }
export interface ProceduralAssetRow { id: string; name: string; category: string; theme: string; derivatives: number }
const byId = new Map(catalogAssets.map(asset => [asset.id, asset]));
const row = (asset: SceneAsset): ProceduralAssetRow => ({ id: asset.id, name: asset.name, category: asset.category, theme: asset.theme ?? '', derivatives: countAssetDerivatives(asset) });
export function proceduralAssetCounts() {
  const main = catalogAssets.length;
  const derivatives = catalogAssets.reduce((sum, asset) => sum + countAssetDerivatives(asset), 0);
  return { main, derivatives, total: main + derivatives };
}
export function searchProceduralAssets(query: string, page = 0, pageSize = 24) {
  if (!Number.isSafeInteger(page) || page < 0 || !Number.isSafeInteger(pageSize) || pageSize < 1 || pageSize > 96) throw new Error('素材分页参数无效');
  const tokens = query.trim().toLocaleLowerCase().split(/\s+/).filter(Boolean);
  const matched = tokens.length ? catalogAssets.filter(asset => tokens.every(token => matchesSearchToken(getAssetSearchText(asset), token))) : catalogAssets;
  return { total: matched.length, rows: matched.slice(page * pageSize, (page + 1) * pageSize).map(row) };
}
export function proceduralAssetVariants(parentId: string) {
  const parent = byId.get(parentId);
  if (!parent) throw new Error('找不到程序化素材');
  return getAssetDerivatives(parent).map(row);
}
function findAsset(id: string, parentId?: string) {
  const main = byId.get(id);
  if (main) return main;
  const parent = parentId && byId.get(parentId);
  const derivative = parent && getAssetDerivatives(parent).find(asset => asset.id === id);
  if (!derivative) throw new Error('找不到程序化素材，请重新选择');
  return derivative;
}
export function createProceduralAsset(assetId: string, parentId?: string) {
  const asset = findAsset(assetId, parentId);
  const definition = resolveAssetModel(asset);
  if (!definition && asset.kind !== 'primitive') throw new Error('该素材没有可构建的本地模型');
  const object = definition ? createModelGroup(definition, undefined, true) : createToonflowPrimitive(asset, '#8b87ad');
  object.name = asset.name;
  object.userData.proceduralSource = { generator: 'toonflow', version: TOONFLOW_GENERATOR_VERSION, assetId, ...(parentId ? { parentId } : {}) };
  return object;
}
