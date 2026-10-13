import 'dart:async';
import 'dart:convert';
import 'dart:io';

import 'package:flutter/services.dart';

import 'api.dart';

const discoveryProducts = {'pccontroller', 'pealayer', 'controller-mobile'};

class DiscoveredDevice {
  const DiscoveredDevice(
    this.name,
    this.endpoint,
    this.protocol, {
    this.product = '',
    this.verified = false,
  });
  final String name, endpoint, protocol, product;
  final bool verified;
}

/// Hints, never authorization or evidence of board presence. Run on intent or
/// network change, not continuous polling; no credentials or persistence.
class DeviceDiscovery {
  static const channel = MethodChannel('ir.digitalogic.pccontroller/discovery');
  static final localEndpoints = <String>{};
  final errors = <String, String>{};
  bool _scanning = false;
  Future<List<DiscoveredDevice>> scan() async {
    if (_scanning) throw StateError('Discovery already running');
    _scanning = true;
    errors.clear();
    final found = <DiscoveredDevice>[];
    try {
      try {
        await channel.invokeMethod<void>('acquire');
      } on MissingPluginException {
        /* Desktop sockets need no Android lock. */
      }
      await Future.wait([
        _guard('mDNS / DNS-SD', () async {
          final raw = await channel.invokeListMethod<dynamic>('scan');
          for (final v in raw ?? []) {
            if (v is! Map || v['endpoint'] is! String || found.length >= 128) {
              continue;
            }
            try {
              found.add(
                DiscoveredDevice(
                  '${v['name']}',
                  PakarApi.normalizeEndpoint(v['endpoint']),
                  'mDNS / DNS-SD',
                  product: '${v['product'] ?? ''}',
                ),
              );
            } on FormatException {
              /* Invalid advertised origin. */
            }
          }
        }),
        _guard('SSDP', () => _udp(found, false)),
        _guard('MNDP', () => _udp(found, true)),
      ]);
      final candidates = <String, DiscoveredDevice>{};
      for (final d in found.take(128)) {
        if (localEndpoints.contains(d.endpoint)) continue;
        final old = candidates[d.endpoint];
        candidates[d.endpoint] = DiscoveredDevice(
          d.name,
          d.endpoint,
          {...?old?.protocol.split(', '), d.protocol}.join(', '),
          product: d.product.isEmpty ? old?.product ?? '' : d.product,
        );
      }
      final queue = candidates.values.toList(), verified = <DiscoveredDevice>[];
      var next = 0;
      final deadline = DateTime.now().add(const Duration(seconds: 12));
      await Future.wait(
        List.generate(4, (_) async {
          while (next < queue.length && DateTime.now().isBefore(deadline)) {
            final peer = await verifyDiscoveredDevice(queue[next++]);
            if (peer != null) verified.add(peer);
          }
        }),
      );
      return verified..sort((a, b) => a.name.compareTo(b.name));
    } finally {
      try {
        await channel.invokeMethod<void>('release');
      } on Object {
        /* Cleanup. */
      }
      _scanning = false;
    }
  }

  Future<void> _guard(String protocol, Future<void> Function() work) async {
    try {
      await work();
    } on SocketException catch (e) {
      errors[protocol] = '${e.osError?.errorCode ?? 'socket'}';
    } on PlatformException catch (e) {
      errors[protocol] = e.code;
    } on MissingPluginException {
      errors[protocol] = 'unsupported_platform';
    }
  }

  Future<void> _udp(List<DiscoveredDevice> found, bool mndp) async {
    final socket = await RawDatagramSocket.bind(
      InternetAddress.anyIPv4,
      mndp ? 5678 : 0,
      reuseAddress: true,
    );
    StreamSubscription<RawSocketEvent>? listener;
    try {
      socket.broadcastEnabled = true;
      if (mndp) socket.joinMulticast(InternetAddress('224.0.0.88'));
      listener = socket.listen((event) {
        if (event != RawSocketEvent.read) return;
        Datagram? packet;
        while ((packet = socket.receive()) != null) {
          if (found.length >= 128) continue;
          final p = packet!;
          final d = mndp
              ? parseMndp(p.data, p.address.address)
              : parseSsdp(p.data, source: p.address.address);
          if (d != null) found.add(d);
        }
      });
      if (mndp) {
        socket.send([0, 0, 0, 0], InternetAddress('255.255.255.255'), 5678);
        socket.send([0, 0, 0, 0], InternetAddress('224.0.0.88'), 5678);
      } else {
        socket.send(
          utf8.encode(
            'M-SEARCH * HTTP/1.1\r\nHOST: 239.255.255.250:1900\r\n'
            'MAN: "ssdp:discover"\r\nMX: 1\r\nST: ssdp:all\r\n\r\n',
          ),
          InternetAddress('239.255.255.250'),
          1900,
        );
      }
      await Future<void>.delayed(const Duration(seconds: 4));
    } finally {
      await listener?.cancel();
      socket.close();
    }
  }
}

/// Source-pinned read-only probe. No proxy, redirects, secrets or mutations.
Future<DiscoveredDevice?> verifyDiscoveredDevice(
  DiscoveredDevice candidate,
) async {
  final origin = Uri.parse(candidate.endpoint);
  final ip = InternetAddress.tryParse(origin.host);
  if (ip == null ||
      ip.isMulticast ||
      ip.isLoopback ||
      ip.address == '0.0.0.0') {
    return null;
  }
  final client = HttpClient()
    ..findProxy = ((_) => 'DIRECT')
    ..connectionTimeout = const Duration(milliseconds: 800);
  try {
    final request = await client.getUrl(origin.resolve('/healthz'));
    request.followRedirects = false;
    final response = await request.close().timeout(const Duration(seconds: 1));
    if (response.statusCode != 200) return null;
    final bytes = <int>[];
    await for (final chunk in response.timeout(const Duration(seconds: 1))) {
      if (bytes.length + chunk.length > 4096) return null;
      bytes.addAll(chunk);
    }
    var value = jsonDecode(utf8.decode(bytes));
    // Compatibility proof for installed alpha hosts predating stable health IDs.
    if (value is Map &&
        value['ok'] == true &&
        value['status'] == null &&
        (candidate.product.isEmpty || candidate.product == 'pccontroller')) {
      final publicRequest = await client.getUrl(
        origin.resolve('/upnp/public.json'),
      );
      publicRequest.followRedirects = false;
      final publicResponse = await publicRequest.close().timeout(
        const Duration(seconds: 1),
      );
      if (publicResponse.statusCode != 200) return null;
      final publicBytes = <int>[];
      await for (final chunk in publicResponse.timeout(
        const Duration(seconds: 1),
      )) {
        if (publicBytes.length + chunk.length > 16384) return null;
        publicBytes.addAll(chunk);
      }
      final directory = jsonDecode(utf8.decode(publicBytes));
      if (directory is! Map ||
          directory['product'] != 'PCController' ||
          directory['hostname'] is! String ||
          directory['health'] is! Map ||
          directory['health']['ok'] != true) {
        return null;
      }
      value = {'status': 'ok', 'product': 'pccontroller'};
    }
    if (value is! Map ||
        value['status'] != 'ok' ||
        !discoveryProducts.contains(value['product'] ?? value['service'])) {
      return null;
    }
    final product = (value['product'] ?? value['service']) as String;
    if (candidate.product.isNotEmpty && candidate.product != product) {
      return null;
    }
    return DiscoveredDevice(
      candidate.name,
      candidate.endpoint,
      candidate.protocol,
      product: product,
      verified: true,
    );
  } on Object {
    return null;
  } finally {
    client.close(force: true);
  }
}

DiscoveredDevice? parseSsdp(Uint8List packet, {String? source}) {
  if (packet.length > 4096) return null;
  try {
    final lines = const LineSplitter().convert(utf8.decode(packet));
    if (lines.isEmpty || !lines.first.startsWith('HTTP/1.1 200 ')) return null;
    final headers = <String, String>{};
    for (final line in lines.skip(1)) {
      final i = line.indexOf(':');
      if (i > 0) {
        final key = line.substring(0, i).trim().toLowerCase();
        if (headers.containsKey(key)) return null;
        headers[key] = line.substring(i + 1).trim();
      }
    }
    final u = Uri.parse(headers['location'] ?? '');
    if (u.userInfo.isNotEmpty ||
        u.host.isEmpty ||
        !['http', 'https'].contains(u.scheme)) {
      return null;
    }
    if (source != null && InternetAddress.tryParse(u.host)?.address != source) {
      return null;
    }
    final endpoint = PakarApi.normalizeEndpoint(
      u.replace(path: '', query: null, fragment: null).toString(),
    );
    final product =
        headers['x-digitalogic-product'] ??
        (headers.containsKey('x-pccontroller-name') ? 'pccontroller' : '');
    return DiscoveredDevice(
      headers['x-pccontroller-name'] ??
          headers['x-digitalogic-name'] ??
          headers['usn'] ??
          u.host,
      endpoint,
      'SSDP',
      product: discoveryProducts.contains(product) ? product : '',
    );
  } on FormatException {
    return null;
  }
}

DiscoveredDevice? parseMndp(Uint8List packet, String source) {
  if (packet.length < 14 ||
      packet.length > 2048 ||
      packet[0] != 0 ||
      packet[1] != 0) {
    return null;
  }
  final ip = InternetAddress.tryParse(source);
  if (ip == null ||
      ip.type != InternetAddressType.IPv4 ||
      ip.isMulticast ||
      source == '0.0.0.0') {
    return null;
  }
  final data = ByteData.sublistView(packet);
  final fields = <int, Uint8List>{};
  for (var i = 4; i < packet.length;) {
    if (i + 4 > packet.length) return null;
    final type = data.getUint16(i), length = data.getUint16(i + 2);
    i += 4;
    if (i + length > packet.length || fields.containsKey(type)) return null;
    fields[type] = packet.sublist(i, i + length);
    i += length;
  }
  try {
    final name = utf8.decode(fields[5] ?? []),
        product = utf8.decode(fields[0xff01] ?? []);
    final portBytes = fields[0xff02];
    if (fields[1]?.length != 6 ||
        name.isEmpty ||
        name.length > 96 ||
        RegExp(r'[\x00-\x1f\x7f]').hasMatch(name) ||
        !discoveryProducts.contains(product) ||
        portBytes?.length != 2) {
      return null;
    }
    final port = ByteData.sublistView(portBytes!).getUint16(0);
    if (port == 0) return null;
    // Standard MNDP has no API port. This vendor extension never assumes 80.
    return DiscoveredDevice(
      name,
      PakarApi.normalizeEndpoint('http://$source:$port'),
      'MNDP',
      product: product,
    );
  } on FormatException {
    return null;
  }
}
