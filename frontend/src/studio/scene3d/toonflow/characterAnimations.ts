// Derived from Toonflow-app; Copyright (c) 2026 HBAI-Ltd; MIT. See ../../THIRD_PARTY_NOTICES.md.
import { AnimationClip, Bone, NormalAnimationBlendMode, PropertyBinding, type Object3D } from "three";
import data from "./characterAnimationData.json";
import { setAnimationCatalogSource } from "../animationPreview";

/**
 * 目录数据自身的修订号：动作数据整体升级时它会变。
 * 只描述仓库里这份数据的版本，不代表任何场景已使用或已验收。
 */
export const characterAnimationCatalogRevision: number = data.revision;

const mirroredAnimations = [
  { sourceId: "cc0PickUp", id: "cc0PickUpRight", name: "右手台面拾取" },
  { sourceId: "cc0Interact", id: "cc0InteractRight", name: "右手伸手交互" },
];
const sourceCatalog = data.animations.map(entry => ({
  id: entry.id,
  name: entry.name,
  category: entry.category,
  duration: entry.clip.duration,
  sourceName: entry.sourceName,
  sourceUrl: entry.sourceUrl,
  license: entry.license,
  author: entry.author,
  contactHints: entry.contactHints,
}));
function mirrorBounds(bounds: { min: number[]; max: number[] }) {
  return { min: [-bounds.max[0]!, bounds.min[1]!, bounds.min[2]!], max: [-bounds.min[0]!, bounds.max[1]!, bounds.max[2]!] };
}
export const characterAnimationCatalog = Object.freeze([
  ...sourceCatalog,
  ...mirroredAnimations.map(mirror => {
    const source = sourceCatalog.find(entry => entry.id === mirror.sourceId)!;
    return { ...source, id: mirror.id, name: mirror.name, sourceName: source.sourceName + " (mirrored)", contactHints: { ...source.contactHints, leftHand: mirrorBounds(source.contactHints.rightHand), rightHand: mirrorBounds(source.contactHints.leftHand) } };
  }),
].map(entry => Object.freeze(entry)));

export function getCharacterAnimation(id: string) { return characterAnimationCatalog.find(entry => entry.id === id); }

const mirroredIds = new Set(mirroredAnimations.map(entry => entry.id));

export type CharacterAnimationCatalogEntry = {
  id: string;
  name: string;
  category: string;
  duration: number;
  /** 镜像派生项：ID 与源项不同，但不是另一份独立模型。 */
  mirrored: boolean;
  sourceName: string;
  license: string;
  author: string;
};

/** 目录逐项（稳定 ID、分类、真实时长、许可）；顺序与数据文件一致，不排序也不补齐。 */
export function listCharacterAnimationCatalog(): readonly CharacterAnimationCatalogEntry[] {
  return characterAnimationCatalog.map(entry => ({
    id: entry.id,
    name: entry.name,
    category: entry.category,
    duration: entry.duration,
    mirrored: mirroredIds.has(entry.id),
    sourceName: entry.sourceName,
    license: entry.license,
    author: entry.author,
  }));
}

export type CharacterAnimationCatalogSummary = {
  revision: number;
  /** 条目总数 = 源 + 镜像（镜像不是独立模型，数量分开报）。 */
  total: number;
  source: number;
  mirrored: number;
  categories: readonly { category: string; count: number }[];
};

/** 真实计数：总数、源项、镜像派生、逐分类；分类按目录首次出现顺序。 */
export function summarizeCharacterAnimationCatalog(): CharacterAnimationCatalogSummary {
  const counts = new Map<string, number>();
  for (const entry of characterAnimationCatalog) counts.set(entry.category, (counts.get(entry.category) ?? 0) + 1);
  return {
    revision: characterAnimationCatalogRevision,
    total: characterAnimationCatalog.length,
    source: sourceCatalog.length,
    mirrored: mirroredIds.size,
    categories: [...counts].map(([category, count]) => ({ category, count })),
  };
}

/** 目录里的独立片段副本：挂到角色上时不会改到共享目录里的那一份。 */
export function createCharacterAnimationClip(id: string): AnimationClip {
  if (!getCharacterAnimation(id)) throw new Error(`动作目录没有这个片段：${id}`);
  const source = createCharacterAnimationClips().find(clip => clip.name === id);
  if (!source) throw new Error(`动作目录片段无法构建：${id}`);
  return setAnimationCatalogSource(source.clone(), { catalog: 'toonflow-quaternius', id, revision: characterAnimationCatalogRevision, license: 'CC0-1.0' });
}

const boneNameCache = new Map<string, readonly string[]>();

/**
 * 该片段需要的骨骼节点名（按轨道首次出现顺序）。
 * 只接受骨骼局部 TRS：带 objectName、属性索引或材质/相机轨道的片段直接判为不合约定，不做猜测性过滤。
 */
export function characterAnimationBoneNames(id: string): readonly string[] {
  const cached = boneNameCache.get(id);
  if (cached) return cached;
  const names: string[] = [];
  for (const track of createCharacterAnimationClip(id).tracks) {
    const binding = PropertyBinding.parseTrackName(track.name);
    if (!binding.nodeName || binding.objectName || binding.propertyIndex !== undefined ||
      !["position", "quaternion", "scale"].includes(binding.propertyName)) {
      throw new Error(`动作目录片段「${id}」含非骨骼局部 TRS 轨道：${track.name}`);
    }
    if (!names.includes(binding.nodeName)) names.push(binding.nodeName);
  }
  if (!names.length) throw new Error(`动作目录片段「${id}」没有骨骼轨道`);
  const frozen = Object.freeze(names);
  boneNameCache.set(id, frozen);
  return frozen;
}

export type CharacterAnimationBinding = {
  id: string;
  bones: readonly string[];
  missing: readonly string[];
  bindable: boolean;
};

/** 只按同名骨骼核对可绑定：不做跨骨架重定向，缺一根即判不可绑定。 */
export function characterAnimationBinding(root: Object3D, id: string): CharacterAnimationBinding {
  const bones = characterAnimationBoneNames(id);
  const missing = bones.filter(name => !(PropertyBinding.findNode(root, name) instanceof Bone));
  return { id, bones, missing, bindable: missing.length === 0 };
}

/**
 * 可绑定才给出片段副本；否则明确报出缺哪些骨骼，不降级、不改名、不换成别的片段。
 * 调用方仍须在加入前做整份存档校验（见 animationPreview.attachAnimationClip）。
 */
export function createCharacterAnimationClipFor(root: Object3D, id: string): AnimationClip {
  const binding = characterAnimationBinding(root, id);
  if (!binding.bindable) {
    throw new Error(`动作目录片段「${binding.id}」需要 ${binding.bones.length} 根同名人形骨骼，当前角色缺少：${binding.missing.join("、")}；不做骨架重定向，也不替换成其他动作`);
  }
  return createCharacterAnimationClip(id);
}

let clips: AnimationClip[] | undefined;
export function createCharacterAnimationClips(): readonly AnimationClip[] {
  if (!clips) {
    clips = data.animations.map(entry => AnimationClip.parse({ ...entry.clip, blendMode: NormalAnimationBlendMode }));
    for (const mirror of mirroredAnimations) {
      const clip = clips.find(clip => clip.name === mirror.sourceId)!.clone();
      clip.name = mirror.id;
      for (const track of clip.tracks) {
        track.name = track.name.replace(/^(left|right)/, side => side === "left" ? "right" : "left");
        const stride = track.getValueSize();
        for (let index = 0; index < track.values.length; index += stride) {
          if (track.name.endsWith(".quaternion")) { track.values[index + 1] *= -1; track.values[index + 2] *= -1; }
          else if (track.name.endsWith(".position")) track.values[index] *= -1;
        }
      }
      clips.push(clip);
    }
  }
  return clips;
}
