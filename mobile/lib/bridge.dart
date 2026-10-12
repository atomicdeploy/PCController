import 'dart:async';
import 'dart:convert';
import 'dart:io';

import 'package:flutter/foundation.dart';
import 'package:mqtt_client/mqtt_client.dart';
import 'package:mqtt_client/mqtt_server_client.dart';

import 'api.dart';
import 'device_registry.dart';
import 'firmware.dart';
import 'models.dart';

/// Shared admission for MQTT, outbound WebSocket and Android loopback clients.
/// IDs are consumed before dispatch; lost acknowledgments never trigger replay.
class BridgeAdmission {
  BridgeAdmission(this.session);
  final String session;
  final seen = <String>{};
  void admit(
    Map<String, dynamic> value,
    DeviceStatus status, {
    bool retained = false,
  }) {
    final now = DateTime.now().millisecondsSinceEpoch;
    final id = value['id'], expires = value['expires_at_ms'];
    if (retained ||
        value['bridge_session'] != session ||
        id is! String ||
        !RegExp(r'^[A-Za-z0-9_-]{1,64}$').hasMatch(id) ||
        expires is! int ||
        expires <= now ||
        expires > now + 60000 ||
        value['board_id'] is! String ||
        value['boot_id'] is! String ||
        (status.json['board_id'] as String? ?? '').isEmpty ||
        (status.json['boot_id'] as String? ?? '').isEmpty ||
        value['board_id'] != status.json['board_id'] ||
        value['boot_id'] != status.json['boot_id']) {
      throw const FormatException(
        'Rejected retained, expired or wrong-session/peer request',
      );
    }
    if (seen.length >= 1024 || !seen.add(id)) {
      throw const FormatException(
        'Duplicate request or session budget reached; nothing replayed',
      );
    }
  }
}

class BridgeOptions {
  BridgeOptions({
    this.broker = '',
    this.topic = 'pakar/bridge',
    this.user = '',
    this.password = '',
    this.remoteWebSocket = '',
    this.remoteToken = '',
    this.local = true,
    this.commands = false,
    this.firmwareStage = false,
    this.telemetrySeconds = 5,
    this.qos = 0,
  });
  final String broker, topic, user, password, remoteWebSocket, remoteToken;
  final bool local, commands, firmwareStage;
  final int telemetrySeconds, qos;
  void validate() {
    if (telemetrySeconds < 1 ||
        telemetrySeconds > 3600 ||
        qos < 0 ||
        qos > 1 ||
        topic.isEmpty ||
        topic.length > 128 ||
        RegExp(r'[+#\x00-\x20]').hasMatch(topic)) {
      throw const FormatException('Invalid interval, QoS or exact base topic');
    }
    if (broker.isNotEmpty) {
      final u = Uri.tryParse(broker);
      if (u == null ||
          !['mqtts', 'wss'].contains(u.scheme) ||
          u.host.isEmpty ||
          u.userInfo.isNotEmpty ||
          u.hasFragment ||
          u.hasQuery ||
          (u.scheme == 'mqtts' && u.path.isNotEmpty && u.path != '/')) {
        throw const FormatException(
          'Use mqtts://hostname:8883 or wss://hostname/mqtt; TLS is required',
        );
      }
    }
    if (remoteWebSocket.isNotEmpty) {
      final u = Uri.tryParse(remoteWebSocket);
      if (u == null ||
          u.scheme != 'wss' ||
          u.host.isEmpty ||
          u.userInfo.isNotEmpty ||
          u.hasFragment ||
          u.hasQuery ||
          RegExp(r'[\r\n]').hasMatch(remoteToken)) {
        throw const FormatException(
          'Use a direct wss:// endpoint without credentials/query',
        );
      }
    }
  }
}

/// Foreground-only bridge. Never promises Android background/lockscreen uptime.
/// No remote peer receives the ESP password, Wi-Fi settings or arbitrary status JSON.
class AndroidBridge extends ChangeNotifier {
  AndroidBridge(this.api, this.observed) : owner = api.connectionRevision;
  final PakarApi api;
  final DeviceStatus? Function() observed;
  final int owner;
  BridgeOptions? options;
  BridgeAdmission? admission;
  HttpServer? server;
  MqttServerClient? mqtt;
  WebSocket? remote;
  final localSockets = <WebSocket>{};
  StreamSubscription<dynamic>? mqttEvents, remoteEvents;
  Timer? timer, watchdog;
  String token = '', state = 'Stopped', error = '';
  final incidents = <String, int>{};
  String? peerBoard, peerBoot;
  int received = 0, rejected = 0, sent = 0, epoch = 0;
  bool running = false, busy = false, uncertain = false, closed = false;
  bool subscriptionAcknowledged = false;
  bool get mqttConnected =>
      mqtt?.connectionStatus?.state == MqttConnectionState.connected;
  bool get webSocketConnected => remote?.readyState == WebSocket.open;
  DeviceStatus current() {
    final value = observed();
    if (!running ||
        closed ||
        owner != api.connectionRevision ||
        value == null ||
        !value.peer.known ||
        value.json['board_id'] != peerBoard ||
        value.json['boot_id'] != peerBoot) {
      throw const StaleConnection();
    }
    return value;
  }

  Future<void> start(BridgeOptions selected) async {
    selected.validate();
    await stop();
    if (closed) throw const StaleConnection();
    final value = observed();
    if (owner != api.connectionRevision ||
        value == null ||
        !value.peer.known ||
        value.json['board_id'] is! String ||
        !RegExp(r'^[!-~]{1,80}$').hasMatch(value.json['board_id']) ||
        value.json['boot_id'] is! String ||
        !RegExp(r'^[!-~]{1,80}$').hasMatch(value.json['boot_id'])) {
      throw const FormatException(
        'Connect to a live peer with board/boot identity before starting a bridge',
      );
    }
    peerBoard = value.json['board_id'];
    peerBoot = value.json['boot_id'];
    options = selected;
    admission = BridgeAdmission(randomIdentifier());
    token = randomIdentifier() + randomIdentifier();
    final generation = ++epoch;
    running = true;
    uncertain = false;
    state = 'Starting';
    error = '';
    incidents.clear();
    notifyListeners();
    try {
      if (selected.local) {
        final active = await HttpServer.bind(InternetAddress.loopbackIPv4, 0);
        if (generation != epoch || closed) {
          await active.close(force: true);
          throw const StaleConnection();
        }
        server = active;
        active.listen((r) => unawaited(_local(r, generation)));
      }
      if (selected.broker.isNotEmpty) {
        final u = Uri.parse(selected.broker),
            ws = Uri.parse(selected.broker).scheme == 'wss';
        final active = MqttServerClient.withPort(
          ws ? selected.broker : u.host,
          'pakar-${admission!.session.substring(0, 16)}',
          u.hasPort
              ? u.port
              : ws
              ? 443
              : 8883,
        );
        mqtt = active;
        active.useWebSocket = ws;
        active.secure = !ws;
        active.logging(on: false);
        active.setProtocolV311();
        active.keepAlivePeriod = 20;
        active.autoReconnect =
            false; // No offline command backlog/session resumption.
        active.connectTimeoutPeriod = 8000;
        active.connectionMessage = MqttConnectMessage()
            .withClientIdentifier(
              'pakar-${admission!.session.substring(0, 16)}',
            )
            .withWillTopic('${selected.topic}/availability')
            .withWillMessage('{"online":false}')
            .withWillQos(MqttQos.atMostOnce)
            .withWillRetain()
            .startClean();
        active.onSubscribed = (topic) {
          if (running &&
              !closed &&
              generation == epoch &&
              topic == '${selected.topic}/request') {
            subscriptionAcknowledged = true;
            notifyListeners();
          }
        };
        active.onDisconnected = () {
          if (running && !closed) {
            subscriptionAcknowledged = false;
            _failure('MQTT disconnected; reconnect explicitly');
          }
        };
        active.onSubscribeFail = (_) {
          if (running && !closed) {
            _failure('MQTT command subscription rejected');
          }
        };
        await active.connect(
          selected.user.isEmpty ? null : selected.user,
          selected.password.isEmpty ? null : selected.password,
        );
        if (generation != epoch || !mqttConnected) {
          throw const StaleConnection();
        }
        current();
        mqttEvents = active.updates!.listen((messages) {
          for (final m in messages) {
            if (m.topic != '${selected.topic}/request' ||
                m.payload is! MqttPublishMessage) {
              continue;
            }
            final publish = m.payload as MqttPublishMessage;
            if (publish.payload.message.length > 8192) {
              rejected++;
              continue;
            }
            unawaited(
              _incoming(
                utf8.decode(publish.payload.message, allowMalformed: true),
                retained: publish.header?.retain == true,
                generation: generation,
              ).then((v) {
                if (generation == epoch && running && !closed) {
                  _publish('${selected.topic}/reply', v);
                }
              }),
            );
          }
        });
        active.subscribe('${selected.topic}/request', MqttQos.atMostOnce);
      }
      if (selected.remoteWebSocket.isNotEmpty) {
        final active = await WebSocket.connect(
          selected.remoteWebSocket,
          headers: selected.remoteToken.isEmpty
              ? {}
              : {'Authorization': 'Bearer ${selected.remoteToken}'},
        ).timeout(const Duration(seconds: 8));
        if (generation != epoch || closed) {
          await active.close();
          throw const StaleConnection();
        }
        remote = active;
        active.pingInterval = const Duration(seconds: 20);
        current();
        remoteEvents = active.listen(
          (message) {
            if (message is String && message.length <= 8192) {
              unawaited(
                _incoming(message, generation: generation).then((v) {
                  if (active.readyState == WebSocket.open) {
                    active.add(jsonEncode(v));
                  }
                }),
              );
            }
          },
          onError: (Object _) => _failure('Remote WebSocket disconnected'),
          onDone: () {
            if (running && !closed) _failure('Remote WebSocket disconnected');
          },
        );
      }
      if (generation != epoch) throw const StaleConnection();
      current();
      state = 'Running · foreground only';
      _publish('${selected.topic}/availability', {
        'online': true,
        'bridge_session': admission!.session,
      }, retain: true);
      notifyListeners();
      telemetry();
      timer = Timer.periodic(
        Duration(seconds: selected.telemetrySeconds),
        (_) => telemetry(),
      );
      // Freshness must not depend on a user-selected long telemetry interval.
      watchdog = Timer.periodic(const Duration(seconds: 1), (_) {
        if (!running || closed) return;
        try {
          current();
        } on Object {
          unawaited(stop());
          _failure(
            'Peer expired/changed; bridge stopped and requests discarded',
          );
        }
      });
    } on Object {
      await stop();
      _failure('Bridge startup failed; no commands retried');
      rethrow;
    }
  }

  void _failure(String reason) {
    if (closed) return;
    incidents[reason] = (incidents[reason] ?? 0) + 1;
    error = reason;
    notifyListeners();
  }

  Map<String, dynamic> snapshot() {
    final s = current();
    return {
      'type': 'telemetry',
      'bridge_session': admission!.session,
      'observed_at_ms': DateTime.now().millisecondsSinceEpoch,
      'client': {
        'application': 'PakarController',
        'platform': Platform.operatingSystem,
        'bridge': 'foreground',
        'commands_enabled': options!.commands,
        'firmware_stage_enabled': options!.firmwareStage,
      },
      'board': {
        for (final key in [
          'board_id',
          'boot_id',
          'hostname',
          'version',
          'avr_online',
          'tm1637_ack',
          'mode',
          'effect',
          'brightness',
          'winner',
          'active_inputs',
          'active_outputs',
          'ntp_synced',
          'station_rssi',
        ])
          if (s.json.containsKey(key)) key: s.json[key],
      },
    };
  }

  void telemetry() {
    if (!running || closed) return;
    try {
      final value = snapshot();
      _publish('${options!.topic}/telemetry', value);
      if (webSocketConnected) remote!.add(jsonEncode(value));
      for (final socket in localSockets) {
        if (socket.readyState == WebSocket.open) socket.add(jsonEncode(value));
      }
      sent++;
      notifyListeners();
    } on Object {
      unawaited(stop());
      _failure('Peer expired/changed; bridge stopped and requests discarded');
    }
  }

  void _publish(
    String topic,
    Map<String, dynamic> value, {
    bool retain = false,
  }) {
    if (!mqttConnected) return;
    try {
      final builder = MqttClientPayloadBuilder()..addString(jsonEncode(value));
      mqtt!.publishMessage(
        topic,
        options!.qos == 1 ? MqttQos.atLeastOnce : MqttQos.atMostOnce,
        builder.payload!,
        retain: retain,
      );
    } on Object {
      _failure('MQTT publish failed; not queued or replayed');
    }
  }

  Future<Map<String, dynamic>> _incoming(
    String raw, {
    bool retained = false,
    required int generation,
  }) async {
    received++;
    String? id;
    try {
      if (!running ||
          closed ||
          generation != epoch ||
          raw.length > 8192 ||
          busy ||
          uncertain) {
        throw const StaleConnection();
      }
      final decoded = jsonDecode(raw);
      if (decoded is! Map<String, dynamic>) {
        throw const FormatException('Invalid request');
      }
      id = decoded['id'] is String ? decoded['id'] : null;
      final s = current();
      admission!.admit(decoded, s, retained: retained);
      busy = true;
      try {
        if (decoded['type'] == 'command' && options!.commands) {
          final path = decoded['path'], rawValues = decoded['values'];
          if (path is! String ||
              rawValues is! Map ||
              rawValues.length > 16 ||
              rawValues.keys.any((k) => k is! String) ||
              rawValues.values.any((v) => v is! String || v.length > 512)) {
            throw const FormatException('Invalid command');
          }
          final values = Map<String, String>.from(rawValues);
          if (!s.peer.allows(path, values)) {
            throw const FormatException('Unacknowledged capability or path');
          }
          await api.post(path, values);
          current();
          return {
            'id': id,
            'accepted': true,
            'outcome': 'HTTP acknowledged; peripheral completion is separate',
          };
        }
        if (decoded['type'] == 'firmware-stage' && options!.firmwareStage) {
          if (decoded['url'] is! String ||
              decoded['sha256'] is! String ||
              decoded['targets'] is! int) {
            throw const FormatException('Invalid pinned firmware request');
          }
          final controller = FirmwareController(
            api,
            authorized: (f) => current().peer.has(f),
          );
          try {
            final job = await controller.stage(
              decoded['url'],
              decoded['sha256'],
              decoded['targets'],
              '',
            );
            current();
            return {
              'id': id,
              'accepted': true,
              'job': job.json,
              'outcome': 'Staging only. Apply requires the firmware page confirmation.',
            };
          } finally {
            controller.dispose();
          }
        }
        throw const FormatException(
          'Request type disabled or unsupported; no arbitrary proxy',
        );
      } finally {
        busy = false;
      }
    } on OperationUncertain {
      uncertain = true;
      _failure(
        'Command outcome uncertain. Inspect board before restarting bridge.',
      );
      return {
        'id': id,
        'accepted': false,
        'outcome': 'uncertain',
        'replayed': false,
      };
    } on Object {
      rejected++;
      if (!closed) notifyListeners();
      return {
        'id': id != null && id.length <= 64 ? id : null,
        'accepted': false,
        'outcome': 'Rejected by session/identity/deadline/capability policy; nothing replayed',
      };
    }
  }

  Future<void> _local(HttpRequest r, int generation) async {
    try {
      if (generation != epoch ||
          !running ||
          r.headers.value('authorization') != 'Bearer $token' ||
          r.headers.value('origin') != null) {
        r.response.statusCode = 403;
        await r.response.close();
        return;
      }
      if (r.method == 'GET' &&
          r.uri.path == '/ws' &&
          WebSocketTransformer.isUpgradeRequest(r)) {
        if (localSockets.length >= 8) {
          r.response.statusCode = 429;
          await r.response.close();
          return;
        }
        final s = await WebSocketTransformer.upgrade(r);
        if (generation != epoch || !running) {
          await s.close();
          return;
        }
        localSockets.add(s);
        s.pingInterval = const Duration(seconds: 20);
        s.listen(
          (m) {
            if (m is String && m.length <= 8192) {
              unawaited(
                _incoming(m, generation: generation).then((v) {
                  if (s.readyState == WebSocket.open) s.add(jsonEncode(v));
                }),
              );
            }
          },
          onDone: () => localSockets.remove(s),
          onError: (Object _) => localSockets.remove(s),
        );
        s.add(jsonEncode(snapshot()));
        return;
      }
      if (r.method == 'GET' &&
          r.uri.path == '/api/firmware/download' &&
          options!.firmwareStage) {
        final target = r.uri.queryParameters['target'];
        if (!{'esp', 'avr', 'eeprom'}.contains(target)) {
          throw const FormatException('Explicit firmware target required');
        }
        current();
        final controller = FirmwareController(
          api,
          authorized: (f) => current().peer.has(f),
        );
        try {
          final bytes = target == 'esp'
              ? await controller.espBytes()
              : await controller.backupBytes(target == 'eeprom');
          current();
          if (generation != epoch || !running) throw const StaleConnection();
          r.response.headers.contentType = ContentType.binary;
          r.response.headers.set('Cache-Control', 'no-store');
          r.response.contentLength = bytes.length;
          r.response.add(bytes);
          await r.response.close();
          return;
        } finally {
          controller.dispose();
        }
      }
      r.response.headers.contentType = ContentType.json;
      r.response.headers.set('Cache-Control', 'no-store');
      Map<String, dynamic> value;
      if (r.method == 'GET' && r.uri.path == '/api/status') {
        value = snapshot();
      } else if (r.method == 'POST' && r.uri.path == '/api/request') {
        final elapsed = Stopwatch()..start();
        final chunks = <int>[];
        final reader = StreamIterator(r);
        try {
          while (true) {
            final remaining = const Duration(seconds: 8) - elapsed.elapsed;
            if (remaining <= Duration.zero) {
              throw TimeoutException('Request deadline');
            }
            if (!await reader.moveNext().timeout(remaining)) break;
            if (chunks.length + reader.current.length > 8192) {
              throw const FormatException('Request limit');
            }
            chunks.addAll(reader.current);
          }
        } finally {
          await reader.cancel();
        }
        if (generation != epoch || !running) {
          throw const StaleConnection();
        }
        value = await _incoming(utf8.decode(chunks), generation: generation);
      } else {
        r.response.statusCode = 404;
        value = {'accepted': false};
      }
      r.response.write(jsonEncode(value));
      await r.response.close();
    } on Object {
      r.response.statusCode = 400;
      await r.response.close();
    }
  }

  Future<void> stop() async {
    if (running && options != null) {
      _publish('${options!.topic}/availability', {
        'online': false,
      }, retain: true);
    }
    ++epoch;
    running = false;
    timer?.cancel();
    watchdog?.cancel();
    watchdog = null;
    timer = null;
    final m = mqtt, w = remote, h = server;
    mqtt = null;
    subscriptionAcknowledged = false;
    remote = null;
    server = null;
    m?.disconnect();
    await mqttEvents?.cancel();
    await remoteEvents?.cancel();
    mqttEvents = remoteEvents = null;
    for (final s in localSockets.toList()) {
      await s.close();
    }
    localSockets.clear();
    await w?.close();
    await h?.close(force: true);
    token = '';
    state = 'Stopped';
    if (!closed) notifyListeners();
  }

  @override
  void dispose() {
    closed = true;
    unawaited(stop());
    super.dispose();
  }
}
