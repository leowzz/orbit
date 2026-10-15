import 'dart:async';
import 'dart:convert';
import 'package:flutter/foundation.dart';
import 'package:flutter/material.dart';
import 'package:flutter/services.dart';
import 'package:flutter_secure_storage/flutter_secure_storage.dart';
import 'inbox_controller.dart';
import 'matrix_display.dart';

class MatrixScreen extends StatefulWidget {
  final InboxController? controller;
  const MatrixScreen({super.key, this.controller});
  @override
  State<MatrixScreen> createState() => _MatrixScreenState();
}

class _MatrixScreenState extends State<MatrixScreen> {
  static const storage = FlutterSecureStorage();
  MatrixStyle style = const MatrixStyle();
  bool loading = true;
  Future<void> writes = Future.value();
  bool get shortLandscape {
    final size = MediaQuery.sizeOf(context);
    return size.width >= 600 && size.height < 500;
  }

  @override
  void initState() {
    super.initState();
    restore();
  }

  Future<void> restore() async {
    try {
      final saved = await storage.read(key: 'matrix_style');
      if (saved != null) {
        style = MatrixStyle.fromJson(jsonDecode(saved) as Map<String, dynamic>);
      }
    } catch (_) {
      if (mounted) {
        ScaffoldMessenger.of(
          context,
        ).showSnackBar(const SnackBar(content: Text('未能读取显示设置，已使用默认值')));
      }
    }
    if (mounted) setState(() => loading = false);
  }

  void save() {
    final value = jsonEncode(style.toJson());
    writes = writes
        .then((_) => storage.write(key: 'matrix_style', value: value))
        .catchError((Object _) {
          if (mounted) {
            ScaffoldMessenger.of(
              context,
            ).showSnackBar(const SnackBar(content: Text('显示设置未保存，请重试调整')));
          }
        });
  }

  Widget control(
    String label,
    String value,
    double current,
    double min,
    double max,
    int divisions,
    ValueChanged<double> change,
  ) => Column(
    crossAxisAlignment: CrossAxisAlignment.start,
    children: [
      Row(
        children: [
          Expanded(child: Text(label)),
          Text(value, style: const TextStyle(color: Color(0xff277765))),
        ],
      ),
      SizedBox(
        height: shortLandscape ? 32 : 48,
        child: Slider(
          value: current,
          min: min,
          max: max,
          divisions: divisions,
          label: value,
          onChanged: loading ? null : change,
          onChangeEnd: (_) => save(),
        ),
      ),
    ],
  );

  @override
  Widget build(BuildContext context) {
    final preview = ClipRRect(
      borderRadius: BorderRadius.circular(12),
      child: AspectRatio(
        aspectRatio: 20 / 9,
        child: MatrixLiveDisplay(controller: widget.controller, style: style),
      ),
    );
    final controls = Column(
      crossAxisAlignment: CrossAxisAlignment.stretch,
      children: [
        control(
          '像素密度',
          '${style.density}',
          style.density.toDouble(),
          64,
          128,
          16,
          (value) => setState(
            () => style = MatrixStyle(
              density: value.round(),
              glowRadius: style.glowRadius,
              glowStrength: style.glowStrength,
            ),
          ),
        ),
        control(
          '光晕范围',
          style.glowRadius == 0
              ? '关闭'
              : '${style.glowRadius.toStringAsFixed(1)} 倍',
          style.glowRadius,
          0,
          2.5,
          25,
          (value) => setState(
            () => style = MatrixStyle(
              density: style.density,
              glowRadius: value,
              glowStrength: style.glowStrength,
            ),
          ),
        ),
        control(
          '光晕强度',
          '${(style.glowStrength * 100).round()}%',
          style.glowStrength,
          0,
          1,
          20,
          (value) => setState(
            () => style = MatrixStyle(
              density: style.density,
              glowRadius: style.glowRadius,
              glowStrength: value,
            ),
          ),
        ),
      ],
    );
    final launch = FilledButton.icon(
      onPressed: loading
          ? null
          : () => Navigator.of(context).push(
              MaterialPageRoute<void>(
                builder: (_) => MatrixFullscreen(
                  controller: widget.controller,
                  style: style,
                ),
              ),
            ),
      icon: const Icon(Icons.fullscreen),
      label: const Text('横向全屏'),
    );
    final guidance = widget.controller == null
        ? '连接 Orbit 后按转发规则展示数据；轻触全屏返回设置。'
        : '轻触全屏返回设置；展示方式与来源在管理台的转发规则中设置。';
    return Scaffold(
      appBar: AppBar(
        title: const Text('点阵屏'),
        toolbarHeight: shortLandscape ? 44 : kToolbarHeight,
      ),
      body: LayoutBuilder(
        builder: (context, bounds) {
          // Same short-landscape breakpoint as the Web Node / Android node.
          if (bounds.maxWidth >= 600 && bounds.maxHeight < 500) {
            return Padding(
              padding: const EdgeInsets.fromLTRB(20, 0, 20, 12),
              child: Row(
                crossAxisAlignment: CrossAxisAlignment.stretch,
                children: [
                  Expanded(
                    flex: 6,
                    child: ListView(
                      children: [
                        preview,
                        const SizedBox(height: 10),
                        Text(
                          guidance,
                          style: Theme.of(context).textTheme.bodySmall,
                        ),
                      ],
                    ),
                  ),
                  const SizedBox(width: 24),
                  Expanded(
                    flex: 4,
                    child: Column(
                      crossAxisAlignment: CrossAxisAlignment.stretch,
                      children: [
                        Expanded(child: SingleChildScrollView(child: controls)),
                        launch,
                      ],
                    ),
                  ),
                ],
              ),
            );
          }
          return Center(
            child: ConstrainedBox(
              constraints: const BoxConstraints(maxWidth: 760),
              child: ListView(
                padding: const EdgeInsets.all(20),
                children: [
                  preview,
                  const SizedBox(height: 24),
                  controls,
                  launch,
                  const SizedBox(height: 12),
                  Text(guidance),
                ],
              ),
            ),
          );
        },
      ),
    );
  }
}

class MatrixLiveDisplay extends StatefulWidget {
  final InboxController? controller;
  final MatrixStyle style;
  const MatrixLiveDisplay({super.key, this.controller, required this.style});
  @override
  State<MatrixLiveDisplay> createState() => _MatrixLiveDisplayState();
}

class _MatrixLiveDisplayState extends State<MatrixLiveDisplay>
    with WidgetsBindingObserver {
  Timer? timer;
  @override
  void initState() {
    super.initState();
    WidgetsBinding.instance.addObserver(this);
    start();
  }

  void start() {
    timer?.cancel();
    timer = Timer.periodic(const Duration(seconds: 1), (_) => setState(() {}));
  }

  @override
  void didChangeAppLifecycleState(AppLifecycleState state) {
    if (state == AppLifecycleState.resumed) {
      start();
      setState(() {});
    } else {
      timer?.cancel();
    }
  }

  @override
  void dispose() {
    timer?.cancel();
    WidgetsBinding.instance.removeObserver(this);
    super.dispose();
  }

  @override
  Widget build(BuildContext context) => AnimatedBuilder(
    animation: widget.controller ?? const AlwaysStoppedAnimation(0),
    builder: (context, _) => MatrixDisplay(
      reading: MatrixReading.fromView(
        widget.controller?.view,
        widget.controller?.online ?? false,
        DateTime.now(),
      ),
      style: widget.style,
    ),
  );
}

class MatrixFullscreen extends StatefulWidget {
  final InboxController? controller;
  final MatrixStyle style;
  const MatrixFullscreen({super.key, this.controller, required this.style});
  @override
  State<MatrixFullscreen> createState() => _MatrixFullscreenState();
}

class _MatrixFullscreenState extends State<MatrixFullscreen> {
  static const channel = MethodChannel('dev.orbit/node');
  bool get android => defaultTargetPlatform == TargetPlatform.android;
  @override
  void initState() {
    super.initState();
    if (android) unawaited(enter());
  }

  Future<void> enter() async {
    try {
      await channel.invokeMethod<void>('matrixFullscreen', true);
    } on PlatformException {
      if (mounted) {
        ScaffoldMessenger.of(
          context,
        ).showSnackBar(const SnackBar(content: Text('未能进入系统全屏，轻触画面返回后重试')));
      }
    }
  }

  @override
  void dispose() {
    if (android) {
      unawaited(channel.invokeMethod<void>('matrixFullscreen', false));
    }
    super.dispose();
  }

  @override
  Widget build(BuildContext context) => CallbackShortcuts(
    bindings: {
      const SingleActivator(LogicalKeyboardKey.escape): () =>
          Navigator.of(context).pop(),
    },
    child: Focus(
      autofocus: true,
      child: Scaffold(
        backgroundColor: Colors.black,
        body: GestureDetector(
          behavior: HitTestBehavior.opaque,
          onTap: () => Navigator.of(context).pop(),
          child: Semantics(
            button: true,
            label: '退出全屏，返回点阵屏设置',
            child: LayoutBuilder(
              builder: (context, bounds) => RotatedBox(
                // Some embedded displays ignore Android's requested orientation.
                quarterTurns: bounds.maxHeight > bounds.maxWidth ? 1 : 0,
                child: MatrixLiveDisplay(
                  controller: widget.controller,
                  style: widget.style,
                ),
              ),
            ),
          ),
        ),
      ),
    ),
  );
}
