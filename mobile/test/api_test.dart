import 'dart:convert';
import 'dart:async';
import 'dart:io';

import 'package:flutter_test/flutter_test.dart';
import 'package:http/http.dart' as http;
import 'package:http/testing.dart';
import 'package:pccontroller_mobile/api.dart';

void main() {
  test('Changed endpoint discards an in-flight status and never dispatches its queued POST', () async {
    final pending = Completer<http.Response>();
    var posts = 0;
    final api = PakarApi(
      client: MockClient((request) async {
        if (request.method == 'POST') {
          posts++;
          return http.Response('{}', 200);
        }
        return pending.future;
      }),
    );
    final write = api.post('/api/game/reset');
    final check = expectLater(write, throwsA(isA<StaleConnection>()));
    api.baseUrl = 'http://other.local';
    pending.complete(http.Response('{"hostname":"ledboard"}', 200));
    await check;
    expect(posts, 0);
    await api.close();
  });
  test('Changing credentials revokes same-host cached identity without logging secrets', () async {
    var reads = 0, posts = 0;
    final pending = Completer<http.Response>();
    final api = PakarApi(
      client: MockClient((request) async {
        if (request.method == 'POST') {
          posts++;
          return http.Response('{}', 200);
        }
        reads++;
        return reads == 1
            ? http.Response('{"hostname":"ledboard"}', 200)
            : pending.future;
      }),
    );
    await api.status();
    api.password = 'replacement';
    final write = api.post('/api/game/reset');
    final check = expectLater(write, throwsA(isA<StaleConnection>()));
    api.password = 'second replacement';
    pending.complete(http.Response('{"hostname":"ledboard"}', 200));
    await check;
    expect(reads, 2);
    expect(posts, 0);
    await api.close();
  });
  test('Endpoint rejects credentials/path injection', () {
    expect(
      PakarApi.normalizeEndpoint(' ledboard.local '),
      'http://ledboard.local',
    );
    expect(
      PakarApi.normalizeEndpoint('https://renamed.local:8443/'),
      'https://renamed.local:8443',
    );
    for (final value in [
      'ftp://board.local',
      'http://user:pass@board.local',
      'http://board.local/path',
      'http://board.local?x=1',
      'http://board.local#frag',
    ]) {
      expect(() => PakarApi.normalizeEndpoint(value), throwsFormatException);
    }
  });
  test('Mutation with lost response is attempted exactly once', () async {
    var posts = 0, discoveries = 0;
    final api = PakarApi(
      client: MockClient((request) async {
        if (request.method == 'POST') {
          ++posts;
          throw const SocketException('lost response after apply');
        }
        return http.Response(jsonEncode({'hostname': 'ledboard'}), 200);
      }),
      discovery: () async {
        ++discoveries;
        return {'http://192.0.2.1'};
      },
    );
    await expectLater(
      api.post('/api/game/reset'),
      throwsA(isA<OperationUncertain>()),
    );
    expect(posts, 1);
    expect(discoveries, 0);
    await api.close();
  });
  test('Renamed local label uses identity-checked read discovery', () async {
    final api = PakarApi(
      baseUrl: 'http://renamed.local',
      client: MockClient((request) async {
        if (request.url.host.endsWith('.local')) {
          throw const SocketException('no mDNS');
        }
        return http.Response(
          jsonEncode({
            'hostname': request.url.host == '192.0.2.1' ? 'other' : 'renamed',
          }),
          200,
        );
      }),
      discovery: () async => {'http://192.0.2.1', 'http://192.0.2.2'},
    );
    expect((await api.status()).hostname, 'renamed');
    expect(api.transportUrl, 'http://192.0.2.2');
    api.baseUrl = 'http://new.local';
    expect(api.transportUrl, 'http://new.local');
    await api.close();
  });
  test('Wrong responding hostname cannot authorize a mutation', () async {
    var posts = 0;
    final api = PakarApi(
      client: MockClient((request) async {
        if (request.method == 'POST') ++posts;
        return http.Response('{"hostname":"other"}', 200);
      }),
    );
    await expectLater(api.post('/api/game/reset'), throwsFormatException);
    expect(posts, 0);
    await api.close();
  });
}
