import { z } from 'zod';
import { MathUtils, Vector3, Mesh, type Group, type MeshStandardMaterial } from 'three';
import { materialRevision } from './sceneSafety';
import type { ExecutionEntry, StudioExecutionContext } from './studioExecutionContext';
import type { StudioExecutionObject } from './studioExecutionAdapter';
import { disposeObject } from './toonflow/sceneResources';

const text = z.string().trim().min(1).max(8000);
const id = z.string().min(1).max(160);
const number = (min: number, max: number) => z.number().finite().min(min).max(max);
const vector = (min: number, max: number) => z.tuple([number(min, max), number(min, max), number(min, max)]);
const objectSchema = z.object({ id, asset_id: id, name: z.string().min(1).max(500), position: vector(-1000, 1000), rotation: vector(-2 * Math.PI, 2 * Math.PI), scale: vector(.001, 1000) }).strict();
const notes = z.array(text).max(64);
export const sceneDesignSchema = z.object({
  context_revision: z.string().min(1).max(200), summary: text.max(4000),
  questions: notes.max(4), asset_queries: z.array(text.max(160)).max(4),
  template: z.object({ id, position: vector(-1000, 1000), yaw: number(-2 * Math.PI, 2 * Math.PI) }).strict().nullable(),
  objects: z.array(objectSchema).max(256), remove_ids: z.array(id).max(256), facts: notes, assumptions: notes, limitations: notes,
}).strict();
export type SceneDesignPlan = z.infer<typeof sceneDesignSchema>;
export type SceneDesignMessage = { role: 'user' | 'assistant'; content: string };
export type SceneDesignSnapshot = { sceneKey: string; context: StudioExecutionContext; editKeys?: Record<string, string> };

/** Local-only edit evidence: never sent to the model. Protect deletion after material/mesh/pose edits too. */
export function captureSceneDesignEditKeys(entries: readonly ExecutionEntry[]): Record<string, string> {
  return Object.fromEntries(entries.map(entry => {
    const nodes: unknown[] = [];
    entry.object.traverse(node => {
      const mesh = node as Mesh;
      nodes.push([node.uuid, node.name, node.position.toArray(), node.quaternion.toArray(), node.scale.toArray(), node.visible,
        mesh.geometry ? [mesh.geometry.uuid, mesh.geometry.index?.version, Object.entries(mesh.geometry.attributes).map(([key, attribute]) => [key, attribute.version, attribute.count])] : null,
        mesh.material ? (Array.isArray(mesh.material) ? mesh.material : [mesh.material]).map(material => materialRevision(material as MeshStandardMaterial)) : null]);
    });
    return [entry.id, JSON.stringify({ name: entry.name, locked: !!entry.locked, splat: !!entry.splat, nodes, clips: entry.object.animations.map(clip => [clip.uuid, clip.name, clip.duration]) })];
  }));
}
export type SceneDesignEntry = { id: string; name: string; object: Group };
export type SceneDesignBatch = { additions: SceneDesignEntry[]; updates: StudioExecutionObject[]; removeIds: string[]; preserved: string[] };
export type SceneDesignResult = { added: number; updated: number; removed: number; preserved: number };
export type SceneDesignHost = {
  capture: () => Promise<SceneDesignSnapshot>;
  apply: (plan: SceneDesignPlan, base: SceneDesignSnapshot, current: () => boolean) => Promise<SceneDesignResult>;
};

/** Lightweight real catalog, never geometry/GLB/media in model context. */
export async function captureSceneDesignContext(entries: readonly ExecutionEntry[]): Promise<StudioExecutionContext> {
  const { captureStudioExecutionContext } = await import('./studioExecutionContext');
  const context = await captureStudioExecutionContext(entries);
  const { builtinAssets } = await import('./toonflow/assetLibrary');
  context.assets = [
    ...context.assets.filter(asset => asset.id.startsWith('object:')),
    ...builtinAssets.filter(asset => asset.kind === 'primitive').map(asset => ({ id: `primitive:${asset.id}`, name: asset.name, clips: [] })),
  ];
  if (context.assets.length > 512) throw Error('可编辑对象过多，超过本轮512项上下文预算，请分场处理');
  return context;
}
export async function sceneDesignTemplates() { return (await import('./toonflowSets')).listSetTemplates(); }

/** Search terms come from AI, not mandatory user configuration. Only locally rebuildable catalog entries. */
export async function searchSceneDesignAssets(queries: string[]): Promise<StudioExecutionContext['assets']> {
  const { catalogAssets, getAssetSearchText, matchesSearchToken } = await import('./toonflow/assetCatalog');
  const found = new Map<string, StudioExecutionContext['assets'][number]>();
  for (const query of queries) {
    const tokens = query.trim().toLowerCase().split(/\s+/).filter(Boolean);
    if (!tokens.length) continue;
    const matches = catalogAssets.filter(asset => (asset.model || asset.recipe || asset.blueprint) && tokens.every(token => matchesSearchToken(getAssetSearchText(asset).toLowerCase(), token))).slice(0, 12);
    matches.forEach(asset => found.set(`catalog:${asset.id}`, { id: `catalog:${asset.id}`, name: `${asset.name} · ${asset.theme || ''} · ${asset.category}`, clips: [] }));
  }
  return [...found.values()];
}
export function validateSceneDesign(value: unknown, context: StudioExecutionContext, userTexts: readonly string[]): SceneDesignPlan {
  const plan = sceneDesignSchema.parse(value);
  if (plan.context_revision !== context.revision) throw Error('AI建景草稿修订不匹配');
  if ((plan.questions.length || plan.asset_queries.length) && (plan.template || plan.objects.length || plan.remove_ids.length)) throw Error('补问或检索不能夹带场景改动');
  if (plan.facts.some(fact => !userTexts.some(source => source.includes(fact)))) throw Error('AI建景事实不是用户原话');
  const assets = new Set(context.assets.map(asset => asset.id));
  const existing = new Map(context.objects.map(object => [object.id, object]));
  const ids = new Set<string>();
  for (const object of plan.objects) {
    if (ids.has(object.id) || !assets.has(object.asset_id)) throw Error('AI建景对象重复或素材不存在');
    ids.add(object.id);
    const old = existing.get(object.id);
    if (old ? old.asset_id !== object.asset_id || old.name !== object.name : !/^(catalog|primitive):/.test(object.asset_id)) throw Error('AI不得替换已有对象或克隆其动作资产');
  }
  for (const removed of plan.remove_ids) {
    if (!existing.has(removed) || ids.has(removed)) throw Error('AI删除目标不存在、重复或与更新冲突');
    ids.add(removed);
  }
  return plan;
}

export async function requestSceneDesign(command: string, history: SceneDesignMessage[], snapshot: SceneDesignSnapshot, signal: AbortSignal, status: (text: string) => void,
  post = async (body: unknown) => {
    const raw = JSON.stringify(body);
    if (new TextEncoder().encode(raw).byteLength > 2 * 1024 * 1024) throw Error('建景请求超过2MiB，未发送');
    const response = await fetch('/api/studio/scene-design/plan', { method: 'POST', signal, headers: { 'Content-Type': 'application/json' }, body: raw });
    const data = await response.json();
    if (!response.ok) throw Error(data?.error || `AI建景失败 HTTP ${response.status}`);
    return data;
  },
): Promise<{ plan: SceneDesignPlan; snapshot: SceneDesignSnapshot }> {
  const context = structuredClone(snapshot.context);
  const templates = await sceneDesignTemplates();
  const turns = history.map(message => ({ ...message }));
  const userTexts = [command, ...history.filter(message => message.role === 'user').map(message => message.content)];
  const searched = new Set<string>();
  // At most three calls including catalog retrieval, never an unbounded autonomous loop.
  for (let step = 0; step < 3; step++) {
    signal.throwIfAborted();
    status(step ? 'AI正在根据素材检索结果设计…' : 'AI正在设计布景…');
    const reply = await post({ command, history: turns, context, templates });
    signal.throwIfAborted();
    if (reply.persisted !== false || reply.applied !== false || reply.validation !== 'structure_and_catalog_references_only') throw Error('AI建景响应缺少未应用状态或验证边界');
    const plan = validateSceneDesign(reply.plan, context, userTexts);
    if (plan.template && !templates.some(template => template.id === plan.template!.id)) throw Error('AI选择的整套景不存在');
    if (!plan.asset_queries.length) return { plan, snapshot: { ...snapshot, context } };
    if (step === 2 || plan.asset_queries.some(query => searched.has(query))) throw Error('AI素材检索未收敛，已停止；请补充需求后重试');
    plan.asset_queries.forEach(query => searched.add(query));
    status(`正在本地检索：${plan.asset_queries.join('、')}`);
    const matches = await searchSceneDesignAssets(plan.asset_queries);
    const all = new Map(context.assets.map(asset => [asset.id, asset])); matches.forEach(asset => all.set(asset.id, asset));
    if (all.size > 512) throw Error('素材检索上下文超过512项，已停止');
    context.assets = [...all.values()];
    turns.push({ role: 'assistant', content: `素材检索：${plan.asset_queries.join('、')}。结果已加入context.assets，${matches.length}项；无匹配时请明确限制，不重复检索。` });
  }
  throw Error('AI建景达到本轮上限');
}

/** Three-way transform merge. User changes win per entire field; user deletions never resurrect. */
export function mergeSceneDesignEdits(plan: SceneDesignPlan, base: StudioExecutionContext, latest: StudioExecutionContext) {
  const before = new Map(base.objects.map(object => [object.id, object]));
  const ours = new Map(latest.objects.map(object => [object.id, object]));
  const same = (a: unknown, b: unknown) => JSON.stringify(a) === JSON.stringify(b);
  const updates: StudioExecutionObject[] = [], removeIds: string[] = [], preserved: string[] = [];
  for (const proposed of plan.objects) {
    const old = before.get(proposed.id); if (!old) continue;
    const now = ours.get(proposed.id);
    if (!now || now.asset_id !== old.asset_id || now.name !== old.name) { preserved.push(proposed.id); continue; }
    const result = structuredClone(now);
    for (const field of ['position', 'rotation', 'scale'] as const) {
      if (same(proposed[field], old[field])) continue;
      if (same(now[field], old[field])) result[field] = [...proposed[field]];
      else preserved.push(`${proposed.id}:${field}`);
    }
    if (!same(result, now)) updates.push(result);
  }
  for (const id of plan.remove_ids) {
    if (same(before.get(id), ours.get(id))) removeIds.push(id);
    else preserved.push(id);
  }
  return { updates, removeIds, preserved: [...new Set(preserved)] };
}

/** Detached, locally reconstructible static geometry. No model-authored code/URLs/recipes accepted. */
export async function materializeSceneDesign(plan: SceneDesignPlan, base: StudioExecutionContext, signal: AbortSignal): Promise<SceneDesignEntry[]> {
  const entries: SceneDesignEntry[] = [];
  const existing = new Set(base.objects.map(object => object.id));
  signal.throwIfAborted();
  if (!plan.template && plan.objects.every(object => existing.has(object.id))) return entries;
  try {
    const [{ prepareSetDraft, materializeSetDraft, materializeSetObject, listSetPersonObjects }, { builtinAssets }, { createSceneObject }] = await Promise.all([import('./toonflowSets'), import('./toonflow/assetLibrary'), import('./toonflow/sceneDocument')]);
    signal.throwIfAborted();
    if (plan.template) {
      const draft = await prepareSetDraft(plan.template.id, signal);
      // AI templates supply scenery only. Template demonstration crowds must not
      // silently override a novel's cast/count; all NEW people belong in objects.
      // Filter the detached draft before geometry allocation, never the live scene
      // or the manual template path. Asset identity, not Chinese names, defines people.
      const people = new Set(listSetPersonObjects(draft).map(person => person.id));
      draft.document.objects = draft.document.objects.filter(object => !people.has(object.id) && object.asset.itemType !== '人物');
      const built = materializeSetDraft(draft);
      entries.push(...built);
      const t = plan.template;
      built.forEach(entry => {
        entry.id = `ai-set:${crypto.randomUUID()}`;
        entry.object.position.applyAxisAngle(new Vector3(0, 1, 0), t.yaw).add(new Vector3(...t.position));
        entry.object.rotation.y += t.yaw; entry.object.updateMatrixWorld(true);
      });
    }
    for (const placement of plan.objects) {
      signal.throwIfAborted(); if (existing.has(placement.id)) continue;
      const [kind, ...parts] = placement.asset_id.split(':'); const assetId = parts.join(':');
      const asset = builtinAssets.find(value => value.id === assetId && (kind === 'primitive' ? value.kind === 'primitive' : value.kind === 'model'));
      if (!asset || kind === 'catalog' && !asset.model && !asset.recipe && !asset.blueprint) throw Error(`素材无法本地重建：${placement.asset_id}`);
      const saved = createSceneObject(asset, { x: placement.position[0], y: placement.position[1], z: placement.position[2] });
      saved.id = placement.id; saved.name = placement.name;
      saved.rotation = { x: MathUtils.radToDeg(placement.rotation[0]), y: MathUtils.radToDeg(placement.rotation[1]), z: MathUtils.radToDeg(placement.rotation[2]) };
      saved.scale = { x: placement.scale[0], y: placement.scale[1], z: placement.scale[2] }; saved.height = placement.position[1];
      entries.push({ id: saved.id, name: saved.name, object: materializeSetObject(saved) });
    }
    if (entries.length > 4096 || new Set(entries.map(entry => entry.id)).size !== entries.length) throw Error('AI布景实例超限或身份重复');
    signal.throwIfAborted(); return entries;
  } catch (cause) { entries.forEach(entry => disposeObject(entry.object)); throw cause; }
}

/** Synchronous publication with rollback; history retains removed resources for undo. */
export function commitSceneDesign<T extends ExecutionEntry>(batch: SceneDesignBatch, before: T[], scene: { add: (object: Group) => unknown }, checkpoint: () => void): T[] {
  const ids = new Set(before.map(entry => entry.id));
  if (batch.additions.some(entry => ids.has(entry.id)) || new Set(batch.additions.map(entry => entry.id)).size !== batch.additions.length) throw Error('AI新增对象ID冲突');
  const touched = [...batch.updates.map(update => update.id), ...batch.removeIds];
  if (new Set(touched).size !== touched.length) throw Error('AI修改目标重复');
  const targets = touched.map(id => {
    const entry = before.find(value => value.id === id);
    if (!entry || entry.locked || entry.splat || !entry.object.visible) throw Error('AI目标已不可编辑');
    return { entry, parent: entry.object.parent, position: entry.object.position.clone(), quaternion: entry.object.quaternion.clone(), scale: entry.object.scale.clone() };
  });
  checkpoint();
  try {
    batch.updates.forEach(update => {
      const entry = targets.find(target => target.entry.id === update.id)!.entry;
      entry.object.position.set(...update.position as [number, number, number]); entry.object.rotation.set(...update.rotation as [number, number, number]); entry.object.scale.set(...update.scale as [number, number, number]); entry.object.updateMatrixWorld(true);
    });
    targets.filter(target => batch.removeIds.includes(target.entry.id)).forEach(target => target.entry.object.removeFromParent());
    batch.additions.forEach(entry => scene.add(entry.object));
    return [...before.filter(entry => !batch.removeIds.includes(entry.id)), ...batch.additions as unknown as T[]];
  } catch (cause) {
    batch.additions.forEach(entry => entry.object.removeFromParent());
    targets.forEach(({ entry, parent, position, quaternion, scale }) => { entry.object.position.copy(position); entry.object.quaternion.copy(quaternion); entry.object.scale.copy(scale); if (parent && entry.object.parent !== parent) parent.add(entry.object); entry.object.updateMatrixWorld(true); });
    throw cause;
  }
}
