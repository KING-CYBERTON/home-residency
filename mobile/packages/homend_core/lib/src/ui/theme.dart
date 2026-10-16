import 'package:flutter/material.dart';

/// The portals' Tailwind brand palette (`internal/web/styles/app.css`).
abstract final class Brand {
  static const c50 = Color(0xFFF0FDFA);
  static const c100 = Color(0xFFCCFBF1);
  static const c200 = Color(0xFF99F6E4);
  static const c500 = Color(0xFF14B8A6);
  static const c600 = Color(0xFF0D9488);
  static const c700 = Color(0xFF0F766E);
  static const c800 = Color(0xFF115E59);
  static const c900 = Color(0xFF134E4A);
  static const accent = Color(0xFFF59E0B);

  static const slate50 = Color(0xFFF8FAFC);
  static const slate200 = Color(0xFFE2E8F0);
  static const slate300 = Color(0xFFCBD5E1);
  static const slate500 = Color(0xFF64748B);
  static const slate600 = Color(0xFF475569);
  static const slate900 = Color(0xFF0F172A);
  static const amber50 = Color(0xFFFFFBEB);
  static const amber200 = Color(0xFFFDE68A);
  static const amber800 = Color(0xFF92400E);
  static const red700 = Color(0xFFB91C1C);
  static const red50 = Color(0xFFFEF2F2);
}

ThemeData homendTheme() {
  final scheme = ColorScheme.fromSeed(
    seedColor: Brand.c700,
    primary: Brand.c700,
    onPrimary: Colors.white,
    secondary: Brand.accent,
    surface: Colors.white,
    error: Brand.red700,
  );
  final border = OutlineInputBorder(
    borderRadius: BorderRadius.circular(8),
    borderSide: const BorderSide(color: Brand.slate300),
  );
  return ThemeData(
    useMaterial3: true,
    colorScheme: scheme,
    scaffoldBackgroundColor: Brand.slate50,
    appBarTheme: const AppBarTheme(
      backgroundColor: Colors.white,
      foregroundColor: Brand.slate900,
      elevation: 0,
      scrolledUnderElevation: 1,
      surfaceTintColor: Colors.transparent,
      titleTextStyle: TextStyle(fontSize: 18, fontWeight: FontWeight.w600, color: Brand.slate900),
    ),
    cardTheme: CardThemeData(
      color: Colors.white,
      elevation: 0,
      margin: EdgeInsets.zero,
      shape: RoundedRectangleBorder(
        borderRadius: BorderRadius.circular(12),
        side: const BorderSide(color: Brand.slate200),
      ),
    ),
    inputDecorationTheme: InputDecorationTheme(
      filled: true,
      fillColor: Colors.white,
      border: border,
      enabledBorder: border,
      focusedBorder: border.copyWith(borderSide: const BorderSide(color: Brand.c600, width: 2)),
      contentPadding: const EdgeInsets.symmetric(horizontal: 12, vertical: 14),
    ),
    filledButtonTheme: FilledButtonThemeData(
      style: FilledButton.styleFrom(
        backgroundColor: Brand.c700,
        minimumSize: const Size.fromHeight(48),
        shape: RoundedRectangleBorder(borderRadius: BorderRadius.circular(8)),
        textStyle: const TextStyle(fontSize: 16, fontWeight: FontWeight.w600),
      ),
    ),
    outlinedButtonTheme: OutlinedButtonThemeData(
      style: OutlinedButton.styleFrom(
        foregroundColor: Brand.slate900,
        minimumSize: const Size.fromHeight(48),
        side: const BorderSide(color: Brand.slate300),
        shape: RoundedRectangleBorder(borderRadius: BorderRadius.circular(8)),
        textStyle: const TextStyle(fontSize: 16, fontWeight: FontWeight.w600),
      ),
    ),
    textButtonTheme: TextButtonThemeData(style: TextButton.styleFrom(foregroundColor: Brand.c700)),
    navigationBarTheme: NavigationBarThemeData(
      backgroundColor: Colors.white,
      indicatorColor: Brand.c100,
      labelTextStyle: WidgetStateProperty.resolveWith(
        (s) => TextStyle(
          fontSize: 12,
          fontWeight: s.contains(WidgetState.selected) ? FontWeight.w600 : FontWeight.w400,
          color: s.contains(WidgetState.selected) ? Brand.c700 : Brand.slate600,
        ),
      ),
    ),
    dividerTheme: const DividerThemeData(color: Brand.slate200, space: 1),
  );
}
