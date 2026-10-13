import 'dart:typed_data';

import 'package:flutter/material.dart';
import 'package:flutter_test/flutter_test.dart';
import 'package:pccontroller_mobile/pakar_reference.dart';
import 'package:pccontroller_mobile/mirroring.dart';
import 'package:pccontroller_mobile/models.dart';

import 'peer_fixture.dart';

class SilentOutput implements ToneOutput {
  final notes = <(int, int)>[];
  int opens = 0, stops = 0;
  @override
  Future<void> open(String session, double volume) async {
    opens++;
  }

  @override
  Future<void> note(String session, int hz, int ms) async {
    notes.add((hz, ms));
  }

  @override
  Future<void> lease(String session) async {}
  @override
  Future<void> stop(String session) async {
    stops++;
  }
}

DeviceStatus mirroredPeer({int session = 1, bool timestamps = true}) =>
    DeviceStatus({
      ...peerFixture().json,
      'board_id': 'board-a',
      'boot_id': 'boot-a',
      'mirroring': {
        'avr_session': session,
        'front_panel': true,
        'buzzer_timestamps': timestamps,
      },
      'front_panel': {
        'menu_page': 2,
        'menu_active': true,
        'running': false,
        'captured': false,
        'meta': 0,
      },
      'active_inputs': 5,
      'active_outputs': 2,
      'input_9': true,
    });
Map<String, dynamic> toneEvent(
  int id,
  int hz,
  int ms, {
  int at = 1000,
  int observed = 1000,
  int session = 1,
}) {
  final bytes = ByteData(13)
    ..setUint16(0, hz, Endian.little)
    ..setUint16(2, ms, Endian.little)
    ..setUint32(5, at, Endian.little)
    ..setUint32(9, observed, Endian.little);
  return {
    'type': 'pc-contract-event',
    'board_id': 'board-a',
    'boot_id': 'boot-a',
    'avr_session': session,
    'event_id': id,
    'response_opcode': 0x9d,
    'payload_hex': bytes.buffer
        .asUint8List()
        .map((b) => b.toRadixString(16).padLeft(2, '0'))
        .join(),
  };
}

void main() {
  test('Actual donor cadence: wraps, delayed service, bounded skew and absolute expiry', () {
    final timeline = ToneTimeline();
    expect(
      timeline.plan(const MirrorTone(440, 100, 0xffffff00, 0xffffff00), 0),
      (50000, 150000),
    );
    expect(timeline.plan(const MirrorTone(660, 80, 0x1d3c0, 0x1d3c0), 145000), (
      170000,
      250000,
    ));
    timeline.clear();
    expect(timeline.plan(const MirrorTone(880, 100, 1000, 91000), 100000), (
      60000,
      160000,
    ));
    expect(timeline.plan(const MirrorTone(880, 100, 2000, 2000), 2000000), (
      2050000,
      2150000,
    ));
    expect(timeline.plan(const MirrorTone(440, 100, 0, 70000000), 0), isNull);
    expect(MirrorTone.decode('xyz'), isNull);
  });
  testWidgets(
    'Exact tone opt-in, duplicate/wrong peer drop, Stop flush and disconnect revoke',
    (tester) async {
      var time = 0;
      final output = SilentOutput(),
          mirror = PeripheralMirror(output: SilentOutput());
      mirror.dispose(); // Idempotent no-audio startup, separate real fixture below.
      final actual = PeripheralMirror(output: output, now: () => time);
      actual.observe(mirroredPeer(), 1);
      actual.event(toneEvent(1, 440, 100));
      expect(output.notes, isEmpty);
      await actual.setBuzzer(true);
      expect(output.opens, 1);
      actual.event(toneEvent(2, 880, 125));
      actual.event(toneEvent(2, 880, 125));
      actual.event({...toneEvent(3, 660, 100), 'board_id': 'other'});
      time = 50000;
      await tester.pump(const Duration(milliseconds: 50));
      expect(output.notes, [(880, 125)]);
      actual.event(toneEvent(4, 440, 100, at: 200000, observed: 200000));
      actual.event(toneEvent(5, 0, 0));
      time = 300000;
      await tester.pump(const Duration(milliseconds: 300));
      expect(output.notes, [(880, 125), (0, 0)]);
      actual.observe(mirroredPeer(session: 2), 1);
      expect(actual.buzzerEnabled, false);
      expect(output.stops, 1);
      actual.event(toneEvent(6, 2000, 100));
      await tester.pump(const Duration(milliseconds: 100));
      expect(output.notes.length, 2);
      actual.observe(null, 1);
      expect(actual.segments, isEmpty);
      expect(actual.panelAvailable, false);
      actual.dispose();
    },
  );
  testWidgets(
    'Front panel/segments independent controls; no capability inference or device writes',
    (tester) async {
      tester.view.physicalSize = const Size(1100, 1800);
      tester.view.devicePixelRatio = 1;
      addTearDown(tester.view.resetPhysicalSize);
      addTearDown(tester.view.resetDevicePixelRatio);
      final output = SilentOutput(),
          mirror = PeripheralMirror(output: SilentOutput());
      mirror.observe(mirroredPeer(timestamps: false), 1);
      await mirror.setBuzzer(true);
      expect(mirror.buzzerEnabled, false);
      await tester.pumpWidget(
        MaterialApp(
          home: Scaffold(
            body: FrontPanelPage(
              status: mirroredPeer(timestamps: false),
              mirror: mirror,
              command: (_, [values = const {}]) async {
                fail('Mirroring must not mutate the board');
              },
            ),
          ),
        ),
      );
      expect(find.text('Live front panel'), findsOneWidget);
      expect(find.byType(SevenSegmentPreview), findsOneWidget);
      mirror.setSegments(false);
      await tester.pump();
      expect(find.byType(SevenSegmentPreview), findsNothing);
      mirror.setPanel(false);
      await tester.pump();
      expect(find.text('Live front panel'), findsNothing);
      expect(output.opens, 0);
      await tester.pumpWidget(const SizedBox());
      mirror.dispose();
    },
  );
  testWidgets(
    'Background and status expiry stop finite audio; foreground never replays',
    (tester) async {
      final output = SilentOutput(),
          mirror = PeripheralMirror(output: SilentOutput());
      mirror.dispose();
      final actual = PeripheralMirror(output: output);
      actual.observe(mirroredPeer(), 1);
      await actual.setBuzzer(true);
      actual.foreground(false);
      expect(actual.buzzerEnabled, false);
      expect(output.stops, 1);
      actual.foreground(true);
      expect(output.opens, 1);
      await actual.setBuzzer(true);
      await tester.pump(const Duration(seconds: 6));
      expect(actual.status, isNull);
      expect(actual.buzzerEnabled, false);
      expect(output.stops, 2);
      actual.dispose();
    },
  );
  testWidgets(
    'Segment events retain decimal masks, require bus ACK and never renew freshness',
    (tester) async {
      final mirror = PeripheralMirror(output: SilentOutput());
      mirror.observe(mirroredPeer(), 1);
      mirror.event({
        ...toneEvent(1, 0, 0),
        'response_opcode': 0x9c,
        'payload_hex': '869180ffc7',
      });
      expect(mirror.segments, [0x86, 0x91, 0x80, 0xff]);
      expect(mirror.brightness, 7);
      mirror.event({
        ...toneEvent(2, 0, 0),
        'response_opcode': 0x9c,
        'payload_hex': '869180ff87',
      });
      expect(mirror.segments, isEmpty);
      await tester.pump(const Duration(seconds: 6));
      expect(mirror.status, isNull);
      // Older connected firmware still supplies its actual segment preview;
      // missing note clock acknowledgment never invents audio availability.
      final old = DeviceStatus({...mirroredPeer().json}..remove('mirroring'));
      mirror.observe(old, 1);
      expect(mirror.segments.length, 4);
      expect(mirror.buzzerAvailable, false);
      mirror.dispose();
    },
  );
}
