# Orbit Android Node

Flutter Android 应用，包含两个可独立添加、可缩放的桌面小组件：**用量**和 **Session 状态**。最低 Android 8.0（API 26）。

## 使用

1. 安装 APK，在应用中填写 MQTT 服务地址、Node ID、用户名和密码。
2. 点击「测试连接」验证网络、TLS/认证及本 Node 的 view 订阅权限。测试使用临时 Client ID，不保存输入、不发布 Node State；成功不代表 Core 已配置或有数据。
3. 点击「保存配置」，再「开始同步」。需要通知权限时允许通知；同步服务显示常驻通知，可在通知或应用里停止。
4. 点击卡片右上角添加到桌面；不支持固定组件的启动器可长按桌面 → 小组件 → Orbit，分别添加「用量」「Session 状态」。点击桌面组件返回应用。小米 HyperOS 路径为：长按桌面 → 小部件 → 全部应用 → 安卓小部件 → Orbit。

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

Flutter 负责配置和应用内预览；Android 原生前台服务负责 MQTT、持久缓存和 RemoteViews，Flutter 页面关闭后仍能更新。网络断开后每 15 秒重试，恢复后重新订阅 view、发布 State。未启用开机自动启动，重启手机后打开应用开始同步。

组件分别按 Usage/Codex 的 `fresh_until` 显示过期状态，连接状态单独显示；达到 `retain_until` 后隐藏旧值。拒绝超过 32 KiB、错误 Node ID、无效元数据、重复或倒退版本以及已过保留期限的视图。Session 组件优先展示运行中的 Session，最多显示 4 条。

服务运行时约 5 秒检查一次过期状态；系统强停、Doze 或厂商省电策略仍可能暂停后台网络/组件更新，不能保证锁屏持续实时。每个组件显示绝对更新时间供判断。系统强停后须重新打开应用启动；系统周期性组件刷新最低约 30 分钟。桌面上的多个同类组件共享该手机的配置与数据。

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

Android Gradle 构建从仓库根 `proto/` 自动生成 Java lite 代码，无需修改 Go 协议或提交生成目录。APK：`build/app/outputs/flutter-apk/app-debug.apk`。当前 release 使用 Flutter 模板的 debug 签名，仅用于本地安装；正式分发前需配置自己的签名。

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
