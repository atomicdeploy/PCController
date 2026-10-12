import 'package:flutter/material.dart';
import 'package:flutter_test/flutter_test.dart';
import 'package:pccontroller_mobile/main.dart';

void main() {
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
