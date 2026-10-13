import 'dart:async';
import 'dart:convert';

import 'package:flutter/material.dart';
import 'package:flutter_test/flutter_test.dart';
import 'package:http/http.dart' as http;
import 'package:http/testing.dart';
import 'package:shared_preferences/shared_preferences.dart';
import 'package:pccontroller_mobile/main.dart';
import 'package:pccontroller_mobile/unified/live_widgets.dart';
import 'package:pccontroller_mobile/unified/transport.dart';

void main() {
  for (final size in [const Size(412, 700), const Size(1280, 480)]) {
    testWidgets(
      'front-panel pending request retains viewport and scroll at $size',
      (tester) async {
        SharedPreferences.setMockInitialValues({});
        tester.view.physicalSize = size;
        tester.view.devicePixelRatio = 1;
        addTearDown(tester.view.resetPhysicalSize);
        addTearDown(tester.view.resetDevicePixelRatio);
        final receipt = Completer<http.Response>();
        final requests = <Json>[];
        await tester.pumpWidget(const UnifiedControllerApp(restore: false));
        final dynamic home = tester.state(find.byType(ControllerHome));
        final PeerSession board = home.board;
        board.transport = PeerTransport(
          'http://fixture',
          client: MockClient((request) {
            requests.add(object(jsonDecode(request.body)));
            return receipt.future;
          }),
        );
        board.online = true;
        board.received = DateTime.now();
        board.state = {
          'connected': true,
          'have_front_panel': true,
          'front_panel': {
            'host_captured': false,
            'lcd_available': false,
            'pressed_keys': 0,
            'menu_page': 1,
          },
        };
        home.selectActivity(2);
        await tester.pumpAndSettle();
        final scrollView = find.byType(SingleChildScrollView).last;
        final scrollable = tester.state<ScrollableState>(
          find
              .descendant(of: scrollView, matching: find.byType(Scrollable))
              .first,
        );
        final position = scrollable.position;
        expect(position.maxScrollExtent, greaterThan(40));
        position.jumpTo(40);
        await tester.pump();
        final button = find
            .descendant(
              of: find.byType(FrontPanelButtons),
              matching: find.byType(OutlinedButton),
            )
            .first;
        final viewport = tester.getRect(scrollView);
        final target = tester.getRect(button);
        final extent = position.maxScrollExtent;
        expect(find.byType(LinearProgressIndicator), findsNothing);

        await tester.tap(button);
        await tester.pump();
        await tester.runAsync(() async {
          await Future<void>.delayed(Duration.zero);
        });
        await tester.pump(const Duration(milliseconds: 100));
        expect(home.busy, true);
        expect(requests, hasLength(1));
        expect(requests.single['method'], 'controller.command.execute');
        expect(object(requests.single['params'])['command'], 'menu prev');
        expect(find.byType(LinearProgressIndicator), findsOneWidget);
        expect(identical(scrollable.position, position), true);
        expect(position.pixels, 40);
        expect(position.maxScrollExtent, extent);
        expect(tester.getRect(scrollView), viewport);
        expect(tester.getRect(button), target);

        await tester.runAsync(() async {
          receipt.complete(
            http.Response(
              jsonEncode({
                'jsonrpc': '2.0',
                'id': requests.single['id'],
                'result': {'accepted': true},
              }),
              200,
            ),
          );
          await Future<void>.delayed(Duration.zero);
        });
        await tester.pumpAndSettle();
        expect(home.busy, false);
        expect(find.byType(LinearProgressIndicator), findsNothing);
        expect(position.pixels, 40);
        expect(position.maxScrollExtent, extent);
        expect(tester.getRect(scrollView), viewport);
        expect(tester.getRect(button), target);
        expect(requests, hasLength(1));
        expect(tester.takeException(), null);
        await tester.pumpWidget(const SizedBox());
      },
    );
  }
}
