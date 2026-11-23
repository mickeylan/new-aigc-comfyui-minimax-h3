import * as THREE from "three";
import { clone } from "three/examples/jsm/utils/SkeletonUtils.js";

export function isPoseEditorHelper(object: THREE.Object3D) {
  return object.name === "__FKHelperSphere__" || object.userData.poseEditorHelper === true ||
    object.type === "SkeletonHelper" || object.type === "TransformControlsRoot";
}

/** Hide without destroying selection/drag state; caller must restore in finally. */
export function hidePoseEditorHelpers(root: THREE.Object3D) {
  const hidden: THREE.Object3D[] = [];
  root.traverse(object => {
    if (isPoseEditorHelper(object) && object.visible) { object.visible = false; hidden.push(object); }
  });
  return () => hidden.forEach(object => { object.visible = true; });
}

/** SkeletonUtils remaps SkinnedMesh.skeleton to cloned bones, never live editor bones. */
export function clonePosedContent(root: THREE.Object3D) {
  const copy = clone(root);
  const helpers: THREE.Object3D[] = [];
  copy.traverse(object => { if (isPoseEditorHelper(object)) helpers.push(object); });
  helpers.forEach(object => object.removeFromParent());
  return copy;
}
