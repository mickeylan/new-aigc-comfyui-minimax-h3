// 移植自 ArtCraft（MIT / Apache-2.0 双许可）；来源与依赖声明见 ../../../THIRD_PARTY_NOTICES.md。
// 骨骼摆姿（FK）的纯逻辑层：筛出可摆的骨、按骨树序列化/还原姿势。
// 这一层不碰 WebGL、不碰 DOM，便于单元测试；交互部分在 FKHelper.ts。
import * as THREE from "three";

// 手指、面部与末端骨不参与整体摆姿：球太多会挡住画面，且这些骨对构图影响极小。
export const FK_BONE_BLACKLIST = [
  "def",
  "toe_end",
  "thumb",
  "index",
  "handmiddle",
  "handring",
  "pinky",
  "headtop",
  "-ear",
  "teeth",
  "eye",
  "lid",
  "jaw",
  "tongue",
  "lip",
  "chin",
  "brow",
  "cheek",
  "forehead",
  "nose",
  "temple",
] as const;

export function isBlacklistedBone(name: string) {
  const lower = name.toLowerCase();
  return FK_BONE_BLACKLIST.some((item) => lower.includes(item));
}

export function isPoseableBone(object: THREE.Object3D): object is THREE.Bone {
  if (!(object as THREE.Bone).isBone) return false;
  if (!object.name) return false;
  return !isBlacklistedBone(object.name);
}

/** 收集一棵模型下所有可摆的骨（用于决定是否显示「骨骼摆姿」入口）。 */
export function collectPoseableBones(root: THREE.Object3D): THREE.Bone[] {
  const bones: THREE.Bone[] = [];
  root.traverse((child) => {
    if (isPoseableBone(child)) bones.push(child);
  });
  return bones;
}

export function hasSkeleton(root: THREE.Object3D) {
  return collectPoseableBones(root).length > 0;
}

/**
 * 找骨树的根：优先 Mixamo 的 hips，其次任意一个没有 Bone 父节点的骨。
 * 找不到时返回 null（调用方据此禁用摆姿并给出提示）。
 */
export function findSkeletonRoot(root: THREE.Object3D): THREE.Bone | null {
  let hip: THREE.Bone | null = null;
  let anyRoot: THREE.Bone | null = null;
  root.traverse((child) => {
    if (!(child as THREE.Bone).isBone) return;
    const bone = child as THREE.Bone;
    if (!hip && bone.name.toLowerCase().includes("mixamorighips")) hip = bone;
    if (!anyRoot && !(bone.parent as THREE.Bone | null)?.isBone) anyRoot = bone;
  });
  return hip ?? anyRoot;
}

export interface BonePoseNode {
  name: string;
  position: [number, number, number];
  quaternion: [number, number, number, number];
  scale: [number, number, number];
  children: BonePoseNode[];
}

function snapshotBone(bone: THREE.Bone): BonePoseNode {
  return {
    name: bone.name,
    position: [bone.position.x, bone.position.y, bone.position.z],
    quaternion: [bone.quaternion.x, bone.quaternion.y, bone.quaternion.z, bone.quaternion.w],
    scale: [bone.scale.x, bone.scale.y, bone.scale.z],
    children: bone.children.filter((child) => (child as THREE.Bone).isBone).map((child) => snapshotBone(child as THREE.Bone)),
  };
}

/** 把整棵骨树的局部变换存成纯 JSON（够小，可直接塞进场景存档）。 */
export function serializeBonePose(root: THREE.Object3D): BonePoseNode | null {
  const bone = findSkeletonRoot(root);
  if (!bone) return null;
  return snapshotBone(bone);
}

function restoreBone(bone: THREE.Bone, node: BonePoseNode) {
  bone.position.set(node.position[0], node.position[1], node.position[2]);
  bone.quaternion.set(node.quaternion[0], node.quaternion[1], node.quaternion[2], node.quaternion[3]);
  bone.scale.set(node.scale[0], node.scale[1], node.scale[2]);

  // 先按名字配对子骨；名字对不上（导出的骨名带前缀/后缀）时退回按顺序配。
  const boneChildren = bone.children.filter((child) => (child as THREE.Bone).isBone) as THREE.Bone[];
  const used = new Set<string>();
  for (const childNode of node.children) {
    const matched =
      boneChildren.find((candidate) => candidate.name === childNode.name && !used.has(candidate.name)) ??
      boneChildren.find((candidate) => !used.has(candidate.name));
    if (!matched) break;
    used.add(matched.name);
    restoreBone(matched, childNode);
  }
}

/** 把存档里的骨树姿势套回模型；成功返回 true。 */
export function applyBonePose(root: THREE.Object3D, pose: BonePoseNode | null | undefined) {
  if (!pose) return false;
  const bone = findSkeletonRoot(root);
  if (!bone) return false;
  restoreBone(bone, pose);
  bone.updateMatrixWorld(true);
  return true;
}