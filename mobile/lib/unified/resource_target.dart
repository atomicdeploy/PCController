import 'endpoint_address.dart';

/// Resource paths retain their path/query contract; only URL authority input
/// receives the localized endpoint character policy.
String normalizeMediaTarget(String input) {
  final value = input.trim();
  if (value.isEmpty) {
    throw const FormatException('Enter a host path or media URL.');
  }
  if (RegExp(r'[\x00-\x1f\x7f]').hasMatch(value)) {
    throw const FormatException('Remove control characters from the target.');
  }
  final separator = value.indexOf('://');
  if (separator < 0) {
    if (RegExp(
      r'^(https?|rtsps?|rtmps?|ftp):',
      caseSensitive: false,
    ).hasMatch(value)) {
      throw const FormatException('Use a complete media URL with ://.');
    }
    return value; // Native host paths, including Windows/UNC.
  }
  final scheme = value.substring(0, separator).toLowerCase();
  final tail = value.substring(separator + 3);
  final split = tail.indexOf(RegExp(r'[/#?]'));
  final authority = normalizeAddressCharacters(
    split < 0 ? tail : tail.substring(0, split),
  );
  final suffix = split < 0 ? '' : tail.substring(split);
  final candidate = '$scheme://$authority$suffix';
  final uri = Uri.tryParse(candidate);
  if (uri == null ||
      (scheme != 'file' && uri.host.isEmpty) ||
      RegExp(r'\s').hasMatch(authority) ||
      uri.userInfo.isNotEmpty) {
    throw const FormatException(
      'Enter a valid media URL without embedded credentials.',
    );
  }
  if (uri.hasPort && (uri.port < 1 || uri.port > 65535)) {
    throw const FormatException('Port must be between 1 and 65535.');
  }
  if (scheme == 'file' && uri.path.isEmpty) {
    throw const FormatException('Enter a file path.');
  }
  return uri.toString();
}

String? mediaTargetError(String value) {
  if (value.trim().isEmpty) return null;
  try {
    normalizeMediaTarget(value);
    return null;
  } on FormatException catch (error) {
    return error.message;
  }
}
