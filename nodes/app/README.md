# Orbit App

Android 统一显示为 **Orbit**。底部「收件箱」管理文本、待办和图片；「状态与组件」复用收件箱连接显示摘要；「设置」页面提供连接设置。可选的「Android 桌面组件」入口保留原来的设备设置和后台同步。

沿用 `dev.orbit.orbit_android` 应用身份，可覆盖升级原 Android 版本并保留 MQTT 配置与已放置的小组件。收件箱与组件分别使用 HTTPS 设备令牌和原 MQTT 配置，后台省电策略沿用组件实现。`nodes/android` 作为共享功能模块复用，正式 APK 从本目录构建。

Android 优先的个人收件箱。浏览、复制、编辑文本，完成/撤销待办，发送和查看图片。
Note（收件箱）与 Web Node 沿用同一套浅绿背景、白色卡片与绿色操作按钮。顶栏提供搜索按钮和紧凑的无边框筛选菜单，点击搜索后在顶栏展开输入框，关闭时清空关键词并保留筛选。消息按日期分组；卡片顶部直接复制或打开操作菜单，底部输入卡片用于编辑草稿。

首次使用时新消息默认为文本，输入框下方的「设为待办」选项会记住上次选择；消息菜单可在文本和待办之间切换。添加图片后先显示在草稿中，可移除或补充文字，再点击「发送」。已完成待办保留勾选状态，正文不加删除线。超过 8 行的消息默认收起，可展开阅读全文。打开应用后同步，后台通知尚未提供。

顶部可搜索全部本机缓存，筛选全部、链接、未完成待办、已完成待办和图片；消息可直接复制和打开链接。Ctrl / ⌘ + Enter 发送多行输入。上滑浏览消息时自动收起输入框和底部导航，下滑或切换筛选后恢复，草稿保持不变。

发送中、排队和上传过程不展示警告卡，失败或冲突时才显示草稿及处理入口。
内容先保存在本机待发送队列，Core 确认持久化后显示“已保存”。离线时可查看已缓存内容、
复制文字和新增内容；再次打开或恢复连接后提交。冲突保留草稿，可复制后重新编辑，
不会静默覆盖另一台设备的修改。删除后从所有设备隐藏，可在控制台「App 与收件箱 → 收件箱 → 已删除」中逐条恢复，正文、完成状态和图片附件保留。

[查看 Note 界面预览](docs/inbox-preview.png)（Flutter 渲染的公共示例，状态源未连接）。

## 工具链与运行

本次使用 Flutter 3.44.8 / Dart 3.12.2；实际插件版本锁定在 pubspec.lock：
ChangeNotifier、sqflite（Windows 使用 sqflite_common_ffi）、flutter_secure_storage、
image_picker。Flutter 工程共享 Android/macOS/Windows，首版验收以 Android 为主。
Android 最低 Android 8 / API 26，以支持原有桌面组件和前台同步服务。

```sh
cd nodes/app
flutter pub get
flutter analyze
flutter test
flutter run -d <device-id>
flutter build apk --debug
```

调试 APK：`build/app/outputs/flutter-apk/app-debug.apk`。这不是正式签名发行版。
macOS 使用 `flutter build macos`，需要在 Xcode 配置开发团队/签名，Keychain entitlement
用于安全令牌存储。Windows 在 Windows 主机运行 `flutter build windows`。
三端均保留共享代码，但桌面系统插件与发布打包不能由 Android 构建替代验证。

## CI 构建

[Build App](../../.github/workflows/build-app.yml) 在 App、Makefile 或该工作流变更的
PR/main 推送时运行，也支持 `v*` 标签推送和手动触发。固定使用 Flutter 3.44.8、
Java 17，安装锁定依赖后执行 `make test-app` 和 `make build-app`。

构建成功后，从 Actions 对应运行的 Artifacts 下载 `orbit-android-debug`，包含可安装的
调试 APK，保留 14 天。当前 CI 覆盖 Android；macOS/Windows 打包和正式签名发布仍待补充。

## 连接服务

在管理台「App 与收件箱」添加设备（或重置令牌）后，使用 Android App 连接页的
「扫码填写」扫描弹窗二维码，或点击「复制连接信息」后在 App 粘贴。地址和令牌自动填入，点击「连接」后先验证服务和授权再保存。二维码仅在
此次凭据弹窗中显示；不要分享。相机权限只在扫码时申请，也可随时返回手动填写。

首次打开填写服务地址和**这台设备独立的令牌**。生产只接受 HTTPS；debug 构建允许
localhost、127.0.0.1、10.0.2.2（Android 模拟器宿主机）和 ::1 的 HTTP 测试地址。
令牌保存在系统安全存储。每个“服务地址 + 令牌”使用独立的缓存和队列；切换连接
不会把旧连接的未发送内容传到新服务，切回原连接后可继续处理。

## Core 配置

启用 `configs/core.example.yaml` 中的 app 配置，数据目录要可写。每台设备生成独立令牌：

```sh
umask 077
mkdir -p configs/secrets
openssl rand -hex 32 > configs/secrets/phone-01-token
python3 -c 'import hashlib,pathlib; print(hashlib.sha256(pathlib.Path("configs/secrets/phone-01-token").read_text().strip().encode()).hexdigest())'
```

以上手动配置仅用于首次导入。日常使用可直接在管理台「App 与收件箱 → 添加设备」生成令牌，将地址与令牌填入 App。更换令牌与撤销访问即时生效。
撤销设备：在管理台撤销该设备；首次导入后的 YAML 修改不会覆盖数据库中的授权。不要将令牌放进 URL、日志或仓库。
仅收件箱可省略 projection_routes/observation_policies；管理台仍需设置 console.password。
状态摘要在 Core 管理台新增对应设备的 `overview-app` 路由并选择数据源。
下面的 YAML 仅用于首次创建路由数据库时导入，已有数据库以管理台保存的路由为准：

```yaml
projection_routes:
  phone-01:
    profile: overview-app
    inputs:
      - agent_id: agent-local
        observation_type: usage
observation_policies:
  usage:
    max_ttl: 5m
app:
  listen: 0.0.0.0:7622
  data_dir: ../data/app
  devices:
    phone-01:
      label: 我的手机
      platform: android
      token_sha256: <生成的摘要>
```

只用消息时可采用 `configs/core.inbox.example.yaml`，无需 MQTT、Agent 或状态转发规则。详见 [个人收件箱](../../docs/personal-inbox.md)。API 契约见
[app-api.md](../../docs/app-api.md)，HTTPS 代理和备份见 [运行说明](../../docs/app-operations.md)。

使用无私人内容的固定示例：

```sh
go run ./cmd/orbit-core -config configs/core.local.yaml -seed-inbox
```

该命令从仓库根目录运行，写入两条幂等示例后退出，不连接 Broker。

## 验证

`flutter test` 覆盖缓存/游标原子性、持久 outbox、重复响应、冲突草稿、分页中断、360/900
逻辑像素布局和离线发送。Android 端到端测试使用真实 SQLite/安全存储和真实 Go API：

```sh
# 从仓库根目录运行；先启动一个专用测试模拟器。
python3 scripts/app-smoke.py --emulator emulator-5554 --flutter /absolute/path/to/flutter
```

脚本创建临时数据库与两个测试设备，故意指向不可达 Broker，运行前会清理模拟器上的
Orbit 测试应用数据。不要对日常使用的设备运行。测试覆盖设备连接、前台同步、离线
提交、待办完成、跨设备冲突、图片上传/下载。端口 17622 必须空闲。

本地数据库和图片缓存位于系统应用支持目录，Android 禁用自动备份以免恢复与旧安全
凭据不匹配的数据。内容缓存未另外加密；卸载通常会移除本机数据，已保存到 Core 的
条目可以再次同步。图片按需缓存；当前不提供批量缓存清理界面。
