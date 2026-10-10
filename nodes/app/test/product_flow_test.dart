import 'dart:convert';
import 'dart:io';
import 'dart:ui' as ui;
import 'package:flutter/material.dart';
import 'package:flutter/rendering.dart';
import 'package:flutter_test/flutter_test.dart';
import 'package:orbit_app/inbox_controller.dart';
import 'package:orbit_app/inbox_screen.dart';
import 'package:orbit_app/local_store.dart';
import 'package:orbit_app/orbit_home.dart';
import 'package:sqflite_common_ffi/sqflite_ffi.dart';
import 'sync_test.dart' show item, page;
import 'widget_test.dart' show waitForLocalUpdate;

void main() {
  sqfliteFfiInit();
  test('message links exclude credentials and executable schemes', () {
    expect(
      messageLinks(
        'https://example.com/a。 javascript:alert(1) https://u:p@example.com',
      ),
      [Uri.parse('https://example.com/a')],
    );
  });
  for (final width in [360.0, 900.0]) {
    testWidgets('capture, find and copy flow at $width', (tester) async {
      tester.view.physicalSize = Size(width, 800);
      tester.view.devicePixelRatio = 1;
      addTearDown(tester.view.resetPhysicalSize);
      addTearDown(tester.view.resetDevicePixelRatio);
      late Directory dir;
      late InboxController c;
      await tester.runAsync(() async {
        dir = await Directory.systemTemp.createTemp('orbit-product-');
        final local = await LocalStore.open(
          databaseFactoryFfi,
          '${dir.path}/inbox.sqlite',
        );
        await local.applyPage(
          page('1', [
            {
              ...item('1', body: 'https://example.com/article\n到家再看这篇文章'),
              'id': 'link',
              'kind': 'text',
            },
            {...item('1', body: '到公司检查同步体验'), 'id': 'todo'},
          ]),
        );
        c = InboxController(local, 'https://example.test', 'test', dir.path);
        await c.load();
      });
      if (Platform.environment['ORBIT_CAPTURE'] != null) {
        await tester.runAsync(() async {
          await ui.loadFontFromList(
            await File('/System/Library/Fonts/STHeiti Light.ttc').readAsBytes(),
            fontFamily: 'Capture',
          );
          await ui.loadFontFromList(
            await File(
              'build/unit_test_assets/fonts/MaterialIcons-Regular.otf',
            ).readAsBytes(),
            fontFamily: 'MaterialIcons',
          );
        });
      }
      final capture = GlobalKey();
      await tester.pumpWidget(
        RepaintBoundary(
          key: capture,
          child: MaterialApp(
            debugShowCheckedModeBanner: false,
            theme: ThemeData(
              colorScheme: ColorScheme.fromSeed(
                seedColor: const Color(0xff277765),
              ),
              fontFamily: Platform.environment['ORBIT_CAPTURE'] != null
                  ? 'Capture'
                  : null,
            ),
            home: OrbitHome(
              controller: c,
              widgetsEnabled: true,
              inbox: InboxScreen(controller: c),
            ),
          ),
        ),
      );
      await tester.pumpAndSettle();
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
      expect(find.byTooltip('复制消息'), findsNWidgets(2));
      expect(find.text('example.com'), findsOneWidget);
      expect(tester.takeException(), isNull);
      if (Platform.environment['ORBIT_CAPTURE'] != null) {
        final boundary =
            capture.currentContext!.findRenderObject() as RenderRepaintBoundary;
        await tester.runAsync(() async {
          final image = await boundary.toImage();
          final bytes = await image.toByteData(format: ui.ImageByteFormat.png);
          await File(
            '/tmp/orbit-app-${width.toInt()}.png',
          ).writeAsBytes(bytes!.buffer.asUint8List());
          image.dispose();
        });
      }
      await tester.tap(find.byTooltip('搜索消息'));
      await tester.pumpAndSettle();
      await tester.enterText(find.widgetWithText(TextField, '搜索文字、链接…'), '到公司');
      await tester.pump(const Duration(milliseconds: 200));
      await tester.runAsync(c.load);
      await tester.pumpAndSettle();
      expect(find.text('到公司检查同步体验'), findsOneWidget);
      expect(find.text('example.com'), findsNothing);
      await tester.tap(find.byTooltip('更多操作'));
      await tester.pumpAndSettle();
      await tester.tap(find.text('编辑'));
      await tester.pumpAndSettle();
      expect(find.text('编辑消息'), findsOneWidget);
      await tester.enterText(find.widgetWithText(TextField, '写点什么…'), '编辑后的待办');
      await tester.tap(find.text('保存'));
      await waitForLocalUpdate(tester, () => c.pending.isNotEmpty);
      expect(jsonDecode(c.pending.single['payload'])['body'], '编辑后的待办');
      expect(tester.takeException(), isNull);
      await tester.tap(find.text('状态与组件'));
      await tester.pumpAndSettle();
      expect(find.text('Android 桌面组件'), findsOneWidget);
      expect(find.text('开始同步'), findsNothing);
      expect(tester.takeException(), isNull);
      await tester.pumpWidget(const SizedBox());
      await tester.runAsync(() async {
        await c.close();
        await dir.delete(recursive: true);
      });
    });
  }
}
