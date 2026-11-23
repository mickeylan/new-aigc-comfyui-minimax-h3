import * as THREE from 'three';
import { normalizePlaneSettings, type PlaneSettings } from './planeDocument';

/** Temporary reference material; owns no textures. Match planeMaterial's alpha/key mask. */
export function referencePlaneMaterial(base: THREE.Material, texture: THREE.Texture, input: Partial<PlaneSettings>) {
  const settings = normalizePlaneSettings(input);
  const material = base.clone();
  material.side = settings.doubleSided ? THREE.DoubleSide : THREE.FrontSide;
  material.transparent = true;
  if (texture.matrixAutoUpdate) texture.updateMatrix();
  material.onBeforeCompile = (shader) => {
    Object.assign(shader.uniforms, {
      referenceMap: { value: texture }, referenceUvMatrix: { value: texture.matrix.clone() },
      referenceOpacity: { value: settings.opacity }, referenceAlphaTest: { value: settings.alphaTest },
      referenceKeyEnabled: { value: settings.chromaKey }, referenceKey: { value: new THREE.Color(settings.keyColor) },
      referenceSimilarity: { value: settings.similarity }, referenceSmoothness: { value: settings.smoothness },
    });
    shader.vertexShader = 'varying vec2 vReferenceUv; uniform mat3 referenceUvMatrix;\n' + shader.vertexShader.replace('void main() {', 'void main() {\nvReferenceUv = (referenceUvMatrix * vec3(uv, 1.0)).xy;');
    shader.fragmentShader = `varying vec2 vReferenceUv;
uniform sampler2D referenceMap; uniform float referenceOpacity; uniform float referenceAlphaTest;
uniform bool referenceKeyEnabled; uniform vec3 referenceKey; uniform float referenceSimilarity; uniform float referenceSmoothness;
vec2 referenceUV(vec3 rgb) { return vec2(dot(rgb, vec3(-0.169,-0.331,0.5))+0.5, dot(rgb, vec3(0.5,-0.419,-0.081))+0.5); }
` + shader.fragmentShader.replace('void main() {', `void main() {
vec4 referencePixel = texture2D(referenceMap, vReferenceUv);
float referenceAlpha = referencePixel.a * referenceOpacity;
if (referenceKeyEnabled) {
  float mask = distance(referenceUV(referencePixel.rgb), referenceUV(referenceKey)) - referenceSimilarity;
  referenceAlpha *= pow(clamp(mask / referenceSmoothness, 0.0, 1.0), 1.5);
}
if (referenceAlpha <= 0.0 || referenceAlpha < referenceAlphaTest) discard;
`);
    const end = shader.fragmentShader.lastIndexOf('}');
    shader.fragmentShader = shader.fragmentShader.slice(0, end) + '\ngl_FragColor.a = referenceAlpha;\n' + shader.fragmentShader.slice(end);
  };
  material.customProgramCacheKey = () => `reference-alpha:${base.type}`;
  return material;
}
