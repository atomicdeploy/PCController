import 'dart:convert';
import 'dart:io';
import 'dart:typed_data';

import 'package:flutter_test/flutter_test.dart';
import 'package:http/http.dart' as http;
import 'package:http/testing.dart';
import 'package:crypto/crypto.dart';
import 'package:pccontroller_mobile/api.dart';
import 'package:pccontroller_mobile/bridge.dart';
import 'package:pccontroller_mobile/device_registry.dart';
import 'package:pccontroller_mobile/discovery.dart';
import 'package:pccontroller_mobile/firmware.dart';
import 'package:pccontroller_mobile/models.dart';

import 'peer_fixture.dart';

DeviceStatus bridgePeer() => DeviceStatus({
  ...peerFixture().json,
  'board_id': 'board-one',
  'boot_id': 'boot-one',
  'password': 'never-publish',
});
Map<String, dynamic> request(String session, {String id = 'request-one'}) => {
  'id': id,
  'bridge_session': session,
  'board_id': 'board-one',
  'boot_id': 'boot-one',
  'expires_at_ms': DateTime.now().millisecondsSinceEpoch + 30000,
  'type': 'command',
  'path': '/api/brightness',
  'values': {'value': '90'},
};

void main() {
  test('Same-host board/boot replacement retires firmware owners synchronously and never downgrades identity', () async {
    var value = bridgePeer();
    var retired = 0, writes = 0;
    final api = PakarApi(
      client: MockClient((r) async {
        if (r.method == 'POST') writes++;
        return http.Response(jsonEncode(value.json), 200);
      }),
    );
    await api.status();
    final owner = api.connectionRevision;
    final controller = FirmwareController(api, authorized: (_) => true);
    api.onPeerChanged = () {
      retired++;
      expect(api.connectionRevision, greaterThan(owner));
    };
    value = DeviceStatus({...value.json, 'boot_id': 'replacement-boot'});
    await api.status();
    expect(retired, 1);
    expect(controller.allowed('ota'), false);
    await expectLater(controller.espBytes(), throwsA(isA<StaleConnection>()));
    value = DeviceStatus(
      {...value.json}
        ..remove('board_id')
        ..remove('boot_id'),
    );
    await expectLater(api.status(), throwsA(isA<StaleConnection>()));
    await expectLater(api.status(), throwsA(isA<StaleConnection>()));
    expect(retired, 2);
    expect(writes, 0);
    api.onPeerChanged = null;
    controller.dispose();
    await api.close();
  });
  test('Running ESP export verifies complete response and advertised digest before SAF', () async {
    final bytes = Uint8List(16)..[0] = 0xe9;
    for (final digest in [md5.convert(bytes).toString(), '0' * 32, null]) {
      final api = PakarApi(
        client: MockClient(
          (r) async => r.url.path == '/api/status'
              ? http.Response(jsonEncode(bridgePeer().json), 200)
              : http.Response.bytes(
                  bytes,
                  200,
                  headers: digest == null ? {} : {'x-firmware-md5': digest},
                ),
        ),
      );
      final controller = FirmwareController(api, authorized: (_) => true);
      if (digest == md5.convert(bytes).toString()) {
        expect(await controller.espBytes(), bytes);
      } else {
        await expectLater(controller.espBytes(), throwsFormatException);
      }
      controller.dispose();
      await api.close();
    }
  });
  test(
    'Saved metadata never includes passwords or claimed board capabilities',
    () {
      final d = SavedDevice(
        id: 'one',
        name: 'Remote table',
        endpoint: 'https://controller.example',
        location: DeviceLocation.remote,
      );
      expect(SavedDevice.fromJson(d.toJson()).location, DeviceLocation.remote);
      expect(d.toJson().containsKey('password'), false);
      expect(d.toJson().containsKey('capabilities'), false);
      expect(
        () => SavedDevice(
          id: '../bad',
          name: 'Bad',
          endpoint: 'http://host',
          location: DeviceLocation.local,
        ),
        throwsFormatException,
      );
    },
  );
  test('Metadata parsers reject truncated TLVs, duplicates and credential locations', () {
    Uint8List bytes(String s) => Uint8List.fromList(utf8.encode(s));
    expect(
      parseSsdp(
        bytes(
          'HTTP/1.1 200 OK\r\nLOCATION: http://ledboard.local/description.xml\r\n',
        ),
      )?.endpoint,
      'http://ledboard.local',
    );
    expect(
      parseSsdp(bytes('HTTP/1.1 200 OK\r\nLOCATION: http://u:p@host/\r\n')),
      isNull,
    );
    expect(parseSsdp(bytes('NOTIFY * HTTP/1.1\r\n')), isNull);
    final packet = Uint8List.fromList([
      0,
      0,
      0,
      1,
      0,
      5,
      0,
      3,
      ...utf8.encode('led'),
    ]);
    expect(parseMndp(packet, '192.0.2.1'), isNull); // No invented HTTP port80.
    final application = Uint8List.fromList([
      ...packet,
      0,
      1,
      0,
      6,
      2,
      0,
      0,
      0,
      0,
      1,
      255,
      1,
      0,
      12,
      ...utf8.encode('pccontroller'),
      255,
      2,
      0,
      2,
      0x22,
      0x53,
    ]);
    expect(
      parseMndp(application, '192.0.2.1')?.endpoint,
      'http://192.0.2.1:8787',
    );
    expect(parseMndp(application, '192.0.2.1')?.product, 'pccontroller');
    expect(
      parseSsdp(
        bytes('HTTP/1.1 200 OK\r\nLOCATION: http://192.0.2.2:8787/\r\n'),
        source: '192.0.2.1',
      ),
      isNull,
    );
    expect(parseMndp(packet.sublist(0, 10), '192.0.2.1'), isNull);
    expect(
      parseMndp(Uint8List.fromList([...packet, 0, 5, 0, 1, 65]), '192.0.2.1'),
      isNull,
    );
  });
  test('Shared admission consumes IDs and rejects retained, wrong peer and expired messages', () {
    final a = BridgeAdmission('session'), s = bridgePeer();
    a.admit(request('session'), s);
    expect(() => a.admit(request('session'), s), throwsFormatException);
    for (final v in [
      {...request('session'), 'id': 'two', 'boot_id': 'old-boot'},
      {...request('session'), 'id': 'three', 'expires_at_ms': 0},
      request('old-session', id: 'four'),
    ]) {
      expect(() => a.admit(v, s), throwsFormatException);
    }
    expect(
      () => a.admit(request('session', id: 'retained'), s, retained: true),
      throwsFormatException,
    );
  });
  test('MQTT TLS, QoS and endpoint policy fails closed', () {
    BridgeOptions(
      broker: 'mqtts://mqtt.example:8883',
      remoteWebSocket: 'wss://remote.example/bridge',
    ).validate();
    BridgeOptions(broker: 'wss://mqtt.example/mqtt').validate();
    for (final option in [
      BridgeOptions(broker: 'mqtt://host'),
      BridgeOptions(broker: 'mqtts://u:p@host'),
      BridgeOptions(topic: 'commands/#'),
      BridgeOptions(qos: 2),
      BridgeOptions(telemetrySeconds: 0),
      BridgeOptions(remoteWebSocket: 'ws://host'),
    ]) {
      expect(option.validate, throwsFormatException);
    }
  });
  test('Actual loopback bridge requires bearer and forwards once with board/boot fences', () async {
    DeviceStatus? observed = bridgePeer();
    var writes = 0;
    final api = PakarApi(
      client: MockClient((r) async {
        if (r.method == 'POST') {
          writes++;
          expect(r.headers['X-Pakar-Board'], 'board-one');
          expect(r.headers['X-Pakar-Boot'], 'boot-one');
          expect(r.followRedirects, false);
          expect(r.url.hasQuery, false);
          expect(r.bodyFields['value'], '90');
        }
        return http.Response(jsonEncode(observed!.json), 200);
      }),
    );
    await api.status();
    final bridge = AndroidBridge(api, () => observed);
    final client = HttpClient();
    try {
      await bridge.start(BridgeOptions(commands: true));
      final base = 'http://127.0.0.1:${bridge.server!.port}';
      Future<Map<String, dynamic>> send(Map<String, dynamic> v) async {
        final r = await client.postUrl(Uri.parse('$base/api/request'));
        r.headers.set('Authorization', 'Bearer ${bridge.token}');
        r.write(jsonEncode(v));
        final response = await r.close();
        return jsonDecode(await utf8.decoder.bind(response).join());
      }

      final unauthenticated = await (await client.getUrl(
        Uri.parse('$base/api/status'),
      )).close();
      expect(unauthenticated.statusCode, 403);
      await unauthenticated.drain<void>();
      expect(bridge.snapshot()['board'].containsKey('password'), false);
      final v = request(bridge.admission!.session);
      expect((await send(v))['accepted'], true);
      expect((await send(v))['accepted'], false);
      expect(writes, 1);
      observed = null;
      bridge.telemetry();
      await bridge.stop();
      expect(bridge.running, false);
    } finally {
      client.close(force: true);
      await bridge.stop();
      bridge.dispose();
      await api.close();
    }
  });
}
