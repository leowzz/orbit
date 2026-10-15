import 'package:flutter/material.dart';

/// Gives short landscape displays more usable space, including pushed routes
/// and overlays. Keep system insets in the same coordinates as the content.
class AppViewport extends StatelessWidget {
  final Widget child;
  const AppViewport({super.key, required this.child});

  static bool compactOf(BuildContext context) =>
      context.dependOnInheritedWidgetOfExactType<_DisplayLayout>()?.compact ??
      false;

  @override
  Widget build(BuildContext context) {
    final media = MediaQuery.of(context);
    // Use the full view size so opening the keyboard cannot switch layouts.
    final compact =
        media.size.width >= 600 &&
        media.size.height < 500 &&
        media.size.aspectRatio >= 1.6;
    final scale = compact ? 0.85 : 1.0;
    return _DisplayLayout(
      compact: compact,
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
              child: child,
            ),
          ),
        ),
      ),
    );
  }
}

class _DisplayLayout extends InheritedWidget {
  final bool compact;
  const _DisplayLayout({required this.compact, required super.child});

  @override
  bool updateShouldNotify(_DisplayLayout oldWidget) =>
      compact != oldWidget.compact;
}
