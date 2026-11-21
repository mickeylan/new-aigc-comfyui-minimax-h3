import { useEffect } from "react";
import { createRoot } from "react-dom/client";
import { MemoryRouter, useLocation } from "react-router-dom";
import {
  EditorCore,
  VideoEditor,
  buildDefaultScene,
  buildElementFromMedia,
  mediaTimeFromSeconds,
  ZERO_MEDIA_TIME,
  type MediaHandle,
  type MediaKind,
  type TProject,
  type VideoEditorAdapters,
} from "../video-editor/index";
import { processMediaAssets } from "../video-editor/lib/media/processing";
import { createComfyAdapters, mimeForMedia } from "./comfy-adapters";
import "../styles/studio.css";

export interface InitialMediaEntry {
  url: string;
  name: string;
  kind: MediaKind;
  mime?: string;
}

export interface MountVideoEditorOptions {
  projectId?: string;
  exitTo?: string;
  initialMedia?: InitialMediaEntry[];
  // 分镜上下文：编辑器据此把成片写回这个 Scene（另存候选）。
  scene?: { projectId: string | number; sceneId: number } | null;
}

export interface VideoEditorHandle {
  unmount(): void;
}

function buildBootstrapProject({ id }: { id: string }): TProject {
  const scene = buildDefaultScene({ name: "主场景", isMain: true });
  const now = new Date();
  return {
    metadata: {
      id,
      name: "未命名项目",
      duration: ZERO_MEDIA_TIME,
      createdAt: now,
      updatedAt: now,
    },
    scenes: [scene],
    currentSceneId: scene.id,
    settings: {
      fps: { numerator: 30, denominator: 1 } as never,
      canvasSize: { width: 1920, height: 1080 },
      background: { type: "color", color: "#000000" },
    },
    version: 1,
  };
}

/**
 * The editor uses react-router's useNavigate for its exit actions. The host
 * is Vue Router, so the island keeps a private MemoryRouter and hands any
 * navigation to `exitTo` back to the browser (which Vue Router then owns).
 */
function HostExit({ exitTo }: { exitTo: string }) {
  const location = useLocation();

  useEffect(() => {
    if (location.pathname === exitTo) {
      window.location.assign(exitTo);
    }
  }, [location.pathname, exitTo]);

  return null;
}

/**
 * Import server-hosted results (task outputs, materials) before the user
 * touches the editor. The editor's normal import path expects a File; we
 * fetch the URL once, keep the HTTP handle for later reloads, and drop the
 * clip onto the timeline so "进入剪辑" lands on an editable sequence.
 */
async function importInitialMedia(
  editor: EditorCore,
  adapters: VideoEditorAdapters,
  projectId: string,
  entries: InitialMediaEntry[],
): Promise<void> {
  let cursorSeconds = 0;

  for (const entry of entries) {
    try {
      const response = await fetch(entry.url);
      if (!response.ok) {
        throw new Error(`HTTP ${response.status}`);
      }
      const blob = await response.blob();
      const mime = entry.mime || blob.type || mimeForMedia(entry.kind, entry.name);
      const file = new File([blob], entry.name, { type: mime });
      const handle: MediaHandle = {
        id: `comfy|${encodeURIComponent(entry.url)}|${mime}`,
        kind: entry.kind,
      };

      const [asset] = await processMediaAssets({
        files: [file],
        toast: adapters.toast,
        mediaSource: adapters.mediaSource,
        existingHandles: [handle],
        existingResolved: [{ url: entry.url, mime }],
      });
      if (!asset) continue;

      await editor.media.addMediaAsset({ projectId, asset });
      if (asset.type === "audio") continue;

      const seconds =
        typeof asset.duration === "number" && asset.duration > 0
          ? asset.duration
          : 5;
      const element = buildElementFromMedia({
        mediaId: asset.id,
        mediaType: asset.type,
        name: asset.name,
        duration: mediaTimeFromSeconds({ seconds }),
        startTime: mediaTimeFromSeconds({ seconds: cursorSeconds }),
      });
      editor.timeline.insertElement({
        element,
        placement: { mode: "auto" },
      });
      cursorSeconds += seconds;
    } catch (error) {
      console.error("Initial media import failed:", entry, error);
      adapters.toast.error(`无法导入 ${entry.name}`);
    }
  }
}

/**
 * Mount the ArtCraft video editor as a React island inside the Vue app.
 *
 * The editor owns the full viewport while mounted, so it also owns the
 * `studio-theme` body class and the body scroll lock. Radix portals render
 * into <body>; the class keeps their styles inside the editor theme.
 */
export function mountVideoEditor(
  el: HTMLElement,
  options: MountVideoEditorOptions = {},
): VideoEditorHandle {
  const exitTo = options.exitTo ?? "/projects";

  // Initialize before React mounts: EditorCore is first-wins, so this pins
  // the ComfyStudio adapter bundle before EditorProvider's own initialize call.
  const adapters = createComfyAdapters({
    projectId: options.projectId,
    scene: options.scene ?? null,
  });
  EditorCore.initialize({ adapters });

  const editor = EditorCore.getInstance();
  const project = buildBootstrapProject({
    id: options.projectId ?? `local-${crypto.randomUUID()}`,
  });
  if (editor.project.getActive()?.metadata.id !== project.metadata.id) {
    editor.project.setActiveProject({ project });
    editor.scenes.initializeScenes({
      scenes: project.scenes,
      currentSceneId: project.currentSceneId,
    });
  }

  document.body.classList.add("studio-theme");
  document.body.style.overflow = "hidden";

  const root = createRoot(el);
  root.render(
    <MemoryRouter initialEntries={["/studio"]}>
      <HostExit exitTo={exitTo} />
      <VideoEditor
        adapters={adapters}
        projectId={project.metadata.id}
        exitTo={exitTo}
      />
    </MemoryRouter>,
  );

  if (options.initialMedia?.length) {
    void importInitialMedia(
      editor,
      adapters,
      project.metadata.id,
      options.initialMedia,
    );
  }

  return {
    unmount() {
      root.unmount();
      document.body.classList.remove("studio-theme");
      document.body.style.overflow = "";
    },
  };
}