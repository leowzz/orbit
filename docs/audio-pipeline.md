# 音频采集与 ASR

Android 持续采集 16 kHz 单声道 PCM，通过本地 WebRTC VAD 切出语音并编码为 Opus。采集、编码及 MQTT 上传独立运行；语音结束约 600 ms 后提交片段，连续讲话最长每 10 秒提交一次，提交时不停止 AudioRecord。静音不上传。设备和 worker 使用本地持久队列，收到 Core 的应用层确认后才释放待发数据。

Dayflow 的参考点是持续采集、异步批次处理和 SQLite 历史记录；其 macOS 屏幕采集代码不直接用于 Android。本阶段结构化内容是转写文本、语言、录制时间与处理信息，未增加摘要、待办或说话人识别。

## 启用

1. 部署更新后的 Core。启动时自动在现有 `console.database` 创建 `audio_sessions`、`audio_segments`、`audio_jobs`、`audio_results`、`audio_workers`，无物理外键；无 MQTT 的控制台仍可查看历史数据。
2. 更新 Android 统一 App，打开后允许麦克风权限。录音状态和停止入口位于设置页与常驻通知。统一 App 使用 Core 下发的 MQTT 连接；Core 会将自己管理的、与旧版标准 Node ACL 完全匹配的 Android 账号升级为本设备音频权限。自定义权限或其他设备的权限不会被覆盖。
3. 手动配置旧 Android node 时，在管理页创建「录音设备」MQTT 账号，使用相同 Node ID；已有手动账号需在 EMQX 增加下表对应的两条音频权限，或换用新账号。
4. 创建「语音识别（Worker Agent）」MQTT 账号，按 [worker 部署说明](../agents/asr-worker/README.md) 在 Windows 安装并打开桌面界面，配置 MQTT、模型与 CPU/CUDA 后启动，可收起到托盘运行。worker 使用独立 Agent ID，状态显示在「音频记录」。
5. 打开 Core 管理页「音频记录」，按设备、状态或转写文字查询。未上线的 worker 不影响录音和 Core 入队。

此实现不会开机静默启用麦克风；由用户进入 App 并授权后启动。Android 系统强行停止应用、收回权限或厂商后台限制仍可能终止采集，需真机验证。Core 保存的音频和转写暂不自动清理，请为持续采集预留磁盘空间。

## MQTT 权限

| 账号 | 发布 | 订阅 |
| --- | --- | --- |
| Android 录音设备 | 自己的 `nodes/{id}/audio/segments` | 自己的 `nodes/{id}/audio/acks` |
| Worker Agent | 自己的 `agents/{id}/audio/results`、`agents/{id}/audio/state` | `audio/jobs`、自己的 `agents/{id}/audio/acks` |
| Core | `audio/jobs`、各设备/worker 的 `audio/acks` | `nodes/+/audio/segments`、`agents/+/audio/results`、`agents/+/audio/state` |

表内 Topic 均加 `orbit/v1/` 前缀。保留原状态显示所需权限；普通 Node 和电脑 Agent 账号不会自动获得全部音频访问权。Core 现有部署若使用精确 ACL，必须补齐这些 Topic。EMQX 对 shared subscription 按去掉 `$share/{group}/` 的实际 Topic 授权，参见 [官方说明](https://docs.emqx.com/en/emqx/latest/develop/message-queue/message-queue-concept.html)。

## 消息协议

音频通道使用 UTF-8 JSON、`schema_version: 1`、MQTT QoS 1。除 worker 状态外均禁止 retained；不改变现有 Protobuf 状态和命令协议。每条消息最大 128 KiB。身份只能包含小写字母、数字、`_`、`-`，首位为字母或数字，最长 64 位。时间为 UTC RFC3339，ID 为 UUID。

| Topic | 发送方 → 接收方 | 内容 |
| --- | --- | --- |
| `orbit/v1/nodes/{node_id}/audio/segments` | Android → Core | Segment |
| `orbit/v1/nodes/{node_id}/audio/acks` | Core → Android | `{schema_version, segment_id, status:"stored"}` |
| `orbit/v1/audio/jobs` | Core → worker | `{schema_version, job_id, segment}` |
| `orbit/v1/agents/{agent_id}/audio/results` | worker → Core | Result |
| `orbit/v1/agents/{agent_id}/audio/acks` | Core → worker | `{schema_version, job_id, status:"stored"}` |
| `orbit/v1/agents/{agent_id}/audio/state` | worker → Core | WorkerState（retained，约每 20 秒更新） |

worker 通过 `$share/audio-asr/orbit/v1/audio/jobs` 消费。Core 先在同一事务保存片段与任务，再确认 Android；任务在收到结果前周期性重发。`job_id` 等于 `segment_id`。重复消息必须幂等，复用 ID 却修改内容必须拒绝。队列提供至少一次投递，终态结果以 Core 首次接受的内容为准。

Segment 字段：

```json
{"schema_version":1,"segment_id":"UUID","session_id":"UUID","node_id":"android-1","sequence":0,"started_at":"2026-10-23T12:00:00Z","duration_ms":1000,"speech_ms":800,"sample_rate":16000,"channels":1,"codec":"opus","frame_ms":20,"packets":["base64 opus packet"]}
```

每个 packet 独立为 20 ms Opus 帧（320 个 PCM 样本），同一片段内编码器状态连续，每个片段重新初始化编码器；不含 Ogg 容器。`duration_ms = packets.length * 20`，范围 20–10000；`speech_ms` 在 0–duration_ms 之间。时间包含 VAD 的预留和尾部静音。包数量最多 500，单包最多 1275 字节。

Result 字段：`schema_version, job_id, segment_id, node_id, agent_id, status, text, language, segments, model, device, processing_ms, completed_at, error_code, error_message`。`status` 为 `completed` 或 `failed`；`device` 为 `cpu` 或 `cuda`；`segments` 为 `{start_ms,end_ms,text}` 数组，时间相对音频片段且不得超出其时长。未启用 forced aligner 时，一段转写对应整段音频，不宣称词级时间戳。失败时不伪造转写，填写有限长的错误信息。成功时错误字段为空。

结果文本和每段文本最多 64000 UTF-8 字节，语言 64 字节、模型 256 字节、错误码 128 字节、错误信息 2048 字节；最多 1000 个时间段；`processing_ms` 为 0–86400000 的整数。worker 的 `host_label` 最多 256 UTF-8 字节。Core 也检查包装后的 Job 总长度，防止合法 Segment 包装后超出 128 KiB。

WorkerState 字段：`schema_version, agent_id, host_label, model, device, status, updated_at`。`status` 为 `starting`, `ready`, `processing`, `error`, `offline` 之一；Core 按接收时间判定过期，不将 retained 状态当成实时在线证据。

## 数据查看

Core 管理页的「音频记录」提供设备、状态和文字筛选、分页、转写及结构化结果详情，同时展示 worker 最近状态。音频数据只通过已认证的管理 API 读取；不暴露 MQTT 密码或本地路径。
