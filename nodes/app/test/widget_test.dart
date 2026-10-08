import 'dart:convert';
import 'dart:io';
import 'package:flutter/material.dart';
import 'package:flutter_test/flutter_test.dart';
import 'package:orbit_app/inbox_controller.dart';
import 'package:orbit_app/inbox_screen.dart';
import 'package:orbit_app/local_store.dart';
import 'package:sqflite_common_ffi/sqflite_ffi.dart';
import 'sync_test.dart' show item, page;

void main() {
  sqfliteFfiInit();
  for (final width in [360.0, 900.0]) {
    testWidgets('inbox fits $width pixels and can queue offline text', (
      tester,
    ) async {
      tester.view.physicalSize = Size(width, 800);
      tester.view.devicePixelRatio = 1;
      addTearDown(tester.view.resetPhysicalSize);
      addTearDown(tester.view.resetDevicePixelRatio);
      late Directory dir;
      late LocalStore local;
      late InboxController c;
      await tester.runAsync(() async {
        dir = await Directory.systemTemp.createTemp('orbit-widget-');
        local = await LocalStore.open(
          databaseFactoryFfi,
          '${dir.path}/cache.sqlite',
        );
        await local.applyPage(
          page('1', [item('1', body: '整理本周的想法与待办')]),
          replace: true,
          snapshot: [item('1', body: '整理本周的想法与待办')],
        );
        c = InboxController(local, 'https://example.test', 'token', dir.path);
        await c.load();
      });
      await tester.pumpWidget(
        MaterialApp(
          home: InboxScreen(controller: c, onSettings: () {}),
        ),
      );
      await tester.pumpAndSettle();
      expect(tester.takeException(), isNull);
      expect(find.text('整理本周的想法与待办'), findsOneWidget);
      final composer = tester.getRect(find.byType(TextField));
      expect(find.text('周期用量'), findsNothing);
      expect(find.byType(ExpansionTile), findsNothing);
      expect(tester.widget<Switch>(find.byType(Switch)).value, isFalse);
      expect(
        tester.getRect(find.byType(Switch)).top,
        greaterThanOrEqualTo(composer.bottom),
      );
      expect(composer.bottom, greaterThan(680));
      final message = find.text('整理本周的想法与待办');
      final position = tester.getRect(message);
      for (final operation in ['set_completed', 'update', 'delete']) {
        await tester.runAsync(
          () => c.submit(
            operation,
            item: c.items.single,
            completed: operation == 'set_completed' ? true : null,
            body: operation == 'update' ? '修改后的内容' : null,
          ),
        );
        await tester.pumpAndSettle();
        expect(tester.getRect(message), position);
        expect(find.text('待发送'), findsNothing);
        expect(find.textContaining('待同步'), findsOneWidget);
        expect(
          tester.widget<Checkbox>(find.byType(Checkbox)).onChanged,
          isNull,
        );
        if (operation == 'set_completed') {
          expect(tester.widget<Checkbox>(find.byType(Checkbox)).value, isTrue);
          expect(
            tester.widget<Text>(message).style?.decoration,
            isNot(TextDecoration.lineThrough),
          );
        }
        // Simulate acknowledgment: the temporary state must disappear in place.
        await tester.runAsync(() async {
          await local.acknowledge(c.pending.single['id'], {
            ...c.items.single,
            'revision': '${int.parse(c.items.single['revision']) + 1}',
            if (operation == 'set_completed') 'completed': true,
          });
          await c.load();
        });
        await tester.pumpAndSettle();
        expect(tester.getRect(message), position);
        expect(find.textContaining('待同步'), findsNothing);
        expect(
          tester.widget<Text>(message).style?.decoration,
          isNot(TextDecoration.lineThrough),
        );
      }
      for (final kind in ['text', 'todo']) {
        await tester.tap(find.byTooltip('更多操作'));
        await tester.pumpAndSettle();
        await tester.tap(find.text(kind == 'todo' ? '设为待办' : '改为文本'));
        await tester.runAsync(() async {
          await Future<void>.delayed(const Duration(milliseconds: 100));
        });
        await tester.pumpAndSettle();
        final operation = jsonDecode(c.pending.single['payload']);
        expect(operation['type'], 'set_kind');
        expect(operation['kind'], kind);
        expect(
          find.byType(Checkbox),
          kind == 'todo' ? findsOneWidget : findsNothing,
        );
        expect(find.text('待发送'), findsNothing);
        await tester.runAsync(() async {
          await local.acknowledge(c.pending.single['id'], {
            ...c.items.single,
            'revision': '${int.parse(c.items.single['revision']) + 1}',
            'kind': kind,
            'completed': false,
          });
          await c.load();
        });
        await tester.pumpAndSettle();
      }
      // Errors still expose the preserved draft and its recovery action.
      await tester.runAsync(() async {
        await c.submit('update', item: c.items.single, body: '保留失败草稿');
        await local.mark(
          c.pending.single['id'],
          'failed',
          error: 'invalid_fields',
        );
        await c.load();
      });
      await tester.pumpAndSettle();
      expect(find.text('保留失败草稿'), findsOneWidget);
      expect(find.text('重试'), findsOneWidget);
      await tester.runAsync(() => c.discard(c.pending.single));
      await tester.pumpAndSettle();
      await tester.enterText(find.byType(TextField), '离线时也能记下来');
      await tester.tap(find.text('发送'));
      await tester.runAsync(() async {
        await Future<void>.delayed(const Duration(milliseconds: 100));
      });
      await tester.pumpAndSettle();
      expect(find.text('待发送'), findsNothing);
      expect(find.text('离线时也能记下来'), findsNothing);
      expect(tester.getRect(message), position);
      expect((await tester.runAsync(local.pending))!.length, 1);
      expect(jsonDecode(c.pending.single['payload'])['kind'], 'text');
      // Upload progress stays quiet; only failures expose the preserved draft.
      c.uploadingID = c.pending.single['id'];
      await tester.runAsync(c.load);
      await tester.pumpAndSettle();
      expect(find.text('正在上传图片'), findsNothing);
      for (final state in ['failed', 'conflict', 'pending']) {
        await tester.runAsync(() async {
          await local.mark(
            c.pending.single['id'],
            state,
            error: 'invalid_fields',
          );
          await c.load();
        });
        await tester.pumpAndSettle();
        expect(
          find.text('离线时也能记下来'),
          state == 'pending' ? findsNothing : findsOneWidget,
        );
        expect(
          find.text('重试'),
          state == 'failed' ? findsOneWidget : findsNothing,
        );
        expect(
          find.text('处理'),
          state == 'conflict' ? findsOneWidget : findsNothing,
        );
      }
      c.uploadingID = null;
      await tester.runAsync(() => c.discard(c.pending.single));
      await tester.pumpAndSettle();
      await tester.enterText(find.byType(TextField), '只把这一条设为待办');
      await tester.tap(find.byType(Switch));
      await tester.runAsync(() async {
        await Future<void>.delayed(const Duration(milliseconds: 100));
      });
      await tester.pumpAndSettle();
      expect(find.text('只把这一条设为待办'), findsOneWidget);
      expect(tester.widget<Switch>(find.byType(Switch)).value, isTrue);
      await tester.tap(find.text('发送'));
      await tester.runAsync(() async {
        await Future<void>.delayed(const Duration(milliseconds: 100));
      });
      await tester.pumpAndSettle();
      expect(jsonDecode(c.pending.single['payload'])['kind'], 'todo');
      expect(tester.widget<Switch>(find.byType(Switch)).value, isTrue);
      expect(tester.takeException(), isNull);
      await tester.pumpWidget(const SizedBox());
      // A new screen and database connection must restore the preference.
      await tester.runAsync(() async {
        await c.close();
        local = await LocalStore.open(
          databaseFactoryFfi,
          '${dir.path}/cache.sqlite',
        );
        c = InboxController(local, 'https://example.test', 'token', dir.path);
        await c.load();
      });
      await tester.pumpWidget(
        MaterialApp(
          home: InboxScreen(controller: c, onSettings: () {}),
        ),
      );
      await tester.runAsync(() async {
        await Future<void>.delayed(const Duration(milliseconds: 100));
      });
      await tester.pumpAndSettle();
      expect(tester.widget<Switch>(find.byType(Switch)).value, isTrue);
      await tester.tap(find.byType(Switch));
      await tester.runAsync(() async {
        await Future<void>.delayed(const Duration(milliseconds: 100));
      });
      await tester.pumpAndSettle();
      expect(tester.widget<Switch>(find.byType(Switch)).value, isFalse);
      expect(await tester.runAsync(() => local.meta('composer_kind')), 'text');
      await tester.pumpWidget(const SizedBox());
      await tester.runAsync(() async {
        await c.close();
        await dir.delete(recursive: true);
      });
    });
  }

  testWidgets(
    'lazy inbox keeps expansion attached to a message after insertion',
    (tester) async {
      late Directory dir;
      late InboxController c;
      final messages = List.generate(
        100,
        (i) => {
          ...item(
            '1',
            body: i == 0 ? List.filled(9, '一行正文').join('\n') : '消息 $i',
          ),
          'id': 'item-${(100 - i).toString().padLeft(3, '0')}',
          'kind': 'text',
        },
      );
      await tester.runAsync(() async {
        dir = await Directory.systemTemp.createTemp('orbit-lazy-');
        final local = await LocalStore.open(
          databaseFactoryFfi,
          '${dir.path}/test.db',
        );
        await local.applyPage(page('1', messages));
        c = InboxController(local, 'https://example.test', '', dir.path);
        await c.load();
      });
      await tester.pumpWidget(
        MaterialApp(
          home: InboxScreen(controller: c, onSettings: () {}),
        ),
      );
      await tester.runAsync(
        () => Future<void>.delayed(const Duration(milliseconds: 100)),
      );
      await tester.pumpAndSettle();
      expect(find.byType(InboxMessageBody).evaluate().length, lessThan(20));
      await tester.tap(find.text('展开'));
      await tester.pumpAndSettle();
      final original = find.descendant(
        of: find.byKey(const ValueKey('item-100')),
        matching: find.byType(InboxMessageBody),
      );
      final state = tester.state(original);
      await tester.runAsync(() async {
        await c.local.applyPage(
          page('2', [
            {...item('1', body: '新消息'), 'id': 'item-101', 'kind': 'text'},
          ]),
        );
        await c.load();
      });
      await tester.pumpAndSettle();
      expect(tester.state(original), same(state));
      expect(find.text('收起'), findsOneWidget);
      expect(tester.takeException(), isNull);
      await tester.pumpWidget(const SizedBox());
      await tester.runAsync(() async {
        await c.close();
        await dir.delete(recursive: true);
      });
    },
  );

  testWidgets('cached text layout responds to content, width and text scale', (
    tester,
  ) async {
    Future<void> show(
      String body, {
      double width = 400,
      double scale = 1,
    }) async {
      await tester.pumpWidget(
        MaterialApp(
          home: Scaffold(
            body: MediaQuery(
              data: MediaQueryData(textScaler: TextScaler.linear(scale)),
              child: SizedBox(
                width: width,
                child: InboxMessageBody(
                  body,
                  style: const TextStyle(fontSize: 16, height: 1.5),
                ),
              ),
            ),
          ),
        ),
      );
      await tester.pumpAndSettle();
    }

    await show(List.filled(9, '正文').join('\n'));
    expect(find.text('展开'), findsOneWidget);
    await show('短消息');
    expect(find.text('展开'), findsNothing);
    final paragraph = List.filled(10, '这是自动换行的正文。').join();
    await show(paragraph);
    expect(find.text('展开'), findsNothing);
    await show(paragraph, width: 100);
    expect(find.text('展开'), findsOneWidget);
    await show(paragraph, scale: 2);
    expect(find.text('展开'), findsOneWidget);
    expect(tester.takeException(), isNull);
  });

  for (final width in [280.0, 800.0]) {
    testWidgets('message collapses after eight rendered lines at $width', (
      tester,
    ) async {
      const style = TextStyle(fontSize: 16, height: 1.5);
      Future<void> show(String body) async {
        await tester.pumpWidget(
          MaterialApp(
            home: Scaffold(
              body: SingleChildScrollView(
                child: SizedBox(
                  width: width,
                  child: InboxMessageBody(
                    body,
                    key: ValueKey(body),
                    style: style,
                  ),
                ),
              ),
            ),
          ),
        );
        await tester.pumpAndSettle();
      }

      await show(List.filled(8, '一行正文').join('\n'));
      expect(find.text('展开'), findsNothing);
      await show(List.filled(9, '一行正文').join('\n'));
      expect(find.text('展开'), findsOneWidget);
      final collapsed = tester.getSize(find.byType(InboxMessageBody)).height;
      await tester.tap(find.text('展开'));
      await tester.pumpAndSettle();
      expect(find.text('收起'), findsOneWidget);
      expect(
        tester.getSize(find.byType(InboxMessageBody)).height,
        greaterThan(collapsed),
      );
      await tester.tap(find.text('收起'));
      await tester.pumpAndSettle();
      expect(tester.getSize(find.byType(InboxMessageBody)).height, collapsed);
      // Long paragraphs wrap even when they contain no explicit newline.
      await show(List.filled(100, '自动换行也计入行数。').join());
      expect(find.text('展开'), findsOneWidget);
      expect(tester.widget<Text>(find.byType(Text).first).maxLines, 8);
      expect(tester.takeException(), isNull);
    });
  }

  for (final mode in ['short', 'collapsed', 'expanded']) {
    testWidgets('swiping $mode message text scrolls the inbox', (tester) async {
      final scroll = ScrollController();
      addTearDown(scroll.dispose);
      final body = List.filled(
        mode == 'short' ? 6 : 12,
        '滑动正文应该滚动消息列表',
      ).join('\n');
      await tester.pumpWidget(
        MaterialApp(
          theme: ThemeData(platform: TargetPlatform.android),
          home: Scaffold(
            body: ListView(
              controller: scroll,
              children: [
                const SizedBox(height: 80),
                InboxMessageBody(
                  body,
                  style: const TextStyle(fontSize: 16, height: 1.5),
                ),
                const SizedBox(height: 1600),
              ],
            ),
          ),
        ),
      );
      await tester.pumpAndSettle();
      if (mode == 'expanded') {
        await tester.tap(find.text('展开'));
        await tester.pumpAndSettle();
      }
      final message = find.byType(InboxMessageBody);
      await tester.dragFrom(
        tester.getTopLeft(message) + const Offset(40, 70),
        const Offset(0, -80),
      );
      await tester.pumpAndSettle();
      expect(scroll.offset, greaterThan(40));
      final offset = scroll.offset;
      await tester.dragFrom(
        tester.getTopLeft(message) + const Offset(40, 100),
        const Offset(0, 60),
      );
      await tester.pumpAndSettle();
      expect(scroll.offset, lessThan(offset));
      if (mode != 'collapsed') {
        scroll.jumpTo(0);
        await tester.pumpAndSettle();
        await tester.longPressAt(
          tester.getTopLeft(message) + const Offset(40, 12),
        );
        await tester.pumpAndSettle();
        expect(find.text('Copy'), findsOneWidget);
      }
      expect(tester.takeException(), isNull);
    });
  }
}
