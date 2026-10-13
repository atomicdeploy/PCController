import 'dart:async';
import 'dart:io';
import 'dart:ui' as ui;

import 'package:flutter/material.dart';
import 'package:flutter/rendering.dart';
import 'package:flutter/services.dart';
import 'package:flutter_test/flutter_test.dart';
import 'package:pccontroller_mobile/main.dart';
import 'package:pccontroller_mobile/unified/endpoint_dialog.dart';

void main() {
  final directory = Platform.environment['PC_ENDPOINT_PREVIEW_DIR'];
  testWidgets(
    'opt-in source renderer checks actual connection modal themes and keyboard layout',
    (tester) async {
      for (final font in [
        ('Vazirmatn', 'assets/fonts/Vazirmatn-Regular.ttf'),
        ('MaterialIcons', 'fonts/MaterialIcons-Regular.otf'),
        ('Phosphor', 'assets/fonts/Phosphor.ttf'),
        ('DroidSansMono', 'assets/fonts/DroidSansMono.ttf'),
      ]) {
        await (FontLoader(font.$1)..addFont(rootBundle.load(font.$2))).load();
      }
      final boundary = GlobalKey();
      for (final fixture in [
        (
          'phone-vibrant',
          const Size(412, 700),
          ThemeMode.light,
          'vibrant',
          0.0,
          '۱۹۲.۰.۲.۲۰:۸۷۸۷',
        ),
        (
          'tablet-pastel-dark',
          const Size(1280, 800),
          ThemeMode.dark,
          'pastel',
          0.0,
          'https://pc.example:8787/api/rpc',
        ),
        (
          'phone-keyboard-error',
          const Size(320, 480),
          ThemeMode.light,
          'pastel',
          220.0,
          'pc.example:65536',
        ),
      ]) {
        tester.view.physicalSize = fixture.$2;
        tester.view.devicePixelRatio = 1;
        tester.view.viewInsets = FakeViewPadding(bottom: fixture.$5);
        await tester.pumpWidget(
          RepaintBoundary(
            key: boundary,
            child: const UnifiedControllerApp(restore: false),
          ),
        );
        final dynamic home = tester.state(find.byType(ControllerHome));
        await tester.runAsync(() async {
          final context = tester.element(find.byType(ControllerHome));
          await precacheImage(
            const AssetImage('assets/branding/pccontroller.png'),
            context,
          );
          await precacheImage(
            const AssetImage('assets/branding/pealayer.png'),
            context,
          );
        });
        home.preferences.theme = fixture.$3;
        home.preferences.tone = fixture.$4;
        home.preferences.refresh();
        unawaited(home.configure(true));
        await tester.pumpAndSettle();
        await tester.enterText(
          find.byKey(const ValueKey('peer-address')),
          fixture.$6,
        );
        await tester.pumpAndSettle();
        expect(find.byType(EndpointDialog), findsOneWidget);
        expect(find.byTooltip('Close'), findsOneWidget);
        expect(tester.takeException(), null);
        await tester.runAsync(() async {
          final render =
              boundary.currentContext!.findRenderObject()
                  as RenderRepaintBoundary;
          final image = await render.toImage();
          final bytes = await image.toByteData(format: ui.ImageByteFormat.png);
          final path = File('$directory/${fixture.$1}.png');
          await path.parent.create(recursive: true);
          await path.writeAsBytes(bytes!.buffer.asUint8List());
          image.dispose();
        });
        await tester.tap(find.byTooltip('Close'));
        await tester.pumpAndSettle();
        await tester.pumpWidget(const SizedBox());
      }
      tester.view.resetPhysicalSize();
      tester.view.resetDevicePixelRatio();
      tester.view.resetViewInsets();
    },
    skip: directory == null,
  );
}
