import 'dart:convert';
import 'package:flutter/material.dart';
import 'package:flutter/services.dart';
import 'package:flutter_test/flutter_test.dart';
import 'package:mobile_scanner/mobile_scanner.dart';
import 'package:orbit_app/connection_config.dart';
import 'package:orbit_app/main.dart';

const testToken = 'orbit-test-device-token-0123456789';
const pairing = {
  'type': 'orbit-app',
  'version': 1,
  'server': 'https://orbit.example.com',
  'token': testToken,
};

void main() {
  test('reads console pairing format and normalizes fields', () {
    final config = ConnectionConfig.fromQr(
      jsonEncode({...pairing, 'server': ' https://orbit.example.com/ '}),
    );
    expect(config.server, pairing['server']);
    expect(config.token, testToken);
  });

  test('rejects unrelated, incomplete, unsafe and future QR payloads', () {
    final invalid = [
      'https://example.com',
      'null',
      '[]',
      jsonEncode({...pairing, 'type': 'orbit-mqtt'}),
      jsonEncode({...pairing, 'version': 2}),
      jsonEncode({...pairing, 'token': null}),
      jsonEncode({...pairing, 'server': 1}),
      jsonEncode({...pairing, 'token': 'short'}),
      for (final server in [
        'http://orbit.example.com',
        'javascript:alert(1)',
        'https://user:pass@example.com',
        'https://example.com?token=secret',
        'https://example.com#secret',
      ])
        jsonEncode({...pairing, 'server': server}),
    ];
    for (final value in invalid) {
      expect(() => ConnectionConfig.fromQr(value), throwsFormatException);
    }
  });

  const channel = MethodChannel(
    'dev.steenbakker.mobile_scanner/scanner/method',
  );
  const events = MethodChannel('dev.steenbakker.mobile_scanner/scanner/event');
  final calls = <String>[];
  var permission = 1;
  setUp(() {
    calls.clear();
    permission = 1;
    final messenger =
        TestDefaultBinaryMessengerBinding.instance.defaultBinaryMessenger;
    messenger.setMockMethodCallHandler(events, (_) async => null);
    messenger.setMockMethodCallHandler(channel, (call) async {
      calls.add(call.method);
      if (call.method == 'state') return permission;
      if (call.method == 'start') {
        return {
          'textureId': 1,
          'size': {'width': 640.0, 'height': 480.0},
          'cameraDirection': 0,
          'handlesCropAndRotation': true,
        };
      }
      return null;
    });
  });
  tearDown(() {
    final messenger =
        TestDefaultBinaryMessengerBinding.instance.defaultBinaryMessenger;
    messenger.setMockMethodCallHandler(channel, null);
    messenger.setMockMethodCallHandler(events, null);
  });

  testWidgets('scan fills both fields once and waits for explicit connection', (
    tester,
  ) async {
    Map<String, dynamic>? saved;
    await tester.pumpWidget(
      MaterialApp(
        home: ConnectionScreen(
          initial: const {
            'server': 'https://old.example.com',
            'token': testToken,
          },
          onConnect: (config) async => saved = config,
        ),
      ),
    );
    await tester.tap(find.text('扫码填写'));
    await tester.pumpAndSettle();
    final detect = tester
        .widget<MobileScanner>(find.byType(MobileScanner))
        .onDetect!;
    detect(BarcodeCapture(barcodes: const [Barcode(rawValue: 'unrelated')]));
    await tester.pump();
    expect(find.textContaining('二维码无效'), findsOneWidget);
    expect(saved, isNull);
    final capture = BarcodeCapture(
      barcodes: [Barcode(rawValue: jsonEncode(pairing))],
    );
    detect(capture);
    detect(capture);
    await tester.pumpAndSettle();
    expect(find.byType(MobileScanner), findsNothing);
    final fields = tester
        .widgetList<TextField>(find.byType(TextField))
        .toList();
    expect(fields[0].controller!.text, pairing['server']);
    expect(fields[1].controller!.text, testToken);
    expect(saved, isNull);
    expect(calls, contains('stop'));
    await tester.scrollUntilVisible(
      find.text('连接'),
      160,
      scrollable: find.byType(Scrollable).first,
    );
    await tester.tap(find.text('连接'));
    await tester.pumpAndSettle();
    expect(saved, {'server': pairing['server'], 'token': testToken});
    expect(tester.takeException(), isNull);
  });

  testWidgets('denied camera access and cancellation preserve typed values', (
    tester,
  ) async {
    permission = 2;
    await tester.pumpWidget(
      MaterialApp(
        home: ConnectionScreen(
          initial: const {
            'server': 'https://old.example.com',
            'token': testToken,
          },
          onConnect: (_) async => fail('must not connect'),
        ),
      ),
    );
    await tester.tap(find.text('扫码填写'));
    await tester.pumpAndSettle();
    expect(find.textContaining('未获得相机权限'), findsOneWidget);
    await tester.tap(find.text('返回填写'));
    await tester.pumpAndSettle();
    final fields = tester
        .widgetList<TextField>(find.byType(TextField))
        .toList();
    expect(fields[0].controller!.text, 'https://old.example.com');
    expect(fields[1].controller!.text, testToken);
    expect(tester.takeException(), isNull);
  });
}
