# ArtCraft 迁移：ComfyUI 工作流与模型盘点

> 状态：盘点稿，2026-11-24。
> 本文只回答三件事：**仓库模板实际要求什么**、**这些模型/节点应从哪来**、**上线前如何在实例上只读核对**。
> 本文**不**声称任何模型或自定义节点“已安装”，也**不**声称任何 ComfyUI 实例“已支持”。实例能力必须以目标机 `GET /object_info` 与 `GET /system_stats` 的返回为准。

## 快速安装建议（个人本地使用）

不必一次装齐全部模型，按需要分批安装：

1. **图片抠图**：先装 `ComfyUI-RMBG`，使用模板默认BEN2，由插件下载所需模型。无网环境请按插件模型清单预先准备文件，不要只复制一个未知权重。
2. **图生网格**：下载 `hunyuan_3d_v2.1.safetensors` 到 `<ComfyUI>/models/checkpoints/`。当前模板生成几何GLB，不含混元纹理绘制流程，也不需要下载整套Hunyuan3D-Paint。
3. **图生泼溅**：优先安装第3.1节的TripoSplat五个文件，选择BiRefNet分支；RMBG-3.0是额外可选的大模型链路，不是所有3D生成的必需依赖。
4. **文本分割**：装Easy-Sam3和SAM权重，但模板默认SAM3.1与该插件的兼容性尚须验证；不要把文件改名当作兼容方案。
5. **精修与重绘**：按需再装SDMatte、Qwen重绘依赖；Krea2指定权重来源未核实，暂不提供猜测下载地址。

本文所有路径以实际运行的ComfyUI根目录为准。Linux示例用 `/opt/comfyUI`；Windows例如 `D:\\ComfyUI`，则 `models/checkpoints/` 对应 `D:\\ComfyUI\\models\\checkpoints\\`。便携版通常是解压目录内的 `ComfyUI` 子目录，不是 `python_embeded`。已有模型可复用，不必重复下载。

## 1. 范围与证据

### 1.1 盘点范围

本文只覆盖 ArtCraft 编辑器迁移相关的 8 个模板，均位于 `backend/internal/service/templates/`：

| # | 模板文件 | code | 用途 |
|---|---|---|---|
| 1 | `image_inpaint.json` | `image_inpaint` | Qwen-Image 2.1 局部重绘 |
| 2 | `image_inpaint_krea2.json` | `image_inpaint_krea2` | Krea2 局部重绘 |
| 3 | `image_remove_background.json` | `image_remove_background` | ComfyUI-RMBG / BEN2 抠图 |
| 4 | `image_segment_sam3.json` | `image_segment_sam3` | SAM3 文本定位分割 |
| 5 | `image_segment_sam3_matte.json` | `image_segment_sam3_matte` | SAM3 + SDMatte 边缘精修 |
| 6 | `image_to_3d_hunyuan3d.json` | `image_to_3d_hunyuan3d` | 混元 3D 2.1 图生 3D |
| 7 | `image_to_3d_triposplat.json` | `image_to_3d_triposplat` | TripoSplat 图生高斯泼溅，RMBG-3.0 / BiRefNet 可切换 |
| 8 | `image_to_3d_triposplat_birefnet.json` | `image_to_3d_triposplat_birefnet` | TripoSplat + BiRefNet 抠图 |

不把仓库里其他 H3 / MiniMax / Moss 模板混入本文；它们不属于这次 ArtCraft 迁移的 8 个模板。

### 1.2 已核对的事实来源

| 事实 | 证据 |
|---|---|
| 8 个模板存在，code 与文件名一致 | 逐文件读取；`backend/internal/service/new_image_workflows_test.go` 对 8 个模板做嵌入与占位符渲染断言 |
| 模板里的节点类名、默认模型名、参数默认值 | 逐文件读取 JSON 的 `workflow` 与 `inputs` |
| 仓库内嵌 ComfyUI 版本为 `0.30.0` | `comfyui/comfyui_version.py` |
| 内嵌快照包含/不包含哪些节点 | `comfyui/comfy_extras/*.py` 与 `comfyui/nodes.py` 检索 |
| 生产默认安装目录 `/opt/comfyUI` | `deploy.sh` 的 `COMFY_DIR=/opt/comfyUI`；`docker-compose.yml` 的 `/opt/comfyUI:/opt/comfyUI`；`backend/config.yaml.example` 的 `comfy.comfy_dir` |
| 多实例端口 8188–8195 | `shell/start-multi-gpu.sh` 的 `BASE_PORT=8188` 与端口注释；`deploy.sh` 的 `port=$((8188 + gpu))` |
| 模型文件大小与许可 | Hugging Face API 的仓库文件树与 `cardData` |
| 自定义节点仓库、节点 ID、许可 | GitHub 仓库 API、README 与源码 |

### 1.3 特别说明：“模板可渲染”不等于“实例可执行”

- 模板层：仓库测试 `TestNewImageWorkflowsAreEmbedded` 与 `TestNewImageWorkflowsRender` 对 8 个 JSON 的嵌入和占位符渲染做了断言；文档复核时已执行 `go -C backend test ./internal/service -run 'TestNewImageWorkflows|TestQwenImage21' -count=1`，退出0；该结果不代表实例生成成功。
- 内核层：仓库内嵌的是 `0.30.0` 快照，**不是**生产实例的实际版本；快照里没有的节点，生产实例可能有；快照里有的节点，生产实例也可能因裁剪/降级而没有。
- 实例层：本次没有连接任何 ComfyUI，没有调用 `/object_info`，因此下文所有“需要/应放/建议”都是部署要求，不是“已安装”的结论。

## 2. 8 个模板总表

| code | 输入 → 输出 | 关键节点 | 依赖模型 | 0.30.0 快照核心节点是否齐备 |
|---|---|---|---|---|
| `image_inpaint` | 原图 + 蒙版 + 提示词 → PNG | `INPAINT_MaskedFill`、`TextEncodeQwenImage21`、`QwenImage21Cache`、`VAEEncodeForInpaint`、`KSampler`、`VAEDecode` | Qwen-Image 2.1 UNET / Qwen3-VL 8B 文本编码器 / Qwen-Image 2.1 VAE | **否**：缺 `TextEncodeQwenImage21`、`QwenImage21Cache`、`INPAINT_MaskedFill` |
| `image_inpaint_krea2` | 原图 + 蒙版 + 提示词 → PNG | `UNETLoader`、`CLIPLoader(type=krea2)`、`InpaintModelConditioning`、`KSampler`、`VAEDecode` | Krea2 UNET / Qwen3-VL 4B 文本编码器 / `qwen_image_vae` | **是**（`krea2` 类型在 0.30.0 `CLIPLoader` 枚举中）；但权重来源待确认 |
| `image_remove_background` | 原图 → 透明 PNG | `RMBG`、`SaveImage` | 由 ComfyUI-RMBG 管理，默认 BEN2；可切 RMBG-2.0 / INSPYRENET / BEN | **否**：`RMBG` 是自定义节点 |
| `image_segment_sam3` | 原图 + 文本主体 → 透明 PNG | `easy sam3ModelLoader`、`easy sam3ImageSegmentation`、`JoinImageWithAlpha`、`SaveImage` | SAM3.1 权重 | **否**：核心 SAM3 节点 ID 不同；模板要 Easy-Sam3 的 `easy sam3*` |
| `image_segment_sam3_matte` | 原图 + 文本主体 → 透明 PNG | 上者 + `AILab_SDMatte` | SAM3.1 + SDMatte | **否**：需 Easy-Sam3 + ComfyUI-RMBG |
| `image_to_3d_hunyuan3d` | 参考图 → GLB | `ImageOnlyCheckpointLoader`、`Hunyuan3Dv2Conditioning`、`EmptyLatentHunyuan3Dv2`、`VAEDecodeHunyuan3D`、`VoxelToMesh`、`SaveGLB` | `hunyuan_3d_v2.1.safetensors`（CLIP Vision 与 VAE 打包在 checkpoint 内） | **是** |
| `image_to_3d_triposplat` | 参考图 → SPZ | TripoSplat 三件套 + Flux2 VAE + `TextEncodeFibo2` + `CLIPLoader(type=fibo2)` + RMBG-3.0 链路 + `ComfySwitchNode` | TripoSplat 三件套、Flux2 VAE、BiRefNet、RMBG-3.0 int8、Qwen3-VL 4B | **否**：缺 `TextEncodeFibo2` 与 `fibo2` 枚举 |
| `image_to_3d_triposplat_birefnet` | 参考图 → SPZ | `LoadBackgroundRemovalModel`、`RemoveBackground`、`TripoSplat*`、`SplatToFile3D`、`SaveGLB` | TripoSplat 三件套、Flux2 VAE、BiRefNet | **是** |

> “0.30.0 快照核心节点是否齐备”只说明仓库内那份源码快照，不是生产版本判断。生产版本必须按第 6 节核对。

## 3. 新增模型盘点

这里的“新增”指 **ArtCraft 迁移模板新引用或新要求**，不表示这些模型此前从未在仓库其他模板中出现。

### 3.1 图生 3D

| 模型文件 | 应放目录（相对 `models/`） | 大小 | 许可 | 可信来源 | 备注 |
|---|---|---:|---|---|---|
| `hunyuan_3d_v2.1.safetensors` | `checkpoints/` | 7.37 GB | Tencent Hunyuan Community License（HF 标注 `license: other` / `tencent-hunyuan-community`） | [Comfy-Org/Hunyuan-3D-2.1](https://huggingface.co/Comfy-Org/Hunyuan-3D-2.1) | 单文件；模板说明 CLIP Vision 与 VAE 已打包在 checkpoint 内 |
| `triposplat_fp16.safetensors` | `diffusion_models/` | 741 MB | MIT | [VAST-AI/TripoSplat](https://huggingface.co/VAST-AI/TripoSplat) | TripoSplat 主模型 |
| `dino_v3_vit_h.safetensors` | `clip_vision/` | 1.68 GB | MIT（随 TripoSplat 仓库） | [VAST-AI/TripoSplat](https://huggingface.co/VAST-AI/TripoSplat) | TripoSplat 的 CLIP Vision |
| `triposplat_vae_decoder_fp16.safetensors` | `vae/` | 576 MB | MIT（随 TripoSplat 仓库） | [VAST-AI/TripoSplat](https://huggingface.co/VAST-AI/TripoSplat) | 解码高斯泼溅 |
| `flux2-vae.safetensors` | `vae/` | 336 MB | MIT（从 TripoSplat 仓库取得时） | [VAST-AI/TripoSplat](https://huggingface.co/VAST-AI/TripoSplat) 的 `vae/flux2-vae.safetensors` | 条件编码与 RMBG-3.0 解码共用；不建议从 CC BY-NC 的 Bria 仓库取这一份，避免把非商用分发条款带进 3D 链路 |
| `birefnet.safetensors` | `background_removal/` | 444 MB | MIT（随 TripoSplat 仓库；原始 BiRefNet 以 `ZhengPeng7/BiRefNet` 模型卡为准） | [VAST-AI/TripoSplat](https://huggingface.co/VAST-AI/TripoSplat) 的 `background_removal/birefnet.safetensors` | BiRefNet 分支和 TripoSplat 默认模板的 fallback 都用它 |

> `VAST-AI/TripoSplat` 仓库里还有 `vae/triposplat_vae_encoder_fp16.safetensors`（682 MB），但当前 8 个模板均未引用；不要因为“仓库里有”就当成必需文件。

### 3.2 抠图与分割

| 模型文件 | 应放目录（相对 `models/`） | 大小 | 许可 | 可信来源 | 备注 |
|---|---|---:|---|---|---|
| `sam3.1_multiplex_fp16.safetensors` | `sam3/` | 1.75 GB | Meta SAM License（HF 标注 `license: other` / `sam-license`） | [Comfy-Org/sam3.1](https://huggingface.co/Comfy-Org/sam3.1) 的 `checkpoints/sam3.1_multiplex_fp16.safetensors` | 模板默认值；Easy-Sam3 README 自己也指向 `facebook/sam3` 与 `yolain/sam3-safetensors`，两者是否完全等价需实测 |
| `RMBG-3.0` 权重（默认 `rmbg3_int8_convrot.safetensors`） | `diffusion_models/` | 8.41 GB | **CC BY-NC 4.0，仅非商用** | [briaai/RMBG-3.0-Comfy](https://huggingface.co/briaai/RMBG-3.0-Comfy) | 模板 `image_to_3d_triposplat` 默认开启；商用必须关闭 `use_rmbg3` 走 BiRefNet，或取得 Bria 商业授权 |
| `qwen3vl_4b_fp8_scaled.safetensors` | `text_encoders/krea2/` | 5.24 GB | Apache-2.0（Comfy-Org/Qwen3-VL 仓库标注） | [Comfy-Org/Qwen3-VL](https://huggingface.co/Comfy-Org/Qwen3-VL) 的 `text_encoders/qwen3vl_4b_fp8_scaled.safetensors` | 模板以 `CLIPLoader(type=fibo2)` 加载；Bria 的 RMBG-3.0 仓库也提供 `qwen3vl_4b_*`，但该仓库整体是 CC BY-NC，优先从 Apache-2.0 的 Qwen3-VL 仓库取 |
| `SDMatte.safetensors` | `RMBG/SDMatte/` | 上游 `.pth` 约 12.1 GB；转换后的 `.safetensors` 大小以节点下载为准 | MIT（上游 SDMatte 仓库；转换/打包件以实际来源为准） | 由 [1038lab/ComfyUI-RMBG](https://github.com/1038lab/ComfyUI-RMBG) 的模型管理下载/转换；上游 [LongfeiHuang/SDMatte](https://huggingface.co/LongfeiHuang/SDMatte) 只有 `.pth` | 不要手工把 `.pth` 改名为 `.safetensors`；模板要求的是 `models/RMBG/SDMatte/SDMatte.safetensors` |
| `BEN2` / `RMBG-2.0` / `INSPYRENET` / `BEN` | `RMBG/`（由 `RMBG` 节点首次运行时自动下载） | 因模型而异 | 逐个看模型卡；其中 `RMBG-2.0` 为 **CC BY-NC 4.0**，商用需特别核对 | 由 [1038lab/ComfyUI-RMBG](https://github.com/1038lab/ComfyUI-RMBG) 管理，仓库不自带固定直链 | 模板默认 `BEN2`；商用请先确认所选模型的许可，不要默认“开源=可商用” |

> `SDMatte` 的目录名大小写是 `RMBG/SDMatte/`。Linux 区分大小写，写成 `models/rmbg/sdmatte/` 会找不到。

### 3.3 局部重绘

| 模型文件 | 应放目录（相对 `models/`） | 大小 | 许可 | 可信来源 | 备注 |
|---|---|---:|---|---|---|
| `qwen_image_2.1_int8_convrot.safetensors` | `diffusion_models/qwen-image2.1/` | 7.26 GB | Qwen Research License（HF 标注 `license: other` / `qwen-research`） | [Comfy-Org/Qwen-Image-2.1](https://huggingface.co/Comfy-Org/Qwen-Image-2.1) 的 `diffusion_models/qwen_image_2.1_int8_convrot.safetensors` | 模板默认 int8；同仓库也有 bf16 版本，但更大 |
| `qwen3vl_8b_int8_convrot.safetensors` | `text_encoders/qwen-image2.1/` | 9.35 GB | Qwen Research License（随 Qwen-Image 2.1 仓库） | [Comfy-Org/Qwen-Image-2.1](https://huggingface.co/Comfy-Org/Qwen-Image-2.1) 的 `text_encoders/qwen3vl_8b_int8_convrot.safetensors` | 模板 `CLIPLoader(type=qwen_image)` |
| `qwen_image_2.1_vae_bf16.safetensors` | `vae/qwen-image2.1/` | 676 MB | Qwen Research License（随 Qwen-Image 2.1 仓库） | [Comfy-Org/Qwen-Image-2.1](https://huggingface.co/Comfy-Org/Qwen-Image-2.1) 的 `vae/qwen_image_2.1_vae_bf16.safetensors` | 模板默认 VAE |
| `Krea2-MuseByStable_v15Turbo_fp8.safetensors` | `diffusion_models/krea2/` | 未核实 | **未核实** | **本次未找到可信公开下载源** | 模板默认值；在拿到权重提供方的许可与校验值之前，不要从不明网盘/镜像站下载 |
| `qwen3vl_4b_fp8_scaled.safetensors` | `text_encoders/krea2/` | 5.24 GB | Apache-2.0 | [Comfy-Org/Qwen3-VL](https://huggingface.co/Comfy-Org/Qwen3-VL) | 与 RMBG-3.0 用的是同一份文本编码器，模板放在 `krea2/` 子目录 |
| `qwen_image_vae.safetensors` | `vae/` | 254 MB | 随 Qwen-Image 仓库（以模型卡为准） | [Comfy-Org/Qwen-Image_ComfyUI](https://huggingface.co/Comfy-Org/Qwen-Image_ComfyUI) 的 `split_files/vae/qwen_image_vae.safetensors` | Krea2 模板用它做 VAE；不要和 Qwen-Image 2.1 的新 VAE 混用 |

> `Krea2-MuseByStable_v15Turbo_fp8.safetensors` 是本次盘点中唯一没有可信公开来源的必需权重。它的名称像第三方融合模型，不能仅凭“Krea2”推断为某个官方仓库的文件；商用/分发前必须补齐来源、许可和哈希。

### 3.4 模型目录一览

```text
/opt/comfyUI/models/
├── checkpoints/
│   └── hunyuan_3d_v2.1.safetensors
├── diffusion_models/
│   ├── triposplat_fp16.safetensors
│   ├── rmbg3_int8_convrot.safetensors
│   ├── qwen-image2.1/
│   │   └── qwen_image_2.1_int8_convrot.safetensors
│   └── krea2/
│       └── Krea2-MuseByStable_v15Turbo_fp8.safetensors
├── text_encoders/
│   ├── qwen-image2.1/
│   │   └── qwen3vl_8b_int8_convrot.safetensors
│   └── krea2/
│       └── qwen3vl_4b_fp8_scaled.safetensors
├── clip_vision/
│   └── dino_v3_vit_h.safetensors
├── vae/
│   ├── triposplat_vae_decoder_fp16.safetensors
│   ├── flux2-vae.safetensors
│   ├── qwen_image_vae.safetensors
│   └── qwen-image2.1/
│       └── qwen_image_2.1_vae_bf16.safetensors
├── background_removal/
│   └── birefnet.safetensors
├── sam3/
│   └── sam3.1_multiplex_fp16.safetensors
└── RMBG/
    ├── (BEN2 / RMBG-2.0 / INSPYRENET / BEN，由插件自动下载)
    └── SDMatte/
        └── SDMatte.safetensors
```

## 4. 自定义节点盘点

### 4.1 必须安装的自定义节点

| 节点 ID（模板实际使用） | 提供方 | 仓库 | 许可 | 安装位置 | 说明 |
|---|---|---|---|---|---|
| `RMBG` | 1038lab / ComfyUI-RMBG | [GitHub](https://github.com/1038lab/ComfyUI-RMBG) | GPL-3.0 | `/opt/comfyUI/custom_nodes/ComfyUI-RMBG` | 抠图；模型下拉包含 BEN2 / RMBG-2.0 / INSPYRENET / BEN |
| `AILab_SDMatte` | 同上 | 同上 | GPL-3.0 | 同上 | SDMatte 精修；模型从 `models/RMBG/SDMatte/` 读取 |
| `easy sam3ModelLoader` | yolain / ComfyUI-Easy-Sam3 | [GitHub](https://github.com/yolain/ComfyUI-Easy-Sam3) | Apache-2.0 | `/opt/comfyUI/custom_nodes/ComfyUI-Easy-Sam3` | 注册 `models/sam3/`；默认模型名是 `sam3.pt`，需在 UI 里改选实际文件 |
| `easy sam3ImageSegmentation` | 同上 | 同上 | Apache-2.0 | 同上 | 文本/点/框提示分割；模板只用了文本提示 |
| `INPAINT_MaskedFill` | Acly / comfyui-inpaint-nodes | [GitHub](https://github.com/Acly/comfyui-inpaint-nodes) | GPL-3.0 | `/opt/comfyUI/custom_nodes/comfyui-inpaint-nodes` | Qwen-Image 2.1 局部重绘前处理；负责把蒙版区域填成中性色 |

安装时使用**该实例实际使用的 Python 环境**安装依赖，不要用系统 Python 猜测：

```bash
cd /opt/comfyUI/custom_nodes
git clone https://github.com/1038lab/ComfyUI-RMBG.git
git clone https://github.com/yolain/ComfyUI-Easy-Sam3.git
git clone https://github.com/Acly/comfyui-inpaint-nodes.git

# 以下命令仅为示例；先确认实例的 Python/venv 路径，再逐仓库执行
/opt/comfyUI/venv/bin/python -m pip install -r ComfyUI-RMBG/requirements.txt
/opt/comfyUI/venv/bin/python -m pip install -r ComfyUI-Easy-Sam3/requirements.txt
/opt/comfyUI/venv/bin/python -m pip install -r comfyui-inpaint-nodes/requirements.txt
```

> 不要凭本文假定 `/opt/comfyUI/venv` 一定存在；有些部署用 conda 或系统 Python。安装后必须重启对应实例，`/object_info` 才会出现新节点。

### 4.2 ComfyUI 内核节点（不要重复装成自定义节点）

以下节点由 ComfyUI 本体提供，模板依赖它们：

| 模板 | 依赖的内核节点 | 0.30.0 快照 |
|---|---|---|
| Hunyuan3D | `ImageOnlyCheckpointLoader`、`CLIPVisionEncode`、`Hunyuan3Dv2Conditioning`、`ModelSamplingAuraFlow`、`EmptyLatentHunyuan3Dv2`、`VAEDecodeHunyuan3D`、`VoxelToMesh`、`SaveGLB` | 有 |
| TripoSplat | `TripoSplatPreprocessImage`、`TripoSplatConditioning`、`VAEDecodeTripoSplat`、`SplatToFile3D` | 有 |
| BiRefNet 分支 | `LoadBackgroundRemovalModel`、`RemoveBackground` | 有 |
| Krea2 重绘 | `CLIPLoader(type=krea2)`、`InpaintModelConditioning`、`KSampler`、`VAEDecode` | 有（`krea2` 在 `CLIPLoader` 枚举中） |
| 条件切换 | `ComfySwitchNode` | 有（`comfy_extras/nodes_logic.py`） |
| SAM3 模板的 alpha 合成 | `JoinImageWithAlpha` | 有 |
| Qwen-Image 2.1 重绘 | `TextEncodeQwenImage21`、`QwenImage21Cache` | **没有** |
| RMBG-3.0 / TripoSplat 默认分支 | `TextEncodeFibo2`、`CLIPLoader(type=fibo2)` | **没有** |

### 4.3 版本兼容矩阵

| 模板 | 0.30.0 内核能否直接跑 | 缺什么 | 处理方向 |
|---|---|---|---|
| `image_to_3d_hunyuan3d` | 可以（前提是模型存在） | 无 | 直接用；仍需核对 checkpoint 文件 |
| `image_to_3d_triposplat_birefnet` | 可以（前提是模型存在） | 无 | 直接用；这是 12 GB 卡更稳的 3D 分支 |
| `image_inpaint_krea2` | 可以（前提是 Krea2 权重存在） | 无内核节点 | 先解决权重来源与许可 |
| `image_inpaint` | **不可以** | `TextEncodeQwenImage21`、`QwenImage21Cache`、`INPAINT_MaskedFill` | 升级内核到包含这些节点的版本，并安装 inpaint 自定义节点 |
| `image_to_3d_triposplat` | **不可以** | `TextEncodeFibo2`、`CLIPLoader(type=fibo2)` | 升级内核；或改用 `image_to_3d_triposplat_birefnet` |
| `image_remove_background` | **不可以** | `RMBG` | 安装 ComfyUI-RMBG |
| `image_segment_sam3` | **不可以** | `easy sam3ModelLoader`、`easy sam3ImageSegmentation` | 安装 ComfyUI-Easy-Sam3；不要拿核心 `SAM3_Detect` 直接替换 |
| `image_segment_sam3_matte` | **不可以** | 上述两者 + `AILab_SDMatte` | 同时安装 ComfyUI-Easy-Sam3 与 ComfyUI-RMBG |

> 内嵌 0.30.0 里虽然已有 `SAM3_Detect` / `SAM3_VideoTrack` / `SAM3_TrackPreview` / `SAM3_TrackToMask`，但它们的节点 ID、输入输出与模板的 `easy sam3*` 不同，**不是可直接替换的别名**。

## 5. ComfyUI 安装目录与部署形态

### 5.1 生产默认：`/opt/comfyUI`

| 项目 | 值 | 证据 |
|---|---|---|
| ComfyUI 根目录 | `/opt/comfyUI` | `deploy.sh` 的 `COMFY_DIR`；`config.yaml.example` 的 `comfy.comfy_dir`；`docker-compose.yml` 的挂载 |
| 模型目录 | `/opt/comfyUI/models` | `deploy.sh` 启动前检查 `${COMFY_DIR}/models` |
| 自定义节点目录 | `/opt/comfyUI/custom_nodes` | ComfyUI 默认布局；插件安装位置 |
| 输出目录 | `/opt/comfyUI/output` | ComfyUI 默认布局；模板 `SaveImage`/`SaveGLB` 的 `filename_prefix` 会落在这里 |
| 实例管理脚本 | `/opt/comfyUI/start-multi-gpu.sh` | `deploy.sh` 调用 `bash "${COMFY_DIR}/start-multi-gpu.sh" start` |
| 端口 | `8188`–`8195`，对应 GPU 0–7 | `shell/start-multi-gpu.sh` 与 `deploy.sh` |
| 控制台容器端口 | `18000` | `docker-compose.yml` 的 `18000:18000` |
| 控制台挂载 | `/opt/comfyUI:/opt/comfyUI` | `docker-compose.yml` |
| 部署形态 | 宿主机裸进程，不是每个 GPU 一个 ComfyUI 容器 | `deploy.sh` 注释与 `docker-compose.yml` 注释一致 |

`backend/config.yaml.example` 的默认值：

```yaml
comfy:
  comfy_dir: /opt/comfyUI
  base_port: 8188
  gpu_count: 8
  reserve_vram: 6
  force_fp16: true
  enable_manager: true
  mode: ssh
  container_prefix: comfyui-gpu
```

含义：

- `mode: ssh`：控制台通过 SSH 在算力节点上启停裸进程；`comfy_dir` 必须是**远端主机**上的路径。
- `gpu_count: 8` + `base_port: 8188`：得到 8188–8195 八个实例。
- `reserve_vram: 6`：每个实例预留约 6 GB 显存给调度/波动，不是模型权重上限。
- `force_fp16: true`：控制台侧的统一精度开关；遇到 int8/fp8 权重加载异常时，先确认它是否与模型原生量化格式冲突。
- `enable_manager: true`：允许 ComfyUI-Manager 参与；**不等于**所需自定义节点已经装好。

跨机实例可用 `instances` + `managed: false`：

```yaml
comfy:
  instances:
    - { gpu_index: 0, host: "192.168.1.101", port: 8188, managed: false }
    - { gpu_index: 1, host: "192.168.1.102", port: 8188, managed: false }
```

`managed: false` 表示平台只调度、不负责远程启停；实例身份是 `host+port`，所以不同机器可以都用 8188。

### 5.2 开发机记录：`K:\comfyui`

提交 `0219fed` 记录过本机调试实例：`K:\comfyui`、端口 `:8188`，并记录了 TripoSplat 模型文件大小与一次真实冒烟（约 20 秒，输出 `spz` 429.8 KB）。

这只能说明**当时的开发机**，不是生产部署默认。生产仍以 `/opt/comfyUI` 为准；如果在本机 `local` 模式下调试，才把 `comfy_dir` 指向 `K:\comfyui`。

## 6. 版本与实例能力核对

### 6.1 为什么不能只看仓库快照

仓库内嵌 `comfyui/` 是 `0.30.0` 快照，它包含 Hunyuan3D、TripoSplat、BiRefNet、Gaussian Splat、SAM3 核心节点，但**不包含**：

- `TextEncodeFibo2`
- `CLIPLoader` 的 `fibo2` 枚举
- `TextEncodeQwenImage21`
- `QwenImage21Cache`
- `INPAINT_MaskedFill`（本来就是 Acly 的自定义节点）
- `easy sam3ModelLoader` / `easy sam3ImageSegmentation`
- `RMBG` / `AILab_SDMatte`

所以：

- 若生产实例是 0.30.0 同代内核，`image_inpaint`、`image_to_3d_triposplat`、抠图、两个 SAM3 模板都不能直接跑。
- 若生产实例是较新的内核（当前上游 master 已确认包含 `TextEncodeQwenImage21`、`QwenImage21Cache`、`ComfySwitchNode`；`TextEncodeFibo2` 据公开合并记录已进入上游，但本文未在目标版本实测），则通常只差三个自定义节点包。
- 若生产实例是 0.34.6 一类较旧内核，Qwen-Image 2.1 有社区 backport 方案（如 `Ccm-Tim/ComfyUI-QwenImage21-Backport`），但它是**特定旧内核的兼容补丁**，描述称升级到 v0.37+ 后自动失效；本文不把它作为默认推荐，使用前必须自行核实版本、维护状态与许可。

### 6.2 只读核对：版本与节点

在能访问实例的机器上执行（只读，不改任何东西）：

```bash
for p in $(seq 8188 8195); do
  echo "== :$p =="
  curl -fsS "http://127.0.0.1:$p/system_stats" \
    | jq -r '.system.comfyui_version // "unknown"'
  curl -fsS "http://127.0.0.1:$p/object_info" \
    | jq 'keys | length'
done
```

检查模板需要的节点是否存在：

```bash
curl -fsS http://127.0.0.1:8188/object_info \
  | jq -r 'keys[]' \
  | grep -E '^(TextEncodeFibo2|TextEncodeQwenImage21|QwenImage21Cache|INPAINT_MaskedFill|easy sam3ModelLoader|easy sam3ImageSegmentation|AILab_SDMatte|RMBG|ComfySwitchNode)$' \
  | sort
```

预期应看到 9 行；缺哪一行，就对应哪个模板不可执行。

检查 `CLIPLoader` 的枚举值：

```bash
curl -fsS http://127.0.0.1:8188/object_info/CLIPLoader \
  | jq -r '.CLIPLoader.input.required.type[0][]' \
  | grep -E '^(krea2|fibo2|qwen_image)$'
```

预期至少看到 `krea2`、`qwen_image`；`fibo2` 只有较新内核才有。

检查模型是否被实例识别（不是检查磁盘，而是检查 `folder_paths` 的扫描结果）：

```bash
OBJ=$(mktemp)
curl -fsS http://127.0.0.1:8188/object_info > "$OBJ"

jq -r '.UNETLoader.input.required.unet_name[0][]' "$OBJ" \
  | grep -E 'triposplat|rmbg3|qwen_image_2.1|Krea2' || true

jq -r '.VAELoader.input.required.vae_name[0][]' "$OBJ" \
  | grep -E 'triposplat_vae_decoder|flux2-vae|qwen_image_vae|qwen_image_2.1_vae' || true

jq -r '."LoadBackgroundRemovalModel".input.required.bg_removal_name[0][]' "$OBJ" \
  | grep -F 'birefnet.safetensors' || true

jq -r '."easy sam3ModelLoader".input.required.model[0][]' "$OBJ" 2>/dev/null \
  | grep -F 'sam3' || true

jq -r '.RMBG.input.required.model[0][]' "$OBJ" 2>/dev/null || true

jq -r '."AILab_SDMatte".input.required.model[0][]' "$OBJ" 2>/dev/null || true

rm -f "$OBJ"
```

> 若节点不存在，`jq` 会报 `Cannot index`；用 `2>/dev/null || true` 只是为了让脚本继续，不代表节点存在。最终以第 6.2 节的节点列表为准。

### 6.3 只读核对：模板渲染

模板渲染测试是仓库侧最接近“提交前校验”的手段：

```bash
go -C backend test ./internal/service -run 'TestNewImageWorkflows|TestQwenImage21' -count=1
```

这一步只证明 Go 侧能把模板渲染成 JSON，不证明 ComfyUI 能执行。真正执行仍以实例 `/prompt` 的结果为准（本次未执行）。

## 7. 显存与性能注意事项

### 7.1 生产硬件与调度

- `docker-compose.yml` 注释写明适用 8×NVIDIA L40；L40 单卡 48 GB，8 卡实例并行。
- `reserve_vram: 6` 意味着每个实例要留约 6 GB 余量；不要按“48 GB 全部给模型”估算。
- 多实例共享同一套 `/opt/comfyUI/models`，模型文件只存一份；但每个实例加载模型时各自占用显存/内存。

### 7.2 各链路的权重规模（不等于显存需求）

以下显存建议仅作试运行顺序参考，未在目标实例实测，不是“12GB一定能跑”或“48GB一定足够”的保证。权重磁盘大小不等于运行峰值：还需考虑激活、解码、分辨率、offload和其他任务。RMBG分支共享Flux2 VAE，磁盘预算不要重复计数。

| 链路 | 权重规模（约） | 12 GB 卡 | 48 GB L40 |
|---|---:|---|---|
| Hunyuan3D 2.1 | 7.37 GB checkpoint | 可尝试，但体素/八叉树分辨率一高就吃紧 | 宽裕；分辨率是主要成本 |
| TripoSplat + BiRefNet | 0.74 + 1.68 + 0.58 + 0.34 + 0.44 ≈ 3.8 GB | 可行，推荐 12 GB 卡优先选这个分支 | 宽裕 |
| TripoSplat + RMBG-3.0 | 上述 3.8 GB + 8.41 + 5.24 + 0.34 ≈ 17.8 GB | **不适合全链路常驻** | 可行，但要注意实例并发与 `reserve_vram` |
| Qwen-Image 2.1 重绘 | 7.26 + 9.35 + 0.68 ≈ 17.3 GB | **不适合** | 可行 |
| Krea2 重绘 | 未知 UNET + 5.24 + 0.25 | 取决于 UNET 大小 | 需实测 |
| SAM3 + SDMatte | SAM3 1.75 + SDMatte 约 12 GB 量级 | 768 分辨率可跑；1024 会明显吃紧 | 宽裕 |

### 7.3 模板描述记录的历史观测（本次未复测）

- `image_segment_sam3_matte.json` 的描述记录：RTX 4070 12 GB、`process_res=768` 时整条链路约 16 秒，边缘柔和比例 3.6%；`process_res=1024` 曾观测到 5 分钟级并伴随显存吃紧。
- 因此 12 GB 卡做 SAM3 + SDMatte 时，**保持 768** 是更稳的默认；不要为了“更精细”直接拉到 1024。
- Hunyuan3D 的 `resolution`（默认 4096）与 `octree_resolution`（默认 256）会显著影响显存和耗时；先按模板默认跑通，再逐级提高。
- TripoSplat 的 `num_gaussians` 默认 262144；提高会增加解码与文件体积，不是越高越好。
- Qwen-Image 2.1 的 `QwenImage21Cache` 是实验性 KV cache 节点；`device=auto` 可能把缓存放到 CPU，速度与显存表现需实测。

## 8. 许可证注意事项

> 以下是工程盘点，不是法律意见。最终以各仓库 `LICENSE`、模型卡和贵司法务意见为准。

| 组件 | 许可 | 关键注意 |
|---|---|---|
| ComfyUI 本体 | GPL-3.0 | 仓库内 `comfyui/LICENSE` 为 GPL-3.0；分发/修改需评估 GPL 义务 |
| ComfyUI-RMBG | GPL-3.0 | 自定义节点；内部部署通常不触发分发，但对外分发需评估 |
| comfyui-inpaint-nodes | GPL-3.0 | 同上 |
| ComfyUI-Easy-Sam3 | Apache-2.0 | 相对宽松；仍需保留声明 |
| TripoSplat 代码/权重 | MIT | 以 `VAST-AI/TripoSplat` 模型卡为准 |
| Hunyuan3D 2.1 | Tencent Hunyuan Community License（`license: other`） | 含商用条件/地域等条款，不能按 MIT/Apache 处理 |
| SAM3.1 | Meta SAM License（`license: other`） | 自定义条款，不是 Apache/MIT；商用前逐条阅读 |
| Qwen-Image 2.1 | Qwen Research License（`license: other` / `qwen-research`） | **不要默认可商用**；商用需按 Qwen 官方许可取得授权 |
| Qwen3-VL 文本编码器 | Apache-2.0 | `Comfy-Org/Qwen3-VL` 仓库标注 |
| RMBG-3.0 权重 | **CC BY-NC 4.0** | **仅非商用**；`image_to_3d_triposplat` 默认开启 RMBG-3.0，商用必须关掉 `use_rmbg3` 或取得 Bria 商业授权 |
| BiRefNet | MIT（以 `ZhengPeng7/BiRefNet` 为准） | 12 GB 卡和商用场景更稳的替代 |
| SDMatte | MIT（上游仓库） | 转换/打包权重以实际来源为准 |
| RMBG-2.0 | CC BY-NC 4.0 | 若 `RMBG` 节点切到 RMBG-2.0，同样是非商用 |
| BEN2 / INSPYRENET / BEN | 各自模型卡 | 不要因为“节点里能选”就默认可商用 |
| Krea2 权重 | **未核实** | 来源、许可、哈希都未确认；在补齐前不要用于对外商用 |

**商用建议（仅工程角度）**：

1. 3D 链路优先用 `image_to_3d_triposplat_birefnet`，避开 RMBG-3.0 的 CC BY-NC。
2. 抠图优先 BiRefNet；若必须用 BEN2/RMBG-2.0，逐个确认许可。
3. Qwen-Image 2.1 与 Hunyuan3D 2.1 都标 `license: other`，商用前先走法务，不要只看“开源可下载”。
4. 自定义节点是 GPL-3.0 时，评估“内部使用”与“对外分发产品”的边界。

## 9. 配置注意事项

### 9.1 模型路径与大小写

模板的模型名是相对 `models/` 的路径，**大小写和子目录都必须一致**：

- `models/sam3/`：小写 `sam3`，由 Easy-Sam3 注册。
- `models/RMBG/`：大写 `RMBG`，由 ComfyUI-RMBG 注册。
- `models/RMBG/SDMatte/`：`SDMatte` 大小写敏感。
- `qwen-image2.1/`、`krea2/` 是加载器类别目录之下的子目录，例如 `models/diffusion_models/qwen-image2.1/` 与 `models/text_encoders/krea2/`，不是直接放到 `models/qwen-image2.1/`。
- `models/background_removal/`：小写下划线，是 ComfyUI 标准目录键。

### 9.2 反斜杠默认值在 Linux 上的风险

`image_inpaint.json`、`image_inpaint_krea2.json`、`image_to_3d_triposplat.json` 的默认值用了 Windows 风格反斜杠，例如：

```text
qwen-image2.1\qwen_image_2.1_int8_convrot.safetensors
krea2\qwen3vl_4b_fp8_scaled.safetensors
```

这些默认值在 Windows 开发机（`K:\comfyui`）上正常；但生产是 Linux `/opt/comfyUI`。ComfyUI 的 `folder_paths.get_full_path()` 在 Linux 上按 POSIX 路径拼接，反斜杠不会被当作目录分隔符，可能直接找不到文件。

上线前必须做其中一项：

- 在模板/UI 参数里把 `\` 改成 `/`；
- 或把模型放到对应类别目录根下（如 `models/diffusion_models/`、`models/text_encoders/`），同时修改模板参数为不带子目录的文件名；不要直接放到 `models/` 根目录；
- 或在实例上确认实际提交的 `unet_name`/`clip_name` 已被规范化。

这是本次盘点里最容易被“Windows 冒烟成功”掩盖的配置风险。

### 9.3 `extra_model_paths.yaml`

如果模型不放在 `/opt/comfyUI/models` 下，可以用 `extra_model_paths.yaml` 集中挂载。示例：

```yaml
artcraft:
  base_path: /opt/comfyUI
  checkpoints: models/checkpoints
  diffusion_models: |
    models/diffusion_models
    models/unet
  text_encoders: |
    models/text_encoders
    models/clip
  vae: models/vae
  clip_vision: models/clip_vision
  background_removal: models/background_removal
```

注意：

- `background_removal` 是 ComfyUI 标准键，示例配置里已有。
- `sam3` 与 `RMBG` 是插件自己注册的目录键，**不一定**能被 `extra_model_paths.yaml` 识别；最稳妥是直接放在实例 `models/sam3`、`models/RMBG` 下。
- 修改后重启实例，再用 `/object_info` 看模型下拉是否出现；不要只 `ls` 磁盘就认为 ComfyUI 已识别。

### 9.4 自动下载与代理

- `RMBG` 节点首次运行会自动下载所选模型到 `models/RMBG`；生产机若无外网，需要提前离线放置，否则首次任务会卡在下载。
- `ComfyUI-Manager` 的 `enable_manager: true` 只表示允许管理，不保证能访问 GitHub/Hugging Face。
- 内网部署时，建议先把模型与自定义节点同步到本地，再断网运行；不要把“首次运行自动下载”当成部署方案。

### 9.5 实例重启与缓存

安装自定义节点或新增模型后：

1. 只重启对应 GPU 实例，避免影响其他实例；
2. 重启后先查 `/object_info`，再提交任务；
3. 若节点存在但模型下拉为空，检查目录权限、大小写、`extra_model_paths.yaml` 与实例启动用户；
4. 若模型下拉有但执行报 shape/type 错误，优先核对权重版本（int8/bf16/fp8）与节点期望是否一致。

## 10. 未核实 / 待确认清单

以下项目在本次盘点中**没有**得到确定结论，部署前必须补齐：

| # | 待确认项 | 影响 | 建议核对方式 |
|---|---|---|---|
| 1 | 生产实例的实际 ComfyUI 版本 | 决定 `TextEncodeFibo2`、`TextEncodeQwenImage21`、`QwenImage21Cache` 是否存在 | `GET /system_stats` 的 `comfyui_version` |
| 2 | 生产实例是否已安装 ComfyUI-RMBG、Easy-Sam3、inpaint-nodes | 决定抠图/分割/重绘是否可执行 | `GET /object_info` 查节点 ID |
| 3 | 生产实例是否已有 8 个模板要求的模型文件 | 决定任务是否会在加载阶段失败 | `/object_info` 的模型下拉 + 磁盘核对 |
| 4 | `Krea2-MuseByStable_v15Turbo_fp8.safetensors` 的来源、许可、哈希 | 法务与可复现性 | 向权重提供方索取；不要从不明镜像下载 |
| 5 | `SDMatte.safetensors` 的实际转换来源与哈希 | 可复现性与许可 | 以 ComfyUI-RMBG 模型管理下载结果为准；上游是 `.pth` |
| 6 | `sam3.1_multiplex_fp16.safetensors` 能否被 Easy-Sam3 直接加载 | SAM3 两模板能否跑 | 实例实测；失败时对照 Easy-Sam3 README 的 `sam3-fp16.safetensors` |
| 7 | `qwen3vl_4b_fp8_scaled.safetensors` 与 `fibo2` 的实际兼容性 | TripoSplat RMBG-3.0 分支 | 实例实测；对照 Bria/Comfy-Org 的 README |
| 8 | `INPAINT_MaskedFill` 的参数名是否与模板完全一致 | Qwen 重绘 | `/object_info` 查 `INPAINT_MaskedFill` 的 inputs |
| 9 | 生产 L40 上的实际耗时与显存峰值 | 容量规划 | 用单条真实任务测；不要用 4070 的 16 秒直接外推 |
| 10 | `force_fp16: true` 与 int8/fp8 权重的交互 | 加载/精度异常 | 对比开启/关闭后的加载日志与结果 |
| 11 | GPL-3.0 自定义节点的分发边界 | 合规 | 法务评估；本文不提供法律结论 |
| 12 | 反斜杠默认值在生产 Linux 上的实际行为 | 模型找不到 | 提交一条任务看 `unet_name` 是否被实例识别 |

## 11. 只读验收清单

前六项为只读预检；最小生成任务会写入输出并占用GPU，属于后续人工验收，不是只读操作。不要在没有备份的情况下改现有配置；本文未执行任何生成。

- [ ] 确认目标实例的 `comfyui_version`。
- [ ] 确认 9 个关键节点 ID 是否存在（第 6.2 节）。
- [ ] 确认 `CLIPLoader` 枚举包含 `krea2`、`qwen_image`，以及（如用 RMBG-3.0）`fibo2`。
- [ ] 确认 `UNETLoader` / `VAELoader` / `CLIPVisionLoader` 的下拉里能看到对应模型。
- [ ] 确认 `sam3`、`RMBG`、`RMBG/SDMatte` 目录大小写与模板一致。
- [ ] 确认 `birefnet.safetensors` 在 `LoadBackgroundRemovalModel` 下拉中。
- [ ] 用一条最小任务分别验证：Hunyuan3D、TripoSplat-BiRefNet、抠图、SAM3、SAM3+SDMatte、Krea2 重绘、Qwen 重绘。
- [ ] 记录每条任务的耗时、显存峰值、输出文件大小与失败日志。
- [ ] 商用前完成许可证复核，尤其是 RMBG-3.0、Qwen-Image 2.1、Hunyuan3D 2.1、SAM3.1 与 Krea2 权重。

---

## 附录 A：8 个模板的节点清单（按模板去重）

| 模板 | 节点类名 |
|---|---|
| `image_inpaint` | `UNETLoader`、`CLIPLoader`、`VAELoader`、`LoadImage`、`ImageToMask`、`GrowMask`、`INPAINT_MaskedFill`、`TextEncodeQwenImage21`、`QwenImage21Cache`、`VAEEncodeForInpaint`、`KSampler`、`VAEDecode`、`SaveImage` |
| `image_inpaint_krea2` | `UNETLoader`、`CLIPLoader`、`VAELoader`、`LoadImage`、`ImageToMask`、`GrowMask`、`CLIPTextEncode`、`InpaintModelConditioning`、`KSampler`、`VAEDecode`、`SaveImage` |
| `image_remove_background` | `LoadImage`、`RMBG`、`SaveImage` |
| `image_segment_sam3` | `LoadImage`、`easy sam3ModelLoader`、`easy sam3ImageSegmentation`、`JoinImageWithAlpha`、`SaveImage` |
| `image_segment_sam3_matte` | `LoadImage`、`easy sam3ModelLoader`、`easy sam3ImageSegmentation`、`AILab_SDMatte`、`SaveImage` |
| `image_to_3d_hunyuan3d` | `ImageOnlyCheckpointLoader`、`LoadImage`、`CLIPVisionEncode`、`Hunyuan3Dv2Conditioning`、`ModelSamplingAuraFlow`、`EmptyLatentHunyuan3Dv2`、`KSampler`、`VAEDecodeHunyuan3D`、`VoxelToMesh`、`SaveGLB` |
| `image_to_3d_triposplat` | `LoadImage`、`UNETLoader`、`CLIPLoader`、`TextEncodeFibo2`、`TripoSplatPreprocessImage`、`CLIPVisionLoader`、`VAELoader`、`TripoSplatConditioning`、`KSampler`、`VAEDecodeTripoSplat`、`SplatToFile3D`、`SaveGLB`、`VAEDecode`、`ImageToMask`、`ThresholdMask`、`MaskComposite`、`LoadBackgroundRemovalModel`、`RemoveBackground`、`ComfySwitchNode` |
| `image_to_3d_triposplat_birefnet` | `LoadImage`、`LoadBackgroundRemovalModel`、`RemoveBackground`、`TripoSplatPreprocessImage`、`CLIPVisionLoader`、`VAELoader`、`UNETLoader`、`TripoSplatConditioning`、`KSampler`、`VAEDecodeTripoSplat`、`SplatToFile3D`、`SaveGLB` |

## 附录 B：外部来源索引

- Hunyuan3D 2.1 权重：[https://huggingface.co/Comfy-Org/Hunyuan-3D-2.1](https://huggingface.co/Comfy-Org/Hunyuan-3D-2.1)
- TripoSplat 权重：[https://huggingface.co/VAST-AI/TripoSplat](https://huggingface.co/VAST-AI/TripoSplat)
- SAM3.1 权重：[https://huggingface.co/Comfy-Org/sam3.1](https://huggingface.co/Comfy-Org/sam3.1)
- SAM3 上游：[https://huggingface.co/facebook/sam3](https://huggingface.co/facebook/sam3)
- Qwen-Image 2.1 权重：[https://huggingface.co/Comfy-Org/Qwen-Image-2.1](https://huggingface.co/Comfy-Org/Qwen-Image-2.1)
- Qwen3-VL 文本编码器：[https://huggingface.co/Comfy-Org/Qwen3-VL](https://huggingface.co/Comfy-Org/Qwen3-VL)
- Qwen-Image VAE（Krea2 模板用）：[https://huggingface.co/Comfy-Org/Qwen-Image_ComfyUI](https://huggingface.co/Comfy-Org/Qwen-Image_ComfyUI)
- RMBG-3.0（CC BY-NC 4.0）：[https://huggingface.co/briaai/RMBG-3.0-Comfy](https://huggingface.co/briaai/RMBG-3.0-Comfy)
- ComfyUI-RMBG：[https://github.com/1038lab/ComfyUI-RMBG](https://github.com/1038lab/ComfyUI-RMBG)
- ComfyUI-Easy-Sam3：[https://github.com/yolain/ComfyUI-Easy-Sam3](https://github.com/yolain/ComfyUI-Easy-Sam3)
- comfyui-inpaint-nodes：[https://github.com/Acly/comfyui-inpaint-nodes](https://github.com/Acly/comfyui-inpaint-nodes)
- SDMatte 上游：[https://huggingface.co/LongfeiHuang/SDMatte](https://huggingface.co/LongfeiHuang/SDMatte)
- BiRefNet 上游：[https://huggingface.co/ZhengPeng7/BiRefNet](https://huggingface.co/ZhengPeng7/BiRefNet)