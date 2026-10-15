import 'package:flutter/material.dart';
import 'package:flutter/services.dart';
import 'package:flutter_secure_storage/flutter_secure_storage.dart';
import 'package:flutter_test/flutter_test.dart';
import 'package:orbit_app/matrix_display.dart';
import 'package:orbit_app/matrix_screen.dart';

void main() {
  TestWidgetsFlutterBinding.ensureInitialized();
  final calls = <bool>[];
  setUp(() {
    FlutterSecureStorage.setMockInitialValues({});
    calls.clear();
    TestDefaultBinaryMessengerBinding.instance.defaultBinaryMessenger
        .setMockMethodCallHandler(const MethodChannel('dev.orbit/node'), (
          call,
        ) async {
          if (call.method == 'matrixFullscreen') {
            calls.add(call.arguments as bool);
          }
          return null;
        });
  });
  tearDown(
    () => TestDefaultBinaryMessengerBinding.instance.defaultBinaryMessenger
        .setMockMethodCallHandler(const MethodChannel('dev.orbit/node'), null),
  );

  for (final size in [
    const Size(800, 360),
    const Size(360, 800),
    const Size(640, 280),
  ]) {
    testWidgets('settings, persistence and fullscreen restoration at $size', (
      tester,
    ) async {
      tester.view.physicalSize = size;
      tester.view.devicePixelRatio = 1;
      addTearDown(tester.view.resetPhysicalSize);
      addTearDown(tester.view.resetDevicePixelRatio);
      await tester.pumpWidget(const MaterialApp(home: MatrixScreen()));
      await tester.pumpAndSettle();
      expect(find.byType(Slider), findsNWidgets(3));
      final slider = find.byType(Slider).first;
      await tester.ensureVisible(slider);
      await tester.tapAt(tester.getCenter(slider));
      await tester.pumpAndSettle();
      expect(
        tester.widget<MatrixDisplay>(find.byType(MatrixDisplay)).style.density,
        96,
      );
      final saved = await const FlutterSecureStorage().read(
        key: 'matrix_style',
      );
      expect(saved, contains('96'));
      await tester.ensureVisible(find.text('横向全屏'));
      await tester.tap(find.text('横向全屏'));
      await tester.pumpAndSettle();
      expect(calls, [true]);
      final display = tester.getSize(find.byType(MatrixDisplay).last);
      expect(display.width, greaterThan(display.height));
      await tester.tapAt(tester.getCenter(find.byType(MatrixFullscreen)));
      await tester.pumpAndSettle();
      expect(calls, [true, false]);
      expect(tester.takeException(), isNull);
      await tester.pumpWidget(const SizedBox());
      await tester.pumpWidget(const MaterialApp(home: MatrixScreen()));
      await tester.pumpAndSettle();
      expect(
        tester.widget<MatrixDisplay>(find.byType(MatrixDisplay)).style.density,
        96,
      );
      await tester.pumpWidget(const SizedBox());
    });
  }
}
