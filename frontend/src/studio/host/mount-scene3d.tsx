import { createRoot } from "react-dom/client";
import Scene3DStage from "../scene3d/Scene3DStage";
import type { SceneStudioContext } from '../scene3d/scene-archive';
import "../styles/studio.css";

export interface MountScene3DOptions {
  exitTo?: string;
  context?: SceneStudioContext;
}

export interface Scene3DHandle {
  unmount(): void;
}

/**
 * Mount the ArtCraft-derived 3D scene as a React island inside the Vue app.
 * No account, credits, or model picker; generation goes through ComfyStudio's
 * task API.
 */
export function mountScene3D(
  el: HTMLElement,
  options: MountScene3DOptions = {},
): Scene3DHandle {
  const root = createRoot(el);
  root.render(<Scene3DStage exitTo={options.exitTo} context={options.context} />);
  return {
    unmount() {
      root.unmount();
    },
  };
}