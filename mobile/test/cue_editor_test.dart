import 'package:flutter/material.dart';
import 'package:flutter_test/flutter_test.dart';
import 'package:pccontroller_mobile/unified/cue_editor.dart';
import 'package:pccontroller_mobile/unified/transport.dart';

void main() {
  final cue = <String, dynamic>{
    'id': '00000000-0000-4000-8000-000000000001',
    'name': 'Lighting cue',
    'start_time_ms': 1200,
    'duration_ms': 1000,
    'resizable': true,
  };
  Future<void> open(
    WidgetTester tester,
    Json value,
    Future<void> Function(int, int) save,
  ) async {
    await tester.pumpWidget(
      MaterialApp(
        home: Builder(
          builder: (context) => Scaffold(
            body: TextButton(
              onPressed: () => showDialog<void>(
                context: context,
                barrierDismissible: false,
                builder: (_) => CueEditor(cue: value, save: save),
              ),
              child: const Text('Open'),
            ),
          ),
        ),
      ),
    );
    await tester.tap(find.text('Open'));
    await tester.pumpAndSettle();
  }

  test(
    'seconds input keeps exact integer milliseconds and rejects invalid times',
    () {
      expect(cueSecondsToMs('1.2345'), 1235);
      expect(cueSecondsToMs('.001'), 1);
      expect(cueSecondsToMs('9223372036854775.807'), 9223372036854775807);
      expect(cueStartText(9223372036854775807), '9223372036854775.807');
      for (final invalid in [
        '-1',
        'NaN',
        'Infinity',
        '1e400',
        '9223372036854775.808',
      ]) {
        expect(cueSecondsToMs(invalid), null);
      }
    },
  );
  testWidgets('opening or unchanged Save never sends a mutation', (
    tester,
  ) async {
    var calls = 0;
    await open(tester, cue, (_, _) async {
      calls++;
    });
    expect(calls, 0);
    await tester.tap(find.text('Save'));
    await tester.pumpAndSettle();
    expect(calls, 0);
    expect(find.byType(CueEditor), findsNothing);
  });
  testWidgets(
    'invalid duration cannot save and corrected fields send one typed edit',
    (tester) async {
      final calls = <(int, int)>[];
      await open(tester, cue, (start, duration) async {
        calls.add((start, duration));
      });
      await tester.enterText(find.byType(TextFormField).last, '0');
      await tester.tap(find.text('Save'));
      await tester.pumpAndSettle();
      expect(calls, isEmpty);
      expect(
        find.text('Enter a duration from 1 ms to 24 hours'),
        findsOneWidget,
      );
      await tester.enterText(find.byType(TextFormField).first, '2.125');
      await tester.enterText(find.byType(TextFormField).last, '1500');
      await tester.tap(find.text('Save'));
      await tester.pumpAndSettle();
      expect(calls, [(2125, 1500)]);
      expect(find.byType(CueEditor), findsNothing);
    },
  );
  testWidgets(
    'intrinsic duration is read-only and failed acknowledgement keeps edits',
    (tester) async {
      await open(tester, {...cue, 'resizable': false}, (_, _) async {
        throw const Rejected('Acknowledgement unavailable');
      });
      expect(
        tester.widget<TextFormField>(find.byType(TextFormField).last).enabled,
        false,
      );
      await tester.enterText(find.byType(TextFormField).first, '2');
      await tester.tap(find.text('Save'));
      await tester.pumpAndSettle();
      expect(find.text('Acknowledgement unavailable'), findsOneWidget);
      expect(find.byType(CueEditor), findsOneWidget);
      expect(
        tester
            .widget<TextFormField>(find.byType(TextFormField).first)
            .controller!
            .text,
        '2',
      );
    },
  );
  testWidgets(
    'Close confirms dirty edits and Keep editing preserves them without a request',
    (tester) async {
      var calls = 0;
      await open(tester, cue, (_, _) async {
        calls++;
      });
      await tester.enterText(find.byType(TextFormField).first, '2');
      await tester.tap(find.byTooltip('Close'));
      await tester.pumpAndSettle();
      expect(find.text('Unsaved cue'), findsOneWidget);
      await tester.tap(find.text('Keep editing'));
      await tester.pumpAndSettle();
      expect(
        tester
            .widget<TextFormField>(find.byType(TextFormField).first)
            .controller!
            .text,
        '2',
      );
      expect(calls, 0);
    },
  );
}
