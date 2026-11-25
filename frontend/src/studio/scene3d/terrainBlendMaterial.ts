import * as THREE from 'three';
import type { SceneTerrain } from './toonflow/sceneDocument';
import { terrainSurfaceOptions } from './toonflow/terrainSurfaces';

/** Legacy two-layer field, not the upstream modern surfacePaint atlas. */
export function createTerrainBlendMaterial(terrain: SceneTerrain): THREE.MeshStandardMaterial {
  const width = terrain.segments + 1, paletteWidth = terrainSurfaceOptions.length;
  const field = new Uint8Array(width * width * 4), palette = new Float32Array(paletteWidth * 2 * 4);
  for (let i = 0; i < terrain.surfaces.length; i++) {
    field.set([terrain.surfaces[i], terrain.surfaceBlend?.surfaces[i] ?? terrain.surfaces[i], Math.round((terrain.surfaceBlend?.weights[i] ?? 0) * 255), 255], i * 4);
  }
  for (const surface of terrainSurfaceOptions) {
    const color = new THREE.Color(terrain.surfaceColors?.[surface.id] ?? surface.color);
    palette.set([color.r, color.g, color.b, surface.roughness], surface.id * 4);
    palette.set([surface.metalness ?? 0, surface.emission ?? 0, surface.group === 'built' ? 1 : 0, 0], (paletteWidth + surface.id) * 4);
  }
  const fieldTexture = new THREE.DataTexture(field, width, width);
  const paletteTexture = new THREE.DataTexture(palette, paletteWidth, 2, THREE.RGBAFormat, THREE.FloatType);
  for (const texture of [fieldTexture, paletteTexture]) {
    texture.minFilter = texture.magFilter = THREE.NearestFilter;
    texture.generateMipmaps = false; texture.flipY = false; texture.colorSpace = THREE.NoColorSpace; texture.needsUpdate = true;
  }
  const material = new THREE.MeshStandardMaterial({ color: 0xffffff, roughness: 1, metalness: 1 });
  material.name = 'terrain-two-layer-v1';
  material.customProgramCacheKey = () => 'terrain-two-layer-v1';
  material.onBeforeCompile = shader => {
    Object.assign(shader.uniforms, {
      terrainField: { value: fieldTexture }, terrainPalette: { value: paletteTexture },
      terrainGrid: { value: new THREE.Vector2(terrain.size, terrain.segments) },
      terrainPaletteWidth: { value: paletteWidth }, terrainSeed: { value: terrain.seed },
    });
    shader.vertexShader = 'varying vec2 terrainXZ;\n' + shader.vertexShader.replace('#include <begin_vertex>', '#include <begin_vertex>\nterrainXZ = position.xz;');
    shader.fragmentShader = /* glsl */ `
      varying vec2 terrainXZ;
      uniform sampler2D terrainField, terrainPalette;
      uniform vec2 terrainGrid;
      uniform float terrainPaletteWidth, terrainSeed;
      void terrainLayer(float id, float weight, inout vec3 tint, inout float rough, inout float metal, inout vec3 glow) {
        vec4 style = texture2D(terrainPalette, vec2((id + 0.5) / terrainPaletteWidth, 0.25));
        vec4 physical = texture2D(terrainPalette, vec2((id + 0.5) / terrainPaletteWidth, 0.75));
        vec2 tile = floor(fract(terrainXZ / 2.0) * 32.0);
        float noise = fract(sin(dot(tile, vec2(127.1, 311.7)) + terrainSeed * 0.17 + id * 19.3) * 43758.5453);
        float joint = (mod(tile.y, 16.0) < 1.0 || mod(tile.x + (tile.y < 16.0 ? 0.0 : 8.0), 16.0) < 1.0) ? physical.b : 0.0;
        float grain = pow(mix((215.0 + noise * 40.0) / 255.0, 110.0 / 255.0, joint), 2.2);
        tint += style.rgb * grain * weight; rough += style.a * weight;
        metal += physical.r * weight; glow += style.rgb * physical.g * weight;
      }
      void terrainCorner(vec2 corner, float weight, inout vec3 tint, inout float rough, inout float metal, inout vec3 glow) {
        vec3 field = texture2D(terrainField, (corner + 0.5) / (terrainGrid.y + 1.0)).rgb;
        terrainLayer(floor(field.r * 255.0 + 0.5), weight * (1.0 - field.b), tint, rough, metal, glow);
        terrainLayer(floor(field.g * 255.0 + 0.5), weight * field.b, tint, rough, metal, glow);
      }
    ` + shader.fragmentShader;
    shader.fragmentShader = shader.fragmentShader.replace('#include <map_fragment>', /* glsl */ `
      #include <map_fragment>
      vec2 gridPosition = clamp((terrainXZ / terrainGrid.x + 0.5) * terrainGrid.y, vec2(0.0), vec2(terrainGrid.y));
      vec2 cell = min(floor(gridPosition), vec2(terrainGrid.y - 1.0));
      vec2 fraction = gridPosition - cell;
      fraction = fraction * fraction * (3.0 - 2.0 * fraction);
      vec3 terrainTint = vec3(0.0), terrainGlow = vec3(0.0);
      float terrainRough = 0.0, terrainMetal = 0.0;
      terrainCorner(cell, (1.0-fraction.x)*(1.0-fraction.y), terrainTint, terrainRough, terrainMetal, terrainGlow);
      terrainCorner(cell+vec2(1.0,0.0), fraction.x*(1.0-fraction.y), terrainTint, terrainRough, terrainMetal, terrainGlow);
      terrainCorner(cell+vec2(0.0,1.0), (1.0-fraction.x)*fraction.y, terrainTint, terrainRough, terrainMetal, terrainGlow);
      terrainCorner(cell+vec2(1.0,1.0), fraction.x*fraction.y, terrainTint, terrainRough, terrainMetal, terrainGlow);
      diffuseColor.rgb *= terrainTint;
    `).replace('#include <roughnessmap_fragment>', '#include <roughnessmap_fragment>\nroughnessFactor *= terrainRough;')
      .replace('#include <metalnessmap_fragment>', '#include <metalnessmap_fragment>\nmetalnessFactor *= terrainMetal;')
      .replace('#include <emissivemap_fragment>', '#include <emissivemap_fragment>\ntotalEmissiveRadiance += terrainGlow;');
  };
  material.addEventListener('dispose', () => { fieldTexture.dispose(); paletteTexture.dispose(); });
  return material;
}
