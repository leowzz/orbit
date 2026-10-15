# Orbit Agent

独立的 Tauri 2 桌面 Agent。React / TypeScript 提供设置窗口，Rust 完成采集、通信和本机操作，不需要安装 Go 或运行 Go sidecar。

支持 macOS 和 Windows 的同一套源代码。macOS 已进行本地构建及窗口验收；Windows 的构建和测试由 `build-desktop-agent.yml` 覆盖，仍需在 Windows 真机验证安装、凭据存储、登录启动及 Codex 链接关联。

## 使用

1. 打开程序，在「连接」中填写 MQTT 地址和凭据，或导入现有 `agent.local.yaml`。导入会读取 YAML 引用的凭据文件，但不会修改原文件。
2. 在「数据来源」中启用 Codex、Sub2API。默认不上传会话标题和项目名称；需要在工作台显示时单独开启。
3. 在「偏好设置」中按需允许工作台打开 Codex 会话，并设置登录电脑后自动启动。
4. 点击「保存并应用」。关闭窗口后继续在托盘运行；暂停和退出使用托盘菜单。

首次启动不会自动连接。保存的配置会在后续启动时自动运行。密码使用 macOS Keychain / Windows Credential Manager 保存，普通配置位于 Tauri 的本机应用配置目录 `dev.orbit.agent/settings.json`。系统凭据不可读时会显示错误，不会将密码降级保存到 JSON。

迁移现有 Go Agent 时保持设备标识与 Core 投影路由一致。macOS 默认标识沿用 Go 的平台 UUID 派生算法；Windows 首次生成并在保存时持久化，也可指定已有标识。开始使用新 Agent 前先停止旧进程（旧 macOS launchd 服务可运行仓库根目录的 `make stop-agent`）。两个实例共享标识时，收到 MQTT Session Taken Over 的实例会停止重连。

远程打开要求本机已安装 Codex 桌面应用，并注册 `codex://` 链接。Agent 只接受短时有效的指定会话打开指令；不提供远程任意命令执行。周限通过已登录的 `codex app-server` 读取，不读取或导出 Codex 登录令牌。Windows 上若不能自动发现 Codex 可执行文件，在采集设置中指定它。

## 开发与验证

需要 Node.js 24、pnpm 11.9.0、Rust stable，以及目标平台的 Tauri 编译依赖：macOS Xcode Command Line Tools；Windows MSVC Build Tools 与 WebView2。Protobuf 编译器由构建依赖提供。

```sh
cd agents/desktop
pnpm install --frozen-lockfile
pnpm tauri dev

pnpm check
pnpm test
cargo clippy --manifest-path src-tauri/Cargo.toml --all-targets -- -D warnings
pnpm tauri build --debug --bundles app   # macOS
pnpm tauri build --debug --bundles nsis  # Windows
```

构建产物在 `src-tauri/target/debug/bundle/`。当前构建流程产出本地测试包，未配置分发签名、公证或自动更新，不会发布到现有 Release。

仅检查界面时可运行 `pnpm dev` 并打开 `http://127.0.0.1:1420/?preview`。预览明确标记且禁止保存、连接、打开会话等操作；正式构建不包含预览入口。

## 协议与实现

| 模块                        | 能力                                                                          |
| --------------------------- | ----------------------------------------------------------------------------- |
| `src-tauri/src/codex.rs`    | 只读 SQLite、进程存活、rollout 状态校正、子任务和路径过滤、隐私字段、账户周限 |
| `src-tauri/src/usage.rs`    | Sub2API 登录、刷新、401 恢复、429 退避、时区日窗口、精确费用换算              |
| `src-tauri/src/runtime.rs`  | MQTT 5、TLS / mTLS、QoS 1、重连、状态与采集健康、有效期、业务字段去重         |
| `src-tauri/src/commands.rs` | 目标和时间验证、UUID 链接、重复指令缓存、打开结果回传                         |
| `src-tauri/src/config.rs`   | YAML 导入、本机配置、系统凭据存储、原子保存                                   |
| `src-tauri/src/clock.rs`    | NTP 应用级时间校准，失败回退系统时间                                          |
| `src-tauri/src/lib.rs`      | Tauri 命令、单实例、托盘、设置窗口、登录启动                                  |

直接编译仓库 `proto/orbit/v1`，保持现有 MQTT topic、消息类型和字段含义。首次连接与重连均先发布 retained AgentState，收到确认后再发送 observation；重连使用新 epoch 和最新仍有效的数据，丢弃旧连接的发送队列。用量和会话 observation 非 retained，业务字段未变时在到期前刷新。停机后 Core 按现有 TTL 将数据标记过期。

测试包含 SQLite / rollout 规则、隐私、费用精度、周限、命令过期和去重，以及本地 MQTT 5 服务验证重连、状态先于 observation、结果回传和重复标识退出。`testdata/*.bin` 由现有 Go protobuf 生成，用于 Rust 解码与逐字节往返验证；只在协议变更时从仓库根目录运行 `go run ./agents/desktop/testdata` 更新。日常构建与运行不依赖 Go。
