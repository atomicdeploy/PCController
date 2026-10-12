import 'dart:async';
import 'dart:convert';
import 'dart:io';

import 'package:flutter_test/flutter_test.dart';
import 'package:pccontroller_mobile/api.dart';
import 'package:pccontroller_mobile/models.dart';

import 'mirroring_test.dart' show mirroredPeer, toneEvent;

// Pure transport test, deliberately isolated from TestWidgetsFlutterBinding's
// blocked HTTP client. It is a real loopback socket, not a physical board.
void main() {
  test('Actual authenticated WebSocket routes only verified-owner peripheral events', () async {
    final server = await HttpServer.bind(InternetAddress.loopbackIPv4, 0);
    final observed = <Map<String, dynamic>>[], done = Completer<void>();
    final api = PakarApi(
      baseUrl: 'http://127.0.0.1:${server.port}',
      user: 'fixture',
      password: 'fixture',
    );
    StreamSubscription<DeviceStatus>? subscription;
    final requests = server.listen((request) async {
      expect(
        request.headers.value('authorization'),
        'Basic ${base64Encode(utf8.encode('fixture:fixture'))}',
      );
      if (request.uri.path == '/ws') {
        final socket = await WebSocketTransformer.upgrade(request);
        socket.listen((_) {
          socket.add(
            jsonEncode({...toneEvent(1, 440, 50), 'board_id': 'other'}),
          );
          socket.add(jsonEncode(toneEvent(2, 880, 100)));
          socket.add(jsonEncode(mirroredPeer().json));
        });
      } else {
        expect(request.method, 'GET');
        request.response.headers.contentType = ContentType.json;
        request.response.write(jsonEncode(mirroredPeer().json));
        await request.response.close();
      }
    });
    try {
      api.onPeripheralEvent = observed.add;
      await api.status();
      subscription = api.liveStatus().listen((_) {
        if (!done.isCompleted) done.complete();
      });
      await done.future.timeout(const Duration(seconds: 5));
      expect(observed.length, 1);
      expect(observed.single['event_id'], 2);
    } finally {
      await api.close();
      await subscription?.cancel();
      await requests.cancel();
      await server.close(force: true);
    }
  });
}
