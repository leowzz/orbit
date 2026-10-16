import 'package:flutter/material.dart';
import 'package:flutter/foundation.dart';
import 'package:flutter/services.dart';

/// Keeps NFCD display adjustments separate from ordinary adaptive layouts.
class AppViewport extends StatefulWidget {
  final Widget child;
  const AppViewport({super.key, required this.child});

  static bool compactOf(BuildContext context) =>
      context.dependOnInheritedWidgetOfExactType<_DisplayLayout>()?.compact ??
      false;

  static bool isAlipayBoxOf(BuildContext context) =>
      context
          .dependOnInheritedWidgetOfExactType<_DisplayLayout>()
          ?.isAlipayBox ??
      false;

  @override
  State<AppViewport> createState() => _AppViewportState();
}

class _AppViewportState extends State<AppViewport> {
  bool isAlipayBox = false;

  @override
  void initState() {
    super.initState();
    if (defaultTargetPlatform == TargetPlatform.android) _readDeviceModel();
  }

  Future<void> _readDeviceModel() async {
    try {
      final model = await const MethodChannel(
        'dev.orbit/node',
      ).invokeMethod<String>('deviceModel');
      if (mounted && model == 'NFCD_AlipayBoxN5DEL') {
        setState(() => isAlipayBox = true);
      }
    } on PlatformException {
      // Unknown devices use the ordinary layout.
    } on MissingPluginException {
      // Desktop and older hosts use the ordinary layout.
    }
  }

  @override
  Widget build(BuildContext context) {
    final media = MediaQuery.of(context);
    // Use the full view size so opening the keyboard cannot switch layouts.
    final compact =
        media.size.width >= 600 &&
        media.size.height < 500 &&
        media.size.aspectRatio >= 1.6;
    final scale = isAlipayBox && compact ? 0.85 : 1.0;
    return _DisplayLayout(
      compact: compact,
      isAlipayBox: isAlipayBox,
      child: LayoutBuilder(
        builder: (context, bounds) => FittedBox(
          fit: BoxFit.fill,
          child: SizedBox(
            width: bounds.maxWidth / scale,
            height: bounds.maxHeight / scale,
            child: MediaQuery(
              data: media.copyWith(
                size: media.size / scale,
                devicePixelRatio: media.devicePixelRatio * scale,
                padding: media.padding / scale,
                viewPadding: media.viewPadding / scale,
                viewInsets: media.viewInsets / scale,
                systemGestureInsets: media.systemGestureInsets / scale,
              ),
              child: widget.child,
            ),
          ),
        ),
      ),
    );
  }
}

class _DisplayLayout extends InheritedWidget {
  final bool compact;
  final bool isAlipayBox;
  const _DisplayLayout({
    required this.compact,
    required this.isAlipayBox,
    required super.child,
  });

  @override
  bool updateShouldNotify(_DisplayLayout oldWidget) =>
      compact != oldWidget.compact || isAlipayBox != oldWidget.isAlipayBox;
}
