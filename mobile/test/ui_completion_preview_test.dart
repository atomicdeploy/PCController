import 'dart:io';
import 'dart:ui' as ui;

import 'package:flutter/material.dart';
import 'package:flutter/rendering.dart';
import 'package:flutter/services.dart';
import 'package:flutter_test/flutter_test.dart';
import 'package:shared_preferences/shared_preferences.dart';
import 'package:pccontroller_mobile/main.dart';
import 'package:pccontroller_mobile/unified/transport.dart';

void main() {
  final directory = Platform.environment['PC_UI_GAPS_PREVIEW_DIR'];
  testWidgets('controlled phone tablet and editor previews', (tester) async {
    for (final entry in [
      ('Vazirmatn', 'assets/fonts/Vazirmatn-Regular.ttf'),
      ('MaterialIcons', 'fonts/MaterialIcons-Regular.otf'),
      ('DroidSansMono', 'assets/fonts/DroidSansMono.ttf'),
    ]) {
      await (FontLoader(entry.$1)..addFont(rootBundle.load(entry.$2))).load();
    }
    Future<void> capture(GlobalKey key, String name) async {
      await tester.runAsync(() async {
        for (final brand in ['pccontroller', 'pealayer']) {
          await precacheImage(
            AssetImage('assets/branding/$brand.png'),
            tester.element(find.byType(Scaffold).first),
          );
        }
      });
      await tester.pumpAndSettle();
      expect(tester.takeException(), null);
      await tester.runAsync(() async {
        final render =
            key.currentContext!.findRenderObject() as RenderRepaintBoundary;
        final picture = await render.toImage();
        final bytes = await picture.toByteData(format: ui.ImageByteFormat.png);
        final file = File('$directory/$name.png');
        await file.parent.create(recursive: true);
        await file.writeAsBytes(bytes!.buffer.asUint8List());
        picture.dispose();
      });
    }

    for (final entry in [
      ('phone-live', const Size(412, 800)),
      ('tablet-live', const Size(1280, 800)),
    ]) {
      SharedPreferences.setMockInitialValues({});
      tester.view.physicalSize = entry.$2;
      tester.view.devicePixelRatio = 1;
      final key = GlobalKey();
      await tester.pumpWidget(
        RepaintBoundary(
          key: key,
          child: const UnifiedControllerApp(restore: false),
        ),
      );
      final dynamic home = tester.state(find.byType(ControllerHome));
      final PeerSession board = home.board;
      await capture(key, '${entry.$1}-offline');
      board.online = true;
      board.received = DateTime.now();
      board.state = {
        'host_instance_id': 'fixture',
        'connection_generation': 1,
        'connected': true,
        'have_status': true,
        'status_updated': DateTime.now().toUtc().toIso8601String(),
        'status': {
          'ina219_available': true,
          'bus_mv': 12500,
          'current_ma': 280,
          'power_mw': 3500,
          'active_relays': 16,
        },
      };
      home.preferences.refresh();
      await tester.pumpAndSettle();
      await capture(key, entry.$1);
      home.preferences.theme = ThemeMode.dark;
      home.preferences.tone = 'pastel';
      home.preferences.refresh();
      await capture(key, '${entry.$1}-pastel-dark');
      await tester.pumpWidget(const SizedBox());
    }
    for (final entry in [
      ('phone-preferences', const Size(412, 900)),
      ('tablet-effect-pastel', const Size(1280, 800)),
      ('phone-effect', const Size(360, 780)),
    ]) {
      SharedPreferences.setMockInitialValues({});
      tester.view.physicalSize = entry.$2;
      final key = GlobalKey();
      await tester.pumpWidget(
        RepaintBoundary(
          key: key,
          child: const UnifiedControllerApp(restore: false),
        ),
      );
      final dynamic home = tester.state(find.byType(ControllerHome));
      if (entry.$1.contains('pastel')) {
        home.preferences.tone = 'pastel';
        home.preferences.theme = ThemeMode.dark;
        home.preferences.refresh();
        await tester.pumpAndSettle();
      }
      if (entry.$1 == 'phone-preferences') {
        home.openSettings();
      } else {
        home.editEffect({
          'id': 'arrival',
          'name': 'Arrival lights',
          'category': 'Lighting',
          'kind': 'sequence',
          'steps': [
            {
              'kind': 'buzzer',
              'at_us': 0,
              'duration_ms': 115,
              'frequency_hz': 880,
            },
            {
              'kind': 'rgb',
              'at_us': 115000,
              'duration_ms': 1000,
              'red': 70,
              'green': 155,
              'blue': 220,
              'brightness': 180,
            },
          ],
        });
      }
      await tester.pumpAndSettle();
      await capture(key, entry.$1);
      await tester.pumpWidget(const SizedBox());
    }
    tester.view.resetPhysicalSize();
    tester.view.resetDevicePixelRatio();
  }, skip: directory == null);
}
