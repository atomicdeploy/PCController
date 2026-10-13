import 'package:flutter/material.dart';
import 'package:flutter_test/flutter_test.dart';
import 'package:shared_preferences/shared_preferences.dart';
import 'package:pccontroller_mobile/main.dart';
import 'package:pccontroller_mobile/unified/transport.dart';
import 'package:pccontroller_mobile/unified/workspace_chrome.dart';

void main() {
  for (final size in [const Size(360, 800), const Size(1280, 800)]) {
    testWidgets(
      'navigation starts after connection and offline drafts remain accessible at $size',
      (tester) async {
        SharedPreferences.setMockInitialValues({});
        tester.view.physicalSize = size;
        tester.view.devicePixelRatio = 1;
        addTearDown(tester.view.resetPhysicalSize);
        addTearDown(tester.view.resetDevicePixelRatio);
        await tester.pumpWidget(const UnifiedControllerApp(restore: false));
        final dynamic home = tester.state(find.byType(ControllerHome));
        final PeerSession board = home.board;
        expect(find.byType(ActivityStrip), findsNothing);
        expect(find.text('Live'), findsNothing);
        board.enabled = true;
        board.notifyListeners();
        await tester.pumpAndSettle();
        expect(find.byType(ActivityStrip), findsNothing);
        board.online = true;
        board.received = DateTime.now();
        board.state = {'connected': false};
        board.notifyListeners();
        await tester.pumpAndSettle();
        expect(find.byType(ActivityStrip), findsOneWidget);
        expect(find.text('Channels'), findsNothing);
        expect(find.text('Panel'), findsNothing);
        // Stale data must not remove navigation during recovery.
        board.received = DateTime.now().subtract(const Duration(seconds: 20));
        board.notifyListeners();
        await tester.pumpAndSettle();
        expect(find.byType(ActivityStrip), findsOneWidget);
        board.disconnect();
        await tester.pumpAndSettle();
        expect(find.byType(ActivityStrip), findsNothing);
        await tester.tap(find.byTooltip('Workspace'));
        await tester.pumpAndSettle();
        await tester.tap(find.text('Local drafts'));
        await tester.pumpAndSettle();
        expect(home.section, 3);
        expect(find.byType(ActivityStrip), findsNothing);
        expect(tester.takeException(), null);
        await tester.pumpWidget(const SizedBox());
      },
    );
  }

  testWidgets(
    'activity icons keep distinct semantic colors in light and dark palettes',
    (tester) async {
      for (final brightness in Brightness.values) {
        await tester.pumpWidget(
          MaterialApp(
            theme: ThemeData(brightness: brightness),
            home: Scaffold(body: ActivityStrip(selected: 3, changed: (_) {})),
          ),
        );
        final icons = tester
            .widgetList<Icon>(
              find.descendant(
                of: find.byType(ActivityStrip),
                matching: find.byType(Icon),
              ),
            )
            .toList();
        expect(icons, hasLength(7));
        expect(icons.map((icon) => icon.color).toSet(), hasLength(7));
        expect(
          tester.getSize(find.byTooltip('Studio')).height,
          greaterThanOrEqualTo(44),
        );
        expect(tester.takeException(), null);
      }
      await tester.pumpWidget(const SizedBox());
    },
  );
}
