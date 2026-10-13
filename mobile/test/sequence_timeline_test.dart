import 'package:flutter/material.dart';
import 'package:flutter_test/flutter_test.dart';
import 'package:pccontroller_mobile/unified/sequence_timeline.dart';
import 'package:pccontroller_mobile/unified/transport.dart';

void main() {
  Widget screen(List<Json> steps, void Function(int, int, int?) edit) =>
      MaterialApp(
        home: Scaffold(
          body: SizedBox(
            width: 640,
            child: SequenceTimeline(
              steps: steps,
              selected: 0,
              select: (_) {},
              edit: edit,
            ),
          ),
        ),
      );
  final step = <String, dynamic>{
    'kind': 'display',
    'at_us': 200000,
    'duration_ms': 1000,
    'text': 'Example',
    'unknown_metadata': {'preserve': true},
  };

  testWidgets('drag commits one snapped start edit without altering duration', (
    tester,
  ) async {
    final changes = <(int, int, int?)>[];
    await tester.pumpWidget(
      screen([step], (i, at, duration) => changes.add((i, at, duration))),
    );
    final gesture = await tester.startGesture(
      tester.getCenter(find.byKey(const ValueKey('timeline-step-0'))),
    );
    await gesture.moveBy(const Offset(20, 0));
    await tester.pump();
    await gesture.moveBy(const Offset(80, 0));
    await tester.pump();
    expect(changes, isEmpty);
    await gesture.up();
    await tester.pump();
    expect(changes, hasLength(1));
    expect(changes.single.$2, greaterThan(200000));
    expect(changes.single.$2 % 10000, 0);
    expect(changes.single.$3, null);
    expect(step['unknown_metadata'], {'preserve': true});
    expect(tester.takeException(), null);
  });
  testWidgets(
    'duration resize keeps the original start and produces one edit',
    (tester) async {
      final changes = <(int, int, int?)>[];
      await tester.pumpWidget(
        screen([step], (i, at, duration) => changes.add((i, at, duration))),
      );
      final gesture = await tester.startGesture(
        tester.getCenter(find.byKey(const ValueKey('timeline-resize-0'))),
      );
      await gesture.moveBy(const Offset(-20, 0));
      await tester.pump();
      await gesture.moveBy(const Offset(-80, 0));
      await tester.pump();
      await gesture.up();
      await tester.pump();
      expect(changes, hasLength(1));
      expect(changes.single.$2, 200000);
      expect(changes.single.$3, inInclusiveRange(1, 990));
      expect(changes.single.$3! % 10, 0);
      expect(tester.takeException(), null);
    },
  );
  testWidgets(
    'descriptor changed during drag cancels instead of editing replacement',
    (tester) async {
      final changes = <(int, int, int?)>[];
      void record(int i, int at, int? duration) =>
          changes.add((i, at, duration));
      await tester.pumpWidget(screen([step], record));
      final gesture = await tester.startGesture(
        tester.getCenter(find.byKey(const ValueKey('timeline-step-0'))),
      );
      await gesture.moveBy(const Offset(20, 0));
      await tester.pump();
      await gesture.moveBy(const Offset(50, 0));
      await tester.pump();
      await tester.pumpWidget(
        screen([
          {...step, 'text': 'Another step'},
        ], record),
      );
      await gesture.up();
      await tester.pump();
      expect(changes, isEmpty);
      expect(tester.takeException(), null);
    },
  );
  testWidgets('removing an identical step during a drag cancels its index', (
    tester,
  ) async {
    final changes = <(int, int, int?)>[];
    void record(int i, int at, int? duration) => changes.add((i, at, duration));
    await tester.pumpWidget(screen([step, step], record));
    final gesture = await tester.startGesture(
      tester.getCenter(find.byKey(const ValueKey('timeline-step-0'))),
    );
    await gesture.moveBy(const Offset(20, 0));
    await tester.pump();
    await gesture.moveBy(const Offset(50, 0));
    await tester.pump();
    await tester.pumpWidget(screen([step], record));
    await gesture.up();
    await tester.pump();
    expect(changes, isEmpty);
  });
  for (final width in [288.0, 920.0]) {
    testWidgets('timeline zoom and repeat previews fit width $width', (
      tester,
    ) async {
      tester.view.physicalSize = Size(width + 32, 800);
      tester.view.devicePixelRatio = 1;
      addTearDown(tester.view.resetPhysicalSize);
      addTearDown(tester.view.resetDevicePixelRatio);
      await tester.pumpWidget(
        MaterialApp(
          home: Scaffold(
            body: SizedBox(
              width: width,
              child: SequenceTimeline(
                steps: [
                  {...step, 'repeat_count': 3, 'repeat_interval_ms': 1500},
                ],
                selected: 0,
                select: (_) {},
                edit: (_, _, _) {},
              ),
            ),
          ),
        ),
      );
      await tester.tap(find.byTooltip('Zoom in'));
      await tester.pumpAndSettle();
      await tester.tap(find.text('Fit'));
      await tester.pumpAndSettle();
      expect(tester.takeException(), null);
    });
  }
}
