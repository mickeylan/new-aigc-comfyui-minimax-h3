# ADR-0034：观察证据→受控修订提案（无模型半程）

- 状态：确定性半程与面板已实现并验证；模型侧提示词已逐字获批并保存为 `docs/previs-revision-prompt-v1.md`，尚未接入任何调用路径
- 前置：ADR-0024 A1/A2（运行诊断、时序观察报告）、ADR-0024 C2b

## 背景

P0 的“视觉反馈→受控修订闭环”包含两半：模型侧（读观察报告并用人话给建议）与确定性侧（把建议变成可审阅、可拒绝、可回滚的具体改动）。模型侧需要新提示词，须先逐字获批；确定性侧完全不调用模型，可以现在落地，并且是模型侧的下游契约。

## 契约（确定性半程）

`buildRevisionProposals(plan, report)`：只读，不修改计划、不写盘、不调用模型。

- 只处理**取景水平对准**这一类问题：对报告中每张观察截图，按该帧时间重采样执行角色的根位置（`sampleExecutionActors`，无执行则为空），用该帧记录的机位投影到 NDC。
- 当某角色 `|ndc.x| > 0.95` 或 `|ndc.y| > 0.95` 时，产出一条提案：把该镜机位的 `target` 改为“该角色的 XZ + 原机位目标高度”，即只做水平对准，不改机位位置、不改焦距、不改高度。
- 每条提案含：`id`（镜+秒+角色，稳定可去重）、`shotId`、`frameFile`、`evidence`（秒、角色、NDC）、`previousTarget`、`target`、`predictedNdc`、`resolved`（对准后该角色是否进入 ±0.9）、`requiresHumanConfirmation: true`、`applied: false`、`note`（中文，说明只做水平对准；未解决时明说“仅对准不足，未自动改焦距或机位”）。
- 上限：单次提案 ≤ 64 条，按镜序与秒序稳定排序；超过即抛错而不是截断。角色已在画面内、无执行角色、无观察帧都返回空数组，不伪造提案。
- 不推测剧情、不读图像像素、不声称遮挡或表演正确；`visuallyApproved` 永远为 false。

`applyRevisionProposal(plan, proposal)`：显式应用一条提案。

- 必须匹配当前计划：镜存在、当前机位 `target` 与提案 `previousTarget` 完全一致、当前 `position/fov` 未变（提案不含它们，因此只校验 target）。
- 只改该镜 `camera.target`，返回重新校验过的计划；其它镜、执行、诊断一律不动。
- 不匹配即抛错（“提案已过期”），不猜测、不部分应用；失败不修改入参。
- 应用后**不会**自动重新观察，也不会写入任何审批状态；必须由人重新观察/确认。

## 不做

- 不实现模型调用、不写提示词到 Skill/渲染文件、不自动应用提案、不改 schema 版本、不改 UI 之外的行为。
- 不声称视觉通过；本契约只把“可计算的取景偏差”变成可拒绝的建议。

## 验证

单元用例覆盖：越界角色产出可解/不可解提案、画面内不产出、无执行/无帧返回空、上限与排序稳定、应用只改目标镜、过期提案拒绝、应用后计划仍通过校验、入参不变。
## 实际结果

- `buildRevisionProposals` 与 `applyRevisionProposal` 新增 6 项用例通过：越界角色产出只水平对准的可解提案（NDC 与对准后预测值都断言）、画面内/无执行/无帧/帧越镜/零宽截图均不产出、窄焦距下明说“仅对准不足”且不改焦距或机位、id 稳定并拒绝超过 64 条、应用只改目标镜 target 且保留 position/fov/执行并通过计划校验、过期提案/镜缺失/非法坐标/非法类型拒绝且不改入参。
- 首轮 2/6 失败均为我用例假设错误：以为“把目标移到 60 米外”会不可解，实际水平/垂直都仍在画面内（改为 15° 焦距 + 近距角色才真正不可解）；以为 65 条会返回 65 条，实际契约是超 64 条抛错（改为 64 条通过、65 条抛错）。
- 独立全量 `npm --prefix frontend exec -- vitest run --root frontend` exit0，**92 文件 1021/1021**（`.rscode/r4-final-vitest.log`）；`npm --prefix frontend run build` exit0（`.rscode/r4-final-build.log`）。
- 未接 UI、未调用模型、未写提示词文件、无 schema 变更；提案只是坐标建议，不构成画面通过（`visuallyApproved` 恒为 false），也不声称遮挡、表演或剧情正确。未提交推送。

## 面板与提示词（本轮补）

- `PrevisRevisionPanel`：在观察完成后列出确定性提案（证据 NDC、建议目标、对准后预测、是否足够），每行“应用（改机位目标）/忽略”，不自动应用、不读像素、不调用模型。应用调用 `applyRevisionProposal`，成功后清空本次报告并要求重新观察，状态文案明确“仍未视觉批准”。3 项组件用例通过（列表与显式应用、失败提示与空态、未解与忙碌禁用）。`Scene3DStage` 只在观察 `completed` 时保留报告，失败/取消不保留半份证据。
- 已获批提示词逐字保存于 `docs/previs-revision-prompt-v1.md`（标注获批时间、未接入、改动须重新逐字获批）；未写入任何 Skill、后端或渲染提示词文件，未新增调用路径。
- 全量 `npm --prefix frontend exec -- vitest run --root frontend` exit0，**93 文件 1024/1024**（`.rscode/r1-ui-final-vitest.log`）；`npm --prefix frontend run build` exit0（`.rscode/r1-ui-build.log`）。首次全量运行出现既有 flaky `DirectorSourcePanel.spec.tsx:73`（`undefined.click`）失败，单独重跑该项 15/15 通过、再跑全量通过；未修改该文件、未改断言、未声称已修复。
