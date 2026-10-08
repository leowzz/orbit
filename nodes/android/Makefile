.DEFAULT_GOAL := help

FLUTTER ?= flutter
DART ?= dart
ADB ?= adb
DEVICE ?=
SMOKE_CONFIG ?=
APP_ID := dev.orbit.orbit_android
DEBUG_APK := build/app/outputs/flutter-apk/app-debug.apk

.PHONY: help deps devices doctor run build build-release install open logs \
	test test-native smoke analyze fmt fmt-check verify clean check-device

help:
	@printf '%s\n' \
	  'deps           安装 Flutter 依赖' \
	  'devices        列出可用设备' \
	  'doctor         检查 Flutter / Android 开发环境' \
	  'run            调试运行（DEVICE=设备ID）' \
	  'build          构建 debug APK' \
	  'build-release  构建 release APK（签名沿用项目配置）' \
	  'install        构建并覆盖安装 debug APK，保留应用数据（DEVICE=设备ID）' \
	  'open           打开已安装的应用（DEVICE=设备ID）' \
	  'logs           查看应用日志（DEVICE=设备ID）' \
	  'test           运行 Flutter 单元 / Widget 测试' \
	  'test-native    运行 Android JVM 测试' \
	  'smoke          真机 MQTT 测试（DEVICE=设备ID SMOKE_CONFIG=私有JSON路径）' \
	  'analyze        Dart 静态检查' \
	  'fmt            格式化 Dart 代码' \
	  'fmt-check      检查 Dart 格式，不修改文件' \
	  'verify         顺序执行格式、静态检查、Flutter 和 Android 测试' \
	  'clean          清理 Flutter 构建产物' \
	  '' \
	  '示例：make install DEVICE=fa7b189f' \
	  '注意：smoke 会替换设备 MQTT 配置并启动同步；测试 APK 不可分发。' \
	  '测试后执行 make clean，再 make install DEVICE=设备ID，恢复普通 APK。'

deps:
	$(FLUTTER) pub get

devices:
	$(FLUTTER) devices

doctor:
	$(FLUTTER) doctor -v

check-device:
	@test -n "$(DEVICE)" || { echo '请指定 DEVICE，例如 DEVICE=fa7b189f' >&2; exit 2; }

run: check-device
	$(FLUTTER) run -d "$(DEVICE)"

build:
	$(FLUTTER) build apk --debug

build-release:
	$(FLUTTER) build apk --release

install: check-device
	$(MAKE) build
	$(ADB) -s "$(DEVICE)" install -r "$(DEBUG_APK)"

open: check-device
	$(ADB) -s "$(DEVICE)" shell am start -n "$(APP_ID)/.MainActivity"

logs: check-device
	$(FLUTTER) logs -d "$(DEVICE)"

test:
	$(FLUTTER) test test

test-native: deps
	cd android && ./gradlew :app:testDebugUnitTest --console=plain

smoke: check-device
	@test -n "$(SMOKE_CONFIG)" && test -f "$(SMOKE_CONFIG)" || { echo '请指定存在的 SMOKE_CONFIG 私有 JSON 文件' >&2; exit 2; }
	$(FLUTTER) test integration_test/node_smoke_test.dart -d "$(DEVICE)" --dart-define-from-file="$(SMOKE_CONFIG)"

analyze:
	$(FLUTTER) analyze

fmt:
	$(DART) format lib test integration_test

fmt-check:
	$(DART) format --output=none --set-exit-if-changed lib test integration_test

verify:
	$(MAKE) fmt-check
	$(MAKE) analyze
	$(MAKE) test
	$(MAKE) test-native

clean:
	$(FLUTTER) clean
