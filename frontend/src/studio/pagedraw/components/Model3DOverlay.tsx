import React, { useCallback, useEffect, useImperativeHandle, useRef } from "react";
import * as THREE from "three";
import { OrbitControls } from "three/examples/jsm/controls/OrbitControls.js";
import { GLTFLoader } from "three/examples/jsm/loaders/GLTFLoader.js";
import type Konva from "konva";
import type { Node } from "../Node";
import {
  DEFAULT_MODEL3D_PARAMS,
  disposeObject3D,
  type Model3DParams,
} from "../utilities/render3DModel";

/**
 * 覆盖在画布节点上的一层实时 3D 预览。
 *
 * 为什么不用 OrbitControls 直接交互：节点下面就是画布的选中框与拖拽，
 * 让用户在覆盖层里直接转相机必然和拖动节点打架。这里把交互收成
 * 「拖一条滑杆」式的三个动作（转相机 / 缩模型 / 改视角），
 * 覆盖层本身只负责显示，提交后把画面烤成一张 PNG 写回节点。
 */
export interface Model3DOverlayHandle {
  onRotateDrag: (dx: number, dy: number) => void;
  onScaleDrag: (dx: number, dy: number) => void;
  onFovDrag: (dx: number, dy: number) => void;
  commit: () => void;
}

interface Model3DOverlayProps {
  node: Node;
  stageRef: React.RefObject<Konva.Stage>;
  onCommit: (imageDataUrl: string, params: Model3DParams) => void;
  onDismiss: () => void;
}

/** Konva 节点在视口里的矩形，供 `position: fixed` 的覆盖层定位。 */
export function buildNodeScreenRect(
  node: Pick<Node, "x" | "y" | "width" | "height" | "scaleX" | "scaleY">,
  stage: Konva.Stage | null,
): { left: number; top: number; width: number; height: number } {
  if (!stage) return { left: 0, top: 0, width: 300, height: 300 };

  const containerRect = stage.container().getBoundingClientRect();
  const stageScaleX = stage.scaleX();
  const stageScaleY = stage.scaleY();
  const nodeScaleX = node.scaleX ?? 1;
  const nodeScaleY = node.scaleY ?? 1;

  return {
    left: containerRect.left + stage.x() + node.x * stageScaleX,
    top: containerRect.top + stage.y() + node.y * stageScaleY,
    width: node.width * nodeScaleX * stageScaleX,
    height: node.height * nodeScaleY * stageScaleY,
  };
}

export const Model3DOverlay = React.forwardRef<
  Model3DOverlayHandle,
  Model3DOverlayProps
>(function Model3DOverlay({ node, stageRef, onCommit, onDismiss }, ref) {
  const overlayRef = useRef<HTMLDivElement>(null);
  const canvasRef = useRef<HTMLCanvasElement>(null);

  const sceneRef = useRef<THREE.Scene | null>(null);
  const cameraRef = useRef<THREE.PerspectiveCamera | null>(null);
  const rendererRef = useRef<THREE.WebGLRenderer | null>(null);
  const controlsRef = useRef<OrbitControls | null>(null);
  const frameRef = useRef<number | null>(null);
  const modelRef = useRef<THREE.Object3D | null>(null);
  const modelScaleRef = useRef<number>(
    node.model3dParams?.modelScale ?? DEFAULT_MODEL3D_PARAMS.modelScale,
  );
  // 提交只能发生一次：Esc、点外部、按钮三条路都可能触发。
  const committedRef = useRef(false);

  const rect = buildNodeScreenRect(node, stageRef.current);

  const captureParams = useCallback((): Model3DParams => {
    const camera = cameraRef.current;
    const controls = controlsRef.current;
    const base = node.model3dParams ?? DEFAULT_MODEL3D_PARAMS;
    if (!camera || !controls) return base;
    return {
      cameraPosition: {
        x: camera.position.x,
        y: camera.position.y,
        z: camera.position.z,
      },
      cameraTarget: {
        x: controls.target.x,
        y: controls.target.y,
        z: controls.target.z,
      },
      fov: camera.fov,
      modelScale: modelScaleRef.current,
      // 原生尺寸在节点生命周期内固定，换了会让已排好的版跳位。
      nativeWidth: base.nativeWidth,
      nativeHeight: base.nativeHeight,
    };
  }, [node.model3dParams]);

  const commit = useCallback(() => {
    if (committedRef.current) return;
    committedRef.current = true;

    const renderer = rendererRef.current;
    const scene = sceneRef.current;
    const camera = cameraRef.current;
    if (!renderer || !scene || !camera) {
      onDismiss();
      return;
    }

    // 按原生尺寸出图，再由 Konva 拉伸到节点当前框，
    // 这样用户之前拉出来的变形会被保住。
    const base = node.model3dParams ?? DEFAULT_MODEL3D_PARAMS;
    const nativeW = base.nativeWidth ?? Math.round(node.width * (node.scaleX ?? 1));
    const nativeH = base.nativeHeight ?? Math.round(node.height * (node.scaleY ?? 1));
    renderer.setSize(nativeW, nativeH);
    camera.aspect = nativeW / nativeH;
    camera.updateProjectionMatrix();
    renderer.render(scene, camera);

    onCommit(renderer.domElement.toDataURL("image/png"), captureParams());
  }, [captureParams, node, onCommit, onDismiss]);

  const onRotateDrag = useCallback((dx: number, dy: number) => {
    const camera = cameraRef.current;
    const controls = controlsRef.current;
    if (!camera || !controls) return;
    const offset = new THREE.Vector3().subVectors(camera.position, controls.target);
    const spherical = new THREE.Spherical().setFromVector3(offset);
    spherical.theta -= dx * 0.01;
    spherical.phi = Math.max(0.01, Math.min(Math.PI - 0.01, spherical.phi - dy * 0.01));
    offset.setFromSpherical(spherical);
    camera.position.copy(controls.target).add(offset);
    camera.lookAt(controls.target);
    controls.update();
  }, []);

  const onScaleDrag = useCallback((dx: number) => {
    const model = modelRef.current;
    if (!model) return;
    const next = Math.max(0.1, Math.min(3, modelScaleRef.current + dx * 0.01));
    const ratio = next / modelScaleRef.current;
    model.scale.multiplyScalar(ratio);
    modelScaleRef.current = next;
    // 先把位置归零再算包围盒，否则缩放后模型会在世界空间里漂。
    model.position.set(0, 0, 0);
    const box = new THREE.Box3().setFromObject(model);
    const center = box.getCenter(new THREE.Vector3());
    model.position.x = -center.x;
    model.position.z = -center.z;
    model.position.y = -box.min.y;
  }, []);

  const onFovDrag = useCallback((dx: number) => {
    const camera = cameraRef.current;
    if (!camera) return;
    camera.fov = Math.max(20, Math.min(90, camera.fov + dx * 0.3));
    camera.updateProjectionMatrix();
  }, []);

  useImperativeHandle(
    ref,
    () => ({ onRotateDrag, onScaleDrag, onFovDrag, commit }),
    [commit, onFovDrag, onRotateDrag, onScaleDrag],
  );

  useEffect(() => {
    const canvas = canvasRef.current;
    if (!canvas) return;

    const params = node.model3dParams ?? DEFAULT_MODEL3D_PARAMS;
    // 渲染尺寸固定为原生尺寸，CSS 负责拉伸填满覆盖层，
    // 于是「看到的拉伸效果」就是提交后会得到的效果。
    const nativeW = params.nativeWidth ?? Math.round(rect.width);
    const nativeH = params.nativeHeight ?? Math.round(rect.height);

    const scene = new THREE.Scene();
    scene.background = null;
    sceneRef.current = scene;

    const renderer = new THREE.WebGLRenderer({ antialias: true, alpha: true, canvas });
    renderer.setClearColor(0x000000, 0);
    // 第三个参数 false：不要改 canvas 的 style 宽高，交给 CSS 拉伸。
    renderer.setSize(nativeW, nativeH, false);
    rendererRef.current = renderer;

    const camera = new THREE.PerspectiveCamera(params.fov, nativeW / nativeH, 0.1, 1000);
    camera.position.set(
      params.cameraPosition.x,
      params.cameraPosition.y,
      params.cameraPosition.z,
    );
    cameraRef.current = camera;

    const controls = new OrbitControls(camera, canvas);
    controls.target.set(
      params.cameraTarget.x,
      params.cameraTarget.y,
      params.cameraTarget.z,
    );
    // 交互交给外面的滑杆，这里只借 OrbitControls 的相机数学。
    controls.enabled = false;
    controls.update();
    controlsRef.current = controls;

    scene.add(new THREE.AmbientLight(0xffffff, 2));
    scene.add(new THREE.HemisphereLight(0xffffff, 0x888888, 1.2));
    const key = new THREE.DirectionalLight(0xffffff, 2);
    key.position.set(2, 10, 8);
    scene.add(key);
    const fill = new THREE.DirectionalLight(0xffffff, 1.2);
    fill.position.set(-6, 6, -4);
    scene.add(fill);
    const front = new THREE.DirectionalLight(0xffffff, 1);
    front.position.set(0, 4, 10);
    scene.add(front);

    // 覆盖层关掉或换了模型后才加载完的模型要立刻释放，不能留在场景里。
    let cancelled = false;
    new GLTFLoader().load(
      node.modelUrl!,
      (gltf) => {
        if (cancelled) {
          disposeObject3D(gltf.scene);
          return;
        }
        const model = gltf.scene;
        const box = new THREE.Box3().setFromObject(model);
        const size = box.getSize(new THREE.Vector3());
        const maxDim = Math.max(size.x, size.y, size.z) || 1;
        model.scale.multiplyScalar((2 / maxDim) * params.modelScale);
        const scaled = new THREE.Box3().setFromObject(model);
        const center = scaled.getCenter(new THREE.Vector3());
        model.position.x = -center.x;
        model.position.z = -center.z;
        model.position.y = -scaled.min.y;
        scene.add(model);
        modelRef.current = model;
      },
      undefined,
      (error) => console.error("[Model3DOverlay] 模型加载失败：", error),
    );

    const animate = () => {
      frameRef.current = requestAnimationFrame(animate);
      renderer.render(scene, camera);
    };
    animate();

    return () => {
      cancelled = true;
      if (frameRef.current) cancelAnimationFrame(frameRef.current);
      controls.dispose();
      disposeObject3D(scene);
      renderer.dispose();
      // 换模型会在同一个 canvas 上重建 renderer，上下文还能复用；
      // 只有覆盖层真的离开 DOM 之后才能强制丢弃。
      if (!canvas.isConnected) renderer.forceContextLoss();
    };
    // rect 只在挂载时决定初始渲染尺寸，之后不该重跑。
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [node.modelUrl]);

  useEffect(() => {
    const onKey = (event: KeyboardEvent) => {
      if (event.key === "Escape") commit();
    };
    window.addEventListener("keydown", onKey);
    return () => window.removeEventListener("keydown", onKey);
  }, [commit]);

  const onOverlayPointerDown = (event: React.PointerEvent) => {
    if (overlayRef.current && !overlayRef.current.contains(event.target as Element)) {
      commit();
    }
  };

  return (
    <div
      className="pointer-events-auto fixed inset-0 z-50"
      onPointerDown={onOverlayPointerDown}
    >
      <div
        ref={overlayRef}
        className="absolute overflow-hidden rounded-lg shadow-2xl ring-1 ring-white/20"
        style={{
          left: rect.left,
          top: rect.top,
          width: rect.width,
          height: rect.height,
          transform: `rotate(${node.rotation ?? 0}deg)`,
          transformOrigin: "0 0",
        }}
        onPointerDown={(event) => event.stopPropagation()}
      >
        <canvas ref={canvasRef} className="h-full w-full" style={{ display: "block" }} />
      </div>
    </div>
  );
});