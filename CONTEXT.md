# Orbit

Orbit 将可信主机的状态与显式能力连接到个人设备。本文只定义项目中的规范领域术语。

## Language

**Agent**:
运行在可信主机上的 Orbit 实例，通常一台机器只运行一个；它可以承载多个 Source 和 Capability。
_Avoid_: Collector, Host Service

**Agent ID**:
Agent 实例的规范身份，用于消息路由、授权和命令寻址；它可以由部署显式指定，也可以从所在机器的稳定信息生成默认值。
_Avoid_: Machine ID, Host ID, Host Label, MQTT Client ID

**Agent Epoch**:
一次 Agent 进程生命周期的随机身份，用于区分同一 Agent ID 重启前后的 Observation 修订序列。
_Avoid_: Agent ID, MQTT Session

**Agent State**:
Agent 对当前 Epoch、版本、Host Label、可用功能及各 Source 状态的 retained 自描述，不包含领域观测值。
_Avoid_: Observation, Presence

**Host Label**:
供人辨认 Agent 所在主机的可变显示名称，不参与路由、授权或唯一性判断。
_Avoid_: Host ID, Agent ID

**Core**:
维护规范状态、应用策略并为设备生成视图的参与者。
_Avoid_: Backend, Cloud

**Core Instance**:
一次正在运行的 Core 部署；V1 只允许一个活动实例。
_Avoid_: Core Node, Node

**Core Epoch**:
一次 Core 进程生命周期的随机身份，用于区分同一 Core ID 重启前后的 DeviceView 修订序列。
_Avoid_: Core ID, MQTT Session

**Core State**:
Core 对当前 Epoch 和版本的 retained 自描述，不包含 Canonical State 或 DeviceView。
_Avoid_: Canonical State, DeviceView, Presence

**Orbit Network**:
共享一个专用 MQTT Broker 部署和 ACL 边界的一组 Core、Agent 与 Node；V1 不在协议中增加租户或网络标识。
_Avoid_: Tenant, MQTT Topic Prefix

**Node**:
设备或软件客户端接入 Orbit 网络后承担的运行时角色。
_Avoid_: Device Type, Firmware

**Node Instance**:
拥有唯一 `node_id` 和凭据、实际接入 Orbit 的一个物理或软件 Node。物理 Node 还关联 Device Series、Device Model 和 Hardware Variant。
_Avoid_: Device Instance, Client

**Node Epoch**:
一次 Node 进程或固件启动周期的随机身份，用于区分同一 Node ID 重启前后的 Node State 修订序列。
_Avoid_: Node ID, MQTT Session

**Node State**:
Node 对当前 Epoch、产品身份、固件版本和唯一目标 Agent 的 retained 自描述，是 Core 通过 MQTT 发现 Node 的依据。
_Avoid_: Node Registration, Hello

**Device Series**:
共享产品定位、交互范式和固件主干的一组物理设备。
_Avoid_: Node Type, Product Name

**Device Model**:
Device Series 内具有固定显示与交互能力的稳定产品定义。
_Avoid_: Board, Variant

**Hardware Variant**:
Device Model 的具体开发板、器件装配或引脚组合，不改变 Model 对外表达的能力。
_Avoid_: Device Model, Device Instance

**Observation**:
Source 在特定时刻观察到的领域事实，不包含设备布局。
_Avoid_: Device State, View

**Source**:
Agent 承载的一类数据获取功能，向 Orbit 提供一种 Observation；它不是独立的网络参与者或 Command Capability。
_Avoid_: Backend Source, MQTT Producer, Capability

**Source Status**:
Agent State 中描述某个 Source 是否启用、当前是否健康及最近成功时间的状态，不是独立 Observation。
_Avoid_: SourceHealth Observation, Presence

**Capability**:
Agent 明确注册、可由有效 Command 调用的一种受限动作能力。
_Avoid_: Shell Command, Intent

**Canonical State**:
Core 根据最新有效 Observation 维护的规范领域状态，是 DeviceView 的投影来源而不是公共线协议。
_Avoid_: DeviceView, History

**DeviceView**:
Core 为一个 Node Instance 投影出的设备型号专属视图，包含已经格式化的文本与状态，由该 Node 映射到本地像素。
_Avoid_: Frame, Observation

**Display Slot**:
DeviceView 中由 Device Model 定义位置的一块最终显示内容；OLED 128x32 固定为 Primary、Secondary 和 Footer 三个槽位。
_Avoid_: Arbitrary Line, Metric, Pixel Frame

**Intent**:
Node 基于当前 DeviceView 表达的有限用户意图，尚不是发往主机的具体命令。
_Avoid_: Command, Event

**Command**:
发往指定 Agent、具有明确类型和有效期的能力调用。
_Avoid_: Shell Command, Intent

**Command Result**:
一次短生命周期 Command 的最终执行状态；V1 不保证跨进程重启恢复或交付。
_Avoid_: Intent Result, DeviceView

**Command Feedback**:
Core 从 Command Result 裁剪并投影给原始 Intent 发起者的安全状态。
_Avoid_: Command Result, Raw Result

**Presence**:
参与者通过 retained Last Will 表达的当前连接存活状态，不代表其业务数据仍然新鲜。
_Avoid_: Freshness, Health
