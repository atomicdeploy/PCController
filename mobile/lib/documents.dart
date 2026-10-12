import 'package:flutter/services.dart';

/// Actual Pealayer preferenceFileAllowed rule, adapted without path guessing.
bool preferenceFileAllowed(String name, Iterable<String> extensions) =>
    extensions.any(
      (extension) => name.toLowerCase().endsWith('.${extension.toLowerCase()}'),
    );

class FirmwareDocument {
  const FirmwareDocument(this.name, this.bytes);
  final String name;
  final Uint8List bytes;
}

/// Pealayer picker selection/cancellation contract delegated to Android SAF.
/// Only a user-created document token can be written, never an arbitrary URI.
class AndroidDocuments {
  static const channel = MethodChannel('ir.digitalogic.pccontroller/documents');
  Future<FirmwareDocument?> pick({
    int maximum = 2 * 1024 * 1024,
    List<String> extensions = const ['bin'],
  }) async {
    final value = await channel.invokeMapMethod<String, dynamic>('pick', {
      'maximum': maximum,
    });
    if (value == null) return null;
    final name = value['name'], bytes = value['bytes'];
    if (name is! String ||
        name.isEmpty ||
        name.length > 255 ||
        RegExp(r'[\x00-\x1f\x7f/\\]').hasMatch(name) ||
        !preferenceFileAllowed(name, extensions) ||
        bytes is! Uint8List ||
        bytes.isEmpty ||
        bytes.length > maximum) {
      throw const FormatException(
        'Select a bounded firmware document; filename alone does not authenticate firmware',
      );
    }
    return FirmwareDocument(name, bytes);
  }

  Future<bool> export(
    String name,
    Uint8List bytes, {
    required bool Function() stillOwned,
  }) async {
    if (!stillOwned()) {
      throw const FormatException('Board changed before export');
    }
    final token = await channel.invokeMethod<String>('create', {'name': name});
    if (token == null) return false;
    if (!stillOwned()) {
      await channel.invokeMethod<void>('abandon', {'token': token});
      throw const FormatException(
        'Board changed while choosing the destination; a new empty document may remain, but no backup was written',
      );
    }
    // Native consumes the one-use token even on failure. Never replay a write.
    await channel.invokeMethod<void>('write', {'token': token, 'bytes': bytes});
    return true;
  }
}
