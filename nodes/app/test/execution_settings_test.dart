import 'dart:convert';
import 'package:flutter/material.dart';
import 'package:flutter_test/flutter_test.dart';
import 'package:orbit_app/execution_settings.dart';
import 'package:orbit_app/inbox_controller.dart';
import 'package:orbit_app/local_store.dart';
import 'package:sqflite_common/sqlite_api.dart';

class _UnusedDatabase implements Database {
  @override
  Future<void> close() async {}
  @override
  dynamic noSuchMethod(Invocation invocation) => throw UnimplementedError();
}

class SettingsMemoryStore extends LocalStore {
  SettingsMemoryStore() : super(_UnusedDatabase());
  final metadata = <String, String>{};
  @override
  Future<String?> meta(String key) async => metadata[key];
  @override
  Future<void> setMeta(String key, String value) async {
    metadata[key] = value;
  }
}

void main() {
  TestWidgetsFlutterBinding.ensureInitialized();
  late SettingsMemoryStore store;
  late InboxController controller;
  setUp(() async {
    store = SettingsMemoryStore();
    controller = InboxController(store, 'https://example.test', 'test', '/tmp');
  });
  tearDown(() {
    // This fixture never starts the controller or opens a database.
    controller.client.close();
    controller.dispose();
  });
  for (final size in [const Size(360, 640), const Size(640, 280)]) {
    testWidgets('local URL editor validates, saves and fits $size', (
      tester,
    ) async {
      tester.view.physicalSize = size;
      tester.view.devicePixelRatio = 1;
      addTearDown(tester.view.reset);
      await tester.pumpWidget(
        MaterialApp(home: ExecutionSettingsScreen(controller: controller)),
      );
      await tester.pumpAndSettle();
      await tester.tap(find.text('添加网址'));
      await tester.pumpAndSettle();
      await tester.ensureVisible(find.text('添加到列表'));
      await tester.tap(find.text('添加到列表'));
      await tester.pumpAndSettle();
      expect(find.text('请填写名称'), findsOneWidget);
      expect(find.text('请填写有效的完整网址或协议链接'), findsOneWidget);
      await tester.enterText(find.byType(TextFormField).at(0), '本机浏览器');
      await tester.enterText(
        find.byType(TextFormField).at(1),
        'https://example.com/private',
      );
      await tester.ensureVisible(find.text('添加到列表'));
      await tester.tap(find.text('添加到列表'));
      await tester.pumpAndSettle();
      expect(find.text('本机浏览器'), findsOneWidget);
      await tester.tap(find.text('保存'));
      await tester.pumpAndSettle();
      final saved =
          jsonDecode((await store.meta('execution_settings'))!) as Json;
      expect(saved['enabled'], isFalse);
      expect(
        (saved['items'] as List).single['target'],
        'https://example.com/private',
      );
      expect(tester.takeException(), isNull);
      await tester.pumpWidget(const SizedBox());
    });
  }
}
