# App API 运行与备份

Core 启用 app 后仍只需一个进程。`app.data_dir` 相对 Core YAML 文件目录解析，内部保存
inbox.sqlite（WAL/SHM）与 attachments/。不要把可写数据放进只读配置挂载。

`deploy/docker-compose.yml` 已为 Core 增加 `orbit-core-data:/app/data` 和仅绑定回环的
7622 端口；配置可用 `data_dir: ../data/app`、`listen: 0.0.0.0:7622`。
镜像准备的 /app/data 归属 uid/gid 65532；若改用宿主机绑定目录，需赋予该 UID 写权限。
未启用 app 配置时不监听 API，不改变既有 MQTT Node 行为。

## HTTPS

示例 Caddy 配置（替换域名，并按现有部署配置 DNS）：

```caddyfile
orbit.example.com {
    reverse_proxy 127.0.0.1:7622 {
        flush_interval -1
    }
}
```

HTTPS 终止于反向代理，不直接暴露 Core 明文端口。代理必须转发 Authorization，禁用
SSE 缓冲，并允许长连接；上传请求上限至少 10 MiB。不要记录 Authorization 或请求正文。

## 备份与恢复

首版使用停机备份：停止 Core，**整体备份 app.data_dir**（数据库、WAL/SHM、附件），
同时通过私有渠道保存设备配置。不要只复制仍在写入的 inbox.sqlite。

恢复：停止 Core，整体还原同一批备份，确认文件权限，执行：

```sh
go run ./cmd/orbit-core -config configs/core.local.yaml -reset-sync-generation
```

命令修改数据集 generation 后退出，不连接 Broker。随后启动 Core。App 下次前台同步
将重建权威缓存，待发送队列和本地草稿仍保留；过旧的编辑可能需要处理版本冲突。
正常重启不运行此命令。不要在运行中的 Core 上修改数据库或 generation。

设备登记、撤销和令牌替换通过管理台即时完成；摘要路由在转发规则页调整。每个物理 App 安装使用
独立 node_id/令牌；一个人所有已授权 App 共享条目。暂不提供多用户权限隔离。

## 与管理台共存

Core 管理台继续使用 `console.database`，App 使用 `app.data_dir`，二者放在同一持久卷的不同路径。
必须设置 `console.password`；App 的设备令牌保持独立。状态摘要路由通过管理台新增或编辑，
选择 `App · 收件箱`（`overview-app`），设备 ID 与 `app.devices` 的键一致。
YAML 路由仅在首次创建管理台数据库时导入；已有部署修改 YAML 不会覆盖已保存路由。

## 设备登记升级

管理台新增「App 与收件箱」，支持设备令牌管理和文本、待办、图片操作。首次启动会将 YAML 中的设备导入 `inbox.sqlite`；之后 YAML 不再覆盖设备登记。令牌明文仅在创建或更换时返回，备份数据库即可保留授权状态。

收件箱数据库版本升级为 2，旧版 Core 会拒绝打开，以防回退后重新接受 YAML 中已经撤销的旧令牌。回滚二进制需同步恢复升级前的完整数据备份；恢复旧快照后的 App 同步仍按本文的 generation 重置流程处理。不要直接降低数据库版本号。

## 仅使用消息

`configs/core.inbox.example.yaml` 可省略 MQTT。此模式启动管理台和 App API，状态转发规则必须为空；已有规则时会拒绝启动并说明需要保留 MQTT 配置。App 和网页的消息不需要设备投影路由。

管理台同域提供个人 API 和网页，但仍分别校验管理员会话与设备凭据。最少配置、Web Node 可选连接和浏览器缓存边界见 [个人收件箱](personal-inbox.md)。

## 2026-10-15：统一展示链路部署

- tx 的 Core/Web 更新为本机构建镜像 `orbit-core:unified-display-20261015`、`orbit-web:unified-display-20261015`，未推送镜像仓库。
- 发布产物、源码差异、二进制哈希和验证记录：`/srv/orbit-deploy/releases/unified-display-20261015`。基线提交 `d24b4239a4f333ade5261a0244463abbbb7336c8` 加本次工作区改动。
- 停机备份：`/srv/orbit-deploy/backups/unified-display-20261015T030750Z`，含 Compose、配置和完整 Core 数据。两个 SQLite 数据库完整性均为 `ok`。
- 部署前后路由修订保持 94，7 条路由与 4 个设备授权不变；Core/Web HTTPS、原设备令牌和 protobuf 状态接口验证通过，运行二进制哈希与构建产物一致。
- 此本地镜像使用 `docker compose up -d --pull never orbit-core orbit-web`，不要执行远程拉取。
- 137（`10.10.16.137:5555`，Alipay Box）覆盖安装 ARM64 profile APK，安装后的 versionCode 为 4002，签名与原调试包一致；首次安装时间不变，原连接与应用数据保留。应用启动和共享状态接收正常。
- 部署后设备验收发现 AP001 的 App 规则误选 OLED profile，仅将该规则改为 `overview-app` + `display_mode: usage`，保留原来源；保存为修订 116。修正前单条规则备份在发布目录的 `ap001-route-before.json`，未覆盖其他并发编辑。

## 2026-10-15：Web 账号周限四行展示

- tx Web 更新为本机构建镜像 `orbit-web:weekly-four-rows-20261015`，左侧依次显示剩余额度、重置日期、重置时间、剩余时间（如 `6天23时`）。Core 未重启。
- 产物、源码差异和验证记录：`/srv/orbit-deploy/releases/web-weekly-four-rows-20261015`；Compose 备份：`/srv/orbit-deploy/backups/web-weekly-four-rows-20261015T031828Z`，旧 Web 镜像保留。
- 运行二进制及公网 JavaScript 哈希与构建产物一致；认证后的状态接口返回 200，当前 `display_kind` 为 `codex`。镜像未推送仓库，重建继续使用 `--pull never`。

### 2026-10-15 Codex 会话转发

新增独立展示模式 `sessions`，来源仍为 `codex`。选择会话仅投影会话信息，选择周限仅投影额度，旧综合模式保持兼容；循环显示仍轮换用量与周限。Core、Web 发布镜像为 `orbit-core:sessions-final-20261015`、`orbit-web:sessions-final-20261015`，构建清单、源码快照及部署结果位于 tx `/srv/orbit-deploy/releases/sessions-20261015`。每次更新前备份数据库、配置和 Compose，部署校验二进制摘要、数据库完整性、路由与设备注册不变及 HTTPS / Protobuf 接口。

137（`10.10.16.137:5555`）已覆盖安装 ARM64 profile APK（versionCode 4003），首装时间及连接保留。临时切换 AP001 到 `sessions` 后，App 状态页与点阵屏均显示实时会话计数（验证时 1 运行中 / 1963 总数）；验证后恢复原来的 `codex` 周限模式。验证记录见发布目录 `android-verification.json`。Go 全量、Web 14 项、Flutter 48 项、Flutter analyze、Android JVM 测试与 ARM64 构建均通过。

### 2026-10-15 Session 信息改为独立开关（替代上节的独立指标页）

按用户澄清，数据展示仅保留 API 用量、账号周限、循环显示。转发规则新增独立的 Session 信息列，通过 `sessions_enabled` 控制会话详情，开启后与任何指标同时转发；关闭后不携带详情并清空接收端缓存。点阵屏继续只显示指标。旧 `display_mode=sessions` 兼容解释为周限指标及开启会话详情，前端不再提供该模式。

已部署 `orbit-core:session-toggle-20261015`、`orbit-web:session-toggle-20261015` 至 tx，发布记录 `/srv/orbit-deploy/releases/session-toggle-20261015`，部署前备份 `/srv/orbit-deploy/backups/session-toggle-20261015T032921Z`；数据库完整性、路由及设备注册保留、二进制摘要和 HTTPS / Protobuf 验证通过。137 覆盖安装 ARM64 profile versionCode 4004，原连接及数据保留。Go 全量、Web 15 项、Flutter 47 项、Flutter analyze、Android JVM 测试和构建通过。实测 Web 用量及周限均可同时显示 20 条会话，关闭开关后会话行归零而指标保留；控制台桌面及 390px 窄屏无横向溢出。

### 2026-10-15 工作台会话打开接口部署

工作台已能同步状态但点击会话失败的原因是 Core 仍运行 `session-toggle-20261015`，
`POST /api/v1/sessions/{id}/open` 返回 404。更新 Core 为 `orbit-core:workbench-open-20261015`；
Web 未更新。发布记录 `/srv/orbit-deploy/releases/workbench-open-20261015`，
停机备份 `/srv/orbit-deploy/backups/workbench-open-20261015T042546Z`。
两份 SQLite 完整性检查通过，3 条路由（修订 149）、4 个设备授权保持不变；
运行二进制摘要、HTTPS、原设备鉴权和 Protobuf 状态接口验证通过。

137 使用 `adb install -r` 覆盖安装 ARM64 profile versionCode 4010，
首次安装时间 `2026-10-15 12:10:41` 不变，连接与本机数据保留。
App 对缺失接口的 404/405 改为明确提示更新 Core。工作台测试 7 项、Flutter analyze 及
相关 Go 测试通过。真机点击工作台会话，Core 向 `macos_agent` 发出
`357316af0b3c83d57e980fb6b9b18d0e` 指令，Agent 返回 `COMMAND_STATUS_SUCCEEDED`，
执行耗时 79 ms。详情见发布目录中的 `mac-verification.json` 和 `android-verification.json`。

后续覆盖升级至 versionCode 4012：成功打开会话时不再弹提示，仅保留异常提示；
工作台剩余时间使用 `4d23h` 格式。工作台 7 项测试通过，首次安装时间保持不变。

### 2026-10-15：本机切换到 Tauri Agent

- 新 Agent 安装于 `~/Applications/Orbit Agent.app`，复用 `macos_agent` 标识及原服务配置；凭据保存到系统钥匙串。旧 `com.leo.orbit.agent` 与 legacy dev 服务已停止并禁用登录启动，原二进制和配置保留供回退。
- 新登录启动项为 `~/Library/LaunchAgents/Orbit Agent.plist`，启动已安装应用并带 `--background`。退出后通过该启动项重新启动，配置、凭据、连接和采集均恢复。
- 修复 YAML 导入将 `~/.codex` 错当成配置文件相对路径的问题；配置回归测试 4 项及格式检查通过，macOS 应用重新构建安装。
- 137 工作台显示实时连接、20 条会话和账号周限；Agent 上报 1967 个会话、1 个运行中，Codex 与 Sub2API 均健康。标题点击进入全屏、左上角标题退出全屏均已验证。
- 普通模式与重启后的全屏模式各点击一次「同步 app node 布局与交互」，指令 `af76bc3767e64b82092cbf9398d3a8ac`、`6e7850b32524146f5803786a2af95e4d` 均由新 Agent 返回 `COMMAND_STATUS_SUCCEEDED`。用户已确认 Mac 上打开了对应 Codex 会话，工作台点击到本机打开的完整链路验证通过。

### 2026-10-15：Agent 管理工作台快捷导航

- 最终分工为 Agent 管理应用、网址、图标、顺序和标题显示；App 工作台仅展示与点击。Core 按现有工作台路由自动投影来源 Agent 的导航，不再接受 App 写导航配置。
- tx Core 更新为 `orbit-core:agent-navigation-20261015`，Web 未更新。发布记录 `/srv/orbit-deploy/releases/agent-navigation-20261015`；停机备份 `/srv/orbit-deploy/backups/agent-navigation-20261015T063919Z`。两份数据库完整性为 `ok`，3 条路由（修订 151）和 4 个设备授权保留，二进制摘要、HTTPS、原授权及状态接口通过。
- 137 覆盖安装 ARM64 profile APK versionCode 4014，首次安装时间仍为 `2026-10-15 12:10:41`。普通与全屏底栏均显示居中的大图标、默认隐藏标题，没有配置入口；Agent 新增和排序后自动同步，工作台数据恢复实时更新。
- 在普通模式点击网址与 Microsoft Edge，Agent 分别回报指令 `80b94e72-8cf2-4a78-a837-30dde109e22c`、`8a8a0ca1-1ec1-4b85-aa49-130ecdfea7ce` 为 `COMMAND_STATUS_SUCCEEDED`；全屏点击亦成功。验证未修改用户现有入口。
- 已安装最新 Mac Agent，保留原凭据和入口；修复后台运行时重新打开应用不能显示设置窗口的问题。Go 全量及 API/Core race、Rust 22 项测试和 Clippy、Flutter analyze 与工作台/导航 10 项测试、Protobuf lint 通过。Windows 的应用选择、图标提取和打开仍待真机验收。


## NFCD 工作台边距配置（2026-10-15）

- Mac Agent「快捷导航」新增「NFCD 指标左侧留白」，范围 0–64，默认 16，保存并应用后随快捷导航目录下发。设置字段 `nfcdMetricsLeftPadding`，目录和导航 JSON 字段 `nfcd_metrics_left_padding`。
- Core「转发规则」的 App 规则可开启「单独设置 NFCD 指标左侧留白」。规则字段 `nfcd_metrics_left_padding` 投影到 DeviceView 的同名 Protobuf 字段（JSON 为 `nfcdMetricsLeftPadding`）；关闭后恢复跟随 Agent。显式 0 与未设置分别保留。
- App 只在原生型号为 `NFCD_AlipayBoxN5DEL` 且使用横屏紧凑工作台时应用：Core 设备规则优先，随后使用 Agent 默认值，最后回退到 16。多个 Agent 来源按规则输入顺序取第一个已配置值。其他型号不受该设置影响。
- tx Core 镜像 `orbit-core:nfcd-layout-20261015`；发布目录 `/srv/orbit-deploy/releases/nfcd-layout-20261015`；停机备份 `/srv/orbit-deploy/backups/nfcd-layout-20261015T085159Z`。部署保留 4 条规则、4 个设备授权，两份数据库完整性、二进制摘要、HTTPS 与原令牌验证通过。
- Mac Agent 已更新，137 覆盖安装 build 4022。实测 Core 设为 0 后立即生效，取消覆盖后恢复 Agent 当前值 24；测试后 Core 保持跟随 Agent（规则修订 156）。Go 全量、Flutter 92 项、Agent Rust 31 项及 Clippy、前端构建和 Protobuf lint 通过。

## 2026-10-15：Core 设备管理页简化

- 优化提交 `2f3d5e1`，合并至 `opt/struct` 的提交为 `e228314`。设备详情直接选择显示内容和来源，移除旧规则抽屉与手动预配置表单；快捷来源点选添加，自定义入口及排序按需展开，支持撤销修改和冲突后保留草稿。
- tx Core 更新为本机构建镜像 `orbit-core:core-console-e228314-20261015`，二进制构建自合并提交 `e228314`；Web 容器未更换或重启。源码快照、构建清单、部署脚本和验证记录位于 `/srv/orbit-deploy/releases/core-console-e228314-20261015`。
- 停机备份位于 `/srv/orbit-deploy/backups/core-console-20261015T111129Z`，包含 Compose、配置和完整 Core 数据。路由修订 157、4 条路由、4 个设备授权、快捷设置、收件箱内容及附件均保留，两份 SQLite 完整性检查通过。
- 运行二进制、HTTPS 页面引用及 JS/CSS 哈希与构建产物一致；原设备令牌的 JSON/Protobuf 状态和快捷导航接口通过。Core 正常运行，MQTT 连接和订阅正常。
- 更新前的 `orbit-core:device-pairings-20261015-174547` 镜像保留。当前镜像未推送仓库，重建使用 `docker compose up -d --no-deps --pull never orbit-core`。
- 合并后前端构建、格式检查和相关 Core/App API/嵌入资源 Go 测试通过；桌面及 390px 窄屏交互使用本地模拟数据验证，未增加本次真机操作验收。

### Core 来源状态提示修正

- 修复 `a7b76d9`：Core 的 Agent State 使用 Protobuf JSON 枚举（`OBSERVATION_TYPE_USAGE` / `OBSERVATION_TYPE_CODEX`），前端曾错误地与 `usage` / `codex` 比较，导致正常来源显示未启用，且无法自动推荐。现在统一按协议字段识别，区分明确关闭与尚未报告采集状态。
- tx 更新为 `orbit-core:source-status-a7b76d9-20261015`，发布及验证记录位于 `/srv/orbit-deploy/releases/source-status-a7b76d9-20261015`，停机备份位于 `/srv/orbit-deploy/backups/source-status-20261015T111406Z`。路由修订 158、4 条路由、4 个设备授权和既有数据保留，数据库、线上资源哈希及设备状态/导航接口检查通过，Web 容器未变。
- 新增基于实际 Protobuf JSON 格式的 3 项回归测试，前端构建和格式检查通过。

## 2026-10-15：设备改名

- 功能提交 `3c068e6`：设备详情标题旁增加「改名」。App 复用登记中的 `label`，MQTT 名称存入 Core 的 `node_labels` 表，与自描述及路由独立；名称用于设备列表、详情、搜索、总览及数据流向，设备 ID、令牌和撤销状态不变。已有数据库启动时补建名称表。
- tx Core 镜像更新为 `orbit-core:device-names-3c068e6-20261015`，发布记录及源码快照位于 `/srv/orbit-deploy/releases/device-names-3c068e6-20261015`，停机备份位于 `/srv/orbit-deploy/backups/device-names-20261015T112115Z`。路由修订 164、4 条路由、4 个设备授权、快捷设置及既有数据保留，Web 容器未变。
- 数据库完整性、线上 JS/CSS 和二进制摘要、原设备鉴权及状态/导航接口通过；相关 Go 测试及 race、前端构建/测试/格式检查通过。本地隔离环境验证 App/MQTT 改名、取消、刷新、进程重启持久化、列表搜索、总览与数据流向名称及 390px 窄屏，未改动用户在线设备的名称。

## 2026-10-15：设备归档与恢复

- 功能提交 `a1a3b87`：设备详情标题旁增加归档/恢复操作，「已归档」入口支持查看、搜索及恢复。归档设备从默认列表、总览和数据流向隐藏，数据配置、名称、同步和访问权限保留；状态按稳定设备 ID 存入 Core `node_archives` 表，重启、自描述刷新或暂时未发现设备均不会丢失。
- tx Core 更新为 `orbit-core:device-archive-a1a3b87-20261015`。发布与验证记录位于 `/srv/orbit-deploy/releases/device-archive-a1a3b87-20261015`，停机备份位于 `/srv/orbit-deploy/backups/device-archive-20261015T113124Z`。路由修订 164、4 条路由、4 个设备授权、已有名称及快捷设置保留，数据库完整性、线上资源和运行二进制摘要、设备状态与导航接口检查通过，Web 容器未变。
- Go 相关测试及 race、前端测试、构建和格式检查通过。本地隔离服务验证 MQTT/App 归档和恢复、默认页面隐藏、刷新与后端重启持久化、原令牌继续可用、暂未发现记录恢复，以及 390px 窄屏入口和按钮对齐。未代用户选择或归档在线设备。

## 2026-10-15：Web 状态与收件箱统一设备

- 功能提交 `c0d6d9e`：配置 `web.inbox` 后，Web 的状态、会话打开及收件箱共用令牌对应的 HTTP 设备身份，不再连接 MQTT 或发布另一份 NodeState。首页继续显示状态，`/inbox/` 继续使用原访问密码与服务器端令牌；没有 HTTP 配置的旧 MQTT 节点继续兼容。`6e62920` 补充 HTTP 最近访问状态，区分持续连接、最近活跃和未连接。
- tx 保留设备 ID `app-de96c414-5400-40a3-81f2-5eca731d1486`，沿用原状态设备名称「web-node 线上」。将 `node_web_overview_tx` 的数据来源、显示模式、会话开关、轮播间隔和快捷设置迁入该设备，路由 profile 转为 `overview-app`，修订升至 168；4 条路由、4 个设备授权保留，令牌摘要、撤销状态、收件箱历史与附件不变。
- 迁移前停止 Core/Web 并备份，在数据库副本上演练，核对仅预期配置表变化。迁移同时转移旧 `navigation_configs` 与 `navigation_settings`，防止启动时从旧配置重新生成旧设备设置；清理旧 MQTT retained state/view，移除 Web 配置中的旧 MQTT 连接。旧设备通过身份迁移退出列表，没有用归档隐藏重复记录。
- Web 镜像为 `orbit-web:unify-web-c0d6d9e-20261015`；Core 最终镜像为 `orbit-core:web-activity-6e62920-20261015`。迁移脚本、清理工具源码、构建清单与验证记录位于 `/srv/orbit-deploy/releases/unify-web-c0d6d9e-20261015`，迁移前备份为 `/srv/orbit-deploy/backups/unify-web-20261015T114828Z`；连接提示更新位于 `/srv/orbit-deploy/releases/web-activity-6e62920-20261015`，对应备份为 `/srv/orbit-deploy/backups/web-activity-20261015T115021Z`。
- Web/配置/启动相关 Go 测试、Web/Core/App API race、Web 15 项脚本测试、管理台 5 项测试、构建与格式检查通过。线上数据库完整性、运行二进制和公开资源摘要、原令牌 JSON/Protobuf 状态与导航接口均通过；浏览器确认 Web 分类只有一张设备卡，旧 ID 无入口，状态页与收件箱身份一致，消息正常同步，原循环显示与快捷来源保留，390px 窄屏无横向溢出。未触发真实会话打开操作。

## 2026-10-15：设备卡片配置摘要

- 提交 `5594b16`：设备列表卡片用实际显示内容、数据来源及 Codex 会话开关替换「已设置数据内容」。循环显示补充切换间隔；相同来源合并，不同来源分别标注用量、周限或会话来源，名称未知时保留来源 ID。兼容旧显示配置，OLED 不显示不支持的会话开关，未配置设备提供简短说明。卡片高度保持紧凑，同排摘要和管理入口对齐，长名称自然换行。
- tx Core 更新为 `orbit-core:card-summary-5594b16-20261015`，发布记录位于 `/srv/orbit-deploy/releases/card-summary-5594b16-20261015`，备份位于 `/srv/orbit-deploy/backups/card-summary-20261015T115604Z`。路由修订 168、4 条路由与 4 个设备授权保留，数据库、运行二进制及 HTTPS 资源摘要、原设备状态和导航接口验证通过，Web 容器未变。
- 前端构建、5 项已有回归测试、格式检查、嵌入资源 Go 测试与差异检查通过。本地以线上配置快照验证不同来源、未知来源、长名称、旧会话模式及 OLED 默认轮播；线上浏览器确认五张卡片摘要与配置一致，同排摘要顶沿和底部入口坐标一致，390px 窄屏无横向溢出。本次仅增加只读摘要，没有修改设备配置。

## 2026-10-15：设备状态颜色与活跃时间

- 提交 `1bef3ce`：设备列表和「连接与访问」共用状态标签。已连接为浅绿、最近活跃为浅青、已发现为浅蓝、未连接为浅灰、已归档为浅紫、已撤销访问为浅红、预配置为浅黄，文字和圆点保留，支持颜色之外的状态识别。
- 悬停或键盘聚焦显示完整年月日与时分秒。App 使用现有 `activity.last_seen`，无记录时明确提示「本次服务运行尚无访问记录」；MQTT 使用自描述的报告时间，明确标注「最近设备报告」及不代表当前在线，不将 retained 记录当作实时活跃。本次不改变既有访问记录的保存方式。
- tx Core 更新为 `orbit-core:device-status-1bef3ce-20261015`，发布目录 `/srv/orbit-deploy/releases/device-status-1bef3ce-20261015`，备份 `/srv/orbit-deploy/backups/device-status-20261015T120711Z`。路由修订 168、4 条路由及 4 个设备授权保留，数据库、二进制与公开资源摘要、原令牌状态及导航接口检查通过，Web 容器未变。
- 前端构建、5 项既有测试、格式检查、嵌入资源 Go 测试与差异检查通过；本地验证全部七种状态、归档优先级、已知与缺失时间、悬停、键盘聚焦及连接详情中的共用样式。线上确认颜色和实际时间显示正确，390px 下提示框完整位于视口内，无横向溢出；未改动设备配置。

### 2026-10-16 — Web Node 仅保留收件箱并加固登录

- 首页直接进入 `/inbox/`，Web 进程不再连接 MQTT 或轮询状态；关闭状态面板、旧静态资源和 Codex 会话操作入口。身份接口只返回设备 ID 与名称，不再返回状态投影。
- 登录预算为整个实例每分钟 5 次，返回 429 与 `Retry-After`；随机 256 位会话取代密码派生签名。退出立即撤销会话并结束同步连接，过期连接同样结束。Cookie 使用 HttpOnly、Secure、SameSite=Strict，并补充同源检查、防嵌入策略和 HTTPS HSTS。
- 按用户选择保留原来的 8 位访问密码和 `480h` 会话配置；配置文件从 `0755` 收紧至 `0640`（root:65532）。新会话保存在进程内，服务重启后需要重新登录；已有消息、浏览器草稿和设备授权无需重建。
- tx 仅更新 Web 为 `orbit-web:web-inbox-20261016T072232Z`；Core 容器 ID 未变。构建源码与校验记录：`/srv/orbit-deploy/releases/web-inbox-20261016T072232Z`；Compose 和 Web 配置备份：`/srv/orbit-deploy/backups/web-inbox-20261016T072232Z`。原镜像保留，可还原备份 Compose 后仅重建 Web。
- Go 全量测试、Web/App API race 检查、Go vet、15 项 JS 测试通过。真实浏览器验证 1440px 与 390px 布局、发送后刷新保留及搜索；公网确认 401/403/429、退出后旧 Cookie 失效、旧接口 404、HTTPS Cookie 与运行二进制 SHA-256。Go 1.27.2 下 `govulncheck ./cmd/orbit-web` 为 0 个可达漏洞；仍有 15 项仅位于所需模块中的未调用公告。线上旧二进制本来已是 Go 1.27.2；本次显式固定本地与 Docker 工具链，未将本地旧工具链扫描结果当作线上漏洞。
- 安全判断：原实现存在不限次猜密码、密码签名可被用于离线猜测，以及退出只清 Cookie 的问题，现已修复。当前适合个人部署的基础防护，但保留短密码与 20 天会话，未提供 MFA 或抗 DDoS 保证；全局限流也可能被攻击者占用，影响新登录。撤销或到期不会擦除已下载的浏览器缓存。

### 2026-10-16 — Web 收件箱恢复明暗切换

- 删除 Web 网关初始化时强制深色的逻辑；登录页和收件箱顶部均提供明暗切换按钮。首次使用跟随系统，手动选择复用 `orbit.web.theme` 并在刷新后保留；主题脚本在首屏绘制前应用，避免先显示错误主题。
- tx 仅更新 Web 为 `orbit-web:web-theme-20261016T073758Z`，Core 容器与原连接配置保持不变。发布源码、构建清单及验证记录位于 `/srv/orbit-deploy/releases/web-theme-20261016T073758Z`；Compose 回退备份位于 `/srv/orbit-deploy/backups/web-theme-20261016T073758Z`。
- 15 项 JS 回归与 Web Go 测试通过。真实浏览器验证系统主题变化、手动偏好、刷新及登录后保持，以及 1440px、390px、320px 和搜索状态无横向溢出。线上复核主题按钮及刷新记忆；前端资源、运行二进制摘要与本地构建一致，原登录及快照接口正常。
