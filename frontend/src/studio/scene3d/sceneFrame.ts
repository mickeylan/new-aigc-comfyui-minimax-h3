export const FRAME_ASPECTS = { '16:9': 16 / 9, '9:16': 9 / 16, '1:1': 1, '3:2': 3 / 2, '2:3': 2 / 3 } as const;
export type FrameAspect = keyof typeof FRAME_ASPECTS;
export function fitFrame(width: number, height: number, aspect: FrameAspect) {
  const [x, y] = aspect.split(':').map(Number);
  const unit = Math.max(1, Math.floor(Math.min(width / x, height / y)));
  return { width: x * unit, height: y * unit, aspect: x / y };
}
/** Fixed 24mm sensor height. Vertical FOV remains the only stored authority. */
export const focalFromFov = (fov: number) => 12 / Math.tan(fov * Math.PI / 360);
export const fovFromFocal = (mm: number) => 360 / Math.PI * Math.atan(12 / Math.max(1, mm));
