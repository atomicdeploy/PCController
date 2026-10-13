// Explicit simulated public configuration; never substitute for device reads.
const simulatedNetwork = <String, dynamic>{
  'station_enabled': true,
  'ap_enabled': true,
  'station_ssid': 'Fixture network',
};
const simulatedCloud = <String, dynamic>{
  'mqtt_enabled': false,
  'mqtt_tls': true,
  'mqtt_host': 'mqtt.example.invalid',
  'mqtt_port': 8883,
  'mqtt_user': 'fixture',
  'mqtt_base_topic': 'pakar/fixture',
  'mqtt_password_set': true,
  'websocket_enabled': false,
  'websocket_tls': true,
  'websocket_host': '',
  'websocket_port': 443,
  'websocket_path': '/controller',
  'websocket_token_set': false,
  'telemetry_seconds': 30,
};
