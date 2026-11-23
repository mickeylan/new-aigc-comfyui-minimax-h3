import { describe, it, expect, vi } from 'vitest';
import * as THREE from 'three';
import { referencePlaneMaterial } from './referenceAlpha';

describe('reference plane alpha', () => {
  for (const base of [new THREE.MeshStandardMaterial(), new THREE.MeshNormalMaterial(), new THREE.ShaderMaterial()]) {
    it(`preserves alpha/key mask for ${base.type} without owning source`, () => {
      const texture = new THREE.Texture();
      const disposed = vi.fn(); texture.addEventListener('dispose', disposed);
      const material = referencePlaneMaterial(base, texture, { opacity: 0.6, chromaKey: true, alphaTest: 0.05, doubleSided: false });
      const shader: any = { uniforms: {}, vertexShader: 'void main() { gl_Position = vec4(0.0); }', fragmentShader: 'void main() { gl_FragColor = vec4(1.0); }' };
      material.onBeforeCompile(shader, {} as any);
      expect(material).not.toBe(base);
      expect(material.side).toBe(THREE.FrontSide);
      expect(shader.uniforms.referenceMap.value).toBe(texture);
      expect(shader.uniforms.referenceOpacity.value).toBe(0.6);
      expect(shader.uniforms.referenceAlphaTest.value).toBe(0.05);
      expect(shader.uniforms.referenceKeyEnabled.value).toBe(true);
      expect(shader.vertexShader).toContain('referenceUvMatrix * vec3(uv, 1.0)');
      expect(shader.fragmentShader).toContain('referencePixel.a * referenceOpacity');
      expect(shader.fragmentShader).toContain('referenceAlpha < referenceAlphaTest) discard');
      expect(shader.fragmentShader).toContain('distance(referenceUV(referencePixel.rgb), referenceUV(referenceKey))');
      expect(shader.fragmentShader).toContain('gl_FragColor.a = referenceAlpha;');
      material.dispose(); expect(disposed).not.toHaveBeenCalled();
    });
  }
});
