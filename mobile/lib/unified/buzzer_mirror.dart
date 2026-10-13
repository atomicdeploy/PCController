import 'dart:async';

import '../device_registry.dart' show randomIdentifier;
import '../mirroring.dart'
    show ToneOutput, ScheduledToneOutput, AndroidToneOutput;
import 'transport.dart' show Json, object;

class _QueuedTone {
  _QueuedTone(this.hz, this.ms, this.start, this.observed);
  final int hz, ms, observed;
  int start;
}

/// Selected-connection audio only. Like Linux, fresh late notes retain their
/// duration; stale observations and previous owners never replay.
class BuzzerMirror {
  BuzzerMirror({ToneOutput? output, this.failed, this.nowMicros})
    : output = output ?? AndroidToneOutput();
  final ToneOutput output;
  final void Function(String)? failed;
  final int Function()? nowMicros;
  final clock = Stopwatch()..start();
  final _pending = <_QueuedTone>[];
  Timer? _lease, _timer;
  String _owner = '', _generation = '';
  String? _session;
  Future<void>? _opening;
  bool _enabled = false, _ready = false;
  double _volume = .35;
  int _epoch = 0, _lastId = 0, _notesEpoch = 0;
  int? _device, _start;
  int get _now => nowMicros?.call() ?? clock.elapsedMicroseconds;

  void configure({
    required String owner,
    required Object? generation,
    required bool enabled,
    required double volume,
  }) {
    if (owner == _owner &&
        generation.toString() == _generation &&
        enabled == _enabled &&
        volume == _volume) {
      return;
    }
    stop();
    _owner = owner;
    _generation = generation.toString();
    _enabled = enabled;
    _volume = volume;
  }

  Future<void> event(Json event) async {
    final observed = _now;
    if (!_enabled || _owner.isEmpty || event['kind'] != 'buzzer.note') return;
    final data = object(event['metadata']);
    // Bridged sources need an explicit selected-source identity, not merely a
    // timestamp or a coincidentally equal connection generation.
    if (data.containsKey('bridge.ingress') ||
        data['connection_generation'].toString() != _generation) {
      return;
    }
    final id = event['id'];
    final hz = int.tryParse(data['frequency_hz'].toString());
    final ms = int.tryParse(data['duration_ms'].toString());
    final micros = data.containsKey('device_micros')
        ? int.tryParse(data['device_micros'].toString())
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
      ++_notesEpoch;
      _timer?.cancel();
      _pending.clear();
      _device = _start = null;
      // In-flight open has received no notes. Clear the queue instead of
      // racing a Stop against native initialization.
      if (_ready && _session != null) {
        await _call(output.note(_session!, 0, 0), _epoch);
      }
      return;
    }
    if (_pending.length >= 32) {
      _fail();
      return;
    }
    var start = observed + 50000;
    if (micros != null) {
      if (_device != null && _start != null) {
        final delta = (micros - _device!) & 0xffffffff;
        final candidate = _start! + delta;
        if (delta <= 300000000 &&
            (candidate - observed).abs() <= 250000 &&
            candidate >= observed) {
          start = candidate;
        }
      }
      _device = micros;
      _start = start;
    }
    _pending.add(_QueuedTone(hz, ms, start, observed));
    final epoch = _epoch;
    if (!_ready) await (_opening ??= _open(epoch));
    if (epoch == _epoch && _ready) _dispatch();
  }

  Future<void> _open(int epoch) async {
    final session = randomIdentifier();
    _session = session;
    try {
      await output.open(session, _volume);
      if (epoch != _epoch || !_enabled) {
        await output.stop(session);
        return;
      }
      _ready = true;
      _lease = Timer.periodic(const Duration(seconds: 2), (_) {
        if (epoch == _epoch) unawaited(_call(output.lease(session), epoch));
      });
    } catch (_) {
      if (epoch == _epoch) _fail();
    }
  }

  void _dispatch() {
    _timer?.cancel();
    final epoch = _epoch, notesEpoch = _notesEpoch;
    while (_pending.isNotEmpty && _ready && _session != null) {
      final tone = _pending.first, now = _now;
      // A suspended receiver cannot replay an old melody after waking.
      if (now - tone.observed > 250000 && tone.start < now) {
        _pending.removeAt(0);
        continue;
      }
      if (tone.start < now) {
        final late = now - tone.start;
        for (final queued in _pending) {
          queued.start += late;
        }
        if (_start != null) _start = _start! + late;
      }
      final delay = (tone.start - now).clamp(0, 250000);
      final receiver = output;
      if (receiver is ScheduledToneOutput) {
        _pending.removeAt(0);
        unawaited(
          _call(receiver.schedule(_session!, tone.hz, tone.ms, delay), epoch),
        );
      } else if (delay > 0) {
        _timer = Timer(Duration(microseconds: delay), () {
          if (epoch == _epoch && notesEpoch == _notesEpoch) _dispatch();
        });
        return;
      } else {
        _pending.removeAt(0);
        unawaited(_call(output.note(_session!, tone.hz, tone.ms), epoch));
      }
    }
  }

  Future<void> _call(Future<void> operation, int epoch) async {
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
    ++_notesEpoch;
    _lease?.cancel();
    _lease = null;
    _timer?.cancel();
    _timer = null;
    _pending.clear();
    _device = _start = null;
    _lastId = 0;
    _ready = false;
    _opening = null;
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
