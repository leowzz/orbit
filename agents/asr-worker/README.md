# Orbit ASR worker

独立 Python worker，通过 MQTT 5 shared subscription 消费 Core 音频任务。无需桌面 Agent 同时运行；`agent_id` 在 Orbit 内必须唯一，当前仅发布专用 `audio/state` 注册。协议见 [audio-pipeline.md](../../docs/audio-pipeline.md)。

## Windows 安装和启动

准备带 `py` 启动器的 64 位 Python 3.11，或安装 `uv`，使用当前用户的 PowerShell 进入本目录。脚本优先使用 `py -3.11`；没有可用的 64 位 3.11 时，由 `uv` 复用或下载其管理的 Python 3.11，并创建带 pip 的独立虚拟环境。只有 Python 3.12/3.13、没有 `py` 时也可走 `uv` 路径。

安装脚本检测到已有 `.venv` 会停止，不覆盖环境或更新运行中进程的依赖；升级时先停止旧 worker，再明确维护原环境，或在另一个项目目录安装。

```powershell
./scripts/install.ps1 -Device cpu
# NVIDIA GPU：先安装兼容的 NVIDIA 驱动，再选匹配驱动的官方 PyTorch wheel index。
./scripts/install.ps1 -Device cuda -TorchIndex https://download.pytorch.org/whl/cu128
./scripts/start-gui.ps1
```

默认安装桌面界面和模型运行依赖，不加载或下载模型。界面中填写 MQTT 主机（仅域名或 IP）、端口、TLS、管理页创建的 `worker` 角色账号，以及唯一设备 ID 和模型。保存后点击「启动」。首次实际启动会按 Hugging Face 缓存规则下载模型，离线部署需预先缓存全部模型文件；模型也可填写本地目录。CPU 使用 float32，CUDA 使用 float16；设备可选自动、CPU 或 CUDA，显式 CUDA 不可用时启动失败。

界面提供运行状态、队列与最近日志，以及启动和停止。关闭窗口后可从托盘重新打开；从托盘「退出」结束程序。启动并直接收起到托盘：

```powershell
./scripts/start-gui.ps1 -Config config.json -StartWorker -Minimized
```

配置文件采用原子替换，保留高级字段；密码不写入 JSON。界面保存的密码由当前用户的系统凭据库保管，每个配置文件的绝对路径独立对应一条凭据，移动或复制配置后需要重新保存密码。GUI 启动时优先使用密码输入框中的值（包括从系统凭据加载的密码），仅输入框留空时使用命名环境变量（默认 `ORBIT_MQTT_PASSWORD`）；命令行模式仍以环境变量优先。默认不勾选「记住密码」；勾选后保存会写入系统凭据，取消勾选再保存会删除该配置对应的系统凭据，输入框里的密码仍可在当前会话使用。凭据库不可用时界面会提示失败，不会降级为明文保存。TLS 默认开启，界面可选择私有 CA 证书，对应 `mqtt.ca_file`；双向 TLS 可在 JSON 中另设 `cert_file` / `key_file`，证书路径建议为绝对路径，不关闭证书验证。

需要登录后自动启动并收起到托盘时，显式选择 GUI 模式：

```powershell
./scripts/install-task.ps1 -Gui
```

它为当前用户创建交互式登录任务，以 `pythonw.exe -m orbit_asr.gui --start --minimized` 运行，不打开控制台窗口；使用该用户保存的凭据。任务失败最多重启三次，已运行实例不重复启动。卸载任务：`Unregister-ScheduledTask -TaskName 'Orbit ASR Worker' -Confirm:$false`。每个数据库只允许一个 worker 进程，不能用相同 agent_id 启动两个实例；安装登录任务前先退出手动启动的实例。

### 保留命令行模式

只安装模型与命令行依赖可传 `./scripts/install.ps1 -Device cpu -Headless`；默认 GUI 安装也包含命令行。编辑生成的 `config.json` 后执行：

```powershell
$env:ORBIT_MQTT_PASSWORD = '<worker password>'
./scripts/start.ps1 -Config config.json
# 不带 -Gui 时仍安装原有命令行登录任务：
./scripts/install-task.ps1
```

命令行模式仍支持原来的密码环境变量。通过任务计划程序使用环境变量时，需为同一用户预先配置，再重新登录以继承更新的用户环境；安装脚本和任务本身不保存密码。SDT 的部署路径、环境和真机验收记录见 [sdt-deployment.md](sdt-deployment.md)。

## 持久化和重试

网络循环只执行大小限制、协议校验和短 SQLite 事务，模型加载、Opus 解码和推理在独立线程。QoS 1 PUBACK 在 SQLite commit 后发送。SQLite WAL + FULL 同步持久保存 inbox、尝试次数和 result outbox；重启将 processing 恢复到 queued。容量上限包含 queued、processing 和未被 Core 应用确认的结果；满时拒绝接受且不 PUBACK，并报告 error，Core 会继续定期投递。务必为 Core 和 broker 设置相同 128 KiB 消息容量。

识别完成（成功或最终失败）后，先将结果提交到 SQLite outbox，再立即以 QoS 1、非 retained 投递到专用结果 Topic `orbit/v1/agents/{agent_id}/audio/results`，不等待定时扫描。Core 收到后立即校验、落库并回 ACK。Core 的 `audio/acks` 才删除 outbox 内容；MQTT PUBACK 本身不代表 Core 已保存。断网、发送失败或未收到 Core ACK 的结果保留在 outbox，默认每五秒重发（`result_retry_seconds`），重连时重新订阅。相同 job_id 相同内容幂等；内容变化拒绝。完成后保留 ID+内容 hash tombstone，避免很晚的重发再次调用 ASR。tombstone 不自动过期，因此长期运行需监控数据库磁盘空间；不要删除仍可能被 Core 重发的记录。无效或 retained 输入作为毒消息 PUBACK 后丢弃；不能验证身份的消息不伪造 failed result。

每个合法作业最多三次推理，失败后发布结构化 failed result，错误只含有限的异常类型，不暴露音频或底层异常内容。未启用 forced aligner；成功结果仅一段覆盖整段音频，不能当作词级时间戳。20 秒更新 retained 状态，配置 offline last will；Core 以接收时间判断活性。

## 验证

```powershell
./.venv/Scripts/python.exe -m pip install ".[test]"
./.venv/Scripts/python.exe -m pytest tests -q
```

测试使用 PyAV 真正编码的裸 Opus packets，验证顺序解码、20 ms 帧、样本数；mock 模型验证实际 SQLite 队列、落盘后确认、容量拒绝、断电恢复、结果重发缓存、Core ack、tombstone 和失败结果。模型依赖 import smoke 在安装脚本完成，无需权重。配置测试另覆盖默认合并、字段校验、原子保存失败时保留原文件，以及不同配置路径的凭据隔离；凭据库全程使用 mock，不读取真实密码。测试覆盖 macOS 和 Windows；Windows 另验证 GUI 入口、保持控制管道打开时导入 NumPy，以及超时停止后实际子孙进程全部退出。停止进程时处理线程为 daemon；已提交至 SQLite 的 processing 作业由下次启动恢复，关闭不会等候耗时推理完成。自动化测试和真机链路验收分别记录，参见部署记录。

## 官方依据（2026-10-23 核对）

- [Qwen 官方 transformers 示例](https://github.com/QwenLM/Qwen3-ASR/blob/main/examples/example_qwen3_asr_transformers.py)：`Qwen3ASRModel.from_pretrained(dtype=..., device_map=...)`，`transcribe(audio=(numpy_array, sample_rate), language=None, return_time_stamps=False)`，结果 `.text` / `.language`。使用官方 `qwen-asr==0.0.6`，不安装 vLLM 或 flash-attn。
- [Qwen 官方依赖](https://github.com/QwenLM/Qwen3-ASR/blob/main/pyproject.toml)：其 transformers 和 accelerate 版本由官方包约束。
- [PyAV 官方安装](https://pyav.org/docs/stable/overview/installation.html) 与 [FFmpeg wheel 构建](https://github.com/PyAV-Org/pyav-ffmpeg)：Windows wheels 自带 FFmpeg/libopus，不要求 Ogg 容器或外部 ffmpeg 可执行程序。本项目约束 PyAV <19 保持 Python 3.11 路径；19 起要求 Python 3.12。
- [Qt for Python 官方安装](https://doc.qt.io/qtforpython-6/gettingstarted.html)：GUI 仅安装 `PySide6-Essentials` 6.x，包含本界面使用的 Core、Gui、Widgets 模块，仍从 `PySide6` 命名空间导入，不安装 Addons。
- [keyring 官方 API 与平台后端](https://keyring.readthedocs.io/en/stable/)：通过 `get_password` / `set_password` / `delete_password` 使用当前用户的系统凭据库。
- [PyTorch 官方安装选择器](https://pytorch.org/get-started/locally/)：Windows CPU/CUDA wheel 从官方 index 安装，CUDA index 需要适配实际驱动。CPU float32 是本项目选择，官方示例主要使用 GPU；SDT 上的实际 CPU 速度及模型兼容性已单独验收，其他机器仍需测量。


## 可选本机 MQTT 集成测试

默认测试不连接 broker。`tests/test_mqtt_integration.py` 仅在环境开关启用时运行；需先由操作者启动独立测试用 MQTT5 broker（loopback、无认证、allow-all）及连接该 broker 的 Orbit Core，Core 使用独立临时 SQLite 与管理端口。测试不会启动或停止这些服务；会在测试 Core 中留下带随机 `test-node-*` / `test-asr-*` 的合成音频记录，禁止指向生产实例。模型被注入的 Mock Qwen 替代，无需安装 PyTorch 或下载权重。

```powershell
$env:ORBIT_ASR_MQTT_INTEGRATION = "1"
$env:ORBIT_ASR_TEST_MQTT_PORT = "18894"
$env:ORBIT_ASR_TEST_CORE_URL = "http://127.0.0.1:17620"
$env:ORBIT_ASR_TEST_CONSOLE_PASSWORD = "<isolated Core password>"
# 可选：直接以只读连接交叉确认 Core SQLite 终态记录。
$env:ORBIT_ASR_TEST_CORE_DB = "C:/temp/orbit-test/core.sqlite"
./.venv/Scripts/python.exe -m pytest tests/test_mqtt_integration.py -q
```

可选 `ORBIT_ASR_TEST_MQTT_HOST` 默认为 127.0.0.1；测试强制 broker 与 Core URL 使用 loopback。覆盖真实 MQTT5 Node Segment → Core 落盘/Node ACK → shared worker 解码/Mock 转写 → Core result ACK → worker SQLite done；同时验证已认证 list/detail API、结果文本与片段时间范围、音频包不出现在详情中，以及重复 Segment 和直接重发 job 均不二次识别。本机 macOS 的独立 Mochi MQTT5 broker + 实际 Orbit Core 测试已通过；它仍不代表 Windows 模型推理验收。


## 导入 DayTrace 历史音频

脚本 `scripts/import-daytrace.py` 可直接运行，也可使用 `python -m orbit_asr.importer`。只需 worker 基础依赖，无需在导入机器安装模型或 GUI。使用管理页创建的「录音设备」账号；其 Node ID 必须与准备计划时一致。

先完整校验并转换归档，此步骤不连接网络：

```sh
python -m orbit_asr.importer prepare /path/DayTrace-analysis.zip \
  --node-id daytrace-import --output data/imports/daytrace
```

归档中的 SHA-256、时间、样本数及 Ogg Opus 解码均验证通过后才生成计划。Opus 容器的 preskip 和末尾裁切由解码器处理，再编码为协议要求的 20 ms 裸 Opus；长音频均匀拆分为不超过 10 秒的消息，不连接不同来源的录音。`segments.jsonl` 保留源片段 ID、SHA-256、分片偏移和编码延迟的映射；Core 的片段时间来自原始 UTC。设备时钟未校准，结果仍是整片转写，不提供词级对齐。

MQTT 配置示例（密码推荐通过 `ORBIT_MQTT_PASSWORD` 环境变量提供）：

```json
{"host":"mqtt.example.com","port":8883,"tls":true,"username":"import-node","node_id":"daytrace-import","password_env":"ORBIT_MQTT_PASSWORD"}
```

也支持配置文件中的 `password`，应限制文件权限并在导入后移除。远程 broker 必须使用 TLS，支持可选 `ca_file` / `cert_file` / `key_file`。脚本不修改 MQTT 账户或权限。

先发送少量新片段，在 Core 确认转写完成后，再省略 `--limit` 导入剩余部分：

```sh
python -m orbit_asr.importer publish --plan data/imports/daytrace \
  --mqtt-config /private/import-mqtt.json --limit 3
python -m orbit_asr.importer publish --plan data/imports/daytrace \
  --mqtt-config /private/import-mqtt.json --rate 5
python -m orbit_asr.importer status --plan data/imports/daytrace
```

`stored` 表示收到 Core 持久落库的应用确认，不等于 ASR 已完成；转写进度在 Core 的音频记录页按 Node ID 筛选。默认每秒最多投递 5 条。`Ctrl+C` 后执行相同命令可继续：已经确认的片段跳过，未确认片段使用相同 ID 和相同内容重发。首次投递前绑定 broker；修改目的 broker、改动已准备的内容或复用不匹配的计划目录会被拒绝。

计划目录包含私密音频和来源时间，保存在被 Git 忽略的 `data/` 下。准备文件和回执使用受限权限；请保留计划以便恢复或追溯，完成后按需要删除。原始压缩包不会被修改。
