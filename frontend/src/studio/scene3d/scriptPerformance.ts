import { Euler, type Object3D } from 'three';
import { assertScriptShotMotionTargets, validateScriptShotPlan, SCRIPT_SHOT_LIMIT, type ScriptShotPlan, type ScriptShotMotionTarget, type ScriptShotMotionAnimation } from './scriptShotPlan';
import type { ExecutionBeat } from './shotExecution';
import type { ScriptCharacterBinding, ScriptScene } from './toonflowSets';

export type PerformanceTarget = ScriptShotMotionTarget & { position: { x: number; z: number }; yaw: number };
export type ScriptPerformanceContext = { revision: string; targets: PerformanceTarget[] };
export type PerformanceCast = { character: string; targetId: string; idle: ScriptShotMotionAnimation; talk: ScriptShotMotionAnimation; walk: ScriptShotMotionAnimation };
export type PerformanceSettings = { speed: number; approachDistance: number; departureDistance: number };
type Entry = { id: string; name: string; object: Object3D; locked?: boolean; splat?: unknown };
const LIMIT = 32;
const NARRATOR = /^(旁白|画外音|解说|OS|V\.?O\.?)$/i;

/** Synchronous editing snapshot: callers compare again immediately before application.
 * Camera is deliberately absent so recording a camera does not invalidate the cast.
 * Geometry/path safety is still checked separately by the production application gate. */
export function captureScriptPerformanceContext(entries: readonly Entry[]): ScriptPerformanceContext {
  if (new Set(entries.map(e => e.id)).size !== entries.length) throw Error('真实对象目录身份重复');
  for (const { object } of entries) {
    const parent = object.parent;
    if (parent && (!(parent as Object3D & { isScene?: boolean }).isScene || parent.parent || !parent.visible || parent.position.lengthSq() !== 0 || parent.quaternion.x !== 0 || parent.quaternion.y !== 0 || parent.quaternion.z !== 0 || parent.quaternion.w !== 1 || parent.scale.toArray().some(n => n !== 1))) throw Error('表演快照仅支持直接挂载在未变换场景下的对象，请先整理层级');
  }
  const captured = entries.map(e => ({ id: e.id, name: e.name, uuid: e.object.uuid,
    position: e.object.position.toArray(), quaternion: e.object.quaternion.toArray(), scale: e.object.scale.toArray(),
    visible: e.object.visible, locked: !!e.locked, splat: !!e.splat,
    clips: e.object.animations.map((c, index) => ({ index, name: c.name, duration: c.duration, uuid: c.uuid })),
  }));
  return { revision: JSON.stringify(captured), targets: captured.filter(e => !e.splat).map(e => {
    const object = entries.find(item => item.id === e.id)!.object;
    return { id: e.id, name: e.name, visible: e.visible, locked: e.locked, position: { x: e.position[0], z: e.position[2] },
      yaw: new Euler().setFromQuaternion(object.quaternion, 'YXZ').y,
      animations: e.clips.map(({ index, name, duration }) => ({ index, name, duration })) };
  }) };
}

/** Explicitly requested staging suggestion, never a source fact or name-based reuse.
 * The existing cast controls remain editable and require a separate insertion approval. */
export function suggestScriptCast(characters: readonly string[]): ScriptCharacterBinding[] {
  if (characters.length > LIMIT) throw Error('自动表演最多32个角色，请拆分场次');
  if (new Set(characters).size !== characters.length) throw Error('出场角色重复');
  const columns = Math.min(4, characters.length);
  return characters.map((character, i) => ({ character, target: { kind: 'placeholder', rig: 'skeletal',
    position: { x: (i % columns - (columns - 1) / 2) * 8, z: Math.floor(i / columns) * 8 } } }));
}

function point(p: { x: number; z: number }) {
  if (![p?.x, p?.z].every(n => Number.isFinite(n) && Math.abs(n) <= 1000)) throw Error('演员站位超出±1000米');
  return { x: p.x, z: p.z };
}

/** Repeat the explicitly chosen real clip, restarting at zero at each boundary.
 * No invented looping/blending, no frozen tail masquerading as continued walking. */
function beats(duration: number, from: { x: number; z: number }, to: { x: number; z: number }, yaw: number, animation: ScriptShotMotionAnimation): ExecutionBeat[] {
  const count = Math.ceil(duration / animation.duration);
  if (!Number.isSafeInteger(count) || count > 128) throw Error('单镜动作超过128段预算，请拆分或改选片段');
  const at = (t: number) => ({ x: from.x + (to.x - from.x) * t / duration, z: from.z + (to.z - from.z) * t / duration });
  return Array.from({ length: count }, (_, i) => {
    const start = i * animation.duration, end = Math.min(duration, (i + 1) * animation.duration);
    return { start, end, from: at(start), to: at(end), yaw, animation: { ...animation } };
  });
}

/** Offline, bounded, literal-cue compiler. It preserves the selected source order,
 * makes layout/timing assumptions explicit, and leaves all unhandled prose unresolved.
 * No source requirement is invented and no scene is mutated. Real geometry approval
 * belongs to assertScriptShotApplicationSafe, not this proposal helper. */
export function buildScriptPerformancePlan(base: ScriptShotPlan, scene: ScriptScene, source: string, cast: readonly PerformanceCast[], context: ScriptPerformanceContext, settings: PerformanceSettings): ScriptShotPlan {
  validateScriptShotPlan(base);
  if (source.length > 65536) throw Error('剧本表演原文超过65536字符，请拆分场次');
  if (base.shots.some(s => s.execution || s.motion)) throw Error('已有动作或执行轨道，不能自动覆盖；请重新生成基础镜头表');
  if (scene.id !== base.sceneId || scene.heading !== base.heading || JSON.stringify(scene.action) !== JSON.stringify(base.action) ||
    JSON.stringify(scene.dialogue) !== JSON.stringify(base.shots.flatMap(s => s.dialogue.map(l => ({ speaker: l.speaker, line: l.line }))))) throw Error('场次或原文与镜头表不一致');
  if (!cast.length || cast.length > LIMIT || cast.length !== scene.characters.length || new Set(cast.map(c => c.character)).size !== cast.length || new Set(cast.map(c => c.targetId)).size !== cast.length ||
    scene.characters.some(name => !cast.some(c => c.character === name))) throw Error('请为本场每个角色一一绑定真实演员（最多32人）');
  if (scene.characters.some(name => NARRATOR.test(name))) throw Error('旁白或画外音不能自动排成实体演员，请另行人工编排');
  if (new Set(context.targets.map(t => t.id)).size !== context.targets.length) throw Error('真实对象目录身份重复');
  if (![settings.speed, settings.approachDistance, settings.departureDistance].every(Number.isFinite) || settings.speed < .2 || settings.speed > 3 || settings.approachDistance < 1 || settings.approachDistance > 10 || settings.departureDistance < .2 || settings.departureDistance > 20) throw Error('请核对走位速度（0.2–3）、接近距离（1–10）与离开距离（0.2–20）');
  const actors = cast.map(c => {
    const target = context.targets.find(t => t.id === c.targetId);
    if (!target || !target.visible || target.locked) throw Error(`角色「${c.character}」的真实演员不可用`);
    const clips = Object.fromEntries((['idle', 'talk', 'walk'] as const).map(key => {
      const wanted = c[key], live = target.animations[wanted?.index];
      if (!live || live.index !== wanted.index || live.name !== wanted.name || live.duration !== wanted.duration || !Number.isFinite(live.duration) || live.duration <= 0) throw Error(`角色「${c.character}」的动作资源已变化，请重新选择`);
      return [key, { ...live }];
    })) as Pick<PerformanceCast, 'idle' | 'talk' | 'walk'>;
    if (!Number.isFinite(target.yaw)) throw Error('演员朝向无效');
    return { ...c, ...clips, targetName: target.name, position: point(target.position), yaw: target.yaw };
  });
  if (!scene.heading.trim()) throw Error('请先补显式场次标题，再编排表演');
  const lines = source.replace(/\r\n?/g, '\n').split('\n');
  if (!Number.isSafeInteger(scene.lineStart) || !Number.isSafeInteger(scene.lineEnd) || scene.lineStart < 1 || scene.lineEnd > lines.length || scene.lineEnd < scene.lineStart || lines[scene.lineStart - 1].trim() !== scene.heading) throw Error('场次原文行区间已变化');
  const result = structuredClone(base); result.shots = [];
  const events: { line: number; quote: string; kind: string; shotId?: string }[] = [];
  const unresolved: { line: number; quote: string; reason: string }[] = [];
  let dialogueIndex = 0, actionIndex = 0, cursor = 0;
  function append(duration: number, speaker?: string, template?: ScriptShotPlan['shots'][number], moving?: { id: string; to: { x: number; z: number }; yaw: number }) {
    if (result.shots.length >= SCRIPT_SHOT_LIMIT) throw Error('表演镜头超过64镜，请拆分场次（不会截断）');
    const start = cursor, end = Math.round((cursor + duration) * 100) / 100;
    const delta = template ? start - template.start : 0;
    result.shots.push({ id: `performance-${result.shots.length + 1}`, order: result.shots.length + 1, start, end,
      camera: template?.camera ? structuredClone(template.camera) : null,
      dialogue: template ? template.dialogue.map(l => ({ ...l, start: Math.round((l.start + delta) * 100) / 100, end: Math.round((l.end + delta) * 100) / 100 })) : [],
      execution: { version: 1, actors: actors.map(a => {
        const move = moving?.id === a.targetId ? moving : undefined;
        return { targetId: a.targetId, targetName: a.targetName, beats: beats(end - start, a.position, move?.to ?? a.position, move?.yaw ?? a.yaw, move ? a.walk : speaker === a.character ? a.talk : a.idle) };
      }) },
    });
    if (moving) { const a = actors.find(a => a.targetId === moving.id)!; a.position = point(moving.to); a.yaw = moving.yaw; }
    cursor = end;
  }
  for (let i = scene.lineStart; i < scene.lineEnd; i++) {
    const quote = lines[i], text = quote.trim(); if (!text) continue;
    const match = text.match(/^([^\s:：()（）]{1,24})\s*[（(][^）)]{0,60}[）)]\s*[:：]\s*(.*)$/) ?? text.match(/^([^\s:：()（）]{1,24})\s*[:：]\s*(.*)$/);
    const next = scene.dialogue[dialogueIndex];
    if (match && next && match[1] === next.speaker && match[2] === next.line) {
      const qualifier = text.match(/^[^\s:：()（）]{1,24}\s*[（(]([^）)]*)[）)]/)?.[1] ?? '';
      if (/旁白|画外|解说|\bOS\b|\bV\.?O\.?\b/i.test(qualifier)) throw Error('旁白或画外音不能自动排成实体演员，请另行人工编排');
      const original = base.shots[dialogueIndex++];
      append(original.end - original.start, next.speaker, original);
      events.push({ line: i + 1, quote, kind: 'dialogue', shotId: result.shots.at(-1)!.id }); continue;
    }
    if (text !== scene.action[actionIndex++]) throw Error('场次原文顺序或台词已变化，请重新解析');
    const cue = text.replace(/^[（(](.*)[）)]$/, '$1').replace(/[。.]$/, '');
    let move: { id: string; to: { x: number; z: number }; yaw: number } | undefined;
    let reason = '未支持或不明确的动作行，须人工编排；不能用通用动作冒充';
    // Exact whole-line equality, not substring matching: no pronouns, conditional clauses or quoted instructions.
    const candidates: { id: string; to: { x: number; z: number }; yaw: number }[] = [];
    let matches = 0;
    for (const a of actors) {
      for (const other of actors) if (other !== a && (cue === `${a.character}走到${other.character}面前` || cue === `${a.character}走近${other.character}`)) {
        matches++;
        const dx = other.position.x - a.position.x, dz = other.position.z - a.position.z, distance = Math.hypot(dx, dz);
        const front = cue === `${a.character}走到${other.character}面前`;
        if (!front && distance <= settings.approachDistance + .001) reason = '当前距离不大于人工指定的接近距离，未编造移动';
        else {
          const to = front
            ? { x: other.position.x + Math.sin(other.yaw) * settings.approachDistance, z: other.position.z + Math.cos(other.yaw) * settings.approachDistance }
            : { x: other.position.x - dx / distance * settings.approachDistance, z: other.position.z - dz / distance * settings.approachDistance };
          if (Math.hypot(to.x - a.position.x, to.z - a.position.z) <= .001) reason = '演员已在指定距离的落点，未编造移动';
          else candidates.push({ id: a.targetId, to, yaw: front ? Math.atan2(other.position.x - to.x, other.position.z - to.z) : Math.atan2(dx, dz) });
        }
      }
      if (cue === `${a.character}离开`) { matches++; candidates.push({ id: a.targetId, to: { x: a.position.x - Math.sin(a.yaw) * settings.departureDistance, z: a.position.z - Math.cos(a.yaw) * settings.departureDistance }, yaw: Math.atan2(-Math.sin(a.yaw), -Math.cos(a.yaw)) }); }
    }
    if (matches > 1) reason = '动作端点有多种角色解析，须人工编排';
    else move = candidates[0];
    if (move) {
      const actor = actors.find(a => a.targetId === move!.id)!;
      const duration = Math.max(.2, Math.ceil(Math.hypot(move.to.x - actor.position.x, move.to.z - actor.position.z) / settings.speed * 100) / 100);
      point(move.to); append(duration, undefined, undefined, move);
      events.push({ line: i + 1, quote, kind: 'move', shotId: result.shots.at(-1)!.id });
    } else { unresolved.push({ line: i + 1, quote, reason }); events.push({ line: i + 1, quote, kind: 'unresolved' }); }
  }
  if (dialogueIndex !== scene.dialogue.length || actionIndex !== scene.action.length) throw Error('场次原文覆盖不完整，请重新解析');
  const originalActionNote = `${base.action.length} 行动作只作画面说明，未参与自动计时，也不推断表演。`;
  result.diagnostics = result.diagnostics.map(text => text === originalActionNote ? `基础镜头表（编排前）的记录：${text} 本次表演草稿另为 ${events.filter(e => e.kind === 'move').length} 行明确走位单独计时，${unresolved.length} 行动作未覆盖。` : text);
  result.diagnostics.push('表演草稿仅覆盖明确对白与局部走位；站位、速度、接近/离开距离是人工布景推定，不是原文事实。每个片段重复时从零播放，无连续混合、口型、微表情或接触表演；新增动作镜头须人工记录机位。');
  result.diagnostics.push(JSON.stringify({ kind: 'script-performance-proposal', version: 1, coverage: 'literal-stage-cues-only',
    sourceFingerprint: base.sourceFingerprint, sceneFingerprint: base.sceneFingerprint, settings: { ...settings }, cast: actors.map(a => ({ character: a.character, targetId: a.targetId, idle: a.idle, talk: a.talk, walk: a.walk })), events, unresolved }));
  return assertScriptShotMotionTargets(validateScriptShotPlan(result), context.targets);
}
