import 'dart:convert';
import 'dart:typed_data';

import 'api.dart';

// Fully read canonical desktop/src/maintenance.rs and actual firmware routes.
const firmwarePhases = {
  'idle',
  'manifest',
  'download_esp',
  'download_avr',
  'ready',
  'prepare',
  'program_avr',
  'program_esp',
  'awaiting_reboot',
  'verify_pair',
  'release_pair',
  'complete',
  'failed',
  'cancelled',
};
bool firmwareHash(String value) => RegExp(r'^[0-9a-f]{64}$').hasMatch(value);

class RemoteJob {
  RemoteJob._(this.json);
  final Map<String, dynamic> json;
  int get id => json['job_id'];
  String get boot => json['boot_id'];
  String get hash => json['manifest_sha256'];
  String get phase => json['phase'];
  int get targets => json['targets'];
  bool get busy => json['busy'];
  bool get cancellable => json['cancellable'];
  int get received => json['received_bytes'];
  int get expected => json['expected_bytes'];
  String get error => json['error'];
  bool get canApply => id > 0 && phase == 'ready' && !busy;
  bool get canCancel =>
      id > 0 &&
      (phase == 'ready' || cancellable) &&
      {'manifest', 'download_esp', 'download_avr', 'ready'}.contains(phase);
  bool sameIdentity(RemoteJob other) =>
      id == other.id &&
      boot == other.boot &&
      hash == other.hash &&
      targets == other.targets;
  static RemoteJob parse(Map<String, dynamic> value) {
    bool uint(String field) =>
        value[field] is int && value[field] >= 0 && value[field] <= 0xffffffff;
    if (!uint('job_id') ||
        !uint('received_bytes') ||
        !uint('expected_bytes') ||
        value['targets'] is! int ||
        value['targets'] < 1 ||
        value['targets'] > 3 ||
        value['busy'] is! bool ||
        value['cancellable'] is! bool ||
        value['boot_id'] is! String ||
        !RegExp(r'^[!-~]{1,80}$').hasMatch(value['boot_id']) ||
        value['phase'] is! String ||
        !firmwarePhases.contains(value['phase']) ||
        value['manifest_sha256'] is! String ||
        (value['job_id'] > 0 && !firmwareHash(value['manifest_sha256'])) ||
        value['error'] is! String ||
        utf8.encode(value['error']).length > 1024 ||
        value['received_bytes'] > value['expected_bytes']) {
      throw const FormatException(
        'Malformed remote firmware job; no editable defaults',
      );
    }
    return RemoteJob._(Map.unmodifiable(value));
  }

  Map<String, String> get identityForm => {
    'job_id': '$id',
    'boot_id': boot,
    'sha256': hash,
  };
}

void validateRemote(String url, String hash, int targets, String bearer) {
  final uri = Uri.tryParse(url);
  if (url.length > 383 ||
      uri == null ||
      uri.scheme != 'https' ||
      uri.host.isEmpty ||
      uri.userInfo.isNotEmpty ||
      uri.hasFragment ||
      RegExp(r'[\x00-\x20\x7f-\uffff\\]').hasMatch(url) ||
      !firmwareHash(hash) ||
      targets < 1 ||
      targets > 3 ||
      bearer.length > 256 ||
      RegExp(r'[^!-~]').hasMatch(bearer)) {
    throw const FormatException(
      'Use a direct HTTPS URL, lowercase manifest SHA-256 and explicit board target; bearer must be printable ASCII without spaces',
    );
  }
}

void validateFirmwareImage(bool esp, Uint8List bytes) {
  if (esp
      ? bytes.length < 16 ||
            bytes.length > 2 * 1024 * 1024 ||
            bytes.first != 0xe9
      : bytes.length != 32768) {
    throw FormatException(
      esp ? 'Use an uncompressed ESP8266 E9 .bin, 16 bytes–2 MiB' : 'Use exactly 32768 raw AVR bytes including the intended bootloader; not partial application or HEX',
    );
  }
}

class FirmwareController {
  FirmwareController(this.api, {required this.authorized})
    : owner = api.connectionRevision;
  final PakarApi api;
  final int owner;
  final bool Function(String feature) authorized;
  bool disposed = false;
  void check(String feature) {
    if (disposed || owner != api.connectionRevision || !authorized(feature)) {
      throw const StaleConnection();
    }
  }

  bool allowed(String feature) =>
      !disposed && owner == api.connectionRevision && authorized(feature);
  bool allowedTargets(int targets) =>
      targets >= 1 &&
      targets <= 3 &&
      ((targets & 1) == 0 || allowed('ota')) &&
      ((targets & 2) == 0 || allowed('isp'));
  void checkTargets(int targets) {
    if (!allowedTargets(targets)) throw const StaleConnection();
  }

  Future<RemoteJob> status() async {
    if (!allowed('isp') && !allowed('ota')) throw const StaleConnection();
    final value = await api.maintenanceJson(
      '/api/firmware/remote',
      authorize: () => allowed('isp') || allowed('ota'),
    );
    if (!allowed('isp') && !allowed('ota')) {
      throw const StaleConnection();
    }
    return RemoteJob.parse(value);
  }

  Future<RemoteJob> stage(
    String url,
    String hash,
    int targets,
    String bearer, {
    bool allowDowngrade = false,
  }) async {
    validateRemote(url, hash, targets, bearer);
    checkTargets(targets);
    final ack = RemoteJob.parse(
      await api.maintenanceJson(
        '/api/firmware/fetch',
        post: true,
        form: {
          'url': url,
          'sha256': hash,
          'targets': '$targets',
          'apply': 'false',
          'bearer': bearer,
          'allow_downgrade': '$allowDowngrade',
        },
        authorize: () => allowedTargets(targets),
      ),
    );
    checkTargets(targets);
    if (ack.id == 0 ||
        ack.hash != hash ||
        ack.targets != targets ||
        ack.phase != 'manifest') {
      throw const FormatException(
        'Staging reply does not acknowledge this pinned request; no operation replayed',
      );
    }
    return ack;
  }

  Future<RemoteJob> act(RemoteJob expected, {required bool apply}) async {
    checkTargets(expected.targets);
    if (apply ? !expected.canApply : !expected.canCancel) {
      throw const FormatException(
        'Observed job cannot perform this action; refresh first',
      );
    }
    final fresh = await status();
    checkTargets(expected.targets);
    if (!fresh.sameIdentity(expected) ||
        (apply ? !fresh.canApply : !fresh.canCancel)) {
      throw const FormatException(
        'Job changed while confirmation was open; no mutation sent',
      );
    }
    final ack = RemoteJob.parse(
      await api.maintenanceJson(
        apply ? '/api/firmware/apply' : '/api/firmware/cancel',
        post: true,
        form: expected.identityForm,
        authorize: () => allowedTargets(expected.targets),
      ),
    );
    checkTargets(expected.targets);
    if (!ack.sameIdentity(expected) ||
        ack.phase != (apply ? 'prepare' : 'cancelled')) {
      throw const FormatException(
        'Mutation reply did not match this exact job; inspect before any new request',
      );
    }
    return ack;
  }

  Future<String> manual(String action, {Uint8List? bytes}) async {
    final esp = action == 'update';
    check(esp ? 'ota' : 'isp');
    // Do not overwrite or interfere with another client's ready pinned job.
    final remote = await status();
    check(esp ? 'ota' : 'isp');
    if (remote.busy || remote.phase == 'ready') {
      throw const FormatException(
        'Remote job owns maintenance; inspect/cancel it before a manual operation',
      );
    }
    final path = switch (action) {
      'probe' => '/api/avr/probe',
      'backup' => '/api/avr/backup',
      'program' => '/api/avr/program',
      'stage' => '/api/avr/image',
      'update' => '/update',
      _ => throw const FormatException('Unknown maintenance action'),
    };
    if (action == 'stage' || esp) {
      if (bytes == null) {
        throw const FormatException('Select a complete binary first');
      }
      validateFirmwareImage(esp, bytes);
      if (!esp) {
        final ack = await api.maintenanceBytes(
          path,
          post: true,
          upload: bytes,
          uploadField: 'image',
          authorize: () => allowed('isp'),
        );
        check('isp');
        if (utf8.decode(ack).trim() != 'AVR image stored') {
          throw const FormatException(
            'AVR staging acknowledgment unavailable; shared image state unconfirmed',
          );
        }
        return 'AVR stored, NOT programmed. Shared staging has no advertised hash; confirm provenance before programming.';
      }
    }
    final value = await api.maintenanceJson(
      path,
      post: action != 'probe',
      mayMutate: true,
      upload: esp ? bytes : null,
      uploadField: esp ? 'firmware' : null,
      authorize: () => allowed(esp ? 'ota' : 'isp'),
    );
    check(esp ? 'ota' : 'isp');
    final success = switch (action) {
      'probe' => value['signature'] == '1E950F',
      'backup' => value['backed_up'] == true,
      'program' =>
        value['programmed'] == true &&
            value['verified'] == true &&
            value['eeprom_preserved'] == true,
      'update' => value['updated'] == true && value['rebooting'] == true,
      _ => false,
    };
    if (!success) {
      throw const FormatException(
        'Device did not confirm the expected maintenance result; nothing replayed',
      );
    }
    return switch (action) {
      'probe' => 'ATmega328P ISP signature confirmed: 1E950F',
      'backup' =>
        'Device confirmed full flash and EEPROM backup in ESP storage',
      'program' =>
        'Device confirmed AVR flash verification and EEPROM preservation',
      'update' => 'ESP accepted update and scheduled reboot; reconnect to verify its running version',
      _ => '',
    };
  }

  Future<Uint8List> backupBytes(bool eeprom) async {
    check('isp');
    final size = eeprom ? 1024 : 32768;
    final bytes = await api.maintenanceBytes(
      '/api/avr/backup/${eeprom ? 'eeprom' : 'flash'}',
      maximum: size,
      authorize: () => allowed('isp'),
    );
    check('isp');
    if (bytes.length != size) {
      throw const FormatException(
        'Backup length differs; no export file created',
      );
    }
    return bytes;
  }

  Future<Uint8List> espBytes() async {
    check('ota');
    final bytes = await api.maintenanceBytes(
      '/firmware/download',
      maximum: 2 * 1024 * 1024,
      verifySketch: true,
      authorize: () => allowed('ota'),
    );
    check('ota');
    validateFirmwareImage(true, bytes);
    return bytes;
  }

  void dispose() {
    disposed = true;
  }
}
