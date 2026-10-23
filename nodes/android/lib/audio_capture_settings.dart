import 'package:flutter/foundation.dart';
import 'package:flutter/material.dart';
import 'package:flutter/services.dart';

String audioDuration(int milliseconds) {
  if (milliseconds < 1000) return '$milliseconds 毫秒';
  final seconds = (milliseconds / 1000)
      .toStringAsFixed(2)
      .replaceFirst(RegExp(r'\.?0+$'), '');
  return '$seconds 秒';
}

class AudioCaptureSettingsScreen extends StatefulWidget {
  const AudioCaptureSettingsScreen({super.key});

  @override
  State<AudioCaptureSettingsScreen> createState() =>
      _AudioCaptureSettingsScreenState();
}

class _AudioCaptureSettingsScreenState
    extends State<AudioCaptureSettingsScreen> {
  static const channel = MethodChannel('dev.orbit/node');
  static const defaults = {
    'minSpeechMs': 300,
    'silenceMs': 600,
    'preRollMs': 300,
    'maxSegmentMs': 20000,
    'vadMode': 2,
    'uploadConcurrency': 16,
  };
  Map<String, int>? saved;
  Map<String, int> draft = {};
  bool saving = false;
  bool clearing = false;
  String? queueResult;
  String? loadError;

  @override
  void initState() {
    super.initState();
    load();
  }

  Map<String, int> settingsFrom(Object? value) {
    if (value is! Map) throw const FormatException();
    final settings = {
      for (final key in defaults.keys)
        key: value[key] is int
            ? value[key] as int
            : throw const FormatException(),
    };
    const ranges = {
      'minSpeechMs': (100, 2000),
      'silenceMs': (200, 2000),
      'preRollMs': (0, 1000),
      'maxSegmentMs': (3000, 20000),
      'vadMode': (0, 3),
      'uploadConcurrency': (1, 64),
    };
    for (final entry in settings.entries) {
      final (min, max) = ranges[entry.key]!;
      if (entry.value < min ||
          entry.value > max ||
          (entry.key.endsWith('Ms') && entry.value % 20 != 0)) {
        throw const FormatException();
      }
    }
    return settings;
  }

  Future<void> load() async {
    setState(() => loadError = null);
    try {
      final status = await channel.invokeMapMethod<String, dynamic>(
        'audioStatus',
      );
      final settings = settingsFrom(status?['settings']);
      if (!mounted) return;
      setState(() {
        saved = settings;
        draft = {...settings};
      });
    } catch (_) {
      if (mounted) {
        setState(() => loadError = '未能读取录音设置，请重试');
      }
    }
  }

  Future<void> save() async {
    if (saving) return;
    setState(() => saving = true);
    try {
      final result = await channel.invokeMapMethod<String, dynamic>(
        'audioSaveSettings',
        draft,
      );
      final settings = settingsFrom(result);
      if (!mounted) return;
      setState(() {
        saved = settings;
        draft = {...settings};
      });
      ScaffoldMessenger.of(
        context,
      ).showSnackBar(const SnackBar(content: Text('已保存录音设置')));
    } catch (_) {
      if (mounted) {
        ScaffoldMessenger.of(
          context,
        ).showSnackBar(const SnackBar(content: Text('未能保存录音设置，修改已保留，请重试')));
      }
    } finally {
      if (mounted) setState(() => saving = false);
    }
  }

  Future<void> clearQueue() async {
    if (clearing) return;
    final confirmed = await showDialog<bool>(
      context: context,
      builder: (context) => AlertDialog(
        title: const Text('清空缓存队列？'),
        content: const Text(
          '将永久删除本机所有连接下尚未发送的录音缓存，无法恢复。已发送的内容不会撤回。录音会继续，新片段仍会进入队列。',
        ),
        actions: [
          TextButton(
            onPressed: () => Navigator.pop(context, false),
            child: const Text('取消'),
          ),
          TextButton(
            onPressed: () => Navigator.pop(context, true),
            child: const Text('确认清空'),
          ),
        ],
      ),
    );
    if (confirmed != true || !mounted) return;
    setState(() {
      clearing = true;
      queueResult = null;
    });
    try {
      final removed = await channel.invokeMethod<int>('audioClearQueue');
      if (mounted) {
        setState(() => queueResult = '已清空缓存队列，删除 ${removed ?? 0} 段录音');
      }
    } catch (_) {
      if (mounted) {
        setState(() => queueResult = '未能完全清空缓存队列，请重试');
      }
    } finally {
      if (mounted) setState(() => clearing = false);
    }
  }

  Widget duration({
    required String key,
    required String label,
    required String explanation,
    required int min,
    required int max,
    int step = 100,
  }) {
    final value = draft[key]!;
    return Padding(
      padding: const EdgeInsets.only(bottom: 16),
      child: Column(
        crossAxisAlignment: CrossAxisAlignment.start,
        children: [
          Row(
            crossAxisAlignment: CrossAxisAlignment.start,
            children: [
              Expanded(
                child: Text(
                  label,
                  style: Theme.of(context).textTheme.titleMedium,
                ),
              ),
              const SizedBox(width: 12),
              Text(audioDuration(value)),
            ],
          ),
          const SizedBox(height: 4),
          Text(explanation, style: Theme.of(context).textTheme.bodyMedium),
          Semantics(
            label: label,
            child: Slider(
              key: ValueKey(key),
              min: min.toDouble(),
              max: max.toDouble(),
              divisions: (max - min) ~/ step,
              value: value.toDouble(),
              label: audioDuration(value),
              semanticFormatterCallback: (v) => audioDuration(v.round()),
              onChanged: saving
                  ? null
                  : (v) => setState(() => draft[key] = v.round()),
            ),
          ),
        ],
      ),
    );
  }

  @override
  Widget build(BuildContext context) => Scaffold(
    appBar: AppBar(
      title: const Text('录音设置'),
      actions: [
        TextButton(
          onPressed: saved == null || saving || mapEquals(saved, draft)
              ? null
              : save,
          child: Text(saving ? '保存中' : '保存'),
        ),
      ],
    ),
    body: Center(
      child: ConstrainedBox(
        constraints: const BoxConstraints(maxWidth: 720),
        child: saved == null
            ? loadError == null
                  ? const CircularProgressIndicator()
                  : Padding(
                      padding: const EdgeInsets.all(24),
                      child: Column(
                        mainAxisSize: MainAxisSize.min,
                        children: [
                          Text(loadError!),
                          TextButton(onPressed: load, child: const Text('重试')),
                        ],
                      ),
                    )
            : ListView(
                padding: const EdgeInsets.all(24),
                children: [
                  Text('缓存队列', style: Theme.of(context).textTheme.titleMedium),
                  const SizedBox(height: 4),
                  const Text('异常积压时可清除本机待发送的录音片段，释放存储空间。'),
                  Align(
                    alignment: Alignment.centerLeft,
                    child: TextButton.icon(
                      onPressed: clearing ? null : clearQueue,
                      icon: const Icon(Icons.delete_outline),
                      label: Text(clearing ? '正在清空…' : '清空缓存队列'),
                    ),
                  ),
                  if (queueResult != null) Text(queueResult!),
                  const Divider(height: 32),
                  duration(
                    key: 'minSpeechMs',
                    label: '人声连续时长阈值',
                    explanation: '连续检测到人声达到此时长才保留片段，短促声音会被忽略。',
                    min: 100,
                    max: 2000,
                  ),
                  duration(
                    key: 'silenceMs',
                    label: '结束静音时长',
                    explanation: '连续静音达到此时长后结束当前片段并上传。',
                    min: 200,
                    max: 2000,
                  ),
                  DropdownButtonFormField<int>(
                    key: ValueKey('vadMode-${draft['vadMode']}'),
                    initialValue: draft['vadMode'],
                    isExpanded: true,
                    decoration: const InputDecoration(labelText: '人声检测严格度'),
                    items: const [
                      DropdownMenuItem(value: 0, child: Text('宽松')),
                      DropdownMenuItem(value: 1, child: Text('标准')),
                      DropdownMenuItem(value: 2, child: Text('严格')),
                      DropdownMenuItem(value: 3, child: Text('最严格')),
                    ],
                    onChanged: saving
                        ? null
                        : (value) {
                            if (value != null) {
                              setState(() => draft['vadMode'] = value);
                            }
                          },
                  ),
                  const SizedBox(height: 8),
                  const Text('越严格越容易过滤噪声，也可能漏掉轻声说话。'),
                  const SizedBox(height: 24),
                  duration(
                    key: 'preRollMs',
                    label: '人声前保留时长',
                    explanation: '保留人声开始前的一小段声音，减少字头丢失。',
                    min: 0,
                    max: 1000,
                  ),
                  duration(
                    key: 'maxSegmentMs',
                    label: '单段最长时长',
                    explanation: '持续说话时按此时长分段上传，录音不中断。',
                    min: 3000,
                    max: 20000,
                    step: 1000,
                  ),
                  Text(
                    '最大上传并发：${draft['uploadConcurrency']}',
                    style: Theme.of(context).textTheme.titleMedium,
                  ),
                  const SizedBox(height: 4),
                  const Text('同时发送的片段数量。新录音优先，发送完成立即续发；停止录音后仍继续上传。'),
                  Slider(
                    key: const ValueKey('uploadConcurrency'),
                    min: 1,
                    max: 64,
                    divisions: 63,
                    value: draft['uploadConcurrency']!.toDouble(),
                    label: '${draft['uploadConcurrency']}',
                    semanticFormatterCallback: (v) => '${v.round()} 段',
                    onChanged: saving
                        ? null
                        : (v) => setState(
                            () => draft['uploadConcurrency'] = v.round(),
                          ),
                  ),
                  const Text('发送成功即清除本机片段，不等待服务器确认。网络或服务器异常可能丢失录音。'),
                  const SizedBox(height: 16),
                  const Text('并发设置保存后立即生效；录音参数在片段边界生效。'),
                  Align(
                    alignment: Alignment.centerLeft,
                    child: TextButton(
                      onPressed: saving
                          ? null
                          : () => setState(() {
                              draft['silenceMs'] = 300;
                              draft['maxSegmentMs'] = 3000;
                              draft['uploadConcurrency'] = 16;
                            }),
                      child: const Text('使用低延迟预设'),
                    ),
                  ),
                  const Text('停顿 300 毫秒即提交；连续讲话每 3 秒提交。转写还需要语音识别处理时间。'),
                  const SizedBox(height: 8),
                  Align(
                    alignment: Alignment.centerLeft,
                    child: TextButton(
                      onPressed: saving || mapEquals(draft, defaults)
                          ? null
                          : () => setState(() => draft = {...defaults}),
                      child: const Text('恢复默认值'),
                    ),
                  ),
                ],
              ),
      ),
    ),
  );
}
