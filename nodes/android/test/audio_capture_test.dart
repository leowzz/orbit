import 'package:flutter/material.dart';
import 'package:flutter/services.dart';
import 'package:flutter_test/flutter_test.dart';
import 'package:orbit_android/audio_capture.dart';

void main() {
  const channel = MethodChannel('dev.orbit/node');
  final calls = <String>[];
  Map<String, Object> state = {};
  setUp(() {
    calls.clear();
    state = {'permission': false, 'active': false};
    TestDefaultBinaryMessengerBinding.instance.defaultBinaryMessenger
        .setMockMethodCallHandler(channel, (call) async {
          calls.add(call.method);
          if (call.method == 'audioStatus') return state;
          return null;
        });
  });
  tearDown(
    () => TestDefaultBinaryMessengerBinding.instance.defaultBinaryMessenger
        .setMockMethodCallHandler(channel, null),
  );

  testWidgets('denied microphone offers retry and settings in narrow layout', (
    tester,
  ) async {
    tester.view.physicalSize = const Size(360, 640);
    tester.view.devicePixelRatio = 1;
    addTearDown(tester.view.resetPhysicalSize);
    addTearDown(tester.view.resetDevicePixelRatio);
    await tester.pumpWidget(
      const MaterialApp(home: Scaffold(body: AudioCaptureCard())),
    );
    await tester.pumpAndSettle();
    expect(find.text('申请麦克风权限'), findsOneWidget);
    await tester.tap(find.text('申请麦克风权限'));
    await tester.pumpAndSettle();
    expect(calls, contains('audioStart'));
    await tester.tap(find.text('打开设置'));
    await tester.pumpAndSettle();
    expect(calls, contains('appSettings'));
    expect(tester.takeException(), isNull);
    await tester.pumpWidget(const SizedBox());
  });

  testWidgets(
    'capture and upload status remain distinct and recording can stop',
    (tester) async {
      state = {
        'permission': true,
        'active': true,
        'state': '正在后台录音',
        'transfer': '无法上传 · 录音保存在本机，将自动重试',
      };
      await tester.pumpWidget(
        const MaterialApp(home: Scaffold(body: AudioCaptureCard())),
      );
      await tester.pumpAndSettle();
      expect(find.text('正在后台录音'), findsOneWidget);
      expect(find.textContaining('无法上传'), findsOneWidget);
      await tester.tap(find.text('停止录音'));
      await tester.pumpAndSettle();
      expect(calls, contains('audioStop'));
      expect(tester.takeException(), isNull);
      await tester.pumpWidget(const SizedBox());
    },
  );
}
