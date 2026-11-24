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
import { isPoseEditorHelper } from './posing/poseExport';
import { prepareSetDraft, materializeSetDraft, parseScriptScenes, buildScriptSceneDraft, listSetPersonObjects, applyScriptCharacterBindings, buildScriptShotPlan, validateScriptShotPlan } from './toonflowSets';
import { disposeObject } from './toonflow/sceneResources';
import { createStoreZip } from './zip';
import { validateCompositionDocument, validateSceneSize } from './planeDocument';
import { serializeBonePose } from './posing/bonePose';
import { saveAnimationClips, restoreAnimationClips } from './animationPreview';

const frontend = resolve(existsSync('src/studio/scene3d') ? '.' : 'frontend');
const stage = readFileSync(resolve(frontend, 'src/studio/scene3d/Scene3DStage.tsx'), 'utf8');
function callback(name: string, next: string, scope: Record<string, any>) {
  const text = stage.slice(stage.indexOf(`const ${name} = useCallback(`), stage.indexOf(`const ${next} =`));
  const start = text.indexOf('useCallback(') + 'useCallback('.length;
  const ending = [...text.matchAll(/\n[ \t]*},\s*\[/g)].at(-1);
  if (!ending) throw new Error(`Callback boundary not found: ${name}`);
  const end = ending.index! + ending[0].indexOf('}');
  const expression = text.slice(start, end + 1).replaceAll("await import('./proceduralArchive')", 'await Promise.resolve(recipeModuleForTest)').replaceAll("await import('./toonflowSets')", 'await Promise.resolve(scriptShotModuleForTest)');
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
    THREE, GLTFLoader, recipeModuleForTest: recipes, scriptShotModuleForTest: { validateScriptShotPlan },
    scriptShots: null,
    objectsRef: { current: entries }, cameraRef: { current: camera }, controlsRef: { current: { target: new THREE.Vector3(), update: vi.fn() } },
    sceneRef: { current: scene }, sceneEpochRef: { current: 0 }, frameRef: { current: '16:9' },
    fingerprintRef: { current: () => ({ full: () => 'unchanged' }) }, saveGuardRef: { current: { isDirty: () => false, assertUnchanged: (a: string, b: string) => expect(a).toBe(b) } },
    stopAnimation: vi.fn(), archiveName: 'test ancient street', skyboxName: '', skybox: {}, skyboxSourceRef: { current: null }, lightPreset: 'studio',
    moverId: null, groundPoints: {}, toGroundPath: () => null, normalizeCameraDuration: (x: any) => x, normalizeCameraFov: (x: any) => x ?? 45, normalizeCameraEasing: (x: any) => x,
    cameraKeys: [], arrayBufferToBase64: (data: ArrayBuffer) => Buffer.from(data).toString('base64'), base64ToArrayBuffer: (data: string) => Uint8Array.from(Buffer.from(data, 'base64')).buffer,
    serializeBonePose, saveAnimationClips, restoreAnimationClips, validateSceneSize, validateCompositionDocument,
    operationsRef: { current: { start: () => ({ finish: vi.fn() }) } }, moveRafRef: { current: null }, cameraPathRef: { current: null }, cameraPathTimerRef: { current: null },
    disposeSceneEntry: (entry: any) => disposeObject(entry.object), normalizeGroundPath: () => null, restoreMoveAnimation: () => -1,
    selectObject: vi.fn(), historyRef: { current: { clear: vi.fn() } }, clearSkybox: vi.fn(), sequenceFramesRef: { current: null }, referenceUrlRef: { current: null }, cleanOnRenderRef: { current: false },
    syncObjects: vi.fn(), previousMoverRef: { current: null }, normalizeMoveSeconds: () => 3, applyLightPreset: vi.fn(), placingGroundRef: { current: null },
    resizeFrameRef: { current: vi.fn() }, focalFromFov: () => 24, SPLAT_LOD_BUSY: 'splat', setError: vi.fn(),
    motionBaselineRef: { current: new Map() },
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
