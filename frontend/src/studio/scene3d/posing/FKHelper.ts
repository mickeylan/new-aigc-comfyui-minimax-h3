// 移植自 ArtCraft（MIT / Apache-2.0 双许可）；来源与依赖声明见 ../../../THIRD_PARTY_NOTICES.md。
// 骨骼摆姿（FK）的交互层：给每根可摆的骨挂一个红球，点球把旋转 gizmo 吸到该骨上。
import * as THREE from "three";
import { TransformControls } from "three/examples/jsm/controls/TransformControls.js";
import { isPoseableBone } from "./bonePose.js";

export const FK_SPHERE_NAME = "__FKHelperSphere__";

export interface FKHelperOptions {
  camera: THREE.Camera;
  domElement: HTMLElement;
  scene: THREE.Scene;
  /** 拖动骨骼 gizmo 时通知外部暂停轨道控制，避免相机跟着转。 */
  onDragChange: (dragging: boolean) => void;
  /** 选中某根骨时通知外部（用于面板显示骨名）。 */
  onBoneSelected?: (boneName: string | null) => void;
}

export class FKHelper {
  private readonly scene: THREE.Scene;
  private readonly controls: TransformControls;
  private readonly raycaster = new THREE.Raycaster();
  private readonly spheres: THREE.Mesh[] = [];
  private skeletonHelper: THREE.SkeletonHelper | null = null;
  private target: THREE.Object3D | null = null;
  private helperAdded = false;

  constructor({ camera, domElement, scene, onDragChange, onBoneSelected }: FKHelperOptions) {
    this.scene = scene;
    // 局部空间旋转：绕骨自身轴转，才符合 FK 的手感。
    this.controls = new TransformControls(camera, domElement);
    this.controls.setSpace("local");
    this.controls.setMode("rotate");
    this.controls.setSize(0.5);
    this.controls.addEventListener("dragging-changed", (event) => {
      onDragChange(Boolean((event as { value?: boolean }).value));
    });
    this.controls.addEventListener("objectChange", () => {
      const attached = this.controls.object;
      if (attached) attached.updateMatrixWorld(true);
    });
    this.controls.addEventListener("mouseUp", () => {
      const attached = this.controls.object as THREE.Bone | undefined;
      onBoneSelected?.(attached?.isBone ? attached.name : null);
    });
  }

  /** 把 FK 目标切到某个模型；模型没有骨时返回 false。 */
  setTarget(target: THREE.Object3D) {
    this.clear();
    this.target = target;

    const bones: THREE.Bone[] = [];
    target.traverse((child) => {
      if (isPoseableBone(child)) bones.push(child);
    });
    if (bones.length === 0) return false;

    const sphereGeometry = new THREE.SphereGeometry(0.015, 16, 16);
    for (const bone of bones) {
      // depthTest:false 让红球永远画在模型前面，否则躯干内的骨会被遮住。
      const material = new THREE.MeshBasicMaterial({
        color: 0xff3355,
        depthTest: false,
        transparent: true,
      });
      const sphere = new THREE.Mesh(sphereGeometry, material);
      // 骨的世界缩放常常很小（Mixamo 模型缩到 0.01 级别），球要反向缩放才看得出大小。
      const parentScale = new THREE.Vector3();
      bone.getWorldScale(parentScale);
      sphere.scale.set(
        parentScale.x ? 1 / parentScale.x : 1,
        parentScale.y ? 1 / parentScale.y : 1,
        parentScale.z ? 1 / parentScale.z : 1,
      );
      sphere.name = FK_SPHERE_NAME;
      sphere.renderOrder = 999;
      bone.add(sphere);
      this.spheres.push(sphere);
    }

    this.skeletonHelper = new THREE.SkeletonHelper(target);
    (this.skeletonHelper.material as THREE.LineBasicMaterial).depthTest = false;
    this.skeletonHelper.renderOrder = 998;
    this.scene.add(this.skeletonHelper);
    return true;
  }

  /**
   * 在画布坐标上点一下：命中红球则把 gizmo 吸到该骨，未命中则收起 gizmo。
   * 返回被选中的骨名（用于面板提示），未命中返回 null。
   */
  handleClick(ndc: THREE.Vector2): string | null {
    if (this.spheres.length === 0) return null;
    this.raycaster.setFromCamera(ndc, this.controls.camera as THREE.Camera);
    const hits = this.raycaster.intersectObjects(this.spheres, false);
    if (hits.length === 0) {
      this.resetHighlight();
      this.detachControls();
      return null;
    }
    const sphere = hits[0].object as THREE.Mesh;
    this.highlight(sphere);
    const bone = sphere.parent;
    if (!bone || !(bone as THREE.Bone).isBone) return null;
    this.controls.attach(bone);
    if (!this.helperAdded) {
      this.scene.add(this.controls.getHelper());
      this.helperAdded = true;
    }
    return bone.name;
  }

  /** 姿势还原或撤销后，让 gizmo 跟着更新。 */
  refresh() {
    this.controls.object?.updateMatrixWorld(true);
  }

  getAttachedBoneName() {
    const attached = this.controls.object as THREE.Bone | undefined;
    return attached?.isBone ? attached.name : null;
  }

  private highlight(active: THREE.Mesh) {
    for (const sphere of this.spheres) {
      (sphere.material as THREE.MeshBasicMaterial).opacity = sphere === active ? 1 : 0.25;
    }
  }

  private resetHighlight() {
    for (const sphere of this.spheres) {
      (sphere.material as THREE.MeshBasicMaterial).opacity = 1;
    }
  }

  private detachControls() {
    this.controls.detach();
    if (this.helperAdded) {
      this.scene.remove(this.controls.getHelper());
      this.helperAdded = false;
    }
  }

  private removeSpheres() {
    for (const sphere of this.spheres) {
      sphere.parent?.remove(sphere);
      sphere.geometry.dispose();
      (sphere.material as THREE.Material).dispose();
    }
    this.spheres.length = 0;
  }

  clear() {
    this.detachControls();
    this.removeSpheres();
    if (this.skeletonHelper) {
      this.scene.remove(this.skeletonHelper);
      this.skeletonHelper.geometry.dispose();
      (this.skeletonHelper.material as THREE.Material).dispose();
      this.skeletonHelper = null;
    }
    this.target = null;
  }

  get boneCount() {
    return this.spheres.length;
  }

  dispose() {
    this.clear();
    this.controls.dispose();
    this.target = null;
  }
}