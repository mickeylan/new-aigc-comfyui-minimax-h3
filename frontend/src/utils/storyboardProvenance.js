// Client-declared provenance only: a matching hash is not semantic approval or a
// claim that either artifact is current against an unopened Studio document.
export function provenanceObject(value) {
  if (typeof value === 'string') { try { value = JSON.parse(value) } catch { return null } }
  return value && typeof value === 'object' && !Array.isArray(value) ? value : null
}
export function storyboardParent(value, sceneId) {
  const source = provenanceObject(value)
  if (!source || source.version !== 1 || source.sceneId !== `director-scene:${Number(sceneId)}` || source.samplerVersion !== 'script-shot-timeline-v1') return null
  if (![source.revision, source.sceneRevision, source.planRevision].every(hash => typeof hash === 'string' && /^[a-f0-9]{64}$/.test(hash))) return null
  if (typeof source.sourceFingerprint !== 'string' || !source.sourceFingerprint || !['16:9', '9:16', '1:1', '3:2', '2:3'].includes(source.frameAspect)) return null
  return source
}
export function candidateStoryboardSource(candidate, projectId, sceneId) {
  const provenance = provenanceObject(candidate?.provenance_json)
  if (provenance?.source !== 'studio_location' || provenance.project_id !== Number(projectId) || provenance.scene_id !== Number(sceneId)) return null
  const source = provenanceObject(provenanceObject(provenance.camera)?.storyboard_source)
  if (source?.projectId !== Number(projectId) || source.sceneIdNumber !== Number(sceneId) || source.shotId !== provenance.shot_id || source.planShotId !== `director-shot:${source.shotId}` || !Number.isSafeInteger(source.shotId) || source.shotId < 1) return null
  return storyboardParent(source, sceneId)
}
export function materialStoryboardSource(material, projectId, sceneId) {
  if (!material || (material.project_id != null && Number(material.project_id) !== Number(projectId))) return null
  return storyboardParent(provenanceObject(material.derivation_manifest)?.storyboardSource, sceneId)
}
export function storyboardComparison(imageSource, videoSource) {
  if (!imageSource || !videoSource) return { status: 'unknown', message: '来源尚未核齐：手工/旧产物或未选择参考视频，不能判断同源。' }
  if (imageSource.revision !== videoSource.revision) return { status: 'different', message: '图与视频不是同一布景/镜头表版本，请回 Studio 核对并重新捕获、导出；不会静默替换。' }
  return { status: 'same', message: '图与视频同源版本一致；仅核对这两份产物，当前 Studio 编辑状态仍需复核。' }
}
