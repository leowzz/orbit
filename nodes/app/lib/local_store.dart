import 'dart:convert';
import 'package:sqflite_common/sqlite_api.dart';

typedef Json = Map<String, dynamic>;

class LocalStore {
  final Database db;
  LocalStore(this.db);

  static Future<LocalStore> open(DatabaseFactory factory, String path) async {
    final db = await factory.openDatabase(
      path,
      options: OpenDatabaseOptions(
        version: 1,
        onCreate: (db, _) async {
          await db.execute(
            'CREATE TABLE items(id TEXT PRIMARY KEY, revision INTEGER NOT NULL, created TEXT NOT NULL, deleted INTEGER NOT NULL, payload TEXT NOT NULL)',
          );
          await db.execute(
            'CREATE TABLE metadata(key TEXT PRIMARY KEY, value TEXT NOT NULL)',
          );
          await db.execute(
            'CREATE TABLE pending(id TEXT PRIMARY KEY, item_id TEXT NOT NULL UNIQUE, payload TEXT NOT NULL, photo TEXT, state TEXT NOT NULL, error TEXT, created INTEGER NOT NULL)',
          );
        },
      ),
    );
    return LocalStore(db);
  }

  Future<String?> meta(String key) async {
    final rows = await db.query('metadata', where: 'key=?', whereArgs: [key]);
    return rows.firstOrNull?['value'] as String?;
  }

  Future<void> setMeta(String key, String value) => _meta(db, key, value);
  static Future<void> _meta(
    DatabaseExecutor db,
    String key,
    String value,
  ) async {
    await db.insert('metadata', {
      'key': key,
      'value': value,
    }, conflictAlgorithm: ConflictAlgorithm.replace);
  }

  Future<List<Json>> items({int limit = 100}) async => (await db.query(
    'items',
    where: 'deleted=0',
    orderBy: 'created DESC, id DESC',
    limit: limit,
  )).map((row) => jsonDecode(row['payload'] as String) as Json).toList();

  Future<Json?> item(String id) async {
    final rows = await db.query('items', where: 'id=?', whereArgs: [id]);
    return rows.isEmpty
        ? null
        : jsonDecode(rows.first['payload'] as String) as Json;
  }

  static Future<void> _put(DatabaseExecutor db, Json item) async {
    // A replay or an old receipt must never overwrite a newer local version.
    final revision = int.parse(item['revision']);
    final current = await db.query(
      'items',
      columns: ['revision'],
      where: 'id=?',
      whereArgs: [item['id']],
    );
    if (current.isNotEmpty && (current.first['revision'] as int) >= revision) {
      return;
    }
    // Use basic SQLite syntax supported by Android 8's system SQLite too.
    await db.insert('items', {
      'id': item['id'],
      'revision': revision,
      'created': item['created_at'],
      'deleted': item['deleted_at'] == null ? 0 : 1,
      'payload': jsonEncode(item),
    }, conflictAlgorithm: ConflictAlgorithm.replace);
  }

  Future<void> applyPage(
    Json page, {
    bool replace = false,
    List<Json>? snapshot,
  }) => db.transaction((tx) async {
    if (replace) await tx.delete('items');
    final items =
        snapshot ??
        (page['changes'] as List).map((c) => c['item'] as Json).toList();
    for (final item in items) {
      await _put(tx, item);
    }
    await _meta(tx, 'generation', page['generation']);
    await _meta(tx, 'cursor', page['cursor']);
  });

  Future<List<Json>> pending() async => (await db.query(
    'pending',
    orderBy: 'created ASC, id ASC',
  )).map((e) => Map<String, dynamic>.from(e)).toList();
  Future<void> enqueue(Json op, {String? photo}) async {
    await db.insert('pending', {
      'id': op['operation_id'],
      'item_id': op['item_id'],
      'payload': jsonEncode(op),
      'photo': photo,
      'state': 'pending',
      'created': DateTime.now().microsecondsSinceEpoch,
    });
  }

  Future<void> mark(String id, String state, {String? error}) async {
    await db.update(
      'pending',
      {'state': state, 'error': error},
      where: 'id=?',
      whereArgs: [id],
    );
  }

  Future<void> uploaded(String id, Json op) async {
    await db.update(
      'pending',
      {'payload': jsonEncode(op)},
      where: 'id=?',
      whereArgs: [id],
    );
  }

  Future<void> acknowledge(String id, Json item) => db.transaction((tx) async {
    await _put(tx, item);
    await tx.delete('pending', where: 'id=?', whereArgs: [id]);
  });
  Future<void> discard(String id) async {
    await db.delete('pending', where: 'id=?', whereArgs: [id]);
  }

  Future<void> close() => db.close();
}
