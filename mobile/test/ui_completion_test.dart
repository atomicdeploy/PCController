import 'dart:async';

import 'package:flutter/material.dart';
import 'package:flutter_test/flutter_test.dart';
import 'package:shared_preferences/shared_preferences.dart';
import 'package:pccontroller_mobile/main.dart';
import 'package:pccontroller_mobile/unified/effect_editor.dart';
import 'package:pccontroller_mobile/unified/cue_editor.dart';
import 'package:pccontroller_mobile/unified/resource_target.dart';
import 'package:pccontroller_mobile/unified/transport.dart';
import 'package:pccontroller_mobile/unified/workspace_chrome.dart';

final effect = <String, dynamic>{
  'id': 'sequence-a',
  'name': 'Arrival',
  'kind': 'sequence',
  'category': 'Old folder',
  'steps': [
    {'kind': 'buzzer', 'at_us': 0, 'duration_ms': 115, 'frequency_hz': 880},
    {'kind': 'display', 'at_us': 115000, 'duration_ms': 1000, 'text': 'HELLO'},
  ],
};
Finder field(String label) => find.ancestor(
  of: find.byWidgetPredicate(
    (widget) => widget is TextField && widget.decoration?.labelText == label,
  ),
  matching: find.byType(TextFormField),
);
Future<void> openEditor(
  WidgetTester tester,
  Future<void> Function(Json) save,
) async {
  await tester.pumpWidget(
    MaterialApp(
      home: Builder(
        builder: (context) => Scaffold(
          body: TextButton(
            onPressed: () => showDialog<void>(
              context: context,
              barrierDismissible: false,
              builder: (_) => EffectEditor(
                initial: effect,
                folders: const ['Lighting'],
                saved: save,
              ),
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

void main() {
  test('resource authority normalization preserves media path/query and native paths', () {
    final target = Uri.parse(
      normalizeMediaTarget(' HTTPS://HOST:۸۰۸۰/فیلم۲.mp4?q=۳ '),
    );
    expect(target.host, 'host');
    expect(target.port, 8080);
    expect(Uri.decodeComponent(target.path), '/فیلم۲.mp4');
    expect(target.queryParameters['q'], '۳');
    expect(normalizeMediaTarget(r'C:\Media\فیلم۲.mp4'), r'C:\Media\فیلم۲.mp4');
    expect(
      normalizeMediaTarget('rtsp://[::1]:8554/live'),
      'rtsp://[::1]:8554/live',
    );
    for (final value in [
      'http:/bad',
      'https://',
      'http://host:99999/video',
      'https://user:secret@host/video',
      'http://bad host/a',
      'x\ny',
    ]) {
      expect(mediaTargetError(value), isNotNull, reason: value);
    }
  });
  test('native timing/repetition constraints validate without clipping authored values', () {
    expect(effectDefinitionError(effect), null);
    expect(effectSpanUs(effect['steps'] as List<Json>), 1115000);
    expect(
      effectDefinitionError({
        ...effect,
        'steps': [
          {'kind': 'buzzer', 'duration_ms': 0},
        ],
      }),
      isNotNull,
    );
    expect(
      effectDefinitionError({
        ...effect,
        'steps': [
          {'kind': 'buzzer', 'duration_ms': 100, 'repeat_count': 1001},
        ],
      }),
      isNotNull,
    );
    expect(
      effectDefinitionError({
        ...effect,
        'steps': [
          {'kind': 'display', 'text': 'متن'},
        ],
      }),
      isNotNull,
    );
    expect(
      effectDefinitionError({
        ...effect,
        'steps': [
          {'kind': 'rgb', 'red': 'oops'},
        ],
      }),
      isNotNull,
    );
    expect(
      effectDefinitionError({
        ...effect,
        'steps': [
          {
            'kind': 'display',
            'text': 'OK',
            'at_us': 2147483640,
            'duration_ms': 1,
          },
        ],
      }),
      isNotNull,
    );
    expect(
      effectSpanUs([
        {
          'at_us': 0,
          'duration_ms': 100,
          'repeat_count': 3,
          'repeat_interval_ms': 0,
        },
      ]),
      300000,
    );
  });
  testWidgets(
    'folder picker retains legacy identity and unchanged Save sends nothing',
    (tester) async {
      var calls = 0;
      await openEditor(tester, (_) async {
        calls++;
      });
      expect(find.byType(DropdownButtonFormField<String>), findsOneWidget);
      expect(find.text('Old folder'), findsOneWidget);
      expect(field('Folder'), findsNothing);
      await tester.tap(find.text('Save local draft'));
      await tester.pumpAndSettle();
      expect(calls, 0);
      expect(find.byType(EffectEditor), findsNothing);
    },
  );
  testWidgets(
    'invalid numeric edits stay visible across steps and cannot save old values',
    (tester) async {
      var calls = 0;
      await openEditor(tester, (_) async {
        calls++;
      });
      await tester.ensureVisible(find.widgetWithText(ChoiceChip, '1 · buzzer'));
      await tester.pumpAndSettle();
      await tester.tap(find.widgetWithText(ChoiceChip, '1 · buzzer'));
      await tester.pumpAndSettle();
      await tester.ensureVisible(field('Duration (ms)'));
      await tester.enterText(field('Duration (ms)'), '-5');
      await tester.pump();
      await tester.ensureVisible(
        find.widgetWithText(ChoiceChip, '2 · display'),
      );
      await tester.tap(find.widgetWithText(ChoiceChip, '2 · display'));
      await tester.pumpAndSettle();
      await tester.tap(find.text('Save local draft'));
      await tester.pumpAndSettle();
      expect(calls, 0);
      await tester.ensureVisible(find.widgetWithText(ChoiceChip, '1 · buzzer'));
      await tester.tap(find.widgetWithText(ChoiceChip, '1 · buzzer'));
      await tester.pumpAndSettle();
      expect(
        tester.widget<TextFormField>(field('Duration (ms)')).initialValue,
        '-5',
      );
      await tester.ensureVisible(field('Duration (ms)'));
      await tester.enterText(field('Duration (ms)'), '120');
      await tester.tap(find.text('Save local draft'));
      await tester.pumpAndSettle();
      expect(calls, 1);
    },
  );
  testWidgets(
    'cue loading keeps modal geometry and close cannot interrupt an outstanding save',
    (tester) async {
      final pending = Completer<void>();
      await tester.pumpWidget(
        MaterialApp(
          home: Builder(
            builder: (context) => Scaffold(
              body: TextButton(
                onPressed: () => showDialog<void>(
                  context: context,
                  barrierDismissible: false,
                  builder: (_) => CueEditor(
                    cue: {
                      'name': 'Cue',
                      'start_time_ms': 1000,
                      'duration_ms': 100,
                      'resizable': true,
                    },
                    save: (_, _) => pending.future,
                  ),
                ),
                child: const Text('Open'),
              ),
            ),
          ),
        ),
      );
      await tester.tap(find.text('Open'));
      await tester.pumpAndSettle();
      await tester.enterText(field('Start (seconds)'), '2');
      await tester.pump();
      final before = tester.getRect(find.byType(Dialog));
      await tester.tap(find.text('Save'));
      await tester.pump(const Duration(milliseconds: 50));
      expect(tester.getRect(find.byType(Dialog)), before);
      expect(
        tester
            .widget<IconButton>(
              find.byWidgetPredicate(
                (w) => w is IconButton && w.tooltip == 'Close',
              ),
            )
            .onPressed,
        null,
      );
      pending.complete();
      await tester.pumpAndSettle();
      expect(find.byType(CueEditor), findsNothing);
    },
  );
  testWidgets(
    'unapplied technical JSON disables numeric edits and cannot silently save the old descriptor',
    (tester) async {
      var calls = 0;
      await openEditor(tester, (_) async {
        calls++;
      });
      await tester.ensureVisible(find.widgetWithText(ChoiceChip, '1 · buzzer'));
      await tester.pumpAndSettle();
      await tester.tap(find.widgetWithText(ChoiceChip, '1 · buzzer'));
      await tester.pumpAndSettle();
      await tester.ensureVisible(find.text('Technical definition'));
      await tester.tap(find.text('Technical definition'));
      await tester.pumpAndSettle();
      final raw = find.byWidgetPredicate(
        (widget) =>
            widget is TextField &&
            widget.decoration?.labelText == 'Effect JSON',
      );
      await tester.ensureVisible(raw);
      await tester.enterText(raw, '{}');
      await tester.pump();
      expect(
        tester.widget<TextFormField>(field('Duration (ms)')).enabled,
        false,
      );
      await tester.tap(find.text('Save local draft'));
      await tester.pumpAndSettle();
      expect(calls, 0);
      await tester.ensureVisible(find.text('Revert technical edits'));
      await tester.tap(find.text('Revert technical edits'));
      await tester.pumpAndSettle();
      expect(
        tester.widget<TextFormField>(field('Duration (ms)')).enabled,
        true,
      );
    },
  );
  testWidgets(
    'failed local save retains edited name and all fields stay disabled while pending',
    (tester) async {
      final pending = Completer<void>();
      await openEditor(tester, (_) => pending.future);
      await tester.enterText(field('Name'), 'Arrival edited');
      await tester.tap(find.text('Save local draft'));
      await tester.pump();
      expect(tester.widget<TextFormField>(field('Name')).enabled, false);
      pending.completeError(const Rejected('Save failed'));
      await tester.pumpAndSettle();
      expect(find.text('Save failed'), findsOneWidget);
      expect(
        tester.widget<TextFormField>(field('Name')).controller!.text,
        'Arrival edited',
      );
      expect(tester.widget<TextFormField>(field('Name')).enabled, true);
      expect(find.text('Old folder'), findsOneWidget);
    },
  );
  testWidgets(
    '320px compact connections retain one pill and lifecycle control per peer',
    (tester) async {
      SharedPreferences.setMockInitialValues({});
      tester.view.physicalSize = const Size(320, 700);
      tester.view.devicePixelRatio = 1;
      addTearDown(tester.view.resetPhysicalSize);
      addTearDown(tester.view.resetDevicePixelRatio);
      await tester.pumpWidget(const UnifiedControllerApp(restore: false));
      expect(find.byTooltip('Connect PCController'), findsOneWidget);
      expect(find.byTooltip('Connect Pealayer'), findsOneWidget);
      expect(find.text('Offline'), findsNWidgets(2));
      expect(tester.takeException(), null);
      await tester.pumpWidget(const SizedBox());
    },
  );
  testWidgets('selected activity scrolls into view and short RTL rails fit', (
    tester,
  ) async {
    var selected = 0;
    late StateSetter change;
    await tester.pumpWidget(
      MaterialApp(
        home: StatefulBuilder(
          builder: (context, setState) {
            change = setState;
            return Scaffold(
              body: SizedBox(
                width: 320,
                child: ActivityStrip(selected: selected, changed: (_) {}),
              ),
            );
          },
        ),
      ),
    );
    change(() => selected = 6);
    await tester.pumpAndSettle();
    expect(
      tester
          .getRect(find.text('Events'))
          .overlaps(const Rect.fromLTWH(0, 0, 320, 600)),
      true,
    );
    await tester.pumpWidget(
      MaterialApp(
        home: Directionality(
          textDirection: TextDirection.rtl,
          child: SizedBox(
            height: 160,
            child: ActivityStrip(selected: 6, changed: (_) {}, vertical: true),
          ),
        ),
      ),
    );
    await tester.pumpAndSettle();
    expect(tester.takeException(), null);
  });
}
