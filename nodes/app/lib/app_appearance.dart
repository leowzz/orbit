import 'dart:io';
import 'package:flutter/material.dart';
import 'package:flutter/services.dart';
import 'package:flutter_secure_storage/flutter_secure_storage.dart';

/// Android shares this preference with the launch window and home-screen widgets.
class AppearanceStore {
  final bool android;
  AppearanceStore({bool? android}) : android = android ?? Platform.isAndroid;
  static const channel = MethodChannel('dev.orbit/node');
  static const storage = FlutterSecureStorage();

  Future<bool> read() async => android
      ? await channel.invokeMethod<bool>('getDarkMode') ?? false
      : await storage.read(key: 'appearance') == 'dark';

  Future<void> write(bool dark) async {
    if (android) {
      await channel.invokeMethod<void>('setDarkMode', dark);
    } else {
      await storage.write(key: 'appearance', value: dark ? 'dark' : 'light');
    }
  }
}

class AppAppearance extends InheritedWidget {
  final bool dark;
  final bool saving;
  final ValueChanged<bool> onChanged;
  const AppAppearance({
    super.key,
    required this.dark,
    required this.saving,
    required this.onChanged,
    required super.child,
  });

  static AppAppearance? maybeOf(BuildContext context) =>
      context.dependOnInheritedWidgetOfExactType<AppAppearance>();

  @override
  bool updateShouldNotify(AppAppearance oldWidget) =>
      dark != oldWidget.dark || saving != oldWidget.saving;
}
