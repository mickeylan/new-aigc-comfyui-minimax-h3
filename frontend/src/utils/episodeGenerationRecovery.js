// Recover only the proven consumed-source conflict, not arbitrary generation errors.
// Review and CAS application remain explicit; the episode request is retried once.
export async function generateWithEpisodeRecovery({ generate, reflow, episode, confirm }) {
  try {
    return await generate()
  } catch (error) {
    const message = error?.response?.data?.error || ''
    if (!message.includes('映射与前集实际剧情冲突') || !message.includes('来源对白已在前集全部使用')) throw error
    const { data: preview } = await reflow({ start_episode: Number(episode) })
    if (!preview?.token || preview.start_episode !== Number(episode) || !preview.items?.length) throw error
    const lines = preview.items.map(item => `第${item.episode_n}集：${item.previous_title} → ${item.title}\n${item.adaptation_goal}\n开场：${item.mapping?.opening_state || ''}\n结束：${item.mapping?.ending_state || ''}`)
    const accepted = await confirm(`检测到旧规划重复使用前集剧情。\n\n保留第1–${preview.start_episode - 1}集，自动调整未生产规划：\n${lines.join('\n\n')}\n\n第${preview.unplanned_episode}集留作待规划。系统先备份；旧剧本保留至新剧本生成成功，已有媒体不允许调整。\n\n确认应用以上方案，并继续生成第${episode}集？`)
    if (!accepted) throw error
    await reflow({ start_episode: preview.start_episode, token: preview.token, apply: true })
    return await generate()
  }
}
