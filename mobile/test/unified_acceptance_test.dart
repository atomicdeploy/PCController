import 'dart:convert';

import 'package:flutter/material.dart';
import 'package:http/http.dart' as http;
import 'package:http/testing.dart';
import 'package:flutter_test/flutter_test.dart';
import 'package:pccontroller_mobile/main.dart';
import 'package:pccontroller_mobile/unified/transport.dart';
import 'package:pccontroller_mobile/unified/live_widgets.dart';
import 'package:pccontroller_mobile/unified/automation.dart';

void main() {
  testWidgets('native cue editor refuses a changed cue owner before any RPC', (
    tester,
  ) async {
    await tester.pumpWidget(const UnifiedControllerApp(restore: false));
    final dynamic home = tester.state(find.byType(ControllerHome));
    final PeerSession peer = home.player;
    var calls = 0;
    peer.transport = PeerTransport(
      'http://fixture',
      client: MockClient((request) async {
        calls++;
        return http.Response('{}', 200);
      }),
    );
    final cue = <String, dynamic>{
      'id': '00000000-0000-4000-8000-000000000001',
      'name': 'Fixture cue',
      'start_time_ms': 1000,
      'duration_ms': 500,
      'resizable': true,
    };
    peer.online = true;
    peer.received = DateTime.now();
    peer.state = {
      'cues': [cue],
    };
    final open = home.editPlayerCue(cue) as Future<void>;
    await tester.pumpAndSettle();
    await tester.enterText(find.byType(TextFormField).first, '2');
    peer.state = {
      'cues': [
        {...cue, 'start_time_ms': 3000},
      ],
    };
    await tester.tap(find.text('Save'));
    await tester.pumpAndSettle();
    expect(
      find.text(
        'Cue or player changed. Reopen the cue to edit its current values.',
      ),
      findsOneWidget,
    );
    expect(calls, 0);
    await tester.tap(find.byTooltip('Close'));
    await tester.pumpAndSettle();
    await tester.tap(find.text('Discard'));
    await tester.pumpAndSettle();
    await open;
    await tester.pumpWidget(const SizedBox());
  });
  testWidgets(
    'live slider keeps drag preview and sends one request only on release',
    (tester) async {
      final values = <double>[];
      Widget screen(double value) => MaterialApp(
        home: Scaffold(
          body: Center(
            child: SizedBox(
              width: 320,
              child: RemoteSlider(
                key: const ValueKey('same-owner'),
                value: value,
                maximum: 100,
                changed: values.add,
              ),
            ),
          ),
        ),
      );
      await tester.pumpWidget(screen(10));
      final gesture = await tester.startGesture(
        tester.getCenter(find.byType(Slider)),
      );
      await gesture.moveBy(const Offset(50, 0));
      await tester.pump();
      final dragging = tester.widget<Slider>(find.byType(Slider)).value;
      expect(values, isEmpty);
      await tester.pumpWidget(screen(20));
      expect(tester.widget<Slider>(find.byType(Slider)).value, dragging);
      await gesture.up();
      await tester.pump();
      expect(values, hasLength(1));
      expect(values.single, dragging);
      await tester.pumpWidget(const SizedBox());
    },
  );
  for (final size in [const Size(360, 800), const Size(1280, 800)]) {
    testWidgets(
      'Pealayer advertises actual workspaces and media options at $size',
      (tester) async {
        tester.view.physicalSize = size;
        tester.view.devicePixelRatio = 1;
        addTearDown(tester.view.resetPhysicalSize);
        addTearDown(tester.view.resetDevicePixelRatio);
        await tester.pumpWidget(const UnifiedControllerApp(restore: false));
        final dynamic home = tester.state(find.byType(ControllerHome));
        final PeerSession peer = home.player;
        peer.online = true;
        peer.received = DateTime.now();
        peer.state = {
          'playing': false,
          'volume': 120.0,
          'muted': true,
          'playback_rate': 1.0,
          'current_video': 'fixture.mp4',
          'seekable': true,
          'duration': 120.0,
          'playback_time': 10.0,
          'media_session_capabilities': {
            'can_play': true,
            'can_pause': false,
            'can_stop': true,
            'can_next': false,
            'can_previous': false,
          },
          'active_workspace_profile': 'custom',
          'workspace_profiles': [
            {'id': 'custom', 'name': 'My workspace'},
          ],
          'chapters': [
            {'index': 0, 'title': 'Opening', 'time_seconds': 0.0},
          ],
          'current_chapter_index': 0,
          'media_tracks': [
            {
              'id': 1,
              'kind': 'audio',
              'title': 'Persian audio',
              'selected': true,
            },
            {
              'id': 2,
              'kind': 'sub',
              'title': 'English subtitles',
              'selected': false,
            },
          ],
        };
        home.section = 0;
        peer.notifyListeners();
        await tester.pumpAndSettle();
        expect(find.byTooltip('Play'), findsOneWidget);
        expect(find.byTooltip('Pause'), findsNothing);
        expect(find.text('Unmute'), findsOneWidget);
        expect(find.text('My workspace'), findsOneWidget);
        expect(find.text('Audio track'), findsOneWidget);
        expect(find.text('Subtitles'), findsOneWidget);
        expect(tester.takeException(), null);
        await tester.pumpWidget(const SizedBox());
      },
    );
  }
  testWidgets(
    'local panel API validates current mode, bounds and E-STOP before dispatch',
    (tester) async {
      await tester.pumpWidget(const UnifiedControllerApp(restore: false));
      final dynamic home = tester.state(find.byType(ControllerHome));
      final PeerSession peer = home.board;
      final requests = <Json>[];
      peer.transport = PeerTransport(
        'http://fixture',
        client: MockClient((request) async {
          final body = object(jsonDecode(request.body));
          requests.add(body);
          return http.Response(
            jsonEncode({
              'jsonrpc': '2.0',
              'id': body['id'],
              'result': {'accepted': true},
            }),
            200,
          );
        }),
      );
      peer.online = true;
      peer.received = DateTime.now();
      peer.state = {
        'host_instance_id': 'fixture',
        'connection_generation': 7,
        'connected': true,
        'have_front_panel': true,
        'front_panel': {'host_captured': false, 'lcd_available': false},
      };
      home.automationActuation = true;
      final LocalAutomation api = home.automation;
      final target = {'expected_target': peer.targetStamp};
      for (final call in [
        ('board.menu.press', {'button': 'K1'}),
        ('board.display.show', {'display': 'lcd', 'text': 'test'}),
        ('board.buzzer.play', {'frequency_hz': 880, 'duration_ms': 1001}),
        ('board.mode.set', {'mode': 'invalid'}),
      ]) {
        await expectLater(
          api.dispatch(call.$1, {...target, ...call.$2}),
          throwsA(isA<AutomationFailure>()),
        );
      }
      expect(requests, isEmpty);
      await tester.runAsync(
        () => api.dispatch('board.buzzer.play', {
          ...target,
          'frequency_hz': 880,
          'duration_ms': 115,
        }),
      );
      expect(requests.single['params'], {'command': 'buzzer 880 115'});
      peer.state['emergency_stop'] = {'active': true};
      await expectLater(
        api.dispatch('board.menu.press', {...target, 'button': 'prev'}),
        throwsA(isA<AutomationFailure>()),
      );
      expect(requests, hasLength(1));
      await tester.runAsync(() => api.dispatch('board.buzzer.stop', target));
      expect(requests.last['params'], {'command': 'melody stop'});
      expect(requests, hasLength(2));
      await tester.pumpWidget(const SizedBox());
    },
  );
  for (final size in [const Size(360, 800), const Size(1280, 800)]) {
    testWidgets(
      'channel indicator shares title row and management menu at $size',
      (tester) async {
        tester.view.physicalSize = size;
        tester.view.devicePixelRatio = 1;
        addTearDown(tester.view.resetPhysicalSize);
        addTearDown(tester.view.resetDevicePixelRatio);
        await tester.pumpWidget(const UnifiedControllerApp(restore: false));
        final dynamic home = tester.state(find.byType(ControllerHome));
        final PeerSession peer = home.board;
        peer.online = true;
        peer.received = DateTime.now();
        peer.state = {
          'host_instance_id': 'fixture',
          'connection_generation': 7,
          'connected': true,
          'have_status': true,
          'status_updated': DateTime.now().toIso8601String(),
          'status': {'active_relays': 16},
        };
        peer.peripherals = {
          'board_profile': {
            'attached': true,
            'configured': true,
            'revision': 'fixture',
          },
          'controls': [
            {
              'key': 'relay.5',
              'kind': 'relay',
              'control': 'relay',
              'name': 'Lobby lighting',
              'actions': [],
            },
          ],
        };
        home.section = 1;
        peer.notifyListeners();
        await tester.pumpAndSettle();
        final title = find.text('Lobby lighting'), indicator = find.text('On');
        expect(indicator, findsOneWidget);
        expect(
          (tester.getCenter(title).dy - tester.getCenter(indicator).dy).abs(),
          lessThan(3),
        );
        expect(
          tester.getCenter(indicator).dx,
          greaterThan(tester.getCenter(title).dx),
        );
        await tester.tap(find.byTooltip('Manage Lobby lighting'));
        await tester.pumpAndSettle();
        expect(find.text('Edit channel'), findsOneWidget);
        expect(find.text('Lock channel'), findsOneWidget);
        expect(tester.takeException(), null);
        await tester.pumpWidget(const SizedBox());
      },
    );
  }
  for (final captured in [false, true]) {
    testWidgets(
      'one front-panel row dispatches only current mode, captured=$captured',
      (tester) async {
        final commands = <String>[];
        await tester.pumpWidget(
          MaterialApp(
            home: Scaffold(
              body: SizedBox(
                width: 288,
                child: FrontPanelButtons(
                  captured: captured,
                  enabled: true,
                  dispatch: commands.add,
                ),
              ),
            ),
          ),
        );
        expect(find.byType(OutlinedButton), findsNWidgets(4));
        final positions = [
          for (final e in find.byType(OutlinedButton).evaluate())
            tester.getCenter(find.byWidget(e.widget)).dy,
        ];
        expect(positions.toSet().length, 1);
        await tester.tap(find.byType(OutlinedButton).first);
        expect(commands, [captured ? 'host-menu key K1 press' : 'menu prev']);
        expect(tester.takeException(), null);
      },
    );
  }
  testWidgets('settings are reachable without a connected host', (
    tester,
  ) async {
    await tester.pumpWidget(const UnifiedControllerApp(restore: false));
    await tester.tap(find.byTooltip('Settings'));
    await tester.pumpAndSettle();
    expect(find.text('Appearance'), findsOneWidget);
    expect(find.text('Launcher icons'), findsOneWidget);
    expect(find.text('Mirror board beeps on this device'), findsOneWidget);
    expect(tester.takeException(), null);
    await tester.pumpWidget(const SizedBox());
  });
}
