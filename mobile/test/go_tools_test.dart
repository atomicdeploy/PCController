import 'dart:convert';
import 'dart:io';
import 'dart:ui' as ui;

import 'package:flutter/material.dart';
import 'package:flutter/rendering.dart';
import 'package:flutter/services.dart';
import 'package:flutter_test/flutter_test.dart';
import 'package:http/http.dart' as http;
import 'package:http/testing.dart';
import 'package:pccontroller_mobile/unified/go_commands.dart';
import 'package:pccontroller_mobile/unified/go_tools.dart';
import 'package:pccontroller_mobile/unified/preferences.dart';
import 'package:pccontroller_mobile/unified/transport.dart';

import 'go_commands_test.dart' as fixture;

PeerSession toolsSession(List<Json> calls) =>
    fixture.session(
        MockClient((request) async {
          final rpc = object(jsonDecode(request.body));
          calls.add(rpc);
          final result = switch (rpc['method']) {
            'controller.command.catalog' => fixture.catalog(),
            'controller.melodies.list' => [
              {'name': 'Startup', 'notes': 4},
            ],
            _ => {'output': 'fixture receipt'},
          };
          return http.Response(
            jsonEncode({'jsonrpc': '2.0', 'id': rpc['id'], 'result': result}),
            200,
          );
        }),
      )
      ..state.addAll({
        'hello': {'capabilities': (1 << 4) | (1 << 22)},
        'have_status_led': true,
        'status_led': {
          'red': 113,
          'green': 102,
          'blue': 206,
          'brightness': 128,
        },
      })
      ..peripherals = {
        'strip_effects': [
          {'id': 'warm-pulse', 'name': 'Warm pulse'},
        ],
        'strip': {
          'maximum_pixels': 100,
          'maximum_fps': 30,
          'default_pixels': 100,
          'default_fps': 20,
          'modes': ['solid', 'pixel', 'frame', 'rainbow', 'effect'],
        },
      };

Widget app(Widget child, AppPreferences preferences) => MaterialApp(
  debugShowCheckedModeBanner: false,
  theme: ThemeData(
    useMaterial3: true,
    fontFamily: 'Vazirmatn',
    colorScheme: preferences.scheme(Brightness.light),
    inputDecorationTheme: const InputDecorationTheme(
      border: OutlineInputBorder(),
      isDense: true,
    ),
  ),
  home: child,
);

Future<void> settle(WidgetTester tester) async {
  await tester.runAsync(() async {
    await Future<void>.delayed(const Duration(milliseconds: 25));
  });
  await tester.pump(const Duration(milliseconds: 300));
}

void main() {
  testWidgets(
    'tools load only catalogs; manual mutation can be cancelled; RF uses timed contract and recording requires a name',
    (tester) async {
      tester.view.physicalSize = const Size(1100, 800);
      tester.view.devicePixelRatio = 1;
      addTearDown(() {
        tester.view.resetPhysicalSize();
        tester.view.resetDevicePixelRatio();
      });
      final calls = <Json>[];
      final peer = toolsSession(calls), preferences = AppPreferences();
      await tester.pumpWidget(
        app(GoToolsPage(session: peer, preferences: preferences), preferences),
      );
      await settle(tester);
      expect(calls.map((c) => c['method']), [
        'controller.command.catalog',
        'controller.melodies.list',
      ]);
      await tester.tap(find.text('Commands'));
      await settle(tester);
      await tester.enterText(find.byType(TextField).first, 'relay 5 on');
      await tester.pump();
      await tester.tap(find.text('Run'));
      await settle(tester);
      expect(find.text('Run controller command?'), findsOneWidget);
      await tester.tap(find.text('Cancel'));
      await settle(tester);
      expect(calls.length, 2);
      await tester.tap(find.text('Lighting'));
      await settle(tester);
      await tester.tap(find.text('Solid'));
      await settle(tester);
      await tester.tap(find.text('Effect').last);
      await settle(tester);
      await tester.tap(find.byKey(const ValueKey('Effect/')));
      await settle(tester);
      await tester.tap(find.text('Warm pulse').last);
      await settle(tester);
      await tester.tap(find.text('Play'));
      await settle(tester);
      expect(
        object(calls.last['params'])['command'],
        'effect play "warm-pulse" 100 20',
      );
      await tester.enterText(find.byKey(const ValueKey('Pixels')), '50');
      await tester.pump();
      await tester.tap(find.byTooltip('Refresh tools'));
      await settle(tester);
      await settle(tester);
      expect(
        tester
            .widget<TextFormField>(find.byKey(const ValueKey('Pixels')))
            .initialValue,
        '50',
      );
      await tester.tap(find.text('Recording'));
      await settle(tester);
      expect(
        tester
            .widget<OutlinedButton>(
              find.widgetWithText(OutlinedButton, 'Record Automatic'),
            )
            .onPressed,
        null,
      );
      await tester.enterText(find.byType(TextField).first, 'Demo');
      await tester.pump();
      await tester.tap(find.text('Record Automatic'));
      await settle(tester);
      expect(find.text('macro record start "Demo"'), findsOneWidget);
      await tester.tap(find.text('Run'));
      await settle(tester);
      expect(
        object(calls.last['params'])['command'],
        'macro record start "Demo"',
      );
      await tester.tap(find.text('Learn 30 seconds'));
      await settle(tester);
      expect(calls.last['method'], 'controller.rf.learn.start');
      expect(calls.last['params'], {'mode': 'timer', 'timeout_ms': 30000});
      expect(tester.takeException(), null);
      await tester.pumpWidget(const SizedBox());
      peer.dispose();
      preferences.dispose();
    },
  );
  testWidgets(
    'settings submit conflict keeps the edited draft and dialog open',
    (tester) async {
      final preferences = AppPreferences();
      var attempts = 0;
      final settings = GoBoardSettings.fromJson(fixture.settings());
      await tester.pumpWidget(
        app(
          Scaffold(
            body: Builder(
              builder: (context) => TextButton(
                onPressed: () => showDialog<void>(
                  context: context,
                  builder: (_) => GoSettingsDialog(
                    settings: settings,
                    submit: (_) async {
                      attempts++;
                      throw const Rejected('Board settings changed.');
                    },
                  ),
                ),
                child: const Text('Edit'),
              ),
            ),
          ),
          preferences,
        ),
      );
      await tester.tap(find.text('Edit'));
      await settle(tester);
      await tester.ensureVisible(find.byKey(const ValueKey('on_brightness')));
      await tester.enterText(
        find.byKey(const ValueKey('on_brightness')),
        '200',
      );
      await tester.tap(find.text('Review changes'));
      await settle(tester);
      expect(attempts, 1);
      expect(find.byType(GoSettingsDialog), findsOneWidget);
      expect(settings.values['on_brightness'], 200);
      await tester.ensureVisible(find.text('Board settings changed.'));
      expect(find.text('Board settings changed.'), findsOneWidget);
      expect(tester.takeException(), null);
      await tester.pumpWidget(const SizedBox());
      preferences.dispose();
    },
  );
  testWidgets(
    'display modal validates native ASCII and duration, and supports both displays',
    (tester) async {
      final preferences = AppPreferences();
      Json? result;
      await tester.pumpWidget(
        app(
          Scaffold(
            body: Builder(
              builder: (context) => TextButton(
                onPressed: () async {
                  result = await showDialog<Json>(
                    context: context,
                    builder: (_) => const GoDisplayDialog(
                      text: 'HELLO',
                      target: 'both',
                      lcd: true,
                    ),
                  );
                },
                child: const Text('Edit'),
              ),
            ),
          ),
          preferences,
        ),
      );
      await tester.tap(find.text('Edit'));
      await settle(tester);
      await tester.ensureVisible(
        find.byKey(const ValueKey('display-duration')),
      );
      await tester.enterText(
        find.byKey(const ValueKey('display-duration')),
        '70',
      );
      await tester.tap(find.text('Send'));
      await settle(tester);
      expect(result, null);
      expect(find.text('80–65535'), findsOneWidget);
      await tester.enterText(
        find.byKey(const ValueKey('display-duration')),
        '1000',
      );
      await tester.tap(find.text('Send'));
      await settle(tester);
      expect(result?['target'], 'both');
      expect(result?['duration_ms'], 1000);
      expect(result?['text'], 'HELLO');
      await tester.pumpWidget(const SizedBox());
      preferences.dispose();
    },
  );
  final directory = Platform.environment['PC_GO_PREVIEW_DIR'];
  testWidgets(
    'opt-in actual Go tools phone/tablet source renders have no overflow',
    (tester) async {
      for (final font in [
        ('Vazirmatn', 'assets/fonts/Vazirmatn-Regular.ttf'),
        ('MaterialIcons', 'fonts/MaterialIcons-Regular.otf'),
        ('DroidSansMono', 'assets/fonts/DroidSansMono.ttf'),
      ]) {
        await (FontLoader(font.$1)..addFont(rootBundle.load(font.$2))).load();
      }
      final key = GlobalKey();
      for (final sample in [
        ('phone-commands', const Size(412, 820), 'Commands'),
        ('tablet-lighting', const Size(1280, 800), 'Lighting'),
      ]) {
        tester.view.physicalSize = sample.$2;
        tester.view.devicePixelRatio = 1;
        final calls = <Json>[], preferences = AppPreferences();
        final peer = toolsSession(calls);
        await tester.pumpWidget(
          RepaintBoundary(
            key: key,
            child: app(
              GoToolsPage(session: peer, preferences: preferences),
              preferences,
            ),
          ),
        );
        await settle(tester);
        final dynamic state = tester.state(find.byType(GoToolsPage));
        for (var attempt = 0; state.loading && attempt < 20; attempt++) {
          await settle(tester);
        }
        expect(state.loading, false);
        await tester.ensureVisible(find.text(sample.$3));
        await tester.tap(find.text(sample.$3));
        await settle(tester);
        if (sample.$3 == 'Commands') {
          await tester.enterText(
            find.byType(TextField).first,
            'settings export-live',
          );
          await settle(tester);
        }
        await tester.pumpAndSettle();
        expect(state.ready, true);
        expect(
          tester
              .widget<OutlinedButton>(
                find.widgetWithText(
                  OutlinedButton,
                  sample.$3 == 'Commands' ? 'Run' : 'Apply',
                ),
              )
              .onPressed,
          isNotNull,
        );
        expect(tester.takeException(), null);
        expect(calls.length, 2);
        await tester.runAsync(() async {
          final image =
              await (key.currentContext!.findRenderObject()
                      as RenderRepaintBoundary)
                  .toImage();
          final bytes = await image.toByteData(format: ui.ImageByteFormat.png);
          final file = File('$directory/${sample.$1}.png');
          await file.parent.create(recursive: true);
          await file.writeAsBytes(bytes!.buffer.asUint8List());
          image.dispose();
        });
        await tester.pumpWidget(const SizedBox());
        peer.dispose();
        preferences.dispose();
      }
      tester.view.resetPhysicalSize();
      tester.view.resetDevicePixelRatio();
    },
    skip: directory == null,
  );
}
