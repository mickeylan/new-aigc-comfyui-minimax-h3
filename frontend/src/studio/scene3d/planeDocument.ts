// ArtCraft chroma-key UV-distance approach; MIT/Apache-2.0, see THIRD_PARTY_NOTICES.md.
import * as THREE from "three";
import { validateBonePose } from "./posing/bonePose";
import { SCENE_ARCHIVE_MAX_BYTES } from "./scene-archive";
import { DEFAULT_PLANE_LAYER, clampOpacity, type PlaneLayerSettings } from "./compositing";
export interface PlaneSettings extends PlaneLayerSettings { chromaKey: boolean; keyColor: string; similarity: number; smoothness: number; spill: number }
export const DEFAULT_PLANE_SETTINGS: PlaneSettings = { ...DEFAULT_PLANE_LAYER, chromaKey: false, keyColor: "#00ff00", similarity: 0.159, smoothness: 0.082, spill: 0.1 };
export function normalizePlaneSettings(value: Partial<PlaneSettings> | null = {}): PlaneSettings {
  value = value ?? {};
  const unit = (v: unknown, fallback: number) => typeof v === "number" && Number.isFinite(v) ? Math.max(0, Math.min(1, v)) : fallback;
  return { opacity: clampOpacity(value.opacity ?? 1), doubleSided: value.doubleSided !== false, alphaTest: unit(value.alphaTest, 0.02),
    chromaKey: value.chromaKey === true, keyColor: /^#[0-9a-f]{6}$/i.test(value.keyColor ?? "") ? value.keyColor! : "#00ff00",
    similarity: unit(value.similarity, 0.159), smoothness: Math.max(0.001, unit(value.smoothness, 0.082)), spill: Math.max(0.001, unit(value.spill, 0.1)) };
}
export function planeMaterial(texture: THREE.Texture, input: Partial<PlaneSettings>) {
  const settings = normalizePlaneSettings(input);
  const material = new THREE.MeshBasicMaterial({ map: texture, side: settings.doubleSided ? THREE.DoubleSide : THREE.FrontSide,
    transparent: true, depthWrite: false, opacity: settings.opacity, alphaTest: settings.alphaTest });
  if (settings.chromaKey) {
    material.onBeforeCompile = (shader) => {
      shader.uniforms.planeKeyColor = { value: new THREE.Color(settings.keyColor) };
      shader.uniforms.planeSimilarity = { value: settings.similarity };
      shader.uniforms.planeSmoothness = { value: settings.smoothness };
      shader.uniforms.planeSpill = { value: settings.spill };
      shader.fragmentShader = `uniform vec3 planeKeyColor; uniform float planeSimilarity; uniform float planeSmoothness; uniform float planeSpill;
vec2 planeUV(vec3 rgb) { return vec2(dot(rgb, vec3(-0.169,-0.331,0.5))+0.5, dot(rgb,vec3(0.5,-0.419,-0.081))+0.5); }
` + shader.fragmentShader.replace("#include <map_fragment>", `#include <map_fragment>
float planeMask = distance(planeUV(diffuseColor.rgb), planeUV(planeKeyColor)) - planeSimilarity;
diffuseColor.a *= pow(clamp(planeMask / planeSmoothness, 0.0, 1.0), 1.5);
float planeDesat = dot(diffuseColor.rgb, vec3(0.2126,0.7152,0.0722));
diffuseColor.rgb = mix(vec3(planeDesat), diffuseColor.rgb, pow(clamp(planeMask / planeSpill,0.0,1.0),1.5));`);
    };
    material.customProgramCacheKey = () => JSON.stringify(settings);
  }
  return material;
}
export const SCENE_MAX_BYTES = SCENE_ARCHIVE_MAX_BYTES;
export async function mediaDataURL(url: string): Promise<string> {
  const response = await fetch(url);
  if (!response.ok) throw new Error(`媒体读取失败 HTTP ${response.status}`);
  const blob = await response.blob();
  if (blob.size > SCENE_MAX_BYTES * 0.75) throw new Error("媒体过大，无法纳入32MiB场景存档");
  return new Promise((resolve, reject) => { const reader = new FileReader(); reader.onload = () => resolve(String(reader.result)); reader.onerror = () => reject(new Error("媒体读取失败")); reader.readAsDataURL(blob); });
}
export function validateSceneSize(text: string) {
  if (new Blob([text]).size > SCENE_MAX_BYTES) throw new Error("场景超过32MiB，无法完整保存；请减少媒体大小");
}

/** Validate before loading/clearing resources. No network sources in portable composition layers. */
export function validateCompositionDocument(doc: any) {
  const vector = (value: unknown, length: number) => Array.isArray(value) && value.length === length && value.every((n) => typeof n === "number" && Number.isFinite(n));
  const transform = (item: any) => item && vector(item.position, 3) && vector(item.quaternion, 4) && vector(item.scale, 3);
  if (!doc || ![1, 2, 3, 4, 5, 6].includes(doc.version) || typeof doc.model?.data !== "string" || !doc.model.data ||
    !vector(doc.camera?.position, 3) || !vector(doc.camera?.target, 3)) throw new Error("不是可识别的场景文件");
  if (doc.frame && !['16:9', '9:16', '1:1', '3:2', '2:3'].includes(doc.frame.aspect)) throw new Error('场景画幅无效');
  const ids = new Set<string>();
  for (const key of ["objects", "planes", "splats", "cameraKeys"]) {
    if (doc[key] !== undefined && !Array.isArray(doc[key])) throw new Error("场景图层列表无效");
  }
  for (const item of [...(doc.objects ?? []), ...(doc.splats ?? []), ...(doc.planes ?? [])]) {
    if (!transform(item)) throw new Error("场景对象变换无效");
    if (doc.version >= 5) {
      if (typeof item.id !== 'string' || !item.id || ids.has(item.id)) throw new Error('对象ID缺失或重复');
      ids.add(item.id);
      if (typeof item.visible !== 'boolean' || typeof item.locked !== 'boolean') throw new Error('对象显隐/锁定无效');
    }
  }
  for (const splat of doc.splats ?? []) {
    if (splat.contentTransform !== undefined && !transform(splat.contentTransform)) throw new Error('泼溅内容变换无效');
  }
  for (const object of doc.objects ?? []) {
    validateBonePose(object.bonePose);
    validateBonePose(object.initialBonePose);
  }
  for (const plane of doc.planes ?? []) {
    if (doc.version < 5) {
      if (!plane.id || ids.has(plane.id)) throw new Error("平面ID缺失或重复");
      ids.add(plane.id);
    }
    if (!["image", "video"].includes(plane.kind) || typeof plane.source !== "string" || !plane.source.startsWith(`data:${plane.kind}/`)) throw new Error("平面媒体来源无效");
  }
  if (doc.skybox && (typeof doc.skybox.source !== "string" || !doc.skybox.source.startsWith("data:image/") || !Number.isFinite(doc.skybox.intensity) || doc.skybox.intensity < 0 || doc.skybox.intensity > 2)) throw new Error("天空盒无效");
}
