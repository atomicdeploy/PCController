import 'dart:io';
import 'dart:ui' as ui;

import 'package:flutter/material.dart';
import 'package:flutter/rendering.dart';
import 'package:flutter/services.dart';
import 'package:flutter_test/flutter_test.dart';
import 'package:pccontroller_mobile/pakar_reference.dart';
import 'package:pccontroller_mobile/models.dart';
import 'package:pccontroller_mobile/device_settings_page.dart';
import 'package:pccontroller_mobile/firmware_page.dart';
import 'package:pccontroller_mobile/bridge_page.dart';
import 'package:pccontroller_mobile/mirroring.dart';

import 'mirroring_test.dart' show mirroredPeer, SilentOutput;

import 'peer_fixture.dart';
import 'device_settings_page_test.dart' show SettingsApi;
import 'firmware_page_test.dart' show FirmwareApi;

void main() {
  final path = Platform.environment['PAKAR_MOBILE_CAPTURE_PATH'];
  final view = Platform.environment['PAKAR_MOBILE_CAPTURE_VIEW'] ?? 'Table';
  testWidgets(
    'Explicit simulated renderer capture, no production IPC or device writes',
    (tester) async {
      if (path == null) return;
      expect([
        'Table',
        'SettingsTablet',
        'SettingsPhone',
        'FirmwareTablet',
        'FirmwarePhone',
        'BridgeTablet',
        'BridgePhone',
        'FrontPanelTablet',
        'FrontPanelPhone',
      ], contains(view));
      final firmware = view.startsWith('Firmware');
      final bridge = view.startsWith('Bridge');
      final frontPanel = view.startsWith('FrontPanel');
      final mirror = PeripheralMirror(output: SilentOutput())
        ..observe(mirroredPeer(), 1);
      addTearDown(mirror.dispose);
      final settings = view != 'Table', phone = view.endsWith('Phone');
      final api = SettingsApi();
      final firmwareApi = FirmwareApi();
      addTearDown(api.close);
      addTearDown(firmwareApi.close);
      final output = File(path);
      expect(output.isAbsolute, true);
      expect(
        output.existsSync(),
        false,
        reason: 'Never overwrite capture evidence',
      );
      for (final font in ['Vazirmatn', 'Phosphor', 'MaterialIcons']) {
        final loader = FontLoader(font)
          ..addFont(
            rootBundle.load(
              font == 'MaterialIcons'
                  ? 'fonts/MaterialIcons-Regular.otf'
                  : font == 'Vazirmatn'
                  ? 'assets/fonts/Vazirmatn-Regular.ttf'
                  : 'assets/fonts/Phosphor.ttf',
            ),
          );
        await loader.load();
      }
      tester.view.physicalSize = settings
          ? (phone ? const Size(390, 1500) : const Size(1200, 1100))
          : const Size(1100, 1500);
      tester.view.devicePixelRatio = 1;
      addTearDown(tester.view.resetPhysicalSize);
      addTearDown(tester.view.resetDevicePixelRatio);
      final boundary = GlobalKey();
      await tester.pumpWidget(
        RepaintBoundary(
          key: boundary,
          child: MaterialApp(
            debugShowCheckedModeBanner: false,
            theme: ThemeData(
              fontFamily: 'Vazirmatn',
              colorScheme: studioDark,
              scaffoldBackgroundColor: const Color(0xff080a0e),
              useMaterial3: true,
            ),
            home: Scaffold(
              appBar: AppBar(
                title: const Text('PakarController · Simulated fixture'),
              ),
              body: frontPanel
                  ? FrontPanelPage(
                      status: mirroredPeer(),
                      mirror: mirror,
                      command: (_, [values = const {}]) async {
                        fail('No board mutation in screenshot');
                      },
                    )
                  : bridge
                  ? BridgePage(api: api, observed: () => peerFixture())
                  : firmware
                  ? SingleChildScrollView(
                      child: FirmwarePage(
                        api: firmwareApi,
                        authorized: (feature) =>
                            ['isp', 'ota'].contains(feature),
                        busyChanged: (_) {},
                      ),
                    )
                  : settings
                  ? DeviceSettingsPage(
                      api: api,
                      status: peerFixture(),
                      authorized: peerFixture().peer.has,
                      navigate: (_) {},
                      adoptConnection: (_, _, _) async {},
                    )
                  : TablePage(
                      status: peerFixture(),
                      mapping: CellMap.defaults(),
                      command: (path, [values = const {}]) async {},
                      verifiedCommand: (path, values) async => false,
                    ),
            ),
          ),
        ),
      );
      await tester.pumpAndSettle();
      if (settings && !firmware && !bridge && !frontPanel) {
        await tester.tap(
          phone
              ? find.widgetWithText(ChoiceChip, 'MQTT')
              : find.widgetWithText(ListTile, 'MQTT'),
        );
      } else if (!firmware && !bridge && !frontPanel) {
        await tester.enterText(find.byType(TextField).first, '1–3, 12, 80');
      }
      await tester.pumpAndSettle();
      expect(tester.takeException(), isNull);
      expect(
        api.writes,
        0,
        reason: 'Capture must never issue a device mutation',
      );
      expect(
        firmwareApi.mutations,
        0,
        reason: 'Firmware capture must never issue a device mutation',
      );
      final render =
          boundary.currentContext!.findRenderObject() as RenderRepaintBoundary;
      await tester.runAsync(() async {
        final image = await render.toImage(pixelRatio: 1);
        final bytes = await image.toByteData(format: ui.ImageByteFormat.png);
        expect(bytes, isNotNull);
        await output.writeAsBytes(bytes!.buffer.asUint8List(), flush: true);
        image.dispose();
      });
      mirror.dispose(); // Retire the lease before widget-test invariant checks.
    },
  );
}
