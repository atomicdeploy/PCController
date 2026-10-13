import 'package:flutter/material.dart';
import 'package:flutter/services.dart';
import 'package:flutter_test/flutter_test.dart';
import 'package:shared_preferences/shared_preferences.dart';
import 'package:pccontroller_mobile/main.dart';
import 'package:pccontroller_mobile/unified/preferences.dart';
import 'package:pccontroller_mobile/unified/transport.dart';
import 'package:pccontroller_mobile/unified/workspace_chrome.dart';
import 'package:pccontroller_mobile/unified/app_upgrade.dart';
import 'package:pccontroller_mobile/unified/endpoint_dialog.dart';
import 'package:pccontroller_mobile/pakar_reference.dart' show studioDark;

void main() {
  setUp(() => SharedPreferences.setMockInitialValues({}));
  testWidgets(
    'verified update requests permission and install separately; pending receipt is not success',
    (tester) async {
      final methods = <String>[];
      tester.binding.defaultBinaryMessenger.setMockMethodCallHandler(
        AppUpgrade.channel,
        (call) async {
          methods.add(call.method);
          if (call.method == 'permission') {
            return {
              'state': 'verified',
              'transfer_id': 'fixture',
              'install_permission': true,
            };
          }
          if (call.method == 'install') {
            return {'state': 'awaiting_android', 'session_id': 1};
          }
          if (call.method == 'status') {
            return {
              'state': 'idle',
              'install_status': '{"status":-1,"session_id":1}',
            };
          }
          return {};
        },
      );
      addTearDown(
        () => tester.binding.defaultBinaryMessenger.setMockMethodCallHandler(
          AppUpgrade.channel,
          null,
        ),
      );
      await tester.pumpWidget(const UnifiedControllerApp(restore: false));
      final dynamic home = tester.state(find.byType(ControllerHome));
      home.upgrades.state = {
        'state': 'verified',
        'transfer_id': 'fixture',
        'install_permission': false,
      };
      home.selectActivity(4);
      await tester.pumpAndSettle();
      expect(methods, isEmpty);
      await tester.ensureVisible(find.text('Allow installation'));
      await tester.tap(find.text('Allow installation'));
      await tester.pumpAndSettle();
      expect(methods, ['permission']);
      await tester.tap(find.text('Install'));
      await tester.pumpAndSettle();
      expect(methods, ['permission', 'install']);
      expect(find.text('Awaiting Android'), findsOneWidget);
      await home.refreshUpgrade();
      await tester.pumpAndSettle();
      expect(find.text('Awaiting consent'), findsOneWidget);
      expect(find.text('Installed'), findsNothing);
      await tester.pumpWidget(const SizedBox());
    },
  );
  testWidgets(
    'offline workspace omits unknown telemetry and dormant auth; one lifecycle action',
    (tester) async {
      await tester.pumpWidget(const UnifiedControllerApp(restore: false));
      final dynamic home = tester.state(find.byType(ControllerHome));
      final PeerSession board = home.board;
      expect(find.byType(NavigationBar), findsNothing);
      expect(find.byType(NavigationRail), findsNothing);
      expect(find.byType(ActivityStrip), findsNothing);
      expect(find.text('Voltage'), findsNothing);
      expect(find.text('Temperature'), findsNothing);
      expect(find.textContaining('Unavailable'), findsNothing);
      expect(find.byTooltip('Connect PCController'), findsOneWidget);
      expect(find.byTooltip('Disconnect PCController'), findsNothing);
      board.enabled = true;
      board.notifyListeners();
      await tester.pump();
      expect(find.byTooltip('Connect PCController'), findsNothing);
      expect(find.byTooltip('Cancel connection PCController'), findsOneWidget);
      await tester.tap(find.byTooltip('Cancel connection PCController'));
      await tester.pumpAndSettle();
      expect(board.enabled, false);
      expect(home.boardDiscoveryBlocked, true);
      expect(
        (await SharedPreferences.getInstance()).getBool(
          'pccontroller.discovery-blocked',
        ),
        true,
      );
      await tester.tap(find.byTooltip('Connect PCController'));
      await tester.pumpAndSettle();
      expect(find.byType(EndpointDialog), findsOneWidget);
      expect(find.text('Endpoint'), findsOneWidget);
      expect(find.textContaining('Access token'), findsNothing);
      await tester.tap(find.text('Cancel'));
      await tester.pumpAndSettle();
      await tester.pumpWidget(const SizedBox());
    },
  );

  testWidgets(
    'sparse authoritative snapshot shows facts without empty sensor cards',
    (tester) async {
      await tester.pumpWidget(const UnifiedControllerApp(restore: false));
      final dynamic home = tester.state(find.byType(ControllerHome));
      final PeerSession board = home.board;
      board.online = true;
      board.received = DateTime.now();
      board.state = {
        'connected': true,
        'have_status': true,
        'status_updated': DateTime.now().toUtc().toIso8601String(),
        'status': {'ina219_available': false, 'program_mode': 'idle'},
        'hello': {'name': 'Fixture board'},
      };
      board.notifyListeners();
      await tester.pumpAndSettle();
      expect(find.text('Fixture board'), findsOneWidget);
      expect(find.text('Voltage'), findsNothing);
      expect(find.text('Current'), findsNothing);
      expect(find.text('Temperature'), findsNothing);
      expect(find.text('Released'), findsNothing);
      expect(find.byTooltip('Disconnect PCController'), findsOneWidget);
      expect(find.byTooltip('Connect PCController'), findsNothing);
      expect(tester.takeException(), null);
      await tester.pumpWidget(const SizedBox());
    },
  );

  testWidgets(
    'branding invokes Android task adapter with artwork and persists independently of activity',
    (tester) async {
      final calls = <MethodCall>[];
      tester.binding.defaultBinaryMessenger.setMockMethodCallHandler(
        AppPreferences.native,
        (call) async {
          calls.add(call);
          return null;
        },
      );
      addTearDown(
        () => tester.binding.defaultBinaryMessenger.setMockMethodCallHandler(
          AppPreferences.native,
          null,
        ),
      );
      final prefs = AppPreferences();
      await prefs.setBranding('pealayer');
      expect(calls.single.method, 'setTask');
      expect(calls.single.arguments['title'], 'Pealayer');
      expect(calls.single.arguments['icon'], isNotEmpty);
      expect(
        (await SharedPreferences.getInstance()).getString(
          'appearance.branding',
        ),
        'pealayer',
      );
      prefs.palette = 'pakar';
      expect(prefs.scheme(Brightness.dark).primary, studioDark.primary);
      expect(prefs.scheme(Brightness.dark).surface, studioDark.surface);
      expect(
        prefs.scheme(Brightness.dark).surfaceContainerLowest,
        const Color(0xff080a0e),
      );
      prefs.palette = 'custom';
      prefs.customColor = const Color(0xffcc5500);
      expect(prefs.scheme(Brightness.light).brightness, Brightness.light);
      prefs.dispose();
    },
  );

  for (final fixture in [
    (const Size(320, 800), 1.0),
    (const Size(320, 800), 1.2),
    (const Size(320, 800), 1.4),
    (const Size(320, 800), 2.0),
    (const Size(1280, 800), 1.4),
  ]) {
    final size = fixture.$1;
    testWidgets(
      'activity workspace handles RTL and ${fixture.$2}x text at $size',
      (tester) async {
        tester.view.physicalSize = size;
        tester.view.devicePixelRatio = 1;
        addTearDown(tester.view.resetPhysicalSize);
        addTearDown(tester.view.resetDevicePixelRatio);
        final prefs = AppPreferences();
        await tester.pumpWidget(
          MaterialApp(
            home: MediaQuery(
              data: MediaQueryData(
                size: size,
                textScaler: TextScaler.linear(fixture.$2),
              ),
              child: Directionality(
                textDirection: TextDirection.rtl,
                child: ControllerHome(restore: false, preferences: prefs),
              ),
            ),
          ),
        );
        await tester.pumpAndSettle();
        expect(tester.takeException(), null);
        final dynamic home = tester.state(find.byType(ControllerHome));
        final PeerSession board = home.board;
        for (final state in [
          (
            false,
            true,
            false,
            null,
            'Cancel connection PCController',
            'Connecting',
          ),
          (
            false,
            true,
            false,
            'Read failed',
            'Cancel connection PCController',
            'Reconnecting',
          ),
          (true, true, true, null, 'Disconnect PCController', 'Connected'),
          (true, true, false, null, 'Disconnect PCController', 'Host only'),
        ]) {
          board.online = state.$1;
          board.enabled = state.$2;
          board.error = state.$4;
          board.received = DateTime.now();
          board.state = {
            'host_instance_id': 'fixture',
            'connection_generation': 1,
            'connected': state.$3,
          };
          board.notifyListeners();
          await tester.pumpAndSettle();
          expect(find.byTooltip(state.$5), findsOneWidget);
          expect(find.text(state.$6), findsOneWidget);
          expect(
            find.byTooltip('Manage PCController connection'),
            findsOneWidget,
          );
          expect(tester.takeException(), null, reason: state.$6);
        }
        await tester.pumpWidget(const SizedBox());
        prefs.dispose();
      },
    );
  }
}
