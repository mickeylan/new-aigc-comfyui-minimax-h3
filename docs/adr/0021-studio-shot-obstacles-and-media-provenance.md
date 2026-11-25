# ADR-0021：逐镜障碍拒绝与派生视频来源关联

状态：用户已选择「先记录再实现」；实现和验收进行中，不代表真实生成联调通过。

## 范围与既有边界

延续 ADR-0020 前五项迁移。只扩展现有 Studio、采样器、素材上传入口，不引入框架、第二上传通道、自动生成或自动关联。不提交、不推送、不部署、不接触18000与业务数据库；测试仅用内存/临时目录。源库只读。

## 决策

1. 人工逐镜走位仍是原起终点定义的XZ直线，遇静态障碍显式拒绝，**不隐式绕行**。播放、视频和参考包在任何临时位移前调用同一检查。既有地面路径编辑器的显式自动绕行功能保持不变。
2. 障碍按编辑基线的世界XZ包围盒保守检查，外扩0.35米；隐藏对象、splat不参与，锁定对象仍阻挡。排除整份镜头表所有走位角色（避免自身阻挡）；不证明动态角色间碰撞。大面积地板等也可能保守拒绝，错误应指出障碍对象，用户调整布景或走位，不静默忽略起终点内的盒子。
3. 包围盒收集留在Three.js场景模块，纯几何线段检查留在无Three依赖的ground-path。障碍快照只在会话启动时生成；场景改变仍按原有来源指纹失效。不是寻路、不是精确网格碰撞、不是脚底IK。
4. 视频派生清单增加artifact UUID、采样器版本、动作目录修订号和障碍检查摘要，保留完整镜头表、环境、源场景指纹和输出参数。目录修订号只作溯源；回放仍用存档完整轨道，不依赖运行时目录解析。UUID不是内容哈希或真实性证明。
5. 沿既有POST /api/materials增加可选`derivation_manifest` JSON文本，与视频文件一起显式入库；Material增加同名可空文本列，由现有AutoMigrate创建。旧上传不传该字段，旧素材保持空值，API增量兼容。新增清单必须在落文件前验证类型、版本、尺寸预算及必要字段；非法输入400且不写文件/DB，不允许客户端清单作为服务端命令或可信生成证明。
6. 清单与素材记录一次Create保存，列表返回清单便于后续来源追溯；不允许静默忽略清单并向用户宣称来源已保留。前端核对返回artifact ID，旧后端未保留时提示已上传但追溯未确认，避免重复上传。人工Scene关联和真实目标生成工作流能力保持独立验收，不能以素材入库代替。
7. 不改变已有素材文件生命周期，不增加自动重试或超时。不为审计引入256MiB整块哈希内存复制。保留32MiB场景上限与视频2880帧/256MiB限制。
8. 已选择3D参考视频时，专用模板缺失、禁用、损坏或不兼容都明确拒绝；禁止回退到不带该视频的模板并静默丢弃人工选择。
9. 视频输入按上传登记解析：当前项目`material_p<ID>`优先，其次共享`materials`；不读取其他项目同名输入，文件名拒绝路径逃逸。
10. 生成快照复制素材ID与经验证的派生清单，后续素材修改不改变已存来源；这是声明留存，不证明真实生成实例消费成功。
11. 256MiB限制覆盖完整WebM（EBML/轨道/Cues开销），在Blob复制payload前检查；测试复用1MiB缓冲，不实际分配256MiB。
12. 通过动作目录新加入的片段，以可选`catalogSource`字段随既有完整轨道存档，保存目录标识、片段ID、修订和许可；旧档缺字段继续读取，不根据动作名称猜来源。来源字段严格校验，但历史正整数修订不要求等于当前目录；回放始终使用存档轨道，不联网、不用当前目录替换。该字段是溯源声明而非轨道未修改的证明；普通外部GLB无此声明。

13. 地形增量接受既有上游字段`surfaceBlend{surfaces,weights}`、`surfaceColors`、`waterColors`，旧档缺字段原样读取；长度、ID、有限权重0–1与六位十六进制颜色严格验证并复制。地表色是全局调色板（原始地面38不可覆盖），水色是有水顶点的稀疏覆盖；混合是每顶点基础/次级地表覆盖率，不等同现代上游`surfacePaint`贴图层。未知现代字段仍拒绝，不能静默丢弃。
14. 新增显式混合笔刷（本笔覆盖权重=径向衰减×强度，不隐式累加第三层）、全局地表色应用/重置、局部水色。替换地表/河道清理该点旧混合，失效水位同时清理水色；只改副本，撤销与既有场景存档共用。渲染对四角基础/次级共八项按平滑双线性权重混合颜色、粗糙度、金属度和自发光，水面独立顶点色。保持现有程序化纹理而非声称迁入上游全部贴图库/凹凸细节。没有混合字段的旧档沿用现有材质路径，避免无关画面变化。首次显式应用混合会切换整片地形为平滑渲染（圆外旧边界/纹理外观可能变化），入口在应用前加醒目提示，撤销首笔恢复原路径；不宣称新旧材质逐像素等价。

15. 大场景先有界测量再优化：本次Profiler+GL方法包装窗口仅用于归因（包装本身降速、不可与静态RAF基准直接比较）。纯网格时不执行空Spark渲染器的全场景遍历；每帧仅按登记对象判定是否存在splat，有任意splat（即使隐藏）继续原路径，不改混合素材行为、模型树、配方版本与导出结构。优化不能替代交互轨迹验收，需同夹具/分辨率独立复测。

16. 生产构建以Vite现有`generateBundle/emitFile`把唯一源文件`src/studio/THIRD_PARTY_NOTICES.md`原样带入dist，复制dist至嵌入资源的既有流程因此可携带声明；不另维护容易漂移的副本、不改变部署。此项只是分发携带门槛，不等于完成全部许可证全文/第三方商业条款审计。

17. 场景载入复用既有代次而不引入第二套状态：在调用本地文件读取前取得代次；只有当前代次能提交或发布错误/清busy，过期操作仍须清理自己的待提交资源并finish。恢复返回布尔结果，失败或取消保留确认窗口，旧预览/迟到成功不得关闭后来窗口；格式和32MiB限制不变。

18. 视频面板的异步工作归每次镜头表会话所有，包括A→B→A：旧导出错误/成功不能写新产物；已发上传不得假装未发生。迟到回执独立标为「先前视频」，保留已知素材ID与来源确认/未确认信息，不给新视频设置已入库状态、不自动重试/关联/生成；卸载后不再写UI状态。

## 验收门槛

- 纯线段障碍（含薄墙、端点位于障碍、非法盒子）、角色自身排除、隐藏/锁定/splat策略、修改前拒绝、播放/采样器一致性回归。
- 真实生成视频清单、前端上传携带及服务端回读；非法清单无副作用、旧上传兼容、临时SQLite往返、数据库重开后来源仍存在。
- 固定分辨率/场景/镜数记录64镜输出数量、总字节、耗时和能获得的内存；规模测量明确硬件、浏览器条件与不可获得的GPU内存，不能外推为生产FPS或显存验收。
- 项目规定前端全量test/build、Go全量test；既有失败须如实报告，不改测试期待掩盖失败。

## 本轮验收记录（2026-11-25）

- 最终前端全量 `npm --prefix frontend test` exit 0：Node **659/659**，Vitest **32文件/268项**（包含本轮目录来源与解码失败新增用例）。Go全量 `go -C backend test ./... -count=1` exit 0；新增交接测试gofmt后专项再运行exit 0。最终 `npm --prefix frontend run build` exit 0（4502 modules），不复制嵌入资源、不部署。
- 真实Go上传/列表/临时SQLite关闭重开断言：`MATERIAL_DERIVATION=PASS upload=200 list=retained reopen=retained invalid=400 writes=0 legacy=200`。来源清单与视频同行入库，非法清单零文件零记录，旧上传兼容。前端另外覆盖旧后端丢失清单时保留已上传ID、禁止重复上传。
- 隔离生产构建5183→18997真实Go素材处理器→18999内存存档夹具；人工载入单镜阻挡档后，产品导出入口明确报「直线走位与障碍「李四」相交」「不会自动绕行」，没有产物。
- 正向64镜：两个人物、雨天、80×80米/64段地形、64秒、每镜均人工绑定走位。浏览器确认导出参考包，独立解包断言 **193条目=192张PNG+manifest**，全部PNG **2280×1282（本次视口原尺寸）**，逐图尺寸与清单相符；ZIP **130609956字节**，记录阶段到捕获的墙钟上界 **230119毫秒**（含人工点击及捕获传输，不是纯渲染基准）。清单有UUID、动作目录修订3、`script-shot-timeline-v1`、`static-xz-reject-v1`及64条可走证明。信号 `BROWSER_PRESSURE64_REFERENCE=PASS`；隔离产物 `.rscode/pressure64-reference-result.json`，不入库。
- 后续隔离5184/专用Chrome验收完成64镜/64秒/12fps：**768帧、1280×720、15963631字节、墙钟70277毫秒**，人工存库成功，信号`BROWSER_VIDEO64_UPLOAD=PASS`；清单205272字节。内嵌播放器实际播放：duration=64、currentTime=1.0061、readyState=4、error=null。产物与结果在`.rscode/continuous-video64-*`，不入库。此前388帧取消记录不再作为此项最终结论。
- 有效探针窗口：viewport=1582×904、DPR=1、canvas=1568×882、页面可见；GPU为ANGLE/NVIDIA GeForce GTX 960/D3D11。播放后RAF约59.95次/秒、p50=16.7ms、p95=16.8ms、观察到的峰值JS堆130212262字节（空载87790544字节）。这是双人物场景的RAF/JS堆窗口，不是1577实例渲染FPS或GPU显存；**峰值显存无对应API，仍不可得**。
- 离线真实处理器链覆盖共享/项目归属上传→Scene人工保存回读→真实文件字节→快照API来源留存，首尾帧与状态不变、tasks=0、providerCalls=0；`STUDIO_SCENE_HANDOFF_OFFLINE=PASS`两种目录均通过。专用模板四种不可用态、跨项目目录隔离、来源不可被后续素材修改覆盖均有自动化断言；未调用真实生成。
- 256MiB编码payload拒绝与包含封装开销的完整产物预算测试通过（视频专项9/9）。前轮隔离页恢复无响应已通过**只重启49200专用headless进程树、使用新测试profile**恢复；未重启业务服务，也不声称已查明卡死根因。
- 最新生产构建的64镜/120秒/24fps上限导出与人工测试素材入库完成：**2880帧、VP9、1280×720、31986399字节、墙钟232633毫秒**；清单与实际文件独立交叉断言`UPPER_MANIFEST=PASS`。`ffprobe -v error -count_frames ...`读取2880帧/24fps/120秒；`ffmpeg -v error -xerror -i .rscode/continuous-1795583431685.webm -map 0:v:0 -f null -`严格全片解码exit 0。
- **浏览器内嵌播放不通过**：最初无用户手势报NotAllowedError；第一次鼠标坐标误点进度条，截图核对后改点播放按钮，实际报`PipelineStatus::PIPELINE_ERROR_DECODE: video decode error!`（验收脚本exit 1）。独立原生浏览器上下文播放又在Runtime.evaluate等待30秒超时exit 1，停止该路径继续重试。FFmpeg通过只能证明该独立解码器可读，不证明Chrome播放能力；尚未确定浏览器/驱动/资源或文件兼容性原因。面板新增独立解码错误提示，保留视频和追溯下载、不自动重试/上传/生成，新产物清除旧提示；真实error事件自动化通过。
- 121秒/24fps=**2904帧**真实产品入口明确报`最多2880帧，请缩小时间范围或降低FPS`，无视频成品：`BROWSER_FRAME_OVER_LIMIT=PASS`。断言目标为面板`role=alert`，不是帮助文案。初版验收脚本误匹配静态说明，已纠正并重新断言实际错误，不把初版结果作证据。
- **1577实例规模测量已取得，但流畅性未通过**：真实载入/截图可见19组古街（非原上游演示），同一GTX960/viewport1582×904/DPR1/canvas1568×882可见页，15窗口合计30375.2ms，RAF加权**12.7077次/秒**，窗口范围7.49–13.65，窗口p50=66.8–83.4ms、p95=83.5–563.4ms，观察峰值JS堆**363062948字节**。首窗口含恢复后负载，不删慢窗口。编辑器每RAF调用一次render，但此测量不是GPU完成时间或交互轨迹验收；显存无API、不可得。信号`LARGE1577_MEASUREMENT=RECORDED`，不是性能优化通过。
- **目录动作来源闭环**：先记录决策12，再给目录新加入片段保存可选`catalogSource`。真实GLB两轮往返、旧档不猜来源、历史修订保留、非法来源原子拒绝通过（`ANIMATION_CLIP_PROVENANCE=PASS`）。最新构建真实浏览器加载单角色、选择cc0Run、显式加入、保存至内存夹具、读取JSON核对来源/修订3/CC0、刷新后恢复通过：`BROWSER_ANIMATION_CATALOG_SOURCE=PASS archiveId=1000017 clips=1 refreshRestore=true`。旧内置37片段与外部片段不根据同名推断来源。
- OpenCut许可按上游LICENSE补全真实版权行与MIT全文；声明门禁3/3通过。Apache全文与分发包许可携带仍需独立收尾，不以本次补齐一项代表全部第三方许可审计完成。
- 验证过程纠错：`npm --prefix frontend exec -- vitest ... --root frontend` 被npm吞参数，错误从根目录跑导致`document/FileReader is not defined`和Node测试套件误收集，exit 1；改用项目规定的`npm --prefix frontend test`，exit 0。独立解包脚本初次误把视频1280×720限制用于参考包，exit 1（`2280 !== 1280`）；核对产品契约与manifest后按视口2280×1282并逐项交叉校验通过，没有修改产品测试期待或放宽校验。

## 地形增量验收与本机边界（2026-11-25）

- 契约与编辑：`environmentSurface.spec.ts`6/6（严格字段/深复制、不可变两层覆盖、全局调色板重置、水色湿点限定与失效清理、物理通道纹理释放）；`EnvironmentBrushPanel.spec.tsx`3/3（真实React事件、撤销/重置/错误/禁用、零fetch、首笔整片渲染转换提示）。生产save/load回调两轮`STUDIO_TERRAIN_COLOR_ARCHIVE=PASS cycles=2 invalid=atomic`。
- 真实GPU夹具`TERRAIN_GPU_BLEND=PASS`：0/1/0.5权重读回[120,0,0]/[0,0,159]/[60,0,80]，绿水覆盖[28,110,0]；GL错误码全部0、shader错误为空，验证实际编译与颜色混合，不只匹配shader文本。未测试完整上游贴图库。
- 真实生产UI选混合→涂全局蓝色→涂局部绿水→存到内存后端→独立JSON核对→刷新恢复→截图入口通过（`BROWSER_TERRAIN_COLOR_ARCHIVE=PASS archiveId=1000020 blendWeight=0.475 baseSurface=0 secondarySurface=15 refreshRestore=true consoleErrors=[]`）。这是小场景功能验收，不是1577实例性能验收。
- 已有1577实例Profiler/GL方法包装窗口：30个RAF/5074.9ms，约9942次draw/帧，捕获Spark空渲染器全场景traverse/traverseVisible与matrix更新成本；**采样工具显著扰动性能，不把5.91与原12.7比较**。只做纯网格跳过空Spark的最小修改；生产RAF分支测试`MESH_ONLY_SPARK_GUARD=PASS`含隐藏splat仍启用与最后一项移除后关闭，未改模型树、配方版本、画质或输出尺寸；未取得同条件优化前后FPS，不宣称加速数值。
- 用户明确指出本机尤其显卡较弱：低配GTX960结果不外推其他机器，不再反复2880帧/1577实例压力；小场景功能和CPU离线测试继续。Chrome播放问题保持未知根因，不能只凭硬件说明断定是显卡或文件问题。
- 分发插件复用Vite generateBundle，声明从单一源文件发出、不维护副本；插件断言`NOTICE_BUNDLE_PLUGIN=PASS bytes=11481`，声明专项4/4通过。源码许可分发不等于全部第三方许可审计完成。
- 验证纠错：新增源码未登记导致首次全量exit1（导入门禁`environmentTerrain.ts -> terrainBlendMaterial.ts`），仅`git add -N`登记可追踪性、不暂存内容不提交后门禁通过。新增分发测试直接import实际已跟踪的vite.config，又触发门禁只索引src/tests的范围误报；改用Vite官方loadConfigFromFile（按真实生产加载配置），保留全部逐字断言、不修改既有门禁或期待，联合专项5/5通过。第二次浏览器验收初遇5184连接拒绝，查明该隔离静态服务已退出、18997/18999仍200；仅恢复5184后小场景验收通过，不作为产品故障或GPU卡死证据。

- 最终全量：`npm --prefix frontend test` exit0，Node **660/660**、Vitest **35文件/279项**；`npm --prefix frontend run build` exit0（4503 modules，53.04s），`NOTICE_DIST_IDENTICAL=PASS bytes=11481`确认dist声明与源文件逐字节一致。`go -C backend test ./... -count=1` exit0（service 42.140s）。均未复制到嵌入资源或部署。
- 已将保存完成的专用编辑器测试页置空（`DEDICATED_TEST_RENDER_PAUSED=PASS`），停止该页持续RAF占用；纯静态5185夹具Work已cancelled。未关闭用户业务页，也未停止18997/18999夹具或业务实例。

## 连续收尾：存档并发保护（2026-11-25）

- 新测试先复现缺陷：生产`loadScene`在`File.text()`之后才申请代次，A晚于B读完会覆盖B，Vitest专项exit1（对象引用不应替换却被替换）。把读取改为传给`importSceneText`的惰性函数，在确认后/读取前分配代次；旧请求不能清新busy/报旧错误。没有改弱测试期待。
- `STUDIO_ARCHIVE_CONCURRENCY=PASS`覆盖逆序读取、旧读错、过期GLB清理、当前读取/JSON失败不替换、脏场景真实取消分支及每个开始操作finish恰好一次；`STUDIO_ARCHIVE_CONFIRMATION=PASS`覆盖失败保留、旧预览不触发恢复、迟到成功不关新窗口、当前成功关闭。使用真实生产回调与GLTF解析，不跑GPU压力。
- 最新构建真实单对象浏览器`BROWSER_ARCHIVE_CONCURRENCY=PASS`：A/B逆序仍保留B、旧读错不清C busy、不污染新错误，C最终恢复。证据`.rscode/archive-concurrency-browser-result.json`，零业务请求。只在新建专用页对一次性合成档明确处理确认；首轮缺该处理而CDP Runtime.evaluate 30秒超时exit1，第二轮通过。不把浏览器的确认替身当脏场景取消证据（该分支由上面的CPU生产回调另验）；结束关闭所建页，未再次施压2880帧/1577实例。
- 独立只读复核未发现阻断回归；其指出缺少非法JSON、真实取消分支和finish次数断言，已追加并重新验证专项2/2通过。未宣称纹理/video/splat的所有资源路径均已并发压力验收。
- 许可声明移除「未修改、原样import即满足MPL义务」的过强判断；正式Vite声明分发专项与导入门禁联合5/5通过。全部商业授权、Apache全文及其他直接/间接依赖的发布审计仍不由该工程检查证明。

- 本次最终全量：`npm --prefix frontend test` exit0，Node **660/660**、Vitest **35文件/281项**；`npm --prefix frontend run build` exit0（4503 modules，53.73s）。纠正后的分发声明再次逐字节核对：`NOTICE_DIST_IDENTICAL=PASS bytes=11677`。`go -C backend test ./... -count=1` exit0（service 43.499s）。`git diff --check` exit0；HEAD仍8ee8b9a，HEAD...origin/main为0/0，暂存内容为空，没有提交/推送/部署。

## 本机验收收口与明确跳过（2026-11-25）

- 用户最新要求：需要启动ComfyUI才能做的测试标记跳过，本机无法跑ComfyUI。真实模型加载、目标节点接收参考视频、实际采样/视频生成、真实输出与来源回溯均标为**SKIPPED：本机无法运行ComfyUI**。不再等待实例信息、不启动或连接ComfyUI；此前一次真实生成授权不在本轮执行。离线处理器与模板/输入/快照测试不需要ComfyUI，照常执行且不假称实机验收。
- 本地只读发现配置覆盖未设置，配置样例没有隔离实例；`console.db`用SQLite `mode=ro`+`query_only`仅取实例的非敏感地址/状态列，未写业务库、未读凭据、未访问18000。未把默认8188擅自指定为测试目标。最新跳过决定后不再对此索要信息。
- 新视频异步回归先失败2项（旧导出错误出现在新面板、旧上传回执未隔离，Vitest exit1），修改面板所有权而不放宽期待后10/10通过：`VIDEO_HANDOFF_ASYNC_OWNERSHIP=PASS staleExport=ignored lateUpload=separate ABA=isolated receiptId=retained autoRetry=0`。旧后端未保留来源时仍留素材ID/警示，当前产物不会被误标为已入库。
- 独立只读复核未发现确认缺陷，指出卸载后Promise完成、旧导出成功等路径未单独动态测量；已知生产实现要求取消由编码器所有者响应、计划按不可变引用更新，不承诺强杀无响应任务。没有改动超时/重试/自动生成策略。
- Chrome解码新增小样本对照仅stream-copy已有两文件各前1秒，不重渲染。`node .rscode/diagnose-video-software-decode.mjs`45秒工具超时，无诊断结果；停止该路径，不归因、不作通过证据。随后CDP可达，未发现该诊断标题的遗留页。与ComfyUI无关，不能因本次跳过范围而把已有`PIPELINE_ERROR_DECODE`改成通过。

### 本次最终本地验收结果

| 检查 | 结果/信号 |
|---|---|
| `npm --prefix frontend test` | exit0，Node **660/660**；Vitest **35文件/284项**（含视频异步回执新增3项） |
| `npm --prefix frontend run build` | exit0，4503 modules，约61秒；不复制嵌入资源、不部署 |
| `go -C backend test ./... -count=1` | exit0，service 40.592s；临时数据库/HTTP夹具，不要求ComfyUI |
| 构建分发声明 | `NOTICE_DIST_IDENTICAL=PASS bytes=11677`，dist与唯一源码逐字节一致 |
| 真实生成/模型加载/目标节点消费/真实结果回溯 | **SKIPPED：本机无法运行ComfyUI（用户明确要求）**；不是失败或通过，不再作为继续条件 |
| Chrome上限视频播放 | **未通过/根因未知**；未重新渲染压力场景，不因ComfyUI跳过而改写结果 |
| 跨设备/大场景交互性能、峰值显存 | **未验收/不可得**；已有低配测量保留，不重复施压 |
| Git边界 | `git diff --check` exit0；HEAD=8ee8b9a、HEAD...origin/main=0/0、暂存内容为空，未提交/推送/部署 |

本地可跑收尾本轮已连续完成；无需用户提供ComfyUI地址来结束本轮本地验收。上述SKIPPED/未通过/未验收不能算入通过数量。

## 素材上传有界读取契约（2026-11-25）

用户已选择先记录分类上限再实施：图片/文档20MiB、音频100MiB、视频/3D512MiB；未知类型沿用现有类型语义但按20MiB兜底，不新增类型/MIME白名单。3D缩略图限8MiB，超过后明确拒绝，不再截断。派生清单仍限1MiB。

- 在任何表单解析前限制完整请求体：512MiB主文件 + 8MiB缩略图 + 1MiB清单 + 1MiB multipart/其他字段开销；不是允许多个主文件各占512MiB。已知Content-Length与未知长度流均须受限。
- 完整请求或选定主文件/缩略图超限返回413；读取失败返回明确错误；不得吞掉multipart解析或缩略图读取错误。拒绝发生在持久化调用前，零素材/缩略图文件、零Material/UploadFile记录；清理解析产生的临时文件。此保证不扩展为所有存储失败的事务原子性。
- 默认image、project_id校验、无清单旧上传、带清单视频、项目归属、响应结构及生成链保持；非法清单仍400。允许范围内的旧上传兼容；大于新上限的旧上传有意改为413，不宣称无条件向后兼容。
- 测试覆盖分类选择、阈值与加一、已知/未知请求长度、总量超限、临时文件清理、拒绝零持久化以及旧上传/清单视频/3D缩略图。测试使用临时目录和隔离SQLite，不访问业务数据库或ComfyUI。

### 本项执行信号与新机范围更正

- 修复前 `go -C backend test ./internal/service -run TestMaterialUploadRejectsOversizedImageBeforePersistence -count=1` exit1：真实20MiB+1图片仍进入持久化，nil服务哨兵由Recovery转为500，断言为`status=500 want=413`；修复后同一测试exit0。
- 复用既有有界读取工具，增加解析前522MiB完整请求闸门、分类主文件限额及8MiB缩略图拒绝。显式处理解析错误，未知长度及multipart最终边界后尾随数据也受请求体限额约束；没有新增MIME白名单或存档字段。
- 小预算注入仅用于私有同一处理路径的边界测试，不修改全局生产限额：5类×阈值/加一×已知/未知长度共20组；缩略图/完整请求阈值、流式超限、尾随超限、损坏multipart、9MiB落盘后拒绝清理共8组。生产分类常量和真实20MiB+1另有断言，不冒称实际上传512MiB压力验收。拒绝路径检查Material/UploadFile均零行且素材目录无文件；成功路径回读主文件和缩略图字节一致。
- 专项exit0，信号`MATERIAL_UPLOAD_BOUNDARIES=PASS exact=accepted over=413 malformed=400 rejectedWrites=0 temporaryFiles=0`；旧无清单上传、清单视频及SQLite重开来源留存仍通过。
- `go -C backend test ./... -count=1` exit0，service25.760秒。未部署、未提交推送、未改用户H3模板；日志 `.rscode/material-upload-limits-go-tests.log` 不提交。本项仅后端/文档，无新增前端改动，不把上一项前端测试当作本项重新执行。
- **用户在本项期间更正机器条件：当前电脑配置较高，可以测试ComfyUI。** 前文“本机无法运行”的SKIPPED记录仅描述旧环境，不再作为当前机器不测试的理由；新机实机消费、生成与回溯仍为待执行，不能因此改为PASS。本项没有连接或启动ComfyUI、没有创建真实生成任务；实机测试仍须确认当前实例、模型和明确测试范围，H3实验沿用8步及不超过832×480的约束。

### Chrome原生multipart补验（2026-11-25）

- 隔离HeadlessChrome/157.0.0.0通过原生Blob/FormData/fetch访问127.0.0.1:18081上的真实 `Service{}.HandleUploadMaterial`。临时Go哨兵不初始化数据库/存储/ComfyUI；误达持久化将由Recovery显式变500，绝不连接真实服务。编译 `go -C backend build -o ../.rscode/upload-browser-sentinel.exe ./.rscode-upload-probe` exit0。
- 浏览器脚本 `node .rscode/motion-upload-chrome.mjs` 最终exit0：真实20MiB+1 image及unknown主文件均413且响应“文件超过20MiB限制”；非法project_id用例400，损坏multipart400；联合信号 `CHROME_COMPONENT_AND_UPLOAD_FIXTURE=PASS`。20MiB带非法项目用例仅验证非法项目拒绝，不是“20MiB合法上传接受”的证明。完整接收/入库继续仅由隔离Go夹具证明，不冒称Chrome成功入库、视频/3D512MiB、缩略图8MiB或压力验收。
- 首轮浏览器脚本exit1：超限夹具错误附带project_id=0，按既有前置项目校验返回400（`400 !== 413`）；读处理器确认后仅移除超限夹具无关字段，保留413断言，非法项目仍单独400；未改产品逻辑或削弱断言。临时服务初版用相对路径读bundle导致HTTP500，改为可执行文件目录定位后HTTP200；独立脚本规避PowerShell内联引号错误。以上均为夹具准备问题，不作为产品失败或PASS隐去。
- 临时Go哨兵源在 `backend/.rscode-upload-probe/`，脚本/bundle/JSON/log/profile在 `.rscode/`，均不提交。结束后已停止核对过路径的进程并取得 `SENTINEL_STOPPED=PASS`；未访问真实业务后端、未写真实素材、未触发生成。没有完整应用上传UI/像素验收。qa-only流程仍因缺fresh project-gate evidence而blocked。

## 新机顺序验收补记（2026-11-25）

用户要求按播放→新机性能→ComfyUI实机逐项推进，并选择在旧文件缺失时重建独立夹具。真实 `ScriptVideoPanel`/采样器/WebGL/WebCodecs路径的新64镜简化场景导出2880帧VP9/120秒/1280×720，Chrome157、RTX4070、D3D11VideoDecoder连续播放到ended=true，60/110秒seek后继续播放，无媒体错误；严格FFmpeg全片解码exit0，ffprobe确认为2880帧。`NEW_UPPER_PANEL_PLAYBACK=PASS`，完整条件、旧文件缺失及夹具首次身份错误见[高性能验收清单§3](../2026-11-25-Toonflow迁移-高性能电脑待验收清单.md)。**这是新机新片通过，不确认或推翻旧故障硬件假设，不代表复杂雨天角色压力通过。**

第2项已重建古街19组1577实例/20配方定义，生成断言exit0；实际页面自动化两次exit1（`Could not find node with given id`），收窄API拦截仍未解决；已按停止规则暂停，不做第三次变体，没有可报告的有效性能样本。T03/T04仍未验，第3项实机生成未开始，不能沿用下方历史“本机无法运行”理由。本轮无产品代码修改、无真实素材写入、无生成、无提交推送。

### 继续测试：第2项部分通过与新seek失败

用户要求继续测试后，先只读诊断入口模块（当前Scene3DStage和input均挂载，无模块加载失败，旧空白根因未确认），再定位刷新后读取旧input的夹具竞态；等待新文档load事件后，1577实例载入、选择、0.1m移动、撤销、聚焦、显隐、鼠标旋转、保存刷新重载断言exit0（`NEW_SCALE_INTERACTION_AND_ROUNDTRIP=PASS`）。最终静置35.22次RAF/s、旋转16.49次RAF/s、旋转p95=83.4ms/56个>50ms长帧，不能报交互流畅。JS堆只有阶段快照，NVIDIA每秒47个整卡样本观察最高7533MiB，不归因网页、不冒称精确峰值或进程显存。

新3网格64镜简化档真实导出193条目参考包，192张1424×801 PNG逐张对应清单；视频导出1280×720/64秒/12fps/768帧/2546046字节，追溯清单帧数/FPS/artifactId断言通过。但真实Chrome起播后seek60秒，观察到 `PipelineStatus::PIPELINE_ERROR_DECODE: video decode error!`、readyState=1、seeking=true，后续play Promise导致30秒CDP超时；整条压力脚本exit1，不报视频通过，不提高超时。FFmpeg严格全片解码exit0、FFprobe768帧；包时间戳单调/32个每2秒关键帧检查通过仍不证明Chrome兼容或根因。保留原2880片PASS与本768片FAIL各自范围；停止失败播放路径，未进入ComfyUI，不越序生成。完整原始日志、夹具准备失败、数值及未验项见[验收清单§8](../2026-11-25-Toonflow迁移-高性能电脑待验收清单.md)。本轮只改临时脚本和执行记录，未修改产品、模板、提示词、业务数据，未提交推送；原生investigate仍不代表根因确认或完成。

## 未完成的边界

本文以上是当时的逐步记录；后续用户明确授权六类增强，已按[ADR-0022](0022-studio-character-direction-and-surface-enhancements.md)统一实现IK/脚底贴合、混合、人工校准重定向、人工引导机位/表演、顺序时序避让及现代surfacePaint/59种程序材质，真实小场景入口/存档/画面已验。不再将这些实现记作「未迁入」。**ComfyUI依赖真实生成验收按用户要求SKIPPED**，不是继续等待授权的阻塞；离线交接不能替代实机消费证明。Chrome上限视频播放仍失败，跨设备/大场景交互性能、59种材质逐项外观未验收，峰值显存不可得，不反复施压弱显卡。本地可测项与全环境验收分别报告，不能以跳过或全量测试通过宣称所有环境已通过。
