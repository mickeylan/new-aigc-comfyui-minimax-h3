import { createScriptShotSampler } from './scriptShotSampler';
import type { ScriptShotPlan } from './toonflowSets';
import { encodePrevisVideo, pickVideoCodec, videoFrameTimes, webmBlob, VIDEO_WIDTH, VIDEO_HEIGHT } from '../../utils/previsVideo.js';

export async function renderScriptShotVideo(plan: ScriptShotPlan, options: Parameters<typeof createScriptShotSampler>[1] & {
  fps: number; Encoder: any; Frame: any; signal: AbortSignal;
  size?: { width: number; height: number };
  sourceFingerprint: string; mode: string;
  assertSource(): void;
  renderFrame(sample: ReturnType<ReturnType<typeof createScriptShotSampler>['sampleAt']>, seconds: number, timing: { timestamp: number; duration: number }): Promise<any> | any;
  onProgress(done: number, total: number): void;
}) {
  if (options.signal.aborted) throw Error('导出已取消');
  // Codec support and frame budget are checked before changing a camera or actor.
  const size = options.size ?? { width: VIDEO_WIDTH, height: VIDEO_HEIGHT };
  const codec = await pickVideoCodec(options.Encoder, options.Frame, options.fps, size);
  options.assertSource();
  if (options.signal.aborted) throw Error('导出已取消');
  const duration = plan.shots.at(-1)?.end ?? 0;
  const endMs = Math.ceil(duration * 1000);
  const times = videoFrameTimes(0, endMs, options.fps, endMs);
  const sampler = createScriptShotSampler(plan, options);
  try {
    const encoded = await encodePrevisVideo({ times, fps: options.fps, codec, Encoder: options.Encoder,
      isCancelled: () => options.signal.aborted || options.isCurrent?.() === false,
      onProgress: options.onProgress,
      renderFrame: async (_index: number, milliseconds: number, timing: { timestamp: number; duration: number }) => {
        options.assertSource();
        const seconds = milliseconds / 1000;
        const sample = sampler.sampleAt(seconds);
        // Caller's render must capture the clean canvas synchronously, before restoring helpers.
        return options.renderFrame(sample, seconds, timing);
      },
    });
    options.assertSource();
    const blob = webmBlob({ codecID: codec.webm, ...size, fps: options.fps, chunks: encoded.chunks, durationMs: encoded.duration_ms });
    const { characterAnimationCatalogRevision } = await import('./toonflow/characterAnimations');
    options.assertSource();
    if (options.signal.aborted) throw Error('导出已取消');
    const manifest = { version: 1, kind: 'studio-script-shot-video', artifactId: crypto.randomUUID(), container: 'webm', codec: codec.name,
      samplerVersion: 'script-shot-timeline-v1', characterAnimationCatalogRevision, walkProof: sampler.walkProof,
      width: size.width, height: size.height, fps: options.fps, frameCount: times.length,
      durationMs: encoded.duration_ms, sourceDurationMs: duration * 1000, timesMs: times,
      sourceFingerprint: options.sourceFingerprint, mode: options.mode,
      shots: sampler.plan, environment: options.environment, sessionEvents: sampler.sessionEvents,
      notes: ['无音频：台词时间轴只作审阅，不合成对白。', '首帧包含、末端不包含；按固定帧率编码，末帧不足一帧时补足帧长。', '深度/法线/白模是结构参考，不保证目标生成工作流接受；深度视频为有损编码，精确深度使用PNG参考包。', '仅本地生成，入库及关联须人工操作，不自动提交生成。'] };
    return { blob, manifest };
  } finally { sampler.stop(); }
}
