import 'dart:convert';
import 'package:flutter_test/flutter_test.dart';
import 'package:orbit_app/local_store.dart';
import 'package:orbit_app/node_execution.dart';
import 'package:sqflite_common_ffi/sqflite_ffi.dart';

class ExecutionMemoryStore extends LocalStore {
  ExecutionMemoryStore(super.db);
  final metadata = <String, String>{};
  @override
  Future<String?> meta(String key) async => metadata[key];
  @override
  Future<void> setMeta(String key, String value) async {
    metadata[key] = value;
  }
}

void main() {
  TestWidgetsFlutterBinding.ensureInitialized();
  sqfliteFfiInit();
  late ExecutionMemoryStore store;
  setUp(() async {
    store = ExecutionMemoryStore(
      (await LocalStore.open(databaseFactoryFfi, inMemoryDatabasePath)).db,
    );
  });
  tearDown(() => store.close());
  final entry = <String, dynamic>{
    'id': 'browser',
    'title': 'Browser',
    'kind': 'url',
    'target': 'https://private.example/path',
  };
  test(
    'publishes metadata only and executes a valid command once despite clock skew',
    () async {
      final opened = <Json>[];
      final requests = <String>[];
      final now = DateTime.utc(2000);
      final execution = NodeExecution(
        store,
        (path, {body}) async {
          requests.add(path);
          if (path == 'execution/catalog' && body == null) {
            return {'revision': 0};
          }
          if (path == 'execution/catalog') {
            expect(jsonEncode(body), isNot(contains('private.example')));
            expect((body!['items'] as List).single, {
              'id': 'browser',
              'title': 'Browser',
              'kind': 'url',
              'icon_name': 'globe',
            });
            return {};
          }
          return {
            'now': now.toIso8601String(),
            'commands': [
              {
                'id': 'command',
                'shortcut_id': 'browser',
                'catalog_revision': 2,
                'expires_at': now
                    .add(const Duration(seconds: 10))
                    .toIso8601String(),
              },
            ],
          };
        },
        open: (e) async {
          opened.add(e);
          return true;
        },
      );
      await execution.save(true, [entry]);
      await execution.sync(active: () => true);
      await execution.sync(active: () => true);
      expect(opened, hasLength(1));
      expect(opened.single['target'], entry['target']);
      expect(requests.where((p) => p == 'execution/catalog'), hasLength(2));
    },
  );
  test('rejects expired, unknown, and stale catalog commands', () async {
    var launches = 0;
    final now = DateTime.utc(2026);
    final execution = NodeExecution(
      store,
      (path, {body}) async => path.endsWith('catalog')
          ? {}
          : {
              'now': now.toIso8601String(),
              'commands': [
                {
                  'id': 'expired',
                  'shortcut_id': 'browser',
                  'catalog_revision': 2,
                  'expires_at': now.toIso8601String(),
                },
                {
                  'id': 'unknown',
                  'shortcut_id': 'removed',
                  'catalog_revision': 2,
                  'expires_at': now
                      .add(const Duration(seconds: 10))
                      .toIso8601String(),
                },
                {
                  'id': 'stale',
                  'shortcut_id': 'browser',
                  'catalog_revision': 1,
                  'expires_at': now
                      .add(const Duration(seconds: 10))
                      .toIso8601String(),
                },
              ],
            },
      open: (_) async {
        launches++;
        return true;
      },
    );
    await execution.save(true, [entry]);
    await execution.sync(active: () => true);
    expect(launches, 0);
  });
  test(
    'local revocation while fetching prevents execution and publishes an empty catalog',
    () async {
      var launches = 0;
      final catalogs = <Json>[];
      late NodeExecution execution;
      execution = NodeExecution(
        store,
        (path, {body}) async {
          if (path.endsWith('catalog') && body == null) return {'revision': 0};
          if (path.endsWith('catalog')) {
            catalogs.add(body!);
            return {};
          }
          await execution.save(false, [entry]);
          final now = DateTime.now().toUtc();
          return {
            'now': now.toIso8601String(),
            'commands': [
              {
                'id': 'racing',
                'shortcut_id': 'browser',
                'catalog_revision': 2,
                'expires_at': now
                    .add(const Duration(seconds: 10))
                    .toIso8601String(),
              },
            ],
          };
        },
        open: (_) async {
          launches++;
          return true;
        },
      );
      await execution.save(true, [entry]);
      await execution.sync(active: () => true);
      await execution.sync(active: () => true);
      expect(launches, 0);
      expect(catalogs.last['items'], isEmpty);
    },
  );
  test(
    'inactive application does not publish or receive executable requests',
    () async {
      var requests = 0;
      final execution = NodeExecution(store, (_, {body}) async {
        requests++;
        return {};
      });
      await execution.save(true, [entry]);
      await execution.sync(active: () => false);
      expect(requests, 0);
    },
  );
  test(
    'a new local store revokes a higher previous catalog after token rotation',
    () async {
      Json? published;
      final execution = NodeExecution(store, (path, {body}) async {
        if (body == null) return {'revision': 100};
        published = body;
        return {};
      });
      await execution.sync(active: () => true);
      expect(published?['revision'], 101);
      expect(published?['items'], isEmpty);
      expect(execution.enabled, isFalse);
      final saved =
          jsonDecode((await store.meta('execution_settings'))!) as Json;
      expect(saved['revision'], 101);
    },
  );
}
