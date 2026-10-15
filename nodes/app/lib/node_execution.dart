import 'dart:convert';
import 'package:flutter/foundation.dart';
import 'package:flutter/services.dart';
import 'package:url_launcher/url_launcher.dart';
import 'local_store.dart';

typedef ExecutionRequest = Future<Json> Function(String path, {Json? body});

/// Only entry metadata leaves this device. Paths, packages and URLs stay local.
class NodeExecution {
  static const _channel = MethodChannel('dev.orbit/node');
  final LocalStore local;
  final ExecutionRequest request;
  final Future<bool> Function(Json)? open;
  Json config = {'revision': 1, 'enabled': false, 'items': <Json>[]};
  String? error;
  Future<void>? _loaded;
  Future<void>? _running;
  Future<void> _writes = Future.value();
  int? _publishedRevision;
  bool get enabled => config['enabled'] == true;
  List<Json> get items => (config['items'] as List? ?? []).cast<Json>();

  NodeExecution(this.local, this.request, {this.open});

  Future<void> load() => _loaded ??= _load();
  Future<void> _load() async {
    final raw = await local.meta('execution_settings');
    if (raw != null) config = jsonDecode(raw) as Json;
  }

  Future<void> save(bool enabled, List<Json> entries) async {
    await load();
    if (entries.length > 24 ||
        entries.any((e) => !validEntry(e)) ||
        entries.map((e) => e['id']).toSet().length != entries.length) {
      throw const FormatException('请检查快捷入口，最多添加 24 项');
    }
    await _write(() async {
      final next = <String, dynamic>{
        'revision': (config['revision'] as int) + 1,
        'enabled': enabled,
        'items': entries.map((e) => {...e}).toList(),
      };
      await local.setMeta('execution_settings', jsonEncode(next));
      config = next;
      _publishedRevision = null;
    });
  }

  Future<void> _write(Future<void> Function() action) {
    final next = _writes.then((_) => action());
    _writes = next.catchError((Object _) {});
    return next;
  }

  static bool validEntry(Json entry) {
    final id = entry['id'];
    final title = entry['title'];
    final target = entry['target'];
    if (id is! String ||
        !RegExp(r'^[A-Za-z0-9_.-]{1,128}$').hasMatch(id) ||
        title is! String ||
        title.trim().isEmpty ||
        utf8.encode(title).length > 160 ||
        target is! String ||
        target.isEmpty) {
      return false;
    }
    if (entry['kind'] == 'app') {
      return RegExp(
        r'^[A-Za-z][A-Za-z0-9_]*(\.[A-Za-z0-9_]+)+$',
      ).hasMatch(target);
    }
    if (entry['kind'] != 'url') return false;
    final uri = Uri.tryParse(target);
    return uri != null &&
        uri.hasScheme &&
        (!['http', 'https'].contains(uri.scheme) || uri.host.isNotEmpty) &&
        ![
          'javascript',
          'data',
          'file',
          'vbscript',
        ].contains(uri.scheme.toLowerCase());
  }

  Future<List<Json>> applications() async {
    if (defaultTargetPlatform != TargetPlatform.android) return [];
    final apps = await _channel.invokeListMethod<dynamic>('listLaunchableApps');
    return (apps ?? [])
        .map((e) => Map<String, dynamic>.from(e as Map))
        .toList();
  }

  Future<bool> _open(Json entry) async {
    if (open != null) return open!(entry);
    if (entry['kind'] == 'app') {
      if (defaultTargetPlatform != TargetPlatform.android) return false;
      return await _channel.invokeMethod<bool>(
            'openLocalApp',
            entry['target'],
          ) ??
          false;
    }
    return launchUrl(
      Uri.parse(entry['target'] as String),
      mode: LaunchMode.externalApplication,
    );
  }

  Future<void> close() async {
    await _running;
    await _writes;
  }

  Future<void> sync({required bool Function() active}) =>
      _running ??= _performSync(active).whenComplete(() => _running = null);
  Future<void> _performSync(bool Function() active) async {
    if (!active()) return;
    try {
      await load();
      if (!active()) return;
      error = null;
      if (_publishedRevision != config['revision']) {
        final remote = await request('execution/catalog');
        final remoteRevision = remote['revision'];
        if (remoteRevision is! int || remoteRevision < 0) return;
        await _write(() async {
          // Token rotation can create a fresh local store for an existing Node.
          // Advance beyond its last catalog so removed local permissions win.
          if (remoteRevision >= (config['revision'] as int)) {
            final next = {...config, 'revision': remoteRevision + 1};
            await local.setMeta('execution_settings', jsonEncode(next));
            config = next;
          }
        });
        if (!active()) return;
        final revision = config['revision'] as int;
        await request(
          'execution/catalog',
          body: {
            'revision': revision,
            'items': enabled
                ? items
                      .map(
                        (e) => {
                          'id': e['id'],
                          'title': e['title'],
                          'kind': e['kind'],
                          'icon_name': e['kind'] == 'url' ? 'globe' : 'app',
                        },
                      )
                      .toList()
                : <Json>[],
          },
        );
        _publishedRevision = revision;
      }
      if (!enabled || !active()) return;
      final received = Stopwatch()..start();
      final response = await request('execution/commands');
      final serverNow = DateTime.tryParse(response['now'] as String? ?? '');
      if (serverNow == null) return;
      for (final command
          in (response['commands'] as List? ?? []).cast<Json>()) {
        if (!active() || !enabled) break;
        final expires = DateTime.tryParse(
          command['expires_at'] as String? ?? '',
        );
        final id = command['id'];
        if (id is! String ||
            expires == null ||
            !expires.isAfter(serverNow.add(received.elapsed)) ||
            expires.difference(serverNow) > const Duration(seconds: 15) ||
            command['catalog_revision'] != config['revision']) {
          continue;
        }
        final matching = items.where((e) => e['id'] == command['shortcut_id']);
        if (matching.isEmpty || !validEntry(matching.first)) continue;
        final seen =
            (jsonDecode(await local.meta('execution_receipts') ?? '[]') as List)
                .cast<String>();
        if (seen.contains(id)) continue;
        // Persist before opening: a repeated request must never launch twice.
        await local.setMeta(
          'execution_receipts',
          jsonEncode(
            [...seen, id].reversed.take(128).toList().reversed.toList(),
          ),
        );
        if (!active() ||
            !enabled ||
            command['catalog_revision'] != config['revision']) {
          continue;
        }
        if (!await _open(matching.first)) error = '未能打开本机快捷入口，请检查应用或网址';
      }
    } catch (_) {
      error = '本机快捷操作未能同步，请检查连接或更新 Core';
      _publishedRevision = null;
    }
  }
}
