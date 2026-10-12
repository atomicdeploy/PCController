import 'dart:convert';
import 'dart:typed_data';

import 'package:flutter_test/flutter_test.dart';
import 'package:http/http.dart' as http;
import 'package:http/testing.dart';
import 'package:pccontroller_mobile/unified/transport.dart';
import 'package:pccontroller_mobile/unified/output_events.dart';

void main() {
  test(
    'incomplete catalog is rejected, not converted to a false empty inventory',
    () {
      expect(() => verifiedCatalog({}), throwsA(isA<Rejected>()));
      expect(
        () => verifiedCatalog({
          'controls': [42],
          'peripherals': [],
          'board_profile': {},
        }),
        throwsA(isA<Rejected>()),
      );
      expect(
        verifiedCatalog({
          'controls': [],
          'peripherals': [],
          'board_profile': {},
        })['controls'],
        isEmpty,
      );
    },
  );
  test(
    'terminal operation does not remain stuck at its former preparing stage',
    () {
      for (final state in ['completed', 'failed', 'cancelled']) {
        expect(operationLabel({'state': state, 'stage': 'preparing'}), state);
      }
    },
  );
  test('board profile drift retires an open confirmation target', () {
    final session = PeerSession(controller: true);
    session.state = {
      'host_instance_id': 'host',
      'port': 'usb',
      'connection_generation': 1,
    };
    session.peripherals = {
      'board_profile': {'revision': 'before'},
    };
    final stamp = session.targetStamp;
    session.peripherals = {
      'board_profile': {'revision': 'after'},
    };
    expect(session.targetStamp, isNot(stamp));
    session.dispose();
  });
  test('an authoritative old measurement does not become fresh after a host refresh', () {
    final now = DateTime.utc(2026, 10, 12, 12);
    expect(
      measurementIsFresh(
        {
          'have_status': true,
          'status_updated': now
              .subtract(const Duration(minutes: 1))
              .toIso8601String(),
        },
        2000,
        now: now,
      ),
      false,
    );
    expect(
      measurementIsFresh(
        {'have_status': true, 'status_updated': now.toIso8601String()},
        2000,
        now: now,
      ),
      true,
    );
    expect(
      measurementIsFresh(
        {'have_status': false, 'status_updated': now.toIso8601String()},
        2000,
        now: now,
      ),
      false,
    );
  });
  test('origins reject credential, path, query and non-HTTP pivots', () {
    for (final value in [
      'file:///a',
      'http://u:p@host',
      'http://host/api',
      'http://host?token=x',
      'http://host#secret',
    ]) {
      expect(() => endpoint(value), throwsFormatException);
    }
    expect(endpoint('https://board:8787/').toString(), 'https://board:8787');
  });
  test(
    'RPC correlates IDs, bearer stays in header, mutations do not retry',
    () async {
      var calls = 0;
      final peer = PeerTransport(
        'http://board:8787',
        token: 'private',
        client: MockClient((request) async {
          calls++;
          expect(request.url.toString(), 'http://board:8787/api/rpc');
          expect(request.headers['Authorization'], 'Bearer private');
          expect(request.followRedirects, false);
          final value = jsonDecode(request.body);
          return http.Response(
            jsonEncode({
              'jsonrpc': '2.0',
              'id': value['id'],
              'result': {'accepted': true},
            }),
            200,
          );
        }),
      );
      expect(
        await peer.rpc('controller.action.invoke', {
          'action_id': 'relay.5.off',
        }),
        {'accepted': true},
      );
      expect(calls, 1);
      peer.close();
    },
  );
  test(
    'malformed replies and missing acknowledgement remain uncertain',
    () async {
      var calls = 0;
      final peer = PeerTransport(
        'http://board',
        client: MockClient((_) async {
          calls++;
          return http.Response('{"jsonrpc":"2.0","id":900,"result":{}}', 200);
        }),
      );
      await expectLater(
        peer.rpc('controller.action.invoke'),
        throwsA(isA<Uncertain>()),
      );
      expect(calls, 1);
      peer.close();
    },
  );
  test(
    'structured RPC rejection is not an uncertain accepted operation',
    () async {
      final peer = PeerTransport(
        'http://board',
        client: MockClient((request) async {
          final id = jsonDecode(request.body)['id'];
          return http.Response(
            jsonEncode({
              'jsonrpc': '2.0',
              'id': id,
              'error': {'code': -32000, 'message': 'Programming latch active'},
            }),
            200,
          );
        }),
      );
      await expectLater(
        peer.rpc('controller.action.invoke'),
        throwsA(isA<Rejected>()),
      );
      peer.close();
    },
  );
  test('provisioning never fabricates 0 percent', () {
    expect(
      operationProgress({
        'progress_known': true,
        'progress_percent': 0,
        'state': 'toolchain-provisioning',
      }),
      null,
    );
    expect(
      operationProgress({
        'progress_known': true,
        'progress_percent': 0,
        'kind': 'firmware',
        'toolchain': {'ready': false},
      }),
      null,
    );
    expect(
      operationProgress({'progress_known': false, 'progress_percent': 30}),
      null,
    );
    expect(
      operationProgress({
        'progress_known': true,
        'progress_percent': 30,
        'kind': 'firmware',
        'toolchain': {'ready': true},
      }),
      .3,
    );
  });
  test('raw digit events preserve LCD/key authority without inventing it', () {
    final partial = applyOutputEvent({}, {
      'kind': 'front_panel.segment',
      'time': 'now',
      'metadata': {'raw_segments': '065B4F66', 'brightness': '7'},
    });
    expect(object(partial['front_panel'])['raw_segments'], [6, 91, 79, 102]);
    expect(partial.containsKey('have_front_panel'), false);
    expect(object(partial['front_panel']).containsKey('pressed_keys'), false);
    final current = {
      'front_panel': {'pressed_keys': 3, 'lcd_line_1': 'Hello'},
    };
    expect(
      object(
        applyOutputEvent(current, {
          'kind': 'front_panel.segment',
          'metadata': {'raw_segments': '00000000', 'brightness': '0'},
        })['front_panel'],
      )['lcd_line_1'],
      'Hello',
    );
    expect(
      applyOutputEvent(current, {
        'kind': 'front_panel.segment',
        'metadata': {'raw_segments': 'oops', 'brightness': '7'},
      }),
      same(current),
    );
  });
  test('late RGB revision cannot overwrite current authoritative LED', () {
    final current = {'status_led_revision': 8};
    expect(
      applyOutputEvent(current, {
        'kind': 'status_led.changed',
        'metadata': {
          'revision': '7',
          'red': '1',
          'green': '2',
          'blue': '3',
          'brightness': '255',
          'effect': '0',
          'condition': '0',
        },
      }),
      same(current),
    );
  });
  test(
    'relay sends only typed controller envelope and its own credential',
    () async {
      final relay = ControllerRelayTransport(
        'http://player',
        token: 'player-token',
        client: MockClient((request) async {
          expect(request.url.path, '/api/peer/controller');
          expect(request.headers['Authorization'], 'Bearer player-token');
          expect(jsonDecode(request.body), {
            'method': 'controller.snapshot',
            'params': {},
          });
          return http.Response(
            '{"host_instance_id":"host","connected":true}',
            200,
          );
        }),
      );
      expect(
        object(await relay.request('/api/snapshot'))['host_instance_id'],
        'host',
      );
      expect(
        () => relay.rpc('controller.bridge.call'),
        throwsA(isA<Rejected>()),
      );
      await expectLater(
        relay.upload('a.hex', Uint8List(1)),
        throwsA(isA<Rejected>()),
      );
      relay.close();
    },
  );
}
