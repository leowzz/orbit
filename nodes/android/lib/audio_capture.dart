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
      // Capture is available in the Android application.
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
    final permission = status['permission'] == true;
    final active = status['active'] == true;
    final notificationsDenied = status['notifications'] == false;
    return Card(
      margin: const EdgeInsets.only(bottom: 16),
      child: Padding(
        padding: const EdgeInsets.all(16),
        child: Column(
          crossAxisAlignment: CrossAxisAlignment.start,
          children: [
            Text('录音', style: Theme.of(context).textTheme.titleLarge),
            const SizedBox(height: 8),
            Text(
              permission
                  ? status['state'] as String? ?? '正在读取录音状态'
                  : '需要麦克风权限才能后台录音。允许后会自动开始，语音片段将上传到你配置的 Core。',
            ),
            if ((status['transfer'] as String? ?? '').isNotEmpty)
              Padding(
                padding: const EdgeInsets.only(top: 8),
                child: Text(status['transfer'] as String),
              ),
            if (notificationsDenied)
              const Text('系统未允许通知，停止录音请使用本页按钮；可在设置中开启通知。'),
            if (error != null) Text(error!),
            const SizedBox(height: 8),
            Wrap(
              spacing: 8,
              children: [
                FilledButton.tonal(
                  onPressed: busy
                      ? null
                      : () => action(active ? 'audioStop' : 'audioStart'),
                  child: Text(
                    active
                        ? '停止录音'
                        : permission
                        ? '开始录音'
                        : '申请麦克风权限',
                  ),
                ),
                if (!permission || notificationsDenied)
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
                  : null,
              trailing: const Icon(Icons.chevron_right),
              onTap: openSettings,
            ),
          ],
        ),
      ),
    );
  }
}
