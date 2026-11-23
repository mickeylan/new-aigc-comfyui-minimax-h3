import test from 'node:test';
import assert from 'node:assert/strict';
import { readFileSync } from 'node:fs';
const source = readFileSync(new URL('../src/studio/scene3d/Scene3DStage.tsx', import.meta.url), 'utf8');
const section = (a, b) => source.slice(source.indexOf(a), source.indexOf(b, source.indexOf(a) + a.length));

test('线性深度材质按Three标准顶点顺序应用形变与蒙皮后计算深度', () => {
  const body = section('export function createReferenceMaterial(', 'function normalizeCameraEasing(');
  const vertex = body.slice(body.indexOf('vertexShader: `'), body.indexOf('fragmentShader: `'));
  const chunks = ['common', 'morphtarget_pars_vertex', 'skinning_pars_vertex', 'skinbase_vertex', 'morphinstance_vertex', 'begin_vertex', 'morphtarget_vertex', 'skinning_vertex', 'project_vertex'];
  let previous = -1;
  for (const chunk of chunks) {
    const index = vertex.indexOf(`#include <${chunk}>`);
    assert.ok(index > previous, `${chunk} must exist in deformation order`);
    previous = index;
  }
  const viewPosition = vertex.indexOf('vec4 viewPosition = mvPosition;');
  assert.ok(viewPosition > previous);
  assert.ok(vertex.indexOf('vViewDepth = -viewPosition.z;') > viewPosition);
  assert.doesNotMatch(vertex, /vec4\(position,\s*1\.0\)/);
});

test('两个捕获入口仅同步占用导出采样并在编码完成前finally恢复', () => {
  for (const body of [section('const captureReference =', 'const captureViewportAt ='), section('const captureViewportAt =', 'const exportFirstLastFrames =')]) {
    assert.match(body, /const sampleExport = exportSampleRef\.current;\s*exportSampleRef\.current = null;/);
    assert.match(body, /const restoreExportSample = sampleExport\?\.\(\)/);
    assert.match(body, /finally \{ restoreExportSample\?\.\(\); \}/);
    assert.match(body, /return new Promise<Blob \| null>/);
    assert.doesNotMatch(body, /await new Promise/);
  }
});

test('序列骨骼按逐帧绝对时间取样，停止绑定并同步恢复编辑变换', () => {
  const body = section('const exportSequence =', 'const saveReferenceToLibrary =');
  assert.match(body, /stopAnimation\(\)/);
  assert.match(body, /preview\.sampleAt\(Math\.min\(index \/ fps, moverSeconds\)\)/);
  assert.match(body, /return \(\) => \{\s*preview\?\.stop\(\)/);
  assert.match(body, /camera\.position\.copy\(savedPosition\)/);
  assert.match(body, /mover\.object\.quaternion\.copy\(savedMover\.quaternion\)/);
  assert.match(body, /finally \{\s*exportSampleRef\.current = null/);
});

test('首尾帧按相机端点时间取样角色与跟拍位移，失败也释放骨骼控制权', () => {
  const body = section('const exportFirstLastFrames =', 'const copyFirstLastNote =');
  assert.match(body, /edge === 'first' \? 0 : cameraPathDuration\(cameraKeys\)/);
  assert.match(body, /groundPathFollowDelta\(path, progress\)/);
  assert.match(body, /preview\.sampleAt\(Math\.min\(elapsed, seconds\)\)/);
  assert.match(body, /catch \(err\) \{ preview\?\.stop\(\)/);
  assert.match(body, /return \(\) => \{ preview\?\.stop\(\); mover\.object\.position\.copy\(p\)/);
  assert.match(body, /!canMoveEntry\(mover\) \|\| !objectsRef\.current\.includes\(mover\)/);
});
