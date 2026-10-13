import 'dart:async';

import 'package:flutter/material.dart';
import 'package:flutter_test/flutter_test.dart';
import 'package:pccontroller_mobile/unified/pwm_control.dart';

void main() {
  testWidgets(
    'drag and release keep pending thumb through old readings; one commit uses actual readback',
    (tester) async {
      final receipt = Completer<double>(), values = <int>[];
      Widget frame(double observed, {bool enabled = true}) => MaterialApp(
        home: Scaffold(
          body: SizedBox(
            width: 320,
            child: PwmSetpointControl(
              observed: observed,
              enabled: enabled,
              commit: (percent) {
                values.add(percent);
                return receipt.future;
              },
            ),
          ),
        ),
      );
      await tester.pumpWidget(frame(10));
      final gesture = await tester.startGesture(
        tester.getCenter(find.byType(Slider)),
      );
      await gesture.moveBy(const Offset(45, 0));
      await tester.pump();
      final preview = tester.widget<Slider>(find.byType(Slider)).value;
      await tester.pumpWidget(frame(15));
      expect(tester.widget<Slider>(find.byType(Slider)).value, preview);
      await gesture.up();
      await tester.pump();
      expect(values, [preview.round()]);
      await tester.pumpWidget(frame(20, enabled: false));
      expect(
        tester.widget<Slider>(find.byType(Slider)).value,
        preview.round().toDouble(),
      );
      expect(find.byType(CircularProgressIndicator), findsOneWidget);
      receipt.complete(59.8);
      await tester.pump();
      expect(tester.widget<Slider>(find.byType(Slider)).value, 59.8);
      expect(values, hasLength(1));
      await tester.pumpWidget(frame(59.8));
      expect(find.byType(CircularProgressIndicator), findsNothing);
      await tester.pumpWidget(const SizedBox());
    },
  );
  testWidgets(
    'uncertain receipt returns to observed value and does not replay',
    (tester) async {
      var calls = 0;
      await tester.pumpWidget(
        MaterialApp(
          home: Scaffold(
            body: PwmSetpointControl(
              observed: 24,
              enabled: true,
              commit: (_) async {
                calls++;
                throw StateError('PWM readback unavailable');
              },
            ),
          ),
        ),
      );
      await tester.drag(find.byType(Slider), const Offset(70, 0));
      await tester.pumpAndSettle();
      expect(calls, 1);
      expect(tester.widget<Slider>(find.byType(Slider)).value, 24);
      expect(find.textContaining('PWM readback unavailable'), findsOneWidget);
      await tester.pumpWidget(const SizedBox());
    },
  );
  testWidgets('unknown level is an empty bounded input, never a zero thumb', (
    tester,
  ) async {
    final values = <int>[];
    await tester.pumpWidget(
      MaterialApp(
        home: Scaffold(
          body: PwmSetpointControl(
            enabled: true,
            commit: (v) async {
              values.add(v);
              return v.toDouble();
            },
          ),
        ),
      ),
    );
    expect(find.byType(Slider), findsNothing);
    expect(find.text('Setpoint 0%'), findsNothing);
    await tester.enterText(find.byType(TextFormField), '101');
    await tester.tap(find.byTooltip('Apply PWM setpoint'));
    await tester.pump();
    expect(values, isEmpty);
    await tester.enterText(find.byType(TextFormField), '42');
    await tester.tap(find.byTooltip('Apply PWM setpoint'));
    await tester.pump();
    expect(values, [42]);
    expect(tester.widget<Slider>(find.byType(Slider)).value, 42);
    await tester.pumpWidget(const SizedBox());
  });
  testWidgets('replacement owner ignores an old pending receipt', (
    tester,
  ) async {
    final receipt = Completer<double>();
    var calls = 0;
    Widget frame(String owner, double observed) => MaterialApp(
      home: Scaffold(
        body: PwmSetpointControl(
          key: ValueKey(owner),
          observed: observed,
          enabled: true,
          commit: (_) {
            calls++;
            return receipt.future;
          },
        ),
      ),
    );
    await tester.pumpWidget(frame('first', 10));
    await tester.drag(find.byType(Slider), const Offset(70, 0));
    await tester.pump();
    await tester.pumpWidget(frame('second', 35));
    receipt.complete(70);
    await tester.pump();
    expect(calls, 1);
    expect(tester.widget<Slider>(find.byType(Slider)).value, 35);
    expect(tester.takeException(), null);
    await tester.pumpWidget(const SizedBox());
  });
}
