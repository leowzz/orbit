import 'dart:async';
import 'package:flutter/material.dart';
import 'package:flutter/services.dart';
import 'audio_capture_settings.dart';

class AudioCaptureCard extends StatefulWidget {
  const AudioCaptureCard({super.key});
  @override
  State<AudioCaptureCard> createState() => _AudioCaptureCardState();
}

class _AudioCaptureCardState extends State<AudioCaptureCard>
    with WidgetsBindingObserver {
  static const channel = MethodChannel('dev.orbit/node');
  Map<String, dynamic> status = {};
  Timer? timer;
  String? error;
  bool busy = false;
  @override
  void initState() {
    super.initState();
    WidgetsBinding.instance.addObserver(this);
    refresh();
    startPolling();
  }

  void startPolling() {
    timer?.cancel();
    timer = Timer.periodic(const Duration(seconds: 2), (_) => refresh());
  }

  Future<void> refresh() async {
    try {
      final next = await channel.invokeMapMethod<String, dynamic>(
        'audioStatus',
      );
      if (mounted) {
        setState(() {
          status = next ?? {};
          error = null;
        });
      }
    } on PlatformException {
      if (mounted) setState(() => error = '未能读取录音状态，请重试');
    } on MissingPluginException {
      if (mounted) setState(() => error = '当前设备不支持录音');
    }
  }

  Future<void> action(String method) async {
    if (busy) return;
    setState(() {
      busy = true;
      error = null;
    });
    try {
      await channel.invokeMethod<void>(method);
      await refresh();
    } on PlatformException catch (e) {
      if (mounted) setState(() => error = e.message ?? '操作失败，请重试');
    } finally {
      if (mounted) setState(() => busy = false);
    }
  }

  Future<void> openSettings() async {
    await Navigator.of(context).push<void>(
      MaterialPageRoute(builder: (_) => const AudioCaptureSettingsScreen()),
    );
    if (mounted) await refresh();
  }

  @override
  void didChangeAppLifecycleState(AppLifecycleState state) {
    timer?.cancel();
    if (state == AppLifecycleState.resumed) {
      refresh();
      startPolling();
    }
  }

  @override
  void dispose() {
    timer?.cancel();
    WidgetsBinding.instance.removeObserver(this);
    super.dispose();
  }

  @override
  Widget build(BuildContext context) {
    final theme = Theme.of(context);
    final colors = theme.colorScheme;
    final loaded = status.isNotEmpty;
    final permission = status['permission'] == true;
    final active = status['active'] == true;
    final notificationsDenied = status['notifications'] == false;
    final captureState = status['state'] as String? ?? '';
    final preparing = active && captureState == '正在准备录音';
    final captureTitle = !loaded
        ? error == null
            ? '正在读取状态'
            : '状态暂不可用'
        : !permission
            ? '麦克风未授权'
            : preparing
                ? '正在准备录音'
                : active
                    ? '正在录音'
                    : '录音未开启';
    final transferState = status['transferState'] as String? ?? 'idle';
    final needsAttention =
        const ['retrying', 'waiting', 'paused'].contains(transferState);
    final transferTitle = switch (transferState) {
      'connecting' => '正在连接',
      'sending' => '正在发送',
      'retrying' => '等待重试',
      'waiting' => '等待连接',
      'paused' => '发送已暂停',
      _ => '暂无待发送',
    };
    final inFlight = number('inFlight');
    final queued = (number('pending') - inFlight).clamp(0, 1 << 62);
    final transfer = status['transfer'] as String? ?? '';
    return Card(
      margin: const EdgeInsets.only(bottom: 16),
      child: Padding(
        padding: const EdgeInsets.all(16),
        child: Column(
          crossAxisAlignment: CrossAxisAlignment.start,
          children: [
            Text('录音', style: Theme.of(context).textTheme.titleLarge),
            const SizedBox(height: 12),
            Container(
              width: double.infinity,
              padding: const EdgeInsets.all(14),
              decoration: BoxDecoration(
                color: active
                    ? colors.primaryContainer
                    : colors.surfaceContainerHighest,
                borderRadius: BorderRadius.circular(12),
              ),
              child: Row(
                crossAxisAlignment: CrossAxisAlignment.start,
                children: [
                  Icon(
                    active ? Icons.mic : Icons.mic_off_outlined,
                    color: active
                        ? colors.onPrimaryContainer
                        : colors.onSurfaceVariant,
                    size: 28,
                  ),
                  const SizedBox(width: 12),
                  Expanded(
                    child: Column(
                      crossAxisAlignment: CrossAxisAlignment.start,
                      children: [
                        Text(captureTitle,
                            style: theme.textTheme.titleMedium?.copyWith(
                              fontWeight: FontWeight.w700,
                              color: active
                                  ? colors.onPrimaryContainer
                                  : colors.onSurface,
                            )),
                        const SizedBox(height: 4),
                        Text(
                          !loaded
                              ? error == null
                                  ? '正在获取麦克风和发送状态…'
                                  : '暂时无法获取麦克风和发送状态，请重试。'
                              : !permission
                                  ? '允许麦克风权限后开始录音，语音会分段发送到你连接的 Core。'
                                  : active && !preparing
                                      ? '正在后台录音 · 说话后自动分段发送，安静时包数不增加。'
                                      : captureState.isNotEmpty
                                          ? captureState
                                          : '点击下方按钮开始录音。',
                          style: theme.textTheme.bodyMedium?.copyWith(
                            color: active
                                ? colors.onPrimaryContainer
                                : colors.onSurfaceVariant,
                          ),
                        ),
                      ],
                    ),
                  ),
                ],
              ),
            ),
            if (loaded) ...[
              const SizedBox(height: 16),
              Row(
                children: [
                  Icon(
                      needsAttention
                          ? Icons.cloud_off_outlined
                          : Icons.cloud_upload_outlined,
                      size: 20,
                      color: needsAttention ? colors.error : colors.primary),
                  const SizedBox(width: 8),
                  Expanded(
                      child: Text('发送状态 · $transferTitle',
                          style: theme.textTheme.titleSmall?.copyWith(
                            color: needsAttention
                                ? colors.error
                                : colors.onSurface,
                          ))),
                ],
              ),
              if (transfer.isNotEmpty) ...[
                const SizedBox(height: 4),
                Text(transfer,
                    style: theme.textTheme.bodySmall
                        ?.copyWith(color: colors.onSurfaceVariant)),
              ],
              const SizedBox(height: 12),
              LayoutBuilder(builder: (context, constraints) {
                final minWidth = 90 * MediaQuery.textScalerOf(context).scale(1);
                final columns = ((constraints.maxWidth + 8) / (minWidth + 8))
                    .floor()
                    .clamp(1, 3);
                final width =
                    (constraints.maxWidth - (columns - 1) * 8) / columns;
                return Wrap(
                  spacing: 8,
                  runSpacing: 8,
                  children: [
                    metric('已发送包数', number('sentPackets'), width,
                        highlight: true),
                    metric('待发送', queued, width),
                    metric('发送中', inFlight, width),
                  ],
                );
              }),
              const SizedBox(height: 10),
              Text(
                  '已发送 ${formatBytes(number('sentBytes'))} · 最近发送：${lastSent()}',
                  style: theme.textTheme.bodySmall
                      ?.copyWith(color: colors.onSurfaceVariant)),
              const SizedBox(height: 4),
              Text('1 包 = 1 段语音。已发送为本次应用运行累计，表示手机已发出，不代表服务器已收到。',
                  style: theme.textTheme.bodySmall
                      ?.copyWith(color: colors.onSurfaceVariant)),
            ],
            if (notificationsDenied)
              const Padding(
                padding: EdgeInsets.only(top: 10),
                child: Text('系统未允许通知，停止录音请使用本页按钮；可在设置中开启通知。'),
              ),
            if (error != null) ...[
              const SizedBox(height: 10),
              Text(loaded ? '$error（上方为上次读取的数据）' : error!,
                  style: TextStyle(color: colors.error)),
              TextButton(onPressed: refresh, child: const Text('重新读取状态')),
            ],
            const SizedBox(height: 12),
            Wrap(
              spacing: 8,
              children: [
                FilledButton.icon(
                  onPressed: busy || !loaded
                      ? null
                      : () => action(active ? 'audioStop' : 'audioStart'),
                  icon: Icon(active ? Icons.stop_rounded : Icons.mic),
                  label: Text(
                    busy
                        ? '正在处理…'
                        : active
                            ? '停止录音'
                            : permission
                                ? '开始录音'
                                : '申请麦克风权限',
                  ),
                ),
                if (loaded && (!permission || notificationsDenied))
                  TextButton(
                    onPressed: busy ? null : () => action('appSettings'),
                    child: const Text('打开设置'),
                  ),
              ],
            ),
            ListTile(
              contentPadding: EdgeInsets.zero,
              title: const Text('录音设置'),
              subtitle: status['settingsPending'] == true
                  ? const Text('设置已保存，等待当前片段结束后生效')
                  : const Text('分段时长、语音检测与发送设置'),
              trailing: const Icon(Icons.chevron_right),
              onTap: openSettings,
            ),
          ],
        ),
      ),
    );
  }

  int number(String key) => (status[key] as num?)?.toInt() ?? 0;

  String formatBytes(int bytes) {
    if (bytes < 1024) return '$bytes B';
    if (bytes < 1024 * 1024) return '${(bytes / 1024).toStringAsFixed(1)} KB';
    return '${(bytes / (1024 * 1024)).toStringAsFixed(1)} MB';
  }

  String lastSent() {
    final timestamp = number('lastSentAt');
    if (timestamp <= 0) return '尚未发送';
    final elapsed = DateTime.now()
        .difference(DateTime.fromMillisecondsSinceEpoch(timestamp));
    if (elapsed.inSeconds < 10) return '刚刚';
    if (elapsed.inSeconds < 60) return '${elapsed.inSeconds} 秒前';
    if (elapsed.inMinutes < 60) return '${elapsed.inMinutes} 分钟前';
    if (elapsed.inHours < 24) return '${elapsed.inHours} 小时前';
    return '${elapsed.inDays} 天前';
  }

  Widget metric(String label, int value, double width,
      {bool highlight = false}) {
    final theme = Theme.of(context);
    return SizedBox(
      width: width,
      child: DecoratedBox(
        decoration: BoxDecoration(
          color: theme.colorScheme.surfaceContainerLow,
          borderRadius: BorderRadius.circular(10),
          border: Border.all(color: theme.colorScheme.outlineVariant),
        ),
        child: Padding(
          padding: const EdgeInsets.symmetric(horizontal: 10, vertical: 12),
          child: Column(
            crossAxisAlignment: CrossAxisAlignment.start,
            children: [
              Text(label, style: theme.textTheme.labelMedium),
              const SizedBox(height: 4),
              FittedBox(
                fit: BoxFit.scaleDown,
                child: Text('$value',
                    style: theme.textTheme.headlineSmall?.copyWith(
                      fontWeight: FontWeight.w700,
                      color: highlight
                          ? theme.colorScheme.primary
                          : theme.colorScheme.onSurface,
                    )),
              ),
            ],
          ),
        ),
      ),
    );
  }
}
