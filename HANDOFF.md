# Orbit 实现交接

更新日期：2026-10-07。

App 的原始需求与执行进展见 [App Node 与个人收件箱交接文档](docs/app-node-handoff.md) 开始，内容包括用户目标、当前代码边界、建议技术栈、Core 新增模块、各端能力、同步契约、里程碑和验收步骤。

## 当前代码已经提供

- Go Agent 采集 Sub2API 用量和本地 Codex session 状态，通过 MQTT + Protobuf 上报。
- Go Core 校验 Observation，维护内存中的最新状态，根据显式路由向 Node 投影 DeviceView。
- ESP32 OLED Node 显示用量摘要；Web Node 缓存投影并通过 HTTP/SSE 展示用量和 session。
- Web 可发起短时的打开 Codex session Intent，Core 路由到 Agent 执行受限能力。
- Agent 发布最终 CommandResult；Core/Web 的结果反馈链路尚未完成。

Core 已新增可选 App HTTP/SSE API、独立 SQLite WAL 收件箱和附件目录；Android App 提供缓存、待发送队列、版本冲突、文本/待办/图片和固定状态摘要。MQTT retained View 仍不充当历史数据库。共享工程在 `nodes/app/`；macOS/Windows 的平台发布验证尚未完成。详见 [运行说明](nodes/app/README.md) 和 [API 契约](docs/app-api.md)。

## 下一阶段目标

个人自用，Android 查看端优先，随后支持从任意 App Node 发送文本、待办、图片，在其他 App 查看、复制、完成或修改。用量与 session 信息在固定摘要区更新，不随每次采集刷入消息列表。

用户明确选择首版打开 App 后同步，后台通知后续增加。本次交接沿用不引入 IM、自行实现基础能力的路线；Flutter、SQLite 和 HTTPS/SSE 是建议实施默认值，详细边界与决策状态见专题交接文档。

## 阅读与验证入口

| 入口 | 内容 |
| --- | --- |
| [App 实现交接](docs/app-node-handoff.md) | 原始实现方案及当前验收进展 |
| [README](README.md) | 当前安装、配置、运行、测试与部署命令 |
| [CONTEXT](CONTEXT.md) | Agent、Core、Node、Observation、View、Intent 等领域术语 |
| [设计](docs/design.md) | 当前系统设计、实现边界与验收说明 |
| [MQTT Topic](docs/mqtt-topics.md) | 当前 Topic 与参与者权限矩阵 |
| [安全说明](docs/security.md) | 凭据、隐私、TLS 与部署边界 |
| [ADR](docs/adr/README.md) | 架构决策及演进关系 |

仓库路径以当前机器实际 checkout 为准。接手先核对 `git status`、代码版本和工具链，再按专题文档运行基线检查。实际运行的检查和未完成项记录在专题交接文档末尾；模拟器验证不代表物理真机或生产验收。
