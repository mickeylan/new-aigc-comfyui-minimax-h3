import { createRoot } from "react-dom/client";
import PageDrawStage from "../pagedraw-host/PageDrawStage";
import "../styles/studio.css";

export interface MountPageDrawOptions {
  initialImageUrl?: string;
  exitTo?: string;
}

export interface PageDrawHandle {
  unmount(): void;
}

/**
 * Mount the ArtCraft 2D canvas as a React island inside the Vue app.
 * The canvas has no account, credits, or model-selection UI; generation goes
 * through ComfyStudio's task API.
 */
export function mountPageDraw(
  el: HTMLElement,
  options: MountPageDrawOptions = {},
): PageDrawHandle {
  const root = createRoot(el);
  root.render(
    <PageDrawStage
      initialImageUrl={options.initialImageUrl}
      exitTo={options.exitTo}
    />,
  );
  return {
    unmount() {
      root.unmount();
    },
  };
}