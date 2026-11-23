import { beforeEach, afterEach, describe, expect, it, vi } from "vitest";
import { Node } from "./Node";
import { useSceneStore } from "./stores/SceneState";
import { captureDocumentCanvas } from "./utilities/exportDocument";
import type Konva from "konva";

const shape = (id: string, extra = {}) => new Node({ id, type: "rectangle", x: 0, y: 0, width: 40, height: 30, fill: "red", ...extra });
const state = () => useSceneStore.getState();
beforeEach(() => state().RESET());
afterEach(() => vi.unstubAllGlobals());

describe("2D compositing document", () => {
  it("defaults legacy nodes and clamps layer alpha without using brush opacity", () => {
    expect(shape("a").layerOpacity).toBe(1);
    expect(shape("a").visible).toBe(true);
    expect(shape("a", { layerOpacity: -1 }).layerOpacity).toBe(0);
    expect(shape("a", { layerOpacity: 2 }).layerOpacity).toBe(1);
    expect(shape("a", { layerOpacity: NaN }).layerOpacity).toBe(1);
  });

  it("retains alpha and visibility through undo/redo, JSON and clipboard", async () => {
    state().addNode(shape("a"));
    state().updateNode("a", { layerOpacity: 0.35, visible: false });
    await state().undo();
    expect(state().drawNodes[0].layerOpacity).toBe(1);
    await state().redo();
    expect(state().drawNodes[0]).toMatchObject({ layerOpacity: 0.35, visible: false });
    const json = await state().exportSceneAsJson();
    state().RESET();
    expect(state().importSceneFromJson(json)).toBe(true);
    expect(state().drawNodes[0]).toMatchObject({ layerOpacity: 0.35, visible: false });
    state().selectNode("a");
    state().copySelectedItems();
    state().pasteItems();
    expect(state().drawNodes.at(-1)).toMatchObject({ layerOpacity: 0.35, visible: false });
  });

  it("keeps hidden layers in place, reorders and restores base-specific layers", () => {
    const base = { url: "", isBlankCanvas: true, blankCanvasWidth: 640, blankCanvasHeight: 480 };
    state().setBaseImageInfo(base);
    ["a", "b", "c"].forEach((id) => state().addNode(shape(id)));
    state().updateNode("b", { visible: false, layerOpacity: 0.2 });
    expect(state().drawNodes.map((n) => n.id)).toEqual(["a", "b", "c"]);
    state().selectNode("b");
    state().bringForward(["b"]);
    expect(state().drawNodes.map((n) => n.id)).toEqual(["a", "c", "b"]);
    state().sendToBack(["b"]);
    expect(state().drawNodes.map((n) => n.id)).toEqual(["b", "a", "c"]);
    state().setBaseImageInfo({ ...base });
    expect(state().drawNodes).toHaveLength(0);
    state().setBaseImageInfo(base);
    expect(state().drawNodes[0]).toMatchObject({ id: "b", visible: false, layerOpacity: 0.2 });
  });

  it("adds multiple independent image layers without replacing the base", async () => {
    class LoadedImage {
      naturalWidth = 2000; naturalHeight = 1000;
      onload?: () => void;
      set src(_value: string) { queueMicrotask(() => this.onload?.()); }
    }
    vi.stubGlobal("Image", LoadedImage);
    const base = { url: "", isBlankCanvas: true, blankCanvasWidth: 640, blankCanvasHeight: 480 };
    state().setBaseImageInfo(base);
    const file = new File(["fake image bytes"], "layer.png", { type: "image/png" });
    await state().createImageFromFile(0, 0, file);
    await state().createImageFromFile(20, 30, file);
    expect(state().baseImageInfo).toBe(base);
    expect(state().drawNodes).toHaveLength(2);
    expect(state().drawNodes[0]).toMatchObject({ width: 640, height: 320, type: "image" });
    expect(state().selectedNodeIds).toEqual([state().drawNodes[1].id]);
    expect(state().drawNodes[0].id).not.toBe(state().drawNodes[1].id);
    await expect(state().createImageFromFile(0, 0, new File(["x"], "bad.txt", { type: "text/plain" }))).rejects.toThrow();
    expect(state().drawNodes).toHaveLength(2);
  });
});

function fakeStage(fail = false) {
  const node = (id: string, initial = true) => {
    let visible = initial;
    return { id: () => id, visible: (next?: boolean) => next === undefined ? visible : (visible = next) };
  };
  const layers = [node("bg"), node("draw"), node("mask", false), node("handles")];
  const checker = node("checker");
  const selection = node("selection", false);
  let position = { x: 120, y: -30 };
  let scale = { x: 0.4, y: 0.4 };
  const canvas = {} as HTMLCanvasElement;
  let tweenAlpha = 0;
  const image = { opacity: (next?: number) => next === undefined ? tweenAlpha : (tweenAlpha = next) };
  const imageWrapper = { getChildren: () => [image] };
  const stage = {
    getLayers: () => layers,
    find: (selector: string) => selector === ".editor-only" ? [checker, selection] : selector === ".document-layer" ? [imageWrapper] : [],
    position: (next?: typeof position) => next ? (position = next) : position,
    scale: (next?: typeof scale) => next ? (scale = next) : scale,
    draw: vi.fn(),
    toCanvas: vi.fn((options) => {
      expect(options).toEqual({ x: 0, y: 0, width: 640, height: 480, pixelRatio: 1 });
      expect(position).toEqual({ x: 0, y: 0 });
      expect(scale).toEqual({ x: 1, y: 1 });
      expect(layers.map((n) => n.visible())).toEqual([true, true, false, false]);
      expect(checker.visible()).toBe(false);
      expect(image.opacity()).toBe(1);
      if (fail) throw new Error("tainted canvas");
      return canvas;
    }),
  };
  return { stage, canvas, layers, checker, selection, image };
}

it.each([false, true])("captures document bounds and restores exact editor state (failure=%s)", (fail) => {
  const { stage, canvas, layers, checker, selection, image } = fakeStage(fail);
  const capture = () => captureDocumentCanvas(stage as unknown as Konva.Stage, 640, 480, ["bg", "draw"]);
  if (fail) expect(capture).toThrow("tainted canvas");
  else expect(capture()).toBe(canvas);
  expect(stage.position()).toEqual({ x: 120, y: -30 });
  expect(stage.scale()).toEqual({ x: 0.4, y: 0.4 });
  expect(layers.map((n) => n.visible())).toEqual([true, true, false, true]);
  expect(checker.visible()).toBe(true);
  expect(selection.visible()).toBe(false);
  expect(image.opacity()).toBe(0);
});
