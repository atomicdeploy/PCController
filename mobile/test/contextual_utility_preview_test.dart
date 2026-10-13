import 'dart:io';
import 'dart:ui' as ui;

import 'package:flutter/material.dart';
import 'package:flutter/rendering.dart';
import 'package:flutter/services.dart';
import 'package:flutter_test/flutter_test.dart';
import 'package:pccontroller_mobile/main.dart';
import 'package:pccontroller_mobile/unified/transport.dart';

void main() {
  final directory = Platform.environment['PC_UTILITY_PREVIEW_DIR'];
  testWidgets(
    'opt-in source renderer checks actual hardware workspace and folder modal themes',
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
          'phone-hardware',
          const Size(412, 820),
          ThemeMode.light,
          'vibrant',
          false,
        ),
        (
          'tablet-hardware',
          const Size(1280, 800),
          ThemeMode.dark,
          'pastel',
          false,
        ),
        (
          'phone-folder-dialog',
          const Size(412, 700),
          ThemeMode.light,
          'vibrant',
          true,
        ),
      ]) {
        tester.view.physicalSize = fixture.$2;
        tester.view.devicePixelRatio = 1;
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
        home.preferences.relayLayout = 'explicit';
        final PeerSession board = home.board;
        board.online = true;
        board.received = DateTime.now();
        board.state = {
          'host_instance_id': 'source-fixture',
          'connection_generation': 1,
          'connected': true,
          'have_status': true,
          'status_updated': DateTime.now().toUtc().toIso8601String(),
          'status': {'active_relays': 16},
        };
        Json relay(int id, String name, String icon, String color) => {
          'key': 'relay.$id',
          'name': name,
          'kind': 'relay',
          'control': 'relay',
          'color': color,
          'icon': icon,
          'group': 'Environment',
          'actions': [
            {'id': 'relay.$id.on', 'name': 'ON', 'verb': 'on'},
            {'id': 'relay.$id.off', 'name': 'OFF', 'verb': 'off'},
          ],
        };
        board.peripherals = {
          'board_profile': {'key': 'fixture', 'revision': 'r1'},
          'folders': [
            {'kind': 'relay', 'name': 'Environment'},
            {'kind': 'relay', 'name': 'Lighting'},
          ],
          'controls': [
            relay(5, 'Window', 'fan', '#7166CE'),
            relay(6, 'Lighting', 'lightbulb', '#E0AC62'),
            {
              'key': 'pwm.0',
              'name': 'Light intensity',
              'kind': 'pwm',
              'control': 'pwm-user',
              'icon': 'lightning',
              'group': 'Lighting',
              'color': '#62BCAE',
              'actions': [],
            },
            {
              'key': 'seat.a',
              'name': 'Left seat',
              'kind': 'seat',
              'control': 'seat',
              'icon': 'armchair',
              'group': 'Motion',
              'actions': [
                {'id': 'seat.a.up', 'verb': 'up', 'name': 'Raise'},
                {'id': 'seat.a.down', 'verb': 'down', 'name': 'Lower'},
                {'id': 'seat.a.stop', 'verb': 'stop', 'name': 'Stop'},
              ],
            },
          ],
        };
        board.pwmValues = {
          'available': true,
          'channels': [
            {'channel': 0, 'logical_percent': 42},
          ],
        };
        board.pwmUpdated = DateTime.now();
        home.preferences.refresh();
        home.selectActivity(1);
        await tester.pumpAndSettle();
        if (fixture.$5) {
          await tester.runAsync(() async {
            home.editChannel(
              (board.peripherals['controls'] as List).first,
              'edit',
            );
          });
          await tester.pumpAndSettle();
          expect(find.text('Edit channel'), findsOneWidget);
        }
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
        if (fixture.$5) {
          await tester.tap(find.byTooltip('Close'));
          await tester.pumpAndSettle();
        }
        await tester.pumpWidget(const SizedBox());
      }
      tester.view.resetPhysicalSize();
      tester.view.resetDevicePixelRatio();
      tester.view.resetViewInsets();
    },
    skip: directory == null,
  );
}
