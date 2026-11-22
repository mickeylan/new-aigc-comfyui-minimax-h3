import * as THREE from "three";

// Spark 要 WebGL 上下文，单测换成可控替身。替身故意不继承 THREE.Object3D：
// vi.mock 的工厂会被提升到 import 之前执行，那里拿不到 THREE。
vi.mock("@sparkjsdev/spark", () => {
  const created: unknown[] = [];

  class SplatMesh {
    url?: string;
    position = { x: 0, y: 0, z: 0 };
    rotation = { x: 0, y: 0, z: 0 };
    scale = {
      x: 1,
      y: 1,
      z: 1,
      multiplyScalar(value: number) {
        this.x *= value;
        this.y *= value;
        this.z *= value;
        return this;
      },
    };
    userData: Record<string, unknown> = {};
    disposed = 0;
    initialized: Promise<unknown>;
    constructor(options: { url?: string } = {}) {
      this.url = options.url;
      created.push(this);
      this.initialized = Promise.resolve(this);
    }
    updateMatrixWorld() {}
    dispose() {
      this.disposed += 1;
    }
  }

  class SparkRenderer {
    constructor(public options: { renderer: unknown }) {}
  }

  return { SplatMesh, SparkRenderer, __created: created };
});

import {
  SPLAT_EXTENSIONS,
  SPLAT_FPS_GUARD,
  createSplatRenderer,
  disposeSplat,
  isSplatUrl,
  loadSplatMesh,
  placeSplatOnGround,
  splatExtension,
  stepSplatDetail,
  type SplatDetailGuardState,
} from "./splat";

async function sparkMock() {
  return (await import("@sparkjsdev/spark")) as unknown as {
    __created: { url?: string; disposed: number }[];
  };
}

beforeEach(async () => {
  (await sparkMock()).__created.length = 0;
});

test("按扩展名识别泼溅文件，查询串不干扰", () => {
  expect(isSplatUrl("room.spz")).toBe(true);
  expect(isSplatUrl("room.SPZ")).toBe(true);
  expect(isSplatUrl("room.ply")).toBe(true);
  expect(isSplatUrl("room.ksplat")).toBe(true);
  expect(isSplatUrl("http://host/a/room.spz?token=1")).toBe(true);
  expect(isSplatUrl("room.glb")).toBe(false);
  expect(isSplatUrl("room")).toBe(false);
  expect(isSplatUrl("")).toBe(false);
  expect(SPLAT_EXTENSIONS).toContain("spz");
});

test("取扩展名只认最后一段小写", () => {
  expect(splatExtension("a.B.SpZ")).toBe("spz");
  expect(splatExtension("a.spz#frag")).toBe("spz");
  expect(splatExtension("noext")).toBe("");
});

test("splat 按包围盒贴地并水平居中", () => {
  const mesh = new THREE.Object3D();
  const box = new THREE.Box3(
    new THREE.Vector3(-1, -2, -1),
    new THREE.Vector3(3, 4, 1),
  );
  vi.spyOn(THREE.Box3.prototype, "setFromObject").mockReturnValueOnce(box);

  const size = placeSplatOnGround(mesh);

  // 中心 x=1 → 反向平移 -1；最低点 y=-2 → 抬高 2；中心 z=0 不动。
  expect(mesh.position.x).toBeCloseTo(-1);
  expect(mesh.position.y).toBeCloseTo(2);
  expect(mesh.position.z).toBeCloseTo(0);
  expect(size.x).toBeCloseTo(4);
  expect(size.y).toBeCloseTo(6);
});

test("空包围盒不动位置", () => {
  const mesh = new THREE.Object3D();
  vi.spyOn(THREE.Box3.prototype, "setFromObject").mockReturnValueOnce(
    new THREE.Box3(),
  );
  placeSplatOnGround(mesh);
  expect(mesh.position.toArray()).toEqual([0, 0, 0]);
});

test("加载后会转 180°、标记为 splat 并贴地", async () => {
  // 贴地会读包围盒，替身没有几何，直接给一个固定盒子。
  vi.spyOn(THREE.Box3.prototype, "setFromObject").mockReturnValue(
    new THREE.Box3(new THREE.Vector3(-1, -2, -1), new THREE.Vector3(3, 4, 1)),
  );
  const mesh = (await loadSplatMesh("room.spz")) as unknown as {
    rotation: { z: number };
    position: { x: number; y: number; z: number };
    userData: Record<string, unknown>;
    scale: { x: number };
  };

  expect(mesh.rotation.z).toBeCloseTo(Math.PI);
  expect(mesh.userData.isSplat).toBe(true);
  expect(mesh.position.y).toBeCloseTo(2);
  expect(mesh.scale.x).toBe(1);
});

test("传给 Spark 的是原始 URL", async () => {
  await loadSplatMesh("http://host/a/room.spz?token=1", {
    placeOnGround: false,
  });
  expect((await sparkMock()).__created[0].url).toBe(
    "http://host/a/room.spz?token=1",
  );
});

test("可以关掉贴地与自定义旋转", async () => {
  const mesh = (await loadSplatMesh("room.spz", {
    placeOnGround: false,
    rotationZ: 0,
  })) as unknown as {
    rotation: { z: number };
    position: { x: number; y: number; z: number };
  };
  expect(mesh.rotation.z).toBe(0);
  expect(mesh.position).toEqual({ x: 0, y: 0, z: 0 });
});

test("scale 参数乘到自身缩放上", async () => {
  const mesh = (await loadSplatMesh("room.spz", {
    placeOnGround: false,
    scale: 2,
  })) as unknown as { scale: { x: number; y: number } };
  expect(mesh.scale.x).toBeCloseTo(2);
  expect(mesh.scale.y).toBeCloseTo(2);
});

test("SparkRenderer 收下 WebGLRenderer，并把深度策略写死", () => {
  // jsdom 里没有 WebGL 上下文，构造真 renderer 会抛错，用替身顶替。
  const renderer = { domElement: {} } as unknown as THREE.WebGLRenderer;
  const sparkRenderer = createSplatRenderer(renderer) as unknown as {
    options: { renderer: unknown; transparent?: boolean; depthTest?: boolean; depthWrite?: boolean };
  };
  expect(sparkRenderer.options.renderer).toBe(renderer);
  // 三个值都是故意显式传的：库默认变了也要保持"能挡泼溅、不挖洞"的行为。
  expect(sparkRenderer.options.transparent).toBe(true);
  expect(sparkRenderer.options.depthTest).toBe(true);
  expect(sparkRenderer.options.depthWrite).toBe(false);
});

test("SparkRenderer 默认开 LoD，细节上限可选", async () => {
  const renderer = { domElement: {} } as unknown as THREE.WebGLRenderer;
  const withDefault = createSplatRenderer(renderer) as unknown as {
    options: { enableLod?: boolean; lodSplatCount?: number };
  };
  expect(withDefault.options.enableLod).toBe(true);
  // 不传上限时别硬塞一个数进去，让库按平台取默认（桌面 2.5M）。
  expect("lodSplatCount" in withDefault.options).toBe(false);

  const withLimit = createSplatRenderer(renderer, { lodSplatCount: 750_000 }) as unknown as {
    options: { lodSplatCount?: number };
  };
  expect(withLimit.options.lodSplatCount).toBe(750_000);
});

test("帧率守护：连续掉帧降一档，恢复后升一档，升降之间带滞回与冷却", () => {
  const fresh: SplatDetailGuardState = { index: 2, belowSince: null, aboveSince: null, cooldownUntil: 0 };

  // 一帧掉到 20fps 不够，记下起点但不动档
  const first = stepSplatDetail(fresh, 20, 1_000, 3);
  expect(first.index).toBe(2);
  expect(first.belowSince).toBe(1_000);
  // 还没到 2 秒
  expect(stepSplatDetail(first, 20, 2_500, 3).index).toBe(2);
  // 满 2 秒 → 降一档，并进 30 秒冷却
  const down = stepSplatDetail(first, 20, 3_000, 3);
  expect(down.index).toBe(1);
  expect(down.cooldownUntil).toBe(3_000 + SPLAT_FPS_GUARD.cooldownMs);
  expect(down.belowSince).toBeNull();

  // 冷却期内即使一直很高也不升
  expect(stepSplatDetail(down, 120, 10_000, 3).index).toBe(1);

  // 帧率回中间区（30~50fps）不累积任何趋势
  const calm = stepSplatDetail(down, 45, 40_000, 3);
  expect(calm.aboveSince).toBeNull();
  expect(calm.belowSince).toBeNull();

  // 持续 5 秒高帧率 → 升一档
  const highStart = stepSplatDetail(down, 120, 40_000, 3);
  expect(highStart.aboveSince).toBe(40_000);
  expect(stepSplatDetail(highStart, 120, 44_000, 3).index).toBe(1);
  expect(stepSplatDetail(highStart, 120, 45_000, 3).index).toBe(2);
});

test("帧率守护不会越界：最低档不再降，最高档不再升", () => {
  const bottom: SplatDetailGuardState = { index: 0, belowSince: null, aboveSince: null, cooldownUntil: 0 };
  const stillBottom = stepSplatDetail(bottom, 5, 1_000, 3);
  expect(stillBottom.index).toBe(0);
  expect(stillBottom.belowSince).toBeNull();
  expect(stepSplatDetail(stillBottom, 5, 60_000, 3).index).toBe(0);

  const top: SplatDetailGuardState = { index: 3, belowSince: null, aboveSince: null, cooldownUntil: 0 };
  const stillTop = stepSplatDetail(top, 120, 1_000, 3);
  expect(stillTop.index).toBe(3);
  expect(stillTop.aboveSince).toBeNull();
  expect(stepSplatDetail(stillTop, 120, 60_000, 3).index).toBe(3);
});

test("释放走 Spark 自己的 dispose", async () => {
  const mesh = await loadSplatMesh("x.spz", { placeOnGround: false });
  disposeSplat(mesh);
  expect((await sparkMock()).__created[0].disposed).toBe(1);
});

test("释放空值不报错", () => {
  expect(() => disposeSplat(null)).not.toThrow();
  expect(() => disposeSplat(undefined)).not.toThrow();
});

test("非 SplatMesh 退回遍历释放几何与材质", () => {
  const geometry = new THREE.BufferGeometry();
  const material = new THREE.MeshBasicMaterial();
  const onGeometry = vi.fn();
  const onMaterial = vi.fn();
  geometry.addEventListener("dispose", onGeometry);
  material.addEventListener("dispose", onMaterial);
  const group = new THREE.Group();
  group.add(new THREE.Mesh(geometry, material));

  disposeSplat(group);

  expect(onGeometry).toHaveBeenCalledTimes(1);
  expect(onMaterial).toHaveBeenCalledTimes(1);
});