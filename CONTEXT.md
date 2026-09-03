# Orbit

Orbit 将可信主机的状态与显式能力连接到个人设备。本文只定义项目中的规范领域术语。

## Language

**Agent**:
运行在可信主机上的参与者，代表该主机发布观测并执行明确允许的命令。
_Avoid_: Collector, Host Service

**Agent ID**:
代表运行 Agent 的物理机器的规范身份，用于消息路由、授权和命令寻址；系统重装或双系统不改变它，更换主板则形成新身份。
_Avoid_: Host ID, Host Label, Agent Installation ID, MQTT Client ID

**Host Label**:
供人辨认 Agent 所在主机的可变显示名称，不参与路由、授权或唯一性判断。
_Avoid_: Host ID, Agent ID

**Core**:
维护规范状态、应用策略并为设备生成视图的参与者。
_Avoid_: Backend, Cloud

**Core Instance**:
一次正在运行的 Core 部署；V1 只允许一个活动实例。
_Avoid_: Core Node, Node

**Node**:
设备或软件客户端接入 Orbit 网络后承担的运行时角色。
_Avoid_: Device Type, Firmware

**Node Instance**:
拥有唯一 `node_id` 和凭据、实际接入 Orbit 的一个物理或软件 Node。物理 Node 还关联 Device Series、Device Model 和 Hardware Variant。
_Avoid_: Device Instance, Client

**Node Registration**:
Core 持有的 Node Instance 权威记录，将 `node_id` 绑定到允许的产品身份和目标 Agent。
_Avoid_: Node Hello, Self-Reported Identity

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
向 Orbit 提供特定类型 Observation 的命名事实来源。
_Avoid_: Device, Projector

**Host Source**:
由 Agent 在可信主机本地读取的 Source。
_Avoid_: Backend Source, Capability

**Backend Source**:
在后台产生 Observation 并通过 MQTT 发布给 Core 的 Source。
_Avoid_: Host Source, Agent

**Capability**:
Agent 明确注册、可由有效 Command 调用的一种受限动作能力。
_Avoid_: Shell Command, Intent

**Canonical State**:
Core 根据最新有效 Observation 维护的规范领域状态，是 DeviceView 的投影来源而不是公共线协议。
_Avoid_: DeviceView, History

**DeviceView**:
Core 为一个 Node Instance 投影出的设备型号专属视图，包含已经格式化的文本与状态，由该 Node 映射到本地像素。
_Avoid_: Frame, Observation

**Text Line**:
Core 为指定 Device Model 生成的一行最终显示文本及其有限强调语义。
_Avoid_: Metric, Pixel Frame

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
