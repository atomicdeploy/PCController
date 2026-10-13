import 'dart:convert';
import 'dart:io';

import 'package:http/http.dart' as http;
import 'package:http/io_client.dart';

import 'endpoint_address.dart';

class DetectedEndpoint {
  const DetectedEndpoint(this.origin, this.product);
  final Uri origin;
  final String product;
}

/// Read-only identity checks on bounded ports of the explicitly entered host.
/// Open TCP ports, redirect pages and arbitrary JSON never prove a product.
class EndpointDetector {
  EndpointDetector({http.Client? client})
    : client =
          client ??
          IOClient(
            HttpClient()
              ..findProxy = ((_) => 'DIRECT')
              ..connectionTimeout = const Duration(seconds: 2),
          );
  final http.Client client;

  Future<Map<String, dynamic>?> _read(Uri uri) async {
    final request = http.Request('GET', uri)
      ..followRedirects = false
      ..headers['Accept'] = 'application/json';
    try {
      final response = await client
          .send(request)
          .timeout(const Duration(seconds: 2));
      if (response.statusCode != 200) return null;
      final bytes = <int>[];
      final reading = Stopwatch()..start();
      await for (final chunk in response.stream.timeout(
        const Duration(seconds: 2),
      )) {
        if (reading.elapsed > const Duration(seconds: 2) ||
            bytes.length + chunk.length > 16384) {
          return null;
        }
        bytes.addAll(chunk);
      }
      final value = jsonDecode(utf8.decode(bytes));
      return value is Map ? Map<String, dynamic>.from(value) : null;
    } on Object {
      return null;
    }
  }

  Future<DetectedEndpoint> detect(
    EndpointAddress address,
    String product, {
    Iterable<String> advertised = const [],
    bool Function()? canceled,
  }) async {
    final candidates = address.candidates(product, advertised);
    final matched = <Uri>[];
    final wrongProducts = <String>{};
    var next = 0;
    final deadline = DateTime.now().add(const Duration(seconds: 6));
    await Future.wait(
      List.generate(2, (_) async {
        while (next < candidates.length &&
            !(canceled?.call() ?? false) &&
            DateTime.now().isBefore(deadline)) {
          final origin = candidates[next++];
          final health = await _read(origin.resolve('/healthz'));
          if (canceled?.call() ?? false) return;
          var identity = (health?['product'] ?? health?['service'])
              ?.toString()
              .toLowerCase();
          var healthy = health?['status'] == 'ok';
          if (product == 'pccontroller' &&
              health?['ok'] == true &&
              identity == null) {
            final directory = await _read(origin.resolve('/upnp/public.json'));
            if (canceled?.call() ?? false) return;
            if (directory?['product'] == 'PCController' &&
                directory?['hostname'] is String &&
                directory?['health'] is Map &&
                directory?['health']['ok'] == true) {
              identity = 'pccontroller';
              healthy = true;
            }
          }
          if (healthy && identity == product) {
            matched.add(origin);
          } else if (healthy &&
              {'pccontroller', 'pealayer'}.contains(identity)) {
            wrongProducts.add(identity!);
          }
        }
      }),
    ).timeout(const Duration(seconds: 9));
    if (canceled?.call() ?? false) {
      throw const FormatException('Endpoint check canceled.');
    }
    if (matched.isEmpty) {
      if (wrongProducts.isNotEmpty) {
        throw FormatException(
          'This host advertises ${wrongProducts.join(', ')}. Select its matching connection or bridge route.',
        );
      }
      throw FormatException(
        'No verified ${product == 'pccontroller' ? 'PCController' : 'Pealayer'} endpoint found. Check the address and service port.',
      );
    }
    matched.sort(
      (a, b) => candidates.indexOf(a).compareTo(candidates.indexOf(b)),
    );
    return DetectedEndpoint(matched.first, product);
  }

  void close() => client.close();
}
