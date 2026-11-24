/** Endpoint-inclusive sampling. Never silently truncate a path at the frame budget. */
export function sequenceTiming(seconds: number, fps: number, limit = 240) {
  if (!Number.isFinite(seconds) || seconds < 0 || !Number.isFinite(fps) || fps <= 0) throw new Error('序列时长或帧率无效');
  const frames = Math.max(1, Math.ceil(seconds * fps) + 1);
  return { seconds, frames, exceedsLimit: frames > limit };
}
export function sequenceSampleTime(index: number, frames: number, seconds: number, fps: number) {
  return index >= frames - 1 ? seconds : Math.min(seconds, Math.max(0, index) / fps);
}
