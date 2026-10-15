import 'dart:convert';
import 'dart:io';
import 'dart:ui' as ui;
import 'package:flutter/material.dart';
import 'package:flutter/rendering.dart';
import 'package:flutter/services.dart';
import 'package:flutter_test/flutter_test.dart';
import 'package:http/http.dart' as http;
import 'package:http/testing.dart';
import 'package:orbit_app/app_viewport.dart';
import 'package:orbit_app/inbox_controller.dart';
import 'package:orbit_app/local_store.dart';
import 'package:orbit_app/orbit_home.dart';
import 'package:orbit_app/workbench_screen.dart';
import 'package:sqflite_common_ffi/sqflite_ffi.dart';

Json fixture() => {
  'coreEpoch': 'epoch-a',
  'metadata': {
    'revision': '42',
    'producedAt': DateTime.now().toIso8601String(),
  },
  'displayKind': 'codex',
  'sessionsEnabled': true,
  'freshUntil': DateTime.now()
      .add(const Duration(minutes: 2))
      .toIso8601String(),
  'primary': {'text': '72%'},
  'secondary': {'text': '4 10/19'},
  'footer': {'text': '3 12:30'},
  'codex': {
    'runningCount': 2,
    'totalCount': 20,
    'sessions': List.generate(
      20,
      (i) => {
        'sessionId': 'session-$i',
        'displayName': [
          '完善工作台布局与交互',
          '检查同步状态和消息展示',
          '整理项目文档',
          '修复页面滚动问题',
        ][i % 4],
        'projectName': 'orbit',
        'model': 'gpt-6',
        'status': i < 2
            ? 'CODEX_SESSION_STATUS_RUNNING'
            : 'CODEX_SESSION_STATUS_COMPLETED',
        'updatedAt': DateTime.now()
            .subtract(Duration(minutes: i))
            .toIso8601String(),
      },
    ),
  },
};

void main() {
  TestWidgetsFlutterBinding.ensureInitialized();
  sqfliteFfiInit();
  late Directory dir;
  late InboxController controller;
  final calls = <bool>[];
  final requests = <http.Request>[];
  var fail = false;
  setUp(() async {
    dir = await Directory.systemTemp.createTemp('orbit-workbench-');
    controller = InboxController(
      await LocalStore.open(databaseFactoryFfi, '${dir.path}/test.sqlite'),
      'https://example.test',
      'device-token',
      dir.path,
      client: MockClient((request) async {
        requests.add(request);
        return http.Response(
          fail ? '{"code":"session_unavailable"}' : '{"status":"accepted"}',
          fail ? 409 : 202,
        );
      }),
    );
    controller.view = fixture();
    controller.online = true;
    calls.clear();
    requests.clear();
    fail = false;
    TestDefaultBinaryMessengerBinding.instance.defaultBinaryMessenger
        .setMockMethodCallHandler(const MethodChannel('dev.orbit/node'), (
          call,
        ) async {
          if (call.method == 'matrixFullscreen') {
            calls.add(call.arguments as bool);
          }
          return null;
        });
  });
  tearDown(() async {
    await controller.close();
    await dir.delete(recursive: true);
    TestDefaultBinaryMessengerBinding.instance.defaultBinaryMessenger
        .setMockMethodCallHandler(const MethodChannel('dev.orbit/node'), null);
  });
  testWidgets('data display shows session counts without the workbench list', (
    tester,
  ) async {
    (controller.view!['codex'] as Map)['totalCount'] = 123;
    await tester.pumpWidget(
      MaterialApp(
        home: OrbitHome(
          widgetsEnabled: false,
          controller: controller,
          inbox: const Text('消息'),
        ),
      ),
    );
    await tester.tap(find.widgetWithText(NavigationDestination, '数据展示'));
    await tester.pumpAndSettle();
    expect(find.text('2 个运行中 · 共 123 个'), findsOneWidget);
    expect(find.text('完善工作台布局与交互'), findsNothing);
    expect(find.text('检查同步状态和消息展示'), findsNothing);
    await tester.tap(find.widgetWithText(NavigationDestination, '工作台'));
    await tester.pumpAndSettle();
    expect(find.byKey(const ValueKey('session-session-0')), findsOneWidget);
    expect(find.text('完善工作台布局与交互'), findsWidgets);
    expect(tester.takeException(), isNull);
    await tester.pumpWidget(const SizedBox());
  });

  for (final size in [
    const Size(800, 360),
    const Size(640, 280),
    const Size(360, 800),
    const Size(1200, 800),
  ]) {
    testWidgets(
      'workbench navigation, fullscreen, commands and layout at $size',
      (tester) async {
        tester.view.physicalSize = size;
        tester.view.devicePixelRatio = 1;
        addTearDown(tester.view.reset);
        await tester.pumpWidget(
          MaterialApp(
            builder: (_, child) => AppViewport(child: child!),
            home: OrbitHome(
              widgetsEnabled: false,
              controller: controller,
              inbox: const TextField(),
            ),
          ),
        );
        await tester.enterText(find.byType(TextField), '保留草稿');
        await tester.tap(find.text('工作台'));
        await tester.pumpAndSettle();
        if (find.byType(NavigationRail).evaluate().isNotEmpty) {
          expect(
            tester
                .widget<NavigationRail>(find.byType(NavigationRail))
                .destinations[1]
                .label,
            isA<Text>().having((t) => t.data, 'label', '工作台'),
          );
        }
        expect(find.text('72%'), findsOneWidget);
        expect(find.text('4d3h'), findsOneWidget);
        expect(tester.takeException(), isNull);
        await tester.tap(find.byKey(const ValueKey('workbench-fullscreen')));
        await tester.pumpAndSettle();
        expect(calls, [true]);
        if (size == const Size(800, 360)) {
          final first = tester.getRect(
            find.byKey(const ValueKey('session-session-0')),
          );
          final second = tester.getRect(
            find.byKey(const ValueKey('session-session-1')),
          );
          expect(first.top, second.top);
          expect(first.height, second.height);
          expect(first.right, lessThan(second.left));
        }
        expect(find.byType(NavigationRail), findsNothing);
        expect(find.byType(NavigationBar), findsNothing);
        final card = find.byKey(const ValueKey('session-session-0'));
        await tester.ensureVisible(card);
        await tester.tap(card);
        await tester.pumpAndSettle();
        expect(requests.single.url.path, '/api/v1/sessions/session-0/open');
        expect(requests.single.headers['Authorization'], 'Bearer device-token');
        expect(jsonDecode(requests.single.body), {
          'core_epoch': 'epoch-a',
          'view_revision': '42',
        });
        expect(find.byType(SnackBar), findsNothing);
        expect(find.byTooltip('退出全屏'), findsOneWidget);
        expect(calls, [true]); // Session taps never leave full screen.
        fail = true;
        await tester.tap(card);
        await tester.pumpAndSettle();
        expect(find.text('会话状态已变化，请同步后重试'), findsOneWidget);
        await tester.tap(find.byKey(const ValueKey('workbench-fullscreen')));
        await tester.pumpAndSettle();
        expect(calls, [true, false]);
        await tester.tap(find.text('收件箱'));
        await tester.pumpAndSettle();
        expect(find.text('保留草稿'), findsOneWidget);
        expect(tester.takeException(), isNull);
        await tester.pumpWidget(const SizedBox());
      },
    );
  }
  testWidgets('session switch clears cards and offline open is not queued', (
    tester,
  ) async {
    await tester.pumpWidget(
      MaterialApp(home: WorkbenchScreen(controller: controller)),
    );
    controller.online = false;
    await tester.tap(find.byKey(const ValueKey('session-session-0')));
    await tester.pumpAndSettle();
    expect(requests, isEmpty);
    controller.view = {...fixture(), 'sessionsEnabled': false};
    await tester.pumpWidget(
      MaterialApp(
        home: WorkbenchScreen(
          key: const ValueKey('off'),
          controller: controller,
        ),
      ),
    );
    expect(find.byKey(const ValueKey('session-session-0')), findsNothing);
    await tester.pumpWidget(const SizedBox());
  });
  testWidgets(
    'old Core reports missing session action instead of connection failure',
    (tester) async {
      final oldCore =
          InboxController(
              controller.local,
              controller.server,
              controller.token,
              dir.path,
              client: MockClient(
                (_) async => http.Response('404 page not found', 404),
              ),
            )
            ..online = true
            ..view = fixture();
      await tester.pumpWidget(
        MaterialApp(home: WorkbenchScreen(controller: oldCore)),
      );
      await tester.tap(find.byKey(const ValueKey('session-session-0')));
      await tester.pumpAndSettle();
      expect(find.text('当前服务不支持打开会话，请更新 Core 后重试'), findsOneWidget);
      expect(oldCore.online, isTrue);
      await tester.pumpWidget(const SizedBox());
      oldCore.client.close();
    },
  );
  testWidgets('render short landscape and portrait previews', (tester) async {
    // Use a local system font for optional review artifacts, never a test dependency.
    final font = Platform.environment['WORKBENCH_PREVIEW_FONT'];
    if (font == null || !File(font).existsSync()) return;
    await tester.runAsync(() async {
      final bytes = await File(font).readAsBytes();
      final loader = FontLoader('Preview')
        ..addFont(Future.value(ByteData.sublistView(bytes)));
      await loader.load();
    });
    for (final size in [const Size(800, 360), const Size(360, 800)]) {
      tester.view.physicalSize = size;
      tester.view.devicePixelRatio = 1;
      await tester.pumpWidget(
        MaterialApp(
          theme: ThemeData(fontFamily: 'Preview'),
          builder: (_, child) => AppViewport(child: child!),
          home: RepaintBoundary(
            key: const ValueKey('preview'),
            child: WorkbenchScreen(controller: controller),
          ),
        ),
      );
      await tester.pumpAndSettle();
      final boundary = tester.renderObject<RenderRepaintBoundary>(
        find.byKey(const ValueKey('preview')),
      );
      await tester.runAsync(() async {
        final image = await boundary.toImage();
        final data = await image.toByteData(format: ui.ImageByteFormat.png);
        final output = Directory('build/workbench-preview')
          ..createSync(recursive: true);
        await File(
          '${output.path}/${size.width.toInt()}x${size.height.toInt()}.png',
        ).writeAsBytes(data!.buffer.asUint8List());
        image.dispose();
      });
      expect(tester.takeException(), isNull);
    }
    tester.view.reset();
    await tester.pumpWidget(const SizedBox());
  });
}
