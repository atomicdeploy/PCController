import 'dart:async';
import 'dart:convert';
import 'dart:io';

import 'package:flutter_test/flutter_test.dart';
import 'package:http/http.dart' as http;
import 'package:http/testing.dart';
import 'package:pccontroller_mobile/unified/transport.dart';

class LiveFixtureTransport extends PeerTransport {
  LiveFixtureTransport(this.port, {required super.client})
    : super('http://fixture');
  final int port;
  bool unavailable = false;
  @override
  Future<WebSocket> socket(String path) => unavailable
      ? Future.error(const SocketException('Fixture WS unavailable'))
      : WebSocket.connect('ws://127.0.0.1:$port/ws');
}

void main() {
  test('acknowledged live stream makes zero periodic HTTP/PWM reads, events read once and WS loss uses fallback', () async {
    final server = await HttpServer.bind(InternetAddress.loopbackIPv4, 0);
    final accepted = Completer<WebSocket>();
    final sockets = <WebSocket>[];
    final heartbeats = <Timer>[];
    final listener = server.listen((request) async {
      final socket = await WebSocketTransformer.upgrade(request);
      sockets.add(socket);
      socket.listen((message) {
        final value = jsonDecode(message as String);
        expect(value['method'], 'controller.subscribe');
        socket.add(
          jsonEncode({
            'jsonrpc': '2.0',
            'id': value['id'],
            'result': {'subscribed': true, 'instance_id': 'fixture'},
          }),
        );
        heartbeats.add(
          Timer.periodic(const Duration(milliseconds: 100), (_) {
            if (socket.readyState != WebSocket.open) return;
            socket.add(
              jsonEncode({
                'jsonrpc': '2.0',
                'method': 'controller.status',
                'params': {
                  'time': DateTime.now().toUtc().toIso8601String(),
                  'status': {'active_relays': 0},
                },
              }),
            );
          }),
        );
        if (!accepted.isCompleted) accepted.complete(socket);
      });
    });
    var reads = 0, pwmReads = 0;
    final transport = LiveFixtureTransport(
      server.port,
      client: MockClient((request) async {
        reads++;
        if (request.url.path == '/api/ui-config') {
          return http.Response(
            '{"websocket_path":"/ws","measurement_freshness_ms":1500,"status_interval_ms":250}',
            200,
          );
        }
        if (request.url.path == '/api/snapshot') {
          return http.Response(
            jsonEncode({
              'host_instance_id': 'fixture',
              'connection_generation': 1,
              'connected': true,
              'have_front_panel': true,
              'have_status': true,
              'status_updated': DateTime.now().toUtc().toIso8601String(),
              'status': {'active_relays': 0},
            }),
            200,
          );
        }
        final rpc = jsonDecode(request.body);
        expect([
          'controller.peripherals.get',
          'controller.pwm.values',
          'controller.app.instances',
        ], contains(rpc['method']));
        Object result = {};
        if (rpc['method'] == 'controller.peripherals.get') {
          result = {
            'board_profile': {'key': 'fixture', 'revision': 'r1'},
            'controls': [
              {
                'key': 'pwm.0',
                'name': 'Window',
                'kind': 'pwm',
                'control': 'pwm-user',
              },
            ],
            'peripherals': [],
            'folders': [],
          };
        } else if (rpc['method'] == 'controller.pwm.values') {
          pwmReads++;
          result = {
            'available': true,
            'channels': [
              {'channel': 0, 'logical_percent': 42},
            ],
          };
        } else {
          result = {'instances': []};
        }
        return http.Response(
          jsonEncode({'jsonrpc': '2.0', 'id': rpc['id'], 'result': result}),
          200,
        );
      }),
    );
    final session = PeerSession(controller: true)
      ..transport = transport
      ..enabled = true;
    addTearDown(() async {
      session.dispose();
      for (final timer in heartbeats) {
        timer.cancel();
      }
      for (final socket in sockets) {
        await socket.close();
      }
      await listener.cancel();
      await server.close(force: true);
    });
    await session.networkAvailable();
    final socket = await accepted.future.timeout(const Duration(seconds: 2));
    await Future<void>.delayed(const Duration(milliseconds: 200));
    expect(session.liveStateAvailable, true);
    expect(pwmReads, 1);
    final baseline = reads;
    session.pwmUpdated = DateTime.now().subtract(const Duration(seconds: 30));
    await Future<void>.delayed(const Duration(milliseconds: 3200));
    expect(reads, baseline);
    expect(pwmReads, 1);
    expect(
      session.pwmReadbackFresh,
      true,
    ); // Continuous same-owner live subscription.
    socket.add(
      jsonEncode({
        'jsonrpc': '2.0',
        'method': 'controller.event',
        'params': {'kind': 'pwm.changed', 'metadata': {}},
      }),
    );
    await Future<void>.delayed(const Duration(milliseconds: 750));
    expect(pwmReads, 2);
    expect(reads, baseline + 1);
    socket.add(
      jsonEncode({
        'jsonrpc': '2.0',
        'method': 'controller.event',
        'params': {
          'kind': 'pwm.changed',
          'metadata': {'revision': '7', 'connection_generation': '1'},
        },
      }),
    );
    socket.add(
      jsonEncode({
        'jsonrpc': '2.0',
        'method': 'controller.pwm.values',
        'params': {
          'host_instance_id': 'fixture',
          'connection_generation': 1,
          'revision': 7,
          'time': DateTime.now().toUtc().toIso8601String(),
          'available': true,
          'selected_channel': 0,
          'values': List.generate(16, (i) => i * 100),
          'channels': List.generate(
            16,
            (i) => {
              'channel': i,
              'raw_value': i * 100,
              'logical_percent': i * 5.0,
            },
          ),
        },
      }),
    );
    await Future<void>.delayed(const Duration(milliseconds: 750));
    expect(session.pwmValues['revision'], 7);
    expect(session.pwmValues['channels'][3]['logical_percent'], 15);
    expect(pwmReads, 2);
    expect(reads, baseline + 1);
    transport.unavailable = true;
    await socket.close();
    await Future<void>.delayed(const Duration(milliseconds: 1300));
    expect(session.liveStateAvailable, false);
    expect(reads, greaterThan(baseline + 1));
    session.disconnect();
    final stopped = reads;
    await Future<void>.delayed(const Duration(milliseconds: 1100));
    expect(reads, stopped);
  });
}
