import test from 'node:test';
import assert from 'node:assert/strict';
import { readFileSync } from 'node:fs';
const stage = readFileSync(new URL('../src/studio/scene3d/Scene3DStage.tsx', import.meta.url), 'utf8');
const draw = readFileSync(new URL('../src/studio/pagedraw-host/PageDrawStage.tsx', import.meta.url), 'utf8');
test('参考图平面按纹理与原设置创建独立alpha材质，并在finally释放', () => {
  assert.match(stage, /referencePlaneMaterial\(material, texture, entry\.plane\)/);
  assert.match(stage, /alphaMaterials\.push\(alphaMaterial\)/);
  assert.match(stage, /finally \{[\s\S]*alphaMaterials\.forEach\(\(temporary\) => temporary\.dispose\(\)\)/);
  assert.match(stage, /item\.mesh\.material = item\.material/);
});
test('抠图显式复用模板，迟到结果和失效原对象均不能提交', () => {
  const body = stage.slice(stage.indexOf('const cutoutSelected ='), stage.indexOf('const applyBackgroundAsSkybox'));
  assert.match(body, /findTemplateId\('image_remove_background'\)/);
  assert.match(body, /epoch === sceneEpochRef\.current/);
  assert.match(body, /objectsRef\.current\.includes\(source\) && !source\.locked && source\.object\.visible/);
  assert.match(body, /if \(!live\(\)\) \{ disposeSceneEntry\(result\)/);
  assert.match(body, /addPlane\(portable,[\s\S]*chromaKey: false \}, true\)/);
  assert.match(body, /objectsRef\.current = \[\.\.\.objectsRef\.current, result\]/);
  assert.match(body, /historyRef\.current\.checkpoint/);
  assert.match(stage, /去背景并加入独立平面/);
});
test('2D工程载入经过解码与版本隔离，错误和中途编辑不替换画布', () => {
  assert.match(draw, /保存工程/); assert.match(draw, /恢复工程/);
  assert.match(draw, /await prepareDrawDocument\(await file\.text\(\)\)/);
  assert.match(draw, /if \(!mountedRef\.current \|\| request !== projectRequestRef\.current\) return/);
  assert.match(draw, /current\.drawNodes !== before\.drawNodes/);
  assert.match(draw, /current\.restorePreparedDocument\(prepared\)/);
  assert.match(draw, /file\.size > DRAW_DOCUMENT_MAX_BYTES/);
});
