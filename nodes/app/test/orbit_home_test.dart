import 'dart:io';
import 'package:flutter/material.dart';
import 'package:flutter/services.dart';
import 'package:flutter_test/flutter_test.dart';
import 'package:orbit_app/orbit_home.dart';
import 'package:orbit_app/inbox_controller.dart';
import 'package:orbit_app/inbox_screen.dart';
import 'package:orbit_app/local_store.dart';
import 'package:sqflite_common_ffi/sqflite_ffi.dart';
import 'sync_test.dart' show item, page;

void main() {
  const channel = MethodChannel('dev.orbit/node');
  final calls = <String>[];
  TestWidgetsFlutterBinding.ensureInitialized();
  sqfliteFfiInit();
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
      await tester.runAsync(() async {
        await Future<void>.delayed(const Duration(milliseconds: 100));
      });
      await tester.pumpAndSettle();
      await tester.enterText(find.byType(TextField).first, '保留草稿');
      await tester.tap(find.text('状态与组件'));
      await tester.pumpAndSettle();
      expect(find.text('添加到桌面').hitTestable(), findsOneWidget);
      await tester.ensureVisible(find.text('Android 桌面组件'));
      await tester.tap(find.text('Android 桌面组件'));
      await tester.pumpAndSettle();
      expect(find.text('开始同步').hitTestable(), findsOneWidget);
      expect(calls.where((method) => method == 'load'), hasLength(1));
      await tester.tap(find.text('开始同步'));
      await tester.pumpAndSettle();
      expect(calls, contains('start'));
      await tester.pageBack();
      await tester.pumpAndSettle();
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
    expect(find.byType(NavigationBar), findsOneWidget);
    expect(find.text('设置'), findsOneWidget);
    expect(calls, isEmpty);
  });

  for (final widgetsEnabled in [true, false]) {
    testWidgets(
      'connection settings has its own page with widgets=$widgetsEnabled',
      (tester) async {
        var opened = false;
        await tester.pumpWidget(
          MaterialApp(
            home: Builder(
              builder: (context) => OrbitHome(
                widgetsEnabled: widgetsEnabled,
                inbox: const Scaffold(body: TextField()),
                onSettings: () {
                  opened = true;
                  Navigator.of(context).push(
                    MaterialPageRoute<void>(
                      builder: (_) => Scaffold(
                        appBar: AppBar(title: const Text('连接 Orbit')),
                      ),
                    ),
                  );
                },
              ),
            ),
          ),
        );
        await tester.enterText(find.byType(TextField), '设置页切换时保留草稿');
        await tester.tap(find.widgetWithText(NavigationDestination, '设置'));
        await tester.pumpAndSettle();
        expect(find.text('连接设置'), findsOneWidget);
        await tester.tap(find.text('连接设置'));
        await tester.pumpAndSettle();
        expect(opened, isTrue);
        expect(find.text('连接 Orbit'), findsOneWidget);
        await tester.pageBack();
        await tester.pumpAndSettle();
        await tester.tap(find.widgetWithText(NavigationDestination, '收件箱'));
        await tester.pumpAndSettle();
        expect(find.text('设置页切换时保留草稿'), findsOneWidget);
        expect(calls, isEmpty);
        expect(tester.takeException(), isNull);
      },
    );
  }

  for (final width in [360.0, 900.0]) {
    testWidgets('inbox chrome follows scroll and preserves draft at $width', (
      tester,
    ) async {
      tester.view.physicalSize = Size(width, 800);
      tester.view.devicePixelRatio = 1;
      addTearDown(tester.view.resetPhysicalSize);
      addTearDown(tester.view.resetDevicePixelRatio);
      late Directory dir;
      late InboxController controller;
      await tester.runAsync(() async {
        dir = await Directory.systemTemp.createTemp('orbit-reading-');
        final local = await LocalStore.open(
          databaseFactoryFfi,
          '${dir.path}/cache.sqlite',
        );
        final items = List.generate(
          20,
          (i) => {
            ...item('1', body: '消息 $i\n用于验证正文滑动'),
            'id': 'item-${i.toString().padLeft(2, '0')}',
            'kind': i.isEven ? 'text' : 'todo',
          },
        );
        await local.applyPage(page('1', items));
        controller = InboxController(
          local,
          'https://example.test',
          'token',
          dir.path,
        );
        await controller.load();
      });
      await tester.pumpWidget(
        MaterialApp(
          home: OrbitHome(
            widgetsEnabled: true,
            inbox: InboxScreen(controller: controller),
          ),
        ),
      );
      await tester.pumpAndSettle();
      final list = find.byKey(const PageStorageKey('inbox'));
      final initialHeight = tester.getSize(list).height;
      // The header and navigation share the inbox label.
      expect(find.text('收件箱'), width > 600 ? findsNWidgets(2) : findsOneWidget);
      expect(find.byType(ChoiceChip), findsNothing);
      expect(
        tester.getRect(find.byType(DropdownButton<String>)).top,
        lessThan(tester.getRect(find.byKey(const PageStorageKey('inbox'))).top),
      );
      await tester.enterText(
        find.byKey(const ValueKey('composer-body')),
        '滚动后保留的草稿',
      );
      await tester.runAsync(() => controller.local.meta('draft'));
      await tester.pumpAndSettle();
      await tester.dragFrom(
        tester.getTopLeft(list) + const Offset(60, 200),
        const Offset(0, -160),
      );
      await tester.pumpAndSettle();
      expect(find.byKey(const ValueKey('composer-body')), findsNothing);
      expect(find.byTooltip('搜索消息'), findsOneWidget);
      expect(find.byType(NavigationBar), findsNothing);
      expect(tester.getSize(list).height, greaterThan(initialHeight + 100));
      final scrollable = tester.state<ScrollableState>(
        find.descendant(of: list, matching: find.byType(Scrollable)).first,
      );
      final offset = scrollable.position.pixels;
      await tester.pump(const Duration(seconds: 1));
      expect(scrollable.position.pixels, offset);
      await tester.dragFrom(
        tester.getTopLeft(list) + const Offset(60, 160),
        const Offset(0, 70),
      );
      await tester.pumpAndSettle();
      expect(find.byType(NavigationBar), findsOneWidget);
      expect(find.text('滚动后保留的草稿'), findsOneWidget);
      expect(tester.getSize(list).height, initialHeight);
      // Changing filters while browsing restores the controls, including an empty list.
      await tester.dragFrom(
        tester.getTopLeft(list) + const Offset(60, 200),
        const Offset(0, -160),
      );
      await tester.pumpAndSettle();
      await tester.tap(find.byType(DropdownButton<String>));
      await tester.pumpAndSettle();
      await tester.tap(find.text('图片').last);
      await tester.runAsync(() => controller.load());
      await tester.pumpAndSettle();
      expect(find.text('还没有图片'), findsOneWidget);
      expect(find.byType(NavigationBar), findsOneWidget);
      expect(find.text('滚动后保留的草稿'), findsOneWidget);
      await tester.drag(list, const Offset(0, -80));
      await tester.pumpAndSettle();
      expect(find.byType(NavigationBar), findsOneWidget);
      expect(tester.takeException(), isNull);
      await tester.pumpWidget(const SizedBox());
      await tester.runAsync(() async {
        await controller.close();
        await dir.delete(recursive: true);
      });
    });
  }
}
