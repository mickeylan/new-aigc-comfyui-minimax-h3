import * as THREE from 'three';
import { groundHeightAt, type SceneEnvironment } from './ground-path';

/** Bounded, stateless rain/snow. Explicit time is also used by offline video frames. */
export function createEnvironmentEffects(environment: SceneEnvironment) {
  const snow = environment.weather.condition === 'snow';
  const active = snow || environment.weather.condition === 'rain';
  const count = active ? Math.round(environment.weather.intensity * 1200) : 0;
  const geometry = new THREE.BufferGeometry();
  const vertices = new Float32Array(count * (snow ? 3 : 6));
  geometry.setAttribute('position', new THREE.BufferAttribute(vertices, 3));
  const material = snow
    ? new THREE.PointsMaterial({ color: '#f0f5ff', size: 0.085, transparent: true, opacity: 0.85, depthWrite: false })
    : new THREE.LineBasicMaterial({ color: '#b8d2ef', transparent: true, opacity: 0.55, depthWrite: false });
  const object = snow ? new THREE.Points(geometry, material as THREE.PointsMaterial) : new THREE.LineSegments(geometry, material as THREE.LineBasicMaterial);
  object.name = '__Weather__';
  object.frustumCulled = false;
  object.userData.environmentEffect = true;
  const random = (i: number) => { const x = Math.sin(i * 127.1 + 41317) * 43758.5453; return x - Math.floor(x); };
  const wrap = (x: number, period: number) => ((x % period) + period) % period;
  const extent = Math.min(100, environment.terrain?.size ?? 24);
  const sampleAt = (seconds: number) => {
    if (!Number.isFinite(seconds) || seconds < 0) throw Error('天气取样时间无效');
    for (let i = 0; i < count; i++) {
      const wind = environment.weather.wind;
      const x = wrap(random(i * 3 + 1) * extent + seconds * wind * (snow ? 1 : 3), extent) - extent / 2;
      const z = wrap(random(i * 3 + 2) * extent + (snow ? Math.sin(seconds + i) * 0.25 : 0), extent) - extent / 2;
      const floor = groundHeightAt(environment.terrain, x, z);
      const y = floor + wrap(random(i * 3 + 3) * 16 - seconds * (snow ? 1.1 : 11), 16) + 0.05;
      const index = i * (snow ? 3 : 6);
      vertices[index] = x; vertices[index + 1] = y; vertices[index + 2] = z;
      if (!snow) { vertices[index + 3] = x + wind * 0.12; vertices[index + 4] = y - 0.4; vertices[index + 5] = z; }
    }
    geometry.attributes.position.needsUpdate = true;
  };
  sampleAt(0);
  return { object, sampleAt, dispose() { object.removeFromParent(); geometry.dispose(); material.dispose(); } };
}
