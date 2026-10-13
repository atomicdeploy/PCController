import 'package:flutter/material.dart';
import 'package:flutter_test/flutter_test.dart';
import 'package:shared_preferences/shared_preferences.dart';
import 'package:pccontroller_mobile/main.dart';
import 'package:pccontroller_mobile/unified/live_widgets.dart';
import 'package:pccontroller_mobile/unified/preferences.dart';
import 'package:pccontroller_mobile/unified/transport.dart';

void main() {
  test('appearance choices persist independently and restore the previous surfaces', () async {
    SharedPreferences.setMockInitialValues({});
    final preferences = AppPreferences();
    expect(preferences.tone, 'vibrant');
    preferences.tone = 'pastel';
    preferences.headerStyle = 'accent';
    preferences.buttonStyle = 'tonal';
    preferences.compact = false;
    await preferences.save();
    final restored = AppPreferences();
    await restored.load();
    expect(restored.tone, 'pastel');
    expect(restored.headerStyle, 'accent');
    expect(restored.buttonStyle, 'tonal');
    expect(restored.compact, false);
    expect(restored.scheme(Brightness.light).surface, const Color(0xfffafafa));
    expect(restored.scheme(Brightness.dark).surface, const Color(0xff232323));
    restored.headerStyle = 'theme';
    expect(restored.effectiveHeader, 'classic');
    restored.tone = 'vibrant';
    expect(restored.effectiveHeader, 'tile');
    restored.headerStyle = 'plain';
    restored.tone = 'pastel';
    expect(restored.effectiveHeader, 'plain');
    preferences.dispose();
    restored.dispose();
  });

  testWidgets(
    'roomy telemetry gives each series its own full-width row and retains connected pill',
    (tester) async {
      SharedPreferences.setMockInitialValues({});
      tester.view.physicalSize = const Size(1280, 800);
      tester.view.devicePixelRatio = 1;
      addTearDown(tester.view.resetPhysicalSize);
      addTearDown(tester.view.resetDevicePixelRatio);
      await tester.pumpWidget(const UnifiedControllerApp(restore: false));
      final dynamic home = tester.state(find.byType(ControllerHome));
      final PeerSession board = home.board;
      board.online = true;
      board.received = DateTime.now();
      board.state = {
        'connected': true,
        'have_status': true,
        'status_updated': DateTime.now().toUtc().toIso8601String(),
        'status': {
          'ina219_available': true,
          'bus_mv': 12500,
          'current_ma': 120,
          'power_mw': 1500,
          'temperature_led_available': true,
          'temperature_led_centi_c': 2250,
          'temperature_bt_audio_available': true,
          'temperature_bt_audio_centi_c': 2300,
        },
      };
      home.preferences.compact = false;
      home.preferences.refresh();
      board.notifyListeners();
      await tester.pumpAndSettle();
      expect(
        find.byWidgetPredicate((w) => w is StatePill && w.label == 'Connected'),
        findsOneWidget,
      );
      final charts = find.byType(TelemetryChart);
      expect(charts, findsNWidgets(5));
      final width = tester.getSize(charts.first).width;
      expect(width, greaterThan(900));
      for (var index = 1; index < 5; index++) {
        expect(tester.getSize(charts.at(index)).width, width);
        expect(
          tester.getTopLeft(charts.at(index)).dy,
          greaterThan(tester.getBottomLeft(charts.at(index - 1)).dy),
        );
      }
      home.preferences.compact = true;
      home.preferences.refresh();
      await tester.pumpAndSettle();
      expect(
        tester.getSize(find.byType(TelemetryChart).first).width,
        lessThan(width / 2),
      );
      expect(tester.takeException(), null);
      await tester.pumpWidget(const SizedBox());
    },
  );

  for (final compact in [true, false]) {
    testWidgets(
      'One connection lifecycle toggle preserves opt-out in ${compact ? 'compact' : 'roomy'} mode',
      (tester) async {
        SharedPreferences.setMockInitialValues({});
        await tester.pumpWidget(const UnifiedControllerApp(restore: false));
        final dynamic home = tester.state(find.byType(ControllerHome));
        final PeerSession board = home.board;
        board.online = true;
        board.received = DateTime.now();
        board.state = {'connected': true};
        home.preferences.compact = compact;
        home.preferences.refresh();
        board.notifyListeners();
        await tester.pumpAndSettle();
        expect(find.byTooltip('Disconnect PCController'), findsOneWidget);
        expect(find.byTooltip('Connect PCController'), findsNothing);
        expect(find.text('Disconnect'), findsNothing);
        await tester.tap(find.byTooltip('Disconnect PCController'));
        await tester.pumpAndSettle();
        expect(board.online, false);
        final saved = await SharedPreferences.getInstance();
        expect(saved.getBool('pccontroller.discovery-blocked'), true);
        expect(saved.getBool('pccontroller.connect'), false);
        expect(tester.takeException(), null);
        await tester.pumpWidget(const SizedBox());
      },
    );
  }

  testWidgets(
    'button styles change real relay actions while retaining lock restrictions',
    (tester) async {
      SharedPreferences.setMockInitialValues({});
      await tester.pumpWidget(const UnifiedControllerApp(restore: false));
      final dynamic home = tester.state(find.byType(ControllerHome));
      final PeerSession board = home.board;
      board.online = true;
      board.received = DateTime.now();
      board.state = {'connected': true};
      board.peripherals = {
        'controls': [
          {
            'key': 'relay.5',
            'kind': 'relay',
            'control': 'relay',
            'name': 'Locked relay',
            'locked': true,
            'actions': [
              {'id': 'relay-on', 'name': 'ON', 'verb': 'on'},
              {'id': 'relay-off', 'name': 'OFF', 'verb': 'off'},
            ],
          },
        ],
      };
      home.preferences.relayLayout = 'explicit';
      home.selectActivity(1);
      await tester.pumpAndSettle();
      expect(find.widgetWithText(OutlinedButton, 'ON'), findsOneWidget);
      for (final style in ['tonal', 'filled']) {
        home.preferences.buttonStyle = style;
        home.preferences.refresh();
        await tester.pumpAndSettle();
        final button = tester.widget<FilledButton>(
          find.widgetWithText(FilledButton, 'ON'),
        );
        expect(button.onPressed, null);
        expect(find.widgetWithText(OutlinedButton, 'ON'), findsNothing);
      }
      expect(tester.takeException(), null);
      await tester.pumpWidget(const SizedBox());
    },
  );
}
