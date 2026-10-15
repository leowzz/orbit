# Orbit App

在「设置 → 暗色模式」手动切换外观，也可点击工作台顶栏的日/月图标。选择会保存在本机，重启后保留，Android 桌面组件使用同一外观，不跟随系统变化；首次使用默认为浅色。矩阵显示保留独立的深色配色。

Android 统一显示为 **Orbit**。「收件箱」管理文本、待办和图片；第二个入口「工作台」展示指标与可点击的 Codex 会话；「数据展示」提供状态摘要、点阵屏和桌面组件，共用收件箱的设备连接与转发规则；「设置」提供连接和本机快捷操作设置。普通屏幕使用底部导航；低高度横屏自动改用左侧竖排导航，并以 85% 比例显示页面、菜单和弹窗。横屏收件箱用于查看，采用紧凑顶栏并隐藏整个发送区，保留消息浏览、筛选和搜索。

沿用 `dev.orbit.orbit_android` 应用身份，覆盖升级保留已放置的小组件。连接 Orbit 后，桌面组件自动使用同一个设备令牌，不需要再填写 MQTT。组件沿用亮屏同步、息屏暂停策略。`nodes/android` 保留旧 MQTT 客户端兼容入口，正式 APK 从本目录构建。

在管理台为此设备设置一条规则，选择 API 用量、账号周限或循环显示，并独立设置 Session 信息开关；点阵屏和组件按同一规则展示。切换展示方式会保留已配置的数据来源。收件箱消息不依赖状态转发规则，也不会因删除规则而清空。


Android 优先的个人收件箱。浏览、复制、编辑文本，完成/撤销待办，发送和查看图片。
Note（收件箱）与 Web Node 沿用同一套浅绿背景、白色卡片与绿色操作按钮。顶栏提供搜索按钮和紧凑的无边框筛选菜单，点击搜索后在顶栏展开输入框，关闭时清空关键词并保留筛选。消息按日期分组；卡片顶部直接复制或打开操作菜单，底部输入卡片用于编辑草稿。

首次使用时新消息默认为文本，输入框下方的「设为待办」选项会记住上次选择；消息菜单可在文本和待办之间切换。添加图片后先显示在草稿中，可移除或补充文字，再点击「发送」。已完成待办保留勾选状态，正文不加删除线。超过 8 行的消息默认收起，可展开阅读全文。打开应用后同步，后台通知尚未提供。

顶部可搜索全部本机缓存，筛选全部、链接、未完成待办、已完成待办和图片；消息可直接复制和打开链接。Ctrl / ⌘ + Enter 发送多行输入。上滑浏览消息时自动收起输入框和底部导航，下滑或切换筛选后恢复，草稿保持不变；低高度横屏的左侧导航保持可用。

发送中、排队和上传过程不展示警告卡，失败或冲突时才显示草稿及处理入口。
内容先保存在本机待发送队列，Core 确认持久化后显示“已保存”。离线时可查看已缓存内容、
复制文字和新增内容；再次打开或恢复连接后提交。冲突保留草稿，可复制后重新编辑，
不会静默覆盖另一台设备的修改。删除后从所有设备隐藏，可在控制台「收件箱 → 已删除」中逐条恢复，正文、完成状态和图片附件保留。

[查看 Note 界面预览](docs/inbox-preview.png)（Flutter 渲染的公共示例，状态源未连接）。

在「数据展示」点击「切换输入法」，从系统列表选择输入法；点击「添加到桌面」并按桌面提示确认后，可从桌面直接打开该列表。无需连接收件箱，也无需 Root。部分桌面需要允许 Orbit 创建桌面快捷方式。

## 工作台

低高度横屏沿用 Web Node 的左侧指标、右侧双列会话布局；窄屏使用纵向布局。`NFCD_AlipayBoxN5DEL` 型号在标准字号的横屏工作台中固定展示 4 行 2 列，行高随可用区域调整，其余会话可滚动查看；其他型号保持内容自适应高度。点击 Orbit 图标或标题进入全屏，再次点击退出，系统返回键与 Escape 同样可退出。全屏中的会话仍可点击，不会退出全屏。剩余时间使用 `4d23h` 格式。

工作台共用当前设备连接和转发规则，支持 API 用量、账号周限与循环显示。Session 信息关闭时不展示会话；开启后点击会话，由 Core 向对应 Mac agent 发送打开指令，成功时不弹出提示，仅在异常时提示。需要同时更新支持会话打开接口的 Core；离线请求不会排队后补发。

## 快捷操作

在 Core「设备 → 选择设备 → 快捷操作」中添加接收设备与执行端的配对，可配对多个 Agent
和具备执行能力的 App Node。可跟随执行端全部入口或选择部分入口并排序，与状态摘要的数据来源独立。
快捷栏标题、位置和 NFCD 留白在 Core 按设备设置。入口提示标明执行设备；点击只发送入口 ID。

此 App 也可作为执行端：在「设置 → 本机快捷操作」添加 URL，Android 还可选择已安装应用，
再开启本机执行许可并保存。应用包名和 URL 目标仅保存在本机；Core 只接收入口名称、类型和标识。
执行端需要 Orbit 在前台连接。切到后台、关闭许可、入口版本不符或超过 15 秒的请求不会执行；
同一请求不重复打开，不进入离线队列。Core 的 202 响应表示请求已发送，不代表目标应用已打开。
旧版 `nodes/android` MQTT 显示客户端仍只接收视图，不提供本机执行能力。

## 点阵屏

从「数据展示」或「设置 → 点阵屏」进入。可调像素密度、光晕范围和光晕强度，预览即时更新，设置保存在本机。「横向全屏」隐藏系统栏并保持屏幕常亮，轻触画面或按返回键回到设置；退出后恢复原方向策略。低高度横屏使用左右分栏，预览和全屏按钮保持可达。

点阵展示精简为三项：大号整数剩余百分比（LEFT）、重置日期（RESET，月/日）和剩余天/小时（D/H）。展示区不含中文，移除已用比例、进度条、费用、Token、TPM 和更新时间；只有足够宽的屏幕保留 ChatGPT 图标。日期使用设备本地时区，离线和过期数据分别标记 OFFLINE / STALE，到达重置时间后显示 RESETTING 并等待新额度，不自动恢复为 100%。设置页面保持中文。

数据沿用 Orbit 连接；在管理台为此设备的 `overview-app` 规则配置 usage / codex 来源和展示方式。账号周限需要 Agent 启用周额度采集。循环显示由 Core 统一选择当前页面。没有对应来源数据时显示 NO DATA。视觉参考 [glow-clock](https://github.com/leowzz/glow-clock)，图标来源与许可见 [assets/matrix/README.md](assets/matrix/README.md)。

真机布局测试使用独立包名 `dev.orbit.orbit_android.benchmark`、临时数据库和明确的测试额度：

```sh
flutter drive --no-pub --keep-app-running --driver=test_driver/matrix_driver.dart --target=integration_test/matrix_smoke_test.dart -d <device-id>
adb -s <device-id> uninstall dev.orbit.orbit_android.benchmark
```

测试截图写入 `build/matrix-smoke/`。完成后只清理独立测试包，正常应用保持安装。

滑块性能回归使用独立包名 `dev.orbit.orbit_android.benchmark`，Gradle 根据下方测试入口自动添加后缀。
测试使用临时数据库和模拟显示设置，不连接服务器。先构建，再确认 APK 的 application ID
为上述独立包名后运行；不要把正式包名的 APK 交给 `flutter drive`，它默认在测试结束时卸载应用。

```sh
flutter build apk --profile --target-platform android-arm64 --target integration_test/matrix_performance_test.dart
MATRIX_PERF_LABEL=after flutter drive --profile --no-pub --no-dds --keep-app-running --target integration_test/matrix_performance_test.dart --driver test_driver/matrix_performance_driver.dart --use-application-binary build/app/outputs/flutter-apk/app-profile.apk -d <device>
adb -s <device> uninstall dev.orbit.orbit_android.benchmark
```

`MATRIX_PERF_LABEL=before` / `after` 将三组固定拖动的帧耗时和预览截图写入
`build/matrix-performance/` 下的对应目录。清理时仅卸载上述独立测试包。

2026-10-15 在 137（1600×720）上用 profile 模式与上述固定数据实测：
批量绘制与缓存后，密度、光晕范围、光晕强度拖动的平均 raster 耗时分别从
308.7 / 311.6 / 333.0 ms 降至 7.8 / 12.1 / 11.6 ms。
这是高密度压力场景的单帧绘制耗时，不代表所有页面或设备的帧率。

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

Session 信息独立于指标展示：开启后状态页同时展示 Codex 会话详情；关闭后清空详情。点阵屏始终显示所选指标，桌面指标组件和会话组件各自展示对应内容。
