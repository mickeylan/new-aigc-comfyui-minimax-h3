# ADR-0023：复用已有小说理解与导演结果，衔接可执行3D预演

- 日期：2026-11-25
- 状态：方向经用户确认，分项实施中；不是整链路验收通过

## 背景与目标

核心目标是小说到可编辑动画预演。小说理解已存在，不再开发第二套分析器。
后端 `NovelAnalysisService` 已提供章节/窗口分析及故事圣经；`scene_director_draft.go`
已有场次分析、表演节拍和镜头规划；`models.Scene` / `models.Shot` 已承接落地结果。
缺口是这些已有语义数据与 scene3d 的布景、动作资源和摄像机之间的执行适配。

当前 `draftShotDirection` 仍需要显式对象/clip映射，且限制单主体；
`ScriptShotPlan` 并非后端 Shot 的同构表示，不能把每句台词再拆一镜来替代已有导演设计。
自然语言 blocking / camera_movement 不等于空间坐标或真实动作资源。

## 决策

1. 复用已有分析链，不新增小说分析入口，不改既有 Skill、提示词、用户模板。
2. 首先接收已存在的 Scene + Shots：保留项目/场次/镜头ID、原顺序、时长、导演来源、
   表演、动作时间轴和对白范围。输入来自既有只读API，不调用导演生成端点，不写业务库。
3. 增加独立的内存来源快照契约，不能把后端导演数据伪装成已经可播放的 ScriptShotPlan。
   原字段及未知字段完整复制保留；来源修订标识用于拒绝迟到结果和使旧确认失效。
   此标识是内容一致性键，不是安全签名。
4. 后续执行规划以此快照为输入，结合真实布景/对象/骨骼clip目录生成建议；
   缺资产、无法映射动作、多主体超出现有采样器能力等须列为阻塞。
   自动适配是目标，不能把所有任务退回人工完成而宣称核心功能交付。
5. 结构合法、语义符合剧情、动作可执行、视觉验收是四种不同信号。
   原始动作文字必须保留，不能以通用走路或静止画面冒充剑术/交互等复杂表演。
6. 用户审阅确认后才原子应用到编辑场景；来源、资源或场景改变后旧确认失效。
   不自动提交图像/视频任务，不修改正式Scene/Shot，不改变现有v6/v7存档契约。
7. 新增模型提示词如确有必要，先原文展示并获批准，再实施；不得借此ADR绕过批准。

## 小项与验收

- A：已有导演结果来源适配。测试非手写分场输入、无对白动作镜头、镜头顺序与时长、
  动作时间轴原样保留、旧导演来源拒绝、跨场次/重复ID/无效时长拒绝、来源变化键改变、
  不修改输入且不产生网络或写操作。
- B：只读项目/场次选择与来源审阅入口，加载竞态和取消不污染场景。
- C：基于现有资产的布景/表演/镜头执行建议与缺口报告，接真实编辑器预览。
- D：确认应用、播放、保存回读、导出与代表性小说样例的端到端验收。

A完成不等于B–D完成，不得把“数据接通”报告成“小说自动生成准确动画”通过。
不重做理解层、不新增框架、不并入video-editor；本轮不提交或推送，不运行H3。

## 执行规划入口（第二小项）

用户已逐字批准独立“3D预演执行规划器”系统提示词，新增Skill v1；不修改既有Skill。
后端新增独立只读来源与规划端点，从业务库重取Scene/Shot，使用服务端来源哈希拒绝旧稿，
调用前后复核来源；客户端不能提交一段替代正文让模型绕过现有导演设计。
规划调用仅允许写既有Skill审计，不保存Scene/Shot/素材/预演、不创建媒体任务。
资产和当前对象目录由编辑器按实际资源提交，只代表本次目录快照，不作为服务端已核实资产的证明；
应用前须由编辑器重新校验真实资源。服务端校验JSON、来源、镜头覆盖、时长和目录引用。
首版执行结构支持目录资产空间摆放、复用对象、固定机位和单对象直线clip走位；
运镜、原地骨骼表演及复杂多主体动作仍不能冒充已有采样器支持，必须列出阻塞。
模型草稿不等于已可播放；本小项先交付后端规划与隔离测试，编辑器应用及真实模型质量另验。

## 首轮实施与验证

- A 已实施：`directorSource.ts` 复制完整既有Scene/Shot JSON，验证项目/场次、顺序、
  时长、动作区间及过期状态；SHA-256修订键包含原文、来源和未知字段，不生成可播放计划。
- B 已接UI：整套布景面板按需加载“从已有小说项目读取导演结果”，显式选项目/场次，
  只读GET并展示已有原文、镜头和时间轴；变更选择/卸载取消旧结果。两次读取场次发现变化时拒绝；
  现有API没有事务快照，因此不声称并发一致性已完整解决，应用前仍须重新校验。
- C/D 未实现；未接模型执行规划、真实资产自动绑定、确认应用或端到端小说动画验收。
- 直接适配器冒烟：`DIRECTOR_SOURCE_SMOKE=PASS`，exit0。
- 专项3文件40项通过，exit0；完整Vitest 43文件356项通过，exit0。
- `npm --prefix frontend run build` exit0；来源入口独立chunk 7.05kB（gzip 3.20kB），
  原有大chunk警告仍在。
- `npm --prefix frontend test` exit1：Node阶段的“已跟踪文件的相对 import 都指向已跟踪文件”
  失败，原因 `SetTemplatePanel.tsx -> DirectorSourcePanel.tsx` 新组件尚未纳入Git。
  紧凑Node诊断再次exit1确认同一原因后停止该路径；未改测试、未暂存或提交以绕过检查。
  因此前端完整test命令不能报通过（它在Node阶段停止，完整Vitest是另外独立执行的）。
- 组件编译首轮检查exit1 `missing UI`：esbuild默认ASCII转义导致中文字面量检查不匹配；
  夹具指定charset=utf8后相同文字断言通过，未改产品来迁就检查。
- 未访问真实业务数据；UI测试使用隔离响应，未做新入口浏览器/真实后端联调。
- 原生standard-change运行停在ADR用户决策节点；用户已通过提问工具批准先记录，
  当前工作流工具无决策提交字段，未宣称工作流完成。

## 执行规划入口接入与回归（2026-11-26）

- C 部分实施：前端捕获完整真实素材目录和可用场景对象/已挂载clip，生成目录修订；
  显式点击后重取服务端来源，再调用已批准的独立Skill生成待审草稿。模型回复后再捕获目录，
  若来源、资源修订或选择会话变化，拒绝或丢弃结果。此处“取消”是界面结果失效，
  不表示已经中止模型计算或计费。只展示草稿JSON和阻塞，不应用到编辑器。
- 实测素材目录11750条，服务端目录上限16384；真实上下文约1164195字节。
  请求超过2MiB直接拒绝、不静默截断。尚未验证真实模型的上下文容量、耗时与输出质量。
- 后端独立复核JSON、原文依据、镜头ID/顺序/时长、可见角色、目录资产和所属clip；
  未绑定的可见角色/表演、不能执行的运镜或动作结构列为阻塞，不伪装成执行成功。
  完成模型调用后来源发生变化则返回409，不向客户端交付可用计划。
- 增补UI测试：显式规划仅POST规划端点、请求使用服务端来源哈希与真实目录快照；
  资源变化拒绝、导演来源变化不调用模型、切换来源忽略迟到回复、模型失败不伪造草稿。
  增补后端测试：模型调用期间修改表演返回409；未知请求字段及无效目录在调用模型前拒绝。
- `go -C backend test ./...` exit0；随后新增并发来源测试后，
  `go -C backend test ./internal/service -run '^TestStudioExecution' -count=1` exit0。
- 完整Vitest `npm --prefix frontend exec -- vitest run --root frontend --reporter=default`
  exit0，45文件363项通过。此前同范围一轮exit1（362通过/1失败），新UI夹具在并发负载下
  用固定20ms等待WebCrypto，导致控件尚未出现；改为等待真实控件出现且可用，未放宽
  测试超时、未改变预期或产品行为；专项4项及随后完整363项通过。
- `npm --prefix frontend run build` exit0；来源面板独立chunk 9.02kB（gzip 3.75kB），
  完整目录和3D宿主的大chunk警告仍在。前述`npm test`的Git跟踪检查失败仍未解除，
  不把独立Vitest通过等同于完整`npm test`通过；未暂存、提交或推送以绕过检查。
- 未使用真实模型、未做浏览器/真实后端联调；未应用、播放、保存回读或导出执行草稿。
  C的编辑器预览和D端到端验收仍未完成，不能据此宣称小说自动生成准确动画已交付。

## 确认应用适配（2026-11-26，后续小项）

- C 增加独立应用边界 `studioExecutionAdapter.ts`：严格拒绝未知字段、非法素材/对象/clip、
  旧修订、镜头覆盖或时长变化、非原文依据、画外人物入画；不按台词重新拆镜。
  保留后端镜头ID、顺序和原时长，转换到现有 `ScriptShotPlan` 的固定机位/单对象XZ走位。
  完整权威来源、原始表演与动作时间轴、依据/假设及绑定通过既有action/diagnostics字段保留，
  不增加v6/v7存档字段或修改存档版本，也不覆盖用户已保存的执行稿。
- `studioExecutionApply.ts` 负责应用事务：按实际目录ID构建素材，沿用既有布景的米制定位和
  配方重建路径，保留草稿角度（弧度）、缩放和世界位置；已有对象不克隆、不替换素材。
  来源与真实目录在物化前后复核；编辑器同步指纹、场景epoch、来源选择及预览会话变化会拒绝。
  所有新增几何先暂存，最后同步提交；失败释放暂存资源，不留下半个布景。
  来源API仍非事务锁，两次读取不能证明提交后后端不再编辑，不声称跨端强一致性已解决。
- UI 新增独立勾选确认与应用按钮：说明追加布景、更新复用对象及替换当前镜头表。
  未勾选不调用应用；失败保留草稿但清除确认；切换来源/卸载后旧事务失效。
  Stage在确认应用开始时还原旧播放基线，再校验；提交失败回滚复用对象位置/角度/缩放/名称及
  新增场景子对象，不替换当前镜头表。成功后不自动播放、不保存、不导出、不触发媒体任务。
- 执行阻塞不因人工勾选而被绕过：缺机位/可见角色/表演、多段动作、运镜、不能兑现的Y高度
  和超出采样器精度的动作时长均拒绝生成可播放替代。Shot对白缓存和rune引用不是权威对白与
  时间映射；含对白的镜头当前明确阻塞应用，不静默丢弃或猜台词。后续需复用现有结构化对白
  及其时序，不能通过删除对白或修改测试来获得“可播放”。动作clip的语义正确性仍未量化。
- 新增测试20项：适配9项、真实目录物化/重建及事务6项、执行生产Stage回调2项、UI确认3项。
  覆盖了真实配方存档重建、来源/资源改变、伪造目录、失败资源回收和注入发布异常后的回滚；
  不是仅以mock回调存在作为应用成功信号。Stage既有27项生产回调回归亦通过。
- 完整Vitest `npm --prefix frontend exec -- vitest run --root frontend --reporter=default`
  exit0，49文件383项通过；`npm --prefix frontend run build` exit0，应用模块按需chunk
  8.12kB（gzip 3.79kB），大chunk警告保留。完整`npm test`既有未跟踪文件检查阻塞仍在，
  未暂存、提交或推送绕过它。
- 新模块编译检查首轮exit1 `ERR_MODULE_NOT_FOUND: esbuild`，原因根目录没有该依赖；
  改为从frontend/package.json解析项目已有工具链后 `EXECUTION_ADAPTER_COMPILE=PASS`。
  UI新夹具首轮通过但出现act警告，改为在act中等待可观察就绪；未改断言、未抑制警告。
- 本小项未改提示词、Skill、用户H3模板及后端业务数据；未运行真实模型或H3。
  C已接入确认应用代码与隔离夹具，但代表性小说的真实模型→浏览器播放→完整保存回读→导出
  尚未验收；D仍未完成。新增布景的视觉效果、角色外观和具体动作语义不能据此宣称准确。

### C 补项：既有权威对白 → 确认应用的镜头字幕（2026-11-26）

- 读取来源时同步读取既有 Dialogue，并仅复用明确时间段或既有解析器支持的
  `【0.5—2秒】` 来源标记；不按字数估时，不改台词，不写回业务数据。
  Dialogue 与解析后的 timeline 同时进入 source_hash；模型调用中修改台词时序返回409，
  应用前后重取来源发现变化也不发布。
- 只接完整且唯一引用的对白；局部rune、跨镜切分、同组多行、不完整关联、缺时序、
  超镜时长均阻塞。现有字幕采样一次只显示一行，所以重叠/乱序对白同样阻塞，
  不静默隐藏后一句，也不自动重排或拆镜。
- 前端以权威行填充既有 ScriptTimedLine（全时间轴秒数、连续index、原文与说话人），
  不使用 Shot.dialogue 展示缓存；画外对白仅进入字幕，不创建可见角色或音频。
  SpeechType 与权威来源置于既有 diagnostics，未增加v6/v7存档字段或改版本。
- 验证：`go -C backend test ./...` exit0；
  `npm --prefix frontend exec -- vitest run --root frontend --reporter=dot`
  exit0，51文件389项通过；`npm --prefix frontend run build` exit0，
  studioExecutionApply chunk 10.48kB（gzip 4.60kB），大chunk警告保留。
  隔离集成断言覆盖确认应用→JSON往返→真实播放采样器的精确字幕起止与相机还原，
  以及来源变化后的零发布；这不是完整场景存档、浏览器或视频导出的端到端验收。
- 失败记录：新增哈希测试首轮exit1，夹具误新增第二条对白，造成既有行未关联；
  改为更新隔离夹具已有权威行后，原有唯一行及文本变动哈希断言通过。
  新集成用例专项通过但全量首轮exit1：`Test timed out in 5000ms`，388项通过、1项超时；
  原因是并发时在用例内首次加载11750条真实目录。改为模块准备阶段静态加载相同真实目录，
  未mock、未调高超时、未改断言；专项2项与全量389项随后通过。
- 完整`npm test`的Git未跟踪导入检查仍为已知阻塞，未重试、未暂存或提交绕过。
  本补项未改Skill/提示词/H3模板，未运行真实模型或生成媒体；真实模型剧情/动作质量、
  真实浏览器联调与D项完整闭环仍未验收。未提交、未推送。

### 用户授权提交与推送时的复核（2026-11-26）

- 用户明确要求提交并推送后，按精确文件清单正式暂存导演执行功能组，排除用户H3模板、
  所有临时产物与业务数据；此前未跟踪文件导入检查的阻塞因此解除，而非修改测试绕过。
- `npm --prefix frontend test`（项目原始完整命令：Node测试后执行Vitest）exit0；
  保留本轮出现的React act警告、Three法线规范化及WASM实验警告，不以警告消失为通过依据。
- 暂存新增ADR时发现尾部空行，移除纯空行后`git diff --cached --check` exit0。
  本次仅提交与推送既有改动，不追认真实模型、浏览器联调、导出或原生workflow完成。

## 三项缺口续作契约（2026-11-26）

用户要求继续解决小说执行、视频seek解码和来源回溯三个缺口。本轮不提交、推送或部署；不访问业务数据库，不启动/连接ComfyUI，不提交真实生成。

- 镜头表增加显式可选 `execution.version=1`，不改变原计划version=1或场景v6/v7；旧档缺字段不启用新语义。支持镜内线性插值机位键、多个角色的有界顺序clip段和原地动作；每镜最多32角色/128段/64机位键，保留原镜ID、顺序和时长。与旧motion混用明确拒绝，不静默丢弃任一方。
- 动作段必须引用实际对象挂载clip；每段以原始action_timeline索引、主体绑定和精确区间审计，不凭动作名称证明剑术/持物等语义。缺资源、缺依据、无法完整覆盖则阻塞。角色间及静态障碍不能证明安全时拒绝；运镜只提供线性空间取样，不宣称遮挡/穿模质量验收。
- 播放、参考包和视频继续使用同一绝对时间采样器；随机跳转一致，停止恢复对象/骨姿/机位；人工确认后原子应用。保留人工修订，不自动生成、保存或导出。既有系统提示词及Skill不修改；仅能力/结构数据随实现同步，不扩大模型权限。
- seek问题先复现并控制变量核实编码/封装，不靠降FPS、降清晰度、增超时或自动重试掩盖。无法确认根因时保留失败和范围，不宣布修复。
- 来源回溯用隔离SQLite/HTTP可控provider覆盖素材→Scene→真实任务创建→可控完成→输出身份→历史快照，不冒充真实模型通过。真实ComfyUI闭环本轮SKIPPED；旧机失败、新机成功与本轮证据分别记录。

### 本轮实现与实际边界

- 前后端接入可选execution：机位键覆盖整镜且段内视点不能重合；多个角色可绑定顺序真实clip段、原地动作和显式yaw。后端规划与前端应用均校验真实对象/所属clip、索引/名称/时长、sourceIndex的原区间及每个主体完整覆盖；整份计划拒绝执行角色与旧motion混用。旧档保持原语义，未改提示词或Skill。
- 统一采样器增加绝对时间机位/多角色采样、跨镜端点重放与停止还原；播放/参考包/视频沿用此同一实现。执行前以编辑基线的XZ包围体半径加0.35米保守边距检查静态障碍、连续相对路径检查角色间距，不宣称任意骨骼动画全程体积碰撞或机位遮挡已证明。
- 已验证：受控双人物小说片段（沈青/林禾院中相遇）保留原文/镜头ID/时长/动作时间轴，审计确认应用一次；各主体绑定同一原始顺序时间轴的精确区间，取样骨姿和移动机位数值正确，乱序跳转一致、停止还原。是手工可控执行草稿，不是真实模型规划质量或浏览器视觉验收。
- 已验证：生产GLB导出/存档载入回调在v6/v7均保留execution及真实蒙皮角色clip，载入后可取样且骨姿精确还原；无execution的旧镜头不启用新语义。最初纯骨骼无蒙皮夹具和普通网格动画夹具分别被载入/保存契约拒绝；停止试改并核对已有生产蒙皮夹具后复用真实导出路径，保留并加强数值断言，未放宽校验。
- 代表样例首轮因手工夹具把并发主体写成重叠的源时间段而被既有directorSource拒绝（exit1）；改成既有“顺序时间段、每段多个主体”格式，两个角色各完整绑定每段，专项2项通过。没有改原有导演时间轴契约或弱化断言。
- 机位/多角色轨道目前由导演草稿路径产生，尚无逐段人工编辑UI；既有人工旧走位入口不冒充新轨道编辑器。代表性小说真实模型→浏览器审阅→完整存档→参考包/真实编码视频的一次整链仍未验收，D及原有大场景/跨设备/许可未通过项不转为完成。
- seek对照：Chrome158默认路径两份旧片60秒seek均decode失败，复制码流remux仍失败；独立profile禁用硬件视频解码后均恢复至61.79秒（readyState4/error=null）。限定为本机平台/硬件解码路径相关；没有产品编码修复证据，不自动切换用户硬件加速，不追认默认路径/原片全片通过。详见高性能清单T01和ADR-0022补记。
- 来源链：共享及本项目目录两种夹具均通过真实上传/Scene回读/CreateTask、受控HTTP完成/生产输出接口身份、GenerationSnapshot的素材ID及派生清单不可变；中文主稿存在但英文执行稿为空时任务数0、首尾帧保持。受控输出仅标记字节，不代表可播放模型输出；本轮真实ComfyUI **SKIPPED（本机无法运行 ComfyUI）**。
- 原生standard-change仍因本地Chrome profile文件锁而blocked，未宣称workflow完成。`npm --prefix frontend test` exit1（Node669/670）：Git跟踪检查拒绝已有文件导入未跟踪新模块`shotExecution.ts`，Vitest阶段未执行；不暂存/提交绕过，也不把独立Vitest通过报成完整npm test通过。
- 只读复核前的独立完整Vitest：`node frontend/node_modules/vitest/vitest.mjs run --root frontend` exit0，53文件/400项；`go -C backend test ./... -count=1` exit0（service 102.664s）；`npm --prefix frontend run build` exit0；`git diff --check` exit0。保留既有React act、GLTF法线规范化、大chunk及动态/静态导入混用警告，不以警告消失作为通过信号。
- 只读复核前的本地EXE已在备份旧产物后重建（后续浮点修复后须再次构建，不能用此哈希声称最新交付），未启动/替换运行中服务。`go -C backend build -o comfyui-console.exe .` exit0；95项前端资源与嵌入目录、EXE内字节逐项一致，许可声明source/dist/embed/exe四处相同（不等于法律合规已完成）。76032512字节，SHA-256 `9f3f1f6bf3cf599e118c90be733650c8bb056707df7ed66d89c2ee690c205bff`；vcs.revision为基线78b3f90且vcs.modified=true，不能把未提交构建称为78b3f90干净发布版。备份在`.rscode/gap-deliverable-backup-20261126-105159`；EXE/dist/诊断产物不入库。
- 只读复核发现小数镜长累加后`end-start`与原时长不逐位相等，会误拒后镜机位键/动作末端。沿用现有1e-6秒时间容差，只处理累加浮点边界，不改写源区间或放宽sourceIndex精确匹配；新增上下舍入及真正越界拒绝回归，专项6项exit0。修后串行`go -C backend test ./... -count=1` exit0（service 44.793s）、独立完整Vitest exit0（53文件/401项）、`npm --prefix frontend run build` exit0；Monitor的最终退出码为0。随后仅澄清后端fallback注释，英文兼容及来源链专项再次exit0（service 0.782s）。完整`npm test`的跟踪文件门禁仍未通过，不以这些独立通过替代。
- 复核提出的“确认哈希硬门禁”不直接采纳：现有`TestSavedEnglishPromptBypassesTranslationFreshnessGate`明确要求已保存英文稿兼容、不受来源漂移拦截。保留此行为并修正注释；新增非空门禁不证明确认哈希有效。上述兼容专项与来源链专项exit0；不得把“空英文稿零任务”扩大解释为“所有未确认稿均禁止提交”。模板fallback仍可重新assemble提示词，注释已限定到当前选择，不能声称所有分支均逐字提交。
- 保留运行时按真实包围体的严格间距，不采纳降至0.7米换通过的建议。此前应用只做结构校验，实际几何阻塞延后到播放/导出；本轮已补应用前同源证明，详见下节。单独保存草稿仍不等于几何可播放保证，应用后的编辑也可能令计划失效，播放/导出继续重新验证。
- C4应用前缺口本轮已修：前后端结构校验均检查整计划角色并集≤32；确认应用前检查真实包围体半径≤10米、角色间距、静态障碍及逐段地形水域/坡度安全，包含跨镜held角色。静止段继续执行现有可行走证明，不免证、不降低阈值。
- 当时相邻未处理项：执行角色资源预检门槛、移动execution机位的参考包provenance、面板只按`shot.camera`计数；这三项已在下文“产品面板重渲与执行机位语义续验”修正。新增部分错误仍为英文，逐段执行轨道人工编辑UI仍未提供。
- 浮点修复后的上一版本地产物（已由下节应用前预检版本取代）：`backend/comfyui-console.exe`，76032512字节，SHA-256 `b6869332117c6b57990058c0b93c6016bb879a6a476ae683acae0d1194e931d1`。旧EXE/嵌入目录已备份到`.rscode/gap-deliverable-backup-20261126-110033`；嵌入新dist后`go -C backend build -o comfyui-console.exe .` exit0，`node .rscode/verify-gap-deliverable.mjs` exit0（CURRENT_DELIVERABLE=PASS，95项资源逐字节一致，notices四处一致）。构建信息为windows/amd64、CGO_ENABLED=0、vcs.revision=78b3f90、vcs.modified=true；不是干净提交发布版，也不代表许可合规。未启动/重启服务，未提交/推送/部署。

### 应用前几何预检与真实文本小样续验（2026-11-26）

- 新增同步 `assertScriptShotApplicationSafe`，在拟应用的真实位置/旋转/缩放与新增对象上运行同一 `assertScriptShotWalkable` / `compileShotNavigation`。`finally`按编辑器Euler契约还原借用变换；不await、不发布、不写历史。生产Stage的执行计划确认在checkpoint之前调用，单独替换镜头表也在setScriptShots之前调用。失败原子拒绝；它不替代应用后每次播放/参考包/视频的再预检。
- 生产Stage专项4项exit0：结构合法但1米间距的双角色计划拒绝，checkpoint/collect/setScriptShots/syncObjects均0次；6米安全计划应用一次。几何专项5项exit0：真实包围体间距、拟应用墙/缩放、变换还原、超大包围体、跨镜held角色、水域；没有通过减小半径或跳过静止段取巧。新增Stage用例初轮因夹具targetName与应用后名称不符失败，修夹具为真实对象名称后通过，未放宽绑定规则。
- 角色并集前移：前端结构验证新增34角色拒绝回归；后端新增32边界接受/34拒绝，且两镜各17分别通过完整校验。后端新增夹具首轮因共享素材ID违反`object:ID`实际挂载契约失败；给每个对象各自挂载真实clip身份后通过，没有修改身份校验。后端轨道专项exit0。
- 可见Chrome夹具页19049有标题、播放/跳60秒按钮与状态，不再以空白文档冒充媒体验收。默认49210：首帧1280×720正常，seek后`PIPELINE_ERROR_DECODE`，exit1；Media域实际记录`D3D11VideoDecoder`、`kIsPlatformVideoDecoder=true`、D3D11 backend，错误链`d3d_video_decoder.cc:912`→`D3DStatus::6`（:667，VDA Error=0）→`DecoderStatus::1`，最终renderer的`DecoderStatus::108`。软件对照49211使用`VpxVideoDecoder`、platform=false，seek后61.6437秒、readyState4、seeking=false、error=null，exit0。证据在`.rscode/visible-decode-49210-0.{png,json}`与`49211-0`对应文件。定位到本机D3D11硬件VP9解码路径；尚未确认驱动/GPU内部根因，也未证明默认路径已修复或排除所有编码兼容性问题。不自动切换用户硬件加速，不改编码/FPS/尺寸/超时/重试。
- 新增显式opt-in `TestStudioExecutionLiveTextPlanning`，默认SKIP；以`mode=ro`及query_only读取业务库的现有文本配置，小说/镜头夹具和Skill审计均在隔离测试库。真实小样用已核对的[《西游记》第十四回](https://zh.wikisource.org/wiki/西遊記/第014回)短摘录、人工分镜及明确无动画的代理角色，不冒充自动改编或真实模型资产。`COMFYUI_CONSOLE_LIVE_EXECUTION=1`下运行`go -C backend test ./internal/service -run '^TestStudioExecutionLiveTextPlanning$' -count=1 -v`，exit1：模型首次调用118.071秒成功返回5568字符，接口HTTP422，未产生通过产物。provider原始响应/错误正文未输出以避免泄露私有地址；本次未留足脱敏校验诊断，具体422原因未知，不能声称“真实模型规划通过”。未重试同参调用、未弱化校验、未改系统提示词/Skill。真实小说→导演→浏览器→存档→参考包/视频整链仍未完成；下一轮需先补安全的校验分类诊断再继续，不把文本返回成功当整链通过。独立模型日志为Monitor `work-001a215dcb0c7-307`。
- 最终回归：`go -C backend test ./... -count=1` exit0（service 44.901s，opt-in测试默认SKIP）；独立完整Vitest exit0，54文件/409项；`npm --prefix frontend run build` exit0（4517模块）。`npm --prefix frontend test`仍exit1，Node669/670，Git跟踪门禁拒绝未跟踪的`shotExecution.ts`，其Vitest阶段未执行；没有暂存或提交绕过。串行Monitor `work-001a215de5e57-308`最终exit1保留该npm失败，不以其后独立通过改称全绿。既有大chunk、动态/静态混用、React act/GLTF警告仍保留。
- 本节当时的本地EXE（已由后文VP8兼容修复版取代）：76035072字节，SHA-256 `5de9509c2bd76cb47d3674481fa1246dca5ea5dd718028f6510a7cdc460a1f5d`。先备份旧EXE/嵌入目录到`.rscode/gap-deliverable-backup-20261126-112734`，嵌入新dist并`go -C backend build -o comfyui-console.exe .` exit0；逐资源脚本CURRENT_DELIVERABLE=PASS，93项资源逐字节一致，notices source=dist=embed=exe。windows/amd64、CGO_ENABLED=0、vcs.revision=78b3f90、vcs.modified=true；非干净提交发布版、不代表许可合规。EXE/dist/诊断产物不入库；未启动/重启服务，未提交/推送/部署。
- 来源链仍仅受控provider通过；需ComfyUI的真实消费/生成/回溯本轮均 **SKIPPED（本机无法运行 ComfyUI）**。保留原未验收项，不新增替代任务凑完成。

### 迟到只读报告复核与诊断夹具修正（2026-11-26）

- 逐player复核本地Media事件：默认最终player `9A23F82AFEFD4462BEEB1E1B9EFBF246`的D3D11身份、60秒kSeek和D3DStatus/PIPELINE错误均属同一player；软件最终player的Vpx身份及seek恢复亦有记录。迟到报告“失败路径无落盘/无解码器身份”描述的是更早夹具，已被`visible-decode-49210-0.json`/`49211-0.json`补证取代。没有因该建议重跑seek，也不接受“容器/码流兼容性问题全部排除”的扩大结论。
- 仅修改`studio_execution_live_test.go`：新增测试内固定错误分类白名单，非422/未知内容只输出固定标签，不输出模型正文、错误插值或凭据；7种分类/敏感哨兵回归通过。只读设置连接限定单连接，使connection-local query_only持续生效。失败时也检查来源不变和零任务，不仅成功分支检查。
- 带新诊断的第二次真实小样：同一opt-in命令，Monitor `work-001a215ea14cf-30b` exit1。模型单次162.631秒返回5731字符；实际终止错误是`planning must not create tasks`，分类输出前已被夹具断言打断。复核`newTestDBWithNewModels`发现该导演测试库不迁移Task表，查询错误与非零任务共用该错误文案；**不能据此声称创建了任务，也不能确认本次HTTP状态或复现了第一次422**。源不变断言已执行通过；模型原始正文未输出/落盘，本次结果未验收。
- 修正仅限隔离测试库：在请求前迁移Task表并确认零任务基线，失败响应的固定分类先于其他后置断言记录。新增真实路由/受控HTTP用例，证明未知字段返回422/json_schema、源哈希不变、tasks查询有效且为0；专项exit0（2项PASS、真实模型入口默认SKIP）。未改生产校验或系统提示词/Skill，未改变模型超时/重试。两次真实小样均未完成验收，本轮停止外部模型调用，不追加第三次；修正后的真实运行尚未验收。下一步仍是取得真实422的固定分类，再按对应契约定位，不能凭猜测放宽JSON或资源校验。
- 修正夹具后的`go -C backend test ./... -count=1` exit0（service 43.020s，真实模型默认SKIP）；专项2项PASS与7种分类哨兵检查均未调用外部模型。本小节仅涉及测试与文档，生产源码/dist未变化，无需重建EXE；既有93项嵌入资源与哈希复验PASS。真实小说整链、默认解码和真实ComfyUI来源链状态不变，未提交/推送/部署。

### 视频首段解码故障修复与普通Chrome全片验收（2026-11-26）

- 用户明确反馈旧视频只能播放约1秒即错误。不做seek的`node .rscode/chrome-codec-check.mjs full old64`亦exit1，`PIPELINE_ERROR_DECODE: video decode error!`；首帧可解不能称播放正常，更不能只归因于拖动。复核进程发现先前49210含`--headless=new`：其页面截图和D3D11事件仍可用于诊断，但不能算普通可见窗口验收，先前相关措辞更正。
- 产品修复只改变共享`pickVideoCodec`顺序：优先VP8，仅VP8不可用/探测异常才回退VP9。逐帧时间、2秒关键帧、分辨率、FPS、预算、封装和采样器不变；不关闭硬件加速，不改超时或重试。Vue/React面板同步说明策略，React产物状态显示实际编码，解码错误不再建议“降低FPS”。旧VP9文件不会随升级自动修复，必须重新导出或另做兼容转码；保留旧文件及失败证据。
- 普通窗口重新验收：独立Chrome/158.0.8127.37，CDP49212，启动进程核对headless/禁GPU/禁视频解码/禁视频编码四项均false；`Browser.getWindowForTarget`为normal，`SystemInfo.getInfo`确认GPU compositing/WebGL/video_decode/video_encode均enabled，NVIDIA GTX960、驱动32.0.15.8278。环境证据`.rscode/chrome-visible-environment.json`；不是用无头截图或禁加速的软件profile冒充普通窗口。
- 用两份旧原片的全部768/2880帧，经显式软件解原片→WebGL画布→生产选择器/编码器/封装器输出VP8；编码及播放均在49212普通Chrome。**这是原画面兼容转码，不是从3D场景重新渲染，也不替代完整产品面板/小说整链验收。** 严格串行从0播放至ended后再seek，未降低FPS/尺寸、未漏帧：64秒12fps到64秒/768帧，seek60后61.839秒；120秒24fps到120秒/2880帧，seek60/110后61.7811/111.7927秒；均readyState4、seeking=false、error=null、Media错误0。默认对VP8自动选择VpxVideoDecoder、platform=false，GPU加速仍开启，这不是手动禁用硬件解码。
- Monitor `work-001a2160932b8-31e` exit0；`.rscode/chrome-visible-{encode,full}-production-old*.json/png`保留环境、逐player事件和截图。`node .rscode/chrome-visible-verify.mjs` exit0，`VISIBLE_CHROME_FULL_PLAYBACK=PASS`，逐项断言完整ended帧数/seek/编码/GPU功能与无Media错误，并核对普通窗口产物与已独立全片FFmpeg解码的字节SHA相同。ffprobe分别为VP8/1280×720/12fps/768帧/64秒与24fps/2880帧/120秒；`ffmpeg -v error -xerror ... -f null -`两片均exit0。兼容副本为`.rscode/预演64秒-兼容版VP8.webm`（16772264字节，SHA-256 `fd89be3817d374b9a6736ef16df632391b5d06c52062c2426b95b7a33a876798`）及120秒版（33335876字节，SHA-256 `ae8c96d436fdcdf17fdbf7c9ead922785a21d9866ed6ca43b20eb51b62d39041`），各附独立转码说明，不冒用原VP9来源清单。
- 对照边界：同源重新编码的VP9也有全片/seek通过，不能称“所有VP9不兼容”；观察到旧VP9为full-range、通过的新VP9为limited-range，只是线索，未证明色彩范围、驱动或GPU内部根因。VP8优先是已验证的本机兼容策略，不是所有机型/画面已验收。首轮VP8全片测试被另一并行测试切页而暂停，exit1 `complete playback must reach ended`；改为独占前台串行并增加非隐藏/非暂停断言，不延长原播放时限或自动恢复播放。诊断端口残留旧白名单导致save failed/EADDRINUSE，核对进程身份后只清理自有夹具；两次启动参数检查失败后读取实际开关，才发现headless并补普通窗口，未隐去这些失败。
- 回归：新增编码偏好/回退测试先在旧代码exit1（VP9≠VP8、探测顺序不符），产品修复后Node视频专项11/11；生产逐镜驱动4/4覆盖五尺寸的encoder/manifest/WebM均为VP8；React面板11/11、Vue模板及素材契约5/5。`node frontend/node_modules/vitest/vitest.mjs run --root frontend` exit0，54文件/409项；`go -C backend test ./... -count=1` exit0（service68.641秒，真实模型默认SKIP）；`npm --prefix frontend run build` exit0。完整`npm test`此前的未跟踪`shotExecution.ts`导入门禁仍未解决，未重跑相同确定性失败，未暂存/提交绕过，不把独立Vitest称为完整npm test通过。原生workflow仍因Chrome profile LOCK/os error33 blocked。
- 最新EXE已重新嵌入构建：`backend/comfyui-console.exe`，76035584字节，SHA-256 `a89feb7aa507a0e031c25c93a19eb7e9db468341b79fbc699a6c6355a2d42b11`；旧产物备份`.rscode/chrome-fix-deliverable-backup-20261126-120736`。构建与`node .rscode/verify-gap-deliverable.mjs`均exit0，CURRENT_DELIVERABLE=PASS/93资源逐字节一致/notices四处一致；windows/amd64、CGO_ENABLED=0、vcs.revision=78b3f90、vcs.modified=true，非干净提交发布版。不启动/重启业务服务，不部署、不提交/推送；源码/测试/文档未提交，EXE/dist/诊断与视频不入库。真实ComfyUI消费/生成/回溯仍 **SKIPPED（本机无法运行 ComfyUI）**；小说整链、跨设备、性能和许可等原未验收项不转为完成。

### 本批用户授权提交与推送复核（2026-11-26）

- 用户本轮明确要求“提交并推送”，授权范围为本批三缺口续作及VP8兼容修复的源码、测试和文档；此前“未提交/不提交”均是当时状态，以包含本节的实际提交及远端同步核对为准，不追认部署授权。精确暂存24个源码/测试文件（含9个新增文件），再包含3份相关文档；排除`.rscode`、dist、EXE、视频、node_modules、业务数据及无关诊断空壳，不修改系统提示词/Skill。
- 两份只读静态复核未发现明确阻塞性问题；复核提醒必须包含新`shotExecution.ts`及测试，本轮已显式登记。暂存范围/常见凭据模式检查PASS，不输出匹配正文；这不是穷尽式安全或功能证明。
- 正式暂存相关新模块后运行项目原始完整命令`npm --prefix frontend test` exit0：Node671/671（Git导入完整性门禁通过），其后Vitest54文件/409项均通过。此前Node669/670失败被实际完整通过取代，不是修改测试、跳过门禁或以独立Vitest冒充。`go -C backend test ./... -count=1` exit0（service91.666秒），真实文本opt-in显式关闭，不调用外部模型或ComfyUI。
- 最新生产源码与上一节成功构建时一致；`git diff --exit-code -- backend frontend` exit0，既有前端build的Monitor最终exit0，93资源的EXE逐字节复验CURRENT_DELIVERABLE=PASS（SHA仍为上一节未提交构建，不冒充本次新提交的干净构建）。`git diff --cached --check`通过。原生ship workflow `flow-1795671796375-11928-3`仍因历史Chrome profile的LOCK/os error33 blocked；本次不声称原生workflow通过，不为提交而关闭用户浏览器或更改忽略规则。
- 真实ComfyUI消费/生成/回溯仍 **SKIPPED（本机无法运行 ComfyUI）**；普通Chrome兼容转码全片通过不替代产品同档重渲/内嵌、小说整链、跨设备、性能及许可验收。提交与推送不包含EXE，不启动/重启服务，不部署。

### 产品面板重渲与执行机位语义续验（2026-11-26）

- 本轮“继续”不构成新的提交/推送授权。产品视频验收针对已提交`524d204`对应dist；随后四个前端源码/测试文件续修与本文档尚未提交。下述最新本地EXE已另行重建并包含续修，但本次64秒视频在续修之前导出，不宣称已在该视频中端到端验证新修。
- 在只托管生产dist的5186隔离服务，用产品载入按钮导入既有合成64镜存档（双胶囊人物、雨0.50、80×80米/64段地形、种子41317），真实点击渲染/下载。无业务后端代理，非GET业务API均403，产物只落`.rscode`且拒绝覆盖；未调用模型或ComfyUI，未入库、创建Scene或生成任务。普通Chrome158.0.8127.37/49212、normal窗口，启动无headless/禁GPU/禁视频加速开关，GPU compositing/WebGL/video_decode/video_encode均enabled，GTX960驱动32.0.15.8278；Media记录编码器VpxVideoEncoder/platform=false，不冒称硬件编码。
- **产品3D重渲64秒档PASS，不是转码**：约64944ms，产品显示“已生成768帧·1280×720·VP8·15.41MiB”。`.rscode/product-64.webm`16158257字节/SHA-256 `e2d02f0311abb2b6b0bb32b9fa7c4afdb940567ade6eb42217830b80ac705809`；原生manifest205272字节/SHA-256 `b2873a905b4c95d1ea4eaf15760aa22085e71e206fc8dc77bcdcb4a5ce12a200`，64镜、768采样、12fps、sourceDurationMs64000。`ffprobe -count_frames`精确断言vp8/1280×720/12fps/768帧/64秒exit0，`ffmpeg -v error -xerror -i .rscode/product-64.webm -f null -`严格全片解码exit0。场景截图能看见山地、雨线及两个角色，非空白页；不以侧栏未显示播放器的场景截图证明播放器可见。
- **普通Chrome内嵌全片仍未通过，停止第三次重试**：`node .rscode/product-long-video.mjs 64` exit1，`true !== false`（`hidden`断言）；重渲产物已成功捕获。`node .rscode/product-inset-playback.mjs 64`将内嵌Blob逐字节SHA核对为同一产品文件，再从0重播；每轮主动bringToFront但不恢复暂停、不延长时限，仍exit1于`ordinary Chrome must stay foreground`。末态62.3291/64秒、752帧、丢帧2、paused=true、hidden=true、ended=false、error=null，有kPause而无Media解码错误；Media有空`playerErrorsRaised:[]`字段不等于有错误。隐藏/暂停的具体来源尚未证明，不把猜测的窗口切换或GPU原因称为结论。两份result JSON及playing/scene截图均保留。120秒/2880帧产品同档本轮未开始；跨设备也未验，不拿旧片兼容转码代替。
- 本轮相邻修复：面板计数接受执行机位，预览取共享采样器的本镜开始值（执行轨道优先于记录机位）；清除记录机位明确保留轨道。应用/播放/参考包前对execution actors重新检查真实对象、可见性、锁定及clip。参考包逐镜增加`cameraSource`，汇总允许`mixed`，说明每镜开始取景不等于整镜固定机位；静态来源仍为`viewport-recorded`。manifest版本未变，外部严格枚举消费者需按新增来源值兼容；没有宣称已有全部下游消费者验证。本面板仍不提供逐段轨道编辑，英文错误也未在本轮改写。
- 回归先在旧实现复现4项provenance及5项面板失败；逐文件修后48/48、44/44专项exit0。只读复核未发现阻塞问题。完整`npm --prefix frontend test`首次exit1：Node671/671通过，Vitest52文件通过/2文件失败，412/419通过，7项全部为TestTimeoutError（6项5000ms、1项60000ms），涉及studioExecutionStage与studioRecipeRoundtrip。保留日志，不把测试中间打印PASS误认为最终通过；未放宽超时/断言/测试。关闭自有49212 Chrome、取消自有5186夹具服务后按原命令仅复跑一次，Monitor `work-001a216819ddb-333`最终exit0：Node671/671、Vitest54文件419/419全通过（75.74秒）。未修改超时、并发配置或断言；这支持当时负载相关超时的判断，但不证明具体争用源。没有触碰18000或业务进程。LSP因本机没有typescript-language-server不可用，未安装全局工具，不声称类型检查通过。
- 完整前端回归通过后，`npm --prefix frontend run build` exit0；先备份旧EXE与嵌入目录到`.rscode/execution-camera-deliverable-backup-1795674142436`，再复制新dist，93项逐字节断言PASS，`go -C backend build -o comfyui-console.exe .` exit0。`node .rscode/verify-gap-deliverable.mjs` exit0、CURRENT_DELIVERABLE=PASS，93项source/dist/embed/EXE资源及许可声明字节一致（不等于法律合规）。最新EXE76036608字节，SHA-256 `22c9aba6320179d2d4905487253b772d8a1d9799a8e164d8674a2a7f15d2a1df`；windows/amd64、CGO_ENABLED=0、vcs.revision=524d20447f27c13d01d7eeed46548ce3690de161、vcs.modified=true，是含未提交源码的本地构建，不是干净发布版。本轮未改后端源码，未重跑Go全量测试，Go构建成功不冒充全量测试；此前Go全量通过仍只是历史证据。未启动/重启业务服务；新EXE也未追加产品浏览器端到端验收。
- `git diff --check`及`git diff --cached --exit-code` exit0，暂存为空、受跟踪仅4个前端源码/测试及2份相关文档有改动；未提交/推送。第一次文档校验命令曾因本机PowerShell不支持`&&`而exit1（ParserError）；改用Node显式捕获子进程退出码后文档断言与diff检查均exit0，没有用流水线末命令掩盖失败。原生qa-only仍blocked（历史Chrome profile LOCK/os error33），不宣称workflow通过。
- 真实小说整链仍未验收且不追加第三次模型调用；真实ComfyUI **SKIPPED（本机无法运行 ComfyUI）**。所有视频/manifest/诊断、dist、EXE不入库，不部署；原32任务中真实待验收项保持未完成。

### 本轮续修的独立提交推送授权（2026-11-26）

- 用户在上述续验结果后明确“提交并推送”，本次仅包含4个前端源码/测试文件与本ADR、待验收清单共6个文件；此前“未提交/未推送”均为授权前状态，以包含本节的实际提交及远端同步结果为准。没有部署授权，不改系统提示词/Skill、业务数据或测试门槛，不提交生成产物。
- 复核本批完整源码diff及既有完整前端测试Monitor的最终exit0，生产源码未再变更，仍为Node671/671与Vitest54文件419/419通过的版本。本轮仅补交付授权文案并逐文件校验；EXE不额外重建，仍使用上节记录的提交前本地产物与哈希，不冒充新提交干净构建。
- 新建原生ship流程`flow-1795674753430-11928-6`在review_gate再次因历史`.rscode/gap-video-profile/Default/Extension Rules/LOCK`、os error33而blocked；保留该状态，不改忽略规则或关闭用户浏览器规避。不把独立提交推送核验称为workflow通过；64秒内嵌全片两次hidden失败、120秒产品未验收及ComfyUI SKIPPED的边界不随提交改变。

### 用户六镜竹林样片与白模通道修复（2026-11-26）

- 本轮基线已核对为`376d398`（上一批6文件已提交推送）；新的样片/修复没有新的提交推送授权，不提交、推送或部署。用户标题写30秒，但六镜原区间共25秒，保留0–5/5–10/10–14/14–18/18–22/22–25秒，不擅补5秒。人工用现有程序化素材/动作搭建，非真实小说→模型导演自动规划，也未修改原稿、系统提示词或Skill。
- 实测发现真实缺陷：视频UI的`white`直接传到仅识别`clay|normal|depth`的参考捕获器，原先会落入深度分支。Stage仅将white映射clay，其他模式不变，原生manifest仍保留white。新增真实生产回调的3路回归，修前white失败（expected clay/received white），normal/depth通过；修后生产回调与视频驱动35/35通过，没有改测试期望换通过。`npm --prefix frontend run build`最终exit0（Monitor `work-001a216976c66-33b`）；实际产品样片使用本次新dist。
- 隔离5187仅托管生产dist，不代理业务后端，业务写请求403；独立普通Chrome49213，无headless/禁GPU/禁视频加速参数，Chrome158.0.8127.37、maximized、GTX960驱动32.0.15.8278，GPU compositing/WebGL/video_decode/video_encode均enabled。载入61对象：3名具名成年主角骨骼代理、6护卫、46竹丛、帐篷代理、黑石、瀑布及96米/64段水潭地形；环境16时/clear。它们不是成品人物、明甲或服装，不声称容貌、湿发衣物已生成。
- 产品本地保存通过：原生`.rscode/bamboo-roundtrip.json`6102999字节，镜头表、环境、9名角色动画身份及位置字段与作者档一致；不把字段核对冒充该保存档又一次浏览器重开。产品参考包`.rscode/bamboo-references.zip`6760054字节，独立断言18张PNG+manifest共19项、2140×1203、6镜开始秒数0/5/10/14/18/22、cameraSource均execution-track、原计划不变。PNG按参考包原生尺寸，不冒称与视频同尺寸。
- 产品白模3D真实重渲约84.294秒：`.rscode/bamboo-white.webm`20842306字节，SHA-256 `9f943c46001a8bcb85f823ee5bcd46f8cced399d53499c0b58b4050bfe9d5cd7`；1280×720/24fps/600帧/25秒/VP8，无音频，不是转码。原生`.rscode/bamboo-white.manifest.json`209969字节、SHA-256 `b7afb8d4e4001752a66f6ee6b81d84cf142fad79e51bec0e6a18599db7b54bb1`，mode=white、6镜/600采样。ffprobe逐帧计数及`ffmpeg -v error -xerror -i .rscode/bamboo-white.webm -map 0:v:0 -f null -`均exit0。实际视频抽出的六镜总览、参考图和原材质视口已目视核对，非空白/深度图；竹林偏稀、第一镜帐篷裁切、末镜头部遮挡及水岸不突出仍不足。
- 第4镜硬要求：原生保存档恢复瀑布真实几何，整个包围盒对两端相同机位的最大前向深度均为−12.993638米，严格小于0，整镜均在摄影机后方；参考包和实际视频16秒画面不见瀑布，原材质视口黑石为黑色。白模统一灰色，不要求黑石仍保留黑色。
- **25秒内嵌从头到尾PASS，但综合脚本仍FAIL**：普通Chrome产品内嵌到25秒/600帧/ended=true/readyState4/seeking=false/error=null/hidden=false，丢帧1；seek7.5后到8.6056秒也通过。随后seek16时hidden=true，Monitor `work-001a216a140b1-33f`最终exit1（true !== false），23秒seek未执行；不自动恢复、不放宽前台断言、不延时、不重跑旧64秒路径。不能将本次25秒通过追认旧64秒两次hidden失败或120秒产品未验收。原始`.rscode/bamboo-product-result.json`保留FAIL，不能只截通过检查点宣称整套全绿。两次更早夹具失败为CDP await作用域SyntaxError与未等保存结束便点击审阅；修夹具后才继续，没有改产品验证门槛。自有49213已关闭（close连接中断exit1，随后端口ECONNREFUSED断言exit0确认），5187已停止，不触18000。
- 原稿动作未完成：原意涉水计划实际拒绝“第13/15个采样点为水域，不支持涉水”；推进抓腕距离实际拒绝“镜内执行角色间距不足，无法证明安全”。交付保留干岸安全站位/约4.5米双人根节点间距，不减小代理/碰撞半径、降阈值或穿模。午醒、下水涟漪、后仰失衡、冲上抓腕、借力扑胸、面部微表情均未实现；现有Idle/Walk/Interact仅表达站位、走位与动作意向，不能算完整剧情表演。逐镜不足及使用方式附`.rscode/竹林六镜25秒-综合测试说明.md`。
- 关闭自有浏览器后，项目原命令`npm --prefix frontend test`最终exit0（Monitor `work-001a216a51538-341`）：Node671/671、Vitest54文件422/422，未改超时/并发/断言。本轮未改后端，未重跑Go全量；新dist仅用于隔离产品测试，旧本地EXE未重新嵌入白模修复，不宣称EXE代表当前源码。原生qa-only `flow-1795675100169-11928-7`仍blocked（QA requires fresh green project-gate evidence），不宣称workflow通过。
- 真实ComfyUI消费/生成/回溯 **SKIPPED（本机无法运行 ComfyUI）**，未调用外部模型、入库或创建任务。视频/存档/manifest/参考包/脚本/profile/dist保持不入库，原32项真实待验收状态不因这份手工25秒样片而改为完成。

### 核心表演优先：双人腕部接触首步（2026-11-26，未完成整段表演）

- 按用户纠正，先补真实跨角色约束，而不是继续把站位/运镜或播放器测试当作剧情完成。新增显式肩—肘—腕三骨IK与另一角色腕锚点，世界空间残差不超过1e-5米；骨长、绑定身份不变，不可达/奇异/动作改变骨长时拒绝并还原双方。全部角色基础clip先评估，之后统一接触求解，结果不依赖角色数组顺序，乱序seek及stop还原已有实测。它是腕部表面贴合，不是闭合握拳、失衡恢复或扑胸。
- 执行轨道新增前后端一致的contacts契约（version1、整镜时间窗、明确双方/骨链/局部锚点/单位旋转/肘向/法线），无声明的镜头沿用原行为。严格0.7米根间距下限始终保留；只有声明双方可把额外粗包围体clearance转交完整姿态证明，第三者与环境没有豁免。前端专项47/47、Go命令`go -C backend test ./internal/service -run 'TestStudio(WristContact|ExecutionTracks)' -count=1`均exit0，含15类后端非法契约拒绝；不是Go全量测试。
- 安全证据采用`whole-skin-separating-plane-v1`：沿明确腕部法线测量双方全部可见蒙皮/衣物/装饰顶点的凸支撑区间，在实际正间隙中取平行分离面；双方凸包必须严格分离（间隙>1e-7米），锚点距真实三角面仍不超过2毫米。分离面不再强制穿过本就可偏离表面2毫米的锚点；这不是减小网格、提高穿透容差或部位豁免。实例网格、Shader位移、非有限顶点等不支持即拒绝。发布/应用与逐帧派生输出复用同一采样器，以60Hz加beat/clip键/切点预检，上限6000个采样，并记录最大间隔/残差；这是离散证明，不能宣称连续无碰撞。
- **保守边界仍未通过**：真实代理转腕约1.1度时平面会扫到抓取者脚部，已保留分离平面拒绝回归，不缩半径/降阈值换通过。当前只覆盖受限姿势；弯曲闭合抓握需要基于skinIndex/skinWeight的手腕部位级安全证据，尚未实现。真实人物夹具证明移动目标腕贴合、不可达/空气锚点/衣物穿身/骨长变化/动态第三者阻挡均会被拒绝，不能据此把六镜抓腕镜头标为完成。
- 生产v6/v7存档回归发现并修复真实阻塞：GLTFLoader全场景唯一化同名骨骼，导致第二个角色腕绑定丢失。导出克隆现在同时保存明确的原骨名与原骨路径，资源全量校验成功后原子恢复各角色骨名；不猜测后缀，不按相似名称重定向。两个版本均实跑生产保存/导入回调、重新求解恢复后的真实蒙皮、乱序seek与停止还原；专项exit0。另18/18动作存档测试通过，覆盖非法/重复元数据、混合命名冲突与失败不改名。首个夹具曾因Float32的0.2关键帧上舍入超出clip.duration而拒绝，改用精确0.25秒有效资源，未放宽存档门禁；随后骨链缺失红例由上述产品修复转绿。
- **仍未完成**：接近失衡→抓紧牵引→扑胸承接→释放的阶段化表演、重心与支撑脚、涉水河床/水深/入出水过渡、微表情及有许可的成品服装资产。现有人物24根身体骨，无面部骨/morph/服装网格；不得以Interact、改色或通用clip冒充。六镜完整剧情表演保持未通过，未重渲新抓腕成片，也未重新构建/嵌入EXE。
- 存档骨骼身份修复后完整`npm --prefix frontend test`最终exit0（Monitor `work-001a216d3eb6e-34f`）：Node671/671、Vitest54文件446/446；未改超时、并发配置或断言。后端腕部/执行轨道专项复跑exit0（0.213秒），不冒称Go全量或真实ComfyUI已通过。`npm --prefix frontend run build`exit0（Monitor `work-001a216d6a238-350`）；只更新本地frontend/dist，未复制到嵌入目录或重建EXE；Vite构建不等于独立TypeScript类型检查。
- 本批仍未获新提交推送授权，未提交、推送、部署；不改业务库、系统提示词/Skill、超时或测试门槛，不启动ComfyUI或调用外部模型。ComfyUI **SKIPPED（本机无法运行 ComfyUI）**。原生standard-change `flow-1795676880846-11928-9`仍blocked（历史profile LOCK/os error33），独立测试不冒充workflow通过。

### 六秒失衡/回拽动作稿（2026-11-26，历史版本：用户指出并排且缺岸边，剧情验收失败）

- 新增`wristRescuePreview.ts`：仅对本项目已标定的程序化成年代理，人工编排“准备→后仰沉身→伸臂拦住→男子沉身屈臂回拽→晃动恢复”。输出普通骨骼clip及显式腕接触，未接新按钮，也不是通用重定向器或物理动力学。女子头部从约1.58米降至1.266米、后移约0.496米；男子肩腕距离由约0.30米伸至0.382米，再收至0.187米。专项断言这些实际位移、双脚位置误差<3毫米、腕残差<1e-5米、乱序seek及stop还原，未把站位或Interact当作动作完成。
- 首次动作预检因肘部越过锚点平面拒绝；调整目标肘向后又暴露腕部混权接缝使锚点离表面>2毫米，改用手侧真实表面点。随后回拽2.7秒处源肘越过锚点平面约0.14毫米，实际双方凸支撑仍有约0.86毫米正间隙：分离面改为上述实际间隙中点，且源肘明确向自身侧收回。没有调低根间距、缩几何或增加碰撞豁免。新增“越过锚点平面但真实分离可通过/真正凸支撑重叠仍拒绝”回归；首版测试夹具误把局部法线当世界X轴，修正为目标骨旋转后的世界法线，原拒绝断言保留。原衣物穿身、1.1度转腕及第三物体拒绝仍通过。
- 实际共享预检529点，最大采样间隔约1/60秒；最大腕残差2.1502e-8米，最大真实表面距离0.001951米。仍仅是离散几何证明，不证明连续碰撞安全、受力或重心平衡。
- 当前生产dist在无业务后端的5189隔离页实际载入动作档，并原生导出`.rscode/rescue-action.webm`：6秒、1280×720、24fps、144帧、VP8、1306444字节，SHA-256 `a17fe63b6bbd004886e5ebf41d0a73c73cd9adaeba1a6cf02625a98dbee4fe27`。原生manifest保留contacts；内嵌播放ended=true/time=6/frames=144/error=null；ffprobe计144帧和6秒、FFmpeg严格全片解码均exit0。真实0/1.875/4.083秒三帧目视可见后仰与恢复，未从三张静帧宣称全程无穿身。视频与1691494字节可编辑场景已发送；不是旧片转码，不经外部模型。
- 保留失败：首次浏览器演示载入成功后CDP Runtime.evaluate超时，标签页变为about:blank，纯白截图不作为动作证据；重开隔离页后仅执行视频导出成功。该6秒档的浏览器原生再次保存未验证，不宣称往返完成；既有v6/v7腕约束生产回调回归独立保持通过。
- 最新完整`npm --prefix frontend test` exit0（`work-001a216f67dcd-359`）：Node671/671、Vitest55文件448/448。`npm --prefix frontend run build` exit0（`work-001a216f1da9e-356`），仅本地dist。新增动作专项1/1与腕部专项21/21通过。没有改既有测试超时或放宽断言。
- **未通过项保持开放**：整镜腕部已贴合，尚无接触前接近/释放、闭合抓握、扑胸承接、自动重心/物理受力、涉水、微表情或成品服装；当前黑底蓝/粉骨骼代理不能作为成品角色。六镜完整表演仍未完成，EXE未重建/嵌入，无提交/推送/部署；ComfyUI仍为 **SKIPPED（本机无法运行 ComfyUI）**。

### 剧情空间纠正：面对面、女子背临水潭（2026-11-26）

- 用户明确否定上节并排版本：女子在水潭岸边，背后是水潭，后仰有落水风险；男子必须站在正面拦住回拉，而非身旁。技术测试与旧视频解码通过不等于剧情正确。旧`.rscode/rescue-action.webm`仅留作历史失败证据。
- `wristRescuePreview.ts`现将女子置于原点面向+Z，男子置于Z=0.78米、面向-Z；后仰仍朝-Z。重新编排支撑脚、正面前倾/伸臂与脚前拦截点，未拉长骨链；原后仰>0.45米/降低>0.25米、伸臂/回拉幅度与脚锁断言全保留。自由臂向各自身后展开，而非伸入对方侧。
- 增加真实环境高度场：20米/128段，水潭中心Z=-3米、近岸约Z=-0.25米，水底-0.65米、水位-0.18米。与原环境渲染/存档/地面门禁共用同一数据，不用装饰水面冒充。两根位都通过干岸检查，后仰头部地面投影被现有门禁明确判为水域；不会把临水后仰等同涉水已实现。侧向三分之四镜头同时纳入岸线、水面、双方正面关系。
- 真正面对面还暴露了执行采样器180度朝向错误：XYZ欧拉角在四元数还原后可表示为(pi,0,pi)，仅改Y会翻回相反朝向。新增独立红例（期望forward.z=-1，实际+1）；`setActorYaw`改用YXZ分解后写回四元数，保留俯仰/侧倾且不改对象Euler顺序。视口/视频/参考共享修复；13/13几何专项通过。面对面、干岸/后仰投影水域与原动作三项均通过。
- 失败保留：初次对向动作男子右腿需要0.785867米，超过实际0.785米骨长，重编排不交错脚目标后解决；之后检测到女子自由手、再检测到脚部越过腕分离面。未放宽分离面，明确自由臂后展并把正面拦截点放于支撑脚前，配合真实可达前倾姿态。原动作位移、脚锁与严格0.7米根下限、水域/第三者门禁未修改。
- `.rscode/face-bank.webm`为当前生产dist隔离5190实载新场景后原生导出：6秒/1280×720/24fps/144帧/VP8/1697215字节，SHA-256 `1f606f75479da490ec0b8cfa3a010d0b1b528a78758a4abb3821de21c5499667`；原生manifest的男子yaw=pi、location=水潭岸边。播放器time=6/ended=true/frames=144/error=null，ffprobe帧数/时长及FFmpeg严格全片解码exit0。三张真实抽帧已目视确认面对面、女子背临可见水潭、朝水后仰、正面回拉；不是换机位掩盖并排，不从三帧断言全程物理合理。视频、三帧图和1916764字节可编辑场景已发送。
- 新共享预检529点，最大腕残差2.6348e-8米、最大表面距离0.001917米；仍是离散证明。完整`npm --prefix frontend test` exit0（`work-001a21713c675-360`）：Node671/671、Vitest55文件451/451。`npm --prefix frontend run build` exit0（`work-001a21713b1b9-35f`），仅本地dist，不重建/嵌入EXE。
- 六镜完整剧情、闭合握指、接触起止、扑胸、涉水、微表情/成品服装仍未通过；新6秒档浏览器再次保存未验证。无提交/推送/部署、无业务库/提示词/Skill改动、无外部模型；ComfyUI **SKIPPED（本机无法运行 ComfyUI）**。新原生standard-change `flow-1795682840435-11928-11`仍因历史profile LOCK/os error33 blocked，不冒充workflow通过。

### 按小说建景的可靠性首步：人工声明、原文定位与静态几何门禁（2026-11-26）

- 用户明确要求小说建景能力过硬。本轮新增`sceneRequirements.ts`，定位是人工审校的声明式要求，不是自动小说分析器。记录完整审阅原文、来源修订、物件数量/对象ID、near/opposite-sides关系及未解决项。fact要求UTF-16精确区间和quote匹配；assumption须有理由；距离/轴向是人工布景选择，必须写stagingNote。原文匹配只证明引用存在，不能证明语义蕴含、全文完整或美术正确。实际比对的是已审阅导演场次的`Scene.content`，未核验它等于小说原始全文，不能据此追认完整小说来源。
- 几何门禁使用可见真实顶点/实例的世界AABB；near逐对象计算包围盒间隔，避免聚合空洞误判，opposite-sides检查参照物轴向严格分离。缺对象、隐藏、空几何、未知字段、重复映射、未解决项以及不支持的位移/自定义着色/骨骼或实例morph/材质裁切透明贴图都拒绝，不发假证明。返回`coverage=declared-only`，不代表真实曲面最短距离、道路连通性、连续碰撞或流体物理。
- 接入执行应用事务、单独镜头表应用，以及视口/参考包/视频共享采样器的编辑基线预检。只读复核发现“静态要求没有actors时会跳过应用检查”、材质裁切及实例morph漏洞，均先新增失败用例，再修正产品门禁；未降低既有0.7米、水域、障碍或腕部安全要求。采样器初始化检查不等于动态表演全程建景一致性证明。
- `DirectorSourcePanel`新增可选的高级JSON人工审校入口，模板带明确未解决项，不能直接确认通过；改动须重新勾选确认，伪引用/异源/未解决项阻止应用。未附要求仍兼容旧流程，但明确显示“场景忠实度未核验”；已存镜头表显示声明覆盖数，不冒称当前几何通过。本轮没有图形化逐项编辑、自动抽取或自动缺项发现。
- 专项：建景要求14/14、导演来源UI6/6、执行应用8/8、镜头表UI33/33均exit0。新增生产Stage静态镜头表发布门禁回归通过；v6/v7各两轮真实生产保存/导入回调保留来源与人工映射，伪证据导入原子拒绝（3/3专项exit0）。这是生产回调与jsdom证据，不是本轮真实浏览器或小说整链验收。
- 项目原命令`npm --prefix frontend test` exit1（Monitor `work-001a21750ea5b-373`）：Node670/671，唯一失败是`studio-imports.test.mjs`的Git导入完整性门禁，4处导入指向未跟踪的新`sceneRequirements.ts`；其Vitest阶段未执行。未改测试、暂存或提交绕过，不重跑相同确定性失败。独立`node frontend/node_modules/vitest/vitest.mjs run --root frontend` exit0（`work-001a21751999b-374`）：56文件473/473，82.45秒；`npm --prefix frontend run build` exit0（`work-001a21752fc95-375`），4518模块、1分41秒。保留原有大chunk/静动态导入混用及GLTF/WASM警告，Vite构建不冒充独立TypeScript类型检查；只更新本地frontend/dist。`go -C backend test ./... -count=1` exit0（`work-001a217549401-376`，service113.387秒），真实文本opt-in显式关闭，不调用外部模型/ComfyUI。最终diff格式检查及暂存为空断言`DIFF_AND_NEW_SOURCE_INTEGRITY=PASS`；新模块/测试仍为未跟踪源码，不把构建成功当成提交完整性通过。
- **未通过仍保留**：小说自动地点/必要物件/数量/关系抽取、全文覆盖/语义错位/道路与水体连通性检测、真实小说→导演→浏览器→存档→参考包/视频整链；六镜整段表演、握指/接触起止/扑胸、涉水、微表情及成品服装不因本轮关闭。不调用外部模型/ComfyUI、不改系统提示词/Skill或业务库；未提交/推送/部署，EXE及后端嵌入目录未更新。ComfyUI **SKIPPED（本机无法运行 ComfyUI）**。原生standard-change `flow-1795686341387-11928-14`仍blocked（profile LOCK/os error33），独立测试不等于workflow通过。

### 本批建景与表演源码的提交推送授权（2026-11-26）

- 用户在上述结果后明确“提交并推送”。范围为当前25个相关文件：2个后端源码/测试、21个前端源码/测试（含新增`sceneRequirements.ts/.spec.ts`与`wristRescuePreview.ts/.spec.ts`）和2份文档；此前“未提交/未推送/新模块未跟踪”均为授权前状态，以包含本节的实际提交及远端核对为准。排除`.rscode`、node_modules、dist、EXE、业务数据、截图视频，不部署或重建EXE。
- 两份只读复核未发现新增实现阻塞，提醒的4个新文件已精确登记；暂存25文件范围、生成物排除、常见凭据哨兵（不输出正文）与diff格式断言通过。这不是穷尽式安全证明。未改系统提示词/Skill、超时/并发/测试门槛。
- 正式登记新增模块后，原命令`npm --prefix frontend test` exit0（`work-001a2175f5774-37b`）：Node671/671，随后Vitest56文件473/473（94.60秒）；Git导入完整性门禁实际通过，先前670/671失败记录保留为历史，不再作为本次最终状态。生产源码未再变更；上一节生产build及Go全量exit0仍适用，本轮只补授权/交付文档，不重建或嵌入EXE。
- 原生ship `flow-1795688330990-11928-15`在review_gate仍因历史Chrome profile LOCK/os error33 blocked；不关闭用户浏览器或修改忽略规则规避，不宣称workflow通过。小说自动理解/全文覆盖、六镜完整表演、浏览器整链及其余待验项保持开放，ComfyUI **SKIPPED（本机无法运行 ComfyUI）**。

### 漫剧Scene与完整Studio的第一阶段整合（2026-11-27）

- 用户确认实施最小闭环，不重做小说理解、不合并Vue轻预演与React完整编辑器。从场次卡片进入`/studio/scene?project_id=…&scene_id=…`，严格校验成对正整数来源，返回原项目；懒加载期间来源改变或卸载时拒绝迟到挂载。
- `SceneArchive`新增可空索引`scene_id`，沿用创建/列表端点及既有迁移机制。创建与场次筛选均检查项目归属和未归档场次；旧项目级/未归属存档兼容。关联是**历史来源，不是当前存档指针或原文忠实度证明**，不改变v6/v7正文契约，也不猜旧档来源。进入Studio只打开本场次历史列表，仍须人工预览确认恢复；只有成功且未过期的恢复才回填来源。前端拒绝未确认场次关联的保存回执及异场列表，提示已上传存档ID，不自动重传。
- 复用`location-generations`：关联场次预填且锁定；人工选择Shot来源并确认当前机位，捕获白模或干净视口PNG，固定为有序参考1；额外人物身份/服装/美术图和提示词独立审阅。捕获图不可变，模式变化清空快照与确认；生成及采纳分别显式点击，不覆盖`Scene.image_prompt`或已有参考选择。Shot仅来源关联，产物仍是**场次待审候选，不是独立逐镜成片**。选择Shot不会自动切相机，当前仍由人核对镜头表与机位对应。
- 捕获当前预演帧不能先停止播放退回人物/相机基线；白模路径同步替换材质并复制画面，在PNG异步编码前恢复现场。相机/画幅元数据取自捕获时刻，`camera.reference_pass`通过既有来源快照保留；只表示消费的图像类型，不冒称深度/法线/视频已约束模型。复核发现92mm有效焦距被旧取景路径夹到15° FOV（实际约14.86°），先补红例，再让取景范围与12–92mm控件一致。
- 专项信号：关联/跨项目/归档拒绝及白模Shot来源/有序参考/不改场次状态的隔离Go测试通过（`go -C backend test ./internal/service -run 'SceneArchive|StudioLocation' -count=1` exit0）；真实React控件6/6、生产Stage关联保存/列表/成功恢复/当前帧捕获4/4专项exit0。Stage测试以真实生产回调、真实Three对象和受控渲染器/网络执行，不是本轮真实浏览器/真实生成验收。92mm红例失败信号为渲染FOV15与来源14.862815942345014不符，修复后专项通过。
- `go -C backend test ./... -count=1` exit0（`work-001a21aae22e0-388`，service63.297秒），真实文本opt-in关闭。最终生产构建`npm --prefix frontend run build` exit0（`work-001a21ab82d3e-38d`，4518模块、1分3秒），仅更新本地frontend/dist，保留既有构建警告，不冒充独立类型检查。
- **最终全量仍未全绿**：`npm --prefix frontend test` exit1（`work-001a21ab47569-38c`），Node673/673；Vitest56文件478/479、130.88秒，唯一失败是新增viewport当前帧捕获集成用例`Test timed out in 5000ms`。此前与build并行的一次也是5000ms超时，但发生在新增关联存档用例。两次均为整套运行的集成回调超时，专项4/4通过；不据此归罪产品或宣称通过，耗时根因尚未定位，不改超时/并发/断言、不作第三次同类重试。完整npm失败是本批尚未关闭的交付阻塞。
- 最终18个相关源/测试/文档文件范围检查、无生成物/暂存、HEAD仍为`165d80f`、常见凭据哨兵及`git diff --check`通过（`SCENE_STUDIO_SCOPE=PASS`）；不是穷尽安全证明。
- 验证过程保留：一次`npm exec`吞并Vitest参数导致误用仓库根配置，改用项目已有Vitest入口/配置；初次完整npm因旧源码提取器不认识新签名/分页函数而失败，保留全部还原约束并将分页改为实际query断言，宿主保留原卸载守卫再加来源票据守卫。后续一次与构建并行的全量npm为Node673/673、Vitest478/479，唯一失败是新增存档测试5000ms超时；不改超时/并发门槛，改为不与构建并行运行。新增回调测试夹具缺相机字段/沙箱依赖的问题已补齐真实依赖，未降低行为断言。
- 小说自动理解/全文覆盖/连通性、六镜完整剧情表演、真实浏览器整链和性能/跨设备/许可仍未验收。未调用外部模型或ComfyUI，未写业务库、改系统提示词/Skill、提交/推送/部署或更新EXE/后端嵌入dist。ComfyUI **SKIPPED（本机无法运行 ComfyUI）**。原生standard-change `flow-1795742566876-11928-17`仍因profile LOCK/os error33 blocked，独立回归不代表workflow通过。

#### 5000ms超时分段诊断（用户要求“找原因”）

- 未作第三次无观测重试：临时Vite转换器只在内存给原spec添加分段计时，未改原spec/生产源码、5000ms超时、并发或断言。专项4/4 exit0：关联存档1050ms，其中4次Node/esbuild调用1017ms、两次GLB导出共14ms；viewport捕获691ms，其中3次Node/esbuild调用687ms，捕获准备结束689ms、编码兑现690ms。
- 默认全套Vitest计时实验`node frontend/node_modules/vitest/vitest.mjs run --root frontend --config ../.rscode/scene-timeout-profile.config.mjs`（`work-001a21ac10885-393`）exit1：56文件478/479、103.03秒，唯一失败为关联存档`Test timed out in 5000ms`。计时抓到该用例5746ms，其中4次同步Node/esbuild子进程797+2349+1918+635=5699ms（99.18%，机器归因断言`TIMEOUT_ATTRIBUTION=PASS`），GLB导出14ms。直接原因是`studioRecipeRoundtrip.spec.ts`的`callback()`每次都`execFileSync`启动Node/esbuild，编译准备被计入用例预算；全套执行时该开销放大并超过5000ms。此观测不证明此前viewport超时的逐段分布，也未区分底层CPU调度/磁盘争用，不能据此认定产品代码已无问题。
- 当时仅定位，未实施修复或关闭全量阻塞（后续修复结果见下节）。修复方向是复用编译服务/按生产源码缓存编译结果并保持每次独立scope和全部行为断言，不是调大超时或减少断言。调查workflow `flow-1795745009913-11928-18`同样因profile LOCK/os error33 blocked；上述是独立实验，不宣称workflow通过。

#### 重复编译超时修复与最终回归

- 用户追问后继续完成修复，不再停在诊断建议。只改`studioRecipeRoundtrip.spec.ts`的编译准备：从本spec的字面量回调请求收集生产回调，仍用原提取逻辑读取当前生产源码；在一个Node子进程内批量复用esbuild服务，按完整TS源码缓存不可变JS。每次执行继续`new Function`绑定独立scope，不缓存回调闭包、业务结果或夹具；其他少量非回调片段首次按需编译并同样缓存。没有改生产功能、系统提示词、5000ms/既有显式超时、并发或原行为断言。
- 保留失败尝试：最先尝试原生`createRequire`复用编译器，专项exit1、0测试，仍触发`new TextEncoder().encode("") instanceof Uint8Array`的jsdom跨realm不变量错误；未修改全局Uint8Array/TextEncoder或依赖，退回隔离Node批量编译。新增1项回归验证同源码复用、改变源码得到不同编译结果、真实生产回调在两个scope中保持不同机位和独立调用。
- 专项`node frontend/node_modules/vitest/vitest.mjs run --root frontend src/studio/scene3d/studioRecipeRoundtrip.spec.ts -t "Production callback compiler|Scene Studio integration production callbacks"` exit0：5/5通过、36跳过，用例合计55ms；初始化/编译仍在总运行时间中（9.13秒），不把准备成本藏成产品性能提升。只读复核无阻塞，提醒字面量发现为文本匹配、未来非字面量请求会走按需回退，非回调片段不属于单进程预编译批次。
- **最终原命令通过**：`npm --prefix frontend test` exit0（`work-001a21ad90af8-396`）：Node673/673、Vitest56文件480/480（78.68秒），其中本spec41/41。原5000ms全量交付阻塞现已关闭；前文478/479失败和“未修复”是历史过程，不是当前结果。源码/文档以外未新增交付范围；生产源码未再改动，既有Go全量/生产build信号仍适用，无需重新构建/嵌入EXE。
- 原生bugfix `flow-1795746573578-11928-19`仍被同一profile LOCK/os error33阻塞，不规避锁、不宣称workflow通过。未提交/推送。小说自动理解、六镜完整表演、真实浏览器/模型整链及性能/跨设备/许可等原未通过项继续开放；ComfyUI **SKIPPED（本机无法运行 ComfyUI）**。

#### 隔离生产页浏览器验收与过期状态提示修复（2026-11-27）

- 使用本地5197隔离服务直接加载`frontend/dist`：无真实后端，存档只在内存；除存档上传外的POST全部拒绝，不连接ComfyUI。核实生产页入口「3D Studio · 白模/机位」携带项目7001/场次7101进入完整Studio；本场历史存档列表不会自动恢复最新档。放入立方体、存档、刷新、预览并确认恢复，实际还原1个对象；返回按钮回到来源项目。无效场次参数显示来源错误而不是静默打开其他场次。
- 实际UI检查：机位出图项目/场次锁定来源，人工选Shot7201后捕获白模，显示结构参考1及Scene/Shot来源；输入提示词并审阅后才可生成，修改提示词即撤销确认。切换为干净视口时清空旧快照、撤销审阅、禁用生成。**没有点击生成或采纳**，不是生成结果验收，也不是逐镜成片。
- 浏览器发现真实产品问题：切换模式后旧快照已清，但“已捕获不可变机位图”状态文案残留。先补断言，`locationGeneration.spec.tsx`专项exit1（5通过/1失败，`expected ... not to contain '已捕获不可变机位图'`）；模式切换同步清空message后同命令exit0、6/6，保留所有旧门禁/断言。重新构建exit0（4518模块、52.70秒，原chunk警告仍在），浏览器重载此产物、恢复同档并复测，旧提示已消失，快照/审阅/生成仍关闭。
- API独立断言`SCENE_STUDIO_BROWSER_API=PASS`：唯一写请求为`POST /api/scene-archives`，存档5534字节、来源7001/7101；有按来源过滤列表和读取记录，正文1个对象且含GLB/预览；生成0、采纳0，场次提示词未改。后端业务库未接入。本轮夹具曾把项目详情返回为裸对象、把素材/字幕列表返回为对象而非数组，修正契约后重跑；这些是夹具问题，不是产品修复或产品无缺陷的证明。
- 最终`npm --prefix frontend test` exit0（`work-001a21aea63a5-39b`）：Node673/673、Vitest56文件480/480、64.26秒。仅更新本地dist，不复制/嵌入EXE；未提交/推送。确认5197监听进程命令为本夹具后停止，`SCENE_STUDIO_QA_STOPPED=PASS`；临时服务/截图/缓存不入库。
- qa-only `flow-1795747172686-11928-20`仍blocked（同一profile LOCK/os error33及fresh project-gate evidence要求）；不将独立测试等同workflow通过。真实模型遵循构图/站位、跨镜连续性、真实小说整链与六镜剧情表演仍未验收；真实生成 **SKIPPED（本机无法运行 ComfyUI）**。

#### 人工原文要求的道路连通核验（2026-11-27）

- 用户要求继续后，优先补建景中“路看似靠近两处地点、实际断开”的缺口。扩展现有可选`sceneRequirements`的`path-connected`关系：subject为道路物件，reference/other为起终点地点，points为人工有序世界XYZ折线，maxEndpointGapMetres为人工端点接近距离（0–2米）。继续保留fact精确UTF-16引用、assumption理由、stagingNote和未解决项；**不自动抽取小说、不自动补路、不修改模型提示词或契约**。
- 几何检查读取真正绘制的静态不透明三角，支持世界变换/实例、材质分组与drawRange；不以根位置、聚合AABB或navigation元数据证明路面存在。逐段裁切三角的XZ覆盖区间并合并，检查整条声明中心线有路面支撑，Y误差带固定2厘米，单段累计数值缝隙容差1微米。端点到真实目标三角的距离不能超过人工距离，空心目标的聚合包围盒不算表面。
- 道路检查与人物导航分开：第三方障碍作连续XZ包围盒检查（0.35米边距）；起终点地点允许末端表面接触，但各段内部仍与地点包围盒检查，不整体排除。远处骨骼/普通morph角色只按当前姿态计障碍，不能拿来当静态原文物件证据。未知着色位移/泼溅/实例morph等无法证明时失败关闭；保守包围盒可能误拒空心地标/复杂布景，不声称所有素材兼容。
- 环境从应用与共享采样器传入，复用`proveWalkableLine`的水域/坡度门禁；另以最多0.25米间距检查离地/埋地（0.1米容差）。水域/坡度仍是既有最多400点的取样，不是连续地形证明。该关系只证明声明的静态中心线及目标接近，**不证明路宽、整身通行、路面同组构件无遮挡、动态碰撞、涉水/桥梁或水体连通性**；既有0.7米人物根间距、人物水域/障碍、腕部与60Hz门禁未降低。
- 有限工作量：最多256场景对象、8192可见节点、262144实例加权顶点（先于全顶点扫描），65536绘制三角、512声明段、1000000三角/端点比较、单段4096离地点；超过就拒绝，不截断。高级JSON入口显示格式示例与能力边界，不自动把示例写入要求；编辑仍撤销应用确认。旧的无要求/near/opposite-sides与v6/v7存档继续兼容。
- 先红后绿：最初路径用例因尚不识别关系而失败；测试夹具曾误用地形构造函数、分段下限和水面地表契约，修正夹具而非降低产品校验。环境未传入的集成红例修复；两轮只读复核指出不可见colorWrite、端点贴合误拒、远处骨骼误拒、预扫描预算及累计微缝问题，逐项补红例后修复，保留全部旧断言/超时。最终建景29/29，连同采样器/导航专项44/44 exit0；人工控件7/7 exit0。v6/v7真实生产保存/导入回调各两轮保留路径/原文并再拒绝几何漂移，`CONNECTED_ROAD_ARCHIVE=PASS`，连同旧建景回调专项5/5 exit0；这是jsdom/生产回调证据，不是本轮真实浏览器整链。
- 最终原命令`npm --prefix frontend test` exit0（`work-001a21b204964-3a4`）：Node673/673、Vitest56文件498/498、81.47秒；随后串行`npm --prefix frontend run build` exit0（`work-001a21b220982-3a5`）：4518模块、1分8秒，既有chunk警告保留。没有独立类型检查（本机缺typescript-language-server，未安装新依赖），没有把上轮浏览器验收冒用为新增道路关系的浏览器证据。当前共23个受跟踪改动文件（含前轮18个，本轮新增5个受跟踪文件的改动），生成物/业务数据不在差异内、暂存为空、HEAD与origin/main不变，`DECLARED_ROUTE_SCOPE=PASS`及`git diff --check` exit0。
- 新standard-change `flow-1795750497701-11928-21`仍被历史profile LOCK/os error33阻塞，不规避锁、不冒称workflow通过。用户已明确不要主动生成EXE，本轮不生成/嵌入EXE、不写业务库、不调用外部模型、不提交推送或部署；真实生成 **SKIPPED（本机无法运行 ComfyUI）**。自动小说理解、全文缺项/错位、道路自动生成、水体连通、六镜剧情表演及原有性能/跨设备/许可待验项仍开放。

#### 道路关系的隔离生产浏览器验收（2026-11-27）

- 直接服务上述4518模块的生产`frontend/dist`，5197内存API、不代理业务后端。人工编写短句“军营外的小路通向潭边。”及3项物件/1项path-connected关系；场景为两段路面与两个地点标记块（4对象）、1镜2秒、无角色/对白。它是**人工测试标记，不是自动小说理解、成品军营/潭水或剧情表演**。隔离夹具/GLB/报告/截图均不入库；生产代码、本轮测试断言及超时未改。
- 浏览器列表选择#9001并确认恢复，显示“已载入场景（4个对象）”及已保存原文建景声明；截图`screenshot-1795755399375.png`实际看见两段相接路面与蓝/绿标记块。展开“本地程序化素材”后连续逐镜播放完成且还原；人工确认参考包后页面显示“1镜×3张+manifest.json（109KB）”，只触发本地下载。视频实际输出24帧、1280×720、VP8、0.01MiB并出现WebM下载链接。帧数表明实际使用12fps；用键盘尝试选1fps未生效，不把它当成1fps验收。未独立解压核对下载文件，也未做视频逐帧解码/seek质量验收。
- 通过真实对象列表选“右半路面”，把变换Z从0改为2：分别点击播放、已确认的参考包导出、视频渲染，均得到“建景道路路面不连续或坡度不支持：camp-to-bank 第1段”；视频面板清除旧产物、显示本次失败。截图`screenshot-1795755610981.png`实际显示两段路面错开及视频错误；该图中底部旧“视频已生成”是先前成功状态，不能当本次导出成功。
- 在断路状态点“存到后端”，仅写内存存档#9002；独立HTTP读取断言`DECLARED_ROUTE_BROWSER_ARCHIVE=PASS`：4对象，右路面位置[1,-.05,2]，整份scriptShots（包括原文、绑定、路线和来源）与原夹具完全相同。刷新页面、选择#9002、确认恢复后，播放仍被同一断路门禁拒绝；人工将右路面Z改回0后再次播放完成并还原。保存允许保留待修复场景，不等于几何审查批准。
- 最终API断言`DECLARED_ROUTE_BROWSER_API=PASS`（exit0）：唯一写请求为1次`POST /api/scene-archives`，#9002确实重新读取；生成0、采纳0、素材入库0、场次提示词未改，业务库未连接。核实监听进程命令行后停止，`DECLARED_ROUTE_QA_STOPPED=PASS`及`DECLARED_ROUTE_QA_PORT=PASS port=5197 listener=none`，验收tab关闭。
- 保留夹具失败：初次GLB导出用Group多套一层根节点，违反v6顶层对象归属契约；改Scene后GLB从7916变7880字节，但取消Monitor未终止监听子进程，第二次仍拿旧GLB。两次同错后停止重复载入，HTTP比对明确local/remote不同、核实并停止旧夹具进程，再启动并先断言`QA_SERVED_ARCHIVE=PASS exactLocalMatch=true topLevel=4 bytes=7880`才恢复验收。这些是夹具/进程管理问题，不记作产品修复或产品无缺陷证明。
- qa-only `flow-1795755038227-11928-22`仍blocked（`QA requires fresh green project-gate evidence`），以上独立浏览器证据不是workflow通过。只关闭这个人工静态道路中心线的隔离UI门禁/存档复核项；真实小说→导演→布景→表演→参考/视频整链、路宽/整身通行、连续地形/水域、成品视觉及原性能/许可项仍开放。真实生成 **SKIPPED（本机无法运行 ComfyUI）**。未生成/更新EXE、未复制嵌入dist、未提交推送部署。

#### 逐镜操作过期反馈修复及隔离复测（2026-11-27）

- 修复上节截图暴露的旧成功提示：`playScriptShots`、`exportShotReferencePack`、`exportScriptVideo`通过busy守卫、调用`stopAnimation()`后，同步清空status和error，再检查视口或进入异步工作。必须在停止之后清空，因为停止旧预演本身也可能发布状态；busy时不动正在进行的操作反馈。不改几何、安全、采样、导出或存档契约。
- 新增12项真实生产回调测试：三入口×视口未就绪/缺机位/断路拒绝，以及三入口busy保护。先红（例如`expected '已停止旧预演' to be ''`），修复后专项12项通过；与既有掉帧回归合跑13/13、42跳过，exit0。旧掉帧测试原先“不调用setError”的期望与现在显式清除旧错误冲突，改为每次播放只调用一次且参数为null，仍拒绝任何实际错误；第二轮因两次播放的mock累计计数失败，已在每次尝试前清计数，没有减少掉帧、姿态、位置或还原断言，也未改超时。
- 原命令`npm --prefix frontend test` exit0，`work-001a21b6a32b5-3ac`：Node673/673、Vitest56文件510/510，Vitest96.56秒；之后串行`npm --prefix frontend run build` exit0，`work-001a21b6e6710-3ae`：4518模块、67.59秒，保留既有chunk警告，仅本地frontend/dist。没有独立TypeScript类型检查。
- 用新生产dist、5197内存API、同一#9001人工4对象/1镜2秒道路夹具复测，先HTTP精确比对本地存档。视频先成功24帧/1280×720/VP8/0.01MiB，右路面Z改2后视频拒绝，旧视频产物与底部旧成功消失；截图`screenshot-1795756750748.png`已人工看图核实两段错开的路面及视频错误。随后各自修回Z=0取得播放结束/参考包109KB成功回执，再改Z=2：播放和参考包都仅显示本次道路错误，不残留各自旧成功。最后Z修回0，再渲染视频成功24帧，旧全局错误消失，截图`screenshot-1795756997661.png`看见路面接回、成功回执及本次底部提示。参考包确认框在成功导出后需重新勾选；首次等候播放使用了不匹配的结束文案，读取实际文案后继续。误点未勾选的禁用导出按钮未触发导出，不当产品失败。
- `SHOT_FEEDBACK_BROWSER_API=PASS`（HTTP断言exit0）：本轮API写请求0、生成0、采纳0、素材入库0，原场次提示词未改，未接业务后端。核实监听进程命令行后停止，`SHOT_FEEDBACK_QA_STOPPED=PASS port=5197 listener=none`，tab关闭。截图/夹具/构建产物不入库；本次只验状态和门禁，不验视频解码/seek、参考包解压、自动小说理解或剧情表演。
- bugfix `flow-1795755963948-11928-23`仍blocked：历史`.rscode/gap-video-profile/.../LOCK`无法hash（os error33）。不删除/规避锁，不将独立测试、只读复核或浏览器通过冒充workflow通过。未生成EXE/嵌入dist、未写业务库/调用外部模型、未提交推送部署；长期能力未验项保持开放。

#### 逐段骨骼动作人工纠正（2026-11-27）

- 新增`performanceCorrection.ts`及`PerformanceCorrectionPanel.tsx`，接入原镜头表审阅入口：明确选择已有镜头、执行角色、动作段、实际挂载clip，填写1–1000字人工理由，先暂存、再重新确认应用。只改所选beat的animation，不改角色身份、原时间区间/sourceIndex、位置朝向、机位、contacts或sceneRequirements；人工选择不是原文事实。校验实时目录唯一身份、可见/未锁定和clip索引/名称/时长，拒绝未变化片段。追加JSON字符串diagnostics记录前后片段、理由和来源指纹，最多128项且不丢弃旧记录。
- 未暂存输入阻断其他镜头修改和确认应用；存档变化后拒绝旧审阅应用。只读复核发现切档时“放弃”也被禁用的恢复死锁，补回归后分离stale与busy，允许放弃再重新载入。应用成功更新审阅基线，后续修正不误判旧档。暂存不自动播放、保存、导出或调用模型。
- 骨姿专项`MANUAL_PERFORMANCE_POSE=PASS`：实际骨骼改变、另一角色不动、停止还原；核心20/20、核心/面板合跑61/61。真实生产v6/v7保存/导入回调两项通过，`MANUAL_PERFORMANCE_ARCHIVE=PASS version=6|7`，保留完整镜头表/人工理由并重新采样真实骨姿。真实成对蒙皮专项14/14，`MANUAL_PERFORMANCE_SKIN_GATE=PASS`：人工换入不安全转腕仍被原分离平面门禁拒绝，原计划与现场骨姿不变；没有降低骨长、0.7米间距、环境或接触安全阈值。
- 原命令`npm --prefix frontend test`首轮exit1：Node672/673，导入完整性测试发现新文件未登记，Vitest未执行。仅对4个新源码/测试`git add -N`登记（不暂存内容、不提交），原完整性测试1/1 exit0；重跑全量`work-001a21b949198-3b6` exit0：Node673/673、Vitest58文件541/541、67.44秒。随后串行`npm --prefix frontend run build`（`work-001a21b974be0-3b8`）exit0：4520模块、Vite56.45秒，既有chunk警告保留；只生成本地frontend/dist，无独立TypeScript类型检查。
- 新生产dist的5197内存API隔离浏览器：人工1角色/2实际骨骼clip/1镜8秒2段夹具，不是小说剧情。精确比对服务存档后恢复#9001，选择第2段前抬片段、填理由；断言未暂存时确认/时长禁用，暂存后确认撤销，重新勾选后应用。连续逐镜播放期间截图`.rscode/manual-performance-before.png`与`after.png`已实际看图：原双臂下垂，播放中左臂（画面右侧）姿态改变；不是站位/机位代理，更不是抓腕或扑胸证据。停止还原，再通过生产保存写入内存#9002。HTTP精确检查只有所选animation改变，原段/区间/来源与既有diagnostics保留，新增理由记录正确；刷新恢复#9002后，第2段仍显示新clip。再制造未暂存输入并恢复#9001，确认旧审阅不能应用、放弃仍可用，放弃后重新载入成功。`MANUAL_PERFORMANCE_BROWSER=PASS`与`MANUAL_PERFORMANCE_BROWSER_API=PASS`：唯一写请求1次内存存档上传，生成0、采纳0、素材入库0、场次提示词未改、无业务后端。
- 夹具/工具失败单列：首次夹具entries()每次新建对象，违反采样器稳定entry身份契约，修成稳定数组后`MANUAL_PERFORMANCE_QA_FIXTURE=PASS`（GLB284020字节）；浏览器工具原生下拉键盘未生效，改为同一工具自有tab的CDP DOM change事件后完成；初次HTTP审计把diagnostics字符串当对象而取不到reason，按生产JSON字符串契约解析后从已保存#9002续验，不重复保存、不改产品或测试门槛。上述失败不冒充产品修复。
- **能力边界仍未通过**：此入口只替换已有动作段，非完整逐段编排UI；整镜腕表面贴合仍无接近/释放、闭合握指、扑胸、自动重心受力、涉水、微表情或成品服装。通用片段不证明剧情完成；六镜完整表演、小说自动理解/完整建景与真实模型整链保持开放。原生standard-change `flow-1795758155811-218248-1`复核仍blocked（历史profile LOCK/os error33），独立测试不等于workflow通过。未更新EXE/后端嵌入dist、未写业务库/改模型契约或系统提示词、未提交推送部署；真实生成 **SKIPPED（本机无法运行 ComfyUI）**。

#### 人工两阶段动作拆分（2026-11-27）

- 在上述入口增量支持把一个已有beat拆成两个阶段：勾选拆分、显式填写镜内分界秒、选择前后两个实际挂载片段和人工理由；仍须暂存、重新勾选确认后才应用。分界严格在原区间内、两侧均超过1微秒；两个新beat共享原sourceIndex（原本缺失则保持缺失），原起止覆盖、线性根轨迹、yaw、其他角色/镜头、机位/对白、contacts及sceneRequirements不变。分界根位置由原直线插值，不手动猜空间。沿用原每镜总beat≤128与纠正记录≤128预算，超限原子拒绝，不丢旧记录。
- `ExecutionClipCorrection.split`仅为本地人工编辑参数，不进入执行存档schema或模型契约；最终仍是既有ExecutionBeat数组。diagnostics追加`operation: split-beat`、分界、完整原beat和两段结果及原人工理由/指纹。两个片段**各自从零秒播放**，短片段停末帧、不拉伸/循环/混合；即使选择相同clip也会重启，并不保证分界骨姿连续。界面明确提示可能跳姿、整镜接触不会释放，不把拆段当成接近/释放/闭合握指或剧情完成。
- 先红后绿：核心新增10项在旧实现exit1（同片段误拒、无分界/后段资源校验），修后核心30/30；UI新增2项先因不存在拆分控件而exit1，接入后面板10/10、连同旧面板/核心73/73 exit0。覆盖显式双clip/分界、非法时刻、根插值、缺sourceIndex、预算及未暂存封锁/重新确认/放弃恢复。只读复核未发现具体新增阻塞，但指出分界实际骨姿和contact分界覆盖不足，随后补真实采样回归。
- v6/v7生产保存/导入回调拓展为普通替换/拆分共4/4，55跳过，exit0：`MANUAL_PERFORMANCE_ARCHIVE=PASS version=6|7 split=true|false`。真实蒙皮胸骨在分界前、恰好分界、分界后和乱序返回时符合相位，保存重开后理由/sourceIndex/完整计划保留，停止还原。成对腕部专项15/15 exit0，`SPLIT_PERFORMANCE_SKIN_GATE=PASS`：分界前后与恰好分界均保持真实腕表面贴合，不安全后段仍被原分离平面拒绝，原计划/现场不变。首次新增不安全夹具只有0.2秒末键不安全、而实际后段仅0.15秒，断言预期错误（exit1，expected function to throw）；改为从后段本地零秒就不安全的恒定姿态后通过，未改产品门禁、拒绝断言或既有测试。
- 全量原命令`npm --prefix frontend test` exit0（`work-001a21bb82085-3be`）：Node673/673、Vitest58文件556/556、96.96秒。随后串行`npm --prefix frontend run build` exit0（`work-001a21bba31e8-3bf`）：4520模块、Vite1分18秒，保留既有chunk警告；只写本地frontend/dist，无独立TypeScript类型检查。
- 生产dist/5197内存API隔离浏览器复测人工1人/2clip/8秒档：恢复#9001，拟拆2–8秒段，分界8秒被拒绝且保持dirty；改为5秒、前段静置/后段前抬，暂存撤销确认、重新确认应用。真实连续播放截图`.rscode/split-performance-first-phase.png`及`second-phase.png`已实际看图：先双臂下垂，后左臂（画面右侧）姿态改变，根位置与取景未替代骨姿；只是人工代理动作段证据。停止还原、生产保存内存#9002，HTTP精确对比三段为0–2/2–5/5–8秒，原sourceIndex及其余镜头字段保留、人工拆分记录正确；刷新恢复#9002仍显示后段新clip。只勾拆分也算dirty，切回#9001后旧审阅应用禁用，允许放弃重载。`SPLIT_PERFORMANCE_BROWSER=PASS`及`SPLIT_PERFORMANCE_BROWSER_API=PASS`：唯一写请求为1次内存存档上传，生成/采纳/素材入库均0，场次提示词未改、无业务后端。核实命令行后停服务，`SPLIT_PERFORMANCE_QA_STOPPED=PASS port=5197 listener=none`，tab关闭；夹具/截图/缓存不入库。
- 本批仅扩展人工阶段编排，不称完整剧情：无自动动作合成/连续混合、无接触起止/闭合握指/扑胸、重心真实受力、涉水、微表情或成品服装；小说自动理解/完整建景、六镜完整表演及真实生成整链仍未通过。原生standard-change `flow-1795760950298-218248-2`在adr_signals仍blocked（历史profile LOCK/os error33），不规避锁、不用独立证据冒充workflow通过。未生成EXE/复制后端嵌入dist、未写业务库/修改提示词或模型契约、未提交推送部署；真实生成 **SKIPPED（本机无法运行 ComfyUI）**。

#### 离线原文建景候选与精确定位（2026-11-27）

- 新增纯函数`sceneryCandidates.ts`，仅接收本次快照的`scene.content`；不从标题、location栏、导演镜头/动作/机位补充取小说事实。不增加第二套模型分析链、不改已有小说理解、提示词或执行契约；这里只辅助原文人工审校。输出`lexical-candidates-only`，始终标“未完成审校”，不产生可执行`SceneRequirements`、对象绑定、距离、轴或道路坐标，不清空unresolved或改变应用确认。
- 固定词表以最长非重叠词识别地点/物件的每次提及，裸提及数量未知、不默认1；仅明确“数词＋量词＋词表名词”给出字面数量，保留量词，不把一片竹林当一株竹子。支持常用中文1–99十进制写法和阿拉伯正整数至4096；范围、模糊、限定、分配、小数/分数/负数/序号及不支持数词交回人工。句级否定/条件/转述/时间风险和跨句引号状态保守标疑；重复提及不合并身份、不累加，不同数量提示数量或身份冲突。
- 局部明确端点支持“在/位于…附近/近岸/对岸”“通向/通往/连接”；每端点指向独立提及，不解析代词，不将对岸自动转成两个主体的opposite-sides，不把通向当成真实可通行证明。识别到空间词但未覆盖句式/端点时提示缺项；同分句已有一条成功关系也不掩盖其他未解析线索。
- 全部证据与语境是原始UTF-16 `[start,end)` 切片，保留emoji、CRLF、空白及重复引用的独立偏移；无trim、归一化或模糊替换。来源快照读取后即可展示，不依赖模型规划/视口资源；点击定位自动展开、聚焦并高亮只读`pre`的原文，避免textarea换行归一化损坏偏移。切项目/场次/禁用清除，迟到读取忽略；规划时快照仍在则候选仍在，定位不改变人工JSON、未解决项或确认勾选。
- 预算为65536 UTF-16原文单元、256次提及、512分句及262144单元的输出证据预算（重复语境按每次输出计入，并预留固定字段开销）。超限整体返回`unavailable`和明确提示、无部分候选，不能以截断结果宣称覆盖。零命中仍显示词表与语义覆盖有限，不能当成没有布景要求。
- 先红后绿：缺模块首轮exit1；核心28项中1项是测试把数组元素完全匹配误当子串匹配，修正该新测试的匹配方式、保留未知数量断言后28/28。补小数/负数/序号、跨逗号条件/对白与分句预算后37/37。只读复核发现“至少有”等限定漏判、逗号后空格数值、拒绝数值只高亮尾数、重复长语境输出膨胀及同句关系缺项掩盖；新增23项先红22项（另1项原已正确），修后核心60/60。复核确认上述具体修复，无新增阻塞；不把静态推演当实测。面板从不存在候选区的5项失败接入至15/15；原文定位展开/聚焦断言先红后修，联合既有建景契约104/104 exit0。
- 全量`npm --prefix frontend test`最终exit0（`work-001a21be4ac39-3cb`）：Node673/673、Vitest59文件624/624、74.68秒。初轮增量全量599/599曾通过；复核修复后的首轮全量exit1（623/624，新64k原文夹具按固定15ms等待而未等异步快照完成，`Cannot read properties of null (reading 'textContent')`）；仅改该新夹具为等待可观察来源快照的`vi.waitFor`，不放宽断言/产品门禁，专项15/15及原全量随后通过。两个新文件仅`git add -N`登记，导入完整性1/1 exit0，无暂存内容。随后串行`npm --prefix frontend run build` exit0（`work-001a21be6aef6-3cc`）：4521模块、Vite1分22秒，保留既有chunk警告，无独立TypeScript类型检查；仅frontend/dist，不更新嵌入目录或EXE。
- 最终新dist/5197只读内存API生产浏览器：原文9次提及/4条关系、非原文100座寺庙与导演500棵树未进入候选、emoji/CRLF与精确切片、高亮定位/展开/聚焦、切场次/项目清除、重读无旧高亮均通过；第二场次5次提及/1条关系，“至少有两块”与“1.5块”数量均未确认，完整高亮1.5而非尾数5，同句关系缺项提示保留。`SCENERY_SOURCE_BROWSER=PASS`、`SCENERY_SOURCE_BROWSER_API=PASS`：写请求0、模型/生成请求0、存档写入0（页面仍读取存档列表），场次提示词未改、无业务后端。`.rscode/scenery-source-located.png`和`scenery-source-qualified.png`已实际看图：左侧候选文本可见并换行，高亮正确；中央空网格**不是已建成场景的证据**。夹具首轮wait返回DOM导致`Object reference chain is too long`，改为布尔条件；最终复验旧tab已失效，严格身份断言拒绝后改用Browser新返回的自有tab ID，不连接其他tab，不改产品。按命令行核实后停本轮服务，`SCENERY_SOURCE_QA_STOPPED=PASS port=5197 listener=none`，验收tab关闭；夹具/截图/缓存不入库。
- **边界与未验收仍开放**：有限词表/句式有漏识别、单字子串误命中和过度保守标疑；例如“附近有两块”可能被近似数规则标成未知，部分“约莫/即使”等语境尚未覆盖，不能把无警告候选当成事实。未实现完整实体消歧、全文缺项/语义完整性、自动场景生成或小说忠实度验收；人工高级JSON仍为真实要求入口。六镜完整剧情表演、真实生成整链、性能/跨设备等原待验项不关闭。原生standard-change `flow-1795762497736-218248-3`仍在adr_signals blocked（历史profile LOCK/os error33），未规避锁，独立测试不冒充workflow通过。未生成EXE/复制后端嵌入dist、未写业务库/修改模型契约或系统提示词、未提交推送部署；真实生成 **SKIPPED（本机无法运行 ComfyUI）**。
