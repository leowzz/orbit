import 'package:flutter/material.dart';
import 'package:orbit_android/main.dart' show NodePage;
import 'inbox_screen.dart' show InboxReadingNotification;
import 'inbox_controller.dart';

/// One Android application for the shared inbox and existing desktop widgets.
class OrbitHome extends StatefulWidget {
  final Widget inbox;
  final InboxController? controller;
  final bool widgetsEnabled;
  const OrbitHome({
    super.key,
    this.controller,
    required this.inbox,
    required this.widgetsEnabled,
  });

  @override
  State<OrbitHome> createState() => _OrbitHomeState();
}

class _OrbitHomeState extends State<OrbitHome> {
  int selected = 0;
  bool reading = false;

  @override
  Widget build(BuildContext context) {
    if (!widget.widgetsEnabled) return widget.inbox;
    return Scaffold(
      body: NotificationListener<InboxReadingNotification>(
        onNotification: (notification) {
          if (selected == 0 && reading != notification.reading) {
            setState(() => reading = notification.reading);
          }
          return true;
        },
        child: IndexedStack(
          index: selected,
          children: [
            widget.inbox,
            if (widget.controller != null)
              _ConnectionStatus(controller: widget.controller!)
            else if (selected == 1)
              const NodePage()
            else
              const SizedBox.shrink(),
          ],
        ),
      ),
      bottomNavigationBar: AnimatedSize(
        duration: const Duration(milliseconds: 200),
        alignment: Alignment.bottomCenter,
        child: reading && selected == 0
            ? const SizedBox.shrink()
            : NavigationBar(
                selectedIndex: selected,
                onDestinationSelected: (value) => setState(() {
                  selected = value;
                  reading = false;
                }),
                destinations: const [
                  NavigationDestination(
                    icon: Icon(Icons.inbox_outlined),
                    selectedIcon: Icon(Icons.inbox),
                    label: '收件箱',
                  ),
                  NavigationDestination(
                    icon: Icon(Icons.widgets_outlined),
                    selectedIcon: Icon(Icons.widgets),
                    label: '状态与组件',
                  ),
                ],
              ),
      ),
    );
  }
}

class _ConnectionStatus extends StatelessWidget {
  final InboxController controller;
  const _ConnectionStatus({required this.controller});
  @override
  Widget build(BuildContext context) => AnimatedBuilder(
    animation: controller,
    builder: (context, _) {
      final view = controller.view;
      final usage = view?['usage'] as Map?;
      final codex = view?['codex'] as Map?;
      final until = DateTime.tryParse(view?['freshUntil'] ?? '');
      final fresh =
          controller.online && until != null && until.isAfter(DateTime.now());
      final cost = num.tryParse('${usage?['actualCostMicros']}');
      return Scaffold(
        appBar: AppBar(title: const Text('状态与组件')),
        body: Center(
          child: ConstrainedBox(
            constraints: const BoxConstraints(maxWidth: 760),
            child: ListView(
              padding: const EdgeInsets.all(24),
              children: [
                ListTile(
                  contentPadding: EdgeInsets.zero,
                  leading: Icon(
                    controller.online
                        ? Icons.cloud_done_outlined
                        : Icons.cloud_off_outlined,
                  ),
                  title: Text(controller.online ? '收件箱已连接' : '收件箱等待同步'),
                  subtitle: Text(Uri.parse(controller.server).host),
                  trailing: IconButton(
                    tooltip: '同步',
                    onPressed: controller.syncing ? null : controller.sync,
                    icon: const Icon(Icons.sync),
                  ),
                ),
                const Text(
                  '消息和状态共用当前连接，无需额外填写连接参数。',
                  style: TextStyle(height: 1.6),
                ),
                const SizedBox(height: 24),
                if (view != null) ...[
                  Text(
                    fresh ? '当前状态' : '上次同步的状态',
                    style: Theme.of(context).textTheme.titleMedium,
                  ),
                  const SizedBox(height: 12),
                  if (usage != null)
                    ListTile(
                      contentPadding: EdgeInsets.zero,
                      title: const Text('今日用量'),
                      subtitle: Text(
                        'Token ${usage['tokenCount'] ?? '—'} · TPM ${usage['tpm'] ?? '—'}',
                      ),
                      trailing: Text(
                        cost == null
                            ? '—'
                            : '${usage['currencyCode'] ?? ''} ${(cost / 1000000).toStringAsFixed(2)}',
                      ),
                    ),
                  if (codex != null)
                    ListTile(
                      contentPadding: EdgeInsets.zero,
                      title: const Text('Codex 任务'),
                      subtitle: Text(
                        '${codex['runningCount'] ?? 0} 个运行中 · 共 ${codex['totalCount'] ?? 0} 个',
                      ),
                    ),
                ] else
                  const Text(
                    '尚未设置状态摘要。不影响收件箱使用；需要时可在管理台为此设备选择状态来源。',
                    style: TextStyle(color: Colors.black54, height: 1.6),
                  ),
                const SizedBox(height: 32),
                const Divider(),
                ListTile(
                  contentPadding: EdgeInsets.zero,
                  leading: const Icon(Icons.widgets_outlined),
                  title: const Text('Android 桌面组件'),
                  subtitle: const Text('可选功能，沿用原有设备连接和后台同步设置。'),
                  trailing: const Icon(Icons.chevron_right),
                  onTap: () => Navigator.of(context).push(
                    MaterialPageRoute<void>(builder: (_) => const NodePage()),
                  ),
                ),
              ],
            ),
          ),
        ),
      );
    },
  );
}
