import 'package:flutter/material.dart';
import 'package:flutter_test/flutter_test.dart';
import 'package:pccontroller_mobile/main.dart';
import 'package:pccontroller_mobile/unified/transport.dart';

void main() {
  testWidgets(
    'partial panel does not invent key/page truth or retain an unsupported LCD selection',
    (tester) async {
      tester.view.physicalSize = const Size(1280, 1000);
      tester.view.devicePixelRatio = 1;
      addTearDown(tester.view.resetPhysicalSize);
      addTearDown(tester.view.resetDevicePixelRatio);
      await tester.pumpWidget(const UnifiedControllerApp(restore: false));
      final dynamic home = tester.state(find.byType(ControllerHome));
      final PeerSession session = home.board;
      // Memory-only renderer fixture: no transport, SDK worker or output action.
      session.state = {
        'host_instance_id': 'fixture-only',
        'connected': true,
        'have_front_panel': true,
        'front_panel': {
          'lcd_available': true,
          'pressed_keys': 1,
          'menu_page': 2,
        },
      };
      session.online = true;
      session.received = DateTime.now();
      home.displayTarget = 'lcd';
      session.notifyListeners();
      await tester.pumpAndSettle();
      await tester.tap(find.text('Front panel'));
      await tester.pumpAndSettle();
      expect(
        tester
            .widget<DropdownButtonFormField<String>>(
              find.byType(DropdownButtonFormField<String>),
            )
            .initialValue,
        'lcd',
      );
      session.state = {
        ...session.state,
        'have_front_panel': false,
        'front_panel': {
          'lcd_available': false,
          'pressed_keys': 0,
          'menu_page': 0,
        },
      };
      session.notifyListeners();
      await tester.pumpAndSettle();
      expect(
        tester
            .widget<DropdownButtonFormField<String>>(
              find.byType(DropdownButtonFormField<String>),
            )
            .initialValue,
        null,
      );
      expect(find.text('Unavailable'), findsWidgets);
      expect(find.text('0'), findsNothing);
      expect(tester.takeException(), null);
      await tester.pumpWidget(const SizedBox());
    },
  );
  for (final size in [
    const Size(360, 800),
    const Size(800, 1100),
    const Size(1280, 800),
  ]) {
    testWidgets('native navigation and offline controls at $size', (
      tester,
    ) async {
      tester.view.physicalSize = size;
      tester.view.devicePixelRatio = 1;
      addTearDown(tester.view.resetPhysicalSize);
      addTearDown(tester.view.resetDevicePixelRatio);
      await tester.pumpWidget(const UnifiedControllerApp(restore: false));
      await tester.pumpAndSettle();
      expect(find.text('Board connected'), findsNothing);
      expect(find.text('Unavailable'), findsWidgets);
      expect(
        find.byType(size.width >= 760 ? NavigationRail : NavigationBar),
        findsOneWidget,
      );
      expect(tester.takeException(), null);
      await tester.ensureVisible(find.text('Front panel'));
      await tester.pumpAndSettle();
      await tester.tap(find.text('Front panel'));
      await tester.pumpAndSettle();
      expect(find.text('Waiting for physical segment frames'), findsOneWidget);
      expect(tester.takeException(), null);
      await tester.tap(find.text('Pealayer').last);
      await tester.pumpAndSettle();
      expect(find.text('Connect Pealayer to load media state'), findsOneWidget);
      expect(tester.takeException(), null);
      await tester.pumpWidget(const SizedBox());
    });
  }
}
