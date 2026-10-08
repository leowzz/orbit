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
      final summary = tester.getRect(find.byType(StatusSummary));
      final composer = tester.getRect(find.byType(TextField));
      expect(summary.top, lessThan(100));
      expect(composer.bottom, greaterThan(680));
      await tester.enterText(find.byType(TextField), '离线时也能记下来');
      await tester.tap(find.text('发送'));
      await tester.runAsync(() async {
        await Future<void>.delayed(const Duration(milliseconds: 100));
      });
      await tester.pumpAndSettle();
      expect(find.text('待发送'), findsOneWidget);
      expect((await tester.runAsync(local.pending))!.length, 1);
      expect(tester.takeException(), isNull);
      await tester.pumpWidget(const SizedBox());
      await tester.runAsync(() async {
        await c.close();
        await dir.delete(recursive: true);
      });
    });
  }
}
