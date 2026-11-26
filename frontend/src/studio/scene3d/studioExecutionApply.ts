import type { Object3D } from 'three';
import { snapshotDirectorSource, type DirectorSourceSnapshot } from './directorSource';
import { prepareStudioExecution, type StudioExecutionObject } from './studioExecutionAdapter';
import { validateScriptShotPlan, type ScriptShotPlan } from './scriptShotPlan';
import { validateSceneRequirements } from './sceneRequirements';
import type { StudioExecutionContext } from './studioExecutionContext';
import { disposeObject } from './toonflow/sceneResources';

type Entry = { id: string; name: string; object: Object3D };
export type StudioExecutionCommit = { additions: Entry[]; updates: StudioExecutionObject[]; shotPlan: ScriptShotPlan };
export type StudioExecutionApplyPort = {
  readSource: () => Promise<{ source_hash: string; source: { scene: unknown; shots: unknown; dialogues?: unknown; dialogue_timeline?: unknown } }>;
  captureContext: () => Promise<StudioExecutionContext>;
  /** Synchronous editor fingerprint; also include scene/load epoch and non-object edits. */
  fingerprint: () => string;
  isCurrent: () => boolean;
  /** Synchronous, all-or-nothing publication. Caller owns additions only on success. */
  commit: (batch: StudioExecutionCommit) => void;
};

/** Only call after explicit human confirmation. Stage every geometry before publication,
 * recheck authoritative source + actual resources, and reject edits at every async boundary.
 * Does not run media tasks, save a file or alter backend business data.
 */
export async function applyStudioExecution(value: unknown, reviewed: DirectorSourceSnapshot, port: StudioExecutionApplyPort, requirements?: unknown): Promise<ScriptShotPlan> {
  const baseline = port.fingerprint();
  const projectID = reviewed.scene.project_id; const sceneID = reviewed.scene.id;
  const reviewedCopy = structuredClone(reviewed);
  const planValue = structuredClone(value);
  const review = requirements === undefined ? undefined : validateSceneRequirements(structuredClone(requirements));
  if (review && (review.sourceRevision !== reviewedCopy.revision || review.sourceContent !== reviewedCopy.scene.content)) throw Error('建景要求来源已变化，请重新审阅');
  if (review?.unresolved.length) throw Error(`建景要求仍未解决：${review.unresolved.join('；')}`);
  const check = () => { if (!port.isCurrent() || port.fingerprint() !== baseline) throw Error('应用期间来源选择或编辑场景已变化，未应用'); };
  check();
  const authoritative = structuredClone(await port.readSource()); check();
  const source = await snapshotDirectorSource(projectID, authoritative.source.scene, authoritative.source.shots); check();
  if (source.scene.id !== sceneID) throw Error('权威来源场次不匹配');
  // Project detail can have additional computed scene fields. Match all shots and
  // reviewed content/revision exactly as the source inspection boundary does.
  const compared = await snapshotDirectorSource(projectID, reviewedCopy.scene, source.shots); check();
  if (compared.revision !== reviewedCopy.revision || source.scene.content !== reviewedCopy.scene.content || source.scene.updated_at !== reviewedCopy.scene.updated_at) throw Error('导演来源已变化，请重新审阅');
  const context = await port.captureContext(); check();
  const speech = authoritative.source.dialogues !== undefined || authoritative.source.dialogue_timeline !== undefined
    ? { dialogues: authoritative.source.dialogues, dialogue_timeline: authoritative.source.dialogue_timeline } : undefined;
  const prepared = await prepareStudioExecution(planValue, source, authoritative.source_hash, context, speech); check();
  if (!prepared.shotPlan || prepared.blockers.length) throw Error(`执行草稿存在阻塞，未应用：${prepared.blockers.join('；')}`);
  if (review) {
    const available = new Set([...context.objects.map(o => o.id), ...prepared.objects.map(o => o.id)]);
    for (const entity of review.entities) for (const id of entity.objectIds) if (!available.has(id)) throw Error(`建景要求缺少对象：${entity.label}（${id}）`);
    // The authoritative Scene can contain different computed metadata. Only after
    // source content and every reviewed shot have matched, bind to its revision.
    prepared.shotPlan.sceneRequirements = { ...review, sourceRevision: source.revision };
    validateScriptShotPlan(prepared.shotPlan);
  }
  const [{ catalogAssets }, { materializeSetObject }] = await Promise.all([import('./toonflow/assetCatalog'), import('./toonflowSets')]); check();
  const catalog = new Map(catalogAssets.map(asset => [`catalog:${asset.id}`, asset]));
  const existing = new Set(context.objects.map(object => object.id));
  const additions: Entry[] = [];
  try {
    // No generated ID/name lookup, template substitution or scene-object cloning.
    for (const object of prepared.objects) {
      if (existing.has(object.id)) continue;
      const asset = catalog.get(object.asset_id);
      if (!asset) throw Error('真实目录已无该素材，未应用');
      const xyz = (v: number[]) => ({ x: v[0]!, y: v[1]!, z: v[2]! });
      const built = materializeSetObject({ id: object.id, name: object.name, asset, position: xyz(object.position), rotation: xyz(object.rotation.map(n => n * 180 / Math.PI)), scale: xyz(object.scale), color: '#bac5d3', visible: true });
      additions.push({ id: object.id, name: object.name, object: built });
      built.rotation.set(...object.rotation); // Preserve exact input radians, no placement normalization.
    }
    const latest = await port.captureContext(); check();
    if (latest.revision !== prepared.plan.context_revision) throw Error('真实资源已变化，未应用');
    const latestSource = await port.readSource(); check();
    if (latestSource.source_hash !== authoritative.source_hash) throw Error('应用期间导演来源已变化，未应用');
    // No await after this guard: publication cannot race a later UI turn.
    port.commit({ additions, updates: prepared.objects.filter(object => existing.has(object.id)), shotPlan: prepared.shotPlan });
    return prepared.shotPlan;
  } catch (error) { additions.forEach(entry => disposeObject(entry.object)); throw error; }
}
