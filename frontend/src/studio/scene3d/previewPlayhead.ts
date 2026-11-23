import * as THREE from 'three';
import { AnimationPreview } from './animationPreview';

export const PREVIEW_FPS = 24;
export function previewFrameCount(duration: number): number {
  return Number.isFinite(duration) && duration > 0 ? Math.ceil(duration * PREVIEW_FPS) : 0;
}
export function previewFrameTime(frame: number, duration: number): { frame: number; seconds: number } {
  const last = previewFrameCount(duration);
  const index = Math.max(0, Math.min(last, Number.isFinite(frame) ? Math.round(frame) : 0));
  return { frame: index, seconds: last ? Math.min(index / PREVIEW_FPS, duration) : 0 };
}

/** One editing baseline across all seeks; never integrate time or capture a sampled pose. */
export class PreviewPlayhead {
  readonly position?: THREE.Vector3;
  readonly quaternion?: THREE.Quaternion;
  private cameraPosition: THREE.Vector3;
  private cameraQuaternion: THREE.Quaternion;
  private target: THREE.Vector3;
  private fov: number;
  private enabled: boolean;
  private damping: boolean | undefined;
  private animation?: AnimationPreview;
  private stopped = false;
  constructor(
    private camera: THREE.PerspectiveCamera,
    private controls: { target: THREE.Vector3; enabled: boolean; enableDamping?: boolean; update(): unknown },
    private mover?: THREE.Object3D,
    clip?: THREE.AnimationClip,
  ) {
    this.cameraPosition = camera.position.clone(); this.cameraQuaternion = camera.quaternion.clone();
    this.target = controls.target.clone(); this.fov = camera.fov; this.enabled = controls.enabled;
    this.position = mover?.position.clone(); this.quaternion = mover?.quaternion.clone();
    if (mover && clip) this.animation = new AnimationPreview(mover, clip);
    this.damping = controls.enableDamping;
    controls.enabled = false;
    controls.enableDamping = false;
    controls.update(); // Flush any pending orbit/pan inertia before random-access sampling.
    camera.position.copy(this.cameraPosition); controls.target.copy(this.target);
    camera.quaternion.copy(this.cameraQuaternion);
  }
  sample(seconds: number, movementSeconds: number, apply: (time: number) => void) {
    if (this.stopped || !Number.isFinite(seconds) || seconds < 0) return;
    apply(seconds);
    this.animation?.sampleAt(Math.min(seconds, movementSeconds));
    this.mover?.updateMatrixWorld(true);
  }
  stop() {
    if (this.stopped) return;
    this.stopped = true;
    this.animation?.stop();
    if (this.mover && this.position && this.quaternion) {
      this.mover.position.copy(this.position); this.mover.quaternion.copy(this.quaternion); this.mover.updateMatrixWorld(true);
    }
    this.camera.position.copy(this.cameraPosition); this.controls.target.copy(this.target);
    this.camera.fov = this.fov; this.camera.updateProjectionMatrix();
    this.controls.update(); this.camera.quaternion.copy(this.cameraQuaternion); this.camera.updateMatrixWorld(true);
    this.controls.enabled = this.enabled;
    this.controls.enableDamping = this.damping;
  }
}
