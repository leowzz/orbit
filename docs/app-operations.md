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
