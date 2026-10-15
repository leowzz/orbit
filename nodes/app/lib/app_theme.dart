import 'package:flutter/material.dart';
import 'package:flutter/services.dart';

/// Dark colors match nodes/web/static/app.css; keep broad surfaces near black.
class OrbitColors {
  final bool dark;
  const OrbitColors(this.dark);
  static OrbitColors of(BuildContext context) =>
      OrbitColors(Theme.of(context).brightness == Brightness.dark);

  Color tone(int light, int night) => Color(dark ? night : light);
  Color get background => tone(0xfffafbf9, 0xff151917);
  Color get inboxBackground => tone(0xfff6f8f5, 0xff151917);
  Color get surface => tone(0xffffffff, 0xff151917);
  Color get ink => tone(0xff263b33, 0xffe6ebe8);
  Color get muted => tone(0xff738078, 0xff9aa49f);
  Color get accent => tone(0xff287663, 0xff55c59a);
  Color get onAccent => tone(0xffffffff, 0xff10231b);
  Color get border => tone(0xffdfe6df, 0xff313a36);
  Color get success => tone(0xff087f5b, 0xff55c59a);
  Color get info => tone(0xff1769aa, 0xff73acd8);
  Color get warning => tone(0xff9a6700, 0xffdda154);
  Color get error => tone(0xffb42318, 0xffed7b7b);
  Color get warningSurface => tone(0xfffff2d8, 0xff382919);
  Color get warningBorder => tone(0xffebd29e, 0xff755a34);
  Color get runningSurface => tone(0xfff4faf7, 0xff17251f);
  Color get runningBorder => tone(0xffb9dfce, 0xff2f624e);
  Color get navigation => tone(0xfff6f9f7, 0xff151917);
}

ThemeData orbitTheme(Brightness brightness) {
  final colors = OrbitColors(brightness == Brightness.dark);
  final border = OutlineInputBorder(
    borderRadius: BorderRadius.circular(14),
    borderSide: BorderSide(color: colors.border),
  );
  final scheme = ColorScheme.fromSeed(
    seedColor: const Color(0xff277765),
    brightness: brightness,
    surface: colors.background,
  );
  return ThemeData(
    useMaterial3: true,
    brightness: brightness,
    colorScheme: colors.dark
        ? scheme.copyWith(
            primary: colors.accent,
            onPrimary: colors.onAccent,
            primaryContainer: const Color(0xff28302c),
            onPrimaryContainer: colors.ink,
            secondary: colors.muted,
            onSecondary: colors.background,
            secondaryContainer: const Color(0xff28302c),
            onSecondaryContainer: colors.ink,
            surface: colors.surface,
            surfaceDim: colors.background,
            surfaceBright: const Color(0xff313a36),
            surfaceContainerLowest: const Color(0xff101412),
            surfaceContainerLow: const Color(0xff1a201d),
            surfaceContainer: const Color(0xff1a201d),
            surfaceContainerHigh: const Color(0xff222925),
            surfaceContainerHighest: const Color(0xff28302c),
            onSurface: colors.ink,
            onSurfaceVariant: colors.muted,
            outline: const Color(0xff46514c),
            outlineVariant: colors.border,
            surfaceTint: Colors.transparent,
            error: colors.error,
            onError: const Color(0xff3a2020),
          )
        : scheme,
    scaffoldBackgroundColor: colors.background,
    appBarTheme: AppBarTheme(
      systemOverlayStyle: orbitSystemUi(brightness),
      backgroundColor: colors.background,
      surfaceTintColor: Colors.transparent,
      centerTitle: false,
    ),
    inputDecorationTheme: InputDecorationTheme(
      filled: true,
      fillColor: colors.surface,
      border: border,
      enabledBorder: border,
    ),
  );
}

SystemUiOverlayStyle orbitSystemUi(Brightness brightness) {
  final dark = brightness == Brightness.dark;
  return SystemUiOverlayStyle(
    statusBarColor: Colors.transparent,
    statusBarIconBrightness: dark ? Brightness.light : Brightness.dark,
    statusBarBrightness: brightness,
    systemNavigationBarColor: OrbitColors(dark).background,
    systemNavigationBarIconBrightness: dark
        ? Brightness.light
        : Brightness.dark,
  );
}
