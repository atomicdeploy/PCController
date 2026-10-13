import 'dart:async';
import 'dart:convert';
import 'dart:io';
import 'dart:math';

import 'transport.dart' show Json, object;

typedef AutomationDispatch = Future<dynamic> Function(
  String method,
  Json params,
);

class AutomationFailure implements Exception {
  const AutomationFailure(this.code, this.message);
  final int code;
  final String message;
}

String newAutomationToken() {
  final random = Random.secure();
  return base64Url
      .encode(List<int>.generate(32, (_) => random.nextInt(256)))
      .replaceAll('=', '');
}

bool sameSecret(String a, String b) {
  if (a.length != b.length || a.isEmpty) return false;
  var difference = 0;
  for (var i = 0; i < a.length; i++) {
    difference |= a.codeUnitAt(i) ^ b.codeUnitAt(i);
  }
  return difference == 0;
}

/// Optional native-local IPC. No remote bind, credential forwarding, shell
/// evaluation, arbitrary host proxy, automatic command retry or disk logging.
class LocalAutomation {
  LocalAutomation({required this.dispatch, required this.snapshot});
  final AutomationDispatch dispatch;
  final Json Function() snapshot;
  HttpServer? _http;
  ServerSocket? _tcp;
  final _websockets = <WebSocket>{},
      _clients = <Socket>{},
      _subscriptions = <Object>{};
  String token = '';
  bool allowed = false;
  int _requests = 0;
  bool _mutating = false;
  int _epoch = 0;
  Timer? _publish;
  bool get running => _http != null && _tcp != null;
  static const httpPort = 18990, tcpPort = 18991, maximum = 32768;
  Future<void> start(String secret) async {
    if (running) return;
    if (secret.length < 32) {
      throw const AutomationFailure(-32001, 'Invalid automation secret');
    }
    final epoch = ++_epoch;
    HttpServer? http;
    ServerSocket? tcp;
    try {
      http = await HttpServer.bind(InternetAddress.loopbackIPv4, httpPort);
      tcp = await ServerSocket.bind(InternetAddress.loopbackIPv4, tcpPort);
      if (epoch != _epoch) {
        await http.close(force: true);
        await tcp.close();
        return;
      }
      token = secret;
      _http = http;
      _tcp = tcp;
      allowed = true;
      _http!.listen(
        (request) => unawaited(
          _request(request).catchError((_) async {
            request.response.statusCode = 400;
            await request.response.close();
          }),
        ),
      );
      _tcp!.listen(_connection);
      // Authoritative coalesced app snapshots, never per-frame persistence.
      _publish = Timer.periodic(
        const Duration(milliseconds: 250),
        (_) => _events(),
      );
    } catch (_) {
      await http?.close(force: true);
      await tcp?.close();
      if (epoch == _epoch) await close();
      rethrow;
    }
  }

  Json error(dynamic id, int code, String message) => {
    'jsonrpc': '2.0',
    'id': id,
    'error': {'code': code, 'message': message},
  };
  Future<Json?> receive(
    dynamic input,
    String auth, {
    bool intent = false,
  }) async {
    final request = object(input), id = request['id'];
    if (!allowed || !sameSecret(auth, token)) {
      return error(
        id,
        -32001,
        'Automation is disabled or authorization failed',
      );
    }
    if (request['jsonrpc'] != '2.0' ||
        request['method'] is! String ||
        (id != null && id is! String && id is! int) ||
        (id is String && id.length > 128) ||
        (request['params'] != null && request['params'] is! Map)) {
      return error(id, -32600, 'Invalid JSON-RPC request');
    }
    // Mutating notifications have no receipt, so they are not dispatched.
    if (!request.containsKey('id')) return null;
    if (_requests >= 8) return error(id, -32003, 'Too many active requests');
    if (intent && request['method'] == 'app.capture') {
      return error(id, -32602, 'Use authenticated HTTP for app capture');
    }
    final method = request['method'] as String;
    final mutating =
        [
          'board.action',
          'board.pwm.set',
          'board.connect',
          'board.disconnect',
          'board.menu.press',
          'board.display.show',
          'board.buzzer.play',
          'board.mode.set',
          'player.call',
        ].contains(method) ||
        method.startsWith('app.upgrade.');
    if (mutating && _mutating) {
      return error(id, -32003, 'Another automation action is pending');
    }
    if (mutating) _mutating = true;
    _requests++;
    try {
      final result = await dispatch(
        request['method'],
        object(request['params']),
      );
      return {'jsonrpc': '2.0', 'id': id, 'result': result};
    } on AutomationFailure catch (e) {
      return error(id, e.code, e.message);
    } catch (_) {
      return error(
        id,
        -32000,
        'Request failed or outcome is unknown; nothing was retried',
      );
    } finally {
      _requests--;
      if (mutating) _mutating = false;
    }
  }

  Future<void> _request(HttpRequest request) async {
    final secret = token;
    request.response.headers.contentType = ContentType.json;
    request.response.headers.set('Cache-Control', 'no-store');
    if (!allowed ||
        !sameSecret(
          request.headers.value('authorization') ?? '',
          'Bearer $secret',
        )) {
      request.response.statusCode = HttpStatus.unauthorized;
      request.response.write(
        jsonEncode(error(null, -32001, 'Authorization required')),
      );
      await request.response.close();
      return;
    }
    if (request.uri.path == '/ws' &&
        WebSocketTransformer.isUpgradeRequest(request)) {
      if (_websockets.length >= 4) {
        request.response.statusCode = 429;
        await request.response.close();
        return;
      }
      final socket = await WebSocketTransformer.upgrade(request);
      _websockets.add(socket);
      socket.pingInterval = const Duration(seconds: 10);
      socket.listen(
        (data) async {
          if (data is! String || utf8.encode(data).length > maximum) {
            await socket.close(1009);
            return;
          }
          try {
            final input = jsonDecode(data);
            final response = await receive(input, secret);
            if (object(input)['method'] == 'app.subscribe' &&
                response?.containsKey('result') == true) {
              _subscriptions.add(socket);
            }
            if (response != null && socket.readyState == WebSocket.open) {
              socket.add(jsonEncode(response));
            }
          } catch (_) {
            if (socket.readyState == WebSocket.open) {
              socket.add(jsonEncode(error(null, -32700, 'Invalid JSON')));
            }
          }
        },
        onDone: () {
          _websockets.remove(socket);
          _subscriptions.remove(socket);
        },
        onError: (_) {
          _websockets.remove(socket);
          _subscriptions.remove(socket);
        },
      );
      return;
    }
    if (request.uri.path != '/rpc' || request.method != 'POST') {
      request.response.statusCode = 404;
      await request.response.close();
      return;
    }
    try {
      final bytes = <int>[];
      await for (final chunk in request.timeout(const Duration(seconds: 5))) {
        if (bytes.length + chunk.length > maximum) {
          throw const AutomationFailure(-32600, 'Request exceeds 32 KiB');
        }
        bytes.addAll(chunk);
      }
      final response = await receive(jsonDecode(utf8.decode(bytes)), secret);
      if (response == null) {
        request.response.statusCode = 204;
      } else {
        request.response.write(jsonEncode(response));
      }
    } catch (_) {
      request.response.statusCode = 400;
      request.response.write(
        jsonEncode(error(null, -32700, 'Invalid or oversized JSON')),
      );
    }
    await request.response.close();
  }

  void _connection(Socket socket) {
    if (_clients.length >= 4) {
      socket.destroy();
      return;
    }
    _clients.add(socket);
    final buffer = <int>[];
    var queued = 0;
    Future<void> queue = Future.value();
    socket.listen(
      (chunk) {
        for (final byte in chunk) {
          if (byte != 10) {
            if (buffer.length >= maximum) {
              socket.destroy();
              return;
            }
            buffer.add(byte);
            continue;
          }
          final line = List<int>.of(buffer);
          buffer.clear();
          if (++queued > 8) {
            socket.destroy();
            return;
          }
          queue = queue.then((_) async {
            try {
              if (!allowed || !_clients.contains(socket)) return;
              final input = object(jsonDecode(utf8.decode(line)));
              final auth = '${input['auth'] ?? ''}';
              final response = await receive(input, auth);
              if (response != null) {
                socket.write('${jsonEncode(response)}\n');
                await socket.flush().timeout(const Duration(seconds: 2));
              }
            } catch (_) {
              socket.destroy();
            } finally {
              queued--;
            }
          });
        }
      },
      onDone: () {
        _clients.remove(socket);
        _subscriptions.remove(socket);
      },
      onError: (_) {
        _clients.remove(socket);
        _subscriptions.remove(socket);
      },
    );
  }

  void _events() {
    if (!allowed || _subscriptions.isEmpty) return;
    final frame = jsonEncode({
      'jsonrpc': '2.0',
      'method': 'app.state.changed',
      'params': snapshot(),
    });
    if (frame.length > 262144) return;
    for (final client in List<Object>.of(_subscriptions)) {
      if (client is WebSocket && client.readyState == WebSocket.open) {
        client.add(frame);
      }
      // TCP is request/response and subscribe receipts; use WS for pushed state
      // rather than bypassing stream backpressure with a repeating write loop.
    }
  }

  Future<void> close() async {
    _epoch++;
    allowed = false;
    _publish?.cancel();
    _publish = null;
    for (final socket in _websockets) {
      unawaited(socket.close());
    }
    for (final socket in _clients) {
      socket.destroy();
    }
    _websockets.clear();
    _clients.clear();
    _subscriptions.clear();
    final http = _http, tcp = _tcp;
    _http = null;
    _tcp = null;
    await http?.close(force: true);
    await tcp?.close();
  }
}
