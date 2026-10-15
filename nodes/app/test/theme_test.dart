import 'package:flutter/material.dart';
import 'package:flutter/services.dart';
import 'package:flutter_secure_storage/flutter_secure_storage.dart';
import 'package:flutter_test/flutter_test.dart';
import 'package:orbit_app/app_theme.dart';
import 'package:orbit_app/app_appearance.dart';
import 'package:orbit_app/main.dart';

double contrast(Color foreground, Color background) {
  final a = foreground.computeLuminance(), b = background.computeLuminance();
  return a > b ? (a + .05) / (b + .05) : (b + .05) / (a + .05);
}

void main() {
  testWidgets('manual appearance persists and ignores system changes', (
    tester,
  ) async {
    FlutterSecureStorage.setMockInitialValues({});
    final platform = tester.binding.platformDispatcher;
    addTearDown(platform.clearPlatformBrightnessTestValue);
    platform.platformBrightnessTestValue = Brightness.dark;
    await tester.pumpWidget(const OrbitApp());
    await tester.pumpAndSettle();
    final field = find.widgetWithText(TextField, '服务地址');
    expect(Theme.of(tester.element(field)).brightness, Brightness.light);
    await tester.enterText(field, 'https://orbit.example.test');
    Future<void> tab(String title) async {
      await tester.tap(find.widgetWithText(NavigationDestination, title));
      await tester.pumpAndSettle();
    }

    await tab('设置');
    final toggle = find.widgetWithText(SwitchListTile, '暗色模式');
    await tester.tap(toggle);
    await tester.pumpAndSettle();
    expect(tester.widget<SwitchListTile>(toggle).value, isTrue);
    platform.platformBrightnessTestValue = Brightness.light;
    await tester.pumpAndSettle();
    expect(Theme.of(tester.element(toggle)).brightness, Brightness.dark);
    await tab('收件箱');
    expect(
      tester.widget<TextField>(field).controller!.text,
      'https://orbit.example.test',
    );
    final theme = Theme.of(tester.element(field));
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
      Brightness.light,
    );
    await tab('工作台');
    await tester.tap(find.byTooltip('进入全屏'));
    await tester.pumpAndSettle();
    await tester.tap(find.byTooltip('切换到浅色模式'));
    await tester.pumpAndSettle();
    expect(find.byTooltip('切换到暗色模式'), findsOneWidget);
    await tester.tap(find.byTooltip('切换到暗色模式'));
    await tester.pumpAndSettle();
    await tester.pumpWidget(const SizedBox());
    await tester.pumpWidget(const OrbitApp());
    await tester.pumpAndSettle();
    // Restoring the app and the fullscreen route retains the manual choice.
    expect(find.byTooltip('切换到浅色模式'), findsOneWidget);
    expect(find.byTooltip('退出全屏'), findsOneWidget);
    await tester.tap(find.byTooltip('切换到浅色模式'));
    await tester.pumpAndSettle();
    platform.platformBrightnessTestValue = Brightness.dark;
    await tester.pumpAndSettle();
    expect(find.byTooltip('切换到暗色模式'), findsOneWidget);
    await tester.pumpWidget(const SizedBox());
    await tester.pumpWidget(const OrbitApp());
    await tester.pumpAndSettle();
    expect(find.byTooltip('切换到暗色模式'), findsOneWidget);
    expect(tester.takeException(), isNull);
    await tester.pumpWidget(const SizedBox());
  });

  test(
    'Android appearance uses the same native preference as widgets',
    () async {
      var dark = false;
      const channel = MethodChannel('dev.orbit/node');
      TestDefaultBinaryMessengerBinding.instance.defaultBinaryMessenger
          .setMockMethodCallHandler(channel, (call) async {
            if (call.method == 'getDarkMode') return dark;
            if (call.method == 'setDarkMode') {
              dark = call.arguments as bool;
              return null;
            }
            throw MissingPluginException();
          });
      addTearDown(
        () => TestDefaultBinaryMessengerBinding.instance.defaultBinaryMessenger
            .setMockMethodCallHandler(channel, null),
      );
      final store = AppearanceStore(android: true);
      expect(await store.read(), isFalse);
      await store.write(true);
      expect(await AppearanceStore(android: true).read(), isTrue);
      await store.write(false);
      expect(await store.read(), isFalse);
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
