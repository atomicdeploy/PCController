import 'dart:convert';
import 'dart:math';

import 'package:flutter_secure_storage/flutter_secure_storage.dart';
import 'package:shared_preferences/shared_preferences.dart';

import 'api.dart';

enum DeviceLocation { local, remote }

/// Saved transport intent is NOT a discovered/acknowledged board capability.
class SavedDevice {
  SavedDevice({
    required this.id,
    required this.name,
    required String endpoint,
    required this.location,
    this.user = 'admin',
  }) : endpoint = PakarApi.normalizeEndpoint(endpoint) {
    if (!RegExp(r'^[a-z0-9-]{1,64}$').hasMatch(id) ||
        name.trim().isEmpty ||
        name.length > 96 ||
        user.length > 96) {
      throw const FormatException('Invalid saved device');
    }
  }
  final String id, name, endpoint, user;
  final DeviceLocation location;
  Map<String, dynamic> toJson() => {
    'id': id,
    'name': name,
    'endpoint': endpoint,
    'location': location.name,
    'user': user,
  };
  factory SavedDevice.fromJson(Map<String, dynamic> value) => SavedDevice(
    id: value['id'] as String,
    name: value['name'] as String,
    endpoint: value['endpoint'] as String,
    user: value['user'] as String,
    location: DeviceLocation.values.byName(value['location'] as String),
  );
}

String randomIdentifier() => List.generate(
  16,
  (_) => Random.secure().nextInt(256).toRadixString(16).padLeft(2, '0'),
).join();

class DeviceRegistry {
  static const key = 'pakar.devices';
  static const vault = FlutterSecureStorage();
  Future<List<SavedDevice>> load() async {
    final prefs = await SharedPreferences.getInstance();
    final raw = jsonDecode(prefs.getString(key) ?? '[]');
    if (raw is! List || raw.length > 64) {
      throw const FormatException('Invalid saved device registry');
    }
    final devices = raw
        .map((v) => SavedDevice.fromJson(Map<String, dynamic>.from(v as Map)))
        .toList();
    if (devices.map((d) => d.id).toSet().length != devices.length) {
      throw const FormatException('Duplicate saved device identity');
    }
    return devices;
  }

  Future<void> save(SavedDevice device, String password) async {
    if (password.isEmpty) {
      throw const FormatException('Device password required');
    }
    final list = await load();
    final i = list.indexWhere((d) => d.id == device.id);
    if (i < 0 && list.length >= 64) {
      throw const FormatException('Device limit reached');
    }
    // Never write secrets to preferences, backups, discoveries or diagnostics.
    await vault.write(key: 'pakar.device.${device.id}', value: password);
    if (i < 0) {
      list.add(device);
    } else {
      list[i] = device;
    }
    final prefs = await SharedPreferences.getInstance();
    if (!await prefs.setString(
      key,
      jsonEncode(list.map((d) => d.toJson()).toList()),
    )) {
      throw StateError('Device metadata was not saved');
    }
  }

  Future<String?> password(SavedDevice device) =>
      vault.read(key: 'pakar.device.${device.id}');
  Future<void> remove(SavedDevice device) async {
    final list = await load();
    list.removeWhere((d) => d.id == device.id);
    final prefs = await SharedPreferences.getInstance();
    if (!await prefs.setString(
      key,
      jsonEncode(list.map((d) => d.toJson()).toList()),
    )) {
      throw StateError('Device was not removed');
    }
    await vault.delete(key: 'pakar.device.${device.id}');
  }
}
