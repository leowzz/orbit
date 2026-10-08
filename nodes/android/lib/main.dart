import 'dart:async';
import 'package:flutter/material.dart';
import 'package:flutter/services.dart';
import 'package:qr_flutter/qr_flutter.dart';

import 'quick_config_server.dart';

void main() => runApp(const OrbitApp());

class OrbitApp extends StatelessWidget {
  const OrbitApp({super.key});
  @override
  Widget build(BuildContext context) => MaterialApp(
    title: 'Orbit',
    debugShowCheckedModeBanner: false,
    theme: ThemeData(
      colorScheme: ColorScheme.fromSeed(seedColor: const Color(0xff24634c)),
      scaffoldBackgroundColor: const Color(0xfff5f5ef),
      useMaterial3: true,
      inputDecorationTheme: const InputDecorationTheme(
        border: OutlineInputBorder(),
        filled: true,
        fillColor: Colors.white,
      ),
    ),
    home: const NodePage(),
  );
}

class NodePage extends StatefulWidget {
  const NodePage({super.key});
  @override
  State<NodePage> createState() => _NodePageState();
}

class _NodePageState extends State<NodePage> with WidgetsBindingObserver {
  static const channel = MethodChannel('dev.orbit/node');
  final form = GlobalKey<FormState>();
  final uri = TextEditingController(text: 'ssl://');
  final nodeId = TextEditingController(text: 'android-phone');
  final username = TextEditingController();
  final password = TextEditingController();
  Map<String, dynamic> snapshot = {};
  Timer? timer;
  QuickConfigServer? quickConfigServer;
  bool busy = false;
  bool loaded = false;
  bool hidden = true;
  bool showSettings = false;
  String? message;

  @override
  void initState() {
    super.initState();
    WidgetsBinding.instance.addObserver(this);
    initialize();
    resumeRefresh();
  }

  void resumeRefresh() {
    timer?.cancel();
    timer = Timer.periodic(const Duration(seconds: 10), (_) => refresh());
  }

  @override
  void didChangeAppLifecycleState(AppLifecycleState state) {
    timer?.cancel();
    if (state == AppLifecycleState.resumed) {
      refresh();
      resumeRefresh();
    }
  }

  Future<void> initialize() async {
    try {
      final config = await channel.invokeMapMethod<String, dynamic>('load');
      if (!mounted) return;
      if (config != null) {
        uri.text = config['uri'] as String;
        nodeId.text = config['nodeId'] as String;
        username.text = config['username'] as String;
        password.text = config['password'] as String;
      }
    } on PlatformException catch (e) {
      if (mounted) setState(() => message = e.message);
    } finally {
      if (mounted) setState(() => loaded = true);
    }
    await refresh();
  }

  Future<void> refresh() async {
    try {
      final value = await channel.invokeMapMethod<String, dynamic>('snapshot');
      if (mounted) setState(() => snapshot = value ?? {});
    } on PlatformException {
      // Keep the last displayed snapshot; a later refresh can recover.
    }
  }

  Map<String, String> get config => {
    'uri': uri.text.trim(),
    'nodeId': nodeId.text.trim(),
    'username': username.text,
    'password': password.text,
  };

  Future<void> action(
    String method, {
    Object? args,
    bool validate = false,
  }) async {
    if (busy || (validate && !form.currentState!.validate())) return;
    setState(() {
      busy = true;
      message = null;
    });
    try {
      final result = await channel.invokeMethod<Object?>(method, args);
      if (!mounted) return;
      if (method == 'pin') {
        if (result == 'requested') {
          ScaffoldMessenger.of(context).showSnackBar(
            SnackBar(
              content: const Text('请在系统弹窗中确认添加'),
              action: SnackBarAction(
                label: '没有弹窗？',
                onPressed: () async {
                  try {
                    await showPinHelp(true);
                  } on PlatformException catch (e) {
                    if (mounted) {
                      setState(() => message = e.message ?? '无法打开应用信息');
                    }
                  }
                },
              ),
            ),
          );
        } else {
          await showPinHelp(false, unsupported: result == 'unsupported');
        }
        return;
      }
      setState(
        () => message = switch (method) {
          'test' => result as String?,
          'save' => '配置已加密保存。点击「开始同步」接收数据。',
          'start' => '已启动同步，请查看连接状态。',
          'stop' => '已停止后台同步。',
          _ => null,
        },
      );
      await refresh();
    } on PlatformException catch (e) {
      if (mounted) setState(() => message = e.message ?? '操作失败');
    } finally {
      if (mounted) setState(() => busy = false);
    }
  }

  Future<void> showQuickConfig() async {
    if (busy) return;
    FocusManager.instance.primaryFocus?.unfocus();
    setState(() {
      busy = true;
      message = null;
    });
    final server = QuickConfigServer(() => config, (method, values) async {
      try {
        final result = await channel.invokeMethod<String>(method, values);
        if (method == 'save') {
          // Reload the normalized native configuration, using the same vault.
          await initialize();
          return '配置已加密保存到设备。运行中的同步已重新加载；未启动时请在设备上开始同步。';
        }
        return result ?? '操作完成';
      } on PlatformException catch (error) {
        return error.message ?? '操作失败，请检查配置';
      }
    });
    quickConfigServer = server;
    try {
      final urls = await server.start();
      if (!mounted) return;
      await showDialog<void>(
        context: context,
        barrierDismissible: false,
        builder: (context) => QuickConfigDialog(urls: urls),
      );
    } catch (error) {
      if (mounted) {
        setState(
          () => message = error is StateError
              ? error.message.toString()
              : '无法启动快捷配置，请检查局域网连接后重试',
        );
      }
    } finally {
      await server.close();
      quickConfigServer = null;
      if (mounted) setState(() => busy = false);
    }
  }

  Future<void> showPinHelp(bool requested, {bool unsupported = false}) async {
    // Some launchers accept the request but silently block it behind their own
    // shortcut permission. A true result does not prove that a widget was added.
    final openSettings = await showDialog<bool>(
      context: context,
      builder: (context) => AlertDialog(
        title: Text(unsupported ? '请从桌面添加组件' : '添加桌面组件'),
        scrollable: true,
        content: Text(
          unsupported
              ? '当前桌面不支持从应用内一键添加组件，这不是 Orbit 缺少运行时权限。\n\n'
                    '请返回桌面，长按空白处进入编辑，打开「小部件」或「添加小部件」，在应用小部件中选择 Orbit 的「用量」或「Session 状态」。'
              : '${requested ? '添加请求已交给桌面，但尚未确认组件已添加。' : '桌面未接受添加请求。'}\n\n'
                    '可以返回桌面，长按空白处，从小部件列表中手动添加 Orbit。部分厂商另有「创建桌面快捷方式」开关；仅在系统提供该开关时检查它，普通应用权限页可能显示未请求权限。',
        ),
        actions: [
          TextButton(
            onPressed: () => Navigator.pop(context, false),
            child: const Text('关闭'),
          ),
          if (!unsupported)
            FilledButton(
              onPressed: () => Navigator.pop(context, true),
              child: const Text('去应用信息'),
            ),
        ],
      ),
    );
    if (openSettings == true && mounted) {
      await channel.invokeMethod<void>('appSettings');
    }
  }

  @override
  void dispose() {
    WidgetsBinding.instance.removeObserver(this);
    timer?.cancel();
    quickConfigServer?.close();
    for (final controller in [uri, nodeId, username, password]) {
      controller.dispose();
    }
    super.dispose();
  }

  Widget statusCard(
    String section,
    String label,
    IconData icon, {
    bool compact = false,
  }) => Card(
    color: const Color(0xff102827),
    margin: const EdgeInsets.only(bottom: 14),
    child: Padding(
      padding: EdgeInsets.all(compact ? 12 : 22),
      child: Column(
        crossAxisAlignment: CrossAxisAlignment.start,
        children: [
          Row(
            children: [
              Icon(icon, size: 18, color: const Color(0xffa4d8bf)),
              const SizedBox(width: 8),
              Text(
                label,
                style: const TextStyle(
                  color: Color(0xffa4d8bf),
                  letterSpacing: 1.4,
                ),
              ),
              const Spacer(),
              IconButton(
                tooltip: '添加到桌面',
                onPressed: busy ? null : () => action('pin', args: section),
                icon: const Icon(
                  Icons.add_to_home_screen,
                  color: Color(0xffa4d8bf),
                ),
              ),
            ],
          ),
          const SizedBox(height: 8),
          Text(
            snapshot['${section}Title'] as String? ?? '—',
            style: const TextStyle(
              color: Colors.white,
              fontSize: 27,
              fontWeight: FontWeight.w600,
            ),
          ),
          const SizedBox(height: 14),
          Text(
            snapshot['${section}Body'] as String? ?? '等待数据',
            style: const TextStyle(color: Color(0xffe0ece5), height: 1.65),
          ),
          const SizedBox(height: 16),
          Text(
            snapshot['${section}State'] as String? ?? '暂无数据',
            style: const TextStyle(color: Color(0xffa4d8bf), fontSize: 12),
          ),
        ],
      ),
    ),
  );

  @override
  Widget build(BuildContext context) {
    final size = MediaQuery.sizeOf(context);
    final compact = size.width >= 600 && size.height < 500;
    final overview = <Widget>[
      if (!compact)
        const Text(
          '你的工作状态，一眼可见。',
          style: TextStyle(fontSize: 24, fontWeight: FontWeight.w600),
        ),
      const SizedBox(height: 10),
      Text(
        snapshot['connection'] as String? ?? '未连接',
        style: const TextStyle(color: Color(0xff24634c)),
      ),
      Text(
        snapshot['updated'] as String? ?? '尚未接收数据',
        style: Theme.of(context).textTheme.bodySmall,
      ),
      const SizedBox(height: 20),
      if (compact)
        Row(
          crossAxisAlignment: CrossAxisAlignment.start,
          children: [
            Expanded(
              child: statusCard('usage', '用量', Icons.data_usage, compact: true),
            ),
            const SizedBox(width: 12),
            Expanded(
              child: statusCard(
                'session',
                'SESSION 状态',
                Icons.terminal,
                compact: true,
              ),
            ),
          ],
        )
      else ...[
        statusCard('usage', '用量', Icons.data_usage),
        statusCard('session', 'SESSION 状态', Icons.terminal),
      ],
      const Text('点击卡片右上角，将两个组件分别添加到 Android 桌面。'),
      const SizedBox(height: 28),
    ];
    final settings = <Widget>[
      const Text(
        'MQTT 连接',
        style: TextStyle(fontSize: 21, fontWeight: FontWeight.w600),
      ),
      const SizedBox(height: 8),
      const Text('亮屏时同步，息屏时暂停。用量约每分钟更新，Session 状态变化及时更新，可随时停止。'),
      const SizedBox(height: 18),
      Form(
        key: form,
        child: Column(
          children: [
            TextFormField(
              controller: uri,
              enabled: loaded && !busy,
              autocorrect: false,
              keyboardType: TextInputType.url,
              decoration: const InputDecoration(
                labelText: 'MQTT 服务地址',
                hintText: 'ssl://mqtt.example.com:8883',
                helperText: 'TLS 使用 mqtts:// 或 ssl://，需填写端口',
              ),
              validator: (value) {
                final parsed = Uri.tryParse(value?.trim() ?? '');
                if (parsed == null ||
                    !['ssl', 'tcp', 'mqtts', 'mqtt'].contains(parsed.scheme) ||
                    parsed.host.isEmpty ||
                    !parsed.hasPort ||
                    parsed.port < 1 ||
                    parsed.port > 65535 ||
                    parsed.userInfo.isNotEmpty ||
                    parsed.path.isNotEmpty ||
                    parsed.hasQuery ||
                    parsed.hasFragment) {
                  return '请输入完整地址，例如 ssl://mqtt.example.com:8883';
                }
                return null;
              },
            ),
            const SizedBox(height: 16),
            TextFormField(
              controller: nodeId,
              enabled: loaded && !busy,
              autocorrect: false,
              decoration: const InputDecoration(
                labelText: 'Node ID',
                helperText: '与 Core 的 projection_routes 中 node_id 一致',
              ),
              validator: (value) =>
                  RegExp(r'^[A-Za-z0-9_-]{1,64}$').hasMatch(value?.trim() ?? '')
                  ? null
                  : '使用 1–64 位字母、数字、下划线或短横线',
            ),
            const SizedBox(height: 16),
            TextFormField(
              controller: username,
              enabled: loaded && !busy,
              autocorrect: false,
              decoration: const InputDecoration(labelText: '用户名（可选）'),
            ),
            const SizedBox(height: 16),
            TextFormField(
              controller: password,
              enabled: loaded && !busy,
              obscureText: hidden,
              autocorrect: false,
              enableSuggestions: false,
              decoration: InputDecoration(
                labelText: '密码（可选）',
                suffixIcon: IconButton(
                  tooltip: hidden ? '显示密码' : '隐藏密码',
                  onPressed: () => setState(() => hidden = !hidden),
                  icon: Icon(
                    hidden
                        ? Icons.visibility_outlined
                        : Icons.visibility_off_outlined,
                  ),
                ),
              ),
              validator: (_) =>
                  password.text.isNotEmpty && username.text.trim().isEmpty
                  ? '使用密码时需要用户名'
                  : null,
            ),
          ],
        ),
      ),
    ];
    final controls = Wrap(
      spacing: 10,
      runSpacing: 10,
      children: [
        OutlinedButton.icon(
          onPressed: busy || !loaded
              ? null
              : () => action('test', args: config, validate: true),
          icon: const Icon(Icons.network_check),
          label: const Text('测试连接'),
        ),
        OutlinedButton.icon(
          onPressed: busy || !loaded ? null : showQuickConfig,
          icon: const Icon(Icons.qr_code),
          label: const Text('二维码'),
        ),
        FilledButton.icon(
          onPressed: busy || !loaded
              ? null
              : () => action('save', args: config, validate: true),
          icon: const Icon(Icons.save_outlined),
          label: const Text('保存配置'),
        ),
      ],
    );
    final feedback = <Widget>[
      if (busy)
        const Padding(
          padding: EdgeInsets.only(top: 18),
          child: LinearProgressIndicator(),
        ),
      if (message != null)
        Padding(padding: const EdgeInsets.only(top: 16), child: Text(message!)),
    ];
    return Scaffold(
      appBar: AppBar(
        title: const Text(
          'Orbit',
          style: TextStyle(fontWeight: FontWeight.w700),
        ),
        toolbarHeight: compact ? 48 : kToolbarHeight,
        backgroundColor: Colors.transparent,
        actions: [
          if (compact) ...[
            TextButton(
              onPressed: () => setState(() => showSettings = false),
              child: Text(
                '状态',
                style: TextStyle(
                  fontWeight: !showSettings
                      ? FontWeight.bold
                      : FontWeight.normal,
                ),
              ),
            ),
            TextButton(
              onPressed: () => setState(() => showSettings = true),
              child: Text(
                '配置',
                style: TextStyle(
                  fontWeight: showSettings
                      ? FontWeight.bold
                      : FontWeight.normal,
                ),
              ),
            ),
            IconButton(
              tooltip: '扫码快捷配置',
              onPressed: busy || !loaded ? null : showQuickConfig,
              icon: const Icon(Icons.qr_code),
            ),
            const SizedBox(width: 8),
          ],
          Padding(
            padding: const EdgeInsets.only(right: 16),
            child: OutlinedButton.icon(
              onPressed: busy || !loaded
                  ? null
                  : () => action(snapshot['active'] == true ? 'stop' : 'start'),
              icon: Icon(
                snapshot['active'] == true
                    ? Icons.stop_circle_outlined
                    : Icons.play_circle_outline,
              ),
              label: Text(snapshot['active'] == true ? '停止同步' : '开始同步'),
            ),
          ),
        ],
      ),
      body: SafeArea(
        child: compact
            ? Column(
                children: [
                  Expanded(
                    child: ListView(
                      key: ValueKey(showSettings),
                      keyboardDismissBehavior:
                          ScrollViewKeyboardDismissBehavior.onDrag,
                      padding: const EdgeInsets.fromLTRB(16, 4, 16, 8),
                      children: showSettings ? settings : overview,
                    ),
                  ),
                  if (showSettings)
                    Padding(
                      padding: const EdgeInsets.fromLTRB(16, 4, 16, 8),
                      child: Align(
                        alignment: Alignment.centerLeft,
                        child: controls,
                      ),
                    ),
                  if (feedback.isNotEmpty)
                    ConstrainedBox(
                      constraints: const BoxConstraints(maxHeight: 64),
                      child: SingleChildScrollView(
                        child: Padding(
                          padding: const EdgeInsets.symmetric(horizontal: 16),
                          child: Column(children: feedback),
                        ),
                      ),
                    ),
                ],
              )
            : ListView(
                keyboardDismissBehavior:
                    ScrollViewKeyboardDismissBehavior.onDrag,
                padding: const EdgeInsets.fromLTRB(20, 8, 20, 32),
                children: [
                  ...overview,
                  ...settings,
                  const SizedBox(height: 18),
                  controls,
                  ...feedback,
                ],
              ),
      ),
    );
  }
}

class QuickConfigDialog extends StatefulWidget {
  const QuickConfigDialog({super.key, required this.urls});
  final List<String> urls;

  @override
  State<QuickConfigDialog> createState() => _QuickConfigDialogState();
}

class _QuickConfigDialogState extends State<QuickConfigDialog> {
  late String selected = widget.urls.first;

  @override
  Widget build(BuildContext context) {
    final size = MediaQuery.sizeOf(context);
    final compact = size.width >= 600 && size.height < 500;
    final details = Column(
      mainAxisSize: MainAxisSize.min,
      children: [
        const Text('用手机扫描二维码，在网页配置和测试 MQTT。两台设备需连接同一局域网，请保持此弹窗打开。'),
        if (widget.urls.length > 1)
          DropdownButton<String>(
            isExpanded: true,
            value: selected,
            items: widget.urls
                .map(
                  (url) => DropdownMenuItem(
                    value: url,
                    child: Text(Uri.parse(url).host),
                  ),
                )
                .toList(),
            onChanged: (url) => setState(() => selected = url!),
          ),
        const SizedBox(height: 12),
        SelectableText(selected, style: const TextStyle(fontSize: 12)),
        const SizedBox(height: 8),
        const Text('关闭后链接失效。仅在可信局域网使用。', style: TextStyle(fontSize: 12)),
      ],
    );
    Widget qr(double side) => Container(
      color: Colors.white,
      padding: const EdgeInsets.all(8),
      child: QrImageView(data: selected, size: side),
    );
    if (compact) {
      return Dialog.fullscreen(
        child: SafeArea(
          child: Column(
            children: [
              SizedBox(
                height: 48,
                child: Row(
                  children: [
                    const SizedBox(width: 16),
                    const Expanded(
                      child: Text(
                        '扫码快捷配置',
                        style: TextStyle(
                          fontSize: 20,
                          fontWeight: FontWeight.w600,
                        ),
                      ),
                    ),
                    TextButton(
                      onPressed: () => Navigator.pop(context),
                      child: const Text('完成并关闭'),
                    ),
                    const SizedBox(width: 8),
                  ],
                ),
              ),
              Expanded(
                child: LayoutBuilder(
                  builder: (context, constraints) {
                    final side = (constraints.maxHeight - 24).clamp(0.0, 280.0);
                    return Row(
                      children: [
                        const SizedBox(width: 16),
                        qr(side),
                        const SizedBox(width: 24),
                        Expanded(child: SingleChildScrollView(child: details)),
                        const SizedBox(width: 20),
                      ],
                    );
                  },
                ),
              ),
            ],
          ),
        ),
      );
    }
    return AlertDialog(
      title: const Text('扫码快捷配置'),
      content: SizedBox(
        width: 300,
        child: SingleChildScrollView(
          child: Column(
            mainAxisSize: MainAxisSize.min,
            children: [qr(230), const SizedBox(height: 16), details],
          ),
        ),
      ),
      actions: [
        TextButton(
          onPressed: () => Navigator.pop(context),
          child: const Text('完成并关闭'),
        ),
      ],
    );
  }
}
