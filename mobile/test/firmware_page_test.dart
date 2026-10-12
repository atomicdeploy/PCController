import 'dart:typed_data';

import 'package:flutter/material.dart';
import 'package:flutter_test/flutter_test.dart';
import 'package:pccontroller_mobile/api.dart';
import 'package:pccontroller_mobile/documents.dart';
import 'package:pccontroller_mobile/firmware_page.dart';

import 'firmware_test.dart' show firmwareJob;

class FirmwareApi extends PakarApi {
  Map<String, dynamic> observed = firmwareJob();
  int mutations = 0, reads = 0;
  bool loseProbe = false;
  @override
  Future<Map<String, dynamic>> maintenanceJson(
    String path, {
    bool post = false,
    bool mayMutate = false,
    Map<String, String> form = const {},
    Uint8List? upload,
    String? uploadField,
    required bool Function() authorize,
  }) async {
    if (!authorize()) throw const StaleConnection();
    if (path == '/api/firmware/remote') {
      ++reads;
      return Map.of(observed);
    }
    ++mutations;
    if (loseProbe) throw const OperationUncertain();
    if (path == '/api/avr/probe') return {'signature': '1E950F'};
    return {};
  }
}

class CancelDocuments extends AndroidDocuments {
  int picks = 0;
  @override
  Future<FirmwareDocument?> pick({
    int maximum = 2097152,
    List<String> extensions = const ['bin'],
  }) async {
    ++picks;
    return null;
  }
}

Widget firmwareFixture(
  FirmwareApi api, {
  List<String> features = const ['isp', 'ota'],
  CancelDocuments? documents,
  void Function(bool)? busy,
}) => MaterialApp(
  theme: ThemeData.dark(useMaterial3: true),
  home: Scaffold(
    body: SingleChildScrollView(
      child: FirmwarePage(
        api: api,
        authorized: features.contains,
        busyChanged: busy ?? (_) {},
        documents: documents,
      ),
    ),
  ),
);

void main() {
  for (final size in [const Size(390, 840), const Size(1200, 900)]) {
    testWidgets(
      'Contextual real firmware layout $size has no startup mutation or overflow',
      (tester) async {
        tester.view.physicalSize = size;
        tester.view.devicePixelRatio = 1;
        addTearDown(tester.view.resetPhysicalSize);
        addTearDown(tester.view.resetDevicePixelRatio);
        final api = FirmwareApi();
        await tester.pumpWidget(firmwareFixture(api));
        await tester.pumpAndSettle();
        expect(find.text('Remote firmware · pinned manifest'), findsOneWidget);
        expect(api.mutations, 0);
        expect(api.reads, 1);
        expect(tester.takeException(), isNull);
        await tester.pumpWidget(const SizedBox());
        await api.close();
      },
    );
  }
  testWidgets(
    'Individual ISP/OTA ACK gates remove unrelated cards and targets',
    (tester) async {
      final api = FirmwareApi();
      await tester.pumpWidget(firmwareFixture(api, features: ['isp']));
      await tester.pumpAndSettle();
      expect(find.text('Manual ESP OTA'), findsNothing);
      expect(find.text('AVR ISP · complete flash and EEPROM'), findsOneWidget);
      await tester.pumpWidget(firmwareFixture(api, features: []));
      await tester.pumpAndSettle();
      expect(find.text('Remote firmware · pinned manifest'), findsNothing);
      expect(api.mutations, 0);
      await tester.pumpWidget(const SizedBox());
      await api.close();
    },
  );
  testWidgets(
    'Cancelled file selection never uploads/programs; confirmation locks parent',
    (tester) async {
      tester.view.physicalSize = const Size(1000, 1800);
      tester.view.devicePixelRatio = 1;
      addTearDown(tester.view.resetPhysicalSize);
      addTearDown(tester.view.resetDevicePixelRatio);
      final api = FirmwareApi(), documents = CancelDocuments();
      final lock = <bool>[];
      await tester.pumpWidget(
        firmwareFixture(api, documents: documents, busy: lock.add),
      );
      await tester.pumpAndSettle();
      await tester.ensureVisible(find.text('Import full AVR image'));
      await tester.tap(find.text('Import full AVR image'));
      await tester.pumpAndSettle();
      expect(documents.picks, 0);
      expect(lock, [true]);
      await tester.tap(find.text('Confirm'));
      await tester.pumpAndSettle();
      expect(documents.picks, 1);
      expect(api.mutations, 0);
      expect(lock, [true, false]);
      expect(find.textContaining('File selection cancelled'), findsOneWidget);
      await tester.pumpWidget(const SizedBox());
      await api.close();
    },
  );
  testWidgets(
    'Uncertain probe never retries and stays locked through background read until explicit refresh',
    (tester) async {
      tester.view.physicalSize = const Size(1000, 1800);
      tester.view.devicePixelRatio = 1;
      addTearDown(tester.view.resetPhysicalSize);
      addTearDown(tester.view.resetDevicePixelRatio);
      final api = FirmwareApi()..loseProbe = true;
      await tester.pumpWidget(firmwareFixture(api));
      await tester.pumpAndSettle();
      await tester.ensureVisible(find.text('Probe ISP'));
      await tester.tap(find.text('Probe ISP'));
      await tester.pumpAndSettle();
      expect(api.mutations, 0);
      await tester.tap(find.text('Confirm'));
      await tester.pumpAndSettle();
      expect(api.mutations, 1);
      OutlinedButton probe() =>
          tester.widget(find.widgetWithText(OutlinedButton, 'Probe ISP'));
      expect(probe().onPressed, isNull);
      await tester.pump(const Duration(seconds: 4));
      await tester.pumpAndSettle();
      expect(api.mutations, 1);
      expect(probe().onPressed, isNull);
      final refresh = find.byTooltip('Refresh and inspect current state');
      await tester.ensureVisible(refresh);
      await tester.tap(refresh);
      await tester.pumpAndSettle();
      expect(probe().onPressed, isNotNull);
      expect(api.mutations, 1);
      await tester.pumpWidget(const SizedBox());
      await api.close();
    },
  );
}
