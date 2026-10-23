import 'dart:async';
import 'dart:convert';
import 'dart:io';
import 'package:flutter/services.dart';
import 'package:flutter_test/flutter_test.dart';
import 'package:http/http.dart' as http;
import 'package:http/testing.dart';
import 'package:orbit_app/app_mqtt_registration.dart';
import 'package:orbit_app/inbox_controller.dart';
import 'package:orbit_app/local_store.dart';
import 'package:sqflite_common_ffi/sqflite_ffi.dart';

void main() {
  TestWidgetsFlutterBinding.ensureInitialized();
  sqfliteFfiInit();
  const credentials = {
    'uri': 'mqtts://broker.test:8883',
    'nodeId': 'phone',
    'username': 'orbit-app-test',
    'password': 'test-password',
  };
  final saved = <Object?>[];
  late Directory dir;
  late InboxController controller;
  late Future<http.Response> Function(http.Request) respond;
  var time = DateTime.utc(2026);
  var requests = 0;
  var nativeAccepts = true;
  http.Response enabled() =>
      http.Response(jsonEncode({'enabled': true, 'mqtt': credentials}), 200);

  setUp(() async {
    saved.clear();
    requests = 0;
    nativeAccepts = true;
    time = DateTime.utc(2026);
    respond = (_) async => enabled();
    dir = await Directory.systemTemp.createTemp('orbit-mqtt-test-');
    final local = await LocalStore.open(
      databaseFactoryFfi,
      '${dir.path}/test.db',
    );
    controller = InboxController(
      local,
      'https://core.test',
      'app-token',
      dir.path,
      client: MockClient((request) async {
        requests++;
        expect(request.method, 'POST');
        expect(request.url.path, '/api/v1/mqtt');
        expect(request.headers['Authorization'], 'Bearer app-token');
        return respond(request);
      }),
    );
    TestDefaultBinaryMessengerBinding.instance.defaultBinaryMessenger
        .setMockMethodCallHandler(const MethodChannel('dev.orbit/node'), (
          call,
        ) async {
          expect(call.method, 'appMqttConnection');
          saved.add(call.arguments);
          return nativeAccepts;
        });
  });
  tearDown(() async {
    await controller.close();
    await dir.delete(recursive: true);
    TestDefaultBinaryMessengerBinding.instance.defaultBinaryMessenger
        .setMockMethodCallHandler(const MethodChannel('dev.orbit/node'), null);
  });
  AppMqttRegistration registration() =>
      AppMqttRegistration(controller, now: () => time);

  test(
    'first connection and restored installs claim with the existing token',
    () async {
      for (var i = 0; i < 2; i++) {
        final r = registration();
        await r.sync();
        await r.sync();
        expect(requests, i + 1);
        expect(saved.last, {
          'server': controller.server,
          'token': controller.token,
          'mqtt': credentials,
        });
        r.close();
      }
    },
  );

  for (final code in [404, 503, 200]) {
    test(
      'old or unavailable Core ($code) leaves HTTP usable and retries',
      () async {
        respond = (_) async => http.Response('{"enabled":false}', code);
        final r = registration();
        await r.sync();
        await r.sync();
        expect(requests, 1);
        expect(saved, isEmpty);
        time = time.add(const Duration(minutes: 1));
        respond = (_) async => enabled();
        await r.sync();
        expect(requests, 2);
        expect(saved, hasLength(1));
        r.close();
      },
    );
  }

  test(
    'switching connection discards the previous in-flight credentials',
    () async {
      final pending = Completer<http.Response>();
      respond = (_) => pending.future;
      final r = registration();
      final request = r.sync();
      r.close();
      pending.complete(enabled());
      await request;
      expect(saved, isEmpty);
    },
  );

  test(
    'native ownership rejection is not mistaken for saved credentials',
    () async {
      nativeAccepts = false;
      final r = registration();
      await r.sync();
      nativeAccepts = true;
      time = time.add(const Duration(minutes: 1));
      await r.sync();
      await r.sync();
      expect(requests, 2);
      r.close();
    },
  );
}
