# Orbit App

Android 优先的个人收件箱。顶部用量和会话摘要独立更新；下方浏览、复制、编辑文本，
完成/撤销待办，发送和查看图片。打开应用后同步，后台通知尚未提供。

内容先保存在本机待发送队列，Core 确认持久化后显示“已保存”。离线时可查看已缓存内容、
复制文字和新增内容；再次打开或恢复连接后提交。冲突保留草稿，可复制后重新编辑，
不会静默覆盖另一台设备的修改。删除会同步到所有设备，无法撤销。

[查看 Android 实际界面](docs/android-inbox.png)（公共示例，状态源未连接）。

## 工具链与运行

本次使用 Flutter 3.44.8 / Dart 3.12.2；实际插件版本锁定在 pubspec.lock：
ChangeNotifier、sqflite（Windows 使用 sqflite_common_ffi）、flutter_secure_storage、
image_picker。Flutter 工程共享 Android/macOS/Windows，首版验收以 Android 为主。
Android minSdk 使用 Flutter 默认值 24；本次在 Android 8 / API 26 ARM64 模拟器验证。

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

把最后输出的摘要填入 `app.devices.phone-01.token_sha256`，令牌本身通过私有渠道输入 App。
撤销设备：设置 `revoked: true` 或删除设备配置并重启 Core。不要将令牌放进 URL、日志或仓库。
仅收件箱可省略 projection_routes/observation_policies；状态摘要需显式配置：

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

完整 Core 仍需 core/mqtt 配置；Broker 离线不妨碍收件箱。API 契约见
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
