import * as THREE from "three";
import { SparkRenderer, SplatMesh } from "@sparkjsdev/spark";

/** Spark 能直接读的泼溅格式。`.ply` 有两种含义，Spark 只认高斯泼溅那种。 */
export const SPLAT_EXTENSIONS = ["spz", "ply", "splat", "ksplat"] as const;

export type SplatExtension = (typeof SPLAT_EXTENSIONS)[number];

/** 从 URL 里取小写扩展名，取不到返回空串。查询串和锚点先切掉。 */
export function splatExtension(url: string): string {
  const clean = (url || "").split(/[?#]/)[0];
  const match = /\.([a-z0-9]+)$/i.exec(clean);
  return match ? match[1].toLowerCase() : "";
}

/** 判断一个素材地址是不是高斯泼溅文件。 */
export function isSplatUrl(url: string): boolean {
  const ext = splatExtension(url);
  return (SPLAT_EXTENSIONS as readonly string[]).includes(ext);
}

/**
 * 把 splat 摆到地面上：Spark 的坐标系 Y 轴朝上，但 TripoSplat 等模型导出的
 * 点云常是 Z 轴朝上、原点在中心，直接塞进场景会半截埋在地里。
 * 这里按包围盒把最低点贴到 y=0，并把水平中心挪到原点，和 GLB 的摆放规则一致。
 */
export function placeSplatOnGround(mesh: THREE.Object3D): THREE.Vector3 {
  const box = new THREE.Box3().setFromObject(mesh);
  if (box.isEmpty()) return new THREE.Vector3();
  const center = box.getCenter(new THREE.Vector3());
  mesh.position.x += -center.x;
  mesh.position.z += -center.z;
  mesh.position.y += -box.min.y;
  return box.getSize(new THREE.Vector3());
}

/**
 * 创建 Spark 的渲染器。必须加进场景，splat 才会被绘制——
 * SplatMesh 自己只是数据，真正的光栅化在 SparkRenderer 里。
 * 官方建议 WebGLRenderer 用 antialias: false（抗锯齿对泼溅无效还掉帧）。
 *
 * 深度：SparkRenderer 本质是个 transparent 的 Mesh，three 把它排在所有不透明
 * 物体之后画，所以只要 depthTest 开着，网格就能挡住泼溅（泼溅在后）。
 * depthWrite 必须关——泼溅边缘大多是半透明的，让它写深度会把后面的东西
 * 挖出洞来，这是它被默认关掉的原因。反过来"泼溅挡住网格"做不到，
 * 属于已知限制。
 */
export function createSplatRenderer(renderer: THREE.WebGLRenderer): SparkRenderer {
  return new SparkRenderer({
    renderer,
    transparent: true,
    depthTest: true,
    depthWrite: false,
  });
}

export interface LoadSplatOptions {
  /** 关掉 Spark 自己的抗锯齿排序副作用，默认 true */
  placeOnGround?: boolean;
  /** 额外旋转（弧度）。默认绕 Z 轴 180°，对齐 Spark 与多数导出器的朝向差异。 */
  rotationZ?: number;
  /** 缩放倍数，默认 1 */
  scale?: number;
}

/**
 * 加载一个 splat 文件并摆好位置。返回的 mesh 需要由调用方加进场景。
 * `initialized` 是 Spark 的解码完成信号：await 之前 mesh 里还没有点数据，
 * 此时读包围盒会得到空盒子。
 */
export async function loadSplatMesh(
  url: string,
  options: LoadSplatOptions = {},
): Promise<SplatMesh> {
  const { placeOnGround = true, rotationZ = Math.PI, scale = 1 } = options;
  const mesh = await new SplatMesh({ url }).initialized;
  mesh.rotation.z = rotationZ;
  if (scale !== 1) mesh.scale.multiplyScalar(scale);
  mesh.userData.isSplat = true;
  if (placeOnGround) {
    // 包围盒要在加进场景、矩阵更新之后才算得准。
    mesh.updateMatrixWorld(true);
    placeSplatOnGround(mesh);
  }
  return mesh;
}

/**
 * 释放一个 splat。Spark 的 SplatMesh 有自己的 dispose（释放 GPU 上的
 * packed splat 缓冲），不能只靠 three 的 geometry/material.dispose。
 * 传进来的若不是 SplatMesh 则退回遍历释放。
 */
export function disposeSplat(object: THREE.Object3D | null | undefined): void {
  if (!object) return;
  const candidate = object as SplatMesh;
  if (typeof candidate.dispose === "function") {
    candidate.dispose();
    return;
  }
  object.traverse((child) => {
    const mesh = child as THREE.Mesh;
    mesh.geometry?.dispose();
    const material = mesh.material;
    if (Array.isArray(material)) {
      material.forEach((entry) => entry.dispose());
    } else {
      material?.dispose();
    }
  });
}