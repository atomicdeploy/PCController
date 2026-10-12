import 'package:flutter_test/flutter_test.dart';
import 'package:pccontroller_mobile/models.dart';

void main() {
  test('settings catalog follows explicit peer ACKs and build info has no defaults', () {
    final unknown = DeviceStatus({'pixels': 480});
    expect(unknown.deviceSettingsPages.map((p) => p['id']), ['overview']);
    expect(unknown.observedBuildInformation.containsKey('link_mode'), false);
    final peer = DeviceStatus({
      'hostname': 'ledboard',
      'link_mode': 'busy-wire',
      'capabilities': {
        'hostname': 'ledboard',
        'board_kind': 'controller',
        'acknowledged_features': ['network', 'buzzer'],
      },
    });
    expect(peer.deviceSettingsPages.map((p) => p['id']), [
      'overview',
      'wifi',
      'mqtt',
      'websocket',
      'access',
      'front-panel',
    ]);
    expect(peer.observedBuildInformation['link_mode'], 'busy-wire');
    expect(peer.deviceSettingsPages.any((p) => p['id'] == 'leds'), false);
  });
}
