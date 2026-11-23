import test from 'node:test';
import assert from 'node:assert/strict';
import { readFileSync } from 'node:fs';

const stage = readFileSync(new URL('../src/studio/scene3d/Scene3DStage.tsx', import.meta.url), 'utf8');
const camera = stage.slice(stage.indexOf('  const playCameraPath ='), stage.indexOf('  // 机位一改'));

test('相机跟拍骨骼采样复用移动动作选择及同一绝对时间', () => {
  assert.match(camera, /const clip = followMoverEntry\?\.object\.animations\[moveAnimationIndex\]/);
  assert.match(camera, /pathClock: true/);
  assert.match(camera, /const elapsed = \(performance\.now\(\) - started\) \/ 1000/);
  assert.match(camera, /const progress = Math\.min\(1, elapsed \/ followSeconds\)/);
  assert.match(camera, /preview\?\.sampleAt\(Math\.min\(elapsed, followSeconds\)\)/);
  assert.doesNotMatch(camera, /preview\?\.update\(/);
});

test('相机收尾幂等释放动作、还原角色且拒绝迟到回调', () => {
  assert.match(camera, /if \(finished\) return;[\s\S]*finished = true/);
  assert.match(camera, /preview\?\.stop\(\)/);
  assert.match(camera, /animationRef\.current = null/);
  assert.match(camera, /followMoverEntry\.object\.position\.copy\(savedMover\.position\)/);
  assert.match(camera, /followMoverEntry\.object\.rotation\.y = savedMover\.rotationY/);
  assert.match(camera, /cancelAnimationFrame\(cameraPathRef\.current\)/);
  assert.match(camera, /window\.clearTimeout\(cameraPathTimerRef\.current\)/);
  assert.match(camera, /epoch !== sceneEpochRef\.current/);
  assert.match(camera, /cancelCameraPreviewRef\.current = finish/);
});

test('统一停止及卸载清理相机预览，跟拍期间不能单独暂停骨骼', () => {
  const stop = stage.slice(stage.indexOf('  const stopAnimation ='), stage.indexOf('  const sceneEpochRef ='));
  assert.match(stop, /cancelCameraPreviewRef\.current\?\.\(\)/);
  assert.match(stage, /cancelAnimationFrame\(frame\);\s*cancelCameraPreviewRef\.current\?\.\(\)/);
  assert.match(stage, /disabled=\{animationState !== 'playing' \|\| playingMove \|\| playingPath\}/);
  assert.match(stage, /onClick=\{stopAnimation\}[^>]*>停止跟拍预览/);
});
