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
    'maxSegmentMs': 10000,
    'vadMode': 2,
  };
  Map<String, int>? saved;
  Map<String, int> draft = {};
  bool saving = false;
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
      'maxSegmentMs': (3000, 10000),
      'vadMode': (0, 3),
    };
    for (final entry in settings.entries) {
      final (min, max) = ranges[entry.key]!;
      if (entry.value < min ||
          entry.value > max ||
          (entry.key != 'vadMode' && entry.value % 20 != 0)) {
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
                    max: 10000,
                    step: 1000,
                  ),
                  const Text('录音中修改会在片段边界生效，录音不会中断。'),
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
