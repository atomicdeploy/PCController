import 'package:pccontroller_mobile/models.dart';

/// Explicit simulated peer; never substitute for physical acceptance.
DeviceStatus peerFixture({
  List<String> features = const [
    'network',
    'telemetry',
    'table',
    'leds',
    'streaming',
    'display',
    'text',
    'buzzer',
    'inputs',
    'outputs',
    'identity',
  ],
  String kind = 'table',
  String hostname = 'ledboard',
  bool displayAck = true,
  String title = 'Pakar Table',
  String application = 'pakar-table',
}) => DeviceStatus({
  'hostname': hostname,
  'mode': 'cell',
  'avr_online': true,
  'tm1637_ack': displayAck,
  'tm1637_segments': [6, 91, 79, 102],
  'tm1637_brightness': 7,
  'audio_cues': ['welcome'],
  'cells': List.filled(80, 'white'),
  'capabilities': <String, dynamic>{
    'hostname': hostname,
    'name': 'Table board',
    'board_kind': kind,
    'acknowledged_features': features,
    'table': <String, dynamic>{'columns': 10, 'rows': 8, 'pixels_per_cell': 6},
    'application': {'id': application, 'name': title, 'title': title},
    'modes': [
      {'id': 'idle', 'code': 0, 'kind': 'presentation', 'label': 'Idle'},
      {'id': 'cell', 'code': 1, 'kind': 'presentation', 'label': 'Cells'},
      {'id': 'first-wins', 'code': 2, 'kind': 'game', 'label': 'First wins'},
      {'id': 'snake', 'code': 3, 'kind': 'presentation', 'label': 'Snake'},
    ],
  },
});
