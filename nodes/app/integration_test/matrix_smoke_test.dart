import 'dart:io';
import 'package:flutter/material.dart';
import 'package:flutter_test/flutter_test.dart';
import 'package:integration_test/integration_test.dart';
import 'package:sqflite/sqflite.dart';
import 'package:orbit_app/inbox_controller.dart';
import 'package:orbit_app/local_store.dart';
import 'package:orbit_app/matrix_screen.dart';

void main() {
  final binding = IntegrationTestWidgetsFlutterBinding.ensureInitialized();
  testWidgets(
    'short landscape settings, live quota, full screen and restoration',
    (tester) async {
      final dir = await Directory.systemTemp.createTemp('orbit-matrix-test-');
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
      controller.view = {
        'codex': {
          'freshUntil': now.add(const Duration(hours: 1)).toIso8601String(),
          'weeklyLimit': {
            'remainingPercent': 72.5,
            'resetsAt': now
                .add(const Duration(days: 4, hours: 3, minutes: 21))
                .toIso8601String(),
            'observedAt': now.toIso8601String(),
            'freshUntil': now.add(const Duration(minutes: 3)).toIso8601String(),
          },
        },
        'usage': {
          'actualCostMicros': '12340000',
          'currencyCode': 'USD',
          'tokenCount': '1234567',
          'tpm': '18400',
          'freshUntil': now.add(const Duration(minutes: 3)).toIso8601String(),
        },
      };
      await tester.pumpWidget(
        MaterialApp(
          debugShowCheckedModeBanner: false,
          theme: ThemeData(colorSchemeSeed: const Color(0xff277765)),
          home: MatrixScreen(controller: controller),
        ),
      );
      await tester.pumpAndSettle();
      for (
        var attempt = 0;
        attempt < 20 &&
            tester.widget<FilledButton>(find.byType(FilledButton)).onPressed ==
                null;
        attempt++
      ) {
        await tester.pump(const Duration(milliseconds: 500));
      }
      expect(
        tester.widget<FilledButton>(find.byType(FilledButton)).onPressed,
        isNotNull,
      );
      await binding.convertFlutterSurfaceToImage();
      await tester.pump(const Duration(seconds: 2));
      expect(find.text('像素密度'), findsOneWidget);
      expect(find.text('光晕范围'), findsOneWidget);
      expect(find.text('光晕强度'), findsOneWidget);
      expect(find.text('横向全屏').hitTestable(), findsOneWidget);
      expect(
        tester.getRect(find.byType(Slider).last).bottom,
        lessThanOrEqualTo(tester.getRect(find.byType(FilledButton)).top),
      );
      await binding.takeScreenshot('matrix-settings-fixture');
      await tester.tap(find.text('横向全屏'));
      await tester.pumpAndSettle();
      await tester.pump(const Duration(seconds: 2));
      final screen = tester.getSize(find.byType(MatrixFullscreen));
      expect(screen.width, greaterThan(screen.height));
      await binding.takeScreenshot('matrix-fullscreen-fixture');
      expect(tester.takeException(), isNull);
      controller.online = false;
      await tester.pump(const Duration(seconds: 2));
      await tester.pumpAndSettle();
      await binding.takeScreenshot('matrix-offline-fixture');
      await tester.tapAt(Offset(screen.width / 2, screen.height / 2));
      await tester.pumpAndSettle();
      expect(find.text('横向全屏').hitTestable(), findsOneWidget);
      await tester.pumpWidget(const SizedBox());
      await controller.close();
      await dir.delete(recursive: true);
    },
  );
}
