import 'dart:convert';

import 'package:flutter/foundation.dart';

import 'transport.dart';

class GoCommand {
  const GoCommand(
    this.name,
    this.usage,
    this.summary,
    this.group,
    this.aliases,
  );
  final String name, usage, summary, group;
  final List<String> aliases;

  static List<GoCommand> catalog(dynamic value) {
    if (value is! List || value.length > 256) {
      throw const Rejected('Invalid command catalog.');
    }
    final result = <GoCommand>[];
    final names = <String>{};
    bool identifier(dynamic value) =>
        value is String && RegExp(r'^[a-z][a-z0-9-]{0,63}$').hasMatch(value);
    for (final entry in value) {
      if (entry is! Map ||
          !identifier(entry['name']) ||
          entry['usage'] is! String ||
          entry['summary'] is! String ||
          (entry['usage'] as String).length > 16384 ||
          (entry['summary'] as String).length > 2048 ||
          !names.add(entry['name'])) {
        throw const Rejected('Invalid command catalog.');
      }
      // Console lifecycle belongs to this Flutter app, not the remote primary.
      if (['clear', 'quit'].contains(entry['name'])) continue;
      final aliases = entry['aliases'];
      if (aliases != null &&
          (aliases is! List ||
              aliases.length > 16 ||
              aliases.any((v) => !identifier(v)))) {
        throw const Rejected('Invalid command aliases.');
      }
      result.add(
        GoCommand(
          entry['name'],
          entry['usage'],
          entry['summary'],
          entry['group'] is String ? entry['group'] : 'Other',
          aliases == null ? const [] : List<String>.from(aliases),
        ),
      );
    }
    return result..sort((a, b) => a.name.compareTo(b.name));
  }

  bool matches(String input) =>
      '$name ${aliases.join(' ')} $usage $summary $group'
          .toLowerCase()
          .contains(input.toLowerCase().trim());
}

List<String> commandWords(String text) {
  if (text.trim().isEmpty ||
      text.length > 32768 ||
      RegExp(r'[\x00-\x1f\x7f]').hasMatch(text)) {
    throw const FormatException('Enter one command, up to 32768 characters.');
  }
  // Arguments are sent intact to Go's shell parser. Only classify plain leading
  // tokens; quoted/ambiguous forms conservatively require confirmation.
  return text.trim().toLowerCase().split(RegExp(r'\s+'));
}

bool commandIsRead(String text) {
  final words = commandWords(text);
  final first = words.first, second = words.length > 1 ? words[1] : '';
  if (words.length == 1 &&
      [
        'ports',
        'port-process',
        'port-owner',
        'hello',
        'status',
        'st',
        'voltage',
        'current',
        'settings',
        'program-state',
        'run-state',
        'temp',
        'temperature',
      ].contains(first)) {
    return true;
  }
  if (first == 'config') return second == 'get' && words.length == 3;
  if (first == 'os') {
    return ['status', 'facts', 'policy'].contains(second) ||
        (second == 'brightness' && words.length == 3 && words[2] == 'get');
  }
  if (first == 'settings') return words.length == 2 && second == 'export-live';
  if (first == 'temp' || first == 'temperature') return second == 'list';
  if (first == 'buzzer' || first == 'estop') {
    return words.length == 2 && second == 'status';
  }
  if (first == 'silent') return words.last == 'status' && words.length <= 3;
  if (first == 'pwm') return words.length == 2 && second == 'get';
  if (first == 'menu') {
    return ['list', 'current'].contains(second) ||
        (second == 'layout' && words.length == 2);
  }
  if (first == 'melody' || first == 'effect') {
    return [
      'list',
      'catalog',
      'status',
      'inspect',
      'show',
      'properties',
    ].contains(second);
  }
  if (first == 'macro') {
    return ['list', 'status', 'monitor', 'show'].contains(second) ||
        (second == 'record' && words.length == 3 && words[2] == 'status');
  }
  if (first == 'rgb') {
    return words.length == 3 &&
        second == 'effect' &&
        ['list', 'status'].contains(words[2]);
  }
  if (first == 'strip') {
    return (words.length == 2 && second == 'status') ||
        (words.length == 3 && second == 'effect' && words[2] == 'list');
  }
  if (first == 'rf') return words.length == 2 && second == 'list';
  if ([
    'automation',
    'board-automation',
    'mcu-automation',
    'bridge',
  ].contains(first)) {
    return words.length == 2 && second == 'list';
  }
  if (first == 'webhook') return ['status', 'pending', 'dead'].contains(second);
  if (['hotkeys', 'keyboard'].contains(first)) {
    return ['status', 'list'].contains(second);
  }
  if (first == 'toolchain') {
    return words.length == 2 && ['profile', 'features'].contains(second);
  }
  if (first == 'board') {
    return words.length == 3 && second == 'name' && words[2] == 'get';
  }
  if (first == 'host-menu') {
    return ['list', 'directory', 'content', 'status'].contains(second);
  }
  return false;
}

bool commandIsStop(String text) => const {
  'estop on',
  'relay off',
  'pwm off',
  'melody stop',
  'strip stop',
  'effect cancel',
  'macro cancel',
  'macro cancel keep',
  'rf cancel',
}.contains(text.trim().toLowerCase());

bool commandNeedsBoard(String text) => !const {
  'config',
  'os',
  'audio',
  'ports',
  'port-process',
  'port-owner',
  'discover',
  'keyboard',
  'hotkeys',
  'bridge',
  'peer-update',
  'webhook',
  'automation',
  'program-state',
  'run-state',
  'toolchain',
  'driver',
  'message',
  'program',
  'boot',
  'open',
  'close',
  'reconnect',
}.contains(commandWords(text).first);

/// One explicit target binding for the entire tools activity, including dialogs.
/// Catalog/read admission does not authorize a mutation; native RPC policy is
/// always enforced by the selected primary. No retries or automatic scripts.
class GoCommandClient extends ChangeNotifier {
  GoCommandClient(this.session)
    : owner = session.targetStamp,
      peer = session.transport;
  final PeerSession session;
  final String owner;
  final PeerTransport? peer;
  List<GoCommand> commands = [];
  bool busy = false;
  bool _disposed = false;
  bool get current =>
      !_disposed &&
      session.fresh &&
      session.targetStamp == owner &&
      identical(session.transport, peer);
  void check({bool board = false, bool stop = false, bool mutation = false}) {
    if (!current || peer == null || (board && !session.boardReady)) {
      throw const Rejected('Connection changed. Reopen controller tools.');
    }
    if (mutation &&
        !stop &&
        object(session.state['emergency_stop'])['active'] == true) {
      throw const Rejected('Emergency stop is active.');
    }
  }

  void emit() {
    if (!_disposed) notifyListeners();
  }

  Future<void> load() async {
    check();
    final value = await peer!.rpc('controller.command.catalog');
    check();
    commands = GoCommand.catalog(value);
    emit();
  }

  bool supports(String name) =>
      commands.any((c) => c.name == name || c.aliases.contains(name));
  Future<dynamic> run(
    String command, {
    Future<bool> Function(String)? confirm,
    Future<void> Function()? preflight,
  }) async {
    final words = commandWords(command);
    if (!supports(words.first)) {
      throw const Rejected('Command is not advertised by this host.');
    }
    if (peer is ControllerRelayTransport &&
        ['bridge', 'peer-update'].contains(words.first)) {
      throw const Rejected(
        'Connect directly to PCController for third-peer operations.',
      );
    }
    final release = command.trim().toLowerCase() == 'estop off';
    if (release && confirm == null) {
      throw const Rejected('Confirm emergency-stop release.');
    }
    final read = commandIsRead(command), stop = commandIsStop(command);
    final board = commandNeedsBoard(command);
    check(board: board, stop: stop || release, mutation: !read);
    if (busy) throw const Rejected('A controller command is in progress.');
    int? pwmWrite;
    final epoch = session.generation;
    busy = true;
    emit();
    try {
      if (!read && !stop && confirm != null && !await confirm(command)) {
        return null;
      }
      if (preflight != null) await preflight();
      check(board: board, stop: stop || release, mutation: !read);
      if (!read && words.first == 'pwm') {
        pwmWrite = session.beginPwmCommand(owner);
      }
      final result = await peer!.rpc('controller.command.execute', {
        'command': command,
      });
      if (!current) {
        if (!read) throw const Uncertain();
        check();
      }
      return result;
    } finally {
      if (pwmWrite != null) session.finishPwmCommand(pwmWrite, epoch);
      busy = false;
      emit();
    }
  }

  Future<dynamic> rpc(
    String method,
    Json params, {
    bool board = true,
    bool mutation = true,
    bool stop = false,
  }) async {
    check(board: board, stop: stop, mutation: mutation);
    if (busy) throw const Rejected('A controller command is in progress.');
    busy = true;
    emit();
    try {
      final result = await peer!.rpc(method, params);
      if (!current) {
        if (mutation) throw const Uncertain();
        check();
      }
      return result;
    } finally {
      busy = false;
      emit();
    }
  }

  Future<dynamic> saveSettings(
    String command,
    String baseline, {
    Future<bool> Function(String)? confirm,
  }) => run(
    command,
    confirm: confirm,
    preflight: () async {
      check(board: true);
      final reply = object(
        await peer!.rpc('controller.command.execute', {
          'command': 'settings export-live',
        }),
      );
      check(board: true);
      final value = object(jsonDecode('${reply['output']}'));
      if (GoBoardSettings.fromJson(object(value['settings'])).command() !=
          baseline) {
        throw const Rejected(
          'Board settings changed. Refresh and review again.',
        );
      }
    },
  );
  Future<dynamic> emergencyStop() async {
    check(board: true, stop: true, mutation: true);
    final result = await peer!.rpc('controller.estop.set', {
      'active': true,
      'source': 'mobile-tools',
    });
    if (!current) throw const Uncertain();
    final state = object(result);
    if (state['active'] == true) {
      session.state = {...session.state, 'emergency_stop': state};
      session.notifyListeners();
    }
    return result;
  }

  @override
  void dispose() {
    _disposed = true;
    super.dispose();
  }
}

class GoDisplayOptions {
  String target = 'segments', text = '', repeat = 'once';
  int speedMs = 220, durationMs = 5000, intervalSeconds = 30;
  bool scroll = false;
  Json params() {
    if (!['segments', 'lcd', 'both'].contains(target) ||
        !['once', 'loop', 'interval'].contains(repeat) ||
        speedMs < 80 ||
        speedMs > 5000 ||
        durationMs < 80 ||
        durationMs > 65535 ||
        intervalSeconds < 1 ||
        intervalSeconds > 255 ||
        text.length > 40 ||
        text.codeUnits.any((v) => v < 32 || v > 126)) {
      throw const FormatException(
        'Use printable ASCII (40 characters), speed 80–5000 ms, duration 80–65535 ms and interval 1–255 seconds.',
      );
    }
    return {
      'target': target,
      'text': text,
      'speed_ms': speedMs,
      'duration_ms': durationMs,
      'repeat': repeat,
      'interval_ms': repeat == 'interval' ? intervalSeconds * 1000 : 0,
      'scroll': scroll,
    };
  }
}

class GoBoardSettings {
  GoBoardSettings._(this.values);
  final Json values;
  static const fields = <(String, String, int, int)>[
    ('light_mode', 'Light mode', 0, 2),
    ('on_brightness', 'Light ON brightness', 0, 255),
    ('off_brightness', 'Light OFF brightness', 0, 255),
    ('display_brightness', 'Display open brightness', 0, 7),
    ('display_closed_brightness', 'Display closed brightness', 0, 7),
    ('status_brightness', 'Status brightness', 0, 255),
    ('stream_period_ms', 'Status interval (ms)', 0, 65535),
    ('default_page', 'Default page', 0, 13),
    ('status_color', 'Status color', 0, 4),
    ('voltage_decimals', 'Voltage decimals', 0, 2),
    ('current_decimals', 'Current decimals', 0, 2),
    ('motion_exit_hold_seconds', 'Motion exit hold (s)', 1, 31),
    ('motion_break_ms', 'Motion dead time (ms)', 1, 255),
    ('relay_restore_mask', 'Relay restore mask', 0, 255),
  ];
  factory GoBoardSettings.fromJson(Json settings) {
    final v = Map<String, dynamic>.from(settings);
    final flags = v['extended_flags'];
    if (flags is! int ||
        flags < 0 ||
        flags > 255 ||
        v['flags'] is! int ||
        v['output_persistence'] is! int) {
      throw const FormatException(
        'Complete board settings required. Refresh settings.',
      );
    }
    int decimals(int shift) {
      final encoded = (flags >> shift) & 3;
      return encoded == 0 ? 2 : encoded - 1;
    }

    v.addAll({
      'save_last_page': (flags & 1) != 0,
      'status_color': (flags >> 1) & 7,
      'voltage_decimals': decimals(4),
      'current_decimals': decimals(6),
    });
    final result = GoBoardSettings._(v);
    result.validate();
    return result;
  }
  void validate() {
    for (final f in fields) {
      final value = values[f.$1];
      if (value is! int || value < f.$3 || value > f.$4) {
        throw FormatException('${f.$2} must be ${f.$3}–${f.$4}.');
      }
    }
    if (values['flags'] is! int ||
        values['flags'] < 0 ||
        values['flags'] > 255 ||
        values['output_persistence'] is! int ||
        values['output_persistence'] < 0 ||
        values['output_persistence'] > 15 ||
        values['save_last_page'] is! bool ||
        (values['stream_period_ms'] != 0 && values['stream_period_ms'] < 100)) {
      throw const FormatException(
        'Invalid settings flags or stream interval (0 or at least 100 ms).',
      );
    }
  }

  String command() {
    validate();
    return 'settings set ${[values['flags'], values['light_mode'], values['on_brightness'], values['off_brightness'], values['display_brightness'], values['display_closed_brightness'], values['status_brightness'], values['output_persistence'], values['stream_period_ms'], values['default_page'], values['save_last_page'] ? 'true' : 'false', values['status_color'], values['voltage_decimals'], values['current_decimals'], values['motion_exit_hold_seconds'], values['motion_break_ms'], values['relay_restore_mask']].join(' ')}';
  }
}

String nativeArgument(String value) {
  if (value.isEmpty ||
      value.length > 256 ||
      value.codeUnits.any((v) => v < 32 || v == 127)) {
    throw const FormatException(
      'Use a nonempty name without control characters.',
    );
  }
  return jsonEncode(value);
}

String melodyPlayCommand(String name, int repeats) {
  if (repeats < 1 || repeats > 20) {
    throw const FormatException('Repeats must be 1–20.');
  }
  return 'melody play ${nativeArgument(name)} $repeats';
}

bool validNativeName(String value, int maximum) =>
    value.isNotEmpty &&
    value.length <= maximum &&
    value.trim() == value &&
    value.codeUnits.every((n) => n >= 32 && n <= 126);
String boardNameCommand(String value) {
  if (!validNativeName(value, 8)) {
    throw const FormatException(
      'Board name must be 1–8 printable ASCII characters without outer spaces.',
    );
  }
  return 'board name set ${nativeArgument(value)}';
}
