import 'settings_catalog.dart';
import 'peer_context.dart';

class DeviceStatus {
  DeviceStatus(this.json);
  final Map<String, dynamic> json;
  late final peer = PeerContext.fromStatus(json);

  /// Shared catalog, filtered by this peer's explicit ACKs. The caller still
  /// owns freshness; this DTO never infers capabilities from pin/pixel counts.
  List<Map<String, dynamic>> get deviceSettingsPages {
    return (deviceSettingsCatalog['pages'] as List)
        .cast<Map<String, dynamic>>()
        .where(
          (page) =>
              (page['features'] as List).isEmpty ||
              (page['features'] as List).any((feature) => peer.has(feature)),
        )
        .toList(growable: false);
  }

  Map<String, dynamic> get observedBuildInformation => {
    for (final key in ['version', 'link_mode', 'pixels', 'avr_clock_hz'])
      if (json.containsKey(key)) key: json[key],
  };

  String get hostname => json['hostname'] as String? ?? '';
  String get mode => json['mode'] as String? ?? '';
  String get stationIp => json['station_ip'] as String? ?? '';
  int? get rssi => json['station_rssi'] as int?;
  bool get avrOnline => json['avr_online'] == true;
  List<String> get audioCues {
    final reported = json['audio_cues'];
    if (!peer.has('buzzer') || !avrOnline || reported is! List) return const [];
    return reported
        .whereType<String>()
        .where(audioCueNames.contains)
        .toSet()
        .toList();
  }

  bool get tmReady => json['tm1637_ack'] == true;
  bool get mdns => json['mdns_started'] == true;
  bool get ssdp => json['ssdp_started'] == true;
  bool get mqtt => json['mqtt_connected'] == true;
  bool get ntp => json['ntp_synced'] == true;
  String get effect => json['effect'] as String? ?? 'none';
  int get brightness => json['brightness'] as int? ?? 255;
  int get winner => json['winner'] as int? ?? 0;
  int get selectedCell => json['selected_cell'] as int? ?? 1;
  int get activeInputs => json['active_inputs'] as int? ?? 0;
  int get activeOutputs => json['active_outputs'] as int? ?? 0;
  List<String> get cells => (json['cells'] as List? ?? const [])
      .map((value) => value.toString())
      .toList(growable: false);
  List<int> get segments {
    final values = json['tm1637_segments'];
    if (values is! List ||
        values.length != 4 ||
        values.any((v) => v is! int || v < 0 || v > 255)) {
      return const [];
    }
    return values.cast<int>();
  }

  int get segmentBrightness => json['tm1637_brightness'] as int? ?? 0;
}

const audioCueNames = [
  'welcome',
  'update-start',
  'update-success',
  'error',
  'winner',
  'finish',
  'lost',
  'incorrect',
  'rapid-error',
  'fault',
  'increase',
  'decrease',
  'welcome-puzzleboard',
  'welcome-40led',
];

class CellMap {
  CellMap({required this.physical, required this.names});
  final List<int> physical;
  final List<String> names;

  bool get valid =>
      physical.length == 80 &&
      names.length == 80 &&
      physical.toSet().length == 80 &&
      physical.every((v) => v >= 1 && v <= 80) &&
      names.every((v) => validText(v, 15));

  factory CellMap.fromJson(Map<String, dynamic> json) {
    final physical = json['physical'], names = json['names'];
    if (physical is! List ||
        names is! List ||
        physical.any((v) => v is! int) ||
        names.any((v) => v is! String)) {
      throw const FormatException('Physical cell mapping unavailable');
    }
    final map = CellMap(
      physical: physical.cast<int>(),
      names: names.cast<String>(),
    );
    if (!map.valid) {
      throw const FormatException('Invalid physical cell mapping');
    }
    return map;
  }

  factory CellMap.defaults() => CellMap(
    physical: List.generate(80, (index) => index + 1),
    names: List.generate(80, (index) => '${index + 1}'),
  );
}
