import 'execution_settings.dart';
import 'package:flutter/material.dart';
import 'package:flutter/services.dart';
import 'inbox_screen.dart' show InboxReadingNotification;
import 'inbox_controller.dart';
import 'input_method_tools.dart';
import 'matrix_screen.dart';
import 'app_viewport.dart';
import 'workbench_screen.dart';
import 'app_location.dart';
import 'app_appearance.dart';
import 'package:orbit_android/software_update.dart';

/// One Android application for the shared inbox and existing desktop widgets.
class OrbitHome extends StatefulWidget {
  final Widget inbox;
  final InboxController? controller;
  final bool widgetsEnabled;
  final VoidCallback? onSettings;
  final AppLocation initialLocation;
  final ValueChanged<AppLocation>? onLocationChanged;
  const OrbitHome({
    super.key,
    this.controller,
    this.onSettings,
    this.initialLocation = AppLocation.inbox,
    this.onLocationChanged,
    required this.inbox,
    required this.widgetsEnabled,
  });

  @override
  State<OrbitHome> createState() => _OrbitHomeState();
}

class _OrbitHomeState extends State<OrbitHome> {
  int selected = 0;
  bool reading = false;
  bool showingMatrix = false;
  bool restoreMatrixFullscreen = false;

  static const channel = MethodChannel('dev.orbit/node');
  @override
  void initState() {
    super.initState();
    selected = widget.initialLocation.tab;
    showingMatrix =
        widget.initialLocation == AppLocation.matrix ||
        widget.initialLocation == AppLocation.matrixFullscreen;
    restoreMatrixFullscreen =
        widget.initialLocation == AppLocation.matrixFullscreen;
    syncConnection();
    WidgetsBinding.instance.addPostFrameCallback((_) {
      if (!mounted) return;
      switch (widget.initialLocation) {
        case AppLocation.workbenchFullscreen:
          openWorkbench();
        default:
          break;
      }
    });
  }

  Future<void> openWorkbench() async {
    widget.onLocationChanged?.call(AppLocation.workbenchFullscreen);
    await Navigator.of(context).push<void>(
      MaterialPageRoute(
        builder: (_) =>
            WorkbenchScreen(controller: widget.controller, fullscreen: true),
      ),
    );
    if (mounted) widget.onLocationChanged?.call(AppLocation.workbench);
  }

  void openMatrix() {
    setState(() {
      showingMatrix = true;
      restoreMatrixFullscreen = false;
    });
    widget.onLocationChanged?.call(AppLocation.matrix);
  }

  void closeMatrix() {
    setState(() => showingMatrix = false);
    widget.onLocationChanged?.call(AppLocation.data);
  }

  @override
  void didUpdateWidget(OrbitHome oldWidget) {
    super.didUpdateWidget(oldWidget);
    if (oldWidget.controller != widget.controller) syncConnection();
  }

  Future<void> syncConnection() async {
    if (!widget.widgetsEnabled) return;
    try {
      final controller = widget.controller;
      await channel.invokeMethod<void>(
        'appConnection',
        controller == null
            ? null
            : {'server': controller.server, 'token': controller.token},
      );
    } on MissingPluginException {
      /* Non-Android previews have no native widgets. */
    } on PlatformException {
      if (mounted) {
        ScaffoldMessenger.of(
          context,
        ).showSnackBar(const SnackBar(content: Text('桌面组件连接未更新，请重新打开应用重试')));
      }
    }
  }

  @override
  Widget build(BuildContext context) {
    final compact = AppViewport.compactOf(context);
    final pages = IndexedStack(
      index: selected,
      children: [
        widget.inbox,
        WorkbenchScreen(
          controller: widget.controller,
          onEnterFullscreen: openWorkbench,
        ),
        showingMatrix
            ? MatrixScreen(
                controller: widget.controller,
                initialFullscreen: restoreMatrixFullscreen,
                onBack: closeMatrix,
                onFullscreenChanged: (value) => widget.onLocationChanged?.call(
                  value ? AppLocation.matrixFullscreen : AppLocation.matrix,
                ),
              )
            : _ConnectionStatus(
                controller: widget.controller,
                widgetsEnabled: widget.widgetsEnabled,
                onMatrix: openMatrix,
              ),
        _AppSettings(
          controller: widget.controller,
          onSettings: widget.onSettings,
        ),
      ],
    );
    void select(int value) {
      setState(() {
        selected = value;
        reading = false;
        showingMatrix = false;
      });
      widget.onLocationChanged?.call(AppLocation.fromTab(value));
    }

    return PopScope(
      canPop: !showingMatrix,
      onPopInvokedWithResult: (didPop, _) {
        if (!didPop && showingMatrix) closeMatrix();
      },
      child: Scaffold(
        body: NotificationListener<InboxReadingNotification>(
          onNotification: (notification) {
            if (selected == 0 && reading != notification.reading) {
              setState(() => reading = notification.reading);
            }
            return true;
          },
          child: Row(
            children: [
              if (compact)
                NavigationRail(
                  minWidth: 80,
                  scrollable: true,
                  labelType: NavigationRailLabelType.all,
                  selectedIndex: selected,
                  onDestinationSelected: select,
                  destinations: const [
                    NavigationRailDestination(
                      icon: Icon(Icons.inbox_outlined),
                      selectedIcon: Icon(Icons.inbox),
                      label: Text('收件箱'),
                    ),
                    NavigationRailDestination(
                      icon: Icon(Icons.dashboard_outlined),
                      selectedIcon: Icon(Icons.dashboard),
                      label: Text('工作台'),
                    ),
                    NavigationRailDestination(
                      icon: Icon(Icons.widgets_outlined),
                      selectedIcon: Icon(Icons.widgets),
                      label: Text('数据展示'),
                    ),
                    NavigationRailDestination(
                      icon: Icon(Icons.settings_outlined),
                      selectedIcon: Icon(Icons.settings),
                      label: Text('设置'),
                    ),
                  ],
                ),
              Expanded(child: pages),
            ],
          ),
        ),
        bottomNavigationBar: compact
            ? null
            : AnimatedSize(
                duration: const Duration(milliseconds: 200),
                alignment: Alignment.bottomCenter,
                child: reading && selected == 0
                    ? const SizedBox.shrink()
                    : NavigationBar(
                        selectedIndex: selected,
                        onDestinationSelected: select,
                        destinations: [
                          const NavigationDestination(
                            icon: Icon(Icons.inbox_outlined),
                            selectedIcon: Icon(Icons.inbox),
                            label: '收件箱',
                          ),
                          const NavigationDestination(
                            icon: Icon(Icons.dashboard_outlined),
                            selectedIcon: Icon(Icons.dashboard),
                            label: '工作台',
                          ),
                          const NavigationDestination(
                            icon: Icon(Icons.widgets_outlined),
                            selectedIcon: Icon(Icons.widgets),
                            label: '数据展示',
                          ),
                          const NavigationDestination(
                            icon: Icon(Icons.settings_outlined),
                            selectedIcon: Icon(Icons.settings),
                            label: '设置',
                          ),
                        ],
                      ),
              ),
      ),
    );
  }
}

class _AppSettings extends StatelessWidget {
  final InboxController? controller;
  final VoidCallback? onSettings;
  const _AppSettings({this.controller, this.onSettings});

  @override
  Widget build(BuildContext context) => Scaffold(
    appBar: AppBar(title: const Text('设置')),
    body: Center(
      child: ConstrainedBox(
        constraints: const BoxConstraints(maxWidth: 760),
        child: ListView(
          padding: const EdgeInsets.all(24),
          children: [
            if (AppAppearance.maybeOf(context) case final appearance?)
              SwitchListTile(
                contentPadding: EdgeInsets.zero,
                secondary: const Icon(Icons.dark_mode_outlined),
                title: const Text('暗色模式'),
                value: appearance.dark,
                onChanged: appearance.saving ? null : appearance.onChanged,
              ),
            if (controller != null)
              ListTile(
                contentPadding: EdgeInsets.zero,
                leading: const Icon(Icons.touch_app_outlined),
                title: const Text('本机快捷操作'),
                subtitle: const Text('设置允许其他设备打开的本机应用与网址'),
                trailing: const Icon(Icons.chevron_right),
                onTap: () => Navigator.of(context).push(
                  MaterialPageRoute<void>(
                    builder: (_) =>
                        ExecutionSettingsScreen(controller: controller!),
                  ),
                ),
              ),
            ListTile(
              contentPadding: EdgeInsets.zero,
              leading: const Icon(Icons.link),
              title: const Text('连接设置'),
              subtitle: Text(
                controller == null
                    ? '连接 Orbit 后可更换连接。'
                    : Uri.parse(controller!.server).host,
              ),
              trailing: const Icon(Icons.chevron_right),
              onTap: onSettings,
            ),
            const SoftwareUpdateCard(),
          ],
        ),
      ),
    ),
  );
}

class _ConnectionStatus extends StatelessWidget {
  final InboxController? controller;
  final bool widgetsEnabled;
  final VoidCallback onMatrix;
  const _ConnectionStatus({
    required this.controller,
    required this.widgetsEnabled,
    required this.onMatrix,
  });
  @override
  Widget build(BuildContext context) => AnimatedBuilder(
    animation: controller ?? const AlwaysStoppedAnimation(0),
    builder: (context, _) {
      final view = controller?.view;
      final usage = view?['usage'] as Map?;
      final codex = view?['codex'] as Map?;
      final until = DateTime.tryParse(view?['freshUntil'] ?? '');
      final fresh =
          (controller?.online ?? false) &&
          until != null &&
          until.isAfter(DateTime.now());
      final cost = num.tryParse('${usage?['actualCostMicros']}');
      return Scaffold(
        appBar: AppBar(title: const Text('数据展示')),
        body: Center(
          child: ConstrainedBox(
            constraints: const BoxConstraints(maxWidth: 760),
            child: ListView(
              padding: const EdgeInsets.all(24),
              children: [
                ListTile(
                  contentPadding: EdgeInsets.zero,
                  leading: Icon(
                    (controller?.online ?? false)
                        ? Icons.cloud_done_outlined
                        : Icons.cloud_off_outlined,
                  ),
                  title: Text(
                    controller == null
                        ? '尚未连接 Orbit'
                        : (controller!.online ? 'Orbit 已连接' : 'Orbit 等待同步'),
                  ),
                  subtitle: controller == null
                      ? const Text('在收件箱页面连接 Orbit 服务。')
                      : Text(Uri.parse(controller!.server).host),
                  trailing: IconButton(
                    tooltip: '同步',
                    onPressed: controller == null || controller!.syncing
                        ? null
                        : controller!.sync,
                    icon: const Icon(Icons.sync),
                  ),
                ),
                if (controller != null)
                  const Text(
                    '消息和状态共用当前连接，无需额外填写连接参数。',
                    style: TextStyle(height: 1.6),
                  ),
                const SizedBox(height: 24),
                ListTile(
                  contentPadding: EdgeInsets.zero,
                  leading: const Icon(Icons.grid_on_outlined),
                  title: const Text('点阵屏'),
                  subtitle: const Text('按转发规则展示 · 横向全屏'),
                  trailing: const Icon(Icons.chevron_right),
                  onTap: onMatrix,
                ),
                if (view != null) ...[
                  Text(
                    fresh ? '当前状态' : '上次同步的状态',
                    style: Theme.of(context).textTheme.titleMedium,
                  ),
                  const SizedBox(height: 12),
                  if (usage == null && codex == null)
                    const Text('尚未收到当前展示的数据，请检查管理台中的数据来源。'),
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
                  if (view['displayKind'] == 'codex')
                    ListTile(
                      contentPadding: EdgeInsets.zero,
                      title: const Text('账号周限'),
                      subtitle: Text(
                        '剩余 ${view['primary']?['text'] ?? '—'} · 重置 ${view['secondary']?['text'] ?? '—'}',
                      ),
                    )
                  else if (codex != null && view['displayKind'] == null)
                    ListTile(
                      contentPadding: EdgeInsets.zero,
                      title: Text(
                        codex['weeklyLimit'] != null ? '账号周限' : 'Codex 会话',
                      ),
                      subtitle: Text(
                        codex['weeklyLimit'] != null
                            ? '剩余 ${codex['weeklyLimit']['remainingPercent'] ?? 0}%'
                            : '${codex['runningCount'] ?? 0} 个运行中 · 共 ${codex['totalCount'] ?? 0} 个',
                      ),
                    ),
                  if (view['sessionsEnabled'] == true ||
                      (view['sessionsEnabled'] == null &&
                          view['displayKind'] == null))
                    ListTile(
                      contentPadding: EdgeInsets.zero,
                      title: const Text('Session 信息'),
                      subtitle: Text(
                        codex == null
                            ? '等待会话数据'
                            : '${codex['runningCount'] ?? 0} 个运行中 · 共 ${codex['totalCount'] ?? 0} 个',
                      ),
                    ),
                ] else
                  Text(
                    '尚未设置状态摘要。不影响收件箱使用；需要时可在管理台为此设备选择状态来源。',
                    style: TextStyle(
                      color: Theme.of(context).colorScheme.onSurfaceVariant,
                      height: 1.6,
                    ),
                  ),
                if (widgetsEnabled) ...[
                  const SizedBox(height: 32),
                  const Divider(),
                  const InputMethodTools(),
                  const SizedBox(height: 24),
                  const Divider(),
                  ListTile(
                    contentPadding: EdgeInsets.zero,
                    leading: const Icon(Icons.widgets_outlined),
                    title: const Text('Android 桌面组件'),
                    subtitle: const Text('与点阵屏共用当前连接和展示规则。'),
                    trailing: const Icon(Icons.chevron_right),
                    onTap: controller == null
                        ? null
                        : () async {
                            try {
                              final result = await const MethodChannel(
                                'dev.orbit/node',
                              ).invokeMethod<String>('pin', 'usage');
                              if (context.mounted) {
                                ScaffoldMessenger.of(context).showSnackBar(
                                  SnackBar(
                                    content: Text(
                                      result == 'requested'
                                          ? '请在桌面确认添加组件'
                                          : '请从桌面的小组件列表添加 Orbit',
                                    ),
                                  ),
                                );
                              }
                            } on PlatformException {
                              if (context.mounted) {
                                ScaffoldMessenger.of(context).showSnackBar(
                                  const SnackBar(
                                    content: Text('未能添加组件，请从桌面的小组件列表添加 Orbit'),
                                  ),
                                );
                              }
                            }
                          },
                  ),
                ],
              ],
            ),
          ),
        ),
      );
    },
  );
}
