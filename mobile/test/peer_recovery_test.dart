import 'dart:async';
import 'dart:convert';
import 'dart:io';

import 'package:flutter_test/flutter_test.dart';
import 'package:http/http.dart' as http;
import 'package:http/testing.dart';
import 'package:pccontroller_mobile/unified/transport.dart';

class RecoveryTransport extends PeerTransport {
  RecoveryTransport(this.connectSocket, {required super.client})
    : super('http://selected');
  final Future<WebSocket> Function() connectSocket;
  int sockets = 0;
  WebSocket? opened;
  @override
  Future<WebSocket> socket(String path) async {
    sockets++;
    return opened = await connectSocket();
  }
}

class RecoveryRelay extends ControllerRelayTransport {
  RecoveryRelay({required super.client}) : super('http://selected-player');
  int sockets = 0;
  @override
  Future<WebSocket> socket(String path) {
    sockets++;
    throw StateError(
      'A typed controller relay cannot open player WS as controller',
    );
  }
}

void main() {
  test(
    'failed authoritative readback cannot open WS or undo explicit disconnect',
    () async {
      var reads = 0;
      final transport = RecoveryTransport(
        () => throw StateError('No unverified socket'),
        client: MockClient((_) async {
          reads++;
          return http.Response('Unavailable', 503);
        }),
      );
      final session = PeerSession(controller: false)
        ..transport = transport
        ..enabled = true;
      await session.networkAvailable();
      expect(reads, 1);
      expect(transport.sockets, 0);
      expect(session.online, false);
      expect(session.error, contains('HTTP 503'));
      expect(session.enabled, true);
      session.disconnect();
      await session.networkAvailable();
      expect(reads, 1);
      session.dispose();
    },
  );

  test('network recovery coalesces opens, cancels backoff and preserves selected origin', () async {
    final server = await HttpServer.bind(InternetAddress.loopbackIPv4, 0);
    final sockets = <WebSocket>[];
    final accepted = Completer<void>();
    final listener = server.listen((request) async {
      final socket = await WebSocketTransformer.upgrade(request);
      sockets.add(socket);
      socket.listen((_) {});
      if (!accepted.isCompleted) accepted.complete();
    });
    var reads = 0;
    final transport = RecoveryTransport(
      () => WebSocket.connect('ws://127.0.0.1:${server.port}'),
      client: MockClient((request) async {
        reads++;
        expect(request.method, 'GET');
        expect(request.url.host, 'selected');
        return http.Response('{"playing":false,"volume":43}', 200);
      }),
    );
    final session = PeerSession(controller: false)
      ..transport = transport
      ..enabled = true;
    addTearDown(() async {
      session.dispose();
      for (final socket in sockets) {
        await socket.close();
      }
      await listener.cancel();
      await server.close(force: true);
    });
    await Future.wait([session.networkAvailable(), session.networkAvailable()]);
    await accepted.future;
    expect(transport.sockets, 1);
    expect(reads, 1);
    expect(transport.opened!.pingInterval, const Duration(seconds: 10));
    await session.networkAvailable();
    expect(transport.sockets, 1);
    final lost = Completer<void>();
    session.addListener(() {
      if (!session.online && !lost.isCompleted) lost.complete();
    });
    await sockets.first.close();
    await lost.future.timeout(const Duration(seconds: 2));
    await Future.wait([session.networkAvailable(), session.networkAvailable()]);
    await Future<void>.delayed(const Duration(milliseconds: 650));
    expect(transport.sockets, 2);
    expect(reads, 2);
    expect(session.transport!.origin.host, 'selected');
    expect(session.online, true);
  });

  test('explicit disconnect rejects a late socket and cannot be undone by network events', () async {
    final server = await HttpServer.bind(InternetAddress.loopbackIPv4, 0);
    final sockets = <WebSocket>[];
    final listener = server.listen((request) async {
      final socket = await WebSocketTransformer.upgrade(request);
      sockets.add(socket);
      socket.listen((_) {});
    });
    final gate = Completer<WebSocket>(), started = Completer<void>();
    final transport = RecoveryTransport(
      () {
        started.complete();
        return gate.future;
      },
      client: MockClient(
        (_) async => http.Response('{"playing":false,"volume":43}', 200),
      ),
    );
    final session = PeerSession(controller: false)
      ..transport = transport
      ..enabled = true;
    addTearDown(() async {
      session.dispose();
      for (final socket in sockets) {
        await socket.close();
      }
      await listener.cancel();
      await server.close(force: true);
    });
    final opening = session.networkAvailable();
    await started.future;
    session.disconnect();
    final late = await WebSocket.connect('ws://127.0.0.1:${server.port}');
    final closed = Completer<void>();
    late.listen((_) {}, onDone: closed.complete);
    gate.complete(late);
    await opening;
    await closed.future.timeout(const Duration(seconds: 2));
    await session.networkAvailable();
    expect(transport.sockets, 1);
    expect(session.transport, null);
    expect(session.enabled, false);
    expect(session.online, false);
    expect(session.state, isEmpty);
    expect(late.readyState, WebSocket.closed);
  });

  test('selected Pealayer controller relay recovers by readback without opening player WS', () async {
    final methods = <String>[];
    final relay = RecoveryRelay(
      client: MockClient((request) async {
        expect(
          request.method,
          'POST',
        ); // Typed read-only RPC over the relay route.
        expect(request.url.path, '/api/peer/controller');
        final rpc = object(jsonDecode(request.body));
        methods.add(rpc['method'] as String);
        final Object result = switch (rpc['method']) {
          'controller.snapshot' => {
            'host_instance_id': 'fixture',
            'connected': false,
          },
          'controller.peripherals.get' => {
            'controls': [],
            'peripherals': [],
            'board_profile': {},
          },
          'controller.app.instances' => [],
          _ => throw StateError('Unexpected command or mutation'),
        };
        return http.Response(jsonEncode(result), 200);
      }),
    );
    final session = PeerSession(controller: true)
      ..transport = relay
      ..enabled = true;
    await session.networkAvailable();
    expect(relay.sockets, 0);
    expect(methods, [
      'controller.snapshot',
      'controller.peripherals.get',
      'controller.app.instances',
    ]);
    expect(session.online, true);
    expect(session.subscribed, false);
    session.dispose();
  });
}
