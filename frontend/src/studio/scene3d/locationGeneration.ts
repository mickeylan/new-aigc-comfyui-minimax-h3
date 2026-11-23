export type LocationTemplate = { code: string; name: string; max_refs: number; reference_syntax: string; template_sha256: string };
export type LocationContext = { project: string; scene: string; shot: string };
export type LocationSnapshot = { blob: Blob; camera: { position: number[]; target: number[]; fov: number } };
export type LocationDraft = LocationContext & { prompt: string; template?: LocationTemplate; snapshot?: LocationSnapshot; references: File[]; reviewed: boolean };
export function locationBlocker(draft: LocationDraft): string {
  if (!draft.project || !draft.scene) return "请明确选择项目和场次";
  if (!draft.template) return "没有可用的图片编辑模板";
  if (!draft.snapshot) return "请先捕获当前干净机位，预览确认";
  if (!draft.prompt.trim()) return "请填写人工提示词";
  if (draft.references.length + 1 > draft.template.max_refs) return `参考图超出模板容量（含取景图最多${draft.template.max_refs}张）`;
  if (!draft.reviewed) return "请审阅机位图、参考图顺序、模板和提示词";
  return "";
}
export function locationReviewKey(draft: Omit<LocationDraft, 'reviewed'>): string {
  return JSON.stringify([draft.project, draft.scene, draft.shot, draft.prompt, draft.template?.code, draft.template?.template_sha256, draft.snapshot?.camera,
    draft.references.map(f => [f.name, f.size, f.lastModified])]);
}
export async function locationJSON(url: string, options?: RequestInit): Promise<any> {
  const response = await fetch(url, options);
  const value = await response.json().catch(() => null);
  if (!response.ok) throw new Error(value?.error || `请求失败 HTTP ${response.status}`);
  return value;
}
export async function uploadLocationImage(blob: Blob, name: string) {
  const form = new FormData(); form.append("file", blob, name); form.append("type", "image");
  return locationJSON("/api/upload", { method: "POST", body: form }) as Promise<{task_id:string;name:string}>;
}
