import 'package:flutter/material.dart';

/// Power intent is semantic, independent of the user's accent/branding seed.
ButtonStyle relayActionStyle(ColorScheme colors, String presentation, bool on) {
  final tone = on
      ? colors.brightness == Brightness.dark
            ? const Color(0xff55bd83)
            : const Color(0xff16753d)
      : colors.error;
  final filled = presentation == 'filled';
  final tonal = presentation == 'tonal';
  final onTone = ThemeData.estimateBrightnessForColor(tone) == Brightness.light
      ? Colors.black
      : Colors.white;
  return ButtonStyle(
    minimumSize: const WidgetStatePropertyAll(Size(44, 44)),
    padding: const WidgetStatePropertyAll(EdgeInsets.symmetric(horizontal: 12)),
    foregroundColor: WidgetStateProperty.resolveWith(
      (states) => states.contains(WidgetState.disabled)
          ? colors.onSurface.withValues(alpha: .38)
          : filled
          ? onTone
          : tone,
    ),
    backgroundColor: WidgetStateProperty.resolveWith(
      (states) => states.contains(WidgetState.disabled)
          ? filled || tonal
                ? colors.onSurface.withValues(alpha: .10)
                : Colors.transparent
          : filled
          ? tone
          : tonal
          ? tone.withValues(alpha: .14)
          : Colors.transparent,
    ),
    overlayColor: WidgetStatePropertyAll(tone.withValues(alpha: .10)),
    side: WidgetStateProperty.resolveWith(
      (states) => BorderSide(
        color: filled || tonal
            ? Colors.transparent
            : states.contains(WidgetState.disabled)
            ? colors.outlineVariant
            : tone.withValues(alpha: .65),
      ),
    ),
    shape: WidgetStatePropertyAll(
      RoundedRectangleBorder(borderRadius: BorderRadius.circular(8)),
    ),
  );
}
