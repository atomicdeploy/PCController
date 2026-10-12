import 'package:flutter_secure_storage/flutter_secure_storage.dart';
import 'package:shared_preferences/shared_preferences.dart';

import 'api.dart';

abstract interface class SecretStore {
  Future<String?> read();
  Future<void> write(String value);
}

class PlatformSecretStore implements SecretStore {
  const PlatformSecretStore();
  static const storage = FlutterSecureStorage();
  static const key = 'pakarcontroller.device_password';
  @override
  Future<String?> read() => storage.read(key: key);
  @override
  Future<void> write(String value) => storage.write(key: key, value: value);
}

class ConnectionPreferences {
  ConnectionPreferences(this.preferences, {SecretStore? secrets})
    : secrets = secrets ?? const PlatformSecretStore();
  final SharedPreferences preferences;
  final SecretStore secrets;

  Future<void> restore(PakarApi api) async {
    api.baseUrl = PakarApi.normalizeEndpoint(
      preferences.getString('endpoint') ?? 'http://ledboard.local',
    );
    api.user = preferences.getString('user') ?? 'admin';
    var password = await secrets.read();
    final legacy = preferences.getString('password');
    if (password == null && legacy != null) {
      await secrets.write(
        legacy,
      ); // Keep the old value if secure storage fails.
      password = legacy;
    }
    if (legacy != null && password != null) {
      await preferences.remove('password');
    }
    api.password = password ?? 'pakarcontroller';
    api.resetTransport();
  }

  Future<void> save(String endpoint, String user, String password) async {
    final normalized = PakarApi.normalizeEndpoint(endpoint);
    if (user.trim().isEmpty || password.isEmpty) {
      throw const FormatException('Username and password are required');
    }
    await secrets.write(password); // Never fall back to plaintext persistence.
    await preferences.setString('endpoint', normalized);
    await preferences.setString('user', user.trim());
    await preferences.remove('password');
  }
}
