# 小智彩屏接入 ORBIT

2026-10-27 已在 Windows USB 真机与 tx Core 之间完成接入。

- 型号：`display/xiaozhi-320x240/ma-xin-wifi`，ESP32-S3 N16R8、ST7789。
- Node ID：`xiaozhi-desk-01`，Core 名称“小智彩屏”。
- Core 管理设备数据来源、API 用量 / 账号周限 / 循环显示，SQLite 持久化。
- 已配置 `macos_agent` 的 usage/codex 两类数据，5 秒轮播。
- 设备使用独立 Node 范围 MQTT 账号、TLS 验证和 Protobuf；账号可在 Core 撤销。
- [固件构建、烧录、更换 Wi-Fi](../nodes/display/models/xiaozhi-320x240/variants/ma-xin-wifi/README.md)。

已验证：

- 小智与原有 OLED 均成功构建；小智生成 `firmware.bin`、`factory.bin` 和 SHA-256 清单。
- COM11 刷写成功并通过设备 Flash 哈希校验。
- 真机 TLS 连接、NodeState 注册、Core 发现型号、独立 MQTT 客户端在线。
- 串口持续收到交替的 API 用量与 Codex 周额度，未出现解码或型号拒绝。
- 用户已确认真机 ORBIT 页面显示正常，API 用量与周限每 5 秒切换正常。
- 后续按用户要求去掉 ORBIT、API / USAGE、CODEX 页面标题与常驻底部说明；
  费用和百分比居中自适应放大，次要指标放在下方，连接正常时仅保留状态点。
- 用户从 HTML 对比页选择 **04 · JetBrains Mono · 细体 300**：主数字使用 300，
  辅助文字使用预览中的 400 字重。固件采用 16 级字形覆盖、RGB565 混色与双线性缩放，
  不再放大默认 5×7 点阵。实际 C++ 渲染结果已检查，应用镜像和整机镜像均构建通过。
  `0.1.1-jbm300` 已经 COM11 刷入并通过 Flash 哈希校验；Core 已识别新版本，
  串口验证周限和费用数据持续交替接收。
- 最终选择更新为 **01 · 居中均衡 · 字重 500**，辅助文字保持 400。
  `0.1.2-jbm500` 已刷入并通过哈希校验，Core 已识别版本且设备持续接收两类数据。
  同时将小智本地库切换为 `symlink://`，避免 PlatformIO 复用旧字形副本。
- 按后续要求收紧 API 费用的字间距：按实际字形边缘排列货币符号、数字和小数点，
  消除点号所在等宽单元的大块空白，主数字仍为 JetBrains Mono 500、居中布局。
  `0.1.3-compact` 已刷入并通过哈希校验，Core 识别和两类数据接收正常。
- 费用显示进一步改为一位小数，例如 `$20.2`。Core 从原始微美元直接四舍五入，
  已覆盖边界和进位测试。tx 已更新，串口确认设备收到 `$22.0`；本次无需刷机。
- 在保留费用一位小数的基础上，将费用字形间隙从字体单元宽度的 9% 调整至 15%，
  适当放宽紧凑排版。`0.1.4-spacing` 已刷入并通过哈希校验，Core 识别新版本，
  串口确认收到 `$23.3` 与周限数据。
- 周限底部改为按信息类别分组：左侧 `RESET` 显示重置日期和时间，右下角
  `REMAINING` 显示剩余天数和小时（如 `5d 15h`）。异常提示移到顶部避免覆盖数据。
  `0.1.5-weekly` 已刷入并通过哈希校验，Core 识别版本且两类数据接收正常。
- 修正周限左右分组的垂直错位：`RESET` 与 `REMAINING` 标题同高，重置时间与
  剩余天数/小时也同高，左右分别对齐屏幕边缘。实际 C++ 渲染已检查；
  `0.1.6-aligned` 已刷入并通过哈希校验，Core 识别版本且两类数据接收正常。
- Core/config/integration/mqttadmin 测试、相关 `go vet` 通过。
- 控制台生产构建与 14 项现有测试通过。
- 17 项配置测试和 10 项共享固件原生测试通过；修复 Windows 下 protobuf 路径
  分隔符和 Unity 缺失 setUp/tearDown 的构建兼容问题。

全量 `go test ./...` 仍在既有 `internal/sources/codex` 测试失败：Windows 绝对路径
被拼为错误 SQLite URI（`invalid uri authority`）。该 Source 文件没有在本次修改，
tx 的 Linux Core 与现有 macOS Agent 数据采集正常。

部署保留了设备全 Flash 备份和 tx 的原镜像、Compose、SQLite 在线备份。
本机临时 Wi-Fi/MQTT 配置、个性化固件和备份均在 Git 忽略路径，不能作为公开发布资产。
此固件当前专用于指标显示，未启用原仓库的语音/音频回放。
