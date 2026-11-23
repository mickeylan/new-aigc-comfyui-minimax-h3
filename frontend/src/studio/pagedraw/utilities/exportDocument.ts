import type Konva from "konva";

/** Capture document pixels, never the editor viewport. Preserve every editor flag even on failure. */
export function captureDocumentCanvas(
  stage: Konva.Stage,
  width: number,
  height: number,
  contentLayerIds: readonly string[],
): HTMLCanvasElement {
  const position = { ...stage.position() };
  const scale = { ...stage.scale() };
  const hidden = [
    ...stage.getLayers().filter((layer) => !contentLayerIds.includes(layer.id())),
    ...stage.find(".editor-only"),
    ...stage.find("Transformer"),
  ];
  const visibility = new Map(hidden.map((node) => [node, node.visible()]));
  // Entrance tween alpha is editor-only. The wrapper owns the authoritative
  // layerOpacity; never normalize brush strokes (their alpha is actual content).
  const animationAlpha = new Map(
    stage.find<Konva.Group>(".document-layer").flatMap((group) =>
      group.getChildren().map((node) => [node, node.opacity()] as const)),
  );
  try {
    hidden.forEach((node) => node.visible(false));
    animationAlpha.forEach((_alpha, node) => node.opacity(1));
    stage.position({ x: 0, y: 0 });
    stage.scale({ x: 1, y: 1 });
    stage.draw();
    return stage.toCanvas({ x: 0, y: 0, width, height, pixelRatio: 1 });
  } finally {
    visibility.forEach((visible, node) => node.visible(visible));
    animationAlpha.forEach((alpha, node) => node.opacity(alpha));
    stage.position(position);
    stage.scale(scale);
    stage.draw();
  }
}
