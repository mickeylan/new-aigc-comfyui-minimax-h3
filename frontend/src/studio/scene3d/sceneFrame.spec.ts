import { it, expect } from 'vitest';
import { fitFrame, FRAME_ASPECTS, FRAME_VIDEO_SIZES, focalFromFov, fovFromFocal } from './sceneFrame';
import { validateCompositionDocument } from './planeDocument';
it('fits exact frame ratios inside arbitrary viewports, never distorts', () => {
 for (const aspect of Object.keys(FRAME_ASPECTS) as Array<keyof typeof FRAME_ASPECTS>) for (const [w,h] of [[1920,1080],[1080,1920],[873,631]]) { const f=fitFrame(w,h,aspect); expect(f.width).toBeLessThanOrEqual(w);expect(f.height).toBeLessThanOrEqual(h);expect(f.width/f.height).toBe(FRAME_ASPECTS[aspect]); }
});
it('keeps all five video ratios exact and even without increasing the old edge or pixel budget', () => {
 expect(Object.keys(FRAME_VIDEO_SIZES)).toEqual(Object.keys(FRAME_ASPECTS));
 for (const aspect of Object.keys(FRAME_ASPECTS) as Array<keyof typeof FRAME_ASPECTS>) {
  const {width,height}=FRAME_VIDEO_SIZES[aspect];
  expect(width/height).toBe(FRAME_ASPECTS[aspect]); expect(width%2).toBe(0); expect(height%2).toBe(0);
  expect(Math.max(width,height)).toBeLessThanOrEqual(1280); expect(width*height).toBeLessThanOrEqual(1280*720);
 }
 expect(FRAME_VIDEO_SIZES['16:9']).toEqual({width:1280,height:720});
});
it('roundtrips focal length through vertical fov using fixed sensor height',()=>{for(const mm of [12,24,35,50,85,92]) expect(focalFromFov(fovFromFocal(mm))).toBeCloseTo(mm,9);});
it('rejects unknown frame before scene replacement while retaining legacy fallback',()=>{const d:any={version:5,objects:[],model:{data:'YQ=='},camera:{position:[0,0,1],target:[0,0,0]},frame:{aspect:'9:16'}};expect(()=>validateCompositionDocument(d)).not.toThrow();d.frame.aspect='bad';expect(()=>validateCompositionDocument(d)).toThrow('画幅');delete d.frame;d.version=4;expect(()=>validateCompositionDocument(d)).not.toThrow();});
