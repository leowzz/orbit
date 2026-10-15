import 'package:flutter_secure_storage/flutter_secure_storage.dart';

/// Stable names keep saved destinations independent of navigation item order.
enum AppLocation {
  inbox,
  workbench,
  data,
  settings,
  workbenchFullscreen,
  matrix,
  matrixFullscreen;

  int get tab => switch (this) {
    inbox => 0,
    workbench || workbenchFullscreen => 1,
    data || matrix || matrixFullscreen => 2,
    settings => 3,
  };

  static AppLocation fromTab(int tab) =>
      [inbox, workbench, data, settings][tab];
}

class AppLocationStore {
  static const key = 'last_page';
  final FlutterSecureStorage storage;
  Future<void> _writes = Future.value();

  AppLocationStore({this.storage = const FlutterSecureStorage()});

  Future<AppLocation> read() async {
    try {
      final saved = await storage.read(key: key);
      return AppLocation.values.firstWhere(
        (page) => page.name == saved,
        orElse: () => AppLocation.inbox,
      );
    } catch (_) {
      // An unavailable preference must not prevent opening the application.
      return AppLocation.inbox;
    }
  }

  Future<void> write(AppLocation page) {
    final next = _writes.then((_) => storage.write(key: key, value: page.name));
    // Keep rapid switches ordered, and allow retry after a failed write.
    _writes = next.catchError((Object _) {});
    return next;
  }
}
