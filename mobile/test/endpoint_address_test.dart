import 'dart:async';
import 'dart:convert';

import 'package:flutter_test/flutter_test.dart';
import 'package:http/http.dart' as http;
import 'package:http/testing.dart';
import 'package:pccontroller_mobile/api.dart';
import 'package:pccontroller_mobile/unified/endpoint_address.dart';
import 'package:pccontroller_mobile/unified/endpoint_detector.dart';
import 'package:pccontroller_mobile/unified/transport.dart';

void main() {
  test(
    'localized paste, bare hosts and known service URLs normalize to origins',
    () {
      final examples = {
        '  ۱۹۲.۱۶۸.۰۰۱.۲۰:۰۸۷۸۷  ': 'http://192.168.1.20:8787',
        '١٩٢.١٦٨.١.٢٠:٨٧٨٧': 'http://192.168.1.20:8787',
        'HOST.LOCAL:8787/': 'http://host.local:8787',
        'https://HOST.LOCAL:00443/': 'https://host.local',
        'wss://host.local:9443/ws': 'https://host.local:9443',
        'ws://host.local:8787/ipc': 'http://host.local:8787',
        'http://host.local:8787/api/snapshot': 'http://host.local:8787',
        'http://host.local:8787/#/outputs': 'http://host.local:8787',
        '2001:db8::1': 'http://[2001:db8::1]',
        '[2001:db8::1]:8787': 'http://[2001:db8::1]:8787',
        'localhost.': 'http://localhost',
        '\u200e１９２.１６８.１.２０：８７８７': 'http://192.168.1.20:8787',
      };
      for (final example in examples.entries) {
        expect(
          EndpointAddress.parse(example.key).origin.toString(),
          example.value,
          reason: example.key,
        );
      }
      expect(
        EndpointAddress.parse('workshop', defaultPort: 8787).origin.toString(),
        'http://workshop:8787',
      );
      expect(
        EndpointAddress.parse(
          'http://workshop',
          defaultPort: 8787,
        ).origin.toString(),
        'http://workshop',
      ); // An explicit scheme retains standard URL defaults.
    },
  );

  test('invalid ports, IPv4, credentials and unsupported paths cannot enter transport', () {
    for (final input in [
      '',
      'host:',
      'host:0',
      'host:65536',
      'host:-1',
      'host:abc',
      '999.168.1.20',
      '192.168..20',
      '1.2.3',
      '0.0.0.0:8787',
      '255.255.255.255',
      '[::]',
      'http://user:secret@host:8787',
      'http://host:8787?token=secret',
      'http://host:8787/arbitrary/proxy',
      'file:///etc/passwd',
      'ftp://host',
      'host with space',
      'host..local',
      '-host.local',
      'http://host:8787/%2e%2e/api/rpc',
      'http://host:8787\\other',
    ]) {
      expect(
        () => EndpointAddress.parse(input),
        throwsFormatException,
        reason: input,
      );
    }
    expect(EndpointAddress.parse('host:65535').origin.port, 65535);
    expect(() => endpoint('http://host/api/rpc'), throwsFormatException);
    expect(endpoint('  HOST:8787/ ').toString(), 'http://host:8787');
    expect(
      PakarApi.normalizeEndpoint('۱۹۲.۱۶۸.۱.۲۰:۸۷۸۷'),
      'http://192.168.1.20:8787',
    );
    expect(
      () => PakarApi.normalizeEndpoint('host:65536'),
      throwsFormatException,
    );
  });

  test(
    'candidate detection stays on entered host and respects explicit port/TLS',
    () {
      final https = EndpointAddress.parse('https://workshop');
      final candidates = https.candidates('pccontroller', [
        'https://workshop:9443',
        'http://workshop:8787',
        'https://other:9443',
      ]);
      expect(
        candidates.every((u) => u.scheme == 'https' && u.host == 'workshop'),
        true,
      );
      expect(candidates.any((u) => u.port == 9443), true);
      expect(candidates.length, lessThanOrEqualTo(6));
      expect(
        EndpointAddress.parse('http://workshop:80')
            .candidates('pccontroller', ['http://workshop:8787']),
        [Uri.parse('http://workshop')],
      );
    },
  );

  test('read-only detector verifies advertised product rather than an open HTTP server', () async {
    final requests = <http.Request>[];
    final detector = EndpointDetector(
      client: MockClient((request) async {
        requests.add(request);
        expect(request.method, 'GET');
        expect(request.followRedirects, false);
        expect(request.headers.containsKey('Authorization'), false);
        return http.Response(
          jsonEncode({
            'status': 'ok',
            'service': request.url.port == 8080 ? 'pealayer' : 'other',
          }),
          200,
        );
      }),
    );
    addTearDown(detector.close);
    final result = await detector.detect(
      EndpointAddress.parse('workshop'),
      'pealayer',
    );
    expect(result.origin, Uri.parse('http://workshop:8080'));
    expect(
      requests.every(
        (r) => r.url.host == 'workshop' && r.url.path == '/healthz',
      ),
      true,
    );
    expect(requests.length, lessThanOrEqualTo(6));
  });

  test(
    'installed controller compatibility requires its public directory identity',
    () async {
      final detector = EndpointDetector(
        client: MockClient((request) async {
          return http.Response(
            jsonEncode(
              request.url.path == '/healthz'
                  ? {'ok': true}
                  : {
                      'product': 'PCController',
                      'hostname': 'fixture',
                      'health': {'ok': true},
                    },
            ),
            200,
          );
        }),
      );
      addTearDown(detector.close);
      final found = await detector.detect(
        EndpointAddress.parse('workshop:8787'),
        'pccontroller',
      );
      expect(found.product, 'pccontroller');
      expect(found.origin.port, 8787);
    },
  );

  test(
    'wrong product, redirects, oversized bodies and cancellation never verify',
    () async {
      for (final response in [
        http.Response('{"status":"ok","service":"pealayer"}', 200),
        http.Response('{"ok":true}', 200),
        http.Response(
          '{"status":"ok","product":"pccontroller"}',
          302,
          headers: {'location': 'http://other'},
        ),
        http.Response(' ' * 17000, 200),
      ]) {
        final detector = EndpointDetector(
          client: MockClient((_) async => response),
        );
        await expectLater(
          detector.detect(
            EndpointAddress.parse('workshop:8787'),
            'pccontroller',
          ),
          throwsFormatException,
        );
        detector.close();
      }
      final gate = Completer<http.Response>();
      var canceled = false;
      final detector = EndpointDetector(client: MockClient((_) => gate.future));
      final checking = detector.detect(
        EndpointAddress.parse('workshop:8787'),
        'pccontroller',
        canceled: () => canceled,
      );
      canceled = true;
      gate.complete(
        http.Response('{"status":"ok","product":"pccontroller"}', 200),
      );
      await expectLater(checking, throwsFormatException);
      detector.close();
    },
  );
}
