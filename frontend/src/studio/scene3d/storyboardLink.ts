import { validateScriptShotPlan, type ScriptShotPlan } from './scriptShotPlan';

export type StoryboardSource = {
  version: 1; revision: string; sceneRevision: string; planRevision: string;
  sceneId: string; sourceFingerprint: string; samplerVersion: 'script-shot-timeline-v1'; frameAspect: string;
};
export type StoryboardShotSource = StoryboardSource & { projectId: number; sceneIdNumber: number; shotId: number; planShotId: string; sampleSeconds: number };
const digest = async (value: string) => Array.from(new Uint8Array(await crypto.subtle.digest('SHA-256', new TextEncoder().encode(value))), byte => byte.toString(16).padStart(2, '0')).join('');

/** Local provenance, not a server signature or semantic approval. No output mode/size/fps:
 * image, reference pack and video from the same editing baseline share this parent. */
export async function storyboardSource(plan: ScriptShotPlan, sceneState: string, frameAspect: string): Promise<StoryboardSource> {
  const detached = structuredClone(validateScriptShotPlan(plan));
  const [sceneRevision, planRevision] = await Promise.all([digest(sceneState), digest(JSON.stringify(detached))]);
  const identity = { version: 1 as const, sceneRevision, planRevision, sceneId: detached.sceneId, sourceFingerprint: detached.sourceFingerprint, samplerVersion: 'script-shot-timeline-v1' as const, frameAspect };
  return { ...identity, revision: await digest(JSON.stringify(identity)) };
}

/** Exact backend IDs only. Never infer Shot from ordinal or a free-form pasted script. */
export function linkedStoryboardShot(plan: ScriptShotPlan, context: { projectId: number; sceneId: number }, shotId: number) {
  const ready = validateScriptShotPlan(plan);
  if (!Number.isSafeInteger(shotId) || shotId < 1 || ready.sceneId !== `director-scene:${context.sceneId}`) throw Error('当前镜头表不属于所选导演场次');
  const sources = ready.diagnostics.flatMap(note => { try { const parsed = JSON.parse(note); return parsed?.directorSource ? [parsed.directorSource] : []; } catch { return []; } });
  if (sources.length !== 1 || sources[0]?.scene?.project_id !== context.projectId || sources[0]?.scene?.id !== context.sceneId || !Array.isArray(sources[0]?.shots)) throw Error('镜头表缺少匹配的项目/导演来源，不能自动关联');
  const shot = ready.shots.find(item => item.id === `director-shot:${shotId}`);
  const original = sources[0].shots.filter((item: any) => item.id === shotId && item.scene_id === context.sceneId);
  if (!shot || original.length !== 1) throw Error('所选Shot尚未绑定到已确认的3D镜头表');
  return shot;
}

/** Borrow the shared timeline only until synchronous pixels are copied. Never keep
 * temporary camera/actor poses alive during async PNG encoding. */
export async function captureStoryboardShot<T>(plan: ScriptShotPlan, shotId: string, host: {
  assertCurrent(): void;
  sampler(): { sampleAt(seconds: number): { camera: any }; stop(): void };
  capture(camera: any): Promise<T>;
}): Promise<T> {
  const shot = validateScriptShotPlan(plan).shots.find(item => item.id === shotId);
  if (!shot) throw Error('所选镜头不存在');
  host.assertCurrent();
  const sampler = host.sampler();
  let pending: Promise<T>;
  try { const sample = sampler.sampleAt(shot.start); pending = host.capture(sample.camera); }
  finally { sampler.stop(); }
  const result = await pending;
  host.assertCurrent();
  return result;
}
