import 'package:flutter/material.dart';
import 'package:flutter_test/flutter_test.dart';
import 'package:shared_preferences/shared_preferences.dart';
import 'package:pccontroller_mobile/main.dart';
import 'package:pccontroller_mobile/unified/transport.dart';

void main() {
  testWidgets(
    'saved relay layout switches compact header to explicit ON/OFF while retaining observed state',
    (tester) async {
      SharedPreferences.setMockInitialValues({});
      await tester.pumpWidget(const UnifiedControllerApp(restore: false));
      final dynamic home = tester.state(find.byType(ControllerHome));
      final PeerSession board = home.board;
      board.online = true;
      board.received = DateTime.now();
      board.state = {
        'connected': true,
        'have_status': true,
        'status_updated': DateTime.now().toUtc().toIso8601String(),
        'status': {'active_relays': 0},
      };
      board.peripherals = {
        'controls': [
          {
            'key': 'relay.5',
            'kind': 'relay',
            'control': 'relay',
            'name': 'Fixture relay',
            'actions': [
              {'id': 'relay-on', 'name': 'ON', 'verb': 'on'},
              {'id': 'relay-off', 'name': 'OFF', 'verb': 'off'},
            ],
          },
        ],
      };
      home.selectActivity(1);
      await tester.pumpAndSettle();
      expect(home.preferences.relayLayout, 'compact');
      expect(find.byTooltip('ON Fixture relay'), findsOneWidget);
      expect(find.text('ON'), findsNothing);
      expect(find.text('Off'), findsOneWidget);
      home.preferences.relayLayout = 'explicit';
      await home.preferences.save();
      await tester.pumpAndSettle();
      expect(find.text('ON'), findsOneWidget);
      expect(find.text('OFF'), findsOneWidget);
      expect(find.byTooltip('ON Fixture relay'), findsNothing);
      expect(find.text('Off'), findsOneWidget);
      expect(
        (await SharedPreferences.getInstance()).getString(
          'controls.relay-layout',
        ),
        'explicit',
      );
      home.preferences.relayLayout = 'compact';
      await home.preferences.save();
      board.state = {...board.state, 'have_status': false};
      board.notifyListeners();
      await tester.pumpAndSettle();
      expect(find.byTooltip('ON Fixture relay'), findsNothing);
      expect(find.byTooltip('Set power Fixture relay'), findsOneWidget);
      expect(find.text('Unknown'), findsOneWidget);
      await tester.tap(find.byTooltip('Set power Fixture relay'));
      await tester.pumpAndSettle();
      expect(find.text('ON'), findsOneWidget);
      expect(find.text('OFF'), findsOneWidget);
      board.generation++;
      await tester.tap(find.text('ON'));
      await tester.pumpAndSettle();
      expect(
        find.text('Channel changed; reopen its controls.'),
        findsOneWidget,
      );
      expect(tester.takeException(), null);
      await tester.pumpWidget(const SizedBox());
    },
  );
}
