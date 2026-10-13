import 'dart:math' as math;

import 'package:flutter/material.dart';

import 'transport.dart' show Json, object, objects;

// Trim only the outer theme leading so visible text groups center beside icons.
// Wrapped lines retain their internal line spacing and accessibility scaling.
const cardTextHeightBehavior = TextHeightBehavior(
  applyHeightToFirstAscent: false,
  applyHeightToLastDescent: false,
);

class TelemetryPoint {
  TelemetryPoint(this.time, this.values);
  final DateTime time;
  final Map<String, double?> values;
}

class TelemetryHistory {
  final points = <TelemetryPoint>[];
  String owner = '';
  void record(Json snapshot) {
    final nextOwner =
        '${snapshot['host_instance_id']}/${snapshot['connection_generation']}';
    if (owner != nextOwner) {
      points.clear();
      owner = nextOwner;
    }
    final time = DateTime.tryParse('${snapshot['status_updated']}');
    if (snapshot['have_status'] != true ||
        time == null ||
        snapshot['connected'] != true) {
      return;
    }
    if (points.isNotEmpty &&
        time.difference(points.last.time).inMilliseconds < 500) {
      return;
    }
    final status = object(snapshot['status']);
    double? read(
      String key,
      String available,
      double scale,
      double min,
      double max,
    ) {
      final value = status[key];
      return status[available] == true &&
              value is num &&
              value.isFinite &&
              value >= min &&
              value <= max
          ? value / scale
          : null;
    }

    points.add(
      TelemetryPoint(time, {
        'voltage': read('bus_mv', 'ina219_available', 1000, 0, 100000),
        'current': read('current_ma', 'ina219_available', 1, -100000, 100000),
        'power': read(
          'power_mw',
          'ina219_available',
          1000,
          -10000000,
          10000000,
        ),
        'temperature': read(
          'temperature_led_centi_c',
          'temperature_led_available',
          100,
          -5500,
          12500,
        ),
        'audio': read(
          'temperature_bt_audio_centi_c',
          'temperature_bt_audio_available',
          100,
          -5500,
          12500,
        ),
      }),
    );
    if (points.length > 600) points.removeRange(0, points.length - 600);
  }
}

class StatePill extends StatelessWidget {
  const StatePill(this.label, {super.key, this.active, this.color});
  final String label;
  final bool? active;
  final Color? color;
  @override
  Widget build(BuildContext context) {
    final scheme = Theme.of(context).colorScheme;
    final tone =
        color ??
        (active == true ? const Color(0xff55bd83) : scheme.onSurfaceVariant);
    return Semantics(
      label: label,
      child: Container(
        padding: const EdgeInsets.symmetric(horizontal: 10, vertical: 5),
        decoration: BoxDecoration(
          color: tone.withValues(alpha: .10),
          borderRadius: BorderRadius.circular(20),
          border: Border.all(color: tone.withValues(alpha: .22)),
        ),
        child: Row(
          mainAxisSize: MainAxisSize.min,
          children: [
            Icon(
              active == null
                  ? Icons.help_outline
                  : active!
                  ? Icons.circle
                  : Icons.circle_outlined,
              size: 10,
              color: tone,
            ),
            const SizedBox(width: 6),
            Text(
              label,
              textHeightBehavior: cardTextHeightBehavior,
              style: TextStyle(
                color: tone,
                fontSize: 12,
                fontWeight: FontWeight.w600,
              ),
            ),
          ],
        ),
      ),
    );
  }
}

class FrontPanelButtons extends StatelessWidget {
  const FrontPanelButtons({
    super.key,
    required this.captured,
    required this.enabled,
    required this.dispatch,
    this.pressedKeys,
  });
  final bool captured, enabled;
  final int? pressedKeys;
  final ValueChanged<String> dispatch;
  @override
  Widget build(BuildContext context) => Row(
    children: [
      for (var index = 0; index < 4; index++) ...[
        if (index > 0) const SizedBox(width: 6),
        Expanded(
          child: OutlinedButton(
            style: OutlinedButton.styleFrom(
              minimumSize: const Size(44, 48),
              padding: const EdgeInsets.symmetric(horizontal: 4),
              backgroundColor:
                  pressedKeys != null && pressedKeys! & (1 << index) != 0
                  ? Theme.of(context).colorScheme.primaryContainer
                  : null,
              shape: RoundedRectangleBorder(
                borderRadius: BorderRadius.circular(8),
              ),
            ),
            onPressed: enabled
                ? () => dispatch(
                    captured
                        ? 'host-menu key K${index + 1} press'
                        : 'menu ${['prev', 'next', 'dec', 'inc'][index]}',
                  )
                : null,
            child: Column(
              mainAxisSize: MainAxisSize.min,
              children: [
                if (!captured)
                  Icon(
                    [
                      Icons.arrow_back,
                      Icons.arrow_forward,
                      Icons.remove,
                      Icons.add,
                    ][index],
                    size: 18,
                  ),
                Text(
                  captured
                      ? 'K${index + 1}'
                      : ['Prev', 'Next', '−', '+'][index],
                  maxLines: 1,
                  overflow: TextOverflow.ellipsis,
                ),
              ],
            ),
          ),
        ),
      ],
    ],
  );
}

/// Keep a user's drag preview stable while live host snapshots continue. Only
/// release sends a request; the owner-bearing key retires a drag on peer change.
class RemoteSlider extends StatefulWidget {
  const RemoteSlider({
    super.key,
    required this.value,
    required this.maximum,
    required this.changed,
    this.enabled = true,
    this.label,
  });
  final double value, maximum;
  final bool enabled;
  final ValueChanged<double> changed;
  final String Function(double)? label;
  @override
  State<RemoteSlider> createState() => _RemoteSliderState();
}

class _RemoteSliderState extends State<RemoteSlider> {
  double? draft;
  @override
  Widget build(BuildContext context) => Slider(
    value: (draft ?? widget.value).clamp(0, widget.maximum),
    max: widget.maximum,
    label: widget.label?.call(draft ?? widget.value),
    onChanged: widget.enabled ? (v) => setState(() => draft = v) : null,
    onChangeEnd: widget.enabled
        ? (v) {
            setState(() => draft = null);
            widget.changed(v);
          }
        : null,
  );
}

class ChannelObservation {
  const ChannelObservation(
    this.label,
    this.active, {
    this.percent,
    this.stale = false,
  });
  final String label;
  final bool? active;
  final double? percent;
  final bool stale;
}

ChannelObservation observeChannel(
  Json control,
  Json snapshot,
  Json pwm,
  bool fresh, {
  bool? pwmFresh,
}) {
  final key = '${control['key']}';
  if (key.startsWith('pwm.') && pwm['available'] == true) {
    final channel = int.tryParse(key.split('.').last);
    for (final reading in objects(pwm['channels'])) {
      final value = reading['logical_percent'];
      if (reading['channel'] == channel &&
          value is num &&
          value.isFinite &&
          value >= 0 &&
          value <= 100) {
        final current = pwmFresh ?? fresh;
        return ChannelObservation(
          current
              ? value > 0
                    ? '${value.toStringAsFixed(1)}%'
                    : 'Off'
              : 'Stale',
          current ? value > 0 : null,
          percent: value.toDouble(),
          stale: !current,
        );
      }
    }
    return const ChannelObservation('Unknown', null);
  }
  if (!fresh || snapshot['have_status'] != true) {
    return const ChannelObservation('Unknown', null);
  }
  final status = object(snapshot['status']);
  if (key.startsWith('relay.')) {
    final id = int.tryParse(key.split('.').last),
        mask = status['active_relays'];
    if (id != null &&
        id >= 1 &&
        id <= 8 &&
        mask is int &&
        mask >= 0 &&
        mask <= 255) {
      final on = (mask & (1 << (id - 1))) != 0;
      return ChannelObservation(on ? 'On' : 'Off', on);
    }
  }
  if (key == 'seat.a' || key == 'seat.b') {
    final side = object(
      object(snapshot['motion'])[key == 'seat.a' ? 'left' : 'right'],
    );
    final applied = side['applied'];
    if (['up', 'down', 'stop'].contains(applied)) {
      return ChannelObservation(
        side['transitioning'] == true
            ? 'Changing direction'
            : applied == 'stop'
            ? 'Stopped'
            : applied == 'up'
            ? 'Moving up'
            : 'Moving down',
        applied != 'stop',
      );
    }
  }
  return const ChannelObservation('Unknown', null);
}

class TelemetryChart extends StatelessWidget {
  const TelemetryChart({
    super.key,
    required this.points,
    required this.field,
    required this.color,
    required this.unit,
    this.height = 80,
  });
  final List<TelemetryPoint> points;
  final String field, unit;
  final Color color;
  final double height;
  @override
  Widget build(BuildContext context) {
    final numbers = points
        .map((p) => p.values[field])
        .whereType<double>()
        .toList();
    if (numbers.isEmpty) return const SizedBox.shrink();
    final low = numbers.reduce(math.min), high = numbers.reduce(math.max);
    return Column(
      crossAxisAlignment: CrossAxisAlignment.stretch,
      children: [
        SizedBox(
          height: height,
          child: CustomPaint(
            painter: _ChartPainter(
              List.of(points),
              field,
              color,
              Theme.of(context).dividerColor,
            ),
          ),
        ),
        const SizedBox(height: 4),
        if (points.length > 1)
          Row(
            mainAxisAlignment: MainAxisAlignment.spaceBetween,
            children: [
              Text(
                low == high
                    ? '${low.toStringAsFixed(field == 'current' ? 0 : 2)} $unit'
                    : '${low.toStringAsFixed(field == 'current' ? 0 : 2)}–${high.toStringAsFixed(field == 'current' ? 0 : 2)} $unit',
                style: TextStyle(
                  fontSize: 11,
                  color: Theme.of(context).colorScheme.onSurfaceVariant,
                ),
              ),
              Text(
                '${points.length > 1 ? points.last.time.difference(points.first.time).inSeconds : 0}s',
                style: TextStyle(
                  fontSize: 11,
                  color: Theme.of(context).colorScheme.onSurfaceVariant,
                ),
              ),
            ],
          ),
      ],
    );
  }
}

class _ChartPainter extends CustomPainter {
  _ChartPainter(this.points, this.field, this.color, this.grid);
  final List<TelemetryPoint> points;
  final String field;
  final Color color, grid;
  @override
  void paint(Canvas canvas, Size size) {
    final values = points
        .map((p) => p.values[field])
        .whereType<double>()
        .toList();
    if (values.isEmpty) return;
    for (var row = 0; row < 3; row++) {
      canvas.drawLine(
        Offset(0, 4 + row * (size.height - 8) / 2),
        Offset(size.width, 4 + row * (size.height - 8) / 2),
        Paint()
          ..color = grid.withValues(alpha: .3)
          ..strokeWidth = 1,
      );
    }
    final min = values.reduce(math.min), max = values.reduce(math.max);
    final padding = math.max(
      (max - min) * .15,
      math.max(max.abs() * .005, .05),
    );
    final low = min - padding, span = max - min + padding * 2;
    final from = points.first.time.millisecondsSinceEpoch,
        duration = math.max(1, points.last.time.millisecondsSinceEpoch - from);
    final path = Path();
    bool joined = false;
    Offset? last;
    DateTime? previousTime;
    for (final point in points) {
      final value = point.values[field];
      if (value == null) {
        joined = false;
        last = null;
        previousTime = null;
        continue;
      }
      final position = Offset(
        (point.time.millisecondsSinceEpoch - from) / duration * size.width,
        size.height - 4 - (value - low) / span * (size.height - 8),
      );
      if (previousTime != null &&
          point.time.difference(previousTime).inMilliseconds > 3000) {
        joined = false;
      }
      if (joined) {
        path.lineTo(position.dx, position.dy);
      } else {
        path.moveTo(position.dx, position.dy);
      }
      joined = true;
      last = position;
      previousTime = point.time;
    }
    canvas.drawPath(
      path,
      Paint()
        ..color = color
        ..style = PaintingStyle.stroke
        ..strokeWidth = 2
        ..strokeJoin = StrokeJoin.round,
    );
    if (last != null) canvas.drawCircle(last, 3, Paint()..color = color);
  }

  @override
  bool shouldRepaint(covariant _ChartPainter old) =>
      old.points.length != points.length ||
      old.field != field ||
      old.points.firstOrNull?.time != points.firstOrNull?.time ||
      old.points.lastOrNull?.time != points.lastOrNull?.time ||
      old.color != color ||
      old.grid != grid;
}
