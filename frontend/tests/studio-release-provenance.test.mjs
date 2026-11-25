import test from 'node:test';
import assert from 'node:assert/strict';
import { readFileSync } from 'node:fs';
import { createHash } from 'node:crypto';
const studio = new URL('../src/studio/', import.meta.url);
const read = path => readFileSync(new URL(path, studio), 'utf8');
const sha256 = bytes => createHash('sha256').update(bytes).digest('hex');

test('Toonflow核验快照逐文件锚定且不冒称历史迁移提交', () => {
  const manifest = JSON.parse(read('scene3d/toonflow/SOURCE_MANIFEST.json'));
  const v = manifest.verification;
  assert.equal(v?.kind, 'verification-snapshot-not-historical-migration-claim');
  assert.match(v.commit, /^[a-f0-9]{40}$/);
  assert.equal(v.historicalMigrationCommit, null);
  assert.equal(v.trackedWorkingTreeStatus, '');
  assert.deepEqual(v.records.map(r => r.file), manifest.files);
  for (const r of v.records) {
    assert.match(r.sourceSha256, /^[a-f0-9]{64}$/);
    assert.equal(r.sourceWorkingTreeSha256, r.sourceSha256, `源文件不属于锚定提交：${r.file}`);
    assert.equal(sha256(readFileSync(new URL('scene3d/toonflow/' + r.file, studio))), r.adaptedSha256, `适配文件哈希漂移：${r.file}`);
    if (r.comparison.startsWith('additional')) assert.ok(r.adaptationNote, `差异未说明：${r.file}`);
  }
  for (const path of ['LICENSE', 'packages/nodes/trumanMap/readme.md']) {
    assert.match(v.licenses.find(r => r.path === path)?.sha256 ?? '', /^[a-f0-9]{64}$/);
  }
});

test('分发声明包含Apache与MPL原许可全文及固定版本源码说明', () => {
  const notices = read('THIRD_PARTY_NOTICES.md');
  for (const name of ['class-variance-authority', 'mediabunny']) {
    const license = readFileSync(new URL(`../node_modules/${name}/LICENSE`, import.meta.url), 'utf8');
    assert.ok(notices.includes(license.trim()), `${name}许可正文不完整`);
  }
  const lock = JSON.parse(readFileSync(new URL('../package-lock.json', import.meta.url), 'utf8'));
  const media = lock.packages['node_modules/mediabunny'];
  assert.ok(notices.includes(media.resolved), '源码包下载地址必须固定到锁定版本');
  assert.ok(notices.includes(media.integrity), '源码包完整性必须对应lockfile');
  assert.match(notices, /package\/src\//);
  assert.match(notices, /Hugeicons[\s\S]*发布阻塞/);
});
