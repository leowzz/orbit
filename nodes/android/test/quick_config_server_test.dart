import 'dart:convert';
import 'dart:io';

import 'package:flutter_test/flutter_test.dart';
import 'package:orbit_android/quick_config_server.dart';

void main() {
  test(
    'LAN session protects config, delegates test/save, and closes',
    () async {
      var config = {
        'uri': 'tcp://localhost:1883',
        'nodeId': 'android',
        'username': '',
        'password': 'secret',
      };
      final calls = <String>[];
      final server = QuickConfigServer(() => config, (method, values) async {
        calls.add(method);
        if (method == 'save') config = values;
        return method == 'save' ? 'saved' : 'tested';
      });
      final url = Uri.parse(
        (await server.start(address: InternetAddress.loopbackIPv4)).single,
      );
      final client = HttpClient();
      addTearDown(() async {
        client.close(force: true);
        await server.close();
      });
      Future<(int, String)> request(
        String path, {
        String? token,
        String? body,
      }) async {
        final req = await client.openUrl(
          body == null ? 'GET' : 'POST',
          url.replace(path: path, fragment: ''),
        );
        if (token != null) req.headers.set('X-Orbit-Token', token);
        if (body != null) req.write(body);
        final res = await req.close();
        return (res.statusCode, await utf8.decoder.bind(res).join());
      }

      final page = await request('/');
      expect(page.$1, 200);
      expect(page.$2, isNot(contains('secret')));
      expect((await request('/config')).$1, 403);
      expect(
        (await request('/save', token: 'wrong', body: jsonEncode(config))).$1,
        403,
      );
      expect(calls, isEmpty);
      expect(
        jsonDecode((await request('/config', token: url.fragment)).$2),
        config,
      );
      final updated = {...config, 'nodeId': 'new-node'};
      expect(
        (await request(
          '/test',
          token: url.fragment,
          body: jsonEncode(updated),
        )).$1,
        200,
      );
      expect(config['nodeId'], 'android');
      expect((await request('/save', token: url.fragment, body: '{}')).$1, 400);
      expect(
        (await request(
          '/save',
          token: url.fragment,
          body: jsonEncode(updated),
        )).$1,
        200,
      );
      expect(config['nodeId'], 'new-node');
      expect(calls, ['test', 'save']);
      await server.close();
      await expectLater(
        request('/config', token: url.fragment),
        throwsA(anyOf(isA<SocketException>(), isA<HttpException>())),
      );
    },
  );
}
