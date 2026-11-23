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
  let selected = hip ?? anyRoot;
  while (selected && (selected.parent as THREE.Bone | null)?.isBone) selected = selected.parent as THREE.Bone;
  return selected;
}

export interface BonePoseNode {
  name: string;
  position: [number, number, number];
  quaternion: [number, number, number, number];
  scale: [number, number, number];
  children: BonePoseNode[];
  /** Other independent skeleton roots; absent in legacy documents. */
  additionalRoots?: BonePoseNode[];
}

/** 红球几何体自带半径（FKHelper 里 SphereGeometry 的入参，两处必须一致）。 */
export const FK_SPHERE_GEOMETRY_RADIUS = 0.015;

/**
 * 球的屏幕尺寸占比：相机距离 × 这个系数 = 期望的世界半径。
 * 固定世界半径会让远处/近处的球一个点不到、一个大得挡住模型
 * （默认机位下 0.015 半径只有 4 px，鼠标根本瞄不中）。
 */
export const FK_SPHERE_SCREEN_RATIO = 0.012;

export const FK_SPHERE_MIN_RADIUS = 0.01;

/** 让红球在屏幕上大小恒定：半径跟着相机距离线性放大。 */
export function boneSphereWorldRadius(cameraDistance: number) {
  if (!Number.isFinite(cameraDistance) || cameraDistance <= 0) return FK_SPHERE_MIN_RADIUS;
  return Math.max(FK_SPHERE_MIN_RADIUS, cameraDistance * FK_SPHERE_SCREEN_RATIO);
}

/**
 * 把「期望的世界半径」折算成球在骨局部空间里的缩放。
 * Mixamo 的骨世界缩放常在 0.01 级，不反向抵消的话球会小到看不见。
 */
export function boneSphereLocalScale(
  worldRadius: number,
  boneWorldScale: [number, number, number],
): [number, number, number] {
  const radius = Math.max(worldRadius, 1e-6) / FK_SPHERE_GEOMETRY_RADIUS;
  return boneWorldScale.map((scale) => radius / (Math.abs(scale) > 1e-6 ? Math.abs(scale) : 1)) as [
    number,
    number,
    number,
  ];
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
  const pose = snapshotBone(bone);
  const roots: THREE.Bone[] = [];
  root.traverse(child => { if ((child as THREE.Bone).isBone && !(child.parent as THREE.Bone)?.isBone && child !== bone) roots.push(child as THREE.Bone); });
  const extra = roots.filter(candidate => { let p: THREE.Object3D | null = bone; while (p) { if (p === candidate) return false; p = p.parent; } return true; });
  if (extra.length) pose.additionalRoots = extra.map(snapshotBone);
  return pose;
}

/** Reject malformed/unbounded input before touching any live bone. */
export function validateBonePose(value: unknown): void {
  if (value == null) return;
  let count = 0;
  const seen = new Set<object>();
  const vector = (v: unknown, n: number) => Array.isArray(v) && v.length === n && v.every(x => typeof x === "number" && Number.isFinite(x));
  const visit = (node: any, depth: number) => {
    if (!node || typeof node !== "object" || seen.has(node) || depth > 128 || ++count > 4096 || typeof node.name !== "string" || node.name.length > 1024 ||
      !vector(node.position, 3) || !vector(node.quaternion, 4) || !Number.isFinite(Math.hypot(...node.quaternion)) || Math.hypot(...node.quaternion) < 1e-12 || !vector(node.scale, 3) || !Array.isArray(node.children)) throw new Error("骨骼姿势无效或超过安全上限");
    seen.add(node);
    for (const child of node.children) visit(child, depth + 1);
    if (node.additionalRoots !== undefined) {
      if (depth !== 0 || !Array.isArray(node.additionalRoots)) throw new Error("骨骼根列表无效");
      for (const child of node.additionalRoots) visit(child, depth + 1);
    }
  };
  visit(value, 0);
}

function planRestore(bone: THREE.Bone, node: BonePoseNode, plan: Array<[THREE.Bone, BonePoseNode]>) {
  plan.push([bone, node]);
  let complete = true;

  // 先按名字配对子骨；名字对不上（导出的骨名带前缀/后缀）时退回按顺序配。
  const boneChildren = bone.children.filter((child) => (child as THREE.Bone).isBone) as THREE.Bone[];
  const used = new Set<THREE.Bone>();
  for (const childNode of node.children) {
    const matched =
      boneChildren.find((candidate) => candidate.name === childNode.name && !used.has(candidate)) ??
      boneChildren.find((candidate) => !used.has(candidate) && !node.children.some(other => other.name === candidate.name));
    if (!matched) { complete = false; continue; }
    used.add(matched);
    if (!planRestore(matched, childNode, plan)) complete = false;
  }
  return complete;
}

/** 把存档里的骨树姿势套回模型；成功返回 true。 */
export function applyBonePose(root: THREE.Object3D, pose: BonePoseNode | null | undefined) {
  if (!pose) return false;
  validateBonePose(pose);
  // Legacy snapshots may start at Hips rather than its Bone ancestor.
  const named = root.getObjectByName(pose.name) as THREE.Bone | undefined;
  const bone = named?.isBone ? named : findSkeletonRoot(root);
  if (!bone) return false;
  const plan: Array<[THREE.Bone, BonePoseNode]> = [];
  let complete = planRestore(bone, pose, plan);
  const roots: THREE.Bone[] = [];
  root.traverse(child => { if ((child as THREE.Bone).isBone && !(child.parent as THREE.Bone)?.isBone && child !== bone) roots.push(child as THREE.Bone); });
  const used = new Set<THREE.Bone>();
  for (const saved of pose.additionalRoots ?? []) {
    const matched = roots.find(b => b.name === saved.name && !used.has(b)) ?? roots.find(b => !used.has(b));
    if (matched) { used.add(matched); if (!planRestore(matched, saved, plan)) complete = false; }
    else complete = false;
  }
  // Incompatible rigs must not silently leave a half-applied pose.
  if (!complete) return false;
  for (const [target, node] of plan) {
    target.position.fromArray(node.position);
    const length = Math.hypot(...node.quaternion);
    target.quaternion.set(node.quaternion[0] / length, node.quaternion[1] / length, node.quaternion[2] / length, node.quaternion[3] / length);
    target.scale.fromArray(node.scale);
  }
  root.updateMatrixWorld(true);
  return true;
}