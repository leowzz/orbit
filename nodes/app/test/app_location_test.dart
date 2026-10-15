import 'package:flutter/material.dart';
import 'package:flutter/services.dart';
import 'package:flutter_secure_storage/flutter_secure_storage.dart';
import 'package:flutter_test/flutter_test.dart';
import 'package:orbit_app/app_location.dart';
import 'package:orbit_app/main.dart';
import 'package:orbit_app/matrix_screen.dart';
import 'package:orbit_app/workbench_screen.dart';

void main() {
  TestWidgetsFlutterBinding.ensureInitialized();
  final fullscreenCalls = <bool>[];
  setUp(() {
    FlutterSecureStorage.setMockInitialValues({});
    fullscreenCalls.clear();
    TestDefaultBinaryMessengerBinding.instance.defaultBinaryMessenger
        .setMockMethodCallHandler(const MethodChannel('dev.orbit/node'), (
          call,
        ) async {
          if (call.method == 'matrixFullscreen') {
            fullscreenCalls.add(call.arguments as bool);
          }
          return null;
        });
  });
  tearDown(() {
    TestDefaultBinaryMessengerBinding.instance.defaultBinaryMessenger
        .setMockMethodCallHandler(const MethodChannel('dev.orbit/node'), null);
  });

  test(
    'unknown destinations default to inbox and rapid writes keep the latest',
    () async {
      FlutterSecureStorage.setMockInitialValues({
        AppLocationStore.key: 'removed-page',
      });
      final store = AppLocationStore();
      expect(await store.read(), AppLocation.inbox);
      await Future.wait([
        store.write(AppLocation.workbench),
        store.write(AppLocation.settings),
        store.write(AppLocation.data),
      ]);
      expect(await AppLocationStore().read(), AppLocation.data);
    },
  );

  for (final entry in {'收件箱': 0, '工作台': 1, '数据展示': 2, '设置': 3}.entries) {
    testWidgets('cold start restores ${entry.key}', (tester) async {
      await tester.pumpWidget(const OrbitApp());
      await tester.pumpAndSettle();
      await tester.tap(find.widgetWithText(NavigationDestination, entry.key));
      await tester.pumpAndSettle();
      await tester.pumpWidget(const SizedBox());
      await tester.pumpAndSettle();
      await tester.pumpWidget(const OrbitApp());
      await tester.pumpAndSettle();
      expect(
        tester.widget<NavigationBar>(find.byType(NavigationBar)).selectedIndex,
        entry.value,
      );
      expect(tester.takeException(), isNull);
      await tester.pumpWidget(const SizedBox());
    });
  }

  for (final systemBack in [false, true]) {
    testWidgets(
      'workbench restores fullscreen and remembers exit (back=$systemBack)',
      (tester) async {
        await tester.pumpWidget(const OrbitApp());
        await tester.pumpAndSettle();
        await tester.tap(find.widgetWithText(NavigationDestination, '工作台'));
        await tester.pumpAndSettle();
        await tester.tap(find.byKey(const Key('workbench-fullscreen')));
        await tester.pumpAndSettle();
        expect(
          await AppLocationStore().read(),
          AppLocation.workbenchFullscreen,
        );
        await tester.pumpWidget(const SizedBox());
        await tester.pumpAndSettle();
        // Disposing the application must not be recorded as leaving fullscreen.
        expect(
          await AppLocationStore().read(),
          AppLocation.workbenchFullscreen,
        );
        await tester.pumpWidget(const OrbitApp());
        await tester.pumpAndSettle();
        expect(
          tester
              .widget<WorkbenchScreen>(find.byType(WorkbenchScreen))
              .fullscreen,
          isTrue,
        );
        expect(fullscreenCalls.last, isTrue);
        if (systemBack) {
          await tester.binding.handlePopRoute();
        } else {
          await tester.tap(find.byKey(const Key('workbench-fullscreen')));
        }
        await tester.pumpAndSettle();
        expect(await AppLocationStore().read(), AppLocation.workbench);
        expect(fullscreenCalls.last, isFalse);
        await tester.pumpWidget(const SizedBox());
        await tester.pumpAndSettle();
        await tester.pumpWidget(const OrbitApp());
        await tester.pumpAndSettle();
        expect(
          tester
              .widget<WorkbenchScreen>(find.byType(WorkbenchScreen))
              .fullscreen,
          isFalse,
        );
        expect(
          tester
              .widget<NavigationBar>(find.byType(NavigationBar))
              .selectedIndex,
          1,
        );
        expect(tester.takeException(), isNull);
        await tester.pumpWidget(const SizedBox());
      },
    );
  }

  testWidgets(
    'matrix restores its page and fullscreen, then tracks back navigation',
    (tester) async {
      await tester.pumpWidget(const OrbitApp());
      await tester.pumpAndSettle();
      await tester.tap(find.widgetWithText(NavigationDestination, '数据展示'));
      await tester.pumpAndSettle();
      await tester.tap(find.text('点阵屏'));
      await tester.pumpAndSettle();
      expect(await AppLocationStore().read(), AppLocation.matrix);
      await tester.pumpWidget(const SizedBox());
      await tester.pumpAndSettle();
      await tester.pumpWidget(const OrbitApp());
      await tester.pumpAndSettle();
      expect(find.byType(MatrixScreen), findsOneWidget);
      await tester.scrollUntilVisible(
        find.text('横向全屏'),
        200,
        scrollable: find
            .descendant(
              of: find.byType(MatrixScreen),
              matching: find.byType(Scrollable),
            )
            .first,
      );
      await tester.tap(find.text('横向全屏'));
      await tester.pumpAndSettle();
      await tester.pumpWidget(const SizedBox());
      await tester.pumpAndSettle();
      expect(await AppLocationStore().read(), AppLocation.matrixFullscreen);
      await tester.pumpWidget(const OrbitApp());
      await tester.pumpAndSettle();
      expect(find.byType(MatrixFullscreen), findsOneWidget);
      await tester.tapAt(tester.getCenter(find.byType(MatrixFullscreen)));
      await tester.pumpAndSettle();
      expect(await AppLocationStore().read(), AppLocation.matrix);
      await tester.binding.handlePopRoute();
      await tester.pumpAndSettle();
      expect(await AppLocationStore().read(), AppLocation.data);
      expect(
        tester.widget<NavigationBar>(find.byType(NavigationBar)).selectedIndex,
        2,
      );
      expect(tester.takeException(), isNull);
      await tester.pumpWidget(const SizedBox());
    },
  );
}
