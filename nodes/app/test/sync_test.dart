import 'dart:convert';
import 'dart:io';
import 'package:flutter_test/flutter_test.dart';
import 'package:http/http.dart' as http;
import 'package:http/testing.dart';
import 'package:orbit_app/inbox_controller.dart';
import 'package:orbit_app/local_store.dart';
import 'package:sqflite_common_ffi/sqflite_ffi.dart';

Json item(
  String revision, {
  String body = 'from server',
  bool deleted = false,
}) => {
  'id': 'item-1',
  'revision': revision,
  'kind': 'todo',
  'body': body,
  'completed': false,
  'created_at': '2026-10-07T00:00:00Z',
  'updated_at': '2026-10-07T00:00:00Z',
  if (deleted) 'deleted_at': '2026-10-07T01:00:00Z',
};
Json page(String cursor, List<Json> items) => {
  'generation': 'g1',
  'cursor': cursor,
  'items': items,
  'changes': items.map((i) => {'seq': cursor, 'item': i}).toList(),
  'has_more': false,
};
http.Response response(Object body, [int code = 200]) =>
    http.Response(jsonEncode(body), code);
void main() {
  sqfliteFfiInit();
  late Directory dir;
  late LocalStore local;
  setUp(() async {
    dir = await Directory.systemTemp.createTemp('orbit-test-');
    local = await LocalStore.open(
      databaseFactoryFfi,
      '${dir.path}/cache.sqlite',
    );
  });
  tearDown(() async {
    await local.close();
    await dir.delete(recursive: true);
  });

  test('cache, cursor and immutable outbox survive a restart', () async {
    await local.applyPage(
      page('1', [item('1')]),
      replace: true,
      snapshot: [item('1')],
    );
    final c = InboxController(local, 'https://example.test', 'token', dir.path);
    await c.submit('update', item: item('1'), body: 'offline draft');
    final original = (await local.pending()).single['payload'];
    await local.close();
    local = await LocalStore.open(
      databaseFactoryFfi,
      '${dir.path}/cache.sqlite',
    );
    expect((await local.items()).single['body'], 'from server');
    expect(await local.meta('cursor'), '1');
    expect((await local.pending()).single['payload'], original);
    c.client.close();
  });
  test(
    'replay cannot overwrite newer data, deletion is atomic with cursor',
    () async {
      await local.applyPage(page('2', [item('2')]));
      await local.acknowledge('old-receipt', item('1', body: 'old'));
      expect((await local.items()).single['revision'], '2');
      await local.applyPage(page('3', [item('3', deleted: true)]));
      expect(await local.items(), isEmpty);
      expect(await local.meta('cursor'), '3');
      await local.acknowledge('old-receipt', item('2'));
      expect(await local.items(), isEmpty);
    },
  );
  test('failed cache write rolls back the applied cursor', () async {
    await local.applyPage(page('1', [item('1')]));
    final bad = item('broken');
    await expectLater(
      local.applyPage(page('2', [item('2'), bad])),
      throwsFormatException,
    );
    expect(await local.meta('cursor'), '1');
    expect((await local.items()).single['revision'], '1');
  });
  test(
    'response loss retries the exact operation ID, conflict retains draft',
    () async {
      bool loseResponse = true;
      final payloads = <String>[];
      final c = InboxController(
        local,
        'https://example.test',
        'token',
        dir.path,
        client: MockClient((r) async {
          if (r.url.path.endsWith('snapshot')) {
            return response(page('1', [item('1')]));
          }
          if (r.url.path.endsWith('changes')) return response(page('1', []));
          if (r.url.path.endsWith('status')) return response({'view': null});
          payloads.add(r.body);
          if (loseResponse) throw const SocketException('response lost');
          return response({'code': 'conflict', 'current': item('2')}, 409);
        }),
      );
      await c.submit('update', item: item('1'), body: 'keep my draft');
      await c.sync();
      expect((await local.pending()).single['state'], 'pending');
      loseResponse = false;
      await c.sync();
      expect(payloads[0], payloads[1]);
      final pending = (await local.pending()).single;
      expect(pending['state'], 'conflict');
      expect(jsonDecode(pending['payload'])['body'], 'keep my draft');
      await c.sync();
      expect(payloads.length, 2);
      c.client.close();
    },
  );
  test('failed snapshot pagination preserves old cache and cursor', () async {
    await local.applyPage(page('1', [item('1')]));
    final c = InboxController(
      local,
      'https://example.test',
      'token',
      dir.path,
      client: MockClient((r) async {
        if (r.url.path.endsWith('changes')) {
          return response({'code': 'reset_required'}, 409);
        }
        if (!r.url.hasQuery) {
          return response({
            ...page('5', [item('5')]),
            'has_more': true,
            'next_id': 'item-1',
          });
        }
        throw const SocketException('interrupted');
      }),
    );
    await c.sync();
    expect(await local.meta('cursor'), '1');
    expect((await local.items()).single['revision'], '1');
    c.client.close();
  });
}
