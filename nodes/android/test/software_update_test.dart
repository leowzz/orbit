import 'package:flutter/material.dart';
import 'package:flutter/services.dart';
import 'package:flutter_test/flutter_test.dart';
import 'package:orbit_android/software_update.dart';

void main() {
  const channel = MethodChannel('dev.orbit/node');
  TestWidgetsFlutterBinding.ensureInitialized();
  final calls = <String>[];
  var phase = 'idle';
  var fail = false;
  Map<String, dynamic> status() => {
    'currentVersion': '0.3.1',
    'latestVersion': '0.3.2',
    'supported': true,
    'available': true,
    'phase': phase,
    'downloaded': 0,
    'total': 10,
    'error': '',
  };
  setUp(() {
    calls.clear();
    phase = 'idle';
    fail = false;
    TestDefaultBinaryMessengerBinding.instance.defaultBinaryMessenger
        .setMockMethodCallHandler(channel, (call) async {
          calls.add(call.method);
          if (fail && call.method == 'checkUpdate') {
            throw PlatformException(code: 'network', message: '无法连接更新服务，请重试');
          }
          if (call.method == 'installUpdate') phase = 'permission';
          return status();
        });
  });
  tearDown(
    () => TestDefaultBinaryMessengerBinding.instance.defaultBinaryMessenger
        .setMockMethodCallHandler(channel, null),
  );

  testWidgets('checks on startup and hands installation to Android', (
    tester,
  ) async {
    await tester.pumpWidget(
      const MaterialApp(
        home: Scaffold(body: SoftwareUpdateCard(enabled: true)),
      ),
    );
    await tester.pumpAndSettle();
    expect(calls, contains('checkUpdate'));
    expect(find.text('发现新版本 0.3.2'), findsOneWidget);
    await tester.tap(find.text('立即更新'));
    await tester.pumpAndSettle();
    expect(calls, contains('installUpdate'));
    expect(find.text('允许安装并继续'), findsOneWidget);
    expect(find.textContaining('请允许安装应用'), findsOneWidget);
    expect(find.text('更新成功'), findsNothing);
    await tester.pumpWidget(const SizedBox());
  });

  testWidgets('failed checks offer retry without claiming latest', (
    tester,
  ) async {
    fail = true;
    await tester.pumpWidget(
      const MaterialApp(
        home: Scaffold(body: SoftwareUpdateCard(enabled: true)),
      ),
    );
    await tester.pumpAndSettle();
    expect(find.text('无法连接更新服务，请重试'), findsOneWidget);
    fail = false;
    await tester.tap(find.text('检查更新'));
    await tester.pumpAndSettle();
    expect(find.text('无法连接更新服务，请重试'), findsNothing);
    expect(find.text('发现新版本 0.3.2'), findsOneWidget);
    await tester.pumpWidget(const SizedBox());
  });

  testWidgets('update card wraps actions on a narrow screen', (tester) async {
    tester.view.physicalSize = const Size(300, 500);
    tester.view.devicePixelRatio = 1;
    addTearDown(tester.view.reset);
    await tester.pumpWidget(
      const MaterialApp(
        home: Scaffold(
          body: SingleChildScrollView(child: SoftwareUpdateCard(enabled: true)),
        ),
      ),
    );
    await tester.pumpAndSettle();
    expect(tester.takeException(), isNull);
    expect(find.text('立即更新'), findsOneWidget);
    await tester.pumpWidget(const SizedBox());
  });
}
