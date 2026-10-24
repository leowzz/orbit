# Windows 桌面 Agent 与原生 ASR 实现交接

更新日期：2026-10-24。状态：**基础模块已实现，真机交付尚未完成**。当前已加入本地推理配置、严格音频协议、SQLite 持久队列、speech.cpp v0.8.11 C ABI 封装、固定资产校验，以及 Core/控制台的 Vulkan/Metal 后端兼容；Windows 真机推理、MQTT worker 生命周期、Opus 解码、安装发布和 SDT 迁移仍需继续实施与验收。

## 1. 用户目标与已确定方向

用户选择 Rust，将现有 macOS Agent 移植到 Windows，并把 macOS Agent 已有能力完整带过去。Windows Agent 还可以启用本地推理：下载模型或配置模型路径后，接收 Core 的 ASR 任务并返回结果。

- 扩展现有 `agents/desktop`（Tauri 2、Rust、React/TypeScript），不另建 Python/PySide6 GUI，也不另起 Go Agent。
- GUI 保持简单，必须有系统托盘；**关闭设置窗口继续后台运行**，只有明确退出才停止进程。
- 推理是可选能力，默认关闭；普通 Agent 不应因没有模型、推理库或 GPU 而无法使用。
- Rust 调用 speech.cpp 的稳定 C ABI，采用 Qwen3-ASR GGUF 模型，不携带 Python、PyTorch、Transformers 运行环境。
- 音频最长 **20 秒**。识别结果持久化后立即投递专用结果 Topic，Core 收到即可处理；定时器仅负责重试，不能让首次投递等待轮询。
- “macOS 能力全部实现”包括 Windows 最近应用、更新、安装发布及所有已有功能的真机行为；不能以编译通过代替功能完成。

下文模块划分、独立 Worker 连接、默认 Q8 模型等属于建议实施默认值，可按验证结果调整。上述用户目标不得在实现中默默缩减。

## 2. 接手基线与真实完成边界

当前分支 `feat/days-audio`，代码基线 `a6102a9`（`feat(audio): deliver ASR results immediately and support 20-second segments`）。接手先检查实际 HEAD、工作区和部署状态，不能把本节快照当作实时状态。

| 范围 | 当前事实 | 后续工作 |
| --- | --- | --- |
| 桌面 Agent | Rust/Tauri 共用源码，有部分 Windows 分支 | 完整 Windows 实现与验收 |
| ASR Worker | 独立 Python Worker 已部署 SDT | 将协议和可靠交付能力迁入桌面 Agent |
| 20 秒、结果立即投递 | 已提交；SDT Python Worker 已更新 | 核对并分别部署 Core、Android，做完整链路验收 |
| 原生 speech.cpp | 已做上游 API、发布包和模型调研 | 尚无编译、真实推理、性能或准确率验收 |
| 最近应用 | macOS NSWorkspace；Windows 返回不支持 | Windows 原生采集实现 |
| 应用内更新 | `update.rs` 仅支持 macOS | Windows 下载、校验、替换、重启及失败恢复 |
| CI / Release | `unit-tests.yml` 桌面检查只有 macOS；`release.yml` 桌面产物只有 macOS Universal DMG | Windows 测试、NSIS 安装包、发布及更新资产 |

现有资料入口：[桌面 Agent](../agents/desktop/README.md)、[音频协议](audio-pipeline.md)、[Python Worker](../agents/asr-worker/README.md)、[SDT 部署记录](../agents/asr-worker/sdt-deployment.md)、[发布](releases.md)。代码优先于过时说明。

## 3. Windows 全能力对齐清单

以下全部在交付范围内。已有 Windows 代码意味着可复用，不代表已在真机通过。

| 能力 | 代码入口（相对 `agents/desktop/`） | Windows 实现及验收要求 |
| --- | --- | --- |
| MQTT 连接 | `src-tauri/src/runtime.rs` | MQTT 5、QoS 1、TLS/mTLS、证书校验、断线退避、休眠唤醒；状态先于 Observation，保留现有 epoch、TTL 与去重语义 |
| 身份与配置 | `src-tauri/src/config.rs` | ID 持久化、YAML 导入及凭据文件读取、原子保存；升级不重置身份；重复 ID 被接管后停止重连 |
| 凭据 | `config.rs` | Windows Credential Manager，UI 仅传新密码或是否已设置；读取失败明确报错，不降级写明文配置，不输出到日志 |
| Codex 采集 | `src-tauri/src/codex.rs` | SQLite/rollout 只读、存活与子任务识别、过滤、Windows 路径和进程兼容；默认不上传标题/项目名 |
| Codex 周限 | `codex.rs` | 发现或手动选择 `codex` 可执行文件，调用已登录的 app-server；不导出登录令牌，无额外控制台窗口 |
| Sub2API 用量 | `src-tauri/src/usage.rs` | 登录/刷新、401 恢复、429 退避、时区窗口、金额精度及异常状态 |
| 时间校准 | `src-tauri/src/clock.rs` | NTP 应用级校准、失败回退；不修改系统时钟 |
| 受限远程打开 | `src-tauri/src/commands.rs` | 目标、UUID、TTL、重复指令检查及结果回传；验证 `codex://` 关联，关联缺失可理解地失败；不提供任意命令执行 |
| 快捷入口 | `src-tauri/src/shortcuts.rs`、`src/Shortcuts.tsx` | `.exe` / `.lnk` 选择和拖入、图标提取、中文/空格路径、应用及 URL 打开、网站元信息与图标、自定义图片、编辑取消、排序、键盘操作、移除后撤销权限、Core 配对和发布 |
| 最近应用推荐 | `src-tauri/src/recent_apps.rs` | 补齐 Windows 运行应用初始化与前台切换采集；选择受支持的 Win32 API，处理权限不足、进程退出、重复路径和系统进程；只记录路径及最近时间，最多 64 个、90 天，本机保存，不读取/上传窗口标题或内容 |
| 托盘与窗口 | `src-tauri/src/lib.rs` | 打开设置、暂停/恢复、明确退出；关闭窗口隐藏且继续工作，双击/菜单能恢复，重复启动聚焦原窗口 |
| 登录启动 | `lib.rs` | 插件配置及 `--background`；在用户交互会话启动托盘，实际注销登录或重启验证；设置保持一致，不与旧计划任务双开 |
| 状态和设置 UI | `src/main.tsx`、`src/types.ts`、`src/bridge.ts`、`src/ui-polling.ts` | 连接测试、保存应用、来源健康、错误状态、只在窗口可见时轮询 UI；隐藏窗口不能影响后台服务；预览模式不能执行真实操作 |
| 应用更新 | `src-tauri/src/update.rs`、`src/SoftwareUpdate.tsx` | 实现 Windows 平台/架构资产匹配、版本比较、可信下载和哈希校验、安装退出重启；处理运行中 exe 锁、权限拒绝、取消及失败，保留配置、凭据、队列和模型 |
| 安装与发布 | `src-tauri/tauri.conf.json`、仓库 `.github/workflows/` | Windows x64 NSIS 安装、升级、卸载、快捷方式、WebView2；明确签名状态和系统提示；正式 Release 资产与 updater 约定一致 |

不改变 macOS 的现有行为。Windows 特有 API 放入平台模块；通用业务、消息校验、队列和 UI 共用。尤其避免为 Windows 推理引入的依赖破坏 macOS Intel/Apple Silicon 普通 Agent 构建。

## 4. 推理架构与生命周期

建议在 `src-tauri/src/inference/` 增加以下模块：

| 模块 | 责任 |
| --- | --- |
| `protocol.rs` | 严格音频 JSON 校验、结果/状态编码、跨语言兼容样例 |
| `store.rs` | SQLite inbox/outbox、幂等、恢复、迁移、容量限制 |
| `worker.rs` | 独立 Worker MQTT 连接、手动 ACK、心跳、立即投递及重试 |
| `engine.rs` | speech.cpp C ABI 封装、版本检查、模型/请求所有权、设备识别、取消 |
| `audio.rs` | 原生 Opus 解码及帧数/采样数验证 |
| `assets.rs` | 固定版本运行库和模型目录、下载、校验、取消、原子安装 |
| `mod.rs` | 生命周期管理、配置与状态接口；避免向 UI 暴露原始指针或敏感错误 |

使用独立推理线程持有动态库和模型；模型加载、Opus 解码、推理不阻塞 Tauri UI 或 MQTT event loop。可以使用异步协调与有界通道，但不能只把同步推理放到 async 函数里。第一版同一模型串行处理任务，限制 CPU 线程/队列，后续再根据数据增加并发。

普通 Agent 和 ASR Worker 的配置、健康状态与启停需可区分。只开启推理应能运行，不应强迫用户启用 Codex/Sub2API：现有 `Config::validate` 要求至少启用二者之一，需要调整为实际能力校验；同时检查仅使用快捷入口的场景。推理关闭时不自动下载或加载模型。

暂停推理应停止接收新任务并保留未完成任务，推荐允许已持久化结果继续投递；退出需要停止接收、取消/结束当前任务、持久化恢复状态、释放模型并等待线程结束。取消不能被当成正常转写或无条件累计为最终识别失败。模型切换先停止当前模型使用，队列不能被删除或换丢。明确托盘“暂停全部”和推理单独启停的作用范围，并用 UI 文字说明。

### 4.1 C ABI 边界

上游是社区项目 [nyosegawa/speech.cpp](https://github.com/nyosegawa/speech.cpp)，不是 Qwen 官方推理包。调研固定版本 [v0.8.11](https://github.com/nyosegawa/speech.cpp/releases/tag/v0.8.11)，以该版 [speech.h](https://github.com/nyosegawa/speech.cpp/blob/v0.8.11/include/speech.h)、[C API](https://github.com/nyosegawa/speech.cpp/blob/v0.8.11/docs/c-api.md) 与 [Qwen3-ASR 文档](https://github.com/nyosegawa/speech.cpp/blob/v0.8.11/docs/models/qwen3-asr.md) 为准。接手需重新核对平台包的实际导出，不能用滚动 main 头文件配旧 DLL。

- 建议 `libloading` 加载明确绝对路径的 `speech.dll`，通过 C 函数调用，不能绑定 C++ 类 ABI。禁止依赖当前工作目录的 DLL 搜索。
- 调研 API 为 major 3 / minor 3，语言接口从 3.1 起可用；加载时验证版本与所需符号，缺失给出错误，不崩溃。
- 流程：`speech_load_params_new` → 设置设备 → `speech_model_load` → `speech_request_new` → 设置 16 kHz 单声道 float32 音频 → `speech_transcribe` → 读取 result/text/language → 释放 request/model/params。逐一核对实际签名、返回值和生命周期。
- 动态库必须晚于所有句柄释放；RAII 包装每个拥有所有权的对象，错误路径也释放。复制返回的文本后才能销毁其所属对象。不随意给裸指针包装添加 `Send` / `Sync`。
- 使用受支持的进度回调/`speech_request_cancel` 取消；跨线程取消必须保证请求仍存活。回调不能 unwind 穿过 C ABI。
- 检查结果停止原因，模型长度上限或取消后的部分输出不能伪装成成功结果。不能捏造词级时间戳；若只有全文，沿用整段时间范围。
- 从模型信息读取实际后端（如 CPU、Vulkan0、MTL0），映射为协议设备；用户显式选择 GPU 时，失败应可见，不能静默降级却显示 GPU。
- UI、日志和协议只给受控错误码/摘要，原生库错误可能包含路径或文本，不能原样外发。

若直接 FFI 经真机发现不可恢复崩溃或停止问题，可用 Rust 原生辅助进程隔离；仍不能重新引入 Python。记录采用原因、通信与进程清理策略。

### 4.2 原生音频解码

采用 libopus 的 Rust 封装或明确的 C ABI，验证 Windows MSVC/CMake 编译和分发依赖。输入为 base64 包装的**裸 Opus 帧，不是 Ogg 文件**：16 kHz、单声道、每帧 20 ms / 320 个采样，最多 1000 帧 / 20 秒，每包 1–1275 字节。每个音频段使用正确的解码器状态，校验实际输出采样数，不能因帧损坏越界、无限分配或默默拼出错误时长。

## 5. MQTT、权限与持久化交付契约

Python Worker 是迁移的行为参照：[protocol.py](../agents/asr-worker/orbit_asr/protocol.py)、[store.py](../agents/asr-worker/orbit_asr/store.py)、[worker.py](../agents/asr-worker/orbit_asr/worker.py)。Core 权威实现为 [audio.go](../internal/core/audio.go)，权限见 [mqttadmin/client.go](../internal/mqttadmin/client.go) 及其音频测试。

### 5.1 连接与 Topic

建议复用 broker/TLS 配置，单独保存 Worker ID、Worker 用户名/密码，使用独立 MQTT 客户端。普通 Agent 当前没有消费全局音频任务的权限，不能直接复用账号并假设订阅成功，也不能为实现方便放宽所有 Agent ACL。首版延续现有 Worker 角色，若改为显式多能力账号，必须同时设计 Core 配置、ACL 和隔离测试。

| 用途 | Topic / 规则 |
| --- | --- |
| 领取任务 | 订阅 `$share/audio-asr/orbit/v1/audio/jobs`；收到的实际 Topic 为 `orbit/v1/audio/jobs` |
| 发布结果 | `orbit/v1/agents/{worker_id}/audio/results` |
| Core 应用确认 | 订阅 `orbit/v1/agents/{worker_id}/audio/acks`；`schema_version: 1`、`job_id`、`status: stored` |
| Worker 状态 | `orbit/v1/agents/{worker_id}/audio/state`，retained，约 20 秒心跳与 offline 遗嘱 |

使用 MQTT 5、QoS 1、UTF-8 JSON、schema 1；jobs/results/acks 非 retained。订阅成功才能显示可接单，连接成功不等于具备 Worker 权限。现有桌面连接 `max_packet_size=32768`，ASR payload 上限是 128 KiB；配置 MQTT packet 上限时还需考虑 Topic 和属性等协议开销，业务 JSON 仍严格限制 128 KiB。

`job_id` 必须等于 segment UUID；校验 node/session/segment、UTC 时间、整数范围、20 秒时长、帧数与字段类型，拒绝重复 JSON 键。布尔值不能当整数接受。畸形/retained 毒消息受控丢弃并 ACK，避免永久重投；合法任务只有成功持久化或确认是同内容重复后才能 PUBACK。数据库失败/队列满时不能确认未保存的新任务。

### 5.2 队列状态机

```text
收到合法 job → SQLite queued 提交 → MQTT PUBACK
queued → processing（记录尝试次数）→ 推理
成功或最终失败 → outbox 提交 → 立即 publish results
outbox → 重试（默认 5 秒）→ 收到 Core stored ACK → done 墓碑
进程重启：processing → queued；outbox 继续投递
```

结果投递是独立于采集 Observation 的可靠链路，不能混入会在重连时丢弃的临时发送队列。broker PUBACK **不等于** Core 已保存；只有 Core 应用 ACK 才能清除 outbox。发送失败保留结果；崩溃于保存和发送之间必须能恢复。Core 已有按 job 幂等、首个终态结果生效及约 30 秒未完成任务重发机制，迁移不能破坏它。

延续 SQLite WAL、`synchronous=FULL`、最多 1000 个非 done 任务（queued/processing/outbox 均计入）、默认最多 3 次推理尝试。保留 job ID + digest 的 done 墓碑，不能简单清库导致老任务再次识别。表可沿用 `jobs(id,digest,payload,status,attempts,result)` 与 status 索引；新增关系采用应用事务和逻辑关联，不加物理外键。

队列按 broker 身份域和 Worker ID 隔离；更换账号/服务器不能把旧结果投到另一个环境。模型路径改变不应换掉同一身份的队列。复用旧 Python 数据库时，digest 必须与 Python 的 UTF-8、递归排序键、紧凑 JSON 序列化结果一致；用跨语言样例验证，包含中文与重复 job，不凭“都是 JSON”判断兼容。

### 5.3 Core 与界面必须同步更新

Core 当前在结果和 Worker 心跳中仅接受 `cpu` / `cuda`，需要增加真实后端 `vulkan` / `metal` 支持及验证测试；保留旧 Python CUDA 兼容。`web/core-console/src/pages/Audio.tsx` 当前用 CUDA 判断 GPU，其余显示 CPU，需要同步改为完整设备映射。不要把 NVIDIA 卡上的 Vulkan 写成 CUDA。

结果保持现有完整字段及长度限制：job/segment/node/agent 标识、状态、text/language/segments、model/device、耗时、完成时间、错误信息。空转写和失败路径沿用 Core 当前语义，不额外生成无意义收件箱条目。先部署兼容后端名称的 Core，再切换正式原生 Worker。

## 6. 模型、运行库与简单设置界面

新增“本地推理”设置页即可：启用开关、模型下载/选择、运行库就绪情况、设备自动/GPU/CPU、实际后端、Worker 身份及凭据、当前状态、任务/待确认数量、最近受控错误。高级项可以折叠；用户不需要理解 DLL 或 C ABI 才能启用。

下载显示大小、进度、取消和重试；本地 GGUF 可通过文件选择器配置。加载失败或文件不存在时保留路径并给出可操作原因。已下载的运行库和模型应支持离线重新启动；关闭功能不能删资产。提供小样本自检有助定位环境问题，但不上传用户本地文件。

### 6.1 调研时可固定的资产

以下为 2026-10-24 调研记录，不是本项目运行验收结果。实施时重新核对上游文件、许可证和哈希，并在代码/受控清单中固定版本；后续更新应一起更新测试与清单。

| 资产 | 数据 |
| --- | --- |
| Windows x64 Vulkan | `speech-0.8.11-windows-x64-vulkan.zip`，28,886,830 字节压缩；全部解压 91,023,339 字节 |
| Windows DLL | 包内 `speech.dll` 44,722,688 字节；另有 exe、header、import lib。直接 FFI 未必需要分发 exe，但须确认干净机器上的依赖闭包 |
| Windows ZIP SHA-256 | `63f4c5b5c60aeab9d4c90193298fe4ee5d2a6c931cdbdcbc8cbb126fbffc56d0` |
| macOS arm64 Metal | `speech-0.8.11-macos-arm64-metal.zip`；`libspeech.3.dylib` 8,712,832 字节；需处理 dylib 符号链接、签名与加载路径 |
| macOS ZIP SHA-256 | `85471d762535cfdb52e962a10542bb6aa6a039f559bee62703c1eb2d274b8125` |
| 推荐首个模型 | `sakasegawa/Qwen3-ASR-0.6B-GGUF` 的 `Qwen3-ASR-0.6B-Q8_0.gguf`，841,502,336 字节 |
| 模型固定 revision | `ee78f8709547f39bea3b924a16c72403826cac92` |
| 模型 SHA-256 | `416e10c15b4a3d9002bd337d18fc450233fdf68502b6e10d1379d2789838afd0` |

来源：[运行库 Release](https://github.com/nyosegawa/speech.cpp/releases/tag/v0.8.11)、[SHA256SUMS](https://github.com/nyosegawa/speech.cpp/releases/download/v0.8.11/SHA256SUMS)、[固定模型下载](https://huggingface.co/sakasegawa/Qwen3-ASR-0.6B-GGUF/resolve/ee78f8709547f39bea3b924a16c72403826cac92/Qwen3-ASR-0.6B-Q8_0.gguf)。

Q6/Q5/Q4 文件分别约 681,496,704 / 593,751,680 / 511,168,128 字节。先用 Q8 建立中文准确率基线，再决定是否提供更小量化模型；不能仅按文件小选择默认。现有 Python 的 Hugging Face 模型目录不能直接当 GGUF 使用。

已查版本没有 macOS Intel、Windows ARM 预编译包。首个推理验收目标为 Windows x64；macOS arm64 可使用对应 Metal 包验证。其余架构应有清晰的能力检测和提示，普通 Agent 始终可用；如果扩展推理支持则补源代码构建和实际验证，不宣称现成支持。

### 6.2 下载与体积边界

- 运行库和模型分开管理，固定可信来源、版本、大小及 SHA-256。模型达到约 842 MB，不能复用现有 updater 的 512 MiB 上限。
- 下载写临时文件，流式哈希，校验完成后原子安装；取消、断网、磁盘不足、哈希错误不能覆盖已有可用资产。并发点击不能产生多次加载/安装竞态。
- ZIP 解压校验路径及文件类型，禁止越界写入。动态库只从受控目录加载；前端不传任意 URL 让后端下载并执行库。
- 本地模型做格式和加载检查；自选文件不强制匹配推荐模型哈希，但不能因为扩展名是 GGUF 就认为可用。
- 模型、持久队列、配置放用户数据目录，升级不清除；安装目录不承担可变数据。卸载的数据保留/删除选项需要明确。
- 分别测量 Agent 安装包、安装后文件、可选运行库、模型、WebView2 前置依赖、内存/显存。上述 DLL/ZIP 数字不等于最终 App 大小，也不是纯 Rust 编译产物大小承诺。

## 7. SDT 迁移与回滚

已部署的是 Python Worker，根目录 **`C:\work\orbit-asr-worker`**；`C:\Users\leo\work` 是到 `C:\work` 的目录联接。目标 `ssh sdt`，Windows 11 Pro、Ryzen 7 9700X、约 32 GB 内存、RTX 4060 Ti 8 GB。细节以 [SDT 部署记录](../agents/asr-worker/sdt-deployment.md) 为准。

身份 `sdt-asr`，专用 Worker 用户 `orbit-sdt-asr`，登录计划任务 `Orbit ASR Worker`，队列 `data\asr.sqlite3`。不要把密码写入文档、命令行或测试报告。Python keyring 使用服务 `orbit-asr-worker` 和绝对配置路径作为键；Tauri 使用 `dev.orbit.agent` 及随机 secret ID 的凭据 JSON。二者不会自动互通，需有受控的一次性导入或在 UI 重新输入，旧凭据保留到迁移验证完成。

2026-10-24 更新备份在 `C:\work\orbit-asr-worker\backups\20261024-013750-direct-results-20s`。当时记录 97 passed / 1 skipped，队列 2184 done、无积压，Core 显示 ready/cuda；这些仅是历史快照，不能作为新 Agent 或当前线上健康证明。

建议执行顺序：

1. 在单独候选目录构建 Windows 原生包，核对 Rust/MSVC、Windows SDK、CMake、WebView2 和 DLL 依赖。此前只找到 Rust/Node/pnpm 与 VS installer，CMake 未在 PATH，未验证 MSVC 组件；接手重新检查。
2. 用隔离的测试 Worker 身份与测试路由/环境验收，避免候选程序误消费生产共享队列。先完成真实 GGUF 加载、中文/英文/静音/噪声/20 秒样本及 CPU/Vulkan 测试。
3. 对比当前 Python 输出、时延、内存/显存，记录版本、模型 hash、输入样本 hash；验证 Mac 现有功能未回归。发布兼容 Vulkan/Metal 的 Core，确认 Android/Core/Worker 均支持 20 秒。
4. 切换前检查实时积压，停止并禁用旧登录任务，确认 Python Worker 进程已退出，再做 SQLite 一致性备份（含 WAL 一致性处理）、配置和计划任务备份。禁止两个实例同时持有生产 Worker 身份或写同一队列。
5. 导入凭据、身份和持久化队列。推荐使用可验证的一次性数据库迁移；覆盖 queued/processing/outbox/done 全状态，迁移 marker 及事务支持重复执行。不能仅拷贝主 DB 文件或仅迁移未完成记录而丢掉墓碑。
6. 启动 Tauri Agent，验证普通 Agent 功能、实际 Vulkan 后端、Core 心跳、真实任务结果、Core 应用 ACK、outbox 清零和重复 job 不重复推理。关闭窗口继续处理，再验证托盘退出及登录启动。
7. 保留旧部署、模型和 Python 环境作为回滚材料。更新部署文档，记录实际安装路径、版本、hash、任务/自启动方式、测试与未验收项；本交接不授权顺便删除旧环境。

回滚先停原生 Agent，备份其最新数据，再恢复旧任务/配置。保留迁移后新收的任务、结果和墓碑；设计向后兼容或导出/回迁工具，**不能直接用旧备份覆盖当前队列**。如原生 outbox 含 Vulkan/Metal 结果，必须保证兼容 Core 仍可接收或先完成应用确认。模型格式不同，Python 回滚仍使用原模型。

## 8. 实施顺序与验收门槛

| 阶段 | 交付内容 | 通过条件 |
| --- | --- | --- |
| M0 基线 | 核对代码、平台工具链、现有测试；固定 speech.cpp 与模型资产 | 未实现项与实测项明确，Windows 可构建普通 Agent |
| M1 Windows 能力 | 完整实现第 3 节，包括最近应用、托盘、自启动、凭据、快捷操作与更新 | 逐项真机证据；不能只测 mock 或 SSH 进程存在 |
| M2 原生推理 | C ABI/Opus/模型加载及设备映射、生命周期、运行库和模型管理 | SDT CPU 与 Vulkan 真实转写、取消/退出/错误恢复，中文路径与离线加载 |
| M3 可靠接入 | Rust 队列与 MQTT、独立凭据、Core/Console 兼容、UI | 断网/崩溃/重投/ACK 丢失/满队列下不丢任务，结果提交后立即投递 |
| M4 发布升级 | Windows CI、NSIS、Release/updater 资产和完整性验证 | 干净 Windows 安装、旧版本升级、失败恢复，Mac 发布构建不回归 |
| M5 切换交付 | SDT 迁移与回滚演练、更新文档 | 新 Agent 承担实际任务，关闭窗口仍运行，登录启动通过；列出所有实测限制 |

M1 和 M2 可按依赖穿插推进，最终验收不能只完成推理而遗漏 Windows 能力。

### 8.1 自动化与故障测试

- 复用现有 Rust、UI、MQTT 集成测试与 Python/Go 协议样例，新增测试围绕边界行为，不复制实现表达式。
- 协议：20 秒/1000 帧成功，超限、无效 base64、损坏 Opus、重复 JSON 键、布尔代整数、payload 越界、retained 输入、ID 不符、UTF-8、空转写。
- 队列：持久化失败不 ACK、队列满、相同 ID 同/异内容、重启恢复、最终失败、完成即发送、网络发送失败、broker PUBACK 不清 outbox、Core ACK 丢失后重试、墓碑保留、跨 broker/身份隔离。
- FFI：版本/符号不符、缺 DLL/依赖、模型损坏、不支持架构、GPU 失败、加载和推理取消、长度上限、反复切换/退出无句柄泄漏；mock 不能替代真实模型验证。
- 下载：取消/重试、磁盘不足、错误长度/hash、ZIP 越界、旧资产保留、本地中文/空格路径、无网启动。
- Windows：真实用户桌面中的托盘、隐藏窗口继续工作、单实例、登录启动、Credential Manager、Codex 链接、图标与 `.lnk`、最近应用、安装升级与失败恢复。仅通过 SSH 跑单元测试不算 GUI 验收。
- macOS：Codex/用量/快捷入口/最近应用/托盘/自启动/更新回归；推理关闭时 Apple Silicon 和 Intel 保持可构建、可运行的支持边界。

现有桌面检查入口（仓库根目录执行）：

```sh
cd agents/desktop
pnpm install --frozen-lockfile
pnpm check
pnpm test:ui
cargo fmt --manifest-path src-tauri/Cargo.toml --check
cargo test --locked --manifest-path src-tauri/Cargo.toml
cargo clippy --locked --manifest-path src-tauri/Cargo.toml --all-targets -- -D warnings
# 在对应平台执行：
pnpm tauri build --debug --bundles nsis  # Windows
pnpm tauri build --debug --bundles app   # macOS
```

Core 修改至少运行 `go test ./internal/core ./internal/mqttadmin`，并按现有 CI 检查 Console。Windows 原生构建/测试进入 CI，不能再引用不存在的 `build-desktop-agent.yml`。正式发布遵循仓库现有版本规则，确保 updater 下载的文件确实由 CI 生成，哈希/签名、架构与命名一致。

最终交付应附能力矩阵的逐项结果、测试命令和结果、实际安装与运行版本、各部分磁盘占用、加载/推理耗时、CPU/显存、中文质量对比、迁移及回滚证据。没有做过的真实硬件或登录重启验证明确写“未验收”，不得写成已支持。

## 9. 新会话可直接使用的任务说明

> 阅读 `HANDOFF.md` 和 `docs/desktop-agent-windows-inference-handoff.md`，按文档在现有 `agents/desktop` 上实施。使用 Rust/Tauri，把 macOS Agent 的全部现有能力在 Windows 实现并逐项真机验收，特别补齐最近应用、应用内更新与正式安装发布。增加可选 speech.cpp C ABI + Qwen3-ASR GGUF 本地推理，提供模型下载/本地路径，默认不启用推理，关闭窗口留托盘。保持 20 秒音频、结果持久化后立即投递、Core 应用 ACK 和崩溃恢复语义，完成 Core 后端设备兼容及 SDT 迁移/回滚。先核对实际工作区和部署状态，再分阶段实现和验证；不要把文档里的调研或历史部署快照当成原生实现已完成，也不要遗漏普通 Agent 的 Windows 能力。
