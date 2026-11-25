// Execute the production save/load callback bodies; only viewport/UI/network dependencies are supplied.
import { describe, expect, it, vi } from 'vitest';
import { existsSync, readFileSync } from 'node:fs';
import { execFileSync } from 'node:child_process';
import { resolve } from 'node:path';
import * as THREE from 'three';
import { GLTFExporter } from 'three/addons/exporters/GLTFExporter.js';
import { GLTFLoader } from 'three/addons/loaders/GLTFLoader.js';
import * as recipes from './proceduralArchive';
import * as shotModule from './toonflowSets';
import { PreviewPlayhead } from './previewPlayhead';
import { AnimationPreview } from './animationPreview';
import { buildSceneFingerprint, canMoveEntry, materialRevision } from './sceneSafety';
import { isPoseEditorHelper, clonePosedContent } from './posing/poseExport';
import { isExportablePlane } from './compositing.js';
import { prepareSetDraft, materializeSetDraft, parseScriptScenes, buildScriptSceneDraft, listSetPersonObjects, applyScriptCharacterBindings, buildScriptShotPlan, validateScriptShotPlan } from './toonflowSets';
import { disposeObject } from './toonflow/sceneResources';
import { createStoreZip } from './zip';
import { validateCompositionDocument, validateSceneSize } from './planeDocument';
import { serializeBonePose, applyBonePose } from './posing/bonePose';
// 生产回调现在还会读环境（时间/天气/地形）：这里给真函数，走位边界与存档校验都走真实实现。
import { createEnvironmentTerrain, defaultSceneEnvironment, environmentLight, groundHeightAt, groundPathPointOnTerrain, proveWalkableLine, validateSceneEnvironment } from './ground-path';
import { saveAnimationClips, restoreAnimationClips, attachAnimationClip, markAnimationArchivePaths } from './animationPreview';
import { characterAnimationBinding, characterAnimationBoneNames, characterAnimationCatalogRevision, createCharacterAnimationClip, createCharacterAnimationClipFor, listCharacterAnimationCatalog, summarizeCharacterAnimationCatalog } from './toonflow/characterAnimations';
import { createToonflowPrimitive } from './toonflowPrimitives';
import { assertScriptShotWalkable, createScriptShotSampler } from './scriptShotSampler';
import { findCapsuleRigs, gaitCapsuleRig, resetCapsuleRig } from './toonflow/capsuleRig';
import { draftShotDirection } from './shotDirection';
import { retargetSkeletalClip } from './characterMotion';
import { editEnvironmentTerrain } from './environmentEdit';

const frontend = resolve(existsSync('src/studio/scene3d') ? '.' : 'frontend');
const stage = readFileSync(resolve(frontend, 'src/studio/scene3d/Scene3DStage.tsx'), 'utf8');
function callback(name: string, next: string, scope: Record<string, any>) {
  const text = stage.slice(stage.indexOf(`const ${name} = useCallback(`), stage.indexOf(`const ${next} =`));
  const start = text.indexOf('useCallback(') + 'useCallback('.length;
  const ending = [...text.matchAll(/\n[ \t]*},\s*\[/g)].at(-1);
  if (!ending) throw new Error(`Callback boundary not found: ${name}`);
  const end = ending.index! + ending[0].indexOf('}');
  const expression = text.slice(start, end + 1).replaceAll("await import('./proceduralArchive')", 'await Promise.resolve(recipeModuleForTest)').replaceAll("await import('./toonflowSets')", 'await Promise.resolve(scriptShotModuleForTest)').replaceAll("await import('./scriptShotSampler')", 'await Promise.resolve(samplerModuleForTest)').replaceAll("await import('./toonflow/characterAnimations')", 'await Promise.resolve(catalogModuleForTest)');
  // Run the project's existing TS transformer in Node's realm (esbuild cannot run in jsdom's Uint8Array realm).
  const code = execFileSync(process.execPath, ['-e', "process.stdout.write(require('esbuild').transformSync(require('fs').readFileSync(0,'utf8'),{loader:'ts',format:'cjs'}).code)"], { input: `const callback = ${expression};`, encoding: 'utf8', cwd: frontend });
  for (const name of code.match(/\bset[A-Z]\w+/g) ?? []) scope[name] ??= vi.fn();
  return new Function('scope', `with(scope) { ${code}; return callback; }`)(scope);
}
function wireProductionFingerprint(scope: Record<string, any>) {
  const start = stage.indexOf('fingerprintRef.current = () => buildSceneFingerprint(');
  const end = stage.indexOf('  if (cleanOnRenderRef.current)', start);
  if (start < 0 || end < start) throw new Error('Production fingerprint boundary missing');
  const code = execFileSync(process.execPath, ['-e', "process.stdout.write(require('esbuild').transformSync(require('fs').readFileSync(0,'utf8'),{loader:'ts',format:'cjs'}).code)"], { input: stage.slice(start, end), encoding: 'utf8', cwd: frontend });
  Object.assign(scope, { buildSceneFingerprint, materialRevision, isPoseEditorHelper,
    documentMetaRef: { current: 'motion-test' }, scrubRef: { current: null }, animationRef: { current: null } });
  new Function('scope', `with(scope) { ${code} }`)(scope);
}
function scopeFor(entries: any[]) {
  const camera = new THREE.PerspectiveCamera(); camera.position.set(18, 22, 46);
  const scene = new THREE.Scene(); entries.forEach(entry => scene.add(entry.object));
  const scope: Record<string, any> = {
    THREE, GLTFLoader, recipeModuleForTest: recipes, scriptShotModuleForTest: { validateScriptShotPlan }, catalogModuleForTest: { characterAnimationCatalogRevision },
    scriptShots: null,
    objectsRef: { current: entries }, cameraRef: { current: camera }, controlsRef: { current: { target: new THREE.Vector3(), update: vi.fn() } },
    sceneRef: { current: scene }, sceneEpochRef: { current: 0 }, frameRef: { current: '16:9' },
    fingerprintRef: { current: () => ({ full: () => 'unchanged' }) }, saveGuardRef: { current: { isDirty: () => false, assertUnchanged: (a: string, b: string) => expect(a).toBe(b) } },
    stopAnimation: vi.fn(), archiveName: 'test ancient street', skyboxName: '', skybox: {}, skyboxSourceRef: { current: null }, lightPreset: 'studio',
    moverId: null, groundPoints: {}, toGroundPath: () => null, normalizeCameraDuration: (x: any) => x, normalizeCameraFov: (x: any) => x ?? 45, normalizeCameraEasing: (x: any) => x,
    cameraKeys: [], arrayBufferToBase64: (data: ArrayBuffer) => Buffer.from(data).toString('base64'), base64ToArrayBuffer: (data: string) => Uint8Array.from(Buffer.from(data, 'base64')).buffer,
    serializeBonePose, saveAnimationClips, restoreAnimationClips, validateSceneSize, validateCompositionDocument,
    findCapsuleRigs, gaitCapsuleRig, resetCapsuleRig, samplerModuleForTest: { assertScriptShotWalkable, createScriptShotSampler },
    operationsRef: { current: { start: () => ({ finish: vi.fn() }) } }, moveRafRef: { current: null }, cameraPathRef: { current: null }, cameraPathTimerRef: { current: null },
    disposeSceneEntry: (entry: any) => disposeObject(entry.object), normalizeGroundPath: () => null, restoreMoveAnimation: () => -1,
    selectObject: vi.fn(), historyRef: { current: { clear: vi.fn() } }, clearSkybox: vi.fn(), sequenceFramesRef: { current: null }, referenceUrlRef: { current: null }, cleanOnRenderRef: { current: false },
    syncObjects: vi.fn(), previousMoverRef: { current: null }, normalizeMoveSeconds: () => 3, applyLightPreset: vi.fn(), placingGroundRef: { current: null },
    resizeFrameRef: { current: vi.fn() }, focalFromFov: () => 24, SPLAT_LOD_BUSY: 'splat', setError: vi.fn(),
    motionBaselineRef: { current: new Map() },
    // 环境是可选成员：默认 null 就是「未启用」，旧档路径仍按平地 + 灯光预设执行。
    environment: null, environmentRef: { current: null }, applyEnvironment: vi.fn(),
    groundHeightAt, groundPathPointOnTerrain, proveWalkableLine, validateSceneEnvironment,
  };
  scope.exportGeometry = async ({ recipeIds }: { recipeIds: Set<string> }) => {
    const root = new THREE.Scene();
    for (const entry of scope.objectsRef.current) if (!recipeIds.has(entry.id)) {
      const object = entry.object.clone(true); object.position.set(0, 0, 0); object.quaternion.identity(); object.scale.set(1, 1, 1); object.userData.sceneDocumentId = entry.id; root.add(object);
    }
    return await new GLTFExporter().parseAsync(root, { binary: true, onlyVisible: false });
  };
  return scope;
}
/** Read a STORE-only zip back: local headers give names and stored byte ranges. */
function readStoreZip(bytes: Uint8Array) {
  const view = new DataView(bytes.buffer, bytes.byteOffset, bytes.byteLength);
  const entries: Array<{ name: string; data: Uint8Array }> = [];
  let offset = 0;
  while (offset + 30 <= bytes.length && view.getUint32(offset, true) === 0x04034b50) {
    const size = view.getUint32(offset + 18, true);
    const nameLength = view.getUint16(offset + 26, true);
    const extraLength = view.getUint16(offset + 28, true);
    const name = new TextDecoder().decode(bytes.subarray(offset + 30, offset + 30 + nameLength));
    const start = offset + 30 + nameLength + extraLength;
    entries.push({ name, data: bytes.subarray(start, start + size) });
    offset = start + size;
  }
  return entries;
}
describe('Studio production v7 callbacks', () => {
  it('rejects mixed revisions during legacy PNG encoding without downloading, uploading or overwriting newer camera/frame edits', async () => {
    for (const operation of ['reference', 'first-last', 'sequence'] as const) for (const edit of ['material', 'object', 'frame-camera'] as const) {
      const object = new THREE.Mesh(new THREE.BoxGeometry(), new THREE.MeshStandardMaterial());
      const scope = scopeFor([{ id: 'ordinary-box', name: 'box', object, locked: false }]);
      wireProductionFingerprint(scope);
      const download = vi.fn(), fetch = vi.fn(async () => ({ ok: true, json: async () => ({ id: 1, name: 'edge.png' }) }));
      const resize = scope.resizeFrameRef.current;
      let changed = false;
      Object.assign(scope, {
        busy: false, exportSampleRef: { current: null }, editorGroundPath: () => null, setReferenceShot: vi.fn(),
        sequenceFps: 1, sequenceWidth: 720, archiveProjectId: '', followMover: false, moveSeconds: 1, moveAnimationIndex: -1,
        cameraKeys: [{ position: [1, 2, 3], target: [0, 0, 0], fov: 45 }, { position: [3, 2, 1], target: [0, 0, 0], fov: 45 }],
        firstLastCameraFrames: (keys: any[]) => ({ first: keys[0], last: keys[1] }),
        cameraPathDuration: () => 1, firstLastMotionNote: () => 'motion', cameraFrameFileName: (edge: string) => `${edge}.png`, fileNameStamp: () => 'test',
        sequencePlan: () => ({ seconds: 1, frames: 2, exceedsLimit: false }), SEQUENCE_MAX_FRAMES: 2880,
        sequenceSampleTime: (index: number) => index * .5, sampleCameraPath: (keys: any[], seconds: number) => keys[seconds ? 1 : 0],
        REFERENCE_BUSY_PREFIX: '正在渲染参考图', REFERENCE_PASS_LABELS: { white: '白模' },
        createStoreZip, fetch, document: { createElement: () => ({ click: download }) },
        URL: { createObjectURL: vi.fn(() => 'blob:test'), revokeObjectURL: vi.fn() }, window: { setTimeout: vi.fn() },
        rendererRef: { current: { getSize: (v: THREE.Vector2) => v.set(800, 450), getPixelRatio: () => 2, setPixelRatio: vi.fn(), setSize: vi.fn() } },
      });
      const mutate = () => {
        if (changed) return; changed = true;
        if (edit === 'material') (object.material as THREE.MeshStandardMaterial).color.setHex(0xff1234);
        if (edit === 'object') object.position.x = 17;
        if (edit === 'frame-camera') {
          scope.frameRef.current = '9:16'; scope.documentMetaRef.current = 'edited-frame';
          scope.cameraRef.current.position.set(77, 8, 9); scope.cameraRef.current.fov = 35;
          scope.controlsRef.current.target.set(1, 2, 3);
        }
      };
      const capture = async () => {
        // Real capture restores temporary sample state synchronously, before PNG encoding resolves.
        const sample = scope.exportSampleRef.current; scope.exportSampleRef.current = null; sample?.()();
        await Promise.resolve(); mutate();
        return { blob: { size: 8, arrayBuffer: async () => Uint8Array.of(1, 2, 3).buffer }, width: 720, height: 405 };
      };
      scope.captureReference = capture; scope.captureViewportAt = capture;
      const fn = operation === 'reference' ? callback('exportReference', 'exportScriptVideo', scope)
        : operation === 'first-last' ? callback('exportFirstLastFrames', 'copyFirstLastNote', scope)
        : callback('exportSequence', 'exportShotReferencePack', scope);
      try {
        await fn('white');
        expect(scope.setError.mock.calls.at(-1)?.[0], `${operation}/${edit}`).toMatch(/变化|改变/);
        expect(download).not.toHaveBeenCalled(); expect(fetch).not.toHaveBeenCalled();
        expect(scope.setReferenceShot).not.toHaveBeenCalled(); expect(scope.sequenceFramesRef.current).toBeNull();
        if (edit === 'frame-camera') {
          expect(scope.cameraRef.current.position.toArray()).toEqual([77, 8, 9]); expect(scope.cameraRef.current.fov).toBe(35);
          if (operation === 'sequence') expect(resize).toHaveBeenCalled();
        }
      } finally { disposeObject(object); }
    }
    console.log('LEGACY_EXPORT_REVISION=PASS operations=3 edits=3 staleOutputs=0 newerCamera=preserved');
  }, 60_000);
  it('keeps the latest selected local archive across reversed file reads, parse failures and pending GLB loads', async () => {
    const deferred = <T,>() => { let resolve!: (value: T) => void, reject!: (reason: Error) => void; const promise = new Promise<T>((ok, fail) => { resolve = ok; reject = fail; }); return { promise, resolve, reject }; };
    const object = new THREE.Mesh(new THREE.BoxGeometry(), new THREE.MeshStandardMaterial());
    const source = scopeFor([{ id: 'archive-box', name: 'box', object, locked: false }]);
    const saved = await callback('buildSceneDocument', 'saveScene', source)();
    const doc = (name: string) => JSON.stringify({ ...saved, name });
    const target = scopeFor([]);
    let busy: unknown = null;
    const finishes: ReturnType<typeof vi.fn>[] = [];
    target.operationsRef.current.start = () => { const finish = vi.fn(); finishes.push(finish); return { finish }; };
    target.setBusy = vi.fn(next => { busy = typeof next === 'function' ? next(busy) : next; });
    target.importSceneText = callback('importSceneText', 'loadScene', target);
    const load = callback('loadScene', '[archivesOpen, setArchivesOpen]', target);
    const readA = deferred<string>(), readB = deferred<string>();
    try {
      const a = load({ text: () => readA.promise });
      const b = load({ text: () => readB.promise });
      readB.resolve(doc('latest B')); await b;
      const latestObjects = target.objectsRef.current;
      readA.resolve(doc('older A')); await a;
      expect(target.objectsRef.current).toBe(latestObjects);
      expect(target.setArchiveName).toHaveBeenLastCalledWith('latest B');
      expect(target.setError.mock.calls.filter((call: any[]) => call[0])).toEqual([]);

      // A rejected old read must not clear B's loading state or publish an error.
      const oldRead = deferred<string>(), newRead = deferred<string>();
      const old = load({ text: () => oldRead.promise });
      const current = load({ text: () => newRead.promise });
      const currentBusy = busy;
      oldRead.reject(new Error('old file read failure')); await old;
      expect(busy).toBe(currentBusy); expect(busy).not.toBeNull();
      expect(target.setError.mock.calls.filter((call: any[]) => call[0])).toEqual([]);
      newRead.resolve(doc('latest read')); await current;
      expect(busy).toBeNull();

      // Complete a GLB parse from A while B is still reading: no UI mutation, no leaked geometry.
      const parsing = deferred<any>(); let pendingRoot: THREE.Object3D | undefined;
      const disposed = vi.fn();
      class DelayedLoader {
        async parseAsync() { const gltf = await new GLTFLoader().parseAsync(source.base64ToArrayBuffer(saved.model.data), ''); pendingRoot = gltf.scene; gltf.scene.traverse(node => { if ((node as THREE.Mesh).isMesh) (node as THREE.Mesh).geometry.addEventListener('dispose', disposed); }); await parsing.promise; return gltf; }
      }
      target.GLTFLoader = DelayedLoader;
      const parsingA = load({ text: async () => doc('old parsing') });
      await vi.waitFor(() => expect(pendingRoot).toBeDefined());
      const latestRead = deferred<string>();
      const loadingB = load({ text: () => latestRead.promise });
      const before = target.objectsRef.current;
      parsing.resolve(undefined); await parsingA;
      expect(target.objectsRef.current).toBe(before); expect(disposed).toHaveBeenCalled();
      expect(busy).not.toBeNull();
      expect(target.setError.mock.calls.filter((call: any[]) => call[0])).toEqual([]);
      target.GLTFLoader = GLTFLoader;
      latestRead.resolve(doc('latest after parse')); await loadingB;
      expect(target.setArchiveName).toHaveBeenLastCalledWith('latest after parse'); expect(busy).toBeNull();
      // The current request's genuine failure is visible and leaves the existing scene intact.
      const currentObjects = target.objectsRef.current;
      await load({ text: async () => { throw Error('current file failure'); } });
      expect(target.objectsRef.current).toBe(currentObjects);
      expect(target.setError).toHaveBeenLastCalledWith('current file failure'); expect(busy).toBeNull();
      await load({ text: async () => '{invalid JSON' });
      expect(target.objectsRef.current).toBe(currentObjects); expect(busy).toBeNull();
      expect(target.setError.mock.calls.at(-1)[0]).toMatch(/JSON|property|position/i);
      target.saveGuardRef.current.isDirty = () => true;
      target.window = { confirm: vi.fn(() => false) };
      const cancelledRead = vi.fn(async () => doc('cancelled'));
      const epoch = target.sceneEpochRef.current, starts = finishes.length;
      await load({ text: cancelledRead });
      expect(target.window.confirm).toHaveBeenCalledOnce(); expect(cancelledRead).not.toHaveBeenCalled();
      expect(target.sceneEpochRef.current).toBe(epoch); expect(finishes).toHaveLength(starts);
      expect(target.objectsRef.current).toBe(currentObjects); expect(busy).toBeNull();
      finishes.forEach(finish => expect(finish).toHaveBeenCalledOnce());
      console.log('STUDIO_ARCHIVE_CONCURRENCY=PASS reversedReads=latest staleError=suppressed staleBusy=preserved staleGLB=disposed currentFailure=visible malformed=atomic dirtyCancel=preserved operations=finished');
    } finally { disposeObject(object); target.objectsRef.current.forEach((entry: any) => disposeObject(entry.object)); }
  }, 60_000);
  it('keeps server archive confirmation open after a failed restore and ignores stale previews and completions', async () => {
    const target = scopeFor([]);
    const doc = JSON.stringify({ version: 6, model: { name: 'empty', data: 'AA==' }, camera: { position: [1, 2, 3], target: [0, 0, 0], fov: 45 } });
    let preview: any = null;
    target.archiveRequestRef = { current: 0 };
    target.setArchivePreview = vi.fn(next => { preview = next; });
    target.setArchivesOpen = vi.fn();
    target.importSceneText = vi.fn(async () => false);
    target.fetch = vi.fn(async () => ({ ok: true, text: async () => doc }));
    const open = callback('openSceneArchive', 'captureReference', target);
    await open({ id: 1, name: 'first' });
    const stalePreview = preview;
    await preview.restore(); // Rejected or cancelled: preserve the user's confirmation context.
    expect(preview).toBe(stalePreview); expect(target.setArchivesOpen).not.toHaveBeenCalled();
    await open({ id: 2, name: 'second' });
    target.importSceneText.mockClear();
    await stalePreview.restore();
    expect(target.importSceneText).not.toHaveBeenCalled(); expect(preview.name).toBe('second');
    let finish!: (value: boolean) => void;
    target.importSceneText.mockImplementation(() => new Promise<boolean>(ok => { finish = ok; }));
    const oldCompletion = preview.restore();
    await open({ id: 3, name: 'third' });
    finish(true); await oldCompletion;
    expect(preview.name).toBe('third'); expect(target.setArchivesOpen).not.toHaveBeenCalled();
    target.importSceneText.mockResolvedValue(true);
    await preview.restore();
    expect(preview).toBeNull(); expect(target.setArchivesOpen).toHaveBeenLastCalledWith(false);
    console.log('STUDIO_ARCHIVE_CONFIRMATION=PASS failed=retained stalePreview=blocked lateCompletion=ignored currentSuccess=closed');
  });
  it('plays recorded cuts with production callback and restores the exact viewport on stop and completion', async () => {
    const draft = await buildScriptSceneDraft(parseScriptScenes('1. 内景 现代卧室 日\n张三：走吧。\n李四：好。'), 'scene-1', new AbortController().signal);
    let plan = shotModule.updateScriptShotDuration(buildScriptShotPlan(draft), 'shot-1', 2.5);
    plan = shotModule.setScriptShotCamera(plan, 'shot-1', { position: [4, 3, 6], target: [0, 1, 0], fov: 45 });
    plan = shotModule.setScriptShotCamera(plan, 'shot-2', { position: [-4, 2, 6], target: [0, 1, 0], fov: 60 });
    const scope = scopeFor([]);
    const camera = scope.cameraRef.current;
    const controls = scope.controlsRef.current;
    controls.enabled = true; controls.enableDamping = true;
    const baseline = camera.position.toArray(); const fov = camera.fov;
    let clock = 0; let frame: (() => void) | null = null;
    Object.assign(scope, {
      busy: false, PreviewPlayhead, scriptShotModuleForTest: shotModule,
      shotPlaybackTokenRef: { current: 0 }, shotPlaybackRef: { current: null }, shotPlaybackTimerRef: { current: null }, cancelCameraPreviewRef: { current: null },
      performance: { now: () => clock }, requestAnimationFrame: vi.fn((fn: () => void) => { frame = fn; return 1; }), cancelAnimationFrame: vi.fn(),
      window: { setTimeout: vi.fn(() => 2), clearTimeout: vi.fn() },
    });
    scope.stopAnimation = () => { scope.shotPlaybackTokenRef.current++; scope.cancelCameraPreviewRef.current?.(); };
    const play = callback('playScriptShots', 'seekPreviewFrame', scope);
    await play(plan); frame!();
    expect(camera.position.toArray()).toEqual([4, 3, 6]); expect(controls.enabled).toBe(false);
    expect(scope.setShotPlayback).toHaveBeenLastCalledWith({ index: 0, shots: 2, line: '张三：走吧。' });
    clock = plan.shots[0].dialogue[0].end * 1000; frame!();
    expect(scope.setShotPlayback).toHaveBeenLastCalledWith({ index: 0, shots: 2, line: null });
    clock = plan.shots[1].start * 1000; frame!();
    expect(scope.setShotPlayback).toHaveBeenLastCalledWith({ index: 1, shots: 2, line: '李四：好。' });
    expect(camera.position.toArray()).toEqual([-4, 2, 6]); expect(camera.fov).toBe(60);
    scope.stopAnimation();
    expect(camera.position.toArray()).toEqual(baseline); expect(camera.fov).toBe(fov);
    expect(controls.enabled).toBe(true); expect(controls.enableDamping).toBe(true);
    frame!(); expect(camera.position.toArray()).toEqual(baseline);
    clock = 0; await play(plan); clock = plan.shots[1].end * 1000; frame!();
    expect(camera.position.toArray()).toEqual(baseline); expect(scope.setShotPlayback).toHaveBeenLastCalledWith(null);
    const missing = structuredClone(plan); missing.shots[1].camera = null;
    await play(missing); expect(scope.setError).toHaveBeenLastCalledWith(expect.stringContaining('未指定机位'));
    // Stop while the lazy module is resolving must prevent a late playback start.
    const pending = play(plan); scope.stopAnimation(); await pending;
    expect(camera.position.toArray()).toEqual(baseline); expect(controls.enabled).toBe(true);
    console.log('SCRIPT_PLAYBACK_CALLBACK=PASS cuts=2 stop=restored completion=restored lateStart=blocked');
  });
  it('plays explicit per-shot walks and a skeletal clip through the production callback, then restores every baseline', async () => {
    const draft = await buildScriptSceneDraft(parseScriptScenes('1. 内景 现代卧室 日\n张三：走吧。\n李四：好。'), 'scene-1', new AbortController().signal);
    let plan = buildScriptShotPlan(draft);
    plan = shotModule.updateScriptShotDuration(plan, 'shot-1', 2);
    plan = shotModule.updateScriptShotDuration(plan, 'shot-2', 1);
    plan = shotModule.setScriptShotCamera(plan, 'shot-1', { position: [4, 3, 6], target: [0, 1, 0], fov: 45 });
    plan = shotModule.setScriptShotCamera(plan, 'shot-2', { position: [-4, 2, 6], target: [0, 1, 0], fov: 60 });
    const actor = new THREE.Group(); actor.position.set(-2, 0, 0); actor.rotation.y = 0.4;
    const bone = new THREE.Bone(); bone.name = 'joint'; bone.position.set(0, 1, 0); actor.add(bone);
    actor.animations.push(new THREE.AnimationClip('walk', 2, [new THREE.VectorKeyframeTrack('joint.position', [0, 2], [0, 1, 0, 0, 1.5, 0])]));
    const prop = new THREE.Group(); prop.position.set(2, 0, 3);
    const entries = [
      { id: 'actor-1', name: '张三', object: actor, locked: false },
      { id: 'prop-1', name: '道具', object: prop, locked: false },
    ];
    plan = shotModule.setScriptShotMotion(plan, 'shot-1', {
      targetId: 'actor-1', targetName: '张三', start: { x: 0, z: 0 }, end: { x: 4, z: 0 }, duration: 2,
      animation: { index: 0, name: 'walk', duration: 2 },
    });
    plan = shotModule.setScriptShotMotion(plan, 'shot-2', {
      targetId: 'prop-1', targetName: '道具', start: { x: 2, z: 3 }, end: { x: 3, z: 4 }, duration: 0.5,
    });
    const scope = scopeFor(entries);
    const camera = scope.cameraRef.current; const controls = scope.controlsRef.current;
    controls.enabled = true; controls.enableDamping = true;
    const actorBaseline = { position: actor.position.toArray(), quaternion: actor.quaternion.toArray(), bone: bone.position.toArray() };
    const propBaseline = prop.position.toArray();
    const cameraBaseline = camera.position.toArray(); const fov = camera.fov;
    let clock = 0; let frame: (() => void) | null = null;
    Object.assign(scope, {
      busy: false, PreviewPlayhead, AnimationPreview, canMoveEntry, scriptShotModuleForTest: shotModule,
      shotPlaybackTokenRef: { current: 0 }, shotPlaybackRef: { current: null }, shotPlaybackTimerRef: { current: null }, cancelCameraPreviewRef: { current: null },
      performance: { now: () => clock }, requestAnimationFrame: vi.fn((fn: () => void) => { frame = fn; return 1; }), cancelAnimationFrame: vi.fn(),
      window: { setTimeout: vi.fn(() => 2), clearTimeout: vi.fn() },
    });
    scope.stopAnimation = () => { scope.shotPlaybackTokenRef.current++; scope.cancelCameraPreviewRef.current?.(); };
    wireProductionFingerprint(scope);
    const savedFingerprint = scope.fingerprintRef.current().full();
    const play = callback('playScriptShots', 'seekPreviewFrame', scope);
    await play(plan); frame!();
    // 第 1 镜：角色从人工起点开始，相机切到人工机位。
    expect(camera.position.toArray()).toEqual([4, 3, 6]);
    expect(actor.position.x).toBeCloseTo(0, 6); expect(actor.position.z).toBeCloseTo(0, 6);
    expect(actor.rotation.y).toBeCloseTo(Math.PI / 2, 6);
    expect(bone.position.y).toBeCloseTo(1, 6);
    expect(scope.motionBaselineRef.current.has('actor-1')).toBe(true);
    clock = 1000; frame!();
    // 第 1 镜中段：直线 XZ 位移 50%，骨骼 clip 采到 0.5 秒；相机不动。
    expect(actor.position.x).toBeCloseTo(2, 6); expect(actor.position.z).toBeCloseTo(0, 6);
    expect(bone.position.y).toBeCloseTo(1.25, 6);
    expect(camera.position.toArray()).toEqual([4, 3, 6]);
    expect(scope.fingerprintRef.current().full()).toBe(savedFingerprint);
    // Mask only temporary motion, never real edits to other fields or another object.
    prop.position.x += 1;
    expect(scope.fingerprintRef.current().full()).not.toBe(savedFingerprint);
    prop.position.x -= 1;
    actor.scale.x = 2;
    expect(scope.fingerprintRef.current().full()).not.toBe(savedFingerprint);
    actor.scale.x = 1;
    expect(scope.fingerprintRef.current().full()).toBe(savedFingerprint);
    // 第 2 镜边界：上一镜走位补到终点，第 2 镜的道具从人工起点开始。
    clock = 2000; frame!();
    expect(actor.position.x).toBeCloseTo(4, 6);
    expect(prop.position.x).toBeCloseTo(2, 6); expect(prop.position.z).toBeCloseTo(3, 6);
    expect(camera.position.toArray()).toEqual([-4, 2, 6]); expect(camera.fov).toBe(60);
    clock = 2250; frame!();
    expect(prop.position.x).toBeCloseTo(2.5, 6); expect(prop.position.z).toBeCloseTo(3.5, 6);
    // 停止：位置、朝向、骨姿、机位与控制全部回到播放前，指纹遮罩清空。
    scope.stopAnimation();
    expect(actor.position.toArray()).toEqual(actorBaseline.position);
    expect(actor.quaternion.toArray()).toEqual(actorBaseline.quaternion);
    expect(bone.position.toArray()).toEqual(actorBaseline.bone);
    expect(prop.position.toArray()).toEqual(propBaseline);
    expect(camera.position.toArray()).toEqual(cameraBaseline); expect(camera.fov).toBe(fov);
    expect(controls.enabled).toBe(true); expect(controls.enableDamping).toBe(true);
    expect(scope.motionBaselineRef.current.size).toBe(0);
    expect(scope.fingerprintRef.current().full()).toBe(savedFingerprint);
    actor.position.x += 1;
    expect(scope.fingerprintRef.current().full()).not.toBe(savedFingerprint);
    actor.position.x -= 1;
    // 自然结束同样还原，不留下走位后的角色或骨姿。
    clock = 0; await play(plan); clock = 3000; frame!();
    expect(actor.position.toArray()).toEqual(actorBaseline.position);
    expect(bone.position.toArray()).toEqual(actorBaseline.bone);
    expect(prop.position.toArray()).toEqual(propBaseline);
    expect(scope.setShotPlayback).toHaveBeenLastCalledWith(null);
    // 目标缺失、隐藏、锁定或 clip 变化都在动视口/角色之前被拒绝。
    const missing = structuredClone(plan); missing.shots[0].motion!.targetId = 'ghost';
    scope.setError.mockClear(); await play(missing);
    expect(scope.setError).toHaveBeenLastCalledWith(expect.stringContaining('不存在'));
    entries[0].object.visible = false; scope.setError.mockClear(); await play(plan);
    expect(scope.setError).toHaveBeenLastCalledWith(expect.stringContaining('隐藏'));
    entries[0].object.visible = true; entries[0].locked = true; scope.setError.mockClear(); await play(plan);
    expect(scope.setError).toHaveBeenLastCalledWith(expect.stringContaining('锁定'));
    entries[0].locked = false;
    const changed = structuredClone(plan); changed.shots[0].motion!.animation!.duration = 1.2;
    scope.setError.mockClear(); await play(changed);
    expect(scope.setError).toHaveBeenLastCalledWith(expect.stringContaining('已变化'));
    console.log('SCRIPT_MOTION_CALLBACK=PASS walk=2 actors=2 stop=restored completion=restored missing=blocked hidden=blocked locked=blocked clipMismatch=blocked');
  });
  it('matches the video sampler for capsule gait, terrain height and skeletal poses while playing in the viewport', async () => {
    const draft = await buildScriptSceneDraft(parseScriptScenes('1. 外景 现代卧室 日\n张三：走。\n李四：停。'), 'scene-1', new AbortController().signal);
    let plan = buildScriptShotPlan(draft);
    plan = shotModule.updateScriptShotDuration(plan, 'shot-1', 2);
    plan = shotModule.updateScriptShotDuration(plan, 'shot-2', 2);
    plan = shotModule.setScriptShotCamera(plan, 'shot-1', { position: [4, 3, 6], target: [0, 1, 0], fov: 45 });
    plan = shotModule.setScriptShotCamera(plan, 'shot-2', { position: [-4, 2, 6], target: [0, 1, 0], fov: 60 });
    const capsule = () => createToonflowPrimitive({ shape: 'person' } as any, '#aabbcc');
    const skeletal = () => {
      const root = new THREE.Group(); root.position.set(-3, 0, 0);
      const bone = new THREE.Bone(); bone.name = 'joint'; bone.position.set(0, 1, 0); root.add(bone);
      root.animations.push(new THREE.AnimationClip('walk', 2, [new THREE.VectorKeyframeTrack('joint.position', [0, 2], [0, 1, 0, 0, 1.5, 0])]));
      return { root, bone };
    };
    // 两套完全相同的对象：一套走生产视口回调，一套走视频随机访问采样器。
    const viewCapsule = capsule(), videoCapsule = capsule();
    const viewBone = skeletal(), videoBone = skeletal();
    const entries = [
      { id: 'capsule-1', name: '张三', object: viewCapsule, locked: false },
      { id: 'actor-1', name: '李四', object: viewBone.root, locked: false },
    ];
    const videoEntries = [
      { id: 'capsule-1', name: '张三', object: videoCapsule, locked: false },
      { id: 'actor-1', name: '李四', object: videoBone.root, locked: false },
    ];
    plan = shotModule.setScriptShotMotion(plan, 'shot-1', { targetId: 'capsule-1', targetName: '张三', start: { x: 8, z: 0 }, end: { x: 12, z: 0 }, duration: 2 });
    plan = shotModule.setScriptShotMotion(plan, 'shot-2', {
      targetId: 'actor-1', targetName: '李四', start: { x: 0, z: 0 }, end: { x: 2, z: 0 }, duration: 2,
      animation: { index: 0, name: 'walk', duration: 2 },
    });
    const environment = { ...defaultSceneEnvironment(), terrain: createEnvironmentTerrain({ size: 40, segments: 64, amplitude: 0.5, seed: 7 }) };
    const scope = scopeFor(entries);
    scope.environmentRef.current = environment;
    const camera = scope.cameraRef.current; const controls = scope.controlsRef.current;
    controls.enabled = true; controls.enableDamping = true;
    const capsuleBaseline = viewCapsule.position.toArray();
    const boneBaseline = viewBone.bone.position.toArray();
    let clock = 0; let frame: (() => void) | null = null;
    Object.assign(scope, {
      busy: false, PreviewPlayhead, AnimationPreview, canMoveEntry, scriptShotModuleForTest: shotModule,
      shotPlaybackTokenRef: { current: 0 }, shotPlaybackRef: { current: null }, shotPlaybackTimerRef: { current: null }, cancelCameraPreviewRef: { current: null },
      performance: { now: () => clock }, requestAnimationFrame: vi.fn((fn: () => void) => { frame = fn; return 1; }), cancelAnimationFrame: vi.fn(),
      window: { setTimeout: vi.fn(() => 2), clearTimeout: vi.fn() }, environment,
    });
    scope.stopAnimation = () => { scope.shotPlaybackTokenRef.current++; scope.cancelCameraPreviewRef.current?.(); };
    const videoCamera = new THREE.PerspectiveCamera(); videoCamera.position.set(18, 22, 46);
    const videoControls = { target: new THREE.Vector3(), enabled: true, enableDamping: true, update: vi.fn() };
    const baselines = new Map();
    const sampler = createScriptShotSampler(plan, { camera: videoCamera, controls: videoControls, entries: () => videoEntries, environment, baselines });
    const viewRig = findCapsuleRigs(viewCapsule)[0], videoRig = findCapsuleRigs(videoCapsule)[0];
    const legBase = viewRig.leg.quaternion.toArray();
    const play = callback('playScriptShots', 'seekPreviewFrame', scope);
    await play(plan); frame!();
    const compare = (seconds: number) => {
      clock = seconds * 1000; frame!();
      sampler.sampleAt(seconds);
      expect(viewCapsule.position.toArray()).toEqual(videoCapsule.position.toArray());
      expect(viewCapsule.rotation.y).toBe(videoCapsule.rotation.y);
      expect(viewRig.leg.quaternion.toArray()).toEqual(videoRig.leg.quaternion.toArray());
      expect(viewRig.torso.quaternion.toArray()).toEqual(videoRig.torso.quaternion.toArray());
      expect(viewRig.hips.position.toArray()).toEqual(videoRig.hips.position.toArray());
      expect(viewBone.root.position.toArray()).toEqual(videoBone.root.position.toArray());
      expect(viewBone.bone.position.toArray()).toEqual(videoBone.bone.position.toArray());
    };
    compare(0.5);
    expect(viewRig.leg.quaternion.toArray()).not.toEqual(legBase);
    compare(1.9); compare(2); compare(2.5); compare(3.9);
    // 第 2 镜不再绑该胶囊人：视频采样器与视口都在镜次边界回到站姿，但保留已走到的位置。
    expect(viewRig.leg.quaternion.toArray()).toEqual(legBase);
    expect(viewCapsule.position.x).toBeCloseTo(12, 6);
    // 停止后两边都精确还原位置与骨姿，且清空逐镜基线。
    scope.stopAnimation(); sampler.stop();
    expect(viewCapsule.position.toArray()).toEqual(capsuleBaseline);
    expect(viewCapsule.rotation.y).toBe(0);
    expect(viewRig.leg.quaternion.toArray()).toEqual(legBase);
    expect(viewBone.bone.position.toArray()).toEqual(boneBaseline);
    expect(scope.motionBaselineRef.current.size).toBe(0);
    expect(baselines.size).toBe(0);
    console.log('SCRIPT_VIEWPORT_SAMPLER_PARITY=PASS capsuleGait=1 terrain=1 skeletal=1 stop=restored');
  });
  it('replays every completed movement when browser frames skip several shots, including a delayed first frame', async () => {
    const draft = await buildScriptSceneDraft(parseScriptScenes('1. 内景 现代卧室 日\n张三：一。\n李四：二。\n张三：三。'), 'scene-1', new AbortController().signal);
    let plan = buildScriptShotPlan(draft);
    const makeEntries = () => [0, 1, 2].map(index => ({ id: `mover-${index}`, name: `角色${index}`, object: createToonflowPrimitive({ shape: 'person' } as any, '#aabbcc'), locked: false }));
    const entries = makeEntries(), videoEntries = makeEntries();
    for (let index = 0; index < 3; index++) {
      const id = `shot-${index + 1}`;
      plan = shotModule.updateScriptShotDuration(plan, id, 2);
      plan = shotModule.setScriptShotCamera(plan, id, { position: [4, 3, 6], target: [0, 1, 0], fov: 45 });
      plan = shotModule.setScriptShotMotion(plan, id, { targetId: entries[index].id, targetName: entries[index].name, start: { x: index * 3, z: 0 }, end: { x: index * 3 + 2, z: 0 }, duration: 2 });
    }
    const scope = scopeFor(entries);
    let clock = 0; let frame: (() => void) | null = null;
    Object.assign(scope, { busy: false, PreviewPlayhead, AnimationPreview, canMoveEntry, scriptShotModuleForTest: shotModule,
      shotPlaybackTokenRef: { current: 0 }, shotPlaybackRef: { current: null }, shotPlaybackTimerRef: { current: null }, cancelCameraPreviewRef: { current: null },
      performance: { now: () => clock }, requestAnimationFrame: vi.fn((fn: () => void) => { frame = fn; return 1; }), cancelAnimationFrame: vi.fn(),
      window: { setTimeout: vi.fn(() => 2), clearTimeout: vi.fn() } });
    scope.stopAnimation = () => { scope.shotPlaybackTokenRef.current++; scope.cancelCameraPreviewRef.current?.(); };
    const play = callback('playScriptShots', 'seekPreviewFrame', scope);
    for (const delayedFirstFrame of [false, true]) {
      clock = 0; await play(plan);
      if (!delayedFirstFrame) { clock = 500; frame!(); }
      clock = 4500; frame!();
      const sampler = createScriptShotSampler(plan, { camera: new THREE.PerspectiveCamera(), controls: { target: new THREE.Vector3(), enabled: true, update: vi.fn() }, entries: () => videoEntries, environment: null });
      sampler.sampleAt(4.5);
      entries.forEach((entry, index) => {
        expect(entry.object.position.toArray()).toEqual(videoEntries[index].object.position.toArray());
        const rig = findCapsuleRigs(entry.object)[0], expectedRig = findCapsuleRigs(videoEntries[index].object)[0];
        expect(rig.leg.quaternion.toArray()).toEqual(expectedRig.leg.quaternion.toArray());
      });
      expect(scope.setError).not.toHaveBeenCalled();
      scope.stopAnimation(); sampler.stop();
      entries.forEach(entry => expect(entry.object.position.toArray()).toEqual([0, 0, 0]));
      expect(scope.motionBaselineRef.current.size).toBe(0);
    }
    console.log('SCRIPT_DROPPED_FRAME_PARITY=PASS skippedShots=2 delayedFirstFrame=1 stop=restored');
  });
  it('renders one ordered reference pack per recorded shot, restores the viewport, and downloads nothing on a failed frame', async () => {
    const draft = await buildScriptSceneDraft(parseScriptScenes('1. 内景 现代卧室 日\n张三：走吧。\n李四：好。'), 'scene-1', new AbortController().signal);
    let plan = shotModule.setScriptShotCamera(buildScriptShotPlan(draft), 'shot-1', { position: [4, 3, 6], target: [0, 1, 0], fov: 45 });
    plan = shotModule.setScriptShotCamera(plan, 'shot-2', { position: [-4, 2, 6], target: [0.5, 1, 0], fov: 60 });
    const scope = scopeFor([]);
    const camera = scope.cameraRef.current;
    const controls = scope.controlsRef.current;
    controls.enabled = true; controls.enableDamping = true;
    const baseline = camera.position.toArray(); const baselineFov = camera.fov;
    const link = { href: '', download: '', click: vi.fn() };
    const downloaded: Blob[] = [];
    let rendered: string[] = [];
    let call = 0; let failAt = -1;
    Object.assign(scope, {
      busy: false,
      PreviewPlayhead,
      REFERENCE_BUSY_PREFIX: '正在渲染参考图',
      scriptShotModuleForTest: shotModule,
      createStoreZip,
      window: { setTimeout: vi.fn(() => 1) },
      document: { createElement: vi.fn(() => link) },
      URL: {
        createObjectURL: vi.fn((blob: Blob) => { downloaded.push(blob); return 'blob:pack'; }),
        revokeObjectURL: vi.fn(),
      },
      setBusy: vi.fn(), setStatus: vi.fn(), setError: vi.fn(),
      operationsRef: { current: { start: () => ({ update: vi.fn(), finish: vi.fn() }) } },
      captureReference: async (pass: string) => {
        call += 1;
        if (call === failAt) throw new Error('取不到参考图（画布为空）');
        rendered.push(`${pass}@${camera.position.toArray().map(value => value.toFixed(2)).join(',')}fov${camera.fov}`);
        return { blob: new Blob([new Uint8Array([137, 80, 78, 71])], { type: 'image/png' }), width: 1280, height: 720 };
      },
    });
    const exportPack = callback('exportShotReferencePack', 'saveReferenceToLibrary', scope);
    expect(await exportPack(plan)).toBe(true);
    // 镜序 × pass 顺序，机位就是每镜人工记录的那一组，不插值、不借用相邻镜头。
    expect(rendered).toEqual([
      'clay@4.00,3.00,6.00fov45', 'normal@4.00,3.00,6.00fov45', 'depth@4.00,3.00,6.00fov45',
      'clay@-4.00,2.00,6.00fov60', 'normal@-4.00,2.00,6.00fov60', 'depth@-4.00,2.00,6.00fov60',
    ]);
    expect(downloaded).toHaveLength(1);
    expect(link.download).toBe('scene-1-shot-reference-pack-2shots.zip');
    expect(link.click).toHaveBeenCalledTimes(1);
    const entries = readStoreZip(new Uint8Array(await downloaded[0]!.arrayBuffer()));
    expect(entries.map(entry => entry.name)).toEqual([
      'shot-01-clay.png', 'shot-01-normal.png', 'shot-01-depth.png',
      'shot-02-clay.png', 'shot-02-normal.png', 'shot-02-depth.png',
      'manifest.json',
    ]);
    const manifest = JSON.parse(new TextDecoder().decode(entries.at(-1)!.data));
    expect(manifest.shots.map((shot: any) => [shot.order, shot.shotId, shot.sampleSeconds])).toEqual([
      [1, 'shot-1', 0], [2, 'shot-2', plan.shots[0]!.end],
    ]);
    expect(manifest.shots[1].camera).toEqual(plan.shots[1]!.camera);
    expect(manifest.artifactId).toMatch(/^[0-9a-f-]{36}$/);
    expect(manifest.characterAnimationCatalogRevision).toBe(characterAnimationCatalogRevision);
    expect(manifest.walkProof.policy).toBe('static-xz-reject-v1');
    expect(manifest.output).toHaveLength(6);
    expect(manifest.output.every((file: any) => file.width === 1280 && file.height === 720)).toBe(true);
    expect(manifest.sourceFingerprint).toBe(plan.sourceFingerprint);
    expect(manifest.sceneFingerprint).toBe(plan.sceneFingerprint);
    expect(manifest.passes).toEqual(['clay', 'normal', 'depth']);
    // 采样后视口与轨道控制都回到导出前状态。
    expect(camera.position.toArray()).toEqual(baseline); expect(camera.fov).toBe(baselineFov);
    expect(controls.enabled).toBe(true); expect(controls.enableDamping).toBe(true);
    // 取图在第二镜第一张失败：整包放弃，不产生第二次下载，视口同样还原。
    call = 0; failAt = 4; rendered = []; link.click.mockClear();
    scope.setError.mockClear();
    expect(await exportPack(plan)).toBe(false);
    expect(downloaded).toHaveLength(1);
    expect(link.click).not.toHaveBeenCalled();
    expect(scope.setError).toHaveBeenLastCalledWith(expect.stringContaining('画布为空'));
    expect(camera.position.toArray()).toEqual(baseline); expect(camera.fov).toBe(baselineFov);
    expect(controls.enabled).toBe(true);
    // 缺机位在动视口之前就被模块拒绝，也不触发任何取图或下载。
    const missing = structuredClone(plan); missing.shots[1].camera = null;
    call = 0; failAt = -1; rendered = []; scope.setError.mockClear();
    expect(await exportPack(missing)).toBe(false);
    expect(rendered).toEqual([]);
    expect(link.click).not.toHaveBeenCalled();
    expect(scope.setError).toHaveBeenLastCalledWith(expect.stringContaining('未指定机位'));
    expect(manifest.exportedSceneFingerprint).toBe(shotModule.scriptFingerprint('unchanged'));
    expect(manifest.scope).toBe('static-mesh-reference-passes');
    // A live scene edit during asynchronous encoding invalidates the entire package.
    let state = 'before';
    scope.fingerprintRef.current = () => ({ full: () => state });
    scope.captureReference = async () => { state = 'after'; return { blob: new Blob(['frame']) }; };
    expect(await exportPack(plan)).toBe(false);
    expect(link.click).not.toHaveBeenCalled();
    expect(scope.setError).toHaveBeenLastCalledWith(expect.stringContaining('场景发生变化'));
    expect(camera.position.toArray()).toEqual(baseline);
    scope.objectsRef.current = [{ splat: true }];
    expect(await exportPack(plan)).toBe(false);
    expect(scope.setError).toHaveBeenLastCalledWith(expect.stringContaining('泼溅或视频平面'));
    scope.objectsRef.current = [{ mediaKind: 'video' }];
    expect(await exportPack(plan)).toBe(false);
    expect(link.click).not.toHaveBeenCalled();
    console.log(`SCRIPT_REFERENCE_PACK=PASS shots=${plan.shots.length} files=${entries.length} restored=1 partial=blocked sourceChange=blocked unsupportedMedia=blocked`);
  });
  it('samples reference packs on the video timeline, preserves earlier endpoints, and restores actors even on capture failure', async () => {
    const draft = await buildScriptSceneDraft(parseScriptScenes('1. 内景 现代卧室 日\n张三：走。\n李四：停。'), 'scene-1', new AbortController().signal);
    let plan = buildScriptShotPlan(draft);
    for (const shot of plan.shots) plan = shotModule.setScriptShotCamera(plan, shot.id, { position: [4, 3, 6], target: [0, 1, 0], fov: 45 });
    const actor = createToonflowPrimitive({ shape: 'person' } as any, '#aabbcc'); actor.position.x = -2;
    const entries = [{ id: 'capsule', name: '张三', object: actor, locked: false }];
    plan = shotModule.setScriptShotMotion(plan, 'shot-1', { targetId: 'capsule', targetName: '张三', start: { x: 0, z: 0 }, end: { x: 2, z: 0 }, duration: 0.2 });
    const scope = scopeFor(entries); const downloaded: Blob[] = [], positions: number[] = [];
    const link = { click: vi.fn() }; let fail = false;
    Object.assign(scope, { busy: false, scriptShotModuleForTest: shotModule, REFERENCE_BUSY_PREFIX: '参考图', createStoreZip,
      document: { createElement: () => link }, URL: { createObjectURL: (blob: Blob) => { downloaded.push(blob); return 'blob:pack'; }, revokeObjectURL: vi.fn() },
      window: { setTimeout: vi.fn() }, operationsRef: { current: { start: () => ({ update: vi.fn(), finish: vi.fn() }) } },
      captureReference: async () => { positions.push(actor.position.x); if (fail && positions.length === 4) throw Error('capture interrupted'); return { blob: new Blob(['png']) }; } });
    wireProductionFingerprint(scope);
    const before = scope.fingerprintRef.current().full();
    const exportPack = callback('exportShotReferencePack', 'saveReferenceToLibrary', scope);
    expect(await exportPack(plan)).toBe(true);
    expect(positions).toEqual([0, 0, 0, 2, 2, 2]);
    const manifest = JSON.parse(new TextDecoder().decode(readStoreZip(new Uint8Array(await downloaded[0].arrayBuffer())).at(-1)!.data));
    expect(manifest.motionSampling).toBe('script-shot-timeline-v1');
    expect(manifest.motionPlan).toEqual(plan);
    expect(manifest.notes.join('')).not.toContain('不播放走位');
    expect(actor.position.toArray()).toEqual([-2, 0, 0]);
    expect(scope.motionBaselineRef.current.size).toBe(0); expect(scope.fingerprintRef.current().full()).toBe(before);
    fail = true; positions.length = 0; link.click.mockClear();
    expect(await exportPack(plan)).toBe(false); expect(link.click).not.toHaveBeenCalled(); expect(downloaded).toHaveLength(1);
    expect(actor.position.toArray()).toEqual([-2, 0, 0]); expect(scope.motionBaselineRef.current.size).toBe(0);
    const invalid = structuredClone(plan); invalid.shots[0].motion!.targetId = 'missing'; positions.length = 0;
    expect(await exportPack(invalid)).toBe(false); expect(positions).toEqual([]); expect(actor.position.x).toBe(-2);
    console.log('SCRIPT_REFERENCE_MOTION=PASS timeline=shared endpoint=2 restored=1 partial=blocked invalid=blocked');
  });
  it('keeps explicitly bound character names, stable identities and edited staging through two v7 save/load cycles', async () => {
    const draft = await buildScriptSceneDraft(parseScriptScenes('1. 内景 现代卧室 日\n张三：门外有人。\n李四：等一下。'), 'scene-1', new AbortController().signal);
    const person = listSetPersonObjects(draft.setDraft)[0];
    expect(person).toBeDefined();
    const bound = applyScriptCharacterBindings(draft, [
      { character: '张三', target: { kind: 'set-person', objectId: person.id } },
      { character: '李四', target: { kind: 'placeholder', position: { x: 3.2, z: -2.1 } } },
    ]);
    const entries = materializeSetDraft(bound.setDraft);
    const source = scopeFor(entries), first = scopeFor([]), second = scopeFor([]);
    try {
      const actor = entries.find(entry => entry.name === '李四')!;
      actor.object.position.set(6.5, 0.2, -4); actor.object.rotation.y = .6;
      const saved = await callback('buildSceneDocument', 'saveScene', source)();
      expect(saved.version).toBe(7);
      await callback('importSceneText', 'loadScene', first)(JSON.stringify(saved));
      expect(first.setError.mock.calls.filter((call: any[]) => call[0])).toEqual([]);
      const again = await callback('buildSceneDocument', 'saveScene', first)();
      await callback('importSceneText', 'loadScene', second)(JSON.stringify(again));
      expect(second.setError.mock.calls.filter((call: any[]) => call[0])).toEqual([]);
      for (const placement of bound.review.bindings!) {
        const original = entries.find(entry => entry.id === placement.objectId)!;
        for (const target of [first, second]) {
          const restored = target.objectsRef.current.find((entry: any) => entry.id === placement.objectId);
          expect(restored.name).toBe(placement.character);
          expect(restored.object.position.toArray()).toEqual(original.object.position.toArray());
          expect(restored.object.quaternion.toArray()).toEqual(original.object.quaternion.toArray());
          expect(new THREE.Box3().setFromObject(restored.object).isEmpty()).toBe(false);
        }
      }
      console.log(`SCRIPT_BINDING_ARCHIVE=PASS characters=2 objects=${entries.length} cycles=2`);
    } finally {
      for (const scope of [source, first, second]) scope.objectsRef.current.forEach((entry: any) => disposeObject(entry.object));
    }
  }, 60_000);
  it('roundtrips 1577 assembled ancient-street instances under the unchanged 32MiB limit', async () => {
    const base = materializeSetDraft(await prepareSetDraft('ancientStreet', new AbortController().signal));
    const entries: any[] = [];
    for (let block = 0; block < 19; block++) for (const original of base) {
      const object = original.object.clone(true);
      object.position.x += (block % 4) * 45; object.position.z += Math.floor(block / 4) * 90;
      entries.push({ id: `${block}:${original.id}`, name: original.name, object, locked: false });
    }
    const source = scopeFor(entries); const target = scopeFor([]);
    try {
      const saved = await callback('buildSceneDocument', 'saveScene', source)();
      const text = JSON.stringify(saved); validateSceneSize(text);
      expect(saved.version).toBe(7); expect(saved.objects).toHaveLength(1577);
      expect(saved.procedural.definitions.length).toBeLessThan(83);
      await callback('importSceneText', 'loadScene', target)(text);
      expect(target.setError.mock.calls.filter((call: any[]) => call[0])).toEqual([]);
      expect(target.objectsRef.current.map((entry: any) => entry.id)).toEqual(entries.map(entry => entry.id));
      for (let i = 0; i < entries.length; i++) {
        expect(target.objectsRef.current[i].object.position.toArray()).toEqual(entries[i].object.position.toArray());
        expect(new THREE.Box3().setFromObject(target.objectsRef.current[i].object).isEmpty()).toBe(false);
      }
      console.log(`STUDIO_SCALE instances=1577 definitions=${saved.procedural.definitions.length} bytes=${new TextEncoder().encode(text).byteLength}`);
    } finally {
      target.objectsRef.current.forEach((entry: any) => disposeObject(entry.object));
      entries.forEach(entry => disposeObject(entry.object)); base.forEach(entry => disposeObject(entry.object));
    }
  }, 120_000);
  it('roundtrips mixed recipe and GLB objects, order, stable IDs, hidden/locked state and material edits', async () => {
    const entries: any[] = materializeSetDraft(await prepareSetDraft('ancientStreet', new AbortController().signal));
    entries.forEach(entry => { entry.locked = false; });
    entries[1].locked = true; entries[1].object.visible = false;
    entries[2].object.position.x += 31; entries[2].object.scale.setScalar(1.4);
    const mesh = new THREE.Mesh(new THREE.BoxGeometry(), new THREE.MeshStandardMaterial());
    entries.splice(2, 0, { id: 'external-mesh', name: 'manual box', object: mesh, locked: false });
    const source = scopeFor(entries);
    const save = callback('buildSceneDocument', 'saveScene', source);
    const doc = await save();
    expect(doc.version).toBe(7); expect(doc.objects).toHaveLength(84);
    const json = JSON.stringify(doc); validateSceneSize(json);
    const target = scopeFor([]);
    const load = callback('importSceneText', 'loadScene', target);
    await load(json);
    try {
      expect(target.setError.mock.calls.filter((call: any[]) => call[0])).toEqual([]);
      expect(target.objectsRef.current.map((entry: any) => entry.id)).toEqual(entries.map(entry => entry.id));
      expect(target.objectsRef.current[1].locked).toBe(true); expect(target.objectsRef.current[1].object.visible).toBe(false);
      expect(target.objectsRef.current[3].object.position.toArray()).toEqual(entries[3].object.position.toArray());
      expect(target.objectsRef.current[3].object.scale.toArray()).toEqual(entries[3].object.scale.toArray());
      // Unknown recipe versions must not replace a live scene, even after parsing the envelope.
      const current = target.objectsRef.current;
      const invalid = JSON.parse(json); invalid.procedural.definitions[0].version = 99;
      await load(JSON.stringify(invalid));
      expect(target.objectsRef.current).toBe(current);
      expect(target.setError.mock.calls.at(-1)[0]).toContain('版本');
      const resave = callback('buildSceneDocument', 'saveScene', target);
      expect((await resave()).version).toBe(7);
      console.log(`STUDIO_ARCHIVE mixedObjects=84 bytes=${new TextEncoder().encode(json).byteLength}`);
    } finally { target.objectsRef.current.forEach((entry: any) => disposeObject(entry.object)); entries.forEach(entry => disposeObject(entry.object)); }
  }, 60_000);
  it('roundtrips blend, palette and sparse water colors through production save/load twice and rejects malformed fields atomically', async () => {
    const terrain = createEnvironmentTerrain({ size: 40, segments: 64, amplitude: 0 });
    const count = terrain.heights.length;
    terrain.surfaceBlend = { surfaces: Array(count).fill(15), weights: Array(count).fill(0.35) };
    terrain.surfaceColors = { '15': '#c04020' }; terrain.waterLevels = Array(count).fill(null);
    terrain.surfaces[0] = 2; terrain.heights[0] = -2; terrain.waterLevels[0] = 0; terrain.waterColors = { '0': '#0088ff' };
    const expected = { ...defaultSceneEnvironment(), terrain };
    const object = new THREE.Mesh(new THREE.BoxGeometry(), new THREE.MeshStandardMaterial());
    const source = scopeFor([{ id: 'terrain-actor', name: 'terrain actor', object, locked: false }]); source.environment = expected;
    let saved = await callback('buildSceneDocument', 'saveScene', source)();
    for (let cycle = 0; cycle < 2; cycle++) {
      const target = scopeFor([]);
      target.applyEnvironment = vi.fn(next => { target.environment = next; target.environmentRef.current = next; });
      const load = callback('importSceneText', 'loadScene', target);
      await load(JSON.stringify(saved));
      try {
        expect(target.setError.mock.calls.filter((call: any[]) => call[0])).toEqual([]);
        expect(target.environment).toEqual(expected);
        const current = target.objectsRef.current, baseline = target.environment;
        const invalid = structuredClone(saved); invalid.environment.terrain.surfaceBlend.weights[0] = 1.01;
        await load(JSON.stringify(invalid));
        expect(target.objectsRef.current).toBe(current); expect(target.environment).toBe(baseline);
        expect(target.setError.mock.calls.at(-1)[0]).toContain('混合权重');
        saved = await callback('buildSceneDocument', 'saveScene', target)();
        expect(saved.environment).toEqual(expected);
      } finally { target.objectsRef.current.forEach((entry: any) => disposeObject(entry.object)); }
    }
    disposeObject(object);
    console.log('STUDIO_TERRAIN_COLOR_ARCHIVE=PASS cycles=2 invalid=atomic blend=retained palette=retained water=retained');
  }, 60_000);
  it('saves the reviewed shot plan with the scene archive, restores it, and refuses an unknown plan version without replacing the scene', async () => {
    const draft = await buildScriptSceneDraft(parseScriptScenes('1. 内景 现代卧室 日\n张三：门外有人。\n李四：等一下。'), 'scene-1', new AbortController().signal);
    const plan = shotModule.setScriptShotMotion(buildScriptShotPlan(draft), 'shot-1', {
      targetId: 'shot-stage', targetName: 'shot stage', start: { x: 0, z: 0 }, end: { x: 2, z: 1 }, duration: 0.4,
      animation: { index: 0, name: 'walk', duration: 1 },
    });
    const mesh = new THREE.Mesh(new THREE.BoxGeometry(), new THREE.MeshStandardMaterial());
    const entries = [{ id: 'shot-stage', name: 'shot stage', object: mesh, locked: false }];
    const source = scopeFor(entries); source.scriptShots = plan;
    const target = scopeFor([]);
    try {
      const saved = await callback('buildSceneDocument', 'saveScene', source)();
      expect(saved.scriptShots).toEqual(plan);
      const json = JSON.stringify(saved); validateSceneSize(json);
      const load = callback('importSceneText', 'loadScene', target);
      await load(json);
      expect(target.setError.mock.calls.filter((call: any[]) => call[0])).toEqual([]);
      expect(target.setScriptShots).toHaveBeenLastCalledWith(plan);
      expect(target.setScriptShots.mock.calls.at(-1)![0].shots[0].motion).toEqual(plan.shots[0]!.motion);
      // 老存档没有镜头表字段：载入后保持空，不补造镜头。
      const legacy = JSON.parse(json); delete legacy.scriptShots;
      await load(JSON.stringify(legacy));
      expect(target.setScriptShots).toHaveBeenLastCalledWith(null);
      // 版本不认识的镜头表必须先失败，场景保持上一次成功载入的内容。
      const current = target.objectsRef.current;
      const invalid = JSON.parse(json); invalid.scriptShots.version = 2;
      await load(JSON.stringify(invalid));
      expect(target.objectsRef.current).toBe(current);
      expect(target.setError.mock.calls.at(-1)[0]).toContain('版本');
      console.log(`SCRIPT_SHOT_ARCHIVE=PASS shots=${plan.shots.length} bytes=${new TextEncoder().encode(json).byteLength}`);
    } finally { target.objectsRef.current.forEach((entry: any) => disposeObject(entry.object)); entries.forEach(entry => disposeObject(entry.object)); }
  }, 60_000);
});

describe('Integrated enhancements through production callbacks', () => {
  it('synchronizes modern terrain wetness and sky/sun direction through the actual environment callback', () => {
    const scope=scopeFor([]), next=defaultSceneEnvironment(), modern={setRain:vi.fn(),setEnvironment:vi.fn()}; next.weather={condition:'rain',intensity:0.7,wind:0.2};
    Object.assign(scope,{terrainMeshRef:{current:{userData:{sourceTerrain:null,terrainMaterial:modern}}},weatherEffectsRef:{current:null},lightsRef:{current:{key:new THREE.DirectionalLight(),hemi:new THREE.HemisphereLight(),fill:new THREE.DirectionalLight(),rim:new THREE.DirectionalLight()}},skyboxTextureRef:{current:null},rebuildTerrainMesh:vi.fn(),createEnvironmentEffects:()=>({object:new THREE.Group(),dispose:vi.fn()}),environmentLight});
    const apply=callback('applyEnvironment','applyTerrainChange',scope); apply(next);
    expect(modern.setRain).toHaveBeenLastCalledWith(0.7); const expected=environmentLight(next), args=modern.setEnvironment.mock.calls[0];
    expect(args[0].getHexString()).toBe(new THREE.Color(expected.skyColor).getHexString()); expect(args[3].toArray()).toEqual(expected.sunDirection);
    next.weather.condition='clear'; apply(next); expect(modern.setRain).toHaveBeenLastCalledWith(0);
  });
  it('plays a confirmed director route with blend/IK and retargeted clip, then saves/loads enhancements and modern paint twice', async () => {
    const actor = createToonflowPrimitive({ shape: 'riggedPerson' } as any, '#ff8800'); actor.position.x = -3;
    const donor = createToonflowPrimitive({ shape: 'riggedPerson' } as any, '#0088ff');
    const chest = actor.getObjectByName('chest')!, sourceChest = donor.getObjectByName('chest')!;
    const performanceClip = (name: string, node: THREE.Object3D, angle: number) => new THREE.AnimationClip(name, 2, [new THREE.QuaternionKeyframeTrack(`${node.uuid}.quaternion`, [0, 2], [0,0,0,1,0,Math.sin(angle/2),0,Math.cos(angle/2)])]);
    actor.animations = [performanceClip('primary', chest, 0.2)];
    const baked = retargetSkeletalClip(donor, actor, performanceClip('source', sourceChest, 0.6), { chest: 'chest' }, 'reviewed-retarget');
    actor.animations = attachAnimationClip(actor, baked);
    const wall = new THREE.Mesh(new THREE.BoxGeometry(0.1, 2, 2), new THREE.MeshStandardMaterial()); wall.position.set(-1,1,0);
    const entries = [{id:'actor',name:'张三',object:actor,locked:false},{id:'wall',name:'墙',object:wall,locked:true}];
    const scope = scopeFor(entries), restored = scopeFor([]);
    // This fixture contains real SkinnedMesh objects. Exercise the actual
    // production exporter (SkeletonUtils + stable animation paths), not the
    // simpler non-skinned geometry stub used by the older recipe-only cases.
    for (const target of [scope, restored]) {
      Object.assign(target, { GLTFExporter, clonePosedContent, markAnimationArchivePaths, isExportablePlane, applyBonePose });
      target.exportGeometry = callback('exportGeometry','exportGlb',target);
    }
    const environment = defaultSceneEnvironment();
    environment.terrain = editEnvironmentTerrain(createEnvironmentTerrain({size:40,segments:64,amplitude:0}), {mode:'surfacePaint',x:6,z:6,radius:3,strength:0.8,height:0,surface:58,color:'#ff0022'});
    scope.environment = scope.environmentRef.current = environment;
    const draft = await buildScriptSceneDraft(parseScriptScenes('1. 内景 现代卧室 日\n张三：按确认的路线走。'), 'scene-1', new AbortController().signal);
    let plan = shotModule.updateScriptShotDuration(buildScriptShotPlan(draft), 'shot-1', 4);
    plan = draftShotDirection(plan, entries, {speakerTargets:{张三:'actor'},animationRefs:{actor:{index:0,name:'primary',duration:2}},framing:'medium',azimuth:Math.PI/2,distance:5,travel:4,avoid:true}, environment);
    plan.shots[0].motion!.characterMotion = {version:1,blend:{animation:{index:1,name:'reviewed-retarget',duration:2},weight:0.5,fadeSeconds:0},feet:{left:{hip:'leftThigh',knee:'leftShin',ankle:'leftFoot'},right:{hip:'rightThigh',knee:'rightShin',ankle:'rightFoot'},soleOffset:0.09,maxCorrection:0.2}};
    scope.scriptShots = validateScriptShotPlan(plan);
    let clock=0, frame:(()=>void)|null=null;
    Object.assign(scope,{busy:false,scriptShotModuleForTest:shotModule,shotPlaybackTokenRef:{current:0},shotPlaybackRef:{current:null},shotPlaybackTimerRef:{current:null},cancelCameraPreviewRef:{current:null},performance:{now:()=>clock},requestAnimationFrame:vi.fn((fn:()=>void)=>{frame=fn;return 1;}),cancelAnimationFrame:vi.fn(),window:{setTimeout:vi.fn(()=>2),clearTimeout:vi.fn()}});
    scope.stopAnimation=()=>{scope.shotPlaybackTokenRef.current++;scope.cancelCameraPreviewRef.current?.();};
    try {
      const baseline=actor.position.toArray(), rest=chest.quaternion.clone();
      await callback('playScriptShots','seekPreviewFrame',scope)(plan); expect(frame).not.toBeNull();
      // 1.5s avoids the intentional 2s loop boundary; blended yaw is
      // (0.2 * .75 + 0.6 * .75) / 2 = 0.3 radians, not just nonzero.
      clock=1500; frame!(); expect(Math.abs(actor.position.z)).toBeGreaterThan(1); expect(chest.quaternion.angleTo(rest)).toBeCloseTo(0.3,4);
      for(const side of ['left','right']) expect(actor.getObjectByName(side+'Foot')!.getWorldPosition(new THREE.Vector3()).y).toBeCloseTo(0.09,5);
      expect(scope.setError.mock.calls.filter((c:any[])=>c[0])).toEqual([]);
      scope.stopAnimation(); expect(actor.position.toArray()).toEqual(baseline); expect(chest.quaternion.angleTo(rest)).toBeLessThan(1e-6); expect(scope.motionBaselineRef.current.size).toBe(0);
      let document = await callback('buildSceneDocument','saveScene',scope)();
      for(let cycle=0;cycle<2;cycle++) {
        await callback('importSceneText','loadScene',restored)(JSON.stringify(document));
        expect(restored.setError.mock.calls.filter((c:any[])=>c[0])).toEqual([]);
        const nextPlan=restored.setScriptShots.mock.calls.at(-1)[0]; expect(nextPlan).toEqual(plan);
        const nextEnvironment=restored.applyEnvironment.mock.calls.at(-1)[0]; expect(nextEnvironment.terrain.surfacePaint).toEqual(environment.terrain!.surfacePaint);
        const nextActor=restored.objectsRef.current.find((e:any)=>e.id==='actor').object; expect(nextActor.animations.map((c:any)=>c.name)).toEqual(['primary','reviewed-retarget']);
        const sampler=createScriptShotSampler(nextPlan,{camera:restored.cameraRef.current,controls:restored.controlsRef.current,entries:()=>restored.objectsRef.current,environment:nextEnvironment});
        expect(sampler.walkProof.navigation!.shots[0].points.length).toBeGreaterThan(2); sampler.sampleAt(2); expect(Math.abs(nextActor.position.z)).toBeGreaterThan(1); sampler.stop();
        restored.scriptShots=nextPlan; restored.environment=nextEnvironment; document=await callback('buildSceneDocument','saveScene',restored)();
      }
      console.log('STUDIO_ENHANCEMENT_CLOSED_LOOP=PASS director=confirmed route=detour blend=sampled feet=0.09 retarget=ordinaryClip archiveCycles=2 paint=gzip');
    } finally { scope.stopAnimation(); restored.objectsRef.current.forEach((e:any)=>disposeObject(e.object)); entries.forEach(e=>disposeObject(e.object)); disposeObject(donor); }
  },60_000);
});

describe('Character animation catalog (Toonflow CC0)', () => {
  function catalogRig(bones: readonly string[]) {
    const root = new THREE.Group();
    root.name = 'riggedPerson';
    for (const name of bones) { const bone = new THREE.Bone(); bone.name = name; root.add(bone); }
    return root;
  }

  it('reports real counts, stable ids and mirrored ids as derived rather than separate models', () => {
    const summary = summarizeCharacterAnimationCatalog();
    expect(summary.revision).toBe(characterAnimationCatalogRevision);
    expect(summary.total).toBe(summary.source + summary.mirrored);
    expect(summary.total).toBe(29);
    expect(summary.source).toBe(27);
    expect(summary.mirrored).toBe(2);
    expect(summary.categories.reduce((count, item) => count + item.count, 0)).toBe(summary.total);
    const entries = listCharacterAnimationCatalog();
    expect(entries).toHaveLength(summary.total);
    expect(new Set(entries.map(entry => entry.id)).size).toBe(summary.total);
    expect(entries.filter(entry => entry.mirrored).map(entry => entry.id)).toEqual(['cc0PickUpRight', 'cc0InteractRight']);
    // 目录只收 CC0 数据：来源或时长说错的条目不允许混进来。
    for (const entry of entries) { expect(entry.license).toBe('CC0-1.0'); expect(entry.duration).toBeGreaterThan(0); }
    console.log(`ANIMATION_CATALOG total=${summary.total} source=${summary.source} mirrored=${summary.mirrored} revision=${summary.revision} categories=${summary.categories.length}`);
  });

  it('derives bone names from real tracks and keeps mirrored entries as side swaps', () => {
    const walk = characterAnimationBoneNames('cc0Walk');
    expect(walk).toHaveLength(23);
    expect(walk).toContain('hips');
    expect(walk).toContain('leftHand');
    const swap = (name: string) => name.replace(/^(left|right)/, side => side === 'left' ? 'right' : 'left');
    expect([...characterAnimationBoneNames('cc0PickUpRight')].sort()).toEqual([...new Set(characterAnimationBoneNames('cc0PickUp').map(swap))].sort());
  });

  it('binds only when every named bone exists and names exactly what is missing', () => {
    const rig = catalogRig(characterAnimationBoneNames('cc0Run'));
    expect(characterAnimationBinding(rig, 'cc0Run')).toMatchObject({ bindable: true, missing: [] });
    const clip = createCharacterAnimationClipFor(rig, 'cc0Run');
    expect(clip.name).toBe('cc0Run');
    expect(clip.tracks).toHaveLength(24);
    rig.getObjectByName('leftHand')!.removeFromParent();
    const report = characterAnimationBinding(rig, 'cc0Run');
    expect(report.bindable).toBe(false);
    expect(report.missing).toEqual(['leftHand']);
    expect(() => createCharacterAnimationClipFor(rig, 'cc0Run')).toThrow('leftHand');
    expect(() => createCharacterAnimationClip('cc0NoSuchAction')).toThrow('动作目录没有这个片段');
    // 每次给出的都是独立副本：挂到角色后改轨道不会污染共享目录。
    const first = createCharacterAnimationClip('cc0Walk'), second = createCharacterAnimationClip('cc0Walk');
    expect(first).not.toBe(second);
    expect(first.tracks[0]).not.toBe(second.tracks[0]);
    console.log(`ANIMATION_CATALOG_BINDING bones=${report.bones.length} missing=${report.missing.join(',')} tracks=${clip.tracks.length}`);
  });
});

describe('Character animation catalog attached to a scene object', () => {
  function catalogRig(id: string) {
    const root = new THREE.Group();
    root.name = 'riggedPerson';
    for (const name of characterAnimationBoneNames(id)) { const bone = new THREE.Bone(); bone.name = name; root.add(bone); }
    return root;
  }

  it('survives save/restore and drives the derived sampler with the same resource', async () => {
    const rig = catalogRig('cc0Run');
    const clip = createCharacterAnimationClipFor(rig, 'cc0Run');
    rig.animations = attachAnimationClip(rig, clip);
    expect(rig.animations).toHaveLength(1);
    // 同名动作不覆盖、不重复加入。
    expect(() => attachAnimationClip(rig, createCharacterAnimationClipFor(rig, 'cc0Run'))).toThrow('同名动作');

    // 生产保存/载入：存档只认轨道与骨骼路径，不认目录对象。
    const saved = saveAnimationClips(rig);
    expect(saved.map(item => item.name)).toEqual(['cc0Run']);
    expect(saved[0]!.duration).toBe(clip.duration);
    const json = JSON.stringify(saved);
    validateSceneSize(json);
    const restored = restoreAnimationClips(rig, JSON.parse(json));
    rig.animations = restored;
    expect(restored[0]!.name).toBe('cc0Run');
    expect(restored[0]!.duration).toBe(clip.duration);
    expect(restored[0]!.tracks).toHaveLength(clip.tracks.length);

    // 派生输出（逐镜视频/参考包共用采样器）只按 index+name+duration 绑定同一份资源。
    const draft = await buildScriptSceneDraft(parseScriptScenes('1. 内景 现代卧室 日\n张三：跑。'), 'scene-1', new AbortController().signal);
    let plan = shotModule.updateScriptShotDuration(buildScriptShotPlan(draft), 'shot-1', 2);
    plan = shotModule.setScriptShotCamera(plan, 'shot-1', { position: [4, 3, 6], target: [0, 1, 0], fov: 45 });
    plan = shotModule.setScriptShotMotion(plan, 'shot-1', {
      targetId: 'actor', targetName: '张三', start: { x: 0, z: 0 }, end: { x: 2, z: 0 }, duration: 2,
      animation: { index: 0, name: restored[0]!.name, duration: restored[0]!.duration },
    });
    const entries = [{ id: 'actor', name: '张三', object: rig, locked: false }];
    const camera = new THREE.PerspectiveCamera(); camera.position.set(0, 1.6, 6);
    const controls = { target: new THREE.Vector3(), enabled: true, update: vi.fn() };
    const sampler = createScriptShotSampler(plan, { camera, controls, entries: () => entries, environment: null });
    const hips = rig.getObjectByName('hips') as THREE.Bone;
    const restPose = hips.quaternion.toArray();
    sampler.sampleAt(0.5);
    expect(sampler.plan.shots[0]!.motion!.animation!.name).toBe('cc0Run');
    expect(hips.quaternion.toArray()).not.toEqual(restPose);
    sampler.sampleAt(1.5);
    sampler.stop();
    expect(hips.quaternion.toArray()).toEqual(restPose);
    expect(rig.position.toArray()).toEqual([0, 0, 0]);

    // 资源被替换成同名不同时长时，派生输出必须在移动任何对象之前拒绝。
    rig.animations = [{ ...restored[0]!, duration: restored[0]!.duration + 0.5 } as THREE.AnimationClip];
    expect(() => createScriptShotSampler(plan, { camera, controls, entries: () => entries, environment: null })).toThrow('骨骼动作');
    expect(rig.position.toArray()).toEqual([0, 0, 0]);
    console.log(`ANIMATION_CATALOG_ARCHIVE=PASS clip=cc0Run tracks=${restored[0]!.tracks.length} duration=${restored[0]!.duration.toFixed(3)} bytes=${new TextEncoder().encode(json).byteLength}`);
  });

  it('marks the scene dirty when an action resource is attached', () => {
    const rig = catalogRig('cc0Idle');
    const entry = { id: 'actor', name: '张三', object: rig, locked: false };
    const scope = scopeFor([entry]);
    wireProductionFingerprint(scope);
    const before = scope.fingerprintRef.current().structure;
    rig.animations = attachAnimationClip(rig, createCharacterAnimationClipFor(rig, 'cc0Idle'));
    const after = scope.fingerprintRef.current().structure;
    expect(after).not.toBe(before);
    // 稳定：没有新改动时指纹不能自己变，否则会一直显示未保存。
    expect(scope.fingerprintRef.current().structure).toBe(after);
    console.log('ANIMATION_CATALOG_DIRTY=PASS clips=1');
  });
});
