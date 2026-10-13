import 'dart:async';
import 'dart:convert';

import 'package:flutter/material.dart';
import 'package:flutter_test/flutter_test.dart';
import 'package:http/http.dart' as http;
import 'package:http/testing.dart';
import 'package:shared_preferences/shared_preferences.dart';
import 'package:pccontroller_mobile/main.dart';
import 'package:pccontroller_mobile/unified/output_organization.dart';
import 'package:pccontroller_mobile/unified/transport.dart';

Json channel(String key, String name, int order, {bool hidden = false}) => {
  'key': key,
  'name': name,
  'kind': 'relay',
  'control': 'relay',
  'order': order,
  'hidden': hidden,
  'actions': [
    {'id': '$key.on', 'verb': 'on', 'name': 'ON'},
    {'id': '$key.off', 'verb': 'off', 'name': 'OFF'},
  ],
};
Json catalog(List<Json> controls, [String revision = 'r1']) => {
  'board_profile': {
    'key': 'fixture',
    'board_identity': 'serial:fixture',
    'attached': true,
    'configured': true,
    'revision': revision,
  },
  'controls': controls,
  'peripherals': controls,
  'folders': [],
};
Future<dynamic> screen(WidgetTester tester, MockClient client) async {
  SharedPreferences.setMockInitialValues({});
  await tester.pumpWidget(const UnifiedControllerApp(restore: false));
  final dynamic home = tester.state(find.byType(ControllerHome));
  final PeerSession board = home.board;
  board.transport = PeerTransport('http://fixture', client: client);
  board.online = true;
  board.received = DateTime.now();
  board.state = {
    'connected': true,
    'have_status': true,
    'status_updated': DateTime.now().toUtc().toIso8601String(),
    'status': {'active_relays': 0},
  };
  board.peripherals = catalog([
    channel('relay.5', 'First', 1),
    channel('relay.6', 'Second', 2),
    channel('relay.7', 'Hidden', 0, hidden: true),
  ]);
  home.selectActivity(1);
  await tester.pumpAndSettle();
  return home;
}

void main() {
  testWidgets(
    'dragging near the edge scrolls long output lists and stops on release',
    (tester) async {
      await tester.pumpWidget(
        MaterialApp(
          home: Scaffold(
            body: SizedBox(
              height: 300,
              child: SingleChildScrollView(
                child: Column(
                  children: [
                    for (var i = 0; i < 20; i++)
                      SizedBox(
                        height: 100,
                        child: OutputDragHandle(
                          drag: OutputDrag('owner', '$i', 'relay', ''),
                          name: 'Output $i',
                          enabled: true,
                          icon: Icons.power,
                        ),
                      ),
                  ],
                ),
              ),
            ),
          ),
        ),
      );
      final scrollView = find.byType(SingleChildScrollView);
      final scrollable = tester.state<ScrollableState>(
        find.byType(Scrollable).first,
      );
      final edge = tester.getRect(scrollView).bottomCenter - const Offset(0, 2);
      final gesture = await tester.startGesture(
        tester.getCenter(find.byTooltip('Reorder Output 0')),
      );
      await tester.pump(const Duration(milliseconds: 250));
      await gesture.moveTo(edge);
      await tester.pump();
      await tester.pump(const Duration(milliseconds: 250));
      expect(scrollable.position.pixels, greaterThan(0));
      await gesture.up();
      await tester.pumpAndSettle();
      final settled = scrollable.position.pixels;
      await tester.pump(const Duration(milliseconds: 500));
      expect(scrollable.position.pixels, settled);
      expect(tester.takeException(), null);
      await tester.pumpWidget(const SizedBox());
    },
  );
  test('reorder includes hidden ranks and does not conflate output kinds or wiring', () {
    final controls = [
      channel('a', 'A', 1),
      channel('b', 'B', 2),
      channel('h', 'Hidden', 0, hidden: true),
    ];
    expect(
      outputMove(
        controls,
        const OutputDrag('owner', 'b', 'relay', ''),
        controls.first,
      ),
      {'order': 1},
    );
    expect(
      () => outputMove(
        controls,
        const OutputDrag('owner', 'b', 'pwm', ''),
        controls.first,
      ),
      throwsA(isA<Rejected>()),
    );
    final raw = {...controls[1], 'control': 'seat-internal'};
    expect(
      () => outputMove(
        [controls.first, raw],
        const OutputDrag('owner', 'b', 'relay', ''),
        {...controls.first, 'group': 'Cinema'},
      ),
      throwsA(isA<Rejected>()),
    );
    expect(
      outputMove(controls, const OutputDrag('owner', 'b', 'relay', ''), {
        ...controls.first,
        'group': 'Cinema',
      }),
      {'order': 1, 'group': 'Cinema'},
    );
  });

  testWidgets(
    'inline rename persists one name-only RPC after acknowledgement without changing header geometry',
    (tester) async {
      tester.view.physicalSize = const Size(412, 915);
      tester.view.devicePixelRatio = 1;
      addTearDown(tester.view.resetPhysicalSize);
      addTearDown(tester.view.resetDevicePixelRatio);
      final reply = Completer<http.Response>();
      final requests = <Json>[];
      final dynamic home = await screen(
        tester,
        MockClient((request) {
          requests.add(object(jsonDecode(request.body)));
          return reply.future;
        }),
      );
      final PeerSession board = home.board;
      final card = find.byWidgetPredicate(
        (w) => w is OutputDropCard && w.control['key'] == 'relay.5',
      );
      final bounds = tester.getRect(card);
      await tester.tap(find.byTooltip('Rename First'));
      await tester.pumpAndSettle();
      expect(tester.getRect(card), bounds);
      await tester.enterText(
        find.byType(TextField).first,
        'The renamed output',
      );
      await tester.testTextInput.receiveAction(TextInputAction.done);
      await tester.pump();
      await tester.runAsync(() async {
        await Future<void>.delayed(Duration.zero);
      });
      expect(requests, hasLength(1));
      expect(
        requests.single['method'],
        'controller.peripheral.presentation.update',
      );
      expect(requests.single['params'], {
        'key': 'relay.5',
        'expected_revision': 'r1',
        'name': 'The renamed output',
      });
      expect(objects(board.peripherals['controls']).first['name'], 'First');
      final next = objects(board.peripherals['controls'])
          .map(
            (c) => c['key'] == 'relay.5'
                ? {...c, 'name': 'The renamed output'}
                : c,
          )
          .toList();
      await tester.runAsync(() async {
        reply.complete(
          http.Response(
            jsonEncode({
              'jsonrpc': '2.0',
              'id': requests.single['id'],
              'result': {
                'board_profile': catalog(next, 'r2')['board_profile'],
                'peripheral': next.first,
                'controls': next,
                'folders': [],
              },
            }),
            200,
          ),
        );
        await Future<void>.delayed(Duration.zero);
      });
      await tester.pumpAndSettle();
      expect(find.byTooltip('Rename The renamed output'), findsOneWidget);
      expect(tester.getRect(card), bounds);
      expect(tester.takeException(), null);
      await tester.pumpWidget(const SizedBox());
    },
  );

  testWidgets(
    'actual handle drag sends one revision-checked move and renders authoritative order',
    (tester) async {
      final requests = <Json>[];
      late dynamic home;
      home = await screen(
        tester,
        MockClient((request) async {
          final rpc = object(jsonDecode(request.body));
          requests.add(rpc);
          final old = objects(
            (home.board as PeerSession).peripherals['controls'],
          );
          final next = old
              .map(
                (c) => {
                  ...c,
                  'order': c['key'] == 'relay.6'
                      ? 1
                      : c['key'] == 'relay.5'
                      ? 2
                      : 0,
                },
              )
              .toList();
          return http.Response(
            jsonEncode({
              'jsonrpc': '2.0',
              'id': rpc['id'],
              'result': {
                'board_profile': catalog(next, 'r2')['board_profile'],
                'peripheral': next[1],
                'controls': next,
                'folders': [],
              },
            }),
            200,
          );
        }),
      );
      final handle = find.byTooltip('Reorder Second');
      final first = find.byWidgetPredicate(
        (w) => w is OutputDropCard && w.control['key'] == 'relay.5',
      );
      final gesture = await tester.startGesture(tester.getCenter(handle));
      await tester.pump(const Duration(milliseconds: 250));
      await gesture.moveTo(tester.getCenter(first));
      await tester.pump(const Duration(milliseconds: 50));
      await gesture.up();
      await tester.runAsync(() async {
        await Future<void>.delayed(Duration.zero);
      });
      await tester.pumpAndSettle();
      expect(requests, hasLength(1));
      expect(requests.single['params'], {
        'key': 'relay.6',
        'expected_revision': 'r1',
        'order': 1,
      });
      final visible = tester
          .widgetList<OutputDropCard>(find.byType(OutputDropCard))
          .map((w) => w.control['key'])
          .toList();
      expect(visible, ['relay.6', 'relay.5']);
      expect(tester.takeException(), null);
      await tester.pumpWidget(const SizedBox());
    },
  );

  testWidgets(
    'cancelled or unchanged inline names send nothing; old owner callbacks are rejected',
    (tester) async {
      var calls = 0;
      final dynamic home = await screen(
        tester,
        MockClient((request) async {
          calls++;
          return http.Response('{}', 200);
        }),
      );
      await tester.tap(find.byTooltip('Rename First'));
      await tester.pumpAndSettle();
      await tester.enterText(find.byType(TextField).first, 'Draft only');
      await tester.tap(find.byTooltip('Manage First'));
      await tester.pumpAndSettle();
      await tester.tap(find.text('Cancel rename'));
      await tester.pumpAndSettle();
      expect(find.byType(TextField), findsNothing);
      expect(calls, 0);
      await tester.tap(find.byTooltip('Rename First'));
      await tester.pumpAndSettle();
      await tester.testTextInput.receiveAction(TextInputAction.done);
      await tester.pumpAndSettle();
      expect(calls, 0);
      final callback = home.outputRename(
        objects((home.board as PeerSession).peripherals['controls']).first,
      ) as Future<void> Function(String);
      (home.board as PeerSession).generation++;
      await expectLater(callback('Wrong owner'), throwsA(isA<Rejected>()));
      expect(calls, 0);
      expect(tester.takeException(), null);
      await tester.pumpWidget(const SizedBox());
    },
  );

  testWidgets(
    'accepted presentation fences an older in-flight catalog response',
    (tester) async {
      await tester.runAsync(() async {
        final oldResponse = Completer<http.Response>(),
            started = Completer<void>();
        final old = catalog([channel('a', 'Old', 0)]),
            next = catalog([channel('a', 'New', 0)], 'r2');
        final session = PeerSession(controller: true);
        session.transport = PeerTransport(
          'http://fixture',
          client: MockClient((request) async {
            if (request.method == 'GET') {
              return http.Response(
                '{"connected":false,"host_instance_id":"fixture"}',
                200,
              );
            }
            final rpc = object(jsonDecode(request.body));
            if (rpc['method'] == 'controller.peripherals.get') {
              started.complete();
              return oldResponse.future;
            }
            return http.Response(
              jsonEncode({'jsonrpc': '2.0', 'id': rpc['id'], 'result': []}),
              200,
            );
          }),
        );
        final refresh = session.refresh();
        await started.future;
        session.acceptPresentationCatalog(next);
        oldResponse.complete(
          http.Response(
            jsonEncode({'jsonrpc': '2.0', 'id': 1, 'result': old}),
            200,
          ),
        );
        await refresh;
        expect(session.peripherals['board_profile']['revision'], 'r2');
        expect(session.peripherals['controls'][0]['name'], 'New');
        session.dispose();
      });
    },
  );
}
