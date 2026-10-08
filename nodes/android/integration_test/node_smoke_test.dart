import 'package:flutter/services.dart';
import 'package:flutter_test/flutter_test.dart';
import 'package:integration_test/integration_test.dart';
import 'package:orbit_android/main.dart';

void main() {
  IntegrationTestWidgetsFlutterBinding.ensureInitialized();
  testWidgets('native configuration, MQTT test and foreground service', (
    tester,
  ) async {
    const uri = String.fromEnvironment('ORBIT_MQTT_URI');
    if (uri.isEmpty) {
      fail('Supply a private --dart-define-from-file configuration.');
    }
    const config = {
      'uri': uri,
      'nodeId': String.fromEnvironment(
        'ORBIT_NODE_ID',
        defaultValue: 'android-phone',
      ),
      'username': String.fromEnvironment('ORBIT_MQTT_USERNAME'),
      'password': String.fromEnvironment('ORBIT_MQTT_PASSWORD'),
    };
    const channel = MethodChannel('dev.orbit/node');
    await tester.pumpWidget(const OrbitApp());
    await tester.pumpAndSettle();
    final test = await channel.invokeMethod<String>('test', config);
    expect(
      test?.contains('订阅成功') == true,
      isTrue,
      reason: test ?? 'MQTT test returned no result',
    );
    await channel.invokeMethod<void>('save', config);
    final saved = await channel.invokeMapMethod<String, dynamic>('load');
    expect(saved?['nodeId'] == config['nodeId'], isTrue);
    expect(
      saved?['password'] == config['password'],
      isTrue,
      reason: 'Encrypted configuration round-trip failed',
    );
    await channel.invokeMethod<void>('start');
    Map<String, dynamic>? status;
    await tester.runAsync(() async {
      for (var attempt = 0; attempt < 20; attempt++) {
        await Future<void>.delayed(const Duration(seconds: 1));
        status = await channel.invokeMapMethod<String, dynamic>('snapshot');
        if ((status?['connection'] as String? ?? '').startsWith('已连接')) break;
      }
    });
    expect(status?['active'], isTrue);
    expect(
      (status?['connection'] as String? ?? '').startsWith('已连接'),
      isTrue,
      reason: 'Foreground MQTT subscription/state publication failed',
    );
    // Logs intentionally contain no credentials or configuration.
    // ignore: avoid_print
    print(
      'Foreground service connected; received view: ${status?["updated"] != "尚未接收数据"}',
    );
  });
}
