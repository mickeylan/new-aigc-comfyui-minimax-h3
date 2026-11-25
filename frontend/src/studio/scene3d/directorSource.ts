// Read-only boundary from existing backend Scene/Shot results, not a novel analyser.
// Keep the entire JSON payload: semantic directions are not executable transforms.
import { SCRIPT_SHOT_LIMIT, SCRIPT_SHOT_MIN_SECONDS, SCRIPT_TIMELINE_MAX_SECONDS } from './scriptShotPlan';

export type DirectorSceneSource = Record<string, unknown> & {
  id: number; project_id: number; title: string; content: string; location: string;
};
export type DirectorShotSource = Record<string, unknown> & {
  id: number; scene_id: number; order: number; duration: number;
};
export type DirectorSourceSnapshot = {
  kind: 'existing-director-source'; version: 1;
  scene: DirectorSceneSource; shots: DirectorShotSource[];
  /** SHA-256 of canonical source JSON; never an approval or authenticity signature. */
  revision: string;
};

const positiveID = (value: unknown, label: string): number => {
  if (typeof value !== 'number' || !Number.isSafeInteger(value) || value <= 0) throw Error(`${label}无效`);
  return value;
};
const record = (value: unknown, label: string): Record<string, unknown> => {
  if (!value || typeof value !== 'object' || Array.isArray(value) || Object.getPrototypeOf(value) !== Object.prototype) throw Error(`${label}不是JSON对象`);
  return value as Record<string, unknown>;
};
// Sort object keys, never array order. Reject non-JSON values rather than losing them
// through JSON.stringify (undefined, NaN, sparse arrays, custom prototypes, cycles).
function canonical(value: unknown, ancestors = new Set<object>()): string {
  if (value === null || typeof value === 'string' || typeof value === 'boolean') return JSON.stringify(value);
  if (typeof value === 'number' && Number.isFinite(value)) return JSON.stringify(value);
  if (!value || typeof value !== 'object' || ancestors.has(value)) throw Error('导演来源不是有限JSON数据');
  ancestors.add(value);
  try {
    if (Array.isArray(value)) {
      if (Object.keys(value).length !== value.length) throw Error('导演来源含稀疏或扩展数组');
      return `[${Array.from(value, item => canonical(item, ancestors)).join(',')}]`;
    }
    const object = record(value, '导演来源');
    return `{${Object.keys(object).sort().map(key => `${JSON.stringify(key)}:${canonical(object[key], ancestors)}`).join(',')}}`;
  } finally { ancestors.delete(value); }
}

/** No fetch/model/DB/editor calls. The caller must explicitly select the project/scene.
 * Does not fabricate cameras, character bindings, dialogue timings or animation clips.
 */
export async function snapshotDirectorSource(projectID: number, sceneValue: unknown, shotsValue: unknown): Promise<DirectorSourceSnapshot> {
  positiveID(projectID, '项目ID');
  const scene = record(sceneValue, '场次');
  positiveID(scene.id, '场次ID');
  if (scene.project_id !== projectID) throw Error('场次不属于所选项目');
  if (scene.archived === true) throw Error('已归档场次不能作为当前导演来源');
  if (scene.director_source_stale !== false) throw Error('导演来源已过期或缺少有效状态，请先复核现有导演结果');
  for (const key of ['title', 'content', 'location']) if (typeof scene[key] !== 'string') throw Error(`场次缺少${key}`);
  if (!(scene.content as string).trim()) throw Error('场次缺少原文');
  if (!Array.isArray(shotsValue) || !shotsValue.length) throw Error('当前场次没有已有导演镜头');
  if (shotsValue.length > SCRIPT_SHOT_LIMIT) throw Error(`已有导演镜头超过${SCRIPT_SHOT_LIMIT}镜，请拆分场次`);
  const ids = new Set<number>();
  let total = 0;
  shotsValue.forEach((value, index) => {
    const shot = record(value, '导演镜头');
    const id = positiveID(shot.id, '镜头ID');
    if (ids.has(id)) throw Error('导演镜头ID重复');
    ids.add(id);
    if (shot.scene_id !== scene.id) throw Error('导演镜头跨场次');
    if (shot.order !== index + 1) throw Error('导演镜头顺序不连续，不能自动重排或重新拆镜');
    if (typeof shot.duration !== 'number' || !Number.isFinite(shot.duration) || shot.duration < SCRIPT_SHOT_MIN_SECONDS) throw Error('导演镜头时长无效');
    total += shot.duration;
    if (total > SCRIPT_TIMELINE_MAX_SECONDS) throw Error('导演镜头总时长超限');
    if (shot.action_timeline !== undefined && shot.action_timeline !== null) {
      if (!Array.isArray(shot.action_timeline)) throw Error('动作时间轴无效');
      let end = 0;
      for (const raw of shot.action_timeline) {
        const step = record(raw, '动作段');
        if (typeof step.start !== 'number' || typeof step.end !== 'number' || !Number.isFinite(step.start) || !Number.isFinite(step.end) || Math.abs(step.start - end) > 1e-6 || step.end <= step.start || step.end > shot.duration + 1e-6) throw Error('动作时间轴必须连续覆盖本镜');
        for (const key of ['subject', 'action', 'state', 'camera']) if (typeof step[key] !== 'string') throw Error(`动作段缺少${key}`);
        end = step.end;
      }
      if (shot.action_timeline.length && Math.abs(end - shot.duration) > 1e-6) throw Error('动作时间轴未覆盖本镜结束');
    }
  });
  // Clone synchronously before awaiting hashing: edits while hashing cannot enter the snapshot.
  const text = canonical({ scene, shots: shotsValue });
  if (new TextEncoder().encode(text).byteLength > 2 * 1024 * 1024) throw Error('导演来源超过2MiB，请拆分场次');
  const source = JSON.parse(text);
  const digest = await crypto.subtle.digest('SHA-256', new TextEncoder().encode(text));
  const revision = Array.from(new Uint8Array(digest), byte => byte.toString(16).padStart(2, '0')).join('');
  return { kind: 'existing-director-source', version: 1, ...source, revision };
}
