import 'dart:async';
import 'package:flutter/services.dart';
import 'inbox_controller.dart';

/// Claims credentials for both first connections and restored pre-MQTT installs.
class AppMqttRegistration {
  final InboxController controller;
  final MethodChannel channel;
  final DateTime Function() now;
  bool _closed = false, _busy = false, _ready = false;
  DateTime? _retryAt;

  AppMqttRegistration(
    this.controller, {
    this.channel = const MethodChannel('dev.orbit/node'),
    DateTime Function()? now,
  }) : now = now ?? DateTime.now;

  void start() {
    controller.addListener(_changed);
    unawaited(sync());
  }

  void _changed() {
    if (controller.online) unawaited(sync());
  }

  Future<void> sync() async {
    if (_closed ||
        _busy ||
        _ready ||
        (_retryAt != null && now().isBefore(_retryAt!))) {
      return;
    }
    _busy = true;
    _retryAt = now().add(const Duration(minutes: 1));
    try {
      final response = await controller.request('mqtt', body: {});
      if (_closed || response['enabled'] != true) return;
      final mqtt = response['mqtt'];
      if (mqtt is! Map ||
          ['uri', 'nodeId', 'username', 'password'].any(
            (key) => mqtt[key] is! String || (mqtt[key] as String).isEmpty,
          )) {
        return;
      }
      // Native checks ownership again, protecting a connection switched while
      // the request was in flight. Its encrypted vault survives App upgrades.
      final saved = await channel.invokeMethod<bool>('appMqttConnection', {
        'server': controller.server,
        'token': controller.token,
        'mqtt': mqtt,
      });
      if (!_closed && saved == true) _ready = true;
    } catch (_) {
      // Old Cores (404), disabled provisioning and temporary broker/network
      // failures must not block the existing HTTP inbox/widget connection.
    } finally {
      _busy = false;
    }
  }

  void close() {
    _closed = true;
    controller.removeListener(_changed);
  }
}
