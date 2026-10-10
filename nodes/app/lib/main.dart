import 'dart:convert';
import 'dart:io';
import 'package:crypto/crypto.dart';
import 'package:flutter/foundation.dart';
import 'package:flutter/material.dart';
import 'package:flutter/services.dart';
import 'package:flutter_secure_storage/flutter_secure_storage.dart';
import 'package:http/http.dart' as http;
import 'package:path_provider/path_provider.dart';
import 'package:sqflite/sqflite.dart' as sqlite;
import 'package:sqflite_common_ffi/sqflite_ffi.dart';
import 'inbox_controller.dart';
import 'inbox_screen.dart';
import 'local_store.dart';
import 'orbit_home.dart';
import 'connection_config.dart';
import 'scan_connection_screen.dart';

void main() {
  WidgetsFlutterBinding.ensureInitialized();
  runApp(const OrbitApp());
}

class OrbitApp extends StatefulWidget {
  const OrbitApp({super.key});
  @override
  State<OrbitApp> createState() => _OrbitAppState();
}

class _OrbitAppState extends State<OrbitApp> with WidgetsBindingObserver {
  final secure = const FlutterSecureStorage();
  InboxController? controller;
  Json? connection;
  String? failure;
  bool loading = true;
  @override
  void initState() {
    super.initState();
    WidgetsBinding.instance.addObserver(this);
    _restore();
  }

  Future<void> _restore() async {
    try {
      final value = await secure.read(key: 'connection');
      if (value != null) {
        connection = jsonDecode(value) as Json;
        await _open(connection!);
      }
    } catch (_) {
      failure = '无法读取本机数据，请重试';
    }
    if (mounted) setState(() => loading = false);
  }

  Future<void> _open(Json config) async {
    final root = await getApplicationSupportDirectory();
    final account = sha256
        .convert(utf8.encode('${config['server']}|${config['token']}'))
        .toString();
    final dir = Directory('${root.path}/$account');
    await dir.create(recursive: true);
    var factory = sqlite.databaseFactory;
    if (Platform.isWindows || Platform.isLinux) {
      sqfliteFfiInit();
      factory = databaseFactoryFfi;
    }
    final local = await LocalStore.open(factory, '${dir.path}/inbox.sqlite');
    final next = InboxController(
      local,
      config['server'],
      config['token'],
      dir.path,
    );
    await next.load();
    await controller?.close();
    controller = next;
    next.start();
  }

  Future<void> _connect(Json config) async {
    final response = await http
        .get(
          Uri.parse('${config['server']}/api/v1/status'),
          headers: {'Authorization': 'Bearer ${config['token']}'},
        )
        .timeout(const Duration(seconds: 15));
    if (response.statusCode == 401) throw ApiError('unauthenticated');
    if (response.statusCode == 403) throw ApiError('device_revoked');
    if (response.statusCode != 200) throw ApiError('unavailable');
    final status = jsonDecode(response.body);
    if (status is! Map || status['node_id'] is! String) {
      throw ApiError('unavailable');
    }
    await _open(config);
    await secure.write(key: 'connection', value: jsonEncode(config));
    if (mounted) {
      setState(() {
        connection = config;
        failure = null;
      });
    }
  }

  Future<void> _settings(BuildContext context) async {
    controller?.stop();
    await Navigator.of(context).push(
      MaterialPageRoute<void>(
        builder: (context) => ConnectionScreen(
          initial: connection,
          onConnect: (config) async {
            await _connect(config);
            if (context.mounted) Navigator.pop(context);
          },
        ),
      ),
    );
    controller?.start();
  }

  @override
  void didChangeAppLifecycleState(AppLifecycleState state) {
    if (state == AppLifecycleState.resumed) {
      controller?.start();
    } else {
      controller?.stop();
    }
  }

  @override
  void dispose() {
    WidgetsBinding.instance.removeObserver(this);
    controller?.close();
    super.dispose();
  }

  @override
  Widget build(BuildContext context) => MaterialApp(
    title: 'Orbit',
    debugShowCheckedModeBanner: false,
    theme: ThemeData(
      useMaterial3: true,
      brightness: Brightness.light,
      colorScheme: ColorScheme.fromSeed(
        seedColor: const Color(0xff277765),
        surface: const Color(0xfffafbf9),
      ),
      scaffoldBackgroundColor: const Color(0xfffafbf9),
      appBarTheme: const AppBarTheme(
        systemOverlayStyle: SystemUiOverlayStyle(
          statusBarIconBrightness: Brightness.dark,
          statusBarBrightness: Brightness.light,
        ),
        backgroundColor: Color(0xfffafbf9),
        surfaceTintColor: Colors.transparent,
        centerTitle: false,
      ),
      inputDecorationTheme: InputDecorationTheme(
        filled: true,
        fillColor: Colors.white,
        border: OutlineInputBorder(
          borderRadius: BorderRadius.circular(14),
          borderSide: const BorderSide(color: Color(0xffdce3df)),
        ),
        enabledBorder: OutlineInputBorder(
          borderRadius: BorderRadius.circular(14),
          borderSide: const BorderSide(color: Color(0xffdce3df)),
        ),
      ),
    ),
    home: loading
        ? const Scaffold(body: Center(child: CircularProgressIndicator()))
        : failure != null
        ? Scaffold(
            body: Center(
              child: Column(
                mainAxisSize: MainAxisSize.min,
                children: [
                  Text(failure!),
                  TextButton(
                    onPressed: () {
                      setState(() => loading = true);
                      _restore();
                    },
                    child: const Text('重试'),
                  ),
                ],
              ),
            ),
          )
        : Builder(
            builder: (context) => OrbitHome(
              controller: controller,
              onSettings: controller == null ? null : () => _settings(context),
              widgetsEnabled: Platform.isAndroid,
              inbox: controller == null
                  ? ConnectionScreen(onConnect: _connect)
                  : InboxScreen(controller: controller!),
            ),
          ),
  );
}

class ConnectionScreen extends StatefulWidget {
  final Json? initial;
  final Future<void> Function(Json) onConnect;
  const ConnectionScreen({super.key, this.initial, required this.onConnect});
  @override
  State<ConnectionScreen> createState() => _ConnectionScreenState();
}

class _ConnectionScreenState extends State<ConnectionScreen> {
  late final server = TextEditingController(text: widget.initial?['server']);
  late final token = TextEditingController(text: widget.initial?['token']);
  String? error;
  bool saving = false;
  @override
  void dispose() {
    server.dispose();
    token.dispose();
    super.dispose();
  }

  Future<void> connect() async {
    late final ConnectionConfig config;
    try {
      config = ConnectionConfig.parse(server.text, token.text);
    } on FormatException catch (e) {
      setState(() => error = e.message);
      return;
    }
    setState(() {
      saving = true;
      error = null;
    });
    try {
      await widget.onConnect(config.toJson());
    } on ApiError catch (e) {
      if (mounted) setState(() => error = message(e.code));
    } catch (_) {
      if (mounted) setState(() => error = '连接未完成，请检查服务地址和网络后重试');
    } finally {
      if (mounted) setState(() => saving = false);
    }
  }

  Future<void> pasteConnection() async {
    try {
      final data = await Clipboard.getData(Clipboard.kTextPlain);
      final config = ConnectionConfig.fromQr(data?.text ?? '');
      if (!mounted) return;
      setState(() {
        server.text = config.server;
        token.text = config.token;
        error = null;
      });
    } catch (_) {
      if (mounted) setState(() => error = '请先在管理台点击「复制连接信息」，再粘贴到这里');
    }
  }

  Future<void> scan() async {
    FocusScope.of(context).unfocus();
    final config = await Navigator.of(context).push<ConnectionConfig>(
      MaterialPageRoute(builder: (_) => const ScanConnectionScreen()),
    );
    if (!mounted || config == null) return;
    setState(() {
      server.text = config.server;
      token.text = config.token;
      error = null;
    });
  }

  @override
  Widget build(BuildContext context) => Scaffold(
    appBar: AppBar(title: const Text('连接 Orbit')),
    body: Center(
      child: ConstrainedBox(
        constraints: const BoxConstraints(maxWidth: 460),
        child: ListView(
          shrinkWrap: true,
          padding: const EdgeInsets.all(28),
          children: [
            const Icon(Icons.inbox_outlined, size: 44),
            const SizedBox(height: 24),
            Text(
              '自己的消息，随手可达',
              style: Theme.of(context).textTheme.headlineSmall,
            ),
            const SizedBox(height: 12),
            const Text(
              '连接你的 Orbit 服务。打开应用后同步，离线时仍可查看已保存的内容。',
              style: TextStyle(height: 1.6),
            ),
            const SizedBox(height: 24),
            OutlinedButton.icon(
              onPressed: saving ? null : pasteConnection,
              icon: const Icon(Icons.content_paste),
              label: const Text('粘贴连接信息'),
            ),
            const SizedBox(height: 12),
            if (defaultTargetPlatform == TargetPlatform.android) ...[
              OutlinedButton.icon(
                onPressed: saving ? null : scan,
                icon: const Icon(Icons.qr_code_scanner),
                label: const Text('扫码填写'),
              ),
              const SizedBox(height: 20),
            ],
            TextField(
              controller: server,
              keyboardType: TextInputType.url,
              autocorrect: false,
              decoration: const InputDecoration(
                labelText: '服务地址',
                hintText: 'https://orbit.example.com',
              ),
            ),
            const SizedBox(height: 16),
            TextField(
              controller: token,
              obscureText: true,
              autocorrect: false,
              enableSuggestions: false,
              decoration: const InputDecoration(labelText: '设备令牌'),
            ),
            const SizedBox(height: 12),
            const Text(
              '从管理台复制连接信息或扫码即可。收件箱无需配置 MQTT 或转发规则。',
              style: TextStyle(color: Colors.black54),
            ),
            if (error != null)
              Padding(
                padding: const EdgeInsets.only(top: 16),
                child: Text(
                  error!,
                  style: TextStyle(color: Theme.of(context).colorScheme.error),
                ),
              ),
            const SizedBox(height: 28),
            FilledButton(
              onPressed: saving ? null : connect,
              child: Text(saving ? '正在连接…' : '连接'),
            ),
          ],
        ),
      ),
    ),
  );
}
