import 'dart:async';

import 'package:flutter_test/flutter_test.dart';
import 'package:pccontroller_mobile/mirroring.dart';
import 'package:pccontroller_mobile/unified/buzzer_mirror.dart';

class RecordingAudio implements ToneOutput {
  final notes = <(int, int)>[];
  final stops = <String>[];
  int opens = 0;
  @override
  Future<void> open(String session, double volume) async {
    opens++;
  }

  @override
  Future<void> note(String session, int frequency, int durationMs) async {
    notes.add((frequency, durationMs));
  }

  @override
  Future<void> lease(String session) async {}
  @override
  Future<void> stop(String session) async {
    stops.add(session);
  }
}

class ScheduledAudio extends RecordingAudio implements ScheduledToneOutput {
  ScheduledAudio({this.openGate});
  final Completer<void>? openGate;
  final scheduled = <(int, int, int)>[];
  @override
  Future<void> open(String session, double volume) async {
    opens++;
    await openGate?.future;
  }

  @override
  Future<void> schedule(String session, int hz, int ms, int delayMicros) async {
    scheduled.add((hz, ms, delayMicros));
  }
}

Map<String, dynamic> note(
  int id, {
  int hz = 880,
  int ms = 115,
  int generation = 7,
}) => {
  'id': id,
  'kind': 'buzzer.note',
  'metadata': {
    'frequency_hz': '$hz',
    'duration_ms': '$ms',
    'connection_generation': '$generation',
    'device_micros': '${id * 200000}',
  },
};

void main() {
  test('actual note contract reaches finite native PCM once; Stop cancels pending notes', () async {
    final audio = RecordingAudio();
    final receiver = BuzzerMirror(output: audio);
    addTearDown(receiver.dispose);
    receiver.configure(
      owner: 'selected-host',
      generation: 7,
      enabled: true,
      volume: .35,
    );
    await receiver.event(note(1));
    await Future<void>.delayed(const Duration(milliseconds: 65));
    expect(audio.opens, 1);
    expect(audio.notes.single.$1, 880);
    expect(audio.notes.single.$2, 115);
    await receiver.event(note(1));
    expect(audio.notes.length, 1);
    await receiver.event(note(2));
    await receiver.event(note(3, hz: 0, ms: 0));
    await Future<void>.delayed(const Duration(milliseconds: 30));
    expect(audio.notes.last, (0, 0));
    expect(audio.notes.where((n) => n.$1 == 880).length, 1);
  });

  test(
    'startup burst waits for native open and rebases all followers equally',
    () async {
      final gate = Completer<void>();
      // Keep one receiver initialization in flight while multiple notes arrive.
      final waiting = ScheduledAudio(openGate: gate);
      var now = 0;
      final mirror = BuzzerMirror(output: waiting, nowMicros: () => now);
      addTearDown(mirror.dispose);
      mirror.configure(
        owner: 'selected',
        generation: 7,
        enabled: true,
        volume: .35,
      );
      final first = note(1, ms: 1), second = note(2, hz: 1200, ms: 3);
      (first['metadata'] as Map)['device_micros'] = '4294967290';
      (second['metadata'] as Map)['device_micros'] =
          '9994'; // uint32 wrap, 10ms.
      final a = mirror.event(first), b = mirror.event(second);
      expect(waiting.opens, 1);
      expect(waiting.scheduled, isEmpty);
      now = 90000; // Open missed both original slots, but receipts are fresh.
      gate.complete();
      await Future.wait([a, b]);
      expect(waiting.scheduled, [(880, 1, 0), (1200, 3, 10000)]);
    },
  );

  test('stalled open never replays stale notes', () async {
    final gate = Completer<void>();
    var now = 0;
    final waiting = ScheduledAudio(openGate: gate);
    final mirror = BuzzerMirror(output: waiting, nowMicros: () => now);
    addTearDown(mirror.dispose);
    mirror.configure(
      owner: 'selected',
      generation: 7,
      enabled: true,
      volume: .35,
    );
    final opening = mirror.event(note(1, ms: 1));
    now = 400000;
    gate.complete();
    await opening;
    expect(waiting.scheduled, isEmpty);
  });

  test(
    'Stop during initialization drops the burst but retains enabled receiver',
    () async {
      final gate = Completer<void>();
      final audio = ScheduledAudio(openGate: gate);
      var now = 0;
      final mirror = BuzzerMirror(output: audio, nowMicros: () => now);
      addTearDown(mirror.dispose);
      mirror.configure(
        owner: 'selected',
        generation: 7,
        enabled: true,
        volume: .35,
      );
      final opening = mirror.event(note(1));
      await mirror.event(note(2, hz: 0, ms: 0));
      gate.complete();
      await opening;
      expect(audio.scheduled, isEmpty);
      now = 100000;
      await mirror.event(note(3, ms: 1));
      expect(audio.opens, 1);
      expect(audio.scheduled, [(880, 1, 50000)]);
      await mirror.event(note(4, hz: 0, ms: 0));
      expect(audio.notes, [(0, 0)]);
    },
  );

  testWidgets('late timer preserves short duration and following cadence', (
    tester,
  ) async {
    final audio = RecordingAudio();
    var now = 0;
    final mirror = BuzzerMirror(output: audio, nowMicros: () => now);
    addTearDown(mirror.dispose);
    mirror.configure(
      owner: 'selected',
      generation: 7,
      enabled: true,
      volume: .35,
    );
    final a = note(1, ms: 1), b = note(2, hz: 1200, ms: 3);
    (a['metadata'] as Map)['device_micros'] = '1000';
    (b['metadata'] as Map)['device_micros'] = '11000';
    await mirror.event(a);
    await mirror.event(b);
    now = 90000;
    await tester.pump(const Duration(milliseconds: 50));
    expect(audio.notes, [(880, 1)]);
    now = 100000;
    await tester.pump(const Duration(milliseconds: 10));
    expect(audio.notes, [(880, 1), (1200, 3)]);
    mirror.dispose();
  });

  test('owner change while opening cannot emit or lease old notes', () async {
    final gate = Completer<void>();
    final audio = ScheduledAudio(openGate: gate);
    final mirror = BuzzerMirror(output: audio);
    addTearDown(mirror.dispose);
    mirror.configure(
      owner: 'selected',
      generation: 7,
      enabled: true,
      volume: .35,
    );
    final opening = mirror.event(note(1));
    mirror.configure(
      owner: 'replacement',
      generation: 8,
      enabled: true,
      volume: .35,
    );
    gate.complete();
    await opening;
    expect(audio.scheduled, isEmpty);
    expect(audio.stops, isNotEmpty);
  });
  test(
    'wrong generation, bridge source and malformed notes do not open audio',
    () async {
      final audio = RecordingAudio();
      final mirror = BuzzerMirror(output: audio);
      addTearDown(mirror.dispose);
      mirror.configure(
        owner: 'selected-host',
        generation: 7,
        enabled: true,
        volume: .35,
      );
      await mirror.event(note(1, generation: 6));
      final bridged = note(2);
      (bridged['metadata'] as Map)['bridge.ingress'] = 'another-board';
      await mirror.event(bridged);
      await mirror.event(note(3, hz: 40000));
      await mirror.event(note(4, ms: 0));
      expect(audio.opens, 0);
    },
  );
  test('disconnect cancels queued audio and new owner cannot replay old generation', () async {
    final audio = RecordingAudio();
    final mirror = BuzzerMirror(output: audio);
    addTearDown(mirror.dispose);
    mirror.configure(
      owner: 'selected-host',
      generation: 7,
      enabled: true,
      volume: .35,
    );
    await mirror.event(note(1));
    mirror.configure(owner: '', generation: null, enabled: false, volume: .35);
    await Future<void>.delayed(const Duration(milliseconds: 20));
    expect(audio.notes, isEmpty);
    expect(audio.stops, hasLength(1));
    mirror.configure(
      owner: 'new-host',
      generation: 8,
      enabled: true,
      volume: .35,
    );
    await mirror.event(note(2));
    expect(audio.opens, 1);
  });
}
