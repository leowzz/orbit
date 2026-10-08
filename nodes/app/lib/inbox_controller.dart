import 'dart:async';
import 'dart:convert';
import 'dart:io';
import 'package:flutter/foundation.dart';
import 'package:http/http.dart' as http;
import 'package:uuid/uuid.dart';
import 'local_store.dart';

class ApiError implements Exception {
  final String code;
  final Json? current;
  ApiError(this.code, [this.current]);
}

class InboxController extends ChangeNotifier {
  final LocalStore local;
  final String server;
  final String token;
  final String cacheDir;
  final http.Client client;
  List<Json> items = [], pending = [];
  Json? view;
  String? error;
  String? uploadingID;
  bool online = false, syncing = false, active = false;
  int limit = 100;
  String query = '', filter = 'all';
  bool hasMore = false;
  int _loadRevision = 0;
  Timer? _timer;
  http.Client? _events;
  int _epoch = 0;
  Future<void>? _sync;

  InboxController(
    this.local,
    this.server,
    this.token,
    this.cacheDir, {
    http.Client? client,
  }) : client = client ?? http.Client();
  Uri uri(String path) => Uri.parse('$server/api/v1/$path');
  Map<String, String> get headers => {'Authorization': 'Bearer $token'};

  Future<Json> request(String path, {Json? body}) async {
    final response =
        await (body == null
                ? client.get(uri(path), headers: headers)
                : client.post(
                    uri(path),
                    headers: {...headers, 'Content-Type': 'application/json'},
                    body: jsonEncode(body),
                  ))
            .timeout(const Duration(seconds: 15));
    return decode(response);
  }

  Json decode(http.Response response) {
    Json data;
    try {
      data = jsonDecode(utf8.decode(response.bodyBytes)) as Json;
    } catch (_) {
      throw ApiError('unavailable');
    }
    if (response.statusCode >= 400) {
      throw ApiError(data['code'] ?? 'unavailable', data['current'] as Json?);
    }
    return data;
  }

  Future<void> load() async {
    final revision = ++_loadRevision;
    final all = await local.items(limit: null);
    final queue = await local.pending();
    // A committed local create must be visible even before the server responds.
    for (final entry in queue) {
      final op = jsonDecode(entry['payload']) as Json;
      if (op['type'] == 'create' &&
          entry['state'] == 'pending' &&
          !all.any((item) => item['id'] == op['item_id'])) {
        all.add({
          'id': op['item_id'],
          'revision': '0',
          'kind': op['kind'],
          'body': op['body'] ?? '',
          'completed': false,
          'created_at': DateTime.fromMicrosecondsSinceEpoch(
            entry['created'],
          ).toUtc().toIso8601String(),
          'attachment_id': op['attachment_id'],
          'local_photo': entry['photo'],
        });
      }
    }
    all.sort((a, b) {
      final date = (b['created_at'] as String).compareTo(
        a['created_at'] as String,
      );
      return date != 0
          ? date
          : (b['id'] as String).compareTo(a['id'] as String);
    });
    final needle = query.trim().toLowerCase();
    final matches = all.where((item) {
      final body = item['body'] as String;
      return body.toLowerCase().contains(needle) &&
          switch (filter) {
            'links' => RegExp(
              r'https?://[^\s]+',
              caseSensitive: false,
            ).hasMatch(body),
            'todo' => item['kind'] == 'todo' && item['completed'] != true,
            'completed' => item['kind'] == 'todo' && item['completed'] == true,
            'image' => item['kind'] == 'image',
            _ => true,
          };
    }).toList();
    final saved = await local.meta('status');
    if (revision != _loadRevision) return;
    items = matches.take(limit).toList();
    hasMore = matches.length > limit;
    pending = queue;
    if (saved != null) view = jsonDecode(saved) as Json?;
    notifyListeners();
  }

  Future<void> search(String text, String kind) async {
    query = text;
    filter = kind;
    limit = 100;
    await load();
  }

  Future<void> loadMore() async {
    limit += 100;
    await load();
  }

  void start() {
    if (active) return;
    active = true;
    final epoch = ++_epoch;
    unawaited(_listen(epoch));
    unawaited(sync());
    _timer = Timer.periodic(
      const Duration(seconds: 15),
      (_) => unawaited(sync()),
    );
  }

  void stop() {
    active = false;
    _epoch++;
    _timer?.cancel();
    _events?.close();
    _events = null;
  }

  Future<void> _listen(int epoch) async {
    while (active && epoch == _epoch) {
      final events = http.Client();
      _events = events;
      try {
        final response = await events
            .send(http.Request('GET', uri('events'))..headers.addAll(headers))
            .timeout(const Duration(seconds: 15));
        if (response.statusCode != 200) break;
        await for (final line
            in response.stream
                .timeout(const Duration(seconds: 35))
                .transform(utf8.decoder)
                .transform(const LineSplitter())) {
          if (epoch != _epoch || !active) break;
          if (line.startsWith('data:')) await sync();
        }
      } catch (_) {
        /* The normal sync request supplies actionable error state. */
      } finally {
        events.close();
      }
      if (active && epoch == _epoch) {
        await Future<void>.delayed(const Duration(seconds: 3));
      }
    }
  }

  Future<void> sync() =>
      _sync ??= _performSync().whenComplete(() => _sync = null);
  Future<void> _performSync() async {
    syncing = true;
    notifyListeners();
    try {
      await _catchUp();
      for (final entry in await local.pending()) {
        if (entry['state'] == 'conflict' || entry['state'] == 'failed') {
          continue;
        }
        final op = jsonDecode(entry['payload']) as Json;
        try {
          if (entry['photo'] != null && op['attachment_id'] == null) {
            uploadingID = entry['id'];
            notifyListeners();
            final bytes = await File(entry['photo']).readAsBytes();
            final response = await client
                .post(uri('attachments'), headers: headers, body: bytes)
                .timeout(const Duration(seconds: 45));
            op['attachment_id'] = decode(response)['id'];
            // The operation becomes immutable before its first HTTP submission.
            await local.uploaded(entry['id'], op);
          }
          await local.mark(entry['id'], 'pending');
          final item = await request('operations', body: op);
          await local.acknowledge(entry['id'], item);
          if (entry['photo'] != null) {
            try {
              await File(entry['photo']).delete();
            } catch (_) {}
          }
        } on ApiError catch (e) {
          if (e.code == 'unauthenticated' ||
              e.code == 'device_revoked' ||
              e.code == 'unavailable') {
            rethrow;
          }
          await local.mark(
            entry['id'],
            e.code == 'conflict' ? 'conflict' : 'failed',
            error: e.code,
          );
        } on FileSystemException {
          await local.mark(entry['id'], 'failed', error: 'photo_missing');
        }
      }
      await _catchUp();
      final status = await request('status');
      await local.setMeta('status', jsonEncode(status['view']));
      online = true;
      error = null;
    } on ApiError catch (e) {
      online = false;
      error = message(e.code);
    } catch (_) {
      online = false;
      error = '暂时无法连接，内容已保存在本机';
    } finally {
      syncing = false;
      uploadingID = null;
      await load();
    }
  }

  Future<void> _catchUp() async {
    var generation = await local.meta('generation');
    if (generation != null) {
      try {
        while (true) {
          final cursor = await local.meta('cursor') ?? '0';
          final page = await request(
            'changes?generation=$generation&after=$cursor',
          );
          await local.applyPage(page);
          if (page['has_more'] != true) return;
        }
      } on ApiError catch (e) {
        if (e.code != 'reset_required') rethrow;
      }
    }
    final all = <Json>[];
    var page = await request('sync/snapshot');
    while (true) {
      all.addAll((page['items'] as List).cast<Json>());
      if (page['has_more'] != true) break;
      page = await request(
        'sync/snapshot?generation=${page['generation']}&at=${page['cursor']}&after_id=${page['next_id']}',
      );
    }
    // Old cache remains usable if pagination fails or the process is killed.
    await local.applyPage(page, replace: true, snapshot: all);
  }

  bool isPending(String id) => pending.any((p) => p['item_id'] == id);
  Future<void> submit(
    String type, {
    Json? item,
    String? kind,
    String? body,
    bool? completed,
    String? photo,
  }) async {
    final op = <String, dynamic>{
      'operation_id': const Uuid().v4(),
      'item_id': item?['id'] ?? const Uuid().v4(),
      'type': type,
      'expected_revision': item?['revision'] ?? '0',
      'kind': ?kind,
      'body': ?body,
      'completed': ?completed,
    };
    String? saved;
    if (photo != null) {
      saved = '$cacheDir/${op['operation_id']}.upload';
      await File(photo).copy(saved);
    }
    await local.enqueue(op, photo: saved);
    await load();
    if (active) {
      // If another pass is running, schedule one more pass for this new entry.
      unawaited(
        sync().then((_) async {
          if (active) await sync();
        }),
      );
    }
  }

  Future<void> retry(Json entry) async {
    await local.mark(entry['id'], 'pending');
    await load();
    await sync();
  }

  Future<void> discard(Json entry) async {
    await local.discard(entry['id']);
    if (entry['photo'] != null) {
      try {
        await File(entry['photo']).delete();
      } catch (_) {}
    }
    await load();
  }

  Future<File> image(String id, {bool full = false}) async {
    final file = File('$cacheDir/$id${full ? '.original' : '.thumb'}');
    if (await file.exists()) return file;
    final response = await client
        .get(
          uri('attachments/$id${full ? '' : '?thumbnail=1'}'),
          headers: headers,
        )
        .timeout(const Duration(seconds: 30));
    if (response.statusCode != 200) throw ApiError('attachment_unavailable');
    final temp = File('${file.path}.tmp');
    await temp.writeAsBytes(response.bodyBytes, flush: true);
    await temp.rename(file.path);
    return file;
  }

  Future<void> close() async {
    stop();
    await _sync;
    client.close();
    await local.close();
    super.dispose();
  }
}

String message(String code) => switch (code) {
  'unauthenticated' => '设备令牌无效，请检查连接设置',
  'device_revoked' => '此设备的访问权限已撤销',
  'conflict' => '另一台设备已修改，请处理冲突',
  'image_too_large' => '图片需小于 10 MB',
  'invalid_image' => '请选择 JPEG、PNG 或 GIF 图片（不超过 2000 万像素）',
  'photo_missing' => '本机图片已丢失，请重新选择',
  'attachment_unavailable' => '图片不可用，请重新选择',
  'operation_id_reused' => '操作编号已使用，请保留草稿后重新发送',
  'invalid_fields' => '内容格式不正确，请检查后重新发送',
  _ => '暂时无法保存，请重试',
};
