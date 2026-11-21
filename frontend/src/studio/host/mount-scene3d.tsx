import { createRoot } from "react-dom/client";
import Scene3DStage from "../scene3d/Scene3DStage";
import "../styles/studio.css";

export interface MountScene3DOptions {
  exitTo?: string;
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
  root.render(<Scene3DStage exitTo={options.exitTo} />);
  return {
    unmount() {
      root.unmount();
    },
  };
}