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
        'transferState': 'retrying',
        'sentPackets': 128,
        'sentBytes': 2048,
        'pending': 7,
        'inFlight': 2,
      };
      await tester.pumpWidget(
        const MaterialApp(home: Scaffold(body: AudioCaptureCard())),
      );
      await tester.pumpAndSettle();
      expect(find.text('正在录音'), findsOneWidget);
      expect(find.textContaining('无法上传'), findsOneWidget);
      expect(find.text('发送状态 · 等待重试'), findsOneWidget);
      expect(find.text('128'), findsOneWidget);
      expect(find.text('5'), findsOneWidget);
      expect(find.text('2'), findsOneWidget);
      expect(find.textContaining('已发送 2.0 KB'), findsOneWidget);
      await tester.tap(find.text('停止录音'));
      await tester.pumpAndSettle();
      expect(calls, contains('audioStop'));
      expect(tester.takeException(), isNull);
      await tester.pumpWidget(const SizedBox());
    },
  );

  testWidgets('stopped recording keeps polling upload metrics on small screens',
      (tester) async {
    tester.view.physicalSize = const Size(320, 720);
    tester.view.devicePixelRatio = 1;
    addTearDown(tester.view.resetPhysicalSize);
    addTearDown(tester.view.resetDevicePixelRatio);
    state = {
      'permission': true,
      'active': false,
      'state': '已停止录音',
      'transferState': 'sending',
      'pending': 4,
      'inFlight': 1,
      'sentPackets': 12,
    };
    await tester.pumpWidget(MaterialApp(
      theme: ThemeData.dark(useMaterial3: true),
      home: MediaQuery(
        data: const MediaQueryData(textScaler: TextScaler.linear(1.5)),
        child: const Scaffold(
            body: SingleChildScrollView(child: AudioCaptureCard())),
      ),
    ));
    await tester.pumpAndSettle();
    expect(find.text('录音未开启'), findsOneWidget);
    expect(find.text('发送状态 · 正在发送'), findsOneWidget);
    expect(find.text('12'), findsOneWidget);
    state = {
      ...state,
      'sentPackets': 16,
      'pending': 0,
      'inFlight': 0,
      'transferState': 'idle'
    };
    await tester.pump(const Duration(seconds: 2));
    await tester.pump();
    expect(find.text('16'), findsOneWidget);
    expect(find.text('发送状态 · 暂无待发送'), findsOneWidget);
    expect(tester.takeException(), isNull);
    await tester.pumpWidget(const SizedBox());
  });

  testWidgets('failed initial status does not display misleading zero counters',
      (tester) async {
    TestDefaultBinaryMessengerBinding.instance.defaultBinaryMessenger
        .setMockMethodCallHandler(
            channel, (_) async => throw PlatformException(code: 'unavailable'));
    await tester.pumpWidget(
        const MaterialApp(home: Scaffold(body: AudioCaptureCard())));
    await tester.pumpAndSettle();
    expect(find.text('未能读取录音状态，请重试'), findsOneWidget);
    expect(find.text('已发送包数'), findsNothing);
    expect(
        tester
            .widget<FilledButton>(
                find.byWidgetPredicate((widget) => widget is FilledButton))
            .onPressed,
        isNull);
    await tester.pumpWidget(const SizedBox());
  });
}
