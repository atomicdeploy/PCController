import 'dart:async';
import 'dart:convert';
import 'dart:io';
import 'dart:typed_data';

import 'package:http/http.dart' as http;
import 'package:crypto/crypto.dart';
import 'package:web_socket_channel/io.dart';
import 'package:web_socket_channel/web_socket_channel.dart';

import 'models.dart';

class PakarApi {
  PakarApi({
    String baseUrl = 'http://ledboard.local',
    String user = 'admin',
    String password = 'pakarcontroller',
    http.Client? client,
    this.discovery,
  }) : _client = client ?? http.Client(),
       _baseUrl = normalizeEndpoint(baseUrl),
       // Preserve public argument names; private setters own revisions.
       // ignore: prefer_initializing_formals
       _user = user,
       // ignore: prefer_initializing_formals
       _password = password;

  String _baseUrl, _user, _password;
  int _connectionRevision = 0;
  int get connectionRevision => _connectionRevision;
  void Function()? onPeerChanged;
  void Function()? onTransportRetired;
  void Function(Map<String, dynamic>)? onPeripheralEvent;
  bool _identityRequired = false;
  String get baseUrl => _baseUrl;
  set baseUrl(String value) {
    final normalized = normalizeEndpoint(value);
    if (_baseUrl != normalized) {
      _baseUrl = normalized;
      _identityRequired = false;
      resetTransport();
    }
  }

  String get user => _user;
  set user(String value) {
    if (_user != value) {
      _user = value;
      resetTransport();
    }
  }

  String get password => _password;
  set password(String value) {
    if (_password != value) {
      _password = value;
      resetTransport();
    }
  }

  final http.Client _client;
  final Future<Set<String>> Function()? discovery;
  WebSocketChannel? _channel;
  String? _discoveredBaseUrl;
  String? _transportOwner;
  String? _verifiedEndpoint;
  String? _verifiedBoard, _verifiedBoot;

  Map<String, String> get _headers => {
    'authorization': 'Basic ${base64Encode(utf8.encode('$user:$password'))}',
  };
  Map<String, String> get _writeHeaders => {
    ..._headers,
    if (_verifiedBoard != null && _verifiedBoot != null) ...{
      'X-Pakar-Board': _verifiedBoard!,
      'X-Pakar-Boot': _verifiedBoot!,
    },
  };

  String get transportUrl =>
      _transportOwner == baseUrl ? _discoveredBaseUrl ?? baseUrl : baseUrl;

  static String normalizeEndpoint(String value) {
    final text = value.trim();
    final uri = Uri.tryParse(text.contains('://') ? text : 'http://$text');
    if (uri == null ||
        !['http', 'https'].contains(uri.scheme) ||
        uri.host.isEmpty ||
        uri.userInfo.isNotEmpty ||
        uri.hasQuery ||
        uri.hasFragment ||
        (uri.path.isNotEmpty && uri.path != '/')) {
      throw const FormatException(
        'Enter an HTTP(S) device hostname or origin without credentials or a path',
      );
    }
    return uri.replace(path: '').toString();
  }

  Uri _uri(String base, String path, [Map<String, String>? query]) =>
      Uri.parse('${base.replaceFirst(RegExp(r'/$'), '')}$path')
          .replace(queryParameters: query);

  Future<Map<String, dynamic>> _get(String path) async {
    final revision = connectionRevision, headers = _headers;
    final response = await _withDiscovery(
      (base) => _client
          .get(_uri(base, path), headers: headers)
          .timeout(const Duration(seconds: 6)),
    );
    if (revision != connectionRevision) throw const StaleConnection();
    if (response.statusCode ~/ 100 != 2) throw Exception(response.body);
    return jsonDecode(response.body) as Map<String, dynamic>;
  }

  Future<Map<String, dynamic>> getJson(String path) => _get(path);

  /// Bounded single-dispatch maintenance transport. Even ISP probe uses this
  /// path: its GET resets AVR, so it must not be retried through discovery.
  Future<Uint8List> maintenanceBytes(
    String path, {
    bool post = false,
    bool mayMutate = false,
    Map<String, String> form = const {},
    Uint8List? upload,
    String? uploadField,
    int maximum = 8192,
    bool verifySketch = false,
    Duration timeout = const Duration(seconds: 180),
    required bool Function() authorize,
  }) async {
    final revision = connectionRevision;
    if (_verifiedEndpoint != baseUrl) await status();
    if (revision != connectionRevision || !authorize()) {
      throw const StaleConnection();
    }
    final uri = _uri(transportUrl, path);
    final http.BaseRequest request;
    if (upload != null) {
      if (!post || !{'image', 'firmware'}.contains(uploadField)) {
        throw const FormatException('Invalid firmware upload contract');
      }
      request = http.MultipartRequest('POST', uri)
        ..files.add(
          http.MultipartFile.fromBytes(
            uploadField!,
            upload,
            filename: uploadField == 'image' ? 'avr.bin' : 'esp8266.bin',
          ),
        );
    } else {
      request = http.Request(post ? 'POST' : 'GET', uri)..bodyFields = form;
    }
    request.headers.addAll(_writeHeaders);
    request.followRedirects = false; // No credential forwarding or POST replay.
    final mutation = post || mayMutate;
    final elapsed = Stopwatch()..start();
    try {
      final response = await _client.send(request).timeout(timeout);
      if (revision != connectionRevision) {
        await response.stream.listen((_) {}).cancel();
        throw const StaleConnection();
      }
      if (response.statusCode ~/ 100 != 2 ||
          (!post && response.statusCode != 200) ||
          (response.contentLength ?? 0) > maximum) {
        await response.stream.listen((_) {}).cancel();
        throw const FormatException(
          'Maintenance rejected or response exceeds its limit; nothing replayed',
        );
      }
      final bytes = BytesBuilder(copy: false);
      final reader = StreamIterator(response.stream);
      try {
        while (true) {
          final remaining = timeout - elapsed.elapsed;
          if (remaining <= Duration.zero) {
            throw TimeoutException('Maintenance deadline');
          }
          if (!await reader.moveNext().timeout(remaining)) break;
          if (revision != connectionRevision) throw const StaleConnection();
          final chunk = reader.current;
          if (bytes.length + chunk.length > maximum) {
            throw const FormatException(
              'Maintenance response exceeds its limit; nothing replayed',
            );
          }
          bytes.add(chunk);
        }
      } finally {
        await reader.cancel();
      }
      if (revision != connectionRevision) throw const StaleConnection();
      final result = bytes.takeBytes();
      if (response.contentLength != null &&
          response.contentLength != result.length) {
        throw const FormatException(
          'Truncated firmware transfer; no export created',
        );
      }
      if (verifySketch) {
        final digest = response.headers['x-firmware-md5'];
        if (digest == null ||
            !RegExp(r'^[a-fA-F0-9]{32}$').hasMatch(digest) ||
            md5.convert(result).toString() != digest.toLowerCase()) {
          throw const FormatException(
            'Running firmware transfer digest unavailable or mismatched; no export created',
          );
        }
      }
      return result;
    } on StaleConnection {
      rethrow;
    } on FormatException {
      rethrow;
    } on Exception {
      if (mutation) throw const OperationUncertain();
      throw const FormatException(
        'Maintenance read failed; no operation was replayed',
      );
    }
  }

  Future<Map<String, dynamic>> maintenanceJson(
    String path, {
    bool post = false,
    bool mayMutate = false,
    Map<String, String> form = const {},
    Uint8List? upload,
    String? uploadField,
    required bool Function() authorize,
  }) async {
    final bytes = await maintenanceBytes(
      path,
      post: post,
      mayMutate: mayMutate,
      form: form,
      upload: upload,
      uploadField: uploadField,
      authorize: authorize,
    );
    final value = jsonDecode(utf8.decode(bytes));
    if (value is! Map<String, dynamic>) {
      throw const FormatException(
        'Malformed maintenance acknowledgment; inspect before any new request',
      );
    }
    return value;
  }

  /// Single dispatch, form BODY (never secret-bearing URL parameters), and
  /// caller-owned capability check after any awaited identity verification.
  Future<Map<String, dynamic>> postJson(
    String path,
    Map<String, String> values, {
    bool Function()? authorize,
  }) async {
    final revision = connectionRevision;
    if (_verifiedEndpoint != baseUrl) await status();
    if (revision != connectionRevision || authorize?.call() == false) {
      throw const StaleConnection();
    }
    late final http.Response response;
    try {
      final request = http.Request('POST', _uri(transportUrl, path))
        ..headers.addAll(_writeHeaders)
        ..bodyFields = values
        ..followRedirects = false;
      response = await http.Response.fromStream(await _client.send(request))
          .timeout(const Duration(seconds: 12));
    } on Exception {
      throw const OperationUncertain();
    }
    if (revision != connectionRevision) throw const StaleConnection();
    if (response.statusCode ~/ 100 != 2) {
      // Never echo a possibly secret-bearing server error or request value.
      throw FormatException(
        'Device write rejected (HTTP ${response.statusCode}); not retried',
      );
    }
    final result = jsonDecode(response.body);
    if (result is! Map<String, dynamic>) throw const OperationUncertain();
    return result;
  }

  Future<void> post(
    String path, [
    Map<String, String> values = const {},
  ]) async {
    final revision = connectionRevision;
    // Resolve/verify through a read first; a POST must never be automatically
    // replayed after a timeout or disconnect (the board may have applied it).
    if (_verifiedEndpoint != baseUrl) await status();
    if (revision != connectionRevision) throw const StaleConnection();
    late final http.Response response;
    try {
      final request = http.Request('POST', _uri(transportUrl, path))
        ..headers.addAll(_writeHeaders)
        ..bodyFields = values
        ..followRedirects = false;
      response = await http.Response.fromStream(await _client.send(request))
          .timeout(const Duration(seconds: 12));
    } on Exception {
      throw const OperationUncertain();
    }
    if (revision != connectionRevision) throw const StaleConnection();
    if (response.statusCode ~/ 100 != 2) throw Exception(response.body);
  }

  Future<DeviceStatus> status() async {
    final result = DeviceStatus(await _get('/api/status'));
    final expected = _localLabel();
    if (expected != null && result.json['hostname'] != expected) {
      throw const FormatException(
        'Device identity does not match the requested hostname',
      );
    }
    _acceptIdentity(result.json);
    _verifiedEndpoint = baseUrl;
    return result;
  }

  // Adapt Session's synchronous retirement boundary: retire captured owners
  // before a new board/boot may authorize any old dialog or in-flight request.
  bool _acceptIdentity(Map<String, dynamic> value) {
    final board = value['board_id'], boot = value['boot_id'];
    bool identity(Object? v) =>
        v is String && RegExp(r'^[!-~]{1,80}$').hasMatch(v);
    if ((_identityRequired ||
            value.containsKey('board_id') ||
            value.containsKey('boot_id')) &&
        (!identity(board) || !identity(boot))) {
      if (_verifiedBoard != null) {
        resetTransport();
        onPeerChanged?.call();
      }
      throw const StaleConnection();
    }
    final changed =
        _verifiedBoard != null &&
        (board != _verifiedBoard || boot != _verifiedBoot);
    if (changed) {
      resetTransport();
      onPeerChanged?.call();
    }
    _verifiedBoard = identity(board) && identity(boot) ? board as String : null;
    _verifiedBoot = identity(board) && identity(boot) ? boot as String : null;
    if (_verifiedBoard != null) _identityRequired = true;
    return changed;
  }

  Future<CellMap> mapping() async =>
      CellMap.fromJson(await _get('/api/cells/mapping'));

  Stream<DeviceStatus> liveStatus() async* {
    final revision = connectionRevision;
    if (_verifiedEndpoint != baseUrl) await status();
    if (revision != connectionRevision) throw const StaleConnection();
    await _channel?.sink.close();
    if (revision != connectionRevision) throw const StaleConnection();
    final source = Uri.parse(transportUrl);
    final uri = source.replace(
      scheme: source.scheme == 'https' ? 'wss' : 'ws',
      path: '/ws',
      query: null,
    );
    final active = IOWebSocketChannel.connect(
      uri,
      headers: _headers,
      pingInterval: const Duration(seconds: 20),
    );
    _channel = active;
    await active.ready;
    if (revision != connectionRevision) {
      await active.sink.close();
      return;
    }
    active.sink.add('{"type":"status"}');
    await for (final message in active.stream) {
      if (revision != connectionRevision) return;
      if (message is! String) continue;
      final value = jsonDecode(message);
      if (value is Map<String, dynamic> &&
          value['type'] == 'pc-contract-event') {
        // Events cannot establish identity or refresh an expired status lease.
        if (_verifiedBoard != null &&
            value['board_id'] == _verifiedBoard &&
            value['boot_id'] == _verifiedBoot) {
          onPeripheralEvent?.call(value);
        }
        continue;
      }
      if (value is Map<String, dynamic> && value.containsKey('hostname')) {
        final expected = _localLabel();
        if (expected != null && value['hostname'] != expected) {
          throw const StaleConnection();
        }
        if (_acceptIdentity(value)) {
          return; // Reconnect only reads; never replay.
        }
        yield DeviceStatus(value);
      }
    }
  }

  Future<void> close() async {
    ++_connectionRevision;
    final active = _channel;
    _channel = null;
    _verifiedEndpoint = _transportOwner = _discoveredBaseUrl = null;
    _verifiedBoard = _verifiedBoot = null;
    await active?.sink.close();
    _client.close();
  }

  void resetTransport() {
    ++_connectionRevision;
    _discoveredBaseUrl = _transportOwner = _verifiedEndpoint = null;
    _verifiedBoard = _verifiedBoot = null;
    onTransportRetired?.call();
    final active = _channel;
    _channel = null;
    if (active != null) {
      unawaited(active.sink.close().then<void>((_) {}, onError: (Object _) {}));
    }
  }

  String? _localLabel() {
    final host = Uri.tryParse(baseUrl)?.host.toLowerCase();
    if (host == null || !host.endsWith('.local')) return null;
    final label = host.substring(0, host.length - 6);
    return RegExp(r'^[a-z0-9](?:[a-z0-9-]{0,61}[a-z0-9])?$').hasMatch(label)
        ? label
        : null;
  }

  Future<http.Response> _withDiscovery(
    Future<http.Response> Function(String base) request,
  ) async {
    final revision = connectionRevision, headers = _headers;
    try {
      return await request(transportUrl);
    } on Exception {
      if (revision != connectionRevision) throw const StaleConnection();
      if (_localLabel() == null) rethrow;
    }

    _discoveredBaseUrl = null;
    _transportOwner = baseUrl;
    for (final candidate in await (discovery?.call() ?? _discoverSsdp())) {
      if (revision != connectionRevision) throw const StaleConnection();
      final origin = Uri.tryParse(candidate);
      if (origin == null ||
          !['http', 'https'].contains(origin.scheme) ||
          origin.userInfo.isNotEmpty) {
        continue;
      }
      try {
        final probe = await _client
            .get(_uri(candidate, '/api/status'), headers: headers)
            .timeout(const Duration(seconds: 2));
        if (probe.statusCode ~/ 100 != 2) continue;
        if (revision != connectionRevision) throw const StaleConnection();
        final json = jsonDecode(probe.body);
        if (json is! Map<String, dynamic> ||
            json['hostname'] != _localLabel()) {
          continue;
        }
      } on Object {
        if (revision != connectionRevision) throw const StaleConnection();
        continue;
      }
      _discoveredBaseUrl = candidate;
      // Only the read probe can advance to another candidate. Do not swallow
      // an error from the selected request and silently choose another board.
      return await request(candidate);
    }
    throw SocketException(
      '${Uri.parse(baseUrl).host} did not resolve and no matching authenticated SSDP device replied',
    );
  }

  Future<Set<String>> _discoverSsdp() async {
    final socket = await RawDatagramSocket.bind(InternetAddress.anyIPv4, 0);
    socket.multicastHops = 2;
    final results = <String>{};
    final done = Completer<void>();
    late final StreamSubscription<RawSocketEvent> subscription;
    subscription = socket.listen((event) {
      if (event != RawSocketEvent.read) return;
      final packet = socket.receive();
      if (packet == null) return;
      final text = utf8.decode(packet.data, allowMalformed: true);
      final lines = const LineSplitter().convert(text);
      if (lines.isEmpty ||
          !lines.first.toUpperCase().startsWith('HTTP/1.1 200')) {
        return;
      }
      for (final line in lines.skip(1)) {
        final separator = line.indexOf(':');
        if (separator < 0 ||
            line.substring(0, separator).trim().toLowerCase() != 'location') {
          continue;
        }
        final location = Uri.tryParse(line.substring(separator + 1).trim());
        if (location?.host.isEmpty ?? true) continue;
        results.add(
          location!
              .replace(path: '', query: null, fragment: null)
              .toString()
              .replaceFirst(RegExp(r'/$'), ''),
        );
      }
    });
    const request =
        'M-SEARCH * HTTP/1.1\r\n'
        'HOST: 239.255.255.250:1900\r\n'
        'MAN: "ssdp:discover"\r\n'
        'MX: 1\r\n'
        'ST: ssdp:all\r\n\r\n';
    socket.send(utf8.encode(request), InternetAddress('239.255.255.250'), 1900);
    Timer(const Duration(milliseconds: 1400), done.complete);
    await done.future;
    await subscription.cancel();
    socket.close();
    return results;
  }
}

class StaleConnection implements Exception {
  const StaleConnection();
  @override
  String toString() =>
      'Connection changed; obsolete board replies and unsent commands were discarded.';
}

class OperationUncertain implements Exception {
  const OperationUncertain();
  @override
  String toString() =>
      'Command outcome unknown; it was not retried. Refresh device state before deciding whether to send again.';
}
