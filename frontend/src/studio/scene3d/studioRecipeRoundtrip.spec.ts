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
import { storyboardSource, captureStoryboardShot, linkedStoryboardShot } from './storyboardLink';
import { correctExecutionBeatClip } from './performanceCorrection';
import { buildScriptPerformancePlan, captureScriptPerformanceContext } from './scriptPerformance';
import { AnimationPreview } from './animationPreview';
import { buildSceneFingerprint, canMoveEntry, materialRevision } from './sceneSafety';
import { isPoseEditorHelper, clonePosedContent, hidePoseEditorHelpers } from './posing/poseExport';
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
import { assertScriptShotApplicationSafe, assertScriptShotWalkable, createScriptShotSampler } from './scriptShotSampler';
import { findCapsuleRigs, gaitCapsuleRig, resetCapsuleRig } from './toonflow/capsuleRig';
import { draftShotDirection } from './shotDirection';
import { retargetSkeletalClip } from './characterMotion';
import { sessionEvents, setSessionEvents, createPrevisEventRuntime } from './previsEvents';
import { captureEventSidecarSource, serializeEventSidecar, parseEventSidecar } from './previsEventSidecar';
import * as deliveryModuleForTest from './previsDelivery';
import { editEnvironmentTerrain } from './environmentEdit';
import { fitFrame, FRAME_VIDEO_SIZES, fovFromFocal } from './sceneFrame';
import { archiveSizeLabel, parseSceneArchiveList, sceneArchiveBlocker, sceneArchiveFileName, sceneArchiveFormBody, sceneArchiveQuery, sceneStudioContext } from './scene-archive';

const frontend = resolve(existsSync('src/studio/scene3d') ? '.' : 'frontend');
const stage = readFileSync(resolve(frontend, 'src/studio/scene3d/Scene3DStage.tsx'), 'utf8');
// esbuild must run outside jsdom's Uint8Array realm. Compile the callback catalog
// in ONE Node process, sharing its compiler service, then cache immutable JS only.
// Every invocation below still creates a fresh closure over the supplied scope.
const compiledSources = new Map<string, string>();
function compileBatch(sources: string[]) {
  const unique = [...new Set(sources)].filter(source => !compiledSources.has(source));
  if (!unique.length) return;
  const result = execFileSync(process.execPath, ['-e', "const { transformSync } = require('esbuild'); const sources = JSON.parse(require('fs').readFileSync(0,'utf8')); process.stdout.write(JSON.stringify(sources.map(source => transformSync(source,{loader:'ts',format:'cjs'}).code)));"], { input: JSON.stringify(unique), encoding: 'utf8', cwd: frontend });
  const codes: string[] = JSON.parse(result);
  if (codes.length !== unique.length) throw new Error('Incomplete production callback compilation');
  unique.forEach((source, index) => compiledSources.set(source, codes[index]));
}
function compileTypeScript(source: string) {
  compileBatch([source]);
  return compiledSources.get(source)!;
}
function callbackSource(name: string, next: string) {
  const text = stage.slice(stage.indexOf(`const ${name} = useCallback(`), stage.indexOf(`const ${next} =`));
  const start = text.indexOf('useCallback(') + 'useCallback('.length;
  const ending = [...text.matchAll(/\n[ \t]*},\s*\[/g)].at(-1);
  if (!ending) throw new Error(`Callback boundary not found: ${name}`);
  const end = ending.index! + ending[0].indexOf('}');
  const expression = text.slice(start, end + 1).replaceAll("await import('./proceduralArchive')", 'await Promise.resolve(recipeModuleForTest)').replaceAll("await import('./toonflowSets')", 'await Promise.resolve(scriptShotModuleForTest)').replaceAll("await import('./scriptShotSampler')", 'await Promise.resolve(samplerModuleForTest)').replaceAll(", import('./scriptShotSampler')", ', Promise.resolve(samplerModuleForTest)').replaceAll("await import('./toonflow/characterAnimations')", 'await Promise.resolve(catalogModuleForTest)').replaceAll("import('./scriptShotVideo')", 'Promise.resolve(videoModuleForTest)').replaceAll(", import('./toonflowSets')", ', Promise.resolve(scriptShotModuleForTest)').replaceAll("await import('./previsDelivery')", 'await Promise.resolve(deliveryModuleForTest)');
  return `const callback = ${expression};`;
}
// Discover literal callback requests from this spec, including branches/loops,
// using the same production-source extractor as the actual test executions.
const specSource = readFileSync(resolve(frontend, 'src/studio/scene3d/studioRecipeRoundtrip.spec.ts'), 'utf8');
const compilerProbeSource = 'const compilerProbe = (value: number) => value + 1;';
compileBatch([
  ...[...specSource.matchAll(/\bcallback\('([^']+)',\s*'([^']+)'/g)].map(([, name, next]) => callbackSource(name, next)),
  compilerProbeSource, compilerProbeSource.replace('value + 1', 'value + 2'),
]);
function callback(name: string, next: string, scope: Record<string, any>) {
  const code = compileTypeScript(callbackSource(name, next));
  for (const name of code.match(/\bset[A-Z]\w+/g) ?? []) scope[name] ??= vi.fn();
  return new Function('scope', `with(scope) { ${code}; return callback; }`)(scope);
}
function wireProductionFingerprint(scope: Record<string, any>) {
  const start = stage.indexOf('fingerprintRef.current = () => buildSceneFingerprint(');
  const end = stage.indexOf('  if (cleanOnRenderRef.current)', start);
  if (start < 0 || end < start) throw new Error('Production fingerprint boundary missing');
  const code = compileTypeScript(stage.slice(start, end));
  Object.assign(scope, { buildSceneFingerprint, materialRevision, isPoseEditorHelper,
    documentMetaRef: { current: 'motion-test' }, scrubRef: { current: null }, animationRef: { current: null } });
  new Function('scope', `with(scope) { ${code} }`)(scope);
}
function scopeFor(entries: any[]) {
  const camera = new THREE.PerspectiveCamera(); camera.position.set(18, 22, 46);
  const scene = new THREE.Scene(); entries.forEach(entry => scene.add(entry.object));
  const scope: Record<string, any> = {
    THREE, GLTFLoader, sessionEvents, storyboardSource, captureStoryboardShot, linkedStoryboardShot, recipeModuleForTest: recipes, scriptShotModuleForTest: { validateScriptShotPlan }, catalogModuleForTest: { characterAnimationCatalogRevision },
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
describe('Production callback compiler', () => {
  it('caches compiled source, invalidates changed source and never caches scope-bound callbacks', async () => {
    const size = compiledSources.size;
    const code = compileTypeScript(compilerProbeSource);
    expect(compileTypeScript(compilerProbeSource)).toBe(code);
    const changed = compileTypeScript(compilerProbeSource.replace('value + 1', 'value + 2'));
    expect(changed).not.toBe(code);
    expect(compiledSources.size).toBe(size);
    expect(new Function(`${code}; return compilerProbe(3);`)()).toBe(4);
    expect(new Function(`${changed}; return compilerProbe(3);`)()).toBe(5);

    const first = scopeFor([]), second = scopeFor([]);
    first.cameraRef.current.position.set(1, 2, 3); second.cameraRef.current.position.set(7, 8, 9);
    first.captureViewportAt = vi.fn(async () => ({ blob: new Blob(['first']) }));
    second.captureViewportAt = vi.fn(async () => ({ blob: new Blob(['second']) }));
    const a = callback('captureLocationSnapshot', 'exportFirstLastFrames', first);
    const compiled = compiledSources.size;
    const b = callback('captureLocationSnapshot', 'exportFirstLastFrames', second);
    expect(compiledSources.size).toBe(compiled);
    expect(a).not.toBe(b);
    expect((await a('viewport')).camera.position).toEqual([1, 2, 3]);
    expect(second.captureViewportAt).not.toHaveBeenCalled();
    expect((await b('viewport')).camera.position).toEqual([7, 8, 9]);
    expect(first.captureViewportAt).toHaveBeenCalledOnce();
    expect(second.captureViewportAt).toHaveBeenCalledOnce();
  });
});

describe('Scene Studio integration production callbacks', () => {
  it('saves source metadata in multipart and lists only the explicitly linked Scene', async () => {
    const object = new THREE.Mesh(new THREE.BoxGeometry(), new THREE.MeshStandardMaterial());
    const scope = scopeFor([{ id: 'linked-box', name: 'box', object }]);
    Object.assign(scope, { archiveProjectId: '7', studioContext: { projectId: 7, sceneId: 11 }, archiveFilter: 'scene', archiveListRequestRef: { current: 0 },
      sceneArchiveFormBody, sceneArchiveBlocker, sceneArchiveFileName, archiveSizeLabel, sceneArchiveQuery, parseSceneArchiveList,
      buildSceneDocument: callback('buildSceneDocument', 'saveScene', scope), captureCurrentView: async () => new Blob(['preview']),
      createImageBitmap: async () => ({ width: 800, height: 450, close: vi.fn() }),
      window: { document: { createElement: () => ({ getContext: () => ({ drawImage() {} }), toDataURL: () => 'data:image/jpeg;base64,AA==' }) } },
      fetch: vi.fn(async (_url: string, options?: any) => ({ ok: true, json: async () => options?.method === 'POST' ? { id: 23, project_id: 7, scene_id: 11 } : { items: [{ id: 23, project_id: 7, scene_id: 11 }], total: 1 } })) });
    scope.saveGuardRef.current.markSaved = vi.fn();
    try {
      await callback('saveSceneToServer', 'importSceneText', scope)();
      expect(scope.fetch).toHaveBeenCalledOnce();
      const body = scope.fetch.mock.calls[0][1].body as FormData;
      expect(body.get('project_id')).toBe('7'); expect(body.get('scene_id')).toBe('11'); expect(body.get('file')).toBeInstanceOf(Blob);
      expect(scope.saveGuardRef.current.markSaved).toHaveBeenCalledOnce();
      const list = callback('loadSceneArchives', 'entryArchivesRequestedRef', scope);
      expect(await list(2)).toBe(true);
      const query = new URL(scope.fetch.mock.calls[1][0], 'http://fixture').searchParams;
      expect(Object.fromEntries(query)).toEqual({ page: '2', page_size: '10', project_id: '7', scene_id: '11' });
      expect(scope.setArchives.mock.calls.at(-1)[0][0]).toMatchObject({ id: 23, projectId: 7, sceneId: 11 });
      // 旧后端若忽略scene_id，不得把普通存档伪报为场次关联成功，也不自动重试上传。
      scope.fetch.mockResolvedValueOnce({ ok: true, json: async () => ({ id: 24, project_id: 7 }) });
      await callback('saveSceneToServer', 'importSceneText', scope)();
      expect(scope.saveGuardRef.current.markSaved).toHaveBeenCalledOnce();
      expect(scope.setError).toHaveBeenLastCalledWith(expect.stringContaining('存档 #24已提交，但后端未确认场次关联'));
      scope.fetch.mockResolvedValueOnce({ ok: true, json: async () => ({ items: [{ id: 24, project_id: 7 }], total: 1 }) });
      scope.setArchives.mockClear(); expect(await list(1)).toBe(false); expect(scope.setArchives).not.toHaveBeenCalled();
      expect(scope.setError).toHaveBeenLastCalledWith(expect.stringContaining('已拒绝显示'));
      expect(scope.fetch).toHaveBeenCalledTimes(4);
      console.log('SCENE_STUDIO_ARCHIVE_LINK=PASS save=source-fields list=scene-only oldBackend=blocked noAutomaticRetry=1');
    } finally { disposeObject(object); }
  });
  it('rebinds Scene identity only after confirmed successful restore, not after failure or stale completion', async () => {
    const scope = scopeFor([]); let preview: any;
    Object.assign(scope, { sceneStudioContext, archiveRequestRef: { current: 0 },
      fetch: vi.fn(async () => ({ ok: true, text: async () => JSON.stringify({ version: 6, model: { name: 'box', data: 'AA==' }, camera: { position: [1, 2, 3], target: [0, 0, 0], fov: 45 } }) })),
      importSceneText: vi.fn(async () => false), setArchivePreview: vi.fn(value => { preview = value; }) });
    const open = callback('openSceneArchive', 'captureReference', scope);
    await open({ id: 23, projectId: 7, sceneId: 11, name: 'linked' });
    expect(preview.source).toEqual({ projectId: 7, sceneId: 11 }); expect(scope.setStudioContext).not.toHaveBeenCalled();
    await preview.restore(); expect(scope.setStudioContext).not.toHaveBeenCalled();
    scope.importSceneText.mockResolvedValue(true); await preview.restore();
    expect(scope.setStudioContext).toHaveBeenLastCalledWith({ projectId: 7, sceneId: 11 });
    expect(scope.setArchiveProjectId).toHaveBeenLastCalledWith('7'); expect(scope.setLocationGenerationOpen).toHaveBeenLastCalledWith(false);
    let finish!: (value: boolean) => void;
    await open({ id: 23, projectId: 7, sceneId: 11, name: 'old' });
    scope.importSceneText.mockImplementationOnce(() => new Promise<boolean>(resolve => { finish = resolve; }));
    const old = preview.restore(); await open({ id: 24, projectId: 8, sceneId: 12, name: 'new' });
    scope.setStudioContext.mockClear(); finish(true); await old; expect(scope.setStudioContext).not.toHaveBeenCalled();
    await preview.restore(); expect(scope.setStudioContext).toHaveBeenLastCalledWith({ projectId: 8, sceneId: 12 });
    await open({ id: 1, projectId: null, sceneId: null, name: 'legacy' }); await preview.restore();
    expect(scope.setStudioContext).toHaveBeenLastCalledWith(undefined); expect(scope.setArchiveProjectId).toHaveBeenLastCalledWith('');
  });
  for (const mode of ['clay', 'viewport'] as const) it(`captures the visible preview frame via ${mode}, restores temporary state before encoding and retains capture-time camera metadata`, async () => {
    const object = new THREE.Mesh(new THREE.BoxGeometry(), new THREE.MeshStandardMaterial({ color: 'red' })); object.position.x = 5;
    const originalMaterial = object.material, scope = scopeFor([{ id: 'actor', name: 'actor', object }]);
    const camera = scope.cameraRef.current, scene = scope.sceneRef.current, originalPosition = camera.position.toArray();
    camera.fov = fovFromFocal(92); // 产品焦距控件的有效边界，小于旧捕获路径的15度下限。
    const grid = new THREE.Object3D(); scene.add(grid); scene.background = new THREE.Color('blue'); scene.fog = new THREE.Fog('white', 1, 10);
    const background = scene.background, fog = scene.fog, blob = new Blob(['copied-frame'], { type: 'image/png' });
    let encode!: (blob: Blob | null) => void, fail = false;
    Object.assign(scope, { exportSampleRef: { current: null }, gridRef: { current: grid }, groundRef: { current: null }, groundPathGroupRef: { current: null }, transformRef: { current: null }, sparkRef: { current: null }, terrainMeshRef: { current: null },
      hidePoseEditorHelpers, clonePosedContent, isPoseEditorHelper, fovFromFocal, createReferenceMaterial: vi.fn(() => new THREE.MeshStandardMaterial()), referencePlaneMaterial: vi.fn(),
      stopAnimation: vi.fn(() => { object.position.x = 0; camera.position.set(0, 0, 0); }),
      rendererRef: { current: { domElement: { width: 800, height: 450 }, clear() {}, render: vi.fn(() => {
        if (mode === 'viewport' && grid.visible) return; // 视口路径 finally 会重画已还原的编辑画面。
        if (fail) { fail = false; throw Error('render interrupted'); }
        expect(grid.visible).toBe(false); expect(object.position.x).toBe(5); expect(camera.position.toArray()).toEqual(originalPosition); expect(camera.fov).toBe(fovFromFocal(92));
        if (mode === 'clay') { expect(object.material).not.toBe(originalMaterial); expect(scene.fog).toBeNull(); }
      }) } }, document: { createElement: () => ({ getContext: () => ({ drawImage: vi.fn() }), toBlob: (cb: typeof encode) => { encode = cb; } }) } });
    try {
      scope.captureReference = callback('captureReference', 'captureViewportAt', scope);
      scope.captureViewportAt = callback('captureViewportAt', 'captureLocationSnapshot', scope);
      const capture = callback('captureLocationSnapshot', 'exportFirstLastFrames', scope);
      const pending = capture(mode);
      expect(encode).toBeTypeOf('function'); expect(scope.stopAnimation).not.toHaveBeenCalled();
      expect(object.material).toBe(originalMaterial); expect(grid.visible).toBe(true); expect(scene.background).toBe(background); expect(scene.fog).toBe(fog);
      camera.position.set(90, 80, 70); scope.frameRef.current = '9:16';
      encode(blob); const result = await pending;
      expect(result.blob).toBe(blob); expect(result.camera).toMatchObject({ position: originalPosition, fov: fovFromFocal(92), reference_pass: mode, frame: { aspect: '16:9' } });
      expect(camera.position.toArray()).toEqual([90, 80, 70]);
      camera.position.fromArray(originalPosition); fail = true;
      await expect(capture(mode)).rejects.toThrow('render interrupted');
      expect(object.material).toBe(originalMaterial); expect(grid.visible).toBe(true); expect(scene.background).toBe(background); expect(scene.fog).toBe(fog);
      expect(scope.stopAnimation).not.toHaveBeenCalled();
      console.log(`SCENE_STUDIO_CAPTURE=PASS mode=${mode} pose=current state=restored-before-encoding metadata=capture-time failure=visible`);
    } finally { disposeObject(object); }
  });
});

describe('Studio storyboard linked capture production callback', () => {
  it.each(['viewport', 'clay'] as const)('samples selected Shot via %s, shares parent, restores before encoding and invalidates edits', async mode => {
    const actor = new THREE.Group(); actor.position.x = -2;
    const scope = scopeFor([{ id: 'actor', name: '甲', object: actor }]);
    const plan: any = { version: 1, sceneId: 'director-scene:2', heading: '院落', location: '院落', sourceFingerprint: 'source', sceneFingerprint: 'director', action: [],
      diagnostics: [JSON.stringify({ directorSource: { scene: { project_id: 1, id: 2 }, shots: [{ id: 9, scene_id: 2 }, { id: 10, scene_id: 2 }] } })],
      shots: [9, 10].map((id, i) => ({ id: `director-shot:${id}`, order: i + 1, start: i, end: i + 1, dialogue: [], camera: { position: [i + 1, 2, 5], target: [0, 0, 0], fov: 45 } })) };
    plan.shots[0].motion = { targetId: 'actor', targetName: '甲', start: { x: 0, z: 0 }, end: { x: 4, z: 0 }, duration: 1 };
    let finish!: (value: any) => void;
    const copied = vi.fn(() => { expect(actor.position.x).toBe(4); return new Promise<any>(ok => { finish = ok; }); });
    Object.assign(scope, { executionUnavailableRef: { current: false }, designShotPlanRef: { current: plan }, captureReference: copied, captureViewportAt: copied });
    wireProductionFingerprint(scope);
    const baseline = scope.fingerprintRef.current().full();
    const capture = callback('captureLocationSnapshot', 'exportFirstLastFrames', scope);
    const cameraBefore = scope.cameraRef.current.position.toArray();
    const pending = capture(mode, { context: { projectId: 1, sceneId: 2 }, shotId: 10 });
    await vi.waitFor(() => expect(copied).toHaveBeenCalledOnce());
    expect(actor.position.x).toBe(-2); expect(scope.cameraRef.current.position.toArray()).toEqual(cameraBefore);
    finish({ blob: new Blob(['png']) }); const result = await pending;
    const parent = await storyboardSource(plan, baseline, '16:9');
    expect(result.camera.storyboard_source).toMatchObject({ ...parent, projectId: 1, sceneIdNumber: 2, shotId: 10, sampleSeconds: 1 });
    expect(result.camera.position).toEqual([2, 2, 5]); expect(result.isCurrent()).toBe(true);
    // Real playback/export sampling borrows transforms, but the production
    // fingerprint uses its editing baselines. Availability only blocks NEW captures.
    scope.executionUnavailableRef.current = true;
    const sampler = createScriptShotSampler(plan, { camera: scope.cameraRef.current, controls: scope.controlsRef.current, entries: () => scope.objectsRef.current, environment: null, baselines: scope.motionBaselineRef.current });
    try {
      sampler.sampleAt(1); expect(actor.position.x).toBe(4);
      expect(result.isCurrent()).toBe(true);
      await expect(capture(mode, { context: { projectId: 1, sceneId: 2 }, shotId: 10 })).rejects.toThrow(/停止播放/);
    } finally { sampler.stop(); scope.executionUnavailableRef.current = false; }
    expect(actor.position.x).toBe(-2); expect(result.isCurrent()).toBe(true);
    actor.position.x++; expect(result.isCurrent()).toBe(false);
    await expect(capture(mode, { context: { projectId: 3, sceneId: 2 }, shotId: 10 })).rejects.toThrow(/来源/);
    scope.designShotPlanRef.current = null;
    await expect(capture(mode, { context: { projectId: 1, sceneId: 2 }, shotId: 10 })).rejects.toThrow(/先应用/);
  });
});

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
  it('seeks actual production shared samples without RAF/timers and restores on reseek, stop and invalid input', async () => {
    const draft = await buildScriptSceneDraft(parseScriptScenes('1. 内景 现代卧室 日\n张三：走吧。\n李四：好。'), 'scene-1', new AbortController().signal);
    let plan = shotModule.updateScriptShotDuration(buildScriptShotPlan(draft), 'shot-1', 2.5);
    plan = shotModule.setScriptShotCamera(plan, 'shot-1', { position: [4, 3, 6], target: [0, 1, 0], fov: 45 });
    plan = shotModule.setScriptShotCamera(plan, 'shot-2', { position: [-4, 2, 6], target: [0, 1, 0], fov: 60 });
    const scope = scopeFor([]), camera = scope.cameraRef.current, baseline = camera.position.toArray(), before = JSON.stringify(plan);
    Object.assign(scope, { busy: false, scriptShotModuleForTest: shotModule, designShotPlanRef: { current: plan },
      shotPlaybackTokenRef: { current: 0 }, shotPlaybackRef: { current: null }, shotPlaybackTimerRef: { current: null }, cancelCameraPreviewRef: { current: null },
      performance: { now: () => 0 }, requestAnimationFrame: vi.fn(), cancelAnimationFrame: vi.fn(), window: { setTimeout: vi.fn(), clearTimeout: vi.fn() } });
    scope.stopAnimation = () => { scope.shotPlaybackTokenRef.current++; scope.cancelCameraPreviewRef.current?.(); };
    const seek = callback('playScriptShots', 'seekPreviewFrame', scope);
    await seek(plan, 0);
    expect(camera.position.toArray()).toEqual([4, 3, 6]);
    expect(scope.setShotPlayback).toHaveBeenLastCalledWith({ index: 0, shots: 2, line: '张三：走吧。', holding: true });
    await seek(plan, plan.shots[1].start);
    expect(camera.position.toArray()).toEqual([-4, 2, 6]);
    expect(scope.setShotTimeline).toHaveBeenLastCalledWith({ index: 1, shots: 2, seconds: plan.shots[1].start, plan });
    expect(scope.requestAnimationFrame).not.toHaveBeenCalled(); expect(scope.window.setTimeout).not.toHaveBeenCalled();
    scope.stopAnimation(); expect(camera.position.toArray()).toEqual(baseline);
    for (const seconds of [NaN, -1, Infinity, plan.shots.at(-1)!.end]) {
      await seek(plan, seconds); expect(camera.position.toArray()).toEqual(baseline); expect(scope.setError).toHaveBeenLastCalledWith('定位时间必须在全片半开区间内');
    }
    const late = seek(plan, 1); scope.stopAnimation(); await late; expect(camera.position.toArray()).toEqual(baseline);
    const changed = seek(plan, 1); plan.heading += 'changed'; await changed;
    expect(scope.setError).toHaveBeenLastCalledWith('定位启动期间场景、计划或事件变化，请重新定位'); expect(camera.position.toArray()).toEqual(baseline);
    plan.heading = JSON.parse(before).heading; expect(JSON.stringify(plan)).toBe(before);
    const replaced = seek(plan, 1); scope.designShotPlanRef.current = structuredClone(plan); await replaced;
    expect(scope.setError).toHaveBeenLastCalledWith('定位启动期间场景、计划或事件变化，请重新定位'); expect(camera.position.toArray()).toEqual(baseline);
    scope.designShotPlanRef.current = plan;
    const altered = seek(plan, 1); scope.fingerprintRef.current = () => ({ full: () => 'changed' }); await altered;
    expect(scope.setError).toHaveBeenLastCalledWith('定位启动期间场景、计划或事件变化，请重新定位'); expect(camera.position.toArray()).toEqual(baseline);
    scope.fingerprintRef.current = () => ({ full: () => 'unchanged' });
    const eventRead = scope.sessionEvents; let eventVersion = 0; scope.sessionEvents = () => eventVersion ? [{ changed: true }] : [];
    const eventChanged = seek(plan, 1); eventVersion++; await eventChanged;
    expect(scope.setError).toHaveBeenLastCalledWith('定位启动期间场景、计划或事件变化，请重新定位'); expect(camera.position.toArray()).toEqual(baseline); scope.sessionEvents = eventRead;
    const first = seek(plan, 0), second = seek(plan, plan.shots[1].start); await Promise.all([first, second]);
    expect(camera.position.toArray()).toEqual([-4, 2, 6]); scope.stopAnimation(); expect(camera.position.toArray()).toEqual(baseline);
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
      scope.setError.mockClear(); // Assert one reset per playback attempt, not the cumulative loop count.
      clock = 0; await play(plan);
      if (!delayedFirstFrame) { clock = 500; frame!(); }
      clock = 4500; frame!();
      expect(scope.setShotTimeline).toHaveBeenLastCalledWith({ plan, index: 2, shots: 3, seconds: 4.5 });
      expect(scope.setShotTimeline.mock.calls.at(-1)[0].plan).toBe(plan);
      const published = scope.setShotTimeline.mock.calls.length;
      clock = 4510; frame!(); expect(scope.setShotTimeline).toHaveBeenCalledTimes(published);
      clock = 4610; frame!(); expect(scope.setShotTimeline).toHaveBeenLastCalledWith({ plan, index: 2, shots: 3, seconds: 4.61 });
      clock = 4500; frame!();
      const sampler = createScriptShotSampler(plan, { camera: new THREE.PerspectiveCamera(), controls: { target: new THREE.Vector3(), enabled: true, update: vi.fn() }, entries: () => videoEntries, environment: null });
      sampler.sampleAt(4.5);
      entries.forEach((entry, index) => {
        expect(entry.object.position.toArray()).toEqual(videoEntries[index].object.position.toArray());
        const rig = findCapsuleRigs(entry.object)[0], expectedRig = findCapsuleRigs(videoEntries[index].object)[0];
        expect(rig.leg.quaternion.toArray()).toEqual(expectedRig.leg.quaternion.toArray());
      });
      // Starting a new attempt now explicitly clears stale feedback; null is not an error.
      expect(scope.setError).toHaveBeenCalledExactlyOnceWith(null);
      scope.stopAnimation(); sampler.stop();
      expect(scope.setShotTimeline).toHaveBeenLastCalledWith(null);
      entries.forEach(entry => expect(entry.object.position.toArray()).toEqual([0, 0, 0]));
      expect(scope.motionBaselineRef.current.size).toBe(0);
    }
    console.log('SCRIPT_DROPPED_FRAME_PARITY=PASS skippedShots=2 delayedFirstFrame=1 stop=restored');
  });
  it.each(['success', 'cancel', 'events'])('prepares a G3 reference child without downloading and restores viewport: %s', async outcome => {
    const draft = await buildScriptSceneDraft(parseScriptScenes('1. 内景 现代卧室 日\n张三：走吧。'), 'scene-1', new AbortController().signal);
    const plan = shotModule.setScriptShotCamera(buildScriptShotPlan(draft), 'shot-1', { position: [4, 3, 6], target: [0, 1, 0], fov: 45 });
    const scope = scopeFor([]), baseline = scope.cameraRef.current.position.clone(), request = new AbortController();
    const download = vi.fn(), prepared = vi.fn(), finish = vi.fn();
    let captures = 0;
    Object.assign(scope, { busy: false, REFERENCE_BUSY_PREFIX: '正在渲染参考图', scriptShotModuleForTest: shotModule, deliveryModuleForTest, createStoreZip,
      operationsRef: { current: { start: () => ({ update: vi.fn(), finish }) } },
      document: { createElement: download }, URL: { createObjectURL: download }, window: { setTimeout: vi.fn() },
      captureReference: async () => {
        captures++;
        if (outcome === 'cancel') request.abort();
        if (outcome === 'events') setSessionEvents(scope.sceneRef.current, [{ kind: 'fire', objectId: 'changed', start: 0, end: 1, offset: [0, 0, 0], rotation: [0, 0, 0], scale: 1, velocity: [0, 0, 0] }]);
        return { blob: new Blob(['png-fixture'], { type: 'image/png' }), width: 1280, height: 720 };
      },
    });
    const result = await callback('exportShotReferencePack', 'saveReferenceToLibrary', scope)(plan, true, { signal: request.signal, onPrepared: prepared });
    expect(result).toBe(outcome === 'success'); expect(download).not.toHaveBeenCalled(); expect(finish).toHaveBeenCalledOnce();
    expect(scope.cameraRef.current.position.toArray()).toEqual(baseline.toArray());
    if (outcome === 'success') {
      expect(prepared).toHaveBeenCalledOnce(); const child = prepared.mock.calls[0][0];
      const entries = readStoreZip(new Uint8Array(await child.blob.arrayBuffer()));
      expect(entries.map(e => e.name)).toEqual(expect.arrayContaining(['shot-01-clay.png', 'shot-01-normal.png', 'shot-01-depth.png', 'shots.html', 'shots.csv', 'timeline.edl', 'delivery.json', 'manifest.json']));
      expect(child.manifest.sessionEvents).toEqual([]); expect(child.manifest.motionPlan).toEqual(plan); expect(captures).toBe(3);
    } else { expect(prepared).not.toHaveBeenCalled(); expect(captures).toBe(1); expect(scope.setError).toHaveBeenLastCalledWith(expect.stringContaining(outcome === 'cancel' ? '取消' : '事件发生变化')); }
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

function requirementsFixture() {
  const object = new THREE.Mesh(new THREE.BoxGeometry(), new THREE.MeshStandardMaterial());
  const plan = validateScriptShotPlan({ version: 1, sceneId: 'review', heading: '院落', location: '院落', sourceFingerprint: 'source', sceneFingerprint: 'review', action: [], diagnostics: [],
    shots: [{ id: 'one', order: 1, start: 0, end: 1, camera: { position: [0, 2, 5], target: [0, 0, 0], fov: 45 }, dialogue: [] }],
    sceneRequirements: { version: 1, sourceRevision: 'review', sourceContent: '院中一块石。', entities: [{ id: 'stone', label: '石', count: 1, objectIds: ['stone-object'], basis: { kind: 'fact', start: 4, end: 5, quote: '石' } }], relations: [], unresolved: [] } });
  return { entries: [{ id: 'stone-object', name: '石', object, locked: false }], plan };
}
describe('Declared scenery requirements through production callbacks', () => {
  it('validates requirements-only shot publication without actors before setting editor state', () => {
    const f = requirementsFixture(), scope = scopeFor(f.entries);
    const start = stage.indexOf('onApplyShots={') + 'onApplyShots={'.length;
    const end = stage.indexOf('} appliedPlan=', start);
    expect(start).toBeGreaterThan('onApplyShots={'.length); expect(end).toBeGreaterThan(start);
    const code = compileTypeScript(`const apply = ${stage.slice(start, end)};`);
    Object.assign(scope, { assertScriptShotApplicationSafe, setScriptShots: vi.fn(), setStatus: vi.fn() });
    const apply = new Function('scope', `with(scope) { ${code}; return apply; }`)(scope);
    try {
      f.entries[0].object.visible = false;
      expect(() => apply(f.plan)).toThrow('不可见'); expect(scope.setScriptShots).not.toHaveBeenCalled();
      f.entries[0].object.visible = true; f.plan.sceneRequirements!.unresolved.push('未确认道路');
      expect(() => apply(f.plan)).toThrow('未确认道路'); expect(scope.setScriptShots).not.toHaveBeenCalled();
      f.plan.sceneRequirements!.unresolved = []; scope.objectsRef.current = [];
      expect(() => apply(f.plan)).toThrow('缺少对象'); expect(scope.setScriptShots).not.toHaveBeenCalled();
      scope.objectsRef.current = f.entries; apply(f.plan); expect(scope.setScriptShots).toHaveBeenCalledExactlyOnceWith(f.plan);
    } finally { disposeObject(f.entries[0].object); }
  });
  for (const version of [6, 7]) it(`preserves exact source and human mappings through real v${version} archives and rejects forged evidence atomically`, async () => {
    const f = requirementsFixture();
    const extra = version === 7 ? materializeSetDraft(await prepareSetDraft('ancientStreet', new AbortController().signal)).slice(0, 1) : [];
    const source = scopeFor([...f.entries, ...extra]), target = scopeFor([]); source.scriptShots = f.plan;
    try {
      for (let cycle = 0; cycle < 2; cycle++) {
        const saved = await callback('buildSceneDocument', 'saveScene', cycle ? target : source)(); expect(saved.version).toBe(version);
        await callback('importSceneText', 'loadScene', target)(JSON.stringify(saved));
        expect(target.setError.mock.calls.filter((c: any[]) => c[0])).toEqual([]);
        const restored = target.setScriptShots.mock.calls.at(-1)[0]; expect(restored.sceneRequirements).toEqual(f.plan.sceneRequirements);
        const sampler = createScriptShotSampler(restored, { camera: target.cameraRef.current, controls: target.controlsRef.current, entries: () => target.objectsRef.current, environment: null });
        expect(sampler.walkProof.sceneRequirements).toMatchObject({ entities: 1, coverage: 'declared-only' }); sampler.stop();
        const current = target.objectsRef.current;
        const invalid = JSON.parse(JSON.stringify(saved)); invalid.scriptShots.sceneRequirements.entities[0].basis.quote = '伪造';
        await callback('importSceneText', 'loadScene', target)(JSON.stringify(invalid));
        expect(target.objectsRef.current).toBe(current); expect(target.setError.mock.calls.at(-1)[0]).toContain('证据'); target.setError.mockClear();
        target.scriptShots = restored;
      }
    } finally { for (const scope of [source, target]) scope.objectsRef.current.forEach((e: any) => disposeObject(e.object)); }
  }, 60_000);
});

describe('Declared connected roads through production archives', () => {
  for (const version of [6, 7]) it(`preserves road points and source through v${version} save/reopen and rechecks a newly broken road`, async () => {
    const mesh = (id: string, x: number, w: number, h: number, y: number) => {
      const object = new THREE.Mesh(new THREE.BoxGeometry(w, h, 1), new THREE.MeshStandardMaterial()); object.position.set(x, y, 0);
      return { id, name: id, object, locked: false };
    };
    const entries = [mesh('road', 0, 4, .1, -.05), mesh('camp', -3, 1, 1, .5), mesh('bank', 3, 1, 1, .5)];
    const extra = version === 7 ? materializeSetDraft(await prepareSetDraft('ancientStreet', new AbortController().signal)).slice(0, 1) : [];
    extra.forEach(e => e.object.position.x = 100);
    const source = scopeFor([...entries, ...extra]), target = scopeFor([]);
    const content = '军营外的小路通向潭边。';
    const basis = { kind: 'fact', start: 0, end: content.length, quote: content };
    const plan = validateScriptShotPlan({ ...requirementsFixture().plan, sceneRequirements: {
      version: 1, sourceRevision: 'review', sourceContent: content, entities: entries.map(e => ({ id: e.id, label: e.name, count: 1, objectIds: [e.id], basis })),
      relations: [{ id: 'route', kind: 'path-connected', subject: 'road', reference: 'camp', other: 'bank', points: [[-2, 0, 0], [2, 0, 0]], maxEndpointGapMetres: .5, stagingNote: '坐标和端点距离为人工布景', basis }], unresolved: [] } });
    source.scriptShots = plan;
    try {
      for (let cycle = 0; cycle < 2; cycle++) {
        const saved = await callback('buildSceneDocument', 'saveScene', cycle ? target : source)(); expect(saved.version).toBe(version);
        await callback('importSceneText', 'loadScene', target)(JSON.stringify(saved));
        expect(target.setError.mock.calls.filter((c: any[]) => c[0])).toEqual([]);
        const restored = target.setScriptShots.mock.calls.at(-1)[0]; expect(restored.sceneRequirements).toEqual(plan.sceneRequirements);
        const options = { camera: target.cameraRef.current, controls: target.controlsRef.current, entries: () => target.objectsRef.current, environment: null };
        const sampler = createScriptShotSampler(restored, options); expect(sampler.walkProof.sceneRequirements?.paths).toHaveLength(1); sampler.stop();
        const road = target.objectsRef.current.find((e: any) => e.id === 'road').object;
        const scale = road.scale.clone(); road.scale.x *= .9;
        expect(() => createScriptShotSampler(restored, options)).toThrow('路面不连续'); road.scale.copy(scale);
        target.scriptShots = restored;
      }
      console.log(`CONNECTED_ROAD_ARCHIVE=PASS version=${version} cycles=2 source=preserved geometryDrift=rejected`);
    } finally { for (const scope of [source, target]) scope.objectsRef.current.forEach((e: any) => disposeObject(e.object)); }
  });
});

describe('Shot operation feedback through production callbacks', () => {
  for (const entry of ['play', 'reference', 'video']) {
    for (const failure of ['viewport', 'plan', 'road']) it(`${entry} clears previous success and error before ${failure} rejection`, async () => {
      const mesh = (id: string, x: number, w: number, h: number, y: number) => {
        const object = new THREE.Mesh(new THREE.BoxGeometry(w, h, 1), new THREE.MeshStandardMaterial()); object.position.set(x, y, 0);
        return { id, name: id, object, locked: false };
      };
      const entries = [mesh('road', 0, 4, .1, -.05), mesh('camp', -3, 1, 1, .5), mesh('bank', 3, 1, 1, .5)];
      const scope = scopeFor(entries), camera = scope.cameraRef.current;
      const basis = { kind: 'assumption', reason: '隔离人工测试布局，不是小说抽取' };
      const plan = validateScriptShotPlan({ version: 1, sceneId: 'feedback', heading: '反馈测试', location: '平地', sourceFingerprint: 'source', sceneFingerprint: 'review', action: [], diagnostics: [],
        shots: [{ id: 'one', order: 1, start: 0, end: 1, camera: { position: [0, 2, 5], target: [0, 0, 0], fov: 45 }, dialogue: [] }],
        sceneRequirements: { version: 1, sourceRevision: 'review', sourceContent: '人工道路夹具', entities: entries.map(e => ({ id: e.id, label: e.id, count: 1, objectIds: [e.id], basis })),
          relations: [{ id: 'route', kind: 'path-connected', subject: 'road', reference: 'camp', other: 'bank', points: [[-2, 0, 0], [2, 0, 0]], maxEndpointGapMetres: .5, stagingNote: '测试坐标', basis }], unresolved: [] } });
      let status = '逐镜视频已生成：旧任务', error: string | null = '旧错误';
      Object.assign(scope, {
        busy: false, setStatus: vi.fn(value => { status = value; }), setError: vi.fn(value => { error = value; }),
        // Stopping an existing preview may itself publish a completion message.
        stopAnimation: vi.fn(() => { status = '已停止旧预演'; }),
        shotPlaybackTokenRef: { current: 0 }, scriptShotModuleForTest: shotModule,
        REFERENCE_BUSY_PREFIX: '参考图', shotVideoExportRef: { current: null }, FRAME_VIDEO_SIZES,
        rendererRef: { current: { domElement: { width: 1280, height: 720 }, getPixelRatio: () => 1, setPixelRatio: vi.fn(), setSize: vi.fn() } },
        captureReference: vi.fn(), captureViewportAt: vi.fn(),
        videoModuleForTest: { renderScriptShotVideo: vi.fn(async (value: any, options: any) => {
          createScriptShotSampler(value, options); throw Error('Unexpected valid fixture');
        }) },
      });
      const baseline = camera.position.toArray();
      if (failure === 'viewport') scope.cameraRef.current = null;
      else if (failure === 'plan') plan.shots[0].camera = null;
      else entries[0].object.position.z = 2;
      const expected = failure === 'viewport' ? '视口尚未就绪' : failure === 'plan' ? '未指定机位' : '路面不连续';
      const run = entry === 'play' ? callback('playScriptShots', 'seekPreviewFrame', scope)
        : entry === 'reference' ? callback('exportShotReferencePack', 'saveReferenceToLibrary', scope)
        : callback('exportScriptVideo', 'exportSequence', scope);
      try {
        const pending = entry === 'video' ? run(plan, 12, 'viewport', new AbortController().signal) : run(plan);
        // Feedback must be reset synchronously, before lazy imports/encoding settle.
        const initialStatus = status, initialError = error;
        if (entry === 'video') await expect(pending).rejects.toThrow(expected);
        else await pending;
        expect(initialStatus).toBe('');
        if (failure !== 'viewport') expect(initialError).toBeNull();
        if (entry !== 'video') expect(error).toContain(expected);
        expect(status).toBe('');
        expect(camera.position.toArray()).toEqual(baseline);
        expect(scope.captureReference).not.toHaveBeenCalled(); expect(scope.captureViewportAt).not.toHaveBeenCalled();
      } finally { entries.forEach(e => disposeObject(e.object)); }
    });
    it(`${entry} does not clear active operation feedback when busy`, async () => {
      const scope = scopeFor([]); Object.assign(scope, { busy: true, setStatus: vi.fn(), setError: vi.fn() });
      const run = entry === 'play' ? callback('playScriptShots', 'seekPreviewFrame', scope)
        : entry === 'reference' ? callback('exportShotReferencePack', 'saveReferenceToLibrary', scope)
        : callback('exportScriptVideo', 'exportSequence', scope);
      if (entry === 'video') await expect(run({}, 12, 'viewport', new AbortController().signal)).rejects.toThrow('请等待');
      else await run({});
      expect(scope.stopAnimation).not.toHaveBeenCalled(); expect(scope.setStatus).not.toHaveBeenCalled(); expect(scope.setError).not.toHaveBeenCalled();
    });
  }
});

describe('Integrated enhancements through production callbacks', () => {
  it('loads an environment-free archive using its saved light, not the callback closure light', async () => {
    const object = new THREE.Mesh(new THREE.BoxGeometry(), new THREE.MeshStandardMaterial());
    const source = scopeFor([{ id: 'light-box', name: 'box', object, locked: false }]);
    source.lightPreset = 'night';
    const saved = await callback('buildSceneDocument', 'saveScene', source)();
    delete saved.environment; // Legacy archive, no environment field.
    const scope = scopeFor([]);
    const lights = { key: new THREE.DirectionalLight(), hemi: new THREE.HemisphereLight(), fill: new THREE.DirectionalLight(), rim: new THREE.DirectionalLight() };
    // Compile the actual preset table and actual application callbacks.
    const table = stage.slice(stage.indexOf('const LIGHT_PRESETS:'), stage.indexOf('const WEATHER_LABELS:'));
    const code = compileTypeScript(table);
    Object.assign(scope, { skyboxTextureRef: { current: null }, LIGHT_PRESETS: new Function(`${code};return LIGHT_PRESETS;`)(), lightsRef: { current: lights }, terrainMeshRef: { current: null }, weatherEffectsRef: { current: null }, rebuildTerrainMesh: vi.fn() });
    scope.applyLightPreset = callback('applyLightPreset', 'rebuildTerrainMesh', scope);
    scope.applyEnvironment = callback('applyEnvironment', 'applyTerrainChange', scope);
    try {
      await callback('importSceneText', 'loadScene', scope)(JSON.stringify(saved));
      expect(scope.setError.mock.calls.filter((call: any[]) => call[0])).toEqual([]);
      expect(scope.setLightPreset).toHaveBeenLastCalledWith('night');
      expect(lights.key.intensity).toBe(scope.LIGHT_PRESETS.night.key.intensity);
      expect(scope.sceneRef.current.background.getHexString()).toBe('080b14');
    } finally { disposeObject(object); scope.objectsRef.current.forEach((entry: any) => disposeObject(entry.object)); }
  });
  it.each(Object.keys(FRAME_VIDEO_SIZES))('keeps %s video size through container resize and restores the latest viewport after success, cancel or failure', async frame => {
    for (const outcome of ['success', 'cancel', 'wrong-size']) {
      const scope = scopeFor([]), controller = new AbortController();
      let ratio = 2;
      const canvas = { width: 1600, height: 900 };
      const container = { clientWidth: 800, clientHeight: 450 };
      const renderer = { domElement: canvas, getPixelRatio: () => ratio, setPixelRatio: (value: number) => { ratio = value; },
        getSize: (v: THREE.Vector2) => v.set(canvas.width / ratio, canvas.height / ratio),
        setSize: (width: number, height: number) => { canvas.width = width * ratio; canvas.height = height * ratio; } };
      Object.assign(scope, { busy: false, archiveProjectId: '7', FRAME_VIDEO_SIZES, frameRef: { current: frame }, shotVideoExportRef: { current: null }, rendererRef: { current: renderer },
        weatherEffectsRef: { current: null }, scriptShotModuleForTest: shotModule, globalThis: { VideoEncoder: class {}, VideoFrame: class {} },
        createImageBitmap: async () => ({ close() {} }), captureViewportAt: vi.fn(async () => ({ blob: {}, width: canvas.width, height: canvas.height })) });
      const resizeStart = stage.indexOf('resizeFrameRef.current = () => {'), resizeEnd = stage.indexOf('const observer =', resizeStart);
      new Function('scope', `with(scope) { ${stage.slice(resizeStart, resizeEnd)} }`)({ ...scope, container, renderer, camera: scope.cameraRef.current, fitFrame });
      scope.videoModuleForTest = { renderScriptShotVideo: async (_plan: unknown, options: any) => {
        container.clientWidth = 640; container.clientHeight = 480;
        scope.resizeFrameRef.current();
        expect(canvas).toEqual(FRAME_VIDEO_SIZES[frame as keyof typeof FRAME_VIDEO_SIZES]);
        if (outcome === 'cancel') { controller.abort(); options.assertSource(); }
        if (outcome === 'wrong-size') canvas.width -= 2;
        await options.renderFrame({ camera: { position: [1,2,3], target: [0,0,0], fov:45 } }, 0, { timestamp: 0, duration: 1000000 });
        return { blob: {}, manifest: { frameCount: 1 } };
      } };
      // Provenance now validates the plan before the encoder stub. Use a real plan;
      // keep every sizing/cancel/restoration assertion and the encoder failure injection.
      const plan = buildScriptShotPlan(await buildScriptSceneDraft(parseScriptScenes('1. 内景 现代卧室 日\n张三：走吧。'), 'scene-1', controller.signal));
      const run = callback('exportScriptVideo', 'exportSequence', scope)(plan, 1, 'viewport', controller.signal);
      if (outcome === 'success') await expect(run).resolves.toMatchObject({ projectId: 7 });
      else await expect(run).rejects.toThrow(outcome === 'cancel' ? '取消' : '尺寸');
      expect(scope.shotVideoExportRef.current).toBe(null); expect(ratio).toBe(2);
      const latest = fitFrame(container.clientWidth, container.clientHeight, frame as any);
      expect(canvas).toEqual({ width: latest.width * 2, height: latest.height * 2 });
      expect(scope.cameraRef.current.aspect).toBe(latest.aspect);
    }
  });
  it.each([['white', 'clay'], ['normal', 'normal'], ['depth', 'depth']] as const)('routes video mode %s to the actual %s reference pass', async (mode, pass) => {
    const scope = scopeFor([]), canvas = { width: 1280, height: 720 };
    Object.assign(scope, { busy: false, archiveProjectId: '', FRAME_VIDEO_SIZES, shotVideoExportRef: { current: null },
      rendererRef: { current: { domElement: canvas, getPixelRatio: () => 1, setPixelRatio() {}, setSize() {} } },
      weatherEffectsRef: { current: null }, scriptShotModuleForTest: shotModule,
      globalThis: { VideoEncoder: class {}, VideoFrame: class {} }, createImageBitmap: async () => ({ close() {} }),
      captureReference: vi.fn(async () => ({ blob: {}, ...canvas })), captureViewportAt: vi.fn(),
      videoModuleForTest: { renderScriptShotVideo: async (_plan: unknown, options: any) => {
        expect(options.mode).toBe(mode); // Keep the user-facing provenance, not the renderer's internal pass name.
        await options.renderFrame({ camera: {} }, 0, { timestamp: 0, duration: 1000000 });
        return { blob: {}, manifest: { frameCount: 1, mode } };
      } } });
    const signal = new AbortController().signal;
    const plan = buildScriptShotPlan(await buildScriptSceneDraft(parseScriptScenes('1. 内景 现代卧室 日\n张三：走吧。'), 'scene-1', signal));
    const result = await callback('exportScriptVideo', 'exportSequence', scope)(plan, 1, mode, signal);
    expect(scope.captureReference).toHaveBeenCalledExactlyOnceWith(pass);
    expect(scope.captureViewportAt).not.toHaveBeenCalled(); expect(result.manifest.mode).toBe(mode);
  });
  it('abandons video on session-event edits and still restores the export lock', async () => {
    const scope = scopeFor([]), canvas = { width: 1280, height: 720 };
    Object.assign(scope, { busy: false, archiveProjectId: '', FRAME_VIDEO_SIZES, shotVideoExportRef: { current: null },
      rendererRef: { current: { domElement: canvas, getPixelRatio: () => 1, setPixelRatio() {}, setSize() {} } },
      weatherEffectsRef: { current: null }, scriptShotModuleForTest: shotModule,
      videoModuleForTest: { renderScriptShotVideo: async (_plan: unknown, options: any) => {
        setSessionEvents(scope.sceneRef.current, [{ kind: 'fire', objectId: 'anchor', start: 0, end: 1, offset: [0, 0, 0], rotation: [0, 0, 0], velocity: [0, 0, 0], scale: 1 }]);
        options.assertSource(); throw Error('must not encode a mixed event revision');
      } } });
    const signal = new AbortController().signal;
    const plan = buildScriptShotPlan(await buildScriptSceneDraft(parseScriptScenes('1. 内景 现代卧室 日\n张三：走吧。'), 'scene-1', signal));
    await expect(callback('exportScriptVideo', 'exportSequence', scope)(plan, 24, 'viewport', signal)).rejects.toThrow('会话事件在导出期间变化');
    expect(scope.shotVideoExportRef.current).toBe(null); expect(scope.resizeFrameRef.current).toHaveBeenCalledOnce();
  });
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

  it('preserves opt-in execution tracks through real v6/v7 archive callbacks and keeps legacy shots opt-out', async () => {
    for (const version of [6, 7]) {
      // Reuse the real skinned fixture and production exporter from the enhancement roundtrip above.
      const rig = createToonflowPrimitive({ shape: 'riggedPerson' } as any, '#ff8800');
      const chest = rig.getObjectByName('chest')!;
      rig.animations = [new THREE.AnimationClip('attached', 2, [new THREE.QuaternionKeyframeTrack(`${chest.uuid}.quaternion`, [0, 2], [0, 0, 0, 1, 0, Math.sin(.1), 0, Math.cos(.1)])])];
      const extras = version === 7 ? materializeSetDraft(await prepareSetDraft('ancientStreet', new AbortController().signal)).slice(0, 1) : [];
      extras.forEach(e => e.object.position.set(30, 0, 30));
      const source = scopeFor([{ id: 'actor', name: 'A', object: rig, locked: false }, ...extras]);
      const target = scopeFor([]);
      for (const scope of [source, target]) {
        Object.assign(scope, { GLTFExporter, clonePosedContent, markAnimationArchivePaths, isExportablePlane, applyBonePose });
        scope.exportGeometry = callback('exportGeometry', 'exportGlb', scope);
      }
      const clip = { index: 0, name: rig.animations[0].name, duration: rig.animations[0].duration };
      source.scriptShots = validateScriptShotPlan({ version: 1, sceneId: 's', heading: '', location: '', sourceFingerprint: 's', sceneFingerprint: 's', action: [], diagnostics: [], shots: [{
        id: 's', order: 1, start: 0, end: 4, camera: null, dialogue: [], execution: { version: 1,
          camera: { keys: [{ at: 0, position: [0, 2, 5], target: [0, 1, 0], fov: 45 }, { at: 4, position: [0, 2, 3], target: [0, 1, 0], fov: 45 }] },
          actors: [{ targetId: 'actor', targetName: 'A', beats: [{ start: 0, end: 4, from: { x: 0, z: 0 }, to: { x: 1, z: 0 }, yaw: 0, animation: clip, sourceIndex: 0 }] }],
        },
      }] });
      try {
        const saved = await callback('buildSceneDocument', 'saveScene', source)(); expect(saved.version).toBe(version);
        await callback('importSceneText', 'loadScene', target)(JSON.stringify(saved));
        expect(target.setError.mock.calls.filter((call: any[]) => call[0])).toEqual([]);
        const restored = target.setScriptShots.mock.calls.at(-1)[0];
        expect(restored.shots[0].execution).toEqual(source.scriptShots.shots[0].execution);
        const sampler = createScriptShotSampler(restored, { camera: target.cameraRef.current, controls: target.controlsRef.current, entries: () => target.objectsRef.current, environment: null });
        const restoredActor = target.objectsRef.current.find((e: any) => e.id === 'actor').object;
        const rest = restoredActor.getObjectByName('chest').quaternion.clone();
        sampler.sampleAt(1.5); expect(restoredActor.position.x).toBeCloseTo(.375);
        expect(restoredActor.getObjectByName('chest').quaternion.angleTo(rest)).toBeCloseTo(.15, 4);
        sampler.sampleAt(2); expect(restoredActor.position.x).toBeCloseTo(.5); sampler.stop();
        expect(restoredActor.position.x).toBe(0); expect(restoredActor.getObjectByName('chest').quaternion.angleTo(rest)).toBeLessThan(1e-6);
        delete saved.scriptShots.shots[0].execution;
        saved.scriptShots.shots[0].camera = { position: [0, 2, 5], target: [0, 1, 0], fov: 45 };
        await callback('importSceneText', 'loadScene', target)(JSON.stringify(saved));
        expect(target.setScriptShots.mock.calls.at(-1)[0].shots[0].execution).toBeUndefined();
      } finally { for (const s of [source, target]) s.objectsRef.current.forEach((e: any) => disposeObject(e.object)); }
    }
  }, 60_000);

  it.each([6, 7])('reimports independent event sidecars after production v%s save/load and restores sampled props', async version => {
    const rig = createToonflowPrimitive({ shape: 'riggedPerson' } as any, '#eee');
    const prop = new THREE.Mesh(new THREE.BoxGeometry(.2, .2, .2), new THREE.MeshStandardMaterial()); prop.position.set(2, 1, 0);
    const extras = version === 7 ? materializeSetDraft(await prepareSetDraft('ancientStreet', new AbortController().signal)).slice(0, 1) : []; extras.forEach(e => e.object.position.set(30, 0, 30));
    const source = scopeFor([{ id: 'actor', name: 'A', object: rig, locked: false }, { id: 'prop', name: '道具', object: prop, locked: false }, ...extras]), target = scopeFor([]);
    for (const scope of [source, target]) { Object.assign(scope, { GLTFExporter, clonePosedContent, markAnimationArchivePaths, isExportablePlane, applyBonePose }); scope.exportGeometry = callback('exportGeometry', 'exportGlb', scope); }
    source.scriptShots = validateScriptShotPlan({ version: 1, sceneId: 's', heading: '', location: '', sourceFingerprint: 'src', sceneFingerprint: 'rev', action: [], diagnostics: [], shots: [{ id: 's', order: 1, start: 0, end: 4, camera: { position: [0, 2, 5], target: [0, 1, 0], fov: 45 }, dialogue: [] }] });
    const bone = rig.getObjectByName('chest')!; expect((bone as THREE.Bone).isBone).toBe(true);
    setSessionEvents(source.sceneRef.current, [{ kind: 'attach', objectId: 'prop', targetId: 'actor', bone: bone.name, start: 0, end: 4, offset: [0, .2, 0], rotation: [0, 0, 0], scale: 1, velocity: [0, 0, 0] }]);
    const text = serializeEventSidecar(captureEventSidecarSource(source.scriptShots, source.objectsRef.current), sessionEvents(source.sceneRef.current));
    try {
      const saved = await callback('buildSceneDocument', 'saveScene', source)(); expect(saved.version).toBe(version); expect(JSON.stringify(saved)).not.toContain('previs-event-sidecar');
      await callback('importSceneText', 'loadScene', target)(JSON.stringify(saved)); expect(target.setError.mock.calls.filter((c: any[]) => c[0])).toEqual([]);
      expect(sessionEvents(target.sceneRef.current)).toEqual([]);
      const restoredPlan = target.setScriptShots.mock.calls.at(-1)[0];
      const parsed = parseEventSidecar(text, captureEventSidecarSource(restoredPlan, target.objectsRef.current));
      expect(parsed.events).toEqual(sessionEvents(source.sceneRef.current)); expect(sessionEvents(target.sceneRef.current)).toEqual([]);
      setSessionEvents(target.sceneRef.current, parsed.events);
      const entry = target.objectsRef.current.find((e: any) => e.id === 'prop'), baseline = entry.object.position.clone();
      const runtime = createPrevisEventRuntime(target.objectsRef.current, 4, new Set(['actor']));
      try { runtime.sample(1); expect(entry.object.position.distanceTo(baseline)).toBeGreaterThan(.1); } finally { runtime.stop(); }
      expect(entry.object.position.toArray()).toEqual(baseline.toArray()); expect(entry.object.parent).toBe(target.sceneRef.current);
      entry.object.position.x += .1; expect(() => parseEventSidecar(text, captureEventSidecarSource(restoredPlan, target.objectsRef.current))).toThrow('不一致');
    } finally { for (const scope of [source, target]) scope.objectsRef.current.forEach((e: any) => disposeObject(e.object)); }
  });

  it.each([6, 7])('preserves stationary boundary edits and exact dialogue through production v%s archives', async version => {
    const rig = createToonflowPrimitive({ shape: 'riggedPerson' } as any, '#eee');
    const chest = rig.getObjectByName('chest')!;
    rig.animations = [0, 1].map(i => new THREE.AnimationClip(i ? 'turn' : 'rest', 2, [new THREE.QuaternionKeyframeTrack(`${chest.uuid}.quaternion`, [0, 2], [0, 0, 0, 1, 0, Math.sin(i * .1), 0, Math.cos(i * .1)])]));
    const clips = rig.animations.map((c, index) => ({ index, name: c.name, duration: c.duration }));
    const extras = version === 7 ? materializeSetDraft(await prepareSetDraft('ancientStreet', new AbortController().signal)).slice(0, 1) : []; extras.forEach(e => e.object.position.set(30, 0, 30));
    const source = scopeFor([{ id: 'actor', name: 'A', object: rig, locked: false }, ...extras]), target = scopeFor([]);
    for (const scope of [source, target]) { Object.assign(scope, { GLTFExporter, clonePosedContent, markAnimationArchivePaths, isExportablePlane, applyBonePose }); scope.exportGeometry = callback('exportGeometry', 'exportGlb', scope); }
    const original = validateScriptShotPlan({ version: 1, sceneId: 's', heading: '', location: '', sourceFingerprint: 'source', sceneFingerprint: 'scene', action: [], diagnostics: [], shots: [{ id: 's', order: 1, start: 0, end: 4, camera: { position: [0, 2, 5], target: [0, 1, 0], fov: 45 }, dialogue: [{ index: 0, speaker: '甲', line: '原文不改。', start: .5, end: 1.5 }], execution: { version: 1, actors: [{ targetId: 'actor', targetName: 'A', beats: [0, 1].map(i => ({ start: i * 2, end: i * 2 + 2, from: { x: 0, z: 0 }, to: { x: 0, z: 0 }, yaw: 0, animation: clips[i], sourceIndex: i })) }] } }] });
    source.scriptShots = correctExecutionBeatClip(original, { shotId: 's', targetId: 'actor', beatIndex: 0, animation: clips[0], reason: '明确延后动作切点', boundary: { at: 3 } }, [{ id: 'actor', name: 'A', animations: clips }]);
    try {
      const saved = await callback('buildSceneDocument', 'saveScene', source)(); expect(saved.version).toBe(version);
      await callback('importSceneText', 'loadScene', target)(JSON.stringify(saved)); expect(target.setError.mock.calls.filter((c: any[]) => c[0])).toEqual([]);
      const restored = target.setScriptShots.mock.calls.at(-1)[0]; expect(restored).toEqual(source.scriptShots); expect(restored.shots[0].dialogue).toEqual(original.shots[0].dialogue);
      expect(JSON.parse(restored.diagnostics.at(-1)).operation).toBe('move-stationary-boundary');
      const actor = target.objectsRef.current.find((e: any) => e.id === 'actor').object, bone = actor.getObjectByName('chest'), rest = bone.quaternion.clone();
      const sampler = createScriptShotSampler(restored, { camera: target.cameraRef.current, controls: target.controlsRef.current, entries: () => target.objectsRef.current, environment: null });
      try { sampler.sampleAt(2.5); expect(bone.quaternion.angleTo(rest)).toBeLessThan(1e-6); sampler.sampleAt(3.5); expect(bone.quaternion.angleTo(rest)).toBeCloseTo(.05, 4); } finally { sampler.stop(); }
      expect(bone.quaternion.angleTo(rest)).toBeLessThan(1e-6); expect(actor.position.toArray()).toEqual([0, 0, 0]);
    } finally { for (const scope of [source, target]) scope.objectsRef.current.forEach((e: any) => disposeObject(e.object)); }
  });

  it.each([6, 7].flatMap(version => [false, true].map(split => ({ version, split }))))('preserves manual beat clip correction and human provenance through production v$version archive callbacks split=$split', async ({ version, split }) => {
    const rig = createToonflowPrimitive({ shape: 'riggedPerson' } as any, '#eee');
    const chest = rig.getObjectByName('chest')!;
    rig.animations = [0, 1].map(i => new THREE.AnimationClip(i ? 'human-chest-turn' : 'rest', 2, [new THREE.QuaternionKeyframeTrack(`${chest.uuid}.quaternion`, [0, 2], [0, 0, 0, 1, 0, Math.sin(i * .1), 0, Math.cos(i * .1)])]));
    const clips = rig.animations.map((c, index) => ({ index, name: c.name, duration: c.duration }));
    const extras = version === 7 ? materializeSetDraft(await prepareSetDraft('ancientStreet', new AbortController().signal)).slice(0, 1) : [];
    extras.forEach(e => e.object.position.set(30, 0, 30));
    const source = scopeFor([{ id: 'actor', name: 'A', object: rig, locked: false }, ...extras]), target = scopeFor([]);
    for (const scope of [source, target]) {
      Object.assign(scope, { GLTFExporter, clonePosedContent, markAnimationArchivePaths, isExportablePlane, applyBonePose });
      scope.exportGeometry = callback('exportGeometry', 'exportGlb', scope);
    }
    const original = validateScriptShotPlan({ version: 1, sceneId: 's', heading: '人工动作', location: '平地', sourceFingerprint: 'source-kept', sceneFingerprint: 'review-kept', action: ['原导演表演字段不改写'], diagnostics: ['原始依据'], shots: [{
      id: 's', order: 1, start: 0, end: 4, camera: { position: [0, 2, 5], target: [0, 1, 0], fov: 45 }, dialogue: [], execution: { version: 1, actors: [{ targetId: 'actor', targetName: 'A', beats: [0, 1].map(i => ({ start: i * 2, end: i * 2 + 2, from: { x: 0, z: 0 }, to: { x: 0, z: 0 }, yaw: 0, animation: clips[0], sourceIndex: i })) }] },
    }] });
    const reason = split ? '人工拆成前半静置后半胸部转动，不证明剧情完成' : '人工替换第二段胸部转动，不证明剧情完成';
    source.scriptShots = correctExecutionBeatClip(original, { shotId: 's', targetId: 'actor', beatIndex: 1, animation: clips[split ? 0 : 1], reason,
      ...(split ? { split: { at: 3, afterAnimation: clips[1] } } : {}) }, [{ id: 'actor', name: 'A', visible: true, locked: false, animations: clips }]);
    try {
      const saved = await callback('buildSceneDocument', 'saveScene', source)(); expect(saved.version).toBe(version);
      await callback('importSceneText', 'loadScene', target)(JSON.stringify(saved));
      expect(target.setError.mock.calls.filter((call: any[]) => call[0])).toEqual([]);
      const restored = target.setScriptShots.mock.calls.at(-1)[0]; expect(restored).toEqual(source.scriptShots);
      expect(JSON.parse(restored.diagnostics.at(-1)).reason).toBe(reason);
      if (split) {
        expect(restored.shots[0].execution.actors[0].beats.slice(1).map((b: any) => [b.start, b.end, b.sourceIndex])).toEqual([[2, 3, 1], [3, 4, 1]]);
        expect(JSON.parse(restored.diagnostics.at(-1)).operation).toBe('split-beat');
      }
      const restoredRig = target.objectsRef.current.find((e: any) => e.id === 'actor').object;
      const rest = restoredRig.getObjectByName('chest').quaternion.clone();
      const sampler = createScriptShotSampler(restored, { camera: target.cameraRef.current, controls: target.controlsRef.current, entries: () => target.objectsRef.current, environment: null });
      try {
        sampler.sampleAt(1); expect(restoredRig.getObjectByName('chest').quaternion.angleTo(rest)).toBeLessThan(1e-6);
        if (split) {
          for (const t of [2.5, 3, 3.5, 2.5, 3.5]) {
            sampler.sampleAt(t); expect(restoredRig.getObjectByName('chest').quaternion.angleTo(rest)).toBeCloseTo(t === 3.5 ? .05 : 0, 4);
          }
        } else { sampler.sampleAt(3); expect(restoredRig.getObjectByName('chest').quaternion.angleTo(rest)).toBeCloseTo(.1, 4); }
        sampler.sampleAt(1); expect(restoredRig.getObjectByName('chest').quaternion.angleTo(rest)).toBeLessThan(1e-6);
      } finally { sampler.stop(); }
      expect(restoredRig.getObjectByName('chest').quaternion.angleTo(rest)).toBeLessThan(1e-6);
      expect(restoredRig.position.toArray()).toEqual([0, 0, 0]);
      console.log(`MANUAL_PERFORMANCE_ARCHIVE=PASS version=${version} split=${split} provenance=preserved actualBonePose=changed stop=restored`);
    } finally { for (const scope of [source, target]) scope.objectsRef.current.forEach((e: any) => disposeObject(e.object)); }
  });

  it.each([6, 7])('preserves literal performance proposal and later correction through production v%s archive callbacks', async version => {
    const text = '1. 外景 古街 日\n张三：等。\n（张三走近李四。）\n李四：好。\n（张三抓腕。）';
    const index = parseScriptScenes(text), draft = await buildScriptSceneDraft(index, index.scenes[0].id, new AbortController().signal, 'bedroom');
    const entries = ['张三', '李四'].map((name, i) => {
      const object = createToonflowPrimitive({ shape: 'riggedPerson' } as any, '#eee'); object.position.x = i * 8;
      object.animations = ['cc0Idle', 'cc0Talk', 'cc0Walk'].map(name => object.animations.find(c => c.name === name)!);
      return { id: `actor-${i}`, name, object, locked: false };
    });
    const extras = version === 7 ? materializeSetDraft(await prepareSetDraft('bedroom', new AbortController().signal)).slice(0, 1) : [];
    extras.forEach(e => e.object.position.set(30, 0, 30));
    const source = scopeFor([...entries, ...extras]), target = scopeFor([]), context = captureScriptPerformanceContext(entries);
    const cast = context.targets.map(t => ({ character: t.name, targetId: t.id, idle: t.animations[0], talk: t.animations[1], walk: t.animations[2] }));
    const proposed = buildScriptPerformancePlan(buildScriptShotPlan(draft), index.scenes[0], text, cast, context, { speed: 1.35, approachDistance: 4, departureDistance: 4 });
    proposed.shots.forEach(s => { s.camera = { position: [0, 4, 16], target: [4, 1, 0], fov: 45 }; });
    source.scriptShots = correctExecutionBeatClip(proposed, { shotId: 'performance-1', targetId: 'actor-0', beatIndex: 0, animation: cast[0].idle, reason: '人工改为静候，不冒充抓腕' }, context.targets);
    for (const scope of [source, target]) {
      Object.assign(scope, { GLTFExporter, clonePosedContent, markAnimationArchivePaths, isExportablePlane, applyBonePose });
      scope.exportGeometry = callback('exportGeometry', 'exportGlb', scope);
    }
    try {
      const saved = await callback('buildSceneDocument', 'saveScene', source)(); expect(saved.version).toBe(version);
      await callback('importSceneText', 'loadScene', target)(JSON.stringify(saved));
      expect(target.setError.mock.calls.filter((call: any[]) => call[0])).toEqual([]);
      const restored = target.setScriptShots.mock.calls.at(-1)[0]; expect(restored).toEqual(source.scriptShots);
      const record = JSON.parse(restored.diagnostics.find((t: string) => t.startsWith('{"kind":"script-performance-proposal"')));
      expect(record.unresolved[0]).toMatchObject({ line: 5, quote: '（张三抓腕。）' });
      expect(JSON.parse(restored.diagnostics.at(-1)).reason).toBe('人工改为静候，不冒充抓腕');
      const rig = target.objectsRef.current.find((e: any) => e.id === 'actor-0').object, baseline = rig.position.clone();
      assertScriptShotApplicationSafe(restored, target.objectsRef.current, null);
      const sampler = createScriptShotSampler(restored, { camera: target.cameraRef.current, controls: target.controlsRef.current, entries: () => target.objectsRef.current, environment: null });
      try {
        const walk = restored.shots[1]; sampler.sampleAt(walk.start + (walk.end - walk.start) / 2); expect(rig.position.x).toBeCloseTo(2);
        const arm = rig.getObjectByName('leftUpperArm').quaternion.clone(); sampler.sampleAt(walk.start + .1);
        expect(rig.getObjectByName('leftUpperArm').quaternion.angleTo(arm)).toBeGreaterThan(.001);
      } finally { sampler.stop(); }
      expect(rig.position.equals(baseline)).toBe(true);
      console.log(`SCRIPT_PERFORMANCE_ARCHIVE=PASS version=${version} sourceGaps=preserved correction=preserved actualBonePose=changed stop=restored`);
    } finally { for (const scope of [source, target]) scope.objectsRef.current.forEach((e: any) => disposeObject(e.object)); }
  }, 30_000);

  it.each([6, 7])('preserves actual paired wrist constraints through production v%s archives and replays restored skin', async version => {
    // Float32 key times must not round past clip.duration; .25 is exact in both
    // archive and key buffers (.2 rounds upward and is correctly rejected).
    const duration = .25;
    const rigs = ['#eee', '#ccc'].map(color => createToonflowPrimitive({ shape: 'riggedPerson' } as any, color));
    rigs[1]!.position.x = .85;
    const q = new THREE.Quaternion().setFromAxisAngle(new THREE.Vector3(0, 0, 1), -.2).toArray();
    rigs.forEach((rig, i) => { const bone = rig.getObjectByName(i ? 'rightUpperArm' : 'chest')!; rig.animations = [new THREE.AnimationClip(i ? 'offer' : 'hold', duration, [new THREE.QuaternionKeyframeTrack(`${bone.uuid}.quaternion`, [0, duration], i ? [...q, ...q] : [0, 0, 0, 1, 0, 0, 0, 1])])]; });
    const extras = version === 7 ? materializeSetDraft(await prepareSetDraft('ancientStreet', new AbortController().signal)).slice(0, 1) : [];
    extras.forEach(e => e.object.position.set(30, 0, 30));
    const source = scopeFor([{ id: 'a', name: 'A', object: rigs[0], locked: false }, { id: 'b', name: 'B', object: rigs[1], locked: false }, ...extras]);
    const target = scopeFor([]);
    for (const scope of [source, target]) {
      Object.assign(scope, { GLTFExporter, clonePosedContent, markAnimationArchivePaths, isExportablePlane, applyBonePose });
      scope.exportGeometry = callback('exportGeometry', 'exportGlb', scope);
    }
    source.scriptShots = validateScriptShotPlan({ version: 1, sceneId: 's', heading: '', location: '', sourceFingerprint: 's', sceneFingerprint: 's', action: [], diagnostics: [], shots: [{
      id: 's', order: 1, start: 0, end: duration, camera: { position: [0, 2, 5], target: [.4, 1, 0], fov: 45 }, dialogue: [], execution: { version: 1,
        actors: rigs.map((rig, i) => ({ targetId: i ? 'b' : 'a', targetName: i ? 'B' : 'A', beats: [{ start: 0, end: duration, from: { x: i * .85, z: 0 }, to: { x: i * .86, z: 0 }, yaw: 0, animation: { index: 0, name: rig.animations[0].name, duration } }] })),
        contacts: [{ version: 1, policy: 'whole-skin-separating-plane-v1', sourceId: 'a', targetId: 'b', start: 0, end: duration, normal: [-1, 0, 0], binding: { version: 1, arm: { shoulder: 'leftUpperArm', elbow: 'leftForeArm', wrist: 'leftHand' }, targetBone: 'rightHand', sourceAnchor: [.066, -.02, .0047], targetAnchor: [-.066, -.02, .0047], rotation: [0, 0, 0, 1], pole: [0, 0, 1] } }],
      },
    }] });
    try {
      const saved = await callback('buildSceneDocument', 'saveScene', source)(); expect(saved.version).toBe(version);
      await callback('importSceneText', 'loadScene', target)(JSON.stringify(saved));
      expect(target.setError.mock.calls.filter((call: any[]) => call[0])).toEqual([]);
      const restored = target.setScriptShots.mock.calls.at(-1)[0];
      expect(restored.shots[0].execution).toEqual(source.scriptShots.shots[0].execution);
      const a = target.objectsRef.current.find((e: any) => e.id === 'a').object, b = target.objectsRef.current.find((e: any) => e.id === 'b').object;
      const before = [serializeBonePose(a), serializeBonePose(b)];
      const sampler = createScriptShotSampler(restored, { camera: target.cameraRef.current, controls: target.controlsRef.current, entries: () => target.objectsRef.current, environment: null });
      try {
        expect(sampler.walkProof.contact!.maxErrorMetres).toBeLessThan(1e-5);
        for (const time of [duration, 0, .1, .03, .1]) {
          sampler.sampleAt(time);
          const hand = a.getObjectByName('leftHand').localToWorld(new THREE.Vector3(.066, -.02, .0047));
          const wrist = b.getObjectByName('rightHand').localToWorld(new THREE.Vector3(-.066, -.02, .0047));
          expect(hand.distanceTo(wrist)).toBeLessThan(1e-5);
        }
      } finally { sampler.stop(); }
      expect([serializeBonePose(a), serializeBonePose(b)]).toEqual(before);
      expect(a.position.x).toBe(0); expect(b.position.x).toBe(.85);
    } finally { for (const s of [source, target]) s.objectsRef.current.forEach((e: any) => disposeObject(e.object)); }
  }, 60_000);

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
