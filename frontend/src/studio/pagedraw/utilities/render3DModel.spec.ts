import * as THREE from "three";
import {
  DEFAULT_MODEL3D_PARAMS,
  render3DModelToDataUrl,
} from "./render3DModel";

const { renderers, gltfOutcome } = vi.hoisted(() => ({
  renderers: [] as FakeRenderer[],
  gltfOutcome: { scene: undefined as unknown, error: undefined as unknown },
}));

type FakeRenderer = {
  dispose: ReturnType<typeof vi.fn>;
  forceContextLoss: ReturnType<typeof vi.fn>;
};

// jsdom has no WebGL; only the teardown calls matter here.
vi.mock("three", async (importOriginal) => {
  const actual = await importOriginal<typeof import("three")>();
  class WebGLRenderer {
    domElement = { toDataURL: () => "data:image/png;base64,AAAA" };
    dispose = vi.fn();
    forceContextLoss = vi.fn();
    constructor() {
      renderers.push(this);
    }
    setClearColor() {}
    setSize() {}
    render() {}
  }
  return { ...actual, WebGLRenderer };
});

vi.mock("three/examples/jsm/loaders/GLTFLoader.js", () => ({
  GLTFLoader: class {
    load(
      _url: string,
      onLoad: (gltf: { scene: unknown }) => void,
      _onProgress: unknown,
      onError: (error: unknown) => void,
    ) {
      if (gltfOutcome.error) onError(gltfOutcome.error);
      else onLoad({ scene: gltfOutcome.scene });
    }
  },
}));

describe("render3DModelToDataUrl", () => {
  beforeEach(() => {
    renderers.length = 0;
    gltfOutcome.scene = undefined;
    gltfOutcome.error = undefined;
  });

  it("frees the model and the renderer's context after rendering", async () => {
    const geometry = new THREE.BoxGeometry();
    const onDispose = vi.fn();
    geometry.addEventListener("dispose", onDispose);
    gltfOutcome.scene = new THREE.Mesh(geometry);

    const dataUrl = await render3DModelToDataUrl(
      "model.glb",
      DEFAULT_MODEL3D_PARAMS,
    );

    expect(dataUrl).toMatch(/^data:image\/png/);
    expect(onDispose).toHaveBeenCalledTimes(1);
    expect(renderers[0].dispose).toHaveBeenCalledTimes(1);
    expect(renderers[0].forceContextLoss).toHaveBeenCalledTimes(1);
  });

  it("frees the renderer's context when the model fails to load", async () => {
    gltfOutcome.error = new Error("404");

    await expect(
      render3DModelToDataUrl("missing.glb", DEFAULT_MODEL3D_PARAMS),
    ).rejects.toThrow("404");

    expect(renderers[0].dispose).toHaveBeenCalledTimes(1);
    expect(renderers[0].forceContextLoss).toHaveBeenCalledTimes(1);
  });

  it("默认取景能把 2 单位高的模型完整收进画面", () => {
    const p = DEFAULT_MODEL3D_PARAMS;
    const distance = Math.hypot(
      p.cameraPosition.x - p.cameraTarget.x,
      p.cameraPosition.y - p.cameraTarget.y,
      p.cameraPosition.z - p.cameraTarget.z,
    );
    const visibleHeight = 2 * distance * Math.tan((p.fov * Math.PI) / 360);

    // normalizeAndPlaceModel 把最大边缩到 2，且底面贴 y=0 → 高度最多 2。
    expect(visibleHeight).toBeGreaterThan(2.4);
    // 视线要落在模型身体上（0–2 之间），不能贴地也不能对着天上。
    expect(p.cameraTarget.y).toBeGreaterThan(0.6);
    expect(p.cameraTarget.y).toBeLessThan(1.4);
  });
});
