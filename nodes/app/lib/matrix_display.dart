import 'dart:async';
import 'dart:math' as math;
import 'dart:typed_data';
import 'dart:ui' as ui;

import 'package:flutter/material.dart';
import 'matrix_icon.dart';
import 'local_store.dart';

/// Only the quota observation can renew quota freshness; session activity cannot.
class MatrixReading {
  final bool showUsage;
  final String costText, tokensText, tpmText;
  final double? remaining;
  final DateTime? reset, observed;
  final String status, countdown, usage;
  final int? hoursLeft;

  const MatrixReading({
    this.showUsage = false,
    this.costText = "--",
    this.tokensText = "--",
    this.tpmText = "--",
    this.remaining,
    this.reset,
    this.observed,
    required this.status,
    required this.countdown,
    required this.usage,
    this.hoursLeft,
  });

  factory MatrixReading.fromView(Json? view, bool online, DateTime now) {
    final codex = view?['codex'] as Map?;
    final limit = codex?['weeklyLimit'] as Map?;
    DateTime? date(dynamic value) => DateTime.tryParse('$value')?.toLocal();
    final reset = date(limit?['resetsAt']);
    final observed = date(limit?['observedAt']);
    // proto3 omits the default double value, so an absent percent means zero.
    final percent = limit == null
        ? null
        : double.tryParse('${limit['remainingPercent'] ?? 0}');
    final valid =
        percent != null &&
        percent.isFinite &&
        percent >= 0 &&
        percent <= 100 &&
        reset != null;
    final expired = reset != null && !reset.isAfter(now);
    final quotaFresh = date(limit?['freshUntil']);
    final sourceFresh = date(codex?['freshUntil']);
    final fresh =
        online &&
        quotaFresh != null &&
        quotaFresh.isAfter(now) &&
        sourceFresh != null &&
        sourceFresh.isAfter(now);
    final minutes = reset == null
        ? 0
        : math.max(0, reset.difference(now).inMinutes);
    final usage = view?['usage'] as Map?;
    final cost = num.tryParse('${usage?['actualCostMicros']}');
    String metric(dynamic value) {
      final number = num.tryParse('$value');
      if (number == null) return '—';
      if (number.abs() >= 1000000) {
        return '${(number / 1000000).toStringAsFixed(1)}M';
      }
      if (number.abs() >= 1000) return '${(number / 1000).toStringAsFixed(1)}K';
      return '$number';
    }

    final usageFresh =
        online && (date(usage?['freshUntil'])?.isAfter(now) ?? false);
    final showUsage = view?['displayKind'] == 'usage';
    return MatrixReading(
      showUsage: showUsage,
      costText: cost == null ? '--' : (cost / 1000000).toStringAsFixed(2),
      tokensText: metric(usage?['tokenCount']),
      tpmText: metric(usage?['tpm']),
      remaining: valid && !expired ? percent : null,
      reset: reset,
      observed: showUsage ? date(usage?['observedAt']) : observed,
      hoursLeft: valid && !expired ? minutes ~/ 60 : null,
      status: showUsage
          ? (usage == null
                ? '暂无数据'
                : usageFresh
                ? '实时'
                : online
                ? '上次数据'
                : '离线缓存')
          : !valid
          ? '暂无周额度'
          : expired
          ? '等待重置更新'
          : fresh
          ? '实时'
          : online
          ? '上次数据'
          : '离线缓存',
      countdown: !valid
          ? '等待同步'
          : expired
          ? '等待更新'
          : '${minutes ~/ 1440}天 ${(minutes ~/ 60) % 24}时 ${minutes % 60}分',
      usage: usage == null
          ? ''
          : '${usageFresh ? '今日' : '上次'} ${cost == null ? '—' : '${usage['currencyCode'] ?? ''} ${(cost / 1000000).toStringAsFixed(2)}'} · Token ${metric(usage['tokenCount'])} · TPM ${metric(usage['tpm'])}',
    );
  }

  String get percent => remaining == null ? '—%' : '${_percent(remaining!)}%';
  String get displayPercent =>
      remaining == null ? '--%' : '${remaining!.round()}%';
  String get displayDate =>
      reset == null ? '--/--' : '${_two(reset!.month)}/${_two(reset!.day)}';
  String get displayCountdown => hoursLeft == null
      ? '--D --H'
      : '${hoursLeft! ~/ 24}D ${_two(hoursLeft! % 24)}H';
  String get displayStatus => switch (status) {
    '实时' => '',
    '离线缓存' => 'OFFLINE',
    '上次数据' => 'STALE',
    '等待重置更新' => 'RESETTING',
    _ => 'NO DATA',
  };
  // Accessibility includes finer-grained data that is not drawn on the LEDs.
  Object get _paintKey => showUsage
      ? (true, costText, tokensText, tpmText, displayStatus)
      : (false, displayPercent, displayDate, displayCountdown, displayStatus);

  String get used =>
      remaining == null ? '已用 —' : '已用 ${_percent(100 - remaining!)}%';
  static String _percent(double value) => value == value.roundToDouble()
      ? value.toInt().toString()
      : value.toStringAsFixed(1);
  static String _two(int value) => value.toString().padLeft(2, '0');
  String get resetLabel => reset == null
      ? '— / —  — : —'
      : '${_two(reset!.month)}/${_two(reset!.day)} ${_two(reset!.hour)}:${_two(reset!.minute)}';
  String get updatedLabel => observed == null
      ? '尚未收到数据'
      : '更新于 ${_two(observed!.hour)}:${_two(observed!.minute)}';
  String get description => showUsage
      ? 'API 用量 $costText，Token $tokensText，TPM $tpmText，$status'
      : '周额度剩余 $percent，$used，$status，重置时间 $resetLabel，$countdown。$usage';
}

class MatrixStyle {
  final int density;
  final double glowRadius, glowStrength;
  const MatrixStyle({
    this.density = 72,
    this.glowRadius = 1.1,
    this.glowStrength = .65,
  });

  factory MatrixStyle.fromJson(Map<String, dynamic> json) {
    double read(String key, double fallback, double min, double max) {
      final value = num.tryParse('${json[key]}')?.toDouble();
      return value != null && value.isFinite ? value.clamp(min, max) : fallback;
    }

    return MatrixStyle(
      density: read('density', 72, 16, 128).round(),
      glowRadius: read('glowRadius', 1.1, 0, 2.5),
      glowStrength: read('glowStrength', .65, 0, 1),
    );
  }
  Map<String, dynamic> toJson() => {
    'density': density,
    'glowRadius': glowRadius,
    'glowStrength': glowStrength,
  };
}

/// Rasterize text once at LED resolution; only paint lit, square cells.
/// The display uses large ASCII digits; accessibility retains the full reading.
class MatrixDisplay extends StatefulWidget {
  final MatrixReading reading;
  final MatrixStyle style;
  const MatrixDisplay({super.key, required this.reading, required this.style});
  @override
  State<MatrixDisplay> createState() => _MatrixDisplayState();
}

class _MatrixDisplayState extends State<MatrixDisplay> {
  _LedFrame? frame;
  ui.Image? sprite;
  Object? spriteKey, requestedKey;
  _RenderRequest? pending;
  bool rendering = false;

  void request(int width, int height) {
    final next = _RenderRequest(width, height, widget.reading, widget.style);
    if (next.key == requestedKey) return;
    requestedKey = next.key;
    // One conversion at a time; replace queued slider positions with the latest.
    pending = next;
    if (!rendering) {
      rendering = true;
      scheduleMicrotask(render);
    }
  }

  Future<void> render() async {
    try {
      while (mounted && pending != null) {
        final next = pending!;
        pending = null;
        final nextFrame = frame?.key == next.rasterKey
            ? frame!
            : await _rasterize(next);
        if (!mounted) return;
        final nextSprite = spriteKey == next.spriteKey && sprite != null
            ? sprite!
            : await _makeSprite(next.style);
        if (!mounted) {
          if (nextSprite != sprite) nextSprite.dispose();
          return;
        }
        final previousSprite = sprite;
        setState(() {
          frame = nextFrame;
          sprite = nextSprite;
          spriteKey = next.spriteKey;
        });
        if (previousSprite != nextSprite) previousSprite?.dispose();
      }
    } catch (error, stack) {
      FlutterError.reportError(
        FlutterErrorDetails(
          exception: error,
          stack: stack,
          library: 'Orbit matrix display',
        ),
      );
    } finally {
      rendering = false;
    }
  }

  @override
  void dispose() {
    pending = null;
    sprite?.dispose();
    super.dispose();
  }

  @override
  Widget build(BuildContext context) => Semantics(
    label: widget.reading.description,
    image: true,
    child: RepaintBoundary(
      child: ColoredBox(
        color: Colors.black,
        child: LayoutBuilder(
          builder: (context, bounds) {
            if (bounds.maxWidth <= 0 || bounds.maxHeight <= 0) {
              return const SizedBox.shrink();
            }
            final height = widget.style.density;
            // Letterbox unusually square windows instead of squeezing the text.
            final aspect = math.max(16 / 9, bounds.maxWidth / bounds.maxHeight);
            final width = (height * aspect).round().clamp(1, 512);
            request(width, height);
            return CustomPaint(
              painter: _LedPainter(frame, sprite),
              size: Size.infinite,
            );
          },
        ),
      ),
    ),
  );
}

class _RenderRequest {
  final int width, height;
  final MatrixReading reading;
  final MatrixStyle style;
  _RenderRequest(this.width, this.height, this.reading, this.style);
  Object get rasterKey => (width, height, reading._paintKey);
  Object get spriteKey => style.glowRadius <= 0 || style.glowStrength <= 0
      ? (0.0, 0.0)
      : (style.glowRadius, style.glowStrength);
  Object get key => (rasterKey, spriteKey);
}

Future<_LedFrame> _rasterize(_RenderRequest request) async {
  final recorder = ui.PictureRecorder();
  final canvas = Canvas(recorder);
  final scale = request.height / 72;
  canvas.scale(scale);
  _drawContent(
    canvas,
    request.width / scale,
    request.reading,
    request.width >= 150,
  );
  final picture = recorder.endRecording();
  ui.Image? image;
  try {
    image = await picture.toImage(request.width, request.height);
    final bytes = await image.toByteData(format: ui.ImageByteFormat.rawRgba);
    if (bytes == null) throw StateError('Matrix raster has no pixel data');
    return _LedFrame(request, bytes.buffer.asUint8List());
  } finally {
    image?.dispose();
    picture.dispose();
  }
}

// A reusable white LED tile is tinted per cell by drawRawAtlas. Keeping its
// dimensions fixed lets density changes reuse the same glow texture.
const _spriteCell = 32.0;
const _spriteSize = 160; // Covers the maximum 2.5-cell glow on both sides.
const _spriteCenter = Offset(_spriteSize / 2, _spriteSize / 2);

Future<ui.Image> _makeSprite(MatrixStyle style) async {
  final recorder = ui.PictureRecorder();
  final canvas = Canvas(recorder);
  final paint = Paint()..isAntiAlias = true;
  if (style.glowRadius > 0 && style.glowStrength > 0) {
    final radius = _spriteCell * style.glowRadius;
    paint.shader = ui.Gradient.radial(_spriteCenter, radius, [
      Colors.white.withValues(alpha: style.glowStrength * .55),
      Colors.white.withValues(alpha: 0),
    ]);
    canvas.drawCircle(_spriteCenter, radius, paint);
    paint.shader = null;
  }
  paint.color = Colors.white;
  canvas.drawRRect(
    RRect.fromRectAndRadius(
      Rect.fromCenter(
        center: _spriteCenter,
        width: _spriteCell * .78,
        height: _spriteCell * .78,
      ),
      const Radius.circular(_spriteCell * .08),
    ),
    paint,
  );
  final picture = recorder.endRecording();
  try {
    return await picture.toImage(_spriteSize, _spriteSize);
  } finally {
    picture.dispose();
  }
}

class _LedFrame {
  final Object key;
  final int columns, rows;
  late final Float32List transforms, rects;
  late final Int32List colors;

  _LedFrame(_RenderRequest request, Uint8List pixels)
    : key = request.rasterKey,
      columns = request.width,
      rows = request.height {
    var count = 0;
    for (var i = 3; i < pixels.length; i += 4) {
      if (pixels[i] >= 65) count++;
    }
    transforms = Float32List(count * 4);
    rects = Float32List(count * 4);
    colors = Int32List(count);
    var cell = 0;
    for (var y = 0; y < rows; y++) {
      for (var x = 0; x < columns; x++) {
        final i = (y * columns + x) * 4;
        if (pixels[i + 3] < 65) continue;
        final offset = cell * 4;
        transforms[offset] = 1;
        transforms[offset + 2] = (x + .5) * _spriteCell - _spriteCenter.dx;
        transforms[offset + 3] = (y + .5) * _spriteCell - _spriteCenter.dy;
        rects[offset + 2] = _spriteSize.toDouble();
        rects[offset + 3] = _spriteSize.toDouble();
        colors[cell++] =
            0xff000000 |
            (pixels[i] << 16) |
            (pixels[i + 1] << 8) |
            pixels[i + 2];
      }
    }
  }
}

const _mint = Color(0xffa8e6cd);
const _white = Color(0xffeaf1e8);
const _muted = Color(0xffa1bcb2);
const _amber = Color(0xffeeb97a);

void _drawContent(
  Canvas canvas,
  double width,
  MatrixReading reading,
  bool icon,
) {
  void text(
    String value,
    double x,
    double y,
    double size,
    Color color,
    double maxWidth,
  ) {
    TextPainter painter(double fontSize) => TextPainter(
      text: TextSpan(
        text: value,
        style: TextStyle(
          fontFamily: 'monospace',
          fontSize: fontSize,
          height: 1.15,
          color: color,
          fontWeight: FontWeight.w600,
        ),
      ),
      textDirection: TextDirection.ltr,
      maxLines: 1,
    )..layout();
    var p = painter(size);
    if (p.width > maxWidth) {
      final fitted = painter(size * maxWidth / p.width);
      p.dispose();
      p = fitted;
    }
    p.paint(canvas, Offset(x, y));
    p.dispose();
  }

  if (reading.showUsage) {
    final left = width * .57;
    text(
      reading.displayStatus.isEmpty ? 'USD' : reading.displayStatus,
      4,
      3,
      9,
      _mint,
      left - 8,
    );
    text(reading.costText, 4, 19, 36, _white, left - 8);
    text('TOKENS', left, 3, 9, _muted, width - left - 4);
    text(reading.tokensText, left, 16, 18, _white, width - left - 4);
    text('TPM', left, 39, 9, _muted, width - left - 4);
    text(reading.tpmText, left, 51, 17, _amber, width - left - 4);
    return;
  }

  // Keep the largest area for the percentage. Only wide displays get an icon.
  final right = width * .58;
  final showIcon = icon && width >= 210;
  final left = showIcon ? 33.0 : 4.0;
  final quotaWidth = right - left - 7;
  final detailWidth = width - right - 4;
  final label = reading.displayStatus.isEmpty ? 'LEFT' : reading.displayStatus;
  text(
    label,
    left,
    3,
    9,
    reading.displayStatus.isEmpty ? _mint : _amber,
    quotaWidth,
  );
  if (showIcon) {
    final paint = Paint()..color = _mint;
    for (var y = 0; y < 24; y++) {
      for (var x = 0; x < 24; x++) {
        if (matrixChatGptIcon[y][x] == '1') {
          canvas.drawRect(Rect.fromLTWH(x + 3, y + 25, 1, 1), paint);
        }
      }
    }
  }
  text(reading.displayPercent, left, 15, 48, _white, quotaWidth);
  text('RESET', right, 3, 9, _muted, detailWidth);
  text(reading.displayDate, right, 19, 20, _white, detailWidth);
  text(reading.displayCountdown, right, 47, 17, _amber, detailWidth);
}

class _LedPainter extends CustomPainter {
  final _LedFrame? frame;
  final ui.Image? sprite;
  _LedPainter(this.frame, this.sprite);

  @override
  void paint(Canvas canvas, Size size) {
    final cells = frame;
    if (cells == null || sprite == null || cells.colors.isEmpty) return;
    final cell = math.min(size.width / cells.columns, size.height / cells.rows);
    canvas.save();
    canvas.clipRect(Offset.zero & size);
    canvas.translate(
      (size.width - cells.columns * cell) / 2,
      (size.height - cells.rows * cell) / 2,
    );
    canvas.scale(cell / _spriteCell);
    canvas.drawRawAtlas(
      sprite!,
      cells.transforms,
      cells.rects,
      cells.colors,
      BlendMode.modulate,
      null,
      Paint()..filterQuality = FilterQuality.low,
    );
    canvas.restore();
  }

  @override
  bool shouldRepaint(_LedPainter old) =>
      frame != old.frame || sprite != old.sprite;
}
