import 'package:flutter/material.dart';
import 'inbox_controller.dart';
import 'app_theme.dart';
import 'local_store.dart' show Json;

IconData navigationIcon(String? name) => switch (name) {
  'globe' => Icons.language_outlined,
  'folder' => Icons.folder_outlined,
  'star' => Icons.star_outline,
  _ => Icons.apps_outlined,
};
Widget shortcutIcon(Json item, InboxController controller, {double size = 30}) {
  final url = item['icon_url'] as String? ?? '';
  final fallback = Icon(
    navigationIcon(item['icon_name'] as String?),
    size: size,
  );
  if (url.isEmpty) return fallback;
  final internal = RegExp(
    r'^/api/v1/navigation/icons/[a-f0-9]{64}$',
  ).hasMatch(url);
  final parsed = Uri.tryParse(url);
  if (!internal &&
      (parsed == null || !['http', 'https'].contains(parsed.scheme))) {
    return fallback;
  }
  return Image.network(
    internal ? '${controller.server}$url' : url,
    headers: internal ? controller.headers : null,
    width: size,
    height: size,
    fit: BoxFit.contain,
    errorBuilder: (_, _, _) => fallback,
  );
}

String navigationMessage(Object error) {
  if (error is ApiError) {
    return switch (error.code) {
      'navigation_conflict' => '配置已在别处修改，请返回后重新打开设置',
      'shortcut_unavailable' => '该入口已在电脑上修改或移除，请重新选择',
      'agent_unavailable' => '未能连接服务，请连接后重试',
      'invalid_navigation' => '请检查名称、网址和图标地址',
      _ => '操作失败，请检查连接后重试',
    };
  }
  return '未能打开，请确认应用已安装或网址有效';
}

class WorkbenchNavigation extends StatefulWidget {
  final InboxController controller;
  const WorkbenchNavigation({super.key, required this.controller});
  @override
  State<WorkbenchNavigation> createState() => _WorkbenchNavigationState();
}

class _WorkbenchNavigationState extends State<WorkbenchNavigation> {
  final _opening = <String>{};
  Future<void> _open(Json item) async {
    final id = item['id'] as String;
    if (!_opening.add(id)) return;
    setState(() {});
    try {
      await widget.controller.openShortcut(id);
    } catch (error) {
      if (mounted) {
        ScaffoldMessenger.of(
          context,
        ).showSnackBar(SnackBar(content: Text(navigationMessage(error))));
      }
    } finally {
      _opening.remove(id);
      if (mounted) setState(() {});
    }
  }

  @override
  Widget build(BuildContext context) {
    final config = widget.controller.navigation;
    final items = (config['items'] as List? ?? []).cast<Json>();
    final titles = config['show_titles'] == true;
    final atTop = config['at_top'] == true;
    const height = 60.0;
    final itemWidth = titles ? 104.0 : height;
    return Container(
      key: const ValueKey('workbench-navigation'),
      height: height,
      decoration: BoxDecoration(
        color: OrbitColors.of(context).navigation,
        border: atTop
            ? Border(bottom: BorderSide(color: OrbitColors.of(context).border))
            : Border(top: BorderSide(color: OrbitColors.of(context).border)),
      ),
      child: LayoutBuilder(
        builder: (context, bounds) {
          if (items.isEmpty) {
            return const SizedBox.expand();
          }
          final rowWidth = items.length * itemWidth + (items.length - 1) * 8;
          return Center(
            child: SizedBox(
              width: rowWidth.clamp(0, bounds.maxWidth),
              height: height,
              child: ListView.separated(
                scrollDirection: Axis.horizontal,
                itemCount: items.length,
                separatorBuilder: (_, _) => const SizedBox(width: 8),
                itemBuilder: (context, index) {
                  final item = items[index];
                  final title = item['title'] as String;
                  return Tooltip(
                    message: title,
                    child: Semantics(
                      label: title,
                      button: true,
                      child: InkWell(
                        key: ValueKey('shortcut-${item['id']}'),
                        onTap: _opening.contains(item['id'])
                            ? null
                            : () => _open(item),
                        child: SizedBox(
                          width: itemWidth,
                          height: height,
                          child: Column(
                            mainAxisAlignment: MainAxisAlignment.center,
                            children: [
                              if (_opening.contains(item['id']))
                                SizedBox(
                                  width: titles ? 36 : 56,
                                  height: titles ? 36 : 56,
                                  child: const Padding(
                                    padding: EdgeInsets.all(10),
                                    child: CircularProgressIndicator(
                                      strokeWidth: 2,
                                    ),
                                  ),
                                )
                              else
                                shortcutIcon(
                                  item,
                                  widget.controller,
                                  size: titles ? 36 : 56,
                                ),
                              if (titles)
                                Text(
                                  title,
                                  maxLines: 1,
                                  overflow: TextOverflow.ellipsis,
                                  style: const TextStyle(fontSize: 12),
                                ),
                            ],
                          ),
                        ),
                      ),
                    ),
                  );
                },
              ),
            ),
          );
        },
      ),
    );
  }
}
