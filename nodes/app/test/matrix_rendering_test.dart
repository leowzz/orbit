import 'dart:ui' as ui;
import 'package:flutter/foundation.dart';
import 'package:flutter/material.dart';
import 'package:flutter/rendering.dart';
import 'package:flutter_test/flutter_test.dart';
import 'package:orbit_app/matrix_display.dart';

final _now = DateTime.utc(2026, 10, 15, 8);
MatrixReading reading({
  double remaining = 72.5,
  String countdown = '4天 2时 31分',
}) => MatrixReading(
  remaining: remaining,
  reset: _now.add(const Duration(days: 4, hours: 2)),
  status: '实时',
  countdown: countdown,
  usage: '',
  hoursLeft: 98,
);

Future<void> show(
  WidgetTester tester,
  MatrixStyle style, {
  MatrixReading? value,
}) => tester.pumpWidget(
  MaterialApp(
    home: Center(
      child: SizedBox(
        width: 400,
        height: 180,
        child: MatrixDisplay(reading: value ?? reading(), style: style),
      ),
    ),
  ),
);

CustomPainter painter(WidgetTester tester) => tester
    .widget<CustomPaint>(
      find.descendant(
        of: find.byType(MatrixDisplay),
        matching: find.byType(CustomPaint),
      ),
    )
    .painter!;

Future<List<int>> pixels(WidgetTester tester) async {
  final boundary = tester.renderObject<RenderRepaintBoundary>(
    find.descendant(
      of: find.byType(MatrixDisplay),
      matching: find.byType(RepaintBoundary),
    ),
  );
  return (await tester.runAsync(() async {
    final image = await boundary.toImage();
    try {
      return (await image.toByteData(
        format: ui.ImageByteFormat.rawRgba,
      ))!.buffer.asUint8List().toList();
    } finally {
      image.dispose();
    }
  }))!;
}

Future<List<int>> waitForPixels(
  WidgetTester tester, {
  List<int>? expected,
}) async {
  for (var attempt = 0; attempt < 100; attempt++) {
    await tester.runAsync(
      () => Future<void>.delayed(const Duration(milliseconds: 20)),
    );
    await tester.pump();
    final result = await pixels(tester);
    final ready = expected == null
        ? result.asMap().entries.any(
            (entry) => entry.key % 4 != 3 && entry.value > 0,
          )
        : listEquals(result, expected);
    if (ready) return result;
  }
  fail('Matrix did not render the requested pixels');
}

void main() {
  testWidgets(
    'unchanged LED content and disabled glow reuse the painted frame',
    (tester) async {
      await show(tester, const MatrixStyle(density: 32, glowRadius: 0));
      await waitForPixels(tester);
      final before = painter(tester);
      await show(
        tester,
        const MatrixStyle(density: 32, glowRadius: 0, glowStrength: .9),
        value: reading(remaining: 72.6, countdown: '4天 2时 30分'),
      );
      await tester.pumpAndSettle();
      expect(painter(tester).shouldRepaint(before), isFalse);
      await tester.pumpWidget(const SizedBox());
      expect(tester.takeException(), isNull);
    },
  );

  testWidgets(
    'rapid changes converge to the final density and glow, and can dispose in flight',
    (tester) async {
      const finalStyle = MatrixStyle(
        density: 16,
        glowRadius: .5,
        glowStrength: .4,
      );
      await show(tester, finalStyle);
      final expected = await waitForPixels(tester);
      for (var density = 20; density <= 128; density += 4) {
        await show(
          tester,
          MatrixStyle(
            density: density,
            glowRadius: 2.5,
            glowStrength: density / 128,
          ),
        );
      }
      await show(tester, finalStyle);
      await waitForPixels(tester, expected: expected);
      await show(tester, const MatrixStyle(density: 128));
      await tester.pumpWidget(const SizedBox());
      await tester.runAsync(
        () => Future<void>.delayed(const Duration(milliseconds: 100)),
      );
      await tester.pump();
      expect(tester.takeException(), isNull);
    },
  );
}
