# Orbit Agent

独立的 Tauri 2 桌面 Agent。React / TypeScript 提供设置窗口，Rust 完成采集、通信和本机操作，不需要安装 Go 或运行 Go sidecar。

支持 macOS 和 Windows 的同一套源代码。macOS 已进行本地构建及窗口验收；Windows 的构建和测试由 `build-desktop-agent.yml` 覆盖，仍需在 Windows 真机验证安装、凭据存储、登录启动及 Codex 链接关联。

## 使用

1. 打开程序，在「连接」中填写 MQTT 地址和凭据，或导入现有 `agent.local.yaml`。导入会读取 YAML 引用的凭据文件，但不会修改原文件。
2. 在「数据来源」中启用 Codex、Sub2API。默认不上传会话标题和项目名称；需要在工作台显示时单独开启。
3. 在「偏好设置」中按需允许工作台打开 Codex 会话，并设置登录电脑后自动启动。
4. 点击「保存并应用」。关闭窗口后继续在托盘运行；暂停和退出使用托盘菜单。

在「快捷入口」中拖入应用，自动读取名称和图标，也可以选择应用或添加任意协议的完整 URL（如 HTTP、FTP、`codex://`、`mailto:`）。
已添加的入口以图标、名称和类型卡片展示，支持拖拽排序、双击在本机打开、右键编辑或删除；键盘 Enter 打开、Alt + 左右键排序、
Shift + F10 打开菜单。编辑表单可修改名称、网址、应用和图标；取消不会修改入口。
图片可拖到入口图标或编辑表单图标上，也可在表单内选择图片。
网址默认填写 `https://`，可手动修改协议。HTTP/HTTPS 地址可自动获取网站信息，其他协议可手动设置名称和图标。填写地址后，点击「获取网站图标」自动读取并预览；名称为空时同时填入网站名称，已填写的名称保留。获取失败会保留原图标，也可手动选择图片。
保存后，Core 可发现这台 Agent 的快捷入口。在 Core「设备 → 选择设备 → 快捷操作」中
添加执行端配对，可跟随全部入口，也可选择部分入口并独立排序；一台接收设备可配对多个 Agent
或有执行能力的 Node。配对不依赖用量或 Codex 的数据来源。
导航标题、顶部位置和 NFCD 留白在 Core 按接收设备设置；旧设备首次迁移保留原有实际显示效果。
应用和网址均由 Agent 所在电脑的关联应用打开；对应协议需要该电脑安装支持它的应用。移除入口并保存后立即撤销其打开权限。
macOS 接受 `.app`，Windows 接受 `.exe` / `.lnk`；Windows 的应用图标提取与打开仍待真机验收。

macOS 的「最近使用」推荐区自动记录 Agent 运行期间的应用启动和前台切换，并用当前运行应用的
启动时间初始化推荐。点击推荐立即加入预览，已添加的应用不会重复推荐；点击「保存并应用」后
同步到工作台。仅记录应用路径和最近时间，最多 64 个应用、保留 90 天，保存在本机
`recent-apps.json`，不上传窗口标题、内容或使用记录。后台每两秒检查一次应用变化；
Windows 暂不采集最近应用，手动配置与底栏交互仍可使用。

应用记录使用 macOS 的 [NSWorkspace.frontmostApplication](https://developer.apple.com/documentation/appkit/nsworkspace/frontmostapplication)
及运行应用的启动时间，不需要辅助功能或屏幕录制权限。

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
pnpm test:ui
cargo clippy --manifest-path src-tauri/Cargo.toml --all-targets -- -D warnings
pnpm tauri build --debug --bundles app   # macOS
pnpm tauri build --debug --bundles nsis  # Windows
```

调试构建产物在 `src-tauri/target/debug/bundle/`。`build-desktop-agent.yml` 在 PR、main 分支变更或手动触发时构建 macOS / Windows 调试包。

推送 `vX.Y.Z` 或 `vX.Y.Z-rc.N` tag 时，`release.yml` 检查并构建 macOS Universal DMG（Apple Silicon / Intel，macOS 12+），以 `orbit-agent-macos-universal-TAG.dmg` 上传到同一 GitHub Release。应用版本由 tag 去掉 `v` 后注入；产物纳入 `SHA256SUMS` 和 Cosign 下载签名。未配置 Apple Developer ID 签名、公证或自动更新，首次打开可能需要在系统「隐私与安全性」中手动允许。Windows 仍仅提供 CI 调试包。完整发布与校验说明见 [Tagged releases](../../docs/releases.md)。

仅检查界面时可运行 `pnpm dev` 并打开 `http://127.0.0.1:1420/?preview`。预览明确标记且禁止保存、连接、打开会话等操作；正式构建不包含预览入口。

## 协议与实现

| 模块                        | 能力                                                                          |
| --------------------------- | ----------------------------------------------------------------------------- |
| `src-tauri/src/codex.rs`    | 只读 SQLite、进程存活、rollout 状态校正、子任务和路径过滤、隐私字段、账户周限 |
| `src-tauri/src/usage.rs`    | Sub2API 登录、刷新、401 恢复、429 退避、时区日窗口、精确费用换算              |
| `src-tauri/src/runtime.rs`  | MQTT 5、TLS / mTLS、QoS 1、重连、状态与采集健康、有效期、业务字段去重         |
| `src-tauri/src/commands.rs` | 目标和时间验证、UUID 链接、重复指令缓存、打开结果回传                         |
| `src-tauri/src/shortcuts.rs` | 应用和图标导入、受限应用/网址打开、导航目录发布 |
| `src-tauri/src/config.rs`   | YAML 导入、本机配置、系统凭据存储、原子保存                                   |
| `src-tauri/src/clock.rs`    | NTP 应用级时间校准，失败回退系统时间                                          |
| `src-tauri/src/lib.rs`      | Tauri 命令、单实例、托盘、设置窗口、登录启动                                  |

直接编译仓库 `proto/orbit/v1`，保持现有 MQTT topic、消息类型和字段含义。首次连接与重连均先发布 retained AgentState，收到确认后再发送 observation；重连使用新 epoch 和最新仍有效的数据，丢弃旧连接的发送队列。用量和会话 observation 非 retained，业务字段未变时在到期前刷新。停机后 Core 按现有 TTL 将数据标记过期。

测试包含 SQLite / rollout 规则、隐私、费用精度、周限、命令过期和去重，以及本地 MQTT 5 服务验证重连、状态先于 observation、结果回传和重复标识退出。`testdata/*.bin` 由现有 Go protobuf 生成，用于 Rust 解码与逐字节往返验证；只在协议变更时从仓库根目录运行 `go run ./agents/desktop/testdata` 更新。日常构建与运行不依赖 Go。
