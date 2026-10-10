import 'package:flutter/material.dart';
import 'package:flutter/services.dart';
import 'package:flutter_test/flutter_test.dart';
import 'package:orbit_app/orbit_home.dart';

void main() {
  const channel = MethodChannel('dev.orbit/node');
  TestWidgetsFlutterBinding.ensureInitialized();
  final calls = <String>[];
  String pinResult = 'requested';
  bool fail = false;

  setUp(() {
    calls.clear();
    pinResult = 'requested';
    fail = false;
    TestDefaultBinaryMessengerBinding.instance.defaultBinaryMessenger
        .setMockMethodCallHandler(channel, (call) async {
          calls.add(call.method);
          if (fail) throw PlatformException(code: 'operation_failed');
          return call.method == 'pinInputMethodShortcut' ? pinResult : null;
        });
  });
  tearDown(
    () => TestDefaultBinaryMessengerBinding.instance.defaultBinaryMessenger
        .setMockMethodCallHandler(channel, null),
  );

  Future<void> open(WidgetTester tester) async {
    tester.view.physicalSize = const Size(360, 800);
    tester.view.devicePixelRatio = 1;
    addTearDown(tester.view.resetPhysicalSize);
    addTearDown(tester.view.resetDevicePixelRatio);
    await tester.pumpWidget(
      const MaterialApp(
        home: OrbitHome(widgetsEnabled: true, inbox: Text('消息')),
      ),
    );
    await tester.tap(find.text('状态与组件'));
    await tester.pumpAndSettle();
  }

  testWidgets('picker works without an inbox or widget connection', (
    tester,
  ) async {
    await open(tester);
    await tester.tap(find.widgetWithText(FilledButton, '切换输入法'));
    await tester.pumpAndSettle();
    expect(calls, ['showInputMethodPicker']);
    expect(tester.takeException(), isNull);
  });

  for (final result in ['requested', 'unsupported', 'rejected']) {
    testWidgets('shortcut reports $result accurately', (tester) async {
      pinResult = result;
      await open(tester);
      await tester.tap(find.text('添加到桌面'));
      await tester.pumpAndSettle();
      expect(calls, ['pinInputMethodShortcut']);
      final text = switch (result) {
        'requested' => '添加请求已发送',
        'unsupported' => '当前桌面不支持',
        _ => '桌面未接受添加请求',
      };
      expect(find.textContaining(text), findsOneWidget);
      expect(find.textContaining('已添加'), findsNothing);
      if (result == 'unsupported') {
        expect(find.text('权限设置'), findsNothing);
      } else {
        await tester.tap(find.text('权限设置'));
        await tester.pumpAndSettle();
        expect(calls.last, 'appSettings');
      }
      expect(tester.takeException(), isNull);
    });
  }

  testWidgets('failed picker can be retried', (tester) async {
    await open(tester);
    fail = true;
    await tester.tap(find.widgetWithText(FilledButton, '切换输入法'));
    await tester.pumpAndSettle();
    expect(find.text('无法打开输入法列表，请重试。'), findsOneWidget);
    fail = false;
    await tester.tap(find.widgetWithText(FilledButton, '切换输入法'));
    await tester.pumpAndSettle();
    expect(calls, ['showInputMethodPicker', 'showInputMethodPicker']);
  });
}
