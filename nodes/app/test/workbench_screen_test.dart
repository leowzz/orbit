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
import 'package:orbit_app/app_theme.dart';
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
    'totalCount': 40,
    'sessions': List.generate(
      40,
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
          if (call.method == 'workbenchFullscreen') {
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
  testWidgets('workbench follows system theme with existing session data', (
    tester,
  ) async {
    final platform = tester.binding.platformDispatcher;
    addTearDown(platform.clearPlatformBrightnessTestValue);
    platform.platformBrightnessTestValue = Brightness.light;
    await tester.pumpWidget(
      MaterialApp(
        theme: orbitTheme(Brightness.light),
        darkTheme: orbitTheme(Brightness.dark),
        home: WorkbenchScreen(controller: controller),
      ),
    );
    await tester.pumpAndSettle();
    for (final brightness in [Brightness.dark, Brightness.light]) {
      platform.platformBrightnessTestValue = brightness;
      await tester.pumpAndSettle();
      final session = find.byKey(const ValueKey('session-session-0'));
      final text = tester.widget<Text>(
        find.descendant(of: session, matching: find.text('完善工作台布局与交互')),
      );
      final material = tester.widget<Material>(
        find.ancestor(of: session, matching: find.byType(Material)).first,
      );
      final colors = OrbitColors(brightness == Brightness.dark);
      expect(text.style!.color, colors.ink);
      expect(material.color, colors.runningSurface);
      expect(tester.takeException(), isNull);
    }
    await tester.pumpWidget(const SizedBox());
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
    testWidgets('Alipay Box fits complete rows with navigation at top=$atTop', (
      tester,
    ) async {
      final font = Platform.environment['WORKBENCH_PREVIEW_FONT'];
      if (font != null && File(font).existsSync()) {
        await tester.runAsync(() async {
          final bytes = await File(font).readAsBytes();
          await (FontLoader(
            'Preview',
          )..addFont(Future.value(ByteData.sublistView(bytes)))).load();
        });
      }
      deviceModel = 'NFCD_AlipayBoxN5DEL';
      tester.view.physicalSize = const Size(1600, 720);
      tester.view.devicePixelRatio = 2;
      tester.view.padding = const FakeViewPadding(top: 48, bottom: 96);
      addTearDown(tester.view.reset);
      controller.navigation['at_top'] = atTop;
      await tester.pumpWidget(
        MaterialApp(
          theme: orbitTheme(Brightness.light).copyWith(
            textTheme: orbitTheme(
              Brightness.light,
            ).textTheme.apply(fontFamily: font == null ? null : 'Preview'),
          ),
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
      void expectRows(int rows) {
        final viewport = tester.getRect(
          find.byKey(const PageStorageKey('workbench-sessions')),
        );
        final cards = List.generate(
          rows * 2,
          (i) => tester.getRect(find.byKey(ValueKey('session-session-$i'))),
        );
        expect(cards.first.top, closeTo(viewport.top, 0.01));
        expect(cards.last.bottom, closeTo(viewport.bottom, 0.01));
        for (var row = 0; row < rows; row++) {
          final left = cards[row * 2], right = cards[row * 2 + 1];
          expect(left.top, right.top);
          expect(left.height, closeTo(cards.first.height, 0.01));
          expect(right.bottom, closeTo(left.bottom, 0.01));
          expect(left.right, lessThan(right.left));
        }
        final next = find.byKey(ValueKey('session-session-${rows * 2}'));
        if (next.evaluate().isNotEmpty) {
          expect(tester.getRect(next).top, greaterThan(viewport.bottom));
        }
        expect(tester.takeException(), isNull);
      }

      expectRows(4);
      await tester.tap(find.byKey(const ValueKey('workbench-fullscreen')));
      tester.view.padding = FakeViewPadding.zero;
      await tester.pumpAndSettle();
      expectRows(5);
      await tester.drag(
        find.byKey(const PageStorageKey('workbench-sessions')),
        const Offset(0, -250),
      );
      await tester.pumpAndSettle();
      expect(
        find.byKey(const ValueKey('session-session-12')).hitTestable(),
        findsOneWidget,
      );
      expect(tester.takeException(), isNull);
      await tester.pumpWidget(const SizedBox());
    });
  }
  testWidgets('short non-fullscreen cards tolerate larger system insets', (
    tester,
  ) async {
    deviceModel = 'NFCD_AlipayBoxN5DEL';
    tester.view.physicalSize = const Size(1600, 720);
    tester.view.devicePixelRatio = 2;
    tester.view.padding = const FakeViewPadding(top: 48, bottom: 136);
    addTearDown(tester.view.reset);
    await tester.pumpWidget(
      MaterialApp(
        theme: orbitTheme(Brightness.light),
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
    expect(tester.takeException(), isNull);
    final first = find.byKey(const ValueKey('session-session-0'));
    final bounds = tester.getRect(first);
    for (final text
        in find.descendant(of: first, matching: find.byType(Text)).evaluate()) {
      expect(
        tester.getRect(find.byWidget(text.widget)).bottom,
        lessThanOrEqualTo(bounds.bottom),
      );
    }
    await tester.drag(
      find.byKey(const PageStorageKey('workbench-sessions')),
      const Offset(0, -180),
    );
    await tester.pumpAndSettle();
    expect(
      find.byKey(const ValueKey('session-session-8')).hitTestable(),
      findsOneWidget,
    );
    expect(tester.takeException(), isNull);
    await tester.pumpWidget(const SizedBox());
  });
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
    expect(find.byType(SliverGrid), findsOneWidget);
    final viewport = tester.getRect(
      find.byKey(const PageStorageKey('workbench-sessions')),
    );
    final last = tester.getRect(
      find.byKey(const ValueKey('session-session-7')),
    );
    expect(last.bottom, closeTo(viewport.bottom, .01));
    expect(tester.takeException(), isNull);
    await tester.pumpWidget(const SizedBox());
  });
  testWidgets('five and a half minimum rows become five complete rows', (
    tester,
  ) async {
    deviceModel = '24031PN0DC';
    tester.view.physicalSize = const Size(800, 423);
    tester.view.devicePixelRatio = 1;
    addTearDown(tester.view.reset);
    final sessions = controller.view!['codex']['sessions'] as List;
    sessions[0]['displayName'] = '一个超过卡片标题可用宽度并且应该被缩略显示的非常长的会话标题';
    await tester.pumpWidget(
      MaterialApp(
        builder: (_, child) => AppViewport(child: child!),
        home: WorkbenchScreen(controller: controller),
      ),
    );
    await tester.pumpAndSettle();
    final viewport = tester.getRect(
      find.byKey(const PageStorageKey('workbench-sessions')),
    );
    expect(viewport.height, 313);
    final first = tester.getRect(
      find.byKey(const ValueKey('session-session-0')),
    );
    expect(first.height, closeTo((313 - 4 * 6) / 5, .01));
    for (var row = 0; row < 5; row++) {
      final left = tester.getRect(
        find.byKey(ValueKey('session-session-${row * 2}')),
      );
      final right = tester.getRect(
        find.byKey(ValueKey('session-session-${row * 2 + 1}')),
      );
      expect(left.height, closeTo(first.height, .01));
      expect(left.top, closeTo(right.top, .01));
      expect(left.bottom, lessThanOrEqualTo(viewport.bottom + .01));
    }
    expect(
      tester.getRect(find.byKey(const ValueKey('session-session-9'))).bottom,
      closeTo(viewport.bottom, .01),
    );
    final title = find.text(sessions[0]['displayName'] as String);
    final text = tester.widget<Text>(title);
    final paragraph = tester.renderObject<RenderParagraph>(title);
    expect(text.maxLines, 1);
    expect(text.overflow, TextOverflow.ellipsis);
    expect(paragraph.didExceedMaxLines, isTrue);
    await tester.drag(
      find.byKey(const PageStorageKey('workbench-sessions')),
      const Offset(0, -250),
    );
    await tester.pumpAndSettle();
    expect(
      find.byKey(const ValueKey('session-session-10')).hitTestable(),
      findsOneWidget,
    );
    expect(tester.takeException(), isNull);
    await tester.pumpWidget(const SizedBox());
  });
  for (final size in [
    const Size(360, 800),
    const Size(411, 914),
    const Size(1200, 800),
  ]) {
    testWidgets('regular cards fill the area below metrics at $size', (
      tester,
    ) async {
      tester.view.physicalSize = size;
      tester.view.devicePixelRatio = 1;
      addTearDown(tester.view.reset);
      await tester.pumpWidget(
        MaterialApp(home: WorkbenchScreen(controller: controller)),
      );
      await tester.pumpAndSettle();
      final viewport = tester.getRect(
        find.byKey(const PageStorageKey('workbench-page')),
      );
      final first = tester.getRect(
        find.byKey(const ValueKey('session-session-0')),
      );
      final visible = <Rect>[];
      for (var i = 0; i < 40; i++) {
        final card = find.byKey(ValueKey('session-session-$i'));
        if (card.evaluate().isEmpty) continue;
        final rect = tester.getRect(card);
        if (rect.top >= viewport.bottom - .01) continue;
        visible.add(rect);
        expect(rect.height, closeTo(first.height, .01));
        expect(rect.bottom, lessThanOrEqualTo(viewport.bottom + .01));
      }
      expect(first.height, greaterThanOrEqualTo(52));
      expect(visible.last.bottom, closeTo(viewport.bottom, .01));
      final rows = visible.map((rect) => rect.top).toSet().length;
      expect(
        (rows + 1) * 52 + rows * 12,
        greaterThan(viewport.bottom - first.top),
      );
      final target = find.byKey(const ValueKey('session-session-15'));
      await tester.scrollUntilVisible(
        target,
        200,
        scrollable: find.descendant(
          of: find.byKey(const PageStorageKey('workbench-page')),
          matching: find.byType(Scrollable),
        ),
      );
      await tester.pumpAndSettle();
      expect(tester.getRect(target).height, closeTo(first.height, .01));
      expect(tester.takeException(), isNull);
      await tester.pumpWidget(const SizedBox());
    });
  }

  for (final width in [320.0, 360.0, 411.0]) {
    for (final config in [(3, false), (3, true), (4, false), (4, true)]) {
      final (count, fullscreen) = config;
      testWidgets(
        'portrait metrics stay in one compact row: $count at $width fullscreen=$fullscreen',
        (tester) async {
          tester.view.physicalSize = Size(width, 914);
          tester.view.devicePixelRatio = 1;
          addTearDown(tester.view.reset);
          if (count == 4) controller.view!.remove('displayKind');
          controller.view!['usage'] = {
            'actualCostMicros': '12340000',
            'tokenCount': '1234567',
            'tpm': '18400',
          };
          await tester.pumpWidget(
            MaterialApp(
              home: WorkbenchScreen(
                controller: controller,
                fullscreen: fullscreen,
              ),
            ),
          );
          await tester.pumpAndSettle();
          final row = tester.getRect(
            find.byKey(const ValueKey('workbench-inline-metrics')),
          );
          expect(row.height, 64);
          expect(find.text('API 用量'), findsNothing);
          expect(find.text('账号周限'), findsNothing);
          if (count == 3) {
            expect(find.text('10/19\n12:30'), findsOneWidget);
          }
          final labels = count == 3
              ? ['剩余额度', '重置时间', '剩余时间']
              : ['实际成本', 'Token', 'TPM', '运行任务'];
          final cells = labels
              .map((label) => tester.getRect(find.text(label)))
              .toList();
          for (var i = 0; i < cells.length; i++) {
            expect(cells[i].top, closeTo(cells.first.top, .01));
            expect(cells[i].left, greaterThanOrEqualTo(row.left));
            expect(cells[i].right, lessThanOrEqualTo(row.right + .01));
            if (i > 0) expect(cells[i].left, greaterThan(cells[i - 1].right));
          }
          final first = tester.getRect(
            find.byKey(const ValueKey('session-session-0')),
          );
          expect(first.top - row.bottom, lessThan(48));
          expect(tester.takeException(), isNull);
          await tester.pumpWidget(const SizedBox());
        },
      );
    }
  }

  testWidgets('portrait four metrics stay in one row with enlarged text', (
    tester,
  ) async {
    tester.view.physicalSize = const Size(320, 914);
    tester.view.devicePixelRatio = 1;
    addTearDown(tester.view.reset);
    controller.view!.remove('displayKind');
    controller.navigation['nfcd_metrics_left_padding'] = 64;
    await tester.pumpWidget(
      MaterialApp(
        builder: (context, child) => MediaQuery(
          data: MediaQuery.of(
            context,
          ).copyWith(textScaler: const TextScaler.linear(1.5)),
          child: child!,
        ),
        home: WorkbenchScreen(controller: controller),
      ),
    );
    await tester.pumpAndSettle();
    final row = tester.getRect(
      find.byKey(const ValueKey('workbench-inline-metrics')),
    );
    expect(row.height, 75);
    final labels = ['实际成本', 'Token', 'TPM', '运行任务'];
    final top = tester.getTopLeft(find.text(labels.first)).dy;
    for (final label in labels) {
      final text = find.text(label);
      expect(tester.widget<Text>(text).maxLines, 1);
      expect(tester.getTopLeft(text).dy, closeTo(top, .01));
      expect(tester.getRect(text).right, lessThanOrEqualTo(row.right + .01));
    }
    expect(tester.takeException(), isNull);
    await tester.pumpWidget(const SizedBox());
  });

  testWidgets('metrics width and visibility reflow session columns live', (
    tester,
  ) async {
    tester.view.physicalSize = const Size(940, 423);
    tester.view.devicePixelRatio = 1;
    addTearDown(tester.view.reset);
    await tester.pumpWidget(
      MaterialApp(
        builder: (_, child) => AppViewport(child: child!),
        home: WorkbenchScreen(controller: controller),
      ),
    );
    await tester.pumpAndSettle();
    int columns() =>
        (tester.widget<SliverGrid>(find.byType(SliverGrid)).gridDelegate
                as SliverGridDelegateWithFixedCrossAxisCount)
            .crossAxisCount;
    double width() =>
        tester.getSize(find.byKey(const ValueKey('workbench-metrics'))).width;
    expect(width(), 150);
    expect(columns(), 2);
    controller.navigation['metrics_width'] = 96;
    controller.notifyListeners();
    await tester.pumpAndSettle();
    expect(width(), 96);
    expect(columns(), 3);
    controller.navigation['metrics_width'] = 320;
    controller.notifyListeners();
    await tester.pumpAndSettle();
    expect(width(), 320);
    expect(columns(), 2);
    controller.navigation['metrics_visible'] = false;
    controller.notifyListeners();
    await tester.pumpAndSettle();
    expect(find.byKey(const ValueKey('workbench-metrics')), findsNothing);
    expect(find.text('剩余额度'), findsNothing);
    expect(columns(), 3);
    final cards = List.generate(
      3,
      (i) => tester.getRect(find.byKey(ValueKey('session-session-$i'))),
    );
    expect(cards[0].top, cards[1].top);
    expect(cards[1].top, cards[2].top);
    expect(cards[0].width, greaterThanOrEqualTo(260));
    await tester.tap(find.byKey(const ValueKey('workbench-fullscreen')));
    await tester.pumpAndSettle();
    expect(columns(), 3);
    expect(find.text('剩余额度'), findsNothing);
    controller.navigation['metrics_visible'] = true;
    controller.navigation['metrics_width'] = 1000;
    controller.notifyListeners();
    await tester.pumpAndSettle();
    expect(width(), 150);
    expect(columns(), 2);
    expect(tester.takeException(), isNull);
    await tester.pumpWidget(const SizedBox());
  });
  for (final size in [
    const Size(411, 914),
    const Size(800, 423),
    const Size(940, 423),
    const Size(1200, 423),
  ]) {
    testWidgets('hidden metrics use the full session region at $size', (
      tester,
    ) async {
      tester.view.physicalSize = size;
      tester.view.devicePixelRatio = 1;
      addTearDown(tester.view.reset);
      controller.navigation['metrics_visible'] = false;
      await tester.pumpWidget(
        MaterialApp(home: WorkbenchScreen(controller: controller)),
      );
      await tester.pumpAndSettle();
      expect(find.text('剩余额度'), findsNothing);
      expect(find.text('账号周限'), findsNothing);
      final grid =
          tester.widget<SliverGrid>(find.byType(SliverGrid)).gridDelegate
              as SliverGridDelegateWithFixedCrossAxisCount;
      expect(grid.crossAxisCount, switch (size.width) {
        411 => 1,
        800 => 2,
        940 => 3,
        _ => 4,
      });
      final viewport = tester.getRect(
        find.byKey(const PageStorageKey('workbench-sessions')),
      );
      final first = tester.getRect(
        find.byKey(const ValueKey('session-session-0')),
      );
      expect(first.top, closeTo(viewport.top, .01));
      expect(first.width, greaterThanOrEqualTo(260));
      expect(tester.takeException(), isNull);
      await tester.pumpWidget(const SizedBox());
    });
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
  for (final size in [const Size(800, 360), const Size(411, 914)]) {
    for (final model in ['NFCD_AlipayBoxN5DEL', 'other-device']) {
      testWidgets(
        'display padding follows device navigation with legacy view fallback on $model at $size',
        (tester) async {
          deviceModel = model;
          tester.view.physicalSize = size;
          tester.view.devicePixelRatio = 1;
          addTearDown(tester.view.resetPhysicalSize);
          addTearDown(tester.view.resetDevicePixelRatio);
          controller.view = fixture();
          controller.navigation = {'items': []};
          await tester.pumpWidget(
            MaterialApp(
              builder: (_, child) => AppViewport(child: child!),
              home: WorkbenchScreen(controller: controller),
            ),
          );
          await tester.pumpAndSettle();
          double left() => tester.getTopLeft(find.text('剩余额度')).dx;
          final initial = left();
          final nfcd = model == 'NFCD_AlipayBoxN5DEL';
          final step = 16 * (nfcd && size.width > size.height ? .85 : 1);
          controller.navigation['nfcd_metrics_left_padding'] = 0;
          controller.notifyListeners();
          await tester.pumpAndSettle();
          expect(left(), closeTo(initial - step, .01));
          controller.view!['nfcdMetricsLeftPadding'] = 64;
          controller.notifyListeners();
          await tester.pumpAndSettle();
          expect(left(), closeTo(initial - step, .01));
          controller.view!['nfcdMetricsLeftPadding'] = 0;
          controller.navigation['nfcd_metrics_left_padding'] = 32;
          controller.notifyListeners();
          await tester.pumpAndSettle();
          expect(left(), closeTo(initial + step, .01));
          controller.view!.remove('nfcdMetricsLeftPadding');
          controller.notifyListeners();
          await tester.pumpAndSettle();
          expect(left(), closeTo(initial + step, .01));
          controller.navigation['nfcd_metrics_left_padding'] = 100;
          controller.notifyListeners();
          await tester.pumpAndSettle();
          expect(left(), closeTo(initial, .01));
          controller.navigation.remove('nfcd_metrics_left_padding');
          controller.view!['nfcdMetricsLeftPadding'] = 64;
          controller.notifyListeners();
          await tester.pumpAndSettle();
          expect(left(), closeTo(initial + 3 * step, .01));
          expect(tester.takeException(), isNull);
          await tester.pumpWidget(const SizedBox());
        },
      );
    }
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
    for (final variant in [
      (false, Brightness.light),
      (true, Brightness.light),
      (false, Brightness.dark),
      (true, Brightness.dark),
    ]) {
      final (atTop, brightness) = variant;
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
            theme: orbitTheme(brightness).copyWith(
              textTheme: orbitTheme(
                brightness,
              ).textTheme.apply(fontFamily: 'Preview'),
            ),
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
            '${output.path}/${size.width.toInt()}x${size.height.toInt()}-${atTop ? 'top' : 'bottom'}-${brightness.name}.png',
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
