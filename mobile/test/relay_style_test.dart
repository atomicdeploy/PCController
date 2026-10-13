import 'package:flutter/material.dart';
import 'package:flutter_test/flutter_test.dart';
import 'package:pccontroller_mobile/unified/relay_style.dart';

void main() {
  test(
    'relay power colors remain semantic across accent, brightness and style',
    () {
      for (final seed in [Colors.purple, Colors.blue, Colors.orange]) {
        for (final brightness in Brightness.values) {
          final colors = ColorScheme.fromSeed(
            seedColor: seed,
            brightness: brightness,
          );
          for (final presentation in ['outlined', 'tonal', 'filled']) {
            for (final on in [true, false]) {
              final style = relayActionStyle(colors, presentation, on);
              final tone = on
                  ? brightness == Brightness.dark
                        ? const Color(0xff55bd83)
                        : const Color(0xff16753d)
                  : colors.error;
              if (presentation == 'filled') {
                expect(style.backgroundColor!.resolve({}), tone);
              } else {
                expect(style.foregroundColor!.resolve({}), tone);
              }
              expect(
                style.foregroundColor!.resolve({WidgetState.disabled}),
                colors.onSurface.withValues(alpha: .38),
              );
            }
          }
        }
      }
    },
  );
}
