# IndexTTS Rust 端补充开发任务

> 日期：2026-10-04
> 目标仓库：`index-tts-rust`
> 目标：补齐供本地 Go 制作平台调用的 IndexTTS-2.5 原生能力，运行时完全不依赖 Python。

## 1. 项目边界

Rust 端只负责：

```text
文本 + PreparedVoice + 情感/时长参数
→ PCM 音频 + 可审核诊断数据
```

以下能力由 Go 制作平台负责，不应在 Rust 仓库重复实现：

- Episode 配音批次数据库；
- Dialogue 状态、输入 Hash 和 `audio_token` CAS；
- SRT 预览令牌及事务式应用；
- 配音候选持久化、审核和采用；
- 项目、Episode、Scene、Shot、Dialogue 业务模型；
- 字幕时间轴；
- 角色分轨和 FFmpeg 整集混音；
- HTTP 权限、项目隔离和前端界面。

必须继续保证：

- 不依赖 Python 运行时；
- 不修改上游 IndexTTS 源码和模型文件；
- 无 DLL、无 CUDA、无模型时，调用方能安全禁用本地 TTS，而不是进程崩溃；
- 旧 C ABI 保持兼容；新增能力优先使用 V2 API。

---

## 2. 当前已具备能力

当前已完成并验证：

- Rust CLI CUDA 推理；
- C ABI CPU/CUDA设备选择；
- Go `LoadWithOptions`；
- `Go → cgo → indextts.dll → Candle CUDA + ORT CUDA → waveform`；
- 22,050 Hz单声道Float PCM；
- Greedy语义生成；
- 模型级合作式取消；
- Windows CUDA运行时打包脚本；
- 普通参考WAV和预置音色生成。

当前主要不足：

- `PrepareVoice`没有缓存实际conditioning；
- 无正式情感输入；
- 无ABI能力查询；
- 取消是Model级而非请求级；
- 无生成诊断元数据；
- 无可靠目标时长控制；
- 长文本缺少原生分段；
- Runtime没有强制校验模型manifest；
- Go module不能独立发布；
- 多Model仍受全局锁限制。

---

# 3. P0：必须优先完成

## 3.1 真正的参考音频条件缓存

### 当前问题

当前：

```go
model.PrepareVoice(referencePath)
```

只验证参考音并保存路径。每次`Generate`仍重新执行：

- 打开WAV；
- 解码和重采样；
- Wav2Vec2-BERT编码；
- CAMPPlus编码；
- Speaker Conditioning计算。

同一角色批量生成对白时会重复计算。

### 目标设计

```rust
pub struct PreparedVoice {
    pub reference_hash: [u8; 32],
    pub speaker_embedding: Tensor,
    pub speech_conditioning_latent: Tensor,
    pub semantic_conditioning: Tensor,
    pub reference_duration: f32,
    pub source_sample_rate: u32,
    pub source_channels: u32,
}
```

生成阶段直接消费`PreparedVoice`，不得再次读取原WAV。

### 缓存规则

- 按参考音频内容SHA-256去重；
- 同一Model内相同Hash复用conditioning；
- Voice handle持有缓存引用；
- `Voice.Close()`减少引用；
- `Model.Close()`清空全部缓存；
- 必须有容量或内存上限；
- 支持LRU或明确淘汰策略；
- 已创建Voice不受原WAV删除或覆盖影响。

### C ABI

保留现有接口：

```c
int32_t indextts_voice_prepare(
    indextts_model_t model,
    const char *reference_audio_path,
    indextts_voice_t *out_voice
);
```

新增内存输入：

```c
int32_t indextts_voice_prepare_pcm(
    indextts_model_t model,
    const float *samples,
    size_t sample_count,
    uint32_t sample_rate,
    uint32_t channels,
    indextts_voice_t *out_voice
);
```

新增Voice信息：

```c
typedef struct {
    char reference_sha256[65];
    float duration_seconds;
    uint32_t source_sample_rate;
    uint32_t source_channels;
    uint64_t cache_bytes;
    uint64_t reserved[8];
} indextts_voice_info_t;

int32_t indextts_voice_get_info(
    indextts_voice_t voice,
    indextts_voice_info_t *info
);
```

### 验收标准

- 同一Voice连续生成10句时，参考编码只执行一次；
- 缓存前后输出一致；
- 删除或替换原WAV后，已有Voice输出不变化；
- 新音频内容生成新Hash；
- 100个Voice创建和释放后内存稳定。

---

## 3.2 正式情感条件接口

### 目标

支持IndexTTS-2.5已有情感输入：

1. 无情感控制；
2. 情感文本；
3. 情感参考音频；
4. 显式情感向量；
5. 情感强度。

Rust接口建议：

```rust
pub enum EmotionInput {
    None,
    Text {
        text: String,
        strength: f32,
    },
    Reference {
        emotion: PreparedEmotion,
        strength: f32,
    },
    Vector {
        values: Vec<f32>,
        strength: f32,
    },
}
```

### C ABI

```c
typedef enum {
    INDEXTTS_EMOTION_NONE = 0,
    INDEXTTS_EMOTION_TEXT = 1,
    INDEXTTS_EMOTION_REFERENCE = 2,
    INDEXTTS_EMOTION_VECTOR = 3
} indextts_emotion_mode_t;

typedef struct {
    int32_t mode;
    const char *text;
    indextts_emotion_t reference;
    const float *vector;
    size_t vector_length;
    float strength;
    uint64_t reserved[4];
} indextts_emotion_options_t;
```

为避免破坏旧ABI，新增：

```c
int32_t indextts_generate_v2(
    indextts_model_t model,
    indextts_voice_t voice,
    const indextts_generate_options_v2_t *options,
    indextts_generation_result_t *result
);
```

不要直接改变旧`indextts_generate_options_t`的尺寸。

### Emotion和Delivery组合

Go平台会提供：

```text
emotion: 担忧
delivery: 压低声音、语速克制、句尾迟疑
```

Rust可以组合为：

```text
担忧；压低声音、语速克制、句尾迟疑
```

但不得修改朗读正文。

### 验收标准

固定测试至少覆盖：

- 中性；
- 温柔；
- 愤怒；
- 悲伤；
- 恐惧；
- 克制担忧。

要求：

- 朗读正文保持一致；
- 情感条件确实改变输出；
- 同seed、同条件可复现；
- `strength=0`接近无情感控制；
- 不支持的模式明确返回能力错误，不得静默忽略。

---

## 3.3 ABI版本、能力查询和模型信息

### ABI版本

```c
uint32_t indextts_abi_version(void);
```

编码：

```text
major << 16 | minor
```

兼容规则：

- ABI major不同：调用方拒绝加载；
- minor低于功能要求：只禁用对应能力；
- 不得因结构体错位导致进程崩溃。

### 能力查询

```c
typedef struct {
    uint32_t abi_major;
    uint32_t abi_minor;
    uint32_t sample_rate;
    uint32_t max_reference_seconds;
    uint32_t max_semantic_tokens;
    uint32_t max_concurrent_requests_per_model;

    int32_t supports_cuda;
    int32_t supports_cpu;
    int32_t supports_cancellation;
    int32_t supports_request_cancellation;
    int32_t supports_voice_cache;
    int32_t supports_emotion_text;
    int32_t supports_emotion_reference;
    int32_t supports_emotion_vector;
    int32_t supports_target_duration;
    int32_t supports_sampling;
    int32_t supports_beam_search;

    uint64_t reserved[8];
} indextts_capabilities_t;

int32_t indextts_get_capabilities(
    indextts_capabilities_t *capabilities
);
```

### 模型信息

```c
typedef struct {
    char runtime_version[64];
    char model_version[64];
    char model_manifest_sha256[65];
    char backend[32];
    char device[32];
    uint64_t reserved[8];
} indextts_model_info_t;

int32_t indextts_model_get_info(
    indextts_model_t model,
    indextts_model_info_t *info
);
```

### Go wrapper

```go
type Capabilities struct {
    ABIMajor                 uint32
    ABIMinor                 uint32
    SampleRate               uint32
    MaxReferenceSeconds      uint32
    MaxSemanticTokens        uint32
    MaxConcurrentRequests    uint32
    SupportsCUDA             bool
    SupportsCancellation     bool
    SupportsRequestCancel    bool
    SupportsVoiceCache       bool
    SupportsEmotionText      bool
    SupportsEmotionReference bool
    SupportsEmotionVector    bool
    SupportsTargetDuration   bool
}

type ModelInfo struct {
    RuntimeVersion string
    ModelVersion   string
    ManifestSHA256 string
    Backend        string
    Device         string
}

func Capabilities() (Capabilities, error)
func (m *Model) Info() (ModelInfo, error)
```

---

## 3.4 请求级可靠取消

### 当前问题

现有`Model.Cancel()`会影响整个Model：

- 等待锁的请求可能取消当前请求；
- 多请求无法准确归属；
- 提前取消可能被生成开始时重置；
- 返回成功前缺少最终context确认。

### 推荐异步请求ABI

```c
typedef void *indextts_request_t;

int32_t indextts_generate_begin(
    indextts_model_t model,
    indextts_voice_t voice,
    const indextts_generate_options_v2_t *options,
    indextts_request_t *request
);

int32_t indextts_request_wait(
    indextts_request_t request,
    indextts_generation_result_t *result
);

int32_t indextts_request_cancel(indextts_request_t request);
void indextts_request_free(indextts_request_t request);
```

如果暂不实现异步ABI，至少要做到：

- 每次生成有唯一request token；
- Cancel只影响指定request；
-进入推理前检查取消；
-每个GPT token检查；
-每个CFM步骤检查；
-各阶段前后检查；
-返回音频前再次检查。

### 验收标准

- 取消A不影响B；
-取消排队请求不影响当前请求；
-取消后Model可继续生成；
-返回明确`cancelled`状态；
-无内存和显存泄漏。

---

# 4. P1：生产质量能力

## 4.1 生成诊断元数据

```c
typedef struct {
    uint32_t semantic_token_count;
    float generated_seconds;
    float reference_encode_ms;
    float gpt_ms;
    float semantic_codec_ms;
    float s2mel_ms;
    float bigvgan_ms;
    float total_ms;
    float peak;
    float rms;
    float silence_ratio;
    uint64_t seed;
    uint64_t reserved[8];
} indextts_generation_info_t;

typedef struct {
    indextts_audio_out_t audio;
    indextts_generation_info_t info;
} indextts_generation_result_t;
```

Go接口：

```go
type GenerationInfo struct {
    SemanticTokens   uint32
    GeneratedSeconds float32
    ReferenceEncode  time.Duration
    GPT              time.Duration
    SemanticCodec    time.Duration
    S2Mel            time.Duration
    BigVGAN           time.Duration
    Total             time.Duration
    Peak              float32
    RMS               float32
    SilenceRatio      float32
    Seed              uint64
}
```

用途：

- 配音候选审计；
- 性能统计；
- 检测静音、削波和异常短音频；
- 前端显示生成耗时。

---

## 4.2 可控目标时长

保留现有`duration_factor`，新增：

```c
float target_duration_seconds;
float duration_tolerance_seconds;
int32_t duration_policy;
```

策略：

```text
INDEXTTS_DURATION_NATURAL
INDEXTTS_DURATION_TARGET_SOFT
INDEXTTS_DURATION_TARGET_STRICT
```

要求：

- `NATURAL`保持现有行为；
- `TARGET_SOFT`尽可能接近目标；
- `TARGET_STRICT`无法合理满足时明确失败；
- 禁止裁掉结尾；
- 禁止删除、概括或改写正文；
- 返回实际时长和偏差。

若模型无法可靠控制，应返回：

```text
supports_target_duration=false
```

不能伪造支持。

---

## 4.3 长文本确定性分段

新增单独API，不改变普通`generate`行为。

分段优先级：

1. `。！？`；
2. `；`；
3. `，`；
4. 最大token边界。

Rust结构建议：

```rust
pub struct TextSegment {
    pub text: String,
    pub start_char: usize,
    pub end_char: usize,
}
```

要求：

- 全部分段拼接后等于规范化原文；
- 不漏字、不增字；
- 每段不超过semantic token/bucket限制；
- 支持可配置段间停顿；
- 返回各段文本区间、语义token数和音频时长；
- 中途失败指出具体分段。

验收文本长度：

```text
500字、1000字、2000字
```

---

## 4.4 模型Manifest强校验

启动时强制校验：

- 必需文件；
- SHA-256；
- 模型格式版本；
-最低Runtime版本；
- ONNX opset；
-输入输出名称；
- shape和dtype；
- S2Mel/BigVGAN bucket列表；
-采样率；
-语义token IDs及频率。

失败时：

- 不得部分加载；
-不得继续生成；
-返回结构化错误。

建议错误：

```rust
ModelManifestError
ModelHashMismatch
UnsupportedModelFormat
MissingModelComponent
IncompatibleRuntime
```

---

## 4.5 健康状态查询

```c
typedef struct {
    int32_t loaded;
    int32_t device_healthy;
    uint64_t voice_cache_entries;
    uint64_t voice_cache_bytes;
    uint64_t active_requests;
    uint64_t queued_requests;
    char last_error[512];
    uint64_t reserved[8];
} indextts_health_t;

int32_t indextts_model_health(
    indextts_model_t model,
    indextts_health_t *health
);
```

健康检查不得通过生成一句测试语音来实现。

---

# 5. P2：性能与发布

## 5.1 去除进程级全局生成锁

目标：

- 错误状态使用thread-local或handle级存储；
-每个Model仅串行自己的请求；
-不同Model/不同GPU可以并行；
-错误读取不依赖覆盖整个生成过程的全局互斥锁。

如果同Model只能单并发，能力接口必须返回：

```text
max_concurrent_requests_per_model = 1
```

---

## 5.2 标准发布包

生成两个正式包：

```text
index-tts-rust-win64-cpu
index-tts-rust-win64-cuda
```

CUDA包至少包含：

- `indextts.dll`；
- `indextts.dll.lib`；
- `libindextts.dll.a`；
- `indextts.h`；
- ONNX Runtime DLL；
- CUDA/cuDNN运行库；
-文件Hash manifest；
-许可证；
-第三方声明；
-运行环境验证脚本。

新增：

```text
verify-runtime.ps1
```

检查：

- DLL存在及SHA-256；
- NVIDIA驱动；
- GPU编号；
- ORT CUDA Provider；
- CUDA/cuDNN版本；
-模型manifest；
-最小模型加载。

不得执行正式语音生成。

名义上的CUDA 12.8发布包不应隐式依赖CUDA 13 cuBLAS；若暂时无法消除，必须在manifest和文档中明确。

---

## 5.3 Go Module可发布性

当前Go wrapper引用仓库外Header：

```go
#cgo CFLAGS: -I../../crates/indextts-ffi
```

应调整为：

```text
bindings/go/
├── go.mod
├── indextts.h
├── api.go
├── native_windows.go
├── native_stub.go
├── integration_test.go
└── README.md
```

要求：

- Go module可独立下载；
- Header位于module内部；
-普通无原生标签构建使用stub；
-只有Windows+cgo+显式标签才链接DLL；
-无DLL环境不影响调用方普通构建。

---

# 6. Go API建议

```go
type GenerateOptionsV2 struct {
    Language       string
    Seed           uint64
    DurationFactor float32
    TargetDuration time.Duration
    Tolerance      time.Duration
    DurationPolicy DurationPolicy
    Emotion        EmotionOptions
}

type GenerateResult struct {
    Audio Audio
    Info  GenerationInfo
}

func (m *Model) PrepareVoicePCM(
    samples []float32,
    sampleRate uint32,
    channels uint32,
) (*Voice, error)

func (v *Voice) Info() (VoiceInfo, error)
func (m *Model) Info() (ModelInfo, error)
func Capabilities() (Capabilities, error)

func (m *Model) GenerateV2(
    ctx context.Context,
    voice *Voice,
    text string,
    options GenerateOptionsV2,
) (GenerateResult, error)
```

旧API继续保留：

```go
func (m *Model) Generate(...)
func (m *Model) GenerateContext(...)
```

---

# 7. 测试要求

## 7.1 单元测试

- ABI版本编码；
- Capabilities结构；
- V1/V2兼容；
- PreparedVoice缓存命中与淘汰；
- PCM输入校验；
- 情感模式参数校验；
- 情感向量长度；
- Duration策略；
- 长文本分段边界；
- Manifest完整性和Hash错误；
- Request取消状态；
- GenerationInfo计算；
- 无DLL stub行为。

## 7.2 真实CUDA集成测试

至少验证：

1. 参考音conditioning只计算一次；
2. 温柔、愤怒、悲伤等条件确实改变输出；
3. 同seed同条件可复现；
4. 取消请求A不影响请求B；
5. 100句连续生成无明显CPU/显存增长；
6. Manifest损坏明确失败；
7. ABI不匹配明确失败；
8. 输出22,050 Hz、有限、非静音；
9. VoiceRef删除后PreparedVoice仍可生成；
10. 多GPU Model在移除全局锁后可以并行。

## 7.3 文本正确性

不能只验证“产生了音频”。应加入ASR或人工听测：

- 原文逐字一致性；
- 无额外对白；
- 无漏字；
- 无重复；
- 长文本分段后顺序一致。

ASR只能作为审核信号，不能改写输入文本。

---

# 8. 交付顺序

建议按下列顺序开发：

```text
1. ABI version + Capabilities
2. ModelInfo + VoiceInfo
3. PreparedVoice真正缓存
4. 情感文本接口
5. 请求级取消
6. GenerationInfo诊断
7. 目标时长能力
8. 长文本分段
9. Manifest强校验
10. 去除全局锁并支持多GPU并行
11. 标准CPU/CUDA发布包
12. 可独立发布的Go module
```

每个阶段应单独提交，避免一次大改导致ABI、模型和生成质量问题无法定位。

---

# 9. 完成定义

Rust端完成必须同时满足：

- Go平台不再硬编码运行时能力；
- Voice conditioning真实缓存；
- 情感字段真实影响模型，不再只是保存；
- 请求可以独立取消；
- 返回性能和质量诊断；
- 目标时长支持情况真实可查询；
- 长文本不会因bucket上限直接崩溃；
- 模型包在加载前经过完整校验；
- CPU/CUDA发布包可在干净目录运行；
- Go module可独立引用；
- 无DLL环境仍可普通编译；
- 不修改或重写Go平台的Dialogue/SRT/候选业务状态。

---

# 10. 最低验收命令

```powershell
cargo fmt --all -- --check
cargo test --workspace
cargo clippy --workspace --all-targets -- -D warnings
cargo build --release -p indextts-ffi --features cuda
go -C bindings/go test ./...
go -C bindings/go test -race ./...
```

真实CUDA测试命令应使用显式环境变量，并输出：

- Runtime/ABI/模型版本；
-设备；
-参考音Hash；
-缓存命中；
-语义token数；
-各阶段耗时；
-生成时长；
- Peak/RMS/Silence Ratio；
-内存与显存变化。

所有测试结果需记录在发布说明中。
