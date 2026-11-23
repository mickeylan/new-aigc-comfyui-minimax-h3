import { useState, useEffect } from "react";
import Konva from "konva";
import { BG_LAYER_ID, DRAW_LAYER_ID } from "../PaintSurface";
import { captureDocumentCanvas } from "../utilities/exportDocument";

export const captureStageEditsBitmap = async (
  stageRef: React.RefObject<Konva.Stage>,
  transformerRefs: React.RefObject<{ [key: string]: Konva.Transformer }>,
  width: number = 1024,
  height: number = 1024,
): Promise<ImageBitmap | undefined> => {
  if (!stageRef.current) {
    return undefined;
  }

  try {
    const canvas = captureDocumentCanvas(stageRef.current, width, height, [BG_LAYER_ID, DRAW_LAYER_ID]);
    return await createImageBitmap(canvas);
  } catch (error) {
    console.error("Failed to create ImageBitmap:", error);
    return undefined;
  }
};

export const useStageSnapshot = (
  stageRef: React.RefObject<Konva.Stage>,
  imageRef: React.RefObject<Konva.Image>,
  isSelectingRef: React.RefObject<boolean>,
  transformerRefs: React.RefObject<{ [key: string]: Konva.Transformer }>,
) => {
  const [animationFrameId, setAnimationFrameId] = useState<number | null>(null);

  const captureStageImage = (
    stageRef: React.RefObject<Konva.Stage>,
    width: number = 1024,
    height: number = 1024,
  ): string => {
    if (!stageRef.current) return "";

    return captureDocumentCanvas(stageRef.current, width, height, [BG_LAYER_ID, DRAW_LAYER_ID]).toDataURL("image/png");
  };

  useEffect(() => {
    const updateSnapshot = () => {
      if (!isSelectingRef.current && stageRef.current && imageRef.current) {
        const uri = captureStageImage(stageRef);
        if (uri) {
          const img = new window.Image();
          img.onload = () => {
            imageRef.current?.setAttrs({
              image: img,
            });
          };
          img.src = uri;
        }
      }
      const frameId = requestAnimationFrame(updateSnapshot);
      setAnimationFrameId(frameId);
    };

    const frameId = requestAnimationFrame(updateSnapshot);
    setAnimationFrameId(frameId);

    return () => {
      if (animationFrameId) {
        cancelAnimationFrame(animationFrameId);
      }
    };
  }, []);
};
