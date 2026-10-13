import 'dart:convert';

import 'package:flutter/material.dart';
import 'package:flutter_test/flutter_test.dart';
import 'package:http/http.dart' as http;
import 'package:http/testing.dart';
import 'package:shared_preferences/shared_preferences.dart';
import 'package:pccontroller_mobile/main.dart';
import 'package:pccontroller_mobile/unified/channel_editor.dart';
import 'package:pccontroller_mobile/unified/live_widgets.dart';
import 'package:pccontroller_mobile/unified/transport.dart';
import 'package:pccontroller_mobile/unified/workspace_chrome.dart';

Future<dynamic> home(WidgetTester tester) async {
  SharedPreferences.setMockInitialValues({});
  await tester.pumpWidget(const UnifiedControllerApp(restore: false));
  final dynamic state = tester.state(find.byType(ControllerHome));
  final PeerSession board = state.board;
  board.online = true;
  board.received = DateTime.now();
  board.state = {
    'host_instance_id': 'fixture',
    'connection_generation': 1,
    'connected': true,
    'have_status': true,
    'status_updated': DateTime.now().toUtc().toIso8601String(),
    'status': <String, dynamic>{'active_relays': 0},
  };
  return state;
}

Json relay() => {
  'key': 'relay.5',
  'name': 'Window',
  'kind': 'relay',
  'control': 'relay',
  'actions': [
    {'id': 'relay.5.on', 'verb': 'on', 'name': 'ON'},
    {'id': 'relay.5.off', 'verb': 'off', 'name': 'OFF'},
  ],
};

void main() {
  testWidgets(
    'folder dropdown retains raw assignment and includes only compatible catalog folders',
    (tester) async {
      await tester.pumpWidget(
        MaterialApp(
          home: Builder(
            builder: (context) => Scaffold(
              body: TextButton(
                onPressed: () => showDialog<void>(
                  context: context,
                  builder: (_) => ChannelEditor(
                    control: {...relay(), 'group': 'Old raw name'},
                    folders: const [
                      {'kind': 'relay', 'name': 'Lighting'},
                      {'kind': 'motion', 'name': 'Seats'},
                    ],
                    controls: const [
                      {'kind': 'relay', 'group': 'Existing assignment'},
                    ],
                  ),
                ),
                child: const Text('Edit'),
              ),
            ),
          ),
        ),
      );
      await tester.tap(find.text('Edit'));
      await tester.pumpAndSettle();
      final dropdown = find.byKey(const ValueKey('channel-folder'));
      expect(dropdown, findsOneWidget);
      expect(find.byType(TextFormField), findsNWidgets(2));
      await tester.tap(dropdown);
      await tester.pumpAndSettle();
      expect(find.text('No folder'), findsOneWidget);
      expect(find.text('Lighting'), findsOneWidget);
      expect(find.text('Existing assignment'), findsOneWidget);
      expect(find.text('Seats'), findsNothing);
      await tester.tap(find.text('Lighting'));
      await tester.pumpAndSettle();
      expect(find.text('Lighting'), findsOneWidget);
      await tester.tap(find.byTooltip('Close'));
      await tester.pumpAndSettle();
      expect(find.text('Edit channel'), findsNothing);
      expect(tester.takeException(), null);
    },
  );

  testWidgets(
    'empty live cards and idle cancellation disappear; actual activity restores cancellation',
    (tester) async {
      final state = await home(tester);
      final PeerSession board = state.board;
      board.state['have_board_name'] = true;
      board.state['board_name'] = {'name': ''};
      board.state['status']['program_mode'] = 'Unavailable';
      board.notifyListeners();
      await tester.pumpAndSettle();
      expect(find.text('Board state'), findsNothing);
      state.selectActivity(2);
      await tester.pumpAndSettle();
      expect(find.text('Live front panel'), findsNothing);
      expect(find.text('Display message'), findsOneWidget);
      state.selectActivity(3);
      await tester.pumpAndSettle();
      expect(find.text('Cancel playback'), findsNothing);
      expect(find.textContaining('No effects advertised'), findsNothing);
      board.state['macros'] = {
        'playback': {'running': true},
      };
      board.notifyListeners();
      await tester.pumpAndSettle();
      expect(find.text('Cancel playback'), findsOneWidget);
      board.state['macros'] = {
        'playback': {'running': false},
      };
      board.notifyListeners();
      await tester.pumpAndSettle();
      expect(find.text('Cancel playback'), findsNothing);
      final PeerSession player = state.player;
      player.online = true;
      player.received = DateTime.now();
      player.state = {
        'hardware_details': {'macro_running': false},
      };
      player.notifyListeners();
      await tester.pumpAndSettle();
      expect(find.text('Effects & timeline'), findsNothing);
      player.state['hardware_details'] = {'macro_running': true};
      player.notifyListeners();
      await tester.pumpAndSettle();
      expect(find.text('Stop effects'), findsOneWidget);
      await tester.pumpWidget(const SizedBox());
    },
  );

  for (final size in [const Size(412, 700), const Size(1280, 480)]) {
    testWidgets(
      'balanced hardware row and indicator preserve guards at $size',
      (tester) async {
        tester.view.physicalSize = size;
        tester.view.devicePixelRatio = 1;
        addTearDown(tester.view.resetPhysicalSize);
        addTearDown(tester.view.resetDevicePixelRatio);
        final state = await home(tester);
        final PeerSession board = state.board;
        final control = relay();
        board.peripherals = {
          'controls': [control],
        };
        state.preferences.relayLayout = 'explicit';
        state.selectActivity(1);
        await tester.pumpAndSettle();
        final on = find.widgetWithText(OutlinedButton, 'ON'),
            off = find.widgetWithText(OutlinedButton, 'OFF');
        expect(tester.getSize(on).width, tester.getSize(off).width);
        expect(tester.getSize(on).width, greaterThan(100));
        expect(tester.getTopLeft(on).dy, tester.getTopLeft(off).dy);
        final pill = tester.widget<StatePill>(find.byType(StatePill));
        expect(pill.onPressed, isNotNull);
        expect(pill.actionLabel, 'ON Window');
        var dispatched = 0;
        board.transport = PeerTransport(
          'http://fixture',
          client: MockClient((request) async {
            final body = jsonDecode(request.body);
            expect(body['method'], 'controller.action.invoke');
            expect(body['params'], {'action_id': 'relay.5.on'});
            dispatched++;
            return http.Response(
              jsonEncode({
                'jsonrpc': '2.0',
                'id': body['id'],
                'result': {'accepted': true},
              }),
              200,
            );
          }),
        );
        board.state['emergency_stop'] = {'active': true};
        pill.onPressed!(); // Captured before E-STOP: dispatch must revalidate.
        await tester.pump();
        expect(dispatched, 0);
        board.notifyListeners();
        await tester.pumpAndSettle();
        expect(
          tester.widget<StatePill>(find.byType(StatePill)).onPressed,
          null,
        );
        board.state['emergency_stop'] = {'active': false};
        board.notifyListeners();
        await tester.pumpAndSettle();
        await tester.runAsync(() async {
          tester.widget<StatePill>(find.byType(StatePill)).onPressed!();
          await Future<void>.delayed(const Duration(milliseconds: 30));
        });
        await tester.pumpAndSettle();
        expect(dispatched, 1); // Typed action, no optimistic state change.
        expect(tester.widget<StatePill>(find.byType(StatePill)).active, false);
        control['locked'] = true;
        board.notifyListeners();
        await tester.pumpAndSettle();
        expect(
          tester.widget<StatePill>(find.byType(StatePill)).onPressed,
          null,
        );
        expect(tester.takeException(), null);
        await tester.pumpWidget(const SizedBox());
      },
    );
  }

  testWidgets('short tablet activity rail scrolls without overflow', (
    tester,
  ) async {
    await tester.pumpWidget(
      MaterialApp(
        home: Scaffold(
          body: SizedBox(
            height: 150,
            child: ActivityStrip(selected: 0, changed: (_) {}, vertical: true),
          ),
        ),
      ),
    );
    await tester.drag(find.byType(ActivityStrip), const Offset(0, -350));
    await tester.pumpAndSettle();
    expect(find.text('Events').hitTestable(), findsOneWidget);
    expect(tester.takeException(), null);
  });
}
