# Orbit Android Node

Flutter Android 应用，包含两个可独立添加、可缩放的桌面小组件：**用量**和 **Session 状态**。最低 Android 8.0（API 26）。

- 用量：浅色紧凑卡片，默认/最小 2×1，优先显示金额，空间充足时增加 TOK / TPM（K/M/B/T 简写，3×1 等扁宽卡片在金额右侧分两行显示，较高卡片放在底部）；USD 使用 `$`，向零截断到两位小数并省略末尾零，正数不足一美分显示 `<$0.01`。
- Session：默认 4×2、最小 2×2，状态和名称分列显示，以颜色区分运行、完成、失败、中断、取消；字号、内边距和行数随组件宽高调整。

长按桌面组件并拖动边框即可缩放。用量金额会自动适配可用空间，最大字号 64sp；Session 最多显示 20 条。Android 12 及以上优先使用启动器提供的实际尺寸，旧版分别适配横竖屏；具体可选占格由启动器决定。

## 使用

1. 安装 APK，在应用中填写 MQTT 服务地址、Node ID、用户名和密码。
2. 点击「测试连接」验证网络、TLS/认证及本 Node 的 view 订阅权限。测试使用临时 Client ID，不保存输入、不发布 Node State；成功不代表 Core 已配置或有数据。
3. 点击「保存配置」，再「开始同步」。需要通知权限时允许通知；同步服务显示常驻通知，可在通知或应用里停止。
4. 点击卡片右上角添加到桌面；不支持固定组件的启动器可长按桌面 → 小组件 → Orbit，分别添加「用量」「Session 状态」。点击桌面组件返回应用。小米 HyperOS 路径为：长按桌面 → 小部件 → 全部应用 → 安卓小部件 → Orbit。

特殊设备不方便输入时，点击「测试连接」旁的「二维码」，用同一局域网的手机扫码打开配置网页。网页读取设备当前表单，支持修改、从 Android 设备测试连接、保存至同一份 Keystore 加密配置；保存后设备表单同步更新，已运行的同步服务重新加载。多个局域网地址可在弹窗中切换。保持弹窗打开，关闭后临时服务停止、链接失效；链接包含随机访问令牌，HTTP 配置页面仅在可信局域网使用，不要转发链接。

服务地址支持 `mqtts://mqtt.example.com:8883` / `ssl://mqtt.example.com:8883`（默认推荐，系统信任链与主机名校验）和显式的 `mqtt://host:1883` / `tcp://host:1883`（明文内网）。不支持 WebSocket、自签名 CA 导入或 mTLS。用户名/密码可留空，取决于 Broker 配置。配置整体由 Android Keystore AES-GCM 加密，关闭应用备份；没有硬编码凭据。

Node ID 在每台手机上必须唯一。正式 MQTT Client ID 是 `orbit-node-{node_id}`，测试连接使用 `orbit-node-{node_id}-test-{random}`；Broker 若限制 Client ID，需要放行对应测试 ID。

## Core 配置

在现有 Core YAML 的 `projection_routes` 下添加路由，Agent ID 替换为实际值：

```yaml
projection_routes:
  android-phone:
    profile: overview-android
    inputs:
      - agent_id: agent-local
        observation_type: usage
      - agent_id: agent-local
        observation_type: codex

observation_policies:
  usage:
    max_ttl: 5m
  codex:
    max_ttl: 1m
```

更新并重启 Core 后，手机发布 `display/android/flutter` Node State，Core 才能为它投影数据。不要把手机伪装为 Web Node。可以只配置 usage 或 codex 一个输入；未配置的数据卡片显示等待。

手机 MQTT ACL：

| 操作 | Topic | QoS / retained |
| --- | --- | --- |
| 发布 | `orbit/v1/nodes/{node_id}/state` | 1 / yes |
| 订阅 | `orbit/v1/nodes/{node_id}/view` | 1 / Core retained |

只接收 Core 的 DeviceView，不访问 Agent observation 或上游服务认证。采用 MQTT 3.1.1，payload 直接使用仓库 `proto/` 生成的 Java lite Protobuf，与 MQTT 5 的 Core/Agent 通过 Broker 互通；MQTT 3.1.1 无 Content Type 属性。

## 后台与数据有效期

Flutter 负责配置和应用内预览；Android 原生前台服务负责 MQTT、缓存和 RemoteViews。保留用户手动开启的同步通知，不申请长期唤醒锁、不绕过系统省电限制。

- 亮屏：维持 MQTT 连接，保活 300 秒；Session 可见内容变化及时刷新，用量约每 60 秒合并刷新，未变化不重绘。App 前台每 10 秒读取本地快照，进入后台停止轮询。
- 息屏：主动断开 MQTT，取消重连与组件定时刷新，丢弃排队的接收消息；保留桌面最后画面。亮屏/解锁后检查网络，重新订阅 retained view 并发布 State，请求 Core 补齐最新快照。
- 断网：取消连接重试，等待默认网络恢复；有网络但连接失败时使用 15 秒起步、最高 5 分钟的指数退避和随机抖动。
- 无桌面组件且 App 不可见：停止同步服务。再次使用时在 App 中开始同步。未启用开机自动启动，重启手机或系统强停后也需打开应用启动。
- 缓存：拒绝重复/倒退版本。有效期、更新时间等变化先留在内存；内容相同时最多每分钟保存一次有效期检查点，内容变化立即保存。

Core 仅对 `overview-android` 路由合并用量推送、过滤时间戳变化；Session 内容或新鲜度变化立即发布，重连 State 强制返回当前视图。未变化的视图仍需在原有效期过半后续期；例如上游 TTL 很短时，网络续期频率可能高于每分钟一次，不会人为延长上游数据有效期。Web/OLED 路由保持原有策略。

组件分别按 Usage/Codex 的 `fresh_until` 显示过期状态，正常时不显示连接和更新时间，仅在停止、离线或过期时显示简短提示；达到 `retain_until` 后隐藏旧值。拒绝超过 32 KiB、错误 Node ID、无效元数据、重复或倒退版本以及已过保留期限的视图。Session 组件优先展示运行中的 Session，其余按更新时间倒序，随组件宽高及系统字号显示 1–20 条。

亮屏时约 60 秒检查过期状态，息屏期间不刷新过期提示。已关闭系统周期性组件刷新；桌面上的多个同类组件共享配置与数据。点击组件进入应用可查看详细连接状态和绝对更新时间。Doze 或厂商策略可能终止服务，亮屏恢复以进程仍存活为前提；不承诺深度休眠或系统强停后的自动恢复。参见 [Android Doze 限制](https://developer.android.com/training/monitoring-device-state/doze-standby)。

## 开发与验证

```sh
cd nodes/android
flutter pub get
flutter analyze
flutter test
flutter build apk --debug
cd android
./gradlew :app:testDebugUnitTest
```

Android Gradle 构建从仓库根 `proto/` 自动生成 Java lite 代码，无需修改 Go 协议或提交生成目录。Debug APK：`build/app/outputs/flutter-apk/app-debug.apk`。Release 必须通过环境变量配置专用 keystore，不再回退到 debug 签名。`v*` tag 自动构建签名 APK 并发布到 GitHub Release，配置方式见[发布文档](../../docs/releases.md)。

协议路由测试：仓库根运行 `go test ./internal/core ./internal/config ./internal/integration`。

## 真机连接冒烟测试

`integration_test/node_smoke_test.dart` 会在指定设备上测试 Broker 连接和订阅、配置加密保存/读取、启动后台服务并发布 Node State。它会替换设备上的 MQTT 配置并启动同步，只针对明确选择的测试设备运行。

将 `ORBIT_MQTT_URI`、`ORBIT_NODE_ID`、`ORBIT_MQTT_USERNAME`、`ORBIT_MQTT_PASSWORD` 写入仓库外的私有 JSON 文件后执行：

```sh
flutter test integration_test/node_smoke_test.dart -d DEVICE_SERIAL --dart-define-from-file=/private/path/device-config.json
```

该测试成功只证明连接/订阅及服务启动；日志另行报告是否收到 DeviceView。测试 APK 含测试配置，不得分发；完成后运行 `flutter clean`，再无参数构建普通 APK 并覆盖安装。普通应用无预置 Broker 凭据。

## 2026-09-08 验证记录

- Flutter analyze、2 个 Flutter 表单测试、4 个 Android JVM 测试通过；Go 全量测试和 vet 通过。
- Android 8.0 模拟器启动及小组件添加入口验证通过。
- Android 16 真机通过本地 MQTT 冒烟测试，验证加密配置、Node State 发布和 DeviceView 接收。
- 同一真机通过实际 TLS Broker 的连接/认证/订阅测试，接收 `android-phone` 的真实用量与 Session 数据；两个组件已放在同一空白桌面页，应用退到后台后仍观察到时间和数据持续更新。
- `tx:/srv/orbit-deploy` 的 Core/Web 更新至 `v0.1.8`，Core 配置包含 `overview-android` 路由；Web HTTP 返回 200，原有视图仍在更新。
- 以上不替代长时间锁屏、Doze、厂商省电及断网恢复测试。正式分发仍需独立签名。

### 省电策略验证（2026-09-08）

- Android debug 构建、JVM 重试/缓存内容策略测试、Flutter 后台暂停轮询测试通过；Go 全量测试及 Core race/vet 通过。
- 真机息屏连续 76 秒，Broker 确认客户端断开，缓存指纹不变；亮屏后约 0.62 秒重新连接并接收数据。此为单次功能检查，不是待机耗电测量。
- Core 在 `tx` 使用本地构建镜像 `orbit-core:v0.1.8-android-power1`；Compose 备份为 `/srv/orbit-deploy/docker-compose.yml.bak-power-20260908`。同一 60 秒窗口 Android 推送 13 次、Web 42 次；不同负载及上游 TTL 下结果会变化。
- 尚未做整夜待机耗电对照、厂商杀进程或长时间断网验收。
