import { observationFrames, observationReviewHTML, type ObservationFrame } from './previsObservationFrames';
import { createScriptShotSampler } from './scriptShotSampler';
import { validateScriptShotPlan, type ScriptShotPlan, type ScriptShotCamera } from './scriptShotPlan';

export function rehearsalTimes(plan: ScriptShotPlan) {
  validateScriptShotPlan(plan);
  const end = plan.shots.at(-1)!.end;
  if (Math.ceil(end * 10) + 1 > 6000) throw Error('运行诊断超过6000个采样预算，请拆分计划');
  const times = new Set<number>(Array.from({ length: Math.ceil(end * 10) + 1 }, (_, i) => Math.min(end, i / 10)));
  for (const shot of plan.shots) {
    times.add(shot.start); times.add(shot.end);
    for (const actor of shot.execution?.actors ?? []) for (const beat of actor.beats) { times.add(shot.start + beat.start); times.add(shot.start + beat.end); }
  }
  if (times.size > 6000) throw Error('运行诊断超过6000个采样预算，请拆分计划');
  return [...times].sort((a, b) => a - b);
}
/** No model or persistence. Each pending PNG is copied synchronously and runtime restored
 * before awaiting its encoder. Caller's freshness check must use editing baselines. */
export async function observePrevis(plan: ScriptShotPlan, options: {
  sourceRevision: string; temporalEvidence?: boolean; assertCurrent(): void;
  sampler(): ReturnType<typeof createScriptShotSampler>;
  actors(): { id: string; position: number[] }[];
  capture(camera: ScriptShotCamera): Promise<{ blob: Blob; width: number; height: number }>;
}) {
  options.assertCurrent();
  const report = { version: 1, kind: 'previs-observation', sourceRevision: options.sourceRevision, plan: structuredClone(plan), visuallyApproved: false,
    capturePolicy: options.temporalEvidence ? 'shot-start-mid-tail-and-beat-boundaries-v1' : 'shot-start-v1',
    status: 'blocked', error: '', sessionEvents: [] as unknown[], completedSamples: 0, maxSampleIntervalSeconds: .1,
    initial: [] as ReturnType<typeof options.actors>, final: [] as ReturnType<typeof options.actors>, walkProof: undefined as unknown, contactMetrics: undefined as unknown,
    observations: [] as (ObservationFrame & { camera: ScriptShotCamera })[],
    limitations: ['离散采样不证明连续无碰撞；截图不等于剧情视觉通过。', '未验证闭合握持；不进行视觉模型判断。'] };
  const files: { name: string; data: Uint8Array }[] = [];
  let runtime: ReturnType<typeof createScriptShotSampler> | undefined;
  try {
    const times = rehearsalTimes(plan), frames = observationFrames(plan, options.temporalEvidence);
    let capturedBytes = 0;
    runtime = options.sampler(); report.walkProof = runtime.walkProof; report.sessionEvents = structuredClone(runtime.sessionEvents ?? []);
    for (const seconds of times) {
      options.assertCurrent(); runtime.sampleAt(seconds);
      if (!report.completedSamples) report.initial = structuredClone(options.actors());
      report.final = structuredClone(options.actors()); report.completedSamples++;
    }
    report.contactMetrics = structuredClone(runtime.contactMetrics);
    runtime.stop(); runtime = undefined;
    for (const [i, point] of frames.entries()) {
      const shot = plan.shots.find(s => s.id === point.shotId)!;
      options.assertCurrent(); runtime = options.sampler();
      let pending: ReturnType<typeof options.capture>, camera: ScriptShotCamera;
      try {
        const sample = runtime.sampleAt(point.seconds); camera = structuredClone(sample.camera);
        pending = options.capture(camera);
      } finally { runtime.stop(); runtime = undefined; }
      const frame = await pending!; options.assertCurrent();
      if (frame.blob.type !== 'image/png' && frame.blob.type !== '' || !Number.isSafeInteger(frame.width) || frame.width <= 0 || !Number.isSafeInteger(frame.height) || frame.height <= 0 || !frame.blob.size) throw Error('观察截图格式或尺寸无效');
      capturedBytes += frame.blob.size;
      if (capturedBytes > 128 * 1024 * 1024) throw Error('观察截图超过128MiB预算，已放弃全部图像');
      const data = new Uint8Array(await frame.blob.arrayBuffer()); options.assertCurrent();
      const name = `observation-${String(i + 1).padStart(3, '0')}.png`;
      files.push({ name, data }); report.observations.push({ file: name, ...point, camera: camera!, width: frame.width, height: frame.height,
        activeBeats: (shot.execution?.actors ?? []).flatMap(actor => actor.beats.flatMap((beat, beatIndex) => point.localSeconds >= beat.start && point.localSeconds < beat.end ? [{ targetId: actor.targetId, targetName: actor.targetName, beatIndex, animation: beat.animation.name }] : [])) });
    }
    if (options.temporalEvidence) files.push({ name: 'review.html', data: new TextEncoder().encode(observationReviewHTML(plan, report.observations)) });
    report.status = 'completed';
  } catch (error) {
    // A stale/cancelled session must never become a downloadable current report.
    runtime?.stop(); runtime = undefined; options.assertCurrent();
    report.error = error instanceof Error ? error.message : String(error); files.length = 0; report.observations = [];
  } finally { runtime?.stop(); }
  options.assertCurrent();
  files.push({ name: 'observation.json', data: new TextEncoder().encode(JSON.stringify(report, null, 2)) });
  return { report, files };
}
