// Versioned Toonflow set recipes. Geometry edits unsupported by the recipe use the existing GLB path.
import { MaterialLoader, Mesh, Object3D } from 'three';
import { createSceneDocument, type SceneObject } from './toonflow/sceneDocument';
import { parseSceneDocument } from './toonflow/sceneValidation';
import { materializeSetObject } from './toonflowSets';
import { disposeObject } from './toonflow/sceneResources';

export interface RecipeDefinition { version: 1; source: SceneObject; nodes: NodeState[] }
interface NodeState { p: number[]; q: number[]; s: number[]; visible: boolean; materials?: Record<string, any>[] }
export interface RecipeArchive { version: 1; definitions: RecipeDefinition[] }
const nodes = (root: Object3D) => { const result: Object3D[] = []; root.traverse(node => result.push(node)); return result; };
function geometryKey(root: Object3D) {
  return JSON.stringify(nodes(root).map(node => {
    const mesh = node as Mesh;
    const geometry = mesh.geometry;
    const instanced = node as any;
    return [node.type, node.children.length, geometry ? [geometry.index?.array, Object.entries(geometry.attributes).map(([name, attr]) => [name, attr.itemSize, attr.normalized, Array.from(attr.array)]), geometry.groups, geometry.drawRange] : null,
      instanced.isInstancedMesh ? [instanced.count, Array.from(instanced.instanceMatrix.array)] : null];
  }));
}
function snapshot(root: Object3D): NodeState[] {
  return nodes(root).map((node, index) => {
    const mesh = node as Mesh;
    const materials = mesh.material ? (Array.isArray(mesh.material) ? mesh.material : [mesh.material]).map(material => {
      if (Object.values(material).some((value: any) => value?.isTexture)) throw new Error('带贴图的编辑需保留GLB');
      const { uuid, metadata, ...json } = material.toJSON();
      return json;
    }) : undefined;
    return { p: index ? node.position.toArray() : [0, 0, 0], q: index ? node.quaternion.toArray() : [0, 0, 0, 1], s: index ? node.scale.toArray() : [1, 1, 1], visible: index ? node.visible : true, ...(materials ? { materials } : {}) };
  });
}
export function createRecipeArchive(entries: readonly { id: string; object: Object3D }[]) {
  const archive: RecipeArchive = { version: 1, definitions: [] };
  const references = new Map<string, number>();
  const definitions = new Map<string, number>();
  const cache = new Map<string, string>();
  for (const entry of entries) {
    const provenance = entry.object.userData.proceduralSource;
    if (provenance?.generator !== 'toonflow-set') continue;
    if (nodes(entry.object).some(node => (node as any).isSkinnedMesh || node.animations.length)) continue;
    if (provenance.version !== 1) throw new Error('不支持的布景配方版本');
    const source: SceneObject = JSON.parse(JSON.stringify(provenance.source));
    source.id = 'recipe'; source.name = source.asset.name;
    source.position = { x: 0, y: 0, z: 0 }; source.rotation = { x: 0, y: 0, z: 0 }; source.scale = { x: 1, y: 1, z: 1 };
    delete source.height; source.visible = true;
    const key = JSON.stringify(source);
    let expected = cache.get(key);
    if (!expected) {
      const baseline = materializeSetObject(source);
      try { expected = geometryKey(baseline); cache.set(key, expected); }
      finally { disposeObject(baseline); }
    }
    // Changed topology/geometry is never silently replaced by a regenerated model.
    if (geometryKey(entry.object) !== expected) continue;
    let state: NodeState[];
    try { state = snapshot(entry.object); } catch { continue; }
    const definition: RecipeDefinition = { version: 1, source, nodes: state };
    const signature = JSON.stringify(definition);
    let index = definitions.get(signature);
    if (index === undefined) { index = archive.definitions.length; definitions.set(signature, index); archive.definitions.push(definition); }
    references.set(entry.id, index);
  }
  return { archive, references };
}
export function validateRecipeArchive(value: unknown): asserts value is RecipeArchive {
  const archive = value as RecipeArchive;
  if (!archive || archive.version !== 1 || !Array.isArray(archive.definitions) || archive.definitions.length > 4096) throw new Error('不支持的程序化存档版本或定义数量');
  for (const definition of archive.definitions) {
    if (!definition || definition.version !== 1) throw new Error('不支持的布景配方版本');
    if (!definition.source?.asset || !Array.isArray(definition.nodes) || definition.nodes.length > 20000) throw new Error('布景配方结构无效');
    parseSceneDocument({ ...createSceneDocument(), objects: [definition.source] });
    if (definition.source.asset.url || definition.source.asset.path) throw new Error('布景配方不得读取外部模型');
    for (const node of definition.nodes) {
      if (!node || typeof node !== 'object') throw new Error('布景内容节点无效');
      for (const [field, length] of [['p', 3], ['q', 4], ['s', 3]] as const) if (!Array.isArray(node[field]) || node[field].length !== length || !node[field].every(Number.isFinite)) throw new Error('布景内容变换无效');
      if (typeof node.visible !== 'boolean' || (node.materials !== undefined && (!Array.isArray(node.materials) || node.materials.some(material => material?.type !== 'MeshStandardMaterial' || Object.keys(material).some(key => /map$/i.test(key)))))) throw new Error('布景材质或显隐无效');
    }
  }
}
export function restoreRecipeObject(archive: RecipeArchive, index: number) {
  if (!Number.isSafeInteger(index) || index < 0 || index >= archive.definitions.length) throw new Error('布景配方引用无效');
  const definition = archive.definitions[index];
  const root = materializeSetObject(definition.source);
  try {
    const content = nodes(root);
    if (content.length !== definition.nodes.length) throw new Error('布景配方内容版本不兼容');
    const loader = new MaterialLoader();
    content.forEach((node, i) => {
      const state = definition.nodes[i];
      node.position.fromArray(state.p); node.quaternion.fromArray(state.q); node.scale.fromArray(state.s); node.visible = state.visible;
      if (state.materials) {
        const mesh = node as Mesh;
        if (!mesh.isMesh) throw new Error('布景材质归属无效');
        const previous = Array.isArray(mesh.material) ? mesh.material : [mesh.material];
        if (!state.materials.length || state.materials.length !== previous.length) throw new Error('布景材质数量无效');
        const restored = state.materials.map(material => loader.parse(material));
        mesh.material = Array.isArray(mesh.material) ? restored : restored[0];
        previous.forEach(material => material.dispose());
      }
    });
    root.updateMatrixWorld(true);
    return root;
  } catch (error) { disposeObject(root); throw error; }
}
