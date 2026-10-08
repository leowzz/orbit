import 'dart:async';
import 'dart:convert';
import 'dart:io';
import 'dart:math';

import 'quick_config_page.dart';

typedef ConfigAction = Future<String> Function(String, Map<String, String>);

/// A temporary, token-protected LAN endpoint owned by the QR dialog.
class QuickConfigServer {
  QuickConfigServer(this.readConfig, this.action);
  final Map<String, String> Function() readConfig;
  final ConfigAction action;
  final String token = base64Url.encode(
    List<int>.generate(32, (_) => Random.secure().nextInt(256)),
  );
  HttpServer? _server;
  bool _busy = false;

  Future<List<String>> start({InternetAddress? address}) async {
    final addresses = address == null
        ? (await NetworkInterface.list(type: InternetAddressType.IPv4))
              .expand((interface) => interface.addresses)
              .where(
                (ip) =>
                    ip.isLinkLocal ||
                    ip.address.startsWith('10.') ||
                    ip.address.startsWith('192.168.') ||
                    (ip.address.startsWith('172.') &&
                        int.parse(ip.address.split('.')[1]) >= 16 &&
                        int.parse(ip.address.split('.')[1]) <= 31),
              )
              .toList()
        : [address];
    if (addresses.isEmpty) throw StateError('未找到局域网地址，请连接 Wi-Fi 或以太网后重试');
    final server = await HttpServer.bind(address ?? InternetAddress.anyIPv4, 0);
    _server = server;
    server.listen(_handle);
    return addresses
        .map((ip) => 'http://${ip.address}:${server.port}/#$token')
        .toList();
  }

  Future<void> close() async {
    final server = _server;
    _server = null;
    await server?.close(force: true);
  }

  Future<void> _handle(HttpRequest request) async {
    final response = request.response;
    response.headers.set('Cache-Control', 'no-store');
    response.headers.set('Referrer-Policy', 'no-referrer');
    response.headers.set('X-Content-Type-Options', 'nosniff');
    response.headers.set('X-Frame-Options', 'DENY');
    try {
      if (request.method == 'GET' && request.uri.path == '/') {
        response.headers.contentType = ContentType.html;
        response.write(quickConfigPage);
        return;
      }
      response.headers.contentType = ContentType.json;
      if (request.headers.value('X-Orbit-Token') != token) {
        response.statusCode = HttpStatus.forbidden;
        response.write(jsonEncode({'message': '链接已失效，请重新扫描设备二维码'}));
        return;
      }
      if (request.method == 'GET' && request.uri.path == '/config') {
        response.write(jsonEncode(readConfig()));
      } else if (request.method == 'POST' &&
          ['/test', '/save'].contains(request.uri.path)) {
        if (_busy) {
          response.statusCode = HttpStatus.conflict;
          response.write(jsonEncode({'message': '正在处理，请稍后重试'}));
          return;
        }
        _busy = true;
        try {
          final bytes = <int>[];
          await for (final chunk in request.timeout(
            const Duration(seconds: 10),
          )) {
            bytes.addAll(chunk);
            if (bytes.length > 16384) throw const FormatException();
          }
          final decoded = jsonDecode(utf8.decode(bytes));
          if (decoded is! Map ||
              decoded.length != 4 ||
              ![
                'uri',
                'nodeId',
                'username',
                'password',
              ].every((key) => decoded[key] is String)) {
            throw const FormatException();
          }
          if (_server == null) return;
          final message = await action(
            request.uri.path.substring(1),
            Map<String, String>.from(decoded),
          );
          response.write(jsonEncode({'message': message}));
        } finally {
          _busy = false;
        }
      } else {
        response.statusCode = HttpStatus.notFound;
      }
    } on FormatException {
      response.statusCode = HttpStatus.badRequest;
      response.write(jsonEncode({'message': '配置格式无效'}));
    } catch (_) {
      response.statusCode = HttpStatus.internalServerError;
      response.write(jsonEncode({'message': '操作失败，请检查设备连接后重试'}));
    } finally {
      await response.close();
    }
  }
}
