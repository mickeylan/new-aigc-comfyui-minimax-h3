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
  createSplatRenderer,
  disposeSplat,
  isSplatUrl,
  loadSplatMesh,
  placeSplatOnGround,
  splatExtension,
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

test("SparkRenderer 收下 WebGLRenderer", () => {
  // jsdom 里没有 WebGL 上下文，构造真 renderer 会抛错，用替身顶替。
  const renderer = { domElement: {} } as unknown as THREE.WebGLRenderer;
  const sparkRenderer = createSplatRenderer(renderer) as unknown as {
    options: { renderer: unknown };
  };
  expect(sparkRenderer.options.renderer).toBe(renderer);
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