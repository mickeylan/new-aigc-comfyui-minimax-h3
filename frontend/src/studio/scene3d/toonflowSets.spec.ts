import { describe, expect, it } from 'vitest';
import { Box3 } from 'three';
import {
  listSetTemplates, prepareSetDraft, materializeSetDraft,
  validateSetDraft, setDraftAssetReferences, setDraftReuseCounts, SET_DRAFT_MAX_OBJECTS, SET_DRAFT_VERSION,
} from './toonflowSets';
import { disposeObject } from './toonflow/sceneResources';

describe('Toonflow set drafts', () => {
  it('exposes exactly 64 distinct, bounded templates', () => {
    const rows = listSetTemplates();
    expect(rows).toHaveLength(64);
    expect(new Set(rows.map(row => row.id)).size).toBe(64);
    expect(rows.every(row => row.width > 0 && row.depth > 0)).toBe(true);
  });
  it('rejects unknown templates and honors cancellation', async () => {
    await expect(prepareSetDraft('missing', new AbortController().signal)).rejects.toThrow('找不到');
    const controller = new AbortController(); controller.abort();
    await expect(prepareSetDraft('ancientStreet', controller.signal)).rejects.toThrow();
  });
  it('builds and materializes all 64 templates without substituting missing assets', async () => {
    for (const row of listSetTemplates()) {
      const draft = await prepareSetDraft(row.id, new AbortController().signal);
      expect(draft.document.objects.length, row.id).toBeGreaterThan(0);
      const entries = materializeSetDraft(draft);
      try {
        expect(entries.length, row.id).toBe(draft.document.objects.length);
        expect(entries.every(entry => !new Box3().setFromObject(entry.object).isEmpty()), row.id).toBe(true);
      } finally { entries.forEach(entry => disposeObject(entry.object)); }
    }
  }, 120_000);
  for (const id of ['ancientStreet', 'inn', 'bedroom']) {
    it(`builds ${id} with stable editable object identities and real geometry`, async () => {
      const draft = await prepareSetDraft(id, new AbortController().signal);
      // Upstream bedroom explicitly places one complete interior + seated lead + standing lead.
      if (id === 'bedroom') expect(draft.document.objects).toHaveLength(3);
      else expect(draft.document.objects.length).toBeGreaterThan(5);
      expect(new Set(draft.document.objects.map(object => object.id)).size).toBe(draft.document.objects.length);
      const roundtrip = JSON.parse(JSON.stringify(draft));
      const objects = materializeSetDraft(roundtrip);
      try {
        expect(objects.map(object => object.id)).toEqual(draft.document.objects.map(object => object.id));
        for (const entry of objects) {
          const bounds = new Box3().setFromObject(entry.object);
          expect(bounds.isEmpty(), entry.name).toBe(false);
          expect(bounds.min.toArray().every(Number.isFinite), entry.name).toBe(true);
          expect(entry.object.userData.proceduralSource.version).toBe(1);
        }
        const version = draft.version;
        (draft as any).version = 99;
        expect(() => materializeSetDraft(draft)).toThrow('版本');
        draft.version = version;
      } finally { objects.forEach(entry => disposeObject(entry.object)); }
    }, 60_000);
  }
});

describe('Toonflow set draft contract', () => {
  const point = (value = 0) => ({ x: value, y: value, z: value });
  const instance = (id: string, overrides: Record<string, unknown> = {}) => ({
    id, name: id, position: point(0), rotation: point(0), scale: point(1), color: '#bac5d3', visible: true,
    asset: { id: 'a-box', name: '方块', category: '基础', kind: 'primitive', shape: 'box' },
    ...overrides,
  });
  const draft = (objects: unknown[], overrides: Record<string, unknown> = {}) => ({
    version: SET_DRAFT_VERSION, templateId: 'ancientStreet', name: '古街',
    document: { version: 1, objects, view: { position: point(9), target: point(0) }, gridVisible: false },
    ...overrides,
  });

  it('rejects drafts whose version, template or object list cannot be trusted', () => {
    expect(() => validateSetDraft(null)).toThrow('格式');
    expect(() => validateSetDraft(draft([instance('a')], { version: 99 }))).toThrow('版本');
    expect(() => validateSetDraft(draft([instance('a')], { templateId: 'missing' }))).toThrow('模板不存在');
    expect(() => validateSetDraft(draft([]))).toThrow('没有可编辑对象');
    expect(() => validateSetDraft({ version: 1, templateId: 'ancientStreet', name: '古街' })).toThrow('没有可编辑对象');
  });

  it('rejects duplicate stable IDs, non-finite transforms and assets without a rebuildable source', () => {
    expect(() => validateSetDraft(draft([instance('a'), instance('a')]))).toThrow('ID重复');
    expect(() => validateSetDraft(draft([instance('a', { position: { x: 0, y: Number.NaN, z: 0 } })]))).toThrow('有限数');
    expect(() => validateSetDraft(draft([instance('a', { scale: undefined })]))).toThrow('有限数');
    expect(() => validateSetDraft(draft([instance('a', { asset: { id: 'm', name: '模型', category: '本地', kind: 'model' } })]))).toThrow('可重建来源');
    expect(() => validateSetDraft(draft([instance('a', { asset: { id: 'p', name: '基础', category: '基础', kind: 'primitive' } })]))).toThrow('形状');
    expect(() => validateSetDraft(draft([instance('a', { asset: { id: 'x', name: 'x', category: '基础', kind: 'video' } })]))).toThrow('素材类型');
  });

  it('caps object count instead of truncating silently', () => {
    const objects = Array.from({ length: SET_DRAFT_MAX_OBJECTS + 1 }, (_, index) => instance(`o${index}`));
    expect(() => validateSetDraft(draft(objects))).toThrow(String(SET_DRAFT_MAX_OBJECTS));
  });

  it('accepts a real template draft and reports real asset references and instance counts', async () => {
    const real = await prepareSetDraft('bedroom', new AbortController().signal);
    expect(validateSetDraft(JSON.parse(JSON.stringify(real))).document.objects).toHaveLength(real.document.objects.length);
    const references = setDraftAssetReferences(real);
    const counts = setDraftReuseCounts(real);
    expect(references.length).toBeGreaterThan(0);
    expect(counts.instances).toBe(real.document.objects.length);
    expect(counts.assets).toBeGreaterThan(0);
    // 实例数不等于模型数：逐素材实例数之和必须等于真实实例总数。
    expect(counts.perAsset.reduce((sum, row) => sum + row.instances, 0)).toBe(counts.instances);
    expect(counts.perAsset.map(row => row.assetId).sort()).toEqual([...new Set(references.map(row => row.assetId))].sort());
  }, 60_000);

  it('refuses a corrupted draft before building any geometry', () => {
    const corrupted = draft([instance('a'), instance('a')]);
    expect(() => materializeSetDraft(corrupted as any)).toThrow('ID重复');
  });
});
