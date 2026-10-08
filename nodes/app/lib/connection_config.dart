import 'dart:convert';
import 'package:flutter/foundation.dart';

/// Shared validation for manual entry and console pairing QR codes.
class ConnectionConfig {
  final String server;
  final String token;
  const ConnectionConfig._(this.server, this.token);

  static ConnectionConfig parse(String server, String token) {
    final url = Uri.tryParse(server.trim());
    final local =
        kDebugMode &&
        url != null &&
        ['localhost', '127.0.0.1', '10.0.2.2', '::1'].contains(url.host);
    if (url == null ||
        url.host.isEmpty ||
        (url.scheme != 'https' && !(local && url.scheme == 'http')) ||
        url.userInfo.isNotEmpty ||
        url.hasQuery ||
        url.hasFragment ||
        token.trim().length < 32) {
      throw const FormatException('请输入 HTTPS 服务地址和有效的设备令牌');
    }
    return ConnectionConfig._(
      url.toString().replaceFirst(RegExp(r'/+$'), ''),
      token.trim(),
    );
  }

  static ConnectionConfig fromQr(String value) {
    try {
      final data = jsonDecode(value);
      if (data is! Map ||
          data['type'] != 'orbit-app' ||
          data['version'] != 1 ||
          data['server'] is! String ||
          data['token'] is! String) {
        throw const FormatException();
      }
      return parse(data['server'], data['token']);
    } on FormatException {
      throw const FormatException('二维码无效，请扫描控制台「连接 App」中的二维码');
    }
  }

  Map<String, dynamic> toJson() => {'server': server, 'token': token};
}
