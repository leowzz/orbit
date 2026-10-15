import 'package:flutter/material.dart';
import 'package:flutter/services.dart';
import 'package:flutter_secure_storage/flutter_secure_storage.dart';
import 'package:flutter_test/flutter_test.dart';
import 'package:orbit_app/app_theme.dart';
import 'package:orbit_app/main.dart';

double contrast(Color foreground, Color background) {
  final a = foreground.computeLuminance(), b = background.computeLuminance();
  return a > b ? (a + .05) / (b + .05) : (b + .05) / (a + .05);
}

void main() {
  testWidgets(
    'system theme changes preserve connection draft and system bars',
    (tester) async {
      FlutterSecureStorage.setMockInitialValues({});
      final platform = tester.binding.platformDispatcher;
      addTearDown(platform.clearPlatformBrightnessTestValue);
      platform.platformBrightnessTestValue = Brightness.light;
      await tester.pumpWidget(const OrbitApp());
      await tester.pumpAndSettle();
      final field = find.widgetWithText(TextField, '服务地址');
      await tester.enterText(field, 'https://orbit.example.test');
      for (final brightness in [Brightness.dark, Brightness.light]) {
        platform.platformBrightnessTestValue = brightness;
        await tester.pumpAndSettle();
        final context = tester.element(field);
        final theme = Theme.of(context);
        expect(theme.brightness, brightness);
        expect(
          tester.widget<TextField>(field).controller!.text,
          'https://orbit.example.test',
        );
        expect(
          contrast(
            theme.colorScheme.onSurface,
            theme.inputDecorationTheme.fillColor!,
          ),
          greaterThanOrEqualTo(4.5),
        );
        final bars = tester.widgetList<AnnotatedRegion<SystemUiOverlayStyle>>(
          find.byType(AnnotatedRegion<SystemUiOverlayStyle>),
        );
        expect(
          bars.first.value.systemNavigationBarIconBrightness,
          brightness == Brightness.dark ? Brightness.light : Brightness.dark,
        );
      }
      await tester.pumpWidget(const SizedBox());
    },
  );

  for (final brightness in Brightness.values) {
    test('semantic text colors remain readable in $brightness', () {
      final colors = OrbitColors(brightness == Brightness.dark);
      for (final color in [
        colors.ink,
        colors.muted,
        colors.accent,
        colors.success,
        colors.info,
        colors.warning,
        colors.error,
      ]) {
        if (brightness == Brightness.dark) {
          expect(contrast(color, colors.surface), greaterThanOrEqualTo(4.5));
        }
      }
      expect(
        contrast(colors.onAccent, colors.accent),
        greaterThanOrEqualTo(4.5),
      );
    });
  }
}
