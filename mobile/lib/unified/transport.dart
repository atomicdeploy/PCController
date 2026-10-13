import 'dart:async';
import 'dart:convert';
import 'dart:io';
import 'dart:typed_data';

import 'package:crypto/crypto.dart';
import 'package:flutter/foundation.dart';
import 'package:http/http.dart' as http;

import 'output_events.dart';

typedef Json = Map<String, dynamic>;
String? selectedControllerOrigin(
  bool wanted,
  bool relay,
  String direct,
  String player,
) {
  if (!wanted) return null;
  final selected = relay ? player : direct;
  return selected.isEmpty ? null : selected;
}

Json object(dynamic value) =>
    value is Map ? Map<String, dynamic>.from(value) : {};
List<Json> objects(dynamic value) =>
    value is List ? value.whereType<Map>().map(object).toList() : [];

bool measurementIsFresh(Json snapshot, int limitMs, {DateTime? now}) {
  if (snapshot['have_status'] != true ||
      snapshot['status_updated'] is! String) {
    return false;
  }
  final time = DateTime.tryParse(snapshot['status_updated']);
  if (time == null) return false;
  final age = (now ?? DateTime.now()).difference(time).inMilliseconds;
  return age >= -1000 && age <= limitMs;
}

Json verifiedCatalog(dynamic value) {
  final catalog = object(value);
  if (catalog['controls'] is! List ||
      catalog['peripherals'] is! List ||
      catalog['board_profile'] is! Map ||
      (catalog['controls'] as List).any(
        (item) =>
            item is! Map ||
            item['key'] is! String ||
            item['name'] is! String ||
            item['control'] is! String,
      )) {
    throw const Rejected(
      'Invalid channel catalog. Last verified inventory retained; controls disabled.',
    );
  }
  return catalog;
}

Uri endpoint(String value) {
  final uri = Uri.parse(value.trim());
  if (!['http', 'https'].contains(uri.scheme) ||
      uri.host.isEmpty ||
      uri.userInfo.isNotEmpty ||
      uri.hasQuery ||
      uri.hasFragment ||
      !['', '/'].contains(uri.path)) {
    throw const FormatException(
      'Use an HTTP(S) host and port, without credentials or a path.',
    );
  }
  return uri.replace(path: '');
}

class Rejected implements Exception {
  const Rejected(this.message);
  final String message;
  @override
  String toString() => message;
}

class Uncertain implements Exception {
  const Uncertain();
  @override
  String toString() =>
      'Acknowledgement unavailable. The action was not retried; refresh before trying again.';
}

/// Native IO transport: credentials never appear in URLs, redirects or peers.
class PeerTransport {
  PeerTransport(String origin, {this.token = '', http.Client? client})
    : origin = endpoint(origin),
      client = client ?? http.Client();
  final Uri origin;
  final String token;
  final http.Client client;
  int _id = 0;
  bool closed = false;
  Map<String, String> get headers => {
    if (token.isNotEmpty) 'Authorization': 'Bearer $token',
    'Content-Type': 'application/json',
  };
  Future<dynamic> request(String path, {dynamic body}) async {
    if (closed) throw const Rejected('Connection changed.');
    final request =
        http.Request(body == null ? 'GET' : 'POST', origin.resolve(path))
          ..followRedirects = false
          ..headers.addAll(headers);
    if (body != null) request.body = jsonEncode(body);
    try {
      final response = await client
          .send(request)
          .timeout(const Duration(seconds: 12));
      final bytes = await boundedBytes(response, 8 * 1024 * 1024);
      if (closed) {
        throw const Rejected('Connection changed; old response discarded.');
      }
      if (response.statusCode < 200 || response.statusCode >= 300) {
        throw Rejected('Host rejected request (HTTP ${response.statusCode}).');
      }
      return jsonDecode(utf8.decode(bytes));
    } on Rejected {
      rethrow;
    } catch (_) {
      if (body != null) throw const Uncertain();
      throw const Rejected(
        'Host unavailable or invalid response. Check address, access token and network.',
      );
    }
  }

  Future<dynamic> rpc(String method, [Json params = const {}]) async {
    final id = ++_id;
    final result = object(
      await request(
        '/api/rpc',
        body: {'jsonrpc': '2.0', 'id': id, 'method': method, 'params': params},
      ),
    );
    if (result['jsonrpc'] != '2.0' ||
        result['id'] != id ||
        result.containsKey('result') == result.containsKey('error')) {
      throw const Uncertain();
    }
    if (result.containsKey('error')) {
      final error = object(result['error']);
      if (error['code'] is! int || error['message'] is! String) {
        throw const Uncertain();
      }
      throw Rejected(error['message']);
    }
    return result['result'];
  }

  Future<WebSocket> socket(String path) => WebSocket.connect(
    origin
        .replace(scheme: origin.scheme == 'https' ? 'wss' : 'ws', path: path)
        .toString(),
    headers: token.isEmpty ? {} : {'Authorization': 'Bearer $token'},
  ).timeout(const Duration(seconds: 8));

  Future<Json> upload(String name, Uint8List bytes) async {
    if (bytes.isEmpty || bytes.length > 2 * 1024 * 1024) {
      throw const Rejected('Firmware must be 1 byte–2 MiB.');
    }
    final digest = sha256.convert(bytes).toString();
    final uri = origin
        .resolve('/api/artifacts/upload')
        .replace(
          queryParameters: {
            'kind': 'firmware',
            'name': name,
            'sha256': digest,
            'bytes': '${bytes.length}',
          },
        );
    final request = http.MultipartRequest('POST', uri)..followRedirects = false;
    if (token.isNotEmpty) request.headers['Authorization'] = 'Bearer $token';
    request.files.add(
      http.MultipartFile.fromBytes('artifact', bytes, filename: name),
    );
    try {
      final response = await client
          .send(request)
          .timeout(const Duration(seconds: 30));
      final value = object(
        jsonDecode(utf8.decode(await boundedBytes(response, 1024 * 1024))),
      );
      if (closed) throw const Uncertain();
      if (response.statusCode != 200 && response.statusCode != 201) {
        throw Rejected('Artifact upload rejected (${response.statusCode}).');
      }
      if (object(value['artifact'])['sha256'] != digest) {
        throw const Uncertain();
      }
      return value;
    } on Rejected {
      rethrow;
    } catch (_) {
      throw const Uncertain();
    }
  }

  Future<Uint8List> download(Json artifact) async {
    final kind = artifact['kind'],
        digest = artifact['sha256'],
        count = artifact['bytes'];
    if (!['firmware', 'flash-backup', 'eeprom'].contains(kind) ||
        digest is! String ||
        !RegExp(r'^[a-f0-9]{64}$').hasMatch(digest) ||
        count is! int ||
        count < 1 ||
        count > 2 * 1024 * 1024) {
      throw const Rejected('Invalid artifact descriptor.');
    }
    final request =
        http.Request('GET', origin.resolve('/api/artifacts/$kind/$digest'))
          ..followRedirects = false
          ..headers.addAll(headers);
    final response = await client
        .send(request)
        .timeout(const Duration(seconds: 30));
    final bytes = await boundedBytes(response, count);
    if (closed ||
        response.statusCode != 200 ||
        bytes.length != count ||
        sha256.convert(bytes).toString() != digest) {
      throw const Rejected(
        'Download integrity check failed; nothing exported.',
      );
    }
    return bytes;
  }

  void close() {
    closed = true;
    client.close();
  }
}

/// An explicit host-selected route, not a protocol fallback or arbitrary proxy.
/// File bytes remain direct-host-only: Pealayer does not advertise artifact IO.
class ControllerRelayTransport extends PeerTransport {
  ControllerRelayTransport(super.origin, {super.token, super.client});
  @override
  Future<dynamic> rpc(String method, [Json params = const {}]) {
    if (!method.startsWith('controller.') ||
        method == 'controller.bridge.call' ||
        method == 'controller.peer.update.host') {
      throw const Rejected(
        'Recursive or third-peer bridge calls are not allowed.',
      );
    }
    return super.request(
      '/api/peer/controller',
      body: {'method': method, 'params': params},
    );
  }

  @override
  Future<dynamic> request(String path, {dynamic body}) {
    if (body != null) throw const Rejected('Use a typed controller action.');
    if (path == '/api/snapshot') return rpc('controller.snapshot');
    if (path == '/api/ui-config') return rpc('controller.ui.config');
    throw const Rejected('This bridge does not advertise that resource.');
  }

  @override
  Future<Json> upload(String name, Uint8List bytes) => Future.error(
    const Rejected('Connect directly to PCController for artifact transfer.'),
  );
  @override
  Future<Uint8List> download(Json artifact) => Future.error(
    const Rejected('Connect directly to PCController for artifact transfer.'),
  );
}

Future<Uint8List> boundedBytes(
  http.StreamedResponse response,
  int maximum,
) async {
  final builder = BytesBuilder(copy: false);
  await for (final chunk in response.stream.timeout(
    const Duration(seconds: 15),
  )) {
    if (builder.length + chunk.length > maximum) {
      throw const Rejected('Response exceeds its safety limit.');
    }
    builder.add(chunk);
  }
  return builder.takeBytes();
}

double? operationProgress(Json value) {
  // No synthetic zero while provisioning/ensuring/verifying the toolchain.
  if (value['progress_known'] != true ||
      '${value['state']}'.startsWith('toolchain-') ||
      (value['kind'] == 'firmware' &&
          object(value['toolchain'])['ready'] != true)) {
    return null;
  }
  final percent = value['progress_percent'];
  return percent is num && percent.isFinite
      ? percent.clamp(0, 100) / 100
      : null;
}

String operationLabel(Json value) {
  final state = '${value['state'] ?? 'Unavailable'}';
  if (['completed', 'failed', 'cancelled'].contains(state)) return state;
  return '${value['stage'] ?? state}';
}

class PeerSession extends ChangeNotifier {
  PeerSession({required this.controller});
  final bool controller;
  void Function(Json)? onBuzzerNote;
  PeerTransport? transport;
  WebSocket? _socket;
  Timer? _retry, _freshness;
  int generation = 0, _attempt = 0;
  bool enabled = false, online = false, subscribed = false;
  String? error;
  Json state = {}, peripherals = {}, operation = {}, pwmValues = {};
  DateTime? pwmUpdated, _lastPanelAttempt;
  bool? _pwmAvailable;
  String _panelOwner = '';
  bool _pwmReading = false;
  int _pwmRevision = 0, _pwmWriteSequence = 0;
  int _catalogRevision = 0;
  int? _activePwmWrite;
  Timer? _pwmTimer;
  List<Json> instances = [], artifacts = [], events = [];
  DateTime? received;
  int freshnessMs = 5000;
  int statusIntervalMs = 250;
  // PWM is polled every two seconds; status freshness can be only 1.5 seconds.
  // Allow two missed read intervals, while still requiring the live board owner.
  int get pwmFreshnessMs => freshnessMs.clamp(6000, 120000);
  bool get pwmReadbackFresh =>
      boardReady &&
      _pwmAvailable != false &&
      pwmUpdated != null &&
      DateTime.now().difference(pwmUpdated!).inMilliseconds < pwmFreshnessMs;
  String websocketPath = '/ws';
  bool get measurementsFresh =>
      fresh &&
      state['connected'] == true &&
      measurementIsFresh(state, freshnessMs);
  bool get fresh =>
      online &&
      received != null &&
      DateTime.now().difference(received!).inMilliseconds < freshnessMs;
  bool get boardReady =>
      controller &&
      fresh &&
      state['connected'] == true &&
      ![
        'programming',
        'bootloader',
        'locked',
      ].contains(state['connection_state']);
  String get identity =>
      '${state['host_instance_id'] ?? object(state['runtime'])['instance_id'] ?? ''}';
  String get targetStamp =>
      '$generation/$identity/${state['connection_generation']}/${state['port']}/${object(state['hello'])['build_hash']}/${object(peripherals['board_profile'])['revision']}';
  String get stableTargetStamp =>
      '${transport?.origin}/${transport is ControllerRelayTransport}/$identity/${state['connection_generation']}/${state['port']}/${object(state['hello'])['build_hash']}/${object(peripherals['board_profile'])['revision']}';

  Future<void> connect(String origin, String token) async {
    disconnect();
    transport = PeerTransport(origin, token: token);
    enabled = true;
    await _open(generation);
  }

  Future<void> connectRelay(String origin, String token) async {
    disconnect();
    transport = ControllerRelayTransport(origin, token: token);
    enabled = true;
    await refresh();
    final epoch = generation;
    // The relay advertises bounded request/response, not controller WS frames.
    _freshness = Timer.periodic(const Duration(seconds: 2), (_) {
      if (epoch == generation && enabled) unawaited(refresh());
    });
  }

  bool _refreshing = false;

  void acceptPresentationCatalog(Json catalog) {
    peripherals = verifiedCatalog(catalog);
    _catalogRevision++;
    notifyListeners();
  }

  Future<void> refresh() async {
    final epoch = generation, peer = transport;
    if (peer == null || _refreshing) return;
    _refreshing = true;
    try {
      final value = object(
        await peer.request(controller ? '/api/snapshot' : '/api/player/status'),
      );
      if (epoch != generation) return;
      _install(value);
      online = true;
      error = null;
      notifyListeners();
      if (controller) {
        final catalogRevision = _catalogRevision;
        final inventory = verifiedCatalog(
          await peer.rpc('controller.peripherals.get'),
        );
        if (epoch != generation) return;
        if (catalogRevision == _catalogRevision) peripherals = inventory;
        await refreshPwm();
        if (epoch != generation) return;
        final owner = '$identity/${state['connection_generation']}';
        if (state['connected'] == true &&
            state['have_front_panel'] != true &&
            _panelOwner != owner &&
            (_lastPanelAttempt == null ||
                DateTime.now().difference(_lastPanelAttempt!).inSeconds >=
                    10)) {
          _lastPanelAttempt = DateTime.now();
          final requestedAt = _lastPanelAttempt!;
          try {
            var panel = object(await peer.rpc('controller.front_panel'));
            if (epoch != generation) return;
            if (owner != '$identity/${state['connection_generation']}') return;
            if (panel['raw_segments'] is List && panel['schema'] is int) {
              final pushedAt = DateTime.tryParse(
                '${state['front_panel_updated']}',
              );
              if (pushedAt != null && pushedAt.isAfter(requestedAt)) {
                panel = {
                  ...panel,
                  for (final field in [
                    'raw_segments',
                    'brightness',
                    'segments_active',
                  ])
                    field: object(state['front_panel'])[field],
                };
              }
              state = {
                ...state,
                'front_panel': panel,
                'have_front_panel': true,
              };
              _panelOwner = owner;
            }
          } catch (_) {
            /* Missing panel does not invalidate telemetry/channel truth. */
          }
        }
        final incomingInstances = objects(
          await peer.rpc('controller.app.instances'),
        );
        if (epoch != generation) return;
        instances = incomingInstances;
        notifyListeners();
      }
    } catch (e) {
      if (epoch == generation) {
        error = '$e';
        online = false;
        notifyListeners();
      }
    } finally {
      _refreshing = false;
    }
  }

  void _install(Json value) {
    if (controller &&
        (value['host_instance_id'] is! String ||
            (value['host_instance_id'] as String).trim().isEmpty ||
            value['connected'] is! bool)) {
      throw const Rejected('Missing controller identity or connection state.');
    }
    if (!controller && (value['playing'] is! bool || value['volume'] is! num)) {
      throw const Rejected('Missing player state.');
    }
    if (controller &&
        (state['host_instance_id'] != value['host_instance_id'] ||
            state['connection_generation'] != value['connection_generation'])) {
      peripherals = {};
      instances = [];
      operation = {};
      events = [];
      pwmValues = {};
      pwmUpdated = null;
      _pwmAvailable = null;
      _pwmRevision++;
      _activePwmWrite = null;
      _panelOwner = '';
      _lastPanelAttempt = null;
    }
    // A slow snapshot must not erase newer pushed display/RGB state from the
    // same board generation. Never retain it across a changed board/host.
    if (controller &&
        state['host_instance_id'] == value['host_instance_id'] &&
        state['connection_generation'] == value['connection_generation']) {
      final oldTime = DateTime.tryParse('${state['front_panel_updated']}');
      final newTime = DateTime.tryParse('${value['front_panel_updated']}');
      if (oldTime != null && (newTime == null || oldTime.isAfter(newTime))) {
        value = {
          ...value,
          'front_panel': {
            ...object(value['front_panel']),
            for (final key in ['raw_segments', 'brightness', 'segments_active'])
              key: object(state['front_panel'])[key],
          },
          'have_front_panel_segments': state['have_front_panel_segments'],
          'front_panel_updated': state['front_panel_updated'],
        };
      }
      if ((state['status_led_revision'] as num? ?? 0) >
          (value['status_led_revision'] as num? ?? 0)) {
        value = {
          ...value,
          'status_led': state['status_led'],
          'status_led_revision': state['status_led_revision'],
          'have_status_led': state['have_status_led'],
          'status_led_updated': state['status_led_updated'],
        };
      }
    }
    state = value;
    received = DateTime.now();
  }

  Future<void> _open(int epoch) async {
    if (!enabled || epoch != generation || transport == null) return;
    try {
      if (controller) {
        final config = object(await transport!.request('/api/ui-config'));
        if (epoch != generation) return;
        final path = config['websocket_path'];
        if (path is! String ||
            !path.startsWith('/') ||
            path.startsWith('//') ||
            path.contains('?') ||
            path.contains('#') ||
            path.contains('..') ||
            path.length > 128) {
          throw const Rejected('Host did not advertise a safe WebSocket path.');
        }
        websocketPath = path;
        final duration = config['measurement_freshness_ms'];
        if (duration is int) freshnessMs = duration.clamp(500, 120000);
        final interval = config['status_interval_ms'];
        if (interval is int && interval >= 50 && interval <= 60000) {
          statusIntervalMs = interval;
        }
      }
      await refresh();
      if (!enabled || epoch != generation) return;
      final socket = await transport!.socket(websocketPath);
      if (epoch != generation || !enabled) {
        await socket.close();
        return;
      }
      _socket = socket;
      if (controller) {
        socket.add(
          jsonEncode({
            'jsonrpc': '2.0',
            'id': 'mobile-subscribe',
            'method': 'controller.subscribe',
            'params': {
              'topics': ['events', 'state', 'status'],
              'interval_ms': statusIntervalMs,
              'after_id': 0,
            },
          }),
        );
      }
      socket.listen(
        (frame) {
          if (epoch != generation || _socket != socket) return;
          try {
            if (frame is! String || frame.length > 8 * 1024 * 1024) {
              throw const FormatException();
            }
            final value = object(jsonDecode(frame));
            if (!controller) {
              if (value['jsonrpc'] == '2.0') return;
              _install(
                value['type'] == 'state' ? object(value['state']) : value,
              );
              online = true;
            } else if (value['id'] == 'mobile-subscribe') {
              final acknowledgement = object(value['result']);
              subscribed =
                  acknowledgement['subscribed'] == true &&
                  acknowledgement['instance_id'] == identity;
              if (!subscribed) {
                socket.close();
                return;
              }
            } else if (value['method'] == 'controller.snapshot') {
              if (!subscribed) return;
              _install(object(value['params']));
              online = true;
            } else if (value['method'] == 'controller.status') {
              if (!subscribed) return;
              final params = object(value['params']);
              if (params['error'] != null) {
                error = '${params['error']}';
                return;
              }
              if (params['status'] is! Map) return;
              state = {
                ...state,
                'status': params['status'],
                'have_status': true,
                'status_updated': params['time'],
              };
              received = DateTime.now();
              online = true;
            } else if ([
              'controller.event',
              'controller.state',
            ].contains(value['method'])) {
              final event = object(value['params']);
              if (!subscribed) return;
              final payload = object(event['metadata']);
              if ([
                'front_panel.segment',
                'status_led.changed',
              ].contains(event['kind'])) {
                final updated = applyOutputEvent(state, event);
                if (!identical(updated, state)) {
                  state = updated;
                  received = DateTime.now();
                  online = true;
                  error = null;
                }
                if (event['kind'] == 'status_led.changed') {
                  _schedulePwmRefresh(epoch);
                }
                notifyListeners();
                return; // Frames never trigger full snapshot/catalog polling.
              }
              if (event['kind'] == 'buzzer.note') {
                onBuzzerNote?.call(event);
                return; // Audio frames must not poll snapshot/catalog per note.
              }
              events = [event, ...events].take(100).toList();
              if ([
                'pwm.changed',
                'pwm',
                'illumination.changed',
                'status_led.changed',
              ].contains(event['kind'])) {
                _schedulePwmRefresh(epoch);
              }
              if ('${event['kind']}'.startsWith('update.')) {
                // Events are cues to read durable status, not fabricated progress.
                unawaited(loadOperation());
              }
              if (payload.isNotEmpty || value['method'] == 'controller.state') {
                _scheduleRefresh(epoch);
              }
            }
            _attempt = 0;
            error = null;
            notifyListeners();
          } catch (_) {
            error = 'Invalid live state; refreshing authoritative snapshot.';
            _scheduleRefresh(epoch);
          }
        },
        onError: (_) {
          if (_socket == socket) {
            _socket = null;
            unawaited(socket.close());
            _lost(epoch);
          }
        },
        onDone: () {
          if (_socket == socket) {
            _socket = null;
            _lost(epoch);
          }
        },
        cancelOnError: true,
      );
      _freshness?.cancel();
      _freshness = Timer.periodic(const Duration(seconds: 1), (_) {
        if (epoch != generation) return;
        if (fresh &&
            state['connected'] == true &&
            (pwmUpdated == null ||
                DateTime.now().difference(pwmUpdated!).inMilliseconds >=
                    2000)) {
          unawaited(refreshPwm());
        }
        // Only repaint when freshness expires, not continuous UI persistence.
        if (!fresh && online) {
          online = false;
          notifyListeners();
          _scheduleRefresh(epoch);
        }
      });
    } catch (e) {
      if (epoch == generation) {
        error = '$e';
        _lost(epoch);
      }
    }
  }

  Timer? _refreshTimer;
  void _schedulePwmRefresh(int epoch) {
    if (_pwmTimer?.isActive == true) return;
    _pwmTimer = Timer(const Duration(milliseconds: 500), () {
      if (epoch == generation) unawaited(refreshPwm());
    });
  }

  Future<void> refreshPwm() async {
    final peer = transport, epoch = generation;
    final revision = _pwmRevision;
    final owner = '$identity/${state['connection_generation']}';
    if (!controller ||
        peer == null ||
        _pwmReading ||
        _activePwmWrite != null ||
        state['connected'] != true) {
      return;
    }
    _pwmReading = true;
    try {
      final value = object(await peer.rpc('controller.pwm.values'));
      if (epoch == generation &&
          revision == _pwmRevision &&
          _activePwmWrite == null &&
          owner == '$identity/${state['connection_generation']}' &&
          value['channels'] is List &&
          value['available'] is bool) {
        _pwmAvailable = value['available'] == true && _validPwmReadback(value);
        if (_pwmAvailable == true) {
          pwmValues = value;
          pwmUpdated = DateTime.now();
        }
        notifyListeners();
      }
    } catch (_) {
      /* Retained PWM cache is never substituted with a false zero. */
    } finally {
      _pwmReading = false;
    }
  }

  bool _validPwmReadback(Json value) {
    final channels = objects(value['channels']);
    if (channels.isEmpty ||
        channels.length != (value['channels'] as List).length) {
      return false;
    }
    final seen = <int>{};
    for (final reading in channels) {
      final channel = reading['channel'], percent = reading['logical_percent'];
      if (channel is! int ||
          channel < 0 ||
          channel > 15 ||
          !seen.add(channel) ||
          percent is! num ||
          !percent.isFinite ||
          percent < 0 ||
          percent > 100) {
        return false;
      }
    }
    for (final control in objects(peripherals['controls'])) {
      final key = '${control['key']}';
      if (key.startsWith('pwm.')) {
        final channel = int.tryParse(key.substring(4));
        if (channel == null || !seen.contains(channel)) return false;
      }
    }
    return true;
  }

  /// Typed set + its actual board readback. The host requests PWMValues only
  /// after command acknowledgement; this is not an optimistic local setpoint.
  Future<double> setPwm(
    int channel,
    num percent, {
    required String expectedTarget,
  }) async {
    final peer = transport, epoch = generation;
    if (!controller ||
        peer == null ||
        !boardReady ||
        targetStamp != expectedTarget ||
        _activePwmWrite != null ||
        channel < 0 ||
        channel > 15 ||
        !percent.isFinite ||
        percent < 0 ||
        percent > 100 ||
        object(state['emergency_stop'])['active'] == true ||
        !objects(peripherals['controls']).any(
          (c) =>
              c['key'] == 'pwm.$channel' &&
              c['control'] == 'pwm-user' &&
              c['locked'] != true &&
              c['hidden'] != true,
        )) {
      throw const Rejected('PWM target is not currently writable.');
    }
    final write = ++_pwmWriteSequence;
    _activePwmWrite = write;
    ++_pwmRevision; // Retire every read issued before this mutation.
    try {
      final value = object(
        await peer.rpc('controller.pwm.set', {
          'channel': channel,
          'percent': percent,
        }),
      );
      if (epoch != generation ||
          !identical(peer, transport) ||
          targetStamp != expectedTarget ||
          _activePwmWrite != write ||
          !boardReady) {
        throw const Uncertain();
      }
      final channels = objects(value['channels']);
      final matches = channels.where((c) => c['channel'] == channel).toList();
      final actual = matches.length == 1
          ? matches.single['logical_percent']
          : null;
      if (value['available'] != true ||
          !_validPwmReadback(value) ||
          actual is! num ||
          !actual.isFinite ||
          actual < 0 ||
          actual > 100) {
        throw const Uncertain(); // Ack without supported readback is not applied proof.
      }
      pwmValues = value;
      pwmUpdated = DateTime.now();
      _pwmAvailable = true;
      notifyListeners();
      return actual.toDouble();
    } finally {
      if (_activePwmWrite == write) _activePwmWrite = null;
      if (epoch == generation) {
        _schedulePwmRefresh(epoch); // One read, never mutation replay.
      }
    }
  }

  void _scheduleRefresh(int epoch) {
    if (_refreshTimer?.isActive == true) return;
    _refreshTimer = Timer(const Duration(milliseconds: 250), () {
      if (epoch == generation) unawaited(refresh());
    });
  }

  bool _loadingOperation = false;
  Future<void> loadOperation() async {
    if (_loadingOperation || transport == null) return;
    _loadingOperation = true;
    final epoch = generation;
    try {
      final value = object(await transport!.rpc('controller.update.status'));
      if (epoch == generation) {
        operation = value;
        notifyListeners();
      }
    } catch (_) {
    } finally {
      _loadingOperation = false;
    }
  }

  Future<void> loadArtifacts() async {
    final epoch = generation, peer = transport;
    if (peer == null) return;
    final value = object(await peer.rpc('controller.artifact.list'));
    if (epoch == generation) {
      artifacts = objects(value['artifacts']);
      notifyListeners();
    }
  }

  void _lost(int epoch) {
    if (epoch != generation || !enabled) return;
    online = false;
    subscribed = false;
    notifyListeners();
    if (_retry?.isActive == true) return;
    _retry = Timer(
      Duration(
        milliseconds: (500 * (1 << _attempt.clamp(0, 5))).clamp(500, 12000),
      ),
      () {
        _attempt++;
        unawaited(_open(epoch));
      },
    );
  }

  void disconnect() {
    generation++;
    enabled = false;
    online = false;
    subscribed = false;
    _retry?.cancel();
    _freshness?.cancel();
    _refreshTimer?.cancel();
    _pwmTimer?.cancel();
    _socket?.close();
    _socket = null;
    transport?.close();
    transport = null;
    state = {};
    peripherals = {};
    instances = [];
    artifacts = [];
    received = null;
    operation = {};
    pwmValues = {};
    pwmUpdated = null;
    _pwmAvailable = null;
    _pwmRevision++;
    _activePwmWrite = null;
    _panelOwner = '';
    _lastPanelAttempt = null;
    events = [];
    notifyListeners();
  }

  @override
  void dispose() {
    disconnect();
    super.dispose();
  }
}
