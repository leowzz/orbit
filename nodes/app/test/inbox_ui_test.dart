import 'dart:convert';
import 'dart:io';
import 'package:flutter/material.dart';
import 'package:flutter/services.dart';
import 'package:flutter_test/flutter_test.dart';
import 'package:orbit_app/inbox_controller.dart';
import 'package:orbit_app/inbox_screen.dart';
import 'package:orbit_app/local_store.dart';
import 'package:sqflite_common_ffi/sqflite_ffi.dart';
import 'sync_test.dart' show item, page;
import 'widget_test.dart' show waitForLocalUpdate;

void main() {
  sqfliteFfiInit();

  Future<InboxController> controller(WidgetTester tester) async {
    late Directory dir;
    late InboxController c;
    await tester.runAsync(() async {
      dir = await Directory.systemTemp.createTemp('orbit-inbox-ui-');
      final local = await LocalStore.open(
        databaseFactoryFfi,
        '${dir.path}/test.db',
      );
      c = InboxController(local, 'https://example.test', 'test', dir.path);
      await c.load();
    });
    addTearDown(() async {
      await tester.pumpWidget(const SizedBox());
      await tester.runAsync(() async {
        await c.close();
        await dir.delete(recursive: true);
      });
    });
    return c;
  }

  for (final (width, scale) in [(320.0, 1.0), (360.0, 1.5), (900.0, 1.5)]) {
    testWidgets('date groups and controls fit $width at text scale $scale', (
      tester,
    ) async {
      tester.view.physicalSize = Size(width, 900);
      tester.view.devicePixelRatio = 1;
      addTearDown(tester.view.resetPhysicalSize);
      addTearDown(tester.view.resetDevicePixelRatio);
      final c = await controller(tester);
      await tester.runAsync(() async {
        await c.local.applyPage(
          page('1', [
            {
              ...item('1', body: '今天的第一条'),
              'id': 'first',
              'created_at': '2026-10-09T08:00:00Z',
            },
            {
              ...item('1', body: '今天的第二条'),
              'id': 'second',
              'created_at': '2026-10-09T07:00:00Z',
            },
            {
              ...item('1', body: '昨天的消息'),
              'id': 'third',
              'created_at': '2026-10-08T08:00:00Z',
            },
          ]),
        );
        await c.load();
      });
      await tester.pumpWidget(
        MaterialApp(
          builder: (context, child) => MediaQuery(
            data: MediaQuery.of(
              context,
            ).copyWith(textScaler: TextScaler.linear(scale)),
            child: child!,
          ),
          home: InboxScreen(controller: c, onSettings: () {}),
        ),
      );
      await tester.pumpAndSettle();
      expect(find.text('2026年10月9日'), findsOneWidget);
      expect(find.text('2026年10月8日'), findsOneWidget);
      expect(find.widgetWithText(TextField, '搜索文字、链接…'), findsOneWidget);
      expect(tester.takeException(), isNull);
      final card = tester.getRect(
        find.byKey(const ValueKey('message-card')).first,
      );
      final compose = tester.getRect(find.byKey(const ValueKey('composer')));
      expect((card.left - compose.left).abs(), lessThanOrEqualTo(2));
      expect((card.right - compose.right).abs(), lessThanOrEqualTo(2));
      final checkbox = tester.getRect(
        find.descendant(
          of: find.byKey(const ValueKey('first')),
          matching: find.byType(Checkbox),
        ),
      );
      final body = tester.getRect(find.text('今天的第一条'));
      expect((checkbox.center.dy - body.center.dy).abs(), lessThanOrEqualTo(2));
    });
  }

  testWidgets('an image stays in the composer until send and can be removed', (
    tester,
  ) async {
    final c = await controller(tester);
    final photo = File('${c.cacheDir}/sample.png');
    await tester.runAsync(
      () => photo.writeAsBytes(
        base64Decode(
          'iVBORw0KGgoAAAANSUhEUgAAABAAAAAMCAIAAADkharWAAAAFUlEQVR4nGPQKE8lCTGMahjVgB0BANOdwwFAryQoAAAAAElFTkSuQmCC',
        ),
      ),
    );
    const picker = MethodChannel('plugins.flutter.io/image_picker');
    TestDefaultBinaryMessengerBinding.instance.defaultBinaryMessenger
        .setMockMethodCallHandler(
          picker,
          (call) async => call.method == 'pickImage' ? photo.path : null,
        );
    addTearDown(
      () => TestDefaultBinaryMessengerBinding.instance.defaultBinaryMessenger
          .setMockMethodCallHandler(picker, null),
    );
    await tester.pumpWidget(
      MaterialApp(
        home: InboxScreen(controller: c, onSettings: () {}),
      ),
    );
    await waitForLocalUpdate(
      tester,
      () =>
          tester
              .widget<OutlinedButton>(
                find.widgetWithText(OutlinedButton, '添加图片'),
              )
              .onPressed !=
          null,
    );
    expect(
      tester
          .widget<FilledButton>(find.widgetWithText(FilledButton, '发送'))
          .onPressed,
      isNull,
    );
    await tester.enterText(find.byKey(const ValueKey('composer-body')), '图片说明');
    await tester.tap(find.text('添加图片'));
    await waitForLocalUpdate(
      tester,
      () => find.text('sample.png').evaluate().isNotEmpty,
    );
    expect(c.pending, isEmpty);
    expect(
      tester
          .widget<Checkbox>(find.byKey(const ValueKey('composer-todo')))
          .onChanged,
      isNull,
    );
    await tester.tap(find.text('移除图片'));
    await tester.pumpAndSettle();
    expect(find.text('sample.png'), findsNothing);
    expect(find.text('图片说明'), findsOneWidget);
    await tester.tap(find.text('添加图片'));
    await waitForLocalUpdate(
      tester,
      () => find.text('sample.png').evaluate().isNotEmpty,
    );
    // A local save failure must retain both the caption and attachment.
    await tester.runAsync(photo.delete);
    await tester.tap(find.text('发送'));
    await waitForLocalUpdate(
      tester,
      () => find.text('保存中…').evaluate().isEmpty,
    );
    expect(find.text('sample.png'), findsOneWidget);
    expect(find.text('图片说明'), findsOneWidget);
    expect(c.pending, isEmpty);
    await tester.runAsync(() => photo.writeAsBytes([1, 2, 3]));
    await tester.tap(find.text('发送'));
    await waitForLocalUpdate(
      tester,
      () => c.pending.isNotEmpty && find.text('保存中…').evaluate().isEmpty,
    );
    final op = jsonDecode(c.pending.single['payload']);
    expect(op['kind'], 'image');
    expect(op['body'], '图片说明');
    expect(
      await tester.runAsync(() => File(c.pending.single['photo']).exists()),
      isTrue,
    );
    expect(find.text('移除图片'), findsNothing);
    expect(
      tester
          .widget<TextField>(find.byKey(const ValueKey('composer-body')))
          .controller!
          .text,
      isEmpty,
    );
    expect(tester.takeException(), isNull);
  });
}
