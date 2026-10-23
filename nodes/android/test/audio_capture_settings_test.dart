import 'package:flutter/material.dart';
import 'package:flutter/services.dart';
import 'package:flutter_test/flutter_test.dart';
import 'package:orbit_android/audio_capture.dart';
import 'package:orbit_android/audio_capture_settings.dart';

void main() {
  const channel = MethodChannel('dev.orbit/node');
  const defaults = {
    'minSpeechMs': 300,
    'silenceMs': 600,
    'preRollMs': 300,
    'maxSegmentMs': 10000,
    'vadMode': 2,
  };
  late Map<String, int> settings;
  late List<MethodCall> calls;
  bool readFails = false;
  bool saveFails = false;

  setUp(() {
    settings = {...defaults};
    calls = [];
    readFails = false;
    saveFails = false;
    TestDefaultBinaryMessengerBinding.instance.defaultBinaryMessenger
        .setMockMethodCallHandler(channel, (call) async {
          calls.add(call);
          if (call.method == 'audioStatus') {
            if (readFails) throw PlatformException(code: 'read_failed');
            return {
              'active': true,
              'permission': true,
              'state': '正在后台录音',
              'settings': settings,
            };
          }
          if (call.method == 'audioSaveSettings') {
            if (saveFails) throw PlatformException(code: 'save_failed');
            settings = Map<String, int>.from(call.arguments as Map);
            return settings;
          }
          return null;
        });
  });

  tearDown(() {
    TestDefaultBinaryMessengerBinding.instance.defaultBinaryMessenger
        .setMockMethodCallHandler(channel, null);
  });

  Future<void> open(WidgetTester tester) async {
    await tester.pumpWidget(
      const MaterialApp(
        home: Scaffold(body: SingleChildScrollView(child: AudioCaptureCard())),
      ),
    );
    await tester.pumpAndSettle();
    await tester.tap(find.text('录音设置'));
    await tester.pumpAndSettle();
  }

  testWidgets(
    'saving settings persists through reopen without stopping capture',
    (tester) async {
      await open(tester);
      final slider = find.byKey(const ValueKey('minSpeechMs'));
      await tester.drag(slider, const Offset(120, 0));
      await tester.pumpAndSettle();
      final changed = tester.widget<Slider>(slider).value.round();
      expect(changed, isNot(defaults['minSpeechMs']));
      // The capture card still polls while the settings screen owns its draft.
      await tester.pump(const Duration(seconds: 3));
      expect(tester.widget<Slider>(slider).value.round(), changed);
      await tester.tap(find.byType(DropdownButton<int>));
      await tester.pumpAndSettle();
      await tester.tap(find.text('宽松').last);
      await tester.pumpAndSettle();
      await tester.tap(find.text('保存'));
      await tester.pumpAndSettle();
      expect(settings, {...defaults, 'minSpeechMs': changed, 'vadMode': 0});
      expect(find.text('已保存录音设置'), findsOneWidget);
      expect(calls.where((c) => c.method == 'audioSaveSettings'), hasLength(1));
      expect(
        calls.where((c) => ['audioStop', 'audioStart'].contains(c.method)),
        isEmpty,
      );
      await tester.pageBack();
      await tester.pumpAndSettle();
      await tester.tap(find.text('录音设置'));
      await tester.pumpAndSettle();
      expect(tester.widget<Slider>(slider).value.round(), changed);
      expect(find.text('宽松'), findsOneWidget);
      await tester.pumpWidget(const SizedBox());
    },
  );

  testWidgets('failed save retains draft and can be retried', (tester) async {
    await open(tester);
    final slider = find.byKey(const ValueKey('minSpeechMs'));
    await tester.drag(slider, const Offset(120, 0));
    await tester.pumpAndSettle();
    final changed = tester.widget<Slider>(slider).value.round();
    saveFails = true;
    await tester.tap(find.text('保存'));
    await tester.pumpAndSettle();
    expect(find.text('未能保存录音设置，修改已保留，请重试'), findsOneWidget);
    expect(settings, defaults);
    expect(tester.widget<Slider>(slider).value.round(), changed);
    saveFails = false;
    await tester.tap(find.text('保存'));
    await tester.pumpAndSettle();
    expect(settings['minSpeechMs'], changed);
    await tester.pumpWidget(const SizedBox());
  });

  testWidgets('load failure offers retry without editable defaults', (
    tester,
  ) async {
    readFails = true;
    await tester.pumpWidget(
      const MaterialApp(home: AudioCaptureSettingsScreen()),
    );
    await tester.pumpAndSettle();
    expect(find.text('未能读取录音设置，请重试'), findsOneWidget);
    expect(find.byType(Slider), findsNothing);
    expect(
      tester
          .widget<TextButton>(find.widgetWithText(TextButton, '保存'))
          .onPressed,
      isNull,
    );
    readFails = false;
    await tester.tap(find.text('重试'));
    await tester.pumpAndSettle();
    expect(find.byKey(const ValueKey('minSpeechMs')), findsOneWidget);
    expect(tester.takeException(), isNull);
  });

  testWidgets('narrow settings scroll, reset only changes draft until saved', (
    tester,
  ) async {
    tester.view.physicalSize = const Size(320, 568);
    tester.view.devicePixelRatio = 1;
    addTearDown(tester.view.resetPhysicalSize);
    addTearDown(tester.view.resetDevicePixelRatio);
    settings = {...defaults, 'minSpeechMs': 1000, 'vadMode': 0};
    await tester.pumpWidget(
      MaterialApp(
        builder: (context, child) => MediaQuery(
          data: MediaQuery.of(
            context,
          ).copyWith(textScaler: TextScaler.linear(1.3)),
          child: child!,
        ),
        home: const AudioCaptureSettingsScreen(),
      ),
    );
    await tester.pumpAndSettle();
    expect(tester.takeException(), isNull);
    await tester.scrollUntilVisible(find.text('恢复默认值'), 200);
    await tester.tap(find.text('恢复默认值'));
    await tester.pumpAndSettle();
    expect(settings['minSpeechMs'], 1000);
    await tester.tap(find.text('保存'));
    await tester.pumpAndSettle();
    expect(settings, defaults);
    expect(tester.takeException(), isNull);
    await tester.scrollUntilVisible(find.text('人声连续时长阈值'), -200);
    expect(find.text('300 毫秒'), findsWidgets);
    expect(tester.takeException(), isNull);
  });

  test('duration labels retain exact values', () {
    expect(audioDuration(300), '300 毫秒');
    expect(audioDuration(1000), '1 秒');
    expect(audioDuration(1500), '1.5 秒');
    expect(audioDuration(1520), '1.52 秒');
    expect(audioDuration(10000), '10 秒');
  });
}
