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
                final foreground = style.foregroundColor!.resolve({})!;
                final a = foreground.computeLuminance(),
                    b = tone.computeLuminance();
                expect(
                  a > b ? (a + .05) / (b + .05) : (b + .05) / (a + .05),
                  greaterThanOrEqualTo(4.5),
                );
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
  test('mid-luminance semantic fill chooses readable text', () {
    final colors = ColorScheme.fromSeed(seedColor: Colors.blue)
        .copyWith(error: const Color(0xffef6461));
    final style = relayActionStyle(colors, 'filled', false);
    expect(style.foregroundColor!.resolve({}), Colors.black);
  });
}
