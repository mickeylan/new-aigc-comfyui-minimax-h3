import type Konva from "konva";
import { buildNodeScreenRect } from "./Model3DOverlay";

/** 只实现 buildNodeScreenRect 用到的几个方法。 */
function fakeStage(options: {
  left?: number;
  top?: number;
  x?: number;
  y?: number;
  scaleX?: number;
  scaleY?: number;
}): Konva.Stage {
  const {
    left = 0,
    top = 0,
    x = 0,
    y = 0,
    scaleX = 1,
    scaleY = 1,
  } = options;
  return {
    container: () => ({
      getBoundingClientRect: () => ({ left, top, width: 800, height: 600 }),
    }),
    x: () => x,
    y: () => y,
    scaleX: () => scaleX,
    scaleY: () => scaleY,
  } as unknown as Konva.Stage;
}

const node = { x: 100, y: 50, width: 200, height: 120, scaleX: 1, scaleY: 1 };

test("把节点坐标换算成视口坐标", () => {
  const rect = buildNodeScreenRect(node, fakeStage({ left: 10, top: 20 }));
  expect(rect).toEqual({ left: 110, top: 70, width: 200, height: 120 });
});

test("叠加舞台平移与缩放", () => {
  const rect = buildNodeScreenRect(
    node,
    fakeStage({ left: 5, top: 7, x: 30, y: 40, scaleX: 2, scaleY: 0.5 }),
  );
  // left = 5 + 30 + 100*2 = 235；top = 7 + 40 + 50*0.5 = 72
  expect(rect.left).toBe(235);
  expect(rect.top).toBe(72);
  expect(rect.width).toBe(400);
  expect(rect.height).toBe(60);
});

test("节点自身的拉伸也计入尺寸", () => {
  const rect = buildNodeScreenRect(
    { ...node, scaleX: 1.5, scaleY: 2 },
    fakeStage({}),
  );
  expect(rect.width).toBe(300);
  expect(rect.height).toBe(240);
});

test("节点缩放的缺省值按 1 处理", () => {
  const rect = buildNodeScreenRect(
    { x: 0, y: 0, width: 40, height: 30 } as never,
    fakeStage({}),
  );
  expect(rect.width).toBe(40);
  expect(rect.height).toBe(30);
});

test("舞台还没挂上时给一块兜底尺寸而不是崩", () => {
  expect(buildNodeScreenRect(node, null)).toEqual({
    left: 0,
    top: 0,
    width: 300,
    height: 300,
  });
});