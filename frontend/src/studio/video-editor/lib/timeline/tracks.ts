import type { TrackType } from "./types";

// Default user-visible names for each track type. Shown in the
// properties panel header and used when creating a new track without
// an explicit name (drag-and-drop, paste-element, etc.).
export const DEFAULT_TRACK_NAMES: Record<TrackType, string> = {
  video: "视频轨道",
  text: "文字轨道",
  audio: "音频轨道",
  graphic: "图形轨道",
  effect: "特效轨道",
} as const;
