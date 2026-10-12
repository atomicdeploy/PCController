import 'dart:async';

import '../device_registry.dart' show randomIdentifier;
import '../mirroring.dart' show ToneOutput, AndroidToneOutput;
import 'transport.dart' show Json, object;

/// Current PCController buzzer.note contract. The selected live connection owns
/// audio; queued notes are never retained across a lost/changed owner.
class BuzzerMirror {
  BuzzerMirror({ToneOutput? output, this.failed})
    : output = output ?? AndroidToneOutput();
  final ToneOutput output;
  final void Function(String)? failed;
  final clock = Stopwatch()..start();
  final pending = <Timer>[];
  Timer? _lease;
  String _owner = '', _generation = '';
  String? _session;
  bool _enabled = false, _opening = false;
  double _volume = .35;
  int _epoch = 0, _lastId = 0;
  int? _device;
  double? _start;

  void configure({
    required String owner,
    required Object? generation,
    required bool enabled,
    required double volume,
  }) {
    if (owner == _owner &&
        '$generation' == _generation &&
        enabled == _enabled &&
        volume == _volume) {
      return;
    }
    stop();
    _owner = owner;
    _generation = '$generation';
    _enabled = enabled;
    _volume = volume;
  }

  Future<void> event(Json event) async {
    if (!_enabled || _owner.isEmpty || event['kind'] != 'buzzer.note') return;
    final data = object(event['metadata']);
    // Bridged source clocks do not belong to the directly selected board.
    if (data.containsKey('bridge.ingress') ||
        '${data['connection_generation']}' != _generation) {
      return;
    }
    final id = event['id'];
    final hz = int.tryParse('${data['frequency_hz']}');
    final ms = int.tryParse('${data['duration_ms']}');
    final micros = data.containsKey('device_micros')
        ? int.tryParse('${data['device_micros']}')
        : null;
    if (id is! int ||
        id <= _lastId ||
        hz == null ||
        ms == null ||
        ms < 0 ||
        ms > 65535 ||
        (hz != 0 && (hz < 20 || hz > 20000 || ms == 0)) ||
        (data.containsKey('device_micros') &&
            (micros == null || micros < 0 || micros > 0xffffffff))) {
      return;
    }
    _lastId = id;
    if (hz == 0 && ms == 0) {
      for (final timer in pending) {
        timer.cancel();
      }
      pending.clear();
      _device = null;
      _start = null;
      final session = _session;
      if (session != null) await _call(output.note(session, 0, 0));
      return;
    }
    final now = clock.elapsedMicroseconds / 1000;
    var start = now + 8;
    if (micros != null) {
      if (_device != null && _start != null) {
        final delta = (micros - _device!) & 0xffffffff;
        final candidate = _start! + delta / 1000;
        if (delta <= 300000000 && (candidate - now).abs() <= 250) {
          start = candidate;
        }
      }
      _device = micros;
      _start = start;
    }
    final end = start + ms, epoch = _epoch;
    if (_session == null) {
      if (_opening) return; // No backlog while the native receiver opens.
      _opening = true;
      final session = randomIdentifier();
      _session = session;
      try {
        await output.open(session, _volume);
        if (epoch != _epoch || !_enabled) {
          await output.stop(session);
          return;
        }
        _lease = Timer.periodic(const Duration(seconds: 2), (_) {
          if (epoch == _epoch) unawaited(_call(output.lease(session)));
        });
      } catch (_) {
        if (epoch == _epoch) _fail();
        return;
      } finally {
        if (epoch == _epoch) _opening = false;
      }
    }
    if (epoch != _epoch || end <= clock.elapsedMicroseconds / 1000) return;
    if (pending.length >= 32) {
      _fail();
      return;
    }
    late final Timer timer;
    timer = Timer(
      Duration(
        microseconds: ((start - clock.elapsedMicroseconds / 1000) * 1000)
            .round()
            .clamp(0, 250000),
      ),
      () {
        pending.remove(timer);
        if (epoch != _epoch || !_enabled || _session == null) return;
        final remaining = (end - clock.elapsedMicroseconds / 1000).ceil();
        if (remaining > 0) {
          unawaited(
            _call(output.note(_session!, hz, remaining.clamp(1, 65535))),
          );
        }
      },
    );
    pending.add(timer);
  }

  Future<void> _call(Future<void> operation) async {
    final epoch = _epoch;
    try {
      await operation;
    } catch (_) {
      if (epoch == _epoch) _fail();
    }
  }

  void _fail() {
    stop();
    failed?.call(
      'Device beep mirroring stopped. Check media volume and enable it again in Settings.',
    );
  }

  void stop() {
    ++_epoch;
    _lease?.cancel();
    _lease = null;
    for (final timer in pending) {
      timer.cancel();
    }
    pending.clear();
    _device = null;
    _start = null;
    _lastId = 0;
    _opening = false;
    final session = _session;
    _session = null;
    if (session != null) {
      unawaited(output.stop(session).catchError((Object _) {}));
    }
  }

  void dispose() {
    _enabled = false;
    stop();
    clock.stop();
  }
}
