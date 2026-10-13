import 'dart:async';
import 'dart:convert';

import 'package:flutter/material.dart';
import 'package:flutter_test/flutter_test.dart';
import 'package:http/http.dart' as http;
import 'package:http/testing.dart';
import 'package:shared_preferences/shared_preferences.dart';
import 'package:pccontroller_mobile/main.dart';
import 'package:pccontroller_mobile/discovery.dart';
import 'package:pccontroller_mobile/unified/preferences.dart';
import 'package:pccontroller_mobile/unified/transport.dart';

class RecordingDiscovery extends DeviceDiscovery {
  final result = Completer<List<DiscoveredDevice>>();
  int scans = 0;
  @override
  Future<List<DiscoveredDevice>> scan() {
    ++scans;
    return result.future;
  }
}

Future<void> pull(WidgetTester tester, Rect viewport) async {
  final gesture = await tester.startGesture(
    viewport.topCenter + const Offset(0, 20),
  );
  await gesture.moveBy(const Offset(0, 50));
  await tester.pump();
  await gesture.moveBy(const Offset(0, 350));
  await tester.pump();
  await gesture.up();
  await tester.pump();
  await tester.pump(const Duration(milliseconds: 200));
}

void main() {
  testWidgets(
    'offline pull refreshes nearby peers without connecting or losing saved intent',
    (tester) async {
      final discovery = RecordingDiscovery();
      final preferences = AppPreferences()..autoConnect = true;
      await tester.pumpWidget(
        MaterialApp(
          home: ControllerHome(
            restore: false,
            preferences: preferences,
            discovery: discovery,
          ),
        ),
      );
      final dynamic home = tester.state(find.byType(ControllerHome));
      home.boardDiscoveryBlocked = false;
      home.playerDiscoveryBlocked = true;
      home.selectActivity(4);
      await tester.pumpAndSettle();
      final viewport = tester.getRect(find.byType(SingleChildScrollView).last);
      await pull(tester, viewport);
      expect(discovery.scans, 1);
      expect(home.discovering, true);
      final Future<void> joining = home.refreshActivity();
      expect(discovery.scans, 1);
      discovery.result.complete([
        const DiscoveredDevice(
          'Nearby board',
          'http://nearby',
          'fixture',
          product: 'pccontroller',
          verified: true,
        ),
      ]);
      await joining;
      await tester.pumpAndSettle();
      expect(home.discovered, hasLength(1));
      expect(find.text('Nearby board'), findsWidgets);
      expect(home.board.transport, null);
      expect(home.player.transport, null);
      expect(home.boardWanted, false);
      expect(home.boardDiscoveryBlocked, false);
      expect(home.playerDiscoveryBlocked, true);
      expect(home.discovering, false);
      expect(tester.takeException(), null);
      await tester.pumpWidget(const SizedBox());
      preferences.dispose();
    },
  );

  for (final size in [const Size(412, 1000), const Size(1280, 1000)]) {
    testWidgets(
      'pull refresh works on short content without layout movement at $size',
      (tester) async {
        SharedPreferences.setMockInitialValues({});
        tester.view.physicalSize = size;
        tester.view.devicePixelRatio = 1;
        addTearDown(tester.view.resetPhysicalSize);
        addTearDown(tester.view.resetDevicePixelRatio);
        final response = Completer<http.Response>();
        final requests = <http.Request>[];
        await tester.pumpWidget(const UnifiedControllerApp(restore: false));
        final dynamic home = tester.state(find.byType(ControllerHome));
        final PeerSession player = home.player;
        player.transport = PeerTransport(
          'http://fixture',
          client: MockClient((request) {
            requests.add(request);
            return response.future;
          }),
        );
        home.selectActivity(
          1,
        ); // Short Outputs view; no local hardware available.
        await tester.pumpAndSettle();
        final view = find.byType(SingleChildScrollView).last;
        final viewport = tester.getRect(view);
        final scrollable = tester.state<ScrollableState>(
          find.descendant(of: view, matching: find.byType(Scrollable)).first,
        );
        final position = scrollable.position;
        expect(position.maxScrollExtent, 0);
        await pull(tester, viewport);
        await tester.runAsync(() async => Future<void>.delayed(Duration.zero));
        await tester.pump();
        expect(requests, hasLength(1));
        expect(requests.single.method, 'GET');
        expect(requests.single.url.path, '/api/player/status');
        expect(home.busy, false);
        expect(tester.getRect(view), viewport);
        expect(position.pixels, 0);
        expect(find.byType(RefreshProgressIndicator), findsOneWidget);
        // A second read joins the same request; no premature completion.
        var joined = false;
        final joining = player.refresh().then((_) => joined = true);
        await tester.pump();
        expect(joined, false);
        expect(requests, hasLength(1));
        await tester.runAsync(() async {
          response.complete(
            http.Response('{"playing":false,"volume":43}', 200),
          );
          await joining;
        });
        await tester.pumpAndSettle();
        expect(find.byType(RefreshProgressIndicator), findsNothing);
        expect(player.state['volume'], 43);
        expect(tester.getRect(view), viewport);
        expect(position.pixels, 0);
        expect(requests, hasLength(1));
        expect(tester.takeException(), null);
        await tester.pumpWidget(const SizedBox());
      },
    );
  }

  testWidgets(
    'pull refresh failure finishes and preserves authoring text and viewport',
    (tester) async {
      SharedPreferences.setMockInitialValues({});
      tester.view.physicalSize = const Size(412, 900);
      tester.view.devicePixelRatio = 1;
      addTearDown(tester.view.resetPhysicalSize);
      addTearDown(tester.view.resetDevicePixelRatio);
      await tester.pumpWidget(const UnifiedControllerApp(restore: false));
      final dynamic home = tester.state(find.byType(ControllerHome));
      final PeerSession player = home.player;
      final response = Completer<http.Response>();
      var calls = 0;
      player.transport = PeerTransport(
        'http://fixture',
        client: MockClient((_) {
          calls++;
          return response.future;
        }),
      );
      home.displayText.text = 'Unsaved display draft';
      home.selectActivity(1);
      await tester.pumpAndSettle();
      final view = find.byType(SingleChildScrollView).last;
      final viewport = tester.getRect(view);
      await pull(tester, viewport);
      await tester.runAsync(() async => Future<void>.delayed(Duration.zero));
      expect(calls, 1);
      final joining = player.refresh();
      await tester.runAsync(() async {
        response.complete(http.Response('Unavailable', 503));
        await joining;
      });
      await tester.pumpAndSettle();
      expect(calls, 1);
      expect(player.error, contains('HTTP 503'));
      expect(home.displayText.text, 'Unsaved display draft');
      expect(home.lastError, null);
      expect(find.byType(MaterialBanner), findsNothing);
      expect(find.byType(RefreshProgressIndicator), findsNothing);
      expect(find.byType(SnackBar), findsOneWidget);
      expect(tester.getRect(view), viewport);
      expect(tester.takeException(), null);
      await tester.pumpWidget(const SizedBox());
    },
  );

  test(
    'refresh joins current owner but a disconnect retires its in-flight read',
    () async {
      final oldResponse = Completer<http.Response>();
      final oldStarted = Completer<void>();
      final session = PeerSession(controller: false);
      session.transport = PeerTransport(
        'http://old',
        client: MockClient((_) {
          oldStarted.complete();
          return oldResponse.future;
        }),
      );
      final old = session.refresh(), joined = session.refresh();
      expect(identical(old, joined), true);
      await oldStarted.future;
      session.disconnect();
      var newReads = 0;
      session.transport = PeerTransport(
        'http://new',
        client: MockClient((_) async {
          newReads++;
          return http.Response('{"playing":true,"volume":72}', 200);
        }),
      );
      await session.refresh();
      oldResponse.complete(http.Response('{"playing":false,"volume":1}', 200));
      await old;
      expect(newReads, 1);
      expect(session.state['volume'], 72);
      expect(session.online, true);
      expect(session.error, null);
      session.dispose();
    },
  );

  testWidgets('Firmware pull reads only snapshot, catalogs and update status', (
    tester,
  ) async {
    SharedPreferences.setMockInitialValues({});
    await tester.pumpWidget(const UnifiedControllerApp(restore: false));
    final dynamic home = tester.state(find.byType(ControllerHome));
    final PeerSession board = home.board;
    final paths = <String>[], methods = <String>[];
    board.transport = PeerTransport(
      'http://fixture',
      client: MockClient((request) async {
        paths.add(request.url.path);
        if (request.method == 'GET') {
          return http.Response(
            '{"host_instance_id":"fixture","connected":false}',
            200,
          );
        }
        final rpc = object(jsonDecode(request.body));
        methods.add(rpc['method'] as String);
        final Object result = switch (rpc['method']) {
          'controller.peripherals.get' => {
            'controls': [],
            'peripherals': [],
            'board_profile': {},
          },
          'controller.artifact.list' => {'artifacts': []},
          'controller.update.status' => {'state': 'idle'},
          'controller.app.instances' => [],
          _ => throw StateError('Unexpected mutation or RPC'),
        };
        return http.Response(
          jsonEncode({'jsonrpc': '2.0', 'id': rpc['id'], 'result': result}),
          200,
        );
      }),
    );
    home.selectActivity(5);
    await tester.runAsync(() async => await home.refreshActivity());
    await tester.pumpAndSettle();
    expect(paths.where((p) => p == '/api/snapshot'), hasLength(1));
    expect(
      methods,
      unorderedEquals([
        'controller.peripherals.get',
        'controller.app.instances',
        'controller.artifact.list',
        'controller.update.status',
      ]),
    );
    expect(home.busy, false);
    expect(board.operation['state'], 'idle');
    expect(tester.takeException(), null);
    await tester.pumpWidget(const SizedBox());
  });
}
