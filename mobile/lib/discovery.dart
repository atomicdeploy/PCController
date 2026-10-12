import 'dart:async';
import 'dart:convert';
import 'dart:io';

import 'package:flutter/services.dart';

import 'api.dart';

class DiscoveredDevice {
  const DiscoveredDevice(this.name, this.endpoint, this.protocol);
  final String name, endpoint, protocol;
}

/// Metadata only, adapted from PCController discovery.go and PU MNDP TLVs.
/// No credentials are sent by scanning, and advertisements never authorize.
class DeviceDiscovery {
  static const channel = MethodChannel('ir.digitalogic.pccontroller/discovery');
  final errors = <String, String>{};
  Future<List<DiscoveredDevice>> scan() async {
    errors.clear();
    final found = <DiscoveredDevice>[];
    await Future.wait([
      _guard('mDNS / DNS-SD', () async {
        final raw = await channel.invokeListMethod<dynamic>('scan');
        for (final v in raw ?? []) {
          if (v is! Map || v['endpoint'] is! String) continue;
          try {
            found.add(
              DiscoveredDevice(
                '${v['name']}',
                PakarApi.normalizeEndpoint(v['endpoint']),
                'mDNS / DNS-SD',
              ),
            );
          } on FormatException {
            /* Reject invalid advertised origins. */
          }
        }
      }),
      _guard('SSDP', () => _udp(found, false)),
      _guard('MNDP', () => _udp(found, true)),
    ]);
    final unique = <String, DiscoveredDevice>{};
    for (final d in found.take(128)) {
      unique['${d.endpoint}|${d.protocol}'] = d;
    }
    return unique.values.toList()..sort((a, b) => a.name.compareTo(b.name));
  }

  Future<void> _guard(String protocol, Future<void> Function() work) async {
    try {
      await work();
    } on Object {
      errors[protocol] = 'Unavailable on this network/platform';
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
              : parseSsdp(p.data);
          if (d != null) found.add(d);
        }
      });
      if (mndp) {
        socket.send([0, 0, 0, 0], InternetAddress('255.255.255.255'), 5678);
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

DiscoveredDevice? parseSsdp(Uint8List packet) {
  if (packet.length > 4096) return null;
  try {
    final lines = const LineSplitter().convert(utf8.decode(packet));
    if (lines.isEmpty || !lines.first.startsWith('HTTP/1.1 200 ')) return null;
    final headers = <String, String>{};
    for (final line in lines.skip(1)) {
      final i = line.indexOf(':');
      if (i > 0) {
        headers[line.substring(0, i).trim().toLowerCase()] = line
            .substring(i + 1)
            .trim();
      }
    }
    final u = Uri.parse(headers['location'] ?? '');
    if (u.userInfo.isNotEmpty ||
        u.host.isEmpty ||
        !['http', 'https'].contains(u.scheme)) {
      return null;
    }
    final endpoint = PakarApi.normalizeEndpoint(
      u.replace(path: '', query: null, fragment: null).toString(),
    );
    return DiscoveredDevice(
      headers['x-pccontroller-name'] ?? headers['usn'] ?? u.host,
      endpoint,
      'SSDP',
    );
  } on FormatException {
    return null;
  }
}

DiscoveredDevice? parseMndp(Uint8List packet, String source) {
  if (packet.length < 8 || packet.length > 2048) return null;
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
    final name = utf8.decode(fields[5] ?? []);
    if (name.isEmpty ||
        name.length > 96 ||
        RegExp(r'[\x00-\x1f\x7f]').hasMatch(name)) {
      return null;
    }
    // MNDP has no API port/auth contract: present as unverified HTTP candidate.
    return DiscoveredDevice(
      name,
      PakarApi.normalizeEndpoint('http://$source'),
      'MNDP',
    );
  } on FormatException {
    return null;
  }
}
