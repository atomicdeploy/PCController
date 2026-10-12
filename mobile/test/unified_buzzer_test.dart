
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
    await Future<void>.delayed(const Duration(milliseconds: 20));
    expect(audio.opens, 1);
    expect(audio.notes.single.$1, 880);
    expect(audio.notes.single.$2, inInclusiveRange(1, 115));
    await receiver.event(note(1));
    expect(audio.notes.length, 1);
    await receiver.event(note(2));
    await receiver.event(note(3, hz: 0, ms: 0));
    await Future<void>.delayed(const Duration(milliseconds: 30));
    expect(audio.notes.last, (0, 0));
    expect(audio.notes.where((n) => n.$1 == 880).length, 1);
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
