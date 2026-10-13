import 'dart:async';
import 'dart:convert';
import 'dart:io';

import 'package:flutter_test/flutter_test.dart';
import 'package:http/http.dart' as http;
import 'package:http/testing.dart';
import 'package:pccontroller_mobile/unified/go_commands.dart';
import 'package:pccontroller_mobile/unified/transport.dart';

dynamic catalog() => jsonDecode(
  File('test/fixtures/go-command-catalog.json').readAsStringSync(),
);
Json settings() => {
  'flags': 134,
  'light_mode': 0,
  'on_brightness': 128,
  'off_brightness': 0,
  'display_brightness': 5,
  'display_closed_brightness': 0,
  'status_brightness': 128,
  'output_persistence': 0,
  'stream_period_ms': 500,
  'default_page': 0,
  'extended_flags': 0,
  'motion_exit_hold_seconds': 2,
  'motion_break_ms': 1,
  'relay_restore_mask': 0,
};
PeerSession session(http.Client client, {bool relay = false}) =>
    PeerSession(controller: true)
      ..transport = relay
          ? ControllerRelayTransport('http://selected', client: client)
          : PeerTransport('http://selected', client: client)
      ..online = true
      ..enabled = true
      ..received = DateTime.now()
      ..state = {
        'host_instance_id': 'fixture',
        'connection_generation': 1,
        'connected': true,
      };

void main() {
  test(
    'settings conflict rejects full write and cancellation sends no preflight',
    () async {
      final executed = <String>[];
      final board = session(
        MockClient((request) async {
          final body = object(jsonDecode(request.body));
          dynamic result = catalog();
          if (body['method'] == 'controller.command.execute') {
            final command = object(body['params'])['command'] as String;
            executed.add(command);
            expect(command, 'settings export-live');
            result = {
              'output': jsonEncode({
                'settings': settings()..['on_brightness'] = 100,
              }),
            };
          }
          return http.Response(
            jsonEncode({'jsonrpc': '2.0', 'id': body['id'], 'result': result}),
            200,
          );
        }),
      );
      final client = GoCommandClient(board);
      addTearDown(() {
        client.dispose();
        board.dispose();
      });
      await client.load();
      final baseline = GoBoardSettings.fromJson(settings()).command();
      final draft = GoBoardSettings.fromJson(settings())
        ..values['on_brightness'] = 200;
      expect(
        await client.saveSettings(
          draft.command(),
          baseline,
          confirm: (_) async => false,
        ),
        null,
      );
      expect(executed, isEmpty);
      await expectLater(
        client.saveSettings(
          draft.command(),
          baseline,
          confirm: (_) async => true,
        ),
        throwsA(isA<Rejected>()),
      );
      expect(executed, ['settings export-live']);
      expect(draft.values['on_brightness'], 200);
    },
  );
  test(
    'emergency-stop release requires confirmation and works while active',
    () async {
      var writes = 0;
      final board = session(
        MockClient((request) async {
          final body = object(jsonDecode(request.body));
          if (body['method'] == 'controller.command.execute') {
            writes++;
            expect(object(body['params'])['command'], 'estop off');
          }
          return http.Response(
            jsonEncode({
              'jsonrpc': '2.0',
              'id': body['id'],
              'result': body['method'] == 'controller.command.catalog'
                  ? catalog()
                  : {'output': 'released'},
            }),
            200,
          );
        }),
      )..state['emergency_stop'] = {'active': true};
      final client = GoCommandClient(board);
      addTearDown(() {
        client.dispose();
        board.dispose();
      });
      await client.load();
      await expectLater(client.run('estop off'), throwsA(isA<Rejected>()));
      await client.run('estop off', confirm: (_) async => false);
      expect(writes, 0);
      await client.run('estop off', confirm: (_) async => true);
      expect(writes, 1);
    },
  );
  test('board names follow the native eight-ASCII-byte limit', () {
    expect(boardNameCommand('PCBOARD1'), 'board name set "PCBOARD1"');
    for (final name in ['TooLongName', ' Board', 'فارسی']) {
      expect(() => boardNameCommand(name), throwsFormatException);
    }
  });
  test('live Go catalog exposes all 50 useful command families and aliases without console lifecycle', () {
    final commands = GoCommand.catalog(catalog());
    expect(commands.length, 50);
    expect(commands.any((c) => c.name == 'quit' || c.name == 'clear'), false);
    expect(
      commands.singleWhere((c) => c.name == 'macro').matches('start-board'),
      true,
    );
    expect(
      commands.singleWhere((c) => c.name == 'query').aliases,
      contains('command'),
    );
    for (final value in [
      null,
      [
        {'name': '../run', 'usage': 'x', 'summary': 'x'},
      ],
      [
        {'name': 'rgb', 'usage': 'x', 'summary': 'x'},
        {'name': 'rgb', 'usage': 'x', 'summary': 'x'},
      ],
    ]) {
      expect(() => GoCommand.catalog(value), throwsA(isA<Rejected>()));
    }
  });
  test('ambiguous writes are never classified as reads or emergency stops', () {
    for (final read in [
      'settings export-live',
      'config get ui.app_title',
      'menu layout',
      'os facts',
      'macro record status',
      'strip effect list',
      'silent both status',
    ]) {
      expect(commandIsRead(read), true, reason: read);
    }
    for (final write in [
      'settings set 0 0',
      'os power shutdown CONFIRM',
      'macro record start name',
      'menu page 2',
      'rf learn',
      'opcode 0x10',
      'query 0x10 0x80',
    ]) {
      expect(commandIsRead(write), false, reason: write);
      expect(commandIsStop(write), false, reason: write);
    }
    expect(commandIsStop('estop off'), false);
    expect(commandIsStop('relay off'), true);
    expect(() => commandWords('status\nrelay 5 on'), throwsFormatException);
  });
  test('display follows actual native byte and timing ranges, both targets and interval semantics', () {
    final display = GoDisplayOptions()
      ..text = 'A"B'
      ..target = 'both'
      ..repeat = 'interval'
      ..intervalSeconds = 255
      ..scroll = true;
    expect(display.params(), {
      'target': 'both',
      'text': 'A"B',
      'speed_ms': 220,
      'duration_ms': 5000,
      'repeat': 'interval',
      'interval_ms': 255000,
      'scroll': true,
    });
    display.repeat = 'loop';
    expect(display.params()['interval_ms'], 0);
    display.text = 'a' * 41;
    expect(display.params, throwsFormatException);
    display.text = 'فارسی';
    expect(display.params, throwsFormatException);
    display.text = '';
    expect(display.params()['text'], ''); // Clear existing override.
    display.speedMs = 79;
    expect(display.params, throwsFormatException);
    display.speedMs = 80;
    display.durationMs = 65536;
    expect(display.params, throwsFormatException);
  });
  test('settings preserve programming/unknown flags and exact 17-argument native layout without guessing defaults', () {
    final value = GoBoardSettings.fromJson(settings());
    expect(
      value.command(),
      'settings set 134 0 128 0 5 0 128 0 500 0 false 0 2 2 2 1 0',
    );
    value.values['flags'] |= 1;
    expect(value.command(), startsWith('settings set 135 '));
    expect(value.values['flags'] & 134, 134);
    for (final key in ['flags', 'motion_break_ms', 'light_mode']) {
      final missing = settings()..remove(key);
      expect(() => GoBoardSettings.fromJson(missing), throwsFormatException);
    }
    for (final change in [
      {'stream_period_ms': 99},
      {'output_persistence': 16},
      {'display_closed_brightness': 8},
      {'extended_flags': 14},
    ]) {
      expect(
        () => GoBoardSettings.fromJson({...settings(), ...change}),
        throwsFormatException,
      );
    }
  });
  test('native melody names remain quoted arguments and repeat counts stay bounded', () {
    expect(
      melodyPlayCommand('two words "quoted"', 20),
      'melody play "two words \\"quoted\\"" 20'.replaceAll('\\\\', '\\'),
    );
    expect(() => melodyPlayCommand('tone', 21), throwsFormatException);
    expect(() => nativeArgument('name\nrelay 1 on'), throwsFormatException);
  });
  test(
    'cancel, changed owner and late E-STOP reject before any command dispatch',
    () async {
      var mutations = 0;
      final board = session(
        MockClient((request) async {
          final body = jsonDecode(request.body);
          if (body['method'] == 'controller.command.execute') mutations++;
          return http.Response(
            jsonEncode({
              'jsonrpc': '2.0',
              'id': body['id'],
              'result': catalog(),
            }),
            200,
          );
        }),
      );
      final commands = GoCommandClient(board);
      addTearDown(() {
        commands.dispose();
        board.dispose();
      });
      await commands.load();
      await commands.run('relay 5 on', confirm: (_) async => false);
      expect(mutations, 0);
      final approval = Completer<bool>();
      final running = commands.run(
        'relay 5 on',
        confirm: (_) => approval.future,
      );
      board.state['emergency_stop'] = {'active': true};
      approval.complete(true);
      await expectLater(running, throwsA(isA<Rejected>()));
      expect(mutations, 0);
      board.generation++;
      await expectLater(commands.run('status'), throwsA(isA<Rejected>()));
      expect(mutations, 0);
    },
  );
  test(
    'lost acknowledgement attempts a mutation once and never retries it',
    () async {
      var mutations = 0;
      final board = session(
        MockClient((request) async {
          final body = jsonDecode(request.body);
          if (body['method'] == 'controller.command.execute') {
            mutations++;
            throw const SocketException('ACK lost');
          }
          return http.Response(
            jsonEncode({
              'jsonrpc': '2.0',
              'id': body['id'],
              'result': catalog(),
            }),
            200,
          );
        }),
      );
      final commands = GoCommandClient(board);
      addTearDown(() {
        commands.dispose();
        board.dispose();
      });
      await commands.load();
      await expectLater(commands.run('relay 5 on'), throwsA(isA<Uncertain>()));
      expect(mutations, 1);
      expect(commands.busy, false);
    },
  );
  test(
    'priority emergency stop remains available during an outstanding command',
    () async {
      final gate = Completer<http.Response>();
      var stops = 0;
      Object? commandId;
      final board = session(
        MockClient((request) async {
          final body = jsonDecode(request.body);
          if (body['method'] == 'controller.command.execute') {
            commandId = body['id'];
            return gate.future;
          }
          if (body['method'] == 'controller.estop.set') {
            stops++;
            expect(body['params'], {'active': true, 'source': 'mobile-tools'});
          }
          return http.Response(
            jsonEncode({
              'jsonrpc': '2.0',
              'id': body['id'],
              'result': body['method'] == 'controller.command.catalog'
                  ? catalog()
                  : {'active': true},
            }),
            200,
          );
        }),
      );
      final commands = GoCommandClient(board);
      addTearDown(() {
        commands.dispose();
        board.dispose();
      });
      await commands.load();
      final running = commands.run('status');
      await Future<void>.delayed(Duration.zero);
      expect(commands.busy, true);
      await commands.emergencyStop();
      expect(stops, 1);
      gate.complete(
        http.Response(
          jsonEncode({
            'jsonrpc': '2.0',
            'id': commandId,
            'result': {'output': 'status'},
          }),
          200,
        ),
      );
      await running;
    },
  );
  test(
    'typed Pealayer relay cannot become a recursive third-peer command proxy',
    () async {
      var executions = 0;
      final board = session(
        MockClient((request) async {
          final body = jsonDecode(request.body);
          if (body['method'] != 'controller.command.catalog') executions++;
          return http.Response(jsonEncode(catalog()), 200);
        }),
        relay: true,
      );
      final commands = GoCommandClient(board);
      addTearDown(() {
        commands.dispose();
        board.dispose();
      });
      await commands.load();
      await expectLater(
        commands.run('bridge call other controller.status'),
        throwsA(isA<Rejected>()),
      );
      await expectLater(
        commands.run('peer-update host other abc'),
        throwsA(isA<Rejected>()),
      );
      expect(executions, 0);
    },
  );
}
