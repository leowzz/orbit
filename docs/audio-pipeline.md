# 音频采集与 ASR

Android 持续采集 16 kHz 单声道 PCM，通过本地 WebRTC VAD 切出语音并编码为 Opus。采集、编码及 MQTT 上传独立运行；默认连续检测到 300 ms 人声才保留片段，连续静音 600 ms 后提交片段，连续讲话最长每 20 秒提交一次，提交时不停止 AudioRecord。静音和未达到连续人声阈值的短促声音不单独上传。Android 使用本地持久队列，MQTT QoS 0 发送成功后立即删除本机片段，不订阅或等待 broker/Core 确认；发送调用报错则保留重试。发送成功仅代表本地 MQTT 客户端完成发送，不保证 Core 收到，允许丢失。worker 的任务与结果仍使用原有持久队列及应用层确认。

Android 的「设置 → 录音 → 录音设置」可调整连续人声阈值（100–2000 ms）、结束静音（200–2000 ms）、人声前保留时长（0–1000 ms，默认 300 ms）、单段最长时长（3–20 秒）和 WebRTC 检测严格度（四档，默认严格）。最大上传并发可设为 1–64（默认 16），保存后立即生效；降低并发不会取消已经发出的片段。上传使用独立的 dataSync 前台服务，发送完成立即补位，新录音优先于旧积压，不再每 5 秒只发送 4 段。停止录音只关闭麦克风，尾段与积压仍继续发送；打开应用时也会恢复发送，即使录音开关已关闭。队列清空后空闲 15 秒关闭上传服务。Android 15 的后台 dataSync 时限或系统强停可能暂停发送，未发送数据保留，回到应用可继续。

「使用低延迟预设」将结束静音设为 300 ms、最长片段设为 3 秒、并发设为 16；点击保存后生效。Core 收到并入库后即可在管理页刷新看到记录，转写文本仍需等待 worker 识别。设置保存到本机；录音参数在片段边界生效，不重启麦克风。门槛满足后保留候选人声和前置声音；持续说话因长度切片时沿用已确认的人声状态，最后不足阈值的尾音也会保留。严格度越高越容易过滤噪声，也可能漏掉轻声说话。

录音卡片分别展示麦克风与发送状态，每 2 秒刷新「已发送包数」「待发送」「发送中」、发送数据量与最近发送时间。卡片中的 1 包指一条完整语音片段消息，不是片段内部的 20 ms Opus 帧；安静时不增加包数。已发送包数和数据量在本次应用进程内累计（含不同连接，上传服务重启不清零），进程重启后归零，数据量按 JSON 消息体字节数计算。仅本地发送成功且完成出队的片段计入累计，不代表 Core 已收到；失败、重复回调及关闭队列后的迟到回调不计入。待发送不含正在发送的片段，二者对应当前连接；其他连接的积压会另外提示。

录音设置顶部提供「清空缓存队列」。确认后断开当前上传连接，忽略迟到的发送回调，并删除本机所有连接下的待发送片段和临时文件，显示实际删除段数。删除与录音落盘共用锁，保留队列目录以便继续录音；清理后产生的新片段仍正常入队。已发送内容不会撤回，连接和录音参数不受影响。删除失败会提示重试，可能已经清除部分片段。

Dayflow 的参考点是持续采集、异步批次处理和 SQLite 历史记录；其 macOS 屏幕采集代码不直接用于 Android。本阶段结构化内容是转写文本、语言、录制时间与处理信息，未增加摘要、待办或说话人识别。

## 启用

1. 部署更新后的 Core。启动时自动在现有 `console.database` 创建 `audio_sessions`、`audio_segments`、`audio_jobs`、`audio_results`、`audio_workers`，无物理外键；无 MQTT 的控制台仍可查看历史数据。
2. 更新 Android 统一 App，打开后允许麦克风权限。录音状态和停止入口位于设置页与常驻通知。统一 App 使用 Core 下发的 MQTT 连接；Core 会将自己管理的、与旧版标准 Node ACL 完全匹配的 Android 账号升级为本设备音频权限。自定义权限或其他设备的权限不会被覆盖。
3. 手动配置旧 Android node 时，在管理页创建「录音设备」MQTT 账号，使用相同 Node ID；已有手动账号需在 EMQX 增加下表对应的两条音频权限，或换用新账号。
4. 创建「语音识别（Worker Agent）」MQTT 账号，按 [worker 部署说明](../agents/asr-worker/README.md) 在 Windows 安装并打开桌面界面，配置 MQTT、模型与 CPU/CUDA 后启动，可收起到托盘运行。worker 使用独立 Agent ID，状态显示在「音频记录」。
5. 打开 Core 管理页「音频记录」，按设备、状态或转写文字查询。未上线的 worker 不影响录音和 Core 入队。

此实现不会开机静默启用麦克风；由用户进入 App 并授权后启动。Android 系统强行停止应用、收回权限或厂商后台限制仍可能终止采集，需真机验证。

## 云端保留数量

Core YAML 配置（修改后重启生效）：

```yaml
audio:
  max_segments: 10000
```

省略时默认 10000，必须为正整数；按所有设备合计，不是每台设备各 10000。按每段 20 秒估算可保留约 55.6 小时；VAD 产生更多短片段或多设备同时录音时，实际覆盖时间会缩短。这是条数限制，不是固定一天或磁盘字节配额。

Core 启动时及每次新增片段时，在事务中按录制时间淘汰最旧记录（同一时间按片段 ID 排序），同步删除原始 Opus 数据、任务、转写结果和不再包含片段的会话。尚未完成转写的旧任务也会被淘汰，worker 离线时同样不会无限积压。降低上限后，重启会立即清理已有超量记录；无 MQTT 模式也会执行。

数据库仅额外保留一条持久化清理边界；已淘汰时间范围内的片段重传或历史导入会被确认并丢弃，不会重新入队。即使重启或调大上限，已清理的数据也不会恢复。启用过清理后，合法但已找不到原片段的迟到结果会被确认并丢弃，允许 worker 释放结果队列；此时 `stored` ACK 表示已处理该消息，不保证内容仍在保留窗口内。

SQLite 会复用删除释放的页，但数据库文件不保证立即缩小；已有大文件若需归还磁盘空间，应另行在维护窗口压缩数据库。此限制不限制其他业务数据，也不改变推理端的队列配置。

## MQTT 权限

| 账号 | 发布 | 订阅 |
| --- | --- | --- |
| Android 录音设备 | 自己的 `nodes/{id}/audio/segments` | 无（旧版客户端仍订阅自己的 `nodes/{id}/audio/acks`） |
| Worker Agent | 自己的 `agents/{id}/audio/results`、`agents/{id}/audio/state` | `audio/jobs`、自己的 `agents/{id}/audio/acks` |
| Core | `audio/jobs`、各设备/worker 的 `audio/acks` | `nodes/+/audio/segments`、`agents/+/audio/results`、`agents/+/audio/state` |

表内 Topic 均加 `orbit/v1/` 前缀。保留原状态显示所需权限；普通 Node 和电脑 Agent 账号不会自动获得全部音频访问权。Core 现有部署若使用精确 ACL，必须补齐这些 Topic。EMQX 对 shared subscription 按去掉 `$share/{group}/` 的实际 Topic 授权，参见 [官方说明](https://docs.emqx.com/en/emqx/latest/develop/message-queue/message-queue-concept.html)。

## 消息协议

音频通道使用 UTF-8 JSON、`schema_version: 1`。新版 Android → Core 的 Segment 使用 MQTT QoS 0；Core/worker 通道及旧版 Android 使用 MQTT QoS 1。除 worker 状态外均禁止 retained；不改变现有 Protobuf 状态和命令协议。每条消息最大 128 KiB。身份只能包含小写字母、数字、`_`、`-`，首位为字母或数字，最长 64 位。时间为 UTC RFC3339，ID 为 UUID。

| Topic | 发送方 → 接收方 | 内容 |
| --- | --- | --- |
| `orbit/v1/nodes/{node_id}/audio/segments` | Android → Core | Segment |
| `orbit/v1/nodes/{node_id}/audio/acks` | Core → 旧版 Android | `{schema_version, segment_id, status:"stored"}`；新版不订阅、不等待 |
| `orbit/v1/audio/jobs` | Core → worker | `{schema_version, job_id, segment}` |
| `orbit/v1/agents/{agent_id}/audio/results` | worker → Core | Result |
| `orbit/v1/agents/{agent_id}/audio/acks` | Core → worker | `{schema_version, job_id, status:"stored"}` |
| `orbit/v1/agents/{agent_id}/audio/state` | worker → Core | WorkerState（retained，约每 20 秒更新） |

worker 通过 `$share/audio-asr/orbit/v1/audio/jobs` 消费。Core 先在同一事务保存片段与任务，保留向旧版 Android 发送确认的兼容行为；新版 Android 不依赖此确认。任务在收到结果前周期性重发。`job_id` 等于 `segment_id`。重复消息必须幂等，复用 ID 却修改内容必须拒绝。Core 到 worker 的队列提供至少一次投递，终态结果以 Core 首次接受的内容为准；新版 Android 上传为尽力发送，不能据本机队列清空推断 Core 已保存。

worker 完成识别后先持久化结果，随即投递到 `orbit/v1/agents/{agent_id}/audio/results`；Core 订阅回调收到结果就校验落库并发送 ACK。首次投递不等待轮询；worker 的定时 outbox 扫描仅负责重发未收到 Core ACK 的结果，默认间隔 5 秒。

Segment 字段：

```json
{"schema_version":1,"segment_id":"UUID","session_id":"UUID","node_id":"android-1","sequence":0,"started_at":"2026-10-23T12:00:00Z","duration_ms":1000,"speech_ms":800,"sample_rate":16000,"channels":1,"codec":"opus","frame_ms":20,"packets":["base64 opus packet"]}
```

每个 packet 独立为 20 ms Opus 帧（320 个 PCM 样本），同一片段内编码器状态连续，每个片段重新初始化编码器；不含 Ogg 容器。`duration_ms = packets.length * 20`，范围 20–20000；`speech_ms` 在 0–duration_ms 之间。时间包含 VAD 的预留和尾部静音。包数量最多 1000，单包最多 1275 字节。

Result 字段：`schema_version, job_id, segment_id, node_id, agent_id, status, text, language, segments, model, device, processing_ms, completed_at, error_code, error_message`。`status` 为 `completed` 或 `failed`；`device` 为 `cpu` 或 `cuda`；`segments` 为 `{start_ms,end_ms,text}` 数组，时间相对音频片段且不得超出其时长。未启用 forced aligner 时，一段转写对应整段音频，不宣称词级时间戳。失败时不伪造转写，填写有限长的错误信息。成功时错误字段为空。

结果文本和每段文本最多 64000 UTF-8 字节，语言 64 字节、模型 256 字节、错误码 128 字节、错误信息 2048 字节；最多 1000 个时间段；`processing_ms` 为 0–86400000 的整数。worker 的 `host_label` 最多 256 UTF-8 字节。Core 也检查包装后的 Job 总长度，防止合法 Segment 包装后超出 128 KiB。

WorkerState 字段：`schema_version, agent_id, host_label, model, device, status, updated_at`。`status` 为 `starting`, `ready`, `processing`, `error`, `offline` 之一；Core 按接收时间判定过期，不将 retained 状态当成实时在线证据。

## 数据查看

Core 管理页的「音频记录」默认忽略已完成但转写文字为空或仅含空白的记录，等待识别与失败记录仍显示；可切换为「查看所有」。筛选在服务端分页前执行，计数与翻页保持一致。页面提供设备、状态和文字筛选、分页、转写及结构化结果详情，同时展示 worker 最近状态。音频数据只通过已认证的管理 API 读取；不暴露 MQTT 密码或本地路径。

## Prometheus / Grafana

Core 可通过独立私网监听暴露 ASR 消费指标：

```yaml
metrics:
  listen: 0.0.0.0:7623
```

默认关闭。Prometheus 与 Core 加入同一 Docker 网络，抓取 `http://orbit-core:7623/metrics`；不要映射到公网。控制台及 App API 端口不提供指标。

WorkerState 新增可选 `queue: {queued, processing, outbox, capacity}`；Rust 桌面 Worker 和 Python Worker 都在约 20 秒心跳中上报，需先更新 Core 再更新 Worker。老 Worker 保持兼容，但缺少队列值不会当作 0。`outbox` 对应桌面的“待确认结果”，须收到 Core 应用层 ACK 才减少。Core 的 pending 包含已经分发但尚未收到结果的任务，与 Worker 队列不能相加。

Worker up 与管理台一致：非 retained、非 offline、Core 接收心跳不足 60 秒才为 1。队列指标只在实时心跳有效且包含 queue 时导出；`queue_reported=0` 表示未知/过期。累计接收与消费 counter 在业务事务中去重更新，持久保存在 SQLite 的 `audio_counters`，从指标首次部署起计数，重启和音频清理不清零；保留数据量是 gauge，不能用其 rate 推算吞吐。

部署配置、指标说明与回滚方式见 [ASR observability](../deploy/observability/README.md)。
