import { assetKind, assetUrl, isSupportedAsset, thumbnailUrl, ASSET_DRAG_MIME } from "./assetLibrary";

test("素材路径拼成 /api/input 下的字节地址", () => {
  expect(assetUrl("3d/robot.glb")).toBe("/api/input/3d/robot.glb");
  expect(assetUrl("robot.glb")).toBe("/api/input/robot.glb");
  // 只切第一个斜杠，子目录里再套一层也要保住。
  expect(assetUrl("3d/scene/robot.glb")).toBe("/api/input/3d/scene/robot.glb");
  // Windows 风格的反斜杠前缀不能变成双斜杠。
  expect(assetUrl("\\3d\\robot.glb")).toBe("/api/input/3d\\robot.glb");
});

test("缩略图优先用后端截的帧，没有就退回模型文件", () => {
  expect(thumbnailUrl({ id: 1, name: "a", path: "3d/a.glb", thumb_path: "3d/a.png" })).toBe(
    "/api/input/3d/a.png",
  );
  expect(thumbnailUrl({ id: 2, name: "b", path: "3d/b.glb" })).toBe("/api/input/3d/b.glb");
});

test("只有 glb/gltf/obj 能直接放进场景", () => {
  expect(assetKind("3d/robot.glb")).toBe("glb");
  expect(assetKind("3d/robot.GLB")).toBe("glb");
  expect(assetKind("3d/robot.gltf")).toBe("gltf");
  expect(assetKind("3d/robot.obj")).toBe("obj");
  expect(assetKind("3d/robot.fbx")).toBe("unsupported");
  expect(assetKind("3d/robot.ply")).toBe("unsupported");
  expect(assetKind("3d/robot.splat")).toBe("unsupported");
  expect(assetKind("robot")).toBe("unsupported");
  // 查询串不该干扰后缀判定。
  expect(assetKind("3d/robot.glb?v=2")).toBe("glb");
  expect(isSupportedAsset("3d/robot.glb")).toBe(true);
  expect(isSupportedAsset("3d/robot.fbx")).toBe(false);
});

test("拖放只用编辑器私有的 MIME 类型", () => {
  // 换成 text/plain 会和「从桌面拖文件进来」抢事件。
  expect(ASSET_DRAG_MIME).toBe("application/x-studio-3d-asset");
  expect(ASSET_DRAG_MIME).not.toBe("text/plain");
});