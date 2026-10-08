# Orbit App Node 与个人收件箱：实现交接

- 日期：2026-10-07（Asia/Shanghai）
- 代码核对基线：`87ed76ffcbdd8b505d8170259fdaf6b0836220d6`（`v0.1.7`）
- 交接分支：`feat/app`
- 状态：Android 收件箱、持久同步、文本/待办与图片链路已实现；桌面发布验证和后续能力见文末执行进展。原方案保留为背景，当前接口以 [App API v1](app-api.md) 为准。

> 下文方案中的“当前/尚未实现”描述的是 2026-10-07 交接基线；实现后的事实和验证范围见本文末尾“执行进展”，API 和运行方法见新文档。

## 1. 接手目标与决策状态

用户希望先增加 Android App Node：查看消息列表，将周期状态和 session 信息固定在紧凑摘要区展示；随后支持从任意 App Node 发送文本、待办、图片，并在其他 App 中查看、复制、完成或修改。后续 macOS、Windows App 具有相同产品定位，目标用户为个人自用。

用户明确选择：**首版打开 App 后同步，后台通知后续增加。**

本次交接按“不引入 IM、自行实现必要基础能力”的讨论路线展开。下列技术选型是建议实施默认值，不应表述成已经落地或经过三端验证的结果：

| 范围 | 建议默认值 | 决策理由 |
| --- | --- | --- |
| App | Flutter + Dart，Riverpod，Drift/SQLite | 共享三端 UI、缓存与同步逻辑；平台插件仍需逐端验证 |
| Core | 沿用 Go，增加业务存储与 App API | 保留已有采集、投影、硬件与受控命令边界 |
| 服务端存储 | 单实例 SQLite WAL，独立业务数据库 | 适合个人数据规模；与 Agent 读取的 Codex 数据库分开 |
| App 通信 | HTTPS API + 前台 SSE | 支持查询、提交操作、增量同步和附件，符合前台同步目标 |
| Agent / 硬件通信 | 保留 MQTT + Protobuf | 延续现有链路和固件兼容性 |
| 附件 | 服务端文件目录 + 数据库元数据 | 初期无需增加对象存储服务；通过接口保留替换空间 |
| 运行形态 | App API、消息业务与当前 Core 同进程，内部按模块分层 | 控制个人自用部署成本 |

首版不要求后台推送、Android 常驻服务、多用户/租户、联系人/群聊、端到端加密、多 Core 写入、富文本协同或 CRDT。指定设备发送、每设备已读、全文搜索、托盘、全局快捷键可后续增加。

## 2. 当前实现与阅读入口

先阅读 [README](../README.md)、[领域术语](../CONTEXT.md)、[当前设计](design.md)、[MQTT Topic](mqtt-topics.md) 与相关 ADR。根目录 [HANDOFF.md](../HANDOFF.md) 是简短的当前状态与导航入口，已移除过时的架构草稿。

| 现有实现 | 核对入口 | 与本次需求的关系 |
| --- | --- | --- |
| Core 维护内存 canonical state，生成有限的 DeviceView | [engine.go](../internal/core/engine.go) | 用量和 session 可复用；没有用户消息历史 |
| 只接受 OLED/Web 产品与对应 profile；同一路由每种 observation type 最多一个 Agent | [engine.go](../internal/core/engine.go)、[ADR-0018](adr/0018-core-owns-node-data-routing.md) | App 产品和投影需扩展；多主机 session 聚合另行设计 |
| DeviceView 包含 UsageView、CodexView、freshness/expiry | [view.proto](../proto/orbit/v1/view.proto) | 适合固定状态摘要，不适合无限增长的消息列表 |
| Web 缓存最新快照，提供 `/api/state`、SSE `/api/events` | [store.go](../nodes/web/store.go)、[server.go](../nodes/web/server.go) | 可参考实现，当前 SSE 没有持久增量回放 |
| Web 服务使用配置中的一个 node_id；登录令牌未绑定独立客户端设备身份 | [runner.go](../nodes/web/runner.go)、[auth.go](../nodes/web/auth.go) | 不能直接把所有 App 当成同一个 Web Node |
| MQTT clean start，session expiry 为 0 | [client.go](../internal/mqtt/client.go)、[ADR-0008](adr/0008-use-ephemeral-idempotent-v1-commands.md) | 不提供业务消息离线补收或跨重启去重 |
| Intent/Command 仅实现 OpenCodexSession；Web Intent TTL 20 秒，Core 限制最多 30 秒 | [command.proto](../proto/orbit/v1/command.proto)、[commands.go](../internal/core/commands.go) | 不能套用到需要持久保存的消息创建、编辑与完成 |
| Agent 发布 CommandResult；Core 当前未订阅 results 并反馈给 Web | [runner.go](../internal/core/runner.go)、[Agent commands](../internal/agent/commands.go) | UI 的 HTTP 202 不表示主机动作成功 |
| Core MQTT 入站和 DeviceView 上限均为 32 KiB | [runner.go](../internal/core/runner.go) | 图片走 HTTP，消息引用附件 |
| 已有 modernc.org/sqlite，当前用于 Codex Source 只读采集 | [go.mod](../go.mod)、[source.go](../internal/sources/codex/source.go) | 仍需新增业务库、迁移和存储接口 |
| Core 目前先连接 MQTT，再启动 runner | [Core main](../cmd/orbit-core/main.go) | 新 App API 的启动与可用性不能被 Broker 离线无限阻塞 |

本次没有执行 Go/固件运行验证；以上为源码核对。既有测试结果、生产环境和另一台机器的工具链需要在实现时重新验证。

## 3. 目标结构与职责

```mermaid
flowchart LR
    Agent["Agent：采集 / 主机能力"] <-->|MQTT| Broker["现有 Broker"]
    Broker <-->|状态 / 命令| Core["Core：投影 / 收件箱 / App API"]
    Broker --> OLED["OLED 等硬件 Node"]
    App["Flutter App Nodes"] <-->|HTTPS / SSE| Core
    App --- Local["本地 SQLite：缓存 / 游标 / 待发送操作"]
    Core --- DB["业务 SQLite：条目 / 版本 / 变更 / 操作结果"]
    Core --- Files["附件与缩略图"]
```

- **Agent**：继续获取可信主机状态、执行显式 Capability。发送用户文本和待办无需经过 Agent。
- **Core**：用户内容与共享状态的权威来源；持久化后确认操作，控制投影、访问范围和同步。
- **App Node**：展示、本地缓存、用户输入、重试、本机交互。Node 是运行角色，App 可通过 HTTPS 接入。
- **MQTT**：延续 Agent 和硬件链路；业务消息可靠性由持久化和增量同步保证。

Core 内的业务存储与 HTTP 服务应独立于 MQTT 重连生命周期。Broker 暂时不可用时，消息仍可查询和提交；Agent 状态标记 stale，远程动作反馈不可用。

同一台电脑上的 App 和 Agent 分别使用 `node_id`、`agent_id`，不共用 MQTT Client ID 或凭据。

## 4. 产品语义与界面

### 两类数据

1. **状态摘要**：用量、session、更新时间、新鲜度。按稳定标识覆盖更新；过期明确显示 stale；不随每次采集增加消息行。
2. **收件箱条目 Item**：文本、待办、图片。拥有稳定 ID、持久版本和生命周期；与 MQTT Metadata.message_id、DeviceView revision、core_epoch 分开。

界面上方固定紧凑状态区，session 可展开；下方为独立滚动的消息列表。文本支持复制，待办支持完成/撤销，图片支持缩略图和详情。原始周期刷新不产生通知；未来若将 session 完成转换成消息，应另做状态变化检测和去重。

### 操作含义

| 操作 | 建议默认语义 | 执行位置 |
| --- | --- | --- |
| 发送文本/待办/图片 | 自己的所有 App Node 可见，来源端也能看到 | Core 持久化与同步 |
| 完成/撤销待办 | 全局共享状态；提交目标值，不能用 toggle 表达重试操作 | Core 事务 |
| 编辑内容 | 共享修改，携带 expected_revision，冲突不静默覆盖 | Core 事务 |
| 复制文本/查看图片 | 当前设备本地动作，不会全局“消费掉”消息 | App |
| 删除/归档 | 建议全局同步；删除留同步标记，归档仍可恢复 | Core 事务 |
| 已读/本地隐藏 | 可选，按设备保存，不等于待办完成 | 本地或独立设备状态 |
| 打开主机 session | 保留时效和授权，等待执行结果；超时表示结果未知 | Core → Agent |

“所有 App 收到”表示：Core 已保存后，各获授权设备下次同步可以获取；首版不承诺锁屏或进程被杀时立即通知。硬件只接收其支持的投影，不强制接收图片和完整历史。

## 5. Core 新增模块

| 模块 | 责任与首版边界 |
| --- | --- |
| 设备管理 | 稳定 node_id、名称、平台、版本、能力；独立凭据与撤销；初期可用受控的设备令牌发放流程，配对 UI 后补 |
| 收件箱业务 | 创建/读取/编辑/完成/撤销/删除；字段校验；默认单用户共享收件箱 |
| 存储 | SQLite WAL、schema migration、事务与索引、数据库路径配置、备份恢复 |
| 同步 | 一致快照、持久增量序号、删除标记、操作去重、版本冲突、游标失效恢复 |
| App API | HTTPS JSON API、设备鉴权、错误码、分页、请求尺寸约束、SSE 生命周期 |
| 附件 | 上传暂存/完成、元数据、受鉴权的下载、缩略图、未引用文件清理 |
| App 投影 | 为 HTTP Node 提供经 Core 路由与隐私裁剪的用量/session 摘要，不要求 App 冒充 Web MQTT 产品 |
| 动作结果桥接 | 实现远程动作时接入 Agent results，用 intent/command ID 关联到来源 Node，并区分提交与执行状态 |

建议新增内部模块，如 `internal/inbox`、`internal/storage`、`internal/sync`、`internal/devices`、`internal/attachments`、`internal/appapi`，由 `cmd/orbit-core` 装配；名称可调整。业务逻辑不依赖 HTTP handler 或 Flutter。

建议共享 App 工程放在 `nodes/app/`，由同一工程生成 Android/macOS/Windows 产物。这些路径目前均为规划，不是已有模块。

### 数据对象草案

| 对象 | 必要字段或约束 |
| --- | --- |
| Device | node_id、label、platform、capabilities、credential digest、revoked_at |
| Item | id、kind、body、todo_status（适用时）、created_by_node_id、revision、created_at、updated_at、deleted_at；图片引用 attachment ID |
| Attachment | id、storage_key、MIME、size、dimensions、thumbnail_key、upload state、created_at |
| Change | 单调递增 seq、item_id、item_revision、变更后的完整条目或删除标记；首版用完整条目降低应用复杂度 |
| OperationReceipt | `(node_id, operation_id)` 唯一、规范化请求摘要、已提交结果；同 ID 不同内容必须拒绝 |
| Sync metadata | 数据集 generation、可用增量下界；正常进程重启不改变 generation |
| DeviceItemState | 可选，`(node_id, item_id)` 对应已读/本地隐藏等设备状态 |

关系使用逻辑 ID 和查询所需索引，**不创建物理 FOREIGN KEY**。引用校验、依赖更新、删除语义放在应用事务中。全局已完成状态不能放入 DeviceItemState。

设备能力声明只用于兼容与展示，不自动授予权限。Node 身份从凭据解析，不能信任请求正文里的任意 node_id。凭据保存在系统安全存储中；服务端避免保存明文令牌，日志不打印正文、图片或凭据。

## 6. 必须保持的同步不变量

### 写入与重试

1. App 生成稳定 operation_id，将操作保存到本地待发送队列；进程重启后仍可重试。
2. Core 校验设备和请求，查找已提交 OperationReceipt，再进行 expected_revision 检查。
3. 在同一个数据库事务中修改 Item、追加 Change、保存成功操作结果。
4. 事务提交后才返回成功并发出更新通知。Broker/SSE 收到数据不等于业务已提交。
5. 已提交但响应丢失时，重试返回原结果，不再次创建条目或追加相同业务变更。
6. 相同条目的本地操作按依赖顺序提交。一次已经发出的 operation 内容不可原地修改；新的用户修改使用新的 operation_id。

首版保留去重结果，避免过早清理。后续若设置保留期，必须定义过期操作拒绝或恢复规则，不能因去重记录被清除而重新执行旧操作。

### 读取与增量

- Item revision 用于并发检查；Change seq 用于补同步。不要用设备时间、列表排序时间或 core_epoch 替代游标。
- 初始快照与其 high-water cursor 必须来自一致数据边界。分页快照不能跳过过程中发生的编辑和删除；实现时选择一致快照方案并用并发测试证明。
- 按 seq 有序应用变更；本地缓存与 applied_cursor 在同一事务中提交。重复下载可安全重放，旧版本不能覆盖新版本。
- SSE 建议作为“有更新”的提示；收到提示不能直接推进 applied_cursor。首次先建立通知订阅再追赶增量，重连/回前台也重新追赶；连接心跳可携带服务端 high-water 以发现漏通知。
- 用户条目变更与临时状态摘要使用独立的游标/版本语义，状态刷新不能推进消息已应用游标。
- 删除用 tombstone 传递；清理变更历史时维护最早可用游标。游标过旧明确返回 reset_required，客户端重新取快照并清理已删除缓存。
- 正常 Core 重启不清零游标。备份恢复到旧数据集时需要改变 sync generation 或显式要求 reset，避免设备拿着旧的较大游标永远看不到更新。

### 冲突与离线

- 首版采用整条 Item 乐观并发控制：提交 expected_revision，冲突返回服务端最新条目；保留本地草稿供用户处理。
- 目标状态操作使用 `set_completed(true/false)`，避免 toggle 重试反转。
- UI 从本地数据库读取；权威快照与本地待提交修改分开，后台同步不能悄悄抹掉草稿。
- 展示“待发送 / 已保存 / 失败 / 冲突”，其中“已保存”明确指 Core 确认持久化。
- 不需要为用户内容复用当前 20/30 秒 Intent TTL；远程主机命令继续保留短时语义，不能离线几小时后自动打开旧 session。

## 7. API 与附件交付边界

以下是待实现的 API 形状，不是当前可调用端点。具体命名可调整，但需要固定错误和版本契约，并保存到仓库供客户端共用。

| 建议接口 | 用途 |
| --- | --- |
| `GET /api/v1/status` | 当前设备获授权的用量/session 摘要 |
| `GET /api/v1/items` | 按稳定排序和游标分页浏览；列表分页游标与同步游标分开 |
| `GET /api/v1/sync/snapshot` | 初始化或 reset，返回一致快照及其 generation/cursor |
| `GET /api/v1/changes?after=...` | 有序增量、next_cursor、has_more、reset_required |
| `GET /api/v1/events` | 前台更新提示、连接心跳与状态摘要更新；使用设备鉴权 |
| `POST /api/v1/operations` | 类型化 create/update/set_completed/delete，operation_id 与 expected_revision |
| `POST /api/v1/attachments` | 上传并返回可引用的附件 ID |
| `GET /api/v1/attachments/{id}` | 受鉴权的原图/缩略图读取 |

设备令牌发放/配对/撤销接口、远程动作接口在对应里程碑中确定。App 首版可用受控配置发放令牌，不需要先建设账号注册和密码找回。

至少区分未认证、设备已撤销、字段错误、版本冲突、操作 ID 被复用、附件未完成/不可访问、游标失效和暂时不可用。整数 revision/cursor 采用跨 Dart/JavaScript 精确往返的表示（例如不透明字符串），不要依赖 JS number 的大整数精度。

附件先完成上传，再创建引用它的条目。失败上传和未引用暂存文件按规则回收；仍被有效条目引用的原图不能误删。下载端按需取原图、缓存缩略图。上传与消息提交需要独立可见的重试状态，MQTT 中不塞图片或 Base64。

部署需增加持久数据卷、附件目录、HTTP 监听与反向代理配置。数据库和附件一起纳入备份；WAL 模式下使用一致备份方式或停机备份，不能只随意复制正在写入的主数据库文件。业务库、上传文件、设备凭据和本地配置应加入忽略规则，不提交真实数据。

## 8. 客户端公共能力与平台差异

公共层实现设备接入、鉴权、本地数据库/迁移、API client、同步协调器、待发送队列、冲突处理、附件缓存、状态区、列表与详情。Android 回前台/网络恢复时追赶同步；各桌面端复用相同规则。

| 端 | 基础交付 | 后续增强 |
| --- | --- | --- |
| Android | 安全凭据存储、前台同步、文本剪贴板、系统图片选择、预览；可安装 APK | 接收系统分享、通知按钮、推送 |
| macOS | 共用 UI/同步、剪贴板、文件选择、键盘交互、窗口状态 | 菜单栏、快捷键、拖放、通知、分享入口 |
| Windows | 共用 UI/同步、剪贴板、文件选择、键盘交互、窗口状态 | 托盘、快捷键、拖放、通知、开机启动 |
| Web | 初期保留现有状态页；接入收件箱时复用相同业务 API，并明确浏览器设备身份 | IndexedDB 离线缓存、浏览器通知 |
| Agent | 保持现有采集与 Capability；用户消息无需安装额外 Agent | 主机动作结果完善，新增平台的采集/执行适配 |
| OLED | 保持现有用量展示 | 经 Core 投影待办数/最新消息摘要，按钮操作按能力另行实现 |

Flutter 的系统插件是否覆盖三端、最低系统版本、打包签名方式要在实现时核实。macOS 构建需要对应工具链，Windows 产物应在 Windows 环境/CI 构建验证；Android 首版通过真实设备或模拟器验证，不能只用桌面预览代替。

## 9. 与既有协议和 ADR 的关系

- [ADR-0010](adr/0010-keep-mqtt-as-core-transport.md) 的“V1 唯一基础设施/内存状态”需要新增 ADR 说明本次持久业务需求与 HTTP 接入边界，不应默默改变既有文档。
- [ADR-0015](adr/0015-core-processes-only-new-observations.md) 可以继续适用于临时 Observation；用户 Item 明确跨重启恢复。
- [ADR-0008](adr/0008-use-ephemeral-idempotent-v1-commands.md) 继续约束旧 Agent 命令；业务 Operation 是新的持久事务契约。
- [ADR-0018](adr/0018-core-owns-node-data-routing.md) 的 Core 路由与隐私边界保留；App 用户输入不会成为自报 Agent 路由授权。
- App 身份在 Core 设备模块登记。不要让 HTTP App 冒充 `display/web/browser`，或为了接受它直接取消产品与授权校验。
- 按 [ADR-0017](adr/0017-version-breaking-protocol-changes.md) 处理不兼容 MQTT/Protobuf 改动。优先通过新增 App API 演进；若改变 Protobuf，运行生成器并验证固件消费者。
- 当前同一路由不聚合多个 Agent 的同种观测。若将来一个面板要展示多台电脑的 session，需新增聚合模型与稳定来源标识；本次不假设已有此能力。

## 10. 建议里程碑与验收

### M0：建立基线与契约

- [x] 阅读本文和当前代码，在下一台机器核对工作区、工具链和现有测试。
- [x] 记录选定的 Flutter/插件版本、App 工程位置、业务数据路径与部署方式。
- [x] 增加 App 接入/持久化 ADR，确定 Item、Operation、Change 与鉴权契约。
- [x] 准备不含私人内容的固定样例数据；当前没有用户消息来源，查看端验收需可重复的 seed/测试 fixture。

### M1：Android 查看端与同步底座

- [x] Core 设备凭据、数据库/迁移、一致快照、增量读取、状态 API/SSE。
- [x] Android 设备配置、安全凭据存储、本地缓存、回前台同步、固定摘要区和分页列表。
- [x] 使用样例文本、待办和附件元数据验收列表与详情；显示 stale/离线状态，复制文本可离线完成。
- [x] Core 重启后样例条目仍存在；杀掉 App 后重开可先看到缓存，再追赶增量。

M1 是只读里程碑，不表示已经支持从 App 创建消息或完整图片上传；可靠写入闭环在 M2/M3 完成。

### M2：文本与待办的跨设备操作

- [x] 创建/编辑/完成/撤销/删除，持久操作去重、版本冲突和删除同步。
- [x] 客户端待发送队列、明确的保存状态、离线编辑草稿、失败重试。
- [x] 用两个独立 node_id 的客户端或集成测试完成端到端验证；所有业务修改经 Core。
- [ ] 如开放 session 远程操作，补齐 Agent result 回传及超时/失败 UI。（本次未开放，沿用后续范围。）

### M3：图片、桌面端与运行交付

- [x] 图片上传/下载/缩略图/缓存/孤立文件回收，正文与附件引用的一致性。
- [ ] macOS、Windows 使用同一 App 核心，分别验证剪贴板、文件选择和打包产物。
- [x] 持久数据卷、备份恢复、设备撤销、日志与错误码、配置示例和运行文档。

### 后续

Android 分享入口、推送、桌面托盘/快捷键、按设备已读、指定设备投递、硬件消息摘要。推送只触发提醒或同步，仍通过持久 API 补齐内容。

### 必测失败场景

| 场景 | 验收结果 |
| --- | --- |
| 服务端提交成功但 HTTP 响应丢失，App 重试 | 只有一条条目/一次业务变更，返回相同提交结果 |
| 相同 operation_id 提交不同内容 | 明确拒绝，不悄悄覆盖 |
| Core 在条目写入与变更记录之间异常 | 同一事务全部提交或回滚，不出现已保存但无法同步的条目 |
| 设备离线期间其他设备编辑/完成/删除 | 恢复后状态正确，删除内容不会复活 |
| 两端基于同一旧版本同时编辑 | 一个成功，另一个明确冲突且草稿保留 |
| 快照分页/建立 SSE 期间发生修改 | 不丢更新；重复记录不会覆盖更高版本 |
| App 应用变更中途崩溃 | 缓存与游标一致，重启继续补同步 |
| Core 正常重启、增量历史过期、恢复旧备份 | 分别正常续传、显式 reset、识别数据集变更后重建缓存 |
| Broker 不可达 | 消息 API 仍能工作，采集摘要 stale，主机动作明确不可用 |
| 设备凭据撤销、冒用其他 node_id、读取不可访问附件 | 服务端拒绝，正文和凭据不进入日志 |
| 图片上传中断或上传成功但条目未创建 | 可重试，暂存文件能回收，有效引用文件保留 |

## 11. 换机后启动与验证

从已同步的 `feat/app` 分支接手；仓库路径以新机器实际 checkout 为准。真实配置、证书、设备凭据和运行数据通过私有渠道配置，不从交接文档恢复。

在仓库根目录核对现有基线：

```sh
git status --short
git log -3 --oneline
go version
go mod download
make test-go
make build-go
```

当前 [go.mod](../go.mod) 要求 Go 1.27.0；检查实际工具链后再处理安装。上面的命令属于接手验证步骤，不表示本次交接已执行。

实现 App 后增加并运行 `flutter analyze`、`flutter test` 与 Android 构建；具体命令和最低 SDK 随工程写入 `nodes/app/README.md`。Go 写入/同步模块增加前述失败场景的事务和集成测试，关键并发路径运行 race 检查。

若更改协议，运行现有 `make generate`、`make proto-lint`；涉及硬件消费者时再执行 `make test-node`、`make build-node`。完整现有验证入口是 `make verify`，它需要 Go、Buf、Python/uv、PlatformIO 等对应环境。纯文档提交只做链接、内容和 diff 检查。

相关既有启动入口是 `make dev-agent`、`make dev-core`、`make dev-web`，配置规则见 [README](../README.md)。新增 App 与 API 配置应补充示例和 Make 命令；不要假设当前 `make dev` 已包含 Web 或未来 App。

可直接交给下一位实现者的任务：

> 阅读 `docs/app-node-handoff.md`，按不引入 IM 的个人收件箱方案，从 M0/M1 开始实现 Android App Node 与 Core 持久化同步底座。按文档区分已存在能力、用户明确需求和建议默认值；保留既有 Agent/OLED/Web 行为。首版只要求打开 App 后同步，后续按 M2/M3 增加文本/待办操作、图片及 macOS/Windows。每个里程碑更新本文清单，记录实际运行的检查、结果与未完成项，不把模拟测试表述为真实设备或生产验收。


## 执行进展

### 2026-10-07 16:52（Asia/Shanghai）— Android 首版已验证

- 实现：`internal/inbox` 持久条目/事务/回执/变更/附件元数据，`internal/appapi` 设备鉴权、JSON API、SSE、图片文件与清理，Core 可选 HTTP 启动独立于 MQTT。设备按配置发放/撤销，令牌仅保存摘要。App 使用独立 `overview-app` 路由，拒绝 MQTT 产品冒充。
- App：`nodes/app/`，Flutter 3.44.8 / Dart 3.12.2；ChangeNotifier + sqflite 简化原建议栈。固定状态摘要、可展开会话、分页列表、文本/待办/图片、复制、编辑、删除、离线缓存与队列、冲突草稿、安全令牌存储、前台 SSE/定时追赶均已接入。无后台推送，无主机会话远程动作入口。
- 服务端证据：`go test ./...`、`go vet ./...`、`go build ./...` 通过；`go test -race ./internal/inbox ./internal/appapi ./internal/core ./internal/config` 通过。覆盖重复提交及重启回执、操作 ID 复用、双写冲突、事务注入失败回滚、固定边界分页、删除、generation reset、鉴权/撤销/身份注入、私有未引用附件、有效引用的清理保护及 App 路由隔离。
- 客户端证据：`flutter analyze` 无问题，`flutter test` 7 项通过（持久队列/游标原子性/旧回执/冲突草稿/分页中断/360 与 900 逻辑像素布局）。`scripts/app-smoke.py` 在 Android 8 / API 26 ARM64 模拟器通过真实 Go API 端到端测试，覆盖连接、安全存储、前台同步、离线文本、待办、两设备冲突、图片上传和原图/缩略图读取。测试 Core 的 Broker 故意不可达。
- 冷启动检查：停止 Core 后强制结束并重开 Android App，缓存仍可读；离线新增后再强制结束重开，待发送记录仍保留；恢复 Core 后仅保存一次，原有条目也仍存在。Android 8 系统 SQLite 不支持 JSON1/新版 UPSERT，本地缓存已使用兼容查询，模拟器验证通过。
- 交付：`nodes/app/build/app/outputs/flutter-apk/app-debug.apk` 为可安装调试包；[实际界面截图](../nodes/app/docs/android-inbox.png)。配置与操作见 [App README](../nodes/app/README.md)、[API 契约](app-api.md)、[部署/备份](app-operations.md)、[ADR-0021](adr/0021-app-inbox-over-http.md)。Compose 配置校验与 diff 检查通过。
- 边界：macOS/Windows 工程复用同一业务与 UI，但未完成平台验收；macOS 构建停在缺少开发签名配置（Keychain entitlement 需要），本机无法生成 Windows 产物。未做物理真机、生产 Broker/HTTPS、生产备份恢复或正式签名发布验收。M3 桌面交付仍未完成。
- 工具环境：本机 Homebrew Flutter 可执行文件启动异常，本次使用仓库忽略目录 `.tools/flutter` 的本地 SDK 副本完成检查；SDK 未纳入源码。命令可通过 `FLUTTER=/absolute/path/to/flutter` 选择可用 SDK。

### 2026-10-08 — 增加 Android App CI

- 新增 `.github/workflows/build-app.yml`：App/Makefile/工作流相关的 PR 和 main 推送触发，另支持 `v*` 标签及手动运行。使用固定 Flutter 3.44.8、Java 17，安装锁定依赖，执行 `make test-app build-app`，上传 `orbit-android-debug` 调试 APK，保留 14 天。
- 验证：`actionlint`、`git diff --check`、锁定依赖安装通过；本地复跑 Make 命令，静态分析、7 项测试和 APK 构建通过。尚未提交/推送本次工作流，未宣称 GitHub runner 已运行成功。
- CI 当前覆盖 Android；macOS/Windows 平台构建及正式签名发布仍待补充。

### 2026-10-08 — 合并管理台主线

- 合并 `main`（`da65e64`）到 `feat/app`，保留 Core 管理台、Android 小组件、Web 改进与 App 收件箱。
- 管理台路由数据库与 App 收件箱分别存储；管理台支持 `overview-app` 路由编辑，已保存路由即时影响 App 摘要。
- 管理台鉴权配置 `console.password` 必填；App 设备仍使用独立令牌，MQTT 不发布 App 路由。
- 验证：管理台生产构建、Go 全量测试/vet、Core/config/App API/inbox race 检查、Web/dev 测试、Flutter analyze 与 7 个测试、Actions 静态检查通过。

### 2026-10-08 — 更新 tx 实例

- 部署代码：合并提交 `1ee0ee8`；Core/Web 本地构建镜像标签 `app-1ee0ee8`，未发布到镜像仓库。
- HTTPS：`https://orbit-core.wleo.cn` 保留管理台，`/api/v1/*` 转发 App API。服务器回环端口为 `17622`，容器内仍为 `7622`（宿主机 `7622` 已被其他服务占用）。
- 原有 5 条路由保留；新增 `phone-01` App 路由，沿用 tx Web 节点的 macOS 用量/session 数据源。摘要已收到两类数据。
- 私有令牌文件：tx `/srv/orbit-deploy/private/app-phone-01.json`；本地副本 `configs/secrets/tx-app-phone-01.json`（Git 忽略，权限 0600）。
- 停机备份：tx `/srv/orbit-deploy/backups/app-20261008T033758Z`，含原配置、Caddyfile、完整数据。旧 `v0.2.5` 镜像保留。
- 线上验证通过：Core/Web HTTPS、管理台登录与路由、App 未授权拒绝、认证访问、SSE、创建/幂等重试/完成/删除、Core 重启后条目与 generation 保留、两个 SQLite 完整性检查。自检条目已删除。
- 部署目录 `/srv/orbit-deploy` 使用本机镜像；当前重建命令为 `docker compose up -d --pull never orbit-core orbit-web`，不要对未发布的此标签执行 `pull`。

### 2026-10-08 — App 管理台

- 新增设备添加、令牌生成与轮换、即时撤销、当前连接及最近访问/同步请求记录。
- 收件箱支持分页、搜索、类型筛选、文本/待办/图片创建与编辑、完成待办、图片预览和删除，复用版本校验与同步通知。
- 设备从 YAML 首次导入后持久保存；数据库升级到版本 2，阻止旧程序忽略撤销记录。
- 管理台生产构建、Go 全量测试/vet、相关竞态检查通过。浏览器完成设备添加/撤销、文本创建、待办完成、图片上传与预览，390px 与 1280px 布局无横向溢出。截图接口不可用，未做截图像素审查。
- tx Core 已更新为本机构建的 `app-admin-20261008`；管理台 `/apps`、认证接口、原手机令牌、6 条路由和数据库完整性验证通过。
- 升级前完整备份：`/srv/orbit-deploy/backups/app-admin-20261008T035417Z`。产物、基线提交和改动源码快照位于 `/srv/orbit-deploy/releases/app-admin-20261008`，镜像未推送到仓库。


### 2026-10-08 — 统一 Orbit Android 应用

- 新版统一命名 Orbit，以 `nodes/app` 为 Android 构建和发布入口；底部导航集成收件箱与原有状态/桌面组件页面，切换页面保留草稿和状态。
- 复用 `nodes/android` 的 Flutter 页面、原生 MQTT/二维码/前台同步服务和组件实现，沿用 `dev.orbit.orbit_android` 包名，可覆盖升级旧应用并保留加密配置及桌面组件。
- CI 增加共享模块/协议变更触发及原生 JVM 测试；正式签名发布和旧 Make 构建入口均指向统一应用。
- 验证：Flutter analyze、18 项 Flutter 测试、11 项原生 JVM 测试、Android APK 构建、actionlint 和 diff 检查通过；尚未推送或执行远程 CI。
- 已在当前 ADB 真机覆盖安装；旧 MQTT 配置与两个桌面组件编号保留，收件箱账户缓存迁移、HTTPS 连接和 MQTT 数据接收已验证。临时独立收件箱应用已停用并保留数据，桌面仅保留统一 Orbit 入口。
- 升级前应用私有数据备份存放于本地 Git 忽略的 `configs/secrets/android-merge-backup/`，不随源码发布。


### 2026-10-08 — 扫码填写 App 连接

- 控制台添加设备/重置令牌的凭据弹窗增加本地生成的二维码；复用一次性令牌展示，不上传二维码内容到外部服务。凭据弹窗居中并支持窄屏滚动。
- Android 连接页增加「扫码填写」，校验二维码类型、版本、HTTPS 地址和令牌后仅填表，点击连接才保存；无效内容、取消和拒绝权限均保留原输入。相机由扫码组件管理前后台生命周期及退出释放。
- 验证：Flutter analyze、22 项 Flutter 测试和 APK 构建通过，包含格式校验、重复回调、填表后手动连接、拒绝权限和取消保留输入；管理台生产构建通过。浏览器验证添加设备后二维码显示、390/1280px 无横向溢出；独立原生解码器成功读取控制台生成的二维码。浏览器截图接口不可用，未做像素截图验收。
- 当前 ADB 真机已覆盖安装，验证扫码入口、权限请求、相机启动和返回；未宣称完成真实光学扫描验收。未更改原连接凭据。
- tx Core 已更新为本机构建 `app-qr-20261008`，验证新前端资源、HTTPS/管理台鉴权、原手机令牌、6 条路由及数据库完整性。升级前备份 `/srv/orbit-deploy/backups/app-qr-20261008T041919Z`；产物与源码快照位于 `/srv/orbit-deploy/releases/app-qr-20261008`，未推送远程仓库或运行远程 CI。


### 2026-10-08 — 控制台亮色风格

- 白色侧栏/卡片、浅灰蓝背景和蓝色主操作，统一文本、边框、表单、弹窗及拓扑配色；保留成功/警告/失败状态区别。登录页改为简洁居中表单，移除大块深色插画与重复英文装饰，导航和页面标题统一为中文。
- 管理台生产构建、格式和 diff 检查通过；浏览器验证登录与六个页面在 390/1440px 无页面横向溢出，桌面同排卡片顶沿/高度一致，添加设备弹窗和移动导航正常。截图接口超时，未做像素截图验收。
- 发布验收曾因历史手机令牌已撤销触发回退；回退前后两个数据库的全部业务表逐行指纹一致。检查脚本改为验证当前设备注册表，尊重现有撤销状态。
- tx 已部署 `console-light-20261008`，新主题资源、管理台登录/API、6 条路由、数据库完整性检查通过。发布备份 `/srv/orbit-deploy/backups/console-light-20261008T042619Z`，产物和源码快照位于 `/srv/orbit-deploy/releases/console-light-20261008`。


### 2026-10-08 — 可配置的控制台会话有效期

- Core YAML 增加 `console.session_hours`，单位小时，默认 72（3 天），拒绝非正值和会导致 duration 溢出的时长。Cookie Max-Age/Expires 与服务端过期时间使用同一配置；系统信息返回实际时长，登录页去掉固定 24 小时文案。
- 配置修改后重启 Core 生效；原有退出撤销、重启失效和固定到期语义保留。
- Go 全量测试/vet、Core/config race 检查、前端生产构建与格式检查通过；覆盖默认值、自定义配置、非法值、Cookie 属性、到期前/到期时边界及系统接口时长。
- tx 已更新 `console-session-20261008`，线上登录 Cookie Max-Age=259200、系统信息 session_hours=72 校验通过，管理台接口、6 条路由和数据库完整性正常。备份 `/srv/orbit-deploy/backups/console-session-20261008T042902Z`，产物和源码快照位于 `/srv/orbit-deploy/releases/console-session-20261008`。


### 2026-10-08 — 消息列表避免临时操作提示挤动

- 普通完成/撤销完成、编辑、删除的待发送操作不再作为卡片插入列表顶部，改为原消息状态行的「待同步」；待办勾选先反映本机排队结果，确认后恢复正常状态。新建内容草稿及失败/冲突恢复入口保留。
- 复制和错误反馈使用 2 秒悬浮 SnackBar，并清理旧提示队列。
- Flutter analyze、22 项 Flutter 测试、APK 构建与 diff 检查通过。360/900 逻辑像素下，回归检查完成/编辑/删除排队和确认前后的消息矩形一致，同时保留离线新增草稿与失败重试入口。


### 2026-10-08 — 转发规则接入 App 设备选择

- 根因：规则编辑器仅使用 MQTT 发现的 nodes 候选，遗漏独立 App 注册表；App 卡片链接没有携带设备 ID。
- 接收设备下拉合并已发现设备与 App 注册设备，按名称选择并自动匹配视图；MI14U 使用 `overview-app`，旧 Android 小组件保留 `overview-android`。已撤销项标注并禁选，手动输入 ID 预配置保留，App 列表加载失败提供重试。
- App 卡片「配置转发规则」直接打开对应设备编辑器；已有 App 规则显示设备名称与注册状态。
- 前端生产构建、格式和 diff 检查通过；浏览器验证真实 MI14U 的卡片直达、新建规则选择、自动视图、旧 Android 切换及已撤销项禁选；390px 页面/抽屉无横向溢出。验收未保存或修改用户的转发规则。
- tx 已部署 `app-routing-20261008`，线上新前端资源、管理台登录/设备/路由接口、72 小时会话和数据库完整性验证通过。备份 `/srv/orbit-deploy/backups/app-routing-20261008T053020Z`，产物与源码快照位于 `/srv/orbit-deploy/releases/app-routing-20261008`。

### 2026-10-08 — 同步 tx 服务构建

- 重新构建当前源码，Core 二进制 SHA-256 与线上 `app-routing-20261008` 完全一致，保留运行实例；Web 更新为 `web-sync-20261008`，线上二进制与本地构建一致。
- 前端构建、Web/config Go 测试通过；线上 Core/Web 登录与 API、MI14U 有效注册和未授权拒绝检查通过。未修改设备、路由或收件箱数据。
- Compose 备份 `/srv/orbit-deploy/backups/web-sync-20261008T054914Z`；Web 产物与更新脚本 `/srv/orbit-deploy/releases/web-sync-20261008`，旧镜像保留。

### 2026-10-08 — 简化 App 收件箱

- 移除顶部用量/会话摘要；输入框下方以「待办」开关替代类型菜单，新消息默认文本，成功保存后复位。已有待办保留勾选操作，完成后正文不加删除线。
- Flutter analyze、22 项测试、APK 构建及 diff 检查通过；360/900 像素验证开关位置、文本/待办提交、发送后复位、完成样式与消息位置稳定。
- 已覆盖安装当前 ADB 手机并打开 Orbit，真机界面确认顶部用量已移除、待办开关默认关闭。

### 2026-10-08 — 消息折叠与待办切换

- 消息按实际排版行数判断，超过 8 行默认收起，可独立展开/收起，包含自动换行与离线新建草稿。
- 输入框下方待办开关持久保存在本机，发送后保持选择；文本/待办菜单增加类型互转，图片保留附件类型。新增 set_kind 操作，复用离线队列、版本冲突、幂等回执和变更同步；实际类型改变时清除完成状态并保留正文。
- Flutter analyze、24 项 Flutter 测试、APK 构建、Go 全量测试/vet、inbox/App API race 检查通过。覆盖 8/9 行边界与自动换行、开关数据库重开恢复、菜单双向转换、正文保留、冲突/重试与跨设备同步。
- tx Core 已更新 app-inbox-20261008，登录/API、会话、数据库完整性及二进制哈希验证通过；备份 /srv/orbit-deploy/backups/app-inbox-20261008T070649Z。APK 已覆盖安装当前 ADB 手机，真机可见展开入口。

### 2026-10-08 — 隐藏发送过程警告卡

- 收件箱仅为失败/冲突操作展示草稿提示；排队、发送中和图片上传不再插入黄色卡片。失败保留重试/移除入口，冲突保留处理入口，待发送内容继续保存在本机队列。
- Flutter analyze、4 项相关界面测试、APK 构建与 diff 检查通过；覆盖 360/900 像素下待发送不挤动已有消息、上传不提示、失败/冲突展示及恢复 pending 后隐藏。已覆盖安装当前 ADB 手机；无需更新服务端。

### 2026-10-08 — 删除消息可在控制台恢复

- 沿用 deleted_at 标记实现软删除；控制台收件箱增加「未删除 / 已删除」筛选和单条恢复。恢复仅允许管理台调用，校验版本并复用幂等回执与同步变更，保留正文、完成状态及创建信息。手机删除确认说明可在控制台恢复。
- 已删除图片保留附件，管理台仍可预览；未引用上传继续按原规则清理。旧版本已清理的附件无法恢复，恢复操作会拒绝缺失附件。
- Go 全量测试/vet、inbox/App API/Core race、Flutter analyze 与 24 项测试、前端和 APK 构建通过。覆盖删除/恢复、权限、附件保留、旧回执和版本冲突，以及恢复后手机缓存重新显示。
- ego-browser 在独立本地数据集验证筛选、单条恢复、图片预览和 390px 无横向溢出。未恢复或删除用户线上内容。
- tx 已部署 app-trash-20261008，已删除查询、登录/API、会话和数据库完整性检查通过。备份 /srv/orbit-deploy/backups/app-trash-20261008T071732Z；APK 已覆盖安装当前 ADB 手机。

### 2026-10-08 — 消息正文滑动交给列表

- 正文从自带滚动层的 SelectableText 改为 SelectionArea + Text，保留长按复制和消息展开/收起，移除正文的独立滚动层。
- Flutter analyze、7 项界面测试、APK 构建和 diff 检查通过；覆盖短消息、收起和展开正文上的双向拖动，以及长按复制入口。
- 已覆盖安装当前 ADB 手机；真机界面树显示消息区域只有外层列表可滚动，正文节点不再可滚动。无需更新服务端。

### 2026-10-08 — 浏览消息时收起底部区域

- 上滑浏览列表时，以短动画收起输入框和底部导航；下滑或切换筛选时恢复。草稿及待办开关状态保留，空列表不触发收起。
- 移除收件箱标题和筛选标签行，将全部/待办/图片放入刷新按钮左侧的无边框下拉组件。
- Flutter analyze、29 项测试、APK 构建和 diff 检查通过；覆盖 360/900 像素布局、滚动方向、停下后位置稳定、草稿保留和空筛选恢复。
- 已覆盖安装当前 ADB 手机；真机上滑后输入框与导航同时隐藏，列表高度由 2077 增至 2836 物理像素，下滑后均恢复。无需更新服务端。

### 2026-10-08 — v0.2.7 个人收件箱发布到 tx

- `v0.2.7` 指向 `3ffd3c0`，包含 App/Web 收件箱、纯收件箱模式、连接简化、图标样式及待办复选框对齐修复。Release 构建：https://github.com/leowzz/orbit/actions/runs/37755159627 。
- tx Core 和 Web 均已切换到阿里云镜像 `v0.2.7`；镜像摘要与 CI 签名验收后的产物记录一致。
- Web Node 使用独立的「tx Web 收件箱」授权接入 Core，沿用原登录密码；令牌仅保存于服务端、由运行用户读取。入口：https://orbit-web.wleo.cn/inbox/ 。
- 公网登录、Secure Cookie、收件箱快照、SSE、未授权拒绝、线上 CSS 哈希及数据库完整性验证通过。原有消息、附件、6 条路由及设备令牌/撤销状态保持不变，两个容器重启计数为 0。
- 升级前备份：`/srv/orbit-deploy/backups/v0.2.7-20261008T091826Z`；部署脚本与镜像摘要记录：`/srv/orbit-deploy/releases/v0.2.7`。旧镜像保留，未变更 Caddy 或 MQTT 配置。

### 2026-10-08 — v0.2.9 设备删除操作发布到 tx

- 设备列表增加「删除设备」及确认提示；删除登记会使令牌立即失效、断开 SSE，保留共享消息和转发规则，重启不会重新导入 YAML 中的旧设备。支持仍在使用和已撤销的设备。
- 功能提交 `7ca079d`；`v0.2.8` 因 Flutter 测试固定等待 100ms 导致偶发失败，未部署到 tx。`7c1e7f7` 改为等待真实保存状态，本地 35 项 Flutter 测试及 analyze 通过；`v0.2.9` 指向 `2d159f0`。
- tx Core/Web 已更新到 `v0.2.9`，镜像摘要与 CI 签名记录一致。公网验证删除接口的管理员权限、未登录拒绝、跨站拒绝与不存在设备返回值；线上前端资源包含删除按钮。没有删除任何已有设备。
- Web 收件箱登录与快照正常，消息、附件、设备注册表及 6 条路由保持不变，数据库完整性正常，容器重启计数为 0。
- 升级前备份：`/srv/orbit-deploy/backups/v0.2.9-20261008T093920Z`；部署记录：`/srv/orbit-deploy/releases/v0.2.9`；构建：https://github.com/leowzz/orbit/actions/runs/37757699412 。
