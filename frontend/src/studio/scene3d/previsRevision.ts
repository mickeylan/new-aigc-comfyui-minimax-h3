import * as THREE from 'three';
import { sampleExecutionActors } from './shotExecution';
import { validateScriptShotPlan, type ScriptShotCamera, type ScriptShotPlan } from './scriptShotPlan';

export type ObservationPoint = { file: string; shotId: string; seconds: number; localSeconds: number; camera: ScriptShotCamera; width: number; height: number };
export type RevisionProposal = {
  id: string; kind: 'camera-pan'; shotId: string; frameFile: string;
  evidence: { seconds: number; targetId: string; ndc: [number, number] };
  previousTarget: [number, number, number]; target: [number, number, number];
  predictedNdc: [number, number]; resolved: boolean; requiresHumanConfirmation: true; applied: false; note: string;
};
const MARGIN = .95, RESOLVED_MARGIN = .9, LIMIT = 64;
const ndcOf = (camera: ScriptShotCamera, width: number, height: number, point: [number, number, number]) => {
  const view = new THREE.PerspectiveCamera(camera.fov, width / height, .1, 10000);
  view.position.set(...camera.position); view.lookAt(new THREE.Vector3(...camera.target)); view.updateMatrixWorld(true);
  const v = new THREE.Vector3(...point).project(view);
  return [Math.round(v.x * 1e4) / 1e4, Math.round(v.y * 1e4) / 1e4] as [number, number];
};
/** Deterministic, read-only proposals from an observation report. Never inspects pixels,
 * never calls a model, never applies anything: every proposal needs an explicit human apply. */
export function buildRevisionProposals(plan: ScriptShotPlan, report: { observations: readonly ObservationPoint[] }): RevisionProposal[] {
  const valid = validateScriptShotPlan(plan);
  const out: RevisionProposal[] = [];
  for (const frame of report.observations) {
    const shot = valid.shots.find(s => s.id === frame.shotId);
    if (!shot?.camera) continue;
    if (frame.seconds < shot.start || frame.seconds >= shot.end) continue;
    if (frame.width <= 0 || frame.height <= 0) continue;
    for (const sample of sampleExecutionActors(shot.execution, frame.seconds - shot.start)) {
      const ndc = ndcOf(shot.camera, frame.width, frame.height, [sample.position.x, 0, sample.position.z]);
      if (Math.abs(ndc[0]) <= MARGIN && Math.abs(ndc[1]) <= MARGIN) continue;
      const target: [number, number, number] = [sample.position.x, shot.camera.target[1], sample.position.z];
      const predicted = ndcOf({ ...shot.camera, target }, frame.width, frame.height, [sample.position.x, 0, sample.position.z]);
      const resolved = Math.abs(predicted[0]) <= RESOLVED_MARGIN && Math.abs(predicted[1]) <= RESOLVED_MARGIN;
      out.push({ id: `${shot.id}:${frame.seconds}:${sample.targetId}`, kind: 'camera-pan', shotId: shot.id, frameFile: frame.file,
        evidence: { seconds: frame.seconds, targetId: sample.targetId, ndc }, previousTarget: [...shot.camera.target] as [number, number, number],
        target, predictedNdc: predicted, resolved, requiresHumanConfirmation: true, applied: false,
        note: resolved ? '只把机位目标水平对准该角色；未改机位位置、焦距或高度，需人重新观察。'
          : '仅把机位目标水平对准仍不足以入画；未自动改焦距或机位位置，需人工调整后重新观察。' });
      if (out.length > LIMIT) throw Error(`修订提案超过${LIMIT}条预算，请拆分计划或分批观察；不会截断建议`);
    }
  }
  return out.sort((a, b) => a.shotId === b.shotId ? a.evidence.seconds - b.evidence.seconds || a.evidence.targetId.localeCompare(b.evidence.targetId) : a.shotId.localeCompare(b.shotId));
}
/** Applies ONE proposal's camera aim. Anything stale or mismatched is rejected, never guessed. */
export function applyRevisionProposal(plan: ScriptShotPlan, proposal: RevisionProposal): ScriptShotPlan {
  const valid = structuredClone(validateScriptShotPlan(plan));
  if (proposal?.kind !== 'camera-pan' || proposal.requiresHumanConfirmation !== true) throw Error('修订提案类型无效');
  const shot = valid.shots.find(s => s.id === proposal.shotId);
  if (!shot?.camera) throw Error('提案对应的镜头或机位已不存在');
  if (shot.camera.target.some((value, i) => value !== proposal.previousTarget?.[i])) throw Error('提案已过期：机位目标已变化，请重新观察');
  const target = proposal.target;
  if (!Array.isArray(target) || target.length !== 3 || target.some(value => !Number.isFinite(value) || Math.abs(value) > 1000)) throw Error('提案目标坐标无效');
  shot.camera = { ...shot.camera, target: [target[0], target[1], target[2]] };
  return validateScriptShotPlan(valid);
}