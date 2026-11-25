// @vitest-environment node
// Binary muxing requires one Uint8Array/TextEncoder realm, as in the actual browser.
import { describe, expect, it } from 'vitest';
import * as THREE from 'three';
import { renderScriptShotVideo } from './scriptShotVideo';
import { parseScriptScenes, buildScriptSceneDraft, buildScriptShotPlan, setScriptShotCamera, setScriptShotMotion, updateScriptShotDuration } from './toonflowSets';

class Encoder {
  static async isConfigSupported() { return { supported: true }; }
  state = 'unconfigured'; encodeQueueSize = 0;
  constructor(private options: any) {}
  configure() { this.state = 'configured'; }
  encode(frame: any, options: any) { this.options.output({ byteLength: 1, timestamp: frame.timestamp, type: options.keyFrame ? 'key' : 'delta', copyTo(target: Uint8Array) { target[0] = 42; } }); }
  async flush() {}
  close() { this.state = 'closed'; }
}
async function setup() {
  const draft = await buildScriptSceneDraft(parseScriptScenes('1. 内景 现代卧室 日\n甲：走吧。'), 'scene-1', new AbortController().signal);
  let plan = updateScriptShotDuration(buildScriptShotPlan(draft), 'shot-1', 2);
  plan = setScriptShotCamera(plan, 'shot-1', { position: [4, 3, 6], target: [0, 1, 0], fov: 45 });
  const actor = new THREE.Group();
  plan = setScriptShotMotion(plan, 'shot-1', { targetId: 'a', targetName: '甲', start: { x: 0, z: 0 }, end: { x: 2, z: 0 }, duration: 2 });
  const camera = new THREE.PerspectiveCamera(); camera.position.set(9, 8, 7);
  const controls = { target: new THREE.Vector3(), enabled: true, enableDamping: true, update() {} };
  const signal = new AbortController(), frames: any[] = [], samples: number[][] = [], baselines = new Map();
  // The editor preserves entry identity; returning newly allocated wrappers simulates deletion/replacement.
  const entries = [{ id: 'a', name: '甲', object: actor }];
  const options = { camera, controls, entries: () => entries, environment: null, baselines,
    fps: 6, Encoder, Frame: function() {}, signal: signal.signal, sourceFingerprint: 'test-hash', mode: 'viewport', assertSource() {}, onProgress() {},
    renderFrame(_sample: any, seconds: number, timing: { timestamp: number; duration: number }) {
      samples.push([seconds, actor.position.x]); const frame = { ...timing, closed: false, close() { this.closed = true; } }; frames.push(frame); return frame;
    } };
  return { plan, options, signal, frames, samples, baselines, camera, controls, actor };
}
describe('production shot video driver', () => {
  it('逐镜走位与帧序号时间一致，复用WebM封装并保留机位/环境追溯；所有帧关闭且场景还原', async () => {
    const f = await setup(); const result = await renderScriptShotVideo(f.plan, f.options);
    expect(result.blob.type).toBe('video/webm'); expect(result.blob.size).toBeGreaterThan(100);
    expect(result.manifest.frameCount).toBe(12); expect(result.manifest.durationMs).toBe(2000); expect(result.manifest.sourceFingerprint).toBe('test-hash');
    expect(result.manifest.artifactId).toMatch(/^[0-9a-f]{8}-[0-9a-f]{4}-4[0-9a-f]{3}-[89ab][0-9a-f]{3}-[0-9a-f]{12}$/);
    expect(result.manifest.samplerVersion).toBe('script-shot-timeline-v1');
    expect(result.manifest.characterAnimationCatalogRevision).toBe(3);
    expect(result.manifest.walkProof.policy).toBe('static-xz-reject-v1');
    expect(result.manifest.walkProof.excludedIds).toEqual(['a']);
    expect(result.manifest.shots.shots[0].camera).toEqual(f.plan.shots[0].camera);
    expect(f.samples.map(sample => sample[1])).toEqual(f.samples.map(sample => sample[0]));
    expect(f.frames.every(frame => frame.closed)).toBe(true); expect(f.frames[1].timestamp).toBe(166667);
    expect(f.actor.position.x).toBe(0); expect(f.camera.position.toArray()).toEqual([9, 8, 7]); expect(f.controls.enabled).toBe(true); expect(f.baselines.size).toBe(0);
  });
  it('uses the selected frame dimensions for codec configuration, muxing and manifest without changing time sampling', async () => {
    for (const [width, height] of [[1280,720],[720,1280],[960,960],[1152,768],[768,1152]]) {
      const f = await setup(); let configured: any;
      class SizedEncoder extends Encoder {
        static async isConfigSupported(config: any) { expect(config.width).toBe(width); expect(config.height).toBe(height); return { supported: true }; }
        configure(config: any) { configured = config; super.configure(); }
      }
      const result = await renderScriptShotVideo(f.plan, { ...f.options, Encoder: SizedEncoder, size: { width, height } });
      expect(configured).toMatchObject({width,height}); expect(result.manifest).toMatchObject({width,height,frameCount:12});
      expect(f.samples.map(sample => sample[1])).toEqual(f.samples.map(sample => sample[0]));
      expect(f.camera.position.toArray()).toEqual([9,8,7]); expect(f.actor.position.x).toBe(0);
    }
  });
  it('取消、渲染失败、场景变化都拒绝半成品并恢复，已返回帧全部关闭', async () => {
    for (const failure of ['cancel', 'render', 'source'] as const) {
      const f = await setup(); let rendered = 0;
      f.options.onProgress = () => { rendered++; if (failure === 'cancel') f.signal.abort(); };
      const original = f.options.renderFrame;
      f.options.renderFrame = (...args) => { if (failure === 'render' && rendered === 1) throw Error('取帧故障'); return original(...args); };
      f.options.assertSource = () => { if (failure === 'source' && rendered === 1) throw Error('场景变化'); };
      await expect(renderScriptShotVideo(f.plan, f.options)).rejects.toThrow(failure === 'cancel' ? '取消' : failure === 'render' ? '取帧故障' : '场景变化');
      expect(f.frames.every(frame => frame.closed)).toBe(true); expect(f.actor.position.x).toBe(0); expect(f.camera.position.toArray()).toEqual([9, 8, 7]); expect(f.baselines.size).toBe(0); expect(f.controls.enabled).toBe(true);
    }
  });
  it('不支持编码器、帧预算超限及预取消均在相机或对象变动前拒绝', async () => {
    const f = await setup();
    await expect(renderScriptShotVideo(f.plan, { ...f.options, Encoder: undefined })).rejects.toThrow('不支持WebCodecs');
    const tooLong = structuredClone(f.plan); tooLong.shots[0].end = 121;
    await expect(renderScriptShotVideo(tooLong, { ...f.options, fps: 24 })).rejects.toThrow('最多2880帧');
    f.signal.abort(); await expect(renderScriptShotVideo(f.plan, f.options)).rejects.toThrow('取消');
    expect(f.frames).toHaveLength(0); expect(f.controls.enabled).toBe(true); expect(f.actor.position.x).toBe(0);
  });
});
