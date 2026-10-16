import 'dart:async';
import 'dart:io';
import 'package:flutter/foundation.dart';
import 'package:flutter/material.dart';
import 'package:flutter/services.dart';

/// Shared by the unified Android app and the standalone widget node.
class SoftwareUpdateCard extends StatefulWidget {
  final bool? enabled;
  const SoftwareUpdateCard({super.key, this.enabled});

  @override
  State<SoftwareUpdateCard> createState() => _SoftwareUpdateCardState();
}

class _SoftwareUpdateCardState extends State<SoftwareUpdateCard>
    with WidgetsBindingObserver {
  static const channel = MethodChannel('dev.orbit/node');
  Map<String, dynamic> status = {};
  String error = '';
  bool pending = false;
  Timer? poll;
  Timer? automatic;
  bool get enabled => widget.enabled ?? (!kIsWeb && Platform.isAndroid);
  String get phase => status['phase'] as String? ?? 'idle';
  bool get busy =>
      pending || ['checking', 'downloading', 'verifying'].contains(phase);

  @override
  void initState() {
    super.initState();
    if (!enabled) return;
    WidgetsBinding.instance.addObserver(this);
    unawaited(load());
    automatic = Timer.periodic(const Duration(hours: 6), (_) {
      if (!busy) unawaited(action('checkUpdate'));
    });
  }

  Future<void> load() async {
    await refresh();
    if (mounted && status['supported'] == true) await action('checkUpdate');
  }

  Future<void> refresh() async {
    try {
      final value = await channel.invokeMapMethod<String, dynamic>(
        'updateStatus',
      );
      if (mounted && value != null) setState(() => status = value);
    } on MissingPluginException {
      if (mounted) setState(() => error = '当前安装版本尚不支持更新，请安装新版后重试');
    } on PlatformException catch (e) {
      if (mounted) setState(() => error = e.message ?? '无法读取更新状态，请重试');
    }
  }

  Future<void> action(String command) async {
    if (busy) return;
    setState(() {
      pending = true;
      error = '';
    });
    poll?.cancel();
    poll = Timer.periodic(
      const Duration(milliseconds: 500),
      (_) => unawaited(refresh()),
    );
    try {
      final value = await channel.invokeMapMethod<String, dynamic>(command);
      if (mounted && value != null) setState(() => status = value);
    } on MissingPluginException {
      if (mounted) setState(() => error = '当前安装版本尚不支持更新，请安装新版后重试');
    } on PlatformException catch (e) {
      if (mounted) setState(() => error = e.message ?? '更新失败，请重试');
    } finally {
      poll?.cancel();
      if (mounted) setState(() => pending = false);
    }
  }

  @override
  void didChangeAppLifecycleState(AppLifecycleState state) {
    if (state == AppLifecycleState.resumed) unawaited(refresh());
  }

  @override
  void dispose() {
    WidgetsBinding.instance.removeObserver(this);
    poll?.cancel();
    automatic?.cancel();
    super.dispose();
  }

  @override
  Widget build(BuildContext context) {
    if (!enabled) return const SizedBox.shrink();
    final available = status['available'] == true;
    final messages = {
      'checking': '正在检查更新…',
      'downloading': '正在下载更新…',
      'verifying': '正在校验安装包…',
      'permission': '请允许安装应用，返回后将继续打开安装器。',
      'installer': '安装器已打开，请按系统提示完成更新。取消后可重试。',
    };
    final failure = error.isNotEmpty ? error : status['error'] as String? ?? '';
    final message =
        messages[phase] ??
        (available
            ? '发现新版本 ${status['latestVersion']}'
            : failure.isNotEmpty
            ? '未能检查更新，请重试'
            : status['latestVersion'] != null
            ? '已是最新版本'
            : '启动时自动检查，也可手动检查更新');
    final total = (status['total'] as num?)?.toDouble() ?? 0;
    final downloaded = (status['downloaded'] as num?)?.toDouble() ?? 0;
    return Card(
      margin: const EdgeInsets.symmetric(vertical: 12),
      child: Padding(
        padding: const EdgeInsets.all(16),
        child: Column(
          crossAxisAlignment: CrossAxisAlignment.start,
          children: [
            Text('软件更新', style: Theme.of(context).textTheme.titleMedium),
            const SizedBox(height: 8),
            Text('当前版本 ${status['currentVersion'] ?? '正在读取…'}'),
            const SizedBox(height: 4),
            Text(message),
            if (phase == 'downloading') ...[
              const SizedBox(height: 12),
              LinearProgressIndicator(
                value: total > 0 ? (downloaded / total).clamp(0, 1) : null,
              ),
            ],
            if (failure.isNotEmpty) ...[
              const SizedBox(height: 8),
              Text(
                failure,
                style: TextStyle(color: Theme.of(context).colorScheme.error),
              ),
            ],
            const SizedBox(height: 12),
            Wrap(
              spacing: 8,
              runSpacing: 8,
              children: [
                OutlinedButton.icon(
                  onPressed: busy
                      ? null
                      : () => unawaited(action('checkUpdate')),
                  icon: const Icon(Icons.refresh),
                  label: const Text('检查更新'),
                ),
                if (available)
                  FilledButton.icon(
                    onPressed: busy
                        ? null
                        : () => unawaited(action('installUpdate')),
                    icon: const Icon(Icons.system_update_outlined),
                    label: Text(
                      phase == 'permission'
                          ? '允许安装并继续'
                          : phase == 'installer'
                          ? '重新打开安装器'
                          : '立即更新',
                    ),
                  ),
              ],
            ),
            if (available) ...[
              const SizedBox(height: 8),
              const Text('下载后按系统提示安装，现有数据和设置会保留。'),
            ],
          ],
        ),
      ),
    );
  }
}
