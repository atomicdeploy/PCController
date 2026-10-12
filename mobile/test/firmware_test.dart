import 'dart:async';
import 'dart:convert';
import 'dart:io';
import 'dart:typed_data';

import 'package:flutter_test/flutter_test.dart';
import 'package:http/http.dart' as http;
import 'package:pccontroller_mobile/api.dart';
import 'package:pccontroller_mobile/firmware.dart';

const manifestHash =
    'aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa';
Map<String, dynamic> firmwareJob({
  String phase = 'idle',
  int id = 0,
  int targets = 3,
}) => {
  'job_id': id,
  'boot_id': 'fixture-boot',
  'manifest_sha256': id == 0 ? '' : manifestHash,
  'phase': phase,
  'targets': targets,
  'busy': {'manifest', 'prepare'}.contains(phase),
  'cancellable': phase == 'manifest',
  'received_bytes': 0,
  'expected_bytes': 0,
  'error': '',
};

class MaintenanceClient extends http.BaseClient {
  MaintenanceClient(this.handler);
  final Future<http.StreamedResponse> Function(http.BaseRequest) handler;
  @override
  Future<http.StreamedResponse> send(http.BaseRequest request) =>
      handler(request);
}

http.StreamedResponse jsonResponse(Map<String, dynamic> value) =>
    http.StreamedResponse(Stream.value(utf8.encode(jsonEncode(value))), 200);

void main() {
  test('Strict job fields and truthful action phases reject defaults and impossible counts', () {
    final valid = firmwareJob(phase: 'ready', id: 1);
    expect(RemoteJob.parse(valid).canApply, true);
    expect(RemoteJob.parse(valid).canCancel, true);
    for (final override in [
      {'job_id': 1.0},
      {'job_id': -1},
      {'job_id': 4294967296},
      {'boot_id': 'bad\nboot'},
      {'boot_id': ''},
      {'manifest_sha256': 'A' * 64},
      {'targets': 0},
      {'targets': 4},
      {'busy': 'false'},
      {'cancellable': 1},
      {'phase': 'invented'},
      {'received_bytes': 1},
      {'expected_bytes': -1},
      {'error': 'x' * 1025},
    ]) {
      expect(
        () => RemoteJob.parse({...valid, ...override}),
        throwsFormatException,
      );
    }
    expect(
      RemoteJob.parse({...valid, 'phase': 'program_avr', 'cancellable': true})
          .canCancel,
      false,
    );
    expect(RemoteJob.parse({...valid, 'busy': true}).canApply, false);
  });
  test(
    'Remote URL and image validation use exact board contracts, not filenames',
    () {
      validateRemote(
        'https://example.invalid/manifest.json',
        manifestHash,
        3,
        'token',
      );
      for (final url in [
        'http://example.invalid/m',
        'https://u:p@example.invalid/m',
        'https://example.invalid/m#x',
        'https://example.invalid/a b',
        'https://example.invalid/a\\b',
        'https://example.invalid/${'a' * 384}',
      ]) {
        expect(
          () => validateRemote(url, manifestHash, 3, ''),
          throwsFormatException,
        );
      }
      expect(
        () => validateRemote(
          'https://example.invalid/m',
          manifestHash,
          3,
          'two words',
        ),
        throwsFormatException,
      );
      validateFirmwareImage(false, Uint8List(32768));
      validateFirmwareImage(true, Uint8List(16)..[0] = 0xe9);
      for (final count in [0, 32767, 32769]) {
        expect(
          () => validateFirmwareImage(false, Uint8List(count)),
          throwsFormatException,
        );
      }
      expect(
        () => validateFirmwareImage(true, Uint8List(16)),
        throwsFormatException,
      );
      expect(
        () => validateFirmwareImage(true, Uint8List(2097153)..[0] = 0xe9),
        throwsFormatException,
      );
    },
  );
  test('Stage uses one authenticated form BODY, pinned targets, stage-only, no redirect', () async {
    var mutations = 0;
    final api = PakarApi(
      client: MaintenanceClient((request) async {
        if (request.url.path == '/api/status') {
          return jsonResponse({'hostname': 'ledboard'});
        }
        ++mutations;
        expect(request.followRedirects, false);
        expect(request.url.query, isEmpty);
        expect(request.headers['authorization'], startsWith('Basic '));
        final form = (request as http.Request).bodyFields;
        expect(form['apply'], 'false');
        expect(form['targets'], '3');
        expect(form['sha256'], manifestHash);
        expect(form['bearer'], 'secret-token');
        expect(form['allow_downgrade'], 'false');
        return jsonResponse(firmwareJob(phase: 'manifest', id: 42));
      }),
    );
    final controller = FirmwareController(api, authorized: (_) => true);
    expect(
      (await controller.stage(
        'https://example.invalid/m',
        manifestHash,
        3,
        'secret-token',
      )).id,
      42,
    );
    expect(mutations, 1);
    await api.close();
  });
  for (final difference in [
    'boot_id',
    'manifest_sha256',
    'job_id',
    'targets',
    'phase',
  ]) {
    test(
      'Changed $difference while confirming prevents apply dispatch',
      () async {
        var mutations = 0;
        final original = firmwareJob(phase: 'ready', id: 7);
        final fresh = {
          ...original,
          difference: switch (difference) {
            'boot_id' => 'other-boot',
            'manifest_sha256' => 'b' * 64,
            'job_id' => 8,
            'targets' => 1,
            _ => 'prepare',
          },
        };
        final api = PakarApi(
          client: MaintenanceClient((request) async {
            if (request.url.path == '/api/status') {
              return jsonResponse({'hostname': 'ledboard'});
            }
            if (request.method == 'POST') ++mutations;
            return jsonResponse(fresh);
          }),
        );
        final controller = FirmwareController(api, authorized: (_) => true);
        await expectLater(
          controller.act(RemoteJob.parse(original), apply: true),
          throwsFormatException,
        );
        expect(mutations, 0);
        await api.close();
      },
    );
  }
  test(
    'Apply pins exact job identity and rejects false successful ACK',
    () async {
      var mutations = 0;
      final original = firmwareJob(phase: 'ready', id: 7);
      final api = PakarApi(
        client: MaintenanceClient((request) async {
          if (request.url.path == '/api/status') {
            return jsonResponse({'hostname': 'ledboard'});
          }
          if (request.method == 'GET') return jsonResponse(original);
          ++mutations;
          expect(
            (request as http.Request).bodyFields,
            RemoteJob.parse(original).identityForm,
          );
          return jsonResponse({
            ...original,
            'phase': 'prepare',
            'boot_id': 'other-boot',
          });
        }),
      );
      await expectLater(
        FirmwareController(
          api,
          authorized: (_) => true,
        ).act(RemoteJob.parse(original), apply: true),
        throwsFormatException,
      );
      expect(mutations, 1);
      await api.close();
    },
  );
  test('Lost mutating GET probe is never replayed through SSDP', () async {
    var probes = 0, discoveries = 0;
    final api = PakarApi(
      discovery: () async {
        ++discoveries;
        return {};
      },
      client: MaintenanceClient((request) async {
        if (request.url.path == '/api/status') {
          return jsonResponse({'hostname': 'ledboard'});
        }
        if (request.url.path == '/api/firmware/remote') {
          return jsonResponse(firmwareJob());
        }
        ++probes;
        throw const SocketException('probe may have reset AVR');
      }),
    );
    await expectLater(
      FirmwareController(api, authorized: (_) => true).manual('probe'),
      throwsA(isA<OperationUncertain>()),
    );
    expect(probes, 1);
    expect(discoveries, 0);
    await api.close();
  });
  test('Owner/capability change during identity verification sends no maintenance command', () async {
    final pending = Completer<http.StreamedResponse>();
    var writes = 0, authorized = true;
    final api = PakarApi(
      client: MaintenanceClient((request) async {
        if (request.url.path == '/api/status') return pending.future;
        ++writes;
        return jsonResponse(firmwareJob(phase: 'manifest', id: 3));
      }),
    );
    final controller = FirmwareController(api, authorized: (_) => authorized);
    final operation = controller.stage(
      'https://example.invalid/m',
      manifestHash,
      3,
      '',
    );
    final checked = expectLater(operation, throwsA(isA<StaleConnection>()));
    authorized = false;
    pending.complete(jsonResponse({'hostname': 'ledboard'}));
    await checked;
    expect(writes, 0);
    await api.close();
  });
  test(
    'Disposed AVR stage discards late stored ACK and does not program',
    () async {
      final pending = Completer<http.StreamedResponse>();
      var writes = 0;
      final api = PakarApi(
        client: MaintenanceClient((request) async {
          if (request.url.path == '/api/status') {
            return jsonResponse({'hostname': 'ledboard'});
          }
          if (request.url.path == '/api/firmware/remote') {
            return jsonResponse(firmwareJob());
          }
          ++writes;
          expect(request, isA<http.MultipartRequest>());
          expect(
            (request as http.MultipartRequest).files.single.field,
            'image',
          );
          return pending.future;
        }),
      );
      final controller = FirmwareController(api, authorized: (_) => true);
      final operation = controller.manual('stage', bytes: Uint8List(32768));
      final checked = expectLater(operation, throwsA(isA<StaleConnection>()));
      while (writes == 0) {
        await Future<void>.delayed(Duration.zero);
      }
      controller.dispose();
      pending.complete(
        http.StreamedResponse(
          Stream.value(utf8.encode('AVR image stored')),
          201,
        ),
      );
      await checked;
      expect(writes, 1);
      await api.close();
    },
  );
  for (final count in [1023, 1024, 1025]) {
    test(
      'Stored EEPROM export exact length $count before creating a file',
      () async {
        final api = PakarApi(
          client: MaintenanceClient((request) async {
            if (request.url.path == '/api/status') {
              return jsonResponse({'hostname': 'ledboard'});
            }
            return http.StreamedResponse(
              Stream.value(List.filled(count, 0)),
              200,
            );
          }),
        );
        final operation = FirmwareController(
          api,
          authorized: (_) => true,
        ).backupBytes(true);
        if (count == 1024) {
          expect((await operation).length, 1024);
        } else {
          await expectLater(operation, throwsFormatException);
        }
        await api.close();
      },
    );
  }
  test(
    'Response total deadline cancels drip stream; error body is never echoed',
    () async {
      var cancelled = false;
      final stream = StreamController<List<int>>(
        onCancel: () {
          cancelled = true;
        },
      );
      final api = PakarApi(
        client: MaintenanceClient((request) async {
          if (request.url.path == '/api/status') {
            return jsonResponse({'hostname': 'ledboard'});
          }
          return http.StreamedResponse(stream.stream, 200);
        }),
      );
      await expectLater(
        api.maintenanceBytes(
          '/api/avr/probe',
          mayMutate: true,
          authorize: () => true,
          timeout: const Duration(milliseconds: 30),
        ),
        throwsA(isA<OperationUncertain>()),
      );
      expect(cancelled, true);
      await stream.close();
      await api.close();
    },
  );
  test(
    'Redirect is rejected without forwarding credentials or replaying mutation',
    () async {
      var writes = 0;
      final api = PakarApi(
        client: MaintenanceClient((request) async {
          if (request.url.path == '/api/status') {
            return jsonResponse({'hostname': 'ledboard'});
          }
          ++writes;
          expect(request.followRedirects, false);
          return http.StreamedResponse(
            Stream.value(utf8.encode('secret-token')),
            307,
            headers: {'location': 'https://other.invalid'},
          );
        }),
      );
      try {
        await api.maintenanceBytes(
          '/update',
          post: true,
          authorize: () => true,
        );
        fail('Expected rejection');
      } catch (error) {
        expect(error, isFormatException);
        expect('$error', isNot(contains('secret-token')));
      }
      expect(writes, 1);
      await api.close();
    },
  );
}
