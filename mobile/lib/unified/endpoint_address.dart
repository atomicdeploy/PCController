import 'dart:io';

/// PU850's localized-number input policy, extended for modern URL/IPv6 input.
/// Formatting changes are one code unit per character so selection stays put.
String normalizeAddressCharacters(String value) => String.fromCharCodes(
  value.codeUnits.map(
    (c) => switch (c) {
      >= 0x0660 && <= 0x0669 => c - 0x0660 + 48,
      >= 0x06f0 && <= 0x06f9 => c - 0x06f0 + 48,
      >= 0xff10 && <= 0xff19 => c - 0xff10 + 48,
      0xff1a => 58,
      0xff0e || 0x066b => 46,
      _ => c,
    },
  ),
);

class EndpointAddress {
  EndpointAddress._(this.origin, this.explicitPort, this.explicitScheme);
  final Uri origin;
  final bool explicitPort, explicitScheme;
  static const servicePaths = {
    '',
    '/',
    '/index.html',
    '/ws',
    '/ipc',
    '/api/rpc',
    '/api/ipc',
    '/api/snapshot',
    '/api/player/status',
    '/api/ui-config',
    '/healthz',
  };

  factory EndpointAddress.parse(
    String input, {
    int? defaultPort,
    bool allowServicePath = true,
  }) {
    var text = normalizeAddressCharacters(input)
        .replaceAll(
          RegExp(r'[\u200e\u200f\u202a-\u202e\u2066-\u2069\ufeff]'),
          '',
        )
        .trim();
    if (text.isEmpty) {
      throw const FormatException('Enter a hostname or IP address.');
    }
    if (text.length > 2048 ||
        RegExp(r'[\s\x00-\x1f\x7f\\%]').hasMatch(text) ||
        RegExp(r'(^|/)\.{1,2}(/|$)').hasMatch(text)) {
      throw const FormatException(
        'Remove spaces or unsupported characters from the address.',
      );
    }
    final explicitScheme = text.contains('://');
    if (!explicitScheme) {
      if (text.startsWith('//')) text = text.substring(2);
      final authority = text.split(RegExp(r'[/#?]')).first;
      // An unbracketed IPv6 literal is a host, never an inferred host:port.
      if (!authority.startsWith('[') &&
          InternetAddress.tryParse(authority)?.type ==
              InternetAddressType.IPv6) {
        text = '[$authority]${text.substring(authority.length)}';
      }
      text = 'http://$text';
    }
    Uri uri;
    try {
      uri = Uri.parse(text);
    } on FormatException {
      throw const FormatException(
        'Enter a valid hostname, IPv4 or bracketed IPv6 address.',
      );
    }
    final scheme = switch (uri.scheme.toLowerCase()) {
      'ws' => 'http',
      'wss' => 'https',
      'http' => 'http',
      'https' => 'https',
      _ => throw const FormatException(
        'Use HTTP, HTTPS, WS or WSS for a peer endpoint.',
      ),
    };
    if (!uri.hasAuthority || uri.host.isEmpty) {
      throw const FormatException('Enter a hostname or IP address.');
    }
    if (uri.userInfo.isNotEmpty || uri.authority.contains('@')) {
      throw const FormatException(
        'Remove embedded credentials from the address.',
      );
    }
    if (uri.hasQuery) {
      throw const FormatException(
        'Remove query parameters from the peer address.',
      );
    }
    if ((!allowServicePath && !{'', '/'}.contains(uri.path)) ||
        (allowServicePath && !servicePaths.contains(uri.path)) ||
        (!allowServicePath && uri.hasFragment)) {
      throw const FormatException(
        'Use the host address or a supported API/WebSocket URL.',
      );
    }
    final authority = text
        .substring(text.indexOf('://') + 3)
        .split(RegExp(r'[/#?]'))
        .first;
    final colon = authority.startsWith('[')
        ? authority.indexOf(':', authority.indexOf(']') + 1)
        : authority.indexOf(':');
    final explicitPort = colon >= 0;
    int? port;
    if (explicitPort) {
      final raw = authority.substring(colon + 1);
      port = int.tryParse(raw);
      if (!RegExp(r'^\d+$').hasMatch(raw) ||
          port == null ||
          port < 1 ||
          port > 65535) {
        throw const FormatException(
          'Enter a port from 1 to 65535 after the colon.',
        );
      }
    } else if (!explicitScheme) {
      port = defaultPort;
    }
    var host = uri.host.toLowerCase();
    if (host.startsWith('[') && host.endsWith(']')) {
      host = host.substring(1, host.length - 1);
    }
    if (RegExp(r'^[\d.]+$').hasMatch(host)) {
      final parts = host.split('.');
      if (parts.length != 4 ||
          parts.any(
            (p) => p.isEmpty || int.tryParse(p) == null || int.parse(p) > 255,
          )) {
        throw const FormatException(
          'Enter four IPv4 numbers, each from 0 to 255.',
        );
      }
      host = parts.map(int.parse).join('.');
    }
    final ip = InternetAddress.tryParse(host);
    if (ip != null) {
      if (ip.isMulticast ||
          host == '0.0.0.0' ||
          host == '::' ||
          host == '255.255.255.255') {
        throw const FormatException(
          'Enter a device address, not a listening or broadcast address.',
        );
      }
      host = ip.address;
    } else {
      if (host.endsWith('.')) host = host.substring(0, host.length - 1);
      if (host.length > 253 ||
          host
              .split('.')
              .any(
                (label) =>
                    label.isEmpty ||
                    label.length > 63 ||
                    !RegExp(r'^[a-z0-9](?:[a-z0-9-]*[a-z0-9])?$')
                        .hasMatch(label),
              )) {
        throw const FormatException('Enter a valid hostname or IP address.');
      }
    }
    return EndpointAddress._(
      Uri(scheme: scheme, host: host, port: port),
      explicitPort,
      explicitScheme,
    );
  }

  List<Uri> candidates(String product, Iterable<String> advertised) {
    if (explicitPort) return [origin];
    final values = <Uri>[origin];
    for (final hint in advertised.take(32)) {
      try {
        final candidate = EndpointAddress.parse(hint).origin;
        if (candidate.host == origin.host &&
            candidate.scheme == origin.scheme) {
          values.add(candidate);
        }
      } on FormatException {
        /* Ignore unrelated or malformed hints. */
      }
    }
    values.add(origin.replace(port: product == 'pccontroller' ? 8787 : 8080));
    values.add(origin.replace(port: origin.scheme == 'https' ? 443 : 80));
    // Only a scheme-less input may try TLS, never downgrade explicit HTTPS.
    if (!explicitScheme) values.add(origin.replace(scheme: 'https', port: 443));
    return values.toSet().take(6).toList();
  }
}
