import 'package:flutter/material.dart';
import 'package:flutter/services.dart';
import 'package:flutter_test/flutter_test.dart';
import 'package:orbit_android/main.dart';

void main() {
  TestWidgetsFlutterBinding.ensureInitialized();
  const channel = MethodChannel('dev.orbit/node');
  final calls = <MethodCall>[];
  var pinResult = 'rejected';
  setUp(() {
    calls.clear();
    pinResult = 'rejected';
    TestDefaultBinaryMessengerBinding.instance.defaultBinaryMessenger
        .setMockMethodCallHandler(channel, (call) async {
          calls.add(call);
          if (call.method == 'snapshot') {
            return {'connection': '未连接', 'active': false};
          }
          if (call.method == 'test') return '连接成功';
          if (call.method == 'pin') return pinResult;
          return null;
        });
  });
  tearDown(
    () => TestDefaultBinaryMessengerBinding.instance.defaultBinaryMessenger
        .setMockMethodCallHandler(channel, null),
  );

  testWidgets('short landscape keeps config actions reachable with keyboard', (
    tester,
  ) async {
    tester.view.physicalSize = const Size(1600, 624);
    tester.view.devicePixelRatio = 2;
    addTearDown(tester.view.resetPhysicalSize);
    addTearDown(tester.view.resetDevicePixelRatio);
    addTearDown(tester.view.resetViewInsets);
    await tester.pumpWidget(const OrbitApp());
    await tester.pumpAndSettle();
    expect(find.byTooltip('添加到桌面').hitTestable(), findsNWidgets(2));
    await tester.tap(find.text('配置'));
    await tester.pumpAndSettle();
    expect(find.text('保存配置').hitTestable(), findsOneWidget);
    await tester.enterText(
      find.widgetWithText(TextFormField, 'MQTT 服务地址'),
      'ssl://mqtt.example.com:8883',
    );
    tester.view.viewInsets = const FakeViewPadding(bottom: 240);
    await tester.pumpAndSettle();
    expect(find.text('测试连接').hitTestable(), findsOneWidget);
    await tester.tap(find.text('测试连接'));
    await tester.pumpAndSettle();
    expect(calls.where((c) => c.method == 'test'), hasLength(1));
    expect(tester.takeException(), isNull);
    tester.view.viewInsets = const FakeViewPadding();
    await tester.tap(find.text('状态'));
    await tester.pumpAndSettle();
    await tester.tap(find.text('配置'));
    await tester.pumpAndSettle();
    expect(
      tester
          .widget<TextFormField>(
            find.widgetWithText(TextFormField, 'MQTT 服务地址'),
          )
          .controller!
          .text,
      'ssl://mqtt.example.com:8883',
    );
    await tester.pumpWidget(const SizedBox());
  });

  testWidgets('short landscape QR and close action fit without scrolling', (
    tester,
  ) async {
    tester.view.physicalSize = const Size(1600, 624);
    tester.view.devicePixelRatio = 2;
    addTearDown(tester.view.resetPhysicalSize);
    addTearDown(tester.view.resetDevicePixelRatio);
    await tester.pumpWidget(
      const MaterialApp(
        home: QuickConfigDialog(urls: ['http://192.168.1.2:1234/example']),
      ),
    );
    await tester.pumpAndSettle();
    expect(find.text('完成并关闭').hitTestable(), findsOneWidget);
    expect(tester.takeException(), isNull);
    await tester.pumpWidget(const SizedBox());
  });

  testWidgets('background stops polling and resume refreshes immediately', (
    tester,
  ) async {
    await tester.pumpWidget(const OrbitApp());
    await tester.pumpAndSettle();
    tester.binding.handleAppLifecycleStateChanged(AppLifecycleState.inactive);
    tester.binding.handleAppLifecycleStateChanged(AppLifecycleState.hidden);
    tester.binding.handleAppLifecycleStateChanged(AppLifecycleState.inactive);
    tester.binding.handleAppLifecycleStateChanged(AppLifecycleState.hidden);
    tester.binding.handleAppLifecycleStateChanged(AppLifecycleState.paused);
    calls.clear();
    await tester.pump(const Duration(minutes: 2));
    expect(calls.where((c) => c.method == 'snapshot'), isEmpty);
    tester.binding.handleAppLifecycleStateChanged(AppLifecycleState.hidden);
    tester.binding.handleAppLifecycleStateChanged(AppLifecycleState.inactive);
    tester.binding.handleAppLifecycleStateChanged(AppLifecycleState.hidden);
    tester.binding.handleAppLifecycleStateChanged(AppLifecycleState.inactive);
    tester.binding.handleAppLifecycleStateChanged(AppLifecycleState.resumed);
    await tester.pump();
    expect(calls.where((c) => c.method == 'snapshot'), hasLength(1));
    await tester.pumpWidget(const SizedBox());
  });

  testWidgets('invalid broker is rejected before invoking native test', (
    tester,
  ) async {
    await tester.pumpWidget(const OrbitApp());
    await tester.pumpAndSettle();
    await tester.scrollUntilVisible(
      find.text('测试连接'),
      300,
      scrollable: find.byType(Scrollable).first,
    );
    await tester.tap(find.text('测试连接'));
    await tester.pumpAndSettle();
    expect(calls.where((c) => c.method == 'test'), isEmpty);
    await tester.pumpWidget(const SizedBox());
  });

  testWidgets('test uses entered settings without saving them', (tester) async {
    await tester.pumpWidget(const OrbitApp());
    await tester.pumpAndSettle();
    await tester.scrollUntilVisible(
      find.widgetWithText(TextFormField, 'MQTT 服务地址'),
      200,
    );
    await tester.enterText(
      find.widgetWithText(TextFormField, 'MQTT 服务地址'),
      'ssl://mqtt.example.com:8883',
    );
    await tester.scrollUntilVisible(
      find.text('测试连接'),
      200,
      scrollable: find.byType(Scrollable).first,
    );
    await tester.tap(find.text('测试连接'));
    await tester.pumpAndSettle();
    final call = calls.singleWhere((c) => c.method == 'test');
    expect((call.arguments as Map)['uri'], 'ssl://mqtt.example.com:8883');
    expect(calls.where((c) => c.method == 'save'), isEmpty);
    await tester.pumpWidget(const SizedBox());
  });
  testWidgets(
    'unsupported launcher directs to manual widgets without permissions',
    (tester) async {
      pinResult = 'unsupported';
      await tester.pumpWidget(const OrbitApp());
      await tester.pumpAndSettle();
      await tester.tap(find.byTooltip('添加到桌面').first);
      await tester.pumpAndSettle();
      expect(find.text('请从桌面添加组件'), findsOneWidget);
      expect(find.text('去应用信息'), findsNothing);
      expect(find.textContaining('这不是 Orbit 缺少运行时权限'), findsOneWidget);
      await tester.tap(find.text('关闭'));
      await tester.pumpAndSettle();
      expect(calls.where((c) => c.method == 'appSettings'), isEmpty);
      await tester.pumpWidget(const SizedBox());
    },
  );

  for (final accepted in [false, true]) {
    testWidgets('pin $accepted only shows permission help when needed', (
      tester,
    ) async {
      pinResult = accepted ? 'requested' : 'rejected';
      await tester.pumpWidget(const OrbitApp());
      await tester.pumpAndSettle();
      await tester.tap(find.byTooltip('添加到桌面').first);
      await tester.pumpAndSettle();
      if (accepted) {
        expect(find.text('添加桌面组件'), findsNothing);
        expect(find.text('没有弹窗？'), findsOneWidget);
        await tester.tap(find.text('没有弹窗？'));
        await tester.pumpAndSettle();
      }
      expect(find.text('添加桌面组件'), findsOneWidget);
      expect(calls.where((call) => call.method == 'appSettings'), isEmpty);
      await tester.tap(find.text('去应用信息'));
      await tester.pumpAndSettle();
      expect(calls.where((call) => call.method == 'appSettings'), hasLength(1));
      await tester.pumpWidget(const SizedBox());
    });
  }
}
