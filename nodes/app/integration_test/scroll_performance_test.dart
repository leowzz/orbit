import 'dart:convert';
import 'dart:io';
import 'dart:ui' show FramePhase, FrameTiming;
import 'package:flutter/material.dart';
import 'package:flutter_test/flutter_test.dart';
import 'package:integration_test/integration_test.dart';
import 'package:orbit_app/inbox_controller.dart';
import 'package:orbit_app/inbox_screen.dart';
import 'package:orbit_app/local_store.dart';
import 'package:orbit_app/orbit_home.dart';
import 'package:sqflite/sqflite.dart';

// Run on a physical device in profile mode. Only synthetic data in a separate
// temporary database is used. Always keep the installed app after the run:
// flutter drive --profile --keep-app-running --driver=test_driver/performance.dart
//   --target=integration_test/scroll_performance_test.dart -d DEVICE
// Reinstall the normal app entry point with adb install -r afterwards.
void main() {
  final binding = IntegrationTestWidgetsFlutterBinding.ensureInitialized()
    ..framePolicy = LiveTestWidgetsFlutterBindingFramePolicy.fullyLive;
  testWidgets('inbox scrolling at the device refresh rate', (tester) async {
    final dir = await Directory.systemTemp.createTemp('orbit-scroll-');
    final local = await LocalStore.open(databaseFactory, '${dir.path}/test.db');
    final controller = InboxController(
      local,
      'https://example.test',
      '',
      dir.path,
    );
    controller.items = List.generate(
      100,
      (i) => <String, dynamic>{
        'id': 'sample-$i',
        'revision': '1',
        'kind': i.isEven ? 'text' : 'todo',
        'body': List.generate(
          i % 3 == 0 ? 16 : 3,
          (line) => '滚动性能测试 $i / $line：长短消息混排，检查滚动时的流畅程度。',
        ).join('\n'),
        'completed': i % 5 == 0,
        'created_at': '2026-10-08T08:00:00Z',
      },
    );
    await tester.pumpWidget(
      MaterialApp(
        theme: ThemeData(
          useMaterial3: true,
          colorScheme: ColorScheme.fromSeed(
            seedColor: const Color(0xff277765),
            surface: const Color(0xfffafbf9),
          ),
        ),
        home: OrbitHome(
          widgetsEnabled: true,
          inbox: InboxScreen(controller: controller, onSettings: () {}),
        ),
      ),
    );
    await Future<void>.delayed(const Duration(seconds: 3));
    final list = find.byKey(const PageStorageKey('inbox'));
    final timings = <FrameTiming>[];
    binding.addTimingsCallback(timings.addAll);
    for (var swipe = 0; swipe < 16; swipe++) {
      final rect = tester.getRect(list);
      final up = swipe < 8;
      final gesture = await tester.startGesture(
        Offset(rect.center.dx, up ? rect.bottom - 80 : rect.top + 80),
      );
      for (var step = 0; step < 32; step++) {
        await Future<void>.delayed(const Duration(milliseconds: 8));
        await gesture.moveBy(Offset(0, up ? -12 : 12));
      }
      await gesture.up();
      await Future<void>.delayed(const Duration(milliseconds: 350));
    }
    await Future<void>.delayed(const Duration(seconds: 2));
    binding.removeTimingsCallback(timings.addAll);
    Map<String, dynamic> stats(List<double> values) {
      values.sort();
      return {
        'p50_ms': values[(values.length * .5).floor()],
        'p90_ms': values[(values.length * .9).floor()],
        'p99_ms': values[(values.length * .99).floor()],
        'max_ms': values.last,
        'over_8_33ms': values.where((v) => v > 1000 / 120).length,
      };
    }

    expect(timings.length, greaterThan(100));
    final intervals = <double>[];
    for (var i = 1; i < timings.length; i++) {
      final delta =
          (timings[i].timestampInMicroseconds(FramePhase.vsyncStart) -
              timings[i - 1].timestampInMicroseconds(FramePhase.vsyncStart)) /
          1000;
      if (delta > 0 && delta < 100) intervals.add(delta);
    }
    final report = <String, dynamic>{
      'frames': timings.length,
      'display_hz': tester.view.display.refreshRate,
      'build': stats(
        timings.map((t) => t.buildDuration.inMicroseconds / 1000).toList(),
      ),
      'raster': stats(
        timings.map((t) => t.rasterDuration.inMicroseconds / 1000).toList(),
      ),
      'vsync_interval': stats(intervals),
    };
    binding.reportData = report;
    // ignore: avoid_print
    print('ORBIT_SCROLL_PERF ${jsonEncode(report)}');
    expect(tester.takeException(), isNull);
    await tester.pumpWidget(const SizedBox());
    await controller.close();
    await dir.delete(recursive: true);
  });
}
