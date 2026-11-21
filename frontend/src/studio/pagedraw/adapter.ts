// 移植自 ArtCraft（MIT / Apache-2.0 双许可）；来源与依赖声明见 ../THIRD_PARTY_NOTICES.md。
import type { ReactNode } from "react";
import type { BaseSelectorImage } from "./types";

/** The subset of ArtCraft's image model that the canvas shell needs. */
export interface ImageModel {
  id?: string;
  displayName?: string;
  editingIsInpainting?: boolean;
  [key: string]: unknown;
}

export type GenerationProvider = string;

export type UploadMediaFn = (
  file: File,
) => Promise<{ url: string; mediaToken: string; thumbnailUrl?: string }>;

export interface PageDrawEditRequest {
  model?: ImageModel;
  provider?: GenerationProvider;
  canvasImageMediaToken?: string;
  imageMediaTokens?: string[];
  prompt?: string;
  disableSystemPrompt?: boolean;
  imageCount?: number;
  /** "auto" | "wide" | "tall" | "square" */
  aspectRatio?: string;
  /** "1k" | "2k" | "4k" */
  imageResolution?: string;
  frontendCaller?: string;
  frontendSubscriberId?: string;
}

export interface PageDrawInpaintRequest {
  model?: ImageModel;
  provider?: GenerationProvider;
  imageMediaToken?: string;
  maskImageRawBytes?: Uint8Array;
  prompt?: string;
  imageCount?: number;
  frontendCaller?: string;
  frontendSubscriberId?: string;
}

export interface PageDrawAdapter {
  enqueueEditImage(req: PageDrawEditRequest): Promise<{ status: string }>;

  enqueueInpaint(req: PageDrawInpaintRequest): Promise<{ status: string }>;

  enqueueBgRemoval(base64Image: string, nodeId: string): Promise<void>;

  uploadImage?: UploadMediaFn;

  renderBaseImageSelector(props: {
    onImageSelect: (image: BaseSelectorImage) => void;
    showLoading: boolean;
  }): ReactNode;

  onEnqueueMeta?: (meta: {
    prompt: string;
    refImageUrls: string[];
    modelType: string;
    timestamp: number;
  }) => void;
}