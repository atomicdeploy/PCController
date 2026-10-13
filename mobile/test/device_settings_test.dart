import 'dart:async';
import 'dart:convert';
import 'dart:io';

import 'package:flutter_test/flutter_test.dart';
import 'package:http/http.dart' as http;
import 'package:http/testing.dart';
import 'package:pccontroller_mobile/api.dart';
import 'package:pccontroller_mobile/device_settings.dart';

import 'peer_fixture.dart';
import 'settings_fixture.dart';

void main() {
  test('Required catalog fields, UTF-8 limits and real hardware constraints fail closed', () {
    expect(
      () => settingsSnapshot(SettingsGroup.network, {}),
      throwsFormatException,
    );
    expect(
      () => settingsSnapshot(SettingsGroup.cloud, {}),
      throwsFormatException,
    );
    for (final patch in [
      {'station_ssid': 'é' * 17},
      {'station_enabled': false, 'ap_enabled': false},
      {'station_enabled': 'true'},
    ]) {
      expect(
        () => settingsSnapshot(SettingsGroup.network, {
          ...simulatedNetwork,
          ...patch,
        }),
        throwsFormatException,
      );
    }
    for (final patch in [
      {'mqtt_port': 8883.0},
      {'mqtt_port': 0},
      {'telemetry_seconds': 3601},
      {'mqtt_host': 'https://example.invalid'},
      {'mqtt_base_topic': 'pakar/#'},
      {'websocket_path': 'path'},
      {'mqtt_user': 'quote"'},
      {'websocket_path': r'/back\slash'},
    ]) {
      expect(
        () => settingsSnapshot(SettingsGroup.cloud, {
          ...simulatedCloud,
          ...patch,
        }),
        throwsFormatException,
      );
    }
  });
  test('Actual Draft.observe preserves public and secret-only edits and reports conflicts', () {
    final draft = SettingsDraft(
      settingsSnapshot(SettingsGroup.cloud, simulatedCloud),
    );
    draft.value['mqtt_user'] = 'edited';
    draft.observe(
      settingsSnapshot(SettingsGroup.cloud, {
        ...simulatedCloud,
        'mqtt_user': 'remote',
      }),
    );
    expect(draft.value['mqtt_user'], 'edited');
    expect(draft.conflict, true);
    final secretOnly = SettingsDraft(
      settingsSnapshot(SettingsGroup.cloud, simulatedCloud),
    );
    secretOnly.observe(
      settingsSnapshot(SettingsGroup.cloud, {
        ...simulatedCloud,
        'mqtt_host': 'new.invalid',
      }),
      secretsDirty: true,
    );
    expect(secretOnly.value['mqtt_host'], simulatedCloud['mqtt_host']);
    expect(secretOnly.conflict, true);
  });
  for (final behavior in [
    'success',
    'negative',
    'mismatch',
    'uncertain',
    'external',
  ]) {
    test(
      'Settings write $behavior: one POST, positive ACK and exact readback, no replay',
      () async {
        var saved = Map<String, dynamic>.from(simulatedCloud), posts = 0;
        Map<String, String>? posted;
        final api = PakarApi(
          client: MockClient((request) async {
            if (request.url.path.endsWith('/status')) {
              return http.Response(jsonEncode(peerFixture().json), 200);
            }
            if (request.method == 'POST') {
              ++posts;
              expect(
                request.url.hasQuery,
                false,
                reason: 'Secrets never in request URL',
              );
              posted = request.bodyFields;
              if (behavior == 'uncertain') {
                throw const SocketException('lost ACK');
              }
              if (behavior == 'negative') {
                return http.Response('{"updated":false}', 200);
              }
              if (behavior != 'mismatch') {
                for (final entry in request.bodyFields.entries) {
                  if (entry.key == 'mqtt_password') {
                    saved['mqtt_password_set'] = entry.value.isNotEmpty;
                  } else if (entry.key == 'websocket_token') {
                    saved['websocket_token_set'] = entry.value.isNotEmpty;
                  } else {
                    final type = SettingsGroup.cloud.fields[entry.key];
                    saved[entry.key] = type == 'boolean'
                        ? entry.value == 'true'
                        : type == 'port' || type == 'interval'
                        ? int.parse(entry.value)
                        : entry.value;
                  }
                }
              }
              return http.Response('{"updated":true}', 200);
            }
            return http.Response(jsonEncode(saved), 200);
          }),
        );
        await api.status();
        final session = DeviceSettingsSession(api, authorized: () => true);
        await session.load(SettingsGroup.cloud);
        session.set(SettingsGroup.cloud, 'mqtt_user', 'new-user');
        session.mqttPassword.mode = SecretMode.clear;
        session.websocketToken.mode = SecretMode.replace;
        session.websocketToken.value = 'new-secret';
        if (behavior == 'external') saved['mqtt_user'] = 'remote-change';
        expect(await session.save(SettingsGroup.cloud), behavior == 'success');
        expect(posts, behavior == 'external' ? 0 : 1);
        if (posted != null) {
          expect(posted!['mqtt_password'], '');
          expect(posted!['websocket_token'], 'new-secret');
          expect(posted!.containsKey('mqtt_password_set'), false);
        }
        if (behavior == 'success') {
          expect(session.drafts[SettingsGroup.cloud]!.dirty, false);
          expect(session.websocketToken.value, '');
          expect(session.message, contains('verified'));
        } else {
          expect(
            session.drafts[SettingsGroup.cloud]!.value['mqtt_user'],
            'new-user',
          );
          expect(session.message, contains('not retried'));
        }
        session.dispose();
        await api.close();
      },
    );
  }
  test('Keep omits secret keys; Replace/Clear and network open choice have distinct wire semantics', () async {
    final api = PakarApi(
      client: MockClient(
        (request) async => http.Response(
          jsonEncode(
            request.url.path.endsWith('/network')
                ? simulatedNetwork
                : request.url.path.endsWith('/cloud')
                ? simulatedCloud
                : peerFixture().json,
          ),
          200,
        ),
      ),
    );
    final session = DeviceSettingsSession(api, authorized: () => true);
    await session.load(SettingsGroup.network);
    await session.load(SettingsGroup.cloud);
    final keep = session.write(SettingsGroup.cloud);
    expect(keep.form.containsKey('mqtt_password'), false);
    expect(keep.form.containsKey('websocket_token'), false);
    session.mqttPassword.mode = SecretMode.replace;
    expect(() => session.write(SettingsGroup.cloud), throwsFormatException);
    expect(() => session.write(SettingsGroup.network), throwsFormatException);
    session.openNetwork = true;
    expect(session.write(SettingsGroup.network).form['password'], '');
    session.openNetwork = false;
    session.wifiPassword = 'fixture-password';
    expect(
      session.write(SettingsGroup.network).form['password'],
      'fixture-password',
    );
    session.invalidate();
    expect(session.drafts, isEmpty);
    expect(session.wifiPassword, '');
    expect(session.mqttPassword.mode, SecretMode.keep);
    session.dispose();
    await api.close();
  });
  test(
    'Late read cannot overwrite a newer read, disposed owner or write epoch',
    () async {
      final replies = <Completer<http.Response>>[];
      final api = PakarApi(
        client: MockClient((_) {
          final reply = Completer<http.Response>();
          replies.add(reply);
          return reply.future;
        }),
      );
      final session = DeviceSettingsSession(api, authorized: () => true);
      final first = session.load(SettingsGroup.cloud),
          second = session.load(SettingsGroup.cloud);
      await Future<void>.delayed(Duration.zero);
      replies[1].complete(
        http.Response(
          jsonEncode({...simulatedCloud, 'mqtt_user': 'newest'}),
          200,
        ),
      );
      await second;
      replies[0].complete(http.Response(jsonEncode(simulatedCloud), 200));
      await first;
      expect(session.drafts[SettingsGroup.cloud]!.value['mqtt_user'], 'newest');
      final obsolete = session.load(SettingsGroup.cloud);
      await Future<void>.delayed(Duration.zero);
      session.dispose();
      replies[2].complete(http.Response(jsonEncode(simulatedCloud), 200));
      await obsolete;
      expect(session.drafts, isEmpty);
      await api.close();
    },
  );
  test('Changed owner or lost ACK cannot dispatch a previously authorized settings POST', () async {
    var authorized = true, posts = 0;
    final preflight = Completer<http.Response>();
    final api = PakarApi(
      client: MockClient((request) async {
        if (request.method == 'POST') {
          posts++;
          return http.Response('{}', 200);
        }
        return preflight.future;
      }),
    );
    final action = api.postJson('/api/cloud', {
      'websocket_token': 'private',
    }, authorize: () => authorized);
    final check = expectLater(action, throwsA(isA<StaleConnection>()));
    authorized = false;
    preflight.complete(http.Response(jsonEncode(peerFixture().json), 200));
    await check;
    expect(posts, 0);
    await api.close();
  });
}
