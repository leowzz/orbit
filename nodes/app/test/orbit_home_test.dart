import 'package:flutter/material.dart';
import 'package:flutter/services.dart';
import 'package:flutter_test/flutter_test.dart';
import 'package:orbit_app/orbit_home.dart';

void main() {
  const channel = MethodChannel('dev.orbit/node');
  final calls = <String>[];
  TestWidgetsFlutterBinding.ensureInitialized();
  setUp(() {
    calls.clear();
    TestDefaultBinaryMessengerBinding.instance.defaultBinaryMessenger
        .setMockMethodCallHandler(channel, (call) async {
          calls.add(call.method);
          if (call.method == 'load') {
            return {
              'uri': 'ssl://example.com:8883',
              'nodeId': 'existing-phone',
              'username': '',
              'password': '',
            };
          }
          if (call.method == 'snapshot') {
            return {'active': false, 'connection': '未连接'};
          }
          return null;
        });
  });
  tearDown(
    () => TestDefaultBinaryMessengerBinding.instance.defaultBinaryMessenger
        .setMockMethodCallHandler(channel, null),
  );

  testWidgets(
    'Android keeps the inbox draft and existing widget settings across tabs',
    (tester) async {
      await tester.pumpWidget(
        const MaterialApp(
          home: OrbitHome(
            widgetsEnabled: true,
            inbox: Scaffold(
              body: TextField(decoration: InputDecoration(labelText: '收件箱草稿')),
            ),
          ),
        ),
      );
      await tester.pumpAndSettle();
      await tester.enterText(find.byType(TextField).first, '保留草稿');
      await tester.tap(find.text('状态与组件'));
      await tester.pumpAndSettle();
      expect(find.text('开始同步').hitTestable(), findsOneWidget);
      expect(calls.where((method) => method == 'load'), hasLength(1));
      await tester.tap(find.text('开始同步'));
      await tester.pumpAndSettle();
      expect(calls, contains('start'));
      await tester.tap(find.text('收件箱'));
      await tester.pumpAndSettle();
      expect(find.text('保留草稿').hitTestable(), findsOneWidget);
      expect(tester.takeException(), isNull);
      await tester.pumpWidget(const SizedBox());
    },
  );

  testWidgets('Desktop does not load Android widget services', (tester) async {
    await tester.pumpWidget(
      const MaterialApp(
        home: OrbitHome(widgetsEnabled: false, inbox: Text('消息')),
      ),
    );
    expect(find.text('消息'), findsOneWidget);
    expect(find.byType(NavigationBar), findsNothing);
    expect(calls, isEmpty);
  });
}
