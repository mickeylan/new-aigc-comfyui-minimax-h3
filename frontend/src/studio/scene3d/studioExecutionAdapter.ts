import { z } from 'zod';
import { validateShotExecution, type ShotExecution } from './shotExecution';
import { snapshotDirectorSource, type DirectorSourceSnapshot } from './directorSource';
import { validateScriptShotPlan, type ScriptShotPlan } from './scriptShotPlan';
import type { StudioExecutionContext } from './studioExecutionContext';
import { adaptStudioExecutionDialogue, type StudioExecutionSpeechSource } from './studioExecutionDialogue';

const text = z.string().min(1).max(8000).refine(value => value.trim() !== '', '文本不能为空白');
const notes = z.array(text).max(64);
const id = z.string().min(1).max(160);
const finite = (min: number, max: number) => z.number().finite().min(min).max(max);
const vector = (min: number, max: number) => z.tuple([finite(min, max), finite(min, max), finite(min, max)]);
const objectSchema = z.object({
  id, asset_id: id, name: z.string().min(1).max(500), position: vector(-1000, 1000),
  rotation: vector(-2 * Math.PI, 2 * Math.PI), scale: vector(0.001, 1000),
}).strict();
const planSchema = z.object({
  source_hash: z.string().min(1).max(200), context_revision: z.string().min(1).max(200),
  objects: z.array(objectSchema).max(4096),
  bindings: z.array(z.object({ character: text, object_id: id }).strict()).max(128),
  shots: z.array(z.object({
    shot_id: z.number().int().positive(), duration: finite(0.2, 86400),
    camera: z.object({ position: vector(-1000, 1000), target: vector(-1000, 1000), fov: finite(15, 90) }).strict().nullable(),
    motion: z.object({ object_id: id, clip_id: id, start: vector(-1000, 1000), end: vector(-1000, 1000), duration: finite(0.1, 86400) }).strict().nullable(),
    execution: z.custom<ShotExecution>().optional(),
    evidence: notes.min(1), assumptions: notes, blockers: notes,
  }).strict()).min(1).max(64), assumptions: notes, blockers: notes,
}).strict();
export type StudioExecutionPlan = z.infer<typeof planSchema>;
export type StudioExecutionObject = z.infer<typeof objectSchema>;
export type PreparedStudioExecution = {
  source: DirectorSourceSnapshot;
  plan: StudioExecutionPlan;
  /** Every requested transform, including reuse. No live Object3D is mutated here. */
  objects: StudioExecutionObject[];
  blockers: string[];
  /** Null means fail closed: do not hand a partial/static substitute to playback. */
  shotPlan: ScriptShotPlan | null;
};
const names = (value: unknown) => typeof value === 'string' ? value.split(/[,，、;；\n]/).map(name => name.trim()).filter(Boolean) : [];
const nonempty = (value: unknown) => typeof value === 'string' && value.trim() !== '';

/** Application boundary, not semantic/visual approval. Caller must fetch authoritative
 * source/hash and recapture the real editor context immediately before committing.
 * No network, geometry allocation, live mutations, guessing dialogue timing or re-splitting.
 */
export async function prepareStudioExecution(
  value: unknown, reviewedSource: DirectorSourceSnapshot, sourceHash: string, current: StudioExecutionContext, speech?: StudioExecutionSpeechSource,
): Promise<PreparedStudioExecution> {
  // Parse/clone synchronously before hashing so caller edits during await cannot leak in.
  const plan = planSchema.parse(structuredClone(value));
  const context: StudioExecutionContext = structuredClone(current);
  const speechCopy = speech ? structuredClone(speech) : undefined;
  const reviewedRevision = reviewedSource.revision;
  if (plan.source_hash !== sourceHash || plan.context_revision !== context.revision) throw Error('执行草稿来源或资源修订已变化');
  const source = await snapshotDirectorSource(reviewedSource.scene.project_id, reviewedSource.scene, reviewedSource.shots);
  if (source.revision !== reviewedRevision) throw Error('导演来源快照已变化');
  const assets = new Map(context.assets.map(asset => [asset.id, asset]));
  const existing = new Map(context.objects.map(object => [object.id, object]));
  if (assets.size !== context.assets.length || existing.size !== context.objects.length) throw Error('真实目录身份重复');
  const objects = new Map([...existing].map(([key, object]) => [key, objectSchema.parse(object)]));
  const seen = new Set<string>();
  for (const object of plan.objects) {
    if (seen.has(object.id)) throw Error('执行草稿对象ID重复');
    seen.add(object.id);
    if (!assets.has(object.asset_id)) throw Error('执行草稿引用不存在的真实素材');
    if (existing.has(object.id)) {
      if (existing.get(object.id)!.asset_id !== object.asset_id) throw Error('不能替换已有对象素材');
    } else if (!object.asset_id.startsWith('catalog:')) throw Error('新增对象必须来自真实目录，不能克隆场景对象动作资产');
    objects.set(object.id, object);
  }
  const dialogue = adaptStudioExecutionDialogue(source, speechCopy);
  const blockers = [...plan.blockers, ...dialogue.blockers];
  const visible = new Set(names(source.scene.character_roles_set === true ? source.scene.visible_characters : source.scene.characters));
  const boundNames = new Set<string>(); const boundObjects = new Set<string>();
  for (const binding of plan.bindings) {
    if (!visible.has(binding.character) || !objects.has(binding.object_id) || boundNames.has(binding.character) || boundObjects.has(binding.object_id)) throw Error('可见角色绑定无效或重复');
    boundNames.add(binding.character); boundObjects.add(binding.object_id);
  }
  for (const name of visible) if (!boundNames.has(name)) blockers.push(`可见角色尚未绑定：${name}`);
  if (plan.shots.length !== source.shots.length) throw Error('执行草稿镜头覆盖不完整');
  let cursor = 0;
  const shots: ScriptShotPlan['shots'] = plan.shots.map((shot, index) => {
    const original = source.shots[index]!;
    if (shot.shot_id !== original.id || shot.duration !== original.duration) throw Error('执行草稿镜头ID、顺序或时长变化');
    const evidence = [source.scene.content, original.description, original.performance, original.blocking, original.camera_movement, original.focus_subject].filter(item => typeof item === 'string').join('\n');
    if (shot.evidence.some(quote => !evidence.includes(quote))) throw Error('执行依据不在原文或导演字段中');
    blockers.push(...shot.blockers.map(note => `镜${original.id}：${note}`));
    const execution = shot.execution === undefined ? undefined : validateShotExecution(shot.execution, shot.duration);
    if (execution?.actors && shot.motion) throw Error('镜内多角色执行不能混用旧走位');
    if (!shot.camera && !execution?.camera) blockers.push(`镜${original.id}：缺少机位`);
    else if (shot.camera && Math.hypot(...shot.camera.position.map((n, i) => n - shot.camera!.target[i]!)) < 0.001) throw Error('机位与焦点重合');
    if (nonempty(original.camera_movement) && !['固定', '固定镜头', 'static'].includes((original.camera_movement as string).trim())) {
      const keys = execution?.camera?.keys;
      if (!keys || keys.every(k => JSON.stringify([k.position, k.target, k.fov]) === JSON.stringify([keys[0].position, keys[0].target, keys[0].fov]))) blockers.push(`镜${original.id}：原运镜未绑定有效机位轨道`);
    }
    const timeline = Array.isArray(original.action_timeline) ? original.action_timeline : [];
    if (timeline.length > 1 && !execution?.actors) blockers.push(`镜${original.id}：多段动作时间轴尚不能完整执行`);
    if (execution?.actors) {
      const covered = new Set<string>();
      for (const actor of execution.actors) {
        const object = objects.get(actor.targetId), asset = object && assets.get(object.asset_id);
        const binding = plan.bindings.find(b => b.object_id === actor.targetId);
        if (!object || !binding || object.asset_id !== `object:${actor.targetId}` || actor.targetName !== object.name) throw Error('执行角色必须绑定实际可见对象');
        for (const beat of actor.beats) {
          const clip = asset?.clips[beat.animation.index];
          if (!clip || !clip.id.startsWith(`${beat.animation.index}:`) || clip.name !== beat.animation.name || clip.duration !== beat.animation.duration) throw Error('执行动作资源与当前对象不符');
          if (timeline.length) {
            const step = timeline[beat.sourceIndex ?? -1] as Record<string, unknown> | undefined;
            if (!step || step.start !== beat.start || step.end !== beat.end || !names(step.subject).includes(binding.character)) throw Error('执行段与原动作时间轴的区间或主体不符');
            covered.add(`${beat.sourceIndex}:${binding.character}`);
          } else if (beat.sourceIndex !== undefined) throw Error('原镜头无动作段，不能虚构来源索引');
        }
      }
      if (timeline.some((step, i) => names((step as Record<string, unknown>).subject).some(name => !covered.has(`${i}:${name}`)))) blockers.push(`镜${original.id}：动作时间轴未完整绑定`);
    }
    let motion: ScriptShotPlan['shots'][number]['motion'] = null;
    if (shot.motion) {
      const m = shot.motion; const object = objects.get(m.object_id);
      const asset = object && assets.get(object.asset_id);
      const clip = asset?.clips.find(item => item.id === m.clip_id);
      if (!object || !clip || m.duration > shot.duration || m.start[1] !== m.end[1] || Math.hypot(m.end[0] - m.start[0], m.end[2] - m.start[2]) < 0.001) throw Error('走位或所属动作资源无效');
      if (!boundObjects.has(m.object_id)) blockers.push(`镜${original.id}：表演对象未绑定可见角色`);
      // Runtime retains baseline Y without terrain, and ignores supplied world Y.
      if (m.start[1] !== object.position[1]) blockers.push(`镜${original.id}：走位高度不能由现有XZ采样器执行`);
      const indexText = m.clip_id.split(':')[0]!; const clipIndex = Number(indexText);
      if (object.asset_id !== `object:${object.id}` || !/^\d+$/.test(indexText) || !Number.isSafeInteger(clipIndex) || asset!.clips[clipIndex]?.id !== m.clip_id) throw Error('动作必须是当前对象实际挂载的原索引clip');
      if (Math.round(m.duration * 100) / 100 !== m.duration) blockers.push(`镜${original.id}：走位秒数精度超出现有采样器契约`);
      motion = { targetId: object.id, targetName: object.name, start: { x: m.start[0], z: m.start[2] }, end: { x: m.end[0], z: m.end[2] }, duration: m.duration, animation: { index: clipIndex, name: clip.name, duration: clip.duration } };
    } else if (!execution?.actors && (nonempty(original.performance) || timeline.length)) blockers.push(`镜${original.id}：原始表演未绑定，不得用静止画面替代`);
    const start = cursor; cursor += shot.duration;
    return { id: `director-shot:${original.id}`, order: original.order, start, end: cursor, camera: shot.camera, motion, ...(execution ? { execution } : {}), dialogue: dialogue.byShot.get(original.id) ?? [] };
  });
  const uniqueBlockers = [...new Set(blockers)];
  const shotPlan = uniqueBlockers.length ? null : validateScriptShotPlan({
    version: 1, sceneId: `director-scene:${source.scene.id}`, heading: source.scene.title, location: source.scene.location,
    sourceFingerprint: sourceHash, sceneFingerprint: source.revision,
    action: source.shots.map(shot => JSON.stringify({ shot_id: shot.id, performance: shot.performance ?? '', action_timeline: shot.action_timeline ?? [] })),
    shots, diagnostics: ['执行结构和资源引用校验不等于语义、碰撞或视觉验收。', JSON.stringify({ directorSource: source, executionPlan: plan, ...(dialogue.provenance ? { speechSource: dialogue.provenance } : {}) })],
  });
  return { source, plan, objects: plan.objects, blockers: uniqueBlockers, shotPlan };
}
