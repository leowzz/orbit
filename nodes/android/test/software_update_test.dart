import 'dart:async';

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
    await tester.tap(find.text('重试检查'));
    await tester.pumpAndSettle();
    expect(find.text('无法连接更新服务，请重试'), findsNothing);
    expect(find.text('发现新版本 0.3.2'), findsOneWidget);
    await tester.pumpWidget(const SizedBox());
  });

  testWidgets('failed check unlocks retry and ignores late busy polls', (
    tester,
  ) async {
    final check = Completer<Map<String, dynamic>>();
    final latePoll = Completer<Map<String, dynamic>>();
    var polls = 0;
    var checks = 0;
    TestDefaultBinaryMessengerBinding.instance.defaultBinaryMessenger
        .setMockMethodCallHandler(channel, (call) async {
          if (call.method == 'checkUpdate') {
            checks++;
            if (checks == 1) return check.future;
            phase = 'idle';
            return status();
          }
          polls++;
          if (polls == 1) return status();
          if (polls == 2) {
            phase = 'checking';
            return status();
          }
          return latePoll.future;
        });
    await tester.pumpWidget(
      const MaterialApp(
        home: Scaffold(body: SoftwareUpdateCard(enabled: true)),
      ),
    );
    await tester.pump();
    await tester.pump(const Duration(milliseconds: 500));
    expect(find.text('正在检查更新…'), findsOneWidget);
    expect(
      tester.widget<OutlinedButton>(find.byType(OutlinedButton)).onPressed,
      isNull,
    );
    await tester.pump(const Duration(milliseconds: 500));
    check.completeError(
      PlatformException(code: 'update_failed', message: '更新服务暂不可用，请稍后重试'),
    );
    await tester.pumpAndSettle();
    expect(find.text('重试检查'), findsOneWidget);
    expect(find.text('正在检查更新…'), findsNothing);
    expect(
      tester.widget<OutlinedButton>(find.byType(OutlinedButton)).onPressed,
      isNotNull,
    );
    latePoll.complete(status());
    await tester.pumpAndSettle();
    expect(find.text('正在检查更新…'), findsNothing);
    await tester.tap(find.text('重试检查'));
    await tester.pumpAndSettle();
    expect(checks, 2);
    expect(find.text('更新服务暂不可用，请稍后重试'), findsNothing);
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
