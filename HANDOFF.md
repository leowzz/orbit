# Orbit 实现交接

更新日期：2026-10-07。

下一阶段从 [App Node 与个人收件箱交接文档](docs/app-node-handoff.md) 开始，内容包括用户目标、当前代码边界、建议技术栈、Core 新增模块、各端能力、同步契约、里程碑和验收步骤。

## 当前代码已经提供

- Go Agent 采集 Sub2API 用量和本地 Codex session 状态，通过 MQTT + Protobuf 上报。
- Go Core 校验 Observation，维护内存中的最新状态，根据显式路由向 Node 投影 DeviceView。
- ESP32 OLED Node 显示用量摘要；Web Node 缓存投影并通过 HTTP/SSE 展示用量和 session。
- Web 可发起短时的打开 Codex session Intent，Core 路由到 Agent 执行受限能力。
- Agent 发布最终 CommandResult；Core/Web 的结果反馈链路尚未完成。

当前 Core 不持久保存用户消息，MQTT retained View 不充当历史数据库。Android/macOS/Windows App、消息编辑和完成、附件上传、可靠离线补同步均尚未实现。

## 下一阶段目标

个人自用，Android 查看端优先，随后支持从任意 App Node 发送文本、待办、图片，在其他 App 查看、复制、完成或修改。用量与 session 信息在固定摘要区更新，不随每次采集刷入消息列表。

用户明确选择首版打开 App 后同步，后台通知后续增加。本次交接沿用不引入 IM、自行实现基础能力的路线；Flutter、SQLite 和 HTTPS/SSE 是建议实施默认值，详细边界与决策状态见专题交接文档。

## 阅读与验证入口

| 入口 | 内容 |
| --- | --- |
| [App 实现交接](docs/app-node-handoff.md) | 下一阶段完整实现说明；所有新能力明确标记为规划 |
| [README](README.md) | 当前安装、配置、运行、测试与部署命令 |
| [CONTEXT](CONTEXT.md) | Agent、Core、Node、Observation、View、Intent 等领域术语 |
| [设计](docs/design.md) | 当前系统设计、实现边界与验收说明 |
| [MQTT Topic](docs/mqtt-topics.md) | 当前 Topic 与参与者权限矩阵 |
| [安全说明](docs/security.md) | 凭据、隐私、TLS 与部署边界 |
| [ADR](docs/adr/README.md) | 架构决策及演进关系 |

仓库路径以当前机器实际 checkout 为准。接手先核对 `git status`、代码版本和工具链，再按专题文档运行基线检查。本次提交只交接方案，不代表完成了功能开发、真机测试或生产验收。
