import 'dart:async';
import 'dart:convert';
import 'dart:io';

import 'package:flutter/foundation.dart';
import 'package:flutter/services.dart';

import 'transport.dart' show Json, object, endpoint;

/// One self-package transfer, shared by UI and protected HTTP/WS/TCP/Intent RPC.
/// Installed APK export is read-only; receiving/installing never auto-retries.
class AppUpgrade extends ChangeNotifier {
  static const channel = MethodChannel('ir.digitalogic.pccontroller/upgrade');
  static const maximum = 128 * 1024 * 1024, chunkSize = 16384;
  static const methods = {
    'app.package.info',
    'app.package.read',
    'app.upgrade.begin',
    'app.upgrade.write',
    'app.upgrade.finish',
    'app.upgrade.abort',
    'app.upgrade.status',
    'app.upgrade.install',
    'app.upgrade.permission',
    'app.upgrade.pull',
    'app.upgrade.push',
  };
  Json state = {'state': 'idle'};
  bool pulling = false;
  Timer? _changed;
  void _notify() {
    if (state['state'] == 'receiving' || state['state'] == 'transmitting') {
      _changed ??= Timer(const Duration(milliseconds: 80), () {
        _changed = null;
        notifyListeners();
      });
    } else {
      _changed?.cancel();
      _changed = null;
      notifyListeners();
    }
  }

  AppUpgrade() {
    channel.setMethodCallHandler((call) async {
      if (call.method == 'installStatus') {
        state = {'state': 'android_receipt', ...object(call.arguments)};
        notifyListeners();
      }
    });
  }
  Future<Json> rpc(String method, Json params) async {
    if (!methods.contains(method)) {
      throw const FormatException('Unknown package method');
    }
    if (method == 'app.upgrade.pull') {
      return pull('${params['endpoint'] ?? ''}');
    }
    if (method == 'app.upgrade.push') {
      return push('${params['endpoint'] ?? ''}');
    }
    final operation = method.split('.').last;
    final args = Map<String, dynamic>.of(params);
    if (operation == 'write') {
      final encoded = args.remove('data');
      if (encoded is! String || encoded.length > 21848) {
        throw const FormatException('Invalid APK chunk');
      }
      final bytes = base64Decode(encoded);
      if (bytes.isEmpty || bytes.length > chunkSize) {
        throw const FormatException('APK chunk exceeds 16 KiB');
      }
      args['bytes'] = bytes;
    }
    final result = object(await channel.invokeMethod<dynamic>(operation, args));
    if (operation == 'status' &&
        result['state'] == 'idle' &&
        result['install_status'] is String &&
        (result['install_status'] as String).isNotEmpty) {
      try {
        final receipt = object(jsonDecode(result['install_status']));
        if (receipt['status'] is int) {
          result.clear();
          result.addAll({'state': 'android_receipt', ...receipt});
        }
      } on FormatException {
        /* A malformed receipt is not installation proof. */
      }
    }
    if (operation == 'read') {
      final bytes = result.remove('bytes');
      if (bytes is! Uint8List || bytes.length > chunkSize) {
        throw const FormatException('Invalid APK read response');
      }
      result['data'] = base64Encode(bytes);
    } else if (method.startsWith('app.upgrade.')) {
      state = result;
      _notify();
    }
    return result;
  }

  Future<Json> pull(String address) async {
    if (pulling || state['transfer_id'] != null) {
      throw StateError('Finish the current APK transfer');
    }
    final origin = peerOrigin(address);
    pulling = true;
    final client = HttpClient()
      ..findProxy = ((_) => 'DIRECT')
      ..connectionTimeout = const Duration(seconds: 2);
    String? transfer;
    try {
      // Preflight before 0%: source identity, manifest, version, signer, space.
      state = {'state': 'verifying'};
      notifyListeners();
      final health = await boundedPeerGet(client, origin.resolve('/healthz'));
      if (health['status'] != 'ok' ||
          health['service'] != 'controller-mobile') {
        throw const FormatException('Peer is not a controller app');
      }
      final remote = await peerRpc(client, origin, 'app.package.info', {});
      final local = await rpc('app.package.info', {});
      final size = remote['bytes'];
      if (size is! int ||
          size < 1 ||
          size > maximum ||
          remote['package'] != local['package'] ||
          remote['version_code'] is! int ||
          (remote['version_code'] as int) <= (local['version_code'] as int) ||
          jsonEncode(remote['signers']) != jsonEncode(local['signers']) ||
          !RegExp(r'^[a-f0-9]{64}$').hasMatch('${remote['sha256']}')) {
        throw const FormatException(
          'Peer APK is not a newer compatible signed package',
        );
      }
      final started = await rpc('app.upgrade.begin', {
        'bytes': size,
        'sha256': remote['sha256'],
      });
      transfer = started['transfer_id'] as String;
      var offset = 0;
      while (offset < size) {
        final packet = await peerRpc(client, origin, 'app.package.read', {
          'offset': offset,
          'length': chunkSize,
        });
        final encoded = packet['data'];
        if (packet['offset'] != offset ||
            encoded is! String ||
            encoded.length > 21848) {
          throw const FormatException('Peer APK range changed');
        }
        final bytes = base64Decode(encoded);
        if (bytes.isEmpty ||
            bytes.length > chunkSize ||
            offset + bytes.length > size) {
          throw const FormatException('Peer APK size changed');
        }
        final result = await rpc('app.upgrade.write', {
          'transfer_id': transfer,
          'offset': offset,
          'data': encoded,
        });
        offset += bytes.length;
        if (result['received'] != offset) {
          throw const FormatException(
            'Uncertain APK chunk receipt; not replayed',
          );
        }
      }
      final result = await rpc('app.upgrade.finish', {'transfer_id': transfer});
      transfer = null; // Keep verified staging until explicit Install or Abort.
      return result;
    } catch (error) {
      if (transfer != null) {
        try {
          await rpc('app.upgrade.abort', {'transfer_id': transfer});
        } on Object {
          /* Never replay installation. */
        }
      }
      state = {'state': 'failed', 'error': '$error'};
      rethrow;
    } finally {
      pulling = false;
      client.close(force: true);
      notifyListeners();
    }
  }

  /// Peer approval stages bytes only; sender cannot invoke the installer.
  Future<Json> push(String address) async {
    if (pulling || state['transfer_id'] != null) {
      throw StateError('Finish the current APK transfer');
    }
    final origin = peerOrigin(address);
    final client = HttpClient()
      ..findProxy = ((_) => 'DIRECT')
      ..connectionTimeout = const Duration(seconds: 2);
    pulling = true;
    String? transfer, capability;
    try {
      state = {'state': 'offering'};
      notifyListeners();
      final health = await boundedPeerGet(client, origin.resolve('/healthz'));
      if (health['status'] != 'ok' ||
          health['service'] != 'controller-mobile') {
        throw const FormatException('Peer is not a controller app');
      }
      final manifest = await rpc('app.package.info', {});
      final receipt = await peerRpc(
        client,
        origin,
        'app.upgrade.offer',
        manifest,
      );
      transfer = receipt['transfer_id'] as String;
      capability = receipt['capability'] as String;
      if (capability.length < 32 ||
          capability.length > 128 ||
          receipt['bytes'] != manifest['bytes'] ||
          receipt['sha256'] != manifest['sha256']) {
        throw const FormatException('Invalid peer transfer receipt');
      }
      final size = manifest['bytes'] as int;
      var offset = 0;
      while (offset < size) {
        final packet = await rpc('app.package.read', {
          'offset': offset,
          'length': chunkSize,
        });
        final data = packet['data'] as String;
        final bytes = base64Decode(data);
        if (packet['offset'] != offset ||
            bytes.isEmpty ||
            bytes.length > chunkSize ||
            offset + bytes.length > size) {
          throw const FormatException('Installed APK changed');
        }
        final result = await peerRpc(client, origin, 'app.upgrade.write', {
          'transfer_id': transfer,
          'offset': offset,
          'data': data,
        }, capability: capability);
        offset += bytes.length;
        if (result['received'] != offset) {
          throw const FormatException(
            'Uncertain peer receipt; upload not replayed',
          );
        }
        state = {
          'state': 'transmitting',
          'bytes': size,
          'sent': offset,
          'endpoint': address,
        };
        _notify();
      }
      final result = await peerRpc(client, origin, 'app.upgrade.finish', {
        'transfer_id': transfer,
      }, capability: capability);
      transfer = null;
      state = {
        'state': 'delivered',
        'endpoint': address,
        'version_code': manifest['version_code'],
      };
      notifyListeners();
      return result;
    } catch (error) {
      if (transfer != null && capability != null) {
        try {
          await peerRpc(client, origin, 'app.upgrade.abort', {
            'transfer_id': transfer,
          }, capability: capability);
        } on Object {
          /* No blind replay. */
        }
      }
      state = {'state': 'failed', 'error': '$error'};
      rethrow;
    } finally {
      pulling = false;
      client.close(force: true);
      notifyListeners();
    }
  }

  @override
  void dispose() {
    _changed?.cancel();
    channel.setMethodCallHandler(null);
    super.dispose();
  }
}

Uri peerOrigin(String value) {
  final origin = endpoint(value);
  final ip = InternetAddress.tryParse(origin.host);
  if (ip == null ||
      ip.type != InternetAddressType.IPv4 ||
      ip.isMulticast ||
      ip.isLoopback ||
      ip.address == '0.0.0.0' ||
      ip.address == '255.255.255.255') {
    throw const FormatException(
      'Peer updates require a verified literal LAN origin',
    );
  }
  return origin;
}

Future<Json> boundedPeerGet(HttpClient client, Uri uri) async {
  final request = await client.getUrl(uri);
  request.followRedirects = false;
  final response = await request.close().timeout(const Duration(seconds: 3));
  return readPeerJson(response);
}

Future<Json> readPeerJson(HttpClientResponse response) async {
  if (response.statusCode != 200) {
    throw HttpException(
      'Peer rejected package request (${response.statusCode})',
    );
  }
  final bytes = <int>[];
  await for (final chunk in response.timeout(const Duration(seconds: 3))) {
    if (bytes.length + chunk.length > 32768) {
      throw const FormatException('Peer response exceeds 32 KiB');
    }
    bytes.addAll(chunk);
  }
  return object(jsonDecode(utf8.decode(bytes)));
}

Future<Json> peerRpc(
  HttpClient client,
  Uri origin,
  String method,
  Json params, {
  String? capability,
}) async {
  final request = await client.postUrl(origin.resolve('/rpc'));
  request.followRedirects = false;
  request.headers.contentType = ContentType.json;
  if (capability != null) {
    request.headers.set('authorization', 'Bearer $capability');
  }
  request.write(
    jsonEncode({'jsonrpc': '2.0', 'id': 1, 'method': method, 'params': params}),
  );
  final result = await readPeerJson(
    await request.close().timeout(const Duration(seconds: 35)),
  );
  if (result['id'] != 1 ||
      result.containsKey('error') ||
      result['result'] is! Map) {
    throw const FormatException('Peer rejected package RPC');
  }
  return object(result['result']);
}
