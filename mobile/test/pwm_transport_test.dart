import 'dart:async';
import 'dart:convert';

import 'package:flutter_test/flutter_test.dart';
import 'package:http/http.dart' as http;
import 'package:http/testing.dart';
import 'package:pccontroller_mobile/unified/transport.dart';
import 'package:flutter/material.dart';
import 'package:pccontroller_mobile/main.dart';

Json values(int percent) => {
  'available': true,
  'channels': [
    {'channel': 0, 'logical_percent': percent},
  ],
};
void prepare(PeerSession peer) {
  peer.online = true;
  peer.received = DateTime.now();
  peer.state = {
    'connected': true,
    'host_instance_id': 'fixture',
    'connection_generation': 1,
  };
  peer.peripherals = {
    'controls': [
      {'key': 'pwm.0', 'control': 'pwm-user'},
    ],
  };
}

void main() {
  test('native acknowledged write without confirmation retains stale PWM and never disconnects or retries', () async {
    var writes = 0;
    final peer = PeerSession(controller: true);
    prepare(peer);
    peer.pwmValues = values(42);
    peer.pwmUpdated = DateTime.now();
    peer.transport = PeerTransport(
      'http://fixture',
      client: MockClient((request) async {
        final rpc = object(jsonDecode(request.body));
        expect(rpc['method'], 'controller.pwm.set');
        writes++;
        return http.Response(
          jsonEncode({
            'jsonrpc': '2.0',
            'id': rpc['id'],
            'error': {
              'code': -32010,
              'message': 'Confirmation unavailable',
              'data': {
                'kind': 'confirmation_unavailable',
                'resource': 'pwm',
                'write_acknowledged': true,
                'retryable': false,
              },
            },
          }),
          200,
        );
      }),
    );
    await expectLater(
      peer.setPwm(0, 70, expectedTarget: peer.targetStamp),
      throwsA(isA<ConfirmationUnavailable>()),
    );
    expect(writes, 1);
    expect(peer.online, true);
    expect(peer.pwmReadbackFresh, false);
    expect(objects(peer.pwmValues['channels']).single['logical_percent'], 42);
    peer.dispose();
  });
  test('expert PWM command fences pending pre-write reads without clearing last observation', () async {
    final gate = Completer<http.Response>();
    final peer = PeerSession(controller: true);
    prepare(peer);
    peer.pwmValues = values(42);
    peer.pwmUpdated = DateTime.now();
    peer.transport = PeerTransport(
      'http://fixture',
      client: MockClient((request) => gate.future),
    );
    final read = peer.refreshPwm();
    await Future<void>.delayed(Duration.zero);
    final write = peer.beginPwmCommand(peer.targetStamp);
    gate.complete(
      http.Response(
        jsonEncode({
          'available': true,
          'channels': [
            {'channel': 0, 'logical_percent': 10},
          ],
        }),
        200,
      ),
    );
    await read;
    expect(objects(peer.pwmValues['channels']).single['logical_percent'], 42);
    expect(peer.pwmReadbackFresh, false);
    peer.finishPwmCommand(write, peer.generation);
    peer.dispose();
  });
  testWidgets(
    'protected PWM automation preserves fractional percentage and receipt schema',
    (tester) async {
      await tester.pumpWidget(const UnifiedControllerApp(restore: false));
      final dynamic home = tester.state(find.byType(ControllerHome));
      final PeerSession peer = home.board;
      prepare(peer);
      home.automationActuation = true;
      var calls = 0;
      peer.transport = PeerTransport(
        'http://fixture',
        client: MockClient((request) async {
          calls++;
          final rpc = object(jsonDecode(request.body));
          expect(rpc['method'], 'controller.pwm.set');
          expect(object(rpc['params'])['percent'], 18.75);
          return http.Response(
            jsonEncode({
              'jsonrpc': '2.0',
              'id': rpc['id'],
              'result': values(19),
            }),
            200,
          );
        }),
      );
      final result = object(
        await tester.runAsync<dynamic>(
          () => home.automation.dispatch('board.pwm.set', {
            'channel': 0,
            'percent': 18.75,
            'expected_target': peer.targetStamp,
          }),
        ),
      );
      expect(calls, 1);
      expect(result['available'], true);
      expect(objects(result['channels']).single['logical_percent'], 19);
      await tester.pumpWidget(const SizedBox());
    },
  );
  test('a pre-write read cannot overwrite authoritative typed PWM receipt; no write retry', () async {
    final stale = Completer<http.Response>();
    final methods = <String>[];
    final peer = PeerSession(controller: true);
    peer.transport = PeerTransport(
      'http://fixture',
      client: MockClient((request) async {
        final rpc = object(jsonDecode(request.body));
        methods.add(rpc['method']);
        if (rpc['method'] == 'controller.pwm.values') return stale.future;
        expect(rpc['method'], 'controller.pwm.set');
        expect(rpc['params'], {'channel': 0, 'percent': 75});
        return http.Response(
          jsonEncode({'jsonrpc': '2.0', 'id': rpc['id'], 'result': values(74)}),
          200,
        );
      }),
    );
    prepare(peer);
    final read = peer.refreshPwm();
    await Future<void>.delayed(Duration.zero);
    final actual = await peer.setPwm(0, 75, expectedTarget: peer.targetStamp);
    expect(actual, 74);
    stale.complete(
      http.Response(
        jsonEncode({'jsonrpc': '2.0', 'id': 1, 'result': values(10)}),
        200,
      ),
    );
    await read;
    expect(objects(peer.pwmValues['channels']).single['logical_percent'], 74);
    expect(methods.where((m) => m == 'controller.pwm.set'), hasLength(1));
    peer.dispose();
  });
  test('changed owner rejects an old PWM acknowledgement without applying its cache', () async {
    final receipt = Completer<http.Response>();
    var calls = 0;
    final peer = PeerSession(controller: true);
    peer.transport = PeerTransport(
      'http://fixture',
      client: MockClient((request) async {
        calls++;
        return receipt.future;
      }),
    );
    prepare(peer);
    final write = peer.setPwm(0, 70, expectedTarget: peer.targetStamp);
    final assertion = expectLater(write, throwsA(isA<Uncertain>()));
    await Future<void>.delayed(Duration.zero);
    peer.state = {...peer.state, 'connection_generation': 2};
    receipt.complete(
      http.Response(
        jsonEncode({'jsonrpc': '2.0', 'id': 1, 'result': values(70)}),
        200,
      ),
    );
    await assertion;
    expect(calls, 1);
    expect(peer.pwmValues, isEmpty);
    peer.dispose();
  });
}
