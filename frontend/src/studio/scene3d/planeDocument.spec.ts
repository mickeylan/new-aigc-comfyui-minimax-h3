import { afterEach, describe, expect, it, vi } from "vitest";
import * as THREE from "three";
import { DEFAULT_PLANE_SETTINGS, normalizePlaneSettings, planeMaterial, mediaDataURL, validateSceneSize, validateCompositionDocument, SCENE_MAX_BYTES } from "./planeDocument";
afterEach(() => vi.unstubAllGlobals());
const transform = { position: [0, 1, -3], quaternion: [0, 0, 0, 1], scale: [1, 1, 1] };
const plane = (id: string, kind = "image") => ({ id, name: "same-name", kind, source: `data:${kind}/png;base64,YQ==`, settings: { ...DEFAULT_PLANE_SETTINGS }, ...transform });
const doc = (version = 4) => ({ version, model: { data: "Z2xi" }, camera: { position: [1, 2, 3], target: [0, 0, 0] }, objects: [], planes: [plane("a"), plane("b", "video")], skybox: { source: "data:image/png;base64,YQ==", intensity: 0.7, asEnvironment: true } });

describe("3D composition layers", () => {
  it("accepts v6 while retaining mandatory stable identities and rejecting v7", () => {
    const current = { ...doc(6), planes: [], objects: [{ id: 'actor', visible: true, locked: false, ...transform }] };
    expect(() => validateCompositionDocument(current)).not.toThrow();
    expect(() => validateCompositionDocument({ ...current, version: 7 })).toThrow();
    expect(() => validateCompositionDocument({ ...current, objects: [{ ...current.objects[0], id: '' }] })).toThrow();
    expect(() => validateCompositionDocument({ ...current, objects: [current.objects[0], current.objects[0]] })).toThrow();
    expect(() => validateCompositionDocument({ ...current, objects: [{ ...current.objects[0], visible: undefined }] })).toThrow();
  });
  it("defaults legacy fields, clamps invalid values and never mutates another layer", () => {
    const a = normalizePlaneSettings({ opacity: 0.2, chromaKey: true });
    const b = normalizePlaneSettings();
    a.opacity = 0.1;
    expect(b.opacity).toBe(1);
    expect(DEFAULT_PLANE_SETTINGS.opacity).toBe(1);
    expect(normalizePlaneSettings({ opacity: NaN, smoothness: 0, spill: 0, keyColor: "bad" })).toMatchObject({ opacity: 1, smoothness: 0.001, spill: 0.001, keyColor: "#00ff00" });
    expect(normalizePlaneSettings(null)).toEqual(DEFAULT_PLANE_SETTINGS);
  });
  it("retains soft alpha even at full layer opacity and honors sidedness", () => {
    const texture = new THREE.Texture();
    const material = planeMaterial(texture, { opacity: 1, doubleSided: false });
    expect(material.map).toBe(texture);
    expect(material.transparent).toBe(true);
    expect(material.depthWrite).toBe(false);
    expect(material.side).toBe(THREE.FrontSide);
    expect(material.opacity).toBe(1);
    material.dispose();
  });
  it("uses independent key uniforms and applies key alpha before alpha-test", () => {
    const shader = { uniforms: {}, fragmentShader: "#include <map_fragment>\n#include <alphatest_fragment>" } as any;
    const material = planeMaterial(new THREE.Texture(), { chromaKey: true, similarity: 0.24, keyColor: "#0000ff" });
    material.onBeforeCompile(shader, {} as any);
    expect(shader.uniforms.planeSimilarity.value).toBe(0.24);
    expect(shader.fragmentShader.indexOf("diffuseColor.a *=")).toBeLessThan(shader.fragmentShader.indexOf("#include <alphatest_fragment>"));
    expect(shader.fragmentShader).not.toContain("/ max(opacity");
    expect(material.customProgramCacheKey()).not.toBe(planeMaterial(new THREE.Texture(), { chromaKey: true, similarity: 0.3 }).customProgramCacheKey());
    material.dispose();
  });
  it("roundtrips independent settings, media bytes and duplicate names using stable IDs", () => {
    const original = doc();
    original.planes[0].settings.opacity = 0.2;
    original.planes[1].settings.chromaKey = true;
    const copy = JSON.parse(JSON.stringify(original));
    validateCompositionDocument(copy);
    expect(copy).toEqual(original);
    expect(copy.planes[0].id).not.toBe(copy.planes[1].id);
    expect(copy.planes[0].settings.opacity).toBe(0.2);
    expect(copy.planes[1].settings.opacity).toBe(1);
  });
  it.each([1, 2, 3])("accepts legacy v%s without composition fields", (version) => {
    const value = { version, model: { data: "Z2xi" }, camera: doc().camera, objects: [] };
    expect(() => validateCompositionDocument(value)).not.toThrow();
  });
  it("rejects transient URLs, duplicate IDs, invalid transforms and future versions before replacement", () => {
    const value = doc();
    value.planes[0].source = "blob:temporary";
    expect(() => validateCompositionDocument(value)).toThrow("媒体来源");
    const duplicate = doc(); duplicate.planes[1].id = "a";
    expect(() => validateCompositionDocument(duplicate)).toThrow("重复");
    const invalid = doc(); invalid.planes[0].position = [NaN, 0, 0];
    expect(() => validateCompositionDocument(invalid)).toThrow("变换");
    expect(() => validateCompositionDocument(doc(6))).toThrow();
  });
  it('validates v5 hidden/locked state and globally unique stable identities', () => {
    const value: any = doc(5);
    value.planes.forEach((p: any) => { p.visible = false; p.locked = true; });
    value.objects = [{ id: 'mesh', visible: false, locked: true, ...transform }];
    validateCompositionDocument(value);
    const restored = JSON.parse(JSON.stringify(value)); validateCompositionDocument(restored);
    expect(restored.objects[0]).toMatchObject({ id: 'mesh', visible: false, locked: true });
    restored.objects[0].id = 'a'; expect(() => validateCompositionDocument(restored)).toThrow('重复');
    restored.objects[0].id = 'mesh'; restored.objects[0].locked = 'false'; expect(() => validateCompositionDocument(restored)).toThrow('锁定');
  });
  it('preserves optional splat normalization TRS and rejects malformed content before decoding', () => {
    const value: any = doc(5); value.planes = [];
    value.splats = [{ id: 'splat', visible: true, locked: false, ...transform, contentTransform: { position: [.2, .4, .1], quaternion: [0, 0, 1, 0], scale: [2, 2, 2] } }];
    const restored = JSON.parse(JSON.stringify(value)); validateCompositionDocument(restored);
    expect(restored.splats[0].contentTransform).toEqual(value.splats[0].contentTransform);
    for (const invalid of [null, {}, { ...transform, scale: [1, NaN, 1] }, { ...transform, quaternion: [0, 1] }]) {
      restored.splats[0].contentTransform = invalid;
      expect(() => validateCompositionDocument(restored)).toThrow('泼溅内容变换');
    }
    delete restored.splats[0].contentTransform; expect(() => validateCompositionDocument(restored)).not.toThrow();
  });
  it("checks actual UTF8 bytes rather than character count", () => {
    expect(() => validateSceneSize("x")).not.toThrow();
    expect(() => validateSceneSize("界".repeat(Math.floor(SCENE_MAX_BYTES / 3) + 1))).toThrow("32MiB");
  });
  it("embeds media and rejects HTTP failures without writing a temporary URL", async () => {
    vi.stubGlobal("fetch", vi.fn().mockResolvedValue({ ok: true, blob: async () => new Blob(["hello"], { type: "image/png" }) }));
    expect(await mediaDataURL("blob:temporary")).toBe("data:image/png;base64,aGVsbG8=");
    vi.stubGlobal("fetch", vi.fn().mockResolvedValue({ ok: false, status: 404 }));
    await expect(mediaDataURL("/missing")).rejects.toThrow("HTTP 404");
  });
});
