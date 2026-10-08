import 'dart:async';
import 'dart:convert';
import 'dart:io';
import 'package:flutter/material.dart';
import 'package:flutter/services.dart';
import 'package:image_picker/image_picker.dart';
import 'inbox_controller.dart';
import 'local_store.dart';

class InboxScreen extends StatefulWidget {
  final InboxController controller;
  final VoidCallback onSettings;
  const InboxScreen({
    super.key,
    required this.controller,
    required this.onSettings,
  });
  @override
  State<InboxScreen> createState() => _InboxScreenState();
}

class _InboxScreenState extends State<InboxScreen> {
  final text = TextEditingController();
  String kind = 'text', filter = 'all';
  bool sending = false;
  InboxController get c => widget.controller;
  @override
  void initState() {
    super.initState();
    _draft();
  }

  @override
  void didUpdateWidget(InboxScreen oldWidget) {
    super.didUpdateWidget(oldWidget);
    if (oldWidget.controller != c) {
      text.clear();
      _draft();
    }
  }

  Future<void> _draft() async {
    final draft = await c.local.meta('draft');
    if (mounted && text.text.isEmpty) text.text = draft ?? '';
    if (Platform.isAndroid) {
      try {
        final lost = await ImagePicker().retrieveLostData();
        if (mounted && lost.files?.isNotEmpty == true) {
          await send(photo: lost.files!.first.path);
        }
      } catch (_) {
        notice('未能恢复所选图片，请重新选择');
      }
    }
  }

  @override
  void dispose() {
    text.dispose();
    super.dispose();
  }

  void notice(String message) {
    if (mounted) {
      ScaffoldMessenger.of(
        context,
      ).showSnackBar(SnackBar(content: Text(message)));
    }
  }

  Future<void> copy(String body) async {
    await Clipboard.setData(ClipboardData(text: body));
    notice('已复制');
  }

  Future<void> send({String? photo}) async {
    if (sending || (photo == null && text.text.trim().isEmpty)) return;
    setState(() => sending = true);
    try {
      // Store locally before clearing the composer; network work continues separately.
      final saved = text.text.trim();
      final future = c.submit(
        'create',
        kind: photo == null ? kind : 'image',
        body: saved,
        photo: photo,
      );
      // Keep the composer intact until the local transaction commits.
      await future;
      if (mounted) {
        text.clear();
        await c.local.setMeta('draft', '');
      }
    } catch (_) {
      notice('未能保存到本机，内容已保留，请重试');
    } finally {
      if (mounted) setState(() => sending = false);
    }
  }

  Future<void> pickImage() async {
    try {
      final photo = await ImagePicker().pickImage(source: ImageSource.gallery);
      if (photo == null) return;
      if (await photo.length() > 10 * 1024 * 1024) {
        notice('图片需小于 10 MB');
        return;
      }
      await send(photo: photo.path);
    } catch (_) {
      notice('无法读取图片，请重新选择');
    }
  }

  Future<String?> editText(String initial, String title) async {
    final editor = TextEditingController(text: initial);
    final value = await showDialog<String>(
      context: context,
      builder: (context) => AlertDialog(
        title: Text(title),
        content: SizedBox(
          width: 460,
          child: TextField(
            controller: editor,
            autofocus: true,
            minLines: 3,
            maxLines: 10,
            maxLength: 4000,
            decoration: const InputDecoration(hintText: '写点什么…'),
          ),
        ),
        actions: [
          TextButton(
            onPressed: () => Navigator.pop(context),
            child: const Text('取消'),
          ),
          FilledButton(
            onPressed: () => Navigator.pop(context, editor.text.trim()),
            child: const Text('保存'),
          ),
        ],
      ),
    );
    // The dialog route owns an exit animation that still uses the controller.
    Future<void>.delayed(const Duration(milliseconds: 400), editor.dispose);
    return value;
  }

  Future<void> edit(Json item) async {
    final value = await editText(item['body'], '编辑内容');
    if (value == null || (value.isEmpty && item['kind'] != 'image')) return;
    await c.submit('update', item: item, body: value);
  }

  Future<void> remove(Json item) async {
    final confirmed = await showDialog<bool>(
      context: context,
      builder: (context) => AlertDialog(
        title: const Text('删除这条内容？'),
        content: const Text('删除后会同步到所有设备，无法撤销。'),
        actions: [
          TextButton(
            onPressed: () => Navigator.pop(context, false),
            child: const Text('取消'),
          ),
          TextButton(
            onPressed: () => Navigator.pop(context, true),
            child: const Text('删除'),
          ),
        ],
      ),
    );
    if (confirmed == true) await c.submit('delete', item: item);
  }

  Future<void> resolve(Json entry) async {
    final op = jsonDecode(entry['payload']) as Json;
    final latest = await c.local.item(entry['item_id']);
    if (!mounted) return;
    await showDialog<void>(
      context: context,
      builder: (context) => AlertDialog(
        title: const Text('处理冲突'),
        content: SizedBox(
          width: 460,
          child: SingleChildScrollView(
            child: Column(
              crossAxisAlignment: CrossAxisAlignment.start,
              mainAxisSize: MainAxisSize.min,
              children: [
                const Text(
                  '你的修改',
                  style: TextStyle(fontWeight: FontWeight.w600),
                ),
                const SizedBox(height: 8),
                SelectableText(
                  op['body'] ??
                      (op['type'] == 'delete'
                          ? '删除内容'
                          : op['completed'] == true
                          ? '完成待办'
                          : '撤销完成'),
                ),
                const SizedBox(height: 20),
                const Text(
                  '当前内容',
                  style: TextStyle(fontWeight: FontWeight.w600),
                ),
                const SizedBox(height: 8),
                SelectableText(
                  latest == null || latest['deleted_at'] != null
                      ? '已被删除'
                      : latest['body'],
                ),
                const SizedBox(height: 16),
                const Text('可复制你的修改后重新编辑，或放弃本次修改。'),
              ],
            ),
          ),
        ),
        actions: [
          if (op['body'] != null)
            TextButton(
              onPressed: () => copy(op['body']),
              child: const Text('复制修改'),
            ),
          TextButton(
            onPressed: () async {
              await c.discard(entry);
              if (context.mounted) Navigator.pop(context);
            },
            child: const Text('放弃修改'),
          ),
          FilledButton(
            onPressed: () => Navigator.pop(context),
            child: const Text('保留草稿'),
          ),
        ],
      ),
    );
  }

  Future<void> showImage(String id) => Navigator.of(context).push(
    MaterialPageRoute<void>(
      builder: (_) => Scaffold(
        appBar: AppBar(title: const Text('图片')),
        body: Center(
          child: AttachmentImage(controller: c, id: id, full: true),
        ),
      ),
    ),
  );

  @override
  Widget build(BuildContext context) => AnimatedBuilder(
    animation: c,
    builder: (context, _) {
      final visible = c.items
          .where((i) => filter == 'all' || i['kind'] == filter)
          .toList();
      final scheme = Theme.of(context).colorScheme;
      return Scaffold(
        appBar: AppBar(
          title: const Row(
            mainAxisSize: MainAxisSize.min,
            children: [
              Icon(Icons.blur_circular_rounded, size: 27),
              SizedBox(width: 10),
              Text(
                'Orbit',
                style: TextStyle(
                  fontWeight: FontWeight.w700,
                  letterSpacing: -.5,
                ),
              ),
            ],
          ),
          actions: [
            IconButton(
              tooltip: '同步',
              onPressed: c.syncing ? null : c.sync,
              icon: c.syncing
                  ? const SizedBox(
                      width: 20,
                      height: 20,
                      child: CircularProgressIndicator(strokeWidth: 2),
                    )
                  : const Icon(Icons.sync_rounded),
            ),
            IconButton(
              tooltip: '连接设置',
              onPressed: widget.onSettings,
              icon: const Icon(Icons.settings_outlined),
            ),
            const SizedBox(width: 8),
          ],
        ),
        body: SafeArea(
          top: false,
          child: Center(
            child: ConstrainedBox(
              constraints: const BoxConstraints(maxWidth: 900),
              child: Column(
                children: [
                  StatusSummary(view: c.view, online: c.online),
                  if (c.error != null)
                    Padding(
                      padding: const EdgeInsets.fromLTRB(20, 0, 20, 4),
                      child: Row(
                        children: [
                          const Icon(Icons.cloud_off_outlined, size: 17),
                          const SizedBox(width: 8),
                          Expanded(
                            child: Text(
                              c.error!,
                              style: const TextStyle(fontSize: 13),
                            ),
                          ),
                          TextButton(
                            onPressed: c.syncing ? null : c.sync,
                            child: const Text('重试'),
                          ),
                        ],
                      ),
                    ),
                  Padding(
                    padding: const EdgeInsets.fromLTRB(20, 10, 20, 10),
                    child: Row(
                      children: [
                        Text(
                          '收件箱',
                          style: Theme.of(context).textTheme.titleLarge
                              ?.copyWith(fontWeight: FontWeight.w700),
                        ),
                        const Spacer(),
                        ...[('all', '全部'), ('todo', '待办'), ('image', '图片')].map(
                          (f) => Padding(
                            padding: const EdgeInsets.only(left: 6),
                            child: ChoiceChip(
                              label: Text(f.$2),
                              selected: filter == f.$1,
                              showCheckmark: false,
                              onSelected: (_) => setState(() => filter = f.$1),
                            ),
                          ),
                        ),
                      ],
                    ),
                  ),
                  Expanded(
                    child: RefreshIndicator(
                      onRefresh: c.sync,
                      child: ListView(
                        key: const PageStorageKey('inbox'),
                        physics: const AlwaysScrollableScrollPhysics(),
                        padding: const EdgeInsets.fromLTRB(20, 0, 20, 16),
                        children: [
                          for (final entry in c.pending) _pending(entry),
                          if (visible.isEmpty && c.pending.isEmpty)
                            Padding(
                              padding: const EdgeInsets.symmetric(vertical: 60),
                              child: Column(
                                children: [
                                  Icon(
                                    Icons.inbox_outlined,
                                    size: 42,
                                    color: scheme.outline,
                                  ),
                                  const SizedBox(height: 16),
                                  Text(
                                    c.syncing
                                        ? '正在同步…'
                                        : filter == 'all'
                                        ? (c.error == null
                                              ? '收件箱很清爽'
                                              : '暂未取得消息')
                                        : '还没有${filter == 'todo' ? '待办' : '图片'}',
                                    style: const TextStyle(
                                      fontSize: 18,
                                      fontWeight: FontWeight.w500,
                                    ),
                                  ),
                                  const SizedBox(height: 8),
                                  const Text(
                                    '记下一段文字、待办，或发送一张图片。',
                                    style: TextStyle(color: Colors.black54),
                                  ),
                                ],
                              ),
                            ),
                          for (final item in visible) _item(item),
                          if (c.items.length >= c.limit)
                            TextButton(
                              onPressed: c.loadMore,
                              child: const Text('加载更多'),
                            ),
                        ],
                      ),
                    ),
                  ),
                  Container(
                    decoration: const BoxDecoration(
                      color: Colors.white,
                      border: Border(top: BorderSide(color: Color(0xffe6eae7))),
                    ),
                    padding: const EdgeInsets.fromLTRB(16, 12, 16, 12),
                    child: Column(
                      mainAxisSize: MainAxisSize.min,
                      children: [
                        TextField(
                          controller: text,
                          enabled: !sending,
                          minLines: 1,
                          maxLines: 4,
                          maxLength: 4000,
                          onChanged: (value) => c.local.setMeta('draft', value),
                          decoration: InputDecoration(
                            hintText: kind == 'todo' ? '添加一件待办…' : '写点什么，留给自己…',
                            counterText: '',
                            filled: false,
                            border: InputBorder.none,
                            enabledBorder: InputBorder.none,
                            contentPadding: const EdgeInsets.symmetric(
                              horizontal: 8,
                              vertical: 6,
                            ),
                          ),
                        ),
                        Row(
                          children: [
                            PopupMenuButton<String>(
                              tooltip: '内容类型',
                              initialValue: kind,
                              onSelected: (value) =>
                                  setState(() => kind = value),
                              itemBuilder: (_) => [
                                const PopupMenuItem(
                                  value: 'text',
                                  child: Text('文本'),
                                ),
                                const PopupMenuItem(
                                  value: 'todo',
                                  child: Text('待办'),
                                ),
                              ],
                              child: Padding(
                                padding: const EdgeInsets.all(8),
                                child: Row(
                                  children: [
                                    Icon(
                                      kind == 'todo'
                                          ? Icons.check_box_outlined
                                          : Icons.notes_rounded,
                                      size: 20,
                                    ),
                                    const SizedBox(width: 6),
                                    Text(kind == 'todo' ? '待办' : '文本'),
                                    const Icon(Icons.expand_more, size: 18),
                                  ],
                                ),
                              ),
                            ),
                            IconButton(
                              tooltip: '发送图片',
                              onPressed: sending ? null : pickImage,
                              icon: const Icon(Icons.image_outlined),
                            ),
                            const Spacer(),
                            FilledButton.icon(
                              onPressed: sending ? null : send,
                              icon: const Icon(
                                Icons.arrow_upward_rounded,
                                size: 18,
                              ),
                              label: Text(sending ? '保存中…' : '发送'),
                            ),
                          ],
                        ),
                      ],
                    ),
                  ),
                ],
              ),
            ),
          ),
        ),
      );
    },
  );

  Widget _pending(Json entry) {
    final op = jsonDecode(entry['payload']) as Json;
    final conflict = entry['state'] == 'conflict',
        failed = entry['state'] == 'failed';
    final state = conflict
        ? '冲突 · 草稿已保留'
        : failed
        ? message(entry['error'] ?? '')
        : entry['id'] == c.uploadingID
        ? '正在上传图片'
        : '待发送';
    return Container(
      margin: const EdgeInsets.only(bottom: 10),
      padding: const EdgeInsets.all(14),
      decoration: BoxDecoration(
        color: const Color(0xfffff6e3),
        borderRadius: BorderRadius.circular(16),
      ),
      child: Column(
        crossAxisAlignment: CrossAxisAlignment.start,
        children: [
          Row(
            children: [
              const Icon(Icons.schedule_rounded, size: 17),
              const SizedBox(width: 7),
              Expanded(
                child: Text(
                  state,
                  style: const TextStyle(
                    fontSize: 13,
                    fontWeight: FontWeight.w600,
                  ),
                ),
              ),
              if (conflict)
                TextButton(
                  onPressed: () => resolve(entry),
                  child: const Text('处理'),
                ),
              if (failed) ...[
                TextButton(
                  onPressed: () => c.retry(entry),
                  child: const Text('重试'),
                ),
                IconButton(
                  tooltip: '移除失败草稿',
                  onPressed: () => c.discard(entry),
                  icon: const Icon(Icons.close, size: 18),
                ),
              ],
            ],
          ),
          Text(
            op['body']?.isNotEmpty == true
                ? op['body']
                : op['type'] == 'delete'
                ? '删除内容'
                : op['type'] == 'set_completed'
                ? (op['completed'] == true ? '完成待办' : '撤销完成')
                : '图片',
            maxLines: 4,
            overflow: TextOverflow.ellipsis,
          ),
        ],
      ),
    );
  }

  Widget _item(Json item) {
    final todo = item['kind'] == 'todo',
        completed = item['completed'] == true,
        busy = c.isPending(item['id']);
    final stamp = DateTime.tryParse(item['created_at'])?.toLocal();
    final time = stamp == null
        ? ''
        : '${stamp.month}月${stamp.day}日 · ${stamp.hour.toString().padLeft(2, '0')}:${stamp.minute.toString().padLeft(2, '0')}';
    return Container(
      margin: const EdgeInsets.only(bottom: 10),
      decoration: BoxDecoration(
        color: Colors.white,
        borderRadius: BorderRadius.circular(16),
        border: Border.all(color: const Color(0xffe6eae7)),
      ),
      child: Padding(
        padding: const EdgeInsets.fromLTRB(14, 8, 8, 14),
        child: Column(
          crossAxisAlignment: CrossAxisAlignment.start,
          children: [
            Row(
              children: [
                Icon(
                  todo
                      ? Icons.checklist_rounded
                      : item['kind'] == 'image'
                      ? Icons.image_outlined
                      : Icons.notes_rounded,
                  size: 17,
                  color: Colors.black45,
                ),
                const SizedBox(width: 8),
                Expanded(
                  child: Text(
                    '$time  ·  已保存',
                    style: const TextStyle(color: Colors.black54, fontSize: 12),
                  ),
                ),
                PopupMenuButton<String>(
                  tooltip: '更多操作',
                  onSelected: (action) {
                    if (action == 'copy') copy(item['body']);
                    if (action == 'edit') edit(item);
                    if (action == 'delete') remove(item);
                  },
                  itemBuilder: (_) => [
                    if ((item['body'] as String).isNotEmpty)
                      const PopupMenuItem(value: 'copy', child: Text('复制')),
                    PopupMenuItem(
                      value: 'edit',
                      enabled: !busy,
                      child: const Text('编辑'),
                    ),
                    PopupMenuItem(
                      value: 'delete',
                      enabled: !busy,
                      child: const Text('删除'),
                    ),
                  ],
                ),
              ],
            ),
            if (item['kind'] == 'image')
              Padding(
                padding: const EdgeInsets.only(right: 6, bottom: 8),
                child: GestureDetector(
                  onTap: () => showImage(item['attachment_id']),
                  child: ClipRRect(
                    borderRadius: BorderRadius.circular(10),
                    child: SizedBox(
                      height: 160,
                      width: double.infinity,
                      child: AttachmentImage(
                        controller: c,
                        id: item['attachment_id'],
                      ),
                    ),
                  ),
                ),
              ),
            if ((item['body'] as String).isNotEmpty)
              Row(
                crossAxisAlignment: CrossAxisAlignment.start,
                children: [
                  if (todo)
                    SizedBox(
                      width: 32,
                      height: 28,
                      child: Checkbox(
                        value: completed,
                        onChanged: busy
                            ? null
                            : (value) => c.submit(
                                'set_completed',
                                item: item,
                                completed: value,
                              ),
                        semanticLabel: completed ? '撤销完成' : '完成待办',
                      ),
                    ),
                  Expanded(
                    child: Padding(
                      padding: const EdgeInsets.only(right: 8),
                      child: SelectableText(
                        item['body'],
                        style: TextStyle(
                          fontSize: 16,
                          height: 1.5,
                          color: completed
                              ? Colors.black45
                              : const Color(0xff26352f),
                          decoration: completed
                              ? TextDecoration.lineThrough
                              : null,
                        ),
                      ),
                    ),
                  ),
                ],
              ),
          ],
        ),
      ),
    );
  }
}

class StatusSummary extends StatelessWidget {
  final Json? view;
  final bool online;
  const StatusSummary({super.key, required this.view, required this.online});
  @override
  Widget build(BuildContext context) {
    final usage = view?['usage'] as Json?, codex = view?['codex'] as Json?;
    final expiry = DateTime.tryParse(view?['freshUntil'] ?? '');
    final stale =
        !online ||
        expiry == null ||
        expiry.isBefore(DateTime.now()) ||
        view?['freshness'] != 'FRESHNESS_FRESH';
    final cost = int.tryParse('${usage?['actualCostMicros']}');
    final tokens = int.tryParse('${usage?['tokenCount']}');
    final sessions = (codex?['sessions'] as List?) ?? [];
    return Container(
      margin: const EdgeInsets.fromLTRB(20, 6, 20, 6),
      child: Material(
        color: const Color(0xffeef3ef),
        borderRadius: BorderRadius.circular(18),
        child: Theme(
          data: Theme.of(context).copyWith(dividerColor: Colors.transparent),
          child: ExpansionTile(
            tilePadding: const EdgeInsets.symmetric(horizontal: 16),
            childrenPadding: const EdgeInsets.fromLTRB(16, 0, 16, 12),
            title: Row(
              children: [
                Expanded(
                  child: _metric(
                    '周期用量',
                    cost == null
                        ? '—'
                        : '\$${(cost / 1000000).toStringAsFixed(2)}',
                  ),
                ),
                Expanded(
                  child: _metric(
                    'Tokens',
                    tokens == null
                        ? '—'
                        : tokens >= 1000000
                        ? '${(tokens / 1000000).toStringAsFixed(1)}M'
                        : tokens >= 1000
                        ? '${(tokens / 1000).toStringAsFixed(1)}K'
                        : '$tokens',
                  ),
                ),
                Expanded(
                  child: _metric('运行会话', '${codex?['runningCount'] ?? '—'}'),
                ),
              ],
            ),
            subtitle: Padding(
              padding: const EdgeInsets.only(top: 8, bottom: 2),
              child: Text(
                !online
                    ? '离线 · 显示本机缓存'
                    : view == null
                    ? '等待状态更新'
                    : stale
                    ? '状态已过期 · 等待更新'
                    : '状态已更新',
                style: TextStyle(
                  fontSize: 12,
                  color: stale
                      ? const Color(0xff876c43)
                      : const Color(0xff35705d),
                ),
              ),
            ),
            children: [
              ConstrainedBox(
                constraints: const BoxConstraints(maxHeight: 160),
                child: sessions.isEmpty
                    ? const Align(
                        alignment: Alignment.centerLeft,
                        child: Padding(
                          padding: EdgeInsets.only(top: 8),
                          child: Text(
                            '暂无会话',
                            style: TextStyle(color: Colors.black54),
                          ),
                        ),
                      )
                    : ListView(
                        shrinkWrap: true,
                        children: sessions
                            .map(
                              (s) => Padding(
                                padding: const EdgeInsets.symmetric(
                                  vertical: 7,
                                ),
                                child: Row(
                                  children: [
                                    Icon(
                                      s['processAlive'] == true
                                          ? Icons.circle
                                          : Icons.circle_outlined,
                                      size: 9,
                                      color: const Color(0xff508d72),
                                    ),
                                    const SizedBox(width: 10),
                                    Expanded(
                                      child: Text(
                                        s['displayName']?.isNotEmpty == true
                                            ? s['displayName']
                                            : s['projectName']?.isNotEmpty ==
                                                  true
                                            ? s['projectName']
                                            : 'Codex 会话',
                                      ),
                                    ),
                                    const SizedBox(width: 8),
                                    Text(
                                      s['processAlive'] == true ? '运行中' : '已结束',
                                      style: const TextStyle(
                                        fontSize: 12,
                                        color: Colors.black54,
                                      ),
                                    ),
                                  ],
                                ),
                              ),
                            )
                            .toList(),
                      ),
              ),
            ],
          ),
        ),
      ),
    );
  }

  Widget _metric(String label, String value) => Column(
    crossAxisAlignment: CrossAxisAlignment.start,
    children: [
      Text(label, style: const TextStyle(fontSize: 12, color: Colors.black54)),
      const SizedBox(height: 5),
      Text(
        value,
        style: const TextStyle(
          fontSize: 21,
          fontWeight: FontWeight.w600,
          letterSpacing: -.5,
        ),
      ),
    ],
  );
}

class AttachmentImage extends StatefulWidget {
  final InboxController controller;
  final String id;
  final bool full;
  const AttachmentImage({
    super.key,
    required this.controller,
    required this.id,
    this.full = false,
  });
  @override
  State<AttachmentImage> createState() => _AttachmentImageState();
}

class _AttachmentImageState extends State<AttachmentImage> {
  late Future<File> image = widget.controller.image(
    widget.id,
    full: widget.full,
  );
  @override
  void didUpdateWidget(AttachmentImage oldWidget) {
    super.didUpdateWidget(oldWidget);
    if (oldWidget.id != widget.id ||
        oldWidget.full != widget.full ||
        oldWidget.controller != widget.controller) {
      image = widget.controller.image(widget.id, full: widget.full);
    }
  }

  @override
  Widget build(BuildContext context) => FutureBuilder<File>(
    future: image,
    builder: (context, snapshot) {
      if (snapshot.hasError) {
        return Center(
          child: TextButton.icon(
            onPressed: () => setState(
              () =>
                  image = widget.controller.image(widget.id, full: widget.full),
            ),
            icon: const Icon(Icons.refresh),
            label: const Text('加载图片'),
          ),
        );
      }
      if (!snapshot.hasData) {
        return const Center(child: CircularProgressIndicator(strokeWidth: 2));
      }
      final child = Image.file(
        snapshot.data!,
        fit: widget.full ? BoxFit.contain : BoxFit.cover,
        errorBuilder: (_, _, _) => const Center(child: Text('无法显示图片')),
      );
      return widget.full ? InteractiveViewer(child: child) : child;
    },
  );
}
