import test from 'node:test';
import assert from 'node:assert/strict';
import { build } from 'esbuild';
import { dirname, resolve } from 'node:path';
import { fileURLToPath } from 'node:url';

const frontend = resolve(dirname(fileURLToPath(import.meta.url)), '..');
// esbuild erases type-only edges before collecting the actual runtime dependency graph.
// A direct-import regex would miss re-export barrels and transitive dependency regressions.
for (const entry of ['scriptShotPlan', 'ShotEnhancementPanel', 'shotDirection', 'shotNavigation', 'scriptShotSampler']) {
  test(`镜头运行时边界：${entry}不引入布景引擎或目录构建器`, async () => {
    const result = await build({
      absWorkingDir: frontend,
      entryPoints: [`src/studio/scene3d/${entry}.${entry.endsWith('Panel') ? 'tsx' : 'ts'}`],
      bundle: true, write: false, metafile: true, packages: 'external', format: 'esm', logLevel: 'silent',
    });
    const inputs = Object.keys(result.metafile.inputs);
    const forbidden = inputs.filter(path => /\/(?:toonflowSets|toonflowAssets|setTemplates|assetCatalog|assetModelResolver|modelGeometry)\.[tj]sx?$/.test(path));
    assert.deepEqual(forbidden, [], `运行时越界：${forbidden.join(', ')}`);
    assert.ok(inputs.some(path => path.endsWith('/scriptShotPlan.ts')), '必须直接或间接消费独立镜头契约');
  });
}
