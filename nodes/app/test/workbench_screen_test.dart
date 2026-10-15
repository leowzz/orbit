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
  String? deviceModel;
  setUp(() async {
    dir = await Directory.systemTemp.createTemp('orbit-workbench-');
    controller = InboxController(
      await LocalStore.open(databaseFactoryFfi, '${dir.path}/test.sqlite'),
      'https://example.test',
      'device-token',
      dir.path,
      client: MockClient((request) async {
        requests.add(request);
        if (request.method == 'GET' &&
            request.url.path == '/api/v1/navigation') {
          return http.Response(jsonEncode(controller.navigation), 200);
        }
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
    deviceModel = null;
    TestDefaultBinaryMessengerBinding.instance.defaultBinaryMessenger
        .setMockMethodCallHandler(const MethodChannel('dev.orbit/node'), (
          call,
        ) async {
          if (call.method == 'deviceModel') return deviceModel;
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
    testWidgets('navigation position swaps with status bar live at $size', (
      tester,
    ) async {
      tester.view.physicalSize = size;
      tester.view.devicePixelRatio = 1;
      addTearDown(tester.view.reset);
      controller.navigation = {
        'items': [
          {'id': 'calendar', 'title': '日历', 'icon_name': 'app'},
        ],
      };
      await tester.pumpWidget(
        MaterialApp(
          builder: (_, child) => AppViewport(child: child!),
          home: WorkbenchScreen(controller: controller),
        ),
      );
      void expectPosition(bool atTop) {
        final nav = tester.getRect(
          find.byKey(const ValueKey('workbench-navigation')),
        );
        final status = tester.getRect(
          find.byKey(const ValueKey('workbench-status-bar')),
        );
        final session = tester.getRect(
          find.byKey(const ValueKey('session-session-0')),
        );
        expect(atTop ? nav.top : status.top, 0);
        expect(atTop ? status.bottom : nav.bottom, closeTo(size.height, 0.01));
        expect(
          session.top,
          greaterThanOrEqualTo(atTop ? nav.bottom : status.bottom),
        );
        final shortcut = tester.getRect(
          find.byKey(const ValueKey('shortcut-calendar')),
        );
        expect(shortcut.center.dx, nav.center.dx);
        expect(tester.takeException(), isNull);
      }

      // Older catalogs omit the setting and keep the bottom navigation.
      expectPosition(false);
      for (final atTop in [true, false]) {
        controller.navigation['at_top'] = atTop;
        await tester.runAsync(controller.refreshNavigation);
        await tester.pumpAndSettle();
        expectPosition(atTop);
        await tester.tap(find.byKey(const ValueKey('workbench-fullscreen')));
        await tester.pumpAndSettle();
        expectPosition(atTop);
        await tester.tap(find.byKey(const ValueKey('shortcut-calendar')));
        await tester.pumpAndSettle();
        expect(requests.last.url.path, '/api/v1/navigation/calendar/open');
        await tester.tap(find.byKey(const ValueKey('session-session-0')));
        await tester.pumpAndSettle();
        expect(requests.last.url.path, '/api/v1/sessions/session-0/open');
        await tester.tap(find.byKey(const ValueKey('workbench-fullscreen')));
        await tester.pumpAndSettle();
      }
      expect(calls, [true, false, true, false]);
      await tester.pumpWidget(const SizedBox());
    });
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
  for (final size in [
    const Size(640, 280),
    const Size(800, 360),
    const Size(360, 800),
  ]) {
    testWidgets('session cards fit long details and larger text at $size', (
      tester,
    ) async {
      tester.view.physicalSize = size;
      tester.view.devicePixelRatio = 1;
      addTearDown(tester.view.reset);
      final sessions = (controller.view!['codex']['sessions'] as List)
          .take(3)
          .toList();
      sessions[0]['displayName'] = '优化工作台会话信息的显示效果';
      sessions[0]['projectName'] = 'a-project-with-a-long-name';
      sessions[0]['model'] = 'gpt-6.1-model-with-a-long-name';
      sessions[2]['status'] = 'CODEX_SESSION_STATUS_FAILED';
      controller.view!['codex']['sessions'] = sessions;
      await tester.pumpWidget(
        MaterialApp(
          builder: (context, child) => MediaQuery(
            data: MediaQuery.of(
              context,
            ).copyWith(textScaler: const TextScaler.linear(1.5)),
            child: AppViewport(child: child!),
          ),
          home: WorkbenchScreen(controller: controller),
        ),
      );
      await tester.pumpAndSettle();
      for (var i = 0; i < sessions.length; i++) {
        final card = find.byKey(ValueKey('session-session-$i'));
        await tester.scrollUntilVisible(
          card,
          100,
          scrollable: find.descendant(
            of: find.byKey(const PageStorageKey('workbench-page')),
            matching: find.byType(Scrollable),
          ),
        );
        await tester.pumpAndSettle();
        final bounds = tester.getRect(card);
        for (final text
            in find
                .descendant(of: card, matching: find.byType(Text))
                .evaluate()) {
          final rect = tester.getRect(
            find.descendant(of: card, matching: find.byWidget(text.widget)),
          );
          expect(rect.left, greaterThanOrEqualTo(bounds.left));
          expect(rect.right, lessThanOrEqualTo(bounds.right + 0.01));
          expect(rect.top, greaterThanOrEqualTo(bounds.top));
          expect(rect.bottom, lessThanOrEqualTo(bounds.bottom + 0.01));
        }
      }
      expect(find.text('失败'), findsOneWidget);
      expect(find.text('session-0'), findsNothing);
      expect(tester.takeException(), isNull);
      await tester.pumpWidget(const SizedBox());
    });
  }
  for (final atTop in [false, true]) {
    testWidgets(
      'Alipay Box shows exactly four rows with navigation at top=$atTop',
      (tester) async {
        deviceModel = 'NFCD_AlipayBoxN5DEL';
        tester.view.physicalSize = const Size(1600, 720);
        tester.view.devicePixelRatio = 2;
        tester.view.padding = const FakeViewPadding(top: 48, bottom: 96);
        addTearDown(tester.view.reset);
        controller.navigation['at_top'] = atTop;
        await tester.pumpWidget(
          MaterialApp(
            builder: (_, child) => AppViewport(child: child!),
            home: OrbitHome(
              widgetsEnabled: false,
              controller: controller,
              inbox: const Text('消息'),
            ),
          ),
        );
        await tester.tap(find.text('工作台'));
        await tester.pumpAndSettle();
        void expectFourRows() {
          final viewport = tester.getRect(
            find.byKey(const PageStorageKey('workbench-sessions')),
          );
          final cards = List.generate(
            8,
            (i) => tester.getRect(find.byKey(ValueKey('session-session-$i'))),
          );
          expect(cards.first.top, closeTo(viewport.top, 0.01));
          expect(cards.last.bottom, closeTo(viewport.bottom, 0.01));
          for (var row = 0; row < 4; row++) {
            final left = cards[row * 2], right = cards[row * 2 + 1];
            expect(left.top, right.top);
            expect(left.height, closeTo(cards.first.height, 0.01));
            expect(right.bottom, closeTo(left.bottom, 0.01));
            expect(left.right, lessThan(right.left));
          }
          final ninth = find.byKey(const ValueKey('session-session-8'));
          if (ninth.evaluate().isNotEmpty) {
            expect(tester.getRect(ninth).top, greaterThan(viewport.bottom));
          }
          expect(tester.takeException(), isNull);
        }

        expectFourRows();
        await tester.tap(find.byKey(const ValueKey('workbench-fullscreen')));
        tester.view.padding = FakeViewPadding.zero;
        await tester.pumpAndSettle();
        expectFourRows();
        await tester.drag(
          find.byKey(const PageStorageKey('workbench-sessions')),
          const Offset(0, -250),
        );
        await tester.pumpAndSettle();
        expect(
          find.byKey(const ValueKey('session-session-8')).hitTestable(),
          findsOneWidget,
        );
        expect(tester.takeException(), isNull);
        await tester.pumpWidget(const SizedBox());
      },
    );
  }
  testWidgets('same resolution on another Android model keeps adaptive cards', (
    tester,
  ) async {
    deviceModel = 'Another Android';
    tester.view.physicalSize = const Size(1600, 720);
    tester.view.devicePixelRatio = 2;
    addTearDown(tester.view.reset);
    await tester.pumpWidget(
      MaterialApp(
        builder: (_, child) => AppViewport(child: child!),
        home: WorkbenchScreen(controller: controller),
      ),
    );
    await tester.pumpAndSettle();
    expect(find.byType(SliverGrid), findsNothing);
    expect(find.byType(IntrinsicHeight), findsWidgets);
    expect(tester.takeException(), isNull);
    await tester.pumpWidget(const SizedBox());
  });
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
  for (final model in ['NFCD_AlipayBoxN5DEL', 'other-device']) {
    testWidgets(
      'NFCD padding follows device navigation with legacy view fallback on $model',
      (tester) async {
        deviceModel = model;
        tester.view.physicalSize = const Size(800, 360);
        tester.view.devicePixelRatio = 1;
        addTearDown(tester.view.resetPhysicalSize);
        addTearDown(tester.view.resetDevicePixelRatio);
        controller.view = fixture();
        controller.navigation = {'items': []};
        await tester.pumpWidget(
          MaterialApp(home: WorkbenchScreen(controller: controller)),
        );
        await tester.pumpAndSettle();
        double left() => tester.getTopLeft(find.text('剩余额度')).dx;
        final initial = left();
        final nfcd = model == 'NFCD_AlipayBoxN5DEL';
        controller.navigation['nfcd_metrics_left_padding'] = 0;
        controller.notifyListeners();
        await tester.pumpAndSettle();
        expect(left(), initial - (nfcd ? 16 : 0));
        controller.view!['nfcdMetricsLeftPadding'] = 64;
        controller.notifyListeners();
        await tester.pumpAndSettle();
        expect(left(), initial - (nfcd ? 16 : 0));
        controller.view!['nfcdMetricsLeftPadding'] = 0;
        controller.navigation['nfcd_metrics_left_padding'] = 32;
        controller.notifyListeners();
        await tester.pumpAndSettle();
        expect(left(), initial + (nfcd ? 16 : 0));
        controller.view!.remove('nfcdMetricsLeftPadding');
        controller.notifyListeners();
        await tester.pumpAndSettle();
        expect(left(), initial + (nfcd ? 16 : 0));
        controller.navigation['nfcd_metrics_left_padding'] = 100;
        controller.notifyListeners();
        await tester.pumpAndSettle();
        expect(left(), initial);
        expect(tester.takeException(), isNull);
        await tester.pumpWidget(const SizedBox());
      },
    );
  }

  testWidgets('render short landscape and portrait previews', (tester) async {
    // Use a local system font for optional review artifacts, never a test dependency.
    final font = Platform.environment['WORKBENCH_PREVIEW_FONT'];
    if (font == null || !File(font).existsSync()) return;
    await tester.runAsync(() async {
      final bytes = await File(font).readAsBytes();
      final loader = FontLoader('Preview')
        ..addFont(Future.value(ByteData.sublistView(bytes)));
      await loader.load();
      await (FontLoader(
        'MaterialIcons',
      )..addFont(rootBundle.load('fonts/MaterialIcons-Regular.otf'))).load();
    });
    for (final atTop in [false, true]) {
      controller.navigation = {
        'at_top': atTop,
        'items': [
          {'id': 'calendar', 'title': '日历', 'icon_name': 'app'},
          {'id': 'website', 'title': '网站', 'icon_name': 'globe'},
          {'id': 'folder', 'title': '文件夹', 'icon_name': 'folder'},
        ],
      };
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
            '${output.path}/${size.width.toInt()}x${size.height.toInt()}-${atTop ? 'top' : 'bottom'}.png',
          ).writeAsBytes(data!.buffer.asUint8List());
          image.dispose();
        });
        expect(tester.takeException(), isNull);
      }
    }
    tester.view.reset();
    await tester.pumpWidget(const SizedBox());
  });
}
