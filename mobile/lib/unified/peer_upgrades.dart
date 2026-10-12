import 'dart:async';
import 'dart:convert';
import 'dart:io';
import 'dart:typed_data';

import 'package:crypto/crypto.dart';

import '../discovery.dart';
import 'app_upgrade.dart';
import 'automation.dart' show newAutomationToken, sameSecret;
import 'transport.dart' show Json, object;

typedef UpgradeApproval = Future<bool> Function(Json manifest);

/// Explicit foreground-only APK sharing. Public methods reveal only the APK,
/// not app state, authoring, credentials or board/player commands. An incoming
/// offer must be approved here; its short-lived capability pins one digest and
/// transfer. Installation still requires a separate local action + OS consent.
class PeerUpgrades {
  PeerUpgrades(this.upgrades, {required this.approve});
  final AppUpgrade upgrades;
  final UpgradeApproval approve;
  HttpServer? _server;
  final _sockets = <RawDatagramSocket>[];
  Timer? _announce;
  String? _capability, _transfer;
  DateTime? _expires;
  bool _offering = false;
  int _active = 0, _epoch = 0, _sequence = 0;
  final _instance = newAutomationToken();
  late final _ssdpId = () {
    final hex = sha256
        .convert(utf8.encode(_instance))
        .toString()
        .substring(0, 32);
    return '${hex.substring(0, 8)}-${hex.substring(8, 12)}-${hex.substring(12, 16)}-${hex.substring(16, 20)}-${hex.substring(20)}';
  }();
  final errors = <String, String>{};
  final _addresses = <InternetAddress>[];
  final _responded = <String, DateTime>{};
  bool get running => _server != null;
  int? get port => _server?.port;
  Future<void> start() async {
    if (running) return;
    errors.clear();
    await upgrades.rpc(
      'app.package.info',
      {},
    ); // No unreachable/split APK advertisement.
    final epoch = ++_epoch;
    final server = await HttpServer.bind(InternetAddress.anyIPv4, 18992);
    if (epoch != _epoch) {
      await server.close(force: true);
      return;
    }
    _server = server;
    server.idleTimeout = const Duration(seconds: 5);
    _addresses.clear();
    _addresses.addAll(
      (await NetworkInterface.list(type: InternetAddressType.IPv4))
          .expand((n) => n.addresses)
          .where((ip) => !ip.isLoopback),
    );
    DeviceDiscovery.localEndpoints.addAll(
      _addresses.map((ip) => 'http://${ip.address}:${server.port}'),
    );
    server.listen(
      (request) => unawaited(
        _request(request, epoch).catchError((_) async {
          request.response.statusCode = 400;
          await request.response.close();
        }),
      ),
    );
    // Advertisement failures never turn this API into a board or success proof.
    try {
      await DeviceDiscovery.channel.invokeMethod<void>('advertise', {
        'port': server.port,
      });
    } on Object catch (error) {
      errors['dns-sd'] = '$error';
    }
    for (final mndp in [false, true]) {
      try {
        await _udp(mndp, epoch);
      } on SocketException catch (error) {
        errors[mndp ? 'mndp' : 'ssdp'] =
            '${error.osError?.errorCode ?? 'socket'}';
      }
    }
    _announce = Timer.periodic(const Duration(seconds: 30), (_) => _beacon());
    _beacon();
  }

  Future<void> _request(HttpRequest request, int epoch) async {
    final response = request.response;
    response.headers.contentType = ContentType.json;
    response.headers.set('Cache-Control', 'no-store');
    if (!running || epoch != _epoch || _active >= 4) {
      response.statusCode = 429;
      await response.close();
      return;
    }
    if (request.method == 'GET' && request.uri.path == '/healthz') {
      response.write(
        jsonEncode({'status': 'ok', 'service': 'controller-mobile'}),
      );
      await response.close();
      return;
    }
    if (request.method == 'GET' && request.uri.path == '/api/discovery') {
      response.write(
        jsonEncode({
          'product': 'controller-mobile',
          'rpc': '/rpc',
          'package_sharing': true,
          'adapter_errors': errors,
        }),
      );
      await response.close();
      return;
    }
    if (request.method != 'POST' || request.uri.path != '/rpc') {
      response.statusCode = 404;
      await response.close();
      return;
    }
    _active++;
    dynamic id;
    try {
      final bytes = <int>[];
      await for (final chunk in request.timeout(const Duration(seconds: 3))) {
        if (bytes.length + chunk.length > 32768) {
          throw const FormatException('RPC exceeds 32 KiB');
        }
        bytes.addAll(chunk);
      }
      final rpc = object(jsonDecode(utf8.decode(bytes)));
      id = rpc['id'];
      if (rpc['jsonrpc'] != '2.0' ||
          (id is! int && id is! String) ||
          (id is String && id.length > 128) ||
          rpc['params'] is! Map) {
        throw const FormatException('Invalid package RPC');
      }
      final method = rpc['method'], params = object(rpc['params']);
      Json result;
      if (method == 'app.package.info' || method == 'app.package.read') {
        result = await upgrades.rpc(method as String, params);
      } else if (method == 'app.upgrade.offer') {
        if (_offering ||
            _transfer != null ||
            upgrades.pulling ||
            upgrades.state['transfer_id'] != null) {
          throw StateError('Another upgrade is pending');
        }
        final current = await upgrades.rpc('app.package.info', {});
        if (params['package'] != current['package'] ||
            params['bytes'] is! int ||
            (params['bytes'] as int) < 1 ||
            (params['bytes'] as int) > AppUpgrade.maximum ||
            params['version_code'] is! int ||
            (params['version_code'] as int) <=
                (current['version_code'] as int) ||
            jsonEncode(params['signers']) != jsonEncode(current['signers']) ||
            !RegExp(r'^[a-f0-9]{64}$').hasMatch('${params['sha256']}')) {
          throw const FormatException('Incompatible package offer');
        }
        _offering = true;
        bool accepted;
        try {
          accepted = await approve({
            ...params,
            'source': request.connectionInfo?.remoteAddress.address,
          }).timeout(const Duration(seconds: 30), onTimeout: () => false);
        } finally {
          _offering = false;
        }
        if (!accepted || epoch != _epoch || !running) {
          throw StateError('Offer not approved');
        }
        result = await upgrades.rpc('app.upgrade.begin', {
          'bytes': params['bytes'],
          'sha256': params['sha256'],
        });
        _transfer = result['transfer_id'] as String;
        _capability = newAutomationToken();
        _expires = DateTime.now().add(const Duration(minutes: 5));
        result = {...result, 'capability': _capability};
      } else if ([
        'app.upgrade.write',
        'app.upgrade.finish',
        'app.upgrade.abort',
        'app.upgrade.status',
      ].contains(method)) {
        if (_capability == null ||
            _transfer == null ||
            _expires!.isBefore(DateTime.now()) ||
            !sameSecret(
              request.headers.value('authorization') ?? '',
              'Bearer $_capability',
            ) ||
            params['transfer_id'] != _transfer) {
          throw StateError('Transfer authorization expired or invalid');
        }
        result = await upgrades.rpc(method as String, params);
        if (method == 'app.upgrade.finish' || method == 'app.upgrade.abort') {
          _capability = null;
          _transfer = null;
          _expires = null;
        }
      } else {
        throw const FormatException(
          'Only self-package methods are exposed to peers',
        );
      }
      if (epoch == _epoch) {
        response.write(
          jsonEncode({'jsonrpc': '2.0', 'id': id, 'result': result}),
        );
      }
    } catch (error) {
      response.write(
        jsonEncode({
          'jsonrpc': '2.0',
          'id': id,
          'error': {'code': -32000, 'message': '$error'},
        }),
      );
    } finally {
      _active--;
      await response.close();
    }
  }

  Future<void> _udp(bool mndp, int epoch) async {
    final socket = await RawDatagramSocket.bind(
      InternetAddress.anyIPv4,
      mndp ? 5678 : 1900,
      reuseAddress: true,
    );
    try {
      socket.broadcastEnabled = true;
      socket.joinMulticast(
        InternetAddress(mndp ? '224.0.0.88' : '239.255.255.250'),
      );
      socket.listen((event) {
        if (event != RawSocketEvent.read || epoch != _epoch) return;
        Datagram? p;
        while ((p = socket.receive()) != null) {
          final packet = p!;
          if (packet.data.length > 4096) continue;
          final text = mndp
              ? ''
              : utf8.decode(packet.data, allowMalformed: true).toLowerCase();
          final search = mndp
              ? packet.data.length == 4 && packet.data.every((b) => b == 0)
              : text.startsWith('m-search * http/1.1\r\n') &&
                    text.contains('man: "ssdp:discover"') &&
                    (text.contains('st: ssdp:all') ||
                        text.contains(
                          'st: urn:digitalogic-ir:device:controllermobile:1',
                        ));
          if (!search ||
              packet.address.isMulticast ||
              packet.address.isLoopback) {
            continue;
          }
          _responded.removeWhere(
            (_, at) => DateTime.now().difference(at).inSeconds > 1,
          );
          final key = '${packet.address.address}|$mndp';
          if (_responded.containsKey(key) || _responded.length >= 128) continue;
          _responded[key] = DateTime.now();
          if (mndp) {
            socket.send(_mndp(), packet.address, packet.port);
          } else {
            unawaited(_ssdp(socket, packet.address, packet.port));
          }
        }
      });
      _sockets.add(socket);
    } catch (_) {
      socket.close();
      rethrow;
    }
  }

  Future<void> _ssdp(
    RawDatagramSocket socket,
    InternetAddress source,
    int destinationPort,
  ) async {
    // Bind each responder to its real source address so LOCATION cannot drift
    // from the UDP source on multihomed Android devices. No TCP/network probe.
    for (final ip in _addresses) {
      if (!running) return;
      RawDatagramSocket? response;
      try {
        response = await RawDatagramSocket.bind(ip, 0);
        _sendSsdp(response, source, destinationPort, ip.address);
      } on SocketException {
        /* Interface disappeared; next network change refreshes. */
      } finally {
        response?.close();
      }
    }
  }

  void _sendSsdp(
    RawDatagramSocket socket,
    InternetAddress source,
    int destinationPort,
    String ip,
  ) {
    if (!running) return;
    socket.send(
      utf8.encode(
        'HTTP/1.1 200 OK\r\nST: urn:digitalogic-ir:device:ControllerMobile:1\r\n'
        'USN: uuid:$_ssdpId\r\nLOCATION: http://$ip:$port/api/discovery\r\n'
        'X-Digitalogic-Product: controller-mobile\r\nX-Digitalogic-Name: Controller app\r\nCACHE-CONTROL: max-age=60\r\n\r\n',
      ),
      source,
      destinationPort,
    );
  }

  Uint8List _mndp() {
    final identity = _instance;
    final mac = sha256.convert(utf8.encode(identity)).bytes.take(6).toList();
    mac[0] = (mac[0] | 2) & 254;
    final b = BytesBuilder()
      ..add([0, 0, (_sequence >> 8) & 255, _sequence++ & 255]);
    void field(int type, List<int> bytes) {
      b.add([type >> 8, type & 255, bytes.length >> 8, bytes.length & 255]);
      b.add(bytes);
    }

    field(1, mac);
    field(5, utf8.encode('Controller app'));
    field(8, utf8.encode('Digitalogic'));
    field(11, utf8.encode(identity));
    field(12, utf8.encode('Application endpoint'));
    field(16, utf8.encode('application'));
    field(0xff01, utf8.encode('controller-mobile'));
    field(0xff02, [port! >> 8, port! & 255]);
    return b.takeBytes();
  }

  void _beacon() {
    if (!running) return;
    if (_expires?.isBefore(DateTime.now()) == true && _transfer != null) {
      final expired = _transfer;
      _capability = null;
      _transfer = null;
      _expires = null;
      unawaited(
        upgrades
            .rpc('app.upgrade.abort', {'transfer_id': expired})
            .catchError((_) => <String, dynamic>{}),
      );
    }
    for (final s in _sockets) {
      if (s.port == 5678) {
        s.send(_mndp(), InternetAddress('255.255.255.255'), 5678);
        s.send(_mndp(), InternetAddress('224.0.0.88'), 5678);
      }
    }
  }

  Future<void> close() async {
    ++_epoch;
    _announce?.cancel();
    _announce = null;
    final server = _server;
    if (server != null) {
      DeviceDiscovery.localEndpoints.removeAll(
        _addresses.map((ip) => 'http://${ip.address}:${server.port}'),
      );
    }
    _server = null;
    await server?.close(force: true);
    for (final s in _sockets) {
      s.close();
    }
    _sockets.clear();
    _responded.clear();
    _addresses.clear();
    try {
      await DeviceDiscovery.channel.invokeMethod<void>('unadvertise');
    } on Object {
      /* Cleanup. */
    }
    if (_transfer != null) {
      try {
        await upgrades.rpc('app.upgrade.abort', {'transfer_id': _transfer});
      } on Object {
        /* Receipt retained. */
      }
    }
    _capability = null;
    _transfer = null;
    _expires = null;
  }
}
