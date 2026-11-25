import * as THREE from 'three';
import type { SceneTerrain } from './toonflow/sceneDocument';
import { terrainSurfaceOptions } from './toonflow/terrainSurfaces';
import { createTerrainBlendMaterial } from './terrainBlendMaterial';
import { createTerrainMaterial } from './toonflow/terrainMaterial';

/** Small deterministic procedural textures: no network assets, canvas, or hidden random state. */
export function terrainSurfaceTexture(id: number, seed: number): THREE.DataTexture {
  const side = 32, data = new Uint8Array(side * side * 4);
  const built = terrainSurfaceOptions[id]?.group === 'built';
  for (let z = 0; z < side; z++) for (let x = 0; x < side; x++) {
    const noise = Math.sin(x * 127.1 + z * 311.7 + seed * 0.17 + id * 19.3) * 43758.5453;
    const grout = built && (z % 16 === 0 || (x + (z < 16 ? 0 : 8)) % 16 === 0);
    const value = grout ? 110 : Math.round(215 + (noise - Math.floor(noise)) * 40);
    const i = (z * side + x) * 4;
    data[i] = data[i + 1] = data[i + 2] = value; data[i + 3] = 255;
  }
  const texture = new THREE.DataTexture(data, side, side);
  texture.name = `terrain-procedural-v1-${id}-${seed}`;
  texture.wrapS = texture.wrapT = THREE.RepeatWrapping;
  texture.magFilter = THREE.LinearFilter; texture.minFilter = THREE.LinearMipmapLinearFilter;
  texture.generateMipmaps = true; texture.colorSpace = THREE.SRGBColorSpace; texture.needsUpdate = true;
  return texture;
}

/** Same a/b/d, b/c/d topology as the authoritative terrain height sampler. */
export function createEnvironmentTerrainMesh(terrain: SceneTerrain): THREE.Mesh {
  const width = terrain.segments + 1, count = width * width, cell = terrain.size / terrain.segments;
  const positions = new Float32Array(count * 3), uv = new Float32Array(count * 2);
  const waterPositions = new Float32Array(count * 3);
  for (let row = 0; row < width; row++) for (let col = 0; col < width; col++) {
    const i = row * width + col, x = col * cell - terrain.size / 2, z = row * cell - terrain.size / 2;
    positions.set([x, terrain.heights[i], z], i * 3);
    waterPositions.set([x, terrain.waterLevels?.[i] ?? terrain.heights[i], z], i * 3);
    uv.set([x / 2, z / 2], i * 2); // two metres per tile, independent of world resolution
  }
  const buckets = new Map<number, number[]>(), waterIndices: number[] = [];
  const triangle = (ids: number[]) => {
    const surfaces = ids.map(i => terrain.surfaces[i]);
    const surface = terrain.surfacePaint || terrain.surfaceBlend ? 0 : (surfaces[1] === surfaces[2] ? surfaces[1] : surfaces[0]);
    if (!buckets.has(surface)) buckets.set(surface, []);
    buckets.get(surface)!.push(...ids);
    if (ids.every(i => typeof terrain.waterLevels?.[i] === 'number')) waterIndices.push(...ids);
  };
  for (let row = 0; row < terrain.segments; row++) for (let col = 0; col < terrain.segments; col++) {
    const a = row * width + col, b = a + width, d = a + 1, c = b + 1;
    triangle([a, b, d]); triangle([b, c, d]);
  }
  const geometry = new THREE.BufferGeometry();
  geometry.setAttribute('position', new THREE.BufferAttribute(positions, 3));
  geometry.setAttribute('uv', new THREE.BufferAttribute(uv, 2));
  const indices: number[] = [], materials: THREE.MeshStandardMaterial[] = [];
  const modern = terrain.surfacePaint ? createTerrainMaterial(terrain) : undefined;
  for (const [surfaceId, triangles] of buckets) {
    const surface = terrainSurfaceOptions[surfaceId];
    const color = terrain.surfaceColors?.[surfaceId] ?? surface.color;
    const material = modern?.material ?? (terrain.surfaceBlend ? createTerrainBlendMaterial(terrain) : new THREE.MeshStandardMaterial({
      color, map: terrainSurfaceTexture(surfaceId, terrain.seed),
      roughness: surface.roughness, metalness: surface.metalness ?? 0,
      emissive: surface.emission ? color : 0x000000, emissiveIntensity: surface.emission ?? 0,
    }));
    if (!terrain.surfacePaint && !terrain.surfaceBlend) material.name = `terrain-surface-${surfaceId}`;
    geometry.addGroup(indices.length, triangles.length, materials.length);
    // Avoid spread/apply argument limits on 256-segment worlds.
    triangles.forEach(index => indices.push(index)); materials.push(material);
  }
  geometry.setIndex(indices); geometry.computeVertexNormals();
  const mesh = new THREE.Mesh(geometry, materials);
  mesh.name = '__Terrain__'; mesh.userData.terrain = true; mesh.userData.sourceTerrain = terrain;
  mesh.receiveShadow = true;
  if (modern) { mesh.userData.terrainMaterial = modern; modern.material.name = 'terrain-modern-procedural'; }
  if (waterIndices.length) {
    const waterGeometry = new THREE.BufferGeometry();
    waterGeometry.setAttribute('position', new THREE.BufferAttribute(waterPositions, 3));
    waterGeometry.setIndex(waterIndices); waterGeometry.computeVertexNormals();
    const colored = terrain.waterColors && Object.keys(terrain.waterColors).length > 0;
    if (colored) {
      const colors = new Float32Array(count * 3), color = new THREE.Color();
      for (let i = 0; i < count; i++) { color.set(terrain.waterColors?.[i] ?? '#60abbc'); color.toArray(colors, i * 3); }
      waterGeometry.setAttribute('color', new THREE.BufferAttribute(colors, 3));
    }
    const water = new THREE.Mesh(waterGeometry, new THREE.MeshStandardMaterial({ color: colored ? '#ffffff' : '#60abbc', vertexColors: !!colored, roughness: 0.22, metalness: 0.15, transparent: true, opacity: 0.8, depthWrite: false }));
    water.name = '__TerrainWater__'; water.userData.terrainWater = true;
    mesh.add(water);
  }
  return mesh;
}

export function disposeEnvironmentTerrainMesh(mesh: THREE.Mesh): void {
  const modern = mesh.userData.terrainMaterial as ReturnType<typeof createTerrainMaterial> | undefined;
  modern?.dispose();
  mesh.traverse(object => {
    if (!(object as THREE.Mesh).isMesh) return;
    const part = object as THREE.Mesh;
    part.geometry.dispose();
    const materials = Array.isArray(part.material) ? part.material : [part.material];
    materials.forEach(material => { if (material === modern?.material) return; (material as THREE.MeshStandardMaterial).map?.dispose(); material.dispose(); });
  });
}
