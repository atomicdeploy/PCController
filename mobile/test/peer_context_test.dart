import 'dart:convert';
import 'dart:typed_data';

import 'package:flutter_test/flutter_test.dart';
import 'package:pccontroller_mobile/models.dart';
import 'package:pccontroller_mobile/peer_context.dart';

import 'peer_fixture.dart';

void main() {
  test(
    'No peer ACK cannot be inferred from live pixels, cells or AVR flags',
    () {
      final status = DeviceStatus({
        'hostname': 'ledboard',
        'pixels': 480,
        'avr_online': true,
        'tm1637_ack': true,
        'cells': List.filled(80, 'white'),
      });
      expect(status.peer.known, false);
      expect(status.peer.table, false);
      expect(status.peer.frontPanel, false);
      expect(status.peer.modes, isEmpty);
      expect(status.peer.allows('/api/cell', {'id': '1'}), false);
    },
  );
  test('Independent ACKs and measured display gate follow donor ready AND advertised', () {
    final buzzer = peerFixture(features: ['buzzer'], kind: 'controller');
    expect(buzzer.peer.has('buzzer'), true);
    expect(buzzer.peer.has('text'), false);
    expect(buzzer.peer.has('display'), false);
    expect(buzzer.peer.table, false);
    expect(buzzer.peer.modes, isEmpty);
    expect(peerFixture(displayAck: false).peer.has('display'), false);
    expect(
      peerFixture(features: ['text'], displayAck: false).peer.has('text'),
      true,
    );
  });
  test('Table geometry and advertised mode identity must agree; First wins needs IO', () {
    expect(peerFixture(kind: 'controller').peer.modes, isEmpty);
    expect(peerFixture(features: ['table']).peer.modes.map((m) => m.id), [
      'idle',
      'cell',
      'snake',
    ]);
    final status = peerFixture();
    expect(status.peer.modes.map((m) => m.id), [
      'idle',
      'cell',
      'first-wins',
      'snake',
    ]);
    expect(status.peer.allows('/api/mode', {'value': 'snake'}), true);
    expect(status.peer.allows('/api/mode', {'value': 'not-advertised'}), false);
    final invalid = peerFixture().json;
    (invalid['capabilities']['table'] as Map)['rows'] = 9;
    expect(DeviceStatus(invalid).peer.table, false);
    (invalid['capabilities']['table'] as Map)['rows'] = 8.0;
    expect(DeviceStatus(invalid).peer.table, false);
  });
  test('Mixed host and missing feature ACKs fail closed', () {
    final value = peerFixture().json;
    value['capabilities']['hostname'] = 'other';
    expect(DeviceStatus(value).peer.known, false);
    value['capabilities']['hostname'] = 'ledboard';
    value['capabilities']['acknowledged_features'] = null;
    expect(DeviceStatus(value).peer.known, false);
  });
  test(
    'Inline identity text/icon are bounded; no URL fetch or name translation',
    () {
      expect(
        peerFixture(title: 'میز فارسی').peer.application!.title,
        'میز فارسی',
      );
      for (final title in ['', 'a' * 161, 'Bad\nTitle']) {
        expect(peerFixture(title: title).peer.application, isNull);
      }
      final bytes = Uint8List(33);
      bytes.setRange(0, 8, [137, 80, 78, 71, 13, 10, 26, 10]);
      bytes.setRange(12, 16, ascii.encode('IHDR'));
      ByteData.sublistView(bytes).setUint32(16, 64);
      ByteData.sublistView(bytes).setUint32(20, 64);
      final json = {
        'id': 'app',
        'name': 'App',
        'title': 'App title',
        'icon_png_base64': base64Encode(bytes),
      };
      expect(PeerApplication.parse(json)!.icon, isNotNull);
      ByteData.sublistView(bytes).setUint32(16, 4096);
      json['icon_png_base64'] = base64Encode(bytes);
      expect(PeerApplication.parse(json)!.icon, isNull);
      json['icon_png_base64'] = 'https://other/logo.png';
      expect(PeerApplication.parse(json)!.icon, isNull);
    },
  );
}
