import 'dart:async';

import 'package:flutter/material.dart';
import 'package:flutter_secure_storage/flutter_secure_storage.dart';
import 'package:flutter_test/flutter_test.dart';
import 'package:shared_preferences/shared_preferences.dart';
import 'package:pccontroller_mobile/api.dart';
import 'package:pccontroller_mobile/pakar_reference.dart';
import 'package:pccontroller_mobile/models.dart';

import 'peer_fixture.dart';
import 'settings_fixture.dart';

class TestApi extends PakarApi {
  TestApi({DeviceStatus? snapshot}) : snapshot = snapshot ?? peerFixture();
  DeviceStatus snapshot;
  final updates = StreamController<DeviceStatus>();
  @override
  Future<DeviceStatus> status() async => snapshot;
  @override
  Future<CellMap> mapping() async => CellMap.defaults();
  @override
  Future<Map<String, dynamic>> getJson(String path) async => switch (path) {
    '/api/network' => Map.of(simulatedNetwork),
    '/api/cloud' => Map.of(simulatedCloud),
    '/api/device/name' => {
      'hostname': snapshot.hostname,
      'display_name': snapshot.peer.name,
    },
    '/api/auth/info' => {
      'ready': true,
      'default_credentials': false,
      'username': 'fixture',
    },
    _ => {},
  };
  @override
  Stream<DeviceStatus> liveStatus() => updates.stream;
  @override
  Future<void> close() async {
    await updates.close();
    await super.close();
  }
}

void main() {
  setUp(() {
    SharedPreferences.setMockInitialValues({});
    FlutterSecureStorage.setMockInitialValues({});
  });
  testWidgets('Shell uses injected state and cancels transport on disposal', (
    tester,
  ) async {
    await tester.pumpWidget(PakarControllerApp(api: TestApi()));
    await tester.pump();
    await tester.pump();
    expect(find.text('Pakar Table'), findsWidgets);
    expect(find.text('Table'), findsWidgets);
    await tester.pumpWidget(const SizedBox());
    await tester.pump();
  });
  testWidgets('Offline hardware never invents zero inputs/outputs', (
    tester,
  ) async {
    await tester.pumpWidget(
      const MaterialApp(home: Scaffold(body: HardwarePage(status: null))),
    );
    expect(find.text('0x00'), findsNothing);
    expect(find.text('No acknowledged hardware measurements'), findsOneWidget);
  });
  testWidgets('Marquee defaults to once without forced scroll', (tester) async {
    Map<String, String>? sent;
    await tester.pumpWidget(
      MaterialApp(
        home: Scaffold(
          body: FrontPanelPage(
            status: peerFixture(features: ['text']),
            command: (path, [values = const {}]) async {
              sent = values;
            },
          ),
        ),
      ),
    );
    await tester.ensureVisible(find.text('Present'));
    await tester.tap(find.text('Present'));
    await tester.pump();
    expect(sent?['repeat'], 'once');
    expect(sent?['force_scroll'], 'false');
  });
  testWidgets('Offline table disables hardware mutations', (tester) async {
    var calls = 0;
    await tester.pumpWidget(
      MaterialApp(
        home: Scaffold(
          body: TablePage(
            status: null,
            mapping: CellMap.defaults(),
            command: (path, [values = const {}]) async {
              ++calls;
            },
          ),
        ),
      ),
    );
    expect(find.text('1'), findsNothing);
    await tester.pump();
    expect(calls, 0);
  });
  testWidgets(
    'Unacknowledged board has no Table, hardware, modes, display/text or buzzer cards',
    (tester) async {
      await tester.pumpWidget(
        PakarControllerApp(
          api: TestApi(
            snapshot: DeviceStatus({
              'hostname': 'ledboard',
              'pixels': 480,
              'tm1637_ack': true,
            }),
          ),
        ),
      );
      await tester.pump();
      await tester.pump();
      expect(find.text('Table'), findsNothing);
      expect(find.text('Hardware'), findsNothing);
      expect(find.text('Front panel'), findsNothing);
      expect(find.text('Snake'), findsNothing);
      expect(find.text('Connection'), findsWidgets);
      await tester.pumpWidget(const SizedBox());
      await tester.pump();
    },
  );
  testWidgets('Buzzer-only controller cannot paint display or text editor', (
    tester,
  ) async {
    await tester.pumpWidget(
      MaterialApp(
        home: Scaffold(
          body: FrontPanelPage(
            status: peerFixture(features: ['buzzer'], kind: 'controller'),
            command: (path, [values = const {}]) async {},
          ),
        ),
      ),
    );
    expect(find.byType(SevenSegmentPreview), findsNothing);
    expect(find.text('Marquee text'), findsNothing);
    expect(find.text('Present'), findsNothing);
    expect(find.text('welcome'), findsOneWidget);
    expect(find.text('Snake'), findsNothing);
  });
  testWidgets(
    'Phone and tablet shells use contextual navigation without overflow',
    (tester) async {
      for (final size in [const Size(380, 800), const Size(1100, 800)]) {
        tester.view.physicalSize = size;
        tester.view.devicePixelRatio = 1;
        final api = TestApi();
        await tester.pumpWidget(PakarControllerApp(api: api));
        await tester.pump();
        await tester.pump();
        expect(find.text('Table'), findsWidgets);
        expect(tester.takeException(), isNull);
        if (size.width > 840) {
          expect(find.byType(NavigationRail), findsOneWidget);
        } else {
          expect(find.byType(NavigationBar), findsOneWidget);
        }
        await tester.pumpWidget(const SizedBox());
        await tester.pump();
      }
      addTearDown(tester.view.resetPhysicalSize);
      addTearDown(tester.view.resetDevicePixelRatio);
    },
  );
  testWidgets(
    'One selection backs taps and Find; no physical writes on selection',
    (tester) async {
      tester.view.physicalSize = const Size(1100, 1400);
      tester.view.devicePixelRatio = 1;
      addTearDown(tester.view.resetPhysicalSize);
      addTearDown(tester.view.resetDevicePixelRatio);
      var writes = 0;
      await tester.pumpWidget(
        MaterialApp(
          home: Scaffold(
            body: TablePage(
              status: peerFixture(),
              mapping: CellMap.defaults(),
              command: (path, [values = const {}]) async {
                ++writes;
              },
            ),
          ),
        ),
      );
      final field = find.byType(TextField).first;
      await tester.enterText(field, '۱،٢،۳');
      await tester.pump();
      expect(
        tester
            .widget<CellButton>(find.byKey(const ValueKey('cell-2')))
            .highlighted,
        true,
      );
      // Bring a genuine cell gesture into view; it replaces the editable query.
      await tester.ensureVisible(find.byKey(const ValueKey('cell-4')));
      await tester.tap(find.byKey(const ValueKey('cell-4')));
      await tester.pump();
      expect(tester.widget<TextField>(field).controller!.text, '1–4');
      await tester.enterText(field, '4-');
      await tester.pump();
      expect(
        tester
            .widget<CellButton>(find.byKey(const ValueKey('cell-2')))
            .highlighted,
        true,
      );
      expect(
        tester
            .widget<CellButton>(find.byKey(const ValueKey('cell-2')))
            .showIndicator,
        false,
      );
      expect(writes, 0);
      expect(tester.takeException(), isNull);
    },
  );
  testWidgets(
    'Bulk output stops at first uncertain command; never continues/replays',
    (tester) async {
      final sent = <String>[];
      await tester.pumpWidget(
        MaterialApp(
          home: Scaffold(
            body: TablePage(
              status: peerFixture(),
              mapping: CellMap.defaults(),
              command: (path, [values = const {}]) async {},
              verifiedCommand: (path, values) async {
                sent.add(values['id']!);
                return sent.length < 2;
              },
            ),
          ),
        ),
      );
      await tester.enterText(find.byType(TextField).first, '1-3');
      await tester.pump();
      await tester.ensureVisible(find.text('Set red'));
      await tester.tap(find.text('Set red'));
      await tester.pump();
      expect(sent, ['1', '2']);
      expect(find.textContaining('Stopped after 1/3'), findsOneWidget);
    },
  );
  testWidgets(
    'Peer ACK loss removes table and independent cards and changes identity',
    (tester) async {
      final api = TestApi();
      await tester.pumpWidget(PakarControllerApp(api: api));
      await tester.pumpAndSettle();
      expect(find.text('Table'), findsWidgets);
      api.updates.add(
        peerFixture(
          features: ['buzzer'],
          kind: 'controller',
          hostname: 'rayanlamp',
          title: 'Rayan Controller',
          application: 'rayan-controller',
        ),
      );
      await tester.pumpAndSettle();
      expect(find.text('Table'), findsNothing);
      expect(find.text('Rayan Controller'), findsWidgets);
      expect(find.text('Snake'), findsNothing);
      await tester.pumpWidget(const SizedBox());
      await tester.pump();
    },
  );
}
