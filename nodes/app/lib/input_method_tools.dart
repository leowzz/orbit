import 'package:flutter/material.dart';
import 'package:flutter/services.dart';

class InputMethodTools extends StatefulWidget {
  const InputMethodTools({super.key});

  @override
  State<InputMethodTools> createState() => _InputMethodToolsState();
}

class _InputMethodToolsState extends State<InputMethodTools> {
  static const channel = MethodChannel('dev.orbit/node');
  bool busy = false;

  Future<void> _run({required bool pin}) async {
    setState(() => busy = true);
    String? message;
    bool showSettings = false;
    try {
      if (pin) {
        final result = await channel.invokeMethod<String>(
          'pinInputMethodShortcut',
        );
        showSettings = result != 'unsupported';
        message = switch (result) {
          'requested' => '添加请求已发送，请在桌面提示中确认。若未出现，请检查 Orbit 的桌面快捷方式权限。',
          'unsupported' => '当前桌面不支持从应用内添加快捷方式。可在此页面切换输入法。',
          _ => '桌面未接受添加请求，请检查 Orbit 的桌面快捷方式权限后重试。',
        };
      } else {
        await channel.invokeMethod<void>('showInputMethodPicker');
      }
    } on PlatformException {
      message = pin ? '未能添加快捷方式，请重试。' : '无法打开输入法列表，请重试。';
    } on MissingPluginException {
      message = '当前版本不支持此功能，请更新 Orbit。';
    } finally {
      if (mounted) setState(() => busy = false);
    }
    if (!mounted || message == null) return;
    ScaffoldMessenger.of(context).showSnackBar(
      SnackBar(
        content: Text(message),
        action: showSettings
            ? SnackBarAction(label: '权限设置', onPressed: _openSettings)
            : null,
      ),
    );
  }

  Future<void> _openSettings() async {
    try {
      await channel.invokeMethod<void>('appSettings');
    } on PlatformException {
      if (!mounted) return;
      ScaffoldMessenger.of(context).showSnackBar(
        const SnackBar(content: Text('请在系统设置中打开 Orbit 的权限管理，允许创建桌面快捷方式。')),
      );
    } on MissingPluginException {
      if (!mounted) return;
      ScaffoldMessenger.of(
        context,
      ).showSnackBar(const SnackBar(content: Text('请在系统设置中打开 Orbit 的权限管理。')));
    }
  }

  @override
  Widget build(BuildContext context) => Column(
    crossAxisAlignment: CrossAxisAlignment.start,
    children: [
      const ListTile(
        contentPadding: EdgeInsets.zero,
        leading: Icon(Icons.keyboard_outlined),
        title: Text('切换输入法'),
        subtitle: Text('打开系统输入法列表，选择要使用的输入法。'),
      ),
      Wrap(
        spacing: 12,
        runSpacing: 8,
        children: [
          FilledButton.icon(
            onPressed: busy ? null : () => _run(pin: false),
            icon: const Icon(Icons.keyboard_outlined),
            label: const Text('切换输入法'),
          ),
          OutlinedButton.icon(
            onPressed: busy ? null : () => _run(pin: true),
            icon: const Icon(Icons.add_to_home_screen_outlined),
            label: const Text('添加到桌面'),
          ),
        ],
      ),
    ],
  );
}
