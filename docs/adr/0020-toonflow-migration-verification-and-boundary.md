# ADR-0020：Toonflow 前五看点迁移的核对结果、集成边界与阶段门槛

状态：**迁移决策已记录，分阶段实施中；不代表五项能力已交付**。用户授权迁移前五项，并选择先记录 ADR。
第 7 节为在该授权范围内选用的最小侵入技术方案，不另行冻结或删除既有编辑器。未提交、未推送、未部署，未触碰 18000，未真实生成，
未修改业务数据，源仓库只读。

目标（用户给定）：把 `J:/mickeylan/ai/Toonflow-app` README 前五看点对应能力迁移到本仓库 ——
剧本一键可编辑 3D 预演、大规模古镇组合布景、64 套布景与程序化素材库、天气时间地形编辑、
逐镜参考包与视频导出并接入既有生成流程。

本 ADR 只做三件事：记录**已核对**的现有实现、记录**未能核对**的部分（含原因）、
给出阶段门槛与不变量。不复述功能设想，不把设想当已完成。

---

## 1. 源仓库与许可核对

### 2026-11-25 来源锚点与发布许可补齐契约

用户授权补齐可确认项并单独记录阻塞；本机实际源目录为 `E:/mickeylan/ai/Toonflow-app`，旧 `J:/` 记录保留为历史上下文。源目录只读，不以当前HEAD冒充历史迁移提交。

- 在来源清单登记本轮核验的源HEAD、源工作区状态、列举文件及许可原件SHA-256，与本地适配文件对应；明确这些是本轮对照锚点。仅逐文件相同或可解释适配得到的证据可用于确认来源，不能反推未知迁移时间。源工作区有差异时原样登记而非标为提交内容。
- Apache-2.0依赖补确切版本、版权及本地包内完整许可；检查包内NOTICE，不能凭空创建上游归属声明。
- MPL依赖提供确切版本的可验证下载入口、包完整性、包内源码/许可位置及全文；不将浮动仓库首页当对应源码交付。生产构建继续携带唯一声明源，不另建易漂移副本。
- Hugeicons当前包条款含商业允许与分发限制；授权范围未证明前保留发布阻塞，不擅自替换图标或宣称已经合法/违法。最终Go嵌入产物缺失声明也是独立发布阻塞，前端构建通过不等于已发布二进制通过。
- 逐文件验证并补来源哈希、许可全文与构建分发测试；不改提示词、用户模板、依赖版本或业务数据，不提交推送。本项为工程合规证据，不是法律意见或发布批准。

### 第四项执行记录（2026-11-25）

- 主代理获本轮只读授权后核验本机源库成功：HEAD=`eecf51547af47eb504822186bd3f2dee369125ff`，已跟踪文件status为空；不是先前审计执行节点的访问拒绝所能代替的结果。Git工具对Windows盘符路径解析异常，改用显式 `git -C E:/mickeylan/ai/Toonflow-app` 只读命令确认，未改源库。
- `SOURCE_MANIFEST.json` 新增本轮核验快照：1129份源提交原始字节SHA-256、源工作树SHA-256和本地适配SHA-256，以及根LICENSE、插件readme哈希。1129份源工作树内容均与该提交一致；1117份按明确列出的版权头/换行/主机导入变换相等，另外12份逐项差异已检查并登记。12份包括动作目录绑定扩展、几何池所有权释放、显式.ts导入、type-only zod替换和8份空白变化。不触碰任何源提示词正文。历史迁移提交仍为null，不用本轮HEAD冒充。
- Apache：`class-variance-authority@0.7.1` 原包LICENSE全文（含Copyright 2022 Joe Bell）已纳入唯一分发声明，安装包未发现单独NOTICE。MPL：`mediabunny@1.29.1` 全文、固定npm源码包URL与lockfile SRI已补齐；实际下载校验信号 `MPL_SOURCE_DOWNLOAD=PASS`，58份package/src源码与LICENSE均同安装包逐字相等。源码包仅放 `.rscode/`，不提交；发布仍应复核可获取性。
- 新增两项回归先exit1：缺核验快照、缺Apache全文；补齐后与原声明测试共7/7通过。`npm --prefix frontend test` exit0：Node670/670、Vitest41文件343/343；`npm --prefix frontend run build` exit0：4510模块、29.67秒。前端dist声明与唯一源逐字相等：41083字节，SHA-256=`01fa0a5bc4e9a289cfdebe8b6c6b27c502a77594880642b116b90ba89cbac866`。
- **仍阻塞发布**：Hugeicons渲染包当前分发方式的授权证据不足；本地 `backend/internal/static/dist/THIRD_PARTY_NOTICES.md` 缺失（`GO_EMBEDDED_NOTICE=MISSING_RELEASE_BLOCKED`），没有把前端构建冒充Go二进制/镜像验收，也未覆盖构建旧产物；历史迁移提交无法从本轮对照证明。未擅自替换依赖、同步嵌入目录、提交推送、触发生成或宣称完整法律合规。
- 原生audit流程的设计审查分支遇到memory review锁而失败，另两支给出报告；用户问答已授权补可确认项并保留阻塞。工作流仍停在remediation_decision，当前工具没有接收该决定的字段，因此不宣称workflow完成。临时核验脚本、差异、源码包和日志均在 `.rscode/`，不提交。

### 初次迁移核对记录

- 源路径：`J:/mickeylan/ai/Toonflow-app`（初次记录；本机现为上述E盘路径）。
- 主代理已实际读取源 `README.md:254–310`、根 `LICENSE` 及 `packages/nodes/trumanMap/readme.md`。
  根许可为 MIT，Copyright (c) 2026 HBAI-Ltd；迁入代码保留版权及完整许可。
- 插件声明本地素材为原创程序化几何；Holodeck 派生内容为 MIT、其 examples 为 CC0；Quaternius 动作为 CC0；
  fzstd 为 MIT、SunCalc 为 BSD-2-Clause。引入具体内容时逐项保留原声明，不能把根 MIT 扩大到所有依赖。
- Kiln 是独立建模引擎，首次联网下载，不随插件打包。本轮不执行下载或真实 AI 建模；内置布景不能依赖其可用性。
- 子执行节点曾无法读取外部路径，这是该节点的权限限制，不是主代理不可读取或许可未核实的证据。
- 硬门槛：任何来自 Toonflow 的代码、提示词文本、素材或布景数据进入本仓库前，必须先取得可读访问，
  并按本仓库既有做法逐项登记（格式见 `THIRD_PARTY_NOTICES.md`；盘点做法见
  `docs/ArtCraft迁移-ComfyUI工作流与模型盘点.md` §许可盘点，该文明确“是工程盘点，不是法律意见”）。
  未登记不得进入，也不得以“概念参考”规避。
- 同时遵守用户给定的两条口径：**不把素材衍生数量当独立模型数量**；
  **不假定演示古镇 1500 件场景已提供**（该场景是否随源仓库提供同样未核实）。

## 2. 迁移前基线核对（已核对，可复查；新增实现进展见第 9 节）

| 能力 | 现状 | 代码位置 |
|---|---|---|
| 两套并行 3D 面 | Vue 预演（Scene 级，嵌项目详情页）与 React/ArtCraft 派生 Studio 岛（`/studio/scene`）同时存在，各自有文档与存档 | `frontend/src/views/ProjectDetail.vue:859,1389`；`frontend/src/router/index.js:18`；`frontend/src/views/StudioScene.vue:13`；`frontend/src/studio/host/mount-scene3d.tsx:13-16` |
| Vue 预演文档 | schema v1–v3；≤32 对象、≤12 机位、1–120 秒；请求上限 128KiB；带 revision 与正文哈希 CAS；LLM 只能出计划 | `frontend/src/utils/scenePrevisualization.js:37,47`；`backend/internal/service/previs_agent.go:23-45`；`docs/adr/0007` |
| Studio 场景文档 | **写 v6，读 v1–v6**（v6 校验对象稳定 ID 全覆盖） | `frontend/src/studio/scene3d/Scene3DStage.tsx:2859,3008,3035` |
| Studio 存档 | 单份 **32MiB**；GLB 以 base64 内嵌进 JSON；JSON 落 `data/scene-archives/<id>.json`，库里只存元数据；超限显式报错不截断 | `frontend/src/studio/scene3d/scene-archive.ts:7`；`backend/internal/service/scene_archive_handlers.go:18-20,65`；`backend/internal/api/router.go:226-231` |
| 素材库 | 仅 `glb/gltf/obj/splat` + 图片；ADR-0017 禁止伪造上游语义素材行 | `frontend/src/studio/scene3d/assetLibrary.ts:48`；`docs/adr/0017` §4 |
| 天气 | 只在 Vue 预演：preset/intensity/seed/sun_elevation/fog_density，仅影响渲染，不进提示词。**React Studio 内无天气**（`weather|rain|snow|fog` 在 `frontend/src/studio/scene3d` 无真实命中） | `frontend/src/utils/previsEnvironment.js:1,5`；`docs/adr/0008` |
| 地形 | **不存在**。全 `frontend/src` 检索 `terrain|heightfield|地形` 无命中 | — |
| 生成接入窄口 | 单张干净机位 + 人工提示词 + 有序参考（≤模板 `max_refs`）+ 人工审阅 + 场次候选；不自动采纳 | `frontend/src/studio/scene3d/locationGeneration.ts:5-13`；`backend/internal/api/router.go:203-206`；`docs/adr/0016` |
| 逐镜绑定 | **只有单帧**：不可变历史，记 previs revision、时间、帧哈希、文档 JSON | `backend/internal/models/shot_previs_frame.go:5-20`；`backend/internal/api/router.go:149-152` |
| 视频导出 | 浏览器本地 WebM（VP9/VP8，1280×720，无音轨，≤2880 帧/256MiB），**不上传、不进入生成链** | `docs/adr/0008,0010` |

文档漂移（本轮已修正）：ADR-0017 原记“场景写 v5”，代码实际写 **v6**、读 v1–v6，现已在 ADR-0017 原地更正并注明依据；
`README.md` 的 ADR 表原先只列到 0010，现补齐 0011–0020。能力版本仍应以代码与目录实况为准，而不是表格文字。

## 3. 集成边界（迁移期间不放宽）

沿用 ADR-0004/0005/0007/0008/0010/0016/0017/0018 与 `README.md` §设计边界：

1. AI 只出草稿；写 `image_prompt`、提交生成、采纳候选一律人工显式操作。
2. 不静默替换、不静默截断：参考图超限显示取舍；存档超限报错；模型缺失拒绝导出。
3. 不猜语义：不从文件名/片段名推断动作语义，不推断服装颜色，不把天气自动写进提示词。
4. 来源变化（预演/Scene/Shot/模板/参考图/画幅）使下游显式失效；过期绑定不得用来准备提示词。
5. 预演视频/白模/深度是**参考素材**，是否可作为深度控制输入必须由目标工作流节点实测，不得当普通参考图用。
6. 未获得许可登记的第三方内容不进入仓库。
7. 迁移不得改动既有业务数据、不得触发真实生成、不得触碰 18000 服务。
8. 每个文件改完即验证，再改下一个文件；不把计划当完成。

## 4. 五个看点对应的持久影响（结论摘要）

1. **剧本一键预演**：持久化的不是“生成好的场景”，而是“计划层（只读）→ 人工确认 → 确定性应用”的分离，
   以及“剧本名称 → 对象/素材绑定”的解析契约。先定 ID/来源契约，否则后续布景复用无锚点。
2. **大规模古镇组合布景**：冲击点唯一且根本 —— 存档物理模型（自包含 base64 vs 资产引用 vs 程序化参数）。
   不能仅按件数推断是否超过 32MiB；应实测字节数与渲染开销。重复几何应复用生成配方或共享资源，避免逐实例嵌入相同 GLB。
3. **64 套布景与程序化素材**：布景是**组合文档**而非二进制素材，不能塞进素材库分类（违 ADR-0017）；
   程序化素材必须记 generator id/version/seed/参数，并明确“重放一致”是承诺还是尽力而为。
4. **天气/时间/地形**：地形改变“地面”权威，牵连落点/避障、拾取、构图、白模与深度导出；
   环境必须成为场景文档成员（升版本 + 旧档兼容），并维持“天气不进提示词”。
5. **逐镜参考包与视频接入**：唯一跨越“旁路”边界的变更。参考包必须是有序参考权威的派生视图，
   不能成为第二条上传通道；视频接入会产生服务器侧素材 + 来源元数据，需在生成快照留痕，
   并显式写明取代 ADR-0005/0008/0010 的哪些条款。

## 5. 阶段划分与门槛

| 阶段 | 交付物 | 门槛 / 验证 | 禁止 |
|---|---|---|---|
| 0 核对（**本次完成**） | 本 ADR | 逐条给出代码位置；未能核对项显式标注 | 不得把未核对写成已核对 |
| 1 许可与来源 | Toonflow 逐项许可登记 + 素材清单（数量与许可分离） | 可读访问；`THIRD_PARTY_NOTICES.md` 增补；仍不引入代码 | 未登记不得继续 |
| 2 存档契约 | 第 7 节分叉的用户决定 + 单点小改 | 各改动文件逐一验证；旧档读取保持 | 不得一次性改多文件 |
| 3 布景实体 | 布景模板与场景快照分离的最小实现 | 新增测试 + 旧档兼容测试 | 不得复用存档列表充当模板库 |
| 4 环境 | 天气/时间/地形作为文档成员 | 旧档兼容 + 确定性取样测试 | 地形不得悄悄改变已有落点语义 |
| 5 参考包与视频 | 参考包派生视图 + 视频交接 | 参考顺序/容量/失效测试；格式匹配实测 | 不得绕过人工审阅 |

## 6. 验证

已实施静态布景与程序化素材入口，当前验证信号如下（不等于五项整体交付）：

- `npm --prefix frontend test`：退出码 0，包含 Node 测试、Studio import 门禁与 Vitest；无测试豁免。
- `npm --prefix frontend run build`：退出码 0，4493 modules transformed；仍有大块警告。
  素材模型共享懒加载块约 10.70MB（gzip 4.01MB），布景块约 1.29MB（gzip 0.34MB），不把懒加载当成规模优化已完成。
- `toonflowAssets.spec.ts` 4 项、`toonflowSets.spec.ts` 6 项通过；后者穷举 64 套模板的草稿构建与实际几何实例化，
  另测取消、非法模板、配方版本拒绝、稳定实例 ID 与三套场景的 JSON 草稿往返。这不是 Studio 存档往返验收。
  （本轮新增 5 组草稿契约用例后该文件共 11 组；新增部分未经运行，见 6.1。）
- 5180 隔离预览：古街草稿为 83 个真实可编辑实例，确认后追加；浏览器画面核对连续两侧建筑、铺装、摊位、路灯、牌坊与人物；
  一次撤销回到空场景，重做恢复古街。隔离环境的服务端素材库 HTTP 500 不作为服务端接入通过的证据。
- SunCalc 按真实包入口计算太阳位置，断言有限数通过；Zod 与 SunCalc 许可全文逐字比对 npm 包 LICENSE 通过。
- `git diff --check`：退出码 0。源模块仅登记 `git add -N` 意向，未提交、推送或部署。
- 未执行/未完成：Studio 程序化配方持久化往返、百件级以上及 1500+ 场景规模验收、真实生成、
  时间/天气/地形统一渲染、剧本预演入口、逐镜参考包与视频素材交接、H3 参考视频槽位格式/时长匹配。
  以上均不得在后续会话中被引用为已验收。

### 6.1 本轮（2026-11-24）验证状态：**无测试信号，不得引用为通过**

- 本次改动：`frontend/src/studio/scene3d/toonflowSets.ts` 增加布景草稿契约校验与真实计数，
  `materializeSetDraft`／`prepareSetDraft` 先整份校验再建几何；`toonflowSets.spec.ts` 增加 5 组用例。
- 未取得执行信号：`cargo test` 与 `npm --prefix frontend test` 各尝试一次，均返回
  `permission denied: ask unavailable (non-interactive): not pre-approved`；本仓库无 Rust 工程
  （无 `Cargo.toml`），`cargo test` 本无可运行目标。以上为宿主拒绝，不是通过。
- 因此本轮只能确认为**代码审阅通过**：类型闭包、错误文案与既有 64 套模板用例的兼容性均按源码核对，
  未经运行验证。后续会话必须先真实运行 `npm --prefix frontend test` 与
  `npm --prefix frontend run build`，才能把这一轮计入验收。

### 6.2 本轮（2026-11-24 续）验证状态

- 本次改动：`toonflowSets.ts` 增加剧本选场段（`parseScriptScenes`／`selectScriptScene`／`recommendSetTemplate`／
  `buildScriptSceneDraft`／`scriptFingerprint`：确定性解析、不联网、不调大模型、不写场景、不按姓名猜素材）；
  新增 `scriptSceneDraft.spec.ts` **15 组**用例；`SetTemplatePanel.tsx` 增加「从剧本搭景」区，
  复用既有 `onApply` 原子提交路径，追加前必须勾选核对。
- 实测信号：`npm --prefix frontend run test:unit` **657 通过 / 0 失败**（含 `tests/studio-imports.test.mjs` 导入完整性门禁）；
  `npm --prefix frontend test` 的 Vitest 段 **25 个文件 / 171 项全部通过**；单跑 `scriptSceneDraft.spec.ts` 为 **15 项通过**；
  `npm --prefix frontend run build` 构建成功（`built in 55.15s`），`toonflowSets` 懒加载块 1,295.89 kB（gzip 345.36 kB），未因本轮增长。
- `cargo test`：仍被宿主拒绝（`permission denied: ask unavailable (non-interactive): not pre-approved`）。
  本仓库无 `Cargo.toml`，没有 Rust 目标，**不能算通过**。
- 取舍记录：原打算把剧本选场放进独立模块 `scriptSceneDraft.ts`，但 `git add -N` 被宿主拒绝
  （`protected repository path mutation requires per-call approval`），未登记的新文件会被导入门禁判为「未跟踪」。
  为保持门禁为绿，本轮把实现并入已登记的 `toonflowSets.ts`；`scriptSceneDraft.ts` 已改写为空的注释占位文件，无人引用，**需人工删除**。
  后续若重新拆分模块，必须先登记新文件再改 import。
- 解析口径（审阅请据此核对）：场次标记支持 `第N场`、`场景N：`、`内景/外景/内外景`、`INT./EXT.`、`1. ` 编号；
  编号后必须紧跟内景/外景/INT/EXT，或以时间词结尾，否则当台词；时间词只按完整词元匹配
  （「夜市」不算夜景，中文没有 `\b` 词边界）；地点解析不到只给诊断，不给默认模板。
- 仍未验证：真实生成；界面上的人工点选流程（本轮**无浏览器验收**）；FPS/显存与 1500+ 规模；环境与地形；
  逐镜有序参考包与视频交接。

### 6.3 主代理续验：剧本搭景（不等于完整剧本预演）

- 复核后修复逐场角色归属：角色在前场出现后，后场再次说话仍须列入该场出场名单。
- 增加人工模板选择：规则无匹配时不自动替换；用户可显式选真实模板，保留原地点、原台词与内外景冲突提示。
- 原文修改中止在途草稿并清除确认，迟到结果不恢复旧草稿；中心坐标变化也要求重新确认。
- 实测：`scriptSceneDraft.spec.ts` **17 项通过**；真实 React `SetTemplatePanel.spec.tsx` **3 项通过**，
  覆盖显式选场、人工模板、确认门禁、源变更失效与失败不追加。
- 隔离 5180 浏览器：粘贴两场剧本 → 显式选第二场 → 显示该场两位重复出场角色及原台词 → 勾选确认 →
  追加古街 83 实例，状态为「已追加 古街集市：83 个可编辑实例（静态布景）」。未调用 ComfyUI、未触碰 18000。
- 上游 `shotCoverage.ts` 的本地分场/对白逻辑不依赖网络或 ComfyUI。当前仅完成剧本搭景与 6.4 的角色显式绑定；
  逐镜机位/表演、台词与时间轴持久化仍未接入，不得把可编辑布景当成完整剧本预演。
  未来参考帧接真实图片/视频生成时才需要目标 ComfyUI 实例验收。

### 6.4 本轮（2026-11-24）剧本角色显式绑定：已测信号与边界

- 范围：只做“剧本出场角色 → 布景人物 / 中性占位人 + 站位”的显式绑定，复用既有 `SetDraft`（templateId 不变）
  与 v7 存档；不新增编辑器或应用通道，不写提示词，不调用模型/网络，不改模板目录，不触发真实生成。
- 实现：`toonflowSets.ts` 新增 `listSetPersonObjects`（只列可本地物化的 `person`／`seatedPerson`／`kneelingPerson`）
  与 `applyScriptCharacterBindings`；`SetTemplatePanel.tsx` 的「从剧本搭景」区增加逐角色下拉和占位人站位 X/Z。
- 规则：必须覆盖本场全部出场角色且一一对应；缺项、重复角色、两个角色绑同一布景人物、指向非人物对象、
  站位越界或两名角色站位重合都显式报错，不做默认分配。绑定布景人物时重命名该实例（素材、姿态、原站位不变）；
  占位人是内置胶囊人（来源记为“胶囊人”），不是最终角色模型；其身份由既有 `createSceneObject` 生成，与模板其他实例一样每次唯一，
  重复追加同一剧本场景不会与已有对象撞号。输入草稿不被修改，结果仍是可校验的普通 SetDraft：
  物化后带 `toonflow-set` 来源，沿用既有 v7 配方存档。
- 实测信号：直接执行已安装的 vitest 入口全量 **26 个文件 / 181 项通过**；单跑
  `scriptSceneDraft.spec.ts` 与 `SetTemplatePanel.spec.tsx` **25 项通过**；
  `toonflowSets.spec.ts`、`proceduralArchive.spec.ts`、`studioRecipeRoundtrip.spec.ts` **15 项通过**；
  `npm --prefix frontend run test:unit` 通过；`npm --prefix frontend run build` 成功（4494 modules，51.91s，
  `toonflowSets` 块 1,299.32 kB / gzip 346.48 kB，较上轮 1,295.89 kB 有增长）。
- 宿主限制：带管道或输出重定向的 shell 调用返回
  `permission denied: ask unavailable (non-interactive): not pre-approved`，因此改用已安装的 vitest 入口取得可读信号；
  node 门禁（`npm --prefix frontend run test:unit`）与 Vitest 已分别通过，但没有合并成一次 `npm test` 的退出码。
- 主代理补验：单跑分场/绑定、React 面板与生产存取回调 **3 文件 / 28 项通过**。
  新增生产回调测试：一名绑定已有布景人物、一名占位人，修改站位与朝向后连续两轮 v7 保存/载入；
  逐一断言角色实例名、稳定 ID、位置、四元数与非空几何，输出 `SCRIPT_BINDING_ARCHIVE=PASS characters=2 objects=4 cycles=2`。
- 5180 浏览器补验：现代卧室两角色草稿初始全部未绑定；人工把张三绑到坐姿布景人物，李四选中性占位人并设 X=2.5/Z=-1.5；
  勾选确认后页面显示「已追加 现代卧室：4 个可编辑实例（静态布景）」，对象列表出现张三、李四与未绑定的原布景男主。
  此处仅为 DOM 交互与对象列表证据，未声称自动删除原群演，亦未声称已生成角色表演。
  后续截图曾为整页白屏，同步 URL 查为 `about:blank`；重新导航 5180 后界面与空网格可见，但无模型。
  当时未取得人物像素证据；后续已补验，见下方 6.5。不以 DOM/非空几何代替像素验收。
- 最终主代理原生命令信号：`npm --prefix frontend test` **657 项 Node + 182 项 Vitest（26 文件）通过**；
  `npm --prefix frontend run build` **exit 0，54.61s**。末次计数显示补丁另由 React 断言确认：
  绑定已有布景人物不新增实例，选中性占位人才增加人数；最终全量测试与构建均在该补丁后执行。
- 本节测试当时未验证（人物画面与浏览器往返已由 6.5 补验）：机位/表演/台词时间轴、FPS 与显存、
  真实生成、18000 服务与业务数据。角色绑定的审阅关系和原台词尚未单独持久化；当前保存的是对象身份、实例名与站位，不是完整剧本工程。

### 6.5 人物画面与隔离浏览器保存/刷新恢复补验

- 仅使用 5180 和 127.0.0.1:18999 内存夹具，不连业务 DB、不访问 18000、不调用生成；没有修改产品源码。
  夹具为 `frontend/.tmp-fixture/server.mjs`，支持真实 multipart 保存与按 ID 读取，重启不保留测试新档；不得随产品入库。
- 实际流程：两场剧本显式选现代卧室场，张三绑定已有坐姿人，李四新建占位人 X=2.5/Z=-1.5；确认追加 4 个对象。
  保存 #1000001 → 刷新页面 → 列表选择该档 → 确认恢复，状态为「已载入场景（4 个对象）」。
- 主代理直接看保存前后截图，均可见卧室家具、床上紫色人形、床尾紫色人形与右侧灰蓝胶囊人；相对布局无明显变化。
  恢复后对象列表为现代卧室/张三/男主/李四，未见错误弹窗。只证明可见与恢复，不证明人物比例、遮挡或表演正确。
  证据截图移至 `.rscode/role-binding-before-save.png` 与 `.rscode/role-binding-after-restore.png`，不入库。
- 独立读取实际夹具 #1000001：`ROLE_BROWSER_ARCHIVE=PASS bytes=19874 objects=4 definitions=4 preview=jpeg`；
  断言 v7、全部对象名、李四 X/Z、4 定义及 JPEG 预览。首次严格 Y=0 断言 exit 1，实测贴地 Y=2.384185793236071e-9 米；
  采用明确 1e-7 米容差后 exit 0（X/Z 仍精确比较），没有修改产品数据或测试以掩盖差异。
- 未交付：最终角色模型、机位/表演、台词时间轴与审阅关系持久化、真实业务后端、FPS/显存、真实生成。五项整体仍未完成。

### 6.6 剧本镜头表与台词时间轴：可审阅/编辑，随现有存档持久化（2026-11-24 续）

本步只做「台词 → 确定性镜头表 + 人工机位」的接入，不新增编辑器、不触发生成、不改角色绑定流程。

- 模型：`toonflowSets.ts` 新增 `ScriptShotPlan`（版本 1）与 `buildScriptShotPlan / validateScriptShotPlan /
  validateScriptShotCamera / updateScriptShotDuration / setScriptShotCamera / clearScriptShotCamera /
  assertScriptShotPlanSource / summarizeScriptShotPlan`。每句台词一镜，按原文顺序连续排时间；
  时长复用既有 `estimateChineseDialogueDuration`（每秒 9 字、标点停顿、换说话人 0.15 秒），最小 0.2 秒；
  最多 64 镜、总时长 ≤ 86400 秒。台词、说话人、动作行逐字保留，不做改写或截断。
- 机位不猜：每镜初始 `camera: null`，界面显示「未指定机位」数量；只有用户点「用当前视口机位」才写入真实取景
  （有限坐标、机位≠注视点、视角 15–90°）。「预览」复用现有 `cameraRef/OrbitControls`，不新建第二个预览器，也不渲染序列。
- 时间编辑：改某一镜时长只把后续镜头整体顺延，台词只随镜头平移；新时长装不下本镜台词、非有限数或越界都显式报错，不压缩台词。
- 人工闸门：生成镜头表后仍需单独的「确认镜头表」勾选，才调用 `onApplyShots` 接入场景；角色绑定与布景确认流程原样保留。
  生成/应用都要求「已审阅的场次草稿」，来源指纹（剧本原文 + 本场内容）不符即失效，必须重新生成。
- 持久化：镜头表作为可选字段 `scriptShots` 写入现有场景文档，存档版本仍为 6/7，不新增格式、不改 32MiB 闸门。
  旧存档没有该字段 → 载入后为 `null`，不补造镜头；字段存在但版本/结构不认识 → 显式报错且不替换当前场景。
  `scriptShots` 已进入未保存检测指纹，应用镜头表或改时长会正常触发未保存提示；重新打开存档可把已存镜头表载入面板复核。
- 明确不做：动作行与台词在原解析器里是两条独立数组、交错顺序已丢失，因此动作行只作为计划级「不计时画面说明」保留，
  不硬塞给某一镜；机位/台词表的审阅关系仍不写回剧本原文；不生成表演、不生成参考包、不提交视频任务。

**本轮实测信号（可复跑）**

- `npx --prefix frontend vitest run --root frontend src/studio/scene3d/scriptSceneDraft.spec.ts`：**27/27 通过**
  （其中 7 项为镜头表：一镜一句/原文保真/不补机位、动作行不计时、空台词最小 0.2 秒、越界与坏数据显式失败、
  改时长顺延且不改台词、机位校验与摘要、来源失效）。
- `npx --prefix frontend vitest run --root frontend src/studio/scene3d/SetTemplatePanel.spec.tsx`：**10/10 通过**
  （新增 5 项：二次确认才接入、改时长调用、显式记录机位、错误保留计划、改剧本后计划消失；存档镜头表可载入复核）。
- `npx --prefix frontend vitest run --root frontend src/studio/scene3d/studioRecipeRoundtrip.spec.ts`：**4/4 通过**；
  新增生产回调用例保存/恢复 `scriptShots`，并验证版本 2 被拒后场景对象数组保持原引用，
  日志 `SCRIPT_SHOT_ARCHIVE=PASS shots=2 bytes=3774`；32MiB 用例 `STUDIO_SCALE instances=1577 ... bytes=671832` 不变。
- `npm --prefix frontend test`：node 源结构测试 **657 通过 / 0 失败**；vitest **26 文件 / 195 用例全部通过**。
- `npm --prefix frontend run build`：**exit 0**，`toonflowSets` 1,306.14 kB（gzip 348.78 kB，较上轮 +6.82 kB），
  `mount-scene3d` 2,954.60 kB 未因本步变大（`import('./toonflowSets')` 仅在存档含 `scriptShots` 时懒加载，
  类型引用在编译期擦除）。

**未验收边界（不得引用为通过）**

- 本步没有做浏览器验收：未在真实页面里生成镜头表、记录机位、保存后刷新恢复；只有 jsdom 交互与生产存取回调测试。
  该缺口已由 6.7 补验；6.7 只补浏览器存取往返，不改变本节其它边界。
- 未测 FPS、峰值显存、真实业务后端、18000 端口的任何服务，也未触发生成任务。
- 没有表演/骨骼动画生成，没有逐镜参考包或视频交接；镜头表只是计划元数据，不等于成片。
- 存档只保存镜头表本身，不保存剧本原文：重新复核已存镜头表时无法再次校验来源指纹（面板会明确显示「没有当前草稿可比对来源」）；
  需重新粘贴同一场剧本并生成草稿，才能再次通过来源检查。
- 五项整体仍未完成；本步只把「台词时间轴 + 人工机位」从其清单中部分补齐。

### 6.7 工作流节点补验：测试、构建与 5180/18999 镜头表存取往返（2026-11-24）

本轮只复核 6.6 的既有实现，没有新增功能，也没有修改产品源码。未提交、未推送、未部署。

- 独立复跑 `npm --prefix frontend test`：node 源结构测试 **657 通过 / 0 失败**；vitest **26 文件 / 195 用例通过**。
- 独立复跑 `npm --prefix frontend run build`：**exit 0**，4494 modules，`built in 59.13s`；
  `toonflowSets` 仍为 **1,306.14 kB（gzip 348.78 kB）**，`mount-scene3d` 2,954.60 kB，与 6.6 一致，未见本步回归增长。
- 阶段文件单跑：`scriptSceneDraft.spec.ts`、`SetTemplatePanel.spec.tsx`、`studioRecipeRoundtrip.spec.ts`
  **3 文件 / 41 用例通过**，进程正常退出（工具未报非零退出）。`SCRIPT_SHOT_ARCHIVE=PASS shots=2 bytes=3774`、
  `STUDIO_SCALE instances=1577 definitions=20 bytes=671832` 与 6.6 相同。
- 浏览器隔离环境：仅使用 5180 前端与 `127.0.0.1:18999` 内存夹具；未连接业务数据库、未访问 18000、未调用生成。
  18999 夹具沿用上一轮已在运行的内存实例（新起进程返回 `EADDRINUSE`），其中已有 #1000001 与 #999999；本轮新增 #1000002。
  前端通过 `.rscode/dev-5180.mjs` 把 `VITE_API_PROXY_TARGET` 指向 18999 后启动，产品源码未改。
- 实际流程：在 5180 的 `/studio/scene` 先加一件「中式牌坊」，粘贴两场剧本并显式选 `scene-2`，生成古街集市草稿（83 实例）；
  再生成 **2 镜**镜头表（0.78 秒、2 镜未指定机位），把第 1 镜改为 2.5 秒后总时长 **2.91 秒**、第 2 镜顺延到 2.50 秒，
  用当前视口记录机位（显示 `4.50,3.20,6.00 → 0.00,1.00,0.00 · 45°`），勾选「确认镜头表」后接入，页面提示
  「已接入 2 镜；保存场景后随存档保留。未指定机位 1 镜。」随后存到后端得到 **#1000002（31,215 字节）**。
- 独立读取 #1000002（`node .rscode/check-archive.mjs 1000002`）：`version=6`、`objects=1`、`scriptShots.version=1`、`sceneId=scene-2`、
  两镜时间 **0–2.5 / 2.5–2.91**、第 2 镜机位仍为 `null`、台词「走吧。」「好。」逐字保留、来源指纹非空；
  第 1 镜机位为视口原始浮点值 `4.499999999999997,3.199999999999997,6`（按 **1e-6 米** 容差与记录值比较，X/Z/Y 未改写成别的数）。
  输出 `SCRIPT_SHOT_ROUNDTRIP=PASS`。首次更严的逐位相等断言按预期失败，属于浮点尾差，不是产品缺陷；未修改产品数据或测试来掩盖差异。
- 刷新页面后经「后端存档 → #1000002 → 确认恢复」恢复：页面显示「已载入场景」，对象列表出现「中式牌坊」，
  打开「本地程序化素材」后显示「场景中已保存镜头表：2 镜 · 2.91 秒 · 1 镜未指定机位（scene-2）」；
  点「载入场景镜头表审阅」后两镜、机位与台词可复核，并如实提示「本面板没有当前草稿可比对来源」。
- 本轮未发现该阶段的产品代码缺陷，因此没有修改 `toonflowSets.ts`、`SetTemplatePanel.tsx`、`Scene3DStage.tsx` 或测试。
  辅助脚本 `.rscode/dev-5180.mjs`、`.rscode/check-archive.mjs` 与 `.tmp-fixture/` 只用于隔离验收，不入库；#1000002 只存在于内存夹具。
- 仍未验收：FPS/峰值显存、真实业务后端与 18000、真实生成、表演/骨骼动画、逐镜参考包与视频交接；
  镜头表仍是计划元数据，不能当作成片；存档不含剧本原文，来源指纹只能在重新粘贴同场剧本后复核。

### 6.8 主代理独立复核与截图证据更正

- 6.7 来自工作流节点报告；节点最终因 300 秒超时 blocked，不能把其报告直接当作主代理验收。
- 主代理另行执行 `npm --prefix frontend test` 后接 `npm --prefix frontend run build`，仅前者退出 0 才执行后者；
  监控 Work `work-001a20dfc6e54-21a` 已完成，组合命令 **exit 0**。Vitest **26 文件 / 195 项通过**，
  包含 `SetTemplatePanel` 10 项、`scriptSceneDraft` 27 项与生产往返 4 项；
  `frontend/tests/studio-camera-timeline.test.mjs` 相对 HEAD 无 diff，未削弱该测试。
- 主代理执行 `node .rscode/check-archive.mjs 1000002` **exit 0**，`problems=[]`、
  `SCRIPT_SHOT_ROUNDTRIP=PASS`：两镜 0–2.5 / 2.5–2.91 秒，第一镜原始机位坐标在 1e-6 米容差内，第二镜 null，台词逐字保留。
  脚本的 `bytes=31075` 实际是字符串长度，不是 UTF-8 字节数；列表的 31,215 字节口径不得与之混用。
- 像素更正：节点截图 `screenshot-1795530756612.png` 样式缺失、视口几乎不可见、镜头表不在画面内，**不得作为像素通过证据**。
  检查发现 5180 同时由两个隔离预览进程监听（127.0.0.1 与 ::1）；尚未用受控实验确定旧截图异常根因，不据此修改产品代码。
- 主代理明确访问 `http://127.0.0.1:5180/studio/scene`，从后端存档选择 #1000002 并确认恢复，再载入镜头表审阅、点第 1 镜预览。
  直接看图 `screenshot-1795531227424.png`：工具栏与左右面板样式正常，中心牌坊与网格清晰可见；
  左面板可见两镜共 2.91 秒、第 1 镜 0–2.50 秒及已记录机位、第 2 镜 2.50–2.91 秒及未指定机位，
  来源不可比对提示、未勾选的确认框与禁用应用按钮均可见。仅证明恢复与静态机位预览，不证明连续播放或表演。
- 本次只修正文档证据；不提交、不推送、不部署、不访问 18000、不触发生成。逐镜参考包、视频交接、角色表演与天气/地形仍待实现。

### 6.9 剧本镜头表连续逐镜播放（2026-11-24，下一步小改）

本步只把**已确认的 `scriptShots`**接到现有 React Studio 视口上连续播放：按时间轴切换人工记录的机位并显示本镜台词。
不新建第二个视口或播放器，不做角色表演，不渲染序列，不导出视频，不提交生成。

- 就绪闸门：`toonflowSets.ts` 新增 `assertScriptShotPlaybackReady`。任何一镜 `camera === null` 都显式拒绝，
  报出缺失镜次（如「还有 2 镜未指定机位（第 1、2 镜）」），不借用相邻镜头、不生成默认机位；输入计划不被修改。
- 时间轴采样：新增 `sampleScriptShotPlayback(plan, seconds)`。到镜次边界即切下一镜；`seconds` 钳制在 `[0, 总时长]`，
  超过总时长停在最后一镜；负值或非有限数显式报错；即使绕过就绪检查，当前镜缺机位也显式失败，不沿用上一镜相机。
  当前台词只在 `line.start ≤ t < line.end` 内返回，镜头尾部停顿返回 `null`，不借相邻台词。
- 面板：`SetTemplatePanel.tsx` 只在存在**已保存** `appliedPlan` 时显示「连续逐镜播放」；点击前再次调用就绪闸门，
  失败就显示原因且不调用视口播放。播放中显示「正在播放第 N 镜 / 共 M 镜 · 当前台词：…」；「停止并还原视口」交给场景。
  文案明确这是取景切换预览，不生成角色表演或视频。
- 视口：`Scene3DStage.tsx` 复用现有 `cameraRef/controlsRef` 与 `PreviewPlayhead`。播放前捕获当前机位并禁用轨道控制；
  每个镜次边界只设置该镜人工记录的 `position/target/fov`，不插值。停止按钮沿用既有 `stopAnimation`，
  由 `PreviewPlayhead.stop()` 还原播放前机位并恢复控制。另设 rAF 兜底定时器与场景 epoch 检查；
  懒加载 token 使停止后的迟到模块加载不会重新起播。`toonflowSets` 仍只经动态 `import()` 进入，未变成主视口静态依赖。
- 会话状态：播放中的镜次与播放开关不写入场景、不进存档；保存仍只保存 `scriptShots` 本身。播放不会改变对象、镜头表或来源指纹。

**本轮实测信号（可复跑）**

- `npx --prefix frontend vitest run --root frontend src/studio/scene3d/scriptSceneDraft.spec.ts`：**29/29 通过**
  （新增 2 项：缺机位逐镜拒绝且不修改计划；时间轴采样在 0.1 秒、镜次边界、台词结束停顿、超总时长与负值/NaN 的行为，
  以及绕过就绪检查时缺机位仍报错）。
- `npx --prefix frontend vitest run --root frontend src/studio/scene3d/SetTemplatePanel.spec.tsx`：**12/12 通过**
  （新增 2 项：缺机位时显示错误且 `onPlayShots` 未被调用；就绪计划经视口回调播放、第 2 镜显示「李四：好。」、
  停止按钮委托 `onStopShots`）。
- `npx --prefix frontend vitest run --root frontend`：**26 文件 / 199 用例全部通过**（上一轮 195，本步 +4）。
- `npm --prefix frontend test`：退出码 **0**（node 源结构测试段 + Vitest；本步未单独重算 node 用例数，不沿用旧计数）。
- `npm --prefix frontend run build`：**exit 0**，4494 modules，`built in 49.49s`；
  `toonflowSets` **1,306.97 kB（gzip 349.04 kB）**，较上轮 1,306.14 kB 增 0.83 kB；
  `mount-scene3d` **2,957.87 kB**，较上轮 2,954.60 kB 增 3.27 kB（含逐镜播放与卸载清理），
  未因逐镜播放重新拉入整套素材。

**未验收边界（不得引用为通过）**

- 没有浏览器端到端播放验收：本轮未在 5180 真视口里点播放并核对逐镜切换、停止还原与像素；只有纯采样、
  React 面板门禁/文案和既有 `previewPlayhead.spec.ts` 的基线还原测试。真实 rAF 节奏、后台标签页兜底与停止竞态未经运行验证。
- 未测 FPS、峰值显存、真实业务后端与 18000；未触发生成任务；未提交、推送或部署。
- 只切机位：没有角色表演/骨骼动画、没有逐镜参考包、没有视频编码或导出，不得把播放说成成片。
- 播放不改变也不持久化机位，停止会还原到播放前视口；因此不能把播放后的画面当成新的已保存镜头。
- 仍然要求每一镜都由人工记录机位；缺一镜就不播放，也不提供自动补机位或默认构图。

### 6.10 主代理复核：生产播放回调、真实切镜与台词停顿修复

- §6.9 的实现由工作流节点产出，最终节点超时 blocked；其报告不等于主代理复核。
- 主代理新增生产 `playScriptShots` 回调测试（沿用 `studioRecipeRoundtrip.spec.ts` 的真实代码提取机制），
  用可控时钟与真实 THREE 相机、`PreviewPlayhead` 断言：两镜位置与 FOV 按边界切换；停止、自然结束精确还原基线；
  控制开关/阻尼恢复；缺机位拒绝；停止后的迟到启动被 token 拦下。
  输出 `SCRIPT_PLAYBACK_CALLBACK=PASS cuts=2 stop=restored completion=restored lateStart=blocked`。
- 新测试提取器曾先后因替换到类型 import、包含后续 useEffect、漏掉多行依赖数组而 exit 1；
  已限定只替换 `await import`，并匹配独立行上的回调结束边界（支持依赖数组换行），未削弱存档断言。
  最终生产回调测试 **5/5 通过**，1577 实例与混合存档测试仍通过。
- 主代理补修：懒加载之前捕获场景 epoch，加载完成后再次比较，避免旧场景计划迟到起播；应用新计划前停止旧播放，
  防止界面台词与旧机位错配。
- 隔离浏览器 127.0.0.1:5180 → 18999：恢复 #1000002 后，缺第 2 镜机位时点击播放得到明确错误；
  载入审阅，人工把两镜各改为 12 秒、聚焦牌坊并用当前视口记录第 2 镜、勾选确认应用；两镜共 24 秒。
  第 1 镜机位约 4.50/3.20/6.00，第 2 镜约 4.08/2.87/5.44，均为真实视口记录，不是代码补造。
  主代理直接看 `screenshot-1795532722443.png`、`screenshot-1795532734505.png`：分别显示播放第 1/2 镜，
  牌坊取景确有变化；`screenshot-1795532742169.png` 显示停止成功，取景回到播放前基线。
  这些截图证明切镜和停止，不证明角色表演、连续帧性能或视频输出。
- 上述截图还揭示界面把整镜台词一直标成“当前台词”：主代理修为跟随 `sample.line` 的时间窗口，
  只有进入/离开台词窗口或切镜才更新状态，停顿显示“（停顿／无台词）”。
  生产回调补断言台词开始、结束与下一镜开始；React 测试保留原台词断言、明确提供新状态字段，并新增停顿不沿用台词断言，
  **12/12 通过**。修复前面板测试因旧夹具未提供新 line 字段 exit 1，不是产品把停顿当台词的理由。
- 最终独立复跑 `npm --prefix frontend test` → Node **657 通过 / 0 失败**、Vitest **26 文件 / 200 项通过**；
  随后的 `npm --prefix frontend run build` → **exit 0，55.92s**，组合监控 `work-001a20e1b3550-21f` 已完成。
  `toonflowSets` 1,306.97 kB / gzip 349.05 kB；`mount-scene3d` 2,957.95 kB；既有大包警告仍在，未修改警告阈值。
- 台词停顿修复后的浏览器像素尚未复验（热更新使场景会话清空）；此前截图不能当作该修复的像素证据。
- 不提交、推送、部署，不访问 18000 或业务 DB、不触发真实生成。角色表演、逐镜参考包、视频交接和环境地形仍未完成。

### 6.11 剧本逐镜有序参考包：复用既有三种 pass、ZIP 与时间轴采样（2026-11-24）

本步只做「已确认 `scriptShots` → 逐镜参考图包 → 本地下载」，不新增编辑器、不新增依赖、不触发生成、
不上传素材库、不写业务库、不编码视频。范围仍限于 React Studio 内既有面板与视口。

- 复用而不是另起一套：白模/法线/深度三种材质与取图仍走既有 `createReferenceMaterial` 与 `captureReference`
  （藏网格、地面、gizmo、骨架线与泼溅，换材质取图，`finally` 还原）；打包仍用既有 `createStoreZip`（仅 STORE）；
  镜序与采样仍用既有 `sampleScriptShotPlayback`；就绪闸门复用 `assertScriptShotPlaybackReady`（任一镜 `camera === null` 即拒绝）。
- 清单契约（`toonflowSets.ts`，纯函数、不碰 THREE）：`buildScriptShotReferencePack(plan, { passes?, now? })` 输出版本 1 清单，
  逐镜记录 `shotId`、`order`、`sampleSeconds`、人工机位 `camera`（原始浮点，不取整）、台词、
  包内文件名，以及 `sourceFingerprint`/`sceneFingerprint`、`cameraSource: 'viewport-recorded'`、`exportedAt` 与 `passes`。
  `passes` 默认白模+法线+深度，显式传入时只接受这三种且不可重复；空列表、未知类型或重复都显式报错。
- 固定采样时刻：每镜取 `sampleSeconds = 本镜开始秒数`，并在生成清单时用时间轴采样核对「镜序 ↔ 镜头 ID ↔ 机位」一一对应；
  因镜内机位固定，该时刻即本镜取景，不读实时播放头、不插值、不借相邻镜头。文件名 `shot-01-clay.png`… 的前缀就是镜序。
- 人工闸门：面板新增「逐镜参考包」区，只对**已接入场景**的 `appliedPlan` 生效；必须先勾选「已核对逐镜机位与采样时刻」，
  按钮才可用。导出前先做就绪检查；存在当前剧本草稿时再跑 `assertScriptShotPlanSource`，来源变了直接拒绝。
  视口回调返回 `false`（未产出下载）时保留这次勾选供重试，不假装成功、也不替用户重新勾上。
- 全有或全无：清单先整份算好，再逐镜渲染；帧先进内存数组，只有全部张数与清单一致、manifest 编码成功后才
  `createStoreZip` + 触发一次下载。任一镜取图失败、场景 epoch 变化或张数不符都在下载前抛错，**不产出部分包**。
- 视口还原：导出前保存机位/注视点/视角/轨道开关与阻尼，逐镜只设置人工记录的 `position/target/fov`；
  `finally` 里还原并恢复控制，场景被替换时跳过还原（否则会改到新场景的视口）。不缩放画布，包内图就是当前视口物理像素。
- 明确不做：不生成表演或骨骼动画、不编码视频、不把参考包接入生成链、不存素材库、不新增上传通道、不改存档格式与 32MiB 闸门。

**本轮实测信号（可复跑）**

- `npx --prefix frontend vitest run --root frontend src/studio/scene3d/scriptSceneDraft.spec.ts`：**31/31 通过**
  （新增 2 组：清单逐镜镜序/采样秒数/机位/文件名顺序/指纹与 manifest 正文；缺机位、空或重复或未知 pass、
  坏时间戳、来源失效、未知清单版本均显式失败且输入镜头表不被修改）。
- `npx --prefix frontend vitest run --root frontend src/studio/scene3d/SetTemplatePanel.spec.tsx`：**15/15 通过**
  （新增 3 组：未勾选时按钮禁用、缺机位时显示原因且不调用视口回调；就绪计划交回调并在成功后撤销勾选；
  当前草稿判定来源失效时拒绝且不调用回调；视口返回 `false` 时保留勾选）。
- `npx --prefix frontend vitest run --root frontend src/studio/scene3d/studioRecipeRoundtrip.spec.ts`：**6/6 通过**；
  新增生产回调用例用真实 `toonflowSets` + 真实 `createStoreZip`、只桩掉 `captureReference`：
  断言 2 镜 × 3 pass 的取图顺序与每镜机位/视角（`clay@4.00,3.00,6.00fov45` …）、下载文件名、
  用本地 STORE 解包核对 7 个条目（6 张 PNG + `manifest.json`）、manifest 的镜序/采样秒数/机位/两枚指纹，
  以及失败在第二镜第一张时不产生第二次下载、视口与轨道控制还原、缺机位在动视口前被拒。
  输出 `SCRIPT_REFERENCE_PACK=PASS shots=2 files=7 restored=1 partial=blocked`。
- `npm --prefix frontend test`：**退出码 0**（node 源结构测试段 + Vitest；node 段本轮未重算条数）。
  `npx --prefix frontend vitest run --root frontend`：**26 文件 / 206 项通过**（上一轮 200，本步 +6）。
- `npm --prefix frontend run build`：**exit 0**，4494 modules，`built in 49.25s`；
  `toonflowSets` **1,309.19 kB（gzip 350.02 kB）**，较上轮 1,306.97 kB 增 2.22 kB；
  `mount-scene3d` **2,962.00 kB**，较上轮 2,957.95 kB 增 4.05 kB（只增回调，未把整套素材拉进主视口包）。
  未新增依赖，大块警告仍在且未调阈值。

**未验收边界（不得引用为通过）**

- 没有浏览器端到端导出验收：本轮未在 5180 真视口里点导出、检查下载的 zip 与还原后的画面；
  只有 jsdom 面板交互、纯清单断言与生产回调（`captureReference` 为桩）。
  真实 WebGL 取图、逐镜 `toDataURL/toBlob` 编码耗时与 64 镜×3 张的内存/耗时未测。
- 未测 FPS、峰值显存、真实业务后端与 18000；未触发任何生成任务；未提交、推送或部署。
- 参考包是**参考素材**：不含角色表演、不是视频输出；深度图映射范围按当镜包围盒计算，能否作为深度控制输入仍需目标工作流实测。
- `exportedAt` 为导出时刻（非确定性），因此两次导出同一镜头表的清单不会逐字节相同；镜头/机位/指纹字段仍是确定的。
- 镜头表只有剧本原文与场次的指纹，没有场景对象指纹：记录机位后若又移动/增删了物体，导出不会自动失效，
  需人工复核取景。这一点不得被说成「参考包已校验场景内容」。
- 仍要求每一镜都由人工记录机位；缺一镜就不导出，也不提供自动补机位或默认构图。

### 6.12 参考包主代理复核与边界补修

- §6.11 为工作流节点产出，节点最终超时 blocked，其报告不是主代理验收。
- 主代理复用既有场景指纹，在导出开始捕获 `fingerprintRef.current().full()`，每镜开始、每次取图和读取 Blob 字节之后、打包之前比较；
  任一差异或 epoch 改变均拒绝整包。清单新增 `exportedSceneFingerprint`（既有 FNV-1a 非密码学标识）与
  `scope: static-mesh-reference-passes`。它记录本次导出的状态，不是文件完整性签名，也不把剧本场次指纹冒充场景内容指纹。
  该标识沿用场景脏检测覆盖范围，不是完整几何字节摘要；导出前改物体仍需人工重新核对，不声称机位录制时的场景被自动校验。
- 既有参考图 pass 会隐藏泼溅，视频平面也未按镜头时间同步，因此本入口**显式拒绝含泼溅或视频平面的场景**，不静默省略。
  面板已说明限制；不修改既有单帧参考图入口，不宣称完成混合资产统一取样。
- 导出时关闭轨道阻尼并冲掉旧惯性，最终恢复位置、注视点、FOV、四元数及控制开关，防止记录机位被惯性改变。
- 主代理执行生产回调测试 **6/6 通过**：真实 ZIP 解包得到 6 个图条目加 manifest；
  新断言导出快照标识、途中场景变更拒绝、泼溅/视频显式拒绝与失败不下载，输出
  `SCRIPT_REFERENCE_PACK=PASS shots=2 files=7 restored=1 partial=blocked sourceChange=blocked unsupportedMedia=blocked`。
  面板 **15/15 通过**。图像取图为桩，只证明顺序、清单、闸门、ZIP 和回收，不证明 PNG 像素正确。
- 主代理独立全量 `npm --prefix frontend test`：Node **657 通过 / 0 失败**，Vitest **26 文件 / 206 项通过**；
  随后 `npm --prefix frontend run build` **exit 0，49.61s**，组合监控 `work-001a20e2eb1e8-223` 已完成。
- 5180/18999 隔离页面补验：从内存夹具 #1000002 派生 #1000003，为第 2 镜人工构造测试机位 [-4,3,6] / [0,1,0] / 50°，
  仅用于确定性测试，不冒充经产品“视口记录”入口获得的机位；未写业务库或提交夹具。
  页面恢复该档、勾选导出确认后点击 ZIP 按钮，真实 WebGL 路径完成取图，页面显示
  “已导出逐镜参考包：2 镜 × 3 张 + manifest.json（519 KB）；仅本地下载，未存素材库、未提交生成。”
  主代理直接看 `screenshot-1795534433087.png`：彩色牌坊、网格、变换控件与面板正常可见，未卡在参考 pass 材质。
- 浏览器下载文件尚未取得（工作区与默认 Downloads 未找到该 ZIP），不能把成功文案当作下载文件落盘或六张 PNG 像素验收。
  自动化真实 ZIP 解包与浏览器真实取图路径分别有信号，但尚未贯通到同一份下载产物；64 镜内存/耗时仍未测，此阶段不能宣布全部完成。
- 未提交、推送、部署或真实生成，未访问 18000 和业务 DB；本次只接入本地派生输出，不自动存库。

### 6.13 工作流节点产出：剧本逐镜人工走位与可选骨骼 clip 预演（2026-11-24）

本步只接入「已确认 `scriptShots` → 人工绑定的角色平地走位 → 现有视口播放」的最小闭环：
不新增编辑器、不新建播放器、不做自动表演、不做地形/障碍避让、不生成视频。
目标对象与骨骼 clip 都必须由人从当前场景的稳定对象列表里显式选择，不能按角色名或文件名猜。

- 数据契约：`ScriptShotPlan` 仍为 **version 1**，每个 `ScriptShot` 新增可选 `motion` 字段；
  旧计划没有该字段时按未绑定处理，未知版本仍显式拒绝。每镜最多一个走位目标（一个角色），
  多个角色可分散到不同镜头，但同一镜内多角色同步走位未实现。
- 走位内容：`{ targetId, targetName, start:{x,z}, end:{x,z}, duration, animation? }`。
  `targetId` 是当前场景对象的稳定 ID；XZ 限 ±1000 米；起终点不得重合；时长 0.1 秒起且不得超过本镜时长；
  可选 `animation` 按 `{index,name,duration}` 严格匹配目标对象的已有 clip，不做名称/文件名推断。
  未提供骨骼动作时只做位置移动，不自动挑走路动作。
- 校验：`validateScriptShotMotion` / `validateScriptShotPlan` 对上述字段逐项报错。应用与播放前
  `assertScriptShotMotionTargets` 再用当前真实对象校验「存在、可见、未锁定、clip 索引/名称/时长仍一致」；
  缺目标、隐藏、锁定或 clip 变化都在移动任何对象之前拒绝，不替换对象、不猜替代 clip。
- 采样：`sampleScriptShotMotionAt` 在镜内按 `local = min(t − shot.start, 本镜时长, motion.duration)` 线性插值 XZ；
  朝向为 `atan2(end.x − start.x, end.z − start.z)`；走完后停在终点。没有缓动、路径点、地形高度或障碍绕行；
  Y 始终沿用对象原位，不抬升到地面。可选骨骼 clip 的时间取 `min(local, clip.duration)` 并留 0.1 毫秒余量，
  避免 `LoopRepeat` 在 clip 末尾回绕。
- 视口：`Scene3DStage.tsx` 的 `playScriptShots` 复用现有 `cameraRef/controlsRef`、`PreviewPlayhead`、
  `AnimationPreview` 与 `canMoveEntry`。每镜只设置目标对象的人工 XZ 与朝向，并在需要时采样人工选定的 clip；
  下一镜没有该角色走位时结束其骨骼预览但保留已走到的位置。停止、自然结束、播放失败、镜头表更新或场景替换
  都经同一收尾路径：取消 rAF/兜底定时器、还原机位、把每个被移动对象的 position/quaternion/骨姿还原到播放前，
  并清空逐镜基线。播放前把被移动对象的基线登记到 `motionBaselineRef`，场景指纹按基线计算，
  因此预演中的位移和摆骨不会被当成未保存编辑；停止后立即回到真实基线。
- 面板：`SetTemplatePanel.tsx` 新增 `ShotMotionEditor`，从 `listMotionTargets` 的真实对象列表选对象，
  填起止 X/Z、时长、可选已有动作；`bindShotMotion` / `unbindShotMotion` 只改镜头表，不移动对象。
  应用镜头表与「连续逐镜播放」前都会跑目标校验；没有对象列表时明确提示无法校验。
  面板文案写明「平地直线 XZ、不做地形或障碍避让、Y 跟随对象当前高度、骨骼动作只作局部预演、
  不按名字猜目标」。
- 参考包边界：含走位的镜头表仍可导出逐镜参考包，但参考包按当前静态场景取图，
  清单之外仅在计划诊断中追加「参考包不播放走位或骨骼动作」的提示；它不会应用 motion，也不是视频。
- 存档：motion 随既有 `scriptShots` 写入 v6/v7 场景文档，不新增文件、不改存档版本；
  旧的 `scriptShots` 无 motion 仍可读。生产回调测试已把带 motion 的镜头表写进 v7 存档、载入并断言 motion 往返，
  当前存档往返样本为 `3943` 字节。

**本轮实测信号（可复跑）**

- `npx --prefix frontend vitest run --root frontend src/studio/scene3d/scriptSceneDraft.spec.ts`：**34/34 通过**
  （覆盖 motion 结构校验、坐标/时长越界、起终点重合、未知版本、采样线性插值/朝向/clip 时间钳制、
  目标缺失/隐藏/锁定/clip 不匹配的显式错误）。
- `npx --prefix frontend vitest run --root frontend src/studio/scene3d/SetTemplatePanel.spec.tsx`：**16/16 通过**
  （新增手动选对象、填终点/时长/动作、应用后显示已绑定、接入前调用目标校验、清除走位的 React 交互）。
- `npx --prefix frontend vitest run --root frontend src/studio/scene3d/studioRecipeRoundtrip.spec.ts`：**7/7 通过**。
  新增生产回调用例用真实 `playScriptShots`、真实 THREE 相机/骨骼、`PreviewPlayhead`、`AnimationPreview`
  与可控时钟，断言两镜两个对象的直线 XZ、朝向、骨骼采样、镜次边界补终点、停止与自然结束还原、
  目标缺失/隐藏/锁定/clip 变化在移动前拒绝，并输出
  `SCRIPT_MOTION_CALLBACK=PASS walk=2 actors=2 stop=restored completion=restored missing=blocked hidden=blocked locked=blocked clipMismatch=blocked`。
  同文件既有 `SCRIPT_PLAYBACK_CALLBACK`、`SCRIPT_REFERENCE_PACK`、`SCRIPT_BINDING_ARCHIVE`、
  `STUDIO_SCALE`、`STUDIO_ARCHIVE` 与 `SCRIPT_SHOT_ARCHIVE` 仍通过。
- `npm --prefix frontend test`：**退出码 0**（Node 源结构测试段 + Vitest；本轮未单独重算 Node 条数）。
  随后 `npx --prefix frontend vitest run --root frontend`：**26 文件 / 211 项通过**（本步新增面板与生产回调 2 项）。
- `npm --prefix frontend run build`：**exit 0**，4494 modules，`built in 48.79s`；
  `toonflowSets` **1,313.22 kB（gzip 351.15 kB）**，较上轮 1,309.19 kB 增 4.03 kB；
  `mount-scene3d` **2,970.31 kB**，较上轮 2,962.00 kB 增 8.31 kB（走位回调与指纹遮罩）。
  大块警告仍在，未调整阈值。

**未验收边界（不得引用为通过）**

- 没有浏览器端到端验收：未在 5180 真视口里人工绑定走位、点播放并核对角色位移、朝向、骨骼姿势、
  停止/结束还原与像素；只有纯采样、React 面板和生产回调（相机/骨骼为真实 THREE 对象，但未经过 WebGL 渲染）。
- 未测 FPS、峰值显存、真实业务后端与 18000；未触发生成任务；未提交、推送或部署。
- 骨骼动作只是**局部预览**：必须由人选择目标对象已有的 clip，且仅支持骨骼 TRS 轨道；
  无骨骼轨道或对象无骨架时播放会由 `AnimationPreview` 显式报错并还原，不会自动生成或拼接动作。
  它不是动画师级表演、没有 IK/脚底贴地/动作混合，也没有把动作写回场景或存档。
- 走位是平地直线 XZ，没有地形高度、障碍物、避让、路径点、碰撞或镜头跟随；起终点由人填写，
  不做“从当前站位自动起走”或跨镜连续性校验。同一镜只能绑一个对象。
- 参考包不应用 motion；逐镜视频导出、把走位接入生成链、角色表演持久化均未实现。
- 镜头表仍只有剧本原文与场次指纹，没有场景内容指纹；移动/增删对象或替换 clip 后需人工重新核对并重新应用，
  播放前校验只能保证“当前这一刻仍匹配”，不能把旧参考包或旧镜头表说成已随场景变化自动失效。

### 6.14 主代理复核：走位人工门禁、生产指纹与构建产物浏览器验收（2026-11-25）

- §6.13 来自工作流 `flow-1795534602499-20472-10` 节点产出，最终 300 秒超时 blocked；不能直接当作主代理通过。
  本轮 `qa-only` 工作流亦因缺 fresh project-gate evidence blocked，未提供验收结论；以下为独立执行的证据。
- 修复面板 `Number('') === 0` 导致「不绑定骨骼动作」仍隐式选择第一个 clip 的缺陷：空选项不产生 animation；
  显式选择的 clip 消失时显示局部错误并拒绝应用，不抛出未捕获的事件异常。新 React 用例覆盖未选、选首项、改回空项、所选项消失；面板 **17/17，exit 0**。
- 独立执行 `npm --prefix frontend test` 后接 `npm --prefix frontend run build`，监控 `work-001a20e4c528f-22d` **exit 0**：
  Node **657 通过 / 0 失败**，Vitest **26 文件 / 212 项通过**；构建 **54.66 秒**，`toonflowSets` 1,313.22 kB、
  `mount-scene3d` 2,970.50 kB。大块警告未消除、阈值未改。
- 其后补充生产指纹断言：从真实 `Scene3DStage.tsx` 提取指纹表达式并执行，不使用常量指纹桩；
  临时走位/骨姿期间指纹保持基线，未参与走位对象的位移、参与对象的缩放仍改变指纹；停止清空遮罩，之后真实位移仍可检测。
  定向 `studioRecipeRoundtrip.spec.ts` **7/7，exit 0**，两镜两个对象的走位/骨骼采样与停止、结束还原断言继续通过。
  这组新增断言在上述全量命令之后执行，不把全量结果冒充对新增断言的验证。
- 隔离预览问题：`127.0.0.1:5180` 两次连接被拒绝，日志只公布 localhost；localhost 可访问但截图
  `screenshot-1795536444603.png` 样式缺失、视口不可用，**不计像素通过**。HTTP 检查 studio CSS 无 `.flex` 工具类；
  未据此修改产品配置或认定正式构建缺陷。改用刚构建的 `frontend/dist`，由 `.rscode/preview-5181.mjs` 在
  **127.0.0.1:5181** 预览，仅代理 **18999** 内存夹具；样式、网格、两人和视口正常。
- 浏览器用例是组合测试数据：从既有 #1000001 取「张三/李四」，从 #1000003 取镜头表，建 #1000004；
  两镜机位沿用测试夹具值，**不冒充本轮经视口记录**。在页面载入镜头表审阅，将第一镜改为 30 秒，人工选择张三，
  起点 XZ=(-1,0)、终点=(0,2)、走位时长=30 秒、不绑定骨骼动作，勾选后应用，再经真实「存到后端」得到 #1000005。
- 独立读取 #1000005 的断言 **exit 0**：`BROWSER_MOTION_ARCHIVE=PASS version=7 actors=2 motion=1 duration=30 bytes=7213`；
  motion 目标 ID 对应张三，起止坐标、30 秒、无 animation 均匹配，角色存档位置仍为基线 [-1,0,0]。
  构建产物页面经「后端存档 → #1000005 → 确认恢复」恢复后显示 2 镜 / 30.41 秒 / 1 镜人工走位，证明真实保存后的重载。
- 主代理直接看图：`screenshot-1795536555783.png` 为恢复后的基线；`screenshot-1795536609501.png` 与
  `screenshot-1795536620780.png` 均为第 1 镜，相机/白色李四保持不动，紫色张三相对固定网格发生位移；
  `screenshot-1795536668496.png` 显示自然结束，人物与机位回基线；再次播放并点击停止，
  `screenshot-1795536696383.png` 显示停止提示与同一基线。首次尝试停止时已自然结束，按钮不可操作，不计人工停止证据。
- 边界：浏览器未使用骨骼 clip（选项禁用），故不证明骨骼与走位的真实 WebGL 联动；生产回调仅证明真实 THREE 骨骼的采样还原。
  未测性能、地形避障、混合资产统一取样、视频导出或真实生成。辅助脚本、截图、内存夹具不入库；
  未提交、推送或部署，未访问 18000、未读写业务 DB。五项整体仍未完成。

### 6.15 工作流节点产出：场景环境时间/天气/地形进入文档并统一取样（2026-11-25）

以下为节点当时的实现与报告，不作为主代理独立验收；后续补全及独立复核见 §6.16。

- 契约：环境是 `SceneDocument` 的**可选**成员（`version: 1`；`hour/latitude/northOffset`、`weather{condition,intensity,wind}`、`terrain`）。
  缺省 `null`，旧档与旧行为（灯光预设 + 20×20 平地）完全不变；未知版本、未知字段、越界数值在替换当前场景之前显式报错，
  做法与 `scriptShots` 相同。兼容性取舍：旧客户端重存会静默丢掉该字段（与 `scriptShots` 相同，已写在代码注释里）。
- 纯逻辑放在既有 `ground-path.ts`（该文件必须能被 `node --test` 直接跑）：确定性太阳（春分近似，由 `hour/latitude/northOffset` 推出方向，
  明确是大气氛围而非天文数据）、天气只调制太阳强度/雾密度/背景、地形用整数哈希值噪声并保留中心平垫、高度取整到 1e-4。
  `groundHeightAt` 与 Toonflow `getTerrainHeight` 用同一套 a/b/d、b/c/d 三角插值：因为不能 import `terrainModel`（依赖链带 three/fflate），
  改为本地实现，并用对照用例把两份实现钉在一起；地表编号上限同理硬编码 58，由 `terrainSurfaceMaxId` 对照锁定。
- 视口接线：环境启用后接管主光/半球光、背景（有天空盒时让位）与 `FogExp2`，补光/轮廓光归零；切灯光预设或清除天空盒不会把环境画面顶掉。
  地形网格读同一份高度场与地表配色；落点、拖动微调与全部路径取样（渲染、跟拍、拖动预览、首尾帧、序列导出）统一走一个入口 `groundPathPoint`。
- 可走性：`proveWalkableLine` 沿直线 XZ 等距采样（≤400 点，默认 0.5 m），端点越界或坡度 > 0.6 时在**移动任何对象之前**拒绝并报「第 N 镜」；
  播放期间 Y 按地形高差跟随，基准是播放前的站立高度（不猜脚底偏移）。不证明避障、不证明脚底贴合，无寻路。
- 参考图（白模/法线/深度）取图时临时关雾（几何 pass 不烘天气），地形计为内容（换参考材质、参与深度包围盒），并在 `finally` 还原。
- 验证（本节点实测）：`npx --prefix frontend vitest run --root frontend src/studio/scene3d` → **21 文件 / 196 项通过**（含新 `environment.spec.ts` 21 项）；
  `node --test frontend/tests/*.test.mjs` → **657 通过 / 0 失败**；`npm --prefix frontend test` → **exit 0**，Vitest **27 文件 / 233 项通过**；
  `npm --prefix frontend run build` → **exit 0**，48.55 秒，`toonflowSets` 1,313.39 kB（+0.17 kB）、`mount-scene3d` 2,991.70 kB（+21.20 kB，+0.7%）。
  未把 `terrainModel`/`terrainPaint` 静态拉进主视口包（否则会带上 fflate），地形配色只引 `terrainSurfaces` 的选项表。
- `cargo test` 仍无 Rust 工程：**exit 1**，`error: could not find Cargo.toml in J:\mickeylan\ai\new-aigc-comfyui-minimax-h3 or any parent directory`
  （与 6.1 一致），不作为本项目验证信号。
- 为接入新依赖而更新的既有断言属**依赖接线**，不是放宽：`studioRecipeRoundtrip.spec.ts` 的提取作用域补 `environment/environmentRef/applyEnvironment`
  与真实 `proveWalkableLine/validateSceneEnvironment/groundHeightAt/groundPathPointOnTerrain`；`studio-ground-path`、`studio-kitbash`、
  `studio-compositing` 三个源文本用例改指「地形优先拾取」「单一取样入口」「环境天色」。
- 边界（未做，不得据本节宣称完成）：**无天气粒子/动画**（雨雪雾只有光照与雾密度）、**无地形笔刷/水面/地表编辑**（只有确定性生成器 + 平垫 + 清除）、
  面板未暴露纬度/朝向/风力、参考包 manifest 未写环境快照（环境经场景指纹间接覆盖）、未做浏览器/WebGL 验收、未测 FPS/显存。
- 供主代理：新文件 `frontend/src/studio/scene3d/environment.spec.ts` 仍未跟踪，提交前需 `git add`。本节点未提交、未推送、未部署，
  未访问 18000/业务 DB，未做真实生成，Toonflow 源仓库保持只读。

### 6.16 主代理独立复核：环境编辑、确定性逐镜视频与人工素材交接（2026-11-25）

**实现闭环**

- `environmentEdit.ts` / `EnvironmentBrushPanel.tsx`：不可变圆形笔刷支持抬升、降低、整平、平滑和地表配色；校验坐标、半径、强度和地表编号，无变化明确报错。最多撤销10笔，重建或重载地形清空历史；不会自动挪动已有布景。水体选项明确只是颜色，不是水面。修复 fieldset 的默认最小宽度，避免笔刷撑开窄栏。
- `environmentEffects.ts`：真实雨线/雪点，按显式秒数确定性采样，风力影响水平漂移，替换或卸载释放几何与材质。面板增加纬度、北向和风力；环境保存复用原存档契约。生成高度消除负零，保证 JSON 严格往返。
- `scriptShotSampler.ts`：随机访问每次从编辑基线重放已完成镜次；当前镜采样走位、地形高差、真实骨骼 AnimationPreview 或胶囊步态；停止还原相机、对象与骨姿。它用于视频导出，**不是已完成对旧视口播放、参考包所有路径的统一替换**。
- `scriptShotVideo.ts` / `ScriptVideoPanel.tsx`：复用既有 `previsVideo.js`，WebCodecs VP9/VP8 编码与 WebM 封装，1280×720、1/6/12/24 FPS、最多2880帧/256MiB。逐帧机位/动作/天气共用秒数；确认后才导出，取消/失败还原，不产半成品。导出清单记录机位、时间、环境和场景指纹。白模/法线/深度不烘雨雪；参考包清单也增加环境快照。
- 视频默认只留本地成品；人工再次点击才 POST 既有 `/api/materials`（video、可选 project_id），成功显示素材ID及项目 → Scene → 编辑视频提示词 →「3D结构参考视频」人工关联指引。不会自动关联、覆盖首尾帧或提交生成；场景变化拒绝旧成品入库，仍可下载审阅。**不是实际目标工作流的视频条件联调通过**。

**自动化与构建信号（主代理独立执行）**

- `npm --prefix frontend test`：Node **657通过 / 0失败**；Vitest **30文件 / 249项通过**。随后 `npm --prefix frontend run build` **exit 0**，4500 modules、49.37秒；监控 `work-001a20e8bb28c-242` 已完成。
- 定向用例：environment **26/26**；scriptShotSampler **3/3**；scriptShotVideo **3/3**；ScriptVideoPanel **5/5**。覆盖不可变笔刷与撤销、天气确定性/释放、真实骨骼任意seek与还原、编码帧时序、取消/渲染失败/场景变化、缺编码器和帧预算在改对象前拒绝、人工入库门禁和HTTP失败。
- 窄栏修复之后单独复跑 ScriptVideoPanel **5/5，exit 0**；再运行原生生产构建 **exit 0**（`work-001a20e958742-244`）。不能把 jsdom 测试当成布局像素证明；最终布局另见下述重载截图。
- 曾遇错误并修复：未导出的地表上限改读实际选项表；负零造成 JSON 往返不等而在生成器归一化；视频测试稳定对象列表并用 Node realm 验证二进制封装。未靠删除失败断言通过。

**隔离浏览器与真实文件信号**

- 仅 127.0.0.1:5181 构建预览 → 18999 合成内存夹具。恢复 #1000005，启用环境、设雨、生成80米/64段地形，在 X=10 应用一笔，真实保存得到 #1000006。独立读取断言 **exit 0**：`BROWSER_ENVIRONMENT_ARCHIVE=PASS id=1000006 vertices=4225 weather=rain motion=30`。
- 主代理直接看晴/雨截图 `screenshot-1795540641096.png`、`screenshot-1795540709076.png`：两人、山地和真实雨线可见。选择天气与 FPS 须用 CDP 指针点击 select 后按方向键；仅 DOM click 的第一次选择没有生效，不算雨景证据。
- 1 FPS / 两镜30.41秒：第一次渲染途中人工取消，界面明确“导出已取消”；再次导出完成31帧。原夹具素材接口返回404；先保留全部7份合成存档，再仅替换18999夹具以支持 multipart 素材接收，未改产品后端或业务数据。
- 人工点击入库得到隔离素材 #1000007，并捕获同一份真实上传 WebM：`.rscode/browser-shot-1000007.webm`。`ffprobe` **exit 0**：VP9、1280×720、1 FPS、31秒、299415字节；`ffmpeg` 全量解码 **exit 0**：`frame=31 / dup_frames=0 / drop_frames=0 / progress=end`。编码长度按末帧补足帧长，来源时长不冒充31秒。
- 从该视频提取第15秒帧 `.rscode/browser-shot-1000007-mid.png` 并直接看图：两个人、紫色人物抬腿姿态、起伏地形与雨线可见；没有网格或变换gizmo。截图是**真实浏览器成品经独立解码的像素**，不是测试桩。
- 最终构建重新加载页面，再通过「后端存档 → #1000006 → 确认恢复」；`screenshot-1795541437659.png` 直接核对雨景/地形/两人恢复，笔刷控件不再撑开窄栏，撤销计数为0。只证明合成夹具保存重载，不证明业务库运行。

**明确剩余边界**

- 自动化 Chrome 的内嵌 `<video>` 显示持续缓冲（`screenshot-1795541197453.png`），不能宣称浏览器播放器预览通过。取得的同一份实际文件已由 ffmpeg 完整解码；尚未确定浏览器缓冲原因，不据猜测修改编码器。浏览器下载目录仍未取得文件，实际文件来自人工素材入库的隔离接收，不冒充下载落盘验收。
- 角色动作目录完整接入、参考包仍不应用走位/步态、真实骨骼clip浏览器联动、寻路避障、水面河道/贴图、渲染期跨实例几何复用、FPS/峰值显存、64镜参考包内存/耗时及ZIP实际PNG像素仍未完成（视口逐镜播放与视频采样器的胶囊步态已对齐，见 6.17）。含视频平面/泼溅的受限输出仍显式拒绝。
- 未完成真实项目 Scene 关联和目标生成工作流实测；未触发生成、未访问18000或业务DB、未提交/推送/部署。夹具脚本、合成数据、媒体和截图均不纳入提交。**五项整体仍不得宣称全部完成**。

### 6.17 视口逐镜播放与视频采样器对齐（工作流节点产出，主代理独立复核并追加修复，2026-11-25）

本步只对齐两条已存在的逐镜路径，不新增播放器、依赖、存档字段或生成入口。

- 核对结论（按源码逐项）：
  - 地形 Y：两条路径都已按「播放前站立高度 + 当前点高度 − 起点高度」取样，插值函数同源；一致。
  - 骨骼 clip：都用 `AnimationPreview.sampleAt(min(animationSeconds, duration−1e-4))`，停止时由 `stop()` 还原骨骼快照；一致。
  - 胶囊步态：视频采样器在无人工 clip 时调用 `gaitCapsuleRig(rig, 已走米数, 本镜平均速度)`，并在每次随机访问与停止时 `resetCapsuleRig`；
    视口 `playScriptShots` 既不摆步态，也不在镜次结束/停止时复位关节。**此为不一致，已修。**
- 修复（只改 `frontend/src/studio/scene3d/Scene3DStage.tsx`，复用既有 `toonflow/capsuleRig.ts`，不复制公式）：
  - 播放时为每个走位目标缓存 `findCapsuleRigs`；
  - 无人工 clip 的分支调用 `gaitCapsuleRig`，距离与平均速度算法与 `scriptShotSampler` 相同；
  - 镜次结束、停止、自然结束与场景替换统一 `resetCapsuleRig`；创建骨骼 clip 预览前先回站姿，避免把上一镜的胶囊步态拍进骨骼快照。
- 生产回调一致性测试（`studioRecipeRoundtrip.spec.ts` 新增 1 项，不改旧断言）：
  同一份两镜计划、两套相同对象，一套走生产 `playScriptShots`，一套走生产 `createScriptShotSampler`；启用 40 米 / 64 段 / 0.5 米起伏地形。
  在 0.5、1.9、2.0、2.5、3.9 秒逐项比较胶囊位置（含 Y）、朝向、hips 位置、leg/torso 四元数、骨骼对象与骨骼节点位置，全部相等；
  0.5 秒步态确实非站姿；第 2 镜边界两条路径都回站姿且保留终点；停止后两套对象与骨姿均回基线、逐镜基线清空。
  输出 `SCRIPT_VIEWPORT_SAMPLER_PARITY=PASS capsuleGait=1 terrain=1 skeletal=1 stop=restored`。
- 回归信号（本轮独立执行）：
  - 定向 `studioRecipeRoundtrip.spec.ts`：**8/8 通过**；
  - `npm --prefix frontend test`：**exit 0**；`node --test frontend/tests/*.test.mjs` **657 通过 / 0 失败**，`npx --prefix frontend vitest run --root frontend` **30 文件 / 250 项通过**（上轮 249，+1）；
  - `npm --prefix frontend run build`：**exit 0**，4500 modules，48.23 秒；`mount-scene3d` **3,006.17 kB（gzip 1,043.25 kB）**，`toonflowSets` **1,313.39 kB** 未变；大块警告仍在，未调阈值。
- 未验收/未做：真实 WebGL 像素、FPS/显存与浏览器 rAF 节奏；参考包仍按静态场景取图、不应用走位或步态（本步未改，不得说参考包含表演）；
  逐镜视频的 WebGL 像素本轮未重测；未提交、推送、部署，未访问 18000/业务库，未触发生成，Toonflow 源库只读。

### 6.18 角色动作目录接入 Studio（工作流节点产出，主代理复核与追加实现见 §6.19，2026-11-25）

本步只做“目录数据 → 可绑定核对 → 作为普通动作资源加入对象”，不新增编辑器、不新增存档字段或版本、不触发生成、不改提示词。

- 目录数据契约（`toonflow/characterAnimations.ts`，既有迁移文件，无新文件）：
  - `characterAnimationCatalogRevision` 直接取迁移数据的 `revision`（当前 **3**），不另立常量，避免与数据漂移；
  - `listCharacterAnimationCatalog()` / `summarizeCharacterAnimationCatalog()`：真实计数 **29 = 源 27 + 镜像 2**，分 5 类；
    镜像项带 `mirrored` 标记且 ID 独立（`cc0PickUpRight`/`cc0InteractRight`），仍**不把镜像当独立模型数**；每项只报 `CC0-1.0`。
  - `characterAnimationBoneNames(id)`：从真实轨道解析节点名（当前 23 个唯一骨骼、24 条 TRS 轨道）；非骨骼局部 TRS 轨道直接报错，不做猜测性过滤。
  - `characterAnimationBinding(root, id)` / `createCharacterAnimationClipFor(root, id)`：只做同名骨骼核对，缺一根即不可绑定并**列出缺失骨骼名**，不做跨骨架重定向、不改名、不换别的片段；
    `createCharacterAnimationClip(id)` 每次给独立副本，挂到角色后不会污染共享目录。
- 加入通道（`animationPreview.ts` 的 `attachAnimationClip`）：同名片段显式拒绝（不覆盖既有资源）；加入前用保存同一套规则（`saveAnimationClips`）做整份校验，
  失败不挂载。成功后对象仍是普通 `animations` 数组，**沿用 v6 存档字段**，不新增版本。
- Studio 入口（`Scene3DStage.tsx`）：右栏「内置骨骼动作」下新增「动作目录（CC0 · 按需载入）」，按需 `import()`，
  面板显示真实条数/修订/许可、逐片段名称与时长、当前选中角色的可绑定结论（缺哪几根骨骼）；未绑定或对象锁定/隐藏时按钮禁用。
  目录数据因此是独立懒加载块，不静态进主视口包。
- 一致性：目录片段加入后就是普通动作资源，播放（`playScriptShots`）、逐镜视频（`createScriptShotSampler`）与参考包读**同一份轨道**，
  都按 `index+name+duration` 绑定；资源被替换成同名不同时长时，派生输出在移动任何对象之前拒绝。
- 脏检查补口：动作资源进入场景指纹（`clips` 行：名称/时长/轨道数）。此前加入动作不会触发未保存提示，重开可能静默丢动作；本步已补。
- 实测信号（本轮独立执行）：
  - 定向 `studioRecipeRoundtrip.spec.ts`：**15/15 通过**（含既有 10 项生产回调），新增输出
    `ANIMATION_CATALOG total=29 source=27 mirrored=2 revision=3 categories=5`、
    `ANIMATION_CATALOG_BINDING bones=23 missing=leftHand tracks=24`、
    `ANIMATION_CATALOG_ARCHIVE=PASS clip=cc0Run tracks=24 duration=0.933 bytes=54585`、`ANIMATION_CATALOG_DIRTY=PASS clips=1`。
    覆盖：真实计数与 ID 唯一、镜像项、许可、轨道→骨骼解析、缺骨显式拒绝、同名不覆盖、生产保存/载入后时长与轨道一致、
    采样器按同一资源播放与停止还原、资源变化拒绝、指纹随加入动作变化。
  - `node --test --test-reporter=spec frontend/tests/studio-imports.test.mjs`：**1/1 通过**（无新文件，导入门禁仍绿）。
  - `npm --prefix frontend run build`：**exit 0**，4500 modules，64 秒；`characterAnimations` 为独立懒加载块 **1,183.99 kB（gzip 308.43 kB）**，
    `mount-scene3d` **3,010.70 kB（gzip 1,044.93 kB）**，较 6.17 的 3,006.17 kB 增约 4.5 kB（仅 UI/接线）；大块警告仍在，未调阈值。
- 明确未做/未验收（不得据此说“完整角色表演”或“目录已全覆盖”）：
  - 本节点完成时没有程序化 `riggedPerson` 构建器；主代理随后已迁入真实 24 骨蒙皮人物及 37 片段（见 §6.19）。外部角色仍须骨骼名完全匹配；
    Mixamo/RobotExpressive/Fox 等命名不同的导入角色会在面板显示“缺少骨骼”并被禁用。骨架重定向、IK、脚底贴地、动作混合、根运动剔除都未做。
  - 目录修订号**尚未写入场景存档或派生清单**（参考包/视频 manifest 仍只记 index/name/duration 与场景指纹）；目录数据升级后的溯源仍是待决项。
  - 一个 24 轨片段实测约 **54,585 字节** JSON；32MiB 闸门不变，多角色多动作的真实体积与渲染开销未测。
  - 无浏览器像素/播放验收：未在真实视口点选目录片段、保存、刷新、播放并核对画面；未测 FPS/显存。
  - 未提交、推送、部署；未访问 18000/业务库；未触发生成；Toonflow 源库只读。

### 6.19 主代理连续补齐：真实蒙皮人物、几何复用、河道水面、贴图与导出证据（2026-11-25）

- `toonflow/riggedPerson.ts` 迁自只读 Toonflow 源库（MIT；动作数据保留 CC0）。人工角色绑定可选 24 骨蒙皮占位人，4135 顶点、8 内置 + 29 目录 = 37 动作。不推断角色外貌，不冒充最终角色，不做骨架重定向/IK/混合/根运动剔除。
- 上游部分片段声明时长小于 Float32 末帧（如 3.2 与 3.200000047）；迁入时 `clone().resetDuration()`，按真实轨道计算时长，不放宽存档校验。独立脚本输出 `RIGGED_PERSON=PASS bones=24 clips=37 archiveBindings=37 vertices=4135`。
- 真实浏览器：5182 生产构建，仅代理 18999 内存合成夹具；剧本选场→人工模板→24骨人物绑定→追加84对象→选中/聚焦张三→Run 播放。主代理直接查看两张截图，站立与弯臂抬膝奔跑姿态不同；下拉实际列出全部37片段。真实保存 #1000007 为 4027897 字节，37片段齐全，32MiB 闸门不变。
- **发现并修复真实恢复阻断**：#1000007 刷新恢复被严格校验拒绝。多材质 SkinnedMesh 经 GLB 导出/导入重组层级，旧子节点路径不能对应骨骼。导出克隆现在写骨骼稳定路径到 glTF extras；恢复按已验证映射绑定，旧档无映射仍走原路径；重复、畸形、部分缺失的映射直接拒绝。不会跳过校验，也不把失败存档说成成功。
  `riggedPerson.spec.ts` 用真实 GLTFExporter/GLTFLoader 两轮往返（非假骨架、非模拟解析），37片段完全一致，24骨与蒙皮保留，cc0Run 改变手臂且停止恢复；输出 `RIGGED_PERSON_GLB_ROUNDTRIP=PASS bones=24 clips=37 cycles=2 animation=cc0Run restored=1`。随后已重建前端并补齐真实浏览器恢复：先载入新合成蒙皮/河道夹具 #1000008，再由产品「存到后端」生成 #1000009，整页刷新后人工确认恢复，界面显示「已载入场景（1个对象）」且动作下拉实际恢复37项。存档断言 `BROWSER_RIG_RIVER_RESAVE=PASS id=1000009 objects=1 clips=37 waterVertices=394 bytes=3902519`；仅18999内存服务，未写业务数据。此前旧失败夹具仍不算成功。
- 动作指纹包含 clip.uuid，覆盖同名同长资源替换；异步加入前重核角色仍在场/仍选中/未锁定/可见，停止旧播放后选中新动作。目录数据仍以完整轨道快照存档，目录修订号作为独立溯源元数据尚未补入。
- 不可变生成几何逐属性精确比较后共享；引用计数释放最后一个使用者才 dispose。对象/骨骼/材质独立，视口卸载先移除已释放对象，防止后续场景遍历再次释放共享几何。`GEOMETRY_POOL=PASS shared=2 firstRelease=alive finalRelease=disposed`；1577实例/20定义/671832字节回归保持。FPS、峰值显存尚无测量。
- 环境正式支持可选 `waterLevels` 数组：逐顶点 null 或有限水位，长度须匹配网格，须为水体地表且高于河床。旧测试将该字段视为未知已不符合新契约，现保留未知字段拒绝测试，另验证水面长度、地表、水位、NaN与严格JSON往返，未放宽校验。
- 人工圆形地形笔刷增加河道/湖面与抽水：显式起点/终点、半径、水位和河床深度；同点是湖面。河床高度与水面独立；覆盖干地或抬高河床清理无效水位，原稿不可变，仍可10步撤销。显式水域禁止涉水；不宣称寻路或流体模拟。
- 视口地形按原 a/b/d、b/c/d 三角形生成。水面为独立网格，参考图换材质能同时覆盖河床与水面；地表按编号使用真实程序化纹理及 roughness/metalness/emission，不再只有顶点配色。不冒充上游完整地表混合/颜色编辑/贴图库。纹理由版本化算法、种子和地表编号确定，不联网；材质、纹理、水面几何随重建/卸载释放。
  `environmentTerrain.spec.ts` 4项 + `environment.spec.ts`26项通过；`TERRAIN_WATER_RENDER=PASS triangles=8192 water=620 materials=2`。随后在5182生产构建直接查看 `screenshot-1795572189668.png`：蒙皮人物完整、可见蓝色平水面与河岸和地表纹理。水面边缘仍按网格呈折线，不冒充平滑流体；截图中的规则线是编辑辅助网格，不当成地表贴图证据。像素不能单独证明水位与河床独立，该契约由上述断言核对。
- 已独立补验前轮真实参考ZIP：7条目、2镜×白模/法线/深度6张PNG；主代理逐张看图并解包核对时间轴与走位，`BROWSER_REFERENCE_ARTIFACT=PASS files=7 shots=2 sampleSeconds=0,30 motion=30 weather=rain bytes=4199720`。视口/视频/参考包共用采样器，追加跨镜掉帧与延迟首帧补齐，停止/失败均恢复基线。
- 内嵌真实视频面板取得 loadedmetadata→loadeddata→canplay→playing，readyState=4、1280×720、31秒、error=null；此前持续缓冲问题已补验可播放。64镜压力和真实生成实例未验。
- 本轮全量 `npm --prefix frontend test`：Node **657/657**、Vitest **32文件/263项**、exit 0（含新增GLB两轮往返与异常映射回归）；`npm --prefix frontend run build` exit 0，4502 modules，56.95秒。chunk 过大警告保留；没有调阈值、提交、推送、部署或调用真实生成。原工作流 coding 节点超时 blocked，不作为成功证据。

### 6.20 逐镜障碍拒绝、派生来源入库与64镜参考包（2026-11-25）

- 决策与完整验收见ADR-0021。播放、视频、参考包共用编辑基线静态XZ包围盒检查；外扩0.35米，排除隐藏/splat/走位角色，锁定静态对象仍阻挡。连续线段检查可拒绝薄墙和端点位于障碍，启动前拒绝，不隐式绕行、不修改人工起终点；不是动态碰撞或精确网格寻路。
- 视频及参考包清单包含artifact UUID、采样器版本、动作目录修订与障碍摘要/输出参数。既有素材上传增加可选derivation_manifest；Material同记录保留清单，非法值400且零文件零记录，旧上传兼容；客户端清单不是可信生成证明。真实Go上传/列表/临时SQLite重开全部通过，前端拒绝把旧后端丢失清单误报为可追溯成功并防止重复上传。
- 浏览器单镜阻挡导出明确拒绝；无阻挡64镜参考包真实导出并独立解包：**192张PNG+manifest共193条目，PNG全部2280×1282，ZIP 130609956字节**。64镜均有人工走位、64秒、双人物、雨天、80×80米/64段地形；清单有64条可走证明和目录修订3。记录阶段到捕获墙钟上界230119毫秒（含人工及传输），不作为纯渲染耗时。`BROWSER_PRESSURE64_REFERENCE=PASS`。
- 后续64镜/64秒/12fps浏览器成品完成：**768帧、1280×720、15963631字节、墙钟70277毫秒**，人工视频入库成功（`BROWSER_VIDEO64_UPLOAD=PASS`）；播放器duration=64、currentTime>0、readyState=4、error=null。此前取消记录不代表本项最终结果。
- 双人物场景有效探针：viewport1582×904、DPR1、canvas1568×882、页面可见，ANGLE/NVIDIA GeForce GTX 960/D3D11；播放后RAF约59.95次/秒、p50=16.7ms、p95=16.8ms、观察峰值JS堆130212262字节。**GPU峰值显存无API、仍不可得**。
- 后续只重启卡死专用Chrome进程树并用新测试profile，恢复加载能力；未证明卡死根因。64镜/120秒/24fps导出及测试素材入库完成：**2880帧、VP9、1280×720、31986399字节、232633ms**，清单/实际字节一致，FFprobe读取2880帧，FFmpeg严格全片解码exit 0。2904帧在产品入口实际role=alert拒绝、无成品（`BROWSER_FRAME_OVER_LIMIT=PASS`）。完整256MiB封装预算专项9/9通过，但并未实测256MiB浏览器成品。
- **上限视频Chrome播放仍未通过**：正确点击播放后报`PIPELINE_ERROR_DECODE`，独立原生浏览器播放上下文又30秒超时exit 1；停止继续重试，不把FFmpeg通过写成浏览器播放通过。面板补显式解码失败提示，保留下载/来源、不自动重试或生成。手势限制及误点进度条的验收脚本问题另已记录，不冒充产品故障修复。
- **1577实例性能已测但不流畅**：静态古街总览真实可见，GTX960/同上视口条件；15窗口30375.2ms，RAF加权**12.7077次/秒**（7.49–13.65），窗口p50 66.8–83.4ms、p95 83.5–563.4ms，观察峰值JS堆363062948字节。保留首个慢窗口；这是每RAF调用render的主线程节奏，非GPU完成时间或交互性能验收。记录`LARGE1577_MEASUREMENT=RECORDED`，仍需优化而非宣称通过。
- 离线交接补齐共享/项目归属上传→Scene人工保存回读→上传登记输入解析→快照来源副本（`STUDIO_SCENE_HANDOFF_OFFLINE=PASS`，tasks=0/providerCalls=0）。修复项目目录被固定为materials、模板回退静默丢弃视频两处漏洞；素材ID/清单保存在快照，后续素材修改不覆盖历史。详见ADR-0021决策8–11。
- 最终 `npm --prefix frontend test` exit 0，Node659/659、Vitest32文件/268项（含目录来源及解码失败新增用例）。`go -C backend test ./... -count=1` exit 0；新增Go文件格式化后交接专项exit 0。最终build exit 0（4502 modules）。不提交、不推送、不部署、不触碰18000或业务数据库；真实项目关联→目标生成→来源回溯仍需独立授权实例验收，五项整体未完成。

### 6.21 地表混合/颜色闭环与低配验收边界（2026-11-25）

- 先记ADR-0021决策13–16，再实现既有上游`surfaceBlend`（次级地表与0–1权重）、`surfaceColors`（全局调色板）、`waterColors`（有水顶点稀疏颜色）。严格拒绝非法长度/颜色/ID/无水颜色及未支持现代`surfacePaint`；旧档缺字段不猜测、不强制升级。笔刷不可变复制与撤销、替换/抽水/抬床清理失效混合/水色；原始地面38不改调色板。新增可操作入口为「混合地表」「全局地表色」「局部水色」及重置。
- 有混合时四角×两层以平滑双线性权重混合颜色/粗糙度/金属度/自发光，水面用独立顶点色；旧档无混合沿用原材质路径。**首笔启用混合会切换整片地形渲染，圆外旧边界/纹理外观可能改变**，应用前明确提示、撤销首笔可恢复，不声称逐像素旧画面等价。仍不是完整上游现代多层绘制/贴图库/凹凸细节。
- 自动化：`TERRAIN_COLOR_CONTRACT=PASS`、`TERRAIN_BLEND_EDIT_RENDER=PASS`；真实生产save/load回调两轮保留混合/调色板/水色，非法权重不替换场景（`STUDIO_TERRAIN_COLOR_ARCHIVE=PASS cycles=2 invalid=atomic`）。React入口覆盖显式操作、撤销、重置、错误及外部换档清历史，无网络副作用。
- 真实WebGL夹具渲染+readPixels：0%为[120,0,0]、100%为[0,0,159]、50%为[60,0,80]，水色[28,110,0]；四次GL错误码均0、shader错误为空（`TERRAIN_GPU_BLEND=PASS`）。这是实际GPU像素验收，不是仅shader字符串检查；测试是正交单地形，不外推大场景性能。
- 最新生产构建5184/内存夹具18999：从旧字段单角色河道档开始，UI混合→改地表色→改局部水色→保存→独立核对JSON→刷新恢复→截图入口通过（`BROWSER_TERRAIN_COLOR_ARCHIVE=PASS archiveId=1000020`，基础地表0/次级15/采样权重0.475、调色板#0000ff、水色#00ff00、consoleErrors=[]）。截图可见草地/局部混合与河道，产物保留在`.rscode/terrain-color-*`，不入库。
- 有界CPU+GL调用包装记录1577实例约9942次draw/帧，并捕获Spark空场景的traverse/traverseVisible成本；**包装与Profiler显著扰动结果，不把5.91次/秒与先前12.7直接比较**。最小修复只在纯网格时跳过空Spark，存在任何登记splat（即使隐藏）保持原路径，新增/删除可逐帧恢复；不重构模型树、不改配方结构或降低画质（`MESH_ONLY_SPARK_GUARD=PASS`）。未完成优化前后实测，不能宣称FPS提升。
- 用户指出本机尤其显卡性能较弱：本机GTX960测量仅作低配环境记录，不能作为其他机器或整体功能性能结论；不再重复2880帧/1577实例压力，小场景功能与确定性离线契约继续验证。Chrome上限解码失败保持事实和未知根因，不仅凭硬件说明就归因，也不将独立解码通过扩张为浏览器通过。
- 生产构建增加唯一来源声明的分发插件（`NOTICE_BUNDLE_PLUGIN=PASS`），不是全部第三方许可审计完成；最终全量与产物检查见ADR-0021。

### 6.22 连续收尾：存档并发与确认原子性（2026-11-25）

- 最后选择的存档必须胜出，不能按`File.text()`完成顺序决定场景。复现测试先在生产回调上失败（exit1：迟到A替换已载入B），随后仅把文件读取移入既有载入代次的保护区；先取得代次/确认，再读取文件，旧请求不解析、不提交。迟到GLB仍走原有待提交资源释放；旧catch/finally不写新错误、不清新busy，操作各自finish一次。
- 服务端存档预览同样检查原ticket；失败或用户取消恢复返回false，保留确认窗口。成功且ticket仍匹配才关闭，旧预览/迟到成功不关闭后来打开的窗口。未改变存档格式、路径、数据上限或新增网络接口。
- 生产回调测试`STUDIO_ARCHIVE_CONCURRENCY=PASS`覆盖逆序读文件、旧读取失败、迟到GLB释放、当前读取失败、非法JSON原子拒绝、实际脏场景取消分支及每个操作finish恰好一次；`STUDIO_ARCHIVE_CONFIRMATION=PASS`覆盖失败保留与迟到确认隔离。测试期望没有因缺陷而放宽。
- 最新生产构建单角色/无天气小夹具，真实文件input change事件和可控File.text()完成顺序：B先完成、A后完成仍保留B；旧失败不显示错误、不清C的加载状态；C正常替换（`BROWSER_ARCHIVE_CONCURRENCY=PASS`）。只在新建专用空页对一次性合成档确认返回true；不把该替身当实际确认取消证据（取消由生产回调测试另验）。首轮未处理合成页确认导致Runtime.evaluate 30秒超时exit1；第二轮显式约束确认后通过，不重试业务页。测试结束关闭所建页，没有重复1577实例/2880帧压力。
- `THIRD_PARTY_NOTICES.md`纠正「原样import即满足MPL」的过强结论：未修改依赖并不免除对应版本许可/源码获取义务。唯一源分发机制保持，未将工程测试当法律或商业授权证明。

### 6.23 本机验收范围收口与视频异步交接（2026-11-25）

- **用户最新明确要求优先**：「如果需要启动comfyui才能做的测试就标记一下跳过，本地电脑无法跑comfyui」。依赖启动/连接ComfyUI的真实生成测试统一记为**SKIPPED（本机无法运行ComfyUI）**，不再等待实例地址、不启动ComfyUI、不消费先前的一次生成授权。离线模板/任务输入/上传/Scene/快照测试继续跑，不能写成实机生成通过。
- 继续核对发现视频面板异步所有权缺口：切换镜头表后，旧导出拒绝会污染新错误，旧上传成功可能把旧素材ID应用到后来产物。专项先复现2项失败（exit1），再按每次计划转换的新会话身份隔离（包括A→B→A），卸载后不写状态。迟到入库回执单独显示「先前视频」与素材ID/来源确认结果，不误关联当前镜头表、不自动重试；上传没有被静默取消或当作未发生。
- `VIDEO_HANDOFF_ASYNC_OWNERSHIP=PASS`覆盖迟到成功、旧导出/上传失败、A→B→A、旧后端清单丢失但素材ID仍保留；面板专项10/10。正常人工确认、来源变化拒绝、取消、解码失败保留下载等原测试不变。完整回归数字见ADR-0021最新记录。
- Chrome问题尝试不同的有界对照：仅stream-copy既有正常/上限视频各前1秒码流，不重新渲染场景，通过偏好软件的WebCodecs解码诊断；命令`node .rscode/diagnose-video-software-decode.mjs`在45秒界限超时，无结果文件，不据此归因，也不重复尝试该路径。随后CDP只读可达检查通过，未留下该诊断标题的专用页。它不是ComfyUI测试、不能借本次SKIPPED要求把先前播放失败改成通过。

## 7. 实施决策

1. 在现有 React Studio 扩展；Vue 预演维持兼容，不冻结、不删除、不增加第三套持久化编辑器。
2. 大场景优先采用版本化程序化配方、稳定实例 ID 与几何复用；保留现有外部模型自包含存档和 32MiB 闸门。
   不凭件数上调上限；新版本存档继续读旧版本，未知配方版本显式报错而非换成别的模型。
3. 64 套布景为只读版本化模板目录，与用户存档和服务端真实素材记录分开。应用后成为普通可编辑对象。
4. 剧本先生成可审阅的确定性草稿，用户确认后应用；保留改站位、机位、台词和逐帧调整。
   多场剧本显式选场，不悄悄只做第一场；内置素材与大模型/Kiln 新建物件解耦。
5. 参考包为有序逐镜派生输出，记录来源与采样时间；接入已有素材和人工采用流程，不自动提交生成。
   视频导出后由用户显式存库及关联；模板无对应视频输入槽时只作素材交接，不伪装成已支持视频条件生成。
6. 古镇演示 1500 件场景未找到可复用源数据；使用真实布景工具组装并计数，不能声称迁入了原演示场景。
   素材数分别报告主项、衍生与场景实例；64 套以真实模板 ID 计数。
7. 天气、时间与地形进入场景文档，并统一视口、缩略图、截图、序列的取样；地形接入落点与避障前不得宣传已完成。

## 8. 完成门槛

五项必须有可操作入口、持久化往返、错误提示及自动化测试；大场景另需浏览器规模验收。
纯模块迁入、ADR 或构建通过不等于功能完成。源仓库只读、不提交推送、不部署、不触碰业务实例。

## 9. 当前实施进展与精确交接点

最新增强实现、支持限制及证据以 [ADR-0022](0022-studio-character-direction-and-surface-enhancements.md) 为准；上文按时间记录的「未迁入」是当时状态，不代表本轮仍缺实现。原任务逐项回写，不新增替代任务冒充完成。跨设备性能、显存与上限视频播放仍独立保留未验收/未通过。

- 已迁入运行时与类型闭包 1129 文件，清单见 `scene3d/toonflow/SOURCE_MANIFEST.json`；未引入上游编辑器主机、联网 AI 或 Kiln 下载入口。
  `aiScene.ts` 仅执行配方本地工具，`NodeAiTool` 改为本地类型；基础几何从上游视口提取到 `toonflowPrimitives.ts`。
- 已有入口：Studio → 本地程序化素材，搜索、分页、衍生选择、逐件添加；真实计数为 **11750 主项 + 69560 衍生 = 81310 项**。
- 已有入口：同面板的 64 套整套布景，先生成草稿、显示真实实例数、指定中心 X/Z、显式确认追加、逐件编辑、整批撤销/重做。
  首次空场景采用模板推荐观察机位；非空场景保留原机位与已有对象。追加不自动避让已有物体，界面明确提示。
- 模板时间、天气、群演动线尚未自动应用；环境可人工编辑（§6.16）。内景可能是一整块程序化内景加人物，实例数不能冒充可独立编辑的家具部件数。
- 存档契约已接入：有可归档布景配方时写 v7，否则保留 v6；继续读取 v1–v6。
  v7 用版本化定义表与逐实例索引去重，实例保持稳定 ID、顺序、世界变换、显隐与锁定；记录节点局部变换及可编辑材质。
  几何修改或带骨骼/动作的对象保留 GLB，不以原始配方覆盖编辑；未知配方版本、越界索引显式报错，失败不替换当前场景。32MiB 闸门不变。
- 布景草稿先整份校验再物化：版本、模板存在性、对象数上限 4096、稳定 ID 唯一、变换为有限数、素材必须带可重建来源。
  提供 `setDraftAssetReferences`（含衍生父 ID）与 `setDraftReuseCounts`（逐素材实例数，实例数≠模型数）。
- 自动化实测：83 实例复用 20 定义，配方区 144051 字节；生产 Studio 存取回调的 84 对象（配方 + 真实 GLB）混合存档往返通过。
  19 组古街组装的 **1577 实例**复用 20 定义，完整测试存档 **671832 字节**，恢复后全部 ID、顺序、位置与非空几何检查通过。
  这不是上游原演示古镇，也不是 1577 个独立模型。
- 隔离浏览器验收：5180 → 本轮专用 18999 夹具后端（不连数据库），通过既有存档确认入口恢复 1577 实例，视口实际可见多组建筑、摊位与人物。
  浏览器真实保存上传的 JSON **678040 字节**（含 JPEG 预览），解析核对 v7、1577 实例、20 定义及全部 ID/位置一致；刷新页面后从该真实保存文件再次确认恢复，页面显示「已载入场景（1577 个对象）」。
  可见与持久化往返已通过；后续已取得约12.7077次RAF/秒与观察峰值JS堆363062948字节（ADR-0021），不能当流畅性通过。峰值显存不可得、跨设备交互未验收；仅几何复用，不声称对象级GPU instancing。
- 已有入口：Studio → 整套布景 → 从剧本搭景。粘贴剧本 → 解析场次 → 列表显式选场（多场不默认取第一场）→ 生成该场草稿 →
  显示地点/时间/出场/台词/诊断/内外景冲突/真实实例数 → 勾选核对 → 指定中心 X/Z → 确认追加。
  内部复用 `SetDraft` 与既有 `onApply` 原子提交，不新增应用通道；无匹配模板时直接报错，不退回默认模板；
  不按姓名猜素材、不自动写提示词，台词按原文保留供人工审阅；支持显式人工改选真实模板。
  分场 17 项与 React 交互 3 项通过，浏览器选第二场并确认追加 83 实例通过（见 6.3）。
  剧本搭景、镜头表与人工机位/走位已接入；本轮另补人工引导自动导演、混合、脚底IK、校准重定向和时序避让（ADR-0022）。不是剧情理解、任意骨架自动识别或物理仿真。
- 已有入口（本步新增）：同一「从剧本搭景」区逐角色显式选择布景人物、中性胶囊人或24骨蒙皮人占位，并为占位人填写站位 X/Z；
  未绑定全部出场角色或未勾选核对时确认按钮保持禁用。绑定只改 SetDraft 实例名、追加占位人并写站位，复用既有物化与 v7 存档；
  自动化信号见 6.4：角色名/身份/站位已通过生产回调两轮 v7 往返，浏览器人工绑定与追加通过；
  人物画面与浏览器保存/刷新恢复已补验（6.5）；台词随镜头表保存。搭景前绑定审阅表本身不单独存档，已应用的对象身份/位置与逐镜稳定对象引用会保存；不能宣称重开后自动重建原绑定表。
- 已有入口（本步新增）：同一「从剧本搭景」区生成镜头表（每句一镜）、逐镜改时长、用当前视口机位/预览/清除机位，
  单独勾选「确认镜头表」后才接入场景；镜头表作为可选 `scriptShots` 随现有 v6/v7 存档保存，旧档缺字段仍可载入，
  未知版本显式报错且不替换场景；改时长只顺延后续镜头，装不下台词就报错。自动化与边界见 6.6；
  浏览器生成、记录机位、保存/刷新恢复已补验（6.7）；逐镜参考包实际ZIP与像素证据已补（6.19），FPS/显存与真实生成未验。
- 已有入口（本步新增）：镜头表面板的「连续逐镜播放」只播放已接入场景的 `scriptShots`，按时间轴在现有视口硬切人工记录的机位，
  并在面板显示当前镜与台词；缺任一镜机位就显式拒绝，「停止并还原视口」复用 `PreviewPlayhead` 恢复播放前机位。
  播放临时改变机位及显式绑定对象，停止后还原，不把临时帧写入存档；逐镜视频通过独立确认入口导出。自动化与边界见 6.9、6.17，浏览器走位证据见6.14。
- 已有入口（本步新增）：同一面板的「逐镜参考包」只对已接入场景的镜头表生效，勾选核对后按镜序在每镜开始秒数
  用人工机位渲染白模/法线/深度三张 PNG，与 `manifest.json` 一起打成一个 zip 本地下载；缺任一镜机位、来源失效、
  取图失败或场景替换都拒绝整包，采样后还原视口。复用既有三种 pass、`createStoreZip` 与时间轴采样，
  不新增依赖、不上传素材库、不写业务库、不编码视频。自动化与边界见6.11，真实ZIP/PNG浏览器验收见6.19；视频另走人工确认入口。
- 已有入口（本步新增）：同一镜头表面板为每镜人工选择场景稳定对象、起止 XZ、时长与可选已有骨骼 clip；
  走位随 `scriptShots` 以可选 `motion` 字段保存，旧档无该字段仍可读；应用与「连续逐镜播放」前都校验目标
  存在/可见/未锁定/clip 仍匹配。播放复用现有视口与 `PreviewPlayhead`/`AnimationPreview`，停止或结束后
  还原机位、角色位置与骨姿；XZ 直线，有地形时 Y 跟随高度场，静态障碍显式拒绝、不自动绕行（§6.20），同一镜只绑一个对象，骨骼动作仅局部预览。
  自动化与边界见6.13–6.14；本轮真实三对象浏览器已补骨骼混合/IK、导演、绕行、停止还原及保存刷新（ADR-0022）；
  胶囊步态、地形高度与骨骼 clip 已和视频采样器对齐（生产回调一致性测试见 6.17）。
- 已有入口（本步新增）：右栏「环境（时间/天气/地形）」——启用/关闭、时间滑杆、天气与强度、地形参数（边长/起伏/种子）生成或重建、清除，
  以及 `summarizeEnvironment` 状态行；启用后太阳/雾/背景与地形接管地面，灯光预设旁注明「预设此时只作参考」。
  环境作为可选字段随现有 v6/v7 存档保存（`environment`），旧档缺字段仍按平地 + 灯光预设载入；未知版本或越界值不替换场景。
  落点与全部路径取样读同一高度场，逐镜走位在播放前按坡度上限证明可走。实现与实测见 6.15。
  雨雪粒子、纬度/北向/风力、地形/河道/混合/颜色及现代surfacePaint完整59种程序材质已接入（ADR-0022）。真实浏览器编辑/保存/刷新与实际绘图画面通过；59种外观逐项、首次编译速度及跨设备性能仍未验收。
- 已有入口：右栏「逐镜视频导出与生成交接」，确认后本地逐帧导出、取消还原、清单下载、人工存视频素材库及既有「3D结构参考视频」关联指引。真实浏览器成品31帧经独立解码和像素检查通过（§6.16），内嵌播放器已取得playing证据（§6.19）；真实生成联调仍未验收。
- 已有入口（本步新增）：右栏「内置骨骼动作」下的「动作目录（CC0 · 按需载入）」——真实计数 29 条（源 27 + 镜像 2，修订 3）、
  逐片段名称/分类/时长、当前选中角色的同名骨骼可绑定结论（缺哪几根骨骼）；确认后作为普通 v6 动作资源加入该角色，
  随既有 `animations` 保存，播放/逐镜视频/参考包共用同一份轨道。实现与边界见 §6.18。
  已迁入真实riggedPerson构建器与37动作；本轮支持人工参考姿态校准、显式骨名映射的跨骨架重定向（ADR-0022），不猜GLTF重命名、不自动识别任意FBX。真实奔跑像素与存档恢复阻断修复见6.19。
- 目录新加入片段来源已补：可选catalogSource随完整轨道保存、旧档不猜来源、历史修订保留；真实GLB两轮往返及浏览器选择→加入→保存JSON核对→刷新恢复通过（`BROWSER_ANIMATION_CATALOG_SOURCE=PASS`，详见ADR-0021决策12）。旧内置37片段和普通外部GLB不反推目录来源。OpenCut MIT全文已补，但整体许可分发收尾未完成。
- 精确剩余：大场景交互性能/跨设备稳定性、不可得的峰值显存、2880帧Chrome解码兼容性、浏览器体积极限压力及59种程序材质逐项外观。真实ComfyUI消费/生成/回溯按用户要求SKIPPED。2880帧全片独立解码、2904帧实际拒绝、1577实例测量、离线Scene交接与来源快照、WebM预算保护已补；大场景约12.7次/秒、上限视频Chrome播放失败，不能据此宣布全环境通过。
  IK/混合/人工校准重定向、人工引导导演、顺序时序避让、现代surfacePaint与完整59种程序材质已实现并接入统一播放/输出/存档（ADR-0022）；不再把这些实现记为未迁入，也不把小场景闭环当大场景/跨设备验收。
  五项整体仍未完成，不因自动化测试或构建通过而改变。

### 本地闭环与外部验收分界（连续收尾）

| 原授权目标 | 本地已有闭环 | 尚不能标整体通过的验收 |
|---|---|---|
| 剧本→可编辑3D预演 | 选场、人工绑定、镜头表/台词、导演草稿、混合/IK/校准重定向、时序避让、保存恢复与停止还原 | 本地确定性建议而非剧情理解；须人工确认/校准；非物理/并行人群系统 |
| 大规模古镇组合布景 | 1577真实组装实例、20定义、浏览器可见/保存恢复和规模测量 | 同条件交互性能未通过；本机弱显卡，不再重复压力，不伪造跨设备结果 |
| 64套布景/程序化库 | 64真实模板、11750主项/69560衍生分别计数、搜索追加/逐件编辑/撤销、来源与v7重建 | 不把衍生当独立模型，不声称上游原演示古镇已迁入 |
| 天气/时间/地形 | 环境入档、地形/河道/混合/颜色、现代surfacePaint/59种程序材质、统一取样、UI保存刷新与绘图画面 | 首次编译性能、59种外观逐项及跨GPU未验收；不是外部图片贴图包 |
| 逐镜参考/视频→现有生成 | 64镜ZIP、WebM、人工素材入库、来源清单、项目目录解析、Scene保存回读与任务快照离线测试；迟到入库回执不污染新镜头表 | Chrome上限视频播放未通过；真实实例消费/生成/回溯按用户要求SKIPPED（本机无法运行ComfyUI），不是等待再次授权 |

本轮本地代码/隔离验收连续收尾；**ComfyUI依赖验收按用户明确要求SKIPPED（本机无法运行）**，不再把实例地址或生成授权当本轮继续条件。独立保留跨设备交互性能未验收、不可得显存和Chrome上限播放未通过等事实；不写「所有环境验收全部通过」。本次仍不提交、推送、部署。