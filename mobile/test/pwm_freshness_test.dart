import 'dart:convert';

import 'package:flutter/material.dart';
import 'package:flutter_test/flutter_test.dart';
import 'package:http/http.dart' as http;
import 'package:http/testing.dart';
import 'package:shared_preferences/shared_preferences.dart';
import 'package:pccontroller_mobile/main.dart';
import 'package:pccontroller_mobile/unified/live_widgets.dart';
import 'package:pccontroller_mobile/unified/pwm_control.dart';
import 'package:pccontroller_mobile/unified/transport.dart';

Json reading(int value) => {
  'available': true,
  'channels': [
    {'channel': 0, 'logical_percent': value},
  ],
};

void main() {
  test('PWM readback is independent of fast status freshness and never invents a value', () {
    final control = <String, dynamic>{'key': 'pwm.0'};
    final current = observeChannel(
      control,
      {'have_status': false},
      reading(25),
      false,
      pwmFresh: true,
    );
    expect(current.percent, 25);
    expect(current.label, '25.0%');
    final stale = observeChannel(
      control,
      {},
      reading(25),
      false,
      pwmFresh: false,
    );
    expect(stale.label, 'Stale');
    expect(stale.percent, 25);
    expect(stale.active, null);
    expect(
      observeChannel(control, {}, {}, false, pwmFresh: false).percent,
      null,
    );
  });

  testWidgets(
    'between normal PWM polls slider and card geometry stay stable; genuine stale readback disables without removing slider',
    (tester) async {
      SharedPreferences.setMockInitialValues({});
      await tester.pumpWidget(const UnifiedControllerApp(restore: false));
      final dynamic home = tester.state(find.byType(ControllerHome));
      final PeerSession board = home.board;
      board.online = true;
      board.received = DateTime.now();
      board.freshnessMs = 1500;
      board.state = {
        'connected': true,
        'have_status': true,
        'status_updated': DateTime.now().toUtc().toIso8601String(),
        'status': {'pwm_available': true},
      };
      board.peripherals = {
        'controls': [
          {
            'key': 'pwm.0',
            'name': 'MOSFET 1',
            'kind': 'mosfet',
            'control': 'pwm-user',
            'order': 0,
          },
        ],
      };
      board.pwmValues = reading(25);
      board.pwmUpdated = DateTime.now().subtract(
        const Duration(milliseconds: 1800),
      );
      home.selectActivity(1);
      await tester.pumpAndSettle();
      final control = find.byType(PwmSetpointControl);
      final bounds = tester.getRect(control);
      expect(find.text('Unknown'), findsNothing);
      expect(find.text('Setpoint 25.0%'), findsOneWidget);
      expect(find.byType(Slider), findsOneWidget);
      // Frequent independent status frames must not age a valid PWM read out.
      for (final age in [2000, 2600, 3500]) {
        board.received = DateTime.now();
        board.state = {...board.state, 'have_status': false};
        board.pwmUpdated = DateTime.now().subtract(Duration(milliseconds: age));
        board.notifyListeners();
        await tester.pump();
        expect(find.text('Unknown'), findsNothing);
        expect(tester.getRect(control), bounds);
      }
      board.pwmUpdated = DateTime.now().subtract(const Duration(seconds: 7));
      board.notifyListeners();
      await tester.pump();
      expect(find.text('Stale'), findsOneWidget);
      expect(find.text('Last known 25.0%'), findsOneWidget);
      expect(tester.widget<Slider>(find.byType(Slider)).onChanged, null);
      expect(tester.getRect(control), bounds);
      board.pwmUpdated = DateTime.now();
      board.pwmValues = reading(30);
      board.notifyListeners();
      await tester.pump();
      expect(find.text('Setpoint 30.0%'), findsOneWidget);
      expect(tester.widget<Slider>(find.byType(Slider)).onChanged, isNotNull);
      expect(tester.getRect(control), bounds);
      expect(tester.takeException(), null);
      await tester.pumpWidget(const SizedBox());
    },
  );

  test('unavailable or incomplete reads retain last good percentage marked stale until recovery', () async {
    final replies = [
      <String, dynamic>{'available': false, 'channels': []},
      <String, dynamic>{'available': true, 'channels': []},
      reading(30),
    ];
    final methods = <String>[];
    final peer = PeerSession(controller: true);
    peer.transport = PeerTransport(
      'http://fixture',
      client: MockClient((request) async {
        final rpc = object(jsonDecode(request.body));
        methods.add('${rpc['method']}');
        return http.Response(
          jsonEncode({
            'jsonrpc': '2.0',
            'id': rpc['id'],
            'result': replies.removeAt(0),
          }),
          200,
        );
      }),
    );
    peer.online = true;
    peer.received = DateTime.now();
    peer.state = {
      'connected': true,
      'host_instance_id': 'fixture',
      'connection_generation': 1,
    };
    peer.peripherals = {
      'controls': [
        {'key': 'pwm.0'},
      ],
    };
    peer.pwmValues = reading(25);
    peer.pwmUpdated = DateTime.now();
    for (var i = 0; i < 2; i++) {
      await peer.refreshPwm();
      expect(peer.pwmValues['channels'][0]['logical_percent'], 25);
      expect(peer.pwmReadbackFresh, false);
    }
    await peer.refreshPwm();
    expect(peer.pwmReadbackFresh, true);
    expect(peer.pwmValues['channels'][0]['logical_percent'], 30);
    expect(methods, everyElement('controller.pwm.values'));
    peer.dispose();
  });
}
