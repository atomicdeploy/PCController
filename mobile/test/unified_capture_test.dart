import 'dart:io';

import 'package:flutter/material.dart';
import 'package:flutter/rendering.dart';
import 'package:flutter/services.dart';
import 'package:flutter_test/flutter_test.dart';
import 'package:pccontroller_mobile/main.dart';
import 'package:pccontroller_mobile/unified/effect_editor.dart';

import 'dart:ui' as ui;

void main() {
  final directory = Platform.environment['PC_MOBILE_CAPTURE_DIR'];
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
