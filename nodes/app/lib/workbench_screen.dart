import 'dart:async';
import 'dart:math' as math;
import 'package:flutter/foundation.dart';
import 'package:flutter/material.dart';
import 'package:flutter/services.dart';
import 'inbox_controller.dart';
import 'navigation_bar.dart';
import 'app_theme.dart';
import 'local_store.dart' show Json;

/// Native counterpart of the Web Node's short landscape dashboard.
class WorkbenchScreen extends StatefulWidget {
  final InboxController? controller;
  final bool fullscreen;
  final VoidCallback? onEnterFullscreen;
  const WorkbenchScreen({
    super.key,
    this.controller,
    this.fullscreen = false,
    this.onEnterFullscreen,
  });

  @override
  State<WorkbenchScreen> createState() => _WorkbenchScreenState();
}

class _WorkbenchScreenState extends State<WorkbenchScreen> {
  OrbitColors get _colors => OrbitColors.of(context);
  Color get _green => _colors.success;
  Color get _muted => _colors.muted;
  Color get _line => _colors.border;
  static const _channel = MethodChannel('dev.orbit/node');
  final _opening = <String>{};
  bool _leaving = false;
  bool _isAlipayBox = false;
  bool get _android => defaultTargetPlatform == TargetPlatform.android;

  double get _metricsLeftPadding {
    if (!_isAlipayBox) return 16;
    // Core owns device display preferences; older Core views remain a fallback.
    for (final value in [
      widget.controller?.navigation['nfcd_metrics_left_padding'],
      widget.controller?.view?['nfcdMetricsLeftPadding'],
    ]) {
      if (value is num &&
          value.isFinite &&
          value >= 0 &&
          value <= 64 &&
          value == value.roundToDouble()) {
        return value.toDouble();
      }
    }
    return 16;
  }

  @override
  void initState() {
    super.initState();
    if (_android) unawaited(_readDeviceModel());
    if (widget.fullscreen && _android) unawaited(_enter());
  }

  Future<void> _readDeviceModel() async {
    try {
      final model = await _channel.invokeMethod<String>('deviceModel');
      if (mounted && model == 'NFCD_AlipayBoxN5DEL') {
        setState(() => _isAlipayBox = true);
      }
    } on PlatformException {
      // An unavailable device identity keeps the general layout.
    } on MissingPluginException {
      // Desktop and older Android hosts keep the general layout.
    }
  }

  Future<void> _enter() async {
    try {
      await _channel.invokeMethod<void>('matrixFullscreen', true);
    } catch (_) {
      if (mounted) _feedback('未能进入系统全屏，请返回后重试');
    }
  }

  @override
  void dispose() {
    if (widget.fullscreen && _android) {
      unawaited(_restoreSystemUi());
    }
    super.dispose();
  }

  Future<void> _restoreSystemUi() async {
    try {
      await _channel.invokeMethod<void>('matrixFullscreen', false);
    } catch (_) {
      // The route may be disposed while the native activity is shutting down.
    }
  }

  void _toggleFullscreen() {
    if (widget.fullscreen) {
      if (_leaving) return;
      _leaving = true;
      Navigator.of(context).pop();
    } else if (widget.onEnterFullscreen != null) {
      widget.onEnterFullscreen!();
    } else {
      Navigator.of(context).push(
        MaterialPageRoute<void>(
          builder: (_) =>
              WorkbenchScreen(controller: widget.controller, fullscreen: true),
        ),
      );
    }
  }

  void _feedback(String text) {
    ScaffoldMessenger.of(context)
      ..hideCurrentSnackBar()
      ..showSnackBar(SnackBar(content: Text(text)));
  }

  Future<void> _open(Map session) async {
    final id = session['sessionId'] as String?;
    final controller = widget.controller;
    if (id == null || controller == null || !_opening.add(id)) return;
    setState(() {});
    try {
      await controller.openCodexSession(id);
    } on ApiError catch (error) {
      if (mounted) {
        _feedback(switch (error.code) {
          'session_unavailable' => '会话状态已变化，请同步后重试',
          'session_actions_unsupported' => '当前服务不支持打开会话，请更新 Core 后重试',
          'unauthenticated' || 'device_revoked' => message(error.code),
          _ => '打开请求失败，请检查连接后重试',
        });
      }
    } catch (_) {
      if (mounted) _feedback('打开请求失败，请检查连接后重试');
    } finally {
      _opening.remove(id);
      if (mounted) setState(() {});
    }
  }

  @override
  Widget build(BuildContext context) => CallbackShortcuts(
    bindings: {
      if (widget.fullscreen)
        const SingleActivator(LogicalKeyboardKey.escape): _toggleFullscreen,
    },
    child: Focus(
      autofocus: widget.fullscreen,
      child: Scaffold(
        backgroundColor: _colors.surface,
        body: SafeArea(
          child: AnimatedBuilder(
            animation: widget.controller ?? const AlwaysStoppedAnimation(0),
            builder: (context, _) => LayoutBuilder(
              builder: (context, bounds) {
                final compact =
                    bounds.maxWidth >= 600 &&
                    bounds.maxHeight <= 640 &&
                    bounds.maxWidth > bounds.maxHeight &&
                    MediaQuery.textScalerOf(context).scale(18) <= 18;
                final view = widget.controller?.view;
                final enabled =
                    view?['sessionsEnabled'] == true ||
                    (view?['sessionsEnabled'] == null &&
                        view?['displayKind'] == null);
                final metrics = _metrics(view);
                final navigationAtTop =
                    widget.controller?.navigation['at_top'] == true;
                final statusBar = _header(view, compact, bounds.maxWidth);
                final navigation = widget.controller == null
                    ? const SizedBox.shrink()
                    : WorkbenchNavigation(
                        key: const ValueKey('workbench-navigation-widget'),
                        controller: widget.controller!,
                      );
                return Column(
                  children: [
                    navigationAtTop ? navigation : statusBar,
                    Expanded(
                      child: view == null
                          ? Center(
                              child: Padding(
                                padding: const EdgeInsets.all(24),
                                child: Text(
                                  widget.controller == null
                                      ? '连接 Orbit 后查看工作台'
                                      : widget.controller!.online
                                      ? '等待状态数据，请在管理台为此设备选择来源'
                                      : '未能同步状态，请点击连接状态重试',
                                ),
                              ),
                            )
                          : compact
                          ? Padding(
                              padding: const EdgeInsets.fromLTRB(14, 4, 14, 6),
                              child: Row(
                                crossAxisAlignment: CrossAxisAlignment.stretch,
                                children: [
                                  SizedBox(
                                    width: 150,
                                    child: Column(
                                      children: [
                                        for (var i = 0; i < metrics.length; i++)
                                          Expanded(
                                            child: _metric(
                                              metrics[i],
                                              compact: true,
                                              divider: i > 0,
                                            ),
                                          ),
                                      ],
                                    ),
                                  ),
                                  const SizedBox(width: 6),
                                  Expanded(
                                    child: enabled
                                        ? _sessions(
                                            view,
                                            compact: true,
                                            fourRows: _isAlipayBox,
                                          )
                                        : Center(
                                            child: Text(
                                              '未开启会话信息',
                                              style: TextStyle(color: _muted),
                                            ),
                                          ),
                                  ),
                                ],
                              ),
                            )
                          : _regular(view, metrics, enabled),
                    ),
                    navigationAtTop ? statusBar : navigation,
                  ],
                );
              },
            ),
          ),
        ),
      ),
    ),
  );

  Widget _header(Json? view, bool compact, double width) {
    final online = widget.controller?.online ?? false;
    final until = DateTime.tryParse(view?['freshUntil'] ?? '');
    final fresh = online && until != null && until.isAfter(DateTime.now());
    final date = DateTime.tryParse(
      view?['metadata']?['producedAt'] ?? '',
    )?.toLocal();
    final status = widget.controller == null
        ? '未连接'
        : !online
        ? '等待同步'
        : fresh
        ? '实时连接'
        : '等待更新';
    return SizedBox(
      key: const ValueKey('workbench-status-bar'),
      height: compact ? 40 : 58,
      child: Padding(
        padding: EdgeInsets.symmetric(horizontal: compact ? 18 : 20),
        child: Row(
          children: [
            Tooltip(
              message: widget.fullscreen ? '退出全屏' : '进入全屏',
              child: TextButton(
                key: const ValueKey('workbench-fullscreen'),
                style: TextButton.styleFrom(
                  foregroundColor: _colors.ink,
                  padding: EdgeInsets.zero,
                ),
                onPressed: _toggleFullscreen,
                child: Row(
                  mainAxisSize: MainAxisSize.min,
                  children: [
                    CustomPaint(
                      size: const Size(24, 24),
                      painter: _OrbitMark(widget.fullscreen, _colors),
                    ),
                    const SizedBox(width: 8),
                    const Text(
                      'Orbit',
                      style: TextStyle(
                        fontSize: 16,
                        fontWeight: FontWeight.w700,
                      ),
                    ),
                  ],
                ),
              ),
            ),
            Expanded(
              child: width >= 520 && date != null
                  ? Text(
                      '${_two(date.month)}/${_two(date.day)} ${_two(date.hour)}:${_two(date.minute)}:${_two(date.second)}'
                      '${width >= 780 ? '  星期${'一二三四五六日'[date.weekday - 1]}' : ''}',
                      textAlign: TextAlign.center,
                      style: TextStyle(color: _muted, fontSize: 12),
                    )
                  : const SizedBox(),
            ),
            TextButton.icon(
              onPressed: widget.controller == null || widget.controller!.syncing
                  ? null
                  : widget.controller!.sync,
              icon: Icon(Icons.circle, size: 8, color: fresh ? _green : _muted),
              label: Text(status, style: const TextStyle(fontSize: 12)),
              style: TextButton.styleFrom(foregroundColor: _muted),
            ),
          ],
        ),
      ),
    );
  }

  Widget _regular(
    Json view,
    List<_Metric> metrics,
    bool enabled,
  ) => CustomScrollView(
    key: const PageStorageKey('workbench-page'),
    slivers: [
      SliverPadding(
        padding: const EdgeInsets.fromLTRB(24, 24, 24, 20),
        sliver: SliverToBoxAdapter(
          child: Column(
            crossAxisAlignment: CrossAxisAlignment.start,
            children: [
              Text(
                view['displayKind'] == 'codex' ? '账号周限' : 'API 用量',
                style: const TextStyle(
                  fontSize: 28,
                  fontWeight: FontWeight.w700,
                ),
              ),
              const SizedBox(height: 24),
              LayoutBuilder(
                builder: (context, bounds) => Wrap(
                  children: [
                    for (final metric in metrics)
                      SizedBox(
                        width:
                            bounds.maxWidth /
                            (bounds.maxWidth < 600 ? 2 : metrics.length),
                        height: 112,
                        child: _metric(metric),
                      ),
                  ],
                ),
              ),
              if (enabled) ...[
                const SizedBox(height: 24),
                Text(
                  '${view['codex']?['runningCount'] ?? 0} 个运行中 · 共 ${view['codex']?['totalCount'] ?? 0} 个会话',
                  style: const TextStyle(
                    fontSize: 18,
                    fontWeight: FontWeight.w600,
                  ),
                ),
              ],
            ],
          ),
        ),
      ),
      if (enabled)
        SliverPadding(
          padding: const EdgeInsets.fromLTRB(24, 0, 24, 24),
          sliver: _sessionSliver(view, compact: false),
        ),
    ],
  );

  Widget _metric(
    _Metric metric, {
    bool compact = false,
    bool divider = false,
  }) => Container(
    width: double.infinity,
    decoration: BoxDecoration(
      border: Border(
        top: BorderSide(
          color: divider || !compact ? _line : Colors.transparent,
        ),
      ),
    ),
    padding: EdgeInsets.fromLTRB(compact ? _metricsLeftPadding : 0, 4, 14, 4),
    child: Column(
      crossAxisAlignment: CrossAxisAlignment.start,
      mainAxisAlignment: MainAxisAlignment.center,
      children: [
        Text(
          metric.label,
          style: TextStyle(fontSize: 13, color: _muted, height: 1.15),
        ),
        const SizedBox(height: 3),
        Flexible(
          child: FittedBox(
            fit: BoxFit.scaleDown,
            alignment: Alignment.centerLeft,
            child: Text(
              metric.value,
              style: TextStyle(
                fontSize: metric.prominent ? 44 : (compact ? 25 : 29),
                height: 1.15,
                fontWeight: FontWeight.w700,
                color: metric.accent ? _colors.info : _colors.ink,
              ),
            ),
          ),
        ),
      ],
    ),
  );

  Widget _sessions(Json view, {required bool compact, bool fourRows = false}) =>
      CustomScrollView(
        key: const PageStorageKey('workbench-sessions'),
        slivers: [_sessionSliver(view, compact: compact, fourRows: fourRows)],
      );

  Widget _sessionSliver(
    Json view, {
    required bool compact,
    bool fourRows = false,
  }) {
    final sessions = (view['codex']?['sessions'] as List? ?? [])
        .whereType<Map>()
        .toList();
    if (sessions.isEmpty) {
      return SliverToBoxAdapter(
        child: Padding(
          padding: const EdgeInsets.all(24),
          child: Text(
            view['codex'] == null ? '等待会话数据' : '暂无最近任务',
            style: TextStyle(color: _muted),
          ),
        ),
      );
    }
    return SliverLayoutBuilder(
      builder: (context, bounds) {
        if (fourRows) {
          const gap = 6.0;
          final height = (bounds.viewportMainAxisExtent - 3 * gap) / 4;
          return SliverGrid(
            gridDelegate: SliverGridDelegateWithFixedCrossAxisCount(
              crossAxisCount: 2,
              mainAxisExtent: height,
              mainAxisSpacing: gap,
              crossAxisSpacing: gap,
            ),
            delegate: SliverChildBuilderDelegate(
              (context, index) => _session(sessions[index], rowHeight: height),
              childCount: sessions.length,
            ),
          );
        }
        final columns = bounds.crossAxisExtent >= 560 ? 2 : 1;
        final rows = (sessions.length / columns).ceil();
        final gap = compact ? 6.0 : 12.0;
        return SliverList(
          delegate: SliverChildBuilderDelegate(
            (context, row) => Padding(
              padding: EdgeInsets.only(bottom: row + 1 < rows ? gap : 0),
              child: IntrinsicHeight(
                child: Row(
                  crossAxisAlignment: CrossAxisAlignment.stretch,
                  children: [
                    for (var column = 0; column < columns; column++) ...[
                      if (column > 0) SizedBox(width: gap),
                      Expanded(
                        child: row * columns + column < sessions.length
                            ? _session(sessions[row * columns + column])
                            : const SizedBox.shrink(),
                      ),
                    ],
                  ],
                ),
              ),
            ),
            childCount: rows,
          ),
        );
      },
    );
  }

  Widget _session(Map session, {double? rowHeight}) {
    // Keep breathing room without overflowing the four-row layout outside fullscreen.
    final verticalPadding = rowHeight == null
        ? 8.0
        : ((rowHeight - 42) / 2).clamp(4.0, 10.0);
    final titleLines =
        rowHeight == null || rowHeight - verticalPadding * 2 >= 64 ? 2 : 1;
    final running = session['status'] == 'CODEX_SESSION_STATUS_RUNNING';
    final status = switch (session['status']) {
      'CODEX_SESSION_STATUS_RUNNING' => '运行中',
      'CODEX_SESSION_STATUS_COMPLETED' => '已完成',
      'CODEX_SESSION_STATUS_FAILED' => '失败',
      'CODEX_SESSION_STATUS_INTERRUPTED' => '已中断',
      'CODEX_SESSION_STATUS_CANCELLED' => '已取消',
      _ => '未知',
    };
    final statusColor = switch (session['status']) {
      'CODEX_SESSION_STATUS_RUNNING' => _green,
      'CODEX_SESSION_STATUS_FAILED' => _colors.error,
      'CODEX_SESSION_STATUS_INTERRUPTED' => _colors.warning,
      _ => _muted,
    };
    final pending = _opening.contains(session['sessionId']);
    final title = '${session['displayName'] ?? '未命名会话'}';
    return Material(
      color: running ? _colors.runningSurface : _colors.surface,
      shape: RoundedRectangleBorder(
        borderRadius: BorderRadius.circular(6),
        side: BorderSide(color: running ? _colors.runningBorder : _line),
      ),
      clipBehavior: Clip.antiAlias,
      child: InkWell(
        key: ValueKey('session-${session['sessionId']}'),
        onTap: pending ? null : () => _open(session),
        child: Container(
          decoration: BoxDecoration(
            border: Border(
              left: BorderSide(
                width: 3,
                color: running ? _green : Colors.transparent,
              ),
            ),
          ),
          padding: EdgeInsets.symmetric(
            horizontal: rowHeight == null ? 10 : 14,
            vertical: verticalPadding,
          ),
          child: Column(
            mainAxisSize: MainAxisSize.min,
            crossAxisAlignment: CrossAxisAlignment.start,
            mainAxisAlignment: MainAxisAlignment.spaceBetween,
            children: [
              Row(
                crossAxisAlignment: CrossAxisAlignment.start,
                children: [
                  Expanded(
                    child: Tooltip(
                      message: title,
                      child: Text(
                        title,
                        maxLines: titleLines,
                        overflow: TextOverflow.ellipsis,
                        style: TextStyle(
                          fontSize: 18,
                          height: 1.2,
                          fontWeight: FontWeight.w700,
                          color: _colors.ink,
                        ),
                      ),
                    ),
                  ),
                  const SizedBox(width: 8),
                  Container(
                    padding: const EdgeInsets.symmetric(
                      horizontal: 6,
                      vertical: 3,
                    ),
                    decoration: BoxDecoration(
                      color: statusColor.withValues(alpha: 0.08),
                      borderRadius: BorderRadius.circular(4),
                    ),
                    child: Text(
                      pending ? '发送中' : status,
                      style: TextStyle(
                        fontSize: 13,
                        height: 1.2,
                        fontWeight: FontWeight.w600,
                        color: statusColor,
                      ),
                    ),
                  ),
                ],
              ),
              SizedBox(height: rowHeight == null ? 6 : 4),
              Row(
                children: [
                  Expanded(
                    child: Row(
                      children: [
                        Flexible(
                          child: _sessionDetail(
                            '${session['projectName'] ?? '项目未公开'}',
                          ),
                        ),
                        Text(
                          ' · ',
                          style: TextStyle(
                            fontSize: 13,
                            height: 1.2,
                            color: _muted,
                          ),
                        ),
                        Flexible(
                          child: _sessionDetail(
                            '${session['model'] ?? '模型未知'}',
                          ),
                        ),
                      ],
                    ),
                  ),
                  const SizedBox(width: 10),
                  Text(
                    _relative(session['updatedAt']),
                    style: TextStyle(fontSize: 13, height: 1.2, color: _muted),
                  ),
                ],
              ),
            ],
          ),
        ),
      ),
    );
  }

  Widget _sessionDetail(String text) => Tooltip(
    message: text,
    child: Text(
      text,
      maxLines: 1,
      overflow: TextOverflow.ellipsis,
      style: TextStyle(fontSize: 13, height: 1.2, color: _muted),
    ),
  );
}

class _Metric {
  final String label, value;
  final bool accent, prominent;
  const _Metric(
    this.label,
    this.value, {
    this.accent = false,
    this.prominent = false,
  });
}

List<_Metric> _metrics(Json? view) {
  if (view?['displayKind'] == 'codex') {
    final days = RegExp(
      r'^(\d+)\s+(\d{2}/\d{2})$',
    ).firstMatch(view?['secondary']?['text'] ?? '');
    final hours = RegExp(
      r'^(\d+)\s+(\d{2}:\d{2})$',
    ).firstMatch(view?['footer']?['text'] ?? '');
    return [
      _Metric('剩余额度', view?['primary']?['text'] ?? '--%', prominent: true),
      _Metric('重置时间', '${days?[2] ?? '--/--'} ${hours?[2] ?? '--:--'}'),
      _Metric(
        '剩余时间',
        days != null && hours != null ? '${days[1]}d${hours[1]}h' : '--',
        accent: true,
      ),
    ];
  }
  final usage = view?['usage'] as Map?;
  final cost = num.tryParse('${usage?['actualCostMicros']}');
  return [
    _Metric(
      '实际成本',
      cost == null ? '--' : '\$${(cost / 1000000).toStringAsFixed(2)}',
    ),
    _Metric('Token', _compact(usage?['tokenCount'])),
    _Metric('TPM', _compact(usage?['tpm'])),
    if (view?['displayKind'] == null)
      _Metric(
        '运行任务',
        '${view?['codex']?['runningCount'] ?? '--'}',
        accent: true,
      ),
  ];
}

String _compact(dynamic value) {
  final number = num.tryParse('$value');
  if (number == null) return '--';
  for (final unit in [(1e9, 'B'), (1e6, 'M'), (1e3, 'K')]) {
    if (number >= unit.$1) {
      return '${(number / unit.$1).toStringAsFixed(2).replaceFirst(RegExp(r'\.?0+$'), '')}${unit.$2}';
    }
  }
  return '$number';
}

String _two(int value) => value.toString().padLeft(2, '0');
String _relative(dynamic value) {
  final date = DateTime.tryParse('$value');
  if (date == null) return '--';
  final seconds = DateTime.now().difference(date).inSeconds;
  if (seconds < 60) return '刚刚';
  if (seconds < 3600) return '${seconds ~/ 60}分钟前';
  if (seconds < 86400) return '${seconds ~/ 3600}小时前';
  return '${seconds ~/ 86400}天前';
}

class _OrbitMark extends CustomPainter {
  final bool active;
  final OrbitColors _colors;
  _OrbitMark(this.active, this._colors);
  @override
  void paint(Canvas canvas, Size size) {
    canvas.scale(size.width / 96);
    final stroke = Paint()
      ..style = PaintingStyle.stroke
      ..strokeCap = StrokeCap.round
      ..strokeWidth = 8
      ..color = active ? _colors.success : _colors.ink;
    canvas.drawArc(
      const Rect.fromLTWH(16, 16, 64, 64),
      0,
      math.pi * 1.8,
      false,
      stroke,
    );
    canvas.drawLine(
      const Offset(48, 48),
      const Offset(70.5, 27.5),
      stroke
        ..strokeWidth = 6
        ..color = _colors.success,
    );
    canvas.drawCircle(
      const Offset(48, 48),
      10,
      Paint()..color = _colors.success,
    );
    canvas.drawCircle(const Offset(73, 25), 8, Paint()..color = _colors.info);
  }

  @override
  bool shouldRepaint(_OrbitMark oldDelegate) =>
      active != oldDelegate.active || _colors.dark != oldDelegate._colors.dark;
}
