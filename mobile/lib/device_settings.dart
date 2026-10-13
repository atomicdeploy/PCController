import 'dart:convert';

import 'package:flutter/foundation.dart';

import 'api.dart';
import 'settings_catalog.dart';

enum SettingsGroup { network, cloud }

extension SettingsGroupContract on SettingsGroup {
  String get path => '/api/$name';
  String get acknowledgment =>
      this == SettingsGroup.network ? 'accepted' : 'updated';
  Map<String, String> get fields =>
      (deviceSettingsCatalog[name] as Map).cast<String, String>();
}

void settingsBounded(String text, int maximum, String label) {
  if (utf8.encode(text).length > maximum ||
      RegExp(r'[\x00-\x1f\x7f-\x9f]').hasMatch(text)) {
    throw FormatException(
      '$label exceeds its UTF-8 limit or contains controls',
    );
  }
}

/// Actual native Network/Cloud validation plus the shared Web field catalog.
/// Required fields never fall back to placeholder defaults or coerced values.
Map<String, Object> settingsSnapshot(
  SettingsGroup group,
  Map<String, dynamic> json,
) {
  final result = <String, Object>{};
  for (final entry in group.fields.entries) {
    final v = json[entry.key];
    final valid = switch (entry.value) {
      'boolean' => v is bool,
      'string' => v is String,
      'port' => v is int && v >= 1 && v <= 65535,
      'interval' => v is int && v >= 1 && v <= 3600,
      _ => false,
    };
    if (!valid) throw FormatException('Missing or invalid ${entry.key}');
    result[entry.key] = v as Object;
  }
  if (group == SettingsGroup.network) {
    settingsBounded(result['station_ssid'] as String, 32, 'SSID');
    if (result['station_enabled'] == false && result['ap_enabled'] == false) {
      throw const FormatException('Keep station or recovery AP enabled');
    }
    if (result['station_enabled'] == true && result['station_ssid'] == '') {
      throw const FormatException('Enter the station SSID');
    }
  } else {
    for (final prefix in ['mqtt', 'websocket']) {
      final host = result['${prefix}_host'] as String;
      settingsBounded(host, 63, 'Host');
      if ((result['${prefix}_enabled'] == true && host.isEmpty) ||
          !RegExp(r'^[A-Za-z0-9.-]*$').hasMatch(host)) {
        throw const FormatException('Enter a hostname without scheme or path');
      }
    }
    for (final entry in {
      'mqtt_user': 32,
      'mqtt_base_topic': 64,
      'websocket_path': 96,
    }.entries) {
      final text = result[entry.key] as String;
      settingsBounded(text, entry.value, entry.key);
      if (text.contains('"') || text.contains(r'\')) {
        throw const FormatException(
          'Current public settings do not support quotes or backslashes',
        );
      }
    }
    if ((result['mqtt_base_topic'] as String).isEmpty ||
        RegExp('[+#]').hasMatch(result['mqtt_base_topic'] as String)) {
      throw const FormatException(
        'MQTT topic must be nonempty without wildcards',
      );
    }
    if (!(result['websocket_path'] as String).startsWith('/')) {
      throw const FormatException('WebSocket path must start with /');
    }
  }
  return result;
}

enum SecretMode { keep, replace, clear }

class SecretEdit {
  SecretMode mode = SecretMode.keep;
  String value = '';
  String? wireValue() {
    if (mode == SecretMode.keep) return null;
    if (mode == SecretMode.clear) return '';
    settingsBounded(value, 64, 'Secret');
    if (value.isEmpty) {
      throw const FormatException(
        'Enter a replacement secret or explicitly choose Clear',
      );
    }
    return value;
  }

  bool presence(bool saved) =>
      mode == SecretMode.keep ? saved : mode == SecretMode.replace;
  void clear() {
    mode = SecretMode.keep;
    value = '';
  }
}

/// Adapted actual desktop Draft.observe; dirty drafts are not overwritten.
class SettingsDraft {
  SettingsDraft(Map<String, Object> initial)
    : saved = Map.of(initial),
      value = Map.of(initial);
  Map<String, Object> saved, value;
  bool conflict = false;
  bool get dirty => !mapEquals(saved, value);
  void observe(Map<String, Object> next, {bool secretsDirty = false}) {
    if (!dirty && !secretsDirty) {
      saved = Map.of(next);
      value = Map.of(next);
    } else if (!mapEquals(saved, next)) {
      conflict = true;
    }
  }
}

class SettingsWrite {
  SettingsWrite(this.group, this.form, this.expected);
  final SettingsGroup group;
  final Map<String, String> form;
  final Map<String, Object> expected;
  void verifyAck(Map<String, dynamic> json) {
    if (json[group.acknowledgment] != true) {
      throw const FormatException(
        'Device did not confirm a saved configuration write',
      );
    }
  }

  void verifyReadback(Map<String, Object> actual) {
    if (!mapEquals(expected, actual)) {
      throw const FormatException(
        'Write accepted but readback differs. Refresh before deciding to retry; no write replayed.',
      );
    }
  }
}

/// Selected-peer owner and request epochs adapt actual Web settingsSave and
/// native Editor pending/finish. Contains in-memory drafts; no disk export.
class DeviceSettingsSession extends ChangeNotifier {
  DeviceSettingsSession(this.api, {required this.authorized})
    : owner = api.connectionRevision;
  final PakarApi api;
  final bool Function() authorized;
  final int owner;
  final drafts = <SettingsGroup, SettingsDraft>{};
  final _reads = <SettingsGroup, int>{};
  final mqttPassword = SecretEdit(), websocketToken = SecretEdit();
  String wifiPassword = '',
      message = 'Read the actual saved device settings first';
  bool openNetwork = false, pending = false, disposed = false;
  int epoch = 0;
  bool get live => !disposed && owner == api.connectionRevision && authorized();
  bool secretsDirty(SettingsGroup group) => group == SettingsGroup.network
      ? wifiPassword.isNotEmpty || openNetwork
      : mqttPassword.mode != SecretMode.keep ||
            websocketToken.mode != SecretMode.keep;
  bool get dirty =>
      drafts.entries.any((e) => e.value.dirty || secretsDirty(e.key));
  void check() {
    if (!live) throw const StaleConnection();
  }

  void changed() {
    if (!disposed) notifyListeners();
  }

  void set(SettingsGroup group, String key, Object value) {
    check();
    if (pending) return;
    drafts[group]?.value[key] = value;
    changed();
  }

  void clearSecrets() {
    wifiPassword = '';
    openNetwork = false;
    mqttPassword.clear();
    websocketToken.clear();
  }

  void invalidate() {
    ++epoch;
    drafts.clear();
    clearSecrets();
    pending = false;
    message = 'Device context lost; read this peer again';
    changed();
  }

  Future<void> load(SettingsGroup group, {bool discard = false}) async {
    check();
    if (pending) return;
    final request = (_reads[group] ?? 0) + 1, captured = epoch;
    _reads[group] = request;
    try {
      final next = settingsSnapshot(group, await api.getJson(group.path));
      check();
      if (captured != epoch || request != _reads[group] || pending) return;
      if (discard || drafts[group] == null) {
        drafts[group] = SettingsDraft(next);
        if (group == SettingsGroup.network) {
          wifiPassword = '';
          openNetwork = false;
        } else {
          mqttPassword.clear();
          websocketToken.clear();
        }
      } else {
        drafts[group]!.observe(next, secretsDirty: secretsDirty(group));
      }
      message = drafts[group]!.conflict
          ? 'Another client changed these settings. Draft retained; discard and reload before saving.'
          : 'Observed saved public device settings';
    } catch (error) {
      if (live && captured == epoch && request == _reads[group]) {
        message = error.toString();
      }
    }
    changed();
  }

  SettingsWrite write(SettingsGroup group) {
    check();
    final draft = drafts[group];
    if (draft == null || draft.conflict) {
      throw const FormatException(
        'Reload the current saved settings before saving',
      );
    }
    final expected = settingsSnapshot(group, draft.value);
    final form = <String, String>{
      for (final entry in expected.entries) entry.key: '${entry.value}',
    };
    if (group == SettingsGroup.network) {
      settingsBounded(wifiPassword, 63, 'Wi-Fi password');
      if (expected['station_enabled'] == true &&
          !openNetwork &&
          utf8.encode(wifiPassword).length < 8) {
        throw const FormatException(
          'Enter an 8–63 byte Wi-Fi password or explicitly select an open network',
        );
      }
      form['ssid'] = form.remove('station_ssid')!;
      form['password'] = openNetwork ? '' : wifiPassword;
    } else {
      for (final entry in {
        'mqtt_password': mqttPassword,
        'websocket_token': websocketToken,
      }.entries) {
        final key = '${entry.key}_set', value = entry.value.wireValue();
        if (value != null) form[entry.key] = value;
        expected[key] = entry.value.presence(expected[key] as bool);
        form.remove(key);
      }
    }
    return SettingsWrite(group, form, expected);
  }

  Future<bool> save(SettingsGroup group) async {
    if (pending) return false;
    late final SettingsWrite request;
    try {
      request = write(group);
    } catch (error) {
      message = error.toString();
      changed();
      return false;
    }
    final captured = ++epoch;
    pending = true;
    message = 'Waiting for acknowledgment and saved-settings readback…';
    changed();
    try {
      final current = settingsSnapshot(group, await api.getJson(group.path));
      check();
      if (captured != epoch) throw const StaleConnection();
      if (!mapEquals(current, drafts[group]!.saved)) {
        drafts[group]!.conflict = true;
        throw const FormatException(
          'Another client changed these settings. Discard and reload before saving.',
        );
      }
      final ack = await api.postJson(
        group.path,
        request.form,
        authorize: () => live && captured == epoch,
      );
      check();
      if (captured != epoch) throw const StaleConnection();
      request.verifyAck(ack);
      final actual = settingsSnapshot(group, await api.getJson(group.path));
      check();
      if (captured != epoch) throw const StaleConnection();
      request.verifyReadback(actual);
      drafts[group] = SettingsDraft(actual);
      if (group == SettingsGroup.network) {
        wifiPassword = '';
        openNetwork = false;
      } else {
        mqttPassword.clear();
        websocketToken.clear();
      }
      message = 'Saved public settings verified by device readback. Secrets remain write-only.';
      return true;
    } catch (error) {
      if (live && captured == epoch) {
        message = '$error Uncertain writes are not retried.';
      }
      return false;
    } finally {
      if (!disposed && captured == epoch) {
        pending = false;
        changed();
      }
    }
  }

  @override
  void dispose() {
    ++epoch;
    disposed = true;
    drafts.clear();
    clearSecrets();
    super.dispose();
  }
}
