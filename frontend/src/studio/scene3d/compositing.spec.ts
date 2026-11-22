import {
  DEFAULT_PLANE_ASPECT,
  DEFAULT_PLANE_LAYER,
  DEFAULT_SKYBOX,
  IMAGE_PLANE_ACCEPT,
  VIDEO_PLANE_ACCEPT,
  clampOpacity,
  detectMediaKind,
  isExportablePlane,
  planeSize,
  skyboxBackgroundColor,
} from "./compositing";

test("按扩展名区分图片、视频与不支持", () => {
  expect(detectMediaKind("board.png")).toBe("image");
  expect(detectMediaKind("board.JPG")).toBe("image");
  expect(detectMediaKind("ref.webp")).toBe("image");
  expect(detectMediaKind("plate.mp4")).toBe("video");
  expect(detectMediaKind("plate.WEBM")).toBe("video");
  expect(detectMediaKind("plate.mov")).toBe("video");
  expect(detectMediaKind("model.glb")).toBe("unsupported");
  expect(detectMediaKind("noext")).toBe("unsupported");
  // 查询串不该干扰判定。
  expect(detectMediaKind("board.png?v=3")).toBe("image");
});

test("accept 列表覆盖对应扩展名且带点", () => {
  expect(IMAGE_PLANE_ACCEPT.startsWith(".png")).toBe(true);
  expect(IMAGE_PLANE_ACCEPT).toContain(".jpg");
  expect(VIDEO_PLANE_ACCEPT.startsWith(".mp4")).toBe(true);
  expect(VIDEO_PLANE_ACCEPT).toContain(".webm");
});

test("平面按高度基准等比缩放，竖构图不会被压扁", () => {
  const wide = planeSize(16 / 9, 2);
  expect(wide.height).toBeCloseTo(2, 6);
  expect(wide.width).toBeCloseTo((16 / 9) * 2, 6);

  const tall = planeSize(9 / 16, 2);
  expect(tall.height).toBeCloseTo(2, 6);
  expect(tall.width).toBeCloseTo((9 / 16) * 2, 6);
  expect(tall.width).toBeLessThan(wide.width);
});

test("宽高比非法时退回正方形而不是 NaN", () => {
  expect(planeSize(0).width).toBe(planeSize(1).width);
  expect(planeSize(Number.NaN).width).toBe(planeSize(1).width);
  expect(planeSize(-3).width).toBe(planeSize(1).width);
  expect(Number.isFinite(planeSize(Infinity).width)).toBe(true);
});

test("不透明度夹在 0–1 之间", () => {
  expect(clampOpacity(0.5)).toBe(0.5);
  expect(clampOpacity(-1)).toBe(0);
  expect(clampOpacity(3)).toBe(1);
  expect(clampOpacity(Number.NaN)).toBe(1);
});

test("只有图片平面进导出，视频平面明确排除", () => {
  expect(isExportablePlane("image")).toBe(true);
  expect(isExportablePlane("video")).toBe(false);
  expect(isExportablePlane("unsupported")).toBe(false);
});

test("天空盒亮度夹在 0–2", () => {
  expect(skyboxBackgroundColor(1)).toBe(1);
  expect(skyboxBackgroundColor(-1)).toBe(0);
  expect(skyboxBackgroundColor(5)).toBe(2);
});

test("默认值不启用天空盒，平面默认双面且带轻微透明裁切", () => {
  expect(DEFAULT_SKYBOX.enabled).toBe(false);
  expect(DEFAULT_SKYBOX.asEnvironment).toBe(true);
  expect(DEFAULT_PLANE_LAYER.doubleSided).toBe(true);
  expect(DEFAULT_PLANE_LAYER.opacity).toBe(1);
  expect(DEFAULT_PLANE_LAYER.alphaTest).toBeGreaterThan(0);
  expect(DEFAULT_PLANE_ASPECT).toBeCloseTo(16 / 9, 6);
});