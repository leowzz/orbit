import 'dart:convert';
import 'dart:io';
import 'package:flutter/material.dart';
import 'package:flutter_secure_storage/flutter_secure_storage.dart';
import 'package:flutter_test/flutter_test.dart';
import 'package:integration_test/integration_test.dart';
import 'package:orbit_app/app_viewport.dart';
import 'package:orbit_app/inbox_controller.dart';
import 'package:orbit_app/local_store.dart';
import 'package:orbit_app/matrix_screen.dart';
import 'package:sqflite/sqflite.dart';

void main() {
  final binding = IntegrationTestWidgetsFlutterBinding.ensureInitialized();
  testWidgets('matrix slider frame timings on a fixed local fixture', (
    tester,
  ) async {
    // Never read or overwrite the device's real connection or display settings.
    FlutterSecureStorage.setMockInitialValues({
      'matrix_style': jsonEncode({
        'density': 128,
        'glowRadius': 1.1,
        'glowStrength': .65,
      }),
    });
    final dir = await Directory.systemTemp.createTemp('orbit-matrix-perf-');
    final local = await LocalStore.open(
      databaseFactory,
      '${dir.path}/fixture.sqlite',
    );
    final controller = InboxController(
      local,
      'https://example.test',
      'fixture',
      dir.path,
    );
    controller.online = true;
    final now = DateTime.now();
    controller.view = {
      'codex': {
        'freshUntil': now.add(const Duration(hours: 1)).toIso8601String(),
        'weeklyLimit': {
          'remainingPercent': 73.0,
          'resetsAt': now
              .add(const Duration(days: 4, hours: 2))
              .toIso8601String(),
          'freshUntil': now.add(const Duration(hours: 1)).toIso8601String(),
        },
      },
    };
    await tester.pumpWidget(
      MaterialApp(
        debugShowCheckedModeBanner: false,
        theme: ThemeData(colorSchemeSeed: const Color(0xff277765)),
        builder: (context, child) => AppViewport(child: child!),
        home: MatrixScreen(controller: controller),
      ),
    );
    await tester.pumpAndSettle();
    final sliders = find.byType(Slider);
    for (final scenario in ['density', 'radius', 'strength']) {
      for (var i = 0; i < 3; i++) {
        tester.widget<Slider>(sliders.at(i)).onChanged!([128.0, 1.1, .65][i]);
        await tester.pump();
      }
      await tester.pumpAndSettle();
      final index = ['density', 'radius', 'strength'].indexOf(scenario);
      await tester.ensureVisible(sliders.at(index));
      final left = tester.getTopLeft(sliders.at(index));
      final right = tester.getBottomRight(sliders.at(index));
      final start = Offset(left.dx + 24, (left.dy + right.dy) / 2);
      final end = Offset(right.dx - 24, start.dy);
      await binding.watchPerformance(() async {
        for (var repeat = 0; repeat < 3; repeat++) {
          await tester.timedDragFrom(
            start,
            end - start,
            const Duration(seconds: 1),
          );
          await tester.pumpAndSettle();
          await tester.timedDragFrom(
            end,
            start - end,
            const Duration(seconds: 1),
          );
          await tester.pumpAndSettle();
        }
      }, reportKey: scenario);
    }
    for (var i = 0; i < 3; i++) {
      tester.widget<Slider>(sliders.at(i)).onChanged!([128.0, 1.1, .65][i]);
      await tester.pump();
    }
    await tester.pumpAndSettle();
    await binding.convertFlutterSurfaceToImage();
    await tester.pump();
    await binding.takeScreenshot('matrix-preview');
    expect(tester.takeException(), isNull);
    await tester.pumpWidget(const SizedBox());
    await controller.close();
    await dir.delete(recursive: true);
  });
}
