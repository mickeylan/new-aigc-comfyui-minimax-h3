import * as THREE from "three";
import {
  applyBonePose,
  collectPoseableBones,
  findSkeletonRoot,
  hasSkeleton,
  isBlacklistedBone,
  isPoseableBone,
  serializeBonePose,
} from "./bonePose";

function buildCharacter() {
  // Mixamo 风格的骨层级：hips 下分 spine 与两条腿，另挂一根被黑名单挡掉的指骨。
  const model = new THREE.Group();
  model.name = "character";

  const hips = new THREE.Bone();
  hips.name = "mixamorigHips";
  hips.position.set(0, 1, 0);

  const spine = new THREE.Bone();
  spine.name = "mixamorigSpine";
  spine.position.set(0, 0.2, 0);

  const leftArm = new THREE.Bone();
  leftArm.name = "mixamorigLeftArm";
  leftArm.position.set(0.15, 0.3, 0);

  const leftHandIndex = new THREE.Bone();
  leftHandIndex.name = "mixamorigLeftHandIndex1";

  const leftUpLeg = new THREE.Bone();
  leftUpLeg.name = "mixamorigLeftUpLeg";
  leftUpLeg.position.set(-0.1, -0.1, 0);

  spine.add(leftArm);
  leftArm.add(leftHandIndex);
  hips.add(spine);
  hips.add(leftUpLeg);

  const mesh = new THREE.Mesh(new THREE.BoxGeometry(1, 1, 1), new THREE.MeshBasicMaterial());
  mesh.name = "body";
  model.add(mesh);
  model.add(hips);
  return { model, hips, spine, leftArm, leftHandIndex, leftUpLeg };
}

test("黑名单按子串命中手指与面部骨", () => {
  expect(isBlacklistedBone("mixamorigLeftHandIndex1")).toBe(true);
  expect(isBlacklistedBone("mixamorigLeftHandThumb2")).toBe(true);
  expect(isBlacklistedBone("Eye_J")).toBe(true);
  expect(isBlacklistedBone("mixamorigLeftArm")).toBe(false);
  expect(isBlacklistedBone("mixamorigHips")).toBe(false);
});

test("只有带名字的 Bone 被当作可摆骨", () => {
  const bone = new THREE.Bone();
  expect(isPoseableBone(bone)).toBe(false);

  bone.name = "mixamorigHips";
  expect(isPoseableBone(bone)).toBe(true);

  bone.name = "mixamorigLeftHandIndex1";
  expect(isPoseableBone(bone)).toBe(false);

  const mesh = new THREE.Mesh(new THREE.BoxGeometry(1, 1, 1), new THREE.MeshBasicMaterial());
  mesh.name = "mixamorigSpine";
  expect(isPoseableBone(mesh)).toBe(false);
});

test("收集可摆骨时跳过黑名单骨与普通网格", () => {
  const { model } = buildCharacter();
  const names = collectPoseableBones(model).map((bone) => bone.name);
  expect(names).toEqual([
    "mixamorigHips",
    "mixamorigSpine",
    "mixamorigLeftArm",
    "mixamorigLeftUpLeg",
  ]);
  expect(names).not.toContain("mixamorigLeftHandIndex1");
  expect(names).not.toContain("body");
  expect(hasSkeleton(model)).toBe(true);
});

test("没有骨骼的模型不显示摆姿能力", () => {
  const model = new THREE.Group();
  model.add(new THREE.Mesh(new THREE.BoxGeometry(1, 1, 1), new THREE.MeshBasicMaterial()));
  expect(hasSkeleton(model)).toBe(false);
  expect(findSkeletonRoot(model)).toBeNull();
});

test("骨树根优先取 mixamorighips", () => {
  const { model, hips } = buildCharacter();
  expect(findSkeletonRoot(model)).toBe(hips);
});

test("没有 hips 时退回到最上层的骨", () => {
  const model = new THREE.Group();
  const root = new THREE.Bone();
  root.name = "ArmatureRoot";
  const child = new THREE.Bone();
  child.name = "Neck";
  root.add(child);
  model.add(root);
  expect(findSkeletonRoot(model)).toBe(root);
});

test("姿势序列化后再套回，骨的局部变换完全一致", () => {
  const { model, spine, leftArm } = buildCharacter();
  spine.rotation.set(0.3, 0.4, 0.5);
  leftArm.rotation.set(-0.2, 0.1, 0.05);
  leftArm.position.set(0.2, 0.4, 0.1);
  leftArm.scale.set(1.2, 1.1, 1.3);

  const pose = serializeBonePose(model);
  expect(pose).not.toBeNull();
  expect(pose?.name).toBe("mixamorigHips");
  expect(pose?.children.map((child) => child.name)).toEqual([
    "mixamorigSpine",
    "mixamorigLeftUpLeg",
  ]);

  // 打乱当前姿势，再套回存档，应当逐字段还原。
  spine.rotation.set(0, 0, 0);
  leftArm.rotation.set(0, 0, 0);
  leftArm.position.set(0, 0, 0);
  leftArm.scale.set(1, 1, 1);

  expect(applyBonePose(model, pose)).toBe(true);
  expect(spine.rotation.x).toBeCloseTo(0.3, 6);
  expect(spine.rotation.y).toBeCloseTo(0.4, 6);
  expect(spine.rotation.z).toBeCloseTo(0.5, 6);
  expect(leftArm.rotation.x).toBeCloseTo(-0.2, 6);
  expect(leftArm.position.y).toBeCloseTo(0.4, 6);
  expect(leftArm.scale.x).toBeCloseTo(1.2, 6);
});

test("子骨改名后仍按顺序还原，不会整棵失效", () => {
  const { model, leftArm } = buildCharacter();
  leftArm.rotation.set(0.25, 0, 0);
  const pose = serializeBonePose(model);
  expect(pose).not.toBeNull();

  leftArm.name = "mixamorigLeftArm_renamed";
  leftArm.rotation.set(0, 0, 0);
  expect(applyBonePose(model, pose)).toBe(true);
  expect(leftArm.rotation.x).toBeCloseTo(0.25, 6);
});

test("空姿势或没有骨骼时还原返回 false 而不是抛错", () => {
  const { model } = buildCharacter();
  expect(applyBonePose(model, null)).toBe(false);
  expect(applyBonePose(model, undefined)).toBe(false);

  const plain = new THREE.Group();
  plain.add(new THREE.Mesh(new THREE.BoxGeometry(1, 1, 1), new THREE.MeshBasicMaterial()));
  expect(serializeBonePose(plain)).toBeNull();
  expect(
    applyBonePose(plain, {
      name: "root",
      position: [0, 0, 0],
      quaternion: [0, 0, 0, 1],
      scale: [1, 1, 1],
      children: [],
    }),
  ).toBe(false);
});