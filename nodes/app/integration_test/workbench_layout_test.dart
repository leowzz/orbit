import 'dart:io';
import 'package:flutter/material.dart';
import 'package:flutter/services.dart';
import 'package:flutter_test/flutter_test.dart';
import 'package:integration_test/integration_test.dart';
import 'package:sqflite/sqflite.dart';
import 'package:orbit_app/app_theme.dart';
import 'package:orbit_app/app_viewport.dart';
import 'package:orbit_app/inbox_controller.dart';
import 'package:orbit_app/local_store.dart';
import 'package:orbit_app/workbench_screen.dart';

void main() {
  final binding = IntegrationTestWidgetsFlutterBinding.ensureInitialized();
  testWidgets('metrics width, visibility and three-column session layout', (
    tester,
  ) async {
    final dir = await Directory.systemTemp.createTemp('orbit-workbench-test-');
    final local = await LocalStore.open(
      databaseFactory,
      '${dir.path}/test.sqlite',
    );
    final controller = InboxController(
      local,
      'https://example.test',
      'fixture',
      dir.path,
    );
    final now = DateTime.now();
    controller.online = true;
    controller.navigation = {
      'metrics_visible': true,
      'metrics_width': 150,
      'nfcd_metrics_left_padding': 0,
    };
    controller.view = {
      'displayKind': 'codex',
      'sessionsEnabled': true,
      'freshUntil': now.add(const Duration(minutes: 5)).toIso8601String(),
      'primary': {'text': '72%'},
      'secondary': {'text': '4 10/20'},
      'footer': {'text': '3 12:30'},
      'codex': {
        'runningCount': 1,
        'totalCount': 40,
        'sessions': List.generate(
          40,
          (i) => {
            'sessionId': 'fixture-$i',
            'displayName': i == 0 ? '验证会话标题超出卡片宽度后自动显示省略号' : '示例会话 ${i + 1}',
            'projectName': 'orbit',
            'model': 'gpt-6',
            'status': i == 0
                ? 'CODEX_SESSION_STATUS_RUNNING'
                : 'CODEX_SESSION_STATUS_COMPLETED',
            'updatedAt': now.subtract(Duration(minutes: i)).toIso8601String(),
          },
        ),
      },
    };
    try {
      await SystemChrome.setPreferredOrientations([
        DeviceOrientation.landscapeLeft,
      ]);
      await tester.pumpWidget(
        MaterialApp(
          debugShowCheckedModeBanner: false,
          theme: orbitTheme(Brightness.light),
          builder: (_, child) => AppViewport(child: child!),
          home: WorkbenchScreen(controller: controller, fullscreen: true),
        ),
      );
      await tester.pumpAndSettle();
      await binding.convertFlutterSurfaceToImage();
      await tester.pumpAndSettle();
      final metrics = find.byKey(const ValueKey('workbench-metrics'));
      expect(tester.getSize(metrics).width, closeTo(150, .01));
      await binding.takeScreenshot('metrics-default');
      controller.navigation['metrics_width'] = 320;
      controller.notifyListeners();
      await tester.pumpAndSettle();
      expect(tester.getSize(metrics).width, closeTo(320, .01));
      await binding.takeScreenshot('metrics-wide');
      controller.navigation['metrics_visible'] = false;
      controller.notifyListeners();
      await tester.pumpAndSettle();
      expect(metrics, findsNothing);
      final grid =
          tester.widget<SliverGrid>(find.byType(SliverGrid)).gridDelegate
              as SliverGridDelegateWithFixedCrossAxisCount;
      expect(grid.crossAxisCount, 3);
      final cards = List.generate(
        3,
        (i) => tester.getRect(find.byKey(ValueKey('session-fixture-$i'))),
      );
      expect(cards[0].top, closeTo(cards[2].top, .01));
      expect(cards[0].width, closeTo(cards[2].width, .01));
      expect(cards[0].width, greaterThanOrEqualTo(260));
      expect(tester.takeException(), isNull);
      await binding.takeScreenshot('metrics-hidden-three-columns');
    } finally {
      await tester.pumpWidget(const SizedBox());
      await controller.close();
      await dir.delete(recursive: true);
      await SystemChrome.setPreferredOrientations([]);
    }
  });
}
