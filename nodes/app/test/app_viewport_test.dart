import 'package:flutter/material.dart';
import 'package:flutter/services.dart';
import 'package:flutter_test/flutter_test.dart';
import 'package:orbit_app/app_viewport.dart';
import 'package:orbit_app/orbit_home.dart';

void main() {
  TestWidgetsFlutterBinding.ensureInitialized();
  String? deviceModel;
  setUp(() {
    deviceModel = null;
    TestDefaultBinaryMessengerBinding.instance.defaultBinaryMessenger
        .setMockMethodCallHandler(
          const MethodChannel('dev.orbit/node'),
          (call) async => call.method == 'deviceModel' ? deviceModel : null,
        );
  });
  tearDown(() {
    TestDefaultBinaryMessengerBinding.instance.defaultBinaryMessenger
        .setMockMethodCallHandler(const MethodChannel('dev.orbit/node'), null);
  });
  testWidgets('scaled routes, dialogs and keyboard use matching coordinates', (
    tester,
  ) async {
    deviceModel = 'NFCD_AlipayBoxN5DEL';
    tester.view.physicalSize = const Size(1600, 624);
    tester.view.devicePixelRatio = 2;
    tester.view.padding = const FakeViewPadding(top: 48);
    tester.view.viewPadding = const FakeViewPadding(top: 48);
    addTearDown(tester.view.reset);
    late MediaQueryData media;
    await tester.pumpWidget(
      MaterialApp(
        builder: (context, child) => AppViewport(child: child!),
        home: Builder(
          builder: (context) {
            media = MediaQuery.of(context);
            return Scaffold(
              body: SafeArea(
                child: Align(
                  alignment: Alignment.topLeft,
                  child: SizedBox(
                    width: 100,
                    height: 50,
                    child: FilledButton(
                      onPressed: () => Navigator.of(context).push(
                        MaterialPageRoute<void>(
                          builder: (context) => Scaffold(
                            appBar: AppBar(title: const Text('详情')),
                            body: Center(
                              child: TextButton(
                                onPressed: () => showDialog<void>(
                                  context: context,
                                  builder: (context) => AlertDialog(
                                    title: const Text('确认操作'),
                                    actions: [
                                      TextButton(
                                        onPressed: () => Navigator.pop(context),
                                        child: const Text('关闭'),
                                      ),
                                    ],
                                  ),
                                ),
                                child: const Text('打开弹窗'),
                              ),
                            ),
                          ),
                        ),
                      ),
                      child: const Text('打开'),
                    ),
                  ),
                ),
              ),
            );
          },
        ),
      ),
    );
    await tester.pumpAndSettle();
    expect(media.size.width, closeTo(800 / .85, .01));
    expect(media.devicePixelRatio, 1.7);
    expect(media.padding.top, closeTo(24 / .85, .01));
    expect(tester.getTopLeft(find.byType(FilledButton)).dy, closeTo(24, .01));
    final buttonWidth =
        tester.getTopRight(find.byType(FilledButton)).dx -
        tester.getTopLeft(find.byType(FilledButton)).dx;
    expect(buttonWidth, closeTo(85, .01));
    await tester.tap(find.text('打开'));
    await tester.pumpAndSettle();
    await tester.tap(find.text('打开弹窗'));
    await tester.pumpAndSettle();
    await tester.tap(find.text('关闭'));
    await tester.pumpAndSettle();
    expect(find.byType(AlertDialog), findsNothing);
    await tester.pageBack();
    await tester.pumpAndSettle();
    tester.view.viewInsets = const FakeViewPadding(bottom: 240);
    await tester.pumpAndSettle();
    expect(media.viewInsets.bottom, closeTo(120 / .85, .01));
    expect(media.size.width, closeTo(800 / .85, .01));
    expect(tester.takeException(), isNull);
  });

  for (final model in ['24031PN0DC', null]) {
    testWidgets('ordinary and unknown devices keep native scale on $model', (
      tester,
    ) async {
      deviceModel = model;
      tester.view.physicalSize = const Size(1600, 720);
      tester.view.devicePixelRatio = 2;
      addTearDown(tester.view.reset);
      late MediaQueryData media;
      await tester.pumpWidget(
        MaterialApp(
          builder: (_, child) => AppViewport(child: child!),
          home: Builder(
            builder: (context) {
              media = MediaQuery.of(context);
              expect(AppViewport.isAlipayBoxOf(context), isFalse);
              return const Scaffold();
            },
          ),
        ),
      );
      await tester.pumpAndSettle();
      expect(media.size, const Size(800, 360));
      expect(media.devicePixelRatio, 2);
      tester.view.viewInsets = const FakeViewPadding(bottom: 240);
      await tester.pumpAndSettle();
      expect(media.size, const Size(800, 360));
      expect(media.viewInsets.bottom, 120);
      expect(tester.takeException(), isNull);
    });
  }

  testWidgets('rail switches pages and rotation preserves the inbox draft', (
    tester,
  ) async {
    tester.view.physicalSize = const Size(800, 312);
    tester.view.devicePixelRatio = 1;
    addTearDown(tester.view.reset);
    await tester.pumpWidget(
      MaterialApp(
        builder: (context, child) => AppViewport(child: child!),
        home: const OrbitHome(
          widgetsEnabled: false,
          inbox: Scaffold(body: TextField()),
        ),
      ),
    );
    await tester.enterText(find.byType(TextField), '保留横屏草稿');
    final rail = find.byType(NavigationRail);
    await tester.tap(find.descendant(of: rail, matching: find.text('设置')));
    await tester.pumpAndSettle();
    expect(find.text('连接设置'), findsOneWidget);
    await tester.tap(find.descendant(of: rail, matching: find.text('收件箱')));
    await tester.pumpAndSettle();
    expect(find.text('保留横屏草稿'), findsOneWidget);
    tester.view.physicalSize = const Size(360, 800);
    await tester.pumpAndSettle();
    expect(rail, findsNothing);
    expect(find.byType(NavigationBar), findsOneWidget);
    expect(find.text('保留横屏草稿'), findsOneWidget);
    // A portrait keyboard must not trigger landscape scaling/navigation.
    tester.view.viewInsets = const FakeViewPadding(bottom: 400);
    await tester.pumpAndSettle();
    expect(rail, findsNothing);
    expect(tester.takeException(), isNull);
  });
}
