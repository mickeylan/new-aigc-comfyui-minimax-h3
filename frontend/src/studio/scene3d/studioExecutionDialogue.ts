import { z } from 'zod';
import type { DirectorSourceSnapshot } from './directorSource';
import type { ScriptTimedLine } from './scriptShotPlan';

const lineSchema = z.object({
  shot_id: z.number().int().positive(), dialogue_id: z.number().int().positive(),
  speaker: z.string().min(1).max(64), speech_type: z.enum(['dialogue', 'offscreen_dialogue', 'narration', 'monologue']),
  text: z.string().min(1).max(20000), start: z.number().finite().min(0), end: z.number().finite().max(86400),
}).strict();
const timelineSchema = z.object({ version: z.literal(1), lines: z.array(lineSchema).max(4096), blockers: z.array(z.string().min(1)).max(8192) }).strict();
const rowSchema = z.object({ id: z.number().int().positive(), scene_id: z.number().int().positive(), project_id: z.number().int().positive(), text: z.string(), character: z.string(), speech_type: z.string() }).passthrough();
export type StudioExecutionSpeechSource = { dialogues: unknown; dialogue_timeline: unknown };

/** Consume server-resolved exact timing; never estimate speech duration or use Shot's display cache.
 * Offscreen speech is a caption only, not a visible character/object binding or synthesized audio.
 */
export function adaptStudioExecutionDialogue(source: DirectorSourceSnapshot, speech?: StudioExecutionSpeechSource): {
  byShot: Map<number, ScriptTimedLine[]>; blockers: string[]; provenance: unknown;
} {
  const byShot = new Map<number, ScriptTimedLine[]>();
  if (!speech) return { byShot, blockers: source.shots.filter(shot => (typeof shot.dialogue === 'string' && shot.dialogue.trim()) || (Array.isArray(shot.dialogue_ranges) && shot.dialogue_ranges.length)).map(shot => `镜${shot.id}：尚未读取权威对白及时间映射，不能丢弃或猜测台词`), provenance: null };
  const timeline = timelineSchema.parse(speech.dialogue_timeline);
  const rows = z.array(rowSchema).max(4096).parse(speech.dialogues);
  const rowByID = new Map(rows.map(row => [row.id, row]));
  if (rowByID.size !== rows.length || rows.some(row => row.scene_id !== source.scene.id || row.project_id !== source.scene.project_id)) throw Error('权威对白身份重复或跨场次');
  const blockers = [...timeline.blockers];
  const seen = new Set<number>();
  let cursor = 0; let index = 0;
  for (const shot of source.shots) {
    const start = cursor; cursor += shot.duration;
    const ranges = Array.isArray(shot.dialogue_ranges) ? shot.dialogue_ranges as any[] : [];
    const lines = timeline.lines.filter(line => line.shot_id === shot.id);
    let previous = start;
    for (const line of lines) {
      const row = rowByID.get(line.dialogue_id);
      if (!row || line.text !== row.text || line.speaker !== row.character.trim() || line.speech_type !== row.speech_type || seen.has(line.dialogue_id)) throw Error('对白原文、说话人、类型或唯一引用不匹配');
      if (!ranges.some(range => range.dialogue_id === line.dialogue_id) || line.start < start || line.end <= line.start || line.end > cursor || line.start < previous) throw Error('对白引用或明确时间段无效');
      previous = line.end; seen.add(line.dialogue_id);
      byShot.set(shot.id, [...(byShot.get(shot.id) ?? []), { index: index++, speaker: line.speaker, line: line.text, start: line.start, end: line.end }]);
    }
    if ((typeof shot.dialogue === 'string' && shot.dialogue.trim() && !ranges.length) || ranges.some(range => !lines.some(line => line.dialogue_id === range.dialogue_id))) blockers.push(`镜${shot.id}：权威对白及时间映射不完整`);
  }
  if (timeline.lines.some(line => !source.shots.some(shot => shot.id === line.shot_id))) throw Error('对白时间轴引用未知镜头');
  for (const row of rows) if (row.text.trim() && !seen.has(row.id)) blockers.push(`对白${row.id}缺少精确镜头时序，不可静默丢弃`);
  return { byShot, blockers, provenance: { dialogues: rows, dialogue_timeline: timeline } };
}
