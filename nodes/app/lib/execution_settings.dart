import 'package:flutter/foundation.dart';
import 'package:flutter/material.dart';
import 'package:uuid/uuid.dart';
import 'inbox_controller.dart';
import 'local_store.dart';
import 'node_execution.dart';

class ExecutionSettingsScreen extends StatefulWidget {
  final InboxController controller;
  const ExecutionSettingsScreen({super.key, required this.controller});
  @override
  State<ExecutionSettingsScreen> createState() =>
      _ExecutionSettingsScreenState();
}

class _ExecutionSettingsScreenState extends State<ExecutionSettingsScreen> {
  bool loading = true, enabled = false, saving = false;
  List<Json> items = [];
  String? error;
  @override
  void initState() {
    super.initState();
    _load();
  }

  Future<void> _load() async {
    try {
      await widget.controller.execution.load();
      if (!mounted) return;
      setState(() {
        enabled = widget.controller.execution.enabled;
        items = widget.controller.execution.items.map((e) => {...e}).toList();
        loading = false;
      });
    } catch (_) {
      if (mounted) {
        setState(() {
          error = '未能读取本机快捷操作，请重新打开设置';
          loading = false;
        });
      }
    }
  }

  Future<void> _save() async {
    setState(() {
      saving = true;
      error = null;
    });
    try {
      await widget.controller.execution.save(enabled, items);
      await widget.controller.execution.sync(
        active: () => widget.controller.active,
      );
      if (!mounted) return;
      final failure = widget.controller.execution.error;
      ScaffoldMessenger.of(context).showSnackBar(
        SnackBar(
          content: Text(failure == null ? '已保存本机快捷操作' : '已保存在本机，连接恢复后同步'),
        ),
      );
    } catch (_) {
      if (mounted) setState(() => error = '保存未完成，请检查入口后重试');
    } finally {
      if (mounted) setState(() => saving = false);
    }
  }

  Future<void> _url([Json? previous]) async {
    final result = await showDialog<Json>(
      context: context,
      builder: (_) => _ShortcutUrlDialog(previous: previous),
    );
    if (result == null || !mounted) return;
    setState(() {
      final index = items.indexWhere((e) => e['id'] == result['id']);
      if (index < 0) {
        items.add(result);
      } else {
        items[index] = result;
      }
    });
  }

  Future<void> _app() async {
    try {
      final apps = await widget.controller.execution.applications();
      if (!mounted) return;
      final chosen = await showDialog<Json>(
        context: context,
        builder: (context) => SimpleDialog(
          title: const Text('选择本机应用'),
          children: apps.isEmpty
              ? [
                  const Padding(
                    padding: EdgeInsets.all(24),
                    child: Text('没有可添加的应用'),
                  ),
                ]
              : apps
                    .map(
                      (app) => SimpleDialogOption(
                        onPressed: () => Navigator.pop(context, app),
                        child: Text(app['title'] as String),
                      ),
                    )
                    .toList(),
        ),
      );
      if (chosen == null || !mounted) return;
      if (items.any(
        (e) => e['kind'] == 'app' && e['target'] == chosen['target'],
      )) {
        return;
      }
      setState(
        () => items.add({
          'id': const Uuid().v4(),
          'title': chosen['title'],
          'target': chosen['target'],
          'kind': 'app',
        }),
      );
    } catch (_) {
      if (mounted) setState(() => error = '未能读取本机应用，请重试');
    }
  }

  @override
  Widget build(BuildContext context) => Scaffold(
    appBar: AppBar(
      title: const Text('本机快捷操作'),
      actions: [
        TextButton(
          onPressed: loading || saving ? null : _save,
          child: Text(saving ? '保存中…' : '保存'),
        ),
      ],
    ),
    body: loading
        ? const Center(child: CircularProgressIndicator())
        : Center(
            child: ConstrainedBox(
              constraints: const BoxConstraints(maxWidth: 760),
              child: ListView(
                padding: const EdgeInsets.all(24),
                children: [
                  SwitchListTile(
                    contentPadding: EdgeInsets.zero,
                    title: const Text('允许通过 Orbit 打开本机入口'),
                    subtitle: const Text(
                      '仅执行下方明确添加的应用和网址。此应用在前台连接时接收操作，接收设备的配对在 Core 管理。',
                    ),
                    value: enabled,
                    onChanged: saving
                        ? null
                        : (v) => setState(() => enabled = v),
                  ),
                  const SizedBox(height: 20),
                  ...items.indexed.map(
                    (entry) => ListTile(
                      contentPadding: EdgeInsets.zero,
                      leading: Icon(
                        entry.$2['kind'] == 'app'
                            ? Icons.apps_outlined
                            : Icons.language,
                      ),
                      title: Text(entry.$2['title'] as String),
                      subtitle: Text(entry.$2['target'] as String),
                      onTap: entry.$2['kind'] == 'url' && !saving
                          ? () => _url(entry.$2)
                          : null,
                      trailing: Row(
                        mainAxisSize: MainAxisSize.min,
                        children: [
                          IconButton(
                            tooltip: '上移',
                            onPressed: saving || entry.$1 == 0
                                ? null
                                : () => setState(() {
                                    final item = items.removeAt(entry.$1);
                                    items.insert(entry.$1 - 1, item);
                                  }),
                            icon: const Icon(Icons.arrow_upward, size: 20),
                          ),
                          IconButton(
                            tooltip: '移除',
                            onPressed: saving
                                ? null
                                : () =>
                                      setState(() => items.removeAt(entry.$1)),
                            icon: const Icon(Icons.close, size: 20),
                          ),
                        ],
                      ),
                    ),
                  ),
                  if (items.length < 24)
                    Wrap(
                      spacing: 12,
                      runSpacing: 12,
                      children: [
                        OutlinedButton.icon(
                          onPressed: saving ? null : () => _url(),
                          icon: const Icon(Icons.add_link),
                          label: const Text('添加网址'),
                        ),
                        if (defaultTargetPlatform == TargetPlatform.android)
                          OutlinedButton.icon(
                            onPressed: saving ? null : _app,
                            icon: const Icon(Icons.apps),
                            label: const Text('添加应用'),
                          ),
                      ],
                    ),
                  if (error != null)
                    Padding(
                      padding: const EdgeInsets.only(top: 16),
                      child: Text(
                        error!,
                        style: TextStyle(
                          color: Theme.of(context).colorScheme.error,
                        ),
                      ),
                    ),
                ],
              ),
            ),
          ),
  );
}

class _ShortcutUrlDialog extends StatefulWidget {
  final Json? previous;
  const _ShortcutUrlDialog({this.previous});
  @override
  State<_ShortcutUrlDialog> createState() => _ShortcutUrlDialogState();
}

class _ShortcutUrlDialogState extends State<_ShortcutUrlDialog> {
  final form = GlobalKey<FormState>();
  late final title = TextEditingController(text: widget.previous?['title']);
  late final target = TextEditingController(
    text: widget.previous?['target'] ?? 'https://',
  );
  @override
  void dispose() {
    title.dispose();
    target.dispose();
    super.dispose();
  }

  @override
  Widget build(BuildContext context) => AlertDialog(
    scrollable: true,
    title: Text(widget.previous == null ? '添加网址' : '编辑网址'),
    content: SizedBox(
      width: 400,
      child: Form(
        key: form,
        child: Column(
          mainAxisSize: MainAxisSize.min,
          children: [
            TextFormField(
              controller: title,
              autofocus: true,
              maxLength: 40,
              decoration: const InputDecoration(labelText: '名称'),
              validator: (v) => v == null || v.trim().isEmpty ? '请填写名称' : null,
            ),
            const SizedBox(height: 16),
            TextFormField(
              controller: target,
              keyboardType: TextInputType.url,
              autocorrect: false,
              decoration: const InputDecoration(labelText: '在这台设备打开的网址'),
              validator: (v) =>
                  NodeExecution.validEntry({
                    'id': 'entry',
                    'title': 'Entry',
                    'kind': 'url',
                    'target': v?.trim() ?? '',
                  })
                  ? null
                  : '请填写有效的完整网址或协议链接',
            ),
          ],
        ),
      ),
    ),
    actions: [
      TextButton(
        onPressed: () => Navigator.pop(context),
        child: const Text('取消'),
      ),
      FilledButton(
        onPressed: () {
          if (!form.currentState!.validate()) return;
          final entry = <String, dynamic>{
            'id': widget.previous?['id'] ?? const Uuid().v4(),
            'title': title.text.trim(),
            'kind': 'url',
            'target': target.text.trim(),
          };
          if (NodeExecution.validEntry(entry)) Navigator.pop(context, entry);
        },
        child: Text(widget.previous == null ? '添加到列表' : '保存修改'),
      ),
    ],
  );
}
