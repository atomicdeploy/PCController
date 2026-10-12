import 'dart:io';
import 'dart:convert';

import 'package:flutter/material.dart';
import 'package:flutter/rendering.dart';
import 'package:flutter/services.dart';
import 'package:flutter_test/flutter_test.dart';
import 'package:pccontroller_mobile/main.dart';
import 'package:pccontroller_mobile/unified/effect_editor.dart';
import 'package:pccontroller_mobile/unified/transport.dart';

import 'dart:ui' as ui;

void main() {
  final directory = Platform.environment['PC_MOBILE_CAPTURE_DIR'];
  final readback = Platform.environment['PC_MOBILE_CAPTURE_READBACK'];
  final readings = Platform.environment['PC_MOBILE_CAPTURE_READINGS'];
  testWidgets(
    'Explicit offline renderer captures without any host or device work',
    (tester) async {
      final font = FontLoader('Vazirmatn')
        ..addFont(rootBundle.load('assets/fonts/Vazirmatn-Regular.ttf'));
      await font.load();
      final icons = FontLoader('MaterialIcons')
        ..addFont(rootBundle.load('fonts/MaterialIcons-Regular.otf'));
      await icons.load();
      await tester.runAsync(() async {
        await rootBundle.load('assets/branding/pccontroller.png');
        await rootBundle.load('assets/branding/pealayer.png');
      });
      final boundary = GlobalKey();
      for (final size in [const Size(412, 915), const Size(1280, 800)]) {
        tester.view.physicalSize = size;
        tester.view.devicePixelRatio = 1;
        await tester.pumpWidget(
          RepaintBoundary(
            key: boundary,
            child: const UnifiedControllerApp(restore: false),
          ),
        );
        await tester.pumpAndSettle();
        if (readback != null) {
          final fixture = object(jsonDecode(File(readback).readAsStringSync()));
          final dynamic home = tester.state(find.byType(ControllerHome));
          final PeerSession peer = home.board;
          peer.state = {
            ...object(fixture['snapshot']),
            'status_updated': DateTime.now().toUtc().toIso8601String(),
          };
          peer.peripherals = object(fixture['peripherals']);
          if (readings != null) {
            final recorded = object(
              jsonDecode(File(readings).readAsStringSync()),
            );
            final samples = objects(recorded['history']);
            if (samples.isNotEmpty) {
              // Replay measured values/relative intervals, not wall-clock age.
              // This harness is a renderer preview, never live device evidence.
              final anchor = DateTime.now().toUtc();
              final last = DateTime.parse('${samples.last['status_updated']}');
              final shift = anchor.difference(last);
              for (final sample in samples) {
                home.history.record({
                  ...sample,
                  'status_updated': DateTime.parse(
                    '${sample['status_updated']}',
                  ).add(shift).toIso8601String(),
                });
              }
              peer.state = {
                ...peer.state,
                'status': samples.last['status'],
                'status_updated': anchor.toIso8601String(),
              };
            }
            peer.pwmValues = object(recorded['pwm']);
            peer.pwmUpdated = DateTime.now();
          }
          peer.online = true;
          peer.received = DateTime.now();
          peer.notifyListeners();
          await tester.pumpAndSettle();
        }
        await tester.runAsync(() async {
          await precacheImage(
            const AssetImage('assets/branding/pccontroller.png'),
            boundary.currentContext!,
          );
        });
        await tester.pumpAndSettle();
        final render =
            boundary.currentContext!.findRenderObject()!
                as RenderRepaintBoundary;
        await tester.runAsync(() async {
          final image = await render.toImage();
          final data = await image.toByteData(format: ui.ImageByteFormat.png);
          final output = File('$directory/offline-${size.width.toInt()}.png');
          expect(output.existsSync(), false);
          await output.writeAsBytes(data!.buffer.asUint8List());
          image.dispose();
        });
        if (readback != null) {
          final dynamic home = tester.state(find.byType(ControllerHome));
          for (final page in [(1, 'outputs'), (2, 'panel')]) {
            home.boardTab = page.$1;
            (home.board as PeerSession).notifyListeners();
            await tester.pumpAndSettle();
            await tester.runAsync(() async {
              final image = await render.toImage();
              final data = await image.toByteData(
                format: ui.ImageByteFormat.png,
              );
              await File('$directory/${page.$2}-${size.width.toInt()}.png')
                  .writeAsBytes(data!.buffer.asUint8List());
              image.dispose();
            });
          }
          tester.platformDispatcher.platformBrightnessTestValue =
              Brightness.dark;
          home.boardTab = 0;
          (home.board as PeerSession).notifyListeners();
          await tester.pumpAndSettle();
          await tester.runAsync(() async {
            final image = await render.toImage();
            final data = await image.toByteData(format: ui.ImageByteFormat.png);
            await File('$directory/dark-monitor-${size.width.toInt()}.png')
                .writeAsBytes(data!.buffer.asUint8List());
            image.dispose();
          });
          tester.platformDispatcher.clearPlatformBrightnessTestValue();
        }
        expect(tester.takeException(), null);
        await tester.pumpWidget(const SizedBox());
        await tester.pumpWidget(
          RepaintBoundary(
            key: boundary,
            child: MaterialApp(
              debugShowCheckedModeBanner: false,
              theme: ThemeData(
                brightness: Brightness.dark,
                fontFamily: 'Vazirmatn',
                useMaterial3: true,
              ),
              home: EffectEditor(
                initial: {
                  'id': '200',
                  'name': 'Example offline draft',
                  'category': 'Effects',
                  'kind': 'sequence',
                  'steps': [
                    {
                      'kind': 'display',
                      'at_us': 0,
                      'duration_ms': 1000,
                      'text': 'HELLO',
                      'destination': 'segments',
                    },
                    {
                      'kind': 'buzzer',
                      'at_us': 1200000,
                      'duration_ms': 115,
                      'frequency_hz': 880,
                      'repeat_count': 3,
                      'repeat_interval_ms': 500,
                    },
                  ],
                },
                saved: (_) async {},
              ),
            ),
          ),
        );
        await tester.pumpAndSettle();
        await tester.runAsync(() async {
          final designerImage =
              await (boundary.currentContext!.findRenderObject()!
                      as RenderRepaintBoundary)
                  .toImage();
          final designerData = await designerImage.toByteData(
            format: ui.ImageByteFormat.png,
          );
          final output = File(
            '$directory/offline-designer-${size.width.toInt()}.png',
          );
          expect(output.existsSync(), false);
          await output.writeAsBytes(designerData!.buffer.asUint8List());
          designerImage.dispose();
        });
        expect(tester.takeException(), null);
        await tester.pumpWidget(const SizedBox());
      }
      tester.view.resetPhysicalSize();
      tester.view.resetDevicePixelRatio();
    },
    skip: directory == null,
  );
}
