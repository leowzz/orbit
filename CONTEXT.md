# Orbit

Orbit 将可信主机的状态与显式能力连接到个人设备。本文只定义项目中的规范领域术语。

## Language

**Agent**:
运行在可信主机上的参与者，代表该主机发布观测并执行明确允许的命令。
_Avoid_: Collector, Host Service

**Core**:
维护规范状态、应用策略并为设备生成视图的参与者。
_Avoid_: Backend, Cloud

**Node**:
设备或软件客户端接入 Orbit 网络后承担的运行时角色。
_Avoid_: Device Type, Firmware

**Device Series**:
共享产品定位、交互范式和固件主干的一组物理设备。
_Avoid_: Node Type, Product Name

**Device Model**:
Device Series 内具有固定显示与交互能力的稳定产品定义。
_Avoid_: Board, Variant

**Hardware Variant**:
Device Model 的具体开发板、器件装配或引脚组合，不改变 Model 对外表达的能力。
_Avoid_: Device Model, Device Instance

**Device Instance**:
拥有唯一身份与凭据、实际接入 Orbit 的一台设备。
_Avoid_: Device Model, Client

**Observation**:
Source 在特定时刻观察到的领域事实，不包含设备布局。
_Avoid_: Device State, View

**DeviceView**:
Core 为一个 Device Instance 投影出的紧凑语义状态，由 Node 在本地渲染。
_Avoid_: Frame, Observation

**Intent**:
Node 基于当前 DeviceView 表达的有限用户意图，尚不是发往主机的具体命令。
_Avoid_: Command, Event

**Command**:
发往指定 Agent、具有明确类型和有效期的能力调用。
_Avoid_: Shell Command, Intent
