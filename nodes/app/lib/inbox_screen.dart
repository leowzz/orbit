import 'dart:async';
import 'dart:convert';
import 'dart:io';
import 'package:flutter/material.dart';
import 'package:flutter/rendering.dart' show ScrollDirection;
import 'package:flutter/services.dart';
import 'package:image_picker/image_picker.dart';
import 'package:url_launcher/url_launcher.dart';
import 'inbox_controller.dart';
import 'local_store.dart';

const _background = Color(0xfff6f8f5);
const _ink = Color(0xff263b33);
const _accent = Color(0xff287663);
const _muted = Color(0xff738078);
const _border = Color(0xffdfe6df);
const _composerTodoKey = ValueKey('composer-todo');

class InboxReadingNotification extends Notification {
  final bool reading;
  const InboxReadingNotification(this.reading);
}

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
  final searchText = TextEditingController();
  final composerBounds = GlobalKey();
  Timer? searchTimer;
  XFile? attachment;
  String kind = 'text', filter = 'all';
  bool sending = false, composerReady = false, reading = false;
  InboxController get c => widget.controller;

  void setReading(bool value) {
    if (reading == value) return;
    if (value) FocusScope.of(context).unfocus();
    setState(() => reading = value);
    InboxReadingNotification(value).dispatch(context);
  }

  bool onListScroll(UserScrollNotification notification) {
    if (notification.depth != 0) return false;
    if (notification.direction == ScrollDirection.forward) {
      setReading(false);
    } else if (notification.direction == ScrollDirection.reverse &&
        notification.metrics.maxScrollExtent > 0) {
      setReading(true);
    }
    return false;
  }

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
      attachment = null;
      composerReady = false;
      _draft();
    }
  }

  Future<void> _draft() async {
    final owner = c;
    final draft = await owner.local.meta('draft');
    final savedKind = await owner.local.meta('composer_kind');
    if (!mounted || c != owner) return;
    if (text.text.isEmpty) text.text = draft ?? '';
    setState(() {
      kind = savedKind == 'todo' ? 'todo' : 'text';
      composerReady = true;
    });
    if (Platform.isAndroid) {
      try {
        final lost = await ImagePicker().retrieveLostData();
        if (mounted && lost.files?.isNotEmpty == true) {
          await attachImage(lost.files!.first);
        }
      } catch (_) {
        notice('未能恢复所选图片，请重新选择');
      }
    }
  }

  Future<void> setComposerKind(bool todo) async {
    final owner = c;
    final value = todo ? 'todo' : 'text';
    try {
      await owner.local.setMeta('composer_kind', value);
      if (mounted && c == owner) setState(() => kind = value);
    } catch (_) {
      notice('未能保存待办设置，请重试');
    }
  }

  Future<void> toggleKind(Json item) async {
    try {
      await c.submit(
        'set_kind',
        item: item,
        kind: item['kind'] == 'todo' ? 'text' : 'todo',
      );
    } catch (_) {
      notice('未能保存修改，请重试');
    }
  }

  @override
  void dispose() {
    searchTimer?.cancel();
    searchText.dispose();
    text.dispose();
    super.dispose();
  }

  void notice(String message) {
    if (mounted) {
      final bounds = composerBounds.currentContext?.findRenderObject();
      ScaffoldMessenger.of(context)
        ..clearSnackBars()
        ..showSnackBar(
          SnackBar(
            content: Text(message),
            behavior: SnackBarBehavior.floating,
            margin: EdgeInsets.fromLTRB(
              16,
              0,
              16,
              !reading && bounds is RenderBox ? bounds.size.height + 48 : 16,
            ),
            duration: const Duration(seconds: 2),
          ),
        );
    }
  }

  Future<void> copy(String body) async {
    await Clipboard.setData(ClipboardData(text: body));
    notice('已复制');
  }

  Future<void> send() async {
    final photo = attachment?.path;
    if (!composerReady ||
        sending ||
        (photo == null && text.text.trim().isEmpty)) {
      return;
    }
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
        setState(() => attachment = null);
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
      await attachImage(photo);
    } catch (_) {
      notice('无法读取图片，请重新选择');
    }
  }

  Future<void> attachImage(XFile photo) async {
    if (await photo.length() > 10 * 1024 * 1024) {
      notice('图片需小于 10 MB');
      return;
    }
    if (mounted) {
      setReading(false);
      setState(() => attachment = photo);
    }
  }

  AlertDialog _dialog({
    required Widget title,
    required Widget content,
    required List<Widget> actions,
  }) => AlertDialog(
    title: title,
    content: content,
    actions: actions,
    scrollable: true,
    backgroundColor: Colors.white,
    surfaceTintColor: Colors.transparent,
    shape: RoundedRectangleBorder(
      borderRadius: BorderRadius.circular(16),
      side: const BorderSide(color: _border),
    ),
    titleTextStyle: const TextStyle(
      color: _ink,
      fontSize: 20,
      fontWeight: FontWeight.w600,
    ),
  );

  Future<String?> editText(String initial, String title) async {
    final editor = TextEditingController(text: initial);
    final value = await showDialog<String>(
      context: context,
      builder: (context) => _dialog(
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
    final value = await editText(item['body'], '编辑消息');
    if (value == null || (value.isEmpty && item['kind'] != 'image')) return;
    await c.submit('update', item: item, body: value);
  }

  Future<void> remove(Json item) async {
    final confirmed = await showDialog<bool>(
      context: context,
      builder: (context) => _dialog(
        title: const Text('删除这条内容？'),
        content: const Text('删除后会从所有设备隐藏，可在控制台的「已删除」中恢复。'),
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
      builder: (context) => _dialog(
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
                          : op['type'] == 'set_kind'
                          ? (op['kind'] == 'todo' ? '设为待办' : '改为文本')
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
      final visible = c.items;
      final drafts = c.pending
          .where(
            (entry) =>
                entry['state'] == 'conflict' || entry['state'] == 'failed',
          )
          .toList();
      final empty = visible.isEmpty && drafts.isEmpty;
      final rowIndexes = <Key, int>{
        for (var i = 0; i < drafts.length; i++)
          ValueKey<String>(drafts[i]['id']): i,
        for (var i = 0; i < visible.length; i++)
          ValueKey<String>(visible[i]['id']): drafts.length + i,
      };
      final wide = MediaQuery.sizeOf(context).width > 600;
      final theme = Theme.of(context);
      final shape = RoundedRectangleBorder(
        borderRadius: BorderRadius.circular(9),
      );
      return Theme(
        data: theme.copyWith(
          colorScheme: theme.colorScheme.copyWith(
            primary: _accent,
            onPrimary: Colors.white,
            surface: Colors.white,
            onSurface: _ink,
            onSurfaceVariant: _muted,
            outline: _border,
          ),
          textTheme: theme.textTheme.apply(bodyColor: _ink, displayColor: _ink),
          scaffoldBackgroundColor: _background,
          appBarTheme: theme.appBarTheme.copyWith(
            backgroundColor: _background,
            foregroundColor: _ink,
            surfaceTintColor: Colors.transparent,
            elevation: 0,
            centerTitle: false,
            shape: const Border(bottom: BorderSide(color: _border)),
          ),
          filledButtonTheme: FilledButtonThemeData(
            style: FilledButton.styleFrom(
              backgroundColor: _accent,
              foregroundColor: Colors.white,
              shape: shape,
              minimumSize: const Size(64, 40),
            ),
          ),
          outlinedButtonTheme: OutlinedButtonThemeData(
            style: OutlinedButton.styleFrom(
              foregroundColor: _ink,
              backgroundColor: Colors.white,
              side: const BorderSide(color: _border),
              shape: shape,
              minimumSize: const Size(0, 40),
            ),
          ),
          textButtonTheme: TextButtonThemeData(
            style: TextButton.styleFrom(foregroundColor: _accent, shape: shape),
          ),
          checkboxTheme: CheckboxThemeData(
            shape: RoundedRectangleBorder(
              borderRadius: BorderRadius.circular(3),
            ),
            side: const BorderSide(color: _muted, width: 1.5),
          ),
          chipTheme: theme.chipTheme.copyWith(
            backgroundColor: Colors.white,
            side: const BorderSide(color: _border),
            shape: RoundedRectangleBorder(
              borderRadius: BorderRadius.circular(8),
            ),
            labelStyle: theme.textTheme.bodyMedium?.copyWith(
              color: _accent,
              fontSize: 14,
            ),
          ),
        ),
        child: Scaffold(
          appBar: AppBar(
            toolbarHeight: wide ? 76 : 66,
            titleSpacing: wide ? 24 : 16,
            title: Row(
              mainAxisSize: MainAxisSize.min,
              children: [
                const Icon(Icons.radio_button_checked, size: 22),
                const SizedBox(width: 6),
                const Text(
                  'Orbit',
                  style: TextStyle(fontSize: 20, fontWeight: FontWeight.w700),
                ),
                Container(
                  height: 20,
                  margin: const EdgeInsets.symmetric(horizontal: 12),
                  decoration: const BoxDecoration(
                    border: Border(left: BorderSide(color: _border)),
                  ),
                ),
                const Flexible(
                  child: Text(
                    '收件箱',
                    style: TextStyle(fontSize: 17, fontWeight: FontWeight.w600),
                  ),
                ),
              ],
            ),
            actions: [
              if (wide)
                Text(
                  c.syncing
                      ? '正在同步…'
                      : c.online
                      ? '已同步'
                      : '等待同步',
                  style: const TextStyle(color: _muted, fontSize: 13),
                ),
              IconButton(
                tooltip: '同步',
                onPressed: c.syncing ? null : c.sync,
                icon: c.syncing
                    ? const SizedBox(
                        width: 20,
                        height: 20,
                        child: CircularProgressIndicator(strokeWidth: 2),
                      )
                    : const Icon(Icons.sync_rounded, size: 22),
              ),
              IconButton(
                tooltip: '连接设置',
                onPressed: widget.onSettings,
                icon: const Icon(Icons.settings_outlined, size: 22),
              ),
              const SizedBox(width: 8),
            ],
          ),
          body: SafeArea(
            top: false,
            child: Center(
              child: ConstrainedBox(
                constraints: const BoxConstraints(maxWidth: 820),
                child: Column(
                  children: [
                    Padding(
                      padding: EdgeInsets.fromLTRB(
                        wide ? 24 : 14,
                        wide ? 24 : 18,
                        wide ? 24 : 14,
                        14,
                      ),
                      child: _toolbar(),
                    ),
                    if (c.error != null)
                      Container(
                        margin: EdgeInsets.fromLTRB(
                          wide ? 24 : 14,
                          0,
                          wide ? 24 : 14,
                          10,
                        ),
                        padding: const EdgeInsets.fromLTRB(12, 4, 4, 4),
                        decoration: BoxDecoration(
                          color: const Color(0xfffff2d8),
                          border: Border.all(color: const Color(0xffebd29e)),
                          borderRadius: BorderRadius.circular(10),
                        ),
                        child: Row(
                          children: [
                            Expanded(
                              child: Text(
                                c.error!,
                                style: const TextStyle(
                                  fontSize: 14,
                                  height: 1.6,
                                ),
                              ),
                            ),
                            TextButton(
                              onPressed: c.syncing ? null : c.sync,
                              child: const Text('重试'),
                            ),
                          ],
                        ),
                      ),
                    Expanded(
                      child: NotificationListener<UserScrollNotification>(
                        onNotification: onListScroll,
                        child: RefreshIndicator(
                          onRefresh: c.sync,
                          child: ListView.builder(
                            key: const PageStorageKey('inbox'),
                            physics: const AlwaysScrollableScrollPhysics(),
                            padding: EdgeInsets.fromLTRB(
                              wide ? 24 : 14,
                              0,
                              wide ? 24 : 14,
                              16,
                            ),
                            itemCount:
                                drafts.length +
                                visible.length +
                                (empty ? 1 : 0) +
                                (c.hasMore ? 1 : 0),
                            findChildIndexCallback: (key) => rowIndexes[key],
                            itemBuilder: (context, index) {
                              if (index < drafts.length) {
                                return _failedDraft(drafts[index]);
                              }
                              index -= drafts.length;
                              if (index < visible.length) {
                                final day = _messageDate(visible[index]);
                                return _item(
                                  visible[index],
                                  heading:
                                      index == 0 ||
                                          day !=
                                              _messageDate(visible[index - 1])
                                      ? day
                                      : null,
                                );
                              }
                              if (empty && index == 0) {
                                return Padding(
                                  padding: const EdgeInsets.symmetric(
                                    vertical: 60,
                                    horizontal: 16,
                                  ),
                                  child: Column(
                                    children: [
                                      Text(
                                        c.syncing
                                            ? '正在同步…'
                                            : searchText.text.trim().isNotEmpty
                                            ? '没有找到匹配的消息'
                                            : filter == 'all'
                                            ? (c.error == null
                                                  ? '留给下一次打开的自己'
                                                  : '暂未取得消息')
                                            : '还没有${{'todo': '待办', 'image': '图片', 'links': '链接', 'completed': '已完成待办'}[filter] ?? '消息'}',
                                        textAlign: TextAlign.center,
                                        style: const TextStyle(
                                          fontSize: 20,
                                          fontWeight: FontWeight.w500,
                                          color: Color(0xff456454),
                                        ),
                                      ),
                                      const SizedBox(height: 8),
                                      Text(
                                        searchText.text.trim().isNotEmpty ||
                                                filter != 'all'
                                            ? '试试其他关键词或筛选。'
                                            : '随手存下链接、文字和图片，在自己的设备间接着看。',
                                        textAlign: TextAlign.center,
                                        style: const TextStyle(
                                          color: _muted,
                                          height: 1.8,
                                        ),
                                      ),
                                    ],
                                  ),
                                );
                              }
                              return TextButton(
                                onPressed: c.loadMore,
                                child: const Text('加载更多'),
                              );
                            },
                          ),
                        ),
                      ),
                    ),
                    AnimatedSize(
                      duration: const Duration(milliseconds: 200),
                      alignment: Alignment.bottomCenter,
                      child: Visibility(
                        visible: !reading,
                        maintainState: true,
                        child: Padding(
                          padding: EdgeInsets.fromLTRB(
                            wide ? 24 : 12,
                            10,
                            wide ? 24 : 12,
                            12,
                          ),
                          child: _composer(wide),
                        ),
                      ),
                    ),
                  ],
                ),
              ),
            ),
          ),
        ),
      );
    },
  );

  Widget _toolbar() => LayoutBuilder(
    builder: (context, constraints) {
      final search = TextField(
        controller: searchText,
        decoration: InputDecoration(
          hintText: '搜索文字、链接…',
          hintStyle: const TextStyle(color: _muted, fontSize: 14),
          prefixIcon: const Icon(Icons.search, color: _muted, size: 22),
          suffixIcon: searchText.text.isEmpty
              ? null
              : IconButton(
                  tooltip: '清除搜索',
                  icon: const Icon(Icons.close, size: 18),
                  onPressed: () {
                    searchTimer?.cancel();
                    searchText.clear();
                    setState(() {});
                    setReading(false);
                    c.search('', filter);
                  },
                ),
          filled: true,
          fillColor: Colors.white,
          contentPadding: const EdgeInsets.symmetric(
            horizontal: 12,
            vertical: 12,
          ),
          border: OutlineInputBorder(
            borderRadius: BorderRadius.circular(10),
            borderSide: const BorderSide(color: _border),
          ),
          enabledBorder: OutlineInputBorder(
            borderRadius: BorderRadius.circular(10),
            borderSide: const BorderSide(color: _border),
          ),
        ),
        onChanged: (value) {
          setReading(false);
          setState(() {});
          searchTimer?.cancel();
          searchTimer = Timer(
            const Duration(milliseconds: 150),
            () => c.search(value, filter),
          );
        },
      );
      final selection = Container(
        padding: const EdgeInsets.symmetric(horizontal: 10),
        decoration: BoxDecoration(
          color: Colors.white,
          border: Border.all(color: _border),
          borderRadius: BorderRadius.circular(10),
        ),
        child: DropdownButtonHideUnderline(
          child: DropdownButton<String>(
            value: filter,
            isExpanded: true,
            icon: const Icon(Icons.expand_more_rounded, size: 20),
            borderRadius: BorderRadius.circular(10),
            style: Theme.of(
              context,
            ).textTheme.bodyMedium?.copyWith(color: _ink, fontSize: 14),
            items: const [
              DropdownMenuItem(value: 'all', child: Text('全部消息')),
              DropdownMenuItem(value: 'links', child: Text('链接')),
              DropdownMenuItem(value: 'todo', child: Text('未完成待办')),
              DropdownMenuItem(value: 'image', child: Text('图片')),
              DropdownMenuItem(value: 'completed', child: Text('已完成')),
            ],
            onChanged: (value) {
              if (value == null) return;
              searchTimer?.cancel();
              setState(() => filter = value);
              c.search(searchText.text, filter);
              setReading(false);
            },
          ),
        ),
      );
      if (constraints.maxWidth < 400 &&
          MediaQuery.textScalerOf(context).scale(14) > 18) {
        return Column(children: [search, const SizedBox(height: 8), selection]);
      }
      return Row(
        children: [
          Expanded(child: search),
          const SizedBox(width: 8),
          SizedBox(width: 130, child: selection),
        ],
      );
    },
  );

  Widget _composer(bool wide) => Container(
    key: const ValueKey('composer'),
    padding: const EdgeInsets.symmetric(horizontal: 14, vertical: 12),
    decoration: BoxDecoration(
      color: Colors.white,
      border: Border.all(color: const Color(0xffccdacf)),
      borderRadius: BorderRadius.circular(15),
      boxShadow: const [
        BoxShadow(
          color: Color(0x0a203b25),
          blurRadius: 24,
          offset: Offset(0, 5),
        ),
      ],
    ),
    child: Column(
      key: composerBounds,
      mainAxisSize: MainAxisSize.min,
      children: [
        CallbackShortcuts(
          bindings: {
            const SingleActivator(LogicalKeyboardKey.enter, control: true):
                send,
            const SingleActivator(LogicalKeyboardKey.enter, meta: true): send,
          },
          child: TextField(
            key: const ValueKey('composer-body'),
            controller: text,
            enabled: !sending,
            minLines: 2,
            maxLines: 5,
            maxLength: 4000,
            style: const TextStyle(fontSize: 16, height: 1.6),
            onChanged: (value) => c.local.setMeta('draft', value),
            decoration: const InputDecoration(
              hintText: '写点什么，留给自己…',
              hintStyle: TextStyle(color: _muted),
              counterText: '',
              filled: false,
              border: InputBorder.none,
              enabledBorder: InputBorder.none,
              focusedBorder: InputBorder.none,
              disabledBorder: InputBorder.none,
              contentPadding: EdgeInsets.symmetric(horizontal: 4, vertical: 2),
            ),
          ),
        ),
        if (attachment != null)
          Padding(
            padding: const EdgeInsets.only(top: 4),
            child: Row(
              children: [
                const Icon(Icons.image_outlined, size: 18, color: _accent),
                const SizedBox(width: 8),
                Expanded(
                  child: Text(
                    attachment!.name,
                    style: const TextStyle(fontSize: 13, color: _accent),
                  ),
                ),
                TextButton(
                  onPressed: sending
                      ? null
                      : () => setState(() => attachment = null),
                  child: const Text('移除图片'),
                ),
              ],
            ),
          ),
        const SizedBox(height: 8),
        Row(
          children: [
            Expanded(
              child: Wrap(
                spacing: 8,
                runSpacing: 8,
                crossAxisAlignment: WrapCrossAlignment.center,
                children: [
                  Row(
                    mainAxisSize: MainAxisSize.min,
                    children: [
                      SizedBox(
                        width: 28,
                        height: 40,
                        child: Checkbox(
                          key: _composerTodoKey,
                          value: kind == 'todo',
                          semanticLabel: '设为待办',
                          onChanged:
                              sending || !composerReady || attachment != null
                              ? null
                              : (value) => setComposerKind(value == true),
                        ),
                      ),
                      GestureDetector(
                        onTap: sending || !composerReady || attachment != null
                            ? null
                            : () => setComposerKind(kind != 'todo'),
                        child: const Text(
                          '设为待办',
                          style: TextStyle(fontSize: 14),
                        ),
                      ),
                    ],
                  ),
                  OutlinedButton(
                    onPressed: sending || !composerReady ? null : pickImage,
                    style: OutlinedButton.styleFrom(
                      padding: const EdgeInsets.symmetric(horizontal: 10),
                    ),
                    child: const Text('添加图片', style: TextStyle(fontSize: 14)),
                  ),
                  if (wide)
                    const Padding(
                      padding: EdgeInsets.symmetric(vertical: 12),
                      child: Text(
                        '⌘ / Ctrl + Enter 发送',
                        style: TextStyle(color: _muted, fontSize: 12),
                      ),
                    ),
                ],
              ),
            ),
            const SizedBox(width: 8),
            ValueListenableBuilder<TextEditingValue>(
              valueListenable: text,
              builder: (context, value, _) => FilledButton(
                onPressed:
                    sending ||
                        !composerReady ||
                        (value.text.trim().isEmpty && attachment == null)
                    ? null
                    : send,
                child: Text(sending ? '保存中…' : '发送'),
              ),
            ),
          ],
        ),
      ],
    ),
  );

  String _messageDate(Json item) {
    final stamp = DateTime.tryParse(item['created_at'])?.toLocal();
    return stamp == null
        ? '日期未知'
        : '${stamp.year}年${stamp.month}月${stamp.day}日';
  }

  Widget _failedDraft(Json entry) {
    final op = jsonDecode(entry['payload']) as Json;
    final conflict = entry['state'] == 'conflict',
        failed = entry['state'] == 'failed';
    final state = conflict ? '冲突 · 草稿已保留' : message(entry['error'] ?? '');
    return Container(
      key: ValueKey<String>(entry['id']),
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
              const Icon(Icons.error_outline_rounded, size: 17),
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
          InboxMessageBody(
            op['body']?.isNotEmpty == true
                ? op['body']
                : op['type'] == 'delete'
                ? '删除内容'
                : op['type'] == 'set_completed'
                ? (op['completed'] == true ? '完成待办' : '撤销完成')
                : op['type'] == 'set_kind'
                ? (op['kind'] == 'todo' ? '设为待办' : '改为文本')
                : '图片',
            style: const TextStyle(fontSize: 14),
          ),
        ],
      ),
    );
  }

  Widget _item(Json item, {String? heading}) {
    final entry = c.pending
        .where((p) => p['item_id'] == item['id'])
        .firstOrNull;
    final op = entry == null ? null : jsonDecode(entry['payload']) as Json;
    final waiting = entry != null && entry['state'] == 'pending';
    final changingKind = waiting && op?['type'] == 'set_kind';
    final todo = (changingKind ? op!['kind'] : item['kind']) == 'todo';
    final completed = changingKind
        ? false
        : waiting && op?['type'] == 'set_completed'
        ? op!['completed'] == true
        : item['completed'] == true;
    final busy = entry != null;
    final stamp = DateTime.tryParse(item['created_at'])?.toLocal();
    final time = stamp == null
        ? ''
        : '${stamp.hour.toString().padLeft(2, '0')}:${stamp.minute.toString().padLeft(2, '0')}';
    return Column(
      key: ValueKey<String>(item['id']),
      crossAxisAlignment: CrossAxisAlignment.start,
      children: [
        if (heading != null)
          Padding(
            padding: const EdgeInsets.fromLTRB(2, 12, 2, 10),
            child: Text(
              heading,
              style: const TextStyle(
                color: _muted,
                fontSize: 13,
                fontWeight: FontWeight.w500,
              ),
            ),
          ),
        Container(
          key: const ValueKey('message-card'),
          margin: const EdgeInsets.only(bottom: 10),
          padding: const EdgeInsets.fromLTRB(14, 8, 14, 14),
          decoration: BoxDecoration(
            color: Colors.white,
            borderRadius: BorderRadius.circular(14),
            border: Border.all(color: _border),
          ),
          child: Column(
            crossAxisAlignment: CrossAxisAlignment.start,
            children: [
              Row(
                children: [
                  Expanded(
                    child: Text(
                      '$time${waiting
                          ? ' · 待同步'
                          : busy
                          ? ' · 需要处理'
                          : ''}',
                      style: const TextStyle(color: _muted, fontSize: 13),
                    ),
                  ),
                  if ((item['body'] as String).isNotEmpty)
                    Tooltip(
                      message: '复制消息',
                      child: TextButton(
                        onPressed: () => copy(item['body']),
                        style: TextButton.styleFrom(
                          foregroundColor: _muted,
                          minimumSize: const Size(48, 40),
                          padding: const EdgeInsets.symmetric(horizontal: 8),
                        ),
                        child: const Text('复制', style: TextStyle(fontSize: 13)),
                      ),
                    ),
                  PopupMenuButton<String>(
                    tooltip: '更多操作',
                    icon: const Icon(Icons.more_horiz, color: _muted, size: 22),
                    padding: EdgeInsets.zero,
                    onSelected: (action) {
                      if (action == 'edit') edit(item);
                      if (action == 'delete') remove(item);
                      if (action == 'kind') toggleKind(item);
                    },
                    itemBuilder: (_) => [
                      PopupMenuItem(
                        value: 'edit',
                        enabled: !busy,
                        child: const Text('编辑'),
                      ),
                      if (item['kind'] == 'text' || item['kind'] == 'todo')
                        PopupMenuItem(
                          value: 'kind',
                          enabled: !busy,
                          child: Text(todo ? '改为文本' : '设为待办'),
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
              const SizedBox(height: 6),
              if (item['kind'] == 'image')
                Padding(
                  padding: const EdgeInsets.only(bottom: 10),
                  child: Semantics(
                    button: true,
                    label: '查看图片',
                    child: InkWell(
                      onTap: () {
                        if (item['attachment_id'] != null) {
                          showImage(item['attachment_id']);
                        }
                      },
                      child: ClipRRect(
                        borderRadius: BorderRadius.circular(9),
                        child: SizedBox(
                          height: 260,
                          width: double.infinity,
                          child: item['local_photo'] != null
                              ? Image.file(
                                  File(item['local_photo']),
                                  fit: BoxFit.contain,
                                  errorBuilder: (_, _, _) =>
                                      const Center(child: Text('本机图片不可用')),
                                )
                              : AttachmentImage(
                                  controller: c,
                                  id: item['attachment_id'],
                                ),
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
                      Padding(
                        padding: const EdgeInsets.only(right: 10),
                        child: SizedBox(
                          width: 22,
                          height:
                              MediaQuery.textScalerOf(context).scale(16) * 1.65,
                          child: Checkbox(
                            value: completed,
                            semanticLabel: completed ? '撤销完成' : '完成待办',
                            onChanged: busy
                                ? null
                                : (value) => c.submit(
                                    'set_completed',
                                    item: item,
                                    completed: value,
                                  ),
                          ),
                        ),
                      ),
                    Expanded(
                      child: InboxMessageBody(
                        item['body'],
                        style: TextStyle(
                          fontSize: 16,
                          height: 1.65,
                          color: completed ? _muted : _ink,
                        ),
                      ),
                    ),
                  ],
                ),
            ],
          ),
        ),
      ],
    );
  }
}

class InboxMessageBody extends StatefulWidget {
  final String body;
  final TextStyle style;
  const InboxMessageBody(this.body, {super.key, required this.style});

  @override
  State<InboxMessageBody> createState() => _InboxMessageBodyState();
}

class _InboxMessageBodyState extends State<InboxMessageBody> {
  bool expanded = false;
  // TextPainter reuses its paragraph when text, typography and width are unchanged.
  final painter = TextPainter(maxLines: 8);

  @override
  void dispose() {
    painter.dispose();
    super.dispose();
  }

  @override
  Widget build(BuildContext context) => LayoutBuilder(
    builder: (context, constraints) {
      final style = DefaultTextStyle.of(context).style.merge(widget.style);
      painter
        ..text = TextSpan(text: widget.body, style: style)
        ..textDirection = Directionality.of(context)
        ..textScaler = MediaQuery.textScalerOf(context)
        ..locale = Localizations.maybeLocaleOf(context)
        ..layout(maxWidth: constraints.maxWidth);
      final overflowing = painter.didExceedMaxLines;
      return Column(
        crossAxisAlignment: CrossAxisAlignment.start,
        children: [
          if (overflowing && !expanded)
            Text(
              widget.body,
              style: style,
              maxLines: 8,
              overflow: TextOverflow.ellipsis,
            )
          else
            SelectionArea(child: Text(widget.body, style: style)),
          if (messageLinks(widget.body).isNotEmpty)
            Padding(
              padding: const EdgeInsets.only(top: 8),
              child: Wrap(
                spacing: 8,
                runSpacing: 6,
                children: [
                  for (final link in messageLinks(widget.body))
                    ActionChip(
                      avatar: const Icon(Icons.open_in_new, size: 15),
                      label: Text(link.host),
                      tooltip: link.toString(),
                      onPressed: () async {
                        try {
                          if (await launchUrl(
                            link,
                            mode: LaunchMode.externalApplication,
                          )) {
                            return;
                          }
                        } catch (_) {}
                        if (context.mounted) {
                          ScaffoldMessenger.of(context).showSnackBar(
                            const SnackBar(content: Text('无法打开链接，请复制后在浏览器打开')),
                          );
                        }
                      },
                    ),
                ],
              ),
            ),
          if (overflowing)
            TextButton(
              onPressed: () => setState(() => expanded = !expanded),
              child: Text(expanded ? '收起' : '展开'),
            ),
        ],
      );
    },
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
        fit: BoxFit.contain,
        errorBuilder: (_, _, _) => const Center(child: Text('无法显示图片')),
      );
      return widget.full ? InteractiveViewer(child: child) : child;
    },
  );
}

List<Uri> messageLinks(String body) =>
    RegExp(r'https?://[^\s<>"，。！？、；）》」』]+', caseSensitive: false)
        .allMatches(body)
        .map((m) => Uri.tryParse(m.group(0)!))
        .whereType<Uri>()
        .where((u) => u.host.isNotEmpty && u.userInfo.isEmpty)
        .toSet()
        .toList();
