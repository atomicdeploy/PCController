// Ported from PCController web/src/status-led-event.ts (same current contract).
import 'transport.dart' show Json, object;

Json applyOutputEvent(Json snapshot, Json event) {
  final metadata = object(event['metadata']);
  if (event['kind'] == 'front_panel.segment') {
    final raw = metadata['raw_segments'],
        brightness = int.tryParse('${metadata['brightness']}');
    if (raw is! String ||
        !RegExp(r'^[0-9a-fA-F]{8}$').hasMatch(raw) ||
        brightness == null ||
        brightness < 0 ||
        brightness > 7) {
      return snapshot;
    }
    return {
      ...snapshot,
      'front_panel': {
        ...object(snapshot['front_panel']),
        'raw_segments': [
          for (var offset = 0; offset < 8; offset += 2)
            int.parse(raw.substring(offset, offset + 2), radix: 16),
        ],
        'brightness': brightness,
        'segments_active': true,
      },
      'have_front_panel_segments': true,
      'front_panel_updated': event['time'],
    };
  }
  if (event['kind'] == 'status_led.changed') {
    final fields = [
      'red',
      'green',
      'blue',
      'brightness',
      'effect',
      'condition',
    ];
    final values = {
      for (final field in fields) field: int.tryParse('${metadata[field]}'),
    };
    if (values.values.any(
      (value) => value == null || value < 0 || value > 255,
    )) {
      return snapshot;
    }
    final revision = int.tryParse('${metadata['revision']}') ?? 0;
    if (revision < (snapshot['status_led_revision'] as int? ?? 0)) {
      return snapshot;
    }
    return {
      ...snapshot,
      'status_led': values,
      'have_status_led': true,
      'status_led_revision': revision,
      'status_led_updated': event['time'],
    };
  }
  return snapshot;
}
