# Orbit 总体设计

> 状态：Draft  
> 更新时间：2026-09-03  
> 依据：[项目交接说明](../HANDOFF.md)

## 1. 文档目的

本文定义 Orbit 首个可用版本的系统结构、模块接口、消息契约、运行时流程、故障语义与验收边界，用于指导后续协议和代码实现。

本文不替代具体的 Protobuf 文件、MQTT ACL、部署清单或硬件接线说明。它们应在实现阶段成为本文设计的可执行产物。

## 2. 背景与目标

Orbit 是个人设备与能力网络。它连接可信主机、云端消息通道以及 OLED、TFT、Web 等节点，使主机能够发布经过整理的状态，并执行协议明确允许的命令。

首个版本需要证明一条完整闭环：

1. 主机上的 Orbit Agent 采集本地状态。
2. Orbit Core 将状态投影为设备可消费的语义视图。
3. ESP32 OLED 节点通过 MQTT 接收并渲染最新视图。
4. 可信客户端发送类型化命令。
5. Agent 至多执行一次本地副作用，并返回可关联的结果。

### 2.1 设计目标

- 设备只理解稳定、紧凑的语义状态，不理解上游系统的私有数据结构。
- 命令能力是显式白名单，不形成远程 Shell。
- MQTT 重复投递、离线重连和乱序到达不会导致重复副作用或状态回退。
- Core、Agent 与 Node 可以独立演进；首版允许 Core 与生产端逻辑同进程部署。
- 在网络不稳定时保留最后一个可用画面，并明确标记过期状态。
- 所有发送到设备的数据都经过隐私裁剪。

### 2.2 非目标

- 通用远程执行平台。
- 由云端生成像素帧或统一控制各设备的页面导航。
- 以 MQTT retained message 充当历史数据库。
- 首版支持任意第三方插件、动态脚本或用户自定义命令字符串。
- 首版实现跨区域高可用、复杂多租户或大规模设备编排。

## 3. 关键约束

| 领域 | 约束 |
| --- | --- |
| 后端与主机端 | Go |
| 结构化协议 | Protocol Buffers，包名从 `orbit.v1` 开始 |
| 消息传输 | MQTT over TLS |
| 命令模型 | `oneof` 类型化动作，禁止 `google.protobuf.Any` 和任意命令字符串 |
| 设备数据 | 只发送语义视图，不发送提示词、正文、终端输出、凭据或任意本地路径 |
| 投递语义 | MQTT QoS 1；副作用幂等由应用层保证 |
| 身份 | 每个 Agent、Core、Node 使用独立身份和最小权限 ACL |

## 4. 总体结构

```text
                    observations                 projected views
+----------------+      QoS 1      +------------+      QoS 1 retained      +--------------+
|  Orbit Agent   | --------------> | Orbit Core | -----------------------> |  Orbit Node  |
|  trusted host  |                 | policy +   |                          | OLED/TFT/Web |
|                | <-------------- | projection | <----------------------- |              |
+-------+--------+ commands/results+------+-----+        intents/results   +------+-------+
        |                                  |                                      |
        | source/capability adapters       | latest canonical state               | local render
        v                                  v                                      v
 Codex / Sub2API / OS               optional persistence                   display + input
```

### 4.1 模块划分

#### Orbit Agent

Agent 是可信主机上的深模块。外部接口是“注册观测源、注册能力、启动/停止”；采集调度、连接恢复、命令校验、去重、执行和结果发布隐藏在实现内部。

职责：

- 周期或事件驱动地采集已注册 Source。
- 发布观测值、Source 健康状态和 Agent presence。
- 接收只发往本机身份的命令。
- 在执行前完成解码、版本、目标、时效、授权上下文和参数校验。
- 持久化命令接收状态，保证本地副作用至多执行一次。
- 通过已注册 Capability 执行类型化动作，并发布状态变化和最终结果。

#### Orbit Core

Core 是状态与策略模块，接口是“接收观测或 Intent，发布 canonical state、device view 或 command”。它不依赖显示几何，也不解释任意命令负载。

职责：

- 接收 Agent 和外部生产者的观测。
- 按生产者、修订号和过期时间维护最新 canonical state。
- 应用优先级、隐私、过期与设备策略，生成紧凑的 DeviceView。
- 将受支持的 Intent 映射为类型化命令并路由到目标 Agent。
- 发布 retained view；后续可选择性持久化历史。

#### Orbit Node

Node 是设备接入 Orbit 网络后承担的运行时角色，不是硬件产品名称。带 OLED/TFT 的物理 Node 归入 Device Series；Web 等软件 Node 也可以使用同一协议，但不强制套用硬件型号体系。

职责：

- 使用设备独立凭据连接 MQTT。
- 订阅自己的 retained view，并按本地显示几何渲染。
- 数据过期时保留最后可用内容，同时呈现 stale 状态。
- 可选地将按钮、触摸等输入发布为有限的 Intent。
- 呈现命令已接收、执行中、成功或失败状态。

### 4.2 首版部署形态

逻辑上始终保留 Agent 与 Core 两个模块及各自消息契约。首版可以将二者编译进同一个 Go 进程，以减少部署成本；进程内仍通过明确的 Go 接口传递观测与投影结果，禁止让 Source 直接构造设备视图。这样后续拆分部署时只替换接口适配器，不修改固件协议。

### 4.3 设备产品模型

物理边缘设备使用四层模型，避免把业务用途、芯片、屏幕驱动和实例位置拼成一个不稳定的长名称：

| 层级 | 定义 | 示例 |
| --- | --- | --- |
| Device Series | 共享产品定位、交互范式和固件主干的一组设备 | `display` |
| Device Model | Series 内稳定的显示与交互能力组合 | `oled-128x32` |
| Hardware Variant | 同一 Model 的开发板、引脚或器件装配差异 | `esp32s3-devkitc`、`yd-esp32-s3` |
| Device Instance | 已发放身份和凭据、实际接入网络的一台设备 | `desk-oled-01` |

四层标识必须作为独立字段保存。完整身份由结构化数据表达，不把它们重复拼进 `device_id`。其中：

- `series_id` 和 `model_id` 由项目维护，发布后保持稳定。
- `variant_id` 对应构建目标和硬件配置，不影响 DeviceView 的业务语义。
- `device_id` 在凭据发放时确定，只要求在部署范围内唯一，不承担型号描述。
- Node 上报自身 Series、Model、Variant 和固件版本；Core 据此校验兼容性并选择投影能力。

### 4.4 Orbit Display Series

首个设备系列定名为 **Orbit Display**，规范标识为 `display`。它覆盖 OLED、TFT 及后续同类小型状态显示设备，共享以下能力：

- 订阅语义化 DeviceView，并在本地完成字体、布局、分页和像素渲染。
- 保留最后可用画面并呈现 fresh/stale/offline 状态。
- 根据具体 Model 声明显示尺寸、颜色能力和输入能力。
- 可选地发布按钮或触控 Intent，并显示关联的命令反馈。

首个 Device Model：

| 属性 | 值 |
| --- | --- |
| 展示名称 | Orbit Display OLED 128×32 |
| `series_id` | `display` |
| `model_id` | `oled-128x32` |
| 显示器 | SSD1306，128×32，单色 OLED |
| 主控平台 | ESP32-S3 |
| Hardware Variant | ESP32-S3 DevKitC、YD-ESP32-S3 |
| 首个用途 | 展示 Token 用量等紧凑指标 |

原 `sub2api-usage-esp32s3` 的名称同时绑定了数据来源和芯片，不适合作为长期设备身份。迁入 Orbit 后：

- 固件/目录按 `display/oled-128x32` 组织。
- PlatformIO 环境名只表达 Variant，例如 `esp32s3_devkitc`、`yd_esp32s3`。
- Sub2API 用量是 `UsageObservation` 投影出的内容，不属于 Series、Model 或固件名称。
- 对外展示名称允许带 `Orbit` 品牌前缀；协议与目录标识不重复携带 `orbit`。

## 5. 核心接口

以下接口用于说明模块形状，最终命名可随 Go 包结构微调。

```go
type Source interface {
	Observe(ctx context.Context) (ObservationPayload, error)
}

type Capability interface {
	Execute(ctx context.Context, action Action) (CapabilityResult, error)
}

type Projector interface {
	Apply(ctx context.Context, observation Observation) ([]DeviceView, error)
}

type CommandStore interface {
	Accept(ctx context.Context, command Command) (AcceptResult, error)
	Complete(ctx context.Context, commandID string, result CommandResult) error
}
```

接口约束：

- `Source` 返回领域观测，不直接发布 MQTT 消息。
- `Capability` 只接收 Agent 已验证并转换后的类型化动作。
- `Projector` 是隐私与设备投影的唯一入口；调用者不能绕过它发布 DeviceView。
- `CommandStore.Accept` 必须原子地区分首次接收与重复接收。
- MQTT、Codex、Sub2API 和具体存储实现是各自 seam 上的 Adapter。
- 只有确实存在第二种实现或测试替身时才抽取额外接口。

## 6. 协议模型

### 6.1 公共元数据

所有顶层消息携带统一的 `Metadata`：

| 字段 | 含义 | 约束 |
| --- | --- | --- |
| `message_id` | 本次消息实例 ID | 全局唯一，用于追踪，不承担命令幂等 |
| `producer_id` | 生产者身份 | 必须与 MQTT 认证身份和 Topic ACL 相符 |
| `revision` | 同一逻辑对象的单调修订号 | 消费者拒绝低于当前值的状态更新 |
| `produced_at` | 生产时间 | UTC 时间戳 |
| `expires_at` | 应用层过期时间 | 状态用于 stale 判定，命令用于拒绝迟到执行 |

设备不能仅依赖本地墙钟判断乱序，应先比较 `revision`，再使用 `expires_at` 判断时效。生产者重启后必须恢复修订号，或使用新的逻辑对象/epoch；具体编码在 Protobuf 设计阶段固定。

### 6.2 Observation 与 Canonical State

Observation 表达某个 Source 在某一时刻观察到的事实，不包含设备布局。首版至少支持：

- `SystemObservation`：主机在线与有限健康指标。
- `CodexObservation`：脱敏后的任务状态汇总。
- `UsageObservation`：Sub2API 等来源的额度、成本、Token 和 TPM 摘要。
- `SourceHealth`：最近成功时间、健康状态和稳定错误码。

Core 依据 `(producer_id, source_type, subject_id)` 识别逻辑对象，只接受修订号更新的 Observation。Canonical State 是 Core 内部模型，不直接承诺为公共线协议。

### 6.3 DeviceView

DeviceView 是面向某个设备的最小语义模型，建议由可组合区块构成，但区块类型必须显式枚举：

```text
DeviceView
  metadata
  device_id
  freshness
  sections[]
    summary | metric | task_status | alert | command_feedback
```

首版应限制：

- 最大编码尺寸。
- `sections` 最大数量。
- 所有字符串的最大 UTF-8 字节数。
- 枚举未知值的降级行为。
- 固件端静态内存预算。

Node 决定字体、换行、分页和像素布局；Core 决定展示哪些语义信息。

### 6.4 Command 与 CommandResult

Command 使用 `oneof action` 形成能力白名单。首版建议只实现无破坏性的测试能力，再加入 `OpenCodexSession`。

每个 Command 同时包含：

- `command_id`：副作用幂等键，由发起方生成且不可复用。
- `target_host_id`：明确目标主机。
- `expires_at`：交互命令默认不超过 30 秒。
- 类型化动作及其受限参数。

结果状态机：

```text
                   +----------+
received --------> | ACCEPTED | --------> RUNNING --------> SUCCEEDED
   |               +----------+                |-----------> FAILED
   |                    duplicate returns stored state/result
   +--> REJECTED
   +--> EXPIRED
```

`REJECTED` 表示命令从未获得执行资格；`FAILED` 表示已接受但执行失败。`error_code` 是稳定、可机读的枚举或受控字符串，`safe_message` 只能包含可向发起设备公开的信息。

### 6.5 Intent

Node 不直接决定主机命令参数，而是发送受限 Intent，例如“打开当前选中的 Codex 任务”。Core 根据设备权限、当前 view revision 与投影上下文解析 Intent，并生成具体 Command。

Intent 必须携带触发时所见的 `view_revision`。当选择对象已经变化或过期时，Core 拒绝 Intent，避免按钮操作命中新的对象。

## 7. MQTT 设计

### 7.1 Topic

```text
orbit/v1/hosts/{host_id}/observations
orbit/v1/hosts/{host_id}/state
orbit/v1/hosts/{host_id}/commands
orbit/v1/hosts/{host_id}/results
orbit/v1/devices/{device_id}/view
orbit/v1/devices/{device_id}/intents
orbit/v1/clients/{client_id}/presence
```

### 7.2 投递属性

| 消息 | QoS | Retained | 说明 |
| --- | --- | --- | --- |
| Observation | 1 | 否 | Core 自行维护最新状态 |
| Host state | 1 | 是 | 供受权客户端读取最新主机状态 |
| DeviceView | 1 | 是 | Node 重连后立即获得最新视图 |
| Command | 1 | 否 | 应用层时效与去重 |
| CommandResult | 1 | 否 | 重复命令时可重新发布已存结果 |
| Presence | 1 | 是 | 使用 LWT 发布离线状态 |
| Intent | 1 | 否 | 必须带时效与 view revision |

所有 payload 使用 `application/protobuf`。MQTT 5 Message Expiry 可作为传输优化，但不能替代 Protobuf 内的 `expires_at`。

### 7.3 ACL 原则

| 身份 | 可订阅 | 可发布 |
| --- | --- | --- |
| Agent `{host_id}` | 自己的 commands | 自己的 observations、results、presence |
| Core | 全部 observations、intents、results | views、commands、必要的 canonical state |
| Node `{device_id}` | 自己的 view、获授权的 result | 自己的 intents、presence |
| 管理客户端 | 明确授权范围 | 明确授权范围 |

Broker ACL 负责 Topic 级权限，应用层仍必须核验消息中的 producer、target 和 device identity，形成纵深校验。

## 8. 关键运行时流程

### 8.1 状态发布

```text
Source -> Agent scheduler -> Observation -> MQTT -> Core
Core -> validate/order/expire -> canonical state -> privacy projection
Core -> retained DeviceView -> MQTT -> Node -> local render
```

Source 失败时，Agent 发布稳定的 SourceHealth 变化，不用空值覆盖最后成功观测。Core 根据最后成功时间和 `expires_at` 将相关视图区块标记为 stale。

### 8.2 命令执行与去重

Agent 收到命令后依次执行：

1. 限制 payload 大小并解码 Protobuf。
2. 校验协议版本、动作类型、目标主机、参数、授权上下文和过期时间。
3. 以 `command_id` 调用 `CommandStore.Accept`。
4. 若已存在，读取并重新发布持久化状态或最终结果，不再次执行。
5. 若首次接收，原子持久化 `ACCEPTED`，再发布 acknowledgement。
6. 进入 `RUNNING`，调用注册的 Capability。
7. 原子持久化最终状态与安全结果，再发布 CommandResult。

进程在步骤 6 的副作用完成后、步骤 7 持久化前崩溃，是严格“至多一次”和自动恢复之间的固有冲突。首版采用保守策略：命令进入 `RUNNING` 后重启不自动重放，标记为 `FAILED` 或 `UNKNOWN_OUTCOME`，需要调用方重新确认；不能声称端到端 exactly-once。

### 8.3 重连与陈旧数据

- Node 重连后接收 retained DeviceView。
- Node 比较修订号，拒绝状态回退。
- 超过 `expires_at` 后继续显示最后内容，但呈现 stale 标识。
- Agent 使用 MQTT LWT 标记意外离线，正常停机主动发布 offline。
- Core 重启后若没有持久状态，可以等待新 Observation；不得把旧 retained view 当作新观测来源。

## 9. 持久化

首版只有命令去重存储是正确性依赖，必须落盘。最低数据模型：

| 字段 | 说明 |
| --- | --- |
| `command_id` | 唯一键 |
| `target_host_id` | 目标主机 |
| `action_type` | 动作类型 |
| `payload_digest` | 防止同 ID 不同内容 |
| `status` | 已接受、执行中或最终状态 |
| `result_bytes` | 可重新发布的最终 Protobuf 结果 |
| `accepted_at` / `updated_at` | 生命周期时间 |
| `expires_at` | 清理依据之一 |

若重复 `command_id` 的 payload digest 不同，必须拒绝并记录安全日志。存储实现待选，但必须支持单进程崩溃恢复、唯一约束和原子状态更新；不引入物理外键约束。

Canonical State 与历史记录在首版不是持久化要求。若后续增加历史库，应作为 Core 内部 Adapter，不改变设备协议。

## 10. 配置与身份

建议使用 YAML 配置非敏感项，凭据通过独立文件或环境注入。配置至少包括：

- 进程 identity、host/device ID。
- Node 的 `series_id`、`model_id`、`variant_id` 和 firmware version。
- Broker 地址、TLS CA、客户端证书或凭据引用。
- Source 的启用状态、采集周期和超时。
- Capability 白名单及本地确认策略。
- payload、字符串、队列和并发上限。
- CommandStore 路径与保留周期。
- 日志级别，不包含协议正文的默认脱敏规则。

启动时必须验证 ID 格式、Topic 可用性、TLS 配置和重复注册；配置错误应快速失败，不以降级明文连接继续运行。

## 11. 安全与隐私

- 所有远程连接强制 TLS，禁止回退明文 MQTT。
- 每个身份单独签发与轮换凭据，禁止固件共享全局凭据。
- Capability 由编译时或配置白名单注册；参数在执行前转换为内部类型。
- `OpenUrl` 只允许受控 scheme 和可选 host allowlist。
- Codex 标题、仓库名和任务摘要必须在 Source 或 Projector 中按明确策略脱敏。
- 日志记录 message/command ID、状态与稳定错误码，不默认记录完整 payload。
- 破坏性或隐私敏感动作在设计本地确认机制前不进入协议。
- Node 固件中不保存 Codex、Sub2API 或主机登录凭据。

威胁模型与证书生命周期将在 `docs/security.md` 中细化。

## 12. 可观测性

所有模块使用结构化日志，公共关联字段包括：

- `message_id`
- `command_id`
- `producer_id`
- `host_id` / `device_id`
- `source_type` / `action_type`
- `revision`
- `status`
- `error_code`

首版建议提供以下指标：MQTT 连接状态和重连次数、Source 成功/失败与耗时、Observation/View 发布次数和大小、过期与乱序丢弃数、命令各状态计数、重复命令数及 Capability 执行耗时。

## 13. 故障语义

| 故障 | 预期行为 |
| --- | --- |
| Broker 暂时不可用 | 有界退避重连；内存队列有上限；不无限积压陈旧观测 |
| Source 超时或失败 | 发布健康变化，保留最后成功值直至过期 |
| 重复 Observation | 按逻辑对象与 revision 忽略，不重复投影 |
| 重复 Command | 返回存储状态/结果，不重复执行 |
| 乱序 View | Node 拒绝较低 revision |
| 未知 Protobuf 字段 | 兼容保留；未知 action 不执行 |
| payload 超限或解码失败 | 拒绝、计数并记录不含敏感正文的日志 |
| Agent 执行中崩溃 | 不自动重放 `RUNNING` 副作用，报告结果不确定 |
| Node 时钟明显不准 | 显示连接/时效异常；不据此触发命令 |

## 14. 测试策略

### 14.1 协议测试

- Buf lint 与 breaking-change 检查。
- Go 与固件端的黄金字节互操作测试。
- 未知字段和未知枚举的前后兼容测试。
- 代表性 DeviceView 的编码尺寸和静态内存预算测试。
- 超长字符串、超多区块和畸形 payload 的拒绝测试。

### 14.2 Agent 测试

- 通过 Source 接口验证成功、失败、超时和调度取消。
- 通过 Capability 接口验证参数校验与状态转换。
- 真实落盘 CommandStore 的崩溃恢复和重复投递测试。
- 同 ID 同 payload 返回旧结果；同 ID 不同 payload 确定性拒绝。
- 过期、错目标、未注册动作和未授权上下文均不触发副作用。

### 14.3 Core 测试

- Observation 乱序、重复、过期和生产者重启场景。
- 隐私字段永远不进入 DeviceView 的契约测试。
- 不同设备策略生成受尺寸约束的视图。
- stale 传播和 Intent 的 view revision 校验。

### 14.4 集成与硬件验收

- 使用本地 MQTT broker 完成 retained view、LWT、重复 QoS 1 投递和断网重连测试。
- OLED 真机验证首帧、陈旧标识、长文本和重连后的显示。
- 命令从测试客户端到 Agent 再到结果订阅端闭环，注入进程崩溃验证不重放。
- 自动化测试通过不能替代真实 Broker 配置、部署环境和物理设备验收。

## 15. 代码组织建议

```text
cmd/orbit-agent/          进程装配与生命周期
cmd/orbit-core/           Core 进程装配与生命周期
internal/agent/           调度、命令状态机、去重
internal/core/            canonical state、策略、投影、路由
internal/mqtt/            MQTT Adapter
internal/sources/         Codex、Sub2API、system Adapter
internal/capabilities/    类型化能力 Adapter
proto/orbit/v1/           线协议源文件
gen/go/                   生成代码，禁止手改
nodes/display/            Orbit Display Series
  common/                 共享协议、连接、状态与渲染基础代码
  models/oled-128x32/     SSD1306 128x32 Model
  models/<tft-model>/     后续 TFT Model
configs/                  非敏感示例配置
docs/                     架构、Topic、安全与决策记录
```

`cmd` 只负责装配，不承载领域逻辑。协议生成代码不反向依赖 `internal`。具体 Source 和 Capability 不能直接依赖 MQTT，以便其测试只跨越自身接口。

### 15.1 Orbit Display 固件技术栈

| 类别 | 选型 |
| --- | --- |
| 语言 | C++17 |
| 硬件平台 | ESP32-S3 DevKitC / YD-ESP32-S3 |
| 应用框架 | Arduino for ESP32 |
| 构建系统 | PlatformIO 6.1.19 |
| 底层运行时 | ESP-IDF 提供的 FreeRTOS、NVS、Wi-Fi、TLS 等能力 |
| Python 依赖管理 | `uv` |
| 辅助脚本与测试 | Python 3.13、PyYAML、pytest |
| 开发命令入口 | Makefile |

Makefile 是开发者的稳定入口，内部通过 `uv run` 调用固定版本的 PlatformIO 和 Python 工具。Series 共享 C++ 固件主干，Model 提供显示能力与布局，Variant 只提供板卡、引脚和构建参数，三者不得复制完整应用逻辑。

## 16. 实施顺序

1. 固定 Protobuf 消息、尺寸上限和兼容策略，完成 Go/固件生成链路。
2. 实现内存 Source、无副作用 Capability 和本地 MQTT 循环，验证消息时序。
3. 实现落盘 CommandStore 与 Agent 命令状态机，完成故障注入测试。
4. 实现 Core canonical state、隐私投影和 retained DeviceView。
5. 接入 `display/oled-128x32` 真机，验证 stale、重连和尺寸预算。
6. 接入 Codex Source 与 `OpenCodexSession` Capability。
7. 在模型稳定后接入 Sub2API 和 TFT 交互。

## 17. 首版验收标准

- Agent 能以 Protobuf 发布 Observation，Core 能发布 retained DeviceView。
- OLED 节点重连后立即渲染最新视图，并在过期后保留内容且标记 stale。
- Node 能上报 `display` / `oled-128x32` / Hardware Variant，Core 能拒绝不兼容的投影。
- 可信客户端能发送一种类型化测试命令，并收到关联的状态和最终结果。
- 重复、过期、畸形、错目标、未授权和不支持的命令均被确定性拒绝且不产生副作用。
- Agent 重启后仍能识别已接受或已完成的命令。
- 固件和 DeviceView 中不存在 Codex、Sub2API 或主机登录凭据。
- 协议兼容、payload 尺寸、Core 投影、Agent 去重和 MQTT 重连测试通过。
- OLED 真机完成显示与断网恢复验收。

## 18. 待决策事项

以下事项不阻塞总体结构，但必须在对应实现开始前形成 ADR：

| 决策 | 默认建议 | 决策时点 |
| --- | --- | --- |
| MQTT broker 与部署位置 | 先用本地 Mosquitto 验证，生产选型另议 | 集成测试前 |
| Core 是否独立进程 | 首版同进程、逻辑分模块 | 初始化 Go 进程时 |
| 固件 Protobuf 实现 | 优先验证 nanopb 的内存与生成链路 | 编写首个 DeviceView 前 |
| CommandStore | 优先嵌入式事务型 KV/SQLite，以原子性和恢复测试定案 | 命令状态机前 |
| 设备凭据发放与轮换 | 每设备证书或独立凭据，禁止共享密钥 | 真机接入前 |
| 首个 TFT Model 与分辨率 | 归入 `display` Series，确定前不进入共享协议假设 | TFT 里程碑前 |
| Codex 隐私策略 | 默认隐藏提示词、正文、路径；标题和仓库名需显式配置 | Codex 接入前 |

## 19. 后续文档

- `docs/protocol.md`：完整消息字段、兼容规则和尺寸预算。
- `docs/mqtt-topics.md`：Topic、QoS、retained、LWT 与 ACL 示例。
- `docs/security.md`：威胁模型、凭据生命周期和本地确认机制。
- `docs/adr/`：记录待决策事项及其取舍。
