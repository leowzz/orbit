import 'package:flutter/material.dart';
import 'package:flutter/services.dart';
import 'package:flutter_test/flutter_test.dart';
import 'package:orbit_android/main.dart';

void main() {
  TestWidgetsFlutterBinding.ensureInitialized();
  const channel = MethodChannel('dev.orbit/node');
  final calls = <MethodCall>[];
  setUp(() {
    calls.clear();
    TestDefaultBinaryMessengerBinding.instance.defaultBinaryMessenger
        .setMockMethodCallHandler(channel, (call) async {
          calls.add(call);
          if (call.method == 'snapshot') {
            return {'connection': '未连接', 'active': false};
          }
          if (call.method == 'test') return '连接成功';
          return null;
        });
  });
  tearDown(
    () => TestDefaultBinaryMessengerBinding.instance.defaultBinaryMessenger
        .setMockMethodCallHandler(channel, null),
  );

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
}
