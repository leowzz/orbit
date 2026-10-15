import 'dart:convert';
import 'dart:io';
import 'package:flutter/material.dart';
import 'package:flutter_test/flutter_test.dart';
import 'package:http/http.dart' as http;
import 'package:http/testing.dart';
import 'package:orbit_app/inbox_controller.dart';
import 'package:orbit_app/local_store.dart';
import 'package:orbit_app/navigation_bar.dart';
import 'package:sqflite_common_ffi/sqflite_ffi.dart';

class NavigationWidgetStore extends LocalStore {
  NavigationWidgetStore(super.db);
  final metadata = <String, String>{};
  @override
  Future<void> setMeta(String key, String value) async {
    metadata[key] = value;
  }

  @override
  Future<String?> meta(String key) async => metadata[key];
}

void main() {
  TestWidgetsFlutterBinding.ensureInitialized();
  sqfliteFfiInit();
  late Directory dir;
  late InboxController controller;
  final requests = <http.Request>[];
  Json config() => {
    'revision': 1,
    'show_titles': false,
    'items': [
      {
        'id': 'remote',
        'title': '电脑日历',
        'kind': 'agent',
        'agent_id': 'mac',
        'shortcut_id': 'calendar',
        'icon_name': 'app',
      },
    ],
  };
  setUp(() async {
    dir = await Directory.systemTemp.createTemp('orbit-nav-');
    requests.clear();
    controller = InboxController(
      NavigationWidgetStore(
        (await LocalStore.open(databaseFactoryFfi, '${dir.path}/db')).db,
      ),
      'https://example.test',
      'secret',
      dir.path,
      client: MockClient((r) async {
        requests.add(r);
        if (r.url.path.endsWith('/catalogs')) {
          return http.Response('{"catalogs":[]}', 200);
        }
        if (r.url.path.endsWith('/navigation')) {
          return http.Response(
            jsonEncode(
              r.method == 'POST'
                  ? {...jsonDecode(r.body), 'revision': 2}
                  : config(),
            ),
            200,
            headers: {'content-type': 'application/json; charset=utf-8'},
          );
        }
        return http.Response('{"status":"sent"}', 202);
      }),
    );
    controller.navigation = config();
    controller.online = true;
  });
  tearDown(() async {
    await controller.close();
    await dir.delete(recursive: true);
  });
  testWidgets(
    'icons hide titles and remote clicks send only configured entry ID',
    (t) async {
      await t.pumpWidget(
        MaterialApp(
          home: Scaffold(
            body: Align(
              alignment: Alignment.bottomCenter,
              child: WorkbenchNavigation(controller: controller),
            ),
          ),
        ),
      );
      expect(find.text('电脑日历'), findsNothing);
      expect(find.byTooltip('电脑日历'), findsOneWidget);
      await t.tap(find.byKey(const ValueKey('shortcut-remote')));
      await t.pumpAndSettle();
      expect(requests.single.url.path, '/api/v1/navigation/remote/open');
      expect(jsonDecode(requests.single.body), isEmpty);
      expect(find.byTooltip('导航设置'), findsNothing);
      expect(find.text('添加快捷导航'), findsNothing);
      final icon = t.getRect(find.byKey(const ValueKey('shortcut-remote')));
      final bar = t.getRect(find.byKey(const ValueKey('workbench-navigation')));
      expect(icon.center.dx, bar.center.dx);
      expect(icon.height, bar.height - 1);
    },
  );
  testWidgets(
    'title preference persists and long row scrolls without overflow',
    (t) async {
      t.view.physicalSize = const Size(640, 280);
      t.view.devicePixelRatio = 1;
      addTearDown(t.view.reset);
      controller.navigation['show_titles'] = true;
      controller.navigation['items'] = List.generate(
        20,
        (i) => {
          'id': 'entry-$i',
          'title': '常用应用 $i',
          'kind': 'agent',
          'target': 'com.example.app',
          'icon_name': 'app',
        },
      );
      await t.pumpWidget(
        MaterialApp(
          home: Scaffold(
            body: Align(
              alignment: Alignment.bottomCenter,
              child: WorkbenchNavigation(controller: controller),
            ),
          ),
        ),
      );
      expect(find.text('常用应用 0'), findsOneWidget);
      expect(t.takeException(), isNull);
      final row = find.byType(ListView);
      await t.drag(row, const Offset(-1100, 0));
      await t.pumpAndSettle();
      expect(t.takeException(), isNull);
    },
  );
  testWidgets('external icon never receives device credentials', (t) async {
    final item = {'icon_url': 'https://icons.example/icon.png'};
    final widget = shortcutIcon(item, controller) as Image;
    expect((widget.image as NetworkImage).headers, isNull);
    final internal =
        shortcutIcon({
              'icon_url': '/api/v1/navigation/icons/${'a' * 64}',
            }, controller)
            as Image;
    expect((internal.image as NetworkImage).headers, controller.headers);
  });
}
