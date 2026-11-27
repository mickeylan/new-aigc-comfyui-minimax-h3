import type { Group } from 'three';
import type { ExecutionEntry } from './studioExecutionContext';
import { disposeObject } from './toonflow/sceneResources';
import { commitSceneDesign, materializeSceneDesign, mergeSceneDesignEdits, type SceneDesignBatch, type SceneDesignPlan, type SceneDesignResult, type SceneDesignSnapshot } from './sceneDesign';

export type SceneDesignApplyHost<T extends ExecutionEntry> = {
  state: () => { sceneKey: string; fingerprint: string; available: boolean };
  capture: () => Promise<SceneDesignSnapshot>;
  entries: () => T[];
  scene: { add: (object: Group) => unknown };
  checkpoint: () => void;
  validate: (batch: SceneDesignBatch) => void;
  publish: (entries: T[]) => void;
};

/** All asynchronous work stays detached. Generation-time edits merge, application-time races fail closed. */
export async function applySceneDesign<T extends ExecutionEntry>(plan: SceneDesignPlan, base: SceneDesignSnapshot, current: () => boolean, host: SceneDesignApplyHost<T>): Promise<SceneDesignResult> {
  const controller = new AbortController();
  const assertCurrent = () => { const state = { ...host.state() }; if (!current() || !state.available || state.sceneKey !== base.sceneKey) { controller.abort(); throw Error('场景已切换、忙碌或提案已取消，未应用'); } return state; };
  assertCurrent();
  if (plan.questions.length || plan.asset_queries.length) throw Error('补问或检索不是可应用提案');
  const additions = await materializeSceneDesign(plan, base.context, controller.signal);
  let adopted = false;
  try {
    const before = assertCurrent();
    const latest = await host.capture();
    const after = assertCurrent();
    if (latest.sceneKey !== base.sceneKey || before.fingerprint !== after.fingerprint) throw Error('应用准备期间场景发生变化，未应用，请重试');
    const batch: SceneDesignBatch = { additions, ...mergeSceneDesignEdits(plan, base.context, latest.context) };
    batch.removeIds = batch.removeIds.filter(id => {
      if (base.editKeys?.[id] && base.editKeys[id] === latest.editKeys?.[id]) return true;
      batch.preserved.push(id); return false; // Missing evidence must not authorize deletion.
    });
    host.validate(batch);
    if (assertCurrent().fingerprint !== after.fingerprint) throw Error('应用校验期间场景变化，未应用');
    if (!additions.length && !batch.updates.length && !batch.removeIds.length) return { added: 0, updated: 0, removed: 0, preserved: batch.preserved.length };
    // No await between this last identity/fingerprint guard and the synchronous transaction.
    const next = commitSceneDesign(batch, host.entries(), host.scene, host.checkpoint);
    adopted = true; // Ownership transfers at commit; UI publication must not dispose live geometry.
    host.publish(next);
    return { added: additions.length, updated: batch.updates.length, removed: batch.removeIds.length, preserved: batch.preserved.length };
  } finally { if (!adopted) additions.forEach(entry => disposeObject(entry.object)); }
}
